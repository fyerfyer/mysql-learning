package main

import (
	"ha-switcher/internal/api"
	"ha-switcher/internal/config"
	"ha-switcher/internal/db"
	"ha-switcher/internal/monitor"
	"ha-switcher/internal/switcher"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// 设置日志格式
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("Starting MySQL HA Switcher")

	// 加载配置
	cfg := config.DefaultConfig()
	log.Printf("Loaded configuration: Master=%s:%d, Slave=%s:%d",
		cfg.MasterDB.Host, cfg.MasterDB.Port,
		cfg.SlaveDB.Host, cfg.SlaveDB.Port)

	// 创建数据库管理器
	dbManager, err := db.NewDBManager(cfg)
	if err != nil {
		log.Fatalf("Failed to create database manager: %v", err)
	}
	log.Println("Database manager initialized successfully")

	// 创建切换器
	sw := switcher.NewSwitcher(dbManager, cfg)
	log.Println("Switcher initialized successfully")

	apiServer := api.NewServer(dbManager, sw, 8080)
	go func() {
		if err := apiServer.Start(); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()
	log.Println("HTTP API server started on port 8080")

	// 创建并启动健康检查器
	healthChecker := monitor.NewHealthChecker(dbManager, cfg, sw)
	err = healthChecker.Start()
	if err != nil {
		log.Fatalf("Failed to start health checker: %v", err)
	}
	log.Println("Health checker started successfully")

	// 设置信号处理，以便优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 等待终止信号
	<-sigChan
	log.Println("Received termination signal, shutting down...")

	// 停止健康检查
	healthChecker.Stop()
	log.Println("Health checker stopped")

	// 关闭前等待一小段时间确保资源清理
	time.Sleep(500 * time.Millisecond)
	log.Println("MySQL HA Switcher shutdown complete")
}
