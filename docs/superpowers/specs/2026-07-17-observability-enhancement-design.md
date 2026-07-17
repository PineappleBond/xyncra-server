# 可观测性增强设计文档

**日期**：2026-07-17  
**状态**：设计完成，待实施  
**范围**：日志采集、指标采集、告警系统、性能分析

---

## 概述

为 Xyncra Server 添加完整的可观测性能力，包括结构化日志、Prometheus 指标、告警系统和持续性能分析。目标是建立生产级别的可观测性栈，支持问题诊断、性能优化和业务监控。

---

## 设计目标

1. **日志采集**：结构化日志（slog + JSON）+ 自动切割压缩 + Loki 集中存储
2. **指标采集**：完整实现 wiki 规划的 35 个指标，Prometheus `/metrics` 端点
3. **告警系统**：Prometheus AlertManager，支持多种通知渠道
4. **性能分析**：pprof 独立端点 + Pyroscope 持续采集 + Grafana 可视化
5. **统一可视化**：Grafana 集成 Prometheus、Loki、Jaeger、Pyroscope

---

## 架构设计

```
┌─────────────────────────────────────────────────────────┐
│                    Xyncra Server                         │
│                                                          │
│  :8080 (主服务)           :6060 (pprof)                  │
│  ├─ WebSocket API         └─ /debug/pprof/*              │
│  ├─ /health                                              │
│  ├─ /metrics ←──────────────── Prometheus scrape ───┐   │
│  └─ JSON 日志 (slog) ──→ /var/log/xyncra/*.jsonl    │   │
│                           ↓                          │   │
│  OTLP gRPC → Jaeger:4317   Promtail → Loki          │   │
│                           (日志采集)                  │   │
│                                                      │   │
│  Pyroscope Agent → Pyroscope:4040 (持续性能分析)    │   │
└───────────────────────────────────────────────────────┼───┘
                                                        │
┌───────────────────────────────────────────────────────┼───┐
│                    可观测性栈                           │   │
│                                                        │   │
│  ┌──────────────┐    ┌──────────────┐                 │   │
│  │  Prometheus  │←───│  AlertManager│→ 通知渠道        │   │
│  │  (指标存储)   │    │  (告警路由)   │                 │   │
│  └──────┬───────┘    └──────────────┘                 │   │
│         │                                              │   │
│         ↓                                              │   │
│  ┌──────────────┐    ┌──────────────┐                 │   │
│  │   Grafana    │    │    Loki      │←── Promtail     │   │
│  │  (可视化)     │    │  (日志存储)   │                 │   │
│  └──────┬───────┘    └──────────────┘                 │   │
│         │                                              │   │
│         ├─→ Prometheus (metrics)                       │   │
│         ├─→ Loki (logs)                                │   │
│         ├─→ Jaeger (traces)                            │   │
│         └─→ Pyroscope (profiles)                       │   │
│                                                        │   │
│  ┌──────────────┐    ┌──────────────┐                 │   │
│  │   Jaeger     │    │  Pyroscope   │                 │   │
│  │  (追踪存储)   │    │  (性能分析)   │                 │   │
│  └──────────────┘    └──────────────┘                 │   │
│                                                        │   │
└────────────────────────────────────────────────────────┘   │
```

---

## 组件设计

### 1. 日志系统

#### 1.1 技术选型

- **日志库**：`log/slog`（Go 1.21+ 标准库）
- **输出格式**：JSON
- **日志切割**：`gopkg.in/natefinsh/lumberjack.v2`
- **集中存储**：Loki + Promtail

#### 1.2 实现方案

**新增文件**：
- `internal/logger/logger.go` - 日志初始化
- `internal/logger/context.go` - Context 传递
- `internal/logger/component.go` - 组件标签
- `internal/logger/fields.go` - 结构化字段工具

**核心功能**：

