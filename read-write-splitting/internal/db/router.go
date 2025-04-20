package db

import (
	"regexp"
	"strings"

	"gorm.io/gorm"
)

// SQLRouter SQL路由器，负责判断SQL类型并路由到合适的数据库
type SQLRouter struct {
	dbPool *DBPool // 数据库连接池
}

// NewSQLRouter 创建新的SQL路由器
func NewSQLRouter(pool *DBPool) *SQLRouter {
	return &SQLRouter{
		dbPool: pool,
	}
}

// 用于判断是否为SELECT语句的正则表达式
var selectRegex = regexp.MustCompile(`(?i)^\s*SELECT`)

// IsReadOperation 判断SQL是否为读操作
func IsReadOperation(sql string) bool {
	trimSQL := strings.TrimSpace(sql)
	return selectRegex.MatchString(trimSQL)
}

// Route 根据SQL类型路由到合适的数据库连接
func (r *SQLRouter) Route(sql string) *gorm.DB {
	if IsReadOperation(sql) {
		return r.dbPool.Slave()
	}
	return r.dbPool.Master()
}

// ForceMaster 强制使用主库进行读操作
func (r *SQLRouter) ForceMaster() *gorm.DB {
	return r.dbPool.Master()
}

// ReadDB 获取用于读操作的数据库连接
func (r *SQLRouter) ReadDB() *gorm.DB {
	return r.dbPool.Slave()
}

// WriteDB 获取用于写操作的数据库连接
func (r *SQLRouter) WriteDB() *gorm.DB {
	return r.dbPool.Master()
}
