# MySQL 高可用切换系统

## 系统架构

```
┌─────────────────────────┐
│                         │
│      应用程序           │
│                         │
└────────────┬────────────┘
             │
             ▼
┌─────────────────────────┐
│    高可用切换系统        │
├─────────────────────────┤
│  健康检查器(Monitor)     │      ┌─────────────┐
├─────────────────────────┤      │             │
│  切换器(Switcher)       │◄────►│  HTTP API   │
├─────────────────────────┤      │             │
│  数据库管理器(DBManager) │      └─────────────┘
└────────────┬────────────┘
             │
     ┌───────┴───────┐
     │               │
     ▼               ▼
┌─────────┐     ┌─────────┐
│ Master  │     │ Slave   │
│ Database│     │ Database│
└─────────┘     └─────────┘
```

## 核心组件

### 1. 数据库管理器 (DBManager)

管理与MySQL主从库的连接，包括健康检查和故障切换：

- **连接管理**：维护与主库和从库的连接
- **健康检查**：提供主库健康状态检测
- **切换功能**：在故障发生时切换到从库
- **故障模拟**：支持模拟主库故障场景

```go
// 检查主库健康状态
func (m *DBManager) CheckMasterHealth() bool {
    // 如果设置了故障模拟，直接返回不健康
    m.mu.RLock()
    if m.simulateFailure {
        m.mu.RUnlock()
        log.Println("Simulating master database failure")
        return false
    }
    m.mu.RUnlock()
    
    // 执行简单查询测试连接
    result := &struct{ Value int }{}
    err := m.masterDB.WithContext(ctx).Raw("SELECT 1 as value").Scan(result).Error
    if err != nil {
        log.Printf("Master health check failed: %v", err)
        return false
    }
    
    return result.Value == 1
}
```

### 2. 健康检查器 (HealthChecker)

定期监控主库状态，当检测到故障时触发切换：

- **定时检查**：按配置的时间间隔检查主库健康
- **故障计数**：记录连续失败次数
- **阈值控制**：达到失败阈值时触发切换
- **优雅关闭**：支持系统关闭时停止监控

```go
// 执行一次健康检查
func (hc *HealthChecker) checkHealth() {
    isHealthy := hc.dbManager.CheckMasterHealth()

    if isHealthy {
        // 主库正常，重置失败计数
        if hc.failCount > 0 {
            log.Println("Master database recovered after failures")
            hc.failCount = 0
        }
    } else {
        // 主库异常，增加失败计数
        hc.failCount++
        log.Printf("Master database health check failed (%d/%d)", 
                  hc.failCount, hc.config.FailThreshold)

        // 触发切换
        if hc.failCount >= hc.config.FailThreshold {
            hc.switcher.SwitchToSlave()
            hc.failCount = 0
        }
    }
}
```

### 3. 切换器 (Switcher)

执行从主库到从库的实际切换操作：

- **切换控制**：执行主从切换流程
- **状态追踪**：记录切换历史和统计信息
- **从库提升**：支持从库提升为主库的操作
- **并发控制**：确保切换过程的安全性

```go
// 执行从主库到从库的切换操作
func (s *Switcher) SwitchToSlave() error {
    s.mu.Lock()
    defer s.mu.Unlock()

    log.Println("Starting failover process from master to slave database")
    
    // 执行切换操作
    s.dbManager.SwitchToSlave()
    
    // 更新统计信息
    s.switchCount++
    s.lastSwitchAt = time.Now()
    
    log.Printf("Failover completed. Switch count: %d", s.switchCount)
    return nil
}
```

### 4. API服务器 (APIServer)

提供HTTP接口用于系统控制和状态监控：

- **故障模拟**：通过API触发主库故障模拟
- **状态查询**：提供切换统计和系统状态信息
- **操作控制**：支持通过API控制系统行为

```go
// HTTP API示例：模拟故障
http.HandleFunc("/api/simulate-failure", func(w http.ResponseWriter, r *http.Request) {
    enable := r.URL.Query().Get("enable")
    if enable == "true" {
        s.dbManager.SetSimulateFailure(true)
        fmt.Fprintf(w, "Master failure simulation enabled\n")
    } else if enable == "false" {
        s.dbManager.SetSimulateFailure(false)
        fmt.Fprintf(w, "Master failure simulation disabled\n")
    }
})
```

