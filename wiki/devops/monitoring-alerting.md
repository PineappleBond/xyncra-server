---
last_updated: 2026-07-17
---

# 监控告警

## 概述

Xyncra Server 当前提供基础的日志级别监控。本文档描述可用的监控维度、建议的告警规则以及推荐的监控 Dashboard 配置。

## 当前监控能力

### 日志级别指标

通过 `internal/agent/monitoring.go` 中的 `LLMMetrics` 接口和 `LogMetrics` 实现：

**LLM 调用事件**：
```go
type LLMCallEvent struct {
    AgentID      string
    Model        string
    Duration     time.Duration
    InputTokens  int
    OutputTokens int
    Error        error
}
```

**日志输出**：
```
# 成功调用
[INFO] llm call completed agent_id=weather-bot model=qwen3.7-plus duration_ms=1234 input_tokens=500 output_tokens=200

# 失败调用
[ERROR] llm call failed agent_id=weather-bot model=qwen3.7-plus duration_ms=5000 error="context deadline exceeded"
```

### 健康检查端点

`GET /health` 返回：
```json
{"status":"ok","connections":42}
```

- `status`: `"ok"` 或 `"degraded"`（Redis 不可达时）
- `connections`: 当前活跃 WebSocket 连接数

### 健康检查失败

当 Redis ConnectionStore Ping 失败时：
- HTTP 状态码变为 `503 Service Unavailable`
- `status` 变为 `"degraded"`
- 日志记录错误

## 建议的监控指标

### 系统级指标

| 指标名称 | 类型 | 说明 | 采集方式 |
|----------|------|------|----------|
| `xyncra_connections_active` | Gauge | 当前活跃 WebSocket 连接数 | `/health` 端点 |
| `xyncra_connections_total` | Counter | 累计连接总数 | 日志聚合 |
| `xyncra_messages_sent_total` | Counter | 累计消息发送数 | 日志聚合 |
| `xyncra_llm_calls_total` | Counter | LLM 调用总数 | LLM 日志 |
| `xyncra_llm_calls_failed_total` | Counter | LLM 调用失败数 | LLM 日志 |
| `xyncra_llm_duration_ms` | Histogram | LLM 调用耗时分布 | LLM 日志 |
| `xyncra_llm_tokens_total` | Counter | LLM Token 消耗总数 | LLM 日志 |

### Go 运行时指标

| 指标名称 | 说明 | 采集方式 |
|----------|------|----------|
| `go_goroutines` | Goroutine 数量 | Go runtime |
| `go_memstats_alloc_bytes` | 已分配内存 | Go runtime |
| `go_memstats_heap_inuse` | 堆内存使用 | Go runtime |
| `go_gc_duration_seconds` | GC 暂停时间 | Go runtime |

### 业务指标

| 指标名称 | 说明 | 计算方式 |
|----------|------|----------|
| 活跃 Agent 数量 | 当前正在执行的 Agent 数 | Agent Executor 计数 |
| Agent 平均响应时间 | Agent 从接收到响应的平均耗时 | LLM 日志聚合 |
| 消息延迟 | 消息从发送到投递的延迟 | 时间戳差值 |
| 错误率 | 请求中错误响应的比例 | 错误日志 / 总日志 |

## 告警规则

### 建议的告警规则

| 规则名称 | 条件 | 严重级别 | 说明 |
|----------|------|----------|------|
| ServerDown | `/health` 返回 503 超过 30 秒 | Critical | 服务器宕机或 Redis 不可用 |
| ConnectionSpike | 连接数 5 分钟内增长 > 100% | Warning | 可能是异常流量或 DDoS |
| LLMErrorRate | LLM 调用错误率 > 10%（5分钟窗口） | Warning | LLM API 出现问题 |
| LLMLatency | LLM 平均耗时 > 30 秒 | Warning | 模型响应缓慢 |
| HighMemory | 内存使用 > 80% | Warning | 可能需要扩容 |
| HighGoroutine | Goroutine 数 > 10000 | Warning | 可能存在 goroutine 泄漏 |
| DatabaseError | 数据库错误率 > 1% | Critical | 数据库连接问题 |

### 告警通知方式

| 级别 | 通知方式 | 响应时间 |
|------|----------|----------|
| Critical | 电话 / PagerDuty / 钉钉 | 15 分钟 |
| Warning | 邮件 / Slack / 企业微信 | 1 小时 |
| Info | Dashboard 展示 | 不触发通知 |

## Dashboard 推荐

### 建议的 Grafana Dashboard 面板

#### 第一行：服务概览

| 面板 | 指标 | 类型 |
|------|------|------|
| 服务状态 | 健康检查状态 | Stat |
| 活跃连接数 | 当前 WebSocket 连接数 | Stat |
| 消息吞吐量 | 每秒消息数 | Time Series |
| 错误率 | 错误请求占比 | Time Series |

#### 第二行：LLM 监控

| 面板 | 指标 | 类型 |
|------|------|------|
| LLM 调用量 | 每分钟调用次数 | Time Series |
| LLM 延迟 | P50/P95/P99 耗时 | Time Series |
| LLM 错误率 | 失败调用占比 | Time Series |
| Token 消耗 | Input/Output Token 分布 | Time Series |

#### 第三行：系统资源

