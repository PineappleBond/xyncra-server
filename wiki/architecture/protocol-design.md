---
last_updated: 2026-07-17
---

# WebSocket 协议设计

> last_updated: 2026-07-17

## 概述

Xyncra 使用 **JSON over WebSocket TextMessage** 作为通信协议。协议设计包含三层信封结构，支持请求-响应模型和服务器主动推送。

## 三层信封结构

```
Layer 1: Package        → 最外层：类型路由（Request/Response/Updates）
Layer 2: PackageData    → 中间层：业务数据（ID、Method、Params / Code、Msg）
Layer 3: Payload        → 内层：具体业务负载（Message、Conversation 等）
```

### Layer 1: Package

```go
// pkg/protocol/protocol.go
type Package struct {
    Version uint8           `json:"version,omitempty"` // 协议版本，默认 1
    Type    PackageType     `json:"type"`               // 包类型
    Data    json.RawMessage `json:"data"`               // Layer 2 JSON
}

type PackageType uint8

const (
    PackageTypeRequest  PackageType = iota // 客户端请求
    PackageTypeResponse                    // 服务端响应
    PackageTypeUpdates                     // 服务端推送更新
)
```

- **PackageTypeRequest**：客户端 → 服务端，调用 RPC 方法
- **PackageTypeResponse**：服务端 → 客户端，RPC 响应
- **PackageTypeUpdates**：服务端 → 客户端，推送数据变更（增量同步）

### Layer 2: PackageData (Request)

```go
type PackageDataRequest struct {
    ID             string          `json:"id"`                        // 请求唯一 ID（UUID）
    Method         string          `json:"method"`                    // RPC 方法名
    Params         json.RawMessage `json:"params"`                    // 方法参数
    IdempotencyKey string          `json:"idempotency_key,omitempty"` // 幂等性键（服务端发起请求时使用，D-104）
    Seq            uint64          `json:"seq,omitempty"`             // 反向 RPC 序号（D-106）
}
```

### Layer 2: PackageData (Response)

```go
type PackageDataResponse struct {
    ID   string          `json:"id"`
    Code ResponseCode    `json:"code"` // 0=成功，负数=错误
    Msg  string          `json:"msg"`
    Data json.RawMessage `json:"data"`
}

type ResponseCode int32

const (
    ResponseCodeOK    ResponseCode = 0   // 成功
    ResponseCodeError ResponseCode = -1  // 通用错误
)
```

### Layer 2: PackageData (Updates)

```go
type PackageDataUpdates struct {
    Updates []PackageDataUpdate `json:"updates"`
}

type PackageDataUpdate struct {
    Seq       uint32          `json:"seq"`                 // 单调递增序号（0=瞬时消息）
    Type      string          `json:"type"`                // 更新类型
    Payload   json.RawMessage `json:"payload"`             // 更新数据
    CreatedAt time.Time       `json:"created_at,omitempty"`
}
```

## 更新类型（UpdateType）

```go
const (
    // 持久化类型（Seq > 0）
    UpdateTypeMessage       = "message"         // 新消息
    UpdateTypeDeleteMessage = "delete_message"  // 消息删除
    UpdateTypeMarkRead      = "mark_read"       // 读指针更新
    UpdateTypeConversation  = "conversation"    // 会话变更（删除/恢复/更新）
    UpdateTypeGap           = "gap"             // 运行时 gap 占位（不持久化）

    // 瞬时类型（Seq = 0，不持久化，不拉取）
    UpdateTypeTyping        = "typing"          // 输入中指示
    UpdateTypeStreaming     = "streaming"       // 流式文本
    UpdateTypeAgentStatus   = "agent_status"    // Agent 状态（thinking/tool_calling/generating/idle/asking_user）
    UpdateTypeAgentTimeout  = "agent_timeout"   // Agent 超时
)
```

## RPC 方法目录

### 业务方法

| 方法 | 方向 | 参数 | 说明 |
|------|------|------|------|
| `heartbeat` | C→S | 无 | 心跳保活，被动 TTL 续期 |
| `send_message` | C→S | `{conversation_id, client_message_id, content, type?, reply_to?}` | 发送消息，幂等（D-006） |
| `sync_updates` | C→S | `{after_seq, limit}` | 增量同步，gap 填充（D-029） |
| `create_conversation` | C→S | `{user_id, title}` | find-or-create（D-011） |
| `list_conversations` | C→S | `{offset?, limit?}` | 服务端分页查询 |
| `get_conversation` | C→S | `{conversation_id}` | 单会话 + 未读 + HITL 问题 |
| `get_messages` | C→S | `{conversation_id, after_message_id, limit}` | 消息分页，支持按需拉取（D-126） |
| `search_messages` | C→S | `{conversation_id, query}` | 全文搜索 |
| `delete_conversation` | C→S | `{conversation_id}` | 级联软删除（D-013） |
| `restore_conversation` | C→S | `{conversation_id}` | 级联恢复（D-015） |
| `delete_message` | C→S | `{message_id}` | 仅发送者可删（D-014） |
| `mark_as_read` | C→S | `{conversation_id, message_id}` | MAX 语义更新读指针（D-012） |
| `set_typing` | C→S | `{conversation_id, is_typing}` | 瞬时推送（Seq=0） |
| `stream_text` | C→S | `{stream_id, conversation_id, text, is_done}` | 瞬时推送（Seq=0） |
| `agent_resume` | C→S | `{conversation_id}` | HITL 中断后恢复 Agent（D-085） |

