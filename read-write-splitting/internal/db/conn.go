package db

import (
	"fmt"
	"log"
	"read-write-splitting/internal/config"
	"sync/atomic"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DBPool 数据库连接池
type DBPool struct {
	master     *gorm.DB         // 主库连接
	slaves     []*gorm.DB       // 从库连接列表
	slaveCount int32            // 从库数量
	current    int32            // 当前使用的从库索引，用于轮询
	config     *config.DBConfig // 数据库配置
}

// NewDBPool 创建新的数据库连接池
func NewDBPool(config *config.DBConfig) (*DBPool, error) {
	pool := &DBPool{
		config: config,
	}

	// 初始化主库连接
	masterDB, err := connectDB(config.Master)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to master DB: %w", err)
	}
	pool.master = masterDB

	// 初始化从库连接
	pool.slaves = make([]*gorm.DB, 0, len(config.Slaves))
	for i, slaveConfig := range config.Slaves {
		slaveDB, err := connectDB(slaveConfig)
		if err != nil {
			log.Printf("failed to connect to slave DB #%d: %v", i, err)
			continue
		}
		pool.slaves = append(pool.slaves, slaveDB)
	}

	pool.slaveCount = int32(len(pool.slaves))
	if pool.slaveCount == 0 {
		log.Println("Warning: no slave DBs available, using master DB for all operations")
	}

	return pool, nil
}

// 连接到单个数据库
func connectDB(dbInfo config.DBInfo) (*gorm.DB, error) {
	dsn := dbInfo.GetDSN()

	// 配置GORM
	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info), // 开启详细日志
	}

	db, err := gorm.Open(mysql.Open(dsn), gormConfig)
	if err != nil {
		return nil, err
	}

	// 配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(10)  // 最大空闲连接数
	sqlDB.SetMaxOpenConns(100) // 最大打开连接数

	return db, nil
}

// Master 获取主库连接
func (p *DBPool) Master() *gorm.DB {
	return p.master
}

// Slave 获取从库连接（使用简单轮询策略）
func (p *DBPool) Slave() *gorm.DB {
	// 如果没有从库，则返回主库
	if p.slaveCount == 0 {
		return p.master
	}

	// 简单的轮询策略选择从库
	current := atomic.AddInt32(&p.current, 1) % p.slaveCount
	return p.slaves[current]
}

// Close 关闭所有数据库连接
func (p *DBPool) Close() {
	if p.master != nil {
		sqlDB, _ := p.master.DB()
		sqlDB.Close()
	}

	for _, slave := range p.slaves {
		sqlDB, _ := slave.DB()
		sqlDB.Close()
	}
}