| 面板 | 指标 | 类型 |
|------|------|------|
| CPU 使用率 | 系统 CPU 使用 | Time Series |
| 内存使用 | Go 运行时内存 | Time Series |
| Goroutine 数 | 运行中 Goroutine | Time Series |
| GC 统计 | GC 频率和耗时 | Time Series |

#### 第四行：业务指标

| 面板 | 指标 | 类型 |
|------|------|------|
| 活跃 Agent | 并发执行数 | Time Series |
| 消息延迟 | P50/P95/P99 | Time Series |
| 用户活跃度 | DAU/MAU | Time Series |
| Agent 排行 | 各 Agent 调用量 | Bar Chart |

## 日志聚合

### 推荐方案

| 场景 | 推荐工具 | 说明 |
|------|----------|------|
| 单节点 | 直接查看文件 | `tail -f` 观察 |
| 多节点 | Loki + Grafana | 轻量级日志聚合 |
| 大型部署 | ELK Stack | 功能全面的日志平台 |
| 云端 | 云厂商日志服务 | 阿里云 SLS、AWS CloudWatch |

### 关键日志模式

**需要监控的日志模式**：

```
# 连接事件
"client connected" / "client disconnected"

# 错误事件
"[ERROR]" / "error:" / "failed"

# 健康检查变化
"health check: connection store ping failed"

# LLM 事件
"llm call completed" / "llm call failed"

# 启动/关闭事件
"starting xyncra-server" / "server stopped"
```

## 当前限制

1. **无原生 Prometheus 指标导出**：目前通过日志暴露指标，未集成 Prometheus client
2. **无结构化指标收集**：当前指标分散在日志中，需要通过日志解析提取
3. **无内置告警引擎**：告警需要通过外部系统（如 Prometheus + AlertManager）实现
4. **追踪覆盖有限**：分布式追踪已实现（见 [分布式追踪](../observability/distributed-tracing.md)），但仅覆盖核心业务路径，非所有 HTTP/DB/Redis 操作均有 span

## 告警通知配置示例

### 企业微信 Webhook

```bash
#!/bin/bash
# 发送告警到企业微信

WEBHOOK_URL="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=YOUR_KEY"

curl -s -X POST $WEBHOOK_URL \
  -H "Content-Type: application/json" \
  -d '{
    "msgtype": "markdown",
    "markdown": {
      "content": "## 🔴 Xyncra 告警\n> **级别**: 严重\n> **规则**: ServerDown\n> **详情**: Redis 连接丢失超过 30 秒\n> **时间**: 2024-01-01 10:00:00\n> **节点**: node-1"
    }
  }'
```

### Slack Webhook

```bash
#!/bin/bash
# 发送告警到 Slack

WEBHOOK_URL="https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXX"

curl -s -X POST $WEBHOOK_URL \
  -H "Content-Type: application/json" \
  -d '{
    "text": "🔴 *Xyncra 告警*\n*级别*: 严重\n*规则*: ServerDown\n*详情*: Redis 连接丢失超过 30 秒",
    "attachments": [{"color": "danger", "fields": [{"title": "节点", "value": "node-1", "short": true}, {"title": "时间", "value": "2024-01-01 10:00:00", "short": true}]}]
  }'
```

## 告警抑制与静默

### 告警抑制规则

避免告警风暴：

| 场景 | 抑制策略 | 说明 |
|------|----------|------|
| 批量节点重启 | 抑制 5 分钟内的同类告警 | 维护窗口时不会收到大量告警 |
| 网络抖动 | 使用 `for: 5m` 语句 | 确认问题持续 5 分钟 |
| 已知维护 | 维护期间静默特定机器 | 手动触发静默 |

### 静默时段

| 时间段 | 处理方式 | 说明 |
|--------|----------|------|
| 工作日 09:00-22:00 | 正常告警通知 | 工作时间 |
| 工作日 22:00-09:00 | Warning 静默，Critical 通知 | 非工作时间 |
| 周末/节假日 | Warning 静默，Critical 升级 | 仅处理严重问题 |

## 运行手册（Runbook）

### ServerDown

```text
1. 检查服务器进程是否运行
   ssh node-1 'systemctl status xyncra-server'

2. 检查 Redis 连接
   ssh node-1 'redis-cli -h localhost ping'

3. 查看服务器日志
   ssh node-1 'journalctl -u xyncra-server --tail 50'

4. 重启服务（如需要）
   ssh node-1 'systemctl restart xyncra-server'

5. 验证恢复
   curl http://node-1:8080/health
```

### LLMErrorRate

```text
1. 检查 LLM API Key 是否有效
   - 查看 .env.agent 文件中的 Key
   - 尝试手动调用 API

2. 检查 LLM API 服务状态
   - 访问 provider 的状态页面
   - 检查是否有已知服务中断

3. 检查网络连接
   - curl -v https://dashscope.aliyuncs.com
   - 检查 DNS 解析和网络延迟

4. 如果问题持续，考虑切换 Provider
   - 修改 Agent 配置中的 model 和 base_url
   - 重启服务器使配置生效
```

## 后续优化方向

1. 集成 Prometheus HTTP Handler 暴露指标端点
2. 定义标准化的业务指标接口
3. 提供 Grafana Dashboard JSON 模板
4. 配置 AlertManager 告警规则
5. 扩展追踪覆盖范围至更多业务方法（当前已实现手动业务级追踪，见 D-127）
