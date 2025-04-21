package examples

import (
	"distribute-tx/internal/config"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"distribute-tx/internal/coordinator"
	"distribute-tx/internal/db"
	"distribute-tx/internal/model"
	"distribute-tx/internal/participant"
)

// FailureScenarioTransaction 演示分布式事务中的失败场景及处理
// 包括库存不足、支付失败等情况
func FailureScenarioTransaction() {
	// 步骤1: 创建数据库连接管理器
	dbManager := db.NewDBConnectionManager()
	defer dbManager.Close()

	// 步骤2: 连接到各个服务的数据库
	dbConfig := config.DefaultDBConfig

	// 连接协调者和各服务数据库
	err := dbManager.ConnectDB("coordinator", dbConfig)
	if err != nil {
		log.Fatalf("Failed to connect to coordinator database: %v", err)
	}

	services := []string{"order_service", "inventory_service", "payment_service", "account_service"}
	for _, service := range services {
		if err := dbManager.ConnectDB(service, dbConfig); err != nil {
			log.Fatalf("Failed to connect to %s database: %v", service, err)
		}
	}

	// 步骤3: 初始化数据库表
	if err := dbManager.InitTransactionTables("coordinator"); err != nil {
		log.Fatalf("Failed to initialize transaction tables: %v", err)
	}
	if err := dbManager.InitBusinessTables(); err != nil {
		log.Fatalf("Failed to initialize business tables: %v", err)
	}

	// 步骤4: 准备测试数据
	prepareTestData(dbManager)

	// 步骤5: 演示三种不同的失败场景
	fmt.Println("\n=== FAILURE SCENARIO 1: INSUFFICIENT INVENTORY ===")
	insufficientInventoryScenario(dbManager)

	fmt.Println("\n=== FAILURE SCENARIO 2: PAYMENT PROCESSING ERROR ===")
	paymentFailureScenario(dbManager)

	fmt.Println("\n=== FAILURE SCENARIO 3: COMMIT PHASE FAILURE ===")
	commitFailureScenario(dbManager)
}

// prepareTestData 准备测试所需的数据
func prepareTestData(dbManager *db.DBConnectionManager) {
	// 准备库存数据
	inventoryDB, _ := dbManager.GetDB("inventory_service")
	inventoryDB.Exec("TRUNCATE TABLE inventory")

	inventory1 := model.Inventory{
		ProductID:   "product1",
		ProductName: "Low Stock Item",
		Quantity:    1, // 低库存商品
		Reserved:    0,
	}

	inventory2 := model.Inventory{
		ProductID:   "product2",
		ProductName: "Regular Stock Item",
		Quantity:    10,
		Reserved:    0,
	}

	inventoryDB.Create(&inventory1)
	inventoryDB.Create(&inventory2)

	// 准备账户数据
	accountDB, _ := dbManager.GetDB("account_service")
	accountDB.Exec("TRUNCATE TABLE accounts")

	account1 := model.Account{
		UserID:      "user1",
		Balance:     50.0, // 余额不足
		AccountType: "standard",
		Status:      "active",
	}

	account2 := model.Account{
		UserID:      "user2",
		Balance:     1000.0, // 余额充足
		AccountType: "premium",
		Status:      "active",
	}

	accountDB.Create(&account1)
	accountDB.Create(&account2)

	fmt.Println("Test data prepared successfully")
}

