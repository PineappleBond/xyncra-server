# Xyncra Server

**一个内建 AI Agent 支持的分布式消息后端。**

> WebSocket 双向 RPC · 多设备同步 · 离线弹性 · 流式输出 · 人机协作（HITL）

[架构](#架构) · [快速开始](#快速开始) · [协议](#协议) · [Agent 系统](#agent-系统) · [文档](#文档) · [贡献](#贡献) · [许可证](#许可证)

---

## 为什么选择 Xyncra？

大多数消息系统要求你在**实时基础设施**和 **AI Agent 集成**之间二选一。Xyncra 将两者整合在一个零配置的服务器中：

| 你需要…                    | Xyncra 提供                                                                         |
| -------------------------- | ----------------------------------------------------------------------------------- |
| 实时消息                   | 基于 WebSocket 的双向 RPC，支持服务端主动推送                                         |
| 能与用户交互的 AI Agent    | 内建 Agent 运行时，支持流式输出、工具调用和人机协作（HITL）                             |
| 全场景多设备               | 基于设备维度的连接跟踪，支持离线同步与断点续传                                         |
| 生产级韧性                 | Redis 支持的分布式状态、MQ 任务队列、Fail-Open 设计                                   |
| 零运维负担                 | 默认 SQLite，单一二进制，所有配置均有合理默认值                                       |

---

## 架构

```text
                        ┌─────────────────────────────────┐
                        │         你的应用                 │
                        │   (反向代理 + 认证层)            │
                        └──────────────┬──────────────────┘
                                       │  注入 user_id
                        ┌──────────────▼──────────────────┐
                        │         Xyncra Server           │
                        │                                 │
    ┌───────┐  WebSocket│  ┌───────────┐  ┌───────────┐  │   ┌───────┐
    │ 用户 A│◄─────────►│  │  连接     │  │  Agent    │  │◄─►│ Redis │
    │ 设备  │  RPC+推送  │  │  存储     │  │  运行时   │  │   └───┬───┘
    └───────┘           │  └─────┬─────┘  └─────┬─────┘  │       │
                        │        │               │        │   ┌───▼───┐
    ┌───────┐  WebSocket│  ┌─────▼─────┐  ┌─────▼─────┐  │   │  MQ   │
    │ 用户 B│◄─────────►│  │  Handler  │  │  Tool     │  │   │(Asynq)│
    │ 设备1 │  RPC+推送  │  │  注册表   │  │  Provider │  │   └───────┘
    └───────┘           │  └─────┬─────┘  └───────────┘  │
                        │        │                        │
    ┌───────┐  WebSocket│  ┌─────▼─────────────────────┐  │
    │ 用户 B│◄─────────►│  │        存储层             │  │
    │ 设备2 │  RPC+推送  │  │  (SQLite/PostgreSQL/MySQL)│  │
    └───────┘           │  └───────────────────────────┘  │
                        └─────────────────────────────────┘
```

**三层架构，单一二进制：**

- **连接层** — WebSocket 服务器，按（用户, 设备）维度跟踪连接，Redis Pub/Sub 实现跨节点广播，设备替换协议
- **消息层** — 双向 RPC（客户端↔服务端），基于序列号的持久化更新同步与断点续传，用于输入状态/流式输出/在线状态的临时推送
- **Agent 层** — YAML 配置的 AI Agent，多 LLM 支持（OpenAI、Claude、Ollama、Qwen），MCP 工具集成，通过 ReverseRPC 进行客户端侧工具调用，子 Agent 委派，以及人机协作检查点

---

## 特性

### 💬 消息核心

- **双向 RPC** — 客户端和服务端均可通过单个 WebSocket 连接发起调用
- **持久化更新** — 基于序列号的更新日志，支持游标分页（`sync_updates`）
- **离线弹性** — 客户端重连后自动拉取遗漏更新；gap 占位确保不会静默丢失数据
- **多设备同步** — 按设备维度的连接跟踪（`user_id` + `device_id` + `conn_id`）
- **软删除** — 会话和消息支持删除/恢复，并带有级联语义

### 🤖 AI Agent 运行时

- **声明式 Agent** — 在单个 Markdown 文件中通过 YAML 前置元数据定义 Agent（模型、工具、中间件、系统提示词），无需编码
- **多 LLM 支持** — 可插拔的 Provider：OpenAI、Anthropic Claude、Ollama、Qwen —— 或接入你自己的
- **流式响应** — 通过临时推送（`stream_text`）实现实时文本流式输出，采用累积快照模型
- **工具执行** — 服务端工具（代码执行、搜索）+ 客户端工具（通过 ReverseRPC 调用设备端）+ MCP 服务器集成
- **人机协作（HITL）** — Agent 通过 `ask_user` 暂停并向用户请求确认；问题持久化到数据库，基于拉取通知模式的多设备同步（会话更新作为轻量信号），幂等的 `agent_resume` 支持 409 冲突检测，以及崩溃恢复（答案存储在数据库中，检查点存储在 Redis 中，TTL 为 24 小时，过期会话自动清理）
- **子 Agent 委派** — Agent 可以调用其他 Agent，各自拥有隔离的上下文
- **上下文管理** — Token 感知的截断机制，可选的摘要中间件

### 🏗️ 基础设施

- **零配置启动** — SQLite + Redis 本地默认配置，一条命令即可运行
- **灵活存储** — 支持 SQLite（嵌入式）、PostgreSQL 或 MySQL（通过 GORM）
- **分布式就绪** — Redis Pub/Sub 跨节点推送，Asynq 异步任务队列支持优先级
- **Fail-Open 设计** — MQ 入队失败不会阻塞消息持久化；Redis 故障不会导致 Agent 崩溃
- **临时事件** — 输入指示器、流式文本、Agent 状态 —— 永不持久化，永不回放，始终实时

---

## 快速开始

### 前置条件

- **Go 1.26+**
- **Redis** 运行于 `localhost:6379`（默认）

### 构建与运行

```bash
# 克隆
git clone https://github.com/PineappleBond/xyncra-server.git
cd xyncra-server

# 构建
make build

# 启动服务（零配置：SQLite + Redis localhost:6379）
./bin/xyncra-server
```

就这样，服务已在 `:8080` 端口监听。

### Docker

```bash
docker compose -f deploy/docker-compose.yml up -d
```

### 配置

通过 CLI 参数或 `XYNCRA_` 环境变量覆盖默认值：

| 参数                | 环境变量                      | 默认值           | 描述                                        |
| ------------------- | ----------------------------- | ---------------- | ------------------------------------------- |
| `-addr`             | `XYNCRA_ADDR`                 | `:8080`          | WebSocket 监听地址                           |
| `-redis-addr`       | `XYNCRA_REDIS_ADDR`           | `localhost:6379` | Redis 地址                                   |
| `-redis-password`   | `XYNCRA_REDIS_PASSWORD`       |                  | Redis AUTH 密码                              |
| `-db-driver`        | `XYNCRA_DB_DRIVER`            | `sqlite`         | `sqlite` / `postgres` / `mysql`              |
| `-db-dsn`           | `XYNCRA_DB_DSN`               | `xyncra.db`      | 数据库连接字符串                               |
| `-max-conns`        | `XYNCRA_MAX_CONNS_PER_USER`   | `0`（无限制）     | 每用户最大连接数                              |
| `-redis-db`         | `XYNCRA_REDIS_DB`             | `0`              | Redis 数据库编号                              |
| `-agents-dir`       | `XYNCRA_AGENTS_DIR`            | `agents`         | Agent 定义文件目录路径                         |
| `-max-functions-per-device` | `XYNCRA_MAX_FUNCTIONS_PER_DEVICE` | `200` | 每设备最大注册函数数                           |

---

## 客户端 CLI

Xyncra 包含一个功能完善的 CLI 客户端（`xyncra-client`）用于与服务端交互。

```bash
# 启动守护进程（维护持久 WebSocket 连接）
./bin/xyncra-client listen --user-id alice --device-id laptop

# 创建会话
./bin/xyncra-client create-conversation --peer-id bob

# 发送消息
./bin/xyncra-client send --conversation-id <conv-id> --content "Hello!"

# 查询本地数据（支持离线，从本地 SQLite 读取）
./bin/xyncra-client list-conversations
./bin/xyncra-client get-messages --conversation-id <conv-id>
./bin/xyncra-client search-messages --conversation-id <conv-id> --query "hello"
```

守护进程会自动注册内置函数（`ping`、`get_device_info`、`get_time`），Agent 可通过 ReverseRPC 调用这些函数。还可以通过 `--device-info` 附加自定义设备元数据。

---

## 协议

所有通信均使用 WebSocket 上的**三级信封**格式：

```jsonc
// 客户端 → 服务端（请求，type=0）
{"type": 0, "data": {"id": "req-1", "method": "send_message", "params": {...}}}

// 服务端 → 客户端（响应，type=1）
{"type": 1, "data": {"id": "req-1", "code": 0, "msg": "ok", "data": {...}}}

// 服务端 → 客户端（推送更新，type=2）
{"type": 2, "data": {"updates": [{"seq": 1, "type": "message", "payload": {...}}]}}
```

### RPC 方法

| 方法                   | 描述                                                     |
| ---------------------- | -------------------------------------------------------- |
| `heartbeat`            | 心跳保活，刷新连接 TTL                                    |
| `send_message`         | 发送消息（通过 `client_message_id` 实现幂等）              |
| `sync_updates`         | 基于游标的更新同步，支持断点续传                            |
| `create_conversation`  | 查找或创建一对一会话                                       |
| `get_conversation`     | 获取单个会话，包含未读计数和 HITL 问题                     |
| `list_conversations`   | 列出会话（按 `last_message_at` 降序排列）                  |
| `get_messages`         | 分页消息历史                                               |
| `search_messages`      | 会话内文本搜索（基于 LIKE）                                |
| `mark_as_read`         | 更新已读游标（MAX 语义）                                   |
| `delete_conversation`  | 软删除会话 + 消息                                          |
| `restore_conversation` | 恢复已软删除的会话                                         |
| `delete_message`       | 软删除消息（仅限发送者）                                   |
| `set_typing`           | 临时输入指示器（Seq=0）                                    |
| `stream_text`          | 临时流式文本（Seq=0，累积快照）                             |
| `agent_resume`         | 恢复被 HITL 中断的 Agent                                  |
| `reload_agents`        | 热重载 Agent 配置                                          |
| `system.register_functions` | 注册设备函数能力（ReverseRPC）                         |
| `system.reconnect`     | 重连握手，支持请求重放                                     |

### 推送更新类型

**持久化**（Seq > 0，通过 `sync_updates` 投递）：

| 类型               | 描述                                         |
| ------------------ | -------------------------------------------- |
| `message`          | 新消息                                       |
| `delete_message`   | 消息已删除                                   |
| `mark_read`        | 已读游标已更新                               |
| `conversation`     | 会话状态变更（包含 HITL）                     |
| `gap`              | 合成断点占位（仅运行时存在）                   |

**临时**（Seq = 0，仅实时投递，永不回放）：

| 类型                           | 描述                                                                        |
| ------------------------------ | --------------------------------------------------------------------------- |
| `typing`                       | 用户输入指示器                                                               |
| `streaming`                    | 来自 Agent 的累积文本流                                                      |
| `agent_status`                 | Agent 状态：thinking / tool_calling / generating / idle / asking_user        |
| `agent_timeout`                | Agent 超时                                                                   |

📖 完整协议规范：[docs/API.md](docs/API.md)

---

## Agent 系统

Agent 以**单个 Markdown 文件**定义，使用 YAML 前置元数据 —— 无需编码。

### 示例：天气机器人

```markdown
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

You are a helpful weather assistant. Provide current weather
information, forecasts, and weather-related advice.
```

将此文件放入 `agents/` 目录，通过 `reload_agents` RPC 热重载即可生效。

### Agent 能力

| 特性               | 描述                                                                              |
| ------------------ | --------------------------------------------------------------------------------- |
| **多 LLM 支持**    | OpenAI、Claude、Ollama、Qwen —— 可插拔的 `LLMProvider` 接口                        |
| **工具调用**       | 服务端工具、客户端工具（ReverseRPC）、MCP 服务器                                     |
| **流式输出**       | 实时文本流式输出，采用累积快照模型                                                   |
| **HITL**           | 持久化问题，多设备同步，离线弹性，通过检查点 + 数据库实现崩溃恢复                       |
| **子 Agent**       | 委派给其他 Agent，各自拥有隔离上下文                                                 |
| **中间件**         | 客户端工具、工具调用补丁、摘要、工具结果精简                                          |
| **上下文管理**     | Token 感知截断，消息数量回退，可配置限制                                              |

### Agent 交互流程

```text
用户                 服务端              Agent              LLM
 │  send_message      │                    │                  │
 │───────────────────►│  入队 MQ 任务      │                  │
 │                    │───────────────────►│  提示词 + 上下文  │
 │                    │                    │─────────────────►│
 │  typing (Seq=0)    │◄─── 临时事件 ──────│                  │
 │◄───────────────────│                    │  工具调用         │
 │  agent_status      │                    │─────────────────►│
 │◄───────────────────│◄─── 临时事件 ──────│                  │
 │  streaming (Seq=0) │                    │  响应             │
 │◄───────────────────│◄───────────────────│◄─────────────────│
 │  message (Seq=N)   │                    │                  │
 │◄───────────────────│◄── 持久化 ─────────│                  │
```

### HITL 韧性

Agent 运行时实现了一个**会话状态机**，包含 6 个已定义状态（`idle`、`thinking`、`tool_calling`、`generating`、`asking_user`、`timeout`）。只有 `asking_user` 和 `idle` 会被持久化到数据库 —— 中间状态（`thinking`、`tool_calling`、`generating`）是临时的 WebSocket 广播，仅用于 UI 展示：

```text
                 ┌──────────────────────────────────────────────┐
                 │  会话状态机                                   │
                 │                                              │
                 │  临时（仅广播，不持久化）：                    │
                 │  thinking → tool_calling → generating        │
                 │                                              │
                 │  持久化（数据库）：                             │
 idle ──────────────────────────────────────────► asking_user   │
                                                   │            │
                                                   │ resume     │
                                                   │ (所有问题   │
                                                   │  已回答)    │
                                                   ◄────────────┘
                 timeout（后台任务清理并重置为 idle）
```

**拉取通知模式** —— 当 Agent 因 HITL 暂停时：

1. 问题持久化到 `Question` 表（数据库）
2. 会话 `agent_status` 转换为 `asking_user`
3. 广播一个轻量级 `conversation` 更新（仅包含 `conversation_id` + `updated_at`）
4. 客户端按需拉取完整会话状态 —— 问题、状态、检查点
5. 同时也会为在线客户端发送临时 `agent_status` 推送
6. CLI 守护进程的 `OnConversation` 处理器检测到 `agent_status == "asking_user"` 后，以 `[hitl]` 格式显示 HITL 信息（checkpoint_id、interrupt_id、question_text）

**崩溃恢复** —— 答案存储在数据库中，检查点存储在 Redis 中（24 小时 TTL）。在 HITL 等待期间服务器重启是安全的：用户仍然可以回答，恢复处理器从数据库中读取答案来重建 targets map。

**幂等性** —— `agent_resume` 使用 `UPDATE ... WHERE status='pending'` 实现原子化的答案认领。如果另一个设备已经回答，返回 409（`question_already_answered`）。多问题检查点跟踪部分进度 —— 只有当所有问题都已回答时才触发恢复。

**过期清理** —— 后台任务（基于 Redis 分布式锁，按会话粒度）检测超过可配置阈值仍处于 `asking_user` 状态的会话，将其重置为 `idle`，并清理待处理的问题。

📖 完整场景分析：[docs/design/DESIGN_HITL_RESILIENCE.md](docs/design/DESIGN_HITL_RESILIENCE.md)

📖 Agent 配置详情：[docs/decisions/PRODUCT_DECISIONS.md](docs/decisions/PRODUCT_DECISIONS.md)（D-054 至 D-124）及 [docs/decisions/PRODUCT_DECISIONS_DETAILS.md](docs/decisions/PRODUCT_DECISIONS_DETAILS.md)

---

## 部署模型

Xyncra 专为**内网部署**设计，置于反向代理之后：

```text
         互联网
            │
     ┌──────▼──────┐
     │   Nginx /   │  ← TLS 终止、CORS、限流
     │   Envoy     │
     └──────┬──────┘
            │ 内网
     ┌──────▼──────┐
     │   你的应用   │  ← 认证、业务逻辑
     │   服务端     │     注入已认证的 user_id
     └──────┬──────┘
            │
     ┌──────▼──────┐
     │   Xyncra    │  ← 消息 + Agent
     │   Server    │
     └─────────────┘
```

**Xyncra 有意不包含的功能：**

- ❌ 认证 —— 由你的应用服务端通过反向代理处理
- ❌ TLS 终止 —— 由你的反向代理处理
- ❌ CORS / 限流 —— 由你的反向代理处理
- ❌ CSRF 防护 —— 内网部署不需要

**开箱即有的功能：**

- ✅ `user_id` 查询参数认证（开发默认值，可通过 `WSWithAuthenticate` 覆盖）
- ✅ 接受任意 Origin（内网部署模型）
- ✅ 所有配置均支持函数式选项覆盖

📖 设计理念：[docs/decisions/PRODUCT_DECISIONS.md](docs/decisions/PRODUCT_DECISIONS.md)（D-001 至 D-005）

---

## 项目结构

```text
xyncra-server/
├── cmd/
│   ├── xyncra-server/        # 服务端入口
│   └── xyncra-client/        # CLI 客户端入口
├── agents/                   # Agent 定义文件（Markdown + YAML 前置元数据）
├── internal/
│   ├── server/               # WebSocket 服务器，连接生命周期
│   ├── handler/              # RPC 方法处理器
│   ├── agent/                # Agent 运行时、执行器、工具提供者
│   │   └── tools/            # 内建工具实现
│   ├── cli/                  # CLI 客户端实现（命令、输出）
│   ├── mq/                   # 消息队列（Asynq/Redis）
│   ├── store/                # 持久化层（GORM）
│   │   └── model/            # 数据模型
│   ├── cleanup/              # 过期更新清理
│   └── e2e/                  # 端到端集成测试
├── pkg/
│   ├── protocol/             # 线协议类型（可导入）
│   ├── client/               # Go 客户端 SDK
│   └── store/                # 客户端本地存储（通过 GORM 使用 SQLite）
│       └── model/            # 客户端数据模型
├── scripts/                  # Shell 脚本
├── docs/
│   ├── API.md                    # WebSocket 协议参考
│   ├── decisions/
│   │   ├── PRODUCT_DECISIONS.md      # 架构决策
│   │   └── PRODUCT_DECISIONS_DETAILS.md # 详细决策规格
│   ├── design/
│   │   ├── DESIGN_HITL_RESILIENCE.md  # HITL 故障场景与恢复设计
│   │   ├── DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md # 客户端功能工具设计
│   │   └── DESIGN_TYPING_EPHEMERAL_PUSH.md # 输入状态/推送设计
│   ├── guides/
│   │   ├── DEVELOPER_GUIDE.md        # 开发者入门指南
│   │   └── DEVELOPER_REFERENCE.md    # 开发者参考
│   ├── reviews/
│   │   ├── CLIENT_REVIEW.md          # 客户端代码审查报告
│   │   └── REVIEW_CLIENT_QUERY_ARCHITECTURE.md # 客户端查询架构评审
│   ├── CLI_E2E_TEST_STRATEGY.md       # CLI E2E 测试策略
│   ├── CLI_E2E_TEST_STRATEGY_ROUND2.md # CLI E2E 测试策略第二轮
│   ├── IMPLEMENTATION_PHASES.md      # 实现阶段计划
│   ├── manual-test-cases/            # 手动测试用例文档
│   ├── plans/                        # 设计计划
│   ├── testing/                      # 测试文档与报告
│   └── superpowers/                  # AI 生成的规格文档
├── deploy/                           # Docker 与部署配置
│   ├── Dockerfile
│   ├── docker-compose.yml
│   ├── docker-compose.e2e.yml        # E2E 测试环境
│   ├── docker-compose.multi-node.yml # 多节点分布式测试
│   ├── alertmanager/                 # AlertManager 配置
│   ├── grafana/                      # Grafana 仪表盘与数据源
│   ├── loki/                         # Loki 配置
│   ├── prometheus/                   # Prometheus 规则与配置
│   └── promtail/                     # Promtail 配置
└── wiki/                             # 项目 Wiki
```

---

## 开发

```bash
# 单元测试（无需 Redis）
make test

# E2E 测试（需要 Redis 运行于端口 16379）
make test-e2e

# 全部测试
make test-all

# 格式化与静态检查
make fmt
make vet
```

---

## 文档

| 文档                                                         | 描述                                                |
| ------------------------------------------------------------ | --------------------------------------------------- |
| [API 参考](docs/API.md)                                      | 完整的 WebSocket 协议规范                            |
| [产品决策](docs/decisions/PRODUCT_DECISIONS.md)               | 架构决策（D-001 至 D-124，共定义 111 项）             |
| [产品决策详情](docs/decisions/PRODUCT_DECISIONS_DETAILS.md)   | 详细决策规格                                         |
| [开发者指南](docs/guides/DEVELOPER_GUIDE.md)                  | 项目结构、编码规范、操作指南                         |
| [开发者参考](docs/guides/DEVELOPER_REFERENCE.md)              | 开发者参考文档                                       |
| [HITL 韧性设计](docs/design/DESIGN_HITL_RESILIENCE.md)        | HITL 故障场景、恢复矩阵、数据模型                    |
| [手动测试用例](docs/manual-test-cases/)                       | 端到端手动测试场景                                   |
| [包文档](internal/)                                           | 各包设计文档                                         |

---

## 贡献

欢迎贡献！以下是参与方式：

1. **报告 Bug** —— 提交 Issue 并附上复现步骤
2. **建议新功能** —— 提交 Issue 描述你的使用场景
3. **提交 PR** —— Fork、创建分支、实现、测试、提交

贡献代码时请：

- 遵循现有的模式和命名规范（参见[开发者指南](docs/guides/DEVELOPER_GUIDE.md)）
- 在注释中引用产品决策 ID（例如 `D-011`）
- 编写测试 —— Handler 测试使用内存存储，E2E 测试需要 Redis
- 使用 `fmt.Errorf("context: %w", err)` 进行错误包装

---

## 许可证

[MIT](LICENSE) — 版权所有 (c) 2026 PineappleBond
