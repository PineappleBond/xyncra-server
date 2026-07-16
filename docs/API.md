# Xyncra WebSocket API 文档

> Last updated: 2026-07-16

---

## 目录

- [连接](#连接)
- [协议格式](#协议格式)
- [RPC 方法](#rpc-方法)
  - [heartbeat](#heartbeat)
  - [send_message](#send_message)
  - [sync_updates](#sync_updates)
  - [create_conversation](#create_conversation)
  - [list_conversations](#list_conversations)
  - [get_messages](#get_messages)
  - [search_messages](#search_messages)
  - [get_conversation](#get_conversation)
  - [delete_conversation](#delete_conversation)
  - [restore_conversation](#restore_conversation)
  - [delete_message](#delete_message)
  - [mark_as_read](#mark_as_read)
  - [set_typing](#set_typing)
  - [stream_text](#stream_text)
  - [reload_agents](#reload_agents)
  - [agent_resume](#agent_resume)
  - [system.register_functions](#systemregister_functions)
  - [system.reconnect](#systemreconnect)
- [错误码](#错误码)
- [数据模型](#数据模型)
  - [Conversation](#conversation)
  - [Message](#message)
  - [UserUpdate](#userupdate)
  - [Question](#question)
  - [FunctionInfo](#functioninfo)

---

## 连接

```
ws://host:port/ws?user_id={user_id}&device_id={device_id}
```

通过 URL 查询参数 `user_id` 标识客户端身份 (D-002, D-005)，`device_id` 标识具体设备 (D-093)。

- `user_id`（必填）：用户唯一标识
- `device_id`（必填）：设备唯一标识，用于定向 RPC 调用和函数注册。同 `(user_id, device_id)` 新连接会替换旧连接

**注意**：默认的 `user_id` 查询参数认证仅适用于开发环境。生产环境中应由业务服务器在反向代理层注入已认证的 `user_id` (D-002)。

---

## 协议格式

所有 WebSocket 消息均使用以下信封格式。

### Package (消息信封)

所有消息的顶层结构。

| 字段    | 类型            | 说明                                           |
|---------|-----------------|------------------------------------------------|
| version | uint8           | 协议版本，零值时默认为 1                       |
| type    | uint8           | 消息类型：0=Request, 1=Response, 2=Updates     |
| data    | json.RawMessage | 消息体，根据 type 解析为不同的结构             |

```json
{
  "version": 1,
  "type": 0,
  "data": { ... }
}
```

### Request (PackageDataRequest)

客户端发起的请求。`type = 0`。

| 字段   | 类型            | 说明                       |
|--------|-----------------|----------------------------|
| id     | string          | 请求唯一 ID，用于关联响应  |
| method | string          | RPC 方法名                 |
| params | json.RawMessage | JSON 编码的方法参数        |

```json
{
  "version": 1,
  "type": 0,
  "data": {
    "id": "req-001",
    "method": "send_message",
    "params": { ... }
  }
}
```

### Response (PackageDataResponse)

服务端对请求的响应。`type = 1`。

| 字段 | 类型            | 说明                                       |
|------|-----------------|--------------------------------------------|
| id   | string          | 对应请求的 ID                              |
| code | int32           | 状态码：0=成功，负数=错误（详见[错误码](#错误码)） |
| msg  | string          | 错误消息（成功时为空字符串）               |
| data | json.RawMessage | 响应数据（JSON 编码，结构取决于方法）      |

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

### Updates (PackageDataUpdates)

服务端推送的增量更新批次。`type = 2`。

| 字段    | 类型                  | 说明               |
|---------|-----------------------|--------------------|
| updates | PackageDataUpdate[]   | 增量更新数组       |

```json
{
  "version": 1,
  "type": 2,
  "data": {
    "updates": [ ... ]
  }
}
```

### PackageDataUpdate

单条增量更新记录。

| 字段       | 类型            | 说明                                               |
|------------|-----------------|----------------------------------------------------|
| seq        | uint32          | 单调递增序号                                       |
| type       | string          | 更新类型（message / delete_message / mark_read / conversation） |
| payload    | json.RawMessage | 更新内容（JSON 编码）                              |
| created_at | time.Time       | 更新生成时间                                       |

---

## RPC 方法

### heartbeat

被动 TTL 续期 (D-010)。客户端定期发送 heartbeat 以保持连接活跃，服务端通过 `ConnectionStore.Refresh()` 重置连接 TTL。

**参数**：

所有字段均为可选。一个不含任何参数的 heartbeat 请求也是合法的。

| 字段         | 类型              | 必填 | 默认值 | 说明                                                 |
|--------------|-------------------|------|--------|------------------------------------------------------|
| device_info  | map[string]string | 否   | -      | 设备元信息（如 OS 版本、App 版本等），仅用于日志观测 |

**请求示例**：

```json
{
  "id": "hb-001",
  "method": "heartbeat",
  "params": {
    "device_info": {
      "os": "iOS 17.0",
      "app_version": "1.2.3"
    }
  }
}
```

**响应**：

```json
{
  "status": "ok"
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -101 | connection expired | 连接已过期或被驱逐，需要重新连接 |
| -300 | refresh connection: ... | 刷新连接 TTL 失败 |

**客户端建议**：定期发送 heartbeat，建议间隔 30-60 秒。如果停止发送，连接会在 TTL（默认 30 分钟）后自动过期。

---

### send_message

发送消息到指定会话 (D-006 幂等, D-007 fire-and-forget, D-008 MessageID 分配)。

客户端提供 `client_message_id` 实现幂等性——重复的 `client_message_id` 会返回已持久化的消息（`duplicate=true`），而非报错 (D-006)。消息持久化后，MQ 入队用于实时推送是异步 fire-and-forget 操作，入队失败不影响返回结果 (D-007)。

**参数**：

| 字段              | 类型   | 必填 | 默认值 | 说明                                           |
|-------------------|--------|------|--------|------------------------------------------------|
| conversation_id   | string | 是   | -      | 目标会话 ID                                    |
| client_message_id | string | 是   | -      | 客户端消息唯一 ID（建议使用 UUID v4）          |
| content           | string | 是   | -      | 消息内容                                       |
| type              | string | 否   | "text" | 消息类型                                       |
| reply_to          | uint32 | 否   | 0      | 回复的消息序号（MessageID），0 表示不回复      |

**请求示例**：

```json
{
  "id": "sm-001",
  "method": "send_message",
  "params": {
    "conversation_id": "conv-uuid-001",
    "client_message_id": "550e8400-e29b-41d4-a716-446655440000",
    "content": "Hello, world!",
    "type": "text",
    "reply_to": 0
  }
}
```

**响应**：

```json
{
  "message": {
    "ID": "msg-uuid-001",
    "ClientMessageID": "550e8400-e29b-41d4-a716-446655440000",
    "ConversationID": "conv-uuid-001",
    "MessageID": 1,
    "SenderID": "alice",
    "Content": "Hello, world!",
    "Type": "text",
    "ReplyTo": 0,
    "Status": "sent",
    "CreatedAt": "2026-07-08T12:00:00Z",
    "DeletedAt": null
  },
  "duplicate": false
}
```

**幂等命中响应**（`duplicate=true`）：

```json
{
  "message": { ... },
  "duplicate": true
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | missing required field: conversation_id | 缺少必填字段 |
| -100 | missing required field: client_message_id | 缺少必填字段 |
| -100 | missing required field: content | 缺少必填字段 |
| -101 | conversation not found | 会话不存在 |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |
| -300 | check idempotency: ... | 幂等性检查失败 |
| -300 | send message: ... | 消息持久化失败 |

---

### sync_updates

增量同步用户更新 (D-009)。客户端使用 `after_seq` 作为分页游标拉取自上次同步以来的增量更新。

使用 `after_seq`（排他性下界）+ `limit` 进行分页 (D-009)。`after_seq=0` 表示从头开始拉取。默认 limit=100，上限 500。`has_more=true` 表示还有更多更新，客户端应继续调用。`latest_seq` 是当前用户的全局最新 seq（非返回结果中最大的 seq），用于客户端检测数据间隙。

**参数**：

| 字段      | 类型   | 必填 | 默认值 | 说明                                         |
|-----------|--------|------|--------|----------------------------------------------|
| after_seq | uint32 | 否   | 0      | 排他性下界，返回 seq > after_seq 的更新      |
| limit     | int    | 否   | 100    | 每页数量，范围 [1, 500]                      |

**请求示例**：

```json
{
  "id": "su-001",
  "method": "sync_updates",
  "params": {
    "after_seq": 0,
    "limit": 50
  }
}
```

**响应**：

```json
{
  "updates": [
    {
      "seq": 1,
      "type": "message",
      "payload": { ... },
      "created_at": "2026-07-08T12:00:00Z"
    },
    {
      "seq": 2,
      "type": "mark_read",
      "payload": { ... },
      "created_at": "2026-07-08T12:01:00Z"
    }
  ],
  "has_more": true,
  "latest_seq": 150
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -300 | list updates: ... | 查询更新列表失败 |
| -300 | get latest seq: ... | 获取最新序号失败 |

### Update 类型

`sync_updates` 和实时推送中的每个 Update 包含 `type` 字段，标识更新类型：

| Type | 说明 | Payload 结构 |
| ---- | ---- | ------------ |
| `message` | 新消息 | 完整的 Message 对象 |
| `delete_message` | 消息被删除 | `{message_id, conversation_id, message_id_seq}` |
| `mark_read` | 已读位置更新 | `{conversation_id, last_read_message_id}` |
| `conversation` | 会话状态变更 | `{conversation_id, action}` — action 为 `"delete"` 或 `"restore"` |
| `gap` | 补空占位 | `null` — 客户端应跳过，仅更新 local_max_seq |
| `typing` | ephemeral (Seq=0) | `{conversation_id, user_id, is_typing}` — Typing 指示器 (D-050) |
| `streaming` | ephemeral (Seq=0) | `{stream_id, user_id, conversation_id, text, is_done}` — 流式文本 (D-051) |

**处理策略**：

- `message`：保存到本地数据库，显示在会话中
- `delete_message`：从本地数据库删除对应消息
- `mark_read`：更新本地已读位置（仅操作用户的其他设备收到）
- `conversation`：更新本地会话状态（软删除或恢复）
- `gap`：仅递增 `local_max_seq`，不做其他处理
- `typing`：ephemeral push，直接回调 handler，不持久化 (D-050)
- `streaming`：ephemeral push，直接回调 handler，不持久化 (D-051)
- 未知类型：跳过（向前兼容，未来可能新增更多类型）

### Ephemeral Updates (Seq=0)

从 v0.x 开始，系统支持 ephemeral push 模式 (D-050)。这些更新：

- 使用 `Seq=0` 标识
- 不持久化到数据库
- 不入 MQ 队列
- 离线用户不投递
- `sync_updates` 不返回
- Client 的 `ApplyUpdate` 在入口处通过 `Seq==0` 分支直接回调 handler，绕过 seq 连续性检查、去重和本地持久化

---

### create_conversation

创建 1-on-1 会话 (D-011 find-or-create 幂等)。

使用 find-or-create 模式实现幂等性 (D-011)：先查询是否已存在相同用户对的会话，存在则返回已有会话（`duplicate=true`），不存在则创建新会话。幂等性由用户对唯一性保证，无需客户端提供幂等 key。

**参数**：

| 字段    | 类型   | 必填 | 默认值 | 说明                           |
|---------|--------|------|--------|--------------------------------|
| user_id | string | 是   | -      | 对方的用户 ID                  |
| title   | string | 否   | ""     | 会话标题                       |

**请求示例**：

```json
{
  "id": "cc-001",
  "method": "create_conversation",
  "params": {
    "user_id": "bob",
    "title": "Chat with Bob"
  }
}
```

**响应**：

```json
{
  "conversation": {
    "ID": "conv-uuid-001",
    "UserID1": "alice",
    "UserID2": "bob",
    "Type": "1-on-1",
    "Title": "Chat with Bob",
    "Pinned": false,
    "Muted": false,
    "AvatarURL": "",
    "Description": "",
    "LastProcessedMessageID": 0,
    "CreatedAt": "2026-07-08T12:00:00Z",
    "UpdatedAt": "2026-07-08T12:00:00Z",
    "LastMessageAt": "2026-07-08T12:00:00Z",
    "LastReadMessageID1": 0,
    "LastReadMessageID2": 0,
    "DeletedAt": null
  },
  "duplicate": false
}
```

**幂等命中响应**（`duplicate=true`）：

```json
{
  "conversation": { ... },
  "duplicate": true
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | missing required field: user_id | 缺少必填字段 |
| -100 | cannot create conversation with yourself | 不能和自己创建会话 |
| -300 | check existing conversation: ... | 查询已有会话失败 |
| -300 | create conversation: ... | 创建会话失败 |

**注意**：服务端会规范化用户对顺序（字典序小的为 UserID1，大的为 UserID2），确保无论从哪方发起，会话唯一性一致。

---

### list_conversations

列出当前用户的会话列表，按 `LastMessageAt` 降序排列。

**参数**：

| 字段   | 类型 | 必填 | 默认值 | 说明                      |
|--------|------|------|--------|---------------------------|
| offset | int  | 否   | 0      | 分页起始偏移量（>= 0）    |
| limit  | int  | 否   | 20     | 每页数量，范围 [1, 100]   |

**请求示例**：

```json
{
  "id": "lc-001",
  "method": "list_conversations",
  "params": {
    "offset": 0,
    "limit": 20
  }
}
```

**响应**：

```json
{
  "conversations": [
    {
      "ID": "conv-uuid-001",
      "UserID1": "alice",
      "UserID2": "bob",
      "Type": "1-on-1",
      "Title": "",
      "Pinned": false,
      "Muted": false,
      "AvatarURL": "",
      "Description": "",
      "LastProcessedMessageID": 42,
      "CreatedAt": "2026-07-07T10:00:00Z",
      "UpdatedAt": "2026-07-08T12:00:00Z",
      "LastMessageAt": "2026-07-08T12:00:00Z",
      "LastReadMessageID1": 40,
      "LastReadMessageID2": 42,
      "DeletedAt": null
    }
  ],
  "has_more": false
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -300 | list conversations: ... | 查询会话列表失败 |

**注意**：`conversations` 始终返回数组，不会为 null（空列表时为 `[]`）。

---

### get_messages

获取指定会话的消息历史 (D-008)。消息按 `MessageID` 升序排列（从旧到新），使用 `after_message_id` 作为分页游标。

**参数**：

| 字段             | 类型   | 必填 | 默认值 | 说明                                       |
|------------------|--------|------|--------|--------------------------------------------|
| conversation_id  | string | 是   | -      | 会话 ID                                    |
| after_message_id | uint32 | 否   | 0      | 排他性下界，返回 MessageID > after_message_id 的消息 |
| limit            | int    | 否   | 50     | 每页数量，范围 [1, 200]                    |

**请求示例**：

```json
{
  "id": "gm-001",
  "method": "get_messages",
  "params": {
    "conversation_id": "conv-uuid-001",
    "after_message_id": 0,
    "limit": 50
  }
}
```

**响应**：

```json
{
  "messages": [
    {
      "ID": "msg-uuid-001",
      "ClientMessageID": "550e8400-e29b-41d4-a716-446655440000",
      "ConversationID": "conv-uuid-001",
      "MessageID": 1,
      "SenderID": "alice",
      "Content": "Hello!",
      "Type": "text",
      "ReplyTo": 0,
      "Status": "sent",
      "CreatedAt": "2026-07-08T12:00:00Z",
      "DeletedAt": null
    }
  ],
  "has_more": false
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | missing required field: conversation_id | 缺少必填字段 |
| -101 | conversation not found | 会话不存在 |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |
| -300 | list messages: ... | 查询消息列表失败 |

**注意**：`messages` 始终返回数组，不会为 null（空列表时为 `[]`）。

---

### search_messages

在指定会话中搜索消息。使用 LIKE 匹配进行内容搜索，结果按 `MessageID` 降序排列（从新到旧）。

**参数**：

| 字段             | 类型   | 必填 | 默认值 | 说明                                                   |
|------------------|--------|------|--------|--------------------------------------------------------|
| conversation_id  | string | 是   | -      | 会话 ID                                                |
| query            | string | 是   | -      | 搜索关键词                                             |
| after_message_id | uint32 | 否   | 0      | 分页游标（结果按 MessageID DESC 排序，此字段表示获取比该序号更旧的消息） |
| limit            | int    | 否   | 50     | 每页数量，范围 [1, 200]                                |

**请求示例**：

```json
{
  "id": "se-001",
  "method": "search_messages",
  "params": {
    "conversation_id": "conv-uuid-001",
    "query": "hello",
    "after_message_id": 0,
    "limit": 20
  }
}
```

**响应**：

```json
{
  "messages": [
    {
      "ID": "msg-uuid-005",
      "ClientMessageID": "...",
      "ConversationID": "conv-uuid-001",
      "MessageID": 5,
      "SenderID": "bob",
      "Content": "Hello back!",
      "Type": "text",
      "ReplyTo": 1,
      "Status": "sent",
      "CreatedAt": "2026-07-08T12:05:00Z",
      "DeletedAt": null
    }
  ],
  "has_more": false
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | missing required field: conversation_id | 缺少必填字段 |
| -100 | missing required field: query | 缺少必填字段 |
| -101 | conversation not found | 会话不存在 |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |
| -300 | search messages: ... | 搜索消息失败 |

**注意**：`messages` 始终返回数组，不会为 null。与 `get_messages` 不同，搜索结果按 MessageID **降序**排列（最新消息在前）。

---

### get_conversation

获取单个会话详情，包含当前用户的未读消息数。

**参数**：

| 字段            | 类型   | 必填 | 默认值 | 说明     |
|-----------------|--------|------|--------|----------|
| conversation_id | string | 是   | -      | 会话 ID  |

**请求示例**：

```json
{
  "id": "gc-001",
  "method": "get_conversation",
  "params": {
    "conversation_id": "conv-uuid-001"
  }
}
```

**响应**：

```json
{
  "conversation": {
    "ID": "conv-uuid-001",
    "UserID1": "alice",
    "UserID2": "bob",
    "Type": "1-on-1",
    "Title": "",
    "Pinned": false,
    "Muted": false,
    "AvatarURL": "",
    "Description": "",
    "LastProcessedMessageID": 42,
    "CreatedAt": "2026-07-07T10:00:00Z",
    "UpdatedAt": "2026-07-08T12:00:00Z",
    "LastMessageAt": "2026-07-08T12:00:00Z",
    "LastReadMessageID1": 40,
    "LastReadMessageID2": 42,
    "DeletedAt": null
  },
  "unread_count": 2
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | missing required field: conversation_id | 缺少必填字段 |
| -101 | conversation not found | 会话不存在 |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |
| -300 | get conversation: ... | 获取会话失败 |

**注意**：未读计数基于当前用户的已读游标 (D-012)。如果计算未读计数时发生错误，默认返回 0 而非报错。

---

### delete_conversation

删除会话 (D-013 级联软删除)。

执行级联软删除：先软删除会话，再软删除该会话下的所有消息，两个操作在同一数据库事务中执行 (D-013)。当前模型下，Conversation 是双方共享记录，一方删除会话对双方生效。

**参数**：

| 字段            | 类型   | 必填 | 默认值 | 说明     |
|-----------------|--------|------|--------|----------|
| conversation_id | string | 是   | -      | 会话 ID  |

**请求示例**：

```json
{
  "id": "dc-001",
  "method": "delete_conversation",
  "params": {
    "conversation_id": "conv-uuid-001"
  }
}
```

**响应**：

```json
{
  "status": "ok",
  "deleted_message_count": 42
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | missing required field: conversation_id | 缺少必填字段 |
| -101 | conversation not found | 会话不存在 |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |
| -300 | delete conversation: ... | 删除会话失败 |

**注意**：软删除的消息仍然占据 `client_message_id` 的 unique index 命名空间。被删除的会话可通过 `restore_conversation` 恢复。

---

### restore_conversation

恢复会话 (D-015 级联恢复)。

恢复会话记录的同时，级联恢复该会话下所有被软删除的消息，两个操作在同一事务中执行 (D-015)。恢复后会重新计算 `LastProcessedMessageID` 和 `LastMessageAt`。对未删除的会话调用此方法是幂等的——返回当前会话，不报错。

**参数**：

| 字段            | 类型   | 必填 | 默认值 | 说明     |
|-----------------|--------|------|--------|----------|
| conversation_id | string | 是   | -      | 会话 ID  |

**请求示例**：

```json
{
  "id": "rc-001",
  "method": "restore_conversation",
  "params": {
    "conversation_id": "conv-uuid-001"
  }
}
```

**响应**：

```json
{
  "conversation": {
    "ID": "conv-uuid-001",
    "UserID1": "alice",
    "UserID2": "bob",
    "Type": "1-on-1",
    "Title": "",
    "Pinned": false,
    "Muted": false,
    "AvatarURL": "",
    "Description": "",
    "LastProcessedMessageID": 42,
    "CreatedAt": "2026-07-07T10:00:00Z",
    "UpdatedAt": "2026-07-08T12:30:00Z",
    "LastMessageAt": "2026-07-08T12:00:00Z",
    "LastReadMessageID1": 40,
    "LastReadMessageID2": 42,
    "DeletedAt": null
  },
  "restored_message_count": 42
}
```

**幂等响应**（会话未被删除时）：

```json
{
  "conversation": { ... },
  "restored_message_count": 0
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | missing required field: conversation_id | 缺少必填字段 |
| -101 | conversation not found | 会话不存在（包括已永久删除） |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |
| -300 | restore conversation: ... | 恢复会话失败 |

---

### delete_message

删除消息 (D-014 仅发送者可删)。

仅允许消息的发送者删除该消息 (D-014)。执行软删除。

**参数**：

| 字段       | 类型   | 必填 | 默认值 | 说明                            |
|------------|--------|------|--------|---------------------------------|
| message_id | string | 是   | -      | 消息的 UUID 主键                |

**请求示例**：

```json
{
  "id": "dm-001",
  "method": "delete_message",
  "params": {
    "message_id": "msg-uuid-001"
  }
}
```

**响应**：

```json
{
  "status": "ok"
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | missing required field: message_id | 缺少必填字段 |
| -101 | message not found | 消息不存在 |
| -101 | conversation not found | 消息所属会话不存在 |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |
| -200 | only the sender can delete this message | 非消息发送者无权删除 (D-014) |
| -300 | delete message: ... | 删除消息失败 |

---

### mark_as_read

标记会话已读 (D-012 MAX 语义)。

更新调用者在会话中的已读游标位置。使用 `MAX(current_value, new_value)` 语义，已读位置只能向前推进，不会后退 (D-012)。如果不指定 `message_id`，则默认标记所有消息为已读（使用会话的 `LastProcessedMessageID`）。

**参数**：

| 字段            | 类型   | 必填 | 默认值                      | 说明                                     |
|-----------------|--------|------|-----------------------------|------------------------------------------|
| conversation_id | string | 是   | -                           | 会话 ID                                  |
| message_id      | uint32 | 否   | LastProcessedMessageID      | 已读到哪条消息序号，0 表示标记全部已读   |

**请求示例**：

```json
{
  "id": "mr-001",
  "method": "mark_as_read",
  "params": {
    "conversation_id": "conv-uuid-001",
    "message_id": 40
  }
}
```

**标记全部已读**：

```json
{
  "id": "mr-002",
  "method": "mark_as_read",
  "params": {
    "conversation_id": "conv-uuid-001"
  }
}
```

**响应**：

```json
{
  "status": "ok",
  "unread_count": 0,
  "last_read_message_id": 42
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | missing required field: conversation_id | 缺少必填字段 |
| -101 | conversation not found | 会话不存在 |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |
| -300 | update last read: ... | 更新已读游标失败 |
| -300 | count unread: ... | 计算未读计数失败 |

**注意**：如果传入的 `message_id` 大于会话的 `LastProcessedMessageID`，会被自动截断为 `LastProcessedMessageID`。如果传入的 `message_id` 小于当前已读位置，服务器静默忽略（不报错），返回当前已读位置 (D-012)。

---

### set_typing

发送 typing（正在输入）指示器给会话的所有成员（包括调用者的其他设备，D-050）。这是一种 **ephemeral push** (D-050)：不持久化、不入 MQ、离线不投递、上线不补拉。

**参数**：

| 字段            | 类型   | 必填 | 默认值 | 说明                                     |
|-----------------|--------|------|--------|------------------------------------------|
| conversation_id | string | 是   | -      | 会话 ID                                  |
| is_typing       | bool   | 否   | false  | true=开始输入, false=停止输入            |

**请求示例**：

```json
{
  "id": "st-001",
  "method": "set_typing",
  "params": {
    "conversation_id": "conv-uuid-001",
    "is_typing": true
  }
}
```

**响应**：

```json
{
  "status": "ok"
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数无效或缺少 conversation_id |
| -101 | conversation not found | 会话不存在或已删除 |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |

**行为说明**：

- 调用者也会收到自己的 typing 事件（广播给所有成员，包括调用者的其他设备）
- Rate limit: 每用户每会话 1次/秒/节点（多节点部署时为 best-effort 限流），超限静默返回 OK
- 推送使用 Seq=0 标识 ephemeral 更新 (D-050)

---

### stream_text

发送流式文本给会话的所有成员。这是一种 **ephemeral push** (D-050, D-051)：不持久化、不入 MQ、离线不投递、上线不补拉。

采用**累积文本模式**：每帧包含完整文本快照（非 delta），接收方直接替换显示内容。流式结束后通过 `send_message` 持久化最终消息 (D-052)。

**参数**：

| 字段            | 类型   | 必填 | 默认值 | 说明                                        |
|-----------------|--------|------|--------|---------------------------------------------|
| conversation_id | string | 是   | -      | 会话 ID                                     |
| stream_id       | string | 是   | -      | 流 ID（客户端生成的 UUID，服务端仅透传）      |
| text            | string | 是   | -      | 累积文本内容（完整快照）                      |
| is_done         | bool   | 否   | false  | true=流式结束信号                             |

**请求示例**：

```json
{
  "id": "stream-001",
  "method": "stream_text",
  "params": {
    "conversation_id": "conv-uuid-001",
    "stream_id": "550e8400-e29b-41d4-a716-446655440000",
    "text": "Hello, this is a streaming text",
    "is_done": false
  }
}
```

**响应**：

```json
{
  "status": "ok"
}
```

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数无效或缺少必填字段 |
| -101 | conversation not found | 会话不存在或已删除 |
| -200 | user is not a member of the conversation | 调用者不是会话成员 |

**行为说明**：

- 所有成员（包括调用者的其他设备）都会收到流式推送（D-050）
- Rate limit: 每用户每会话 20次/秒/节点（50ms 间隔），超限静默返回 OK
- 推送使用 Seq=0 标识 ephemeral 更新
- 流式结束后采用两步协议 (D-052)：
  1. `stream_text(is_done=true, text=最终文本)` — 广播结束信号
  2. `send_message(content=最终文本)` — 持久化消息

**流式文本时序图**：

```text
发送方                          服务端                          接收方
  │                               │                               │
  ├──stream_text(text="Hel")──────▶│                               │
  │                               ├────push(Seq=0,streaming)──────▶│
  │                               │                               │
  ├──stream_text(text="Hello")────▶│                               │
  │                               ├────push(Seq=0,streaming)──────▶│
  │                               │                               │
  ├──stream_text(done=true)───────▶│                               │
  │                               ├────push(Seq=0,is_done)────────▶│
  │                               │                               │
  ├──send_message("Hello")────────▶│                               │
  │                               ├────push(Seq=N,message)────────▶│
  │◀──────response(ok)────────────│                               │
```

---

### reload_agents

重新加载 Agent 配置 (D-076)。从磁盘目录重新扫描并加载所有 Agent 配置文件。

**参数**：

无参数。

**请求示例**：

```json
{
  "id": "ra-001",
  "method": "reload_agents",
  "params": {}
}
```

**响应**：

```json
{
  "count": 5
}
```

| 字段  | 类型 | 说明                 |
|-------|------|----------------------|
| count | int  | 加载的 Agent 数量    |

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -300 | reload agents from "agents": ... | 重新加载 Agent 配置失败 |

**注意**：当 AgentRegistry 为 nil 时（未配置 Agent 目录），返回 `{"count": 0}` 而不报错 (D-063)。

---

### agent_resume

恢复被 HITL 中断暂停的 Agent (D-085, D-116)。客户端在回答 Agent 的问题后调用此方法，将答案持久化到数据库并在所有问题回答完毕后触发 Agent 继续执行。

支持并行子代理场景：当一个 checkpoint 下有多个 Question 时，每次调用回答一个问题。只有当所有 Question 都回答后才会入队 MQ 任务恢复 Agent。

**参数**：

| 字段            | 类型   | 必填 | 默认值 | 说明                                              |
|-----------------|--------|------|--------|---------------------------------------------------|
| conversation_id | string | 是   | -      | 会话 ID                                           |
| answer          | string | 是   | -      | 用户的回答                                        |
| agent_id        | string | 是   | -      | Agent 标识符                                      |
| checkpoint_id   | string | 否   | -      | HITL checkpoint ID（不提供时从 Conversation 推断） |
| interrupt_id    | string | 否   | -      | 指定回答哪个 interrupt（多问题时使用）             |

**请求示例**：

```json
{
  "id": "ar-001",
  "method": "agent_resume",
  "params": {
    "conversation_id": "conv-uuid-001",
    "answer": "确认删除",
    "agent_id": "my-smart-agent",
    "checkpoint_id": "",
    "interrupt_id": ""
  }
}
```

**响应**（部分回答，仍有待回答问题）：

```json
{
  "status": "partial",
  "answered": 1,
  "total": 2,
  "pending": 1
}
```

**响应**（全部回答完毕，已入队恢复任务）：

```json
{
  "status": "queued",
  "answered": 2,
  "total": 2
}
```

| 字段     | 类型   | 说明                                           |
|----------|--------|------------------------------------------------|
| status   | string | `"partial"` 或 `"queued"`                      |
| answered | int    | 已回答的问题数                                 |
| total    | int    | 该 checkpoint 下的总问题数                     |
| pending  | int    | 剩余待回答问题数（仅 status="partial" 时返回） |

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | conversation_id, answer and agent_id are required | 缺少必填字段 |
| -100 | checkpoint_id is required and cannot be inferred from conversation | checkpoint_id 无法推断 |
| -101 | conversation not found | 会话不存在 |
| -101 | no pending question found for the given checkpoint/interrupt | 无匹配的待回答问题 |
| -101 | question not found | 问题不存在 |
| -300 | agent_resume: ... | 服务器内部错误 |
| **-409** | **question_already_answered** | **问题已被回答（多设备冲突/幂等检查，D-116）** |

**注意**：答案持久化到 Question 表，MQ 任务 payload 中**不包含答案** (D-116)。服务器重启后答案不会丢失。

---

### system.register_functions

注册设备函数清单 (D-098, D-099)。客户端连接后发送此请求声明自身可调用的函数能力。服务端按 `(user_id, device_id)` 存储函数清单，供 Agent 动态工具注入使用。

此方法为全量替换模式：每次调用会覆盖该设备之前注册的所有函数。空的 `functions` 列表是合法的，会清除该设备的所有已注册函数。

**参数**：

| 字段        | 类型              | 必填 | 默认值 | 说明                                               |
|-------------|-------------------|------|--------|----------------------------------------------------|
| device_id   | string            | 否   | -      | 设备 ID（将被连接认证的 device_id 覆盖，D-093）    |
| device_info | map[string]string | 否   | -      | 设备元信息（如 name, type 等）                     |
| functions   | FunctionInfo[]    | 是   | -      | 函数清单（详见 [FunctionInfo](#functioninfo) 模型）|

**请求示例**：

```json
{
  "id": "rf-001",
  "method": "system.register_functions",
  "params": {
    "device_id": "desktop-abc123",
    "device_info": {
      "name": "My MacBook Pro",
      "type": "desktop"
    },
    "functions": [
      {
        "name": "read_file",
        "description": "读取本地文件内容",
        "parameters": {
          "type": "object",
          "properties": {
            "path": {"type": "string", "description": "文件路径"}
          },
          "required": ["path"]
        },
        "returns": {"type": "string", "description": "文件内容"},
        "tags": ["filesystem", "read"],
        "timeout_ms": 5000
      }
    ]
  }
}
```

**响应**：

```json
{
  "status": "ok",
  "count": 1,
  "device_id": "desktop-abc123"
}
```

| 字段      | 类型   | 说明                                       |
|-----------|--------|--------------------------------------------|
| status    | string | `"ok"`                                     |
| count     | int    | 注册的函数数量                             |
| device_id | string | 实际使用的设备 ID（来自连接认证，非请求参数）|

**错误**：

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params | 参数 JSON 解析失败 |
| -100 | function name must not be empty | 函数名为空 |
| -100 | function name exceeds maximum length | 函数名超过最大长度（255 字符） |
| -100 | duplicate function name in registration | 注册列表中有重复函数名 |
| -100 | max functions per device exceeded | 函数数量超过上限（默认 200） |
| -300 | internal error | 服务器内部错误 |

**注意**：
- 请求参数中的 `device_id` 会被忽略，服务端使用连接认证时的 `device_id` 作为权威来源 (D-093)
- 此方法为 nil-safe：当 FunctionRegistry 未配置时不会注册此方法 (D-063)
- 设备断开连接时自动清理函数清单

---

### system.reconnect

重连握手与请求补发 (D-108)。客户端重连后发送此请求，服务端查找该设备的 pending 请求并按 seq 顺序异步补发。

**参数**：

| 字段          | 类型   | 必填 | 默认值 | 说明                                        |
|---------------|--------|------|--------|---------------------------------------------|
| last_seen_seq | uint64 | 否   | 0      | 客户端最后收到的 seq，服务端补发 seq > 此值的请求 |

**请求示例**：

```json
{
  "id": "re-001",
  "method": "system.reconnect",
  "params": {
    "last_seen_seq": 42
  }
}
```

**响应**：

```json
{
  "status": "ok",
  "replayed": 3,
  "total": 5
}
```

| 字段     | 类型   | 说明                                   |
|----------|--------|----------------------------------------|
| status   | string | `"ok"`                                 |
| replayed | int    | 正在异步补发的请求数量                 |
| total    | int    | 该设备的 pending 请求总数（含不符合补发条件的） |

**错误**：

此方法采用 fail-open 策略 (D-072)：查询 pending 请求失败时仍返回 `{"status": "ok", "replayed": 0, "total": 0}` 而非报错。

| Code | 错误信息 | 说明 |
|------|----------|------|
| -100 | invalid params: ... | 参数 JSON 解析失败 |

**行为说明**：

- 补发是异步的：响应立即返回，补发在后台 goroutine 中逐个执行 (D-109)
- 每个补发的请求使用 `replayTimeout`（默认 10 秒）作为超时
- 补发失败的请求 `retry_count++`，超过 `max_retries` 后从 pending store 中删除
- 补发成功的请求从 pending store 中移除
- 原调用方已离开，补发结果记录到日志
- 此方法为 nil-safe：当 PendingStore 未配置时返回 `{"status": "ok", "replayed": 0, "total": 0}` (D-063)

---

## 错误码

所有错误响应的 `code` 字段使用结构化错误码 (D-017)。负数表示错误，分段分配：

| Code | 名称 | 说明 |
|------|------|------|
| 0 | OK | 成功 |
| -1 | Error | 通用错误（未分类，向后兼容） |
| **-100** | **ValidationError** | **参数验证失败（缺少必填字段、类型错误、JSON 解析失败等）** |
| **-101** | **NotFound** | **资源不存在（会话、消息、连接等）** |
| -102 | Duplicate | 资源重复（幂等命中时返回成功，不会返回此错误码） |
| **-200** | **PermissionDenied** | **权限不足（如非发送者尝试删除消息、非会话成员）** |
| -201 | Forbidden | 禁止访问 |
| **-300** | **InternalError** | **服务器内部错误** |
| -301 | Unavailable | 服务不可用 |
| **-409** | **Conflict** | **资源状态冲突（如 HITL 问题已被回答，幂等检查）** |

**向后兼容**：旧客户端检查 `code < 0` 判断错误仍然有效。

### 各方法的错误码

| 方法 | 可能的错误码 |
|------|-------------|
| heartbeat | -101 (connection expired), -300 (internal) |
| send_message | -100 (validation), -101 (not found), -200 (not a member), -300 (internal) |
| sync_updates | -100 (validation), -300 (internal) |
| create_conversation | -100 (validation), -300 (internal) |
| list_conversations | -100 (validation), -300 (internal) |
| get_messages | -100 (validation), -101 (not found), -200 (not a member), -300 (internal) |
| search_messages | -100 (validation), -101 (not found), -200 (not a member), -300 (internal) |
| get_conversation | -100 (validation), -101 (not found), -200 (not a member), -300 (internal) |
| delete_conversation | -100 (validation), -101 (not found), -200 (not a member), -300 (internal) |
| restore_conversation | -100 (validation), -101 (not found), -200 (not a member), -300 (internal) |
| delete_message | -100 (validation), -101 (not found), -200 (permission denied / not a member), -300 (internal) |
| mark_as_read | -100 (validation), -101 (not found), -200 (not a member), -300 (internal) |
| set_typing | -100 (validation), -101 (not found), -200 (not a member) |
| stream_text | -100 (validation), -101 (not found), -200 (not a member) |
| reload_agents | -300 (internal) |
| agent_resume | -100 (validation), -101 (not found), -300 (internal), -409 (conflict) |
| system.register_functions | -100 (validation), -300 (internal) |
| system.reconnect | -100 (validation) |

---

## 数据模型

### Conversation

| 字段                   | 类型         | 说明                                     |
|------------------------|--------------|------------------------------------------|
| ID                     | string       | 会话唯一 ID（UUID，主键）                |
| UserID1                | string       | 用户 1 的 ID（字典序较小者）             |
| UserID2                | string       | 用户 2 的 ID（字典序较大者），1-on-1 不为空 |
| Type                   | string       | 会话类型：1-on-1 / group / channel       |
| Title                  | string       | 会话标题                                 |
| Pinned                 | bool         | 是否置顶                                 |
| Muted                  | bool         | 是否静音                                 |
| AvatarURL              | string       | 会话头像 URL                             |
| Description            | string       | 会话描述                                 |
| LastProcessedMessageID | uint32       | 最后处理的消息序号                       |
| CreatedAt              | time.Time    | 创建时间                                 |
| UpdatedAt              | time.Time    | 更新时间                                 |
| LastMessageAt          | time.Time    | 最后消息时间                             |
| LastReadMessageID1     | uint32       | UserID1 的已读游标位置 (D-012)           |
| LastReadMessageID2     | uint32       | UserID2 的已读游标位置 (D-012)           |
| AgentStatus            | string       | Agent 状态机：`idle` / `thinking` / `tool_calling` / `generating` / `asking_user` / `timeout` |
| AgentID                | string       | 当前执行的 Agent 标识符（nullable）       |
| CheckpointID           | string       | 当前 HITL checkpoint ID（nullable）       |
| AgentLastActivity      | time.Time    | Agent 最后活动时间                        |
| DeletedAt              | gorm.DeletedAt | 软删除时间戳（null 表示未删除）         |

### Message

| 字段            | 类型           | 说明                                       |
|-----------------|----------------|--------------------------------------------|
| ID              | string         | 消息唯一 ID（UUID，主键）                  |
| ClientMessageID | string         | 客户端消息唯一 ID（unique index，幂等键）  |
| ConversationID  | string         | 所属会话 ID                                |
| MessageID       | uint32         | 会话内单调递增消息序号 (D-008)             |
| SenderID        | string         | 发送者用户 ID                              |
| Content         | string         | 消息内容                                   |
| Type            | string         | 消息类型，默认 "text"                      |
| ReplyTo         | uint32         | 回复的消息序号（MessageID），0 表示不回复  |
| Status          | string         | 消息状态，默认 "sent"                      |
| CreatedAt       | time.Time      | 创建时间                                   |
| DeletedAt       | gorm.DeletedAt | 软删除时间戳（null 表示未删除）            |

### UserUpdate

| 字段      | 类型      | 说明                                                  |
|-----------|-----------|-------------------------------------------------------|
| ID        | string    | 更新记录唯一 ID（UUID，主键）                         |
| UserID    | string    | 所属用户 ID                                           |
| Seq       | uint32    | 单调递增序号，用于增量同步排序 (D-009)                |
| Type      | string    | 更新类型（message / delete_message / mark_read / conversation） |
| Payload   | []byte    | 更新内容（JSON 编码的字节数组）                       |
| CreatedAt | time.Time | 创建时间                                              |

### Question

HITL (Human-in-the-Loop) 问题实体 (D-085, D-116)。Agent 暂停等待用户回答时创建，用于离线用户支持、多设备同步和服务器重启恢复。

| 字段             | 类型           | 说明                                                  |
|------------------|----------------|-------------------------------------------------------|
| ID               | string         | 问题唯一 ID（UUID，主键）                             |
| ConversationID   | string         | 所属会话 ID（FK -> Conversation）                     |
| CheckpointID     | string         | 关联的 Eino checkpoint ID                             |
| InterruptID      | string         | Eino interrupt address ID                             |
| QuestionText     | string         | 问题内容                                              |
| Status           | string         | 状态：`pending` / `answered`                          |
| Answer           | string         | 用户回答（nullable）                                  |
| AnsweredBy       | string         | 回答者 user_id                                        |
| AnsweredDeviceID | string         | 回答设备 ID                                           |
| CreatedAt        | time.Time      | 创建时间                                              |
| AnsweredAt       | *time.Time     | 回答时间（nullable）                                  |
| DeletedAt        | gorm.DeletedAt | 软删除时间戳（null 表示未删除）                       |

### FunctionInfo

客户端设备声明的可调用函数描述 (D-098, D-099)。用于 `system.register_functions` 请求中。

| 字段        | 类型             | 说明                                                    |
|-------------|------------------|---------------------------------------------------------|
| name        | string           | 函数唯一标识符（设备范围内，最长 255 字符）             |
| description | string           | 函数功能描述（可选）                                    |
| parameters  | map[string]any   | JSON Schema (draft 7) 描述的输入参数（可选）            |
| returns     | ReturnInfo       | 函数返回值描述（可选）                                  |
| tags        | string[]         | 标签列表，用于过滤（可选）                              |
| timeout_ms  | int              | 函数超时时间（毫秒），0 表示使用 Agent 默认值（可选）   |

#### ReturnInfo

| 字段        | 类型   | 说明                 |
|-------------|--------|----------------------|
| type        | string | 返回值类型           |
| description | string | 返回值描述（可选）   |
