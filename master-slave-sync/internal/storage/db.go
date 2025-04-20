package storage

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 数据库操作封装
type DB struct {
	conn *gorm.DB
	role string // "master" 或 "slave"
}

// Record 示例数据模型，用于演示主从同步
type Record struct {
	ID        uint      `gorm:"primarykey"`
	Content   string    `gorm:"size:255"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// NewDB 创建数据库连接
func NewDB(dsn string, role string) (*DB, error) {
	// 配置GORM日志
	gormLogger := logger.New(
		log.New(log.Writer(), "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Info,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

	// 连接数据库
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	// 自动迁移模式
	err = db.AutoMigrate(&Record{})
	if err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &DB{
		conn: db,
		role: role,
	}, nil
}

// CreateRecord 创建新记录（仅主节点支持）
func (db *DB) CreateRecord(content string) (*Record, error) {
	if db.role != "master" {
		return nil, fmt.Errorf("write operations not allowed on slave node")
	}

	record := &Record{
		Content: content,
	}

	result := db.conn.Create(record)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to create record: %w", result.Error)
	}

	return record, nil
}

// GetRecord 获取指定ID的记录
func (db *DB) GetRecord(id uint) (*Record, error) {
	var record Record
	result := db.conn.First(&record, id)
	if result.Error != nil {
		return nil, fmt.Errorf("record not found: %w", result.Error)
	}
	return &record, nil
}

// ListRecords 获取所有记录
func (db *DB) ListRecords() ([]Record, error) {
	var records []Record
	result := db.conn.Find(&records)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to list records: %w", result.Error)
	}
	return records, nil
}

// UpdateRecord 更新记录（仅主节点支持）
func (db *DB) UpdateRecord(id uint, content string) error {
	if db.role != "master" {
		return fmt.Errorf("write operations not allowed on slave node")
	}

	result := db.conn.Model(&Record{}).Where("id = ?", id).Update("content", content)
	if result.Error != nil {
		return fmt.Errorf("failed to update record: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("record not found")
	}
	return nil
}

// DeleteRecord 删除记录（仅主节点支持）
func (db *DB) DeleteRecord(id uint) error {
	if db.role != "master" {
		return fmt.Errorf("write operations not allowed on slave node")
	}

	result := db.conn.Delete(&Record{}, id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete record: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("record not found")
	}
	return nil
}

// Close 关闭数据库连接
func (db *DB) Close() error {
	sqlDB, err := db.conn.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetConnection 返回底层数据库连接（仅内部使用）
func (db *DB) GetConnection() *gorm.DB {
	return db.conn
}
