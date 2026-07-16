# 开发环境搭建

## 前置条件

### 必需软件

| 软件 | 最低版本 | 用途 |
|------|----------|------|
| Go | 1.22+（实际使用 1.26） | 编译运行 |
| Redis | 7.x | 连接存储、消息队列、Pub/Sub |
| Docker | 24.x+ | E2E 测试环境、生产容器化 |
| Docker Compose | v2.x+ | 多容器编排 |
| Git | 2.x+ | 版本控制 |

### 推荐工具

- `gofmt` — Go 代码格式化（随 Go 安装）
- `nc` (netcat) — 端口检测（测试脚本使用）
- `curl` — HTTP 健康检查

## 快速开始

### 1. 克隆仓库

```bash
git clone https://github.com/PineappleBond/xyncra-server.git
cd xyncra-server
```

### 2. 编译

```bash
make build
```

编译产物在 `bin/` 目录下：

```
bin/
├── xyncra-server    # 服务端二进制
└── xyncra-client    # CLI 客户端二进制
```

编译信息通过 `-ldflags` 注入版本、commit 和构建时间。

### 3. 启动 Redis

```bash
# 使用 Docker 启动 Redis
docker run -d --name xyncra-redis -p 6379:6379 redis:7-alpine

# 验证 Redis
redis-cli ping  # 应返回 PONG
```

### 4. 启动服务器

```bash
# 默认配置（SQLite + Redis localhost:6379）
./bin/xyncra-server

# 使用自定义配置
./bin/xyncra-server \
    -addr :9090 \
    -redis-addr localhost:6379 \
    -db-driver sqlite \
    -db-dsn ./data/xyncra.db

# 使用环境变量
export XYNCRA_ADDR=:8080
export XYNCRA_REDIS_ADDR=localhost:6379
export XYNCRA_DB_DRIVER=sqlite
./bin/xyncra-server
```

服务器启动流程：
1. 初始化数据库（SQLite 默认 `xyncra.db`）
2. 执行自动迁移（创建表、索引）
3. 连接 Redis（ConnectionStore）
4. 初始化 MQ Broker（Asynq）
5. 初始化 Node Broadcaster（Redis Pub/Sub，为多节点准备）
6. 加载 Agent 配置（`agents/` 目录）
7. 启动 MQ Worker Pool
8. 启动后台清理协程（context cache、tool results、user updates、HITL）
9. 启动 HTTP 服务器，监听 WebSocket 升级

### 5. 验证服务

```bash
curl http://localhost:8080/health
# 返回：{"status":"ok","connections":0}
```

## 使用 Makefile

Xyncra 提供完整的 Makefile 自动化开发流程：

```bash
# 编译（服务器 + 客户端）
make

# 或显式编译
make build
make build-server    # 仅编译服务端
make build-client    # 仅编译客户端

# 运行单元测试（无需 Redis）
make test

# 运行 E2E 测试（需要 Redis 16379 端口）
make test-e2e

# 运行 CLI E2E 测试（需要 Docker E2E 环境）
make test-cli-e2e

# 运行所有测试
make test-all

# 代码质量
make fmt      # gofmt 格式化
make vet      # 静态分析
make tidy     # 整理依赖

# 清理构建产物
make clean

# 跨平台发布（6 个平台）
make release
# 产物在 dist/ 目录：
# xyncra-server-linux-amd64, xyncra-server-darwin-arm64, 等

# Docker
make docker-build      # 构建镜像
make docker-up         # 启动生产环境
make docker-down       # 停止生产环境
make docker-e2e-up     # 启动 E2E 环境
make docker-e2e-down   # 停止 E2E 环境
```

## Docker 开发环境

### 生产环境

```bash
# 启动（Redis + Server）
make docker-up

# 查看日志
docker compose logs -f

# 停止
make docker-down
```

`docker-compose.yml` 定义了两个服务：
- `xyncra-server` — 构建当前代码，监听 8080 端口，使用 SQLite
- `redis` — Redis 7-alpine，健康检查

### E2E 测试环境

```bash
# 启动 E2E 环境
make docker-e2e-up
# Redis 在 16379 端口，Server 在 18080 端口

# 运行 E2E 测试
make test-e2e

# 清理
make docker-e2e-down
```

E2E 环境使用独立的端口和数据库（Redis DB 15），避免干扰开发环境。

## 运行测试

### 单元测试

所有不需要 Redis 的测试使用 `-short` flag 隔离：

```bash
make test
# 等价于：go test -short ./...
```

覆盖的包范围：`internal/server`、`internal/handler`、`internal/agent`、`internal/mq`、`internal/store`、`internal/cli`、`pkg/protocol`、`pkg/client`。

### 服务端 E2E 测试

```bash
# 确保 Redis 在 16379 端口运行
make docker-e2e-up

# 运行 E2E
make test-e2e
# 等价于：go test -v ./internal/e2e/
```

E2E 测试套件覆盖：