// insufficientInventoryScenario 演示库存不足导致的事务失败场景
func insufficientInventoryScenario(dbManager *db.DBConnectionManager) {
	// 创建事务协调者和参与者
	txCoordinator := coordinator.NewCoordinator("coordinator", dbManager, 10*time.Second)

	orderService := participant.NewParticipant("order_service", "order_service", dbManager)
	inventoryService := participant.NewParticipant("inventory_service", "inventory_service", dbManager)
	paymentService := participant.NewParticipant("payment_service", "payment_service", dbManager)

	txCoordinator.RegisterParticipant(orderService)
	txCoordinator.RegisterParticipant(inventoryService)
	txCoordinator.RegisterParticipant(paymentService)

	// 订单信息
	userID := "user2"
	productID := "product1" // 库存只有1个
	orderAmount := 200.0
	quantity := 5 // 尝试购买5个，超过库存
	orderNo := fmt.Sprintf("ORD-INS-%s", uuid.New().String()[0:8])

	fmt.Printf("Starting transaction with insufficient inventory (trying to order %d items, only 1 available)\n", quantity)

	// 开始事务
	xid, err := txCoordinator.Begin("Create order with insufficient inventory")
	if err != nil {
		log.Fatalf("Failed to begin transaction: %v", err)
	}

	// 定义参与者动作
	participantActions := map[string]func(*gorm.DB) error{
		// 订单服务动作
		"order_service": func(tx *gorm.DB) error {
			order := model.Order{
				OrderNo:     orderNo,
				UserID:      userID,
				TotalAmount: orderAmount,
				Status:      "pending",
			}
			if err := tx.Create(&order).Error; err != nil {
				return fmt.Errorf("failed to create order: %w", err)
			}

			orderItem := model.OrderItem{
				OrderNo:   orderNo,
				ProductID: productID,
				Quantity:  quantity,
				UnitPrice: orderAmount / float64(quantity),
			}
			if err := tx.Create(&orderItem).Error; err != nil {
				return fmt.Errorf("failed to create order item: %w", err)
			}

			fmt.Println("Order created in prepare phase")
			return nil
		},

		// 库存服务动作
		"inventory_service": func(tx *gorm.DB) error {
			var inventory model.Inventory
			if err := tx.Where("product_id = ?", productID).First(&inventory).Error; err != nil {
				return fmt.Errorf("product not found: %w", err)
			}

			// 检查库存是否充足
			if inventory.Quantity < quantity {
				return fmt.Errorf("insufficient inventory for product %s, available: %d, required: %d",
					productID, inventory.Quantity, quantity)
			}

			// 减少库存
			inventory.Quantity -= quantity
			inventory.Reserved += quantity

			if err := tx.Save(&inventory).Error; err != nil {
				return fmt.Errorf("failed to update inventory: %w", err)
			}

			return nil
		},

		// 支付服务动作
		"payment_service": func(tx *gorm.DB) error {
			payment := model.PaymentRecord{
				PaymentID: fmt.Sprintf("PAY-%s", uuid.New().String()[0:8]),
				OrderNo:   orderNo,
				UserID:    userID,
				Amount:    orderAmount,
				Status:    "processing",
			}

			if err := tx.Create(&payment).Error; err != nil {
				return fmt.Errorf("failed to create payment record: %w", err)
			}

			return nil
		},
	}

	// 执行准备阶段
	fmt.Println("Executing prepare phase...")
	prepared, err := txCoordinator.Prepare(xid, participantActions)

	// 预期准备阶段会失败，因为库存不足
	if err != nil {
		fmt.Printf("Prepare phase failed as expected: %v\n", err)
		fmt.Println("Executing rollback...")

		_, rollbackErr := txCoordinator.Rollback(xid)
		if rollbackErr != nil {
			fmt.Printf("Rollback error: %v\n", rollbackErr)
		} else {
			fmt.Println("Transaction rolled back successfully")
		}

		// 检查库存未被修改
		inventoryDB, _ := dbManager.GetDB("inventory_service")
		var inventory model.Inventory
		inventoryDB.Where("product_id = ?", productID).First(&inventory)
		fmt.Printf("Inventory after rollback: Available: %d, Reserved: %d\n",
			inventory.Quantity, inventory.Reserved)
	} else if prepared {
		fmt.Println("ERROR: Transaction prepared successfully, expected failure!")
	}
}

