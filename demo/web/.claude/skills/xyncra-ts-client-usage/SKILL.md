# Xyncra TypeScript Client Usage Skill

Use this skill when working with the `@xyncra/client-cli` TypeScript CLI tool.

## 前置条件

1. **启动 Redis**：`redis-server`（默认 localhost:6379）
2. **启动服务器**：`./xyncra-server`（默认 :8080）
3. **启动客户端 daemon**：`xyncra-client listen --user-id <user> --device-id <device>`

## Decision Tree (read first)

1. Start the daemon? → `xyncra-client listen --user-id <user> --device-id <device>`
2. Send / modify data? → send, create-conversation, delete-conversation,
   restore-conversation, delete-message, mark-as-read (IPC+WS fallback)
3. Query local data? → list-conversations / get-conversation / get-messages /
   search-messages (local Dexie.js, offline OK)
4. Manual sync? → sync-updates (requires daemon, IPC-only)
5. Drafts / logs? → draft save/get/delete, logs tail/search/stats/export/cleanup (local Dexie.js)
6. Send typing indicator? → set-typing (IPC-only, fire-and-forget D-050)
7. Send streaming text? → stream-text (IPC-only, fire-and-forget D-051)
8. Stop daemon? → `xyncra-client kill [--force]`
9. Resume HITL-interrupted agent? → `xyncra-client agent-resume` (IPC-only, D-114)
10. Reload agent config? → `xyncra-client reload-agents` (IPC-only, D-076, D-036)
11. Writing tests or direct API client? → **Server Protocol** section below (WebSocket JSON-RPC, sync_updates HTTP, Agent reply flow)
12. More details? → Check references/ links below

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
| `list-conversations` | IPC-only | List conversations from Dexie.js |
| `get-conversation` | IPC-only | Show conversation details |
| `get-messages` | IPC-only | List messages from Dexie.js |
| `search-messages` | IPC-only | Search messages in Dexie.js |
| `sync-updates` | IPC-only | Trigger full sync via daemon (no fallback) |
| `set-typing` | IPC-only | Send typing indicator (fire-and-forget, D-050) |
| `stream-text` | IPC-only | Send streaming text (fire-and-forget, D-051) |
| `agent-resume` | IPC-only | Resume HITL-interrupted agent (D-036, D-114) |
| `reload-agents` | IPC-only | Hot-reload Agent config (D-076, D-036) |
| `draft save/get/delete` | IPC-only | Manage message drafts |
| `logs tail/search/stats/export/cleanup` | IPC-only | View and manage logs |
| `kill` | OS process | Terminate running daemon |

## Global Flags

| Flag | Short | Env Variable | Default | Description |
|------|-------|--------------|---------|-------------|
| `--user-id` | `-u` | `XYNCRA_USER_ID` | `""` | User ID (required) |
| `--device-id` | | `XYNCRA_DEVICE_ID` | `""` | Device ID (default: SHA256(hostname)[:8]) |
| `--server` | `-s` | `XYNCRA_SERVER` | `ws://localhost:8080/ws` | Server URL |
| `--db-path` | | `XYNCRA_DB_PATH` | `$USER_DIR/xyncra.db` | IndexedDB database name (TS-D-012) |
| `--log-dir` | | `XYNCRA_LOG_DIR` | `$USER_DIR/logs/` | Log dir |

Priority: flag > env var > default (D-034). Special: `XYNCRA_DEBUG=1` enables debug logs.

**Only `--user-id` is required.** `--device-id` defaults to SHA256(hostname)[:8] if omitted (D-033).

**`--db-path` semantic change (TS-D-012)**: In the Go client, `--db-path` is a SQLite file path.
In the TS client, it is redefined as an IndexedDB database name. Although the string looks like
a file path (e.g., `~/.xyncra/user123/abc12345/xyncra.db`), it is actually used as the Dexie
IndexedDB database name. In Node.js, `fake-indexeddb` provides the IndexedDB implementation.

## Directory Structure

```
~/.xyncra/{user_id}/{device_id}/
├── xyncra.lock     # Process lock file (fs-ext, D-031)
├── xyncra.sock     # Unix Socket IPC (chmod 0600)
└── logs/           # Log directory
```

> **Note**: Unlike the Go client, there is no `xyncra.db` file on disk. The TS client uses
> IndexedDB (via `fake-indexeddb` in Node.js) for local storage (TS-D-012).

## Core Concepts

**Daemon Mode**: `listen` starts a long-running process. One instance per (user_id, device_id),
enforced by process lock (D-031). Accepts IPC commands + maintains WebSocket connection.

**IPC Communication**: Unix Socket + JSON-RPC 2.0, newline-delimited (D-030).
IPC first with automatic WebSocket fallback (D-032). Exception: `sync-updates` is IPC-only (D-036).

**Local Database**: The TS client uses Dexie.js (IndexedDB wrapper) for local storage.
In Node.js, `fake-indexeddb` provides the IndexedDB polyfill. Data is persisted in memory
and synced with the server.

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

