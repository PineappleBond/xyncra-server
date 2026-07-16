# Xyncra Server 开发者指南

> Last updated: 2026-07-15

本文档面向 Xyncra Server 的开发者，介绍项目结构、开发环境搭建、编码规范和常见开发任务的步骤。

**详细的实现指南和代码示例请参见 [DEVELOPER_REFERENCE.md](./DEVELOPER_REFERENCE.md)**。

---

## 项目结构

```
xyncra-server/
├── cmd/xyncra-server/main.go      # 程序入口：配置解析、组件初始化、启动服务
├── configs/config.example.env     # 环境变量配置示例
├── docs/
│   ├── API.md                     # WebSocket 协议 API 文档
│   ├── PRODUCT_DECISIONS.md       # 产品决策文档（所有开发者必须遵守）
│   └── DEVELOPER_REFERENCE.md     # 详细实现指南和代码示例
├── internal/
│   ├── e2e/                       # 端到端集成测试
│   ├── cleanup/                   # UserUpdate 过期清理
│   ├── handler/                   # RPC Handler（业务逻辑）
│   ├── mq/                        # 消息队列（Asynq/Redis）
│   ├── server/                    # WebSocket 服务器核心
│   └── store/                     # 数据持久化层（GORM）
├── pkg/protocol/                  # WebSocket 协议类型定义
└── scripts/test.sh                # 测试运行脚本
```

---

## 开发环境搭建

### 依赖

- **Go 1.21+**
- **Redis**（开发：`localhost:6379`，E2E 测试：`localhost:16379`）
- **SQLite**（内置，无需额外安装）
- **PostgreSQL / MySQL**（可选，用于跨数据库测试）

### 启动

```bash
# 零配置启动（SQLite + Redis localhost:6379）
go run ./cmd/xyncra-server/

# 自定义配置
go run ./cmd/xyncra-server/ -addr :9090 -redis-addr localhost:6380 -db-driver postgres -db-dsn "host=localhost user=postgres password=secret dbname=xyncra sslmode=disable"
```

**命令行参数：**

| 参数              | 环境变量                    | 默认值           | 说明                               |
| ----------------- | --------------------------- | ---------------- | ---------------------------------- |
| `-addr`           | `XYNCRA_ADDR`               | `:8080`          | WebSocket 监听地址                 |
| `-redis-addr`     | `XYNCRA_REDIS_ADDR`         | `localhost:6379` | Redis 地址                         |
| `-redis-password` | `XYNCRA_REDIS_PASSWORD`     | `""`             | Redis AUTH 密码                    |
| `-redis-db`       | `XYNCRA_REDIS_DB`           | `0`              | Redis 数据库索引                   |
| `-db-driver`      | `XYNCRA_DB_DRIVER`          | `sqlite`         | 数据库驱动（sqlite/postgres/mysql）|
| `-db-dsn`         | `XYNCRA_DB_DSN`             | `xyncra.db`      | 数据库 DSN / 连接字符串            |
| `-max-conns`      | `XYNCRA_MAX_CONNS_PER_USER` | `0`（无限制）    | 每用户最大连接数                   |

### 运行测试

```bash
# 所有单元测试
go test ./...

# Handler 测试（不需要 Redis）
go test ./internal/handler/

# Store 测试（不需要 Redis，SQLite only）
go test ./internal/store/

# E2E 测试（需要 Redis @ localhost:16379）
go test ./internal/e2e/ -timeout 120s

# MQ 测试（需要 Redis）
go test ./internal/mq/

# 带 race detector
go test -race ./...
```

---

## 核心概念

### RPC 请求处理流程

```
客户端 WebSocket 消息
  → Package (type=request)
    → DefaultMessageHandler.HandleMessage()
      → 根据 method 名路由到注册的 MethodHandler
        → MethodHandler.HandleRequest()
          → 返回 json.RawMessage 响应
```

### 关键接口

**`server.MethodHandler`** — 所有 RPC Handler 必须实现：

```go
type MethodHandler interface {
    HandleRequest(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error)
}
```

**`store.StoreAPI`** — Store 层对外接口：

```go
type StoreAPI interface {
    ConversationStore() *ConversationStore
    MessageStore() *MessageStore
    UserUpdateStore() *UserUpdateStore
    SendMessage(ctx context.Context, msg *model.Message, updates []model.UserUpdate,
        convID string, lastMessageAt time.Time, lastProcessedMessageID uint32) error
    Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error
    BeginTx(ctx context.Context) (*Tx, error)
    AutoMigrate(ctx context.Context) error
    Ping(ctx context.Context) error
    HealthCheck(ctx context.Context) error
}
```

**`mq.Broker`** — 消息队列接口：

```go
type Broker interface {
    Enqueue(ctx context.Context, task *Task, opts ...EnqueueOption) (string, error)
    Start(ctx context.Context, handler Handler) error
    Stop()
    GetTaskState(ctx context.Context, taskID string) (TaskState, error)
}
```

---

## 常见开发任务

### 添加新的 RPC Handler

