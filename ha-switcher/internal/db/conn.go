package db

import (
	"context"
	"fmt"
	"ha-switcher/internal/config"
	"log"
	"sync"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// DBManager 管理数据库连接并处理主从切换
type DBManager struct {
	masterDB        *gorm.DB       // 主库连接
	slaveDB         *gorm.DB       // 从库连接
	config          *config.Config // 配置信息
	mu              sync.RWMutex   // 读写锁保护并发访问
	isMasterActive  bool           // 主库是否活跃
	simulateFailure bool           // 模拟故障切换
}

// NewDBManager 创建一个新的数据库管理器
func NewDBManager(cfg *config.Config) (*DBManager, error) {
	// 创建管理器实例
	manager := &DBManager{
		config:         cfg,
		isMasterActive: true,
	}

	// 初始化数据库连接
	err := manager.initConnections()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database connections: %w", err)
	}

	return manager, nil
}

// SetSimulateFailure 设置故障模拟模式
func (m *DBManager) SetSimulateFailure(simulate bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.simulateFailure = simulate
	if simulate {
		log.Println("Failure simulation mode activated - master will be reported as unhealthy")
	} else {
		log.Println("Failure simulation mode deactivated - master health check returns to normal")
	}
}

// initConnections 初始化主从数据库连接
func (m *DBManager) initConnections() error {
	// 连接主库
	masterDB, err := connectToDB(m.config.MasterDB)
	if err != nil {
		return fmt.Errorf("failed to connect to master database: %w", err)
	}
	m.masterDB = masterDB

	// 连接从库
	slaveDB, err := connectToDB(m.config.SlaveDB)
	if err != nil {
		return fmt.Errorf("failed to connect to slave database: %w", err)
	}
	m.slaveDB = slaveDB

	log.Println("Successfully connected to master and slave databases")
	return nil
}

// connectToDB 使用给定的配置连接到数据库
func connectToDB(dbConfig config.DBConfig) (*gorm.DB, error) {
	// 创建DSN字符串
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		dbConfig.Username,
		dbConfig.Password,
		dbConfig.Host,
		dbConfig.Port,
		dbConfig.Database)

	// 配置连接
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// 设置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 配置连接池参数
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

// GetDB 获取当前活跃的数据库连接
func (m *DBManager) GetDB() *gorm.DB {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.isMasterActive {
		return m.masterDB
	}
	return m.slaveDB
}

// SwitchToSlave 将活跃连接从主库切换到从库
func (m *DBManager) SwitchToSlave() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isMasterActive {
		log.Println("Switching from master to slave database")
		m.isMasterActive = false
	}
}

// CheckMasterHealth 检查主库健康状态
func (m *DBManager) CheckMasterHealth() bool {
	m.mu.RLock()
	if m.simulateFailure {
		m.mu.RUnlock()
		log.Println("Simulating master database failure")
		return false
	}
	m.mu.RUnlock()

	// 原有的健康检查逻辑继续保持不变
	result := &struct{ Value int }{}

	ctx, cancel := context.WithTimeout(context.Background(), m.config.HealthCheckTimeout)
	defer cancel()

	err := m.masterDB.WithContext(ctx).Raw("SELECT 1 as value").Scan(result).Error
	if err != nil {
		log.Printf("Master health check failed: %v", err)
		return false
	}

	if result.Value != 1 {
		log.Println("Master health check failed: unexpected result")
		return false
	}

	return true
}
