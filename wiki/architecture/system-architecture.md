---
last_updated: 2026-07-17
---

# 系统架构概览

> last_updated: 2026-07-17

## 概述

Xyncra 是一个基于 Go 语言的实时消息 + AI Agent 服务器。系统以 WebSocket 为通信协议，采用**分层解耦 + 异步处理 + 离线优先**的架构设计原则。单二进制部署，可选依赖组件按需接入。

## 架构层次

```
┌─────────────────────────────────────────────────────┐
│                   Client Layer                       │
│  ┌──────────────────────────────────────────────┐   │
│  │  xyncra-client (Go library / CLI daemon)     │   │
│  │  ┌──────────┐ ┌────────┐ ┌──────────────┐   │   │
│  │  │Connection│ │  Sync  │ │  Retry       │   │   │
│  │  │ Manager  │ │Manager │ │  Manager     │   │   │
│  │  └──────────┘ └────────┘ └──────────────┘   │   │
│  │  ┌──────────────────────────────────────┐   │   │
│  │  │  Local DB (SQLite ClientDB)          │   │   │
│  │  │  - Messages, Conversations           │   │   │
│  │  │  - SyncState, RpcLogs, Questions     │   │   │
│  │  └──────────────────────────────────────┘   │   │
│  └──────────────────────────────────────────────┘   │
└────────────────────▲────────────────────────────────┘
                     │ WebSocket (wss://)
                     ▼
┌─────────────────────────────────────────────────────┐
│                 Protocol Layer                        │
│  ┌──────────────────────────────────────────────┐   │
│  │  3-Layer Envelope: Package → Data → Payload   │   │
│  │  Request / Response / Updates (push)          │   │
│  │  JSON over WebSocket TextMessage              │   │
│  └──────────────────────────────────────────────┘   │
└────────────────────▲────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────┐
│                Handler Layer (RPC)                    │
│  ┌──────────────────────────────────────────────┐   │
│  │  DefaultMessageHandler                       │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────────┐  │   │
│  │  │ Requests │ │Responses │ │   Updates    │  │   │
│  │  │ Dispatch │ │  (RPC)   │ │   (Push)     │  │   │
│  │  └────┬─────┘ └────┬─────┘ └──────┬───────┘  │   │
│  └───────┼─────────────┼──────────────┼─────────┘   │
└──────────┼─────────────┼──────────────┼─────────────┘
           │             │              │
           ▼             ▼              ▼
┌─────────────────────────────────────────────────────┐
│              Business Logic Layer                     │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────┐ │
│  │ Heartbeat│ │SendMsg   │ │SyncUpdate│ │CreateConv│ │
│  │ Handler  │ │ Handler  │ │ Handler  │ │ Handler │ │
│  ├──────────┤ ├──────────┤ ├──────────┤ ├────────┤ │
│  │DelMsg    │ │MarkAsRead│ │GetMsgs   │ │SetTyping│ │
│  │ Handler  │ │ Handler  │ │ Handler  │ │ Handler │ │
│  ├──────────┤ ├──────────┤ ├──────────┤ ├────────┤ │
│  │StreamText│ │AgentResum│ │RegFunc   │ │MQRcv    │ │
│  │ Handler  │ │ Handler  │ │ Handler  │ │ Handler │ │
│  └──────────┘ └──────────┘ └──────────┘ └────────┘ │
└──────────┬──────────────┬──────────────┬─────────────┘
           │              │              │
     ┌─────▼──────┐ ┌────▼───┐ ┌───────▼──────┐
     │   Store    │ │   MQ   │ │    Agent     │
     │ (PostgreSQL│ │(Asynq/ │ │  (Eino AI    │
     │  /MySQL)   │ │ Redis) │ │  Framework)  │
     └────────────┘ └────────┘ └──────────────┘
```

## 核心组件

### 1. Server 层 (`internal/server`)

