---
last_updated: 2026-07-17
---

# 项目结构

> last_updated: 2026-07-17

## 目录总览

```
xyncra-server/
├── cmd/                    # 程序入口
├── internal/               # 私有实现
├── pkg/                    # 公共库
├── agents/                 # AI Agent 定义
├── scripts/                # 辅助脚本
├── docs/                   # 文档
├── wiki/                   # 开发维基（本文档目录）
├── deploy/                 # Docker 与部署配置
│   ├── Dockerfile              # Docker 构建
│   ├── docker-compose.yml      # 生产环境编排
│   ├── docker-compose.e2e.yml  # E2E 测试环境编排
│   ├── alertmanager/           # AlertManager 配置
│   ├── grafana/                # Grafana 仪表盘与数据源
│   ├── loki/                   # Loki 配置
│   ├── prometheus/             # Prometheus 规则与配置
│   └── promtail/               # Promtail 配置
├── .claude/                # AI 辅助开发技能
├── Makefile                # 构建/测试命令
├── go.mod / go.sum         # Go 模块依赖
└── README.md / README-ZH.md # 项目说明
```

## `cmd/` — 入口点

### `cmd/xyncra-server/main.go`

服务器主入口。职责：

1. **配置加载** — 解析命令行 flag 和环境变量（flag 优先），支持 `XYNCRA_*` 环境变量
2. **依赖初始化** — 按正确顺序初始化数据库、Redis 连接、MQ broker、Agent registry
3. **组件装配** — 创建 `WebSocketServer`，使用函数式选项注入所有依赖（`WSWith*`）
4. **服务启动** — 启动 MQ worker pool、后台清理协程、WebSocket 服务器
5. **优雅关闭** — 监听 SIGINT/SIGTERM，按逆序关闭所有资源

关键设计决策：
- 依赖注入通过函数式选项实现（`WSWithStore`、`WSWithBroker`、`WSWithConnectionStore`）
- 每个外部服务使用独立的 Redis 客户端连接（常规命令、Pub/Sub、idempotency）避免相互阻塞
- 后台清理协程（context cache、tool results、user updates、HITL timeout）在 `main()` 中启动

### `cmd/xyncra-client/main.go`

CLI 客户端入口。使用 Cobra 命令框架，支持子命令：

```
xyncra-client listen              # 启动守护进程（维持 WebSocket 长连接）
xyncra-client send                # 发送消息
xyncra-client create-conversation # 创建会话
xyncra-client list-conversations  # 列出会话
xyncra-client delete-conversation # 删除会话
xyncra-client restore-conversation # 恢复会话
xyncra-client get-conversation    # 获取会话详情
xyncra-client get-messages        # 获取消息
xyncra-client delete-message      # 删除消息
xyncra-client search-messages     # 搜索消息
xyncra-client mark-as-read        # 标记已读
xyncra-client sync                # 强制同步
xyncra-client set-typing          # 发送输入中指示
xyncra-client stream-text         # 发送流式文本更新
xyncra-client agent-resume        # 恢复 HITL 暂停的 agent
xyncra-client reload-agents       # 热重载 agent 配置
xyncra-client draft               # 草稿管理
xyncra-client logs                # 日志查看
xyncra-client kill                # 终止守护进程
```

## `internal/` — 私有实现

核心业务逻辑，总共 8 个包（server, handler, agent, mq, store, cli, cleanup, e2e）：

### `internal/server/` — WebSocket 服务器与连接管理

核心文件：

| 文件 | 职责 |
|------|------|
| `server.go` | `Server` 接口定义、`BaseServer` 生命周期实现 |
| `websocket_server.go` | `WebSocketServer` 实现，HTTP upgrade、连接维护、broadcast |
| `websocket_handler.go` | `MessageHandler` 接口、`DefaultMessageHandler` 路由实现 |
| `websocket_client.go` | 客户端连接管理（readPump、writePump） |
| `connection_store.go` | `ConnectionStore` 接口定义 |
| `redis_connection_store.go` | Redis 实现的 ConnectionStore |
| `memory_connection_store.go` | 内存实现的 ConnectionStore（用于测试） |
| `node_broadcaster.go` | `NodeBroadcaster` 接口：跨节点消息路由 |
| `redis_node_broadcaster.go` | Redis Pub/Sub 实现的跨节点广播 |
| `function_registry.go` | `FunctionRegistry`：客户端函数注册管理 |
| `pending_store.go` | `PendingStore`：超时 ReverseRPC 请求持久化 |
| `redis_pending_store.go` | Redis 实现的 PendingStore |
| `reverse_rpc.go` | `ReverseRPC`：服务端发起 RPC 到客户端 |
| `doc.go` | 包文档 |

