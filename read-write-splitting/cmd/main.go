package main

import (
	"log"
	"read-write-splitting/internal/config"
	"time"

	"read-write-splitting/internal/db"
	"read-write-splitting/internal/model"
	"read-write-splitting/internal/service"
)

func main() {
	// 初始化数据库配置
	dbConfig := config.GetDefaultConfig()

	// 创建数据库代理
	dbProxy, err := db.NewDBProxy(dbConfig)
	if err != nil {
		log.Fatalf("Failed to create DB proxy: %v", err)
	}
	defer dbProxy.Close()

	// 自动迁移表结构
	autoMigrate(dbProxy)

	// 创建用户服务
	userService := service.NewUserService(dbProxy)

	// 演示读写分离
	demonstrateReadWriteSplitting(userService)
}

// 自动迁移表结构到数据库
func autoMigrate(dbProxy *db.DBProxy) {
	log.Println("Auto migrating database schema...")
	err := dbProxy.Master().AutoMigrate(&model.User{})
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}
	log.Println("Migration completed successfully")
}

// 演示读写分离功能
func demonstrateReadWriteSplitting(userService *service.UserService) {
	log.Println("Demonstrating read-write splitting:")
	log.Println("------------------------------------")

	// 1. 创建用户（写操作，使用主库）
	log.Println("1. Creating users (Write operation - Master DB)")
	createDemoUsers(userService)

	// 停顿一下，便于观察
	time.Sleep(1 * time.Second)

	// 2. 查询所有用户（读操作，使用从库）
	log.Println("2. Fetching all users (Read operation - Slave DB)")
	users, err := userService.GetAllUsers()
	if err != nil {
		log.Printf("Error fetching users: %v", err)
	} else {
		log.Printf("Retrieved %d users", len(users))
		for _, user := range users {
			log.Printf("User: %s, Email: %s, Age: %d", user.Username, user.Email, user.Age)
		}
	}

	// 停顿一下，便于观察
	time.Sleep(1 * time.Second)

	// 3. 更新用户（写操作，使用主库）
	log.Println("3. Updating user (Write operation - Master DB)")
	if len(users) > 0 {
		user := users[0]
		user.Age = 30
		err := userService.UpdateUser(&user)
		if err != nil {
			log.Printf("Error updating user: %v", err)
		} else {
			log.Printf("Updated user %s, new age: %d", user.Username, user.Age)
		}
	}

	// 停顿一下，便于观察
	time.Sleep(1 * time.Second)

	// 4. 按年龄查询用户（读操作，使用从库）
	log.Println("4. Searching users by age (Read operation - Slave DB)")
	searchAge := 30
	usersFound, err := userService.SearchUsersByAge(searchAge)
	if err != nil {
		log.Printf("Error searching users: %v", err)
	} else {
		log.Printf("Found %d users with age %d", len(usersFound), searchAge)
		for _, user := range usersFound {
			log.Printf("User: %s, Email: %s, Age: %d", user.Username, user.Email, user.Age)
		}
	}

	// 停顿一下，便于观察
	time.Sleep(1 * time.Second)

	// 5. 删除一个用户（写操作，使用主库）
	log.Println("5. Deleting a user (Write operation - Master DB)")
	if len(users) > 0 {
		err := userService.DeleteUser(users[0].ID)
		if err != nil {
			log.Printf("Error deleting user: %v", err)
		} else {
			log.Printf("Deleted user with ID %d", users[0].ID)
		}
	}

	log.Println("------------------------------------")
	log.Println("Demonstration completed")
}

// 创建演示用户
func createDemoUsers(userService *service.UserService) {
	demoUsers := []struct {
		username string
		email    string
		password string
		age      int
	}{
		{"alice", "alice@example.com", "password123", 25},
		{"bob", "bob@example.com", "password456", 30},
		{"charlie", "charlie@example.com", "password789", 35},
	}

	for _, u := range demoUsers {
		user, err := userService.CreateUser(u.username, u.email, u.password, u.age)
		if err != nil {
			log.Printf("Failed to create user %s: %v", u.username, err)
		} else {
			log.Printf("Created user: %s (ID: %d)", user.Username, user.ID)
		}
	}
}
