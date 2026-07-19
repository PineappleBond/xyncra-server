# Xyncra TypeScript Client 项目上下文

> 此文件由 `xyncra-task-planner` SKILL 使用。包含项目架构、已实现组件、关键接口签名和产品决策摘要。
> 分析任务时按需参考，不需要全部读入。

---

## 1. 架构概览

```text
demo/web/packages/
├── xyncra-protocol/          # Phase 1: 类型 + 协议常量（零依赖）
├── xyncra-client-core/       # Phase 2: 环境无关的核心 client
├── xyncra-client-cli/        # Phase 3: Node.js CLI 运行时
└── xyncra-client-web/        # Phase 5: 浏览器适配器（里程碑 2）
```

**依赖链**：`protocol ← core ← cli` / `core ← web`

### Go 参考代码

TypeScript 版本 1:1 复刻 Go 版 `xyncra-client` 的功能。Go 参考代码路径前缀为 `../../`（即 `xyncra-server` 仓库根目录）：

```text
../../pkg/protocol/            ← 协议定义（TS protocol 包参考）
../../pkg/client/              ← Go client 库（TS core 包参考）
../../pkg/store/               ← Go 客户端存储层（TS core/db 参考）
../../internal/cli/            ← Go CLI 实现（TS cli 包参考）
../../cmd/xyncra-client/       ← Go CLI 入口
```

### 数据流

```text
CLI 命令 ──→ IPC (Unix socket) ──→ Daemon 进程 ──→ WebSocket ──→ Xyncra Server
                                                        ↕
                                                 Dexie/IndexedDB
                                                        ↕
                                               UpdateHandler（stdout）

里程碑 2（未来）：
React AI 助手 ──→ XyncraClient（直接导入，无 IPC）──→ WebSocket ──→ Server
```

---

## 2. 已实现组件清单

> 随着各 Phase 的推进，更新此清单。

### Phase 1: xyncra-protocol（目标状态）

| 文件 | 组件 | Go 参考 |
|------|------|---------|
| `src/package.ts` | PackageType, Package, PackageDataRequest/Response/Updates, UpdateType 常量 | `../../pkg/protocol/protocol.go` |
| `src/function.ts` | FunctionInfo, ReturnInfo | `../../pkg/protocol/function.go` |
| `src/errors.ts` | ResponseCode 常量, HandlerError, 工厂函数 | `../../pkg/protocol/errors.go` |
| `src/index.ts` | 公共导出汇总 | - |

### Phase 2: xyncra-client-core（目标状态）

| 文件 | 组件 | Go 参考 |
|------|------|---------|
| `src/db/database.ts` | Dexie 数据库定义（9 个 table） | `../../pkg/store/clientdb.go` |
| `src/db/*.ts` | 各 sub-store CRUD | `../../pkg/store/*_store.go` |
| `src/connection.ts` | 连接管理（重连、退避、4001） | `../../pkg/client/connection.go` |
| `src/sync.ts` | SyncManager（FullSync, debounce pull） | `../../pkg/client/sync.go` |
| `src/retry.ts` | RetryManager | `../../pkg/client/retry.go` |
| `src/heartbeat.ts` | 心跳循环 | `../../pkg/client/client.go` |
| `src/idempotency.ts` | IdempotencyCache (LRU) | `../../pkg/client/idempotency_cache.go` |
| `src/rtt.ts` | RTTTracker（自适应超时） | `../../pkg/client/rtt_tracker.go` |
| `src/response-retry.ts` | ResponseRetryQueue | `../../pkg/client/response_retry_queue.go` |
| `src/client.ts` | XyncraClient 主类 | `../../pkg/client/client.go` |
| `src/interfaces/*.ts` | IWebSocket, IIndexedDBProvider, IUpdateHandler, ILogger | - |

### Phase 3: xyncra-client-cli（目标状态）

| 文件 | 组件 | Go 参考 |
|------|------|---------|
| `src/bin/cli.ts` | CLI 入口 | `../../cmd/xyncra-client/main.go` |
| `src/commands/*.ts` | ~20 个子命令 | `../../internal/cli/*.go` |
| `src/ipc/server.ts` | IPC Server (Unix socket JSON-RPC 2.0) | `../../internal/cli/ipc.go` |
| `src/ipc/client.ts` | IPC Client | `../../internal/cli/ipc.go` |
| `src/daemon/lock.ts` | 文件锁 | `../../internal/cli/lock.go` |
| `src/daemon/paths.ts` | 路径解析 | `../../internal/cli/paths.go` |
| `src/functions/builtin.ts` | 内置 function (ping/get_device_info/get_time) | `../../internal/cli/builtin_functions.go` |
| `src/output/*.ts` | Console/CSV 格式化 | `../../internal/cli/output/*.go` |

