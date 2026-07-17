---
last_updated: 2026-07-17
---

# Prometheus Metrics

> last_updated: 2026-07-17

## Overview

Xyncra Server exposes 32 Prometheus metrics via the `/metrics` HTTP endpoint on the same port as the WebSocket server (default `:8080`). This is enabled by the `XYNCRA_METRICS_ENABLED=true` environment variable (opt-in, per D-063 optional module pattern).

The metrics endpoint is registered via `WSWithExtraRoutes` (D-128) on the same HTTP mux as `/health`, avoiding the need for a separate management port. The server package does not depend on Prometheus -- the `Route` abstraction decouples the two.

All debug/metrics endpoints are only accessible within the internal network (D-003 deployment model).

## Endpoint

```text
GET /metrics
```

Returns Prometheus exposition format. Requires `XYNCRA_METRICS_ENABLED=true`.

## Configuration

| Environment Variable | Default | Description |
|----------------------|---------|-------------|
| `XYNCRA_METRICS_ENABLED` | `false` | Enable Prometheus metrics collection and `/metrics` endpoint |

When disabled (default), no metrics collectors are created and the `/metrics` route is not registered. Zero overhead when unused.

## Implemented Metrics (32 total)

### System Metrics (6) -- Go Runtime & Service Health

Collected every 10 seconds by the runtime collector in `internal/metrics/runtime.go`. These metrics monitor the Go runtime and process health. They are essential for capacity planning and detecting resource exhaustion.

| Metric | Type | Help | Ops Value |
|--------|------|------|-----------|
| `xyncra_goroutines` | Gauge | Current goroutine count | Goroutine leak detection |
| `xyncra_memory_alloc_bytes` | Gauge | Allocated heap memory in bytes | Memory pressure alerting |
| `xyncra_memory_inuse_bytes` | Gauge | Heap memory in use in bytes | Memory footprint tracking |
| `xyncra_gc_duration_seconds` | Summary | GC pause duration (p50/p90/p99) | GC overhead monitoring |
| `xyncra_gc_count` | Gauge | Total GC cycles completed | GC frequency tracking |
| `xyncra_open_fds` | Gauge | Open file descriptors | FD exhaustion detection |

### Connection Metrics (3) -- WebSocket Connection State

Track connection lifecycle and capacity. Critical for detecting connection storms and leaks.

| Metric | Type | Help | Ops Value |
|--------|------|------|-----------|
| `xyncra_connections_active` | Gauge | Current active WebSocket connections | Capacity planning, leak detection |
| `xyncra_connections_total` | Counter | Total connections since start | Connection rate tracking |
| `xyncra_connections_duration_seconds` | Histogram | Connection duration distribution (exponential buckets, 1s start, 2x factor, 15 buckets) | Connection stability analysis |

### Message Metrics (4) -- Message Processing Throughput

Monitor message flow and delivery health. Key indicators of system throughput.

| Metric | Type | Help | Ops Value |
|--------|------|------|-----------|
| `xyncra_messages_sent_total` | Counter | Total messages sent | Throughput tracking |
| `xyncra_messages_received_total` | Counter | Total messages received | Inbound load monitoring |
| `xyncra_message_size_bytes` | Histogram | Message size distribution (exponential buckets, 64B start, 2x factor, 12 buckets) | Payload size analysis |
| `xyncra_message_latency_seconds` | Histogram | Message delivery latency (default buckets) | Delivery SLA monitoring |

### Agent Metrics (9) -- AI Agent Execution & LLM

Monitor agent runtime performance and LLM integration health. Essential for cost control and SLA monitoring.

