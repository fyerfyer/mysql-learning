package replication

import (
	"fmt"
	"sync"
	"time"

	"master-slave-sync/internal/config"
)

// SemiSyncStatus 表示半同步复制状态
type SemiSyncStatus string

const (
	StatusOK        SemiSyncStatus = "OK"        // 正常状态
	StatusTimeout   SemiSyncStatus = "TIMEOUT"   // 等待超时
	StatusDegraded  SemiSyncStatus = "DEGRADED"  // 降级模式（异步）
	StatusRecovered SemiSyncStatus = "RECOVERED" // 恢复半同步模式
)

// ACKResult 表示从节点确认结果
type ACKResult struct {
	SlaveID   string         // 从节点标识
	Position  uint64         // 确认的binlog位置
	Timestamp time.Time      // 确认时间
	Status    SemiSyncStatus // 确认状态
}

// SemiSync 半同步复制管理器
type SemiSync struct {
	config      *config.SemiSyncConfig    // 半同步配置
	acks        map[uint64][]ACKResult    // 每个binlog位置的确认记录
	waitCh      map[uint64]chan ACKResult // 等待确认的通道
	status      SemiSyncStatus            // 当前状态
	failureTime time.Time                 // 最后一次失败时间
	mu          sync.RWMutex              // 并发控制锁
}

// NewSemiSync 创建一个新的半同步复制管理器
func NewSemiSync(cfg *config.SemiSyncConfig) *SemiSync {
	return &SemiSync{
		config:      cfg,
		acks:        make(map[uint64][]ACKResult),
		waitCh:      make(map[uint64]chan ACKResult),
		status:      StatusOK,
		failureTime: time.Time{},
	}
}

// WaitForACK 等待从节点确认
// 返回确认状态和错误信息
func (s *SemiSync) WaitForACK(position uint64) (SemiSyncStatus, error) {
	// 创建等待通道
	s.mu.Lock()
	ch := make(chan ACKResult, s.config.MinSlaves)
	s.waitCh[position] = ch
	s.mu.Unlock()

	// 设置超时时间
	timeout := time.NewTimer(time.Duration(s.config.TimeoutMs) * time.Millisecond)
	defer timeout.Stop()

	// 计数器：已收到的确认数
	received := 0

	// 等待确认或超时
	for {
		select {
		case ack := <-ch:
			received++
			// 记录确认
			s.mu.Lock()
			if _, ok := s.acks[position]; !ok {
				s.acks[position] = make([]ACKResult, 0)
			}
			s.acks[position] = append(s.acks[position], ack)
			s.mu.Unlock()

			// 如果收到足够数量的确认，返回成功
			if received >= s.config.MinSlaves {
				return StatusOK, nil
			}

		case <-timeout.C:
			// 超时处理
			s.mu.Lock()
			s.status = StatusDegraded
			s.failureTime = time.Now()
			delete(s.waitCh, position)
			s.mu.Unlock()

			return StatusTimeout, fmt.Errorf("waiting for slave ACK timed out after %d ms", s.config.TimeoutMs)
		}
	}
}

// RecordACK 记录从节点的确认
func (s *SemiSync) RecordACK(slaveID string, position uint64) {
	ack := ACKResult{
		SlaveID:   slaveID,
		Position:  position,
		Timestamp: time.Now(),
		Status:    StatusOK,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果有等待此位置的通道，发送确认
	if ch, ok := s.waitCh[position]; ok {
		select {
		case ch <- ack:
			// 确认已发送
		default:
			// 通道已满或关闭，忽略
		}
	}

	// 存储确认记录
	if _, ok := s.acks[position]; !ok {
		s.acks[position] = make([]ACKResult, 0)
	}
	s.acks[position] = append(s.acks[position], ack)

	// 如果当前是降级状态，检查是否可以恢复
	if s.status == StatusDegraded {
		// 如果最后一次失败已经超过了超时时间的2倍，尝试恢复
		if time.Since(s.failureTime) > time.Duration(s.config.TimeoutMs*2)*time.Millisecond {
			s.status = StatusRecovered
		}
	}
}

// GetACKs 获取指定位置的确认记录
func (s *SemiSync) GetACKs(position uint64) []ACKResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if acks, ok := s.acks[position]; ok {
		result := make([]ACKResult, len(acks))
		copy(result, acks)
		return result
	}
	return nil
}

// GetStatus 获取当前半同步状态
func (s *SemiSync) GetStatus() SemiSyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// CleanupOldACKs 清理旧的确认记录（可定期调用）
func (s *SemiSync) CleanupOldACKs(beforePosition uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for pos := range s.acks {
		if pos < beforePosition {
			delete(s.acks, pos)
		}
	}

	// 清理等待通道
	for pos := range s.waitCh {
		if pos < beforePosition {
			close(s.waitCh[pos])
			delete(s.waitCh, pos)
		}
	}
}
