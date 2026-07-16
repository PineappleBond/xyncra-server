# Xyncra Server 项目上下文

> 此文件由 `xyncra-task-planner` SKILL 使用。包含项目架构、已实现组件、关键接口签名和产品决策摘要。
> 分析任务时按需参考，不需要全部读入。

---

## 1. 架构概览

```text
cmd/xyncra-server/main.go          ← 入口：配置 + 启动 + 优雅关闭
pkg/protocol/protocol.go           ← 协议定义：Package/Request/Response/Updates
internal/server/                   ← WebSocket 服务器层
internal/store/                    ← 数据持久化层（GORM）
internal/store/model/              ← 数据模型
internal/mq/                       ← 消息队列层（Asynq/Redis）
```

### 数据流

```text
客户端 ──WebSocket──▶ WebSocketServer ──路由──▶ DefaultMessageHandler
                                                    │
                                              MethodHandler（已实现12个）
                                                    │
                                            ┌───────┴───────┐
                                            ▼               ▼
                                         Store            Broker (MQ)
                                       (数据库)        (异步任务队列)
                                            │               │
                                            ▼               ▼
                                      持久化完成        Worker 消费
                                            │               │
                                            └───────┬───────┘
                                                    ▼
                                         BroadcastUpdates()
                                         （推送到在线用户）
```

---

## 2. 已实现组件清单

### 协议层 (`pkg/protocol/protocol.go`)

- `PackageType`: Request(0), Response(1), Updates(2)
- `Package`: 顶层消息信封（Version + Type + Data）
- `PackageDataRequest`: 客户端请求（ID + Method + Params）
- `PackageDataResponse`: 服务端响应（ID + Code + Msg + Data）
- `PackageDataUpdates`: 推送通知批次
- `PackageDataUpdate`: 单条增量更新（Seq + Payload + CreatedAt）
- `ResponseCode`: OK(0), Error(-1)

### 服务器层 (`internal/server/`)

| 文件 | 组件 | 状态 |
|------|------|------|
| `websocket_server.go` | WebSocketServer, 连接升级, BroadcastUpdates, handleHealth | ✅ 完成 |
| `websocket_client.go` | Client, 读写泵, SendPackage, Ping/Pong | ✅ 完成 |
| `websocket_handler.go` | DefaultMessageHandler, MethodHandler, RegisterMethod | ✅ 完成 |
| `server.go` | BaseServer 生命周期管理 | ✅ 完成 |
| `connection_store.go` | ConnectionStore 接口, ConnectionInfo | ✅ 完成 |
| `redis_connection_store.go` | Redis 实现（支持 TTL, per-user 限制） | ✅ 完成 |
| `memory_connection_store.go` | 内存实现（测试用） | ✅ 完成 |

### 存储层 (`internal/store/`)

| 文件 | 组件 | 状态 |
|------|------|------|
| `store.go` | Store, StoreAPI, SendMessage（事务）, AutoMigrate, Transaction | ✅ 完成 |
| `db.go` | Database, 多数据库驱动支持 | ✅ 完成 |
| `conversation.go` | ConversationStore: Create/Get/GetByUser/Update/Delete | ✅ 完成 |
| `message.go` | MessageStore: Create/Get/ListByConversation/Search/Delete/CountUnread/Restore | ✅ 完成 |
| `user_update.go` | UserUpdateStore: Create/ListByUser/GetLatestSeq/CleanupExpired | ✅ 完成 |
| `errors.go` | 错误分类: ErrNotFound, ErrDuplicate, ErrConstraint 等 | ✅ 完成 |

### 数据模型 (`internal/store/model/`)

- `Conversation`: ID, UserID1, UserID2, Type, Title, Pinned, Muted, AvatarURL, Description, LastProcessedMessageID, LastReadMessageID1, LastReadMessageID2, LastMessageAt, CreatedAt, UpdatedAt, DeletedAt
- `Message`: ID, ClientMessageID(幂等键), ConversationID, MessageID(uint32), SenderID, Content, Type, ReplyTo, Status, CreatedAt, DeletedAt
- `UserUpdate`: ID, UserID, Seq(uint32递增), Payload([]byte), CreatedAt

### 消息队列层 (`internal/mq/`)

| 文件 | 组件 | 状态 |
|------|------|------|
| `mq.go` | Broker 接口, Task 类型, 任务类型常量, 队列优先级 | ✅ 完成 |
| `asynq.go` | AsynqBroker 实现: Enqueue/Start/Stop/GetTaskState | ✅ 完成 |
| `handler.go` | TaskHandler: Register/Unregister/ProcessTask | ✅ 完成 |
| `options.go` | EnqueueOption: WithQueue, WithMaxRetry, WithUnique 等 | ✅ 完成 |

