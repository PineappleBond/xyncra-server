# TypeScript Xyncra Client — Design Document

**Date**: 2026-07-18
**Status**: Approved
**Scope**: Milestone 1 (TypeScript CLI) + Milestone 2 prep (global floating AI assistant foundation)

## Overview

Replace the Go-based `xyncra-client` (`cmd/xyncra-client` + `internal/cli/`) with a TypeScript implementation organized as multiple npm workspace packages under `demo/web/packages/`. The TypeScript client must perfectly replicate all Go client functionality while establishing an architecture that supports browser embedding for the Milestone 2 global floating AI assistant.

## Goals

1. **Milestone 1**: TypeScript CLI client that is a drop-in replacement for the Go client — same commands, same flags, same IPC protocol, same file paths. Runs in Node.js terminal.
2. **Milestone 2 prep**: Architecture supports browser embedding — the same core client code runs in both Node.js (CLI daemon) and browser (AI assistant component) via dependency injection.
3. **Developer experience**: Claude Code skill (`xyncra-ts-client-usage`) mirrors the existing Go skill for AI-assisted development.

## Non-Goals

- Modifying the server or protocol — the TypeScript client is a pure client replacement.
- Implementing Milestone 2 AI assistant — only the package skeleton (`xyncra-client-web`) is created in this milestone.
- Data migration — Go and TS clients cannot run simultaneously (complete replacement).

## Architecture Decision: Multi-Package Workspace

**Decision**: 4 packages under `demo/web/packages/` with a clear dependency chain.

**Alternatives considered**:
- Single layered package — rejected; browser builds would pull in Node.js code.
- Two-package split (core + cli) — rejected; protocol types should be independently versioned.

### Package Structure

```
demo/web/packages/
├── xyncra-protocol/          # Phase 1: Types + protocol constants
├── xyncra-client-core/       # Phase 2: Environment-agnostic core client
├── xyncra-client-cli/        # Phase 3: Node.js CLI runtime
└── xyncra-client-web/        # Phase 5 (Milestone 2): Browser adapter (skeleton only)
```

**Dependency chain**: `protocol ← core ← cli` / `core ← web`

### Key Technical Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Package manager | npm workspaces | Consistent with existing `demo/web` setup |
| IndexedDB layer | Dexie.js + fake-indexeddb | User specified Dexie.js; fake-indexeddb compatible with Node 20+ |
| WebSocket (CLI) | `ws` library | Most mature Node.js WebSocket implementation |
| WebSocket (browser) | Native WebSocket | Zero dependency |
| CLI framework | commander.js | Lightweight, maps to Go's cobra |
| IPC protocol | JSON-RPC 2.0 over Unix socket | Full compatibility with Go version |
| Process lock | File lock (PID file) | Same path as Go version |
| Environment abstraction | Constructor injection (IWebSocketFactory, IIndexedDBProvider) | Same core code, two runtime environments |
| Runtime | Node.js 20+ | User specified |

### Data Flow

```
CLI 命令 ──→ IPC (Unix socket) ──→ Daemon 进程 ──→ WebSocket ──→ Xyncra Server
                                                        ↕
                                                 Dexie/IndexedDB
                                                        ↕
                                               UpdateHandler（stdout）
```

```
Milestone 2 (future):
React AI 助手 ──→ XyncraClient（直接导入，无 IPC）──→ WebSocket ──→ Server
```

### Complete Replacement

- Same file paths: `~/.xyncra/{user_id}/{device_id}/` (socket, lock, logs)
- Go and TS versions cannot run simultaneously
- Storage is not shared — Go uses SQLite, TS uses IndexedDB (separate data files)

---

## Phase Breakdown

### Phase 1: xyncra-protocol

**Purpose**: Pure TypeScript type definitions and protocol constants. Zero runtime dependencies.

**Scope**: 1:1 mapping of Go `pkg/protocol/` types and constants.

| Reference File | Content |
|----------------|---------|
| `pkg/protocol/protocol.go` | Package, PackageType, PackageDataRequest/Response/Updates |
| `pkg/protocol/function.go` | FunctionInfo, ReturnInfo |
| `pkg/protocol/errors.go` | ResponseCode constants, protocol error types |

**Verification**: Compiles cleanly, types exported correctly.

---

### Phase 2: xyncra-client-core

**Purpose**: Environment-agnostic core client logic. All environment differences (WebSocket, IndexedDB, logging) injected via constructor.

**Scope**: 4 sub-phases:

#### Phase 2a: Database Layer

Dexie database definition with 9 tables, data models, and sub-store CRUD methods.

| Reference File | Content |
|----------------|---------|
| `pkg/store/clientdb.go` | ClientDB structure, 9 sub-stores, AutoMigrate, SQLite PRAGMAs |
| `pkg/store/model/` directory | All data models: Conversation, Message, Question, SyncState, Draft, RetryTask, RPCLog, NotificationLog, UserUpdate |
| `pkg/store/conversation_store.go` | ConversationStore CRUD |
| `pkg/store/message_store.go` | MessageStore CRUD |
| `pkg/store/question_store.go` | QuestionStore CRUD |
| `pkg/store/sync_state_store.go` | SyncStateStore key-value operations |
| `pkg/store/draft_store.go` | DraftStore CRUD |
| `pkg/store/queue_store.go` | QueueStore (RetryTask) CRUD |
| `pkg/store/rpc_log_store.go` | RPCLogStore CRUD |
| `pkg/store/notification_log_store.go` | NotificationLogStore CRUD |
| `pkg/store/user_update_store.go` | UserUpdateStore CRUD |

#### Phase 2b: Connection & Protocol

WebSocket connection management, serialization, heartbeat, idempotency, RTT tracking.

| Reference File | Content |
|----------------|---------|
| `pkg/client/connection.go` | connectionManager: connect/reconnect/backoff/readPump/writePump/4001 replacement |
| `pkg/client/options.go` | clientOptions: all WithXxx option functions, default constants |
| `pkg/client/idempotency_cache.go` | IdempotencyCache: LRU cache |
| `pkg/client/rtt_tracker.go` | RTTTracker: SRTT calculation, adaptive timeout |
| `pkg/client/response_retry_queue.go` | ResponseRetryQueue |

#### Phase 2c: Sync & RPC

SyncManager, RetryManager, RPC call/response correlation, reverse RPC (server → client function calls).

| Reference File | Content |
|----------------|---------|
| `pkg/client/sync.go` | syncManager: FullSync, ApplyUpdates, debouncedPull, gap handling |
| `pkg/client/retry.go` | retryManager: failed message retry loop |
| `pkg/client/client.go` | XyncraClient: Call, dispatch, registerRequestHandler, handleIncomingRequest |
| `pkg/client/agent.go` | Agent-related client logic |
| `pkg/client/doc.go` | Package-level documentation, design overview |

#### Phase 2d: XyncraClient Main Class

Assemble all sub-modules, public API, environment abstraction interfaces (IWebSocketFactory, IIndexedDBProvider, IUpdateHandler, ILogger).

| Reference File | Content |
|----------------|---------|
| `pkg/client/client.go` | XyncraClient: New, Start, Stop, SendMessage, CreateConversation, FullSync, etc. |

**Verification**: Jest unit tests + fake-indexeddb integration tests.

---

### Phase 3: xyncra-client-cli

**Purpose**: Node.js runtime — daemon process, IPC, CLI commands, file locks, built-in functions.

**Scope**: 4 sub-phases:

#### Phase 3a: Infrastructure

Path resolution, file lock, IPC server/client, Node.js runtime injection (ws, fake-indexeddb, logger).

| Reference File | Content |
|----------------|---------|
| `internal/cli/paths.go` | Path resolution: ~/.xyncra/{user_id}/{device_id}/, SocketPath, LockPath, DBPathDefault, LogDirDefault |
| `internal/cli/lock.go` | File lock: acquireLock, readLockInfo, isProcessAlive, cleanupDaemonFiles |
| `internal/cli/ipc.go` | IPCServer/IPCClient: JSON-RPC 2.0, Unix socket, dispatch |

#### Phase 3b: Daemon (listen command)

Process lifecycle, UpdateHandler, IPC handler registration, built-in function handlers, auto log cleanup.

| Reference File | Content |
|----------------|---------|
| `internal/cli/listen.go` | runListen daemon lifecycle, cliUpdateHandler, registerIPCHandlers, startLogCleanup, parseDeviceInfo |
| `internal/cli/builtin_functions.go` | Built-in function metadata (ping/get_device_info/get_time) + handler registration |

#### Phase 3c: CLI Commands (~20 subcommands)

All CLI subcommands replicating Go's cobra command structure using commander.js.

