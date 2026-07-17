# 分布式链路追踪设计

> **Status**: Draft
> **Date**: 2026-07-17
> **Scope**: OpenTelemetry 全链路追踪实现

---

## 1. 目标

将 Xyncra Server 的所有关键请求路径纳入 OpenTelemetry 分布式链路追踪，包含 LLM 调用的完整细节（迭代、工具调用、token 消耗）。实现一站式排查：从 WebSocket 接收到 Agent 响应输出，每个环节都可以在 Jaeger/Tempo 中可视化。

## 2. 非目标

- 不替换现有的 LLMLogger（JSONL 日志保留作为持久化记录）
- 不引入 Prometheus metrics（metrics 是独立工作项）
- 不实现跨服务传播（当前只有单服务 + LLM API 外部调用）

## 3. 架构决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 追踪 SDK | OpenTelemetry | 行业标准，与 wiki 规划一致 |
| Exporter | OTLP/gRPC | 对接 Jaeger/Tempo，标准协议 |
| 实现方案 | 方案 A：TracingMiddleware + 显式 Span + auto-instrumentation | LLM 内部细节完整，基础设施不重复造轮子 |
| LLM trace 内容 | 默认元数据，debug 模式记录完整内容 | 平衡排查需求与数据量/隐私 |
| 采样策略 | 可配置比例 + debug user/device 强制采样 | 内部部署流量低，默认全量；保留调试能力 |
| MQ 传播 | 同 trace（子 span） | 整个请求在同一个 trace 里，查看最方便 |
| Span 范围 | 全量（WS + handler + MQ + agent + LLM + tool） | 一站式排查 |

## 4. Span 层级树

```
ws.connection (root span per connection lifetime)
├── ws.message.receive
│   └── handler.invoke (method dispatch)
│       ├── handler.store.write (SQL DB write via GORM)
│       ├── handler.store.read (SQL DB read via GORM)
│       ├── handler.broker.enqueue (MQ enqueue)
│       │   └── mq.process (worker side, same trace)
│       │       └── agent.execute
│       │           ├── agent.build
│       │           └── agent.run
│       │               ├── agent.llm.call (iteration 1)
│       │               │   └── [LLM API HTTP call - auto-instrumented]
│       │               ├── agent.tool.call (tool execution)
│       │               ├── agent.llm.call (iteration 2)
│       │               ├── agent.checkpoint.save
│       │               └── agent.stream (streaming output)
│       └── handler.broadcast (cross-node pub/sub)
├── ws.message.send
└── [Redis operations - auto-instrumented by otelredis]
```

> **注意**：`handler.store.*` 专指 SQL DB 操作（GORM）。Redis 操作（ConnectionStore、Pub/Sub）通过 `otelredis` 自动 instrumentation 产生独立 span，不手动创建。

## 5. Span 定义

### 5.1 WebSocket 层

| Span | 父 Span | 属性 | 说明 |
|------|---------|------|------|
| `ws.connection` | root | `user_id`, `device_id`, `ip` | 连接生命周期，连接关闭时 end |
| `ws.message.receive` | `ws.connection` | `method`, `size_bytes` | 收到客户端消息 |
| `ws.message.send` | `ws.connection` | `target_user_id`, `message_type` | 发送消息给客户端 |

### 5.2 Handler 层

| Span | 父 Span | 属性 | 说明 |
|------|---------|------|------|
| `handler.invoke` | `ws.message.receive` | `xyncra.method`, `xyncra.conversation_id` | 方法分发 |
| `handler.store.write` | `handler.invoke` | `db.system`, `db.table`, `db.operation` | SQL DB 写操作（GORM），非 Redis |
| `handler.store.read` | `handler.invoke` | `db.system`, `db.table`, `db.operation` | SQL DB 读操作（GORM），非 Redis |
| `handler.broker.enqueue` | `handler.invoke` | `xyncra.task_type`, `messaging.destination` | 任务入队 |
| `handler.broadcast` | `handler.invoke` | `xyncra.target_user_id`, `xyncra.node_count` | 跨节点广播 |

