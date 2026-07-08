# Xyncra Server 开发者指南

> Last updated: 2026-07-08

本文档面向 Xyncra Server 的开发者，介绍项目结构、开发环境搭建、编码规范和常见开发任务的步骤。

---

## 项目结构

```
xyncra-server/
├── cmd/
│   └── xyncra-server/
│       └── main.go                  # 程序入口：配置解析、组件初始化、启动服务
├── configs/
│   └── config.example.env           # 环境变量配置示例
├── docs/
│   ├── API.md                       # WebSocket 协议 API 文档
│   ├── DEVELOPER_GUIDE.md           # 本文档
│   └── PRODUCT_DECISIONS.md         # 产品决策文档（所有开发者必须遵守）
├── internal/
│   ├── e2e/
│   │   └── e2e_test.go              # 端到端集成测试（需要 Redis）
│   ├── cleanup/
│   │   ├── cleanup.go              # UserUpdate 过期清理 goroutine（D-016）
│   │   └── cleanup_test.go
│   ├── handler/
│   │   ├── register.go              # Handler 注册中心（RegisterAll）
│   │   ├── heartbeat.go             # heartbeat RPC handler
│   │   ├── send_message.go          # send_message RPC handler
│   │   ├── sync_updates.go          # sync_updates RPC handler
│   │   ├── create_conversation.go   # create_conversation RPC handler
│   │   ├── list_conversations.go    # list_conversations RPC handler
│   │   ├── get_messages.go          # get_messages RPC handler
│   │   ├── get_conversation.go      # get_conversation RPC handler
│   │   ├── search_messages.go       # search_messages RPC handler
│   │   ├── delete_conversation.go   # delete_conversation RPC handler
│   │   ├── restore_conversation.go  # restore_conversation RPC handler
│   │   ├── delete_message.go        # delete_message RPC handler
│   │   ├── mark_as_read.go          # mark_as_read RPC handler
│   │   ├── mq_send_message.go       # MQ TaskHandler（异步消息推送）
│   │   └── *_test.go                # 各 handler 的单元测试
│   ├── mq/
│   │   ├── mq.go                    # Broker 接口、Task 类型、常量定义
│   │   ├── handler.go               # TaskHandler 注册/路由
│   │   ├── asynq.go                 # Asynq (Redis) 实现
│   │   ├── options.go               # EnqueueOption 函数式选项
│   │   └── *_test.go
│   ├── server/
│   │   ├── server.go                # WebSocketServer 主结构
│   │   ├── doc.go                   # 包级文档
│   │   ├── websocket_server.go      # 服务器启动/停止逻辑
│   │   ├── websocket_handler.go     # MessageHandler / MethodHandler 接口
│   │   ├── websocket_client.go      # Client 结构（连接上下文）
│   │   ├── connection_store.go      # ConnectionStore 接口 + ConnectionInfo 模型
│   │   ├── redis_connection_store.go    # Redis 实现（生产用）
│   │   ├── memory_connection_store.go   # 内存实现（测试用）
│   │   ├── test_helpers.go          # 测试辅助函数
│   │   ├── node_broadcaster.go      # NodeBroadcaster 接口（D-018）
│   │   ├── redis_node_broadcaster.go # Redis Pub/Sub 实现（D-018）
│   │   ├── node_broadcaster_test.go
│   │   ├── websocket_server_broadcast_test.go
│   │   ├── websocket_server_benchmark_test.go
│   │   └── *_test.go
│   └── store/
│       ├── store.go                 # Store 聚合 + StoreAPI 接口
│       ├── db.go                    # Database（连接池管理）
│       ├── errors.go                # classifyError + 标准错误定义
│       ├── conversation.go          # ConversationStore 方法
│       ├── message.go               # MessageStore 方法
│       ├── user_update.go           # UserUpdateStore 方法
│       ├── model/
│       │   ├── conversation.go      # Conversation GORM 模型
│       │   ├── message.go           # Message GORM 模型
│       │   └── user_update.go       # UserUpdate GORM 模型
│       ├── benchmark_test.go
│       └── *_test.go
├── pkg/
│   └── protocol/
│       ├── protocol.go              # WebSocket 协议类型定义（Package, Request, Response）
│       ├── errors.go                # HandlerError + ResponseCode 常量
│       └── errors_test.go
├── scripts/
│   └── test.sh                      # 测试运行脚本
├── go.mod
├── go.sum
├── Dockerfile                    # Docker 镜像构建
├── docker-compose.yml            # Docker Compose 编排
└── README.md
```

