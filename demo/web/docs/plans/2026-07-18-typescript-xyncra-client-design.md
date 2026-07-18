# TypeScript Xyncra Client — 设计文档

**日期**: 2026-07-18
**状态**: 已批准
**范围**: 里程碑 1（TypeScript CLI）+ 里程碑 2 预备（全局悬浮 AI 助手基础）

## 概述

用 TypeScript 实现替换现有的 Go 版 `xyncra-client`（`cmd/xyncra-client` + `internal/cli/`），代码组织为 `demo/web/packages/` 下的多个 npm workspace 子包。TypeScript 版本必须完美复刻 Go 版的所有功能，同时建立支持浏览器内嵌的架构，为里程碑 2 的全局悬浮 AI 助手打基础。

## 目标

1. **里程碑 1**：TypeScript CLI 客户端，作为 Go 版的直接替代品——相同的命令、相同的 flag、相同的 IPC 协议、相同的文件路径。运行在 Node.js 终端。
2. **里程碑 2 预备**：架构支持浏览器内嵌——同一份核心 client 代码通过依赖注入同时运行在 Node.js（CLI daemon）和浏览器（AI 助手组件）中。
3. **开发体验**：Claude Code skill（`xyncra-ts-client-usage`）镜像现有 Go 版 skill，辅助 AI 开发。

## 非目标

- 修改服务端或协议——TypeScript 客户端是纯粹的客户 replacement。
- 实现里程碑 2 的 AI 助手——本里程碑仅创建 `xyncra-client-web` 包骨架。
- 数据迁移——Go 和 TS 版本不能同时运行（完全替代）。

## 架构决策：多包 Workspace

**决策**：`demo/web/packages/` 下 4 个包，清晰的依赖链。

**备选方案**：
- 单包分层架构——否决；浏览器构建会引入 Node.js 代码。
- 双包拆分（core + cli）——否决；协议类型应可独立版本管理。

### 包结构

```
demo/web/packages/
├── xyncra-protocol/          # Phase 1: 类型 + 协议常量
├── xyncra-client-core/       # Phase 2: 环境无关的核心 client
├── xyncra-client-cli/        # Phase 3: Node.js CLI 运行时
└── xyncra-client-web/        # Phase 5 (里程碑 2): 浏览器适配器（仅骨架）
```

**依赖链**：`protocol ← core ← cli` / `core ← web`

### 关键技术决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 包管理 | npm workspaces | 与 `demo/web` 现有结构一致 |
| IndexedDB 层 | Dexie.js + fake-indexeddb | 用户指定 Dexie.js；fake-indexeddb 兼容 Node 20+ |
| WebSocket (CLI) | `ws` 库 | Node.js 最成熟的 WebSocket 实现 |
| WebSocket (浏览器) | 原生 WebSocket | 零依赖 |
| CLI 框架 | commander.js | 轻量，对应 Go 的 cobra |
| IPC 协议 | JSON-RPC 2.0 over Unix socket | 与 Go 版本完全兼容 |
| 进程锁 | 文件锁（PID file） | 与 Go 版本路径一致 |
| 环境抽象 | 构造函数注入（IWebSocketFactory, IIndexedDBProvider） | 同一份核心代码，两种运行环境 |
| 运行时 | Node.js 20+ | 用户指定 |

### 数据流

```
CLI 命令 ──→ IPC (Unix socket) ──→ Daemon 进程 ──→ WebSocket ──→ Xyncra Server
                                                        ↕
                                                 Dexie/IndexedDB
                                                        ↕
                                               UpdateHandler（stdout）
```

```
里程碑 2（未来）：
React AI 助手 ──→ XyncraClient（直接导入，无 IPC）──→ WebSocket ──→ Server
```

### 完全替代

- 相同文件路径：`~/.xyncra/{user_id}/{device_id}/`（socket、lock、logs）
- Go 和 TS 版本不能同时运行
- 存储不共享——Go 使用 SQLite，TS 使用 IndexedDB（独立的数据文件）

---

## Phase 拆分

### Phase 1: xyncra-protocol

**职责**：纯 TypeScript 类型定义和协议常量。零运行时依赖。

**范围**：1:1 映射 Go `pkg/protocol/` 的类型和常量。

| 参考文件 | 对应内容 |
|----------|----------|
| `pkg/protocol/protocol.go` | Package、PackageType、PackageDataRequest/Response/Updates |
| `pkg/protocol/function.go` | FunctionInfo、ReturnInfo |
| `pkg/protocol/errors.go` | ResponseCode 常量、协议错误类型 |

**验证**：编译通过，类型导出正确。

---

### Phase 2: xyncra-client-core

**职责**：环境无关的核心 client 逻辑。所有环境差异（WebSocket、IndexedDB、日志）通过构造函数注入。