### 5.3 MQ 层

| Span | 父 Span | 属性 | 说明 |
|------|---------|------|------|
| `mq.enqueue` | `handler.broker.enqueue` | `xyncra.task_type`, `messaging.message_id` | 入队（与 handler span 同 trace） |
| `mq.process` | `mq.enqueue` | `xyncra.task_type`, `messaging.message_id`, `xyncra.retry_count` | Worker 处理 |
| `mq.retry` | `mq.process` | `xyncra.attempt`, `xyncra.max_retries`, `error` | 重试 |

**Trace Context 传播**：trace context 序列化到 Asynq task metadata，worker 恢复后作为子 span 继续同一 trace。

### 5.4 Agent 执行层

| Span | 父 Span | 属性 | 说明 |
|------|---------|------|------|
| `agent.execute` | `mq.process` | `xyncra.agent_id`, `xyncra.conversation_id`, `xyncra.user_id` | Agent 执行入口 |
| `agent.build` | `agent.execute` | `xyncra.agent_id`, `xyncra.tool_count` | Agent 构建（图编译、工具注入） |
| `agent.run` | `agent.execute` | `xyncra.agent_id` | Eino Runner 执行 |
| `agent.checkpoint.save` | `agent.run` | `xyncra.checkpoint_id` | HITL checkpoint 保存 |
| `agent.stream` | `agent.run` | `xyncra.chunk_count`, `xyncra.total_chars` | 流式输出 |

### 5.5 LLM 调用层（TracingMiddleware）

| Span | 父 Span | 属性 | 说明 |
|------|---------|------|------|
| `agent.llm.call` | `agent.run` | `xyncra.model`, `xyncra.iteration`, `xyncra.input_tokens`, `xyncra.output_tokens`, `xyncra.total_tokens`, `xyncra.duration_ms` | 每次 LLM API 调用 |
| `agent.tool.call` | `agent.run` | `xyncra.tool_name`, `xyncra.duration_ms` | 工具执行 |

### 5.6 Debug 模式额外属性

当请求的 `user_id` 或 `device_id` 在 `debug_users`/`debug_devices` 列表中时，LLM span 额外记录：

| 属性 | 说明 |
|------|------|
| `xyncra.llm.request.messages` | 完整 prompt（JSON 序列化的 messages 数组） |
| `xyncra.llm.response.output` | 模型响应内容 |
| `xyncra.llm.tool_calls` | 工具调用详情（name + args + result） |

### 5.7 错误处理

所有 span 在遇到错误时：
- `span.SetStatus(codes.Error, err.Error())`
- `span.RecordError(err)` — 记录 error 类型和 stack trace
- span 正常 end（不 panic）

### 5.8 属性命名规范

- 遵循 OpenTelemetry Semantic Conventions
- 业务属性用 `xyncra.` 前缀（如 `xyncra.agent_id`）
- 通用属性用标准语义（如 `db.system`, `http.method`, `messaging.system`）

## 6. 代码结构

### 6.1 新增文件

```
internal/tracing/
├── tracing.go           # OTel 初始化、TracerProvider 配置
├── config.go            # TracingConfig 结构体 + 环境变量解析
├── middleware.go         # 公共 middleware（debug 采样、属性提取等）
└── attributes.go        # 常量定义（span 名称、attribute key）

internal/agent/
└── tracing_middleware.go # TracingMiddleware（镜像 LoggingMiddleware）

internal/server/
└── tracing.go           # WebSocket 层的 span helpers

internal/mq/
└── tracing.go           # Asynq task 的 trace context 序列化/反序列化
```

### 6.2 关键实现

#### OTel 初始化 (`internal/tracing/tracing.go`)

