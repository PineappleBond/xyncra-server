# Metrics 埋点

## 概述

Xyncra Server 目前通过日志方式暴露指标，尚未集成 Prometheus 客户端。本文档定义建议采集的指标体系和实施方案。

## 当前指标能力

### LLMMetrics 接口

通过 `internal/agent/monitoring.go` 定义的指标接口：

```go
type LLMMetrics interface {
    Record(ctx context.Context, event LLMCallEvent)
}

type LLMCallEvent struct {
    AgentID      string
    Model        string
    Duration     time.Duration
    InputTokens  int
    OutputTokens int
    Error        error
}
```

当前通过 `LogMetrics` 实现将 LLM 调用事件记录到日志。

### 健康检查端点

`GET /health` 返回：
- 连接数
- 健康状态（ok/degraded）

## 建议的指标分类

### 系统指标

用于监控 Go 运行时和服务健康状况：

| 指标名称 | 类型 | 说明 | 建议标签 |
|----------|------|------|----------|
| `xyncra_goroutines` | Gauge | 当前 goroutine 数量 | - |
| `xyncra_memory_alloc_bytes` | Gauge | 已分配堆内存 | - |
| `xyncra_memory_inuse_bytes` | Gauge | 使用中的堆内存 | - |
| `xyncra_gc_duration_seconds` | Histogram | GC 暂停时间 | - |
| `xyncra_gc_count` | Counter | GC 次数累计 | - |
| `xyncra_cpu_usage` | Gauge | CPU 使用率 | - |
| `xyncra_open_fds` | Gauge | 打开文件描述符数 | - |

### 连接指标

用于监控 WebSocket 连接状态：

| 指标名称 | 类型 | 说明 | 建议标签 |
|----------|------|------|----------|
| `xyncra_connections_active` | Gauge | 当前活跃连接数 | `user_id` |
| `xyncra_connections_total` | Counter | 累计连接总数 | - |
| `xyncra_connections_per_user` | Gauge | 每用户连接数 | `user_id` |
| `xyncra_connections_per_device` | Gauge | 每设备连接数 | `user_id`, `device_id` |
| `xyncra_connections_duration_seconds` | Histogram | 连接时长分布 | - |

### 消息指标

用于监控消息处理吞吐量：

| 指标名称 | 类型 | 说明 | 建议标签 |
|----------|------|------|----------|
| `xyncra_messages_sent_total` | Counter | 消息发送总数 | `conversation_id` |
| `xyncra_messages_received_total` | Counter | 消息接收总数 | - |
| `xyncra_messages_per_second` | Gauge | 每秒消息处理数 | - |
| `xyncra_message_size_bytes` | Histogram | 消息大小分布 | - |
| `xyncra_message_latency_seconds` | Histogram | 消息投递延迟 | - |

### Agent 指标

用于监控 AI Agent 执行情况：

| 指标名称 | 类型 | 说明 | 建议标签 |
|----------|------|------|----------|
| `xyncra_agent_executions_total` | Counter | Agent 执行总数 | `agent_id`, `model` |
| `xyncra_agent_executions_failed_total` | Counter | Agent 执行失败数 | `agent_id`, `error` |
| `xyncra_agent_duration_seconds` | Histogram | Agent 执行耗时 | `agent_id`, `model` |
| `xyncra_agent_active` | Gauge | 当前活跃 Agent 数 | - |
| `xyncra_agent_queue_depth` | Gauge | Agent 任务队列深度 | - |
| `xyncra_llm_tokens_input_total` | Counter | LLM 输入 Token 总数 | `agent_id`, `model` |
| `xyncra_llm_tokens_output_total` | Counter | LLM 输出 Token 总数 | `agent_id`, `model` |
| `xyncra_llm_calls_total` | Counter | LLM 调用总数 | `agent_id`, `model` |
| `xyncra_llm_calls_failed_total` | Counter | LLM 调用失败数 | `agent_id`, `model`, `error` |

### 业务指标

用于监控业务层面的运行情况：

| 指标名称 | 类型 | 说明 | 建议标签 |
|----------|------|------|----------|
| `xyncra_conversations_active` | Gauge | 活跃会话数 | - |
| `xyncra_conversations_created_total` | Counter | 会话创建总数 | - |
| `xyncra_devices_connected` | Gauge | 已连接设备数 | - |
| `xyncra_functions_registered` | Gauge | 注册的函数数 | `device_id` |
| `xyncra_reverse_rpc_requests_total` | Counter | 反向 RPC 请求数 | - |
| `xyncra_reverse_rpc_failed_total` | Counter | 反向 RPC 失败数 | - |