```go
// 全局初始化
func Init(cfg Config) (cleanup func(), err error)

// Context 传递（自动关联 trace_id）
func FromContext(ctx context.Context) *slog.Logger
func WithContext(ctx context.Context, logger *slog.Logger) context.Context

// 组件标签
func WithComponent(name string) *slog.Logger

// 字段工具
func AgentID(id string) slog.Attr
func UserID(id string) slog.Attr
func Duration(d time.Duration) slog.Attr
```

**配置项**：

```bash
XYNCRA_LOG_LEVEL=info           # debug, info, warn, error
XYNCRA_LOG_DIR=/var/log/xyncra  # 日志文件目录
XYNCRA_LOG_FORMAT=json          # json 或 text
XYNCRA_LOG_MAX_SIZE=100         # 单文件最大 MB
XYNCRA_LOG_MAX_AGE=30           # 保留天数
XYNCRA_LOG_MAX_BACKUPS=10       # 最大备份数
XYNCRA_LOG_COMPRESS=true        # 是否压缩
```

**迁移策略**：一次性完全迁移

- 替换所有 `log.Printf()` → `slog.Info()` / `slog.Error()`
- 替换所有 `log.Fatalf()` → `slog.Error()` + `os.Exit(1)`
- 删除所有 `import "log"` 语句
- 整合 LLM 日志到主日志系统（移除独立的 LLM 日志文件）

**日志格式示例**：

```json
{
  "time": "2026-07-17T10:30:00.123Z",
  "level": "INFO",
  "msg": "llm call completed",
  "component": "agent",
  "trace_id": "abc123def456",
  "agent_id": "weather-bot",
  "model": "qwen3.7-plus",
  "duration_ms": 1250,
  "input_tokens": 100,
  "output_tokens": 200
}
```

#### 1.3 验收标准

- [ ] 所有 `import "log"` 已移除
- [ ] 所有日志输出为 JSON 格式
- [ ] 每个请求自动关联 trace_id
- [ ] 每个组件带 `component` 标签
- [ ] 日志自动切割和压缩
- [ ] Promtail 成功推送日志到 Loki

---

### 2. 指标采集

#### 2.1 技术选型

- **指标库**：`github.com/prometheus/client_golang`
- **暴露端点**：`/metrics`（HTTP，Prometheus 格式）
- **采集间隔**：Prometheus 默认 15s

#### 2.2 指标清单

完整实现 `wiki/observability/metrics.md` 规划的 35 个指标：

**系统指标**（7 个）：
- `xyncra_goroutines`
- `xyncra_memory_alloc_bytes`
- `xyncra_memory_inuse_bytes`
- `xyncra_gc_duration_seconds`
- `xyncra_gc_count`
- `xyncra_cpu_usage`
- `xyncra_open_fds`

**连接指标**（5 个）：
- `xyncra_connections_active` (user_id)
- `xyncra_connections_total`
- `xyncra_connections_per_user` (user_id)
- `xyncra_connections_per_device` (user_id, device_id)
- `xyncra_connections_duration_seconds`

**消息指标**（5 个）：
- `xyncra_messages_sent_total` (conversation_id)
- `xyncra_messages_received_total`
- `xyncra_messages_per_second`
- `xyncra_message_size_bytes`
- `xyncra_message_latency_seconds`

**Agent 指标**（9 个）：
- `xyncra_agent_executions_total` (agent_id, model)
- `xyncra_agent_executions_failed_total` (agent_id, error)
- `xyncra_agent_duration_seconds` (agent_id, model)
- `xyncra_agent_active`
- `xyncra_agent_queue_depth`
- `xyncra_llm_tokens_input_total` (agent_id, model)
- `xyncra_llm_tokens_output_total` (agent_id, model)
- `xyncra_llm_calls_total` (agent_id, model)
- `xyncra_llm_calls_failed_total` (agent_id, model, error)

**业务指标**（6 个）：
- `xyncra_conversations_active`
- `xyncra_conversations_created_total`
- `xyncra_devices_connected`
- `xyncra_functions_registered` (device_id)
- `xyncra_reverse_rpc_requests_total`
- `xyncra_reverse_rpc_failed_total`

