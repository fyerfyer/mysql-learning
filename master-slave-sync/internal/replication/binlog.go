package replication

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"master-slave-sync/internal/storage"
)

// 操作类型枚举
const (
	OpInsert = "INSERT"
	OpUpdate = "UPDATE"
	OpDelete = "DELETE"
)

// BinlogEntry 表示一个简化的binlog条目
type BinlogEntry struct {
	ID        uint64    `json:"id"`         // binlog唯一标识符
	Operation string    `json:"operation"`  // 操作类型：INSERT, UPDATE, DELETE
	TableName string    `json:"table_name"` // 表名
	RecordID  uint      `json:"record_id"`  // 被操作记录的ID
	Data      []byte    `json:"data"`       // 序列化后的记录数据
	Timestamp time.Time `json:"timestamp"`  // 操作时间
}

// Binlog 简化的binlog管理器
type Binlog struct {
	entries  []BinlogEntry // binlog条目集合
	position uint64        // 当前位置
	mu       sync.RWMutex  // 并发控制锁
}

// NewBinlog 创建一个新的binlog管理器
func NewBinlog() *Binlog {
	return &Binlog{
		entries:  make([]BinlogEntry, 0),
		position: 0,
	}
}

// AppendInsert 添加一条插入操作的binlog
func (b *Binlog) AppendInsert(record *storage.Record) (uint64, error) {
	return b.append(OpInsert, record)
}

// AppendUpdate 添加一条更新操作的binlog
func (b *Binlog) AppendUpdate(record *storage.Record) (uint64, error) {
	return b.append(OpUpdate, record)
}

// AppendDelete 添加一条删除操作的binlog
func (b *Binlog) AppendDelete(recordID uint) (uint64, error) {
	// 对于删除操作，我们只需要记录ID
	record := &storage.Record{ID: recordID}
	return b.append(OpDelete, record)
}

// append 通用的添加binlog条目方法
func (b *Binlog) append(operation string, record *storage.Record) (uint64, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize record: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.position++
	entry := BinlogEntry{
		ID:        b.position,
		Operation: operation,
		TableName: "records", // 我们只有一个表
		RecordID:  record.ID,
		Data:      data,
		Timestamp: time.Now(),
	}

	b.entries = append(b.entries, entry)
	return b.position, nil
}

// GetEntries 获取指定位置之后的所有binlog条目
func (b *Binlog) GetEntries(fromPosition uint64) []BinlogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []BinlogEntry
	for _, entry := range b.entries {
		if entry.ID > fromPosition {
			result = append(result, entry)
		}
	}
	return result
}

// GetCurrentPosition 获取当前binlog位置
func (b *Binlog) GetCurrentPosition() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.position
}

// ApplyEntry 应用binlog条目到从库
func ApplyEntry(db *storage.DB, entry BinlogEntry) error {
	switch entry.Operation {
	case OpInsert:
		var record storage.Record
		if err := json.Unmarshal(entry.Data, &record); err != nil {
			return fmt.Errorf("failed to deserialize record: %w", err)
		}
		// 我们需要绕过普通的创建方法，因为它有主节点检查
		result := db.GetConnection().Create(&record)
		if result.Error != nil {
			return fmt.Errorf("failed to apply INSERT: %w", result.Error)
		}

	case OpUpdate:
		var record storage.Record
		if err := json.Unmarshal(entry.Data, &record); err != nil {
			return fmt.Errorf("failed to deserialize record: %w", err)
		}
		// 直接更新记录的内容
		result := db.GetConnection().Model(&storage.Record{}).
			Where("id = ?", record.ID).
			Update("content", record.Content)
		if result.Error != nil {
			return fmt.Errorf("failed to apply UPDATE: %w", result.Error)
		}

	case OpDelete:
		// 直接删除指定ID的记录
		result := db.GetConnection().Delete(&storage.Record{}, entry.RecordID)
		if result.Error != nil {
			return fmt.Errorf("failed to apply DELETE: %w", result.Error)
		}

	default:
		return fmt.Errorf("unknown operation: %s", entry.Operation)
	}

	return nil
}