| 测试组 | 覆盖内容 |
|--------|----------|
| `fullchain_e2e_test.go` | 完整消息投递流程 |
| `fullchain_concurrent_e2e_test.go` | 并发消息处理 |
| `fullchain_delivery_e2e_test.go` | 消息投递可靠性 |
| `fullchain_error_e2e_test.go` | 错误处理路径 |
| `fullchain_reconnect_e2e_test.go` | 重连场景 |
| `agent_basic_test.go` | Agent 基础功能 |
| `agent_streaming_test.go` | Agent 流式输出 |
| `agent_hitl_test.go` | HITL 完整流程 |
| `agent_concurrent_test.go` | Agent 并发处理 |
| `agent_edge_test.go` | Agent 边缘情况 |
| `agent_weaknet_test.go` | 弱网络环境 |
| `agent_reload_test.go` | Agent 热重载 |

### CLI E2E 测试

```bash
# 构建客户端 + 启动 E2E 环境
make build-client
make docker-e2e-up

# 运行 CLI E2E
make test-cli-e2e
# 等价于：go test -v ./internal/cli/e2e/
```

### 运行单个测试

```go
// 运行特定的测试函数
go test -run TestSendMessage -v ./internal/handler/

// 运行测试并检查竞态条件
go test -race -run TestConcurrentAccess -v ./internal/agent/
```

### 测试脚本

`scripts/test.sh` 提供一键测试：

```bash
./scripts/test.sh
```

该脚本自动：
1. 检查 Redis 容器是否运行（`xyncra-test-redis`）
2. 如未运行，启动 Redis 容器在 16379 端口
3. 运行所有测试（含 race 检测）

## 代码质量检查

```bash
# 格式化代码
make fmt

# 静态分析
make vet

# 手动检查（推荐在提交前执行）
make fmt && make vet && make test
```

## 配置详解

### 数据库配置

支持三种数据库驱动，通过 `-db-driver` 和 `-db-dsn` 指定：

```bash
# SQLite（开发默认）
-db-driver sqlite -db-dsn xyncra.db

# PostgreSQL
-db-driver postgres -db-dsn "host=localhost user=xyncra password=secret dbname=xyncra port=5432 sslmode=disable"

# MySQL
-db-driver mysql -db-dsn "xyncra:secret@tcp(localhost:3306)/xyncra?parseTime=true"
```

### 自定义认证

默认认证从 URL query parameter 提取 `user_id`。生产环境应使用自定义认证：

```go
// 在 main.go 中替换默认认证
srv, _ := server.NewWebSocketServer(
    server.WSWithAuthenticate(func(r *http.Request) (string, error) {
        // 从 JWT token、cookie 或 header 中提取用户 ID
        token := r.Header.Get("Authorization")
        userID, err := validateJWT(token)
        return userID, err
    }),
)
```

### LLM 日志

启用 LLM 调用日志以调试 Agent 行为：

```bash
export XYNCRA_LLM_LOG_DIR=./llm-logs
./bin/xyncra-server
# 日志写入 ./llm-logs/llm-calls.log（JSONL 格式）
```

### Agent 配置目录

Agent 定义文件存放在 `agents/` 目录（可通过 `-agents-dir` 自定义）：

```bash
# 自定义 agent 目录
./bin/xyncra-server -agents-dir /etc/xyncra/agents

# 热重载（通过 RPC）
./bin/xyncra-client reload-agents
```

## 常见问题

### Redis 连接失败

```
ERROR: Redis is not reachable at localhost:6379
```

**解决方案：**
```bash
# 检查 Redis 是否运行
redis-cli ping

# 启动 Redis
docker start xyncra-redis  # 或 docker run -d -p 6379:6379 redis:7-alpine
```

### 端口冲突

```
listen tcp :8080: bind: address already in use
```

**解决方案：**
```bash
# 使用其他端口
./bin/xyncra-server -addr :9090

# 或查找占用进程
lsof -i :8080
kill <PID>
```

### SQLite 数据库锁定

```
database is locked (SQLite)
```

**说明：** SQLite 序列化写操作。如果遇到并发写竞争，考虑：
1. 减少连接池大小（SQLite 模式下默认最大连接数已设为 1）
2. 迁移到 PostgreSQL 或 MySQL 进行生产部署

### E2E 测试失败，Redis 端口不匹配

```
ERROR: Redis is not reachable at localhost:16379
```

**解决方案：**
```bash
make docker-e2e-up
# 验证：redis-cli -p 16379 ping
```

### Agent LLM 调用失败

Agent 需要配置 LLM API key。检查环境变量：

```bash
# 确保设置了 API key
echo $DASHSCOPE_API_KEY

# 或在启动前设置
export DASHSCOPE_API_KEY=sk-your-key-here
./bin/xyncra-server
```

### 编译错误：go 版本不匹配

```
go: go.mod requires go >= 1.26
```

升级 Go 版本：
```bash
# macOS
brew upgrade go

# 或手动下载：https://go.dev/dl/
```

### 测试超时

```
panic: test timed out after 10m0s
```

E2E 测试可能因网络问题超时。增加超时时间：

```bash
go test -timeout 30m -v ./internal/e2e/
```

### 调试技巧

```bash
# 使用 go run 直接运行（无需编译）
go run ./cmd/xyncra-server/

# 打印详细日志（设置环境变量）
GORM_LOG_LEVEL=info ./bin/xyncra-server

# 使用 delve 调试
dlv debug ./cmd/xyncra-server/
```
