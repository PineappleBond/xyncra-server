# TC-007: DynamicToolProvider / 客户端工具端到端测试

> **测试编号**: TC-007
> **测试类型**: 端到端集成测试
> **覆盖范围**: DynamicToolProvider 动态工具注入 (D-100)、客户端函数注册与调用 (D-101)、设备上下文传递 (D-102)、function_tags 过滤、excluded_functions 过滤、设备离线降级、函数清理
> **环境**: Docker E2E (D-043) + 真实 LLM (.env)
> **最后更新**: 2026-07-15 (D-115: daemon 内置函数自动注册)

---

## 1. 概述

本测试用例验证 Agent 的 **DynamicToolProvider** 机制：当客户端 daemon 启动时，内置函数（ping、get_device_info、get_time）通过 `system.register_functions` RPC 自动注册到服务端 FunctionRegistry（D-115）。配置了 `enable_client_tools: true` 的 Agent 能动态将这些函数作为 LLM 工具注入，并在 LLM 决定调用时通过 ReverseRPC 将请求发回客户端 daemon 执行。

**测试目标**：

- 验证 daemon 启动后内置函数自动注册出现在 LLM 请求的 tools 列表中
- 验证 ReverseRPC 端到端调用链路（Agent → 服务器 → 客户端 daemon → 响应 → Agent）
- 验证 `function_tags` 和 `excluded_functions` 过滤正确性
- 验证设备离线时的优雅降级（不影响 Agent 正常响应）
- 验证设备断连后函数被清理

**覆盖的关键决策**：

- D-100: DynamicToolProvider 中间件 — 动态注入客户端函数为 LLM 工具
- D-101: ClientFunctionProvider / ClientCaller 接口 — 函数查询与调用
- D-102: 设备上下文传递 — ExecutePayload 中 DeviceID 用于定位注册设备
- D-072: Fail-open 策略 — 函数注册/调用失败不阻塞 Agent 执行
- D-079: 中间件顺序 — DynamicToolProvider → PatchToolCalls → Summarization → ToolReduction
- D-115: Daemon 内置函数自动注册 — 不需要独立 register-functions 进程

---

## 2. 环境拓扑

```
┌───────────────────────────────────────────────────────────────────┐
│                        Docker E2E 网络                             │
│                                                                   │
│  ┌──────────────┐         ┌──────────────────────┐               │
│  │  Redis 7     │◄────────│  xyncra-server       │               │
│  │  16379→6379  │         │  18080→8080           │               │
│  │  (DB 15)     │         │  SQLite: xyncra-e2e.db│               │
│  └──────────────┘         │  LLM Logger: JSONL    │               │
│         ▲                 └──────────────────────┘               │
│         │ 16379                        ▲                         │
└─────────┼──────────────────────────────┼─────────────────────────┘
          │                              │
┌─────────┼──────────────────────────────┼─────────────────────────┐
│         ▼                              │                         │
│  ┌─────────────────────────┐          │                         │
│  │ xyncra-client listen    │          │                         │
│  │ (daemon, D-115)         │          │                         │
│  │ User: alice             │          │                         │
│  │ device: alice-phone     │          │                         │
│  │                         │          │                         │
│  │ 发送消息 ───────────────┼──────────┤                         │
│  │ (MQ payload 含 deviceID)│          │                         │
│  │                         │          │                         │
│  │ 自动注册内置函数 ────────┼──────────┤                         │
│  │ (ping, get_device_info, │          │                         │
│  │  get_time, D-115)       │          │                         │
│  │                         │          │                         │
│  │ 接收 ReverseRPC 调用 ───┼──────────┤                         │
│  │ (内置 handler 响应)      │          │                         │
│  └─────────────────────────┘          │                         │
│                                                                   │
│  ┌─────────────────┐                                             │
│  │ Agent           │                                             │
│  │ weather-bot     │                                             │
│  │ enable_client_  │                                             │
│  │ tools: true     │                                             │
│  └─────────────────┘                                             │
│                                                                   │
│  LLM 日志: ./llm-logs-e2e/llm-calls.log                          │
│  工作目录: $E2E_HOME (mktemp -d)                                  │
└───────────────────────────────────────────────────────────────────┘
```

**数据流**：

1. Alice daemon 启动时自动注册内置函数（ping、get_device_info、get_time）到服务器 FunctionRegistry（D-115）
2. Alice daemon 发送消息 → MQ 任务入队（payload 含 sender 的 deviceID）
3. Agent executor 加载上下文 → DynamicToolProvider 查询 FunctionRegistry（按 deviceID）
4. 注入的 tools 出现在 LLM 请求中
5. LLM 决定调用工具 → ReverseRPC 请求发回 daemon 进程
6. 内置 handler 返回结果 → 结果作为 tool message 返回 LLM
7. LLM 生成最终响应 → 消息持久化

---

## 3. 前置条件

### 3.1 构建二进制

```bash
cd /path/to/xyncra-server
make build
```

确认产出：
- `bin/xyncra-server`
- `bin/xyncra-client`

### 3.2 启动 Docker E2E 环境

```bash
docker compose -f deploy/docker-compose.e2e.yml build --no-cache && \
docker compose -f deploy/docker-compose.e2e.yml up -d
```

### 3.3 健康检查

```bash
redis-cli -p 16379 ping
# 预期: PONG

curl -s http://localhost:18080/health
# 预期: {"status":"ok","connections":N}
```

### 3.4 创建测试工作目录