### 系统方法（`system.` 命名空间）

| 方法 | 方向 | 参数 | 说明 |
|------|------|------|------|
| `system.register_functions` | C→S | `{functions: [...FunctionInfo]}` | 注册客户端函数（D-098） |
| `system.reconnect` | C→S | `{last_seen_seq}` | 重连握手 + 请求重放（D-108） |

## 反向 RPC

服务端可以向客户端发起请求（D-092），使用标准的 `PackageTypeRequest` + `PackageTypeResponse` 交互模式。

```
服务端 ── PackageTypeRequest { method: "client.callFunction", params: {...} } ──→ 客户端
客户端 ── PackageTypeResponse { code: 0, data: {...} } ──→ 服务端
```

**字段说明**（D-104/D-106）：
- `IdempotencyKey`：等于请求 UUID，客户端用于去重
- `Seq`：per-device 单调递增序号，用于重连后识别丢失的请求

**超时持久化**（D-103）：
- 超时的反向 RPC 保存到 Redis `pending:{userID}\x00{deviceID}` 列表中
- 客户端重连后通过 `system.reconnect` 获取并重放

## 连接生命周期

```
                   ┌─────────────────────────┐
                   │    HTTP Upgrade          │
                   │  (user_id + device_id)   │
                   └─────────┬───────────────┘
                             │
                   ┌─────────▼───────────────┐
                   │    Connection Active      │
                   │  readPump / writePump     │
                   │  Heartbeat keepalive      │
                   └─────────┬───────────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
     ┌─────────────┐ ┌───────────┐ ┌──────────────┐
     │  4001 Close │ │  Normal   │ │  Network     │
     │ (Device     │ │  Close    │ │  Disconnect  │
     │  Replace)   │ │           │ │              │
     └─────────────┘ └───────────┘ └──────────────┘
              │              │              │
              ▼              │              ▼
     ┌──────────────┐       │     ┌─────────────────┐
     │ Client 优雅   │       │     │  Exponential     │
     │ 退出(exit 0) │       │     │  Backoff Reconnect│
     └──────────────┘       │     └────────┬────────┘
                            ▼              │
                   ┌──────────────┐        │
                   │  结束会话     │◄───────┘
                   └──────────────┘
```

### 认证（D-005）

认证在 HTTP Upgrade 阶段通过 `user_id` 查询参数实现。生产环境通过反向代理（Nginx/Caddy）进行 JWT 验证。

```
ws://server/ws?user_id=alice&device_id=iphone-xyz
```

### 设备替换（D-095）

当同一个 (userID, deviceID) 建立新连接时：
1. 新连接 HTTP Upgrade 前，`CancelDevice` 取消旧连接的待处理反向 RPC
2. HTTP Upgrade 成功
3. 旧连接收到 `4001 close frame`（原因：`replaced by new connection from same device`）
4. 旧连接识别 4001 → 不重连 → 优雅退出（exit 0）
5. 异步清理旧连接的本地索引和 ConnectionStore

**关键时序保证**：
- `CancelDevice` 在 `Upgrade` 之前执行：新连接注册前取消旧连接的请求
- 新连接 connID 与旧连接不同：`removeClient` 按 connID 删除，不会误删新连接

### 重连策略（D-044）

- 初始连接：无限重试
- 断开后：指数退避重连（baseDelay × 2^(attempt-1)，+/-25% 随机抖动）
- 4001 替换：不重连，daemon 优雅退出
- 重连成功后：`system.reconnect` → `system.register_functions` → `FullSync`

## 错误协议

```go
// 错误码分段
// -100 到 -199: 客户端错误
// -200 到 -299: 权限错误
// -300 到 -399: 服务端错误

const (
    // 客户端错误
    ResponseCodeValidationError  ResponseCode = -100
    ResponseCodeNotFound         ResponseCode = -101
    ResponseCodeDuplicate        ResponseCode = -102

    // 权限错误
    ResponseCodePermissionDenied ResponseCode = -200
    ResponseCodeForbidden        ResponseCode = -201

    // 服务端错误
    ResponseCodeInternalError    ResponseCode = -300
    ResponseCodeUnavailable      ResponseCode = -301
)
```

**传输方式**：`HandlerError` 类型通过 `PackageDataResponse.Code` 和 `PackageDataResponse.Msg` 传递给客户端。

## JSON 编码示例

### 客户端发送消息请求

```json
{
  "version": 1,
  "type": 0,
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "method": "send_message",
    "params": {
      "conversation_id": "conv-abc-123",
      "client_message_id": "client-uniq-id-456",
      "content": "Hello, world!"
    }
  }
}
```