---

## 3. 关键接口定义

```typescript
// === 环境抽象接口（core 包定义，runtime 注入） ===

// WebSocket 抽象
interface IWebSocket {
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onmessage: ((event: { data: string }) => void) | null;
  onclose: ((event: { code: number; reason: string }) => void) | null;
  onopen: (() => void) | null;
  onerror: ((event: unknown) => void) | null;
  readyState: number;
}

interface IWebSocketFactory {
  create(url: string): IWebSocket;
}

// IndexedDB 抽象
interface IIndexedDBProvider {
  open(name: string, version?: number): IDBOpenDBRequest;
}

// 更新处理
interface IUpdateHandler {
  onMessage(msg: Message): void | Promise<void>;
  onDeleteMessage(messageID: string, conversationID: string): void | Promise<void>;
  onMarkRead(conversationID: string, messageID: number): void | Promise<void>;
  onConversation(conv: Conversation): void | Promise<void>;
  onGap(seq: number): void | Promise<void>;
  onTyping(userID: string, conversationID: string, isTyping: boolean): void | Promise<void>;
  onStreaming(userID: string, conversationID: string, streamID: string, text: string, isDone: boolean): void | Promise<void>;
  onAgentStatus(userID: string, conversationID: string, status: string): void | Promise<void>;
  onAgentTimeout(userID: string, conversationID: string, reason: string): void | Promise<void>;
}

// 日志
interface ILogger {
  info(msg: string, ...args: unknown[]): void;
  error(msg: string, ...args: unknown[]): void;
  warn(msg: string, ...args: unknown[]): void;
  debug(msg: string, ...args: unknown[]): void;
}

// === Client 选项 ===

interface ClientOptions {
  serverURL: string;
  userID: string;
  deviceID?: string;
  wsFactory: IWebSocketFactory;
  idbProvider: IIndexedDBProvider;
  updateHandler?: IUpdateHandler;
  logger?: ILogger;
  deviceInfo?: Record<string, string>;
  functions?: FunctionInfo[];
  heartbeatInterval?: number;
  syncBatchSize?: number;
  reconnectBaseDelay?: number;
  reconnectMaxDelay?: number;
  reconnectMaxRetries?: number;
}

// === XyncraClient 公开 API ===

class XyncraClient {
  constructor(options: ClientOptions);
  start(signal?: AbortSignal): Promise<void>;
  stop(): void;
  sendMessage(convID: string, content: string, clientMsgID?: string, replyTo?: number): Promise<SendMessageResult>;
  createConversation(userID2: string, title: string): Promise<CreateConversationResult>;
  call(method: string, params?: unknown): Promise<unknown>;
  registerRequestHandler(method: string, handler: RequestHandler): void;
  // ...
}
```

---

## 4. 产品决策摘要

> 完整内容见 `docs/decisions/PRODUCT_DECISIONS.md`

| 编号 | 决策 | 含义 |
|------|------|------|
| TS-D-001 | 多包 Workspace 架构 | 4 个包（protocol, core, cli, web），清晰依赖链 |
| TS-D-002 | 环境无关核心 | core 包零环境假设，通过构造函数注入 |
| TS-D-003 | Dexie.js + fake-indexeddb | IndexedDB wrapper + Node.js polyfill |
| TS-D-004 | commander.js CLI 框架 | 轻量，对应 Go 的 cobra |
| TS-D-005 | 完全替代 Go 版本 | 共享路径，相同命令/flag，不能同时运行 |
| TS-D-006 | JSON-RPC 2.0 over Unix socket | IPC 协议与 Go 版本完全兼容 |
| TS-D-007 | 浏览器内嵌模式 | Milestone 2 AI 助手直接导入 client，无 IPC |

**对实现的影响：**

- 不实现：浏览器 UI（里程碑 2 范围）、Go 数据迁移
- 实现：完整 Go client 功能复刻、IPC 兼容、双运行环境支持
- 关键约束：core 包不 import 任何 Node.js 或浏览器 API

---

## 5. 代码规范

- TypeScript strict mode
- 注释使用英文，JSDoc 风格
- 错误使用自定义 Error 类 + `cause` 链
- 遵循现有命名和模式（参考 Go 版本命名）
- 新功能必须有单元测试
- 测试文件放在 `src/__tests__/` 目录
- Biome 用于 lint（替代 ESLint + Prettier）
- Commit message 遵循 Conventional Commits
- 使用 `uuid` 库生成 ID
- 使用 `ws` 库处理 WebSocket (Node.js)
- 使用 `dexie` 处理 IndexedDB
- 使用 `fake-indexeddb` 作为 Node.js IndexedDB polyfill
- 使用 `commander` 处理 CLI
