package model

import (
	"time"

	"gorm.io/gorm"
)

// User 用户模型
type User struct {
	ID        uint           `gorm:"primarykey"`               // 用户ID
	Username  string         `gorm:"size:50;not null;unique"`  // 用户名
	Email     string         `gorm:"size:100;not null;unique"` // 邮箱
	Password  string         `gorm:"size:100;not null"`        // 密码
	Age       int            `gorm:"default:0"`                // 年龄
	Active    bool           `gorm:"default:true"`             // 是否激活
	CreatedAt time.Time      // 创建时间
	UpdatedAt time.Time      // 更新时间
	DeletedAt gorm.DeletedAt `gorm:"index"` // 删除时间（软删除）
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}

// BeforeCreate 创建前的钩子
func (u *User) BeforeCreate(tx *gorm.DB) error {
	// 为了保持简单，这里不做实际操作
	return nil
}

// NewUser 创建新用户
func NewUser(username, email, password string, age int) *User {
	return &User{
		Username: username,
		Email:    email,
		Password: password,
		Age:      age,
		Active:   true,
	}
}
