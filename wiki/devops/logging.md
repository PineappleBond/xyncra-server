# 日志采集

## 概述

Xyncra Server 使用结构化日志输出，支持标准输出和文件两种目标。日志采集是将这些日志集中收集、索引和查询的过程。

## 日志输出架构

```
┌───────────────────┐     ┌──────────────┐     ┌──────────────┐
│  Xyncra Server    │────▶│   标准输出    │────▶│ Docker 日志   │
│                   │     │  (stdout)    │     │  (docker logs)│
│  Logger Interface │────▶│              │     └──────────────┘
│  (Info/Error/     │     └──────────────┘
│   Debug)          │     ┌──────────────┐     ┌──────────────┐
│                   │────▶│  LLM 日志文件 │────▶│  日志分析     │
│  LLMLogger        │     │ (JSONL 格式) │     │  (grep/jq)   │
└───────────────────┘     └──────────────┘     └──────────────┘
```

## 日志输出目标

### 目标一：标准输出（stdout）

所有常规日志通过 `stdLogger` 输出到标准输出，使用 `log.Printf`：

```go
type stdLogger struct{}
func (stdLogger) Info(msg string, args ...any)  { log.Printf("[INFO]  "+msg, args...) }
func (stdLogger) Error(msg string, args ...any) { log.Printf("[ERROR] "+msg, args...) }
func (stdLogger) Debug(msg string, args ...any) { log.Printf("[DEBUG] "+msg, args...) }
```

在 Docker 环境中，标准输出由 Docker 的日志驱动自动收集。

### 目标二：LLM 日志文件

LLM 调用日志通过 `LLMLogger` 输出到独立的 JSONL 文件：

- 环境变量：`XYNCRA_LLM_LOG_DIR`
- 日志路径：`{XYNCRA_LLM_LOG_DIR}/llm-calls.log`
- 格式：JSONL（每行一个 JSON 对象）
- 启用方式：设置环境变量即可（不设置则无开销）

```go
if llmLogDir := os.Getenv("XYNCRA_LLM_LOG_DIR"); llmLogDir != "" {
    // 打开文件，按 JSONL 格式写入
}
```

## 日志采集方案

### 方案一：Docker 日志驱动（单节点）

```yaml
services:
  xyncra-server:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

查看日志：
```bash
# 实时查看
docker logs -f xyncra-server

# 最近 100 行
docker logs --tail 100 xyncra-server

# 时间范围
docker logs --since 2024-01-01T00:00:00 xyncra-server
```

### 方案二：Loki + Promtail（推荐）

```yaml
# promtail.yml
scrape_configs:
  - job_name: xyncra
    static_configs:
      - targets: ["localhost"]
        labels:
          job: xyncra-server
          __path__: /var/log/docker/*.log
    pipeline_stages:
      - regex:
          expression: '\[(?P<level>\w+)\]\s+(?P<message>.*)'
      - labels:
          level:
```

Loki 查询示例：
```
# 查询所有错误
{job="xyncra-server"} |= "[ERROR]"

# 查询 LLM 调用
{job="xyncra-server"} |= "llm call"

# 查询特定 Agent
{job="xyncra-server"} |= "agent_id=weather-bot"

# 时间范围查询
{job="xyncra-server"} |= "failed" |= "redis"
```

### 方案三：ELK Stack

```json
// Filebeat 配置
{
  "filebeat.inputs": [
    {
      "paths": ["/var/lib/docker/containers/*/*.log"],
      "processors": [
        {"decode_json_fields": {"fields": ["message"]}}
      ]
    }
  ],
  "output.elasticsearch": {
    "hosts": ["elasticsearch:9200"]
  }
}
```

Kibana 查询示例：
```
level: ERROR
message: "llm call failed"
message: "connection store ping failed"
```

## 日志查询模式

### 实时调试

```bash
# 跟踪启动日志
tail -f /path/to/llm-calls.log

# 过滤错误
tail -f /path/to/llm-calls.log | grep -i error

# 按 Agent 过滤
tail -f /path/to/llm-calls.log | grep "weather-bot"

# JSONL 格式的 jq 查询
tail -f /path/to/llm-calls.log | jq 'select(.agent_id == "weather-bot")'
```

### 故障排查

**场景一：服务器无法启动**

```bash
# 查看最近的日志
docker logs --tail 50 xyncra-server

# 搜索错误
docker logs xyncra-server 2>&1 | grep -i "fail\|error\|fatal"
```

**场景二：Agent 响应异常**

```bash
# 查看 Agent 相关日志
docker logs xyncra-server 2>&1 | grep "weather-bot"

# 查看 LLM 调用详情
tail -20 /app/llm-logs/llm-calls.log | jq '.'
```

**场景三：连接问题**

```bash
# 查看 WebSocket 连接日志
docker logs xyncra-server 2>&1 | grep "connect\|disconnect\|websocket"
```

## 日志轮转

### Docker JSON 文件驱动

```yaml
logging:
  driver: "json-file"
  options:
    max-size: "10m"    # 每个日志文件最大 10MB
    max-file: "3"      # 保留最近 3 个文件
```

### 文件日志轮转

对于 LLM 日志文件，建议使用 `logrotate`：

```bash
# /etc/logrotate.d/xyncra
/app/llm-logs/*.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
```

## 日志存储策略

| 环境 | 保留时间 | 存储位置 | 说明 |
|------|----------|----------|------|
| 开发 | 7 天 | 本地文件系统 | 按需清理 |
| 测试 | 30 天 | Docker 日志 | 自动轮转 |
| 生产 | 90 天 | 集中日志系统 | 根据合规要求调整 |

## 与监控告警的集成

日志采集是实现监控告警的基础。日志中的关键模式被提取为指标，用于驱动告警规则：

```
日志 → 解析 → 指标 → 告警 → 通知
```

参见 [监控告警](monitoring-alerting.md) 了解从日志中提取的具体指标和对应的告警规则。
