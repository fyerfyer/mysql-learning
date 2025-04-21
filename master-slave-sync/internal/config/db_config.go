package config

import "fmt"

// MasterConfig 主节点配置
type MasterConfig struct {
	// 数据库连接信息
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	// API服务配置
	APIPort int
}

// SlaveConfig 从节点配置
type SlaveConfig struct {
	// 数据库连接信息
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	// API服务配置
	APIPort int
	// 主节点连接信息
	MasterHost string
	MasterPort int
}

// SemiSyncConfig 半同步复制配置
type SemiSyncConfig struct {
	// 等待从节点确认的超时时间(毫秒)
	TimeoutMs int
	// 需要等待的从节点确认数
	MinSlaves int
}

// SyncConfig 整体配置结构
type SyncConfig struct {
	Master   MasterConfig
	Slave    SlaveConfig
	SemiSync SemiSyncConfig
}

// GetDSN 生成数据库连接字符串
func (m MasterConfig) GetDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		m.User, m.Password, m.Host, m.Port, m.DBName)
}

// GetDSN 生成数据库连接字符串
func (s SlaveConfig) GetDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		s.User, s.Password, s.Host, s.Port, s.DBName)
}

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() *SyncConfig {
	return &SyncConfig{
		Master: MasterConfig{
			Host:     "localhost",
			Port:     3306,
			User:     "root",
			Password: "",
			DBName:   "test_sync1",
			APIPort:  8080,
		},
		Slave: SlaveConfig{
			Host:       "localhost",
			Port:       3306,
			User:       "root",
			Password:   "",
			DBName:     "test_sync2",
			APIPort:    8081,
			MasterHost: "localhost",
			MasterPort: 8080,
		},
		SemiSync: SemiSyncConfig{
			TimeoutMs: 1000, // 1秒超时
			MinSlaves: 1,    // 至少等待一个从节点确认
		},
	}
}