**Redis 指标**（4 个）：
- `xyncra_redis_connected`
- `xyncra_redis_ping_duration_seconds`
- `xyncra_redis_pool_size`
- `xyncra_asynq_queue_size` (queue)

#### 2.3 实现方案

**新增文件**：
- `internal/metrics/metrics.go` - 指标定义和注册
- `internal/metrics/runtime.go` - 系统指标采集
- `internal/server/metrics.go` - `/metrics` 端点

**埋点位置**：
- 系统指标：定时采集（每 10 秒）
- 连接指标：事件驱动（连接建立/断开）
- 消息指标：事件驱动（消息收发）
- Agent/LLM 指标：替换现有 `LogMetrics` 接口
- Redis 指标：事件驱动（连接/Ping）

**配置项**：

```bash
XYNCRA_METRICS_ENABLED=true  # 是否启用 /metrics 端点
```

#### 2.4 验收标准

- [ ] `/metrics` 端点返回 Prometheus 格式
- [ ] 所有 35 个指标已注册并埋点
- [ ] 系统指标每 10 秒自动采集
- [ ] 事件驱动指标实时更新
- [ ] `curl http://localhost:8080/metrics` 可正常访问

---

### 3. 告警系统

#### 3.1 技术选型

- **告警规则**：Prometheus
- **告警路由**：AlertManager
- **通知渠道**：Webhook（占位，用户可配置钉钉/Slack/邮件）

#### 3.2 告警规则

**系统告警**：
- `HighGoroutineCount`：goroutine > 10000，持续 5 分钟
- `HighMemoryUsage`：内存 > 1GB，持续 5 分钟

**连接告警**：
- `HighConnectionCount`：连接数 > 1000，持续 5 分钟
- `ConnectionSpike`：连接速率 > 100/s，持续 2 分钟

**Agent 告警**：
- `HighLLMErrorRate`：LLM 错误率 > 10%，持续 5 分钟
- `SlowLLMResponse`：LLM P95 延迟 > 30s，持续 5 分钟
- `AgentExecutionFailure`：Agent 执行失败

**Redis 告警**：
- `RedisDown`：Redis 连接断开，持续 1 分钟
- `HighRedisLatency`：Redis P95 延迟 > 100ms，持续 5 分钟

**消息告警**：
- `MessageLatencyHigh`：消息 P95 延迟 > 5s，持续 5 分钟
- `MessageQueueBacklog`：队列积压 > 1000，持续 5 分钟

#### 3.3 实现方案

**配置文件**：
- `deploy/prometheus/prometheus.yml` - Prometheus 配置
- `deploy/prometheus/alerts.yml` - 告警规则
- `deploy/alertmanager/alertmanager.yml` - AlertManager 配置

**配置项**：

```bash
XYNCRA_ALERTS_ENABLED=true
XYNCRA_ALERT_WEBHOOK_URL=http://host.docker.internal:5001/webhook
```

#### 3.4 验收标准

- [ ] Prometheus 正常运行，可访问 `http://localhost:9090`
- [ ] AlertManager 正常运行，可访问 `http://localhost:9093`
- [ ] 告警规则已加载
- [ ] 模拟告警可触发通知

---

### 4. 性能分析

#### 4.1 技术选型

- **pprof**：独立端口 `:6060`，用于手动诊断
- **Pyroscope**：持续性能分析，Grafana 可视化

#### 4.2 实现方案

**pprof 端点**：

```go
// internal/pprof/pprof.go
package pprof

import _ "net/http/pprof" // 自动注册 /debug/pprof/* 路由

func Start(ctx context.Context, cfg Config) error
```

**Pyroscope 集成**：

```go
// internal/profile/profile.go
package profile

import "github.com/grafana/pyroscope-go"

func Start(cfg Config) error
```

**配置项**：

```bash
# pprof
XYNCRA_PPROF_ENABLED=true
XYNCRA_PPROF_ADDR=:6060

# Pyroscope
XYNCRA_PROFILING_ENABLED=true
XYNCRA_PROFILING_SERVER=http://pyroscope:4040
XYNCRA_PROFILING_APP_NAME=xyncra-server
```