| 组件 | 职责 | 关键文件 |
|------|------|----------|
| `BaseServer` | 生命周期管理（Start/Stop/GracefulStop），依赖注入 | `server.go` |
| `WebSocketServer` | 嵌入 BaseServer，管理 WS 连接池、广播、跨节点路由 | `websocket_server.go` |
| `Client` | 单个 WS 连接，readPump/writePump 双协程模型 | `websocket_server.go` |
| `MessageHandler` | 分发入站 Package | `websocket_handler.go` |
| `DefaultMessageHandler` | 按 method 路由到注册的 Handler | `websocket_handler.go` |
| `ReverseRPC` | 服务端发起请求给客户端，支持超时持久化 | `reverse_rpc.go` |
| `ConnectionStore` | 连接元数据管理（Redis 实现） | `redis_connection_store.go` |
| `NodeBroadcaster` | 跨节点消息路由（Redis Pub/Sub） | `node_broadcaster.go` |
| `FunctionRegistry` | 客户端函数能力注册 | `function_registry.go` |
| `PendingStore` | 超时请求持久化（Redis 实现） | `redis_pending_store.go` |

**关键设计**：
- 连接三索引：`clients[connID]`、`clientsByUser[userID][connID]`、`clientsByDevice[userID+deviceID][connID]`
- 设备替换：同 (userID, deviceID) 新连接时，旧连接收到 4001 close frame
- 跨节点广播：本地推送 + Redis Pub/Sub 发布（D-018）

### 2. Handler 层 (`internal/handler`)

业务 RPC 方法的实现层。每个 method 对应一个 Handler 类型，通过 `RegisterAll()` 注册到 `DefaultMessageHandler`。

| RPC 方法 | Handler | 说明 |
|----------|---------|------|
| `heartbeat` | HeartbeatHandler | 被动 TTL 续期 |
| `send_message` | SendMessageHandler | 核心：先落库后处理 |
| `sync_updates` | SyncUpdatesHandler | 增量同步 + gap 填充 |
| `create_conversation` | CreateConversationHandler | find-or-create 幂等 (D-011) |
| `list_conversations` | ListConversationsHandler | 分页列表 |
| `get_messages` | GetMessagesHandler | 消息分页查询 |
| `search_messages` | SearchMessagesHandler | 全文搜索 |
| `get_conversation` | GetConversationHandler | 单会话 + 未读数 + HITL 问题 |
| `delete_conversation` | DeleteConversationHandler | 级联软删除 (D-013) |
| `restore_conversation` | RestoreConversationHandler | 级联恢复 (D-015) |
| `delete_message` | DeleteMessageHandler | 仅发送者可删 (D-014) |
| `mark_as_read` | MarkAsReadHandler | MAX 语义更新读指针 (D-012) |
| `set_typing` | SetTypingHandler | 瞬时推送，Seq=0 |
| `stream_text` | StreamTextHandler | 瞬时推送，Seq=0 |
| `reload_agents` | ReloadAgentsHandler | 运行时热加载 Agent 配置 (D-076) |
| `agent_resume` | AgentResumeHandler | HITL 中断后恢复 Agent (D-085) |
| `system.register_functions` | RegisterFunctionsHandler | 客户端函数能力注册 (D-098) |
| `system.reconnect` | ReconnectHandler | 重连握手 + 请求重放 (D-108) |

### 3. Store 层 (`internal/store`)

基于 GORM 的数据持久化层，支持 PostgreSQL 和 MySQL。

| 子模块 | 表 | 职责 |
|--------|-----|------|
| `ConversationStore` | `conversations` | 会话 CRUD、软删除/恢复、读指针更新 |
| `MessageStore` | `messages` | 消息 CRUD、client_message_id 幂等性 |
| `UserUpdateStore` | `user_updates` | 用户级 Update 序列（fan-out 存储） |
| `QuestionStore` | `questions` | HITL 问题持久化 |

**Conversation 模型约束**：`Conversation` 模型使用 `UserID1`/`UserID2` 双字段（`internal/store/model/conversation.go`），当前仅支持 **1-on-1 私聊**。群组场景需要扩展为多成员模型。

**`SendMessage` 原子操作**（`store.go:SendMessage`）：
```
1. 事务内读取 conversation 的 LastProcessedMessageID
2. MessageID = LastProcessedMessageID + 1（D-008）
3. 以 MAX(UserUpdate.seq) + 1 为每个成员分配 seq
4. INSERT message + INSERT user_updates (batch)
5. UPDATE conversation last_message_at / last_processed_message_id
```

