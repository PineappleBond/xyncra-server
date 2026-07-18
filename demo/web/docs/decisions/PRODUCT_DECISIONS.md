# Xyncra TypeScript Client 产品决策文档

记录非常规的复杂架构决策、影响全局的约束、以及后续开发必须知晓的约定。显而易见的常识性设计不记录。

> 编号规则：使用 `TS-D-xxx` 前缀，与 Go 版 `D-xxx` 区分。

---

## 决策概览

| 编号 | 决策 | 原因 |
|------|------|------|
| TS-D-001 | 多包 Workspace 架构 | 4 个包（protocol, core, cli, web），清晰依赖链；浏览器构建不引入 Node.js 代码 |
| TS-D-002 | 环境无关核心（构造函数注入） | 同一份 core 代码运行在 Node.js CLI 和浏览器 AI 助手中 |
| TS-D-003 | Dexie.js + fake-indexeddb 作为存储层 | 用户指定 Dexie.js；fake-indexeddb 兼容 Node 20+，浏览器用原生 IndexedDB |
| TS-D-004 | commander.js 作为 CLI 框架 | 最轻量的 Node.js CLI 框架，对应 Go 的 cobra |
| TS-D-005 | 完全替代 Go 版本 | 共享 `~/.xyncra/` 路径，相同命令/flag/IPC 协议；不能同时运行 |
| TS-D-006 | JSON-RPC 2.0 over Unix socket (IPC) | 与 Go 版本 IPC 完全兼容，daemon 可互换 |
| TS-D-007 | 浏览器内嵌模式（里程碑 2） | AI 助手作为 React 组件直接导入 client，无 IPC 层 |
| TS-D-008 | TypeScript 版本作为 npm workspace 子包 | 与 demo/web 前端项目统一管理，Milestone 2 直接复用 |
| TS-D-009 | 包名使用 `@xyncra/` scope | workspace 内部引用清晰，避免命名冲突 |
| TS-D-010 | 协议类型 1:1 映射 Go protocol 包 | 不改造、不抽象；降低迁移心智负担 |

---

## 决策详情

### TS-D-001: 多包 Workspace 架构

**决策**：将 TypeScript client 拆分为 4 个 npm workspace 子包：`xyncra-protocol`、`xyncra-client-core`、`xyncra-client-cli`、`xyncra-client-web`。

**原因**：
- 浏览器构建时不应引入 Node.js 专属代码（IPC、文件锁、daemon）
- 协议类型应可独立版本管理和引用
- 依赖链清晰：`protocol ← core ← cli` / `core ← web`

**备选方案**：
- 单包分层架构：否决，浏览器构建会引入 Node.js 代码
- 双包拆分（core + cli）：否决，协议类型应可独立版本管理

### TS-D-002: 环境无关核心

**决策**：`xyncra-client-core` 包零环境假设，所有环境差异通过构造函数注入（`IWebSocketFactory`、`IIndexedDBProvider`、`IUpdateHandler`、`ILogger`）。

**原因**：
- 同一份核心代码同时支持 Node.js CLI 和浏览器 AI 助手
- core 包不 import 任何 Node.js 或浏览器 API
- 新环境适配只需实现接口并注入

### TS-D-003: Dexie.js + fake-indexeddb

**决策**：使用 Dexie.js 作为 IndexedDB wrapper，Node.js 环境使用 fake-indexeddb 作为 polyfill，浏览器使用原生 IndexedDB。

**原因**：
- 用户指定 Dexie.js
- fake-indexeddb 是纯 JS IndexedDB 实现，兼容 Node 20+
- 浏览器环境零成本切换

### TS-D-004: commander.js CLI 框架

**决策**：使用 commander.js 作为 CLI 框架。

**原因**：最轻量、最流行的 Node.js CLI 框架，功能对应 Go 的 cobra。

### TS-D-005: 完全替代 Go 版本

**决策**：TypeScript 版本完全替代 Go 版 xyncra-client，共享 `~/.xyncra/{user_id}/{device_id}/` 路径（socket、lock、logs），相同的命令名和 flag。

**原因**：
- 用户选择完全替代，不需要并存
- 降低用户迁移成本（命令/路径完全一致）
- Go 版本不再维护

### TS-D-006: JSON-RPC 2.0 over Unix socket (IPC)

**决策**：IPC 协议保持 JSON-RPC 2.0 over Unix domain socket，与 Go 版本完全兼容。

**原因**：完全替代策略要求 IPC 协议不变，外部工具（包括 Go 版 daemon 的 IPC 客户端）无需改动。

### TS-D-007: 浏览器内嵌模式

**决策**：里程碑 2 的 AI 助手作为 React 组件直接导入 `xyncra-client-core`，无 IPC 层，直接在浏览器进程内运行。

**原因**：
- 避免额外的 daemon 进程
- 利用环境注入设计，浏览器端提供原生 WebSocket + IndexedDB
- AI 助手与 client 共享同一进程，延迟最低

### TS-D-008: npm workspace 子包

**决策**：所有包放在 `demo/web/packages/` 下，使用 npm workspaces 管理。

**原因**：与 demo/web 前端项目统一管理，Milestone 2 的 AI 助手可直接引用 workspace 内的 client 包。

### TS-D-009: @xyncra/ scope 包名

**决策**：workspace 内的包使用 `@xyncra/` scope（如 `@xyncra/protocol`、`@xyncra/client-core`）。

**原因**：
- 清晰的命名空间
- 避免与 npm 公共包冲突
- workspace 内部引用方便

### TS-D-010: 协议类型 1:1 映射

**决策**：TypeScript 协议类型完全 1:1 映射 Go `pkg/protocol/` 包，不做改造或额外抽象。

**原因**：
- 降低迁移心智负担
- 保持协议层的简单性和可预测性
- Go 参考代码即为最佳文档

---

## 相关文档

- [设计文档](../plans/2026-07-18-typescript-xyncra-client-design.md)
- [Go 版产品决策](../../docs/decisions/PRODUCT_DECISIONS.md)
