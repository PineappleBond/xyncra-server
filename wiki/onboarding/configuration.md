---
last_updated: 2026-07-17
---

# 配置参考

> Xyncra 遵循"零配置"设计哲学——开箱即用，所有选项都有合理的默认值。
> 配置优先级：命令行标志 > 环境变量 > 默认值（D-034）。

---

## 目录

- [配置方式概览](#配置方式概览)
- [服务器配置](#服务器配置)
- [客户端配置](#客户端配置)
- [Agent 配置](#agent-配置)
- [环境特定配置](#环境特定配置)
- [配置模板](#配置模板)

---

## 配置方式概览

Xyncra 支持三种配置方式，优先级从高到低：

### 1. 命令行标志（最高优先级）

```bash
# 服务器
./bin/xyncra-server -addr :9090 -db-driver postgres -db-dsn "host=..."

# 客户端
./bin/xyncra-client listen --user-id alice --device-id laptop --server ws://prod.example.com/ws
```

### 2. 环境变量

所有配置项都有对应的 `XYNCRA_` 前缀环境变量：

```bash
export XYNCRA_ADDR=:9090
export XYNCRA_DB_DRIVER=postgres
export XYNCRA_DB_DSN="host=localhost user=xyncra dbname=xyncra port=5432"
./bin/xyncra-server
```

### 3. 默认值（最低优先级）

当标志和环境变量都未设置时，使用编译时的默认值。所有默认值都针对**本地开发**优化。

### 配置文件

除了上述方式，还可以通过外部工具使用 `.env` 文件。服务器本身不会自动加载 `.env` 文件，但可以通过 `docker-compose` 的 `env_file` 或 `direnv` 等方式使用：

```bash
# .env 文件示例（需要配合外部工具加载）
XYNCRA_ADDR=:8080
XYNCRA_REDIS_ADDR=localhost:6379
XYNCRA_DB_DRIVER=sqlite
XYNCRA_DB_DSN=xyncra.db
```

完整的服务器环境变量列表见 [环境配置](../devops/environment-config.md)。

---

## 服务器配置

### WebSocket 服务器

| 标志 | 环境变量 | 默认值 | 说明 |
|------|---------|--------|------|
| `-addr` | `XYNCRA_ADDR` | `:8080` | WebSocket 监听地址 |
| `-max-conns` | `XYNCRA_MAX_CONNS_PER_USER` | `0`（无限制） | 每用户最大连接数 |
| `-max-functions-per-device` | `XYNCRA_MAX_FUNCTIONS_PER_DEVICE` | `200` | 每设备最大注册函数数 |

### Redis 配置

| 标志 | 环境变量 | 默认值 | 说明 |
|------|---------|--------|------|
| `-redis-addr` | `XYNCRA_REDIS_ADDR` | `localhost:6379` | Redis 服务器地址 |
| `-redis-password` | `XYNCRA_REDIS_PASSWORD` | 空 | Redis AUTH 密码 |
| `-redis-db` | `XYNCRA_REDIS_DB` | `0` | Redis 数据库编号 |

Redis 用于三个独立目的，每个使用独立的 `redis.Client` 连接：

| 用途 | 说明 |
|------|------|
| **ConnectionStore** | 连接跟踪、设备管理、连接 TTL |
| **NodeBroadcaster** | 跨节点消息广播（Redis Pub/Sub，需独占连接） |
| **Idempotency/Checkpoint/Pending** | Agent 幂等性、HITL checkpoint、reverse-RPC pending store、会话锁 |

### 数据库配置

| 标志 | 环境变量 | 默认值 | 说明 |
|------|---------|--------|------|
| `-db-driver` | `XYNCRA_DB_DRIVER` | `sqlite` | 数据库驱动：`sqlite`、`postgres`、`mysql` |
| `-db-dsn` | `XYNCRA_DB_DSN` | `xyncra.db` | 数据库连接字符串 |

DSN 示例：

```text
# SQLite（默认）
xyncra.db
/data/xyncra.db

# PostgreSQL
host=localhost user=xyncra password=secret dbname=xyncra port=5432 sslmode=disable

# MySQL
xyncra:secret@tcp(localhost:3306)/xyncra?parseTime=true
```

### Agent 配置

| 标志 | 环境变量 | 默认值 | 说明 |
|------|---------|--------|------|
| `-agents-dir` | `XYNCRA_AGENTS_DIR` | `agents` | Agent 定义文件目录路径 |

### LLM 日志

| 环境变量 | 说明 |
|---------|------|
| `XYNCRA_LLM_LOG_DIR` | LLM 调用日志目录（JSONL 格式），不设置则不记录 |

LLM 日志文件路径：`{XYNCRA_LLM_LOG_DIR}/llm-calls.log`

每条日志记录的 JSON 格式：
```json
{
  "timestamp": "...",
  "agent_id": "weather-bot",
  "model": "qwen3.7-plus",
  "request": {"messages": [...], "tools": [...]},
  "response": {"content": "...", "tool_calls": [...]},
  "duration_ms": 1234,
  "token_usage": {"prompt": 100, "completion": 50, "total": 150}
}
```

### 分布式追踪

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `XYNCRA_TRACING_ENABLED` | `false` | 是否启用 OpenTelemetry 链路追踪 |
| `XYNCRA_TRACING_OTLP_ENDPOINT` | `localhost:4317` | OTLP/gRPC collector 地址 |
| `XYNCRA_TRACING_OTLP_INSECURE` | `true` | 是否禁用 OTLP Exporter TLS |
| `XYNCRA_TRACING_SAMPLING_RATE` | `1.0` | 采样率（0.0-1.0），1.0 表示全量采样 |
| `XYNCRA_TRACING_SERVICE_NAME` | `xyncra-server` | 服务名称（Jaeger 中显示） |
| `XYNCRA_TRACING_DEBUG_USERS` | 空 | 强制采样的用户 ID，逗号分隔 |
| `XYNCRA_TRACING_DEBUG_DEVICES` | 空 | 强制采样的设备 ID，逗号分隔 |

命令行标志（覆盖环境变量）：

| 标志 | 说明 |
|------|------|
| `-tracing-enabled` | 启用追踪 |
| `-tracing-endpoint` | OTLP endpoint |
| `-tracing-sampling-rate` | 采样率 |

启用后，追踪数据通过 OTLP/gRPC 导出到配置的 collector（如 Jaeger）。详见 [分布式追踪](../observability/distributed-tracing.md)。

### E2E 测试端口约定

| 环境 | Redis 端口 | 服务器端口 | Redis DB |
|------|-----------|-----------|----------|
| 开发/生产 | `6379` | `8080` | `0` |
| E2E 测试 | `16379` | `18080` | `15` |

E2E 端口通过 `Makefile` 中的常量定义（D-043），避免与本地开发环境冲突。

---

## 客户端配置

### 全局标志

所有 `xyncra-client` 子命令共享的全局标志：

| 标志 | 环境变量 | 默认值 | 说明 |
|------|---------|--------|------|
| `--user-id` / `-u` | `XYNCRA_USER_ID` | 必填 | 用户 ID |
| `--device-id` | `XYNCRA_DEVICE_ID` | 必填 | 设备 ID |
| `--server` / `-s` | `XYNCRA_SERVER` | `ws://localhost:8080/ws` | WebSocket 服务器 URL |
| `--db-path` | `XYNCRA_DB_PATH` | `~/.xyncra/{user}/{device}/xyncra.db` | 客户端本地数据库路径 |
| `--log-dir` | `XYNCRA_LOG_DIR` | `~/.xyncra/{user}/{device}/logs/` | 客户端日志目录 |

### 客户端数据目录结构

```
~/.xyncra/
└── {user_id}/
    └── {device_id}/
        ├── xyncra.db          # 本地 SQLite 数据库
        ├── logs/              # 日志目录
        │   ├── rpc.log
        │   └── notification.log
        └── xyncra.lock        # 进程锁文件（防止多实例）
```

### 客户端内部配置默认值

这些选项可通过 Go SDK 的 `ClientOption` 函数覆盖：

| 选项 | 默认值 | 说明 |
|------|--------|------|
| RPC 超时 | 30s | 单次 RPC 调用的超时时间 |
| Heartbeat 间隔 | 30s | 心跳发送间隔 |
| 同步批次大小 | 100 | 每次拉取的更新数量 |
| Pull 防抖 | 500ms | 合并拉取请求的防抖窗口 |
| 重试基础延迟 | 1s | 首次重试前的等待时间 |
| 重试最大次数 | 5 | RPC 重试的最大尝试次数 |
| 重连基础延迟 | 1s | 首次重连前的等待时间 |
| 重连最大延迟 | 30s | 指数退避的最大重连延迟 |
| 幂等缓存大小 | 1024 | 幂等 key LRU 缓存容量 |
| RTT 窗口大小 | 50 | RTT 采样的滑动窗口大小 |
| 自适应超时最小 | 5s | 自适应超时的下限 |
| 自适应超时最大 | 120s | 自适应超时的上限 |

### 调试模式

```bash
# 启用调试日志
export XYNCRA_DEBUG=1

# 测试环境覆盖重连参数
export XYNCRA_TEST_RECONNECT_BASE_DELAY=100ms
export XYNCRA_TEST_RECONNECT_MAX_DELAY=5s
```

---

## Agent 配置

Agent 使用 Markdown 文件的 YAML 前置元数据定义。所有配置项：

### `agents/weather-bot.md` 示例

```markdown
---
id: weather-bot                                    # Agent 唯一标识符
name: Weather Bot                                  # Agent 显示名称
description: "Provides weather information"        # Agent 描述
model: qwen3.7-plus                                # LLM 模型名称
api_key_env: DASHSCOPE_API_KEY                     # API Key 的环境变量名
base_url: "https://coding.dashscope.aliyuncs.com/v1"  # LLM API 基础 URL
parameters:
  temperature: 0.7                                 # 生成温度
  max_tokens: 2000                                 # 最大生成 token 数
  top_p: 0.9                                       # Top-p 采样
context:
  max_tokens: 8000                                 # 上下文最大 token 数
  max_messages: 20                                 # 上下文最大消息数
tools:
  - get_weather                                    # 可用工具列表
  - get_current_time
  - retrieve_tool_result
middleware:
  enable_client_tools: true                        # 启用客户端工具（ReverseRPC）
  enable_patch_tool_calls: true                    # 启用工具调用修补
  enable_summarization: true                       # 启用上下文总结
  summarization_tokens: 160000                      # 总结触发阈值
  enable_tool_reduction: true                      # 启用工具结果精简
  tool_reduction_max_chars: 50000                  # 工具结果最大字符数
timeout:
  execution: 120s                                  # Agent 执行超时
  idle: 300s                                       # 空闲超时
---
```

### Agent 配置字段参考

#### 基本信息

| 字段 | 必填 | 说明 |
|------|------|------|
| `id` | 是 | Agent 唯一标识符，用于 RPC 路由和内部引用 |
| `name` | 否 | Agent 显示名称 |
| `description` | 否 | Agent 功能描述 |

#### LLM 配置

| 字段 | 必填 | 说明 |
|------|------|------|
| `model` | 是 | LLM 模型标识符，如 `gpt-4o`、`claude-3-opus`、`qwen3.7-plus` |
| `api_key_env` | 是 | 包含 API Key 的环境变量名（不直接在配置文件中写入 Key） |
| `base_url` | 否 | LLM API 的基础 URL（兼容 OpenAI 格式） |
| `parameters` | 否 | LLM 调用参数（temperature、max_tokens、top_p 等） |
| `context` | 否 | 上下文管理参数（max_tokens、max_messages） |

支持的大模型提供商：

| 提供商 | model 示例 | 所需环境变量 |
|--------|-----------|-------------|
| OpenAI | `gpt-4o`、`gpt-4o-mini` | `OPENAI_API_KEY` |
| Anthropic | `claude-3-opus`、`claude-3-sonnet` | `ANTHROPIC_API_KEY` |
| Alibaba DashScope | `qwen3.7-plus`、`qwen3.2-max` | `DASHSCOPE_API_KEY` |
| Ollama（本地） | `llama3`、`mistral` | 无需 API Key |

#### 工具配置

| 字段 | 说明 |
|------|------|
| `tools` | Agent 可调用的工具列表（服务端内置工具名） |
| `middleware.enable_client_tools` | 是否允许 Agent 调用客户端设备注册的函数 |
| `middleware.enable_patch_tool_calls` | 是否启用工具调用结果修补 |

服务端内置工具：

| 工具名 | 说明 |
|--------|------|
| `get_weather` | 获取城市天气信息 |
| `get_current_time` | 获取当前时间 |
| `ask_user` | 向用户提问（HITL 工具） |
| `retrieve_tool_result` | 检索之前的工具调用结果 |

客户端工具通过 `system.register_functions` RPC 注册，Agent 可通过 ReverseRPC 调用。

#### 运行时配置

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `max_concurrent` | `10` | 最大并行 Agent 执行数（服务器全局） |
| `timeout.execution` | `120s` | Agent 单次执行的超时时间 |
| `timeout.idle` | `300s` | Agent 空闲超时时间 |

### 内置 Agent 文件

| 文件 | 说明 |
|------|------|
| `agents/weather-bot.md` | 天气查询 Bot（使用 DashScope Qwen 模型 + 天气工具） |
| `agents/hitl-bot.md` | HITL（Human-in-the-Loop）测试 Bot |
| `agents/hitl-parent.md` | HITL 父 Agent（用于子 Agent 委派测试） |
| `agents/hitl-child-a.md` | HITL 子 Agent A |
| `agents/hitl-child-b.md` | HITL 子 Agent B |
| `agents/mcp-bot.md` | MCP 工具集成 Bot |

---

## 环境特定配置

### 开发环境

```bash
# 开发配置——默认值即可
XYNCRA_ADDR=:8080
XYNCRA_DB_DRIVER=sqlite
XYNCRA_DB_DSN=xyncra.db
XYNCRA_REDIS_ADDR=localhost:6379
```

### 测试环境

```bash
# E2E 测试配置（使用非默认端口避免冲突）
XYNCRA_ADDR=:18080
XYNCRA_REDIS_ADDR=localhost:16379
XYNCRA_REDIS_DB=15
XYNCRA_DB_DRIVER=sqlite
XYNCRA_DB_DSN=/app/xyncra-e2e.db
XYNCRA_LLM_LOG_DIR=/app/llm-logs
```

### 生产环境

```bash
# 生产配置示例
XYNCRA_ADDR=:8080
XYNCRA_DB_DRIVER=postgres
XYNCRA_DB_DSN="host=10.0.0.1 user=xyncra password=... dbname=xyncra sslmode=require"
XYNCRA_REDIS_ADDR=10.0.0.2:6379
XYNCRA_REDIS_PASSWORD=...
XYNCRA_REDIS_DB=0
XYNCRA_MAX_CONNS_PER_USER=5
XYNCRA_AGENTS_DIR=/etc/xyncra/agents
XYNCRA_MAX_FUNCTIONS_PER_DEVICE=200
XYNCRA_LLM_LOG_DIR=/var/log/xyncra/llm
```

生产环境建议：
- 使用 PostgreSQL 而非 SQLite（更好的并发性能）
- 使用 Redis AUTH 密码保护
- 设置 `XYNCRA_MAX_CONNS_PER_USER` 防止连接滥用
- 将 Agent 配置文件目录放在版本控制之外
- 启用 LLM 日志用于审计和调试
- 启用分布式追踪（`XYNCRA_TRACING_ENABLED=true`）并部署 Jaeger 用于链路排查
- 在反向代理层处理 TLS（Xyncra 不处理 TLS）

---

## 配置模板

服务器运行时配置通过环境变量和命令行 flags 传入，完整列表见 [环境配置](../devops/environment-config.md)。

Agent 和测试相关的配置文件：

- `.env` — Agent LLM API 密钥 + E2E 测试 LLM 配置（从 `.env.example` 复制）

---

```bash
# ============================================================
# Xyncra Server - 全部可配置项
# ============================================================

# --- WebSocket Server ---
XYNCRA_ADDR=:8080
XYNCRA_MAX_CONNS_PER_USER=0
XYNCRA_MAX_FUNCTIONS_PER_DEVICE=200

# --- Redis ---
XYNCRA_REDIS_ADDR=localhost:6379
XYNCRA_REDIS_PASSWORD=
XYNCRA_REDIS_DB=0

# --- Database ---
XYNCRA_DB_DRIVER=sqlite
XYNCRA_DB_DSN=xyncra.db

# --- Agent ---
XYNCRA_AGENTS_DIR=agents

# --- LLM Logging ---
# XYNCRA_LLM_LOG_DIR=/var/log/xyncra/llm

# --- Distributed Tracing ---
XYNCRA_TRACING_ENABLED=false
XYNCRA_TRACING_OTLP_ENDPOINT=localhost:4317
XYNCRA_TRACING_OTLP_INSECURE=true
XYNCRA_TRACING_SAMPLING_RATE=1.0
XYNCRA_TRACING_SERVICE_NAME=xyncra-server
# XYNCRA_TRACING_DEBUG_USERS=alice,bob
# XYNCRA_TRACING_DEBUG_DEVICES=dev1,dev2

# --- Client ---
XYNCRA_USER_ID=
XYNCRA_DEVICE_ID=
XYNCRA_SERVER=ws://localhost:8080/ws
XYNCRA_DB_PATH=
XYNCRA_LOG_DIR=
```
