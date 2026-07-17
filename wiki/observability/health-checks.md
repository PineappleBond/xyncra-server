# 健康检查

> last_updated: 2026-07-17

## 概述

Xyncra Server 提供内置的健康检查端点，用于监控服务运行状态和依赖可用性。健康检查结果用于负载均衡、容器编排和监控告警。

## 健康检查端点

### 端点信息

| 属性 | 值 |
|------|-----|
| 路径 | `GET /health` |
| 端口 | 8080（服务端口） |
| 协议 | HTTP |
| Content-Type | `application/json` |

### 响应格式

**正常状态**（HTTP 200）：
```json
{
    "status": "ok",
    "connections": 42
}
```

**降级状态**（HTTP 503）：
```json
{
    "status": "degraded",
    "connections": 0
}
```

### 实现细节

定义在 `internal/server/websocket_server.go:1008-1023`：

```go
func (s *WebSocketServer) handleHealth(w http.ResponseWriter, r *http.Request) {
    status := "ok"
    httpStatus := http.StatusOK

    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
    defer cancel()
    if err := s.ConnectionStore().Ping(ctx); err != nil {
        status = "degraded"
        httpStatus = http.StatusServiceUnavailable
        s.logger.Error("websocket: health check: connection store ping failed: %v", err)
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(httpStatus)
    _, _ = fmt.Fprintf(w, `{"status":%q,"connections":%d}`, status, s.ClientCount())
}
```

健康检查逻辑：
1. 设置 2 秒超时上下文
2. 对 Redis ConnectionStore 执行 Ping
3. Ping 成功 → HTTP 200，`status: "ok"`
4. Ping 失败 → HTTP 503，`status: "degraded"`
5. 始终返回当前活跃连接数

## Liveness 与 Readiness

### 当前实现

当前 `/health` 端点兼具 Liveness 和 Readiness 功能：

| 探针类型 | 端点 | 说明 |
|----------|------|------|
| Liveness | `/health` | 服务器是否存活 |
| Readiness | `/health` | 服务器是否准备好接收流量（Redis 可达） |

### 建议的分离

对于生产部署，建议分离 Liveness 和 Readiness：

```
GET /healthz    → Liveness（仅检查进程存活）
GET /readyz     → Readiness（检查所有依赖）
```

```go
// Liveness：仅检查进程存活
func (s *WebSocketServer) handleLiveness(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w, `{"status":"ok"}`)
}

// Readiness：检查所有依赖
func (s *WebSocketServer) handleReadiness(w http.ResponseWriter, r *http.Request) {
    status := "ok"
    httpStatus := http.StatusOK
    checks := make(map[string]string)

    // 检查 Redis
    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
    defer cancel()
    if err := s.ConnectionStore().Ping(ctx); err != nil {
        checks["redis"] = "unreachable"
        status = "degraded"
        httpStatus = http.StatusServiceUnavailable
    } else {
        checks["redis"] = "ok"
    }

    // 检查数据库
    if err := s.Store().Ping(ctx); err != nil {
        checks["database"] = "unreachable"
        status = "degraded"
        httpStatus = http.StatusServiceUnavailable
    } else {
        checks["database"] = "ok"
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(httpStatus)
    json.NewEncoder(w).Encode(map[string]interface{}{
        "status":      status,
        "connections": s.ClientCount(),
        "checks":      checks,
    })
}
```

## Docker Healthcheck

Dockerfile 中已配置健康检查：

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1
```

参数说明：
- `--interval=30s`：每 30 秒执行一次
- `--timeout=5s`：单次检查超时 5 秒
- `--retries=3`：连续 3 次失败标记为不健康

### Docker Compose 健康检查

```yaml
services:
  redis:
    image: redis:7-alpine
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 3
```

E2E 环境的健康检查（`docker-compose.e2e.yml`）：

```yaml
services:
  xyncra-server-e2e:
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 5s
      timeout: 3s
      retries: 10
      start_period: 10s