### 4. MQ 层 (`internal/mq`)

基于 Asynq（Redis）的异步任务队列。`internal/mq/mq.go:64-87` 定义了 7 种任务类型，当前在 `internal/mq/handler.go` 中注册了 3 种处理器：

| 任务类型 | 处理器 | 说明 |
|---------|--------|------|
| `mq:send_message` | NewSendMessageTaskHandler | 广播实时消息给接收方 |
| `mq:sync_updates` | — | 更新 fan-out（预留） |
| `mq:push_notification` | — | 推送通知（预留） |
| `mq:presence_broadcast` | — | 在线状态广播（预留） |
| `mq:conversation_sync` | — | 会话同步（预留） |
| `mq:agent_process` | AgentTaskHandler | Agent AI 处理 |
| `mq:agent_resume` | NewAgentResumeHandler | HITL 恢复后继续 Agent |

### 5. Agent 层 (`internal/agent`)

基于 CloudWeGo Eino 框架的 AI Agent 子系统。

| 组件 | 职责 |
|------|------|
| `AgentRegistry` | 从磁盘加载 Agent 配置，支持运行时热更新 |
| `AgentConfig` | 每个 Agent 的 LLM 提供商/模型/参数配置 |
| `LLMProvider` | 接口抽象，支持 OpenAI/Claude/Ollama/Qwen |
| `AgentBuilder` | 构建 Eino Graph/Chain |
| `AgentExecutor` | Agent 执行管道：上下文加载 → 构建 → 流式生成 → 广播 → 持久化 |
| `StreamBridge` | Agent 流式文本 → WebSocket Updates 桥接 |
| `DynamicToolProvider` | 客户端注册的函数作为 Agent Tool |
| `ContextManager` | 对话上下文管理（DB + 内存缓存 + Token 裁剪） |
| `ConversationLock` | 会话级并发锁（同一会话串行处理） |
| `TokenCounter` | Token 计数和裁剪 |
| `CheckpointStore` | HITL 断点持久化（D-083） |

**MCP Server 集成**（D-086）：
- 通过 `MCPBridge`（`internal/agent/tools/mcp.go`）管理 MCP 服务器连接
- 支持 **SSE** 和 **stdio** 两种传输协议
- Agent 配置的 `mcp_servers` 列表在 `AgentBuilder.Build()` 阶段连接，失败即跳过（fail-open）
- 支持按名称过滤工具列表

**内置工具**（`internal/agent/tools/`）：
所有工具通过 `tools.Registry`（工厂模式）管理，预注册于 `DefaultRegistry`：

| 工具名 | 说明 |
|--------|------|
| `get_weather` | 模拟天气数据（开发/演示用） |
| `get_current_time` | 获取指定时区的当前时间 |
| `retrieve_tool_result` | 按 ID 检索之前截断的工具结果 |
| `ask_user` | HITL 中断，暂停 Agent 等待用户确认 |

工具注册在 `tools/registry.go:init()` 中完成，Agent 的 `tools` 字段引用工具名，未注册的工具跳过。

**Middleware**（D-079，`internal/agent/middleware.go`）：
可选 Eino ADK 中间件，按固定顺序链式执行：

| 中间件 | 配置字段 | 说明 |
|--------|----------|------|
| `DynamicToolProvider` | `middleware.enable_client_tools` | 将客户端注册的函数动态注入为 Agent 工具（D-101） |
| `PatchToolCalls` | `middleware.enable_patch_tool_calls` | 修复 LLM 工具调用的格式问题 |
| `Summarization` | `middleware.enable_summarization` | 上下文超阈值时自动摘要（默认阈值 160K tokens） |
| `ToolReduction` | `middleware.enable_tool_reduction` | 截断工具返回结果（默认阈值 50K 字符，D-080） |
| `LLMLogger` | 代码注入 | 记录所有 LLM 请求/响应/工具调用到专用日志 |

中间件初始化失败跳过（fail-open），无中间件时返回 nil。

**SubAgent**（D-081，`internal/agent/subagent.go`）：
- Agent 配置的 `sub_agents` 字段声明子代理 ID 列表
- `AgentBuilder.Build()` 调用 `resolveSubAgents()` 查找子代理、构建子 Agent，通过 `adk.NewAgentTool` 包装为工具
- 递归深度限制为 1 层（子代理的 `SubAgents` 被清空）