### 已注册处理的 MQ 任务类型

```go
TypeSendMessage = "mq:send_message"  // 已在 main.go 中注册 NewSendMessageTaskHandler
```

其他任务类型（sync_updates, push_notification, presence_broadcast, conversation_sync）已定义常量但尚未注册处理函数。

---

## 3. 已实现的业务层

### 已注册的 RPC 方法（12 个，见 `internal/handler/register.go`）

| 方法名 | Handler 文件 | 依赖 | 相关决策 |
|--------|-------------|------|----------|
| `heartbeat` | heartbeat.go | ConnStore | D-010 |
| `send_message` | send_message.go | Store, Broker | D-006, D-007 |
| `sync_updates` | sync_updates.go | Store | D-009 |
| `create_conversation` | create_conversation.go | Store | D-011 |
| `list_conversations` | list_conversations.go | Store | - |
| `get_messages` | get_messages.go | Store | D-008 |
| `search_messages` | search_messages.go | Store | - |
| `get_conversation` | get_conversation.go | Store | - |
| `delete_conversation` | delete_conversation.go | Store | D-013 |
| `restore_conversation` | restore_conversation.go | Store | D-015 |
| `delete_message` | delete_message.go | Store | D-014 |
| `mark_as_read` | mark_as_read.go | Store | D-012 |

### 已注册的 MQ TaskHandler

| 任务类型 | 处理函数 | 注册位置 |
|----------|----------|----------|
| `mq:send_message` | NewSendMessageTaskHandler | cmd/xyncra-server/main.go |

---

## 4. 关键接口签名

```go
// === 消息处理 ===

// MessageHandler — 处理 WebSocket 入站消息
type MessageHandler interface {
    HandleMessage(ctx context.Context, client *Client, pkg *protocol.Package)
}

// MethodHandler — 处理具体 RPC 方法（如 send_message）
type MethodHandler interface {
    HandleRequest(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error)
}

// MethodHandlerFunc — 函数适配器
type MethodHandlerFunc func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error)

// DefaultMessageHandler — 默认消息路由器
func (h *DefaultMessageHandler) RegisterMethod(method string, handler MethodHandler)
func (h *DefaultMessageHandler) RegisterMethodFunc(method string, fn MethodHandlerFunc)

// === Client ===

// Client 代表一个 WebSocket 连接
type Client struct { ... }
func (c *Client) UserID() string        // 所属用户 ID
func (c *Client) ConnID() string        // 连接唯一 ID
func (c *Client) SendPackage(pkg *protocol.Package) error  // 发送消息包
func (c *Client) Close()                // 关闭连接
func (c *Client) Done() <-chan struct{}  // 连接关闭信号

// === WebSocketServer ===

func (s *WebSocketServer) BroadcastUpdates(userID string, updates *protocol.PackageDataUpdates)
// 向指定用户的所有本地连接推送更新

// === StoreAPI ===

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

// === Broker (MQ) ===

type Broker interface {
    Enqueue(ctx context.Context, task *Task, opts ...EnqueueOption) (string, error)
    Start(ctx context.Context, handler Handler) error
    Stop()
    GetTaskState(ctx context.Context, taskID string) (TaskState, error)
}

type Task struct {
    Type      string          // 任务类型（如 "mq:send_message"）
    Payload   json.RawMessage // 任务数据
    ID        string          // 可选，自定义 ID
    Queue     string          // 目标队列（critical/default/low）
    MaxRetry  int
    Timeout   time.Duration
    ProcessIn time.Duration   // 延迟处理
}

// === TaskHandler ===

type TaskHandler struct { ... }
func NewTaskHandler() *TaskHandler
func (th *TaskHandler) Register(taskType string, fn func(ctx context.Context, task *Task) error) bool

// === ConnectionStore ===

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

type ConnectionInfo struct {
    ID              string
    UserID          string
    SessionID       string
    DeviceID        string
    DeviceType      string
    IPAddress       string
    Protocol        string
    LastHeartbeatAt time.Time
    Status          string
    Metadata        map[string]string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    TTL             time.Duration
}

// === Store 子存储 ===

// ConversationStore
func (cs *ConversationStore) Create(ctx context.Context, conv *model.Conversation) error
func (cs *ConversationStore) Get(ctx context.Context, id string) (*model.Conversation, error)
func (cs *ConversationStore) GetByUsers(ctx context.Context, user1, user2 string) (*model.Conversation, error)
func (cs *ConversationStore) GetByUser(ctx context.Context, userID string, offset, limit int) ([]*model.Conversation, error)
func (cs *ConversationStore) Update(ctx context.Context, conv *model.Conversation) error
func (cs *ConversationStore) Delete(ctx context.Context, id string) error
func (cs *ConversationStore) Restore(ctx context.Context, id string) error
func (cs *ConversationStore) UpdateLastMessage(ctx context.Context, convID string, lastMessageAt time.Time, lastProcessedMessageID uint32) error
func (cs *ConversationStore) SearchByTitle(ctx context.Context, userID, title string, limit int) ([]*model.Conversation, error)
func (cs *ConversationStore) GetUnscoped(ctx context.Context, id string) (*model.Conversation, error)
func (cs *ConversationStore) UpdateLastRead(ctx context.Context, convID, userID string, messageID uint32) error

// MessageStore
func (ms *MessageStore) Create(ctx context.Context, msg *model.Message) error
func (ms *MessageStore) Get(ctx context.Context, id string) (*model.Message, error)
func (ms *MessageStore) GetByClientMessageID(ctx context.Context, clientMessageID string) (*model.Message, error)
func (ms *MessageStore) ListByConversation(ctx context.Context, convID string, afterMessageID uint32, limit int) ([]*model.Message, error)
func (ms *MessageStore) SearchByConversation(ctx context.Context, convID, content string, afterMessageID uint32, limit int) ([]*model.Message, error)
func (ms *MessageStore) ListByTimeRange(ctx context.Context, convID string, startTime, endTime time.Time, limit int) ([]*model.Message, error)
func (ms *MessageStore) Delete(ctx context.Context, id string) error
func (ms *MessageStore) Restore(ctx context.Context, id string) error
func (ms *MessageStore) DeleteByConversation(ctx context.Context, convID string) error
func (ms *MessageStore) RestoreByConversation(ctx context.Context, convID string) (int64, error)
func (ms *MessageStore) CountUnread(ctx context.Context, convID string, afterMessageID uint32) (int64, error)

// UserUpdateStore
func (us *UserUpdateStore) Create(ctx context.Context, updates []model.UserUpdate) error
func (us *UserUpdateStore) ListByUser(ctx context.Context, userID string, afterSeq uint32, limit int) ([]*model.UserUpdate, error)
func (us *UserUpdateStore) GetLatestSeq(ctx context.Context, userID string) (uint32, error)
func (us *UserUpdateStore) CleanupExpiredBefore(ctx context.Context, before time.Time) (int64, error)
func (us *UserUpdateStore) CleanupExpired(ctx context.Context) (int64, error)
```