```

## 依赖健康检查

### 当前检查的依赖

| 依赖 | 检查方式 | 失败影响 |
|------|----------|----------|
| Redis ConnectionStore | `Ping()` | 服务降级（503） |

### 建议增加的依赖检查

| 依赖 | 检查方式 | 建议状态 | 说明 |
|------|----------|----------|------|
| 数据库 | `store.HealthCheck()` | Readiness | 数据库不可用时服务不可用 |
| Redis 消息队列 | Asynq 状态 | Readiness | 消息队列不可用时功能受限 |
| LLM API | 连接检查 | Readiness（可选） | Agent 功能受影响 |
| MCP Server | 进程检查 | Readiness（可选） | 外部工具不可用 |

### 数据库健康检查

`StoreAPI` 接口已定义健康检查方法：

```go
// internal/store/store.go
type StoreAPI interface {
    Ping(ctx context.Context) error
    HealthCheck(ctx context.Context) error
}
```

建议集成到 Readiness 检查中。

### 依赖健康报告格式

建议的多依赖健康检查响应格式：

```json
{
    "status": "ok",
    "connections": 42,
    "checks": {
        "redis": {
            "status": "ok",
            "latency_ms": 2
        },
        "database": {
            "status": "ok",
            "latency_ms": 5
        },
        "mq": {
            "status": "ok",
            "queue_depth": {
                "critical": 0,
                "default": 3,
                "low": 1
            }
        }
    },
    "version": "v1.0.0",
    "uptime_seconds": 86400
}
```

## 集成场景

### Kubernetes

```yaml
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: xyncra-server
    image: xyncra-server:latest
    livenessProbe:
      httpGet:
        path: /healthz
        port: 8080
      initialDelaySeconds: 10
      periodSeconds: 30
    readinessProbe:
      httpGet:
        path: /readyz
        port: 8080
      initialDelaySeconds: 5
      periodSeconds: 10
```

### 负载均衡器（Nginx）

```nginx
upstream xyncra {
    server node1:8080;
    server node2:8080;

    # 健康检查
    health_check interval=10s fails=3 passes=2 uri=/health;
}

server {
    listen 443 ssl;
    location /ws {
        proxy_pass http://xyncra;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

### AWS ALB / NLB

```
Target Group:
  - Health check path: /health
  - Healthy threshold: 2
  - Unhealthy threshold: 3
  - Timeout: 5 seconds
  - Interval: 30 seconds
```

## 健康检查最佳实践

### 1. 超时设置

所有健康检查必须有合理的超时：
- Redis Ping：2 秒
- DB Ping：2 秒
- HTTP 端点：5 秒

### 2. 幂等性

健康检查端点必须是幂等的，不产生副作用。

### 3. 级联故障保护

健康检查不应依赖自身正在检查的服务：
- /health 不依赖数据库（只依赖 Redis）
- /healthz 什么都不依赖

### 4. 检查频率

- Liveness：30 秒（不要太频繁，避免误报）
- Readiness：10 秒（需要快速响应流量变化）
- Docker HEALTHCHECK：30 秒

### 5. 日志

健康检查失败时应记录日志，但不要过度：

```go
// 只在第一次失败和恢复时记录
if err != nil && lastHealthOK {
    logger.Error("health check failed", "error", err)
} else if err == nil && !lastHealthOK {
    logger.Info("health check recovered")
}
```

## 当前限制

1. 仅检查 Redis ConnectionStore，未检查数据库等其他依赖
2. Liveness 和 Readiness 使用同一端点
3. 没有返回详细的依赖状态报告
4. 没有缓存健康检查结果（每次请求都执行 Ping）

## 后续优化方向

1. 分离 `/healthz`（Liveness）和 `/readyz`（Readiness）
2. 集成数据库健康检查
3. 返回依赖级别的健康状态详情
4. 添加健康检查结果的本地缓存（减少 Ping 频率）
5. 支持 Prometheus 指标格式的健康状态导出
6. 健康检查历史记录和趋势展示