### 6. Client 层 (`pkg/client`)

客户端 SDK，内嵌 SQLite 本地数据库。

| 组件 | 职责 |
|------|------|
| `XyncraClient` | 顶层入口，管理连接/同步/重试 |
| `connectionManager` | WebSocket 连接生命周期（重连、心跳） |
| `syncManager` | 增量同步管道（应用 Update + debound 拉取） |
| `retryManager` | 失败 RPC 重试队列 |
| `idempotencyCache` | 请求幂等性去重缓存 |
| `RTTTracker` | 动态 RPC 超时自适应 |
| `ResponseRetryQueue` | 服务端响应重试队列（设备替换场景） |

## Client 本地 DB (`pkg/store`)

客户端内嵌 SQLite 数据库（GORM + `glebarez/sqlite`），实现离线优先架构。

| 表 | 说明 |
|----|------|
| `messages` | 本地消息缓存 |
| `conversations` | 本地会话缓存 |
| `notification_logs` | 通知去重日志 |
| `sync_states` | 同步状态（local_max_seq, latest_seq） |
| `rpc_logs` | RPC 调用日志 |
| `questions` | HITL 问题缓存 |
| `queue_store` | 重试队列持久化 |
| `drafts` | 草稿存储 |
| `user_updates` | 用户更新本地副本 |

## 部署模型

### 单节点部署（默认）

```
┌──────────────────────────────────────────────┐
│               Single Binary                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │WS Server │  │   MQ     │  │  Agent   │   │
│  │:8080     │  │ Consumer │  │  Worker  │   │
│  └──────────┘  └──────────┘  └──────────┘   │
│  ┌────────────────────────────────────────┐  │
│  │  Store (PostgreSQL/MySQL)              │  │
│  └────────────────────────────────────────┘  │
└──────────────────────────────────────────────┘
```

### 多节点部署

```
┌─────── Node A ───────┐    ┌─────── Node B ───────┐
│  WS Server            │    │  WS Server            │
│  MQ Consumer          │    │  MQ Consumer          │
│  Agent Worker         │    │  Agent Worker         │
│  Store (shared DB)    │    │  Store (shared DB)    │
└────────┬──────────────┘    └────────┬──────────────┘
         │                            │
         └─────────── Redis ──────────┘
                    ├ Asynq (MQ)
                    ├ ConnectionStore
                    ├ Pub/Sub (NodeBroadcaster)
                    └ PendingStore
```

**多节点关键机制**（D-018）：
- `NodeBroadcaster` 接口抽象跨节点推送
- 单节点：`NoopBroadcaster`（默认）
- 多节点：`RedisNodeBroadcaster`（Pub/Sub）
- 源节点 ID 防重复（`sourceNodeID == s.nodeID` 跳过）

### 依赖组件

| 组件 | 必需 | 说明 |
|------|------|------|
| PostgreSQL / MySQL | 是 | 核心数据存储 |
| Redis | 是 | MQ 后端 + ConnectionStore + Pub/Sub |
| LLM API (OpenAI/Claude/Qwen/Ollama) | 否 | Agent 功能 |
| 反向代理 (Nginx/Caddy) | 推荐 | TLS 终止 + CORS (D-003) |

## 核心架构原则

1. **先落库后处理**：消息先持久化再到 MQ，MQ 失败不影响数据安全（D-007）
2. **离线优先**：客户端本地 SQLite 优先读取，按需从服务器拉取（D-035, D-126）
3. **Fire-and-Forget**：MQ 入队失败不阻塞请求，数据通过 sync_updates 最终送达（D-007）
4. **Ephemeral 与 Persistent 分离**：Seq=0 表示瞬时消息，不持久化、不拉取（D-050）
5. **Pull-on-Notification**：推送为轻量通知，客户端拉取最新状态（D-118）
6. **nil-safe 可选模块**：Agent、FunctionRegistry 为可选注入，nil 即禁用
7. **接口隔离**：Server / Store / MQ / Agent 通过接口依赖，可独立替换和测试