```bash
export E2E_HOME=$(mktemp -d /tmp/xe2e-XXXXXX)
echo "E2E_HOME=$E2E_HOME"
```

### 3.5 配置真实 LLM (.env)

确保 `.env` 已配置（参考 `.env.example`）：

```bash
test -f .env && echo "✓ .env exists" || echo "✗ .env missing"
```

### 3.6 确认 Agent 配置

确认 `agents/weather-bot.md` 的 middleware 中包含 `enable_client_tools: true`：

```bash
grep "enable_client_tools" agents/weather-bot.md
# 预期: enable_client_tools: true
```

### 3.7 ⚠️ Agent 配置文件同步注意事项

> **重要**：Docker 容器内的 `agents/` 目录是在镜像构建时通过 `COPY` 指令导入的，**并非运行时挂载**。
> 在测试过程中修改宿主机 `agents/` 目录下的文件后，**必须使用 `docker cp` 将文件拷贝进容器**，然后再调用 `reload_agents`：
>
> ```bash
> # 修改宿主机文件后
> docker cp agents/weather-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/weather-bot.md
> curl -s -X POST http://localhost:18080/rpc \
>   -H "Content-Type: application/json" \
>   -d '{"jsonrpc":"2.0","method":"reload_agents","id":1}'
> ```
>
> 仅调用 `reload_agents` **不会**加载宿主机上的新配置——它只重新读取容器内的文件。

### 3.8 确认内置函数

daemon 启动时自动注册 3 个内置函数（D-115）：

| 名称 | 描述 | 标签 |
|------|------|------|
| `ping` | 回声测试，验证 ReverseRPC 通道 | diagnostic |
| `get_device_info` | 设备基本信息（hostname、OS、arch） | diagnostic |
| `get_time` | 设备当前时间（UTC + timezone） | diagnostic |

> **可选**: 通过 `--device-info` flag 传入自定义设备元数据（JSON 格式），如 `'{"name":"MacBook","os":"darwin","app_version":"1.2.3"}'`。默认为空。

---

## 4. 测试数据字典

| 变量 | 值 | 说明 |
|------|-----|------|
| `$SERVER_URL` | `ws://localhost:18080/ws` | E2E 服务器 WebSocket 地址 |
| `$REDIS_ADDR` | `localhost:16379` | E2E Redis 地址 |
| `$REDIS_DB` | `15` | E2E Redis DB 编号 |
| `$ALICE` | `alice` | 测试用户 Alice |
| `$DEVICE_ID` | `alice-phone` | Alice 的设备 ID（daemon 自动注册内置函数） |
| `$E2E_HOME` | `/tmp/xe2e-XXXXXX` | 临时测试目录 |
| `$CONV_ID` | (运行时获取) | Agent 会话 ID |

---

## 5. 完整流程图

```mermaid
flowchart TD
    Start([开始]) --> Prep[🟢 环境准备<br/>构建、启动 Docker<br/>配置 Agent]

    Prep --> Phase1

    subgraph Phase1 [阶段 1: 函数自动注册验证 (D-115)]
        P1A[🔵 启动 Alice daemon<br/>内置函数自动注册] --> P1B[🟡 验证 FunctionRegistry<br/>服务器日志确认 3 个内置函数已注册]
    end

    P1B --> Phase2

    subgraph Phase2 [阶段 2: LLM Tool 注入验证]
        P2A[🔵 Alice 创建 Agent 会话] --> P2B[🔵 Alice 发送消息<br/>"检查我的设备状态"]
        P2B --> P2C[🟡 收集 LLM 日志<br/>JSONL 文件]
        P2C --> P2D[🔴 人工评审<br/>tools 列表包含内置函数 ping/get_device_info/get_time]
    end

    P2D --> Phase3

    subgraph Phase3 [阶段 3: 端到端 Tool 调用验证]
        P3A[🔵 Alice 发送工具触发消息] --> P3B[🟡 检查 daemon 日志<br/>是否收到 ReverseRPC]
        P3B --> P3C[🟡 检查 LLM 日志<br/>是否有 tool role 消息]
        P3C --> P3D[🔴 验证完整链路<br/>内置函数被调用且结果返回 LLM]
    end

    P3D --> Phase4

    subgraph Phase4 [阶段 4: function_tags 过滤]
        P4A[🟢 修改 Agent 配置<br/>function_tags: diagnostic] --> P4B[🟢 docker cp + reload_agents]
        P4B --> P4C[🔵 发送新消息]
        P4C --> P4D[🟡 收集 LLM 日志]
        P4D --> P4E[🔴 评审<br/>全部 3 个内置函数被注入<br/>均为 diagnostic 标签]
    end

    P4E --> Phase5

    subgraph Phase5 [阶段 5: excluded_functions 过滤]
        P5A[🟢 修改 Agent 配置<br/>excluded_functions: ping] --> P5B[🟢 docker cp + reload_agents]
        P5B --> P5C[🔵 发送新消息]
        P5C --> P5D[🟡 收集 LLM 日志]
        P5D --> P5E[🔴 评审<br/>ping 不在 tools 中<br/>get_device_info/get_time 正常注入]
    end

    P5E --> Phase6

    subgraph Phase6 [阶段 6: enable_client_tools 默认禁用]
        P6A[🟢 修改 Agent 配置<br/>enable_client_tools: false] --> P6B[🟢 docker cp + reload_agents]
        P6B --> P6C[🔵 发送新消息]
        P6C --> P6D[🟡 收集 LLM 日志]
        P6D --> P6E[🔴 评审<br/>无任何客户端函数被注入]
    end

    P6E --> Phase7

    subgraph Phase7 [阶段 7: 设备离线降级]
        P7A[🔵 停止 Alice daemon] --> P7B[🟡 验证函数已清理<br/>FunctionRegistry 为空]
        P7B --> P7C[🟢 恢复 Agent 配置<br/>enable_client_tools: true]
        P7C --> P7C2[🟢 docker cp + reload_agents]
        P7C2 --> P7E[🔵 用新 daemon 发送消息]
        P7E --> P7F[🔴 验证 Agent 正常响应<br/>无客户端工具注入（旧设备已清理）]
    end

    P7F --> Cleanup

    subgraph Cleanup [⚪ 环境清理]
        CL1[停止 Alice daemon] --> CL3[停止 Docker]
        CL3 --> CL4[清理临时目录]
    end

    Cleanup --> End([结束])
```

