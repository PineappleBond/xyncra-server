# 单元测试

## 概述

Xyncra 的单元测试分布在三个主要包中，每个包都有独立的 `TESTING_USAGE.md` 文档：

- `internal/server/TESTING_USAGE.md` — 226+ 测试，覆盖 WebSocket 服务器、连接存储、消息处理
- `internal/mq/TESTING_USAGE.md` — 20+ 测试，覆盖 Asynq 消息队列、任务路由
- `internal/store/TESTING_USAGE.md` — 11 个测试函数 x 3 数据库，覆盖 SQLite/PostgreSQL/MySQL

## 单元测试规范

### 文件命名

```
{source_file}_test.go
  ├── handler_test.go      → handler.go
  ├── asynq_test.go        → asynq.go
  └── options_test.go      → options.go
```

### 包命名

```go
// 白盒测试（访问内部符号）
package server

// 黑盒测试（仅测试导出 API）
package server_test  // 或 mq_test, store_test
```

使用黑盒测试优先，仅在需要测试未导出函数时使用白盒测试。

### 测试函数签名

```go
func Test{Method}_{Scenario}(t *testing.T) {
    // Arrange — 准备测试数据
    // Act — 执行被测代码
    // Assert — 验证结果
}
```

嵌套子测试：

```go
func TestConnectionInfo_IsExpired(t *testing.T) {
    tests := []struct{
        name string
        info ConnectionInfo
        want bool
    }{
        {name: "not_expired_with_zero_TTL", ...},
        {name: "expired_past_TTL", ...},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            // ...
        })
    }
}
```

## Table-Driven 测试模式

### 标准模式

```go
func TestSendMessageValidation(t *testing.T) {
    tests := []struct {
        name   string
        params map[string]interface{}
        expect string
    }{
        {
            name: "missing conversation_id",
            params: map[string]interface{}{
                "client_message_id": uuid.New().String(),
                "content":           "hello",
            },
            expect: "conversation_id",
        },
        {
            name: "missing client_message_id",
            params: map[string]interface{}{
                "conversation_id": "conv-1",
                "content":         "hello",
            },
            expect: "client_message_id",
        },
        {
            name: "missing content",
            params: map[string]interface{}{
                "conversation_id":   "conv-1",
                "client_message_id": uuid.New().String(),
            },
            expect: "content",
        },
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            // 每个子测试有独立 E2E 环境
            env := setupE2ETest(t)
            aliceConn := connectClient(t, env.addr, "alice", "alice")
            defer aliceConn.Close()

            sendRequest(t, aliceConn, "req-1", "send_message", tc.params)
            resp := readResponse(t, aliceConn, 5*time.Second)
            assert.Equal(t, protocol.ResponseCodeValidationError, resp.Code)
            assert.Contains(t, strings.ToLower(resp.Msg), tc.expect)
        })
    }
}
```

### 复杂场景模式

对于多步骤验证的测试，使用 `testStepLogger` + `threeLayerCheck` 模式：

```go
func TestComplexScenario(t *testing.T) {
    env := setupE2ETest(t)
    logger := newTestStepLogger(t)
    check := newThreeLayerCheck(t, logger)

    logger.Step("initial-state")
    check.VerifyServerDB("initial-count", func() error {
        // 验证初始状态
    })
    check.VerifyRedis("lock-released", func() error {
        requireRedisSessionLockReleased(t, redisClient, convID)
        return nil
    })

    logger.Step("execute-action")
    // 执行操作...

    logger.Step("verify-result")
    check.VerifyServerDB("message-count", func() error {
        // 验证结果
    })
}
```

## Mock 策略

### Mock 类型

| 包 | Mock 类型 | 说明 |
|----|----------|------|
| `internal/server` | 无需 Mock | RedisConnectionStore 直接连接 Redis |
| `internal/mq` | 无需 Mock | AsynqBroker 直接连接 Redis |
| `internal/store` | 无需 Mock | 使用内存 SQLite |
| `internal/agent` | Mock LLM Server | OpenAI 兼容 HTTP Mock |

### Mock LLM 设计

Agent E2E 测试使用 `mockLLMServer`（`internal/e2e/mock_llm_test.go`），它是一个完整的 OpenAI 兼容 HTTP 服务模拟器：

```go
mockLLM := newMockLLMServer()
defer mockLLM.Close()

// 配置响应
mockLLM.SetResponse("hello", "Hello! I'm the test bot.")
mockLLM.SetToolCallResponse("get_weather", `{"location":"Beijing"}`, `{"temp":"22°C"}`)

// 配置多步序列
mockLLM.SetToolCallSequence([]ToolCallStep{
    {ToolName: "get_weather", Arguments: `{"location":"Beijing"}`},
    {Text: "The weather in Beijing is 22°C and sunny."},
})

// 配置弱网注入
mockLLM.SetWeakNetConfig(llmWeakNetConfig{
    ResponseDelay:       2 * time.Second,
    BlackHoleTimeout:    true,
    StreamDisconnectAfter: 3,
    RateLimitFirstN:     2,
})
```

### Mock 能力