关键抽象：
- `Server` 接口 — `Start(ctx)`、`GracefulStop(ctx)`、`Store()`、`Broker()`、`ConnectionStore()`
- `ConnectionStore` 接口 — `Add/Get/Remove/ListByUser/Ping`
- `MessageHandler` 接口 — `HandleMessage(ctx, client, pkg)`
- `MethodHandler` 接口 — `HandleRequest(ctx, client, req)`
- `NodeBroadcaster` 接口 — `Publish/Subscribe/Close`
- `FunctionRegistry` 接口 — `RegisterFunctions/GetFunctions/OnDeviceDisconnect`
- `PendingStore` 接口 — `Save/List/Remove/Update`

### `internal/handler/` — RPC 方法处理器

每个文件对应一个 `MethodHandler` 实现：

| 文件 | RPC 方法 | 功能 |
|------|----------|------|
| `send_message.go` | `send_message` | 发送消息（持久化 + MQ fanout + Agent 触发） |
| `create_conversation.go` | `create_conversation` | Find-or-create 1-on-1 会话 |
| `delete_conversation.go` | `delete_conversation` | 级联软删除会话和消息 |
| `restore_conversation.go` | `restore_conversation` | 恢复软删除的会话 |
| `delete_message.go` | `delete_message` | 发送者删除消息 |
| `get_conversation.go` | `get_conversation` | 获取单条会话（含未读数和 HITL 问题） |
| `list_conversations.go` | `list_conversations` | 分页列出会话 |
| `get_messages.go` | `get_messages` | 分页获取消息历史 |
| `search_messages.go` | `search_messages` | 会话内文本搜索 |
| `sync_updates.go` | `sync_updates` | 增量更新同步 |
| `mark_as_read.go` | `mark_as_read` | 更新读游标（MAX 语义） |
| `heartbeat.go` | `heartbeat` | 心跳保活 |
| `set_typing.go` | `set_typing` | 输入中指示（ephemeral） |
| `stream_text.go` | `stream_text` | 流式文本（ephemeral） |
| `register.go` | （聚合） | `RegisterAll` 注册所有方法 |
| `register_functions.go` | `system.register_functions` | 注册客户端函数 |
| `reconnect.go` | `system.reconnect` | 重连握手 + 请求回放 |
| `reload_agents.go` | `reload_agents` | 热重载 Agent 配置 |
| `agent_resume.go` | `agent_resume` | 恢复 HITL 暂停的 Agent |
| `mq_send_message.go` | （MQ 任务） | MQ 消息投递到在线客户端 |

每个 handler 都是无状态的（只持有不可变的依赖引用），因此并发安全。所有 handler 注册在 `RegisterAll()` 中完成。

### `internal/agent/` — AI Agent 运行时

Agent 执行引擎，共 40+ 个文件，按职责分组：

| 文件 | 职责 |
|------|------|
| `config.go` | `AgentConfig` 定义（YAML frontmatter 解析结构） |
| `registry.go` | `AgentRegistry`：Agent 配置注册与加载 |
| `executor.go` | `AgentExecutor`：Agent 执行调度 |
| `eino_agent.go` | Eino 框架 Agent 构建 |
| `middleware.go` | Eino 中间件配置（summarization, tool reduction） |
| `context.go` | Agent 上下文管理 |
| `context_keys.go` | 上下文 key 定义 |
| `stream_bridge.go` | 流式输出桥接 |
| `conversation_lock.go` | 会话级别并发锁 |
| `checkpoint_store.go` | HITL checkpoint 存储 |
| `db_context_manager.go` | Agent 对话上下文持久化管理 |
| `dynamic_tool_provider.go` | 动态工具提供者 |
| `client_function_tool.go` | 客户端函数作为 Agent 工具 |
| `hitl_cleanup.go` | HITL 超时清理 |
| `resume_handler.go` | HITL 恢复处理 |
| `subagent.go` | 子 Agent 委托 |
| `monitoring.go` | Agent 监控指标 |
| `llm_logger.go` | LLM 调用日志 |
| `token_counter.go` | Token 计数 |
| `broadcast.go` | Agent 广播通信 |
| `doc.go` | 包文档 |
| `parser.go` | 解析器 |
| `task_handler.go` | 任务处理 |
| `errors.go` | 错误定义 |
| `semaphore.go` | 并发控制信号量 |

`internal/agent/tools/` 提供内置工具：

