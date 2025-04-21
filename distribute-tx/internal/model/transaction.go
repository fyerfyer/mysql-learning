package model

import (
	"time"

	"gorm.io/gorm"
)

// TransactionStatus 表示分布式事务的状态
type TransactionStatus string

// 分布式事务的不同状态
const (
	StatusCreated    TransactionStatus = "created"    // 事务已创建
	StatusPreparing  TransactionStatus = "preparing"  // 事务准备中
	StatusPrepared   TransactionStatus = "prepared"   // 所有参与者已准备好
	StatusCommitted  TransactionStatus = "committed"  // 事务已提交
	StatusRolledBack TransactionStatus = "rolledback" // 事务已回滚
	StatusFailed     TransactionStatus = "failed"     // 事务失败
)

// Transaction 表示一个分布式事务
type Transaction struct {
	gorm.Model
	XID         string            `gorm:"column:xid;type:varchar(64);uniqueIndex"` // 全局唯一事务ID
	Status      TransactionStatus `gorm:"column:status;type:varchar(20)"`          // 事务当前状态
	StartTime   time.Time         `gorm:"column:start_time"`                       // 事务开始时间
	FinishTime  *time.Time        `gorm:"column:finish_time"`                      // 事务完成时间
	Description string            `gorm:"column:description;type:varchar(255)"`    // 事务描述
}

// TableName 定义事务表名
func (Transaction) TableName() string {
	return "distributed_transactions"
}

// ParticipantStatus 表示事务参与者的状态
type ParticipantStatus string

// 参与者的不同状态
const (
	ParticipantRegistered ParticipantStatus = "registered" // 参与者已注册
	ParticipantPrepared   ParticipantStatus = "prepared"   // 参与者已准备
	ParticipantCommitted  ParticipantStatus = "committed"  // 参与者已提交
	ParticipantRolledBack ParticipantStatus = "rolledback" // 参与者已回滚
	ParticipantFailed     ParticipantStatus = "failed"     // 参与者操作失败
)

// TransactionParticipant 表示分布式事务的一个参与者
type TransactionParticipant struct {
	gorm.Model
	XID        string            `gorm:"column:xid;type:varchar(64);index"`   // 关联的全局事务ID
	Name       string            `gorm:"column:name;type:varchar(64)"`        // 参与者名称
	Status     ParticipantStatus `gorm:"column:status;type:varchar(20)"`      // 参与者当前状态
	ResourceID string            `gorm:"column:resource_id;type:varchar(64)"` // 资源标识(如数据库连接名)
}

// TableName 定义参与者表名
func (TransactionParticipant) TableName() string {
	return "transaction_participants"
}

// OperationResult 表示事务操作的结果
type OperationResult struct {
	Success bool   // 操作是否成功
	Err     error  // 错误信息，如果有
	Message string // 结果消息
}
