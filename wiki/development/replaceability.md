# 模块可替换性设计

## 概述

Xyncra 的核心架构原则是**面向接口编程**。每个核心模块都通过接口定义契约，实现细节可以独立替换。这种设计使得开发者可以快速理解系统边界、替换实现、编写测试，而无需理解整个代码库。

## 核心模块接口体系

### 1. 存储层（Store）

**接口定义：`internal/store/store.go`**

```go
type StoreAPI interface {
    ConversationStore() *ConversationStore
    MessageStore() *MessageStore
    UserUpdateStore() *UserUpdateStore
    QuestionStore() *QuestionStore

    SendMessage(ctx context.Context, msg *model.Message, memberIDs []string) (*SendMessageResult, error)
    Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error
    BeginTx(ctx context.Context) (*Tx, error)
    AutoMigrate(ctx context.Context) error
    Ping(ctx context.Context) error
    HealthCheck(ctx context.Context) error
}
```

**可替换维度：**

| 维度 | 当前实现 | 替代选项 |
|------|----------|----------|
| 数据库引擎 | GORM（SQLite/PostgreSQL/MySQL） | 任何实现 `StoreAPI` 的后端 |
| 连接池配置 | GORM 默认（25/5） | 通过 `DatabaseConfig` 调整 |
| 错误分类 | `classifyError` 跨方言 | 新驱动的错误映射 |

**如何替换：**

如果需要使用 MongoDB 替代 GORM，只需实现 `StoreAPI` 接口：

```go
type MongoStore struct {
    client *mongo.Client
}

func (m *MongoStore) ConversationStore() *ConversationStore { ... }
func (m *MongoStore) MessageStore() *MessageStore { ... }
// ... 实现所有 StoreAPI 方法
```

然后在 `main.go` 中替换：

```go
// var dataStore store.StoreAPI = store.NewFromDatabase(db)
var dataStore store.StoreAPI = mongo.NewMongoStore(mongoClient)
```

### 2. 消息队列（Message Queue）

**接口定义：`internal/mq/mq.go`**

```go
type Broker interface {
    Enqueue(ctx context.Context, task *Task, opts ...EnqueueOption) (string, error)
    Start(ctx context.Context, handler Handler) error
    Stop()
    GetTaskState(ctx context.Context, taskID string) (TaskState, error)
}

type Handler interface {
    ProcessTask(ctx context.Context, task *Task) error
}
```

**可替换维度：**

| 维度 | 当前实现 | 替代选项 |
|------|----------|----------|
| 队列后端 | Asynq（Redis） | RabbitMQ、Kafka、NATS、AWS SQS |
| 任务路由 | `TaskHandler` 注册表 | 自动发现、声明式路由 |
| 序列化 | JSON | Protocol Buffers、MessagePack |
| 优先级 | 3 级（critical/default/low） | 可自定义映射 |

**如何替换：**

实现新的 Broker，例如 RabbitMQ：

```go
type RabbitMQBroker struct {
    conn *amqp.Connection
    ch   *amqp.Channel
}

func NewRabbitMQBroker(url string) (*RabbitMQBroker, error) { ... }
func (r *RabbitMQBroker) Enqueue(ctx context.Context, task *mq.Task, opts ...mq.EnqueueOption) (string, error) { ... }
func (r *RabbitMQBroker) Start(ctx context.Context, handler mq.Handler) error { ... }
func (r *RabbitMQBroker) Stop() { ... }
func (r *RabbitMQBroker) GetTaskState(ctx context.Context, taskID string) (mq.TaskState, error) { ... }
```

### 3. 连接存储（Connection Store）

**接口定义：`internal/server/connection_store.go`**

```go
type ConnectionStore interface {
    Add(ctx context.Context, info *ConnectionInfo) error
    Get(ctx context.Context, connID string) (*ConnectionInfo, error)
    Remove(ctx context.Context, connID string) error
    Exists(ctx context.Context, connID string) (bool, error)
    Update(ctx context.Context, connID string, metadata map[string]string) error
    Patch(ctx context.Context, connID string, updater func(*ConnectionInfo)) error
    Refresh(ctx context.Context, connID string) error
    ListByUser(ctx context.Context, userID string, limit int) ([]*ConnectionInfo, error)
    CountByUser(ctx context.Context, userID string) (int64, error)
    CountAll(ctx context.Context) (int64, error)
    RemoveByUser(ctx context.Context, userID string) (int64, error)
    Ping(ctx context.Context) error
    Close() error
}
```

**可替换维度：**

| 维度 | 当前实现 | 替代选项 |
|------|----------|----------|
| 存储后端 | Redis | 内存、MySQL、ETCD、Consul |
| 会话策略 | 单设备多连接 | 单设备单连接、无限制 |
| 元数据丰富 | `ConnectionInfoEnricher` | 自定义注入 |

**现有替代实现：**

