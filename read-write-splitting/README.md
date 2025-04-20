# MySQL 读写分离系统

## 系统架构

```
读写分离系统
├── 应用层 (User Service)
├── 代理层 (DBProxy)
│   ├── SQL路由 (SQLRouter)
│   └── 连接池 (DBPool)
└── 存储层
    ├── 主库 (写操作)
    └── 从库 (读操作)
```

## 核心组件

### 1. 数据库配置 (DBConfig)

负责管理主库和从库的连接信息，包括主机地址、端口、用户名、密码和数据库名。

```go
type DBConfig struct {
    Master DBInfo   // 主库配置
    Slaves []DBInfo // 从库配置列表
}
```

### 2. 数据库连接池 (DBPool)

管理与主库和从库的连接，提供获取连接的方法：

- **连接管理**：维护一个主库连接和多个从库连接
- **负载均衡**：使用简单的轮询策略在多个从库间分发查询
- **容错处理**：当从库不可用时自动使用主库

```go
func (p *DBPool) Slave() *gorm.DB {
    // 如果没有从库，则返回主库
    if p.slaveCount == 0 {
        return p.master
    }

    // 简单的轮询策略选择从库
    current := atomic.AddInt32(&p.current, 1) % p.slaveCount
    return p.slaves[current]
}
```

### 3. SQL路由器 (SQLRouter)

核心组件，负责分析SQL语句并决定使用哪个数据库连接：

- **SQL解析**：使用正则表达式识别SELECT语句作为读操作
- **路由决策**：读操作路由到从库，写操作路由到主库

```go
func IsReadOperation(sql string) bool {
    trimSQL := strings.TrimSpace(sql)
    return selectRegex.MatchString(trimSQL)
}

func (r *SQLRouter) Route(sql string) *gorm.DB {
    if IsReadOperation(sql) {
        return r.dbPool.Slave()
    }
    return r.dbPool.Master()
}
```

### 4. 数据库代理 (DBProxy)

为应用层提供统一的数据库操作接口，封装读写分离逻辑：

- **面向操作**：提供Create、Find、Update等便捷方法
- **自动路由**：根据操作类型自动选择合适的数据库连接
- **透明代理**：对应用层隐藏读写分离的复杂性

```go
func (p *DBProxy) Create(value interface{}) *gorm.DB {
    return p.Master().Create(value)
}

func (p *DBProxy) Find(dest interface{}, conds ...interface{}) *gorm.DB {
    return p.Slave().Find(dest, conds...)
}
```

## 读写分离工作原理

### 1. 操作分类

系统将数据库操作分为两类：
- **读操作**：SELECT查询
- **写操作**：INSERT、UPDATE、DELETE、事务等

### 2. 连接选择

- **写操作**：始终使用主库连接
  ```go
  // 示例：创建操作
  func (p *DBProxy) Create(value interface{}) *gorm.DB {
      return p.Master().Create(value)
  }
  ```

- **读操作**：使用从库连接（轮询负载均衡）
  ```go
  // 示例：查询操作
  func (p *DBProxy) Find(dest interface{}, conds ...interface{}) *gorm.DB {
      return p.Slave().Find(dest, conds...)
  }
  ```

- **原始SQL**：通过正则表达式判断操作类型
  ```go
  func (p *DBProxy) Raw(sql string, values ...interface{}) *gorm.DB {
      return p.router.Route(sql).Raw(sql, values...)
  }
  ```

### 3. 容错机制

如果没有可用的从库，系统会自动降级到使用主库：

```go
// 如果没有从库，则返回主库
if p.slaveCount == 0 {
    return p.master
}
```

### 4. 事务处理

所有事务都在主库上执行，确保数据一致性：

```go
func (p *DBProxy) Transaction(fc func(tx *gorm.DB) error) error {
    return p.Master().Transaction(fc)
}
```

## 用法示例

```go
// 创建数据库代理
dbProxy, err := db.NewDBProxy(config)
if err != nil {
    log.Fatal(err)
}

// 创建记录（写操作 -> 主库）
user := model.NewUser("john", "john@example.com", "password", 25)
dbProxy.Create(user)

// 查询记录（读操作 -> 从库）
var users []model.User
dbProxy.Find(&users)

// 强制使用主库进行读操作
dbProxy.Master().Find(&users)
```
