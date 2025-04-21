package config

// DBConfig 数据库连接配置
type DBConfig struct {
	Host     string // 数据库主机
	Port     int    // 端口
	User     string // 用户名
	Password string // 密码
	DBName   string // 数据库名
}

var DefaultDBConfig = DBConfig{
	Host:     "localhost",
	Port:     3306,
	User:     "root",
	Password: "",
	DBName:   "test_tx",
}
