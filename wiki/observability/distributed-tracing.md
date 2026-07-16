# 分布式追踪

## 概述

Xyncra Server 当前未实现正式的分布式追踪。本文档描述建议的追踪设计方案，覆盖关键操作链路的 Trace Context 传播、Span 定义以及集成方案。

## 当前追踪状态

### 已有追踪基础

项目目前通过以下方式提供有限的可观测性：

1. **日志上下文**：关键操作通过日志记录开始和结束时间
2. **LLM 调用事件**：`LLMCallEvent` 记录了 Agent 执行的耗时
3. **健康检查**：内置 `/health` 端点监控服务可用性

### 缺失的追踪能力

- 跨 goroutine 的 Trace Context 传播
- 端到端的请求延迟分析
- 跨 Redis/MQ/LLM API 调用的链路追踪
- 依赖 OpenTelemetry 或类似标准

## 推荐追踪架构

### 整体架构

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│ WebSocket    │────▶│ Message      │────▶│ Store/DB     │
│ Client       │     │ Handler      │     │              │
└──────────────┘     ├──────────────┤     ├──────────────┤
                     │ Span:        │     │ Span:        │
                     │ handle-msg   │     │ db-write     │
                     └──────────────┘     └──────────────┘
                            │
                            ▼
                     ┌──────────────┐     ┌──────────────┐
                     │ Broker/MQ    │────▶│ Agent        │
                     │ (Asynq)      │     │ Executor     │
                     ├──────────────┤     ├──────────────┤
                     │ Span:        │     │ Span:        │
                     │ enqueue-task │     │ agent-exec   │
                     └──────────────┘     └──────┬───────┘
                                                  │
                                                  ▼
                                          ┌──────────────┐
                                          │ LLM API      │
                                          │ Call         │
                                          ├──────────────┤
                                          │ Span:        │
                                          │ llm-call     │
                                          └──────────────┘
```

### 关键 Span 定义

#### WebSocket 连接生命周期

| Span 名称 | 父 Span | 说明 | 属性 |
|-----------|---------|------|------|
| `ws.connect` | - | WebSocket 连接升级 | `user_id`, `device_id`, `ip` |
| `ws.disconnect` | - | WebSocket 断开 | `user_id`, `device_id`, `duration` |
| `ws.message.receive` | `ws.connect` | 接收客户端消息 | `message_type`, `size` |
| `ws.message.send` | `ws.connect` | 发送消息给客户端 | `target_user`, `message_type` |

#### 消息处理链路

| Span 名称 | 父 Span | 说明 | 属性 |
|-----------|---------|------|------|
| `handler.invoke` | `ws.message.receive` | 方法分发 | `method` |
| `handler.store.write` | `handler.invoke` | 数据库写入 | `store`, `table` |
| `handler.broker.enqueue` | `handler.invoke` | 消息队列入队 | `queue`, `task_type` |
| `handler.broadcast` | `handler.invoke` | 跨节点广播 | `targets` |

#### Agent 执行链路

| Span 名称 | 父 Span | 说明 | 属性 |
|-----------|---------|------|------|
| `agent.execute` | `handler.invoke` | Agent 执行入口 | `agent_id`, `conversation_id` |
| `agent.llm.call` | `agent.execute` | LLM 模型调用 | `model`, `input_tokens`, `max_tokens` |
| `agent.tool.call` | `agent.execute` | 工具调用 | `tool_name` |
| `agent.checkpoint.save` | `agent.execute` | 检查点保存 | `checkpoint_id` |
| `agent.stream` | `agent.execute` | 流式输出 | `chunk_size` |

#### 消息队列链路

| Span 名称 | 父 Span | 说明 | 属性 |
|-----------|---------|------|------|
| `mq.enqueue` | 调用方 | 任务入队 | `task_type`, `queue` |
| `mq.process` | `mq.enqueue` | 任务处理 | `task_id`, `retry_count` |
| `mq.retry` | `mq.process` | 任务重试 | `attempt`, `max_retries` |

## Trace Context 传播

### Context 传递模式

Go 中的 Trace Context 通过 `context.Context` 传递：

```go
// 创建 Span
ctx, span := tracer.Start(ctx, "agent.execute",
    trace.WithAttributes(
        attribute.String("agent_id", agentID),
        attribute.String("conversation_id", convID),
    ),
)
defer span.End()

