---
last_updated: 2026-07-17
---

# 数据流

## 消息发送流程

消息发送是 Xyncra 系统的核心数据流。系统采用 **"先落库后处理"**（persist before process）原则：消息先在数据库持久化，再通过 MQ 异步推送。

```
发送者                   服务器                    数据库                   MQ(Redis)             接收者
  │                        │                        │                      │                     │
  │── send_message ──────→ │                        │                      │                     │
  │  (WebSocket Request)   │                        │                      │                     │
  │                        │── Transaction ───────→ │                      │                     │
  │                        │   BEGIN                │                      │                     │
  │                        │                        │                      │                     │
  │                        │   Read Conv            │                      │                     │
  │                        │   Allocate MessageID   │                      │                     │
  │                        │   Allocate per-user    │                      │                     │
  │                        │   seq (MAX+1)          │                      │                     │
  │                        │   INSERT message       │                      │                     │
  │                        │   INSERT user_updates  │                      │                     │
  │                        │   UPDATE conversation  │                      │                     │
  │                        │── COMMIT ────────────→ │                      │                     │
  │                        │                        │                      │                     │
  │◄─ Response {message} ──│                        │                      │                     │
  │    code=0, duplicate=false                      │                      │                     │
  │                        │                        │                      │                     │
  │                        │── Enqueue ────────────→│                      │                     │
  │                        │  mq:send_message       │   (fire-and-forget)  │                     │
  │                        │  (if agent peer)       │                      │                     │
  │                        │  mq:agent_process      │                      │                     │
  │                        │                        │                      │                     │
  │                        │                        │                      │── Dequeue ─────────→│
  │                        │                        │                      │   BroadcastUpdates   │
  │                        │                        │                      │   (PackageTypeUpdates)│
  │                        │                        │                      │                     │
  │                        │                        │                      │                     │
  │                        │   (Push notification)  │                      │                     │
  │                        │   BroadcastUpdates     │                      │                     │
  │                        │   to online devices    │                      │                     │
  │                        │                        │                      │                     │
```

### 步骤详解

#### 步骤 1：参数验证与幂等性检查

`internal/handler/send_message.go`：

```go
params := {conversation_id, client_message_id, content, type?, reply_to?}
conv := store.ConversationStore().Get(conversationID)
// 验证发送者是会话成员
members := conversationMembers(conv) // [UserID1, UserID2]
verify sender ∈ members
```

`client_message_id` 是客户端生成的 UUID，用于幂等性。服务端通过 `messages(client_message_id, sender_id)` 唯一索引实现 TOCTOU-safe 的幂等性（D-006）。

#### 步骤 2：原子持久化（SendMessage 事务）

`internal/store/store.go:SendMessage`：

```
Transaction:
  1. SELECT conversation  // 读 LastProcessedMessageID（FOR UPDATE 隐式行锁）
  2. msg.MessageID = conv.LastProcessedMessageID + 1  // D-008
  3. FOR EACH member:
       SELECT MAX(seq) FROM user_updates WHERE user_id = member
       seq = MAX + 1
       INSERT user_update (user_id, seq=MAX+1, type="message", payload=msg)
  4. INSERT messages (status="sent")
  5. UPDATE conversations SET
       last_message_at = msg.CreatedAt,
       last_processed_message_id = msg.MessageID
```

**关键设计**：
- `MessageID` 在事务内分配（D-008），避免并发分配相同 ID
- `user_updates` 使用 `seq` 作为单调递增序号，每个用户独立序列
- `client_message_id` + `sender_id` 唯一索引保证幂等

#### 步骤 3：MQ 异步处理（Fire-and-Forget）

事务提交后，发送者立即得到响应。MQ 任务异步执行：

```
// 任务 1: 实时消息推送
broker.Enqueue(mq:send_message, {recipients: [{user_id, updates}]})

// 可选任务 2: Agent 处理（如果接收方是 Agent）
if peer is agent/xxx:
    broker.Enqueue(mq:agent_process, {message_id, conversation_id, agent_id, sender_id, device_id})
```

即使 MQ 不可用，消息已在数据库持久化。接收方在下次 `sync_updates` 拉取时仍能获取（D-007）。

#### 步骤 4：消息推送

`internal/handler/mq_send_message.go` - MQ 任务消费：

```
for each recipient:
    BroadcastUpdates(userID, updates: [{seq, type:"message", payload}])
```