## 高可用切换工作原理

### 1. 健康检查机制

系统通过定期执行简单查询（`SELECT 1`）来监控主库的健康状态：

- **检查频率**：默认每5秒执行一次健康检查
- **超时控制**：设置查询超时（默认2秒）避免长时间阻塞
- **失败计数**：记录连续失败次数，避免因临时故障触发切换
- **阈值控制**：连续失败达到阈值（默认3次）时触发切换

健康检查逻辑示意：
```
1. 发送"SELECT 1"查询到主库
2. 如果查询成功返回结果1，表示主库健康
3. 如果查询失败或超时，表示主库可能存在问题
4. 连续多次失败达到阈值后，触发切换流程
```

### 2. 故障切换流程

当主库被判定为不可用时，系统执行以下切换流程：

1. **决策阶段**：
    - 健康检查器确认连续失败次数达到阈值
    - 触发切换器执行切换操作

2. **切换执行**：
    - 切换器更新数据库管理器中的活跃连接状态
    - 所有后续数据库操作重定向到从库
    - 记录切换时间和统计信息

3. **切换后处理**：
    - 在实际生产环境中，此时可能需要执行从库提升操作
    - 更新应用程序连接信息
    - 可能需要重新配置复制关系

### 3. 故障模拟机制

系统提供API接口模拟主库故障，用于测试切换流程：

- `/api/simulate-failure?enable=true`：激活故障模拟
- `/api/simulate-failure?enable=false`：停止故障模拟
- `/api/status`：查看切换状态和统计信息

当启用故障模拟时，健康检查将始终报告主库不健康，从而触发切换流程。

## 如何运行系统

### 前提条件

- Go 1.16或更高版本
- MySQL 5.7或更高版本
- 两个测试数据库（默认使用`test_sw1`和`test_sw2`）

### 配置系统

1. **配置数据库连接**：
   修改`internal/config/db_config.go`中的配置，设置正确的主库和从库连接信息。

   ```go
   // DefaultConfig 返回默认配置
   func DefaultConfig() *Config {
       return &Config{
           MasterDB: DBConfig{
               Host:     "localhost",
               Port:     3306,
               Username: "root",
               Password: "your_password",
               Database: "test_sw1",
           },
           SlaveDB: DBConfig{
               Host:     "localhost",
               Port:     3306, // 可以是不同端口的同一主机，或完全不同的服务器
               Username: "root",
               Password: "your_password", 
               Database: "test_sw2",
           },
           HealthCheckInterval: 5 * time.Second,
           HealthCheckTimeout:  2 * time.Second,
           FailThreshold:       3,
       }
   }
   ```

2. **确保数据库可访问**：
    - 创建两个测试数据库
    - 确保配置的用户有权限访问这些数据库

### 启动步骤

1. **构建并运行程序**：
   ```bash
   go build -o ha-switcher cmd/main.go
   ./ha-switcher
   ```

   或者直接运行：
   ```bash
   go run cmd/main.go
   ```

2. **观察系统日志**：
   程序启动后会显示初始化信息和健康检查状态。

### 测试故障切换

1. **模拟主库故障**：
   ```bash
   curl http://localhost:8080/api/simulate-failure?enable=true
   ```

2. **观察系统日志**：
   系统将开始报告主库健康检查失败，当失败次数达到阈值（默认3次）时，会自动切换到从库。

3. **查看切换状态**：
   ```bash
   curl http://localhost:8080/api/status
   ```

4. **停止故障模拟**：
   ```bash
   curl http://localhost:8080/api/simulate-failure?enable=false
   ```

## 代码结构

- `cmd/`: 应用入口
    - `main.go`: 主程序，初始化并协调各组件

- `internal/`: 内部实现
    - `config/`: 配置管理
        - `config.go`: 系统配置结构和默认值
    - `db/`: 数据库管理
        - `conn.go`: 数据库连接管理器
    - `monitor/`: 健康监控
        - `health_checker.go`: 主库健康检查器
    - `switcher/`: 切换控制
        - `switcher.go`: 故障切换实现
    - `api/`: HTTP API
        - `server.go`: API服务器实现