| Reference File | Content |
|----------------|---------|
| `internal/cli/app.go` | CLIContext, NewRootCommand, resolveStringFlag, all subcommand registration |
| `internal/cli/send.go` | send command |
| `internal/cli/conversations.go` | create/delete/restore/list/get conversation commands |
| `internal/cli/messages.go` | get-messages, search-messages, delete-message, mark-as-read commands |
| `internal/cli/sync.go` | sync-updates command |
| `internal/cli/set_typing.go` | set-typing command |
| `internal/cli/stream_text.go` | stream-text command |
| `internal/cli/agent_resume.go` | agent-resume command |
| `internal/cli/reload_agents.go` | reload-agents command |
| `internal/cli/draft.go` | draft save/get/delete commands |
| `internal/cli/logs.go` | logs tail/search/stats/export/cleanup commands |
| `internal/cli/kill.go` | kill command: PID read, SIGTERM/SIGKILL, timeout handling |
| `internal/cli/rpc_helper.go` | RPC call helper utilities |

#### Phase 3d: Output Formatting

Console and CSV output formatters.

| Reference File | Content |
|----------------|---------|
| `internal/cli/output/console.go` | Console output formatting (tabwriter) |
| `internal/cli/output/csv.go` | CSV output formatting |

**Verification**: Jest unit tests + E2E tests (start real server, CLI round-trip).

---

### Phase 4: Claude Code Skill — xyncra-ts-client-usage

**Purpose**: Create a Claude Code skill for the TypeScript CLI, mirroring the existing Go client skill structure.

**Scope**:

| Reference File | Content |
|----------------|---------|
| `../../.claude/skills/xyncra-client-usage/SKILL.md` | Go skill main file: decision tree, command table, flag table, protocol docs, test patterns (**structure template**) |
| `../../.claude/skills/xyncra-client-usage/references/architecture/overview.md` | Go architecture overview |
| `../../.claude/skills/xyncra-client-usage/references/architecture/database.md` | Go database documentation |
| `../../.claude/skills/xyncra-client-usage/references/architecture/ipc.md` | Go IPC protocol documentation |
| `../../.claude/skills/xyncra-client-usage/references/commands/*.md` | Go per-command usage docs (listen, send, conversations, messages, sync, draft, logs, agent-resume) |
| `../../.claude/skills/xyncra-client-usage/references/getting-started.md` | Go getting-started guide |
| `../../.claude/skills/xyncra-client-usage/references/scenarios/*.md` | Go scenario docs (basic-usage, multi-device, offline-sync, error-handling, advanced) |
| `../../.claude/skills/xyncra-client-usage/references/troubleshooting/*.md` | Go troubleshooting docs |
| `cmd/xyncra-server/main.go` | Server startup flow (context for overall architecture) |

**Skill file structure**:

```
.claude/skills/xyncra-ts-client-usage/
├── SKILL.md                    # Main entry: decision tree + command table + protocol summary
└── references/
      ├── getting-started.md    # npm build/install/first run
      ├── commands/             # Per-command usage documentation
      ├── architecture/         # TS architecture (package structure, DI, Dexie replacing SQLite)
      ├── scenarios/            # Common scenarios (basic, multi-device, offline sync, errors)
      └── troubleshooting/      # Common issues, debugging tips
```

**Adaptation notes** (Go → TS):
- Command names and flags: identical (complete replacement)
- Directory structure: unchanged (shared `~/.xyncra/` paths)
- Storage description: SQLite → IndexedDB (Dexie.js)
- Build/install: npm workflow
- Environment requirement: Node.js 20+
- Add Go → TS migration difference notes

---

### Phase 5: xyncra-client-web (Milestone 2, not implemented now)

**Purpose**: Browser adapter + React hooks + AI assistant integration.

**Status**: Create package skeleton (`package.json` + placeholder) only. Implementation deferred to Milestone 2.

| Reference File | Content |
|----------------|---------|
| `demo/web/src/app.tsx` | React app entry, AI assistant mount point |
| `demo/web/src/components/` | Existing component patterns |
| `demo/web/CLAUDE.md` | Frontend architecture conventions (Umi Max, antd, tailwind) |

---

## Testing Strategy

| Layer | Method | Go Reference |
|-------|--------|-------------|
| protocol | Type compilation verification | `pkg/protocol/*_test.go` |
| core | Jest unit tests + fake-indexeddb | `pkg/client/*_test.go` |
| cli | Jest unit tests + process-level E2E | `internal/cli/*_test.go`, `internal/cli/e2e/` |
| integration | Start real server, CLI round-trip | `internal/cli/e2e/cli_e2e_test.go` |