| 工具 | 功能 |
|------|------|
| `get_weather` | 查询天气（示例工具） |
| `ask_user` | 向用户提问（HITL 工具） |
| `get_current_time` | 获取当前时间 |
| `retrieve_tool_result` | 获取异步工具结果 |
| MCP bridge | MCP 服务器集成 |

### `internal/mq/` — 消息队列

Asynq (Redis-backed) 消息队列抽象层：

| 文件 | 职责 |
|------|------|
| `mq.go` | `Broker` 接口定义、`Task` 类型、队列常量、哨兵错误 |
| `asynq.go` | `AsynqBroker` 实现 |
| `handler.go` | `TaskHandler`：任务路由注册表 |
| `options.go` | `EnqueueOption` 函数式选项 |

预定义任务类型（共 7 种）：

| 类型 | 用途 |
|------|------|
| `mq:send_message` | 消息投递到在线客户端 |
| `mq:sync_updates` | 更新 fan-out |
| `mq:push_notification` | 推送通知 |
| `mq:presence_broadcast` | 在线状态广播 |
| `mq:conversation_sync` | 会话同步 |
| `mq:agent_process` | Agent 处理消息 |
| `mq:agent_resume` | 恢复 HITL 暂停的 Agent |

队列优先级：`critical`(6) > `default`(3) > `low`(1)。

### `internal/store/` — 持久化层

基于 GORM 的数据访问层，支持 SQLite/PostgreSQL/MySQL：

| 文件 | 职责 |
|------|------|
| `store.go` | `Store` 聚合对象、`StoreAPI` 接口（含 `BeginTx`）、`SendMessage` 复合操作 |
| `db.go` | `Database` 包装、连接池配置、事务管理 |
| `errors.go` | 哨兵错误、`classifyError` 跨方言错误分类 |
| `conversation.go` | `ConversationStore`（CRUD + 搜索 + agent 状态） |
| `message.go` | `MessageStore`（CRUD + 搜索 + 统计） |
| `user_update.go` | `UserUpdateStore`（增量更新日志） |
| `question.go` | `QuestionStore`（HITL 问题） |
| `model/` | 数据模型定义 |

数据模型：

- **Conversation** — `id`, `user_id1`, `user_id2`, `type`, `title`, `agent_status`, `checkpoint_id`, HITL 状态字段
- **Message** — `id`, `client_message_id`, `conversation_id`, `message_id`, `sender_id`, `content`, `type`, `reply_to`, `status`
- **UserUpdate** — `id`, `user_id`, `seq`, `type`, `payload`, `created_at`
- **Question** — `id`, `conversation_id`, `checkpoint_id`, `question_text`, `status`, `answer`

### `internal/cli/` — CLI 客户端

Cobra 命令实现和 IPC 通信：

| 文件 | 职责 |
|------|------|
| `app.go` | CLI 应用根命令 |
| `listen.go` | `listen` 守护进程命令 |
| `send.go` | `send` 消息发送命令 |
| `conversations.go` | 会话管理命令 |
| `messages.go` | 消息查询命令 |
| `ipc.go` | 进程间通信（Unix domain socket） |
| `rpc_helper.go` | RPC 辅助函数 |
| `sync.go` | 同步命令 |
| `paths.go` | 路径管理 |
| `builtin_functions.go` | 内置客户端函数 |
| `agent_resume.go` | Agent 恢复命令 |
| `stream_text.go` | 流式文本命令 |
| `set_typing.go` | 输入中指示命令 |
| `lock.go` | 守护进程锁 |
| `kill.go` | 守护进程终止 |
| `reload_agents.go` | Agent 热重载命令 |
| `draft.go` | 草稿管理 |
| `logs.go` | 日志查看 |

### `internal/cleanup/` — 清理任务

`cleanup.go`：`UserUpdateCleaner` — 定期清理过期 UserUpdate 记录（30 天前）。

### `internal/e2e/` — 集成测试

完整的端到端测试套件，覆盖：
- Agent 基础功能、并发、上下文管理、边缘情况
- HITL 完整流程、弹性、并发
- Agent 中间件、子 Agent、客户端工具
- 全链路消息投递、错误处理、重连
- MQ 诊断、流式输出、输入边界

## `pkg/` — 公共库

### `pkg/protocol/` — 通信协议

WebSocket 协议类型定义：

```go
Package            — 顶层消息信封（type + data）
PackageDataRequest  — 客户端请求（id, method, params）
PackageDataResponse — 服务端响应（id, code, msg, data）
PackageDataUpdates  — 推送更新（updates[]）
PackageDataUpdate   — 增量更新（seq, type, payload）
```

