# 测试策略

## 测试哲学

Xyncra 作为一个实时消息 + AI Agent 服务器，测试策略围绕以下核心理念构建：

1. **测试即保障**：每个新功能必须有对应测试，拒绝"先上线再补测试"
2. **分层验证**：单元测试覆盖逻辑正确性，E2E 测试覆盖系统集成正确性
3. **确定性优先**：Agent 测试使用 Mock LLM 消除外部依赖的不确定性
4. **可复现**：所有测试使用独立 Redis DB（DB 15）、内存 SQLite，确保完全隔离
5. **安全后门**：测试工具不引入安全漏洞，测试后门不可在生产环境利用

## 测试金字塔

```
        ┌──────────┐
        │  E2E     │  197 个测试
        │  集成    │  覆盖：消息投递、Agent 全流程、HITL、断连重连
       ┌┴──────────┴┐
        │  集成测试   │  197 个 Server E2E + 79 个 CLI E2E
       │  (需要 Redis)│  覆盖：HTTP 路由、Redis 连接存储、MQ 消息队列
      ┌┴────────────┴┐
       │  单元测试     │  1,680+ 个测试，3 个包有独立 TESTING_USAGE.md
      │  (快速可靠)   │  覆盖：所有 CRUD 操作、消息处理、配置验证
      └──────────────┘
```

### 各层级职责

| 层级 | 运行时间 | 外部依赖 | 失败影响 | 责任 |
|------|---------|---------|---------|------|
| 单元测试 | <100ms/测试 | 无（mock） | 定位精确 | 验证函数级逻辑 |
| 集成测试 | 1-5s/测试 | Redis | 确认组件交互 | 验证跨组件调用 |
| E2E 测试 | 5-30s/测试 | Redis + Mock LLM | 确认系统功能 | 验证完整用户流程 |

## 覆盖率目标

| 包 | 目标覆盖率 | 当前状态 | 关键路径 |
|----|-----------|---------|---------|
| `internal/server` | >80% | 325 测试 | WebSocket 协议、连接管理 |
| `internal/mq` | >80% | 68 测试 | 消息路由、任务入队 |
| `internal/store` | >90% | 130 测试 | CRUD、事务、迁移 |
| `internal/agent` | >75% | 329 测试 | 执行器、HITL、子代理 |
| `internal/handler` | >80% | 集成测试覆盖 | RPC 方法注册与分发 |
| `pkg/protocol` | >90% | E2E 间接覆盖 | 包序列化、字段定义 |
| `internal/cli` | >70% | CLI E2E | IPC 通信、显示逻辑 |

### 不强制覆盖率的区域

- `cmd/` — 仅入口函数，通过 E2E 验证
- `config/` — 静态配置，通过手动测试验证
- `pkg/client/` — 客户端 SDK，通过 CLI E2E 验证

## 各层级测试内容

### 单元测试覆盖

```
internal/server/  — 覆盖函数级行为
├── BaseServer 生命周期（启动、停止、GracefulStop、并发…）
├── RedisConnectionStore CRUD（Add、Get、Remove、Exists、Update、Refresh）
├── RedisConnectionStore 用户操作（ListByUser、CountByUser、RemoveByUser）
├── RedisConnectionStore TTL 与过期
├── RedisConnectionStore 并发（20 goroutine 并发 Add/Get）
├── WebSocketServer 构建与选项
├── WebSocketServer 生命周期
├── WebSocketServer 路径与认证
├── WebSocket 连接（缺失参数、多次连接、断开清理）
├── WebSocket 消息（请求-响应、广播、错误处理、并发请求）
├── DefaultMessageHandler（注册、回退、并发注册）
└── Client（收发、缓冲满、关闭幂等、PingPong）

internal/mq/      — 覆盖队列操作
├── TaskHandler（注册、注销、路由、并发注册）
├── AsynqBroker（创建、关闭、入队、Worker 处理）
└── 选项配置（Queue、MaxRetry、Timeout、TaskID、Retention、Unique）

internal/store/   — 覆盖三种数据库
├── SQLite（内存数据库、无外部依赖）
├── PostgreSQL（Docker 容器、自动跳过）
├── MySQL（Docker 容器、自动跳过）
├── ConversationCRUD
├── MessageCRUD
├── UserUpdateCRUD
├── SendMessage 事务
├── TransactionCommit / Rollback
└── AutoMigrate 幂等性
```

### E2E 测试覆盖