---

## 开发环境搭建

### 依赖

- **Go 1.21+**
- **Redis**
  - 开发用：`localhost:6379`
  - 测试用：`localhost:16379`（E2E 测试专用，DB 15）
- **SQLite**（内置，无需额外安装）
- **PostgreSQL / MySQL**（可选，用于跨数据库测试）

### 启动

```bash
# 零配置启动（SQLite + Redis localhost:6379）
go run ./cmd/xyncra-server/

# 自定义配置
go run ./cmd/xyncra-server/ -addr :9090 -redis-addr localhost:6380 -db-driver postgres -db-dsn "host=localhost user=postgres password=secret dbname=xyncra sslmode=disable"
```

命令行参数：

| 参数            | 环境变量                      | 默认值           | 说明                          |
| --------------- | ----------------------------- | ---------------- | ----------------------------- |
| `-addr`         | `XYNCRA_ADDR`                 | `:8080`          | WebSocket 监听地址            |
| `-redis-addr`   | `XYNCRA_REDIS_ADDR`           | `localhost:6379` | Redis 地址                    |
| `-redis-password` | `XYNCRA_REDIS_PASSWORD`     | `""`             | Redis AUTH 密码               |
| `-redis-db`     | `XYNCRA_REDIS_DB`             | `0`              | Redis 数据库索引              |
| `-db-driver`    | `XYNCRA_DB_DRIVER`            | `sqlite`         | 数据库驱动（sqlite/postgres/mysql） |
| `-db-dsn`       | `XYNCRA_DB_DSN`               | `xyncra.db`      | 数据库 DSN / 连接字符串       |
| `-max-conns`    | `XYNCRA_MAX_CONNS_PER_USER`   | `0`（无限制）    | 每用户最大连接数              |

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

**`server.MethodHandler`** — 所有 RPC Handler 必须实现的接口：

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

## 如何添加新的 RPC Handler

### 步骤

1. 在 `internal/handler/` 下创建 `xxx.go`
2. 定义参数和响应结构体（JSON tag 与 API 文档一致）
3. 实现 `MethodHandler` 接口（使用结构体 + `HandleRequest` 方法）
4. 在 `register.go` 的 `RegisterAll` 中注册
5. 编写单元测试（使用 `setupTestSQLite` + `MemoryConnectionStore` 模式）

### 示例 1：最小 Handler（无 Store 依赖）

以 `heartbeat.go` 为基础，展示最简 Handler 模式：

```go
package handler

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"

    "github.com/PineappleBond/xyncra-server/internal/server"
    "github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --- 1. 定义请求/响应结构体 ---

type myMethodParams struct {
    SomeField string `json:"some_field"`
}

type myMethodResponse struct {
    Status string `json:"status"`
}

// --- 2. 实现 Handler 结构体 ---

type myMethodHandler struct {
    connStore server.ConnectionStore
}

func NewMyMethodHandler(connStore server.ConnectionStore) *myMethodHandler {
    return &myMethodHandler{connStore: connStore}
}

// --- 3. 实现 HandleRequest ---

func (h *myMethodHandler) HandleRequest(ctx context.Context, client *server.Client,
    req *protocol.PackageDataRequest) (json.RawMessage, error) {

    // 解析参数
    var params myMethodParams
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return nil, fmt.Errorf("invalid params: %w", err)
    }

    // 业务逻辑...

    // 使用 marshalResponse（handler 包内共享辅助函数）序列化响应
    return marshalResponse(myMethodResponse{Status: "ok"})
}
```

在 `register.go` 中注册：

```go
func RegisterAll(h *server.DefaultMessageHandler, deps Dependencies) {
    // ...其他 handler...
    h.RegisterMethod("my_method", NewMyMethodHandler(deps.ConnStore))
}
```

### 示例 2：带 Store 的 Handler

以 `create_conversation.go` 为基础，展示 Store 交互和幂等性模式：

