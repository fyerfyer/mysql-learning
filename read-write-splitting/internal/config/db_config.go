package config

import "fmt"

// DBConfig 数据库配置
type DBConfig struct {
	Master DBInfo   // 主库配置
	Slaves []DBInfo // 从库配置列表
}

// DBInfo 单个数据库连接信息
type DBInfo struct {
	Host     string // 主机地址
	Port     int    // 端口号
	User     string // 用户名
	Password string // 密码
	DBName   string // 数据库名
}

// GetDefaultConfig 获取默认的数据库配置
// 实际项目中这些参数通常来自配置文件或环境变量
func GetDefaultConfig() *DBConfig {
	return &DBConfig{
		Master: DBInfo{
			Host:     "localhost",
			Port:     3306,
			User:     "root",
			Password: "",
			DBName:   "test_db1",
		},
		Slaves: []DBInfo{
			{
				Host:     "localhost",
				Port:     3306,
				User:     "root",
				Password: "",
				DBName:   "test_db2",
			},
			{
				Host:     "localhost",
				Port:     3306,
				User:     "root",
				Password: "",
				DBName:   "test_db2",
			},
		},
	}
}

// GetDSN 根据数据库信息生成DSN连接字符串
func (db DBInfo) GetDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		db.User, db.Password, db.Host, db.Port, db.DBName)
}
