package participant

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"distribute-tx/internal/db"
	"distribute-tx/internal/model"
)

// Participant 表示分布式事务中的一个参与者
type Participant struct {
	Name       string                  // 参与者名称
	ResourceID string                  // 资源标识
	DBManager  *db.DBConnectionManager // 数据库连接管理器
	LocalTx    *gorm.DB                // 本地事务对象
}

// NewParticipant 创建新的事务参与者
func NewParticipant(name string, resourceID string, dbManager *db.DBConnectionManager) *Participant {
	return &Participant{
		Name:       name,
		ResourceID: resourceID,
		DBManager:  dbManager,
	}
}

// Register 将参与者注册到指定的全局事务中
func (p *Participant) Register(coordinatorService string, xid string) (model.OperationResult, error) {
	// 获取协调者数据库连接
	coordDB, err := p.DBManager.GetDB(coordinatorService)
	if err != nil {
		return model.OperationResult{Success: false, Err: err}, err
	}

	// 创建参与者记录
	participant := model.TransactionParticipant{
		XID:        xid,
		Name:       p.Name,
		Status:     model.ParticipantRegistered,
		ResourceID: p.ResourceID,
	}

	// 将参与者记录保存到协调者数据库
	if err := coordDB.Create(&participant).Error; err != nil {
		return model.OperationResult{
			Success: false,
			Err:     err,
			Message: fmt.Sprintf("Failed to register participant %s to transaction %s", p.Name, xid),
		}, err
	}

	return model.OperationResult{
		Success: true,
		Message: fmt.Sprintf("Participant %s successfully registered to transaction %s", p.Name, xid),
	}, nil
}

// Prepare 执行准备阶段操作，在本地资源上尝试事务操作但不提交
func (p *Participant) Prepare(xid string, action func(*gorm.DB) error) (model.OperationResult, error) {
	// 获取资源数据库连接
	db, err := p.DBManager.GetDB(p.ResourceID)
	if err != nil {
		return model.OperationResult{Success: false, Err: err}, err
	}

	// 开始本地事务
	tx := db.Begin()
	p.LocalTx = tx

	// 执行业务逻辑
	if err := action(tx); err != nil {
		// 发生错误，回滚本地事务
		tx.Rollback()
		return model.OperationResult{
			Success: false,
			Err:     err,
			Message: fmt.Sprintf("Prepare phase failed for participant %s in transaction %s", p.Name, xid),
		}, err
	}

	// 准备成功，但不提交(事务仍然保持打开状态)
	return model.OperationResult{
		Success: true,
		Message: fmt.Sprintf("Prepare phase successful for participant %s in transaction %s", p.Name, xid),
	}, nil
}

// UpdateParticipantStatus 更新参与者状态
func (p *Participant) UpdateParticipantStatus(coordinatorService string, xid string, status model.ParticipantStatus) error {
	coordDB, err := p.DBManager.GetDB(coordinatorService)
	if err != nil {
		return err
	}

	// 更新参与者状态
	result := coordDB.Model(&model.TransactionParticipant{}).
		Where("xid = ? AND name = ?", xid, p.Name).
		Update("status", status)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return errors.New("participant record not found")
	}

	return nil
}

// Commit 提交准备好的事务
func (p *Participant) Commit(coordinatorService string, xid string) (model.OperationResult, error) {
	if p.LocalTx == nil {
		return model.OperationResult{
			Success: false,
			Err:     errors.New("no active local transaction"),
			Message: fmt.Sprintf("No active transaction found for participant %s", p.Name),
		}, errors.New("no active local transaction")
	}

	// 提交本地事务
	if err := p.LocalTx.Commit().Error; err != nil {
		// 更新参与者状态为失败
		p.UpdateParticipantStatus(coordinatorService, xid, model.ParticipantFailed)

		return model.OperationResult{
			Success: false,
			Err:     err,
			Message: fmt.Sprintf("Commit failed for participant %s in transaction %s", p.Name, xid),
		}, err
	}

	// 更新参与者状态为已提交
	if err := p.UpdateParticipantStatus(coordinatorService, xid, model.ParticipantCommitted); err != nil {
		return model.OperationResult{
			Success: false,
			Err:     err,
			Message: fmt.Sprintf("Failed to update participant status after commit for %s", p.Name),
		}, err
	}

	// 清空本地事务引用
	p.LocalTx = nil

	return model.OperationResult{
		Success: true,
		Message: fmt.Sprintf("Transaction committed successfully for participant %s in transaction %s", p.Name, xid),
	}, nil
}

// Rollback 回滚准备好的事务
func (p *Participant) Rollback(coordinatorService string, xid string) (model.OperationResult, error) {
	if p.LocalTx == nil {
		return model.OperationResult{
			Success: false,
			Err:     errors.New("no active local transaction"),
			Message: fmt.Sprintf("No active transaction found for participant %s", p.Name),
		}, errors.New("no active local transaction")
	}

	// 回滚本地事务
	if err := p.LocalTx.Rollback().Error; err != nil {
		// 更新参与者状态为失败
		p.UpdateParticipantStatus(coordinatorService, xid, model.ParticipantFailed)

		return model.OperationResult{
			Success: false,
			Err:     err,
			Message: fmt.Sprintf("Rollback failed for participant %s in transaction %s", p.Name, xid),
		}, err
	}

	// 更新参与者状态为已回滚
	if err := p.UpdateParticipantStatus(coordinatorService, xid, model.ParticipantRolledBack); err != nil {
		return model.OperationResult{
			Success: false,
			Err:     err,
			Message: fmt.Sprintf("Failed to update participant status after rollback for %s", p.Name),
		}, err
	}

	// 清空本地事务引用
	p.LocalTx = nil

	return model.OperationResult{
		Success: true,
		Message: fmt.Sprintf("Transaction rolled back successfully for participant %s in transaction %s", p.Name, xid),
	}, nil
}

// ExecuteCompensation 执行补偿操作（当事务失败需要额外补偿时）
func (p *Participant) ExecuteCompensation(xid string, compensation func() error) (model.OperationResult, error) {
	// 执行补偿逻辑
	if err := compensation(); err != nil {
		return model.OperationResult{
			Success: false,
			Err:     err,
			Message: fmt.Sprintf("Compensation failed for participant %s in transaction %s", p.Name, xid),
		}, err
	}

	return model.OperationResult{
		Success: true,
		Message: fmt.Sprintf("Compensation executed successfully for participant %s in transaction %s", p.Name, xid),
	}, nil
}