---

## 6. 分步执行指南

### 阶段 1: 函数自动注册验证 (D-115)

#### 步骤 1.1: 启动 Alice daemon（内置函数自动注册）

```bash
./bin/xyncra-client listen \
  --user-id alice \
  --device-id alice-phone \
  --server ws://localhost:18080/ws \
  --device-info '{"name":"alice-cli","os":"linux","type":"cli"}' \
  > "$E2E_HOME/alice-daemon.log" 2>&1 &
ALICE_PID=$!
sleep 3
```

**验证**：

```bash
# 检查 daemon 进程
ps -p $ALICE_PID
# 预期: 显示进程信息

# 检查 Redis 连接
redis-cli -p 16379 -n 15 SMEMBERS "xyncra:conn:user:alice"
# 预期: 包含至少一个 connID
```

#### 步骤 1.2: 验证内置函数自动注册

```bash
# 检查服务器日志确认 3 个内置函数已注册
docker compose -f deploy/docker-compose.e2e.yml logs xyncra-server-e2e --tail 50 2>&1 | grep -i "functions registered"
# 预期: 看到 "functions registered" 且 count=3

# 检查 daemon 日志确认注册成功
grep -i "function\|register" "$E2E_HOME/alice-daemon.log"
# 预期: 看到函数注册相关日志
```

**验证（数据库）**：

```bash
# 检查 Redis 连接信息
redis-cli -p 16379 -n 15 KEYS "xyncra:conn:info:*"
# 预期: 可看到 alice 的连接信息
```

**记录变量**：

```bash
echo "ALICE_PID=$ALICE_PID"
```

---

### 阶段 2: LLM Tool 注入验证

> **目标**：验证注册的函数出现在 LLM 请求的 tools 列表中。

#### 步骤 2.1: 清空 LLM 日志

```bash
# 清空旧的 LLM 日志，方便后续分析
> ./llm-logs-e2e/llm-calls.log 2>/dev/null || true
```

#### 步骤 2.2: Alice 创建 Agent 会话

```bash
./bin/xyncra-client create-conversation \
  --user-id alice \
  --device-id alice-phone \
  --server ws://localhost:18080/ws \
  --peer-id "agent/weather-bot"
```

**预期输出**（类似）：
```
Conversation created.
ID:       <conv-uuid>
Peer:     agent/weather-bot
Type:     1-on-1
```

**操作**：
```bash
CONV_ID="<从输出中获取的会话 ID>"
echo "CONV_ID=$CONV_ID"
```

**验证（数据库）**：

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db "SELECT id, user_id1, user_id2, type FROM conversations WHERE id = '$CONV_ID';"
# 预期: $CONV_ID|alice|agent/weather-bot|1-on-1
```

#### 步骤 2.3: Alice 发送消息触发 Agent 处理

```bash
./bin/xyncra-client send \
  --user-id alice \
  --device-id alice-phone \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "请获取我的设备信息，包括主机名和操作系统。"
```

**预期**：消息发送成功，返回 MSG_ID 和 Seq。

#### 步骤 2.4: 等待 Agent 处理

```bash
sleep 15
```

#### 步骤 2.5: 收集 LLM 日志

```bash
# 从 volume 挂载目录读取
cp ./llm-logs-e2e/llm-calls.log "$E2E_HOME/llm-records-phase2.jsonl" 2>/dev/null || true

# 或从容器内读取
docker exec xyncra-server-xyncra-server-e2e-1 \
  cat /app/llm-logs/llm-calls.log > "$E2E_HOME/llm-records-phase2.jsonl" 2>/dev/null || true

# 提取 request 阶段记录
grep '"phase":"request"' "$E2E_HOME/llm-records-phase2.jsonl" > "$E2E_HOME/llm-requests-phase2.jsonl"

# 查看记录数
wc -l "$E2E_HOME/llm-requests-phase2.jsonl"
# 预期: 至少 1 条 request 记录
```

#### 步骤 2.6: 人工评审 LLM 日志 — Tool 注入

```bash
cat "$E2E_HOME/llm-requests-phase2.jsonl" | \
  python3 -c "
import sys, json

for line in sys.stdin:
    record = json.loads(line.strip())
    tools = record.get('tools', [])
    print(f'=== Request (iteration={record.get(\"iteration\", \"?\")}) ===')
    print(f'Tools count: {len(tools)}')
    for t in tools:
        fn = t.get('function', {})
        print(f'  - {fn.get(\"name\", \"?\")}: {fn.get(\"description\", \"?\")[:60]}')
    print()
