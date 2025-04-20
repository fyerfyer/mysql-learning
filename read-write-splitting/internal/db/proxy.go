package db

import (
	"context"

	"gorm.io/gorm"
	"mysql-learning/read-write-splitting/config"
)

// DBProxy 数据库代理，封装读写分离逻辑
type DBProxy struct {
	router *SQLRouter // SQL路由器
	pool   *DBPool    // 数据库连接池
}

// NewDBProxy 创建新的数据库代理
func NewDBProxy(config *config.DBConfig) (*DBProxy, error) {
	// 初始化数据库连接池
	pool, err := NewDBPool(config)
	if err != nil {
		return nil, err
	}

	// 创建路由器
	router := NewSQLRouter(pool)

	return &DBProxy{
		router: router,
		pool:   pool,
	}, nil
}

// Master 获取主库连接
func (p *DBProxy) Master() *gorm.DB {
	return p.router.WriteDB()
}

// Slave 获取从库连接
func (p *DBProxy) Slave() *gorm.DB {
	return p.router.ReadDB()
}

// DB 根据操作类型自动选择数据库连接
// 这是一个方便的方法，让调用者不必关心具体使用哪个连接
func (p *DBProxy) DB(operationType string) *gorm.DB {
	if operationType == "read" {
		return p.Slave()
	}
	return p.Master()
}

// Create 创建记录（写操作）
func (p *DBProxy) Create(value interface{}) *gorm.DB {
	return p.Master().Create(value)
}

// Save 保存记录（写操作）
func (p *DBProxy) Save(value interface{}) *gorm.DB {
	return p.Master().Save(value)
}

// Updates 更新记录（写操作）
func (p *DBProxy) Updates(model interface{}, values interface{}) *gorm.DB {
	return p.Master().Model(model).Updates(values)
}

// Delete 删除记录（写操作）
func (p *DBProxy) Delete(value interface{}) *gorm.DB {
	return p.Master().Delete(value)
}

// Find 查询多条记录（读操作）
func (p *DBProxy) Find(dest interface{}, conds ...interface{}) *gorm.DB {
	return p.Slave().Find(dest, conds...)
}

// First 查询第一条记录（读操作）
func (p *DBProxy) First(dest interface{}, conds ...interface{}) *gorm.DB {
	return p.Slave().First(dest, conds...)
}

// Take 查询一条记录（读操作）
func (p *DBProxy) Take(dest interface{}, conds ...interface{}) *gorm.DB {
	return p.Slave().Take(dest, conds...)
}

// Raw 执行原始SQL
func (p *DBProxy) Raw(sql string, values ...interface{}) *gorm.DB {
	return p.router.Route(sql).Raw(sql, values...)
}

// Exec 执行原始SQL
func (p *DBProxy) Exec(sql string, values ...interface{}) *gorm.DB {
	return p.router.Route(sql).Exec(sql, values...)
}

// Transaction 执行事务（总是使用主库）
func (p *DBProxy) Transaction(fc func(tx *gorm.DB) error) error {
	return p.Master().Transaction(fc)
}

// WithContext 设置上下文
func (p *DBProxy) WithContext(ctx context.Context) *DBProxy {
	newProxy := &DBProxy{
		router: p.router,
		pool:   p.pool,
	}
	return newProxy
}

// Close 关闭所有数据库连接
func (p *DBProxy) Close() {
	p.pool.Close()
}
