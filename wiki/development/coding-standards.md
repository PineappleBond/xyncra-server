---
last_updated: 2026-07-17
---

# 编码规范

> last_updated: 2026-07-17

## 概述

本文档定义 Xyncra 项目的 Go 编码规范。所有贡献者必须遵循以下约定，确保代码风格一致、可维护性高。

## Go 代码风格

### 基本原则

- 遵循 [Effective Go](https://go.dev/doc/effective_go) 和 [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) 的所有规范
- 使用 `gofmt -s` 格式化代码（`make fmt` 自动执行）
- 使用 `go vet ./...` 进行静态分析（`make vet` 自动执行）
- 不允许在编译或 lint 中出现 warning

### 导入规范

导入按以下顺序分组，每组之间空一行：

```go
import (
    // 标准库
    "context"
    "fmt"
    "log"

    // 第三方依赖
    "github.com/google/uuid"
    "gorm.io/gorm"

    // 内部包
    "github.com/PineappleBond/xyncra-server/internal/mq"
    "github.com/PineappleBond/xyncra-server/internal/store"
)
```

### 文件结构

每个 Go 文件按以下顺序组织：

1. `package` 声明（含包注释，可选）
2. `import` 块
3. 常量（`const`）
4. 类型定义（`type`）
5. 接口（`interface`）
6. 结构体（`struct`）
7. 工厂函数（`New*`）
8. 方法（按逻辑分组）
9. 辅助函数

### 分段注释

在大型文件中，使用 `// ---` 注释线将代码分为逻辑段落：

```go
// --------------------------------------------------------------------------
// Sentinel errors
// --------------------------------------------------------------------------

var (
    ErrNotFound = errors.New("store: record not found")
)

// --------------------------------------------------------------------------
// Core types
// --------------------------------------------------------------------------

type Task struct {
    ...
}
```

Xyncra 代码库中统一使用 74 个连字符的注释线。

## 命名约定

### 通用规则

| 类别 | 约定 | 示例 |
|------|------|------|
| 包名 | 小写、单数、无下划线 | `store`, `mq`, `agent` |
| 类型 | PascalCase | `ConversationStore`, `TaskHandler` |
| 导出字段 | PascalCase | `UserID`, `ClientMessageID` |
| 非导出字段 | camelCase | `db`, `mu`, `handlers` |
| 接口名 | `*er` 后缀或描述性 | `StoreAPI`, `Broker`, `Handler` |
| 错误变量 | `Err` 前缀 | `ErrNotFound`, `ErrDuplicateKey` |
| 错误类型 | `Error` 后缀 | `HandlerError` |
| 测试辅助函数 | `new*` 或 `create*` | `newTestStore`, `createTestClient` |

### 包名规范

```
internal/agent/       → agent.*
internal/server/      → server.*
internal/handler/     → handler.*
internal/store/       → store.*
internal/store/model/ → model.*
internal/mq/          → mq.*
internal/cli/         → cli.*
internal/cleanup/     → cleanup.*
internal/e2e/         → e2e.*
pkg/protocol/         → protocol.*
pkg/client/           → client.*
pkg/store/            → store (客户端侧, 与 internal/store 结构平行)
```

### 常量命名

- 使用大写驼峰（exported）：`TypeSendMessage`, `QueueCritical`
- 包级常量使用 `Default` 前缀：`DefaultRetryCount`, `DefaultUniqueTTL`
- 模型状态常量：`AgentStatusIdle`, `QuestionStatusPending`

## 包组织原则

### `internal/` vs `pkg/`

- **`internal/`**：私有实现细节，外部模块不得导入。包含服务器核心逻辑。
- **`pkg/`**：可导出的公共库，供外部项目使用。包含协议类型、客户端 SDK、客户端存储。

### 分层依赖

依赖方向必须从上层流向下层：

```
cmd/ (入口)
  → internal/handler/ (RPC 方法处理器)
    → internal/server/ (连接管理、WebSocket 生命周期)
    → internal/agent/ (AI 代理运行时)
    → internal/mq/ (消息队列抽象)
    → internal/store/ (持久化层)
      → gorm (数据库)
```

不允许反向依赖：`internal/store` 不得导入 `internal/server`。

### 接口定义位置

接口定义在使用侧，而非实现侧：

```go
// internal/server/websocket_handler.go
type MessageHandler interface {
    HandleMessage(ctx context.Context, client *Client, pkg *protocol.Package)
}

// internal/handler/send_message.go 中的实现
type sendMessageHandler struct { ... }
func (h *sendMessageHandler) HandleRequest(...) { ... }
```

### 子存储模式

`internal/store` 使用复合模式：`Store` 聚合多个子存储（`ConversationStore`、`MessageStore`、`UserUpdateStore`、`QuestionStore`）。每个子存储负责单一实体的 CRUD。

## 注释和文档要求

### 包注释

每个包应包含包注释，说明其职责：

```go
// Package mq provides the message queue abstraction layer for the Xyncra
// messaging system. It defines a broker interface for asynchronous task
// processing and ships with an implementation backed by Asynq (Redis).
package mq
```

### 导出标识符注释

所有导出的类型、函数、常量必须有注释：

```go
// SendMessageResult is returned by Store.SendMessage after a successful atomic
// persist. It contains the message with its allocated MessageID and the
// per-user update records with their allocated seq values.
type SendMessageResult struct {
    Message *model.Message
    Updates []model.UserUpdate
}
```

### 设计决策引用

当代码实现特定的产品决策时，在注释中引用决策 ID：

```go
// Idempotency (D-006) is enforced by catching the unique constraint violation
// on client_message_id after the insert, avoiding a TOCTOU race.
```

### 复杂逻辑注释

对非显而易见的算法和竞态处理必须加注释：

```go
// CancelDevice before Upgrade: fail pending reverse-RPC requests for
// this device immediately (D-095). Moved here (before Upgrade) so that
// the async cleanup goroutine cannot cancel requests that belong to the
// new connection after it registers.
```

### TODO 注释

使用标准 TODO 格式，附带责任人或 issue 引用：

```go
// TODO(username): implement retry with exponential backoff
// TODO(issue#42): add connection pool metrics
```

## 错误处理模式

### 哨兵错误

使用包级 `var` 声明哨兵错误，并使用 `errors.Is` 检查：

```go
var (
    ErrNotFound     = errors.New("store: record not found")
    ErrDuplicateKey = errors.New("store: duplicate key")
)
```

### 错误包装

始终使用 `fmt.Errorf` 的 `%w` 动词包装错误：

```go
return nil, fmt.Errorf("store: send message - get conversation: %w", err)
```

### 分类错误

在数据库层使用 `classifyError` 将 GORM/驱动错误翻译为包级哨兵错误：

```go
func classifyError(err error) error {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return ErrNotFound
    }
    // ...
}
```

### 结构化错误响应

在 handler 层使用 `HandlerError` 返回结构化错误：

```go
return nil, protocol.NewValidationError("missing required field: user_id")
return nil, protocol.NewNotFoundError("conversation not found")
return nil, protocol.NewInternalError(fmt.Errorf("get conversation: %w", err))
```

### Fire-and-Forget 错误

MQ 入队失败不阻塞主流程：记录日志后继续执行：

```go
if _, err := h.broker.Enqueue(ctx, task); err != nil {
    log.Printf("send_message: MQ enqueue failed (fire-and-forget): %v", err)
}
```

### Nil-Safe 模式

可选依赖使用 nil-safe 模式处理：

```go
if h.agentRegistry != nil && !strings.HasPrefix(senderID, "agent/") {
    // agent detection logic
}
```

## 并发安全

### 锁使用

- `sync.Mutex`：写操作（注册、删除）
- `sync.RWMutex`：读多写少的场景（查询、路由）
- 尽量缩小锁范围，避免在锁中执行 IO 操作

```go
func (th *TaskHandler) Register(taskType string, fn func(ctx context.Context, task *Task) error) {
    th.mu.Lock()
    defer th.mu.Unlock()
    // ...
}
```

### 编译时接口检查

使用 `var _ Interface = (*Type)(nil)` 确保类型实现接口：

```go
var _ StoreAPI = (*Store)(nil)
var _ Server = (*WebSocketServer)(nil)
var _ NodeBroadcaster = (*NoopBroadcaster)(nil)
```

## Go 版本特性

### `range over int`

Go 1.22+ 支持直接遍历整数区间：

```go
for i := range 5 {
    // i = 0, 1, 2, 3, 4
}
```

适用于固定次数的循环，语义比 `for i := 0; i < n; i++` 更简洁。

### `range over func`（迭代器）

Go 1.23+ 支持函数迭代器，标准库 `maps`、`slices` 包提供配套工具：

```go
for k, v := range maps.All(m) {
    // 遍历 map 的键值对
}

for _, item := range slices.Backward(s) {
    // 反向遍历切片
}
```

### 泛型使用

项目使用 Go 1.26 工具链，支持完整的泛型特性：

```go
// 类型参数推导 — 调用时通常无需显式指定类型
func NewStore[T model.Modeler](db *gorm.DB) *Store[T] {
    return &Store[T]{db: db}
}

// 泛型约束使用 interface 中的类型元素
type Numeric interface {
    ~int | ~int64 | ~float64
}
```

### 适用原则

1. **新代码优先使用新语法**：`range over int` 替代计数循环，迭代器替代手写回调
2. **泛型适度**：仅在消除 boilerplate 或保证类型安全时有收益的场景使用
3. **不主动重构**：不为了使用新特性而修改已验证的旧代码
4. **编译兼容**：确保代码通过 `go vet ./...` 且无弃用警告

## Go 1.26 新特性编码指导

### 迭代器（range over func）

Go 1.26 支持自定义迭代器，如标准库 `slices.Backward`、`slices.Chunk`、`maps.Keys` 等。Xyncra 项目在以下场景中使用：

```go
// 反向遍历切片
for i, msg := range slices.Backward(messages) {
    // ...
}

// 按块遍历
for chunk := range slices.Chunk(allItems, 100) {
    // 批量处理
}
```

### 增强的 slices / maps 标准库

优先使用标准库函数，避免手动循环：

```go
// 使用 slices 取代手动 append
collected := slices.Collect(iter)

// 使用 maps 操作
keys := maps.Keys(m)
maps.DeleteFunc(m, func(k string, v int) bool { return v == 0 })
```

### 零值问题修复

Go 1.26 改进了 `sync.Map` 的 `Range` 行为，修复了 range 中修改 map 可能导致的问题。新代码应使用 `maps` 包中的函数替代手动 range+delete。

## 测试规范

### 包命名

测试文件与被测试文件同名，加 `_test` 后缀：

```
conversation.go        → conversation_test.go
send_message.go        → send_message_test.go
websocket_server.go    → websocket_server_test.go
```

### 测试辅助函数

测试辅助函数以 `new` 或 `create` 开头：

```go
func newTestStore(t *testing.T) *store.Store
func createTestClient(t *testing.T) *server.Client
```

### 表驱动测试

使用标准表驱动模式：

```go
func TestSendMessage(t *testing.T) {
    tests := []struct {
        name    string
        params  sendMessageParams
        wantErr error
    }{
        {name: "missing conversation_id", params: sendMessageParams{...}, wantErr: ...},
        {name: "valid message", params: sendMessageParams{...}, wantErr: nil},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

### 短测试隔离

单元测试使用 `-short` 标志跳过需要外部依赖（Redis、Docker）的测试：

```go
func TestRedisConnectionStore(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping test requiring Redis in short mode")
    }
}
```

## 提交信息格式

### 规范

使用 Conventional Commits 格式：

```
<type>(<scope>): <description>

[optional body]
```

### Type

| Type | 用途 |
|------|------|
| `feat` | 新功能 |
| `fix` | 修复 |
| `refactor` | 重构 |
| `test` | 测试 |
| `docs` | 文档 |
| `chore` | 构建/工具 |

### Scope

| Scope | 对应模块 |
|-------|----------|
| `server` | WebSocket server |
| `handler` | RPC handlers |
| `agent` | Agent runtime |
| `store` | Data access layer |
| `mq` | Message queue |
| `cli` | CLI client |
| `protocol` | Wire protocol |
| `e2e` | E2E tests |

### 示例

```
feat(handler): add system.register_functions RPC method

Implement client-declared function registration via ReverseRPC. 
Each device can register up to 200 functions with JSON Schema parameters.
Registration is cleaned up automatically on device disconnect.

D-098, D-099
```

```
fix(server): prevent nil pointer in device replacement path

The oldClients map could be nil when cancelDevice is called before
registering any old connections. Add nil check before iterating.

D-095
```