// paymentFailureScenario 演示支付失败的场景
func paymentFailureScenario(dbManager *db.DBConnectionManager) {
	// 创建事务协调者和参与者
	txCoordinator := coordinator.NewCoordinator("coordinator", dbManager, 10*time.Second)

	orderService := participant.NewParticipant("order_service", "order_service", dbManager)
	inventoryService := participant.NewParticipant("inventory_service", "inventory_service", dbManager)
	paymentService := participant.NewParticipant("payment_service", "payment_service", dbManager)
	accountService := participant.NewParticipant("account_service", "account_service", dbManager)

	txCoordinator.RegisterParticipant(orderService)
	txCoordinator.RegisterParticipant(inventoryService)
	txCoordinator.RegisterParticipant(paymentService)
	txCoordinator.RegisterParticipant(accountService)

	// 订单信息
	userID := "user1" // 余额不足的用户
	productID := "product2"
	orderAmount := 100.0
	quantity := 1
	orderNo := fmt.Sprintf("ORD-PAY-%s", uuid.New().String()[0:8])

	fmt.Printf("Starting transaction with insufficient account balance (order amount: %.2f, account balance: 50.00)\n", orderAmount)

	// 开始事务
	xid, _ := txCoordinator.Begin("Create order with payment failure")

	// 定义参与者动作
	participantActions := map[string]func(*gorm.DB) error{
		"order_service": func(tx *gorm.DB) error {
			order := model.Order{
				OrderNo:     orderNo,
				UserID:      userID,
				TotalAmount: orderAmount,
				Status:      "pending",
			}
			tx.Create(&order)
			return nil
		},

		"inventory_service": func(tx *gorm.DB) error {
			var inventory model.Inventory
			tx.Where("product_id = ?", productID).First(&inventory)

			inventory.Quantity -= quantity
			inventory.Reserved += quantity
			tx.Save(&inventory)

			return nil
		},

		"account_service": func(tx *gorm.DB) error {
			var account model.Account
			if err := tx.Where("user_id = ?", userID).First(&account).Error; err != nil {
				return fmt.Errorf("account not found: %w", err)
			}

			// 检查余额是否足够
			if account.Balance < orderAmount {
				return fmt.Errorf("insufficient balance for user %s, available: %.2f, required: %.2f",
					userID, account.Balance, orderAmount)
			}

			return nil
		},

		"payment_service": func(tx *gorm.DB) error {
			payment := model.PaymentRecord{
				PaymentID: fmt.Sprintf("PAY-%s", uuid.New().String()[0:8]),
				OrderNo:   orderNo,
				UserID:    userID,
				Amount:    orderAmount,
				Status:    "processing",
			}
			tx.Create(&payment)
			return nil
		},
	}

	// 执行准备阶段
	fmt.Println("Executing prepare phase...")
	prepared, err := txCoordinator.Prepare(xid, participantActions)

	if err != nil {
		fmt.Printf("Prepare phase failed as expected: %v\n", err)
		fmt.Println("Executing rollback...")

		_, rollbackErr := txCoordinator.Rollback(xid)
		if rollbackErr != nil {
			fmt.Printf("Rollback error: %v\n", rollbackErr)
		} else {
			fmt.Println("Transaction rolled back successfully")
		}

		// 检查库存状态
		checkInventoryAfterRollback(dbManager, productID)
	} else if prepared {
		fmt.Println("ERROR: Transaction prepared successfully, expected failure!")
	}
}

