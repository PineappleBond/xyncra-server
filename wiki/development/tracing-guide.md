# 分布式追踪指南

Xyncra Server 使用 OpenTelemetry 进行分布式追踪，采用**手动业务级 span**（见 D-127）。本文档面向开发者，说明如何在代码中添加和正确使用追踪。

## 架构概览

追踪采用两层 span 结构：

### 触发层（Trigger Layer）

出现在 Jaeger Operation 列表中的高层业务操作：

- `ws.connection` — WebSocket 连接生命周期
- `ws.message.receive` — 接收 WebSocket 消息
- `handler.invoke` — RPC 方法分发
- `handler.broadcast` — 跨节点广播
- `mq.process` — MQ 任务处理
- `agent.execute` / `agent.build` / `agent.run` — Agent 执行入口
- `agent.llm.call` / `agent.tool.call` — LLM 与工具调用
- `agent.stream` / `agent.checkpoint.save` — 流式输出与检查点

### 业务层（Business Layer）

以子 span 形式存在于触发层之下，**不出现在 Operation 列表**中：

- `db.<entity>.<operation>` — 数据库操作（如 `db.conversation.get`、`db.message.create`）
- `redis.<store>.<operation>` — Redis 操作（如 `redis.connection.add`、`redis.pending.save`）

这种设计确保 Jaeger Operation 列表保持高信噪比，开发者可快速定位真正的业务操作。

---

## 为新方法添加追踪

### Step 1：添加 span 常量

在 `internal/tracing/attributes.go` 中添加新的 span 名称常量。

数据库操作：

```go
// DB layer span names (GORM store methods).
const (
    // ...existing constants...
    SpanDBMyEntityMyOperation = "db.my_entity.my_operation"
)
```

Redis 操作：

```go
// Redis layer span names.
const (
    // ...existing constants...
    SpanRedisMyStoreMyOperation = "redis.my_store.my_operation"
)
```

**命名规范**：`db.<entity>.<operation>` 或 `redis.<store>.<operation>`

### Step 2：在方法中使用 span

#### DB 方法（`internal/store/`）

使用 `internal/store/tracing_helpers.go` 中的 `startSpan` 函数：

```go
func (s *MyStore) MyOperation(ctx context.Context, id string) (result *Model, err error) {
    ctx, finish := startSpan(ctx, tracing.SpanDBMyEntityMyOperation,
        attribute.String(tracing.AttrConversationID, id))
    defer func() { finish(err) }()

    // ... 实际业务逻辑 ...
}
```

#### Redis 方法（`internal/server/`）

使用 `internal/server/tracing_helpers.go` 中的 `startRedisSpan` 函数：

```go
func (s *RedisMyStore) MyOperation(ctx context.Context, id string) (result *Info, err error) {
    ctx, finish := startRedisSpan(ctx, tracing.SpanRedisMyStoreMyOperation,
        attribute.String(tracing.AttrConnID, id))
    defer func() { finish(err) }()

    // ... 实际业务逻辑 ...
}
```

### 关键要求

1. **必须使用命名返回值** — `err error` 作为命名返回值，这样 defer 闭包中的 `finish(err)` 才能捕获到方法返回时的实际错误

2. **使用 `tracing.Attr*` 常量** — 所有属性 key 必须使用 `internal/tracing/attributes.go` 中定义的常量（它们带有 `xyncra.` 前缀），不要使用裸字符串

3. **context 自动传播** — 嵌套的方法调用会自动创建父子 span 关系，因为 `startSpan` 返回的 `ctx` 携带了 trace context。只需确保将返回的 `ctx` 传递给后续调用

---

## 命名规范

| 模式 | 示例 | 适用场景 |
| ---- | ---- | -------- |
| `db.<entity>.<op>` | `db.conversation.get` | GORM store 方法 |
| `redis.<store>.<op>` | `redis.connection.add` | Redis 方法 |
| `ws.<action>` | `ws.connection` | WebSocket 层 |
| `handler.<action>` | `handler.invoke` | Handler 层 |
| `mq.<action>` | `mq.process` | 消息队列 |
| `agent.<action>` | `agent.llm.call` | Agent 层 |

---

## 配置

追踪通过 CLI 标志或环境变量配置：

| 环境变量 | CLI 标志 | 默认值 | 说明 |
| -------- | -------- | ------ | ---- |
| `XYNCRA_TRACING_ENABLED` | `-tracing-enabled` | `false` | 是否启用追踪 |
| `XYNCRA_TRACING_OTLP_ENDPOINT` | `-tracing-endpoint` | `localhost:4317` | OTLP gRPC collector 地址 |
| `XYNCRA_TRACING_SAMPLING_RATE` | `-tracing-sampling-rate` | `1.0` | 采样率（0.0-1.0） |

**当追踪禁用时**（默认值 `false`），`tracing.InitTracer` 安装 no-op TracerProvider。所有 `tracer.Start()` 调用返回零分配的 noop span，**运行时零开销**。

完整配置参考见 [配置文档](../onboarding/configuration.md)。

---

## 代码组织

追踪相关代码分布在以下包中：

| 包 | 文件 | 说明 |
| -- | ---- | ---- |
| `internal/tracing` | `attributes.go` | Span 名称和属性 key 常量 |
| `internal/tracing` | `tracing.go` | `InitTracer` 初始化 |
| `internal/tracing` | `middleware.go` | DebugSampler 采样策略 |
| `internal/tracing` | `mq_propagation.go` | W3C Trace Context MQ 传播 |
| `internal/store` | `tracing_helpers.go` | DB 层 `startSpan` 辅助函数 |
| `internal/server` | `tracing_helpers.go` | Redis 层 `startRedisSpan` 辅助函数 |
| `internal/agent` | `tracing_middleware.go` | TracingMiddleware（LLM/Tool span） |

---

## 相关文档

- [分布式追踪](../observability/distributed-tracing.md) — 完整的追踪架构和 span 定义
- [架构决策 ADR-013](../architecture/design-decisions.md#adr-013手动业务级追踪而非自动基础设施追踪) — D-127 决策记录
- [配置参考](../onboarding/configuration.md) — 所有配置项