- `RedisConnectionStore` — 生产环境
- `MemoryConnectionStore` — 测试环境（miniredis 模拟）

```go
// 测试中替换
connStore := server.NewMemoryConnectionStore()
srv, _ := server.NewWebSocketServer(
    server.WSWithConnectionStore(connStore),
    // ...
)
```

### 4. 消息路由（Node Broadcaster）

**接口定义：`internal/server/node_broadcaster.go`**

```go
type NodeBroadcaster interface {
    Publish(ctx context.Context, userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) error
    Subscribe(ctx context.Context, callback func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string)) error
    Close() error
}
```

**可替换维度：**

| 维度 | 当前实现 | 替代选项 |
|------|----------|----------|
| 传输 | Redis Pub/Sub | Kafka、RabbitMQ fanout、gRPC stream |
| 拓扑 | 无（noop） | 全网格、分片 |
| 序列化 | JSON | Protocol Buffers |

**现有实现：**

- `NoopBroadcaster` — 单节点部署（默认）
- `RedisNodeBroadcaster` — 多节点部署

```go
// 单节点部署（默认，无需配置）
// nodeBroadcaster = &NoopBroadcaster{}

// 多节点部署
nodeBroadcaster := server.NewRedisNodeBroadcaster(redisClient, "xyncra")
```

### 5. 函数注册表（Function Registry）

**接口定义：`internal/server/function_registry.go`**

```go
type FunctionRegistry interface {
    RegisterFunctions(ctx context.Context, userID, deviceID string, params *RegisterFunctionsParams) error
    GetFunctions(ctx context.Context, userID, deviceID string) ([]protocol.FunctionInfo, error)
    GetDeviceFunctions(ctx context.Context, userID, deviceID string) (*DeviceFunctions, error)
    OnDeviceDisconnect(ctx context.Context, userID, deviceID string) (*DeviceFunctions, error)
}
```

**可替换维度：**

| 维度 | 当前实现 | 替代选项 |
|------|----------|----------|
| 存储 | 内存 map | Redis、数据库持久化 |
| 容量 | 每设备 200 函数 | 可配置 |
| 生命周期 | 设备断开时清理 | TTL 过期、手动清理 |

### 6. 待处理请求存储（Pending Store）

**接口定义：`internal/server/pending_store.go`**

```go
type PendingStore interface {
    Save(ctx context.Context, req *PendingRequest) error
    List(ctx context.Context, userID, deviceID string) ([]*PendingRequest, error)
    Remove(ctx context.Context, userID, deviceID, requestID string) error
    RemoveByDevice(ctx context.Context, userID, deviceID string) error
    Update(ctx context.Context, req *PendingRequest) error
}
```

### 7. WebSocket Server 选项模式

`WebSocketServer` 使用函数式选项模式，使依赖注入可组合：

```go
type WebSocketServerOption func(*webSocketServerOptions)

func WSWithStore(s store.StoreAPI) WebSocketServerOption { ... }
func WSWithBroker(b mq.Broker) WebSocketServerOption { ... }
func WSWithConnectionStore(cs ConnectionStore) WebSocketServerOption { ... }
func WSWithNodeBroadcaster(nb NodeBroadcaster) WebSocketServerOption { ... }
func WSWithFunctionRegistry(fr FunctionRegistry) WebSocketServerOption { ... }
func WSWithPendingStore(ps PendingStore) WebSocketServerOption { ... }
func WSWithAuthenticate(fn func(r *http.Request) (string, error)) WebSocketServerOption { ... }
func WSWithLogger(l Logger) WebSocketServerOption { ... }
```

每个选项都是独立的，可以单独替换或省略（省略时使用零值默认行为）。

## 解耦原则

### 1. 依赖方向

所有依赖从上层流向下层，不允许循环依赖：

```
handler → server, agent, mq, store
server  → store, mq, protocol
mq      → （无内部依赖）
store   → model（仅模型）
agent   → store, server（通过接口）
```

### 2. 接口在消费侧定义

```
internal/server/connection_store.go:
  → ConnectionStore 接口定义

internal/server/websocket_server.go:
  → 使用 ConnectionStore 接口

internal/server/redis_connection_store.go:
  → 实现 ConnectionStore 接口
```

这样，`internal/server` 包定义了它需要什么，而实现细节无关。

### 3. 接口隔离原则

`ServerDeps` 从 `Server` 中分离，避免生命周期方法污染依赖消费者：

```go
type ServerDeps interface {
    Store() store.StoreAPI
    Broker() mq.Broker
    ConnectionStore() ConnectionStore
}

type Server interface {
    ServerDeps
    Start(ctx context.Context) error
    Stop()
    GracefulStop(ctx context.Context) error
}
```

### 4. 零值可用（Zero-Config）

所有可选依赖使用 nil-safe 模式：