| Metric | Type | Labels | Help | Ops Value |
|--------|------|--------|------|-----------|
| `xyncra_agent_executions_total` | CounterVec | `agent_id`, `model` | Total agent executions | Execution rate tracking |
| `xyncra_agent_executions_failed_total` | CounterVec | `agent_id`, `error` | Total failed agent executions | Error rate alerting |
| `xyncra_agent_duration_seconds` | HistogramVec | `agent_id`, `model` | Agent execution duration (default buckets) | Latency SLA monitoring |
| `xyncra_agent_active` | Gauge | -- | Currently active agent executions | Concurrency monitoring |
| `xyncra_agent_queue_depth` | Gauge | -- | Agent task queue depth | Queue backlog alerting |
| `xyncra_llm_tokens_input_total` | CounterVec | `agent_id`, `model` | Total LLM input tokens | Cost tracking (input) |
| `xyncra_llm_tokens_output_total` | CounterVec | `agent_id`, `model` | Total LLM output tokens | Cost tracking (output) |
| `xyncra_llm_calls_total` | CounterVec | `agent_id`, `model` | Total LLM calls | LLM usage tracking |
| `xyncra_llm_calls_failed_total` | CounterVec | `agent_id`, `model`, `error` | Total failed LLM calls | LLM error rate alerting |

The `error` label on `xyncra_agent_executions_failed_total` and `xyncra_llm_calls_failed_total` uses classified values from a fixed set to prevent unbounded cardinality: `timeout`, `rate_limit`, `context_length`, `other`.

### Business Metrics (6) -- Application-Level Operations

Track business-layer operations. Provide visibility into feature usage and system correctness.

| Metric | Type | Labels | Help | Ops Value |
|--------|------|--------|------|-----------|
| `xyncra_conversations_active` | Gauge | -- | Active conversations | Load indicator |
| `xyncra_conversations_created_total` | Counter | -- | Total conversations created | Growth tracking |
| `xyncra_devices_connected` | Gauge | -- | Connected devices | Device fleet size |
| `xyncra_functions_registered` | GaugeVec | `device_id` | Registered functions per device | Tool capability tracking |
| `xyncra_reverse_rpc_requests_total` | Counter | -- | Total reverse RPC requests | Bidirectional RPC usage |
| `xyncra_reverse_rpc_failed_total` | Counter | -- | Total failed reverse RPC requests | RPC failure alerting |

### Redis Metrics (4) -- Infrastructure Dependency Health

Monitor Redis connectivity and async task queue health. Redis is a critical dependency -- these metrics enable early detection of infrastructure issues.

| Metric | Type | Labels | Help | Ops Value |
|--------|------|--------|------|-----------|
| `xyncra_redis_connected` | Gauge | -- | Redis connection status (1=connected, 0=disconnected) | Infrastructure health |
| `xyncra_redis_ping_duration_seconds` | Histogram | -- | Redis ping latency (default buckets) | Redis latency degradation |
| `xyncra_redis_pool_size` | Gauge | -- | Redis connection pool size | Connection pool saturation |
| `xyncra_asynq_queue_size` | GaugeVec | `queue` | Asynq queue depth | Task backlog alerting |

## Prometheus Configuration

Add to `deploy/prometheus/prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'xyncra'
    scrape_interval: 15s
    static_configs:
      - targets: ['xyncra-server:8080']
    metrics_path: /metrics
```

## Example Queries

```promql
# Active connections
xyncra_connections_active

# Message rate (per second)
rate(xyncra_messages_sent_total[5m])

# Agent error rate
rate(xyncra_agent_executions_failed_total[5m])
  / rate(xyncra_agent_executions_total[5m])

# LLM P95 latency per agent
histogram_quantile(0.95,
  rate(xyncra_agent_duration_seconds_bucket{agent_id="weather-bot"}[5m]))

# Redis pool utilization
xyncra_redis_pool_size

# Asynq queue backlog
xyncra_asynq_queue_size

# Goroutine trend
deriv(xyncra_goroutines[10m])
```

## Deprecation Policy

When a metric is no longer needed:

1. Mark as `DEPRECATED` in code comments with the replacement metric
2. Keep the metric for at least one release cycle
3. Document the migration in CHANGELOG
4. Remove in the following release

## Related

- [Distributed Tracing](distributed-tracing.md) -- OpenTelemetry tracing
- [Logging](../devops/logging.md) -- Structured logging configuration
- [Profiling](../devops/profiling.md) -- pprof and Pyroscope
- [Alerting](../devops/alerting.md) -- Alert rules based on these metrics
