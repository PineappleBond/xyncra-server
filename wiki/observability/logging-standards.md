# 结构化日志规范

## 概述

Xyncra Server 使用结构化日志记录系统运行状态。本文档定义日志格式、级别、字段规范和查询模式，确保日志在不同环境中的一致性和可操作性。

## 日志接口定义

服务器定义了一个最小化的结构化日志接口：

```go
// internal/server/websocket_server.go
type Logger interface {
    Info(msg string, args ...any)
    Error(msg string, args ...any)
    Debug(msg string, args ...any)
}
```

参数 `args...any` 使用键值对格式：`key1, value1, key2, value2`。

### 默认实现

```go
type stdLogger struct{}
func (stdLogger) Info(msg string, args ...any)  { log.Printf("[INFO]  "+msg, args...) }
func (stdLogger) Error(msg string, args ...any) { log.Printf("[ERROR] "+msg, args...) }
func (stdLogger) Debug(msg string, args ...any) { log.Printf("[DEBUG] "+msg, args...) }
```

默认实现将结构化参数通过 `log.Printf` 的格式化输出。自定义 Logger 实现可以替换为更完善的日志库（如 zap、logrus 等）。

## 日志级别

### 级别定义

| 级别 | 标签 | 使用场景 | 输出目标 |
|------|------|----------|----------|
| `Debug` | `[DEBUG]` | 调试信息、内部状态 | 开发环境 |
| `Info` | `[INFO]` | 正常操作事件、状态变更 | 所有环境 |
| `Error` | `[ERROR]` | 错误事件、异常情况 | 所有环境 |

### 级别使用指南

#### Debug（调试）

用于开发人员调试的信息，生产环境通常关闭：

```go
logger.Debug("processing message",
    "conversation_id", convID,
    "message_type", msgType,
    "queue", queueName,
)
```

仅在以下情况使用：
- 需要追踪内部状态变化
- 性能关键的代码路径中临时调试
- 仅在开发环境启用

#### Info（信息）

用于记录系统正常运行的重要事件：

```go
logger.Info("client connected",
    "user_id", userID,
    "device_id", deviceID,
    "ip", clientIP,
)

logger.Info("llm call completed",
    "agent_id", agentID,
    "model", model,
    "duration_ms", duration.Milliseconds(),
    "input_tokens", inputTokens,
    "output_tokens", outputTokens,
)
```

适用于：
- 连接/断开事件
- Agent 执行完成
- LLM 调用记录
- 服务器启动/关闭

#### Error（错误）

用于记录需要关注的问题：

```go
logger.Error("websocket: authenticate: %v", err)
logger.Error("llm call failed",
    "agent_id", agentID,
    "model", model,
    "duration_ms", duration.Milliseconds(),
    "error", err,
)
logger.Error("health check: connection store ping failed: %v", err)
```

适用于：
- 连接失败
- LLM 调用异常
- 外部依赖不可用
- 未预期的错误

### 不该使用的场景

- 不要用 Info 记录调试信息（用 Debug）
- 不要用 Error 记录预期中的业务逻辑分支（如"查询结果为空"不是错误）
- 不要用 Debug 记录安全相关事件（应始终记录为 Info 或 Error）

## 日志字段规范

### 标准字段

每行日志应包含以下标准信息：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| 时间戳 | time.Time | 是 | 日志产生时间（由日志框架自动添加）|
| 级别 | string | 是 | DEBUG / INFO / ERROR |
| 消息 | string | 是 | 描述事件的简短文本 |
| 键值对 | ...any | 否 | 结构化上下文信息 |

### 推荐的上下文 Key 命名

| Key | 类型 | 说明 | 示例 |
|-----|------|------|------|
| `user_id` | string | 用户标识 | `"alice"` |
| `device_id` | string | 设备标识 | `"dev-001"` |
| `conversation_id` | string | 会话标识 | `"conv-abc"` |
| `agent_id` | string | Agent 标识 | `"weather-bot"` |
| `model` | string | LLM 模型名 | `"qwen3.7-plus"` |
| `duration_ms` | int64 | 耗时（毫秒） | `1234` |
| `error` | error | 错误对象 | `"connection refused"` |
| `ip` | string | IP 地址 | `"10.0.0.1"` |
| `input_tokens` | int | 输入 Token 数 | `500` |
| `output_tokens` | int | 输出 Token 数 | `200` |

### Key 命名约定

1. 使用小写字母和下划线（`snake_case`）
2. Key 应自描述，不加单位后缀以外的修饰词
3. 布尔值用 `is_` 或 `has_` 前缀
4. 时间相关值统一用毫秒，Key 后缀 `_ms`

## LLM 专用日志格式

LLM 调用日志使用独立的 JSONL 格式（每行一个 JSON 对象），通过 `XYNCRA_LLM_LOG_DIR` 环境变量启用。

```json
{"level":"info","msg":"llm call completed","agent_id":"weather-bot","model":"qwen3.7-plus","duration_ms":1234,"input_tokens":500,"output_tokens":200,"time":"2024-01-01T00:00:00Z"}

{"level":"error","msg":"llm call failed","agent_id":"weather-bot","model":"qwen3.7-plus","duration_ms":5000,"error":"context deadline exceeded","time":"2024-01-01T00:00:05Z"}
```

这种格式易于用 `jq` 进行命令行分析和处理：

```bash
# 统计每个 Agent 的调用次数
jq -r 'select(.msg == "llm call completed") | .agent_id' llm-calls.log | sort | uniq -c | sort -rn

# 查看最近的错误
jq 'select(.level == "error")' llm-calls.log | tail -5

# 计算平均耗时
jq -s 'map(select(.msg == "llm call completed") | .duration_ms) | add / length' llm-calls.log
```

## 日志查询模式

### 常用查询

```bash
# 最近 100 行日志
tail -100 /var/log/xyncra/output.log

# 跟踪实时日志
tail -f /var/log/xyncra/output.log

# 只看错误
tail -f /var/log/xyncra/output.log | grep "\[ERROR\]"

# 按用户筛选
grep "user_id=alice" /var/log/xyncra/output.log

# 按 Agent 筛选
grep "agent_id=weather-bot" /var/log/xyncra/output.log

# 时间范围筛选
awk '/2024-01-01T10:00/,/2024-01-01T11:00/' /var/log/xyncra/output.log
```

### LLM 日志查询

```bash
# JSONL 格式查询
cat /app/llm-logs/llm-calls.log | jq 'select(.agent_id == "weather-bot")'

# 错误聚合
cat /app/llm-logs/llm-calls.log | jq 'select(.level == "error") | .error' | sort | uniq -c

# 耗时分布
cat /app/llm-logs/llm-calls.log | \
  jq 'select(.msg == "llm call completed") | .duration_ms' | \
  awk '{if($1<1000) count[1]++; else if($1<5000) count[2]++; else count[3]++}
       END{print "<1s:", count[1]+0, "1-5s:", count[2]+0, ">5s:", count[3]+0}'
```

## 日志安全

### 禁止记录的内容

- API Key 和密码
- 个人隐私信息（PII）
- 完整的 HTTP 请求体
- 数据库连接字符串（含密码）

### 需要脱敏的内容

- 用户 ID 在跨越安全边界时应考虑匿名化
- Token 值只记录前 4 位和后 4 位
- IP 地址可记录但不应作为用户标识

### 审计需求

- 管理操作（Agent 创建、配置变更）必须记录
- 敏感操作（数据删除）必须记录并包含操作人
- 认证失败必须记录，但不记录密码