D-001: Zero-config | D-006: client_message_id 幂等 (UUID v4) | D-008: message_id 会话内单调递增序号
D-009: sync_updates 分页拉取 | D-011: find-or-create 幂等会话 | D-013: 级联软删除会话+消息
D-014: 仅发送者可删除消息 | D-015: 级联恢复会话+消息 | D-016: UserUpdate 保留 30 天
D-017: 服务端错误码 (-100/-200/-300) | D-018: WebSocket + Redis Pub/Sub 多节点路由
D-027: ClientError 扩展错误码 (-400/-401/-402) | D-028: 统一 seq 空间（所有类型共享）
D-029: gap 类型补空（运行时不持久化） | D-030: Unix Socket + JSON-RPC 2.0
D-031: Process lock (fs-ext) | D-032: IPC priority, WS fallback
D-033: device-id = hostname SHA256[:8] | D-034: XYNCRA_ env prefix
D-035: Query commands read local DB | D-036: sync-updates IPC-only
D-037: --peer-id not --user-id | D-038: string UUID vs uint32 message_id
D-039: kill SIGTERM/SIGKILL + cleanup | D-040: logs retain 7d | D-041: tabwriter | D-042: exit codes
D-050: Ephemeral typing (Seq=0) | D-051: Ephemeral streaming (Seq=0)
D-054: Agent user_id = agent/{agentID}
D-076: reload-agents 热加载 Agent 配置 | D-085: HITL event broadcasting | D-087: AgentTimeoutHandler
D-114: agent-resume IPC-only | D-115: Daemon 内置函数自动注册（消除 register-functions 独立进程）
D-116: questions 表（HITL 问题记录） | D-117: conversations.agent_status 字段
D-125: HITL checkpoint_id/interrupt_id 格式
TS-D-002: 环境无关（依赖注入接口）| TS-D-012: --db-path 语义变更为 IndexedDB 数据库名

## Server Protocol (for test writers and direct API clients)

The CLI wraps the server protocol. When writing e2e tests or direct API clients, use these raw interfaces.
Key source: `packages/xyncra-protocol/src/index.ts` (Protocol types), Go `pkg/protocol/protocol.go` (reference), `internal/agent/broadcast.go` (ephemeral payloads).

### 3-Level Package Envelope

All WebSocket messages use a 3-level envelope (`Package`):

```typescript
interface Package {
  version?: number; // defaults to 1
  type: PackageType; // 0=Request, 1=Response, 2=Updates
  data: any;
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
| `"message"` | `Message` | New message |
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

```typescript
import WebSocket from 'ws';
import { v4 as uuidv4 } from 'uuid';

// 1. Connect WebSocket
const wsURL = `ws://${addr}/ws?user_id=${userID}&device_id=${deviceID}`;
const ws = new WebSocket(wsURL);

// 2. Create conversation with agent (find-or-create)
ws.send(JSON.stringify({
  type: 0,
  data: { id: 'req-1', method: 'create_conversation', params: { user_id: 'agent/test-bot' } }
}));
const createResp = await readResponse(ws, 5000);
const convID = createResp.data.data.conversation.id;

// 3. Send message
ws.send(JSON.stringify({
  type: 0,
  data: {
    id: 'req-2', method: 'send_message',
    params: { conversation_id: convID, client_message_id: uuidv4(), content: 'Hello', type: 'text' }
  }
}));
const sendResp = await readResponse(ws, 5000); // assert code == 0

// 4. Read push updates (type=2), filter for agent reply
for await (const raw of ws) {
  const pkg = JSON.parse(raw.toString());
  if (pkg.type !== 2) continue;
  for (const u of pkg.data.updates) {
    if (u.type === 'message') {
      const msg = u.payload;
      if (msg.sender_id === 'agent/test-bot') {
        // This is the agent reply
        console.assert(msg.content.length > 0);
        ws.close();
        process.exit(0);
      }
    }
  }
}
```

### Test Pattern: Offline Sync (recipient not connected)

```typescript
// 1. Sender sends message, then disconnects
const ws1 = new WebSocket(wsURL);
ws1.send(JSON.stringify(sendMsg));
ws1.close();

// 2. Agent processes and persists reply (no Alice WS connected)

// 3. Alice reconnects and syncs
const ws2 = new WebSocket(wsURL);
ws2.send(JSON.stringify({
  type: 0,
  data: { id: 'sync-1', method: 'sync_updates', params: { after_seq: 0, limit: 100 } }
}));
const resp = await readResponse(ws2, 5000);
// resp.data contains {updates[], has_more, latest_seq}
// Iterate updates, find type:"message" with sender_id == "agent/test-bot"
```

## Detailed Documentation

- [Getting Started](references/getting-started.md) -- Build, first run, configuration
- [Architecture](references/architecture/)
  - [Overview](references/architecture/overview.md) | [Database](references/architecture/database.md)
  - [IPC Protocol](references/architecture/ipc.md)
- [Commands](references/commands/)
  - [send](references/commands/send.md) | [conversations](references/commands/conversations.md)
  - [listen](references/commands/listen.md) | [sync](references/commands/sync.md)
  - [set-typing](references/commands/set-typing.md) | [stream-text](references/commands/stream-text.md)
  - [draft](references/commands/draft.md) | [messages](references/commands/messages.md)
  - [agent-resume](references/commands/agent-resume.md) | [reload-agents](references/commands/reload-agents.md)
  - [logs](references/commands/logs.md)
- [Scenarios](references/scenarios/)
  - [Basic Usage](references/scenarios/basic-usage.md) | [Advanced](references/scenarios/advanced.md)
  - [Error Handling](references/scenarios/error-handling.md) | [Multi-Device](references/scenarios/multi-device.md)
  - [Offline Sync](references/scenarios/offline-sync.md)
- [Troubleshooting](references/troubleshooting/)
  - [Common Issues](references/troubleshooting/common-issues.md) | [Debugging](references/troubleshooting/debugging.md)
- [Migration](references/migration/)
  - [From Go Client](references/migration/from-go.md)