```go
package handler

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "time"

    "github.com/google/uuid"

    "github.com/PineappleBond/xyncra-server/internal/server"
    "github.com/PineappleBond/xyncra-server/internal/store"
    "github.com/PineappleBond/xyncra-server/internal/store/model"
    "github.com/PineappleBond/xyncra-server/pkg/protocol"
)

type createConversationParams struct {
    UserID string `json:"user_id"`
    Title  string `json:"title"`
}

type createConversationResponse struct {
    Conversation *model.Conversation `json:"conversation"`
    Duplicate    bool                `json:"duplicate"`
}

type createConversationHandler struct {
    store store.StoreAPI
}

func NewCreateConversationHandler(store store.StoreAPI) *createConversationHandler {
    return &createConversationHandler{store: store}
}

func (h *createConversationHandler) HandleRequest(ctx context.Context,
    client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {

    // 1. 解析参数
    var params createConversationParams
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return nil, fmt.Errorf("invalid params: %w", err)
    }

    // 2. 验证必填字段
    if params.UserID == "" {
        return nil, fmt.Errorf("missing required field: user_id")
    }

    // 3. 幂等性检查（D-011）
    callerID := client.UserID()
    existing, err := h.store.ConversationStore().GetByUsers(ctx, callerID, params.UserID)
    if err == nil {
        return marshalResponse(createConversationResponse{
            Conversation: existing,
            Duplicate:    true,
        })
    }
    if !errors.Is(err, store.ErrNotFound) {
        return nil, fmt.Errorf("check existing conversation: %w", err)
    }

    // 4. 创建新记录
    conv := &model.Conversation{
        ID:        uuid.New().String(),
        UserID1:   callerID,
        UserID2:   params.UserID,
        Type:      "1-on-1",
        Title:     params.Title,
        CreatedAt: time.Now(),
    }
    if err := h.store.ConversationStore().Create(ctx, conv); err != nil {
        return nil, fmt.Errorf("create conversation: %w", err)
    }

    // 5. 返回成功响应
    return marshalResponse(createConversationResponse{
        Conversation: conv,
        Duplicate:    false,
    })
}
```

### 示例 3：带 Broker 的 Handler（MQ 入队）

以 `send_message.go` 为基础，展示 MQ 异步任务入队的 fire-and-forget 模式（D-007）：

```go
// 在 HandleRequest 中，持久化完成后异步入队 MQ 任务：

// 8. 构建 MQ 任务 payload
taskPayload, _ := json.Marshal(myTaskPayload{...})

// 9. 异步入队（fire-and-forget, D-007）
task := &mq.Task{
    Type:    mq.TypeSendMessage,
    Payload: taskPayload,
}
if _, err := h.broker.Enqueue(ctx, task); err != nil {
    // MQ 入队失败不阻塞主流程——数据已持久化（D-007）
    log.Printf("send_message: MQ enqueue failed (fire-and-forget): %v", err)
}

// 10. 返回成功响应
return marshalResponse(resp)
```

---

## 如何添加新的 Store 方法

### 步骤

1. 在 `internal/store/` 对应文件中添加方法（如 `conversation.go`、`message.go`）
2. 错误使用 `classifyError` 包装（参见 `errors.go`）
3. 在对应测试文件中添加测试
4. 使用 `runOnAllDatabases` 确保跨数据库兼容

### 错误处理模式

所有 Store 方法使用 `classifyError` 将底层数据库错误转换为统一的 Store 错误：

```go
// 标准 Store 错误（errors.go）
var (
    ErrNotFound              = errors.New("store: record not found")
    ErrDuplicateKey          = errors.New("store: duplicate key")
    ErrForeignKeyViolation   = errors.New("store: foreign key violation")
    ErrConnectionFailed      = errors.New("store: connection failed")
    ErrContextDeadlineExceeded = errors.New("store: context deadline exceeded")
)
```

### 示例

```go
// 在 conversation.go 中添加新方法：

// GetUnscoped retrieves a conversation including soft-deleted records.
// Returns ErrNotFound if no record exists.
func (cs *ConversationStore) GetUnscoped(ctx context.Context, id string) (*model.Conversation, error) {
    var conv model.Conversation
    err := cs.db.WithContext(ctx).
        Unscoped().
        Where("id = ?", id).
        First(&conv).Error
    if err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return nil, ErrNotFound
        }
        return nil, classifyError(fmt.Errorf("store: get unscoped conversation: %w", err))
    }
    return &conv, nil
}
```

