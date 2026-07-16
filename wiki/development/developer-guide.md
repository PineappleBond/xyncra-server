# 开发者指南

## 如何添加新的 RPC Handler

### 流程概述

添加一个新的 RPC 方法需要修改 4 个文件：

1. 创建 handler 文件（`internal/handler/<method>.go`）
2. 创建测试文件（`internal/handler/<method>_test.go`）
3. 在 `register.go` 中注册方法
4. 在 `pkg/protocol/protocol.go` 中添加更新类型（如果适用）

### 步骤详解

#### 1. 创建 Handler

在 `internal/handler/` 下创建新文件 `my_method.go`：

```go
package handler

import (
    "context"
    "encoding/json"

    "github.com/PineappleBond/xyncra-server/internal/server"
    "github.com/PineappleBond/xyncra-server/internal/store"
    "github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// myMethodParams is the JSON-decoded representation of the client-supplied
// parameters for the "my_method" method.
type myMethodParams struct {
    Field1 string `json:"field1"`
}

// myMethodResponse is the success response payload returned to the client.
type myMethodResponse struct {
    Result string `json:"result"`
}

// myMethodHandler implements MethodHandler for the "my_method" method.
// It is stateless (only holds immutable dependency references) and therefore
// safe for concurrent use.
type myMethodHandler struct {
    store store.StoreAPI
}

// NewMyMethodHandler creates a myMethodHandler.
func NewMyMethodHandler(store store.StoreAPI) *myMethodHandler {
    return &myMethodHandler{store: store}
}

// HandleRequest implements MethodHandler.
func (h *myMethodHandler) HandleRequest(
    ctx context.Context,
    client *server.Client,
    req *protocol.PackageDataRequest,
) (json.RawMessage, error) {
    // 1. Parse parameters
    var params myMethodParams
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return nil, protocol.NewValidationError("invalid params")
    }

    // 2. Validate
    if params.Field1 == "" {
        return nil, protocol.NewValidationError("missing required field: field1")
    }

    // 3. Business logic
    // ...

    // 4. Return success
    resp := myMethodResponse{Result: "ok"}
    return marshalResponse(resp)
}
```

#### 2. 注册 Handler

在 `internal/handler/register.go` 的 `RegisterAll` 函数中添加注册：

```go
func RegisterAll(h *server.DefaultMessageHandler, deps Dependencies) {
    // ... existing registrations
    h.RegisterMethod("my_method", NewMyMethodHandler(deps.Store))
}
```

如果需要新的依赖类型，在 `Dependencies` 结构体中添加字段：

```go
type Dependencies struct {
    // ... existing fields
    MyNewDependency SomeType
}
```

然后在 `cmd/xyncra-server/main.go` 中传入依赖。

#### 3. 更新 Protocol 类型（可选）

如果新方法产生新的更新类型，在 `pkg/protocol/protocol.go` 中添加常量：

```go
const (
    UpdateTypeMyNewEvent = "my_new_event"
)
```

#### 4. 编写测试