`BroadcastUpdates` 流程（`internal/server/websocket_server.go` 中 `WebSocketServer.BroadcastUpdates`）：
1. **本地广播**：遍历 `clientsByUser[userID]` 推送 PackageTypeUpdates
2. **跨节点发布**：Redis Pub/Sub 发布（多节点时）

#### 步骤 5：Agent 处理流程（可选）

当消息接收方是 Agent 时：

```
MQ Consumer ── mq:agent_process ──→ AgentTaskHandler
    │
    ├─ 1. Agent 幂等性检查（Redis SETNX，D-071）
    ├─ 2. 获取会话级并发锁（D-075）
    ├─ 3. 加载对话上下文（ContextManager）
    ├─ 4. 构建 Eino Graph（LLM + Tools）
    ├─ 5. 执行 LLM 流式生成
    │      ├─ 广播 agent_status（thinking/tool_calling/generating）
    │      ├─ 广播 stream_text（流式输出）
    │      └─ BroadcastUpdates → client
    ├─ 6. Agent 生成完整消息
    ├─ 7. 持久化 Agent 消息到 DB（SendMessage）
    ├─ 8. 广播 Agent 回复
    └─ 9. 释放并发锁 / 清理
```

### 状态同步机制

Xyncra 采用 **Push + Pull 结合** 的同步模式。

#### Push：增量更新推送

当有新消息、读指针变更等事件发生时，服务端通过 `PackageTypeUpdates` 推送给在线的客户端设备。

```
Server.sendToUser(userID, updates) → WebSocket PackageTypeUpdates
```

推送是**尽力而为**的：推送失败不重试，数据通过 Pull 通道最终一致。

#### Pull：sync_updates RPC

客户端通过 `sync_updates` RPC 主动拉取：

```
Client ←→ Server: sync_updates(after_seq, limit)
Server → Client: {updates: [...], has_more, latest_seq}
```

**拉取触发场景**：
1. **初次连接 / 重连后**：`FullSync` 全量拉取（分页直至 has_more=false）
2. **断线重连后**：`FullSync` 补全离线期间的数据
3. **序列 gap 检测**：`ApplyUpdate` 检测到 seq 跳跃 → 触发 debounce pull
4. **定时心跳拉取**：在心跳间隔中携带同步请求

#### Gap 填充策略（D-029）

当 `sync_updates` 查询的 seq 范围内存在缺失记录时，服务端生成 **运行时 gap 占位 Update**：

```
actualUpdates = SELECT * FROM user_updates WHERE seq IN (after_seq+1, ..., expectedEnd)
for seq = after_seq+1 to expectedEnd:
    if seq in actualUpdates:
        result.append(actualUpdates[seq])
    else:
        result.append({seq, type:"gap", payload:nil})
```

Gap Update 类型为 `UpdateTypeGap`，仅在运行时存在，不持久化到数据库。客户端收到 gap 后可以做日志记录或触发补充拉取。

#### 客户端同步流程

```
syncManager.ApplyUpdate(update):
    if update.Seq == 0:
        // 瞬时更新：直接通知 handler，不持久化
        return handler.OnTyping/OnStreaming/OnAgentStatus(...)

    localMaxSeq = db.SyncStates.GetLocalMaxSeq()

    if update.Seq <= localMaxSeq:
        return  // 已处理，跳过
    if update.Seq > localMaxSeq + 1:
        scheduleDebouncedPull()  // 序列 gap
        return errSeqGap

    Transaction:
        1. NotificationLog.Save(seq, type, payload)  // 去重日志
        2. dispatchUpdateTx(type, payload)             // 按类型持久化
        3. SyncStates.SetLocalMaxSeq(seq)              // 推进序列

    After commit:
        handler.OnMessage(msg) / OnDeleteMessage(...) / etc.
```

### 补发（Retransmission）机制

补发机制确保消息在弱网或设备替换场景下不丢失。

#### 客户端 RPC 超时重试

`pkg/client/client.go:Call`：

```
Call(method, params):
    reqID = uuid.New()
    pending[reqID] = make(chan response, 1)

    connMgr.SendPackage(request)

    select {
    case resp := <-pending[reqID]:
        return resp
    case <-ctx.Done():
        retryMgr.Enqueue(method, params)  // 异步重试
        return timeout error
    case <-time.After(adaptiveTimeout):
        retryMgr.Enqueue(method, params)  // 异步重试
        return timeout error
    }
```

`retryManager` 将失败的 RPC 持久化到本地 `queue_store` 表，定期轮询重试（指数退避），重试成功或达到最大次数后清除。

#### 反向 RPC 超时持久化（Phase 4, D-103）

