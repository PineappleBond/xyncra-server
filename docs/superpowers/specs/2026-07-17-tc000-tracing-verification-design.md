# TC-000 链路追踪验证设计

> **日期**: 2026-07-17
> **状态**: 草稿
> **关联**: TC-000 完整链路端到端测试, 072-distributed-tracing 实现方案

---

## 1. 目标

在 TC-000 手动测试文档中融入分布式链路追踪（OpenTelemetry + Jaeger）验证步骤，覆盖 WebSocket 层 → Handler 层 → MQ 层 → Agent 层 → LLM 层全链路 span 的正确性。

## 2. 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 组织方式 | 融入现有各阶段（非独立新阶段） | 与现有 DB 验证模式一致，验证时机贴近实际操作 |
| 验证手段 | Jaeger Query API (`curl` + `jq`) | 客观可重复、可脚本化 |
| 验证深度 | span 存在性 + 关键 attributes | 平衡深度与复杂度 |
| 公共工具 | 前置 helper 函数 + 变量定义 | 减少重复，统一格式 |

## 3. 公共验证工具（新增第 4.5 节）

### 3.1 新增变量

在数据字典（第 4 节）中追加：

| 变量 | 值 | 说明 |
|------|-----|------|
| `$JAEGER_API` | `http://localhost:16687/api` | Jaeger Query API 基础地址 |
| `$JAEGER_UI` | `http://localhost:16687` | Jaeger UI 地址（供深度调试） |
| `$TRACING_SERVICE` | `xyncra-server` | OTel service name |

### 3.2 通用验证函数

```bash
# 检查指定 operation 的 span 是否存在
# 用法: check_trace "handler.invoke" "xyncra.method" "create_conversation"
check_trace() {
  local operation="$1"
  local attr_key="$2"
  local attr_val="$3"
  local limit="${4:-5}"

  local url="${JAEGER_API}/traces?service=${TRACING_SERVICE}&operation=${operation}&limit=${limit}"
  local result
  result=$(curl -s "$url")

  local count
  count=$(echo "$result" | jq '[.data[].spans[]] | length')
  echo "Found $count spans for operation=$operation"

  if [ -n "$attr_key" ] && [ -n "$attr_val" ]; then
    local matched
    matched=$(echo "$result" | jq --arg k "$attr_key" --arg v "$attr_val" \
      '[.data[].spans[] | select(.tags[]? | select(.key==$k and .value==$v))] | length')
    echo "Matched $matched spans with $attr_key=$attr_val"
    [ "$matched" -ge 1 ] && return 0 || return 1
  fi

  [ "$count" -ge 1 ] && return 0 || return 1
}

# 验证两个 operation 属于同一 trace_id（MQ 传播验证专用）
check_same_trace() {
  local op1="$1"
  local op2="$2"

  local trace1
  trace1=$(curl -s "${JAEGER_API}/traces?service=${TRACING_SERVICE}&operation=${op1}&limit=1" | jq -r '.data[0].traceID')
  local trace2
  trace2=$(curl -s "${JAEGER_API}/traces?service=${TRACING_SERVICE}&operation=${op2}&limit=1" | jq -r '.data[0].traceID')

  echo "Trace($op1): $trace1"
  echo "Trace($op2): $trace2"

  [ "$trace1" = "$trace2" ] && [ "$trace1" != "null" ] && return 0 || return 1
}
```

### 3.3 Jaeger 健康检查（扩展步骤 3.3）

在现有 Redis/Server 健康检查后追加：

```bash
# 检查 Jaeger
curl -s http://localhost:16687/api/services | jq '.data[]' | grep xyncra-server
# 预期: "xyncra-server"
```

## 4. 各阶段验证步骤

每个阶段在现有"验证服务器 DB"步骤之后，新增一个 `🔍 验证 Jaeger 链路追踪` 步骤。

### 4.1 阶段 1 (Daemon 生命周期) — 步骤 1.6

验证 `ws.connection` span：

```bash
# 等待 span 上报 (OTLP batch 间隔)
sleep 2

# 验证 ws.connection span 存在
check_trace "ws.connection" "xyncra.user.id" "alice"
# 预期: 找到 >= 1 个 span, xyncra.user.id=alice

check_trace "ws.connection" "xyncra.device.id" "test-device-alice"
# 预期: 找到 >= 1 个 span, xyncra.device.id=test-device-alice
```

**验证点**:
- `ws.connection` span 存在
- Attributes: `xyncra.user.id`, `xyncra.device.id`, `xyncra.connection.id`

### 4.2 阶段 2 (会话创建) — 步骤 2.6