| 特性 | 支持 | 说明 |
|------|------|------|
| 非流式响应 | ✓ | 标准 JSON 响应 |
| SSE 流式响应 | ✓ | `text/event-stream`，10ms token 间隔 |
| Tool Call 响应 | ✓ | `finish_reason: "tool_calls"` |
| 内容路由 | ✓ | 关键字匹配返回不同响应 |
| HTTP 错误 | ✓ | `error_trigger` → HTTP 500 |
| 空消息拒绝 | ✓ | 空白内容 → HTTP 500 |
| 响应延迟注入 | ✓ | `ResponseDelay` 配置 |
| 黑盒超时 | ✓ | 接受 TCP 但不发送 HTTP |
| 流式中断 | ✓ | N 个 chunk 后断开 |
| 速率限制 | ✓ | HTTP 429 模拟 |
| 调用计数 | ✓ | `CallCount()` / `ToolCallCount()` |
| 请求记录 | ✓ | `RecordedTools()` / `LastRequestMessages()` |
| 多步序列 | ✓ | 预定义的多轮对话步骤 |

## Testify 使用约定

### 断言选择

```go
// 必须满足的条件 — 失败即中止测试
require.NoError(t, err)
require.NotNil(t, result)
require.Equal(t, expected, actual)

// 非关键验证 — 失败继续执行
assert.Equal(t, expected, actual)
assert.True(t, condition)
assert.Contains(t, str, substring)
assert.Len(t, slice, expectedLen)
assert.Greater(t, value, threshold)
assert.Empty(t, slice)
```

### require.Eventually 模式

对于异步操作，使用 `require.Eventually` 而非 `time.Sleep`：

```go
require.Eventually(t, func() bool {
    conns, err := env.connStore.ListByUser(ctx, "alice", 10)
    return err == nil && len(conns) == 0
}, 5*time.Second, 100*time.Millisecond, "alice should be disconnected")
```

## 测试基础设施

### channelWaiter

用于异步事件的确定性等待，替代 `time.Sleep`：

```go
waiter := newChannelWaiter("message-delivered", 10)
// 在异步回调中: waiter.signal()
err := waiter.wait(1, 5*time.Second)
// 等待 1 次信号，超时 5 秒
```

### testStepLogger

用于复杂测试的分步日志记录：

```go
logger := newTestStepLogger(t)
logger.Step("setup")
logger.Checkpoint("msg-count", "ServerDB", "verified")
logger.FailCheckpoint("lock", "Redis", err)
```

### threeLayerCheck

三层验证（Server DB + Redis + Client DB）的同步检查：

```go
check := newThreeLayerCheck(t, logger)
check.VerifyServerDB("name", func() error { ... })
check.VerifyRedis("name", func() error { ... })
check.VerifyClientDB("name", func() error { ... }, soft=true)
```

`VerifyClientDB` 的 `soft` 参数：当 MQ 推送可能不及时时设为 true，失败仅记录不中止测试。

## 时间常量

```go
const (
    fastTimeout    = 5 * time.Second   // Redis / DB 操作
    normalTimeout  = 15 * time.Second  // WebSocket 消息等待
    agentTimeout   = 30 * time.Second  // Agent 处理（Mock LLM）
    realLLMTimeout = 60 * time.Second  // Agent 处理（真实 LLM）
    mqTimeout      = 20 * time.Second  // MQ 任务完成
)
```

## 运行特定测试

```bash
# Server 包：所有连接存储测试
go test -v -run "TestRedisConnectionStore" ./internal/server/...

# Server 包：仅 WebSocket 消息测试
go test -v -run "TestWebSocketMsg" ./internal/server/...

# MQ 包：仅 TaskHandler 测试
go test -v -run "TestTaskHandler" ./internal/mq/...

# Store 包：仅 SQLite 测试
go test -v -count=1 -run "SQLite" ./internal/store/

# 带竞态检测
go test -v -race -count=1 ./internal/server/...

# 带覆盖率
go test -v -coverprofile=coverage.out ./internal/server/...
go tool cover -html=coverage.out -o coverage.html
```

## 常见陷阱

### 1. gorilla/websocket 读取错误

`gorilla/websocket` 在读取超时后会导致连接永久损坏。解决方案是使用 `wsConn` 的 channel 包装器：

```go
// 正确：使用 channel 包装
conn := wrapConn(rawConn)
msg, err := conn.recv(5*time.Second)

// 错误：直接调用 ReadMessage
_, _, err := rawConn.ReadMessage() // 超时后连接损坏
```

### 2. MQ 消息延迟

MQ 推送是异步的，不要在发送后立即等待。使用 `drainPushUpdates` 清理残留消息：

```go
drainPushUpdates(t, aliceConn)  // 清理可能残留的推送
response := readResponse(t, aliceConn, 5*time.Second)
```

### 3. table-driven E2E 测试隔离

每个子测试需要独立的 `setupE2ETest`，不能共享环境：

```go
// 正确
for _, tc := range tests {
    t.Run(tc.name, func(t *testing.T) {
        env := setupE2ETest(t)  // 每个子测试独立环境
        // ...
    })
}

// 错误
env := setupE2ETest(t)  // 所有子测试共享环境——竞态！
for _, tc := range tests {
    t.Run(tc.name, func(t *testing.T) {
        // ...
    })
}
```
