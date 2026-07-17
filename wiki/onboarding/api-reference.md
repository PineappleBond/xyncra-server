---
last_updated: 2026-07-17
---

# WebSocket API 参考

> Xyncra 使用基于 JSON 的 3 层信封协议（Frame → Package → Message），
> 通过单个 WebSocket 连接实现双向 RPC。

---

## 目录

- [连接 URL](#连接-url)
- [认证方式](#认证方式)
- [协议格式（3 层信封）](#协议格式3-层信封)
- [RPC 方法参考](#rpc-方法参考)
- [错误码](#错误码)
- [数据模型](#数据模型)
- [速率限制](#速率限制)

---

## 连接 URL

```
ws://host:port/ws?user_id={user_id}&device_id={device_id}
```

| 参数 | 必填 | 说明 |
|------|------|------|
| `user_id` | 是 | 用户唯一标识符，服务端通过此参数识别用户身份 |
| `device_id` | 是 | 设备唯一标识符，用于定向 RPC 和设备管理 |

### 连接生命周期

```
客户端                          服务端
  │                               │
  │──── WebSocket 连接建立 ──────▶│
  │                               │── 注册到 ConnectionStore
  │                               │── 替换已有 (user_id, device_id) 连接（发送 4001 close）
  │◀──── connection_id ──────────│── 返回 welcome 消息
  │                               │
  │──── system.reconnect ───────▶│  （重连握手）
  │◀──── replay pending reqs ────│── 异步补发未完成的 reverse-RPC
  │                               │
  │──── system.register_functions▶│  （注册设备函数）
  │                               │
  │──── 正常通信 ────────────────│
  │                               │
  │──── 心跳 (heartbeat) ───────▶│  （每 30 秒）
  │◀──── pong ──────────────────│
  │                               │
  │──── 断开连接 ────────────────│── 清理 ConnectionStore 条目
  │                               │── 清理 FunctionRegistry
```

### 默认端口

| 环境 | WebSocket 端口 | 健康检查端点 |
|------|---------------|-------------|
| 开发 | `:8080` | `http://localhost:8080/health` |
| E2E 测试 | `:18080` | `http://localhost:18080/health` |

---

## 认证方式

### 开发环境

默认使用 URL 查询参数认证（D-002、D-005）：

```
ws://server:8080/ws?user_id=alice&device_id=macbook
```

### 生产环境

生产环境中，认证应由反向代理层完成。推荐架构：

```
       Internet
          │
   ┌──────▼──────┐
   │   Nginx /   │  ← TLS termination + 用户认证
   │   Envoy     │     注入已认证的 user_id
   └──────┬──────┘
          │ 内部网络
   ┌──────▼──────┐
   │  Xyncra     │  ← 信任代理注入的 user_id
   │  Server     │     不再使用查询参数认证
   └─────────────┘
```

可通过 `WSWithAuthenticate` 函数式选项自定义认证逻辑。

### 设备替换（4001 Close Frame）

当同一 `(user_id, device_id)` 建立新连接时，服务端会向旧连接发送 HTTP 4001 Close Frame。
旧客户端应优雅退出而非自动重连（D-095、D-111）。

---

## 协议格式（3 层信封）

所有通信使用 3 层 JSON 信封：**Package → Data → Params**。

### 第 1 层：Package（消息信封）

所有 WebSocket 消息的顶层结构：

```json
{
  "version": 1,
  "type": 0,
  "data": { ... }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `version` | uint8 | 协议版本，零值时默认为 1 |
| `type` | uint8 | 消息类型：0=Request, 1=Response, 2=Updates |
| `data` | json.RawMessage | 消息体，根据 type 解析为不同结构 |

### 请求（PackageTypeRequest, type=0）

客户端发起的 RPC 请求：

```json
{
  "version": 1,
  "type": 0,
  "data": {
    "id": "req-001",
    "method": "send_message",
    "params": { "conversation_id": "...", "content": "..." }
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 请求唯一 ID，用于关联响应 |
| `method` | string | RPC 方法名 |
| `params` | json.RawMessage | JSON 编码的方法参数 |

### 响应（PackageTypeResponse, type=1）

服务端对请求的响应：

```json
{
  "version": 1,
  "type": 1,
  "data": {
    "id": "req-001",
    "code": 0,
    "msg": "",
    "data": { ... }
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 对应请求的 ID |
| `code` | int32 | 状态码：0=成功，负数=错误（见错误码） |
| `msg` | string | 错误消息（成功时为空） |
| `data` | json.RawMessage | 响应数据（结构取决于方法） |

### 推送更新（PackageTypeUpdates, type=2）

服务端主动推送的增量更新：

```json
{
  "version": 1,
  "type": 2,
  "data": {
    "updates": [
      {
        "seq": 1,
        "type": "message",
        "payload": { ... },
        "created_at": "2026-07-08T12:00:00Z"
      }
    ]
  }
}
```

更新类型：

| Seq 范围 | 持久化 | 推送方式 |
|----------|--------|----------|
| `> 0` | 是 | 实时推送 + `sync_updates` 可拉取 |
| `= 0` | 否 | ephemeral 仅实时推送，离线不投递 |

---

## RPC 方法参考

### heartbeat

保持连接活跃。被动 TTL 续期（D-010），建议每 30-60 秒发送一次。

**参数**：所有字段可选

| 字段 | 类型 | 说明 |
|------|------|------|
| `device_info` | map[string]string | 设备元信息，仅用于日志 |

**响应**：

```json
{ "status": "ok" }
```

**错误**：`-101`（连接已过期）、`-300`（内部错误）

### send_message

发送消息到指定会话（D-006 幂等、D-007 fire-and-forget）。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `conversation_id` | string | 是 | 目标会话 ID |
| `client_message_id` | string | 是 | 客户端消息唯一 ID（建议 UUID v4），用于幂等 |
| `content` | string | 是 | 消息内容 |
| `type` | string | 否 | 消息类型，默认 "text" |
| `reply_to` | uint32 | 否 | 回复的消息序号，0=不回复 |

**响应**：

```json
{
  "message": {
    "ID": "msg-uuid",
    "ClientMessageID": "550e8400-...",
    "ConversationID": "conv-uuid",
    "MessageID": 1,
    "SenderID": "alice",
    "Content": "Hello!",
    "Type": "text",
    "ReplyTo": 0,
    "Status": "sent",
    "CreatedAt": "2026-07-08T12:00:00Z",
    "DeletedAt": null
  },
  "duplicate": false
}
```

幂等命中时 `duplicate=true`，返回已持久化的消息。

**错误**：`-100`、`-101`、`-200`、`-300`

### sync_updates

增量同步用户更新（D-009）。

**参数**：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `after_seq` | uint32 | 0 | 排他性下界，返回 seq > after_seq 的更新 |
| `limit` | int | 100 | 每页数量，范围 [1, 500] |

**响应**：

```json
{
  "updates": [ ... ],
  "has_more": false,
  "latest_seq": 150
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `updates` | array | 增量更新数组 |
| `has_more` | bool | 是否还有更多更新 |
| `latest_seq` | uint32 | 当前用户全局最新 seq |

**错误**：`-100`、`-300`

### create_conversation

创建 1-on-1 会话（D-011 find-or-create 幂等）。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user_id` | string | 是 | 对方的用户 ID |
| `title` | string | 否 | 会话标题 |

**响应**：

```json
{
  "conversation": { ... },
  "duplicate": false
}
```

幂等命中时 `duplicate=true`，返回已有会话。

**错误**：`-100`、`-300`。不能和自己创建会话。

### list_conversations

列出当前用户的会话列表，按 `LastMessageAt` 降序。

**参数**：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `offset` | int | 0 | 分页偏移 |
| `limit` | int | 20 | 每页数量，范围 [1, 100] |

**响应**：

```json
{
  "conversations": [ ... ],
  "has_more": false
}
```

**错误**：`-100`、`-300`

### get_messages

获取指定会话的消息历史（D-008，按 MessageID 升序）。

**参数**：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `conversation_id` | string | - | 会话 ID |
| `after_message_id` | uint32 | 0 | 排他性下界 |
| `limit` | int | 50 | 每页数量，范围 [1, 200] |

**响应**：

```json
{
  "messages": [ ... ],
  "has_more": false
}
```

**错误**：`-100`、`-101`、`-200`、`-300`

### search_messages

在会话中搜索消息（LIKE 匹配，按 MessageID 降序）。

**参数**：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `conversation_id` | string | - | 会话 ID |
| `query` | string | - | 搜索关键词 |
| `after_message_id` | uint32 | 0 | 分页游标 |
| `limit` | int | 50 | 每页数量，范围 [1, 200] |

**响应**：

```json
{
  "messages": [ ... ],
  "has_more": false
}
```

**错误**：`-100`、`-101`、`-200`、`-300`

### get_conversation

获取单个会话详情，包含未读消息数和 HITL 问题。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `conversation_id` | string | 是 | 会话 ID |

**响应**：

```json
{
  "conversation": { ... },
  "unread_count": 2,
  "questions": [ ... ]
}
```

**错误**：`-100`、`-101`、`-200`、`-300`

### delete_conversation

软删除会话（级联软删除所有消息，D-013）。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `conversation_id` | string | 是 | 会话 ID |

**响应**：

```json
{
  "status": "ok",
  "deleted_message_count": 42
}
```

**错误**：`-100`、`-101`、`-200`、`-300`

### restore_conversation

恢复软删除的会话（级联恢复消息，D-015）。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `conversation_id` | string | 是 | 会话 ID |

**响应**：

```json
{
  "conversation": { ... },
  "restored_message_count": 42
}
```

对未删除的会话调用是幂等的。

**错误**：`-100`、`-101`、`-200`、`-300`

### delete_message

删除消息（仅发送者可删，D-014）。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `message_id` | string | 是 | 消息的 UUID 主键 |

**响应**：

```json
{ "status": "ok" }
```

**错误**：`-100`、`-101`、`-200`、`-300`

### mark_as_read

标记会话已读（MAX 语义，只进不退，D-012）。

**参数**：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `conversation_id` | string | - | 会话 ID |
| `message_id` | uint32 | LastProcessedMessageID | 已读到哪条消息，0=全部已读 |

**响应**：

```json
{
  "status": "ok",
  "unread_count": 0,
  "last_read_message_id": 42
}
```

**错误**：`-100`、`-101`、`-200`、`-300`

### set_typing

发送 typing 指示器（ephemeral push，Seq=0，D-050）。

**参数**：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `conversation_id` | string | - | 会话 ID |
| `is_typing` | bool | false | true=开始输入，false=停止 |

**响应**：

```json
{ "status": "ok" }
```

**限流**：每用户每会话 1 次/秒/节点，超限静默返回 OK。

**错误**：`-100`、`-101`、`-200`

### stream_text

发送流式文本（ephemeral push，Seq=0，累积模式，D-051）。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `conversation_id` | string | 是 | 会话 ID |
| `stream_id` | string | 是 | 流 ID（客户端生成 UUID） |
| `text` | string | 是 | 累积文本快照（非 delta） |
| `is_done` | bool | 否 | 流式结束信号 |

**响应**：

```json
{ "status": "ok" }
```

**限流**：每用户每会话 20 次/秒/节点，超限静默返回 OK。

**两步协议**（D-052）：
1. `stream_text(is_done=true, text=最终文本)` — 广播结束信号
2. `send_message(content=最终文本)` — 持久化消息

**错误**：`-100`、`-101`、`-200`

### reload_agents

重新加载 Agent 配置（D-076）。

**参数**：无

**响应**：

```json
{ "count": 5 }
```

**错误**：`-300`

### agent_resume

恢复 HITL 暂停的 Agent（D-085、D-116）。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `conversation_id` | string | 是 | 会话 ID |
| `answer` | string | 是 | 用户回答 |
| `agent_id` | string | 是 | Agent 标识符 |
| `checkpoint_id` | string | 否 | HITL checkpoint ID |
| `interrupt_id` | string | 否 | 指定回答哪个 interrupt |

**响应**：

```json
{ "status": "partial", "answered": 1, "total": 2, "pending": 1 }
{ "status": "queued", "answered": 2, "total": 2 }
```

**错误**：`-100`、`-101`、`-300`、`-409`（问题已被回答）

### system.register_functions

注册设备函数清单（D-098、D-099）。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `device_id` | string | 否 | 被连接认证的 device_id 覆盖 |
| `device_info` | map[string]string | 否 | 设备元信息 |
| `functions` | array | 是 | 函数清单 |

**响应**：

```json
{ "status": "ok", "count": 1, "device_id": "desktop-abc123" }
```

**错误**：`-100`、`-300`

### system.reconnect

重连握手与请求补发（D-108）。

**参数**：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `last_seen_seq` | uint64 | 0 | 客户端最后收到的 seq |

**响应**：

```json
{ "status": "ok", "replayed": 3, "total": 5 }
```

**错误**：`-100`（fail-open 策略，查询失败不报错，D-072）

---

## 错误码

结构化错误码分段分配（D-017）：

| Code | 名称 | 说明 |
|------|------|------|
| 0 | OK | 成功 |
| -1 | Error | 通用错误 |
| **-100** | **ValidationError** | 参数验证失败 |
| **-101** | **NotFound** | 资源不存在 |
| -102 | Duplicate | 资源重复（幂等命中返回成功） |
| **-200** | **PermissionDenied** | 权限不足 |
| -201 | Forbidden | 禁止访问 |
| **-300** | **InternalError** | 服务器内部错误 |
| -301 | Unavailable | 服务不可用 |
| **-409** | **Conflict** | 资源状态冲突（如 HITL 问题已答） |

### 客户端扩展错误码

| Code | 名称 | 说明 |
|------|------|------|
| -400 | ConnectionError | WebSocket 连接失败 |
| -401 | TimeoutError | 操作超时 |
| -402 | SyncError | 数据同步失败 |

### 各方法可能的错误码

| 方法 | 错误码范围 |
|------|-----------|
| heartbeat | -101, -300 |
| send_message | -100, -101, -200, -300 |
| sync_updates | -100, -300 |
| create_conversation | -100, -300 |
| list_conversations | -100, -300 |
| get_messages / search_messages | -100, -101, -200, -300 |
| get_conversation | -100, -101, -200, -300 |
| delete_conversation | -100, -101, -200, -300 |
| restore_conversation | -100, -101, -200, -300 |
| delete_message | -100, -101, -200, -300 |
| mark_as_read | -100, -101, -200, -300 |
| set_typing / stream_text | -100, -101, -200 |
| reload_agents | -300 |
| agent_resume | -100, -101, -300, -409 |
| system.register_functions | -100, -300 |
| system.reconnect | -100 |

---

## 数据模型

### Conversation

| 字段 | 类型 | 说明 |
|------|------|------|
| ID | string | UUID 主键 |
| UserID1 | string | 用户 1（字典序较小） |
| UserID2 | string | 用户 2（字典序较大） |
| Type | string | 1-on-1 / group / channel |
| Title | string | 会话标题 |
| AvatarURL | string | 会话头像 URL |
| Description | string | 会话描述 |
| Pinned | bool | 是否置顶 |
| Muted | bool | 是否静音 |
| LastProcessedMessageID | uint32 | 最后处理的消息序号 |
| LastReadMessageID1 | uint32 | UserID1 的已读游标 |
| LastReadMessageID2 | uint32 | UserID2 的已读游标 |
| AgentStatus | string | Agent 状态：idle/thinking/tool_calling/generating/asking_user/timeout |
| AgentID | string | 当前 Agent ID |
| CheckpointID | string | 当前 HITL checkpoint ID |
| AgentLastActivity | timestamp | Agent 最后活动时间 |
| DeletedAt | timestamp | 软删除时间 |

### Message

| 字段 | 类型 | 说明 |
|------|------|------|
| ID | string | UUID 主键 |
| ClientMessageID | string | 客户端幂等键（unique index） |
| ConversationID | string | 所属会话 |
| MessageID | uint32 | 会话内单调递增序号 |
| SenderID | string | 发送者 |
| Content | string | 消息内容 |
| Type | string | 消息类型 |
| ReplyTo | uint32 | 回复的消息序号 |
| Status | string | 消息状态 |
| CreatedAt | timestamp | 创建时间 |
| DeletedAt | timestamp | 软删除时间 |

### Question（HITL）

| 字段 | 类型 | 说明 |
|------|------|------|
| ID | string | UUID 主键 |
| ConversationID | string | 所属会话 |
| CheckpointID | string | Eino checkpoint ID |
| InterruptID | string | Eino interrupt address ID |
| QuestionText | string | 问题内容 |
| Status | string | pending / answered |
| Answer | string | 用户回答 |
| AnsweredBy | string | 回答者 user_id |
| CreatedAt | timestamp | 创建时间 |

### FunctionInfo

| 字段 | 类型 | 说明 |
|------|------|------|
| name | string | 函数名（最长 255 字符） |
| description | string | 函数描述 |
| parameters | JSON Schema | 输入参数描述 |
| returns | ReturnInfo | 返回值描述 |
| tags | string[] | 标签列表 |
| timeout_ms | int | 执行超时（毫秒） |

---

## 速率限制

| 推送类型 | 限制 | 超限行为 |
|---------|------|---------|
| `set_typing` | 1 次/秒/用户/会话/节点 | 静默返回 OK |
| `stream_text` | 20 次/秒/用户/会话/节点 | 静默返回 OK |

速率限制基于服务端节点本地计数，多节点部署时为 best-effort 限流。