// 传递到子调用
result, err := s.Store().SendMessage(ctx, msg, members) // context 携带 trace
```

### 跨 goroutine 传播

```go
// 启动 goroutine 时传递 context
go func(ctx context.Context) {
    _, span := tracer.Start(ctx, "async.task")
    defer span.End()
    // ... 处理逻辑
}(ctx)
```

### 跨进程传播（建议方案）

当前没有跨进程调用。如果未来引入：

```http
# HTTP 头传播（连接第三方 API 时）
traceparent: 00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
tracestate: vendorname1=opaqueValue1
```

对于 Redis/Asynq 消息队列，Trace Context 可以编码在任务元数据中：

```go
type TaskMetadata struct {
    TraceID  string `json:"trace_id"`
    SpanID   string `json:"span_id"`
    // ... 业务字段
}
```

## 集成方案

### 推荐：OpenTelemetry

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/sdk/resource"
    "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// 初始化 Provider
func initTracer() (*trace.TracerProvider, error) {
    exporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint("otel-collector:4317"),
    )
    if err != nil {
        return nil, err
    }

    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceNameKey.String("xyncra-server"),
            semconv.ServiceVersionKey.String(version),
        )),
    )
    otel.SetTracerProvider(tp)
    return tp, nil
}
```

### 导出后端

| 后端 | 部署方式 | 适用场景 |
|------|----------|----------|
| Jaeger | All-in-one 或 Production | 小型团队，快速搭建 |
| Tempo | 集群部署 | 大型部署，与 Grafana 集成 |
| Zipkin | 单节点 | 兼容 OpenZipkin 生态 |
| 云厂商 | 托管服务 | 阿里云链路追踪、AWS X-Ray |

### Grafana Tempo + Loki 关联

```yAML
# 通过 trace_id 关联日志和追踪
loki:
  config:
    derived_fields:
      - name: trace_id
        url: 'http://tempo:3200/trace/$${trace_id}'
        mat regex: 'trace_id=(\w+)'
```

## 手动追踪（过渡方案）

在集成 OpenTelemetry 之前，可以通过日志模拟基本追踪：

```go
// 为每个请求生成 TraceID
type TraceContext struct {
    TraceID string
    SpanID  string
    ParentSpanID string
}

func StartTrace(ctx context.Context, name string) (*TraceContext, context.Context) {
    traceID := uuid.New().String()
    spanID := uuid.New().String()[:8]
    
    // 将 TraceID 注入日志
    ctx = context.WithValue(ctx, traceKey, &TraceContext{
        TraceID: traceID,
        SpanID:  spanID,
    })
    
    logger.Info("trace: started",
        "trace_id", traceID,
        "span", name,
    )
    
    return &TraceContext{TraceID: traceID, SpanID: spanID}, ctx
}

func EndTrace(tc *TraceContext, name string) {
    logger.Info("trace: ended",
        "trace_id", tc.TraceID,
        "span", name,
    )
}
```

## 实施优先级

| 优先级 | 追踪能力 | 预估工作量 | 说明 |
|--------|----------|------------|------|
| P0 | LLM 调用追踪 | 低 | 已有 LLMCallEvent 数据 |
| P0 | WebSocket 连接追踪 | 低 | 已有连接生命周期事件 |
| P1 | Agent 执行全链路 | 中 | 从消息入队到 Agent 响应 |
| P1 | 消息投递延迟追踪 | 中 | 端到端消息延迟分析 |
| P2 | 跨节点追踪 | 高 | 多节点部署时启用 |
| P3 | 集成 OpenTelemetry | 高 | 标准化追踪体系 |
