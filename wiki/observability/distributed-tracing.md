---
last_updated: 2026-07-17
---

# 分布式追踪

## 概述

Xyncra Server 已实现基于 OpenTelemetry 的分布式链路追踪，覆盖从 WebSocket 连接到 Agent LLM 调用的完整请求链路。追踪数据通过 OTLP/gRPC 协议导出到 Jaeger（或其他兼容后端），支持在 Jaeger UI 中进行端到端的链路可视化。

**状态**：已实现

## 追踪架构

### 技术栈

| 组件 | 选择 | 说明 |
|------|------|------|
| 追踪 SDK | OpenTelemetry | 行业标准 |
| Exporter | OTLP/gRPC | 标准协议，对接 Jaeger/Tempo |
| 采样策略 | 可配置比例 + DebugSampler | 支持 debug user/device 强制采样 |
| 默认后端 | Jaeger All-in-One | 开发/测试环境开箱即用 |
| 存储 | Badger（嵌入式） | 无需外部数据库 |

### Span 层级树

```
ws.connection (root span, 每个 WebSocket 连接生命周期)
├── ws.message.receive
│   └── handler.invoke (方法分发)
│       ├── handler.broker.enqueue (MQ 入队)
│       │   └── mq.process (worker 侧，同一 trace)
│       │       └── agent.execute
│       │           ├── agent.build
│       │           └── agent.run
│       │               ├── agent.llm.call (迭代 1)
│       │               ├── agent.tool.call (工具调用)
│       │               ├── agent.llm.call (迭代 2)
│       │               ├── agent.checkpoint.save
│       │               └── agent.stream (流式输出)
│       └── handler.broadcast (跨节点 Pub/Sub)
└── ws.message.send
```

### No-op 保证

当 `XYNCRA_TRACING_ENABLED=false`（默认值）时，安装 no-op TracerProvider。所有 `tracer.Start()` 调用返回零分配的 noop span，`TracingMiddleware` 也不会被添加到 Agent 中间件链中，确保零开销。

**静默降级**：所有追踪操作不阻塞业务路径，错误仅记录日志不返回给调用方。

## Span 定义

### WebSocket 层

| Span 名称 | 常量 | 父 Span | 说明 | 属性 |
|-----------|------|---------|------|------|
| `ws.connection` | `SpanWSConnection` | - | WebSocket 连接生命周期 | `xyncra.user.id`, `xyncra.device.id`, `xyncra.connection.id` |
| `ws.message.receive` | `SpanWSMessageReceive` | `ws.connection` | 接收客户端消息 | `xyncra.method`, `xyncra.size_bytes` |
| `ws.message.send` | `SpanWSMessageSend` | `ws.connection` | 发送消息给客户端 | `xyncra.target_type` |

### Handler 层

| Span 名称 | 常量 | 父 Span | 说明 | 属性 |
|-----------|------|---------|------|------|
| `handler.invoke` | `SpanHandlerInvoke` | `ws.message.receive` | 方法分发 | `xyncra.method` |
| `handler.broker.enqueue` | `SpanBrokerEnqueue` | `handler.invoke` | 消息队列入队 | `xyncra.task.type` |
| `handler.broadcast` | `SpanHandlerBroadcast` | `handler.invoke` | 跨节点广播 | `xyncra.target_user_id` |

### Agent 执行层

| Span 名称 | 常量 | 父 Span | 说明 | 属性 |
|-----------|------|---------|------|------|
| `agent.execute` | `SpanAgentExecute` | `mq.process` | Agent 执行入口 | `xyncra.agent.id`, `xyncra.conversation.id` |
| `agent.build` | `SpanAgentBuild` | `agent.execute` | Agent 构建 | - |
| `agent.run` | `SpanAgentRun` | `agent.execute` | Agent 运行 | - |
| `agent.llm.call` | `SpanAgentLLMCall` | `agent.run` | LLM 模型调用 | `xyncra.llm.model`, `xyncra.iteration`, `xyncra.llm.input_tokens`, `xyncra.llm.output_tokens`, `xyncra.llm.total_tokens`, `xyncra.duration_ms` |
| `agent.tool.call` | `SpanAgentToolCall` | `agent.run` | 工具调用 | `xyncra.tool.name`, `xyncra.duration_ms` |
| `agent.checkpoint.save` | `SpanAgentCheckpointSave` | `agent.run` | 检查点保存 | `xyncra.checkpoint.id` |
| `agent.stream` | `SpanAgentStream` | `agent.run` | 流式输出 | `xyncra.chunk_count`, `xyncra.total_chars` |