还包含 `HandlerError` 结构化错误类型和 `FunctionInfo` 函数描述格式。

### `pkg/client/` — 客户端 SDK

`XyncraClient` 是客户端 SDK 入口，提供：
- WebSocket 连接管理（自动重连、指数退避）
- RPC 调用（`Call` + 超时 + 重试）
- 增量同步（`syncManager` + `sync_updates`）
- 幂等性缓存（`IdempotencyCache`）
- 客户端函数注册（`RegisterRequestHandler`）
- 本地 SQLite 数据读取（`ListConversations`、`GetMessages`、`SearchMessages`）

### `pkg/store/` — 客户端本地存储

客户端侧 SQLite 数据库，使用 GORM，包含与会话、消息、草稿、队列、RPC 日志、同步状态相关的存储实现。与 `internal/store` 结构平行但职责不同：`internal/store` 是服务器持久化，`pkg/store` 是客户端本地缓存。

## `agents/` — Agent 定义目录

Agent 定义文件使用 Markdown + YAML frontmatter 格式：

```yaml
---
id: weather-bot
name: Weather Bot
model: qwen3.7-plus
api_key_env: DASHSCOPE_API_KEY
tools:
  - get_weather
  - get_current_time
middleware:
  enable_client_tools: true
  enable_summarization: true
---
```

通过 `reload_agents` RPC 可热重载。当前内置：
- `weather-bot.md` — 天气助手（示例 agent）
- `hitl-bot.md` — HITL 测试 agent
- `hitl-parent.md` / `hitl-child-*.md` — 父子 agent 测试
- `mcp-bot.md` — MCP 工具集成测试

## `cmd/` 入口设计原则

Xyncra 遵循"薄入口、厚库"原则：

- **`cmd/xyncra-server/main.go`** — ~400 行，职责仅限于：配置解析 → 依赖初始化 → 组件装配 → 启动 → 优雅关闭。所有业务逻辑在 `internal/` 中实现。
- **`cmd/xyncra-client/main.go`** — Cobra 根命令定义，每个子命令委托给 `internal/cli/` 中的实现。

函数式选项模式使主函数中的依赖注入清晰可读：

```go
srv, err := server.NewWebSocketServer(
    server.WSWithAddr(*addr),
    server.WSWithConnectionStore(connStore),
    server.WSWithStore(dataStore),
    server.WSWithBroker(broker),
    server.WSWithMessageHandler(msgHandler),
    server.WSWithNodeBroadcaster(nodeBroadcaster),
    server.WSWithFunctionRegistry(funcRegistry),
    server.WSWithPendingStore(pendingStore),
)
```

## `internal/` 与 `pkg/` 的边界

| 维度 | `internal/` | `pkg/` |
|------|-------------|--------|
| 导入限制 | 仅项目内部可导入 | 外部项目可导入 |
| 职责 | 服务器业务逻辑 | 客户端 SDK、协议定义 |
| 存储 | 服务端数据库（SQLite/PG/MySQL） | 客户端本地缓存（SQLite） |
| 模型 | 服务端完整数据模型 | 客户端使用的数据子集 |
| 测试 | 单元测试 + E2E 测试 | 集成测试（需连接服务端） |

## 配置与环境变量

| flag | 环境变量 | 默认值 | 用途 |
|------|----------|--------|------|
| `-addr` | `XYNCRA_ADDR` | `:8080` | WebSocket 监听地址 |
| `-redis-addr` | `XYNCRA_REDIS_ADDR` | `localhost:6379` | Redis 地址 |
| `-redis-password` | `XYNCRA_REDIS_PASSWORD` | "" | Redis 密码 |
| `-redis-db` | `XYNCRA_REDIS_DB` | 0 | Redis 数据库编号 |
| `-db-driver` | `XYNCRA_DB_DRIVER` | `sqlite` | 数据库驱动 |
| `-db-dsn` | `XYNCRA_DB_DSN` | `xyncra.db` | 数据库 DSN |
| `-max-conns` | `XYNCRA_MAX_CONNS_PER_USER` | 0（不限） | 每用户最大连接数 |
| `-agents-dir` | `XYNCRA_AGENTS_DIR` | `agents` | Agent 定义目录 |
| `-max-functions-per-device` | `XYNCRA_MAX_FUNCTIONS_PER_DEVICE` | 200 | 每设备最大函数数 |
| | `DASHSCOPE_API_KEY` | | LLM API 密钥 |
| | `XYNCRA_LLM_LOG_DIR` | | LLM 调用日志目录 |