- `README.md`: 项目说明文档

## 扩展与实际应用

在实际生产环境中，可以扩展该系统以提供更完整的功能：

1. **自动从库提升**：
   实现真正的从库提升为主库的MySQL命令执行

2. **多从库支持**：
   扩展系统支持多个从库，实现更复杂的切换策略

3. **读写分离**：
   增加读写分离功能，提高系统整体性能

4. **监控集成**：
   与Prometheus等监控系统集成，提供更完善的可观测性

5. **自动化恢复**：
   当原主库恢复后，自动将其配置为新的从库

此简化版高可用切换系统提供了MySQL主从切换的基本功能，适用于学习和演示目的。### 2. 故障切换流程

当主库被判定为不可用时，系统执行以下切换流程：

1. **决策阶段**：
    - 健康检查器确认连续失败次数达到阈值
    - 触发切换器执行切换操作

2. **切换执行**：
    - 切换器更新数据库管理器中的活跃连接状态
    - 所有后续数据库操作重定向到从库
    - 记录切换时间和统计信息

3. **切换后处理**：
    - 在实际生产环境中，此时可能需要执行从库提升操作
    - 更新应用程序连接信息
    - 可能需要重新配置复制关系

### 3. 故障模拟机制

系统提供API接口模拟主库故障，用于测试切换流程：

- `/api/simulate-failure?enable=true`：激活故障模拟
- `/api/simulate-failure?enable=false`：停止故障模拟
- `/api/status`：查看切换状态和统计信息

当启用故障模拟时，健康检查将始终报告主库不健康，从而触发切换流程。

## 如何运行系统

### 前提条件

- Go 1.16或更高版本
- MySQL 5.7或更高版本
- 两个测试数据库（默认使用`test_sw1`和`test_sw2`）

### 配置系统

1. **配置数据库连接**：
   修改`internal/config/db_config.go`中的配置，设置正确的主库和从库连接信息。

   ```go
   // DefaultConfig 返回默认配置
   func DefaultConfig() *Config {
       return &Config{
           MasterDB: DBConfig{
               Host:     "localhost",
               Port:     3306,
               Username: "root",
               Password: "your_password",
               Database: "test_sw1",
           },
           SlaveDB: DBConfig{
               Host:     "localhost",
               Port:     3306, // 可以是不同端口的同一主机，或完全不同的服务器
               Username: "root",
               Password: "your_password", 
               Database: "test_sw2",
           },
           HealthCheckInterval: 5 * time.Second,
           HealthCheckTimeout:  2 * time.Second,
           FailThreshold:       3,
       }
   }
   ```

2. **确保数据库可访问**：
    - 创建两个测试数据库
    - 确保配置的用户有权限访问这些数据库

### 启动步骤

1. **构建并运行程序**：
   ```bash
   go build -o ha-switcher cmd/main.go
   ./ha-switcher
   ```

   或者直接运行：
   ```bash
   go run cmd/main.go
   ```

2. **观察系统日志**：
   程序启动后会显示初始化信息和健康检查状态。

### 测试故障切换

1. **模拟主库故障**：
   ```bash
   curl http://localhost:8080/api/simulate-failure?enable=true
   ```

2. **观察系统日志**：
   系统将开始报告主库健康检查失败，当失败次数达到阈值（默认3次）时，会自动切换到从库。

3. **查看切换状态**：
   ```bash
   curl http://localhost:8080/api/status
   ```

4. **停止故障模拟**：
   ```bash
   curl http://localhost:8080/api/simulate-failure?enable=false
   ```

## 代码结构

- `cmd/`: 应用入口
    - `main.go`: 主程序，初始化并协调各组件

- `internal/`: 内部实现
    - `config/`: 配置管理
        - `config.go`: 系统配置结构和默认值
    - `db/`: 数据库管理
        - `conn.go`: 数据库连接管理器
    - `monitor/`: 健康监控
        - `health_checker.go`: 主库健康检查器
    - `switcher/`: 切换控制
        - `switcher.go`: 故障切换实现
    - `api/`: HTTP API
        - `server.go`: API服务器实现

- `README.md`: 项目说明文档
