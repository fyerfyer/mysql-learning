package config

import "time"

// Config 保存MySQL高可用系统的配置信息
type Config struct {
	// 主库配置
	MasterDB DBConfig
	// 从库配置
	SlaveDB DBConfig
	// 健康检查间隔时间
	HealthCheckInterval time.Duration
	// 健康检查超时时间
	HealthCheckTimeout time.Duration
	// 连续失败次数阈值，超过这个值触发切换
	FailThreshold int
}

// DBConfig 保存数据库连接配置
type DBConfig struct {
	// 数据库主机地址
	Host string
	// 数据库端口
	Port int
	// 数据库用户名
	Username string
	// 数据库密码
	Password string
	// 数据库名称
	Database string
}

// GetDSN 返回数据库连接字符串
func (c *DBConfig) GetDSN() string {
	return c.Username + ":" + c.Password + "@tcp(" + c.Host + ":" +
		string(rune(c.Port)) + ")/" + c.Database + "?charset=utf8mb4&parseTime=True&loc=Local"
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		MasterDB: DBConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "root",
			Password: "",
			Database: "test_sw1",
		},
		SlaveDB: DBConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "root",
			Password: "",
			Database: "test_sw2",
		},
		HealthCheckInterval: 5 * time.Second,
		HealthCheckTimeout:  2 * time.Second,
		FailThreshold:       3,
	}
}
