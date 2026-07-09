# Xyncra Client Usage Skill

Use this skill when working with the xyncra-client CLI tool.

## 前置条件

1. **启动 Redis**：`redis-server`（默认 localhost:6379）
2. **启动服务器**：`./xyncra-server`（默认 :8080）
3. **启动客户端 daemon**：`./xyncra-client listen --user-id <user>`

## Decision Tree (read first)

1. Start the daemon? → `xyncra-client listen --user-id <user>`
2. Send / modify data? → send, create-conversation, delete-conversation,
   restore-conversation, delete-message, mark-as-read (IPC+WS fallback)
3. Query local data? → list-conversations / get-conversation / get-messages /
   search-messages (local SQLite, offline OK)
4. Manual sync? → sync-updates (requires daemon, IPC-only)
5. Drafts / logs? → draft save/get/delete, logs tail/search/stats/export/cleanup (local SQLite)
6. Stop daemon? → `xyncra-client kill [--force]`
7. More details? → Check references/ links below

## Command Table

| Command | Mode | Description |
|---------|------|-------------|
| `listen` | Daemon | Start long-running daemon (IPC + WebSocket) |
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
| `draft save/get/delete` | Local DB | Manage message drafts |
| `logs tail/search/stats/export/cleanup` | Local DB | View and manage logs |
| `kill` | OS process | Terminate running daemon |

## Global Flags

| Flag | Short | Env Variable | Default | Description |
|------|-------|--------------|---------|-------------|
| `--user-id` | `-u` | `XYNCRA_USER_ID` | `""` | User ID (required) |
| `--device-id` | | `XYNCRA_DEVICE_ID` | hostname SHA256[:8] | Device ID |
| `--server` | `-s` | `XYNCRA_SERVER` | `ws://localhost:8080/ws` | Server URL |
| `--db-path` | | `XYNCRA_DB_PATH` | `~/.xyncra/{u}/{d}/xyncra.db` | DB path |
| `--log-dir` | | `XYNCRA_LOG_DIR` | `~/.xyncra/{u}/{d}/logs/` | Log dir |

Priority: flag > env var > default (D-034). Special: `XYNCRA_DEBUG=1` enables debug logs.

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

## Exit Codes (D-042)

0=success, 1=general error, 2=precondition not met (lock conflict), 3=timeout (kill only)

## Product Decision Index

D-001: Zero-config | D-030: Unix Socket + JSON-RPC 2.0 | D-031: fcntl process lock
D-032: IPC priority, WS fallback | D-033: device-id = hostname SHA256[:8]
D-034: XYNCRA_ env prefix | D-035: Query commands read local SQLite
D-036: sync-updates IPC-only | D-037: --peer-id not --user-id | D-038: string UUID vs uint32
D-039: kill SIGTERM/SIGKILL + cleanup | D-040: logs retain 7d | D-041: tabwriter | D-042: exit codes

## Detailed Documentation

- [Getting Started](references/getting-started.md) — Build, first run, configuration
- [Commands](references/commands/)
  - [listen + kill](references/commands/listen.md) — Daemon lifecycle
  - [send](references/commands/send.md) | [conversations](references/commands/conversations.md) (5 cmds)
  - [messages](references/commands/messages.md) (4 cmds) | [sync](references/commands/sync.md) (IPC-only)
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