### 消息队列层

| Span 名称 | 常量 | 父 Span | 说明 | 属性 |
|-----------|------|---------|------|------|
| `mq.process` | `SpanBrokerProcess` | `handler.broker.enqueue` | 任务处理 | `xyncra.task.type`, `xyncra.task.id` |

## TracingMiddleware

`TracingMiddleware` 是 Agent 执行层的 OpenTelemetry 中间件，记录 LLM 调用和工具调用的详细信息。

### 工作原理

- **BeforeModelRewriteState**：在每次 LLM 调用前创建 `agent.llm.call` span，记录模型名和迭代次数
- **AfterModelRewriteState**：LLM 调用完成后关闭 span，写入 token 消耗（input/output/total）和耗时
- **WrapInvokableToolCall**：包裹工具调用，创建 `agent.tool.call` span，记录工具名、耗时和错误状态

### 条件加载

`TracingMiddleware` 仅在 tracing 启用时被添加到 Agent 中间件链（通过 `AgentBuilder.SetTracingEnabled`）。Tracing 关闭时完全不加载，零开销。

### 线程安全

`TracingMiddleware` 非线程安全，依赖 Eino ADK 对每个 runner 的串行调用保证。

## Trace Context 传播

### Context 传递

Go 中通过 `context.Context` 传递 Trace Context：

```go
// 创建 Span
ctx, span := tracer.Start(ctx, "agent.execute",
    trace.WithAttributes(
        attribute.String("xyncra.agent.id", agentID),
        attribute.String("xyncra.conversation.id", convID),
    ),
)
defer span.End()

// 传递到子调用
result, err := s.Store().SendMessage(ctx, msg, members) // context 携带 trace
```

### 跨 goroutine 传播

```go
go func(ctx context.Context) {
    _, span := tracer.Start(ctx, "async.task")
    defer span.End()
    // ... 处理逻辑
}(ctx)
```

### 跨 MQ 传播

通过 `mq_propagation.go` 中的 `InjectTraceContext` 和 `ExtractTraceContext` 实现 W3C Trace Context 在消息队列中的传播：

- **入队侧**：`InjectTraceContext(ctx)` 将当前 trace context 序列化为 `map[string]string`，注入到 MQ 任务元数据中
- **出队侧**：`ExtractTraceContext(metadata)` 从 MQ 任务元数据中恢复 trace context，使 worker 侧的 span 与入队侧处于同一 trace

这确保了从 WebSocket 接收到 Agent 执行的完整链路在同一个 trace 中可见。

### Debug 采样

`DebugSampler` 支持对特定用户或设备强制全量采样：

- 通过 `XYNCRA_TRACING_DEBUG_USERS` 和 `XYNCRA_TRACING_DEBUG_DEVICES` 配置
- 在 WebSocket handler 层通过 `WithDebug(ctx, userID, deviceID)` 标记 context
- 匹配 debug 列表的请求强制 `RecordAndSample`，不受采样率限制
- 非 debug 请求回退到 `TraceIDRatioBased` 采样

## 配置

所有配置通过 `XYNCRA_TRACING_*` 环境变量控制，详见 [配置参考](../onboarding/configuration.md)。

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `XYNCRA_TRACING_ENABLED` | `false` | 是否启用追踪 |
| `XYNCRA_TRACING_OTLP_ENDPOINT` | `localhost:4317` | OTLP/gRPC collector 地址 |
| `XYNCRA_TRACING_OTLP_INSECURE` | `true` | 是否禁用 TLS |
| `XYNCRA_TRACING_SAMPLING_RATE` | `1.0` | 采样率（0.0-1.0） |
| `XYNCRA_TRACING_SERVICE_NAME` | `xyncra-server` | 服务名称 |
| `XYNCRA_TRACING_DEBUG_USERS` | 空 | 强制采样的用户 ID（逗号分隔） |
| `XYNCRA_TRACING_DEBUG_DEVICES` | 空 | 强制采样的设备 ID（逗号分隔） |

## Jaeger 集成

