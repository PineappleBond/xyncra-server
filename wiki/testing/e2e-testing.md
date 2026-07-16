# 端到端测试

## 概述

Xyncra 的 E2E 测试位于 `internal/e2e/` 包，共 **197 个测试**。测试覆盖：

- 基础消息投递与协议（11 个 TC 用例）
- Agent 系统完整流程（8 个阶段，110+ 测试）
- 断连重连与离线同步
- 并发场景（多用户、多 Agent）
- 边缘输入与错误处理
- HITL（Human-in-the-Loop）审批流程

## 架构设计

```
┌─────────────────────────────────────────────┐
│                E2E Test                     │
│  ┌───────────────────────────────────────┐  │
│  │         setupE2ETest / setupAgentE2E │  │
│  │                                       │  │
│  │  ┌────────┐  ┌────────┐  ┌────────┐  │  │
│  │  │ SQLite  │  │ Redis  │  │  Mock  │  │  │
│  │  │ In-Mem  │  │  DB 15 │  │  LLM   │  │  │
│  │  └────────┘  └────────┘  └────────┘  │  │
│  │       │            │            │       │  │
│  │  ┌────▼────────────▼────────────▼───┐  │  │
│  │  │      Xyncra WebSocket Server     │  │  │
│  │  │  (handler + mq + server + agent) │  │  │
│  │  └──────────────────────────────────┘  │  │
│  └───────────────────────────────────────┘  │
│                                             │
│  ┌───────────────────────────────────────┐  │
│  │        WebSocket Client(s)           │  │
│  │    (gorilla/websocket channel-wrap)  │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

### 环境隔离

每个 `setupE2ETest` 调用创建完全隔离的环境：

| 组件 | 隔离方式 | 清理 |
|------|---------|------|
| SQLite | `:memory:` + `t.Name()` DSN | `t.Cleanup` → `db.Close()` |
| Redis | DB 15 + 测试前 `FlushDB` | `t.Cleanup` → `FlushDB` |
| Mock LLM | `httptest.NewServer` 随机端口 | `t.Cleanup` → `mockLLM.Close()` |
| WebSocket Server | `:0` 随机端口 | `t.Cleanup` → `GracefulStop()` |
| Agent 配置 | `t.TempDir()` 临时目录 | 测试框架自动清理 |

## 基础测试环境

### setupE2ETest

基础 E2E 环境构建函数（`e2e_test.go`），步骤：

1. 检查 Redis 连通性（不可用则 `t.Skip`）
2. FlushDB 确保干净状态
3. 创建 Redis PendingStore（D-103）
4. 创建 SQLite 内存数据库 + AutoMigrate
5. 创建 RedisConnectionStore
6. 创建 AsynqBroker
7. 创建 DefaultMessageHandler + FunctionRegistry
8. 创建 WebSocketServer（`:0` 随机端口）
9. 注册所有 handler（`RegisterAll`）
10. 创建 TaskHandler + 注册 TypeSendMessage
11. 启动 broker 和 server goroutine
12. 等待 server 绑定地址
13. 注册 `t.Cleanup`（反向顺序清理）

返回的 `e2eEnv` 包含：

```go
type e2eEnv struct {
    db           *store.Database
    store        *store.Store
    connStore    *server.RedisConnectionStore
    broker       *mq.AsynqBroker
    srv          *server.WebSocketServer
    addr         string
    cancel       context.CancelFunc
    redisKey     string
    taskHandler  *mq.TaskHandler
    msgHandler   *server.DefaultMessageHandler
    funcRegistry *server.MemoryFunctionRegistry
    pendingStore *server.RedisPendingStore
}
```

### setupAgentE2E

Agent E2E 环境构建函数（`agent_helpers_test.go`），在 `setupE2ETest` 基础上增加：

1. 创建 Mock LLM Server（或检测真实 LLM 模式）
2. 写入 Agent 配置文件到临时目录
3. 创建 AgentRegistry + 加载配置
4. 创建 LLMClientFactory → AgentBuilder → AgentExecutor
5. 创建 StreamBridge + BroadcastHelper
6. 创建 ContextManager（DBContextManager）
7. 创建 IdempotencyStore + ConversationLock + CheckpointStore
8. 注册 Agent 任务处理器（`mq:agent_process`, `mq:agent_resume`）
9. 注册 Agent RPC 处理器（`reload_agents`, `agent_resume`）

返回的 `agentE2EEnv` 包含：

```go
type agentE2EEnv struct {
    *e2eEnv
    mockLLM      *mockLLMServer
    registry     *agent.AgentRegistry
    executor     *agent.AgentExecutor
    agentBuilder *agent.AgentBuilder
    agentsDir    string
    lock         agent.ConversationLock
}
```

## Mock LLM 系统

### 设计原理

`mockLLMServer`（`mock_llm_test.go`）是一个完整的 OpenAI 兼容 HTTP 服务器，在 `httptest.Server` 上运行。Agent E2E 测试使用它替代真实 LLM 提供商，实现：

- **确定性**：每次返回相同响应，测试可复现
- **速度**：响应时间 <10ms，无需等待外部 API
- **覆盖**：可模拟错误、超时、流式中断等极端情况
- **验证**：`CallCount()`、`RecordedTools()` 可验证调用行为

### API 兼容性

| 端点 | 方法 | 用途 |
|------|------|------|
| `/v1/chat/completions` | POST | 聊天补全（流式 + 非流式） |
| `/v1/models` | GET | 模型列表查询 |

### 响应路由逻辑

```
用户消息 → 关键字匹配
  ├── "error_trigger"       → HTTP 500
  ├── "" (空/空白)          → HTTP 500
  ├── "tool_weather" + tools → tool_call 响应
  ├── "hello" / "hi"        → "Hello! I'm the test bot..."
  ├── "context"             → "I can see the context..."
  ├── 存在 tool result      → 默认文本响应
  └── 默认                  → "This is a mock response..."
