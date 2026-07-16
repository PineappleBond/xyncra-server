# Xyncra Client Usage Skill

Use this skill when working with the xyncra-client CLI tool.

## 前置条件

1. **启动 Redis**：`redis-server`（默认 localhost:6379）
2. **启动服务器**：`./xyncra-server`（默认 :8080）
3. **启动客户端 daemon**：`./xyncra-client listen --user-id <user> --device-id <device>`

## Decision Tree (read first)

1. Start the daemon? → `xyncra-client listen --user-id <user> --device-id <device>`
2. Send / modify data? → send, create-conversation, delete-conversation,
   restore-conversation, delete-message, mark-as-read (IPC+WS fallback)
3. Query local data? → list-conversations / get-conversation / get-messages /
   search-messages (local SQLite, offline OK)
4. Manual sync? → sync-updates (requires daemon, IPC-only)
5. Drafts / logs? → draft save/get/delete, logs tail/search/stats/export/cleanup (local SQLite)
6. Stop daemon? → `xyncra-client kill [--force]`
7. Resume HITL-interrupted agent? → `xyncra-client agent-resume` (IPC-only, D-114)
8. Writing tests or direct API client? → **Server Protocol** section below (WebSocket JSON-RPC, sync_updates HTTP, Agent reply flow)
9. More details? → Check references/ links below

## Command Table

| Command | Mode | Description |
|---------|------|-------------|
| `listen` | Daemon | Start long-running daemon (IPC + WebSocket + built-in functions) |
| `send` | IPC+WS | Send message to conversation |
| `create-conversation` | IPC+WS | Create 1-on-1 conversation (find-or-create) |
| `delete-conversation` | IPC+WS | Soft-delete conversation + messages |
| `restore-conversation` | IPC+WS | Restore soft-deleted conversation |
| `delete-message` | IPC+WS | Soft-delete message (sender only) |
| `mark-as-read` | IPC+WS | Mark messages as read |
| `list-conversations` | Local DB | List conversations from SQLite |
| `get-conversation` | Local DB | Show conversation details |
| `get-messages` | Local DB | List messages from SQLite |
| `search-messages` | Local DB | Search messages in SQLite |
| `sync-updates` | IPC-only | Trigger full sync via daemon (no fallback) |
| `agent-resume` | IPC-only | Resume HITL-interrupted agent (D-036, D-114) |
| `draft save/get/delete` | Local DB | Manage message drafts |
| `logs tail/search/stats/export/cleanup` | Local DB | View and manage logs |
| `kill` | OS process | Terminate running daemon |

## Global Flags

| Flag | Short | Env Variable | Default | Description |
|------|-------|--------------|---------|-------------|
| `--user-id` | `-u` | `XYNCRA_USER_ID` | `""` | User ID (required) |
| `--device-id` | | `XYNCRA_DEVICE_ID` | `""` | Device ID (required) |
| `--server` | `-s` | `XYNCRA_SERVER` | `ws://localhost:8080/ws` | Server URL |
| `--db-path` | | `XYNCRA_DB_PATH` | `~/.xyncra/{u}/{d}/xyncra.db` | DB path |
| `--log-dir` | | `XYNCRA_LOG_DIR` | `~/.xyncra/{u}/{d}/logs/` | Log dir |

Priority: flag > env var > default (D-034). Special: `XYNCRA_DEBUG=1` enables debug logs.

**Both `--user-id` and `--device-id` are required.** The server uses both to route
WebSocket connections to the correct device. Without `--device-id`, the agent
executor cannot discover client-registered functions.

## Directory Structure

```
~/.xyncra/{user_id}/{device_id}/
├── xyncra.db       # SQLite database (WAL mode)
├── xyncra.lock     # Process lock file (fcntl, D-031)
├── xyncra.sock     # Unix Socket IPC (chmod 0600)
└── logs/           # Log directory
```

## Core Concepts

**Daemon Mode**: `listen` starts a long-running process. One instance per (user_id, device_id),
enforced by process lock (D-031). Accepts IPC commands + maintains WebSocket connection.

**IPC Communication**: Unix Socket + JSON-RPC 2.0, newline-delimited (D-030).
IPC first with automatic WebSocket fallback (D-032). Exception: `sync-updates` is IPC-only (D-036).

**Local Database**: Query commands read SQLite directly (offline-capable, D-035):
list-conversations, get-conversation, get-messages, search-messages, draft *, logs *

## Built-in Functions (Auto-registered by daemon, D-115)

The `listen` daemon automatically registers these functions on startup. No separate command is needed.

| Function | Description | Parameters |
|----------|-------------|------------|
| `ping` | Echo test for ReverseRPC channel | `message` (string, optional) |
| `get_device_info` | Device info (hostname, OS, arch, pid) | none |
| `get_time` | Current device time (UTC, unix, timezone) | none |

These functions are registered via `system.register_functions` RPC and can be invoked by the server/agent through ReverseRPC.

### Device Info