```go
func InitTracer(cfg Config) (*sdktrace.TracerProvider, error) {
    // 根据 cfg.Exporter 选择：
    // - "otlp": otlptracegrpc exporter → Jaeger/Tempo
    // - "stdout": stdout exporter（开发调试）
    //
    // 采样策略：
    // - 默认：TraceIDRatioBased(cfg.SamplingRate)
    // - debug users/devices：AlwaysSample（在 handler 层检查并注入）
    //
    // Resource：service.name=xyncra-server, service.version=..., deployment.environment=...
}
```

#### TracingMiddleware (`internal/agent/tracing_middleware.go`)

```go
type TracingMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
    tracer    trace.Tracer
    agentID   string
    model     string
    debugMode bool  // 从 context 中读取，决定是否记录完整内容
}

// 实现与 LoggingMiddleware 相同的 hooks：
// BeforeAgent        → 创建 agent.run span
// AfterAgent         → 结束 span，设置状态
// BeforeModelRewriteState → 创建 agent.llm.call span，记录 model/iteration
// AfterModelRewriteState  → 结束 span，记录 tokens/duration
// WrapInvokableToolCall   → 创建 agent.tool.call span
```

#### MQ Trace Context 传播 (`internal/mq/tracing.go`)

```go
// 入队时：将 trace context 编码到 Asynq task metadata
func EnqueueWithTrace(ctx context.Context, broker Broker, task *Task) error {
    spanCtx := trace.SpanContextFromContext(ctx)
    task.Metadata["trace_parent"] = spanCtx.String()
    return broker.Enqueue(ctx, task)
}

// Worker 处理时：从 task metadata 恢复 trace context
func ProcessWithTrace(ctx context.Context, task *Task) context.Context {
    if tp, ok := task.Metadata["trace_parent"]; ok {
        spanCtx, _ := spanCtxFromString(tp)
        return trace.ContextWithSpanContext(ctx, spanCtx)
    }
    return ctx
}
```

#### Debug 采样 (`internal/tracing/middleware.go`)

```go
// 在 handler 入口检查 user_id/device_id 是否在 debug 列表中
func WithDebugSampling(ctx context.Context, cfg Config, userID, deviceID string) context.Context {
    if isDebugTarget(cfg, userID, deviceID) {
        // 强制当前 trace 为 sampled
        spanCtx := trace.SpanContextFromContext(ctx)
        spanCtx = spanCtx.WithTraceFlags(spanCtx.TraceFlags().WithSampled(true))
        ctx = trace.ContextWithSpanContext(ctx, spanCtx)
        ctx = context.WithValue(ctx, debugContentKey{}, true)
    }
    return ctx
}
```

#### Handler 层 instrumentation

在 `DefaultMessageHandler.HandleRequest` 中添加：

```go
ctx, span := tracer.Start(ctx, "handler.invoke",
    trace.WithAttributes(
        attribute.String("xyncra.method", req.Method),
    ),
)
defer span.End()
```

#### 基础设施 auto-instrumentation

```go
// Redis（go-redis 已内置 OTel hook）
rdb := redis.NewClient(&redis.Options{...})
rdb.AddHook(otelredis.NewHook())

// HTTP（LLM API 调用）
// 用 otelhttp.NewTransport 包装 default transport
http.DefaultTransport = otelhttp.NewTransport(http.DefaultTransport)
```

## 7. 配置

### 7.1 环境变量

```bash
XYNCRA_TRACING_ENABLED=true
XYNCRA_TRACING_EXPORTER=otlp          # otlp | stdout
XYNCRA_TRACING_OTLP_ENDPOINT=localhost:4317
XYNCRA_TRACING_OTLP_INSECURE=true
XYNCRA_TRACING_SAMPLING_RATE=1.0
XYNCRA_TRACING_DEBUG_USERS=alice,bob  # 逗号分隔
XYNCRA_TRACING_DEBUG_DEVICES=dev-001  # 逗号分隔
```

### 7.2 YAML 配置（等效）