### 服务端成功响应

```json
{
  "version": 1,
  "type": 1,
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "code": 0,
    "msg": "ok",
    "data": {
      "message": {
        "id": "msg-uuid",
        "message_id": 42,
        "content": "Hello, world!",
        "sender_id": "alice",
        "status": "sent"
      },
      "duplicate": false
    }
  }
}
```

### 服务端推送更新

```json
{
  "version": 1,
  "type": 2,
  "data": {
    "updates": [
      {
        "seq": 1024,
        "type": "message",
        "payload": {
          "id": "msg-uuid",
          "message_id": 42,
          "conversation_id": "conv-abc-123",
          "sender_id": "alice",
          "content": "Hello, world!",
          "created_at": "2026-07-16T10:00:00Z"
        },
        "created_at": "2026-07-16T10:00:00Z"
      }
    ]
  }
}
```

### 瞬时推送（Seq=0）

```json
{
  "version": 1,
  "type": 2,
  "data": {
    "updates": [
      {
        "seq": 0,
        "type": "typing",
        "payload": {
          "user_id": "alice",
          "conversation_id": "conv-abc-123",
          "is_typing": true,
          "timestamp": 1721116800
        },
        "created_at": "2026-07-16T10:00:00Z"
      }
    ]
  }
}
```

### 完整请求/响应交互示例

以下展示一次完整的消息发送流程，包含 3 层信封结构：

**客户端请求**（Layer 1 `Package` → Layer 2 `PackageDataRequest` → Layer 3 `params`）：

```json
{
  "version": 1,
  "type": 0,
  "data": {
    "id": "req-a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "method": "send_message",
    "params": {
      "conversation_id": "conv-xyz-987",
      "client_message_id": "cmt-001",
      "content": "Hello!",
      "type": "text"
    }
  }
}
```

**服务端成功响应**（Layer 1 → Layer 2 `PackageDataResponse` → Layer 3 `data.message`）：

```json
{
  "version": 1,
  "type": 1,
  "data": {
    "id": "req-a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "code": 0,
    "msg": "ok",
    "data": {
      "message": {
        "id": "msg-uuid-001",
        "message_id": 100,
        "conversation_id": "conv-xyz-987",
        "sender_id": "alice",
        "content": "Hello!",
        "type": "text",
        "status": "sent",
        "created_at": "2026-07-17T08:00:00Z"
      },
      "duplicate": false
    }
  }
}
```

**服务端推送给其他设备**（Layer 1 → Layer 2 `PackageDataUpdates` → Layer 3 `updates[].payload`）：

```json
{
  "version": 1,
  "type": 2,
  "data": {
    "updates": [
      {
        "seq": 2048,
        "type": "message",
        "payload": {
          "id": "msg-uuid-001",
          "message_id": 100,
          "conversation_id": "conv-xyz-987",
          "sender_id": "alice",
          "content": "Hello!",
          "type": "text",
          "created_at": "2026-07-17T08:00:00Z"
        },
        "created_at": "2026-07-17T08:00:00Z"
      }
    ]
  }
}
```

### 创建会话请求

```json
{
  "version": 1,
  "type": 0,
  "data": {
    "id": "req-1111-2222-3333-4444",
    "method": "create_conversation",
    "params": {
      "user_id": "bob",
      "title": "工作讨论"
    }
  }
}
```

**响应**：
```json
{
  "version": 1,
  "type": 1,
  "data": {
    "id": "req-1111-2222-3333-4444",
    "code": 0,
    "msg": "ok",
    "data": {
      "conversation": {
        "id": "conv-xyz-987",
        "user_id1": "alice",
        "user_id2": "bob",
        "type": "1on1",
        "created_at": "2026-07-17T08:00:00Z"
      },
      "duplicate": false
    }
  }
}
```

### 增量同步请求

```json
{
  "version": 1,
  "type": 0,
  "data": {
    "id": "req-sync-001",
    "method": "sync_updates",
    "params": {
      "after_seq": 2048,
      "limit": 50
    }
  }
}
```

**响应**（含多个更新）：
```json
{
  "version": 1,
  "type": 1,
  "data": {
    "id": "req-sync-001",
    "code": 0,
    "msg": "ok",
    "data": {
      "updates": [
        {"seq": 2049, "type": "message", "payload": {...}},
        {"seq": 2050, "type": "mark_read", "payload": {...}}
      ],
      "has_more": false
    }
  }
}
```

### Agent 恢复请求（HITL）

```json
{
  "version": 1,
  "type": 0,
  "data": {
    "id": "req-resume-001",
    "method": "agent_resume",
    "params": {
      "conversation_id": "conv-xyz-987"
    }
  }
}
```

### 服务端错误响应

```json
{
  "version": 1,
  "type": 1,
  "data": {
    "id": "req-a1b2c3d4-...",
    "code": -100,
    "msg": "validation error: missing required field: conversation_id",
    "data": null
  }
}
```