#### 4.3 验收标准

- [ ] pprof 端点可通过 `http://localhost:6060/debug/pprof/` 访问
- [ ] Pyroscope 服务正常运行
- [ ] xyncra-server 自动上报 profile 数据
- [ ] Grafana 可查看 CPU 火焰图、内存分配、goroutine 等

---

### 5. Docker Compose 配置

#### 5.1 服务清单

| 服务 | 端口 | 用途 | 状态 |
|------|------|------|------|
| xyncra-server | 8080 | WebSocket API + /metrics | 已有 |
| xyncra-server | 6060 | pprof | 已有 |
| redis | 6379 | 缓存/消息 | 已有 |
| jaeger | 16686 | 分布式追踪 | 已有 |
| prometheus | 9090 | 指标采集 | **新增** |
| alertmanager | 9093 | 告警 | **新增** |
| loki | 3100 | 日志存储 | **新增** |
| promtail | - | 日志采集 | **新增** |
| pyroscope | 4040 | 持续性能分析 | **新增** |
| grafana | 3000 | 统一可视化 | **新增** |

#### 5.2 配置文件结构

```
.
├── deploy/
│   ├── docker-compose.yml
│   ├── prometheus/
│   │   ├── prometheus.yml
│   │   └── alerts.yml
│   ├── alertmanager/
│   │   └── alertmanager.yml
│   ├── loki/
│   │   └── loki.yml
│   ├── promtail/
│   │   └── promtail.yml
│   └── grafana/
│       └── provisioning/
│           ├── datasources/
        │   └── datasources.yml
        └── dashboards/
            └── dashboards.yml
```

#### 5.3 验收标准

- [ ] `docker-compose up -d` 所有服务正常启动
- [ ] 所有健康检查通过
- [ ] Grafana 可访问，4 个数据源已配置
- [ ] Prometheus 可 scrape xyncra-server 的 `/metrics`
- [ ] Loki 可接收 promtail 推送的日志
- [ ] Jaeger 可接收 OTLP traces
- [ ] Pyroscope 可接收 profile 数据

---

## 新增依赖

```bash
# Go 依赖
go get github.com/prometheus/client_golang
go get gopkg.in/natefinsh/lumberjack.v2
go get github.com/grafana/pyroscope-go

# Docker 镜像
prom/prometheus:latest
prom/alertmanager:latest
grafana/loki:latest
grafana/promtail:latest
grafana/pyroscope:latest
grafana/grafana:latest
```

---

## 实施计划

### Phase 1：日志系统（1-2 天）

1. 创建 `internal/logger` 包
2. 实现日志初始化、Context 传递、组件标签
3. 一次性迁移所有 `log` 调用到 `slog`
4. 整合 LLM 日志
5. 配置 lumberjack 日志切割

### Phase 2：指标采集（2-3 天）

1. 创建 `internal/metrics` 包
2. 定义和注册所有 35 个指标
3. 实现系统指标定时采集
4. 在连接/消息/Agent/Redis 层埋点
5. 暴露 `/metrics` 端点

### Phase 3：性能分析（1 天）

1. 实现 pprof 独立端口
2. 集成 Pyroscope SDK
3. 添加配置项

### Phase 4：Docker Compose 和告警（2 天）

1. 编写完整的 deploy/docker-compose.yml
2. 配置 Prometheus、AlertManager、Loki、Promtail、Grafana、Pyroscope
3. 编写告警规则
4. 配置 Grafana 数据源

### Phase 5：集成测试和文档（1-2 天）

1. 端到端测试所有功能
2. 更新 wiki 文档
3. 编写使用说明

**总计**：7-10 天

---

## 风险和缓解

### 风险 1：日志迁移工作量大

**缓解**：
- 使用 `grep` 快速定位所有 `log` 调用
- 编写脚本辅助批量替换
- 分模块逐步迁移，每个模块单独测试

### 风险 2：指标埋点影响性能

