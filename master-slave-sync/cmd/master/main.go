package main

import (
	"context"
	"errors"
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
	log.Printf("Starting master node")

	cfg := config.GetDefaultConfig()

	// 创建主节点
	master, err := replication.NewMaster(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize master: %v", err)
	}
	defer func() {
		if err := master.Close(); err != nil {
			log.Printf("Error closing master: %v", err)
		}
	}()

	// 创建API处理器
	handler := api.NewMasterHandler(master)
	mux := handler.SetupMasterRoutes()

	// 创建HTTP服务器
	port := cfg.Master.APIPort
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

		log.Printf("Shutting down master node...")

		// 给服务器10秒时间关闭
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		close(idleConnsClosed)
	}()

	// 启动HTTP服务器
	log.Printf("Master API server listening on port %d", port)
	log.Printf("Binlog replication endpoint: http://localhost:%d/api/binlog", port)
	log.Printf("Semi-sync timeout: %dms, waiting for %d slaves",
		cfg.SemiSync.TimeoutMs, cfg.SemiSync.MinSlaves)

	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP server error: %v", err)
	}

	// 等待所有连接关闭
	<-idleConnsClosed
	log.Printf("Master node shutdown complete")
}