"
```

**评审标准**：

| 检查项 | 预期 | 通过标志 |
|--------|------|----------|
| tools 列表包含 `ping` | ✅ Agent 注入了 diagnostic 函数 | 函数名出现 |
| tools 列表包含 `get_device_info` | ✅ Agent 注入了 diagnostic 函数 | 函数名出现 |
| tools 列表包含 `get_time` | ✅ Agent 注入了 diagnostic 函数 | 函数名出现 |
| tools 列表包含 `get_weather` | ✅ Agent 自带的内置工具也在 | 函数名出现 |
| 函数描述与注册时一致 | ✅ description 字段匹配 | 描述正确 |

**记录评审结果**：
```
阶段 2 评审:
- 总 tools 数量: ___
- 客户端函数数量: ___ (预期 3: ping, get_device_info, get_time)
- 内置工具数量: ___ (预期 ≥1)
- 总体判定: ✅ PASS / ❌ FAIL
```

**验证（客户端命令）**：

```bash
# Alice 同步并检查是否有 Agent 回复
./bin/xyncra-client sync-updates --user-id alice --device-id alice-phone

./bin/xyncra-client get-messages \
  --user-id alice \
  --device-id alice-phone \
  --conversation-id "$CONV_ID" \
  --limit 5
# 预期: 包含 Alice 的消息和 Agent 的回复
```

**验证（数据库）**：

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db \
  "SELECT sender_id, SUBSTR(content, 1, 100) FROM messages WHERE conversation_id = '$CONV_ID' ORDER BY message_id DESC LIMIT 5;"
# 预期: 包含 sender_id = 'agent/weather-bot' 的消息
```

---

### 阶段 3: 端到端 Tool 调用验证

> **目标**：验证 LLM 调用客户端函数时，ReverseRPC 请求到达 daemon 进程并返回结果。

#### 步骤 3.1: 清空 LLM 日志和 daemon 日志

```bash
> ./llm-logs-e2e/llm-calls.log 2>/dev/null || true
> "$E2E_HOME/alice-daemon.log" 2>/dev/null || true
```

#### 步骤 3.2: 发送强烈暗示使用工具的消息

```bash
./bin/xyncra-client send \
  --user-id alice \
  --device-id alice-phone \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "请使用 get_device_info 工具查看我的设备信息。"
```

#### 步骤 3.3: 等待处理并检查 daemon 日志

```bash
sleep 15

cat "$E2E_HOME/alice-daemon.log"
```

**预期（如果 LLM 调用了工具）**：
```
[xyncra] incoming request: method=get_device_info ...
```

**如果 LLM 没有调用工具**：
```
（无 ReverseRPC 相关日志）
```

> ⚠️ **注意**：真实 LLM 不一定会调用工具。如果 LLM 选择不调用，则验证 ReverseRPC 链路无法完成。此情况下，阶段 3 标记为 **INCONCLUSIVE**，但阶段 2 的 tool 注入验证仍然有效。

#### 步骤 3.4: 检查 LLM 日志中的 tool 交互

```bash
cp ./llm-logs-e2e/llm-calls.log "$E2E_HOME/llm-records-phase3.jsonl" 2>/dev/null || true

# 检查是否有包含 tool role 消息的 request（说明工具被调用并返回了结果）
grep '"phase":"request"' "$E2E_HOME/llm-records-phase3.jsonl" | \
  python3 -c "
import sys, json

for line in sys.stdin:
    record = json.loads(line.strip())
    msgs = record.get('messages', [])
    tool_msgs = [m for m in msgs if m.get('role') == 'tool']
    if tool_msgs:
        print(f'=== 发现 tool 交互 (iteration={record.get(\"iteration\", \"?\")}) ===')
        for tm in tool_msgs:
            content = tm.get('content', '')
            print(f'  Tool result: {content[:200]}')
        print()
"
```

**评审标准**：

| 检查项 | 预期 | 通过标志 |
|--------|------|----------|
| daemon 收到 ReverseRPC | 日志包含 `incoming request` | ✅ 如果看到 |
| tool 结果包含设备信息 | 包含 hostname、os 等数据 | ✅ 如果看到 |
| LLM 收到 tool 结果后生成最终回复 | messages 中有 `role: tool` 消息 | ✅ 如果看到 |

**验证（数据库）**：

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db \
  "SELECT sender_id, SUBSTR(content, 1, 200) FROM messages WHERE conversation_id = '$CONV_ID' ORDER BY message_id DESC LIMIT 3;"
# 预期: 包含 Agent 的最终回复（可能引用了设备状态数据）
```

**验证（客户端命令）**：

```bash
./bin/xyncra-client sync-updates --user-id alice --device-id alice-phone

./bin/xyncra-client get-messages \
  --user-id alice \
  --device-id alice-phone \
  --conversation-id "$CONV_ID" \
  --limit 3
# 预期: Agent 回复中可能包含 "online"、"battery"、"85" 等关键词
```

---

### 阶段 4: function_tags 过滤 (D-100)

> **目标**：验证配置 `function_tags: ["diagnostic"]` 后，只有带 `diagnostic` 标签的函数被注入。

#### 步骤 4.1: 备份并修改 Agent 配置

```bash
cp agents/weather-bot.md agents/weather-bot.md.bak

# 在 middleware 段添加 client_tools.function_tags
# 使用 python3 修改 YAML front matter
python3 -c "
import re

with open('agents/weather-bot.md', 'r') as f:
    content = f.read()

# 在 enable_client_tools: true 后添加 client_tools 配置
new_content = content.replace(
    'enable_client_tools: true',
    'enable_client_tools: true\n  client_tools:\n    function_tags:\n      - diagnostic'
)