```yaml
tracing:
  enabled: true
  exporter: otlp            # otlp | stdout
  otlp_endpoint: localhost:4317
  otlp_insecure: true       # 开发环境
  sampling_rate: 1.0        # 0.0 ~ 1.0
  debug_users: []           # user_id 列表，强制采样 + 记录完整内容
  debug_devices: []         # device_id 列表，同上
```

## 8. 测试策略

### 8.1 单元测试

| 模块 | 测试内容 |
|------|----------|
| `internal/tracing/config.go` | Config 解析、默认值、环境变量覆盖 |
| `internal/tracing/tracing.go` | TracerProvider 初始化（mock exporter） |
| `internal/agent/tracing_middleware.go` | 各 hook 创建正确的 span + 属性 |
| `internal/mq/tracing.go` | Trace context 序列化/反序列化往返正确 |

### 8.2 集成测试

- 启动 OTel collector（或 stdout exporter），执行 TC-000 完整链路，验证 trace 包含完整 span 树
- 验证 MQ 跨 goroutine 的 trace 传播（enqueue → process 在同 trace）
- 验证 debug 模式：指定 user_id 后，trace 中包含完整 messages 内容

### 8.3 手动验证

新增 `TC-009-链路追踪验证.md` 测试用例文档，描述如何部署 Jaeger + 验证 trace 可视化。

## 9. Wiki 更新

实现完成后更新以下 wiki 文档：

| 文档 | 变更 |
|------|------|
| `wiki/observability/distributed-tracing.md` | 从"建议方案"改为"已实现"，更新架构图为实际实现，补充 TracingMiddleware 说明 |
| `wiki/observability/index.md` | 更新说明，标注追踪已实现 |
| `wiki/observability/logging-standards.md` | 补充说明 tracing 与 logging 的关系：LLM 日志保留作为持久化记录，tracing 用于实时排查 |
| `wiki/onboarding/configuration.md` | 新增 tracing 配置项说明 |
| `wiki/devops/deployment-topology.md` | 补充 Jaeger/OTel Collector 部署说明 |

## 10. 完成标准

1. ✅ 全量 span 实现：WS 连接 → handler → MQ → agent → LLM → tool 全链路可追踪
2. ✅ OTLP exporter 可对接 Jaeger all-in-one
3. ✅ stdout exporter 可本地调试
4. ✅ 可配置采样率（`XYNCRA_TRACING_SAMPLING_RATE`）
5. ✅ Debug user/device 强制采样 + 记录完整内容
6. ✅ MQ 同 trace 传播
7. ✅ 新增 `TC-009` 测试用例文档
8. ✅ Wiki 文档同步更新
9. ✅ 现有测试不 break（TC-000 ~ TC-008 全部通过）

## 11. 与现有系统的关系

- **LLMLogger**：保留，作为 LLM 调用的持久化日志记录。TracingMiddleware 与 LoggingMiddleware 并行运行，互不干扰。
- **LLMMetrics**：保留，用于 Prometheus metrics（未来工作）。Tracing 和 Metrics 独立，各司其职。
- **日志**：tracing 补充日志，不替代。日志仍用于事件记录，tracing 用于请求链路分析。

## 12. 部署拓扑

```
Xyncra Server ──OTLP/gRPC──▶ OTel Collector ──▶ Jaeger/Tempo
                                                      │
                                                      ▼
                                                Grafana UI
```

开发环境：

```bash
# 启动 Jaeger all-in-one
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 4317:4317 \
  -p 4318:4318 \
  jaegertracing/all-in-one:latest

# Xyncra Server 配置
export XYNCRA_TRACING_ENABLED=true
export XYNCRA_TRACING_EXPORTER=otlp
export XYNCRA_TRACING_OTLP_ENDPOINT=localhost:4317
export XYNCRA_TRACING_OTLP_INSECURE=true
export XYNCRA_TRACING_SAMPLING_RATE=1.0
```

访问 http://localhost:16686 查看 traces。