```go
package handler

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestMyMethodHandler(t *testing.T) {
    // 使用 store.NewTestStore 创建测试存储
    store := store.NewTestStore(t)

    handler := NewMyMethodHandler(store)

    tests := []struct {
        name    string
        params  myMethodParams
        wantErr bool
    }{
        {name: "valid", params: myMethodParams{Field1: "hello"}, wantErr: false},
        {name: "missing field", params: myMethodParams{}, wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 编组参数
            params, _ := json.Marshal(tt.params)
            req := &protocol.PackageDataRequest{
                ID:     "test-1",
                Method: "my_method",
                Params: params,
            }

            _, err := handler.HandleRequest(context.Background(), nil, req)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### 现有 Handler 参考

| 方法 | 文件 | 复杂度 | 依赖 |
|------|------|--------|------|
| `heartbeat` | `heartbeat.go` | 简单 | `ConnectionStore` |
| `send_message` | `send_message.go` | 复杂 | `Store`, `Broker`, `AgentRegistry` |
| `reload_agents` | `reload_agents.go` | 简单 | `AgentRegistry` |
| `system.register_functions` | `register_functions.go` | 中等 | `FunctionRegistry` |

## 如何添加新的 Store 方法

### 流程概述

1. 在对应的 `*Store` 结构体上添加方法
2. 在测试文件中添加测试
3. 如果方法需要在复合事务中使用，在 `Store` 或 `StoreAPI` 接口中暴露

### 示例：为 MessageStore 添加计数方法

#### 1. 在 `internal/store/message.go` 中添加方法

```go
// CountBySender returns the number of messages sent by a specific user
// in a given conversation.
func (ms *MessageStore) CountBySender(ctx context.Context, convID, senderID string) (int64, error) {
    var count int64
    err := ms.db.WithContext(ctx).
        Model(&model.Message{}).
        Where("conversation_id = ? AND sender_id = ?", convID, senderID).
        Count(&count).Error
    if err != nil {
        return 0, classifyError(fmt.Errorf("store: count by sender: %w", err))
    }
    return count, nil
}
```

#### 2. 如果需要通过 StoreAPI 暴露

在 `internal/store/store.go` 的 `StoreAPI` 接口中添加方法：

```go
type StoreAPI interface {
    // ... existing methods
    CountBySender(ctx context.Context, convID, senderID string) (int64, error)
}
```

代理方法在 `Store` 结构体上实现：

```go
func (s *Store) CountBySender(ctx context.Context, convID, senderID string) (int64, error) {
    return s.Messages.CountBySender(ctx, convID, senderID)
}
```

#### 3. 编写测试

在 `internal/store/message_store_test.go` 中添加：

```go
func TestCountBySender(t *testing.T) {
    db := newTestDB(t)
    store := NewMessageStore(db)

    // 插入测试数据
    msg := &model.Message{
        ID:             uuid.New().String(),
        ConversationID: "conv-1",
        MessageID:      1,
        SenderID:       "alice",
        Content:        "Hello",
        CreatedAt:      time.Now(),
    }
    require.NoError(t, db.WithContext(context.Background()).Create(msg).Error)

    // 测试计数
    count, err := store.CountBySender(context.Background(), "conv-1", "alice")
    assert.NoError(t, err)
    assert.Equal(t, int64(1), count)

    // 测试不存在
    count, err = store.CountBySender(context.Background(), "conv-1", "bob")
    assert.NoError(t, err)
    assert.Equal(t, int64(0), count)
}
```

### Store 方法模式

| 操作 | 方法签名模式 | 事务要求 |
|------|--------------|----------|
| 创建 | `Create(ctx, *model) error` | 单表 |
| 查询 | `Get(ctx, id) (*model, error)` | 读 |
| 列表 | `List*(ctx, ..., limit) ([]*model, error)` | 读 |
| 更新 | `Update(ctx, *model) error` | 单表 |
| 删除 | `Delete(ctx, id) error` | 单表 |

复合操作（如 `SendMessage`）在 `Store` 层面使用 `Transaction` 实现原子性。

## 如何添加新的 MQ 任务类型

### 流程概述

需要修改 3 个文件：

1. 在 `internal/mq/mq.go` 中定义任务类型常量
2. 创建任务 handler（或在现有 handler 文件中添加）
3. 在 `cmd/xyncra-server/main.go` 中注册 handler

### 步骤详解

#### 1. 定义任务类型

在 `internal/mq/mq.go` 中添加常量：

```go
const (
    // ... existing types
    TypeMyNewTask = "mq:my_new_task"
)
```

#### 2. 定义任务 Payload

```go
// myNewTaskPayload is the MQ task payload for mq:my_new_task.
type myNewTaskPayload struct {
    ConversationID string `json:"conversation_id"`
    UserID         string `json:"user_id"`
}
```

#### 3. 创建任务 Handler

可以在 `internal/mq/` 或 `internal/handler/` 中创建，取决于职责：

```go
// internal/handler/my_new_task.go
package handler

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/PineappleBond/xyncra-server/internal/mq"
)