with open('agents/weather-bot.md', 'w') as f:
    f.write(new_content)
"

# 验证修改
grep -A 3 "client_tools" agents/weather-bot.md
# 预期: 看到 function_tags: [diagnostic]
```

#### 步骤 4.2: 同步配置到容器并重新加载 Agent

```bash
# 必须 docker cp，因为容器内 agents/ 是构建时 COPY 的（见 §3.7）
docker cp agents/weather-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/weather-bot.md
curl -s -X POST http://localhost:18080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"reload_agents","id":1}'
# 预期: {"result":{"count":N},"error":null}
```

#### 步骤 4.3: 清空日志并发送测试消息

```bash
> ./llm-logs-e2e/llm-calls.log 2>/dev/null || true

./bin/xyncra-client send \
  --user-id alice \
  --device-id alice-phone \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "帮我查看设备信息和当前时间。"

sleep 15
```

#### 步骤 4.4: 收集并评审 LLM 日志

```bash
cp ./llm-logs-e2e/llm-calls.log "$E2E_HOME/llm-records-phase4.jsonl" 2>/dev/null || true

grep '"phase":"request"' "$E2E_HOME/llm-records-phase4.jsonl" | \
  python3 -c "
import sys, json

for line in sys.stdin:
    record = json.loads(line.strip())
    tools = record.get('tools', [])
    tool_names = [t.get('function', {}).get('name', '') for t in tools]
    print(f'Tools: {tool_names}')

    # 检查过滤结果
    client_tools = [n for n in tool_names if n in ['ping', 'get_device_info', 'get_time']]
    print(f'客户端函数: {client_tools}')

    has_diagnostic = 'get_device_info' in tool_names or 'get_time' in tool_names or 'ping' in tool_names

    print(f'包含 diagnostic 标签函数: {has_diagnostic} (预期: True)')
"
```

**评审标准**：

| 检查项 | 预期 | 通过标志 |
|--------|------|----------|
| `ping` 在 tools 中 | ✅ 标签 = diagnostic | 出现 |
| `get_device_info` 在 tools 中 | ✅ 标签 = diagnostic | 出现 |
| `get_time` 在 tools 中 | ✅ 标签 = diagnostic | 出现 |

**记录评审结果**：
```
阶段 4 评审:
- 注入的客户端函数: ___ (预期: ping, get_device_info, get_time — 全部为 diagnostic 标签)
- 总体判定: ✅ PASS / ❌ FAIL
```

#### 步骤 4.5: 恢复 Agent 配置

```bash
mv agents/weather-bot.md.bak agents/weather-bot.md
docker cp agents/weather-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/weather-bot.md
curl -s -X POST http://localhost:18080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"reload_agents","id":1}' > /dev/null
```

---

### 阶段 5: excluded_functions 过滤 (D-100)

> **目标**：验证配置 `excluded_functions: ["ping"]` 后，该函数不被注入，其他函数正常注入。

#### 步骤 5.1: 备份并修改 Agent 配置

```bash
cp agents/weather-bot.md agents/weather-bot.md.bak

python3 -c "
with open('agents/weather-bot.md', 'r') as f:
    content = f.read()

new_content = content.replace(
    'enable_client_tools: true',
    'enable_client_tools: true\n  client_tools:\n    excluded_functions:\n      - ping'
)

with open('agents/weather-bot.md', 'w') as f:
    f.write(new_content)
"

grep -A 3 "client_tools" agents/weather-bot.md
# 预期: 看到 excluded_functions: [ping]
```

#### 步骤 5.2: 同步配置到容器并重新加载 Agent

```bash
docker cp agents/weather-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/weather-bot.md
curl -s -X POST http://localhost:18080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"reload_agents","id":1}'
```

#### 步骤 5.3: 清空日志并发送测试消息

```bash
> ./llm-logs-e2e/llm-calls.log 2>/dev/null || true

./bin/xyncra-client send \
  --user-id alice \
  --device-id alice-phone \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "请帮我检查设备状态。"

sleep 15
```

#### 步骤 5.4: 收集并评审 LLM 日志

```bash
cp ./llm-logs-e2e/llm-calls.log "$E2E_HOME/llm-records-phase5.jsonl" 2>/dev/null || true

grep '"phase":"request"' "$E2E_HOME/llm-records-phase5.jsonl" | \
  python3 -c "
import sys, json

for line in sys.stdin:
    record = json.loads(line.strip())
    tools = record.get('tools', [])
    tool_names = [t.get('function', {}).get('name', '') for t in tools]
    print(f'Tools: {tool_names}')

    client_tools = [n for n in tool_names if n in ['ping', 'get_device_info', 'get_time']]
    print(f'客户端函数: {client_tools}')
    print(f'ping 被排除: {\"ping\" not in tool_names} (预期: True)')
    print(f'其他函数正常注入: {len(client_tools) >= 2} (预期: True, 应有 2 个)')