1. 在 `internal/handler/` 下创建 `xxx.go`
2. 定义参数和响应结构体（JSON tag 使用 `snake_case`）
3. 实现 `MethodHandler` 接口（结构体 + `HandleRequest` 方法）
4. 在 `internal/handler/register.go` 的 `RegisterAll` 中注册
5. 编写单元测试（使用 `setupTestSQLite` + `MemoryConnectionStore`）

**详细代码示例见 [DEVELOPER_REFERENCE.md#添加新的 RPC Handler](./DEVELOPER_REFERENCE.md#添加新的-rpc-handler)**

### 添加新的 Store 方法

1. 在 `internal/store/` 对应文件中添加方法（如 `conversation.go`、`message.go`）
2. 错误使用 `classifyError` 包装（参见 `errors.go`）
3. 在对应测试文件中添加测试（使用 `runOnAllDatabases` 确保跨数据库兼容）

**详细代码示例见 [DEVELOPER_REFERENCE.md#添加新的 Store 方法](./DEVELOPER_REFERENCE.md#添加新的-store-方法)**

### 添加新的 MQ 任务类型

1. 在 `internal/mq/mq.go` 中定义任务类型常量
2. 在 `internal/handler/` 中创建 `mq_xxx.go`，实现处理函数
3. 在 `cmd/xyncra-server/main.go` 中注册到 `TaskHandler`

**详细代码示例见 [DEVELOPER_REFERENCE.md#添加新的 MQ 任务类型](./DEVELOPER_REFERENCE.md#添加新的-mq-任务类型)**

### 添加新的 Update 类型

1. 在 `pkg/protocol/protocol.go` 中添加常量
2. 在对应的 handler 文件中定义 payload 结构
3. 在 Handler 中创建 `UserUpdate`，分配 seq
4. 通过 `Broker.Enqueue` 入队 MQ 广播（fire-and-forget）
5. 更新测试和 API 文档

**Ephemeral Updates (Seq=0)** 用于 typing/presence 等瞬时业务，不需要持久化。

**详细步骤见 [DEVELOPER_REFERENCE.md#添加新的 Update 类型](./DEVELOPER_REFERENCE.md#添加新的-update-类型)**

---

## 测试规范

### Handler 单元测试

- 不需要 Redis，使用内存 Store 和内存 ConnectionStore
- 核心辅助函数：`setupTestSQLite`、`newTestRequest`、`server.NewTestClient(userID)`
- 使用 `testify/assert` 和 `testify/require`
- 断言消息引用产品决策编号：`require.Equal(t, expected, actual, "explanation (D-xxx)")`

### Store 测试

- 使用 `runOnAllDatabases` 确保跨数据库兼容（SQLite, PostgreSQL, MySQL）
- PostgreSQL 和 MySQL 不可用时使用 `t.Skipf` 跳过
- 使用 `cleanAll(t, s, ctx)` 清理测试数据

### E2E 测试

- 需要 Redis @ `localhost:16379`
- 使用 `setupE2ETest` 初始化完整环境
- **不能**并行运行（共享 Redis 实例）
- Redis 不可达时使用 `t.Skipf` 跳过

**详细测试示例见 [DEVELOPER_REFERENCE.md#测试规范](./DEVELOPER_REFERENCE.md#测试规范)**

---

## 编码规范

### 通用

- 注释使用英文，godoc 风格（`// MethodName does ...`）
- 错误使用 `fmt.Errorf("context: %w", err)` 包装，提供上下文
- 使用 `github.com/google/uuid` 生成 ID
- JSON tag 使用 `snake_case`
- 数据模型字段使用 GORM tag
- 遵循现有命名和模式

### 设计模式

- **Functional Options**：用于服务器配置（参见 `server.WSWithXxx` 系列）
- **Dependencies 结构体**：用于 Handler 依赖注入（参见 `handler.Dependencies`）
- **幂等性**：关键操作使用幂等模式（D-006 client_message_id、D-011 find-or-create）
- **Fire-and-forget**：MQ 入队失败不阻塞主流程（D-007）

### 错误处理

- Store 层：使用 `classifyError` 统一错误分类
- Handler 层：使用 `fmt.Errorf` 包装 Store 错误，添加上下文
- 参数验证：返回描述性错误（`"missing required field: xxx"`）
- 不要在 Handler 层吞掉错误（除非是 fire-and-forget 场景并记录日志）

### 并发安全

- Handler 结构体只持有不可变依赖引用，天然并发安全
- 注释中标注 `safe for concurrent use`
- Store 方法使用 `context.Context` 传递

### 产品决策引用

- 在代码注释中引用相关产品决策编号（如 `D-011`）
- 在测试断言消息中引用产品决策编号
- 所有功能实现必须遵守 `docs/PRODUCT_DECISIONS.md` 中的决策

---

## 相关文档

- [产品决策文档](./PRODUCT_DECISIONS.md) — 核心架构决策，所有开发者必须遵守
- [API 文档](./API.md) — WebSocket 协议说明
- [详细实现指南](./DEVELOPER_REFERENCE.md) — 代码示例和详细步骤