### Redis 指标

用于监控 Redis 依赖状态：

| 指标名称 | 类型 | 说明 | 建议标签 |
|----------|------|------|----------|
| `xyncra_redis_connected` | Gauge | Redis 连接状态（1/0） | - |
| `xyncra_redis_ping_duration_seconds` | Histogram | Redis Ping 延迟 | - |
| `xyncra_redis_pool_size` | Gauge | 连接池大小 | - |
| `xyncra_asynq_queue_size` | Gauge | 消息队列深度 | `queue` |

## 指标导出格式

### 建议：Prometheus 格式

通过 `/metrics` HTTP 端点暴露指标：

```
# HELP xyncra_connections_active Current active WebSocket connections
# TYPE xyncra_connections_active gauge
xyncra_connections_active 42

# HELP xyncra_llm_calls_total Total LLM calls
# TYPE xyncra_llm_calls_total counter
xyncra_llm_calls_total{agent_id="weather-bot",model="qwen3.7-plus"} 150
xyncra_llm_calls_total{agent_id="mcp-bot",model="gpt-4"} 75

# HELP xyncra_agent_duration_seconds Agent execution duration
# TYPE xyncra_agent_duration_seconds histogram
xyncra_agent_duration_seconds_bucket{agent_id="weather-bot",le="1"} 50
xyncra_agent_duration_seconds_bucket{agent_id="weather-bot",le="5"} 120
xyncra_agent_duration_seconds_bucket{agent_id="weather-bot",le="+Inf"} 150
xyncra_agent_duration_seconds_sum{agent_id="weather-bot"} 450
xyncra_agent_duration_seconds_count{agent_id="weather-bot"} 150
```

### 实现思路

使用 `prometheus/client_golang`：

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
    activeConnections = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "xyncra_connections_active",
        Help: "Current active WebSocket connections",
    })

    llmCallsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "xyncra_llm_calls_total",
            Help: "Total LLM calls",
        },
        []string{"agent_id", "model"},
    )

    agentDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "xyncra_agent_duration_seconds",
            Help:    "Agent execution duration",
            Buckets: prometheus.DefBuckets,
        },
        []string{"agent_id"},
    )
)

func init() {
    prometheus.MustRegister(activeConnections)
    prometheus.MustRegister(llmCallsTotal)
    prometheus.MustRegister(agentDuration)
}

// 在 /metrics 端点注册
mux.Handle("/metrics", promhttp.Handler())
```

## 当前实现建议

### 从日志导出指标（替代方案）

在集成 Prometheus 客户端之前，可以通过日志解析器导出指标：

```bash
# 从 LLM 日志提取 Prometheus 格式指标
cat /app/llm-logs/llm-calls.log | \
  jq -r 'select(.msg == "llm call completed") | 
    "xyncra_llm_calls_total{agent_id=\"\(.agent_id)\",model=\"\(.model)\"} 1"' | \
  sort | uniq -c | \
  awk '{print $2, $1}'
```

### LLM 调用日志结构化指标

当前 `LogMetrics.Record` 输出已是结构化的键值对格式，可直接被日志采集系统解析：

```go
func (m *LogMetrics) Record(ctx context.Context, event LLMCallEvent) {
    if event.Error != nil {
        m.logger.Error("llm call failed",
            "agent_id", event.AgentID,
            "model", event.Model,
            "duration_ms", event.Duration.Milliseconds(),
            "error", event.Error,
        )
        return
    }
    m.logger.Info("llm call completed",
        "agent_id", event.AgentID,
        "model", event.Model,
        "duration_ms", event.Duration.Milliseconds(),
        "input_tokens", event.InputTokens,
        "output_tokens", event.OutputTokens,
    )
}
```

## 弃用指标

当某个指标不再需要时：
1. 记录弃用原因
2. 标记为 `DEPRECATED` 并说明替代指标
3. 至少保留一个发布周期再移除
4. 在 CHANGELOG 中说明

## 集成建议

### 监控堆栈推荐

```
Xyncra Server → Prometheus → Grafana Dashboard
                 ↓
           AlertManager → 通知渠道
```

### 快速开始

```bash
# 1. 添加依赖
go get github.com/prometheus/client_golang

# 2. 注册 /metrics 端点
mux.Handle("/metrics", promhttp.Handler())

# 3. 配置 Prometheus
# prometheus.yml
scrape_configs:
  - job_name: 'xyncra'
    static_configs:
      - targets: ['localhost:8080']

# 4. 导入 Grafana Dashboard
```