### Docker Compose 部署

所有 docker-compose 文件都包含 Jaeger 服务定义，使用 Badger 嵌入式存储：

```yaml
jaeger:
  image: jaegertracing/all-in-one:latest
  ports:
    - "4317:4317"    # OTLP gRPC
    - "4318:4318"    # OTLP HTTP
    - "16686:16686"  # Jaeger UI
  environment:
    - SPAN_STORAGE_TYPE=badger
    - BADGER_EPHEMERAL=false
    - BADGER_DIRECTORY_KEY=/badger/data_keys
    - BADGER_DIRECTORY_VALUE=/badger/data
  volumes:
    - jaeger-badger:/badger
  healthcheck:
    test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:16686"]
    interval: 10s
    timeout: 5s
    retries: 3
```

### 各环境端口分配

| 环境 | OTLP gRPC | OTLP HTTP | Jaeger UI |
|------|-----------|-----------|-----------|
| docker-compose.yml | 4317 | 4318 | 16686 |
| docker-compose.e2e.yml | 14317 | 14318 | 16687 |
| docker-compose.multi-node.yml | 24317 | 24318 | 26686 |

### 快速启动

```bash
# 1. 启动 Jaeger
docker compose up -d jaeger

# 2. 启动 server（启用 tracing）
XYNCRA_TRACING_ENABLED=true ./bin/xyncra-server

# 3. 打开 Jaeger UI 查看链路
open http://localhost:16686
```

## 属性参考

所有 span attribute key 统一定义在 `internal/tracing/attributes.go`：

| 常量 | Key | 说明 |
|------|-----|------|
| `AttrUserID` | `xyncra.user.id` | 用户标识 |
| `AttrDeviceID` | `xyncra.device.id` | 设备标识 |
| `AttrConnID` | `xyncra.connection.id` | 连接标识 |
| `AttrMethod` | `xyncra.method` | RPC 方法名 |
| `AttrAgentID` | `xyncra.agent.id` | Agent 标识 |
| `AttrConversationID` | `xyncra.conversation.id` | 会话标识 |
| `AttrTaskType` | `xyncra.task.type` | 任务类型 |
| `AttrTaskID` | `xyncra.task.id` | 任务标识 |
| `AttrIteration` | `xyncra.iteration` | LLM 迭代次数 |
| `AttrToolName` | `xyncra.tool.name` | 工具名称 |
| `AttrModel` | `xyncra.llm.model` | LLM 模型名 |
| `AttrInputTokens` | `xyncra.llm.input_tokens` | 输入 token 数 |
| `AttrOutputTokens` | `xyncra.llm.output_tokens` | 输出 token 数 |
| `AttrTotalTokens` | `xyncra.llm.total_tokens` | 总 token 数 |
| `AttrDurationMs` | `xyncra.duration_ms` | 耗时（毫秒） |
| `AttrCheckpointID` | `xyncra.checkpoint.id` | 检查点标识 |
| `AttrChunkCount` | `xyncra.chunk_count` | 流式块数 |
| `AttrTotalChars` | `xyncra.total_chars` | 总字符数 |
| `AttrDebug` | `xyncra.debug` | 调试标记 |
| `AttrSizeBytes` | `xyncra.size_bytes` | 消息大小（字节） |
| `AttrTargetUserID` | `xyncra.target_user_id` | 目标用户 |
| `AttrTargetType` | `xyncra.target_type` | 目标类型 |

## 代码组织

追踪相关代码分布在以下包中：

| 包 | 文件 | 说明 |
|------|------|------|
| `internal/tracing` | `tracing.go` | InitTracer、Tracer 全局初始化 |
| `internal/tracing` | `config.go` | TracingConfig 配置读取 |
| `internal/tracing` | `middleware.go` | DebugSampler 采样策略 |
| `internal/tracing` | `mq_propagation.go` | W3C Trace Context MQ 传播 |
| `internal/tracing` | `attributes.go` | Span 名称和属性常量 |
| `internal/tracing` | `doc.go` | 包文档 |
| `internal/server` | `tracing_helpers.go` | WebSocket/Handler 层 span 辅助函数 |
| `internal/handler` | `tracing_helpers.go` | Broker enqueue span |
| `internal/agent` | `tracing_middleware.go` | TracingMiddleware（LLM/Tool span） |
