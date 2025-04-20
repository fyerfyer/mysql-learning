package replication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"master-slave-sync/internal/config"
	"master-slave-sync/internal/storage"
)

// Slave 从节点管理器，负责同步主节点的binlog并应用
type Slave struct {
	db              *storage.DB         // 数据库连接
	config          *config.SlaveConfig // 从节点配置
	slaveID         string              // 从节点唯一ID
	currentPosition uint64              // 当前同步到的位置
	syncInterval    time.Duration       // 同步间隔
	masterURL       string              // 主节点URL
	lastSyncTime    time.Time           // 上次同步时间
	syncCount       int                 // 同步次数统计
	appliedCount    int                 // 应用条目数统计
	isRunning       bool                // 同步是否在运行
	syncMutex       sync.Mutex          // 同步锁
	startTime       time.Time           // 启动时间
}

// SlaveStats 从节点统计信息
type SlaveStats struct {
	SlaveID         string    // 从节点ID
	CurrentPosition uint64    // 当前同步位置
	LastSyncTime    time.Time // 最后同步时间
	SyncCount       int       // 同步次数
	AppliedCount    int       // 应用条目数量
	IsRunning       bool      // 是否正在运行
	UptimeSeconds   int64     // 运行时间(秒)
}

// NewSlave 创建并初始化从节点
func NewSlave(cfg *config.SyncConfig, slaveID string) (*Slave, error) {
	// 连接数据库
	db, err := storage.NewDB(cfg.Slave.GetDSN(), "slave")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to slave database: %w", err)
	}

	masterURL := fmt.Sprintf("http://%s:%d", cfg.Slave.MasterHost, cfg.Slave.MasterPort)

	return &Slave{
		db:              db,
		config:          &cfg.Slave,
		slaveID:         slaveID,
		currentPosition: 0,
		syncInterval:    5 * time.Second, // 默认5秒同步一次
		masterURL:       masterURL,
		lastSyncTime:    time.Time{},
		syncCount:       0,
		appliedCount:    0,
		isRunning:       false,
		startTime:       time.Now(),
	}, nil
}

// StartSync 开始同步进程
func (s *Slave) StartSync() {
	if s.isRunning {
		log.Printf("Sync is already running")
		return
	}

	s.isRunning = true
	go s.syncLoop()

	// 注册到主节点
	err := s.registerWithMaster()
	if err != nil {
		log.Printf("Warning: Failed to register with master: %v", err)
		// 继续运行，后续同步时会自动注册
	}

	log.Printf("Slave %s started syncing from master at %s", s.slaveID, s.masterURL)
}

// StopSync 停止同步进程
func (s *Slave) StopSync() {
	s.syncMutex.Lock()
	defer s.syncMutex.Unlock()
	s.isRunning = false
	log.Printf("Slave %s stopped syncing", s.slaveID)
}

// syncLoop 同步循环，定期从主节点获取binlog并应用
func (s *Slave) syncLoop() {
	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	for {
		if !s.isRunning {
			return
		}

		err := s.syncOnce()
		if err != nil {
			log.Printf("Error during sync: %v", err)
			// 继续尝试，不要中断循环
		}

		<-ticker.C // 等待下一个同步周期
	}
}

// syncOnce 执行一次同步
func (s *Slave) syncOnce() error {
	s.syncMutex.Lock()
	defer s.syncMutex.Unlock()

	// 从主节点获取最新binlog条目
	entries, err := s.fetchBinlogEntries()
	if err != nil {
		return fmt.Errorf("failed to fetch binlog entries: %w", err)
	}

	if len(entries) == 0 {
		// 没有新条目，跳过
		return nil
	}

	// 应用每个条目
	for _, entry := range entries {
		err := ApplyEntry(s.db, entry)
		if err != nil {
			return fmt.Errorf("failed to apply binlog entry %d: %w", entry.ID, err)
		}

		// 更新位置并发送确认
		s.currentPosition = entry.ID
		s.appliedCount++

		// 向主节点发送ACK
		err = s.sendACKToMaster(entry.ID)
		if err != nil {
			log.Printf("Warning: Failed to send ACK for position %d: %v", entry.ID, err)
			// 继续处理，不中断应用流程
		}
	}

	s.syncCount++
	s.lastSyncTime = time.Now()
	log.Printf("Applied %d binlog entries, current position: %d", len(entries), s.currentPosition)

	return nil
}

// fetchBinlogEntries 从主节点获取binlog条目
func (s *Slave) fetchBinlogEntries() ([]BinlogEntry, error) {
	url := fmt.Sprintf("%s/api/binlog?position=%d&slave_id=%s",
		s.masterURL, s.currentPosition, s.slaveID)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to master: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("master returned error status: %s", resp.Status)
	}

	var entries []BinlogEntry
	err = json.NewDecoder(resp.Body).Decode(&entries)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return entries, nil
}

// sendACKToMaster 向主节点发送确认
func (s *Slave) sendACKToMaster(position uint64) error {
	url := fmt.Sprintf("%s/api/ack", s.masterURL)

	data := map[string]interface{}{
		"slave_id": s.slaveID,
		"position": position,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal ACK data: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send ACK: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("master returned error status for ACK: %s", resp.Status)
	}

	return nil
}

// registerWithMaster 向主节点注册从节点
func (s *Slave) registerWithMaster() error {
	url := fmt.Sprintf("%s/api/register_slave", s.masterURL)

	data := map[string]interface{}{
		"slave_id": s.slaveID,
		"host":     s.config.Host,
		"port":     s.config.APIPort,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal registration data: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to register with master: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("master returned error status for registration: %s", resp.Status)
	}

	log.Printf("Successfully registered with master")
	return nil
}

// GetStats 获取从节点统计信息
func (s *Slave) GetStats() SlaveStats {
	s.syncMutex.Lock()
	defer s.syncMutex.Unlock()

	return SlaveStats{
		SlaveID:         s.slaveID,
		CurrentPosition: s.currentPosition,
		LastSyncTime:    s.lastSyncTime,
		SyncCount:       s.syncCount,
		AppliedCount:    s.appliedCount,
		IsRunning:       s.isRunning,
		UptimeSeconds:   int64(time.Since(s.startTime).Seconds()),
	}
}

// GetCurrentPosition 获取当前同步位置
func (s *Slave) GetCurrentPosition() uint64 {
	s.syncMutex.Lock()
	defer s.syncMutex.Unlock()
	return s.currentPosition
}

// SetSyncInterval 设置同步间隔
func (s *Slave) SetSyncInterval(interval time.Duration) {
	s.syncMutex.Lock()
	defer s.syncMutex.Unlock()
	s.syncInterval = interval
	log.Printf("Sync interval set to %v", interval)
}

// Close 关闭从节点连接
func (s *Slave) Close() error {
	s.StopSync()
	err := s.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	log.Printf("Slave node shut down properly")
	return nil
}

// GetDB 获取数据库连接
func (s *Slave) GetDB() *storage.DB {
	return s.db
}