func NewMyNewTaskHandler(logger server.Logger) func(ctx context.Context, task *mq.Task) error {
    return func(ctx context.Context, task *mq.Task) error {
        var payload myNewTaskPayload
        if err := json.Unmarshal(task.Payload, &payload); err != nil {
            return fmt.Errorf("unmarshal payload: %w", err)
        }

        // 业务处理
        // ...

        return nil
    }
}
```

#### 4. 在 `main.go` 中注册

```go
taskHandler := mq.NewTaskHandler()

// 注册现有 handler
taskHandler.Register(mq.TypeSendMessage, handler.NewSendMessageTaskHandler(...))
taskHandler.Register(mq.TypeAgentProcess, agentTaskHandler)

// 注册新的 handler
taskHandler.Register(mq.TypeMyNewTask, handler.NewMyNewTaskHandler(logger))
```

配置任务选项（如重试次数、队列优先级）：

```go
task := &mq.Task{
    Type:     mq.TypeMyNewTask,
    Payload:  payloadBytes,
    Queue:    mq.QueueDefault,
    MaxRetry: 3,
}
if _, err := broker.Enqueue(ctx, task, mq.WithMaxRetry(5)); err != nil {
    log.Printf("enqueue failed (fire-and-forget): %v", err)
}
```

### 任务注册模式

在 `main.go` 中集中注册 MQ 任务 handler：

```go
taskHandler := mq.NewTaskHandler()

// 消息投递
taskHandler.Register(mq.TypeSendMessage,
    handler.NewSendMessageTaskHandler(srv.BroadcastUpdates, srv.Logger()))

// Agent 处理
agentTaskHandler := agent.NewAgentTaskHandler(...)
taskHandler.Register(mq.TypeAgentProcess, agentTaskHandler)

// Agent 恢复
agentResumeHandler := agent.NewAgentResumeHandler(...)
taskHandler.Register(mq.TypeAgentResume, agentResumeHandler)

