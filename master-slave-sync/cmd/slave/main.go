package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"master-slave-sync/api"
	"master-slave-sync/internal/config"
	"master-slave-sync/internal/replication"
)

func main() {
	// 解析命令行参数
	var slaveID string

	flag.StringVar(&slaveID, "id", "slave1", "Unique slave identifier")
	flag.Parse()

	log.Printf("Starting slave node with ID: %s", slaveID)

	cfg := config.GetDefaultConfig()

	// 创建从节点
	slave, err := replication.NewSlave(cfg, slaveID)
	if err != nil {
		log.Fatalf("Failed to initialize slave: %v", err)
	}
	defer func() {
		if err := slave.Close(); err != nil {
			log.Printf("Error closing slave: %v", err)
		}
	}()

	// 创建API处理器
	handler := api.NewSlaveHandler(slave)
	mux := handler.SetupSlaveRoutes()

	// 创建HTTP服务器
	port := cfg.Slave.APIPort
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// 优雅关闭的通道
	idleConnsClosed := make(chan struct{})

	// 捕获中断信号
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Printf("Shutting down slave node...")

		// 停止同步
		slave.StopSync()

		// 给服务器10秒时间关闭
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		close(idleConnsClosed)
	}()

	// 启动同步
	slave.StartSync()
	log.Printf("Slave synchronization started")

	// 启动HTTP服务器
	log.Printf("Slave API server listening on port %d", port)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP server error: %v", err)
	}

	// 等待所有连接关闭
	<-idleConnsClosed
	log.Printf("Slave node shutdown complete")
}