"
```

**评审标准**：

| 检查项 | 预期 | 通过标志 |
|--------|------|----------|
| `ping` **不在** tools 中 | ✅ 被 excluded_functions 排除 | 不出现 |
| `get_device_info` 在 tools 中 | ✅ 未被排除 | 出现 |
| `get_time` 在 tools 中 | ✅ 未被排除 | 出现 |

**记录评审结果**：
```
阶段 5 评审:
- 注入的客户端函数: ___ (预期: get_device_info, get_time)
- 被排除的函数: ___ (预期: ping)
- 总体判定: ✅ PASS / ❌ FAIL
```

#### 步骤 5.5: 恢复 Agent 配置

```bash
mv agents/weather-bot.md.bak agents/weather-bot.md
docker cp agents/weather-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/weather-bot.md
curl -s -X POST http://localhost:18080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"reload_agents","id":1}' > /dev/null
```

---

### 阶段 6: enable_client_tools 默认禁用验证

> **目标**：验证当 `enable_client_tools: false` 时，即使函数已注册，也不会被注入到 LLM 请求。

#### 步骤 6.1: 修改 Agent 配置

```bash
cp agents/weather-bot.md agents/weather-bot.md.bak

# 将 enable_client_tools: true 改为 false
sed -i '' 's/enable_client_tools: true/enable_client_tools: false/' agents/weather-bot.md

grep "enable_client_tools" agents/weather-bot.md
# 预期: enable_client_tools: false
```

#### 步骤 6.2: 同步配置到容器并重新加载 Agent

```bash
docker cp agents/weather-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/weather-bot.md
curl -s -X POST http://localhost:18080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"reload_agents","id":1}'
```

#### 步骤 6.3: 清空日志并发送测试消息

```bash
> ./llm-logs-e2e/llm-calls.log 2>/dev/null || true

./bin/xyncra-client send \
  --user-id alice \
  --device-id alice-phone \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "请检查我的设备状态。"

sleep 15
```

#### 步骤 6.4: 收集并评审 LLM 日志

```bash
cp ./llm-logs-e2e/llm-calls.log "$E2E_HOME/llm-records-phase6.jsonl" 2>/dev/null || true

grep '"phase":"request"' "$E2E_HOME/llm-records-phase6.jsonl" | \
  python3 -c "
import sys, json

for line in sys.stdin:
    record = json.loads(line.strip())
    tools = record.get('tools', [])
    tool_names = [t.get('function', {}).get('name', '') for t in tools]
    client_tools = [n for n in tool_names if n in ['ping', 'get_device_info', 'get_time']]
    print(f'客户端函数注入数量: {len(client_tools)} (预期: 0)')
    print(f'客户端函数列表: {client_tools}')
    print(f'判定: {\"✅ PASS\" if len(client_tools) == 0 else \"❌ FAIL\"}')
"
```

**评审标准**：

| 检查项 | 预期 | 通过标志 |
|--------|------|----------|
| 客户端函数数量为 0 | ✅ 即使函数已注册也不注入 | 0 个客户端函数 |
| Agent 正常响应 | ✅ 不影响 Agent 基本功能 | 有 Agent 回复消息 |

**验证（数据库）**：

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db \
  "SELECT sender_id, SUBSTR(content, 1, 100) FROM messages WHERE conversation_id = '$CONV_ID' ORDER BY message_id DESC LIMIT 2;"
# 预期: 包含 Agent 回复
```

#### 步骤 6.5: 恢复 Agent 配置

```bash
mv agents/weather-bot.md.bak agents/weather-bot.md
docker cp agents/weather-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/weather-bot.md
curl -s -X POST http://localhost:18080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"reload_agents","id":1}' > /dev/null
```

---

### 阶段 7: 设备离线降级 (D-072)

> **目标**：验证当 daemon 停止后，Agent 正常执行（无客户端工具注入，不崩溃）。

#### 步骤 7.1: 停止 Alice daemon

```bash
./bin/xyncra-client kill --user-id alice --device-id alice-phone
sleep 2

# 验证进程已退出
ps -p $ALICE_PID 2>&1
# 预期: 无输出或 "No such process"
```

#### 步骤 7.2: 验证函数被清理

```bash
# 等待服务器处理断连
sleep 3

# 检查服务器日志
docker compose -f deploy/docker-compose.e2e.yml logs xyncra-server-e2e --tail 20 2>&1 | grep -i "disconnect\|cleanup\|function"
# 预期: 看到设备断连和函数清理相关日志
```

**验证（Redis）**：

```bash
# 检查 alice 的连接数（daemon 连接已断开）
redis-cli -p 16379 -n 15 SMEMBERS "xyncra:conn:user:alice"
# 预期: 无成员或连接数减少
```

#### 步骤 7.3: 启动新 daemon 发送消息（验证旧设备函数已清理）

```bash
# 用新的 device-id 启动 daemon（旧设备函数已被清理）
./bin/xyncra-client listen \
  --user-id alice \
  --device-id alice-tablet \
  --server ws://localhost:18080/ws \
  --device-info '{"name":"alice-tablet","os":"linux","type":"cli"}' \
  > "$E2E_HOME/alice-tablet-daemon.log" 2>&1 &
sleep 3

> ./llm-logs-e2e/llm-calls.log 2>/dev/null || true

./bin/xyncra-client send \
  --user-id alice \
  --device-id alice-tablet \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "你好，请介绍一下你自己。"

sleep 15
```

#### 步骤 7.4: 验证无客户端工具注入

```bash
cp ./llm-logs-e2e/llm-calls.log "$E2E_HOME/llm-records-phase7.jsonl" 2>/dev/null || true

grep '"phase":"request"' "$E2E_HOME/llm-records-phase7.jsonl" | \
  python3 -c "
import sys, json

for line in sys.stdin:
    record = json.loads(line.strip())
    tools = record.get('tools', [])
    tool_names = [t.get('function', {}).get('name', '') for t in tools]
    client_tools = [n for n in tool_names if n in ['ping', 'get_device_info', 'get_time']]
    print(f'客户端函数注入数量: {len(client_tools)} (预期: 0 — 旧设备已清理，新设备尚未注册)')
"
```

