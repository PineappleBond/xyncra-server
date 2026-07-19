---
last_updated: 2026-07-17
---

# 快速开始

> last_updated: 2026-07-17
>
> 在 5 分钟内跑通 Xyncra 从启动到发送第一条消息的完整链路。

---

## 目录

- [前置要求](#前置要求)
- [方式一：Docker Compose 一键启动](#方式一docker-compose-一键启动)
- [方式二：从源码构建](#方式二从源码构建)
- [连接并发送第一条消息](#连接并发送第一条消息)
- [创建第一个 AI Agent](#创建第一个-ai-agent)
- [端到端 Hello World](#端到端-hello-world)
- [故障排除](#故障排除)

---

## 前置要求

### Docker 方式（推荐）

- **Docker** 24.0+
- **Docker Compose** v2.0+
- 端口 `8080` 未被占用（或自定义映射）

### 源码构建方式

- **Go** 1.26+
- **Redis** 7.x，运行在 `localhost:6379`（默认端口）
- **Make**（可选，用于使用 Makefile 快捷操作）

---

## 方式一：Docker Compose 一键启动

这是最快的方式，无需安装 Go 和 Redis。

```bash
# 1. 克隆仓库
git clone https://github.com/PineappleBond/xyncra-server.git
cd xyncra-server

# 2. 一键启动（服务器 + Redis）
docker compose -f deploy/docker-compose.yml up -d

# 3. 验证服务器正在运行
curl http://localhost:8080/health

# 4. 查看日志
docker compose -f deploy/docker-compose.yml logs -f xyncra-server
```

启动成功后，服务器会在 `:8080` 监听 WebSocket 连接。

### Docker Compose 架构

`deploy/docker-compose.yml` 定义了两个服务：

| 服务 | 镜像 | 端口 | 说明 |
|------|------|------|------|
| `xyncra-server` | 本地构建 | `8080:8080` | WebSocket 消息服务器 + Agent 运行时 |
| `redis` | `redis:7-alpine` | `6379` | 连接存储、消息队列、节点广播 |

默认配置：
- 数据库：SQLite（存储在 Docker volume `xyncra-data` 中）
- Redis 地址：`redis:6379`
- 监听地址：`:8080`

### 停止环境

```bash
docker compose -f deploy/docker-compose.yml down
```

---

## 方式二：从源码构建

### 1. 确保 Redis 已启动

```bash
# macOS（Homebrew）
brew install redis && brew services start redis

# 或直接运行
redis-server
```

### 2. 构建并启动

```bash
# 克隆仓库
git clone https://github.com/PineappleBond/xyncra-server.git
cd xyncra-server

# 构建服务器和客户端二进制（输出到 ./bin/）
make build

# 启动服务器（零配置：SQLite + Redis localhost:6379）
./bin/xyncra-server
```

看到如下日志表示启动成功：

```
starting xyncra-server dev (unknown) built 2026-07-16T... on :8080
database migrated successfully
loaded 0 agent configuration(s)
```

### 3. 验证健康检查

```bash
curl http://localhost:8080/health
# 期望输出：{"status":"ok"}
```

### 常用构建命令

| 命令 | 说明 |
|------|------|
| `make build` | 构建服务器和客户端 |
| `make build-server` | 仅构建服务器 |
| `make build-client` | 仅构建客户端 |
| `make docker-build` | 构建 Docker 镜像 |
| `make release` | 交叉编译（Linux/Darwin/Windows × amd64/arm64） |

---

## 连接并发送第一条消息

### 启动 CLI 守护进程

```bash
# 启动客户端守护进程（保持 WebSocket 长连接）
./bin/xyncra-client listen --user-id alice --device-id laptop
```

守护进程会自动：
- 建立 WebSocket 连接到 `ws://localhost:8080/ws?user_id=alice&device_id=laptop`
- 注册内置函数（`ping`、`get_device_info`、`get_time`）
- 开始接收实时推送更新

### 在新终端中发送消息

**步骤 1：创建一个会话**

```bash
./bin/xyncra-client create-conversation --user-id alice --peer-id bob
```

输出示例：
```
Conversation created.
  ID: conv-xxxx-xxxx-xxxx
  Peer: bob
  Duplicate: false
```

记下输出的 `ID`，后续操作需要用到。

**步骤 2：发送消息**

```bash
./bin/xyncra-client send --conversation-id <conv-id> --content "Hello, Bob!"
```

输出示例：
```
Message sent.
  Message ID: 1
  UUID: msg-xxxx-xxxx-xxxx
  Conversation: conv-xxxx-xxxx-xxxx
  Client Msg ID: 550e8400-...
  Duplicate: false
```

**步骤 3：查看消息历史**

```bash
# 列出所有会话
./bin/xyncra-client list-conversations

# 查看会话中的消息
./bin/xyncra-client get-messages --conversation-id <conv-id>

# 搜索消息
./bin/xyncra-client search-messages --conversation-id <conv-id> --query "Hello"
```

**步骤 4：多设备同步测试**

在新终端中启动第二个客户端实例：

```bash
./bin/xyncra-client listen --user-id alice --device-id phone
```

在 `laptop` 客户端发送的消息会实时推送到 `phone` 客户端。

---

## 创建第一个 AI Agent

Agent 定义是简单的 Markdown 文件 + YAML 前置元数据——无需编写任何 Go 代码。

### 内置天气 Bot

项目已包含一个示例 Agent：`agents/weather-bot.md`：

```markdown
---
id: weather-bot
name: Weather Bot
model: qwen3.7-plus
api_key_env: DASHSCOPE_API_KEY
base_url: "https://coding.dashscope.aliyuncs.com/v1"
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

### 配置 API Key

在 `.env` 文件中设置 LLM 提供商 API Key：

```bash
DASHSCOPE_API_KEY=sk-your-api-key-here
```

### 创建自己的 Agent

在 `agents/` 目录下创建一个新的 `.md` 文件：

```markdown
---
id: my-assistant
name: My Assistant
model: qwen3.7-plus
api_key_env: DASHSCOPE_API_KEY
tools:
  - get_current_time
middleware:
  enable_client_tools: true
---

You are a helpful assistant. Answer questions concisely.
```

### 热加载 Agent

```bash
# 通过 CLI 触发热加载
./bin/xyncra-client reload-agents
```

或直接通过 WebSocket RPC：

```json
{"type": 0, "data": {"id": "ra-001", "method": "reload_agents", "params": {}}}
```

---

## 端到端 Hello World

完整的 Hello World 演练：

### 1. 启动环境

```bash
docker compose -f deploy/docker-compose.yml up -d
```

### 2. 启动两个用户的守护进程

终端 1：

```bash
./bin/xyncra-client listen --user-id alice --device-id macbook
```

终端 2：

```bash
./bin/xyncra-client listen --user-id bob --device-id macbook
```

### 3. Alice 创建会话并发送消息

```bash
# Alice 创建与 Bob 的会话
./bin/xyncra-client create-conversation --user-id alice --peer-id bob
# 输出：ID = conv-abc123

# Alice 发送消息
./bin/xyncra-client send --conversation-id conv-abc123 --content "Hello, Bob!"
```

### 4. 观察实时推送

在 Bob 的终端中，应该看到：

```
[new message] seq=1 from=alice conv=conv-abc123 "Hello, Bob!"
```

### 5. Bob 回复

```bash
# Bob 发送回复
./bin/xyncra-client send --user-id bob --conversation-id conv-abc123 --content "Hi Alice!"
```

在 Alice 的终端中，应该看到：

```
[new message] seq=2 from=bob conv=conv-abc123 "Hi Alice!"
```

### 6. 与 Agent 对话

```bash
# 确保 Agent 已加载
./bin/xyncra-client reload-agents

# 向 weather-bot 发送消息（需要事先创建与 agent 的会话）
./bin/xyncra-client send --conversation-id conv-agent-xxx --content "北京的天气怎么样？"
```

Agent 的回复会通过 streaming 推送实时显示：

```
[agent] user=agent:weather-bot conv=conv-xxx stream=stream-xxx status=streaming text="北京..."
[agent] user=agent:weather-bot conv=conv-xxx stream=stream-xxx status=done text="北京当前..."
[new message] seq=3 from=agent:weather-bot conv=conv-xxx "北京当前..."
```

---

## 故障排除

### 服务器无法启动

| 症状 | 检查项 |
|------|--------|
| `failed to open database` | SQLite 依赖 `CGO_ENABLED=1`，确保未在 `CGO_ENABLED=0` 下构建 |
| `failed to create connection store` | Redis 未运行或地址错误，检查 `XYNCRA_REDIS_ADDR` |
| `address already in use` | 端口 `:8080` 被占用，使用 `-addr :8081` 或 `XYNCRA_ADDR=:8081` 更改 |
| `failed to auto-migrate database` | 数据库文件权限问题，检查 DSN 路径是否可写 |
| 配置未生效（如端口、Redis 地址与预期不符） | 环境变量优先级：CLI 参数 > 环境变量 > 默认值。检查 `.env` 文件是否存在，或变量是否正确 `export`；使用 `echo $VAR` 验证当前值 |

### 客户端无法连接

| 症状 | 检查项 |
|------|--------|
| `dial websocket: connection refused` | 服务器未启动或端口错误，检查 `--server` 或 `XYNCRA_SERVER` |
| `user-id is required` | 缺少 `--user-id` 参数或 `XYNCRA_USER_ID` 环境变量 |
| `device-id is required` | 缺少 `--device-id` 参数或 `XYNCRA_DEVICE_ID` 环境变量 |
| 连接被拒绝（4001 close frame） | 同一 `(user_id, device_id)` 的新连接会替换旧连接，这是预期的行为 |

### Agent 不响应

| 症状 | 检查项 |
|------|--------|
| Agent 未加载 | 检查日志 `loaded 0 agent configuration(s)`，确认 `agents/` 目录有 `.md` 文件 |
| LLM 调用失败 | 检查 `api_key_env` 对应的环境变量是否已设置且有效 |
| 工具调用失败 | Agent 的 `tools` 列表中的工具名必须与已注册的工具名一致 |

### 快速检查清单

```bash
# 1. 服务器健康
curl http://localhost:8080/health

# 2. Redis 连接
redis-cli -p 6379 ping

# 3. 数据库文件
ls -la xyncra.db

# 4. Agent 配置
ls -la agents/

# 5. 服务器日志
docker compose -f deploy/docker-compose.yml logs xyncra-server
```