要点：
- `gorm.ErrRecordNotFound` 转换为 `store.ErrNotFound`
- 其他错误使用 `classifyError` + `fmt.Errorf("context: %w", err)` 包装
- 方法签名第一个参数始终为 `ctx context.Context`

---

## 如何添加新的 MQ 任务类型

### 步骤

1. 在 `internal/mq/mq.go` 中定义任务类型常量
2. 在 `internal/handler/` 中创建 `mq_xxx.go`，实现处理函数
3. 在 `cmd/xyncra-server/main.go` 中注册到 `TaskHandler`

### 示例

**第 1 步：定义任务类型常量**（`internal/mq/mq.go`）：

```go
const (
    // TypeSendMessage is the task type for delivering a message to recipients.
    TypeSendMessage = "mq:send_message"

    // TypeMyNewTask is the task type for ...
    TypeMyNewTask = "mq:my_new_task"
)
```

**第 2 步：实现 TaskHandler 函数**（`internal/handler/mq_my_new_task.go`）：

```go
package handler

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/PineappleBond/xyncra-server/internal/mq"
    "github.com/PineappleBond/xyncra-server/internal/server"
)

// NewMyNewTaskHandler returns an mq.TaskHandler-compatible function that
// processes the MyNewTask task.
func NewMyNewTaskHandler(
    broadcastFn func(userID string, updates *protocol.PackageDataUpdates) error,
    logger server.Logger,
) func(ctx context.Context, task *mq.Task) error {
    return func(ctx context.Context, task *mq.Task) error {
        if task == nil {
            return fmt.Errorf("my_new_task: nil task")
        }

        var payload myTaskPayload
        if err := json.Unmarshal(task.Payload, &payload); err != nil {
            if logger != nil {
                logger.Error("my_new_task: unmarshal payload: %v", err)
            }
            return nil // 数据已持久化，重试无意义
        }

        // 处理逻辑...
        return nil
    }
}
```

**第 3 步：注册**（`cmd/xyncra-server/main.go`）：

```go
taskHandler := mq.NewTaskHandler()
taskHandler.Register(mq.TypeSendMessage,
    handler.NewSendMessageTaskHandler(srv.BroadcastUpdates, srv.Logger()))
taskHandler.Register(mq.TypeMyNewTask,
    handler.NewMyNewTaskHandler(srv.BroadcastUpdates, srv.Logger()))
```

---

## 如何添加新的 Update 类型

当需要新增一种 Update 类型（如 `reaction`、`typing` 等）时，按以下步骤操作：

### 1. 定义常量

在 `pkg/protocol/protocol.go` 中添加常量：

```go
const UpdateTypeReaction = "reaction"
```

### 2. 定义 Payload 结构

在对应的 handler 文件中定义 payload 结构体：

```go
type reactionUpdatePayload struct {
    ConversationID string `json:"conversation_id"`
    MessageID      uint32 `json:"message_id"`
    Emoji          string `json:"emoji"`
}
```

### 3. 在 Handler 中创建 UserUpdate

在 handler 的 `HandleRequest` 方法中（操作成功后）：

1. 确定影响范围：只为操作用户创建，还是为所有会话成员创建？
   - 仅影响自己的操作（如 `mark_read`）→ 只为操作用户创建
   - 影响所有人的操作（如 `delete`）→ 为所有成员创建
2. 为每个受影响的用户分配 seq：`GetLatestSeq(userID) + 1`
3. 构建 `model.UserUpdate`，设置 `Type: protocol.UpdateTypeReaction`
4. 调用 `UserUpdateStore().Create(ctx, updates)`

### 4. MQ 广播

构建 `sendMessageRecipient` 列表，通过 `Broker.Enqueue` 入队（fire-and-forget）。
复用现有的 `TypeSendMessage` 任务类型。

### 5. 更新测试

- handler 测试：验证 UserUpdate 被创建，Type 正确，Payload 正确
- sync_updates 测试：验证新类型的 Update 能通过 sync_updates 返回
- E2E 测试：验证端到端流程

### 6. 更新 API 文档

在 `docs/API.md` 的 sync_updates 章节补充新类型的 payload 结构。

### 设计原则

- 所有操作共享用户级 seq 空间（D-028）
- MQ 广播失败不影响数据完整性（D-007）
- gap 由服务器运行时填充（D-029）

---

## 测试规范

### Handler 单元测试