**验证（数据库）**：

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db \
  "SELECT sender_id, SUBSTR(content, 1, 100) FROM messages WHERE conversation_id = '$CONV_ID' ORDER BY message_id DESC LIMIT 2;"
# 预期: 包含 Agent 正常回复（不因设备离线而崩溃）
```

**验证（客户端命令）**：

```bash
./bin/xyncra-client sync-updates --user-id alice --device-id alice-phone

./bin/xyncra-client get-messages \
  --user-id alice \
  --device-id alice-phone \
  --conversation-id "$CONV_ID" \
  --limit 2
# 预期: Agent 正常回复
```

---

## 7. 数据库验证汇总

### 7.1 Server DB 验证命令速查

```bash
DB_EXEC="docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e sqlite3 /app/xyncra-e2e.db"

# 查看会话
$DB_EXEC "SELECT id, user_id1, user_id2, type FROM conversations WHERE id='$CONV_ID';"

# 查看消息（含 Agent 回复）
$DB_EXEC "SELECT sender_id, SUBSTR(content, 1, 100), type FROM messages WHERE conversation_id='$CONV_ID' ORDER BY message_id DESC LIMIT 10;"

# 统计消息数
$DB_EXEC "SELECT sender_id, COUNT(*) FROM messages WHERE conversation_id='$CONV_ID' GROUP BY sender_id;"
```

### 7.2 Server Redis 验证命令速查

```bash
R="redis-cli -p 16379 -n 15"

# 连接信息
$R KEYS "xyncra:conn:info:*"
$R SMEMBERS "xyncra:conn:user:alice"

# Agent 相关
$R KEYS "agent:idempotent:*"
$R KEYS "agent:lock:*"

