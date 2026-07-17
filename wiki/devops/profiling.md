---
last_updated: 2026-07-17
---

# Performance Profiling

> last_updated: 2026-07-17

## Overview

Xyncra Server supports two profiling mechanisms, both opt-in (D-063 optional module pattern):

1. **pprof** -- Go's built-in HTTP profiling server (localhost-only by default)
2. **Pyroscope** -- Continuous profiling agent for long-term performance analysis

All profiling endpoints bind to localhost by default and are only accessible within the internal network (D-003 deployment model).

## pprof

### Configuration

| Environment Variable | Default | Description |
|----------------------|---------|-------------|
| `XYNCRA_PPROF_ENABLED` | `false` | Enable pprof HTTP server |
| `XYNCRA_PPROF_ADDR` | `127.0.0.1:6060` | pprof listen address |

### Endpoints

When enabled, pprof exposes the following endpoints on a **dedicated HTTP mux** (not `http.DefaultServeMux`) for security isolation:

| Path | Handler | Description |
|------|---------|-------------|
| `/debug/pprof/` | `pprof.Index` | HTML index of available profiles |
| `/debug/pprof/cmdline` | `pprof.Cmdline` | Running command line |
| `/debug/pprof/profile` | `pprof.Profile` | CPU profile (30s default) |
| `/debug/pprof/symbol` | `pprof.Symbol` | Symbol lookup |
| `/debug/pprof/trace` | `pprof.Trace` | Execution trace |

### Security

pprof binds to `127.0.0.1` by default. **Never set `XYNCRA_PPROF_ADDR` to `0.0.0.0`** in production -- pprof exposes sensitive runtime information.

### Usage

#### CPU Profile (30 seconds)

```bash
# Via go tool
go tool pprof http://127.0.0.1:6060/debug/pprof/profile

# Or download and analyze
curl -o cpu.prof http://127.0.0.1:6060/debug/pprof/profile
go tool pprof cpu.prof
```

#### Heap Profile

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
```

#### Goroutine Profile

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/goroutine
```

#### Block / Mutex Profiles

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/block
go tool pprof http://127.0.0.1:6060/debug/pprof/mutex
```

#### Interactive Web UI

```bash
go tool pprof -http=:8080 http://127.0.0.1:6060/debug/pprof/heap
```

#### Remote Access via SSH Tunnel

Since pprof binds to localhost, use an SSH tunnel to access it from your local machine:

```bash
# Create SSH tunnel
ssh -L 6060:127.0.0.1:6060 user@server-host

# Then access in browser or go tool
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
```

### Docker

In Docker Compose, pprof port 6060 is exposed on the `xyncra-server` container. To access it:

```bash
# Start with pprof enabled
XYNCRA_PPROF_ENABLED=true docker compose up -d

# Access via docker exec
docker exec -it xyncra-server wget -O /tmp/heap.prof http://127.0.0.1:6060/debug/pprof/heap
docker cp xyncra-server:/tmp/heap.prof .
go tool pprof heap.prof
```

## Pyroscope (Continuous Profiling)

### Configuration

| Environment Variable | Default | Description |
|----------------------|---------|-------------|
| `XYNCRA_PROFILING_ENABLED` | `false` | Enable Pyroscope continuous profiling agent |
| `XYNCRA_PROFILING_SERVER` | *(empty)* | Pyroscope server URL (e.g. `http://pyroscope:4040`) |
| `XYNCRA_PROFILING_APP_NAME` | `xyncra-server` | Application name in Pyroscope |

### Behavior

- **Fail-open** (D-072 pattern): If Pyroscope initialization fails, the server logs a warning and continues without profiling. No crash, no error returned to clients.
- If `XYNCRA_PROFILING_SERVER` is empty but `XYNCRA_PROFILING_ENABLED=true`, Pyroscope silently skips with a warning log.
- The Pyroscope agent runs in the same process and pushes profiling data to the server at regular intervals.

### Docker Compose Stack

The observability profile includes a Pyroscope service:

```bash
docker compose --profile observability up -d
```

Pyroscope is available at `http://localhost:4040`. Grafana is pre-configured with a Pyroscope datasource for flamegraph visualization.

### Viewing Profiles

1. Open Grafana at `http://localhost:3000`
2. Navigate to the Pyroscope datasource
3. Select application: `xyncra-server`
4. View flamegraphs for CPU, memory, goroutines, etc.

Or directly via Pyroscope UI at `http://localhost:4040`.

## Safety Guidelines

1. **Never expose pprof publicly** -- always bind to `127.0.0.1` or use SSH tunnels
2. **pprof has minimal overhead** but `profile` endpoint triggers 30s CPU sampling -- avoid concurrent calls
3. **Pyroscope has low overhead** (~2-5% CPU) but should still be opt-in
4. **All profiling is opt-in** -- disabled by default with zero overhead when not enabled

## Related

- [Metrics](../observability/metrics.md) -- Prometheus metrics
- [Logging](logging.md) -- Structured logging configuration
- [Alerting](alerting.md) -- Alert rules
