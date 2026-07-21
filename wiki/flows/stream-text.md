# Stream Text 业务流程

本文档描述 `stream_text` RPC 方法的完整业务流程，包括主流程、边缘场景和依赖关系。

---

## 目录

- [概述](#概述)
- [主流程](#主流程)
- [边缘场景](#边缘场景)
- [依赖关系](#依赖关系)
- [关键设计决策](#关键设计决策)

---

## 概述

`stream_text` 是一个 ephemeral（瞬时）RPC 方法，用于在会话中广播流式文本更新。它主要用于 Agent 执行过程中的实时文本流推送，让用户能够看到 Agent 逐步生成的回复。

### 触发条件

- Agent 执行过程中，每次收到 LLM 流式响应时调用
- 客户端也可以使用此方法实现自己的流式文本功能
- 最后一次调用应设置 `is_done: true`

### 关键特性

- **Ephemeral**：Seq=0，不持久化到数据库
- **Rate-limited**：每个用户每个会话每 50ms 最多 1 次（20/s）
- **Cumulative text**：每次发送的是累积文本快照，不是增量
- **Broadcast to all**：广播给所有成员（包括发送者自己）
- **No MQ broker**：不经过消息队列（broker），但跨节点部署时通过 Redis Pub/Sub 广播

---

## 主流程

```mermaid
sequenceDiagram
    participant C as 客户端/Agent
    participant WS as WebSocket Server
    participant H as StreamTextHandler
    participant S as Store
    participant BU as BroadcastUpdates
    participant BL as broadcastLocal
    participant NB as NodeBroadcaster

    C->>WS: stream_text {conversation_id, stream_id, text, is_done}
    WS->>H: HandleRequest(ctx, client, req)

    H->>H: 解析参数
    H->>S: ConversationStore.Get(conversation_id)
    S-->>H: 返回会话

    alt 会话不存在
        H-->>WS: NotFoundError
        WS-->>C: 错误响应
    end

    H->>H: 从 conversation.UserID1/UserID2 验证成员身份

    alt 非成员
        H-->>WS: PermissionDeniedError
        WS-->>C: 错误响应
    end

    H->>H: 检查 Rate Limiter (50ms/20s)

    alt Rate-limited
        H-->>WS: 返回 OK (静默)
        WS-->>C: 成功响应 (不广播)
    end

    H->>H: 构建 streamingBroadcastPayload + PackageDataUpdate{Seq:0, Type:"streaming"}
    loop 遍历每个成员
        H->>BU: broadcastFn(memberID, updates)
        BU->>BL: 本地 WebSocket 广播
        BU->>NB: Redis Pub/Sub 跨节点广播
    end

    H-->>WS: 返回 {status: ok}
    WS-->>C: 成功响应
```

### 详细步骤

1. **解析参数**：提取 `conversation_id`、`stream_id`、`text`、`is_done`
2. **校验必填字段**：`conversation_id` 和 `stream_id` 不能为空（`text` 和 `is_done` 无校验，空 text 允许）
3. **获取会话**：通过 `ConversationStore().Get()` 查询会话（单一 SELECT）
4. **身份验证**：从会话模型的 `UserID1`/`UserID2` 字段派生成员列表，验证调用者在其中（无额外成员表查询）
5. **Rate limiting**：检查每个用户每个会话的 rate limiter（50ms 1 次）
   - Key 格式：`userID:conversationID`
   - 超限时静默返回 OK（不报错）
   - 注意：被限流的调用也会更新 `lastAccess`，防止条目被 cleanup 提前清理
6. **构建 update**：
   - 内层 payload（`streamingBroadcastPayload`）：`stream_id`、`user_id`、`conversation_id`、`text`、`is_done`、`timestamp`（Unix 秒）
   - 外层 `PackageDataUpdate`：`Seq=0`（ephemeral）、`Type="streaming"`、`CreatedAt` 省略（omitempty）
   - 包装为 `PackageDataUpdates{Updates: [update]}`
7. **广播**：遍历所有会话成员，调用 `broadcastFn`（即 `WebSocketServer.BroadcastUpdates`）
   - **本地广播**：推送到该成员在本节点的所有在线 WebSocket 连接
   - **跨节点广播**：通过 `NodeBroadcaster.Publish` 发布到 Redis Pub/Sub 频道 `xyncra:broadcast:{userID}`，包含 `sourceNodeID` 以避免发送节点重复接收
   - 单个成员广播失败仅记录 Info 日志，不影响其他成员
8. **返回成功**：返回 `{status: ok}`

---

## 边缘场景

### 1. 参数校验失败

```mermaid
flowchart TD
    A[解析参数] --> B{conversation_id 为空?}
    B -->|是| C[返回 ValidationError]
    B -->|否| D{stream_id 为空?}
    D -->|是| E[返回 ValidationError]
    D -->|否| F[继续]
```

| 场景 | 处理方式 |
|------|----------|
| `conversation_id` 为空 | 返回 `ValidationError('missing required field: conversation_id')` |
| `stream_id` 为空 | 返回 `ValidationError('missing required field: stream_id')` |
| JSON 解析失败 | 返回 `ValidationError('invalid params')` |
| `text` 为空字符串 | 允许，无校验 |

### 2. 会话不存在

```mermaid
flowchart TD
    A[获取会话] --> B{会话存在?}
    B -->|否| C[返回 NotFoundError]
    B -->|是| D[继续]
```

| 场景 | 处理方式 |
|------|----------|
| 会话不存在 | 返回 `NotFoundError('conversation not found')` |
| 会话已被软删除 | GORM 自动过滤，等同于不存在 |

### 3. 非成员操作

```mermaid
flowchart TD
    A[验证成员身份] --> B{是会话成员?}
    B -->|否| C[返回 PermissionDeniedError]
    B -->|是| D[继续]
```

| 场景 | 处理方式 |
|------|----------|
| 调用者非会话成员 | 返回 `PermissionDeniedError('user is not a member of the conversation')` |

成员身份通过会话模型的 `UserID1`/`UserID2` 字段判断（1-on-1 会话模型），不查询独立的成员表。

### 4. Rate Limiting

```mermaid
flowchart TD
    A[检查 Rate Limiter] --> B{距上次 > 50ms?}
    B -->|否| C[静默返回 OK (不广播)]
    B -->|是| D[记录当前时间, 继续广播]
```

| 场景 | 处理方式 |
|------|----------|
| 50ms 内重复发送 | 静默返回 OK，不广播（不是错误） |
| Rate limiter entry 过期 | 后台 cleanup goroutine 每 5 分钟清理 10 分钟未访问的条目 |
| 被限流的调用 | `allow()` 仍更新 `lastAccess`，防止条目被 cleanup 提前清理 |
| cleanup goroutine 生命周期 | 无关闭机制，Handler 构造时启动后永久运行 |

### 5. 广播失败

```mermaid
flowchart TD
    A[广播给成员] --> B{广播成功?}
    B -->|失败| C[记录日志, 继续下一个成员]
    B -->|成功| D[继续]
```

| 场景 | 处理方式 |
|------|----------|
| 单个成员本地广播失败 | 记录 Info 日志（非 Error），继续处理其他成员 |
| 跨节点 Pub/Sub 发布失败 | 记录 Error 日志，不影响本地广播结果和返回值 |
| 所有成员都离线 | 所有广播失败，但不影响返回值（ephemeral 消息对离线用户静默丢弃） |

### 6. Stream ID 管理

```mermaid
flowchart TD
    A[客户端生成 stream_id] --> B[发送多次 stream_text]
    B --> C[每次 text 是累积快照]
    C --> D[最后一次设置 is_done=true]
    D --> E[客户端清理 stream 状态]
```

| 场景 | 处理方式 |
|------|----------|
| `stream_id` 重复使用 | 客户端应为每次流式会话生成新的 UUID |
| `is_done` 未发送 | 客户端应实现超时检测，自动清理未完成的 stream |
| `text` 为空 | 允许，表示清空当前流式文本 |

---

## 依赖关系

### 内部依赖

| 组件 | 用途 |
|------|------|
| `store.StoreAPI` | 获取会话信息（`ConversationStore().Get()`），成员身份从会话模型派生 |
| `broadcastFn` | 即 `WebSocketServer.BroadcastUpdates`，负责本地 + 跨节点广播 |

### 外部依赖

| 组件 | 用途 | 场景 |
|------|------|------|
| Redis Pub/Sub | 跨节点广播 via `NodeBroadcaster` | 仅多节点部署时使用，单节点为 `NoopBroadcaster` |

不依赖消息队列（broker）。`stream_text` 的广播直接通过 WebSocket（本地）和 Redis Pub/Sub（跨节点）完成。

### 数据库操作

| 操作 | 表 | 说明 |
|------|-----|------|
| SELECT | conversations | 获取会话信息（`ConversationStore().Get()`） |

仅一次 SELECT 查询。成员身份从查询结果的 `UserID1`/`UserID2` 字段派生，无额外成员表查询。

### Redis 操作

| 操作 | 用途 | 场景 |
|------|------|------|
| PUBLISH | `xyncra:broadcast:{userID}` | 仅多节点部署，通过 `RedisNodeBroadcaster.Publish` |

Handler 自身不直接操作 Redis。Redis 操作发生在 `broadcastFn` 的跨节点路径中。

### Rate Limiter 清理

Handler 构造时启动后台 goroutine，每 5 分钟执行一次清理：

- 遍历 `limiters` sync.Map，检查每个条目的 `lastAccess` 时间
- 删除超过 10 分钟未访问的条目
- 防止长时间运行时 rate limiter 条目无限增长
- 注意：`allow()` 在每次调用时（包括被限流的调用）都会更新 `lastAccess`，因此只有完全不活跃的条目才会被清理

---

## 关键设计决策

### 1. Cumulative Text vs Incremental

采用累积文本快照而非增量文本：

- **优点**：客户端无需维护拼接状态，每次收到的 `text` 就是完整内容
- **优点**：丢包后自动恢复，下次收到的就是最新状态
- **缺点**：每次传输的数据量随文本增长而增大
- **权衡**：在典型 Agent 回复场景中，文本长度有限，累积快照更简单可靠

### 2. Rate Limiting (50ms)

比 `set_typing`（1s）更宽松的 rate limit：

- **原因**：流式文本需要更频繁的更新以提供流畅的用户体验
- **速率**：50ms 1 次（20/s）
- **超限行为**：静默返回 OK（不是错误）
- **实现**：内存中的 per-user-per-conversation limiter（`sync.Map` + `streamingRateLimiter`），不依赖 Redis

### 3. Ephemeral（Seq=0）

`stream_text` 使用 `Seq=0` 表示这是瞬时消息：

- 不持久化到 `user_updates` 表
- 不通过 `sync_updates` 交付
- 对离线用户静默丢弃
- 最终结果通过 `send_message` 持久化

### 4. Broadcast to All Members

广播给所有成员（包括发送者自己），原因：

- 发送者可能有多个设备，需要同步状态
- 简化实现，避免过滤逻辑
- 广播路径：本地 WebSocket 直推 + 跨节点 Redis Pub/Sub（多节点时）

### 5. Stream ID

使用 `stream_id` 标识一次流式会话：

- 客户端可以同时接收多个 stream（例如多个 Agent 同时回复）
- `is_done=true` 标识流结束
- 客户端应为每次流式会话生成新的 UUID

### 6. Cross-Node Broadcasting

`broadcastFn` 实际为 `WebSocketServer.BroadcastUpdates`，执行两阶段广播：

1. **本地广播**（`broadcastLocal`）：推送到该用户在本节点的所有在线 WebSocket 连接
2. **跨节点广播**（`nodeBroadcaster.Publish`）：发布到 Redis Pub/Sub 频道，payload 包含 `sourceNodeID` 以避免发送节点重复接收

- 单节点部署使用 `NoopBroadcaster`（no-op），不产生 Redis 开销
- 跨节点失败不影响本地广播结果，仅记录日志

---

## 与 Agent 执行的关系

`stream_text` 主要由 Agent 执行器使用：

```mermaid
sequenceDiagram
    participant MQ as Message Queue
    participant Agent as Agent Executor
    participant Handler as StreamTextHandler
    participant C as 客户端

    MQ->>Agent: TypeAgentProcess 任务
    Agent->>Agent: 执行 LLM 流式请求

    loop 每个流式 chunk
        Agent->>Handler: stream_text (累积文本)
        Handler->>C: 广播给所有成员
    end

    Agent->>Handler: stream_text (is_done=true)
    Handler->>C: 广播完成信号

    Agent->>Agent: 持久化最终消息
    Agent->>C: 通过 MQ 广播持久化消息
```

---

## 相关文档

- [Set Typing 业务流程](set-typing.md)
- [Agent 执行流程](agent.md)
- [消息处理业务流程](message.md)
- [WebSocket 连接管理](websocket-connection.md)
- [广播机制](broadcasting.md)
