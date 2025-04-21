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

// SimpleOrderTransaction 演示一个简单的订单创建分布式事务
// 包含三个参与者：订单服务、库存服务和支付服务
func SimpleOrderTransaction() {
	// 步骤1: 创建数据库连接管理器
	dbManager := db.NewDBConnectionManager()
	defer dbManager.Close()

	// 步骤2: 连接到各个服务的数据库
	dbConfig := config.DefaultDBConfig

	// 连接协调者数据库
	err := dbManager.ConnectDB("coordinator", dbConfig)
	if err != nil {
		log.Fatalf("Failed to connect to coordinator database: %v", err)
	}

	// 连接业务服务数据库
	services := []string{"order_service", "inventory_service", "payment_service"}
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

	// 步骤4: 向库存表中插入一些样本数据
	prepareInventoryData(dbManager)

	// 步骤5: 创建事务协调者和参与者
	txCoordinator := coordinator.NewCoordinator("coordinator", dbManager, 30*time.Second)

	// 创建参与者
	orderService := participant.NewParticipant("order_service", "order_service", dbManager)
	inventoryService := participant.NewParticipant("inventory_service", "inventory_service", dbManager)
	paymentService := participant.NewParticipant("payment_service", "payment_service", dbManager)

	// 注册参与者到协调者
	txCoordinator.RegisterParticipant(orderService)
	txCoordinator.RegisterParticipant(inventoryService)
	txCoordinator.RegisterParticipant(paymentService)

	// 步骤6: 开始分布式事务
	// 为简单起见，我们使用固定值，实际应用中应该使用真实数据
	userID := "user123"
	productID := "product456"
	orderAmount := 100.50
	quantity := 2

	// 创建订单号
	orderNo := fmt.Sprintf("ORD-%s", uuid.New().String()[0:8])

	fmt.Println("Starting distributed transaction for order creation...")
	xid, err := txCoordinator.Begin("Create order and process payment")
	if err != nil {
		log.Fatalf("Failed to begin transaction: %v", err)
	}

	// 步骤7: 定义各参与者的动作
	participantActions := map[string]func(*gorm.DB) error{
		// 订单服务动作：创建订单
		"order_service": func(tx *gorm.DB) error {
			// 创建订单
			order := model.Order{
				OrderNo:     orderNo,
				UserID:      userID,
				TotalAmount: orderAmount,
				Status:      "pending",
			}
			if err := tx.Create(&order).Error; err != nil {
				return fmt.Errorf("failed to create order: %w", err)
			}

			// 创建订单项
			orderItem := model.OrderItem{
				OrderNo:   orderNo,
				ProductID: productID,
				Quantity:  quantity,
				UnitPrice: orderAmount / float64(quantity),
			}
			if err := tx.Create(&orderItem).Error; err != nil {
				return fmt.Errorf("failed to create order item: %w", err)
			}

			fmt.Println("Order created successfully in prepare phase")
			return nil
		},

		// 库存服务动作：减少库存
		"inventory_service": func(tx *gorm.DB) error {
			// 查找商品库存
			var inventory model.Inventory
			if err := tx.Where("product_id = ?", productID).First(&inventory).Error; err != nil {
				return fmt.Errorf("product not found: %w", err)
			}

			// 检查库存是否充足
			if inventory.Quantity < quantity {
				return fmt.Errorf("insufficient inventory for product %s, available: %d, required: %d",
					productID, inventory.Quantity, quantity)
			}

			// 减少库存，增加预留数
			inventory.Quantity -= quantity
			inventory.Reserved += quantity

			if err := tx.Save(&inventory).Error; err != nil {
				return fmt.Errorf("failed to update inventory: %w", err)
			}

			fmt.Println("Inventory updated successfully in prepare phase")
			return nil
		},

		// 支付服务动作：记录支付
		"payment_service": func(tx *gorm.DB) error {
			// 创建支付记录
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

			fmt.Println("Payment record created successfully in prepare phase")
			return nil
		},
	}

	// 步骤8: 执行准备阶段
	fmt.Println("Executing prepare phase...")
	prepared, err := txCoordinator.Prepare(xid, participantActions)
	if err != nil {
		fmt.Printf("Prepare phase failed: %v\n", err)
		fmt.Println("Executing rollback...")
		_, rollbackErr := txCoordinator.Rollback(xid)
		if rollbackErr != nil {
			fmt.Printf("Rollback failed: %v\n", rollbackErr)
		} else {
			fmt.Println("Transaction rolled back successfully")
		}
		return
	}

	if prepared {
		fmt.Println("Prepare phase completed successfully")

		// 步骤9: 执行提交阶段
		fmt.Println("Executing commit phase...")
		committed, err := txCoordinator.Commit(xid)
		if err != nil {
			fmt.Printf("Commit failed: %v\n", err)
			return
		}

		if committed {
			fmt.Println("Transaction committed successfully")

			// 显示交易结果
			showTransactionResult(dbManager, orderNo, productID)
		}
	}
}

// prepareInventoryData 准备一些库存数据用于示例
func prepareInventoryData(dbManager *db.DBConnectionManager) {
	inventoryDB, err := dbManager.GetDB("inventory_service")
	if err != nil {
		log.Fatalf("Failed to get inventory database: %v", err)
		return
	}

	// 清空现有数据
	inventoryDB.Exec("TRUNCATE TABLE inventory")

	// 插入测试数据
	inventory := model.Inventory{
		ProductID:   "product456",
		ProductName: "测试商品",
		Quantity:    10,
		Reserved:    0,
	}

	if err := inventoryDB.Create(&inventory).Error; err != nil {
		log.Fatalf("Failed to create inventory data: %v", err)
	}
}

// showTransactionResult 显示事务执行后的结果
func showTransactionResult(dbManager *db.DBConnectionManager, orderNo string, productID string) {
	// 查询订单
	orderDB, _ := dbManager.GetDB("order_service")
	var order model.Order
	orderDB.Where("order_no = ?", orderNo).First(&order)
	fmt.Printf("Order status: %s, Total Amount: %.2f\n", order.Status, order.TotalAmount)

	// 查询库存
	inventoryDB, _ := dbManager.GetDB("inventory_service")
	var inventory model.Inventory
	inventoryDB.Where("product_id = ?", productID).First(&inventory)
	fmt.Printf("Product inventory: Available: %d, Reserved: %d\n", inventory.Quantity, inventory.Reserved)

	// 查询支付记录
	paymentDB, _ := dbManager.GetDB("payment_service")
	var payment model.PaymentRecord
	paymentDB.Where("order_no = ?", orderNo).First(&payment)
	fmt.Printf("Payment status: %s, Amount: %.2f\n", payment.Status, payment.Amount)
}