// 启动 worker
go func() {
    if err := broker.Start(ctx, taskHandler); err != nil {
        log.Printf("broker error: %v", err)
    }
}()
```

## 如何添加新的更新类型

### 流程概述

更新类型用于 `UserUpdate` 和推送通知。新增更新类型需要：

1. 在 `pkg/protocol/protocol.go` 中定义常量
2. 在处理逻辑中使用该类型
3. 在客户端 SDK 中处理该类型

### 示例：添加 `user_online` 更新类型

#### 1. 定义常量

```go
// pkg/protocol/protocol.go
const (
    // ... existing
    UpdateTypeUserOnline = "user_online"
)
```

#### 2. 在 Handler 中使用

```go
// 创建 UserUpdate 记录
update := model.UserUpdate{
    ID:        uuid.New().String(),
    UserID:    memberID,
    Seq:       latestSeq + 1,
    Type:      protocol.UpdateTypeUserOnline,
    Payload:   payload,
    CreatedAt: now,
}
```

#### 3. 在客户端处理

```go
// pkg/client/sync.go 或通过 updateHandler callback
client.OnUpdate(func(update protocol.PackageDataUpdate) {
    switch update.Type {
    case protocol.UpdateTypeUserOnline:
        // 处理用户在线状态变更
    }
})
```

### 更新类型分类

**持久化更新**（Seq > 0，通过 `sync_updates` 拉取）：

| 类型 | 说明 | 对应 Handler |
|------|------|-------------|
| `message` | 新消息 | `send_message` |
| `delete_message` | 消息删除 | `delete_message` |
| `mark_read` | 读游标更新 | `mark_as_read` |
| `conversation` | 会话状态变更 | `create_conversation`, `delete_conversation`, `restore_conversation` |
| `gap` | 合成间隙填充（运行时） | 无 |

**Ephemeral 更新**（Seq = 0，实时推送，不持久化）：

| 类型 | 说明 | 对应 Handler |
|------|------|-------------|
| `typing` | 输入中指示 | `set_typing` |
| `streaming` | 流式文本 | `stream_text` |
| `agent_status` | Agent 状态 | Agent 运行时 |
| `agent_timeout` | Agent 超时 | Agent 运行时 |

## 测试指南

### 单元测试

所有包都应包含充分的单元测试覆盖。测试策略：

| 包 | 测试策略 | 外部依赖 |
|----|----------|----------|
| `internal/handler` | 使用 `miniredis` 和内存数据库 | 无 |
| `internal/server` | 使用 `miniredis` | 无 |
| `internal/agent` | Mock LLM、内存存储 | 无 |
| `internal/mq` | Mock broker | 无 |
| `internal/store` | 使用 SQLite 内存数据库 | 无 |
| `internal/cli` | Mock 连接 | 无 |
| `pkg/protocol` | 纯逻辑测试 | 无 |
| `pkg/client` | Mock WebSocket 服务器 | 无 |
| `pkg/store` | 使用 SQLite 内存数据库 | 无 |

### E2E 测试

`internal/e2e/` 测试套件使用真实 Redis：

```go
// 测试启动前自动连接 Redis
func TestMain(m *testing.M) {
    redisAddr := flag.String("redis-addr", "localhost:16379", "Redis address for E2E tests")
    flag.Parse()
    // ...
    os.Exit(m.Run())
}
```

### 编写测试的建议

1. **使用表驱动测试** — 清晰的输入/输出定义
2. **使用 testify** — `assert` 和 `require` 提供可读的断言
3. **隔离测试数据** — 每个测试使用独立的数据库/集合
4. **覆盖边界条件** — 空输入、nil 依赖、并发竞争
5. **标记短测试** — 使用 `testing.Short()` 隔离需要外部资源的测试

## 常见开发工作流

### 调试一个 Agent 执行问题

1. 启用 LLM 日志：`export XYNCRA_LLM_LOG_DIR=./llm-logs`
2. 启动服务器，发送消息触发 Agent
3. 检查 `llm-logs/llm-calls.log` 查看 LLM 请求/响应
4. 检查 Agent 状态转换日志 `agent_status`
5. 在 `internal/agent/executor.go` 中添加临时调试日志

### 测试一个新 RPC 方法

```bash
# 1. 编译
make build

# 2. 启动服务器
./bin/xyncra-server -db-dsn /tmp/test.db

# 3. 使用 websocat 或 curl 测试 WebSocket
websocat ws://localhost:8080/ws?user_id=testuser
# 发送 JSON：{"type":0,"data":{"id":"1","method":"my_method","params":{"field1":"value1"}}}
```

### 处理数据库迁移

GORM AutoMigrate 自动处理表创建和列变更。复杂迁移需要手动 SQL：

```go
// internal/store/store.go 的 AutoMigrate 方法
func (s *Store) AutoMigrate(ctx context.Context) error {
    // GORM 自动迁移
    s.db.WithContext(ctx).AutoMigrate(
        &model.Conversation{},
        &model.Message{},
    )

    // 手动迁移：替换索引
    s.db.Exec("DROP INDEX IF EXISTS idx_messages_client_message_id")
    s.db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_msg_client_id_sender ON messages(client_message_id, sender_id)")

    return nil
}
```

### 代码审查清单

提交 PR 前自查：

- [ ] `make fmt` 通过
- [ ] `make vet` 通过
- [ ] `make test` 通过
- [ ] 新功能有测试覆盖
- [ ] Handler 是 stateless 的（并发安全）
- [ ] 错误使用 `%w` 包装
- [ ] 日志使用 `log.Printf` 或 `Logger` 接口
- [ ] 设计决策参考决策 ID（D-NNN）
- [ ] 无硬编码的敏感信息
- [ ] 无 TODO 遗留（除非有 issue 引用）