The `listen` daemon accepts an optional `--device-info` flag to attach custom device metadata to function registration:

```bash
xyncra-client listen --device-info '{"name":"MacBook","os":"darwin","app_version":"1.2.3"}'
```

This metadata is sent to the server via `system.register_functions` RPC and stored in the FunctionRegistry. Default is empty (no metadata).

## Exit Codes (D-042)

0=success, 1=general error, 2=precondition not met (lock conflict), 3=timeout (kill only)

## Product Decision Index

D-001: Zero-config | D-030: Unix Socket + JSON-RPC 2.0 | D-031: fcntl process lock
D-032: IPC priority, WS fallback | D-033: device-id = hostname SHA256[:8]
D-034: XYNCRA_ env prefix | D-035: Query commands read local SQLite
D-036: sync-updates IPC-only | D-037: --peer-id not --user-id | D-038: string UUID vs uint32
D-039: kill SIGTERM/SIGKILL + cleanup | D-040: logs retain 7d | D-041: tabwriter | D-042: exit codes
D-085: HITL event broadcasting | D-087: AgentTimeoutHandler | D-114: agent-resume IPC-only
D-115: Daemon 内置函数自动注册（消除 register-functions 独立进程）

## Server Protocol (for test writers and direct API clients)

The CLI wraps the server protocol. When writing e2e tests or direct API clients, use these raw interfaces.
Key source: `pkg/protocol/protocol.go` (Package types), `pkg/protocol/errors.go` (response codes), `internal/agent/broadcast.go` (ephemeral payloads).

### 3-Level Package Envelope

All WebSocket messages use a 3-level envelope (`protocol.Package`):

```go
type Package struct {
    Version uint8           `json:"version,omitempty"` // defaults to 1
    Type    PackageType     `json:"type"`              // 0=Request, 1=Response, 2=Updates
    Data    json.RawMessage `json:"data"`
}
```

**Client → Server (Request, type=0):**
```json
{"type":0, "data":{"id":"req-1", "method":"send_message", "params":{...}}}
```

**Server → Client (Response, type=1):**
```json
{"type":1, "data":{"id":"req-1", "code":0, "msg":"ok", "data":{...}}}
```
- `code`: 0=OK, -1=error, -100=validation, -101=not found, -102=duplicate, -200=permission denied, -300=internal error

**Server → Client (Push Updates, type=2):**
```json
{"type":2, "data":{"updates":[{"seq":1, "type":"message", "payload":{...}, "created_at":"..."}]}}
```

### RPC Methods (via Request/Response)

| Method | Params | Response Data |
|--------|--------|---------------|
| `send_message` | `conversation_id`, `content`, `client_message_id` (UUID, idempotency key D-006), `type` | `{message, duplicate}` |
| `create_conversation` | `user_id` (peer), `title?` | `{conversation, duplicate}` |
| `sync_updates` | `after_seq`, `limit?` (default 100, max 500) | `{updates[], has_more, latest_seq}` |
| `heartbeat` | `{}` | `{status:"ok"}` (refreshes conn TTL, D-010) |
| `get_conversation` | `conversation_id` | `{conversation, unread_count}` |
| `list_conversations` | `offset?`, `limit?` | `{conversations[], has_more}` (ordered LastMessageAt DESC) |
| `get_messages` | `conversation_id`, `after_message_id?`, `limit?` | `{messages[], has_more}` (MessageID ASC) |
| `search_messages` | `conversation_id`, `query`, `after_message_id?`, `limit?` | `{messages[], has_more}` (MessageID DESC) |
| `mark_as_read` | `conversation_id`, `message_id?` (MAX semantics, D-012) | `{status, unread_count, last_read_message_id}` |
| `delete_conversation` | `conversation_id` | `{status, deleted_message_count}` |
| `restore_conversation` | `conversation_id` | `{conversation, restored_message_count}` |
| `delete_message` | `message_id` | `{status}` (sender only) |
| `reload_agents` | `{}` | `{count}` (agent system) |
| `agent_resume` | `conversation_id`, `checkpoint_id`, `interrupt_id?`, `answer`, `agent_id` | `{status: "queued"}` (HITL, D-114) |

### Push Update Types

**Persisted (Seq > 0, appear in `sync_updates`):**

| Type | Payload | Description |
|------|---------|-------------|
| `"message"` | `model.Message` | New message |
| `"delete_message"` | Message ID info | Message deleted |
| `"mark_read"` | Read cursor info | Read position updated |
| `"conversation"` | Conversation state | Conversation changed |
| `"gap"` | nil | Synthetic gap filler (runtime only, D-029) |

**Ephemeral (Seq = 0, NEVER in `sync_updates`):**

