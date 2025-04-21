package switcher

import (
	"log"
	"sync"
	"time"

	"ha-switcher/internal/config"
	"ha-switcher/internal/db"
)

// Switcher 负责处理主从切换的实际逻辑
type Switcher struct {
	dbManager    *db.DBManager  // 数据库连接管理器
	config       *config.Config // 配置信息
	mu           sync.Mutex     // 互斥锁，确保切换操作不会并发执行
	switchCount  int            // 记录切换次数
	lastSwitchAt time.Time      // 记录最后一次切换时间
}

// NewSwitcher 创建一个新的切换器实例
func NewSwitcher(dbManager *db.DBManager, cfg *config.Config) *Switcher {
	return &Switcher{
		dbManager: dbManager,
		config:    cfg,
	}
}

// SwitchToSlave 执行从主库到从库的切换操作
func (s *Switcher) SwitchToSlave() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Println("Starting failover process from master to slave database")

	// 将状态切换到从库
	s.dbManager.SwitchToSlave()

	// 更新切换统计信息
	s.switchCount++
	s.lastSwitchAt = time.Now()

	log.Printf("Failover completed. Active database is now the slave. Switch count: %d", s.switchCount)
	return nil
}

// GetSwitchStats 获取切换相关统计信息
func (s *Switcher) GetSwitchStats() (count int, lastSwitchTime time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.switchCount, s.lastSwitchAt
}

// PromoteSlave 提升从库为新主库（在实际环境中会执行相应的MySQL命令）
func (s *Switcher) PromoteSlave() error {
	// 这是一个模拟方法，在实际环境中，这里会执行类似以下操作：
	// 1. STOP SLAVE;
	// 2. RESET MASTER;
	// 3. 更新从库以指向新主库等

	log.Println("Promoting slave to master role (simulated)")

	// 在这个简化的实现中，我们只是记录操作
	return nil
}

// IsInSwitchingState 检查系统是否处于切换中状态
func (s *Switcher) IsInSwitchingState() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果最后切换时间在指定范围内，认为系统处于切换状态
	if s.lastSwitchAt.IsZero() {
		return false
	}

	// 假定切换过程持续1分钟
	return time.Since(s.lastSwitchAt) < time.Minute
}