```
internal/e2e/     — 覆盖完整系统流程
├── 基础消息投递（TC-1 ~ TC-11）
│   ├── 消息发送与接收
│   ├── 离线消息同步
│   ├── 消息顺序（MessageID 单调递增）
│   └── 幂等性（client_message_id）
│
├── Agent 基础流程（AE-BASIC-001 ~ 005）
│   ├── 用户 → Agent 消息处理
│   ├── Agent 回复持久化
│   ├── sync_updates 获取 Agent 回复
│   ├── 人人消息不受影响
│   └── Agent 间消息不触发处理
│
├── Agent 错误处理（AE-ERR-001 ~ 007）
│   ├── LLM API 错误 → "暂时无法回复"
│   ├── API Key 缺失 → "配置有误"
│   ├── 上下文加载失败
│   ├── 空消息拒绝
│   ├── 执行超时 → "回复超时"
│   └── 工具调用失败 → "工具执行错误"
│
├── Agent HITL（AE-HITL-001 ~ 008）
│   ├── 用户批准
│   ├── 用户拒绝
│   ├── 断连重连恢复
│   ├── 超时自动取消
│   ├── 多设备竞争
│   └── 子 Agent HITL
│
├── Agent 边缘输入（AE-EDGE-001 ~ 008）
│   ├── 超长输入（10000+ 字符）
│   ├── 空消息
│   ├── Emoji / CJK / RTL
│   ├── Null 字节
│   ├── 消息突发（10 条并发）
│   ├── 大上下文
│   └── 多语言混合
│
├── 并发场景
│   ├── 多用户同 Agent
│   ├── 单用户多 Agent
│   └── 压力测试
│
├── 断连重连
│   ├── Agent 处理中断连
│   ├── 多轮断连/重连
│   └── sync_updates 恢复
│
└── 手动 E2E 场景（TC-000 ~ TC-007）
    ├── 全链路消息投递
    ├── 重传与幂等性
    ├── HITL 审批流程
    ├── 上下文管理
    ├── 子 Agent 委派
    ├── 错误持久化
    └── 动态工具
```

## CI 集成

### GitHub Actions 配置模式

```yaml
# Redis 服务（E2E 测试依赖）
- name: Start Redis
  run: docker run -d --name redis -p 16379:6379 redis:7-alpine

# 单元测试（无需外部依赖）
- name: Unit tests
  run: go test -v -race -count=1 ./internal/server/... ./internal/mq/... ./internal/store/... -timeout 300s

# E2E 测试（需要 Redis + Mock LLM）
- name: E2E tests
  run: go test -v -count=1 ./internal/e2e/... -timeout 600s

# 清理
- name: Cleanup
  if: always()
  run: docker rm -f redis
```

### 并行策略

| 步骤 | 并行度 | 说明 |
|------|--------|------|
| lint + build | 并行 | 代码风格检查与编译 |
| 单元测试（无 Redis） | 并行 | store SQLite 测试 |
| 单元测试（需要 Redis）| 串行 | server、mq 测试 |
| E2E 测试 | 串行 | 共享 Redis DB 15 |

### 测试标签

| 构建标签 | 用途 | 执行条件 |
|---------|------|---------|
| 无标签 | 默认 E2E（Mock LLM） | Redis 可用 |
| `real_llm` | 真实 LLM 测试 | `DASHSCOPE_API_KEY` + Redis |

## 测试命名约定

### 单元测试命名

```
Test{Method}_{Scenario}
  ├── TestNewRedisConnectionStore_EmptyAddr
  ├── TestBaseServer_StartTwice
  └── TestSendMessageValidation/missing_content
```

### E2E 测试命名

```
Test{Category}_{SCENARIO_ID}
  ├── TestAgentBasic_AE_BASIC_001
  ├── TestAgentErr_AE_ERR_003
  └── TestAgentHITL_AE_HITL_001

Test{Feature}_{Description}
  ├── TestBasicMessageDelivery
  ├── TestOfflineMessageSync
  └── TestFullChainBoundary_MessageBurst
```

### 手动测试命名

```
TC-{NNN}_{short_description}
  ├── TC-000_full_chain_message_delivery
  └── TC-007_dynamic_tool_registration
```

## 测试运行命令速查

```bash
# 启动测试 Redis
docker run -d --name xyncra-test-redis -p 16379:6379 redis:7-alpine

# 全部单元测试
go test -v -race -count=1 ./internal/... ./pkg/... -timeout 300s

# 仅 server 包
go test -v -race ./internal/server/...

# 仅 mq 包
go test -v -race ./internal/mq/...

# 仅 store 包
go test -v -race ./internal/store/...

# E2E 测试（Mock LLM）
go test -v -count=1 ./internal/e2e/... -timeout 600s

# E2E 测试（真实 LLM）
DASHSCOPE_API_KEY=sk-xxx go test -tags real_llm -v -count=1 ./internal/e2e/... -timeout 600s

# 覆盖率
go test -coverprofile=coverage.out ./internal/...
go tool cover -html=coverage.out -o coverage.html

# 手动 E2E 场景
bash docs/testing/manual/tc-000_full_chain_message_delivery.sh
```

## 测试数据隔离

| 资源 | 隔离策略 |
|------|---------|
| Redis | 使用 DB 15，每个测试前 FlushDB |
| SQLite | 内存数据库 `:memory:`，每个测试独立实例 |
| Mock LLM | 每个 `setupAgentE2E` 创建独立 `httptest.Server` |
| Agent 配置 | `t.TempDir()` 创建临时目录，测试结束后清理 |
| 用户数据 | UUID v4 随机生成，避免命名冲突 |

## 相关文档

- [单元测试](unit-testing.md) — 单元测试规范与 Mock 策略
- [端到端测试](e2e-testing.md) — E2E 架构与 Mock LLM 详解
- [手动测试](manual-testing.md) — 手动测试用例与场景
- [CLI 测试工具](cli-testing-tool.md) — CLI 工具的测试后门
- [边缘场景](edge-cases.md) — 异常场景测试覆盖