```go
// agentRegistry 为 nil 时跳过 agent 检测
if h.agentRegistry != nil && !strings.HasPrefix(senderID, "agent/") {
    // agent detection
}

// functionRegistry 为 nil 时跳过函数注册
if deps.FunctionRegistry != nil {
    h.RegisterMethod("system.register_functions", NewRegisterFunctionsHandler(deps.FunctionRegistry))
}

// reverseRPC 为 nil 时跳过重连处理
if deps.ReverseRPC != nil && deps.ReverseRPC.PendingStore() != nil {
    h.RegisterMethod("system.reconnect", NewReconnectHandler(deps.ReverseRPC, deps.Logger))
}
```

## 开发者上手优化

### 分层学习路径

新开发者可以按以下层次逐步深入：

**第 1 层：理解整体架构（30 分钟）**

- 阅读 `README.md` 的架构图和项目结构
- 阅读 `docs/API.md` 了解协议
- 阅读 `wiki/devops/index.md` 了解部署

**第 2 层：运行和测试（1 小时）**

- `make build && make test`
- 运行 E2E 测试
- 阅读 `wiki/development/development-setup.md`

**第 3 层：理解核心模块（2-4 小时）**

- 阅读每个 `internal/` 包的 `doc.go` 或 `README-ZH.md`
- 阅读核心接口定义（`store.go`、`mq.go`、`server.go`）
- 使用 `codegraph` 工具探索代码依赖图

**第 4 层：修改一个 Handler（1-2 小时）**

- 按照"如何添加新 RPC Handler"指南
- 实现一个简单的 handler（如 `heartbeat`）
- 编写测试

**第 5 层：理解复杂流程（4-8 小时）**

- Agent 执行流程（`send_message` → `mq:agent_process` → `executor` → `eino_agent` → stream）
- HITL 完整流程（ask_user → persist → broadcast → resume）
- 多节点消息路由

### 知识传递机制

| 机制 | 内容 | 位置 |
|------|------|------|
| 包级 README | 中文说明 | `internal/*/README-ZH.md` |
| 产品决策记录 | D-001 到 D-124 | `docs/decisions/PRODUCT_DECISIONS.md` |
| 代码注释引用 | 决策 ID 关联 | 代码中的 `D-NNN` 注释 |
| 设计文档 | HITL、客户端函数等 | `docs/design/` |
| Agent 技能 | AI 辅助开发 | `.claude/skills/` |

## 代码审查流程

### PR 提交前自查清单

- [ ] 代码遵循编码规范（`make fmt && make vet`）
- [ ] 单元测试覆盖新逻辑
- [ ] Handler 并发安全
- [ ] 错误使用 `%w` 包装
- [ ] 设计决策引用决策 ID
- [ ] 无硬编码凭据
- [ ] 向后兼容（接口不变或新增方法）

### 审查指南

| 关注点 | 检查内容 |
|--------|----------|
| 架构 | 是否正确使用了接口？是否有循环依赖？ |
| 并发 | 锁使用是否正确？是否有竞态条件？ |
| 错误处理 | 所有错误路径都处理了吗？是否泄露了敏感信息？ |
| 日志 | 关键路径有日志吗？日志级别是否合理？ |
| 测试 | 边界条件覆盖了吗？测试可重复吗？ |

### 决策记录

重要的设计决策必须在 `docs/decisions/PRODUCT_DECISIONS.md` 中记录，格式为 `D-NNN`：

```
D-001  零配置启动
D-002  device_id 作为连接标识符
D-004  CORS 由反向代理处理
D-006  client_message_id 幂等性
D-007  Fire-and-forget MQ 策略
D-008  消息 ID 原子分配
D-011  Find-or-create 幂等性
D-012  MAX 语义的读游标
D-018  Redis Pub/Sub 跨节点路由
D-043  E2E 端口约定
D-050  Ephemeral 更新 Seq=0
D-062  Agent 作为用户
D-063  Nil-safe 可选依赖
D-074  专用 Redis 客户端
D-083  HITL Checkpoint 存储
D-092  ReverseRPC
D-095  设备替换协议
D-099  函数注册限制
D-101  客户端函数作为 Agent 工具
D-103  超时请求持久化
D-111  设备替换优雅退出
```

## 团队协作实践

### 分支策略

- `main` — 稳定分支，通过 CI 的提交才能合并
- `feat/<name>` — 功能分支，开发完成后合并到 main
- `fix/<name>` — 修复分支

### 知识共享

1. **代码注释** — 复杂逻辑必须有注释说明"为什么"而不仅是"是什么"
2. **设计文档** — 新功能需要设计文档，需经 CEO/Eng review
3. **Agent 技能** — 使用 `.claude/skills/` 共享开发知识和最佳实践
4. **包级文档** — 每个 `internal/` 包有 `README-ZH.md` 中文说明
5. **决策记录** — 每个架构决策有唯一的 D-NNN 编号