验证 `handler.invoke` span：

```bash
sleep 2

check_trace "handler.invoke" "xyncra.method" "create_conversation"
# 预期: 找到 >= 1 个 span, xyncra.method=create_conversation
```

**验证点**:
- `handler.invoke` span 存在
- Attributes: `xyncra.method=create_conversation`, `xyncra.user.id=alice`

### 4.3 阶段 4 (Typing Indicator) — 步骤 4.5

验证 `handler.invoke` span：

```bash
sleep 2

check_trace "handler.invoke" "xyncra.method" "set_typing"
# 预期: 找到 >= 1 个 span
```

### 4.4 阶段 5 (消息发送) — 步骤 5.9

验证完整链路：`handler.invoke` → `handler.broker.enqueue` → `mq.process`

```bash
sleep 2

# 1. 验证 handler.invoke (send_message)
check_trace "handler.invoke" "xyncra.method" "send_message"
# 预期: >= 1

# 2. 验证 MQ 跨 goroutine trace 传播（关键！）
check_same_trace "handler.broker.enqueue" "mq.process"
# 预期: 两个 operation 的 trace_id 相同
```

**验证点**:
- `handler.invoke` span (`xyncra.method=send_message`)
- `handler.broker.enqueue` span 存在
- `mq.process` span 存在
- **enqueue 和 process 属于同一 trace_id**（MQ context 传播正确）

### 4.5 阶段 7 (已读标记) — 步骤 7.7

```bash
sleep 2

check_trace "handler.invoke" "xyncra.method" "mark_as_read"
# 预期: >= 1
```

### 4.6 阶段 8 (消息删除) — 步骤 8.5

```bash
sleep 2

check_trace "handler.invoke" "xyncra.method" "delete_message"
# 预期: >= 1
```

### 4.7 阶段 9 (会话删除/恢复) — 步骤 9.6

```bash
sleep 2

check_trace "handler.invoke" "xyncra.method" "delete_conversation"
# 预期: >= 1

check_trace "handler.invoke" "xyncra.method" "restore_conversation"
# 预期: >= 1
```

### 4.8 阶段 10 (Agent 交互) — 步骤 10.7

验证完整 Agent 执行链路：

```bash
sleep 5  # Agent 处理需要更多时间

# 1. 验证 agent.execute
check_trace "agent.execute" "xyncra.agent.id" "agent/weather-bot"
# 预期: >= 1

# 2. 验证 agent.build
check_trace "agent.build" "xyncra.agent.id" "agent/weather-bot"
# 预期: >= 1

# 3. 验证 agent.run
check_trace "agent.run" "xyncra.agent.id" "agent/weather-bot"
# 预期: >= 1

# 4. 验证 agent.llm.call（LLM 调用）
check_trace "agent.llm.call" "xyncra.agent.id" "agent/weather-bot"
# 预期: >= 1
# 可选: 检查 xyncra.llm.model 属性
```

**验证点**:
- Agent 执行全链路 span 完整
- `xyncra.agent.id`, `xyncra.conversation.id` attributes 正确
- `xyncra.llm.model` 记录模型名称

### 4.9 阶段 10.5 (HITL) — 步骤 10.5.8

验证 `agent.checkpoint.save` span：

```bash
sleep 3

check_trace "agent.checkpoint.save" "xyncra.agent.id" "agent/hitl-bot"
# 预期: >= 1

# 验证 resume 的 agent.execute 与 process 通过 link 关联
# (Jaeger API 对 link 的查询支持有限，可通过 UI 确认)
```

### 4.10 阶段 11 (流式文本) — 步骤 11.3

验证 `agent.stream` span：

```bash
sleep 2

check_trace "agent.stream" "xyncra.agent.id" "agent/weather-bot"
# 预期: >= 1
# 可选: 检查 xyncra.chunk_count, xyncra.total_chars
```

### 4.11 阶段 13 (IPC Fallback) — 步骤 13.9

验证 fallback WS 短连接的 `ws.connection` span：

```bash
sleep 2

# fallback WS 短连接也会产生 ws.connection span
check_trace "ws.connection" "xyncra.user.id" "alice"
# 预期: >= 1 (注意: 可能包含多个 daemon 连接的 span)
```

## 5. 文档其他更新

### 5.1 概述（第 1 节）

- **覆盖范围**追加："+ 链路追踪验证"
- **测试目标**追加：验证 OpenTelemetry 分布式追踪全链路 span 正确性（D-127 Tracing 静默降级）

### 5.2 环境拓扑（第 2 节）

拓扑图增加 Jaeger 容器：

