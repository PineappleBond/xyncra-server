# Xyncra Server 开发参考指南

> Last updated: 2026-07-16

本文档包含 Xyncra Server 开发的详细实现指南和代码示例。快速入门请参见 [DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md)。

---

## 目录

- [添加新的 RPC Handler](#添加新的-rpc-handler)
- [添加新的 Store 方法](#添加新的-store-方法)
- [添加新的 MQ 任务类型](#添加新的-mq-任务类型)
- [添加新的 Update 类型](#添加新的-update-类型)
- [测试规范](#测试规范)

---

## 添加新的 RPC Handler

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

## 添加新的 Store 方法

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
    ErrConflict              = errors.New("store: conflict")
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

## 添加新的 MQ 任务类型

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

## 添加新的 Update 类型

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

### Ephemeral Update types (Seq=0)

Ephemeral updates 用于 typing/presence 等瞬时业务 (D-050)，不需要持久化：

1. 在 `pkg/protocol/protocol.go` 中新增 `UpdateType` 常量
2. 创建 server handler（`internal/handler/`），使用 `broadcastFn` 直接推送 `PackageDataUpdate{Seq: 0, Type: "..."}`
3. 在 `internal/handler/register.go` 中注册，传入 `deps.BroadcastFn`
4. 在 `pkg/client/options.go` 中新增可选 handler 接口（如 `TypingHandler`）
5. 在 `pkg/client/sync.go` 的 `notifyHandler` 中添加分发分支
6. 在 `dispatchUpdateTx` 中为新的 ephemeral type 添加 `case ... : return nil` 分支（defense-in-depth：虽然 Seq=0 不会走到 dispatchUpdateTx，但防止未来重构破坏前置分支）

注意：

- `ApplyUpdate` 已内置 Seq=0 前置分支，ephemeral update 会在入口处直接回调 handler 并返回。步骤 6 是防御性措施。
- Ephemeral type 不参与共享 seq 空间（D-028 例外）
- Gap filling 不受 Seq=0 影响（D-029）
- 推送失败可容忍（D-007 精神的更宽松变体）

### 示例：添加 `streaming` ephemeral type (D-051)

1. `pkg/protocol/protocol.go`: 新增 `UpdateTypeStreaming = "streaming"`
2. `internal/handler/stream_text.go`: 新建 handler，使用 `broadcastFn` 推送 `PackageDataUpdate{Seq: 0, Type: "streaming", Payload: ...}`
3. `internal/handler/register.go`: `h.RegisterMethod("stream_text", NewStreamTextHandler(deps.Store, deps.BroadcastFn))`
4. `pkg/client/options.go`: 新增 `StreamingHandler` 接口（`OnStreaming(ctx, userID, conversationID, streamID, text, isDone) error`）
5. `pkg/client/sync.go`: `notifyHandler` 新增 `case protocol.UpdateTypeStreaming` 分支 + `dispatchUpdateTx` 新增防御性 `return nil` 分支
6. `internal/cli/listen.go`: IPC handler 转发 + `cliUpdateHandler` 实现 `StreamingHandler` 接口
7. `internal/cli/stream_text.go`: 新建 CLI 命令（IPC-only, D-036）

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