**范围**：4 个子 Phase：

#### Phase 2a: 数据库层

Dexie 数据库定义（9 个 table）、数据 model、sub-store CRUD 方法。

| 参考文件 | 对应内容 |
|----------|----------|
| `pkg/store/clientdb.go` | ClientDB 结构体、9 个 sub-store、AutoMigrate、SQLite PRAGMAs |
| `pkg/store/model/` 目录 | 所有数据 model：Conversation、Message、Question、SyncState、Draft、RetryTask、RPCLog、NotificationLog、UserUpdate |
| `pkg/store/conversation_store.go` | ConversationStore CRUD |
| `pkg/store/message_store.go` | MessageStore CRUD |
| `pkg/store/question_store.go` | QuestionStore CRUD |
| `pkg/store/sync_state_store.go` | SyncStateStore key-value 操作 |
| `pkg/store/draft_store.go` | DraftStore CRUD |
| `pkg/store/queue_store.go` | QueueStore (RetryTask) CRUD |
| `pkg/store/rpc_log_store.go` | RPCLogStore CRUD |
| `pkg/store/notification_log_store.go` | NotificationLogStore CRUD |
| `pkg/store/user_update_store.go` | UserUpdateStore CRUD |

#### Phase 2b: 连接与协议

WebSocket 连接管理、序列化/反序列化、心跳、幂等性、RTT 追踪。

| 参考文件 | 对应内容 |
|----------|----------|
| `pkg/client/connection.go` | connectionManager：连接/重连/退避/readPump/writePump/4001 替换检测 |
| `pkg/client/options.go` | clientOptions：所有 WithXxx 选项函数、默认值常量 |
| `pkg/client/idempotency_cache.go` | IdempotencyCache：LRU 缓存 |
| `pkg/client/rtt_tracker.go` | RTTTracker：SRTT 计算、自适应超时 |
| `pkg/client/response_retry_queue.go` | ResponseRetryQueue：响应重试队列 |

#### Phase 2c: 同步与 RPC

SyncManager、RetryManager、RPC call/response 关联、反向 RPC（server → client function 调用）。

| 参考文件 | 对应内容 |
|----------|----------|
| `pkg/client/sync.go` | syncManager：FullSync、ApplyUpdates、debouncedPull、gap 处理 |
| `pkg/client/retry.go` | retryManager：失败消息重试循环 |
| `pkg/client/client.go` | XyncraClient：Call、dispatch、registerRequestHandler、handleIncomingRequest |
| `pkg/client/agent.go` | Agent 相关 client 逻辑 |
| `pkg/client/doc.go` | 包级文档、设计概览 |

#### Phase 2d: XyncraClient 主类

组装所有子模块、公开 API、环境抽象接口（IWebSocketFactory、IIndexedDBProvider、IUpdateHandler、ILogger）。

| 参考文件 | 对应内容 |
|----------|----------|
| `pkg/client/client.go` | XyncraClient：New、Start、Stop、SendMessage、CreateConversation、FullSync 等 |

**验证**：Jest 单元测试 + fake-indexeddb 集成测试。

---

### Phase 3: xyncra-client-cli

**职责**：Node.js 运行时——daemon 进程、IPC、CLI 命令、文件锁、内置 function。

**范围**：4 个子 Phase：

#### Phase 3a: 基础设施

路径解析、文件锁、IPC server/client、Node.js 运行时注入（ws、fake-indexeddb、logger）。

| 参考文件 | 对应内容 |
|----------|----------|
| `internal/cli/paths.go` | 路径解析：~/.xyncra/{user_id}/{device_id}/、SocketPath、LockPath、DBPathDefault、LogDirDefault |
| `internal/cli/lock.go` | 文件锁：acquireLock、readLockInfo、isProcessAlive、cleanupDaemonFiles |
| `internal/cli/ipc.go` | IPCServer/IPCClient：JSON-RPC 2.0、Unix socket、dispatch |

#### Phase 3b: Daemon（listen 命令）

进程生命周期、UpdateHandler、IPC handler 注册、内置 function handlers、自动日志清理。

| 参考文件 | 对应内容 |
|----------|----------|
| `internal/cli/listen.go` | runListen daemon 生命周期、cliUpdateHandler、registerIPCHandlers、startLogCleanup、parseDeviceInfo |
| `internal/cli/builtin_functions.go` | 内置 function 元数据（ping/get_device_info/get_time）+ handler 注册 |

#### Phase 3c: CLI 命令（~20 个子命令）

使用 commander.js 复刻 Go 的 cobra 命令结构。

