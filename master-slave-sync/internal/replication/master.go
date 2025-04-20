package replication

import (
	"fmt"
	"log"
	"sync"
	"time"

	"master-slave-sync/internal/config"
	"master-slave-sync/internal/storage"
)

// Master 主节点管理器，负责处理写操作并维护binlog
type Master struct {
	db          *storage.DB          // 数据库连接
	binlog      *Binlog              // binlog管理器
	semiSync    *SemiSync            // 半同步复制器
	config      *config.MasterConfig // 主节点配置
	slaveInfos  map[string]SlaveInfo // 从节点信息表
	startTime   time.Time            // 启动时间
	totalWrites int                  // 总写入次数
	mu          sync.RWMutex         // 并发控制锁
}

// SlaveInfo 存储从节点信息
type SlaveInfo struct {
	ID              string    // 从节点ID
	Host            string    // 主机地址
	Port            int       // 端口号
	LastSeen        time.Time // 最后一次心跳时间
	CurrentPosition uint64    // 当前同步位置
}

// MasterStats 主节点统计信息
type MasterStats struct {
	BinlogPosition  uint64         // 当前binlog位置
	ConnectedSlaves int            // 已连接从节点数量
	SemiSyncStatus  SemiSyncStatus // 半同步状态
	TotalWrites     int            // 总写入次数
	UptimeSeconds   int64          // 运行时间(秒)
	SlaveInfos      []SlaveInfo    // 从节点详细信息
}

// NewMaster 创建并初始化主节点
func NewMaster(cfg *config.SyncConfig) (*Master, error) {
	// 连接数据库
	db, err := storage.NewDB(cfg.Master.GetDSN(), "master")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to master database: %w", err)
	}

	// 创建binlog管理器
	binlog := NewBinlog()

	// 创建半同步复制器
	semiSync := NewSemiSync(&cfg.SemiSync)

	return &Master{
		db:          db,
		binlog:      binlog,
		semiSync:    semiSync,
		config:      &cfg.Master,
		slaveInfos:  make(map[string]SlaveInfo),
		startTime:   time.Now(),
		totalWrites: 0,
		mu:          sync.RWMutex{},
	}, nil
}

// CreateRecord 创建记录并写入binlog
func (m *Master) CreateRecord(content string) (*storage.Record, error) {
	// 创建记录
	record, err := m.db.CreateRecord(content)
	if err != nil {
		return nil, fmt.Errorf("failed to create record: %w", err)
	}

	// 添加到binlog
	pos, err := m.binlog.AppendInsert(record)
	if err != nil {
		log.Printf("Warning: Failed to write to binlog: %v", err)
		// 虽然binlog失败，但数据已写入，所以继续执行
	}

	// 等待半同步确认（如果失败，降级为异步）
	status, err := m.semiSync.WaitForACK(pos)
	if err != nil {
		log.Printf("Semi-sync replication warning: %v, status: %s", err, status)
	}

	m.mu.Lock()
	m.totalWrites++
	m.mu.Unlock()

	return record, nil
}

// UpdateRecord 更新记录并写入binlog
func (m *Master) UpdateRecord(id uint, content string) error {
	// 先读取记录，确保存在
	record, err := m.db.GetRecord(id)
	if err != nil {
		return fmt.Errorf("record not found: %w", err)
	}

	// 更新记录
	err = m.db.UpdateRecord(id, content)
	if err != nil {
		return fmt.Errorf("failed to update record: %w", err)
	}

	// 更新record对象的内容（用于binlog）
	record.Content = content

	// 添加到binlog
	pos, err := m.binlog.AppendUpdate(record)
	if err != nil {
		log.Printf("Warning: Failed to write to binlog: %v", err)
		// 虽然binlog失败，但数据已更新，所以继续执行
	}

	// 等待半同步确认（如果失败，降级为异步）
	status, err := m.semiSync.WaitForACK(pos)
	if err != nil {
		log.Printf("Semi-sync replication warning: %v, status: %s", err, status)
	}

	m.mu.Lock()
	m.totalWrites++
	m.mu.Unlock()

	return nil
}

// DeleteRecord 删除记录并写入binlog
func (m *Master) DeleteRecord(id uint) error {
	// 先检查记录是否存在
	_, err := m.db.GetRecord(id)
	if err != nil {
		return fmt.Errorf("record not found: %w", err)
	}

	// 删除记录
	err = m.db.DeleteRecord(id)
	if err != nil {
		return fmt.Errorf("failed to delete record: %w", err)
	}

	// 添加到binlog
	pos, err := m.binlog.AppendDelete(id)
	if err != nil {
		log.Printf("Warning: Failed to write to binlog: %v", err)
		// 虽然binlog失败，但数据已删除，所以继续执行
	}

	// 等待半同步确认（如果失败，降级为异步）
	status, err := m.semiSync.WaitForACK(pos)
	if err != nil {
		log.Printf("Semi-sync replication warning: %v, status: %s", err, status)
	}

	m.mu.Lock()
	m.totalWrites++
	m.mu.Unlock()

	return nil
}

// GetBinlogEntries 获取指定位置之后的binlog条目（供从节点调用）
func (m *Master) GetBinlogEntries(fromPosition uint64) []BinlogEntry {
	return m.binlog.GetEntries(fromPosition)
}

// RecordSlaveACK 记录从节点确认信息
func (m *Master) RecordSlaveACK(slaveID string, position uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 更新从节点信息
	info, exists := m.slaveInfos[slaveID]
	if !exists {
		info = SlaveInfo{
			ID:              slaveID,
			CurrentPosition: 0,
		}
	}

	info.LastSeen = time.Now()
	if position > info.CurrentPosition {
		info.CurrentPosition = position
	}
	m.slaveInfos[slaveID] = info

	// 记录确认到半同步复制器
	m.semiSync.RecordACK(slaveID, position)
}

// RegisterSlave 注册新的从节点
func (m *Master) RegisterSlave(slaveID string, host string, port int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.slaveInfos[slaveID] = SlaveInfo{
		ID:              slaveID,
		Host:            host,
		Port:            port,
		LastSeen:        time.Now(),
		CurrentPosition: 0,
	}

	log.Printf("New slave registered: %s (%s:%d)", slaveID, host, port)
}

// GetCurrentBinlogPosition 获取当前binlog位置
func (m *Master) GetCurrentBinlogPosition() uint64 {
	return m.binlog.GetCurrentPosition()
}

// GetStats 获取主节点统计信息
func (m *Master) GetStats() MasterStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	slaves := make([]SlaveInfo, 0, len(m.slaveInfos))
	for _, info := range m.slaveInfos {
		slaves = append(slaves, info)
	}

	return MasterStats{
		BinlogPosition:  m.binlog.GetCurrentPosition(),
		ConnectedSlaves: len(m.slaveInfos),
		SemiSyncStatus:  m.semiSync.GetStatus(),
		TotalWrites:     m.totalWrites,
		UptimeSeconds:   int64(time.Since(m.startTime).Seconds()),
		SlaveInfos:      slaves,
	}
}

// Close 关闭主节点连接
func (m *Master) Close() error {
	// 清理所有资源
	err := m.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	log.Printf("Master node shut down properly")
	return nil
}

// GetDB 获取数据库连接
func (m *Master) GetDB() *storage.DB {
	return m.db
}