```
ServerRequest → timeout → persistAsync:
    PendingStore.Save({reqID, userID, deviceID, method, params, idempotencyKey, seq})
    → Redis key: pending:{userID}␀{deviceID} (list)
```

客户端重连后通过 `system.reconnect` 触发：

```
system.reconnect(last_seen_seq):
    preqs = PendingStore.GetPending(userID, deviceID, last_seen_seq)
    for each preq:
        ReplayRequest(preq)
```

`ReplayRequest` 使用新的 `reqID`（`s-replay-{uuid}`）但保留原始 `IdempotencyKey`，客户端通过幂等性缓存识别重复请求（D-107）。

#### 响应重试队列

当设备替换或网络闪断导致服务端响应未送达客户端时，`ResponseRetryQueue` 在连接恢复后重新发送：

```
responseRetryLoop:
    ticker = 1s
    entries = respRetryQueue.Drain(now)
    for each entry:
        connMgr.SendPackage(entry.Response)
        if fail:
            entries.attempts++
            if attempts < maxRetry:
                respRetryQueue.EnqueueWithBackoff(entry)
```

### 瞬时推送（Ephemeral Push, D-050）

瞬时推送用于不需要持久化、不需要拉取的实时业务场景。典型例子：打字指示、流式文本、Agent 状态。

**设计特点**：
- `Seq = 0`：表示瞬时消息
- **不持久化**：不写入 database，不写入 user_updates
- **不拉取**：sync_updates 忽略 Seq=0 的 update
- **仅推送**：依赖 WebSocket 实时通道，丢失即丢失
- **客户端处理**：客户端直接调用 handler，不经过同步管道

```
// 服务端（Handler）
set_typing_handler:
    update = {seq: 0, type: "typing", payload: {user_id, conversation_id, is_typing, timestamp}}
    BroadcastUpdates(userID, [update])

// 客户端（SyncManager.ApplyUpdate）
if update.Seq == 0:
    if update.Type == "typing":
        handler.OnTyping(ctx, userID, conversationID, isTyping)
    if update.Type == "streaming":
        handler.OnStreaming(ctx, userID, conversationID, streamID, text, isDone)
    if update.Type == "agent_status":
        handler.OnAgentStatus(ctx, userID, conversationID, status)
    if update.Type == "agent_timeout":
        handler.OnAgentTimeout(ctx, userID, conversationID, reason)
    return  // 跳过持久化
```

### Pull-on-Notification 模式（D-118/D-124）

对于会话变更等场景，推送只触发通知，客户端按需拉取最新数据：

```
Server ── Update {seq:0, type:"conversation", action:"update", updated_at:1721116800} ──→ Client
Client ── get_conversation RPC ──→ Server
Server ── {conversation, unread_count, questions} ──→ Client
```

**D-124 优化**：推送携带 `updated_at` 时间戳，客户端比较本地缓存时间戳，本地已最新则跳过拉取。

```
if payload.updated_at > 0:
    localConv = db.Conversations.Get(convID)
    if localConv != nil && payload.updated_at <= localConv.UpdatedAt.Unix():
        return  // 本地已最新，跳过 RPC
```

### 消息按需拉取（D-126）

客户端本地数据不足时（如旧消息从未同步），通过 `FetchMoreMessages` 从服务端拉取：

```
Client ── get_messages(conversation_id, after_message_id, limit) ──→ Server
Server ── {messages, has_more} ──→ Client
Client ── Upsert each message to local DB
Client ── Return messages to caller
```

`ListConversations`、`GetMessages`、`SearchMessages` 优先从本地 DB 读取（D-035），仅当本地数据不足时才触发拉取。

### 数据流总结

| 场景 | 推送 | 持久化 | 拉取 | Seq |
|------|------|--------|------|-----|
| 发送消息 | BroadcastUpdates | DB + UserUpdates | sync_updates | > 0 |
| 消息删除 | BroadcastUpdates | Soft Delete | sync_updates | > 0 |
| 读指针同步 | BroadcastUpdates | DB Update | sync_updates | > 0 |
| 会话变更 | BroadcastUpdates | DB Update | Pull-on-Notification | > 0 |
| 打字指示 | BroadcastUpdates | 否 | 否 | 0 |
| 流式文本 | BroadcastUpdates | 否 | 否 | 0 |
| Agent 状态 | BroadcastUpdates | 否 | 否 | 0 |
| 在线状态 | BroadcastUpdates | 可配置 | 否 | 0 |

<div style="page-break-after: always;"></div>
