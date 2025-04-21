package monitor

import (
	"ha-switcher/internal/switcher"
	"log"
	"sync"
	"time"

	"ha-switcher/internal/config"
	"ha-switcher/internal/db"
)

// HealthChecker 负责监控主库健康状态并在必要时触发切换
type HealthChecker struct {
	dbManager *db.DBManager      // 数据库管理器
	switcher  *switcher.Switcher // 切换器
	config    *config.Config     // 配置信息
	failCount int                // 连续失败计数
	stopChan  chan struct{}      // 停止信号通道
	wg        sync.WaitGroup     // 等待组，用于优雅关闭
	mu        sync.Mutex         // 互斥锁，保护状态更改
	isRunning bool               // 监控器是否正在运行
}

// NewHealthChecker 创建一个新的健康监控器
func NewHealthChecker(dbManager *db.DBManager, cfg *config.Config, sw *switcher.Switcher) *HealthChecker {
	return &HealthChecker{
		dbManager: dbManager,
		switcher:  sw,
		config:    cfg,
		failCount: 0,
		stopChan:  make(chan struct{}),
	}
}

// Start 启动健康监控
func (hc *HealthChecker) Start() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if hc.isRunning {
		return nil // 已经在运行中，不需要再次启动
	}

	log.Println("Starting master database health checker")
	hc.isRunning = true
	hc.wg.Add(1)

	// 在后台goroutine中运行健康检查
	go hc.monitorHealth()

	return nil
}

// Stop 停止健康监控
func (hc *HealthChecker) Stop() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.isRunning {
		return // 已经停止，不需要操作
	}

	log.Println("Stopping master database health checker")
	close(hc.stopChan)
	hc.wg.Wait()
	hc.isRunning = false
}

// monitorHealth 持续监控主库健康状态的后台进程
func (hc *HealthChecker) monitorHealth() {
	defer hc.wg.Done()

	ticker := time.NewTicker(hc.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.stopChan:
			log.Println("Health checker stopping")
			return
		case <-ticker.C:
			hc.checkHealth()
		}
	}
}

// checkHealth 执行一次健康检查
func (hc *HealthChecker) checkHealth() {
	// 获取锁以保护状态修改
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// 检查主库健康状态
	isHealthy := hc.dbManager.CheckMasterHealth()

	if isHealthy {
		// 主库正常，重置失败计数
		if hc.failCount > 0 {
			log.Println("Master database recovered after failures")
			hc.failCount = 0
		}
	} else {
		// 主库异常，增加失败计数
		hc.failCount++
		log.Printf("Master database health check failed (%d/%d)", hc.failCount, hc.config.FailThreshold)

		// 如果连续失败次数达到阈值，触发切换
		if hc.failCount >= hc.config.FailThreshold {
			log.Printf("Failure threshold reached (%d). Triggering failover to slave", hc.config.FailThreshold)
			hc.switcher.SwitchToSlave()

			// 切换后重置计数器
			hc.failCount = 0
		}
	}
}