| 参考文件 | 对应内容 |
|----------|----------|
| `internal/cli/app.go` | CLIContext、NewRootCommand、resolveStringFlag、所有子命令注册 |
| `internal/cli/send.go` | send 命令 |
| `internal/cli/conversations.go` | create/delete/restore/list/get conversation 命令 |
| `internal/cli/messages.go` | get-messages、search-messages、delete-message、mark-as-read 命令 |
| `internal/cli/sync.go` | sync-updates 命令 |
| `internal/cli/set_typing.go` | set-typing 命令 |
| `internal/cli/stream_text.go` | stream-text 命令 |
| `internal/cli/agent_resume.go` | agent-resume 命令 |
| `internal/cli/reload_agents.go` | reload-agents 命令 |
| `internal/cli/draft.go` | draft save/get/delete 命令 |
| `internal/cli/logs.go` | logs tail/search/stats/export/cleanup 命令 |
| `internal/cli/kill.go` | kill 命令：PID 读取、SIGTERM/SIGKILL、超时处理 |
| `internal/cli/rpc_helper.go` | RPC 调用辅助工具 |

#### Phase 3d: 输出格式化

Console 和 CSV 输出格式化器。

| 参考文件 | 对应内容 |
|----------|----------|
| `internal/cli/output/console.go` | Console 输出格式化（tabwriter） |
| `internal/cli/output/csv.go` | CSV 输出格式化 |

**验证**：Jest 单元测试 + E2E 测试（启动真实 server，CLI 对打）。

---

### Phase 4: Claude Code Skill — xyncra-ts-client-usage

**职责**：为 TypeScript CLI 创建 Claude Code skill，镜像现有 Go 版 skill 的结构。

**范围**：

| 参考文件 | 对应内容 |
|----------|----------|
| `../../.claude/skills/xyncra-client-usage/SKILL.md` | Go 版 skill 主文件：决策树、命令表、flag 表、协议文档、测试模式（**结构模板**） |
| `../../.claude/skills/xyncra-client-usage/references/architecture/overview.md` | Go 版架构概览 |
| `../../.claude/skills/xyncra-client-usage/references/architecture/database.md` | Go 版数据库文档 |
| `../../.claude/skills/xyncra-client-usage/references/architecture/ipc.md` | Go 版 IPC 协议文档 |
| `../../.claude/skills/xyncra-client-usage/references/commands/*.md` | Go 版各命令使用文档（listen、send、conversations、messages、sync、draft、logs、agent-resume） |
| `../../.claude/skills/xyncra-client-usage/references/getting-started.md` | Go 版入门指南 |
| `../../.claude/skills/xyncra-client-usage/references/scenarios/*.md` | Go 版场景文档（basic-usage、multi-device、offline-sync、error-handling、advanced） |
| `../../.claude/skills/xyncra-client-usage/references/troubleshooting/*.md` | Go 版故障排查文档 |
| `cmd/xyncra-server/main.go` | Server 启动流程（理解整体架构的上下文） |

**Skill 文件结构**：

```
.claude/skills/xyncra-ts-client-usage/
├── SKILL.md                    # 主入口：决策树 + 命令表 + 协议概要
└── references/
      ├── getting-started.md    # npm 构建/安装/首次运行
      ├── commands/             # 每个命令的使用文档
      ├── architecture/         # TS 版架构（包结构、依赖注入、Dexie 替代 SQLite）
      ├── scenarios/            # 常见场景（基本用法、多设备、离线同步、错误处理）
      └── troubleshooting/      # 常见问题、调试技巧
```

**适配要点**（Go → TS）：
- 命令名和 flag：完全一致（完全替代）
- 目录结构：不变（共享 `~/.xyncra/` 路径）
- 存储描述：SQLite → IndexedDB (Dexie.js)
- 构建/安装：改为 npm 流程
- 环境要求：Node.js 20+
- 新增 Go → TS 迁移差异说明

---

### Phase 5: xyncra-client-web（里程碑 2，暂不实现）

**职责**：浏览器适配器 + React hooks + AI 助手集成。

**状态**：仅创建包骨架（`package.json` + placeholder），实现推迟到里程碑 2。

| 参考文件 | 对应内容 |
|----------|----------|
| `demo/web/src/app.tsx` | React 应用入口，AI 助手挂载点 |
| `demo/web/src/components/` | 现有组件模式 |
| `demo/web/CLAUDE.md` | 前端架构约定（Umi Max、antd、tailwind） |

---

## 测试策略

| 层级 | 方式 | Go 参考 |
|------|------|---------|
| protocol | 类型编译验证 | `pkg/protocol/*_test.go` |
| core | Jest 单元测试 + fake-indexeddb | `pkg/client/*_test.go` |
| cli | Jest 单元测试 + 进程级 E2E | `internal/cli/*_test.go`、`internal/cli/e2e/` |
| 集成 | 启动真实 server，CLI 对打 | `internal/cli/e2e/cli_e2e_test.go` |