```
│  ┌──────────────┐         ┌──────────────────────┐         ┌──────────────────────┐
│  │  Jaeger      │◄────────│  xyncra-server       │         │                      │
│  │  16687→16686 │  OTLP   │  18080→8080           │         │                      │
│  │  (Badger)    │  gRPC   │                       │         │                      │
│  └──────────────┘  14317  └──────────────────────┘         └──────────────────────┘
```

端口约定表追加：

| 组件 | 宿主机端口 | 容器端口 | 说明 |
|------|-----------|---------|------|
| Jaeger OTLP gRPC | 14317 | 4317 | OTLP gRPC 接收端 |
| Jaeger OTLP HTTP | 14318 | 4318 | OTLP HTTP 接收端 |
| Jaeger UI | 16687 | 16686 | Trace 可视化 UI |

### 5.3 健康检查（步骤 3.3）

追加 Jaeger 健康检查（见 3.3 节）。

### 5.4 流程图（第 5 节）

在相关阶段的流程节点后增加 🔍 验证节点：

```
P5D[验证服务器 DB] --> P5T[🔍 验证 Jaeger trace\nhandler.invoke + mq.process]
P5T --> P5E[验证 Redis MQ]
```

类似模式应用于其他阶段。

### 5.5 通过标准（第 9.1 节）

每个阶段追加 tracing 判定条件：

| 阶段 | 追加判定条件 |
|------|------------|
| 环境准备 | Jaeger `/api/services` 返回 `xyncra-server` |
| 阶段 1 | `ws.connection` span 存在，attributes 正确 |
| 阶段 2 | `handler.invoke` span (create_conversation) 存在 |
| 阶段 4 | `handler.invoke` span (set_typing) 存在 |
| 阶段 5 | `handler.invoke` + `handler.broker.enqueue` + `mq.process` 存在，同一 trace_id |
| 阶段 7 | `handler.invoke` span (mark_as_read) 存在 |
| 阶段 8 | `handler.invoke` span (delete_message) 存在 |
| 阶段 9 | `handler.invoke` span (delete/restore_conversation) 存在 |
| 阶段 10 | `agent.execute` → `agent.build` → `agent.run` → `agent.llm.call` 完整 |
| 阶段 10.5 | `agent.checkpoint.save` span 存在 |
| 阶段 11 | `agent.stream` span 存在 |
| 阶段 13 | fallback WS 连接产生 `ws.connection` span |

### 5.6 故障排查（第 10 节）

追加：

| 症状 | 可能原因 | 解决方法 |
|------|---------|---------|
| Jaeger `/api/services` 返回空 | Server 未启用 tracing | 检查 `XYNCRA_TRACING_ENABLED=true` |
| span 不存在 | OTLP 连接失败 | 检查服务器日志中 OTLP exporter 错误 |
| `mq.process` trace_id 与 `handler.broker.enqueue` 不同 | MQ context 传播失败 | 检查 `Metadata` 字段注入/提取逻辑 |

### 5.7 测试执行记录模板（第 14 节）

追加阶段 17 (链路追踪) 行：

```
| 链路追踪验证 | ✅ / ❌ | Jaeger span 完整性 |
```

## 6. Span 覆盖矩阵

| 阶段 | Span Name | 关键 Attributes | 验证方式 |
|------|-----------|----------------|---------|
| 1 | `ws.connection` | user.id, device.id, conn.id | `check_trace` |
| 2 | `handler.invoke` | method=create_conversation, user.id | `check_trace` |
| 4 | `handler.invoke` | method=set_typing | `check_trace` |
| 5 | `handler.invoke` | method=send_message | `check_trace` |
| 5 | `handler.broker.enqueue` | task.type, task.id | `check_same_trace` |
| 5 | `mq.process` | task.type | `check_same_trace` |
| 7 | `handler.invoke` | method=mark_as_read | `check_trace` |
| 8 | `handler.invoke` | method=delete_message | `check_trace` |
| 9 | `handler.invoke` | method=delete/restore_conversation | `check_trace` |
| 10 | `agent.execute` | agent.id, conversation.id | `check_trace` |
| 10 | `agent.build` | agent.id | `check_trace` |
| 10 | `agent.run` | agent.id | `check_trace` |
| 10 | `agent.llm.call` | agent.id, llm.model | `check_trace` |
| 10.5 | `agent.checkpoint.save` | agent.id, checkpoint.id | `check_trace` |
| 11 | `agent.stream` | agent.id, chunk_count | `check_trace` |
| 13 | `ws.connection` | user.id (fallback) | `check_trace` |