// commitFailureScenario 演示提交阶段失败的场景
func commitFailureScenario(dbManager *db.DBConnectionManager) {
	// 创建事务协调者和参与者
	txCoordinator := coordinator.NewCoordinator("coordinator", dbManager, 10*time.Second)

	// 常规参与者
	orderService := participant.NewParticipant("order_service", "order_service", dbManager)
	inventoryService := participant.NewParticipant("inventory_service", "inventory_service", dbManager)

	// 模拟提交时会失败的参与者
	// 这里我们使用正常参与者，但在提交阶段前修改其LocalTx为nil来模拟失败
	paymentService := participant.NewParticipant("payment_service", "payment_service", dbManager)

	txCoordinator.RegisterParticipant(orderService)
	txCoordinator.RegisterParticipant(inventoryService)
	txCoordinator.RegisterParticipant(paymentService)

	// 订单信息
	userID := "user2"
	productID := "product2"
	orderAmount := 80.0
	quantity := 1
	orderNo := fmt.Sprintf("ORD-COM-%s", uuid.New().String()[0:8])

	fmt.Println("Starting transaction with commit phase failure simulation")

	// 开始事务
	xid, _ := txCoordinator.Begin("Create order with commit failure")

	// 定义参与者动作
	participantActions := map[string]func(*gorm.DB) error{
		"order_service": func(tx *gorm.DB) error {
			order := model.Order{
				OrderNo:     orderNo,
				UserID:      userID,
				TotalAmount: orderAmount,
				Status:      "pending",
			}
			tx.Create(&order)

			orderItem := model.OrderItem{
				OrderNo:   orderNo,
				ProductID: productID,
				Quantity:  quantity,
				UnitPrice: orderAmount,
			}
			tx.Create(&orderItem)

			fmt.Println("Order created in prepare phase")
			return nil
		},

		"inventory_service": func(tx *gorm.DB) error {
			var inventory model.Inventory
			tx.Where("product_id = ?", productID).First(&inventory)

			inventory.Quantity -= quantity
			inventory.Reserved += quantity
			tx.Save(&inventory)

			fmt.Println("Inventory updated in prepare phase")
			return nil
		},

		"payment_service": func(tx *gorm.DB) error {
			payment := model.PaymentRecord{
				PaymentID: fmt.Sprintf("PAY-%s", uuid.New().String()[0:8]),
				OrderNo:   orderNo,
				UserID:    userID,
				Amount:    orderAmount,
				Status:    "processing",
			}

			tx.Create(&payment)
			fmt.Println("Payment record created in prepare phase")
			return nil
		},
	}

	// 执行准备阶段
	fmt.Println("Executing prepare phase...")
	prepared, err := txCoordinator.Prepare(xid, participantActions)

	if err != nil {
		fmt.Printf("Prepare phase failed: %v\n", err)
		return
	}

	if prepared {
		fmt.Println("Prepare phase completed successfully")

		// 模拟支付服务在提交阶段失败（例如崩溃）
		// 通过清除LocalTx引用来模拟
		fmt.Println("Simulating payment service failure before commit...")
		paymentService.LocalTx = nil

		// 尝试提交
		fmt.Println("Executing commit phase...")
		committed, err := txCoordinator.Commit(xid)

		if err != nil {
			fmt.Printf("Commit failed as expected: %v\n", err)

			// 在这个场景下，我们需要手动处理剩余的提交或回滚操作
			// 在实际系统中，这通常由恢复服务处理
			fmt.Println("This would require manual recovery or an automatic recovery process")

			// 显示当前状态
			showTransactionStatus(dbManager, xid)
		} else if committed {
			fmt.Println("ERROR: Transaction committed successfully despite simulated failure!")
		}
	}
}

// checkInventoryAfterRollback 检查回滚后的库存状态
func checkInventoryAfterRollback(dbManager *db.DBConnectionManager, productID string) {
	inventoryDB, _ := dbManager.GetDB("inventory_service")
	var inventory model.Inventory
	inventoryDB.Where("product_id = ?", productID).First(&inventory)
	fmt.Printf("Inventory after rollback: Available: %d, Reserved: %d\n",
		inventory.Quantity, inventory.Reserved)
}

// showTransactionStatus 显示事务的当前状态
func showTransactionStatus(dbManager *db.DBConnectionManager, xid string) {
	coordDB, _ := dbManager.GetDB("coordinator")

	var transaction model.Transaction
	coordDB.Where("xid = ?", xid).First(&transaction)

	fmt.Printf("Transaction status: %s\n", transaction.Status)

	var participants []model.TransactionParticipant
	coordDB.Where("xid = ?", xid).Find(&participants)

	fmt.Println("Participants status:")
	for _, p := range participants {
		fmt.Printf("- %s: %s\n", p.Name, p.Status)
	}
}
