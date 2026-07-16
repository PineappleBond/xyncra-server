# 环境配置

## 概述

Xyncra Server 使用命令行标志和环境变量两种方式进行配置。命令行标志优先级高于环境变量，方便在 Docker 和开发环境中灵活切换。

## 配置优先级

```
命令行标志 > 环境变量 > 默认值
```

## 环境变量参考

### 核心配置

| 环境变量 | 对应 Flag | 默认值 | 说明 |
|----------|-----------|--------|------|
| `XYNCRA_ADDR` | `-addr` | `:8080` | WebSocket 服务器监听地址 |
| `XYNCRA_REDIS_ADDR` | `-redis-addr` | `localhost:6379` | Redis 服务器地址 |
| `XYNCRA_REDIS_PASSWORD` | `-redis-password` | `""` | Redis AUTH 密码 |
| `XYNCRA_REDIS_DB` | `-redis-db` | `0` | Redis 数据库索引 |
| `XYNCRA_DB_DRIVER` | `-db-driver` | `sqlite` | 数据库驱动（sqlite, postgres, mysql） |
| `XYNCRA_DB_DSN` | `-db-dsn` | `xyncra.db` | 数据库 DSN / 连接字符串 |
| `XYNCRA_MAX_CONNS_PER_USER` | `-max-conns` | `0` | 每用户最大连接数（0=不限） |
| `XYNCRA_AGENTS_DIR` | `-agents-dir` | `agents` | Agent 配置目录路径 |
| `XYNCRA_MAX_FUNCTIONS_PER_DEVICE` | `-max-functions-per-device` | `200` | 每设备最大函数注册数 |

### Agent / LLM 配置

| 环境变量 | 说明 | 使用位置 |
|----------|------|----------|
| `DASHSCOPE_API_KEY` | DashScope（阿里云通义千问）API Key | `.env.agent`, Agent 配置 |
| `OPENAI_API_KEY` | OpenAI API Key | `mcp-bot.md` 配置 |
| `XYNCRA_TEST_LLM_API_KEY` | E2E 测试用 LLM API Key | `.env.test` |
| `XYNCRA_TEST_LLM_BASE_URL` | E2E 测试用 LLM Base URL | `.env.test` |
| `XYNCRA_TEST_LLM_MODEL` | E2E 测试用模型名 | `.env.test` |
| `XYNCRA_TEST_LLM_PROVIDER` | E2E 测试用 Provider | `.env.test` |
| `XYNCRA_LLM_LOG_DIR` | LLM 调用日志输出目录 | `main.go`（可选，不设置则不开启） |

### E2E 测试配置

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `XYNCRA_TEST_REAL_LLM_ENABLED` | `false` | 是否启用真实 LLM 测试 |
| `XYNCRA_TEST_REAL_LLM_TIMEOUT` | `60s` | 真实 LLM 调用超时 |
| `XYNCRA_TEST_REAL_LLM_MAX_TOKENS` | `500` | 真实 LLM 最大 token 数 |

### 构建时注入

以下变量通过 `-ldflags` 注入：

| 变量 | 来源 | 说明 |
|------|------|------|
| `version` | `git describe --tags --always --dirty` | 版本号 |
| `commit` | `git rev-parse --short HEAD` | Git Commit Hash |
| `buildTime` | `date -u '+%Y-%m-%dT%H:%M:%SZ'` | 构建时间 |

## 配置文件格式

### 环境变量文件

项目使用以下 `.env` 文件（均被 `.gitignore` 排除）：

| 文件 | 用途 | 来源 |
|------|------|------|
| `.env.agent` | Agent LLM API 密钥 | 开发者从 `.env.agent.example` 复制 |
| `.env.test` | E2E 测试 LLM 配置 | 开发者从 `.env.test.example` 复制 |

### .env 文件示例

**`.env.agent.example`**：
```bash
# Agent Configuration
# Copy to .env.agent and fill in your API key.
DASHSCOPE_API_KEY=your-api-key-here
```

