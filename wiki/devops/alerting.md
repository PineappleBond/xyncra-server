---
last_updated: 2026-07-17
---

# Alert Rules

> last_updated: 2026-07-17

## Overview

Xyncra Server ships with 11 pre-defined Prometheus alert rules in `prometheus/alerts.yml`. Alerts are grouped by domain and follow a high signal-to-noise philosophy (D-127). Each alert is designed to catch a specific failure mode with minimal false positives.

## Alert Rules

Alert rules are defined in `prometheus/alerts.yml` and loaded by Prometheus. The evaluation interval is 15 seconds (configured in `prometheus/prometheus.yml`).

### System Alerts

Detect runtime resource exhaustion.

| Alert | Expression | Duration | Severity | Description |
|-------|-----------|----------|----------|-------------|
| `HighGoroutineCount` | `xyncra_goroutines > 10000` | 5m | warning | Possible goroutine leak |
| `HighMemoryUsage` | `xyncra_memory_alloc_bytes > 1e9` | 5m | warning | Heap memory exceeds 1 GB |

### Connection Alerts

Detect abnormal connection patterns.

| Alert | Expression | Duration | Severity | Description |
|-------|-----------|----------|----------|-------------|
| `HighConnectionCount` | `xyncra_connections_active > 1000` | 5m | warning | Unusually high active connections |
| `ConnectionSpike` | `rate(xyncra_connections_total[1m]) > 100` | 2m | critical | Connection storm -- more than 100 new connections per second |

### Agent Alerts

Detect AI agent execution issues.

| Alert | Expression | Duration | Severity | Description |
|-------|-----------|----------|----------|-------------|
| `HighLLMErrorRate` | `rate(xyncra_llm_calls_failed_total[5m]) / rate(xyncra_llm_calls_total[5m]) > 0.1` | 5m | critical | More than 10% of LLM calls failing |
| `SlowLLMResponse` | `histogram_quantile(0.95, rate(xyncra_agent_duration_seconds_bucket[5m])) > 30` | 5m | warning | Agent P95 latency exceeds 30 seconds |
| `AgentExecutionFailure` | `increase(xyncra_agent_executions_failed_total[5m]) > 0` | 1m | warning | Any agent execution failure in the last 5 minutes |

### Redis Alerts

Detect Redis infrastructure issues.

| Alert | Expression | Duration | Severity | Description |
|-------|-----------|----------|----------|-------------|
| `RedisDown` | `xyncra_redis_connected == 0` | 1m | critical | Redis is disconnected |
| `HighRedisLatency` | `histogram_quantile(0.95, rate(xyncra_redis_ping_duration_seconds_bucket[5m])) > 0.1` | 5m | warning | Redis P95 ping latency exceeds 100ms |

### Message Alerts

Detect message delivery issues.

| Alert | Expression | Duration | Severity | Description |
|-------|-----------|----------|----------|-------------|
| `MessageLatencyHigh` | `histogram_quantile(0.95, rate(xyncra_message_latency_seconds_bucket[5m])) > 5` | 5m | warning | Message P95 delivery latency exceeds 5 seconds |
| `MessageQueueBacklog` | `xyncra_asynq_queue_size > 1000` | 5m | warning | Asynq task queue depth exceeds 1000 |

## AlertManager Configuration

AlertManager is configured in `alertmanager/alertmanager.yml`:

```yaml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
  receiver: 'webhook'

receivers:
  - name: 'webhook'
    webhook_configs:
      - url: 'http://host.docker.internal:5001/webhook'
        send_resolved: true
```

### Configuration Parameters

| Parameter | Value | Description |
|-----------|-------|-------------|
| `resolve_timeout` | 5m | Time before marking an alert as resolved |
| `group_by` | `alertname` | Group alerts by name to reduce notification noise |
| `group_wait` | 30s | Wait time before sending initial notification for a group |
| `group_interval` | 5m | Wait time before re-sending notifications for existing groups |
| `repeat_interval` | 4h | Minimum time between re-sending notifications for firing alerts |

## Webhook Notification

The default receiver sends alerts to `http://host.docker.internal:5001/webhook`. Replace this URL with your notification endpoint:

- Slack incoming webhook
- WeCom (Enterprise WeChat) webhook
- DingTalk webhook
- PagerDuty integration
- Custom webhook handler

### Webhook Payload Format

AlertManager sends a JSON payload with the following structure:

```json
{
  "receiver": "webhook",
  "status": "firing",
  "alerts": [
    {
      "status": "firing",
      "labels": {
        "alertname": "RedisDown",
        "severity": "critical"
      },
      "annotations": {},
      "startsAt": "2026-07-17T10:30:00Z",
      "endsAt": "0001-01-01T00:00:00Z"
    }
  ]
}
```

## Customizing Alert Rules

### Modifying Existing Rules

Edit `prometheus/alerts.yml` and restart Prometheus:

```bash
docker compose --profile observability restart prometheus
```

### Adding New Rules

Add rules under the appropriate group in `prometheus/alerts.yml`:

```yaml
groups:
  - name: xyncra-custom
    rules:
      - alert: CustomAlert
        expr: <promql expression>
        for: 5m
        labels:
          severity: warning
```

### Adjusting Thresholds

Thresholds should be tuned to your deployment size. For example, `HighConnectionCount` uses 1000 as a default, but larger deployments may need a higher threshold:

```yaml
- alert: HighConnectionCount
  expr: xyncra_connections_active > 5000  # Adjusted for larger deployment
  for: 5m
  labels:
    severity: warning
```

## Running the Stack

```bash
# Start with observability stack
docker compose --profile observability up -d

# Verify Prometheus is running
curl http://localhost:9090/-/healthy

# Verify AlertManager is running
curl http://localhost:9093/-/healthy

# View active alerts in Prometheus UI
open http://localhost:9090/alerts
```

## Related

- [Metrics](../observability/metrics.md) -- Prometheus metrics used by alert rules
- [Monitoring & Alerting](monitoring-alerting.md) -- Dashboard and runbook guide
- [Logging](logging.md) -- Structured logging configuration
- [Profiling](profiling.md) -- Performance profiling
