---
last_updated: 2026-07-17
---

# Structured Logging

> last_updated: 2026-07-17

## Overview

Xyncra Server uses Go's `log/slog` for structured, leveled logging with optional file output and automatic log rotation via `lumberjack`. All observability features are opt-in (D-063 optional module pattern).

## Configuration

All logging is controlled via `XYNCRA_LOG_*` environment variables:

| Environment Variable | Default | Description |
|----------------------|---------|-------------|
| `XYNCRA_LOG_FORMAT` | `text` | Log output format: `text` (human-readable) or `json` (machine-parseable) |
| `XYNCRA_LOG_LEVEL` | `info` | Minimum log level: `debug`, `info`, `warn`, `error` |
| `XYNCRA_LOG_DIR` | *(empty)* | Directory for rolling log files. When empty, logs go to stdout only |
| `XYNCRA_LOG_MAX_SIZE` | `100` | Maximum size in MB of a log file before rotation |
| `XYNCRA_LOG_MAX_BACKUPS` | `10` | Maximum number of old log files to retain |
| `XYNCRA_LOG_MAX_AGE` | `30` | Maximum number of days to retain old log files |
| `XYNCRA_LOG_COMPRESS` | `true` | Whether to compress rotated log files (gzip) |

Additionally, LLM call logs use a separate configuration:

| Environment Variable | Default | Description |
|----------------------|---------|-------------|
| `XYNCRA_LLM_LOG_DIR` | *(empty)* | Directory for LLM call JSONL logs. When empty, LLM logging is disabled (zero overhead) |

## Log Formats

### Text Format (default)

Human-readable output, suitable for development and debugging:

```text
time=2026-07-17T10:30:00.123+08:00 level=INFO msg="server starting" addr=:8080 db_driver=sqlite
time=2026-07-17T10:30:01.456+08:00 level=INFO msg="client connected" user_id=alice device_id=laptop
time=2026-07-17T10:30:05.789+08:00 level=ERROR msg="redis connection failed" error="connection refused" retry=1
```

### JSON Format

Machine-parseable output, recommended for production with log aggregation:

```json
{"time":"2026-07-17T10:30:00.123+08:00","level":"INFO","msg":"server starting","addr":":8080","db_driver":"sqlite"}
{"time":"2026-07-17T10:30:01.456+08:00","level":"INFO","msg":"client connected","user_id":"alice","device_id":"laptop"}
{"time":"2026-07-17T10:30:05.789+08:00","level":"ERROR","msg":"redis connection failed","error":"connection refused","retry":1}
```

## Log Levels

| Level | Usage | Examples |
|-------|-------|---------|
| `DEBUG` | Detailed diagnostic information | SQL queries, Redis commands, message payloads |
| `INFO` | Normal operational messages | Server start/stop, client connect/disconnect, agent execution |
| `WARN` | Recoverable issues that deserve attention | Redis reconnection, MQ enqueue failure (fail-open), fallback behavior |
| `ERROR` | Failures that affect functionality | Database errors, agent execution failure, LLM API errors |

## Log Rotation (Lumberjack)

When `XYNCRA_LOG_DIR` is set, logs are written to `<XYNCRA_LOG_DIR>/xyncra-server.log` with automatic rotation managed by `lumberjack`. Output goes to **both** stdout and the rolling log file via `io.MultiWriter`:

```text
/var/log/xyncra/xyncra-server.log          <- current active log
/var/log/xyncra/xyncra-server-2026-07-16.log  <- rotated (compressed if enabled)
/var/log/xyncra/xyncra-server-2026-07-15.log  <- retained backup
```

Rotation is triggered when the active log file reaches `XYNCRA_LOG_MAX_SIZE` MB. Old files are compressed (gzip) if `XYNCRA_LOG_COMPRESS` is true and retained up to `XYNCRA_LOG_MAX_BACKUPS` files or `XYNCRA_LOG_MAX_AGE` days, whichever is reached first.

## LLM Call Logs

LLM calls are logged to a separate JSONL file when `XYNCRA_LLM_LOG_DIR` is set:

```jsonl
{"timestamp":"2026-07-17T10:30:05Z","agent_id":"weather-bot","model":"qwen3.7-plus","duration_ms":1234,"input_tokens":500,"output_tokens":200,"status":"success"}
{"timestamp":"2026-07-17T10:31:00Z","agent_id":"weather-bot","model":"qwen3.7-plus","duration_ms":5000,"input_tokens":500,"output_tokens":0,"status":"error","error":"context deadline exceeded"}
```

- Path: `{XYNCRA_LLM_LOG_DIR}/llm-calls.log`
- Format: JSONL (one JSON object per line)
- Zero overhead when `XYNCRA_LLM_LOG_DIR` is not set

## Log Collection

### Docker (stdout)

When running in Docker without `XYNCRA_LOG_FILE`, logs go to stdout and are collected by Docker's log driver:

```bash
# Real-time logs
docker logs -f xyncra-server

# Last 100 lines
docker logs --tail 100 xyncra-server

# Time range
docker logs --since 2026-07-17T00:00:00 xyncra-server
```

### Loki + Promtail (recommended for production)

The Docker Compose observability stack includes Loki and Promtail for centralized log aggregation. Promtail scrapes container logs and forwards them to Loki for querying via Grafana.

```logql
# All errors
{job="xyncra-server"} | json | level="ERROR"

# Agent execution logs
{job="xyncra-server"} | json | msg="agent execution completed"

# Redis issues
{job="xyncra-server"} | json | msg=~".*redis.*"

# Specific user
{job="xyncra-server"} | json | user_id="alice"
```

## Usage Examples

### Development (human-readable)

```bash
XYNCRA_LOG_FORMAT=text XYNCRA_LOG_LEVEL=debug ./bin/xyncra-server
```

### Production (JSON with file rotation)

```bash
XYNCRA_LOG_FORMAT=json \
XYNCRA_LOG_LEVEL=info \
XYNCRA_LOG_DIR=/var/log/xyncra \
XYNCRA_LOG_MAX_SIZE=100 \
XYNCRA_LOG_MAX_BACKUPS=10 \
XYNCRA_LOG_MAX_AGE=30 \
XYNCRA_LOG_COMPRESS=true \
./bin/xyncra-server
```

### Docker Compose

```yaml
services:
  xyncra-server:
    environment:
      XYNCRA_LOG_FORMAT: json
      XYNCRA_LOG_LEVEL: info
      # Logs go to stdout, collected by Promtail
```

## Related

- [Metrics](../observability/metrics.md) -- Prometheus metrics
- [Profiling](profiling.md) -- pprof and Pyroscope
- [Alerting](alerting.md) -- Alert rules
- [Monitoring & Alerting](monitoring-alerting.md) -- Dashboard and alerting guide