| Type | Payload | Description |
|------|---------|-------------|
| `"typing"` | `{user_id, conversation_id, is_typing, timestamp}` | Typing indicator |
| `"streaming"` | `{user_id, conversation_id, stream_id, text, is_done, timestamp}` | Cumulative text (NOT delta) |
| `"agent_status"` | `{user_id, conversation_id, status, timestamp}` | Status: thinking/tool_calling/generating/idle/asking_user |
| `"agent_timeout"` | `{user_id, conversation_id, reason, timestamp}` | Agent timed out |

### sync_updates Pagination

- Cursor-based with `after_seq` (fetch updates where seq > after_seq)
- Limit clamped to [1, 500], default 100
- Response: `{updates[], has_more, latest_seq}`
- If `has_more == true`, repeat with `after_seq = latest_seq`
- Missing seq positions filled with synthetic `type:"gap"` updates (D-029)
- Ephemeral updates (Seq=0) are **never** returned

### Getting Agent Replies: Online vs Offline

**Online (WebSocket connected):**
1. Connect: `ws://{addr}/ws?user_id={userID}&device_id={deviceID}`
2. Send `send_message` to agent conversation (agent user ID: `agent/{agentID}`, D-054)
3. Receive ephemeral push updates (type=2): `typing` → `agent_status` → `streaming` (multiple) → `streaming(is_done=true)` → `typing(stop)`
4. Receive persisted `message` push update (type=2, seq > 0) with agent's reply
5. The reply `SenderID` is `"agent/{agentID}"`

**Offline / sync_updates flow:**
1. Send message (via WebSocket, then disconnect)
2. Agent processes and persists reply
3. Reconnect and call `sync_updates` with `after_seq=0` (or last known seq)
4. Response `updates` array contains the agent's reply as `type:"message"`
5. Ephemeral events (streaming, typing) are **lost** — only persisted messages survive

### Test Pattern: Full Agent Reply via WebSocket

```go
// 1. Connect WebSocket
wsURL := fmt.Sprintf("ws://%s/ws?user_id=%s&device_id=%s", addr, userID, deviceID)
conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
defer conn.Close()

// 2. Create conversation with agent (find-or-create)
sendRequest(t, conn, "req-1", "create_conversation", map[string]any{"user_id": "agent/test-bot"})
resp := readResponse(t, conn, 5*time.Second)
// parse resp.Data → conversation.ID

// 3. Send message
sendRequest(t, conn, "req-2", "send_message", map[string]any{
    "conversation_id":   convID,
    "client_message_id": uuid.New().String(),
    "content":           "Hello",
    "type":              "text",
})
readResponse(t, conn, 5*time.Second) // assert code == 0

// 4. Read push updates (type=2), filter for agent reply
for {
    pkg := readPackage(t, conn, 30*time.Second)
    if pkg.Type != 2 { continue }
    var updates protocol.PackageDataUpdates
    json.Unmarshal(pkg.Data, &updates)
    for _, u := range updates.Updates {
        if u.Type == "message" {
            var msg model.Message
            json.Unmarshal(u.Payload, &msg)
            if msg.SenderID == "agent/test-bot" {
                // This is the agent reply
                assert.NotEmpty(t, msg.Content)
                return
            }
        }
    }
}
```

### Test Pattern: Offline Sync (recipient not connected)

```go
// 1. Sender sends message, then disconnects
aliceConn.WriteJSON(sendMsg)
aliceConn.Close()

// 2. Agent processes and persists reply (no Alice WS connected)

// 3. Alice reconnects and syncs
aliceConn2, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
sendRequest(t, aliceConn2, "sync-1", "sync_updates", map[string]any{
    "after_seq": 0,
    "limit":     100,
})
resp := readResponse(t, aliceConn2, 5*time.Second)
// resp.Data contains {updates[], has_more, latest_seq}
// Iterate updates, find type:"message" with SenderID == "agent/test-bot"
```

## Detailed Documentation

- [Getting Started](references/getting-started.md) — Build, first run, configuration
- [Commands](references/commands/)
  - [listen + kill](references/commands/listen.md) — Daemon lifecycle
  - [send](references/commands/send.md) | [conversations](references/commands/conversations.md) (5 cmds)
  - [messages](references/commands/messages.md) (4 cmds) | [sync](references/commands/sync.md) (IPC-only)
  - [agent-resume](references/commands/agent-resume.md) (IPC-only, HITL)
  - [draft](references/commands/draft.md) (3 cmds) | [logs](references/commands/logs.md) (5 cmds)
- [Architecture](references/architecture/)
  - [Overview](references/architecture/overview.md) | [Database](references/architecture/database.md)
  - [IPC Protocol](references/architecture/ipc.md)
- [Scenarios](references/scenarios/)
  - [Basic](references/scenarios/basic-usage.md) | [Multi-Device](references/scenarios/multi-device.md)
  - [Offline Sync](references/scenarios/offline-sync.md) | [Errors](references/scenarios/error-handling.md)
  - [Advanced](references/scenarios/advanced.md)
- [Troubleshooting](references/troubleshooting/)
  - [Common Issues](references/troubleshooting/common-issues.md) | [Debugging](references/troubleshooting/debugging.md)
