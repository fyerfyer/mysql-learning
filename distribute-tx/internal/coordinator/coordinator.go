package coordinator

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"distribute-tx/internal/db"
	"distribute-tx/internal/model"
	"distribute-tx/internal/participant"
)

// TransactionCoordinator 协调分布式事务的中央组件
type TransactionCoordinator struct {
	ServiceName  string                     // 协调者服务名称
	DBManager    *db.DBConnectionManager    // 数据库连接管理器
	Participants []*participant.Participant // 事务参与者列表
	Timeout      time.Duration              // 事务超时时间
	mutex        sync.Mutex                 // 互斥锁，用于并发控制
}

// NewCoordinator 创建新的事务协调者
func NewCoordinator(serviceName string, dbManager *db.DBConnectionManager, timeout time.Duration) *TransactionCoordinator {
	return &TransactionCoordinator{
		ServiceName:  serviceName,
		DBManager:    dbManager,
		Participants: make([]*participant.Participant, 0),
		Timeout:      timeout,
	}
}

// RegisterParticipant 注册一个参与者到当前协调者
func (c *TransactionCoordinator) RegisterParticipant(participant *participant.Participant) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.Participants = append(c.Participants, participant)
}

// Begin 开始一个新的分布式事务
func (c *TransactionCoordinator) Begin(description string) (string, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 生成全局唯一事务ID
	xid := uuid.New().String()

	// 获取协调者数据库连接
	txDB, err := c.DBManager.GetDB(c.ServiceName)
	if err != nil {
		return "", fmt.Errorf("failed to get coordinator database: %w", err)
	}

	// 创建事务记录
	tx := model.Transaction{
		XID:         xid,
		Status:      model.StatusCreated,
		StartTime:   time.Now(),
		Description: description,
	}

	// 保存事务记录到数据库
	if err := txDB.Create(&tx).Error; err != nil {
		return "", fmt.Errorf("failed to create transaction record: %w", err)
	}

	return xid, nil
}

// Prepare 执行事务的准备阶段，所有参与者尝试准备但不提交
func (c *TransactionCoordinator) Prepare(xid string, participantActions map[string]func(*gorm.DB) error) (bool, error) {
	// 更新事务状态为准备中
	if err := c.updateTransactionStatus(xid, model.StatusPreparing); err != nil {
		return false, err
	}

	// 对于每个参与者执行准备操作
	var wg sync.WaitGroup
	prepareResults := make(map[string]model.OperationResult)
	resultMutex := sync.Mutex{}

	for _, p := range c.Participants {
		wg.Add(1)

		go func(p *participant.Participant) {
			defer wg.Done()

			// 首先注册参与者
			_, err := p.Register(c.ServiceName, xid)
			if err != nil {
				resultMutex.Lock()
				prepareResults[p.Name] = model.OperationResult{Success: false, Err: err}
				resultMutex.Unlock()
				return
			}

			// 如果此参与者有对应的准备动作，则执行
			action, exists := participantActions[p.Name]
			if !exists {
				resultMutex.Lock()
				prepareResults[p.Name] = model.OperationResult{
					Success: false,
					Err:     errors.New("no action defined for participant"),
				}
				resultMutex.Unlock()
				return
			}

			// 执行准备操作
			result, err := p.Prepare(xid, action)

			resultMutex.Lock()
			prepareResults[p.Name] = result
			resultMutex.Unlock()
		}(p)
	}

	// 等待所有参与者完成准备
	wg.Wait()

	// 检查所有参与者是否都准备成功
	allPrepared := true
	var firstError error

	for _, result := range prepareResults {
		if !result.Success {
			allPrepared = false
			firstError = result.Err
			break
		}
	}

	// 如果所有参与者都准备成功，则更新事务状态为已准备
	if allPrepared {
		if err := c.updateTransactionStatus(xid, model.StatusPrepared); err != nil {
			return false, err
		}
		return true, nil
	}

	// 否则，更新事务状态为失败
	c.updateTransactionStatus(xid, model.StatusFailed)

	return false, firstError
}