**缓解**：
- Prometheus 客户端本身性能开销极低
- 使用异步采集（系统指标）
- 避免在高热路径上做复杂计算

### 风险 3：Docker Compose 服务过多

**缓解**：
- 所有服务可选（通过环境变量控制）
- 提供最小化配置（仅核心服务）
- 文档说明各服务的作用

---

## 验收标准汇总

### 功能验收

- [ ] 日志系统：JSON 格式输出，自动切割压缩，Loki 可查询
- [ ] 指标采集：`/metrics` 端点正常，35 个指标完整
- [ ] 告警系统：告警规则已加载，可触发通知
- [ ] 性能分析：pprof 可访问，Pyroscope 持续采集
- [ ] Docker Compose：所有服务正常启动，健康检查通过

### 性能验收

- [ ] 日志系统对主服务性能影响 < 5%
- [ ] 指标采集对主服务性能影响 < 1%
- [ ] Pyroscope 对主服务性能影响 < 3%

### 文档验收

- [ ] 更新 `wiki/observability/metrics.md`
- [ ] 新增 `wiki/observability/logging.md`
- [ ] 新增 `wiki/observability/alerting.md`
- [ ] 新增 `wiki/observability/profiling.md`
- [ ] 更新 `README.md` 可观测性章节

---

## 附录

### A. 端口映射

| 服务 | 端口 | 协议 | 用途 |
|------|------|------|------|
| xyncra-server | 8080 | HTTP | WebSocket API + /metrics + /health |
| xyncra-server | 6060 | HTTP | pprof |
| redis | 6379 | TCP | Redis |
| jaeger | 4317 | gRPC | OTLP |
| jaeger | 4318 | HTTP | OTLP |
| jaeger | 16686 | HTTP | Jaeger UI |
| prometheus | 9090 | HTTP | Prometheus UI |
| alertmanager | 9093 | HTTP | AlertManager UI |
| loki | 3100 | HTTP | Loki API |
| pyroscope | 4040 | HTTP | Pyroscope UI |
| grafana | 3000 | HTTP | Grafana UI |

### B. 环境变量汇总

```bash
# 基础配置
XYNCRA_ADDR=:8080
XYNCRA_REDIS_ADDR=redis:6379
XYNCRA_DB_DRIVER=sqlite
XYNCRA_DB_DSN=/data/xyncra.db

# 日志配置
XYNCRA_LOG_LEVEL=info
XYNCRA_LOG_DIR=/var/log/xyncra
XYNCRA_LOG_FORMAT=json
XYNCRA_LOG_MAX_SIZE=100
XYNCRA_LOG_MAX_AGE=30
XYNCRA_LOG_MAX_BACKUPS=10
XYNCRA_LOG_COMPRESS=true

# 追踪配置
XYNCRA_TRACING_ENABLED=true
XYNCRA_TRACING_OTLP_ENDPOINT=jaeger:4317
XYNCRA_TRACING_OTLP_INSECURE=true
XYNCRA_TRACING_SAMPLING_RATE=1.0

# 指标配置
XYNCRA_METRICS_ENABLED=true

# pprof 配置
XYNCRA_PPROF_ENABLED=true
XYNCRA_PPROF_ADDR=:6060

# 性能分析配置
XYNCRA_PROFILING_ENABLED=true
XYNCRA_PROFILING_SERVER=http://pyroscope:4040
XYNCRA_PROFILING_APP_NAME=xyncra-server

# 告警配置
XYNCRA_ALERTS_ENABLED=true
XYNCRA_ALERT_WEBHOOK_URL=http://host.docker.internal:5001/webhook
```

### C. Grafana Dashboard 建议

1. **系统概览**：goroutines、内存、CPU、GC
2. **连接监控**：活跃连接数、连接速率、连接时长
3. **Agent 监控**：执行次数、成功率、延迟、Token 消耗
4. **消息监控**：消息速率、延迟、队列深度
5. **Redis 监控**：连接状态、延迟、池大小
6. **性能分析**：CPU 火焰图、内存分配、goroutine 堆栈

---

**文档结束**