**`.env.test.example`**：
```bash
# Xyncra Agent E2E Test — Real LLM Configuration (EXAMPLE)
# Copy to .env.test and fill in your credentials.

XYNCRA_TEST_REAL_LLM_ENABLED=true
XYNCRA_TEST_LLM_API_KEY=your-api-key-here
XYNCRA_TEST_LLM_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
XYNCRA_TEST_LLM_MODEL=qwen3.7-plus
XYNCRA_TEST_LLM_PROVIDER=qwen
XYNCRA_TEST_REAL_LLM_TIMEOUT=60s
XYNCRA_TEST_REAL_LLM_MAX_TOKENS=500
```

## Secret 管理

### 当前方案

目前 API Key 直接存储在环境变量文件中：
- `.env.agent` — DashScope API Key（Agent 使用）
- `.env.test` — 测试用 LLM API Key（E2E 测试使用）

这些文件已加入 `.gitignore`，不会被提交到版本控制。

### 推荐方案（生产环境）

对于生产环境，建议使用更安全的密钥管理方案：

1. **Docker Secrets**：在 Docker Compose 中使用 `secrets` 注入
2. **云厂商 Secret Manager**：如 AWS Secrets Manager、阿里云 KMS
3. **Vault**：HashiCorp Vault 集中管理
4. **Kubernetes Secrets**：如部署在 K8s

### 最佳实践

- 开发环境使用 `.env.agent` 文件
- 不在代码中硬编码密钥
- 定期轮换 API Key
- 审计密钥使用情况
- 不同环境使用不同的密钥

## 环境覆盖

### 开发环境

```bash
# 最简启动，使用 SQLite
make build && ./bin/xyncra-server
```

### 测试环境

```bash
# 使用 PostgreSQL
XYNCRA_DB_DRIVER=postgres \
XYNCRA_DB_DSN="host=localhost user=xyncra dbname=xyncra_test sslmode=disable" \
./bin/xyncra-server
```

### Docker 环境

通过 `environment` 字段或 `env_file` 覆盖：

```yaml
services:
  xyncra-server:
    environment:
      - XYNCRA_ADDR=:8080
      - XYNCRA_REDIS_ADDR=redis:6379
      - XYNCRA_REDIS_DB=0
      - XYNCRA_DB_DSN=/data/xyncra.db
```

### 多节点生产环境

```yaml
services:
  xyncra-server:
    environment:
      - XYNCRA_ADDR=:8080
      - XYNCRA_REDIS_ADDR=redis-cluster:6379
      - XYNCRA_DB_DRIVER=postgres
      - XYNCRA_DB_DSN=postgres://xyncra:password@pg-host:5432/xyncra
      - XYNCRA_MAX_CONNS_PER_USER=10
      - XYNCRA_REDIS_PASSWORD=${REDIS_PASSWORD}
    env_file:
      - .env.production  # 包含 LLM API Key
```

## 配置验证

服务器启动时会自动验证关键配置：

```bash
# 验证 Redis 可达性
# 验证数据库连接
# 加载 Agent 配置
# 运行数据库自动迁移
```

如果配置无效，服务器会在启动时立即报错退出，而不是在运行时才失败。

## 常见问题

### 配置未生效

检查：
1. 命令行标志拼写是否正确（`-` 连字符，不是 `_` 下划线）
2. 环境变量是否以 `XYNCRA_` 前缀开头
3. 环境变量和标志的优先级：标志 > 环境变量 > 默认值

### Redis 连接失败

```
failed to create connection store: dial tcp: connect: connection refused
```

**解决方案**：
1. 确认 Redis 是否运行：`redis-cli ping`
2. 确认 `-redis-addr` 或 `XYNCRA_REDIS_ADDR` 配置正确
3. 如果使用 Docker，确认网络连通性

### 数据库连接失败

```
failed to open database: ...
```

**解决方案**：
1. 确认 `-db-driver` 参数正确
2. 确认 `-db-dsn` 连接字符串格式正确
3. 确认数据库服务已启动
4. 对于 SQLite，确认目标路径可写