Handler 测试不需要 Redis，使用内存 Store 和内存 ConnectionStore。

**核心辅助函数：**

```go
// setupTestSQLite 创建内存 SQLite 数据库并执行 AutoMigrate
func setupTestSQLite(t *testing.T) *testSQLiteStore {
    t.Helper()
    db, err := store.NewDatabase(store.DatabaseConfig{
        Driver: "sqlite",
        DSN:    fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
    })
    require.NoError(t, err, "failed to open sqlite")
    s := store.New(db.DB())
    ctx := context.Background()
    require.NoError(t, s.AutoMigrate(ctx), "auto migrate failed")
    return &testSQLiteStore{s}
}

// newTestRequest 创建带有序列化参数的测试请求
func newTestRequest(id, method string, params interface{}) *protocol.PackageDataRequest {
    data, _ := json.Marshal(params)
    return &protocol.PackageDataRequest{
        ID:     id,
        Method: method,
        Params: data,
    }
}

// server.NewTestClient(userID) 创建测试用 Client 对象
```

**测试示例：**

```go
func TestMyHandler_HappyPath(t *testing.T) {
    s := setupTestSQLite(t)
    connStore := server.NewMemoryConnectionStore(0) // 0 = 无连接限制
    handler := NewMyHandler(s)

    ctx := context.Background()
    client := server.NewTestClient("alice")
    req := newTestRequest("req-1", "my_method", map[string]interface{}{
        "some_field": "value",
    })

    data, err := handler.HandleRequest(ctx, client, req)
    require.NoError(t, err)

    var resp myResponse
    require.NoError(t, json.Unmarshal(data, &resp))
    assert.Equal(t, "expected", resp.Field, "explanation (D-xxx)")
}
```

**断言规范：**
- 使用 `testify/assert` 和 `testify/require`
- `require` 用于前置条件失败后无法继续的断言
- `assert` 用于可继续检查的断言
- 断言消息使用描述性说明：`require.Equal(t, expected, actual, "explanation (D-xxx)")`
- 引用相关产品决策编号，如 `(D-011)`

### Store 测试

Store 测试使用 `runOnAllDatabases` 确保跨数据库兼容（SQLite, PostgreSQL, MySQL）：

```go
func TestMyStoreMethod(t *testing.T) {
    runOnAllDatabases(t, func(t *testing.T, s *Store) {
        ctx := context.Background()

        // 准备测试数据
        conv := newTestConv("conv-test", "alice", "bob", "1-on-1", "Test")
        require.NoError(t, s.Conversations.Create(ctx, conv))

        // 测试正常路径
        got, err := s.Conversations.Get(ctx, "conv-test")
        require.NoError(t, err)
        assert.Equal(t, "Test", got.Title)

        // 测试 Not Found
        _, err = s.Conversations.Get(ctx, "nonexistent")
        assert.ErrorIs(t, err, ErrNotFound)
    })
}
```

**要点：**
- PostgreSQL 和 MySQL 不可用时使用 `t.Skipf` 跳过（不会导致测试失败）
- `cleanAll(t, s, ctx)` 用于清理 PostgreSQL/MySQL 测试数据
- `newTestConv` 辅助函数创建带完整时间字段的测试 Conversation
- `testNow` 固定时间值避免 MySQL zero-value datetime 问题

### E2E 测试

E2E 测试需要 Redis @ `localhost:16379`，使用 `setupE2ETest` 初始化完整环境：

```go
func setupE2ETest(t *testing.T) *e2eEnv {
    // 1. 检查 Redis 连通性（不可达则 skip）
    // 2. FlushDB 确保干净状态
    // 3. SQLite 内存数据库（每个测试独立）
    // 4. Store + AutoMigrate
    // 5. Redis ConnectionStore
    // 6. AsynqBroker
    // 7. RegisterAll（注册所有 handler）
    // 8. WebSocket 服务器（:0 随机端口）
    // 9. 启动 Broker worker pool
    // 10. 启动 WebSocket 服务器
    // 11. t.Cleanup 注册清理逻辑
}
```

**要点：**
- E2E 测试**不能**并行运行（共享 Redis 实例）
- 每个测试使用独立的 SQLite 内存数据库和 Redis key 前缀
- Redis 不可达时使用 `t.Skipf` 跳过
- 使用 `e2eRedisDB = 15` 隔离测试数据

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
