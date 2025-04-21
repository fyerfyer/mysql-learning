package db

import (
	"distribute-tx/internal/config"
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"distribute-tx/internal/model"
)

// DBConnectionManager 管理分布式事务中的多个数据库连接
type DBConnectionManager struct {
	DBs map[string]*gorm.DB // 数据库连接映射，键为服务名称
}

// NewDBConnectionManager 创建新的数据库连接管理器
func NewDBConnectionManager() *DBConnectionManager {
	return &DBConnectionManager{
		DBs: make(map[string]*gorm.DB),
	}
}

// ConnectDB 连接到指定的数据库并将其添加到管理器中
func (m *DBConnectionManager) ConnectDB(serviceName string, config config.DBConfig) error {
	// 构建DSN连接字符串
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		config.User, config.Password, config.Host, config.Port, config.DBName)

	// 配置GORM
	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	}

	// 连接数据库
	db, err := gorm.Open(mysql.Open(dsn), gormConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to database %s: %w", serviceName, err)
	}

	// 配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 保存连接
	m.DBs[serviceName] = db

	return nil
}

// GetDB 获取指定服务名称的数据库连接
func (m *DBConnectionManager) GetDB(serviceName string) (*gorm.DB, error) {
	db, exists := m.DBs[serviceName]
	if !exists {
		return nil, fmt.Errorf("no database connection found for service: %s", serviceName)
	}
	return db, nil
}

// InitTransactionTables 在协调者数据库中初始化事务相关表
func (m *DBConnectionManager) InitTransactionTables(coordinatorService string) error {
	db, err := m.GetDB(coordinatorService)
	if err != nil {
		return err
	}

	// 自动创建事务相关表
	if err := db.AutoMigrate(&model.Transaction{}, &model.TransactionParticipant{}); err != nil {
		return fmt.Errorf("failed to create transaction tables: %w", err)
	}

	return nil
}

// InitBusinessTables 在各服务数据库中初始化业务表
func (m *DBConnectionManager) InitBusinessTables() error {
	// 在账户服务数据库中创建账户表
	accountDB, err := m.GetDB("account_service")
	if err == nil {
		if err := accountDB.AutoMigrate(&model.Account{}); err != nil {
			return fmt.Errorf("failed to create account tables: %w", err)
		}
	}

	// 在订单服务数据库中创建订单相关表
	orderDB, err := m.GetDB("order_service")
	if err == nil {
		if err := orderDB.AutoMigrate(&model.Order{}, &model.OrderItem{}); err != nil {
			return fmt.Errorf("failed to create order tables: %w", err)
		}
	}

	// 在库存服务数据库中创建库存表
	inventoryDB, err := m.GetDB("inventory_service")
	if err == nil {
		if err := inventoryDB.AutoMigrate(&model.Inventory{}); err != nil {
			return fmt.Errorf("failed to create inventory tables: %w", err)
		}
	}

	// 在支付服务数据库中创建支付记录表
	paymentDB, err := m.GetDB("payment_service")
	if err == nil {
		if err := paymentDB.AutoMigrate(&model.PaymentRecord{}); err != nil {
			return fmt.Errorf("failed to create payment tables: %w", err)
		}
	}

	return nil
}

// BeginTx 在指定服务上开始一个事务
func (m *DBConnectionManager) BeginTx(serviceName string) (*gorm.DB, error) {
	db, err := m.GetDB(serviceName)
	if err != nil {
		return nil, err
	}

	return db.Begin(), nil
}

// Close 关闭所有数据库连接
func (m *DBConnectionManager) Close() error {
	var lastErr error
	for serviceName, db := range m.DBs {
		sqlDB, err := db.DB()
		if err != nil {
			lastErr = fmt.Errorf("failed to get SQL DB instance for %s: %w", serviceName, err)
			continue
		}

		if err := sqlDB.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close connection to %s: %w", serviceName, err)
		}
	}

	return lastErr
}