---

## 5. 产品决策摘要

> 完整内容见 `docs/decisions/PRODUCT_DECISIONS.md`

| 编号 | 决策 | 含义 |
|------|------|------|
| D-001 | 开箱即用，零配置 | 合理默认值 + Functional Options |
| D-002 | 服务器不做鉴权 | 认证由业务服务器通过反向代理负责 |
| D-003 | 内网部署模型 | TLS/CORS/RateLimit 由反向代理处理 |
| D-004 | 默认接受任意 Origin | 不是 TODO，是设计决策 |
| D-005 | user_id 查询参数认证 | 开发便利，非安全机制 |
| D-006 | client_message_id 幂等性 | 数据库 uniqueIndex 保证，重复返回 duplicate=true |
| D-007 | MQ 入队 fire-and-forget | MQ 失败不影响消息持久化 |
| D-008 | MessageID uint32 递增 | 每会话内单调递增，事务内分配 |
| D-009 | sync_updates 分页 | after_seq + limit，默认100上限500 |
| D-010 | 被动续期策略 | 仅 heartbeat 触发 TTL 续期 |
| D-011 | create_conversation 幂等 | find-or-create，用户对唯一性保证 |
| D-012 | mark_as_read MAX 语义 | 只向前推进，不后退 |
| D-013 | delete_conversation 级联软删除 | 会话+消息同一事务内软删除 |
| D-014 | delete_message 发送者权限 | 仅 SenderID 可删除 |
| D-015 | restore_conversation 级联恢复 | 会话+消息同一事务内恢复 |

**对实现的影响：**

- 不实现：TLS、CORS、CSRF、Rate Limit、认证
- 实现：消息推送、连接管理、心跳、广播
- 提供扩展点：WSWithAuthenticate 等选项

---

## 6. 代码规范

- 注释使用英文，godoc 风格
- 错误使用 `fmt.Errorf("context: %w", err)` 包装
- 遵循 Functional Options 模式
- 新功能必须有单元测试
- 测试文件放在对应包目录下（`xxx_test.go`）
- 使用 `github.com/google/uuid` 生成 ID
- 使用 `github.com/gorilla/websocket` 处理 WebSocket
- 数据库使用 `gorm.io/gorm`
- 消息队列使用 `github.com/hibiken/asynq`