```

### 流式响应模拟

`splitIntoTokens` 将文本按单词分割，每个 token 发送一个 SSE chunk，间隔 10ms：

```
data: {"choices":[{"delta":{"content":"Hello "}}]}

data: {"choices":[{"delta":{"content":"world "}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

## Agent 全流程测试（8 个阶段）

### Phase 1: 基础消息投递

| 测试 | ID | 验证内容 |
|------|----|---------|
| 消息发送与接收 | TC-1 | D-006 幂等性、D-007 fire-and-forget、D-008 MessageID |
| 离线消息同步 | TC-2 | D-009 sync_updates 分页 |
| 消息顺序 | TC-3 | D-008 单调递增 |
| 幂等性 | TC-4 | D-006 client_message_id |
| Heartbeat | TC-5 | D-010 TTL 刷新 |
| 权限控制 | TC-6,7 | 非成员拒绝、不存在会话 |
| 输入验证 | TC-8 | 必填字段校验 |
| 会话查询 | TC-9,10 | D-012 unread_count |
| 删除恢复 | TC-11 | D-013/D-015 级联软删除 |

### Phase 2: Agent 基础流程

| 测试 | ID | 验证内容 |
|------|----|---------|
| Agent 回复 | AE-BASIC-001 | D-054/D-055/D-062 用户→Agent 消息流 |
| 消息格式 | AE-BASIC-002 | D-054/D-055 sender_id = "agent/{id}" |
| 离线同步 | AE-BASIC-003 | D-055/D-009 sync_updates 获取 Agent 回复 |
| 人人不受影响 | AE-BASIC-004 | D-062 Agent 系统不影响普通消息 |
| Agent 间消息 | AE-BASIC-005 | D-062 Agent 间不触发处理 |

### Phase 3: Agent 错误处理

| 测试 | ID | 验证内容 |
|------|----|---------|
| LLM HTTP 500 | AE-ERR-001 | D-067 "暂时无法回复" |
| API Key 缺失 | AE-ERR-002 | D-067 "配置有误" |
| 上下文加载失败 | AE-ERR-003 | D-067 数据库错误 |
| 配置错误 | AE-ERR-004 | D-067 无效配置 |
| 空消息 | AE-ERR-005 | D-091 "抱歉" |
| 执行超时 | AE-ERR-006 | D-067 "回复超时" |
| 工具执行失败 | AE-ERR-007 | D-067 "工具执行错误" |

### Phase 4: Agent 流式

| 测试 | 验证内容 |
|------|---------|
| 流式更新广播 | D-050 Ephemeral Push (Seq=0) |
| 累积文本 | D-051 字段名 `text` |
| 完成信号 | `is_done: true` 最终广播 |
| 兼容性 | 非 Agent 用户不受 streaming 影响 |

### Phase 5: Agent 工具调用

| 测试 | 验证内容 |
|------|---------|
| Tool Call 流程 | Agent 调用工具 → 工具结果 → Agent 总结 |
| 工具定义注入 | DynamicToolProvider 注入客户端函数 |
| 工具注册 | MemoryFunctionRegistry CRUD |
| 工具执行错误 | D-067 工具失败分类 |

### Phase 6: Agent HITL

| 测试 | ID | 验证内容 |
|------|----|---------|
| 用户批准 | AE-HITL-001 | D-084 ask_user_question → approved |
| 用户拒绝 | AE-HITL-002 | D-084 approved: false |
| 断连恢复 | AE-HITL-003 | D-085 重连后批准/拒绝 |
| 超时处理 | AE-HITL-004 | D-086 HITL 超时自动取消 |
| 多设备竞争 | AE-HITL-005 | D-086 一台设备响应后取消其他 |
| 子 Agent HITL | AE-HITL-006 | D-081 子 Agent 触发 HITL |
| HITL 错误恢复 | AE-HITL-RESILIENCE-* | 重复恢复、并发恢复 |

### Phase 7: Agent 上下文管理

| 测试 | 验证内容 |
|------|---------|
| 上下文传递 | D-060 历史消息注入 LLM 请求 |
| Token 修剪 | 长上下文时的自动裁剪 |
| 上下文缓存失效 | `InvalidateCache` 调用 |
| sync_updates | Agent 回复通过同步拉取 |

### Phase 8A: Agent 边缘输入

| 测试 | ID | 验证内容 |
|------|----|---------|
| 超长输入 | AE-EDGE-001 | 10000+ 字符不崩溃 |
| 空消息 | AE-EDGE-002 | 拒绝并返回错误消息 |
| Emoji | AE-EDGE-003 | Emoji 正常处理 |
| CJK 字符 | AE-EDGE-004 | 中文/日文/韩文正常处理 |
| RTL 文本 | AE-EDGE-005 | 阿拉伯语/希伯来语 |
| Null 字节 | AE-EDGE-006 | 不截断或崩溃 |
| 消息突发 | AE-EDGE-007 | 10 条并发消息，锁序列化 |
| 大上下文 | AE-EDGE-008 | 50 条历史消息 |
| 多语言混合 | AE-EDGE-009 | 混合内容文本 |

### Phase 8B: 子 Agent

| 测试 | ID | 验证内容 |
|------|----|---------|
| 子 Agent 委派 | AE-SUB-001 | D-081 父 Agent → 子 Agent |
| 输出合并 | AE-SUB-002 | D-081 子 Agent 结果 → 父 Agent 总结 |
| 深度限制 | AE-SUB-003 | D-081 递归深度限制 |
| 不存在子 Agent | AE-SUB-004 | D-081 缺失子 Agent fail-open |

## 并发测试

### 多用户同 Agent

```go
func TestFullChainConcurrent_MultiUserSameAgent(t *testing.T) {
    // 3 个用户，各自与 "agent/test-bot" 有独立会话
    // 并发触发 executor.Execute（goroutine x3）
    // 验证：所有 3 个 Agent 回复持久化
    // 验证：无死锁，Redis 锁全部释放
}
```

### 同用户多 Agent

```go
func TestFullChainConcurrent_SingleUserMultiAgent(t *testing.T) {
    // 1 个用户，与 2 个 Agent 有会话
    // 并发发送消息到不同 Agent
    // 验证：两个 Agent 都回复
    // 验证：无锁竞争（不同 convID）
}
```

### 压力测试

```go
func TestFullChainConcurrent_Stress(t *testing.T) {
    // 10 个用户 x 10 轮 = 100 次 Agent 调用
    // 验证：全部完成，无死锁
    // 验证：Redis 锁全部释放
    // 验证：Server DB 消息数正确
}
```

## 断连重连测试

```go
func TestFullChainReconnect_DuringAgentProcessing(t *testing.T) {
    // 1. 用户连接，发送消息到 Agent
    // 2. Agent 开始处理（Mock LLM 带延迟）
    // 3. 客户端断连
    // 4. Agent 完成处理（轮询 DB）
    // 5. 客户端重连
    // 6. sync_updates 获取 Agent 回复
}
```

## 运行 E2E 测试

### 前置条件

```bash
# 启动 Redis（必需）
docker run -d --name xyncra-test-redis -p 16379:6379 redis:7-alpine
```

### 运行全部 E2E

```bash
go test -v -count=1 ./internal/e2e/... -timeout 600s 2>&1 | tee e2e-results.txt
```

### 运行指定类别

```bash
# 仅基础消息投递
go test -v -count=1 -run "TestBasic|TestOffline|TestMultiple|TestHeartbeat" ./internal/e2e/...

# 仅 Agent 基础
go test -v -count=1 -run "TestAgentBasic" ./internal/e2e/...

# 仅 Agent 错误处理
go test -v -count=1 -run "TestAgentErr" ./internal/e2e/...

# 仅 Agent HITL
go test -v -count=1 -run "TestAgentHITL" ./internal/e2e/...

# 仅并发测试
go test -v -count=1 -run "TestFullChainConcurrent" ./internal/e2e/...

# 仅断连重连
go test -v -count=1 -run "TestFullChainReconnect" ./internal/e2e/...

# 仅边缘输入
go test -v -count=1 -run "TestAgentEdge|TestFullChainBoundary" ./internal/e2e/...
```

### 真实 LLM 模式

```bash
# 需要 DASHSCOPE_API_KEY
DASHSCOPE_API_KEY=sk-xxx go test -tags real_llm -v -count=1 ./internal/e2e/... -timeout 600s
```

真实 LLM 模式通过 `XYNCRA_TEST_REAL_LLM_ENABLED=true` 环境变量控制，超时自动从 10s 调整为 60s。

## 测试结果报告

测试报告存储在 `docs/testing/reports/` 目录：

```bash
# 生成覆盖率
go test -coverprofile=e2e-coverage.out ./internal/e2e/...
go tool cover -func=e2e-coverage.out

# 记录失败测试详情
go test -v -count=1 ./internal/e2e/... -timeout 600s 2>&1 | \
    grep -E "(FAIL|PASS|---)" | tee e2e-summary.txt
```

## 已知问题

1. **D-110**: Asynq v0.26 在 `Start()` 后注册 handler 不生效，Agent E2E 绕过 MQ 直接调用 executor
2. **Redis 依赖**: Redis 不可用时测试自动 Skip，可能导致 CI 漏检
3. **1 个 FAIL**: 当前有 1 个 E2E 测试失败，详见 `docs/testing/reports/`
4. **时间敏感**: Mock LLM 的 10ms token 间隔在 CI 负载高时可能超时
