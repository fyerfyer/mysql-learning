# MySQL 主从半同步复制系统

## 系统架构

系统由以下主要组件组成：

1. **主节点**：处理写操作并生成binlog
2. **从节点**：从主节点同步binlog并应用变更
3. **半同步复制机制**：确保数据变更在至少一个从节点上得到确认
4. **HTTP API**：提供数据操作和系统监控接口

### 架构图

```
┌────────────────┐          ┌────────────────┐
│                │          │                │
│  Master Node   │◄────────►│  Slave Node(s) │
│  (Port 8080)   │          │  (Port 8081+)  │
│                │          │                │
└───────┬────────┘          └───────┬────────┘
        │                           │
        ▼                           ▼
┌────────────────┐          ┌────────────────┐
│                │          │                │
│  Master MySQL  │          │  Slave MySQL   │
│  Database      │          │  Database      │
│                │          │                │
└────────────────┘          └────────────────┘
```

## 主从同步机制原理

### Binlog 实现

系统实现了一个简化版的binlog机制：

1. **binlog条目**：记录数据变更操作（INSERT、UPDATE、DELETE）及相关数据
2. **位置追踪**：每个binlog条目有一个唯一递增的位置标识
3. **序列化**：操作数据通过JSON序列化存储
4. **过滤查询**：从节点可以请求特定位置之后的所有条目

### 同步流程

1. **主节点写入流程**：
    - 应用程序通过API发起写操作
    - 主节点执行SQL操作
    - 生成对应的binlog条目
    - 等待半同步确认（如有配置）
    - 返回操作结果

2. **从节点同步流程**：
    - 定期（默认5秒）向主节点请求新的binlog条目
    - 应用收到的binlog变更到本地数据库
    - 向主节点发送确认（ACK）
    - 更新本地同步位置

3. **错误处理**：
    - 连接失败时会继续重试
    - 应用失败时会记录错误但不中断同步过程
    - 半同步超时会降级为异步模式

## 半同步复制原理

半同步复制是MySQL 5.5引入的一个重要特性，本项目模拟了其核心工作原理：

1. **写入确认**：
    - 主节点执行写操作后不立即返回
    - 等待至少一个从节点确认接收到binlog
    - 只有在收到确认或超时后才完成事务

2. **超时降级**：
    - 如果在配置的超时时间内未收到足够的确认
    - 系统降级为异步模式并记录警告
    - 事务继续执行完成

3. **状态恢复**：
    - 当从节点再次正常确认时
    - 系统可以从降级状态恢复到半同步状态

半同步复制提高了数据安全性，确保了在主节点故障时至少有一个从节点拥有完整的数据副本。

## 如何运行系统

### 前提条件

- Go 1.16或更高版本
- MySQL 5.7或更高版本
- 配置好的MySQL实例（可以是单实例多库）

### 启动步骤

1. **配置系统**：
   默认配置使用`localhost:3306`的MySQL实例和`test_sync`数据库。如果需要，可以修改`config.go`中的配置。

2. **启动主节点**：
   ```bash
   go run cmd/master/main.go
   ```

3. **启动从节点**：
   ```bash
   go run cmd/slave/main.go -id slave1
   ```
   可以启动多个从节点，使用不同的id和端口。

### 测试同步

1. **向主节点写入数据**：
   ```bash
   curl -X POST http://localhost:8080/api/records -H "Content-Type: application/json" -d '{"content":"Test record"}'
   ```

2. **从从节点查询数据**：
   ```bash
   curl http://localhost:8081/api/records
   ```

3. **查看节点状态**：
   ```bash
   curl http://localhost:8080/api/status  # 主节点状态
   curl http://localhost:8081/api/status  # 从节点状态
   ```

## API接口说明

### 主节点API

- `GET /api/records` - 获取所有记录
- `POST /api/records` - 创建新记录
- `GET /api/records/{id}` - 获取单个记录
- `PUT /api/records/{id}` - 更新记录
- `DELETE /api/records/{id}` - 删除记录
- `GET /api/status` - 获取主节点状态
- `GET /api/binlog` - 获取binlog条目（从节点调用）
- `POST /api/ack` - 接收从节点确认
- `POST /api/register_slave` - 注册新的从节点

### 从节点API

- `GET /api/records` - 获取所有记录（只读）
- `GET /api/records/{id}` - 获取单个记录（只读）
- `GET /api/status` - 获取从节点状态
- `POST /api/sync/start` - 启动同步进程
- `POST /api/sync/stop` - 停止同步进程

## 代码结构

- `cmd/`: 应用程序入口
    - `master/`: 主节点启动代码
    - `slave/`: 从节点启动代码

- `internal/`: 内部实现
    - `config/`: 配置管理
    - `storage/`: 数据存储层
    - `replication/`: 复制相关实现
        - binlog.go: binlog实现
        - master.go: 主节点逻辑
        - slave.go: 从节点逻辑
        - semi_sync.go: 半同步复制实现

- `api/`: API处理器
    - handlers.go: HTTP API实现

## 复制机制实现流程

1. **binlog生成**：
   在主节点的写操作中，每次数据变更都会生成一个对应的binlog条目，包含操作类型、表名、记录ID和序列化的数据。

2. **半同步等待**：
   主节点写入数据后，使用`WaitForACK`方法等待至少一个从节点的确认，如果在配置的超时时间内未收到足够确认，则降级为异步模式。

3. **从节点拉取**：
   从节点定期（默认5秒）向主节点请求新的binlog条目，指定当前同步位置，只获取更新的内容。

4. **变更应用**：
   从节点收到binlog条目后，根据操作类型（INSERT/UPDATE/DELETE）应用变更到本地数据库。

5. **确认机制**：
   从节点成功应用变更后，发送ACK到主节点，包含从节点ID和已应用的位置。