# 清理
$R FLUSHDB
```

### 7.3 Client DB Sqlite 验证命令速查

```bash
# 查看客户端本地消息
ALICE_DB=$(ls ~/.xyncra/alice/*/xyncra.db 2>/dev/null | head -1)
sqlite3 "$ALICE_DB" "SELECT COUNT(*) FROM messages WHERE conversation_id='$CONV_ID';"

# 查看客户端本地会话
sqlite3 "$ALICE_DB" "SELECT id, user_id1, user_id2 FROM conversations WHERE id='$CONV_ID';"
```

---

## 8. 通过/失败判定标准

| 阶段 | 判定条件 | 通过标志 | 失败处理 |
|------|---------|----------|---------|
| 阶段 1 | daemon 启动后内置函数自动注册 | ✅ 服务器日志显示 "functions registered" count=3 | 检查 WS 连接、daemon 日志 |
| 阶段 2 | LLM 请求的 tools 列表包含全部 3 个内置函数 | ✅ 3 个函数名均出现在 tools 中 | 检查 enable_client_tools 配置、device-id 一致性 |
| 阶段 3 | ReverseRPC 到达 daemon 并返回结果（如 LLM 调用了工具） | ✅ 或 INCONCLUSIVE（LLM 未调用工具） | 检查 ReverseRPC 链路、设备连接 |
| 阶段 4 | 全部 3 个内置函数被注入（均为 diagnostic 标签） | ✅ ping + get_device_info + get_time 在 tools 中 | 检查 function_tags 配置、YAML 格式 |
| 阶段 5 | ping 被排除，其他 2 个函数正常注入 | ✅ 2 个函数在，ping 不在 | 检查 excluded_functions 配置 |
| 阶段 6 | 无任何客户端函数被注入 | ✅ 客户端函数数量 = 0 | 检查 enable_client_tools 是否已设为 false |
| 阶段 7 | 设备离线后 Agent 正常响应，无客户端工具注入 | ✅ Agent 回复正常，客户端函数数量 = 0 | 检查断连处理、函数清理 |

---

## 9. 故障排查指南

| 症状 | 可能原因 | 解决方法 |
|------|---------|---------|
| daemon 连接失败 | Server 未启动或 URL 错误 | 检查 `curl http://localhost:18080/health` |
| 函数注册失败（日志显示错误） | 网络问题或服务器异常 | 检查服务器日志、daemon 日志 |
| LLM 日志中无客户端函数 | enable_client_tools 未启用 | 检查 `agents/weather-bot.md` 配置 |
| LLM 日志中无客户端函数 | device-id 不匹配 | 确保发送消息时使用与 daemon 相同的 device-id |
| 修改配置后 reload_agents 无效果 | 容器内 `agents/` 是构建时 COPY 的，非挂载 | 修改宿主机文件后必须先 `docker cp` 到容器内再调 `reload_agents`，见 §3.7 |
| LLM 未调用工具 | LLM 自主决策不调用 | 尝试更明确的提示词，如 "请使用 get_device_info 工具" |
| daemon 未收到 ReverseRPC | LLM 未选择调用工具 | 这是正常行为，标记为 INCONCLUSIVE |
| reload_agents 返回 count=0 | Agent 配置加载失败 | 检查 YAML front matter 格式 |
| 阶段 7 Agent 无响应 | 断连处理异常 | 检查服务器日志中的错误 |

---

## 10. 环境清理

```bash
# 停止 Alice daemon（所有实例）
./bin/xyncra-client kill --user-id alice --device-id alice-phone 2>/dev/null || true
./bin/xyncra-client kill --user-id alice --device-id alice-tablet 2>/dev/null || true
./bin/xyncra-client kill --user-id alice --device-id alice-phone --force 2>/dev/null || true
./bin/xyncra-client kill --user-id alice --device-id alice-tablet --force 2>/dev/null || true

# 验证进程退出
ps aux | grep xyncra-client | grep -v grep
# 预期: 无输出

# 恢复 Agent 配置（如被修改）
git checkout agents/weather-bot.md 2>/dev/null || true

# 重新加载 Agent
curl -s -X POST http://localhost:18080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"reload_agents","id":1}' > /dev/null 2>&1

# 停止 Docker E2E
docker compose -f deploy/docker-compose.e2e.yml down

# 清理临时目录
rm -rf "$E2E_HOME"

# 清理 ~/.xyncra 中的测试数据
rm -rf ~/.xyncra/alice

# 清理 LLM 日志
rm -f ./llm-logs-e2e/llm-calls.log

# 清理 Redis（可选）
redis-cli -p 16379 -n 15 FLUSHDB
```

---

## 11. 真实 LLM 测试配置 (.env)

本测试需要真实 LLM 调用，依赖 `.env` 配置：

```bash
# .env 示例（参考 .env.example）
XYNCRA_TEST_REAL_LLM_ENABLED=true
XYNCRA_TEST_LLM_API_KEY=your_api_key_here
XYNCRA_TEST_LLM_BASE_URL=https://coding.dashscope.aliyuncs.com/v1
XYNCRA_TEST_LLM_MODEL=qwen3.7-plus
XYNCRA_TEST_LLM_PROVIDER=qwen
```

**安全提示**：
- ❌ 不要提交 `.env` 到 git（已在 `.gitignore` 中排除）
- ✅ 使用 `.env.example` 作为模板
- ✅ 定期轮换 API Key

**成本控制 (D-090)**：
- 每个阶段约消耗 5k-20k tokens
- 完整测试（7 个阶段）约消耗 50k-150k tokens
- 建议在低峰期执行（避免限流）
- `weather-bot` 使用 `qwen3.7-plus` 模型，成本较低

---

## 12. 依赖关系说明

| 测试阶段 | 可独立执行 | 依赖 |
|---------|-----------|------|
| 阶段 1 (函数自动注册) | ✅ | 环境准备 |
| 阶段 2 (Tool 注入) | ✅ | 阶段 1 |
| 阶段 3 (端到端调用) | ✅ | 阶段 2 |
| 阶段 4 (function_tags) | ✅ | 阶段 1 |
| 阶段 5 (excluded_functions) | ✅ | 阶段 1 |
| 阶段 6 (默认禁用) | ✅ | 阶段 1 |
| 阶段 7 (设备离线) | ✅ | 阶段 1（然后停止 daemon） |

**执行顺序**：阶段 1 → 2 → 3 → 4 → 5 → 6 → 7（严格顺序，因为每个阶段需要修改 Agent 配置并 reload）

**可并行的阶段**：无（每个阶段修改 Agent 配置后需 reload，并行会互相干扰）

---

## 13. 测试执行记录模板

```markdown
### TC-007 测试执行记录

| 字段 | 值 |
|------|-----|
| 日期 | YYYY-MM-DD |
| Git Commit | <sha> |
| 测试者 | <name> |
| 环境 | Docker E2E + 真实 LLM (qwen3.7-plus) |
| E2E_HOME | /tmp/xe2e-XXXXXX |
| 总耗时 | XXm |

| 阶段 | 结果 | 备注 |
|------|------|------|
| 阶段 1: 函数自动注册 | ✅ / ❌ | 3 个内置函数自动注册成功 |
| 阶段 2: Tool 注入 | ✅ / ❌ | LLM tools 列表包含 N 个内置函数 |
| 阶段 3: 端到端调用 | ✅ / ❌ / INCONCLUSIVE | LLM 是否调用了工具 |
| 阶段 4: function_tags | ✅ / ❌ | 全部 3 个 diagnostic 函数被注入 |
| 阶段 5: excluded_functions | ✅ / ❌ | ping 被排除 |
| 阶段 6: 默认禁用 | ✅ / ❌ | 客户端函数数量 = 0 |
| 阶段 7: 设备离线 | ✅ / ❌ | Agent 正常响应 |

**LLM 行为观察**：
- 阶段 2 中 LLM 是否主动调用了工具？是 / 否
- 阶段 3 中 daemon 是否收到 ReverseRPC？是 / 否
- 如果 LLM 未调用工具，使用的提示词是："..."

**发现的问题**：
1. (描述)

**结论**：PASS / FAIL (X/7 阶段通过)
```

---

## 14. 参考文档

- [PRODUCT_DECISIONS.md](../../../docs/PRODUCT_DECISIONS.md) — D-100, D-101, D-102, D-072, D-079, D-115
- [TC-000-完整链路测试.md](../../../docs/manual-test-cases/TC-000-完整链路测试.md) — Agent 基础交互、ReverseRPC 基础
- [TC-002-Phase5补发机制测试.md](../../../docs/manual-test-cases/TC-002-Phase5补发机制测试.md) — ReverseRPC Pending Store
- [internal/agent/dynamic_tool_provider.go](../../../internal/agent/dynamic_tool_provider.go) — DynamicToolProvider 实现
- [internal/agent/client_function_tool.go](../../../internal/agent/client_function_tool.go) — 客户端函数工具实现
- [internal/cli/builtin_functions.go](../../../internal/cli/builtin_functions.go) — 内置函数定义与 handler (D-115)
- [pkg/protocol/function.go](../../../pkg/protocol/function.go) — FunctionInfo 协议定义
