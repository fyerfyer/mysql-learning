package service

import (
	"errors"
	"fmt"
	"log"

	"read-write-splitting/internal/db"
	"read-write-splitting/internal/model"
)

// UserService 用户服务，处理用户相关业务逻辑
type UserService struct {
	dbProxy *db.DBProxy // 数据库代理
}

// NewUserService 创建新的用户服务
func NewUserService(proxy *db.DBProxy) *UserService {
	return &UserService{
		dbProxy: proxy,
	}
}

// CreateUser 创建新用户（写操作，使用主库）
func (s *UserService) CreateUser(username, email, password string, age int) (*model.User, error) {
	user := model.NewUser(username, email, password, age)

	// 使用主库执行写操作
	result := s.dbProxy.Create(user)
	if result.Error != nil {
		log.Printf("Failed to create user: %v", result.Error)
		return nil, result.Error
	}

	log.Printf("Created new user: %s (ID: %d)", username, user.ID)
	return user, nil
}

// GetUserByID 通过ID获取用户（读操作，使用从库）
func (s *UserService) GetUserByID(id uint) (*model.User, error) {
	var user model.User

	// 使用从库执行读操作
	result := s.dbProxy.First(&user, id)
	if result.Error != nil {
		log.Printf("Failed to find user by ID %d: %v", id, result.Error)
		return nil, result.Error
	}

	return &user, nil
}

// GetAllUsers 获取所有用户（读操作，使用从库）
func (s *UserService) GetAllUsers() ([]model.User, error) {
	var users []model.User

	// 使用从库执行读操作
	result := s.dbProxy.Find(&users)
	if result.Error != nil {
		log.Printf("Failed to get all users: %v", result.Error)
		return nil, result.Error
	}

	log.Printf("Retrieved %d users", len(users))
	return users, nil
}

// UpdateUser 更新用户信息（写操作，使用主库）
func (s *UserService) UpdateUser(user *model.User) error {
	if user.ID == 0 {
		return errors.New("user ID cannot be empty")
	}

	// 使用主库执行写操作
	result := s.dbProxy.Save(user)
	if result.Error != nil {
		log.Printf("Failed to update user ID %d: %v", user.ID, result.Error)
		return result.Error
	}

	log.Printf("Updated user ID %d", user.ID)
	return nil
}

// SearchUsersByAge 根据年龄查找用户（读操作，使用从库）
func (s *UserService) SearchUsersByAge(age int) ([]model.User, error) {
	var users []model.User

	// 使用从库执行读操作
	result := s.dbProxy.Find(&users, "age = ?", age)
	if result.Error != nil {
		log.Printf("Failed to search users by age %d: %v", age, result.Error)
		return nil, result.Error
	}

	log.Printf("Found %d users with age %d", len(users), age)
	return users, nil
}

// DeleteUser 删除用户（写操作，使用主库）
func (s *UserService) DeleteUser(id uint) error {
	// 创建一个带有ID的用户对象
	user := model.User{ID: id}

	// 使用主库执行写操作
	result := s.dbProxy.Delete(&user)
	if result.Error != nil {
		log.Printf("Failed to delete user ID %d: %v", id, result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		log.Printf("No user found with ID %d", id)
		return fmt.Errorf("user with ID %d not found", id)
	}

	log.Printf("Deleted user ID %d", id)
	return nil
}

// UpdateUserActive 更新用户激活状态（写操作，使用主库）
func (s *UserService) UpdateUserActive(id uint, active bool) error {
	// 直接使用主库连接进行操作
	result := s.dbProxy.Master().Model(&model.User{}).Where("id = ?", id).Update("active", active)
	if result.Error != nil {
		log.Printf("Failed to update active status for user ID %d: %v", id, result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		log.Printf("No user found with ID %d", id)
		return fmt.Errorf("user with ID %d not found", id)
	}

	status := "activated"
	if !active {
		status = "deactivated"
	}

	log.Printf("User ID %d %s", id, status)
	return nil
}