// Commit 提交事务，通知所有参与者执行提交操作
func (c *TransactionCoordinator) Commit(xid string) (bool, error) {
	// 首先检查事务状态是否为已准备
	status, err := c.getTransactionStatus(xid)
	if err != nil {
		return false, err
	}

	if status != model.StatusPrepared {
		return false, fmt.Errorf("transaction not in prepared state, current status: %s", status)
	}

	// 通知所有参与者提交事务
	var wg sync.WaitGroup
	commitResults := make(map[string]model.OperationResult)
	resultMutex := sync.Mutex{}

	for _, p := range c.Participants {
		wg.Add(1)

		go func(p *participant.Participant) {
			defer wg.Done()

			// 执行提交
			result, _ := p.Commit(c.ServiceName, xid)

			resultMutex.Lock()
			commitResults[p.Name] = result
			resultMutex.Unlock()
		}(p)
	}

	// 等待所有参与者完成提交
	wg.Wait()

	// 检查所有参与者是否都提交成功
	allCommitted := true
	var firstError error

	for _, result := range commitResults {
		if !result.Success {
			allCommitted = false
			firstError = result.Err
			break
		}
	}

	// 更新事务状态
	if allCommitted {
		if err := c.updateTransactionStatus(xid, model.StatusCommitted); err != nil {
			return false, err
		}

		// 记录完成时间
		if err := c.recordFinishTime(xid); err != nil {
			// 只记录错误，不影响提交结果
			fmt.Printf("Warning: Failed to record finish time for transaction %s: %v\n", xid, err)
		}

		return true, nil
	}

	// 如果有失败，更新事务状态为失败
	c.updateTransactionStatus(xid, model.StatusFailed)

	return false, firstError
}

// Rollback 回滚事务，通知所有参与者执行回滚操作
func (c *TransactionCoordinator) Rollback(xid string) (bool, error) {
	// 首先获取事务当前状态
	status, err := c.getTransactionStatus(xid)
	if err != nil {
		return false, err
	}

	// 如果事务已经提交，则无法回滚
	if status == model.StatusCommitted {
		return false, errors.New("cannot rollback an already committed transaction")
	}

	// 通知所有参与者回滚事务
	var wg sync.WaitGroup
	rollbackResults := make(map[string]model.OperationResult)
	resultMutex := sync.Mutex{}

	for _, p := range c.Participants {
		wg.Add(1)

		go func(p *participant.Participant) {
			defer wg.Done()

			// 执行回滚
			result, _ := p.Rollback(c.ServiceName, xid)

			resultMutex.Lock()
			rollbackResults[p.Name] = result
			resultMutex.Unlock()
		}(p)
	}

	// 等待所有参与者完成回滚
	wg.Wait()

	// 更新事务状态为已回滚
	if err := c.updateTransactionStatus(xid, model.StatusRolledBack); err != nil {
		return false, err
	}

	// 记录完成时间
	if err := c.recordFinishTime(xid); err != nil {
		// 只记录错误，不影响回滚结果
		fmt.Printf("Warning: Failed to record finish time for transaction %s: %v\n", xid, err)
	}

	return true, nil
}

// GetTransaction 根据事务ID获取事务详情
func (c *TransactionCoordinator) GetTransaction(xid string) (*model.Transaction, error) {
	txDB, err := c.DBManager.GetDB(c.ServiceName)
	if err != nil {
		return nil, err
	}

	var transaction model.Transaction
	if err := txDB.Where("xid = ?", xid).First(&transaction).Error; err != nil {
		return nil, err
	}

	return &transaction, nil
}

// GetParticipants 获取事务的所有参与者
func (c *TransactionCoordinator) GetParticipants(xid string) ([]model.TransactionParticipant, error) {
	txDB, err := c.DBManager.GetDB(c.ServiceName)
	if err != nil {
		return nil, err
	}

	var participants []model.TransactionParticipant
	if err := txDB.Where("xid = ?", xid).Find(&participants).Error; err != nil {
		return nil, err
	}

	return participants, nil
}

// updateTransactionStatus 更新事务状态
func (c *TransactionCoordinator) updateTransactionStatus(xid string, status model.TransactionStatus) error {
	txDB, err := c.DBManager.GetDB(c.ServiceName)
	if err != nil {
		return err
	}

	result := txDB.Model(&model.Transaction{}).
		Where("xid = ?", xid).
		Update("status", status)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return errors.New("transaction not found")
	}

	return nil
}

// getTransactionStatus 获取事务当前状态
func (c *TransactionCoordinator) getTransactionStatus(xid string) (model.TransactionStatus, error) {
	transaction, err := c.GetTransaction(xid)
	if err != nil {
		return "", err
	}

	return transaction.Status, nil
}

// recordFinishTime 记录事务完成时间
func (c *TransactionCoordinator) recordFinishTime(xid string) error {
	txDB, err := c.DBManager.GetDB(c.ServiceName)
	if err != nil {
		return err
	}

	now := time.Now()
	result := txDB.Model(&model.Transaction{}).
		Where("xid = ?", xid).
		Update("finish_time", &now)

	return result.Error
}
