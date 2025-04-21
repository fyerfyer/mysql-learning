package main

import (
	"distribute-tx/internal/config"
	"fmt"
	"log"
	"time"

	"distribute-tx/examples"
	"distribute-tx/internal/db"
)

func main() {
	fmt.Println("===============================================")
	fmt.Println("   Distributed Transaction Demo Application   ")
	fmt.Println("===============================================")
	fmt.Println()

	dbConfig := config.DefaultDBConfig

	fmt.Println("Testing database connection...")
	testDbManager := db.NewDBConnectionManager()
	err := testDbManager.ConnectDB("test", dbConfig)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	testDbManager.Close()
	fmt.Println("Database connection successful")
	fmt.Println()

	// 运行简单事务示例
	fmt.Println("\n===== SIMPLE TRANSACTION EXAMPLE =====")
	fmt.Println("Running simple order transaction scenario...")
	examples.SimpleOrderTransaction()

	fmt.Println("\nWaiting 3 seconds before running failure scenarios...")
	time.Sleep(3 * time.Second)

	// 运行失败场景示例
	fmt.Println("\n===== FAILURE SCENARIOS EXAMPLE =====")
	fmt.Println("Running failure scenarios...")
	examples.FailureScenarioTransaction()

	fmt.Println("\nAll examples completed.")
}
