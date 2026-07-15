# TC-004: Agent 上下文管理测试

> **测试编号**: TC-004
> **测试类型**: 端到端集成测试（人工评审）
> **覆盖范围**: Agent 上下文加载、Token/消息裁剪、缓存机制、LLM 日志验证
> **环境**: Docker E2E (D-043) + 真实 LLM（.env.test）
> **最后更新**: 2026-07-15

---

## 1. 概述

本测试用例验证 Agent 的上下文管理机制：**如何从数据库加载消息历史、如何裁剪上下文以适应 token 限制、缓存是否有效工作**。

**测试目标**：
- 验证 Agent 正确加载对话历史
- 验证 token/消息裁剪按配置工作
- 验证 LLM 日志记录完整的上下文快照
- 验证缓存减少数据库查询
- 验证长对话正确截断

**关键特点**：
- ⚠️ **非确定性测试**：LLM 输出不固定，需要人工评审 LLM 日志
- ✅ **可验证标准**：通过检查 `request` 阶段的 `messages` 字段验证上下文正确性
- 📊 **依赖 LLM 日志**：所有验证基于 LLMLogger 输出的 JSONL 记录

**覆盖的关键决策**：
- D-060: DB-backed context with in-memory cache (30s TTL)
- D-001: HeuristicTokenCounter (len/4)
- Token-based trimming with message-count fallback

---

## 2. 环境拓扑

```
┌─────────────────────────────────────────────────────────────┐
│                     Docker E2E 网络                          │
│                                                             │
│  ┌──────────────┐         ┌──────────────────────┐         │
│  │  Redis 7     │◄────────│  xyncra-server       │         │
│  │  16379→6379  │         │  18080→8080           │         │
│  │  (DB 15)     │         │  SQLite: xyncra-e2e.db│        │
│  └──────────────┘         │  LLM Logger: JSONL    │         │
│         ▲                 └──────────────────────┘         │
│         │ 16379                        ▲                   │
└─────────┼──────────────────────────────┼───────────────────┘
          │                              │
┌─────────┼──────────────────────────────┼───────────────────┐
│         ▼                              │                   │
│  ┌─────────────────┐                   │                   │
│  │ xyncra-client   │                   │                   │
│  │ User: alice     │                   │                   │
│  │ Daemon (IPC)    │                   │                   │
│  └─────────────────┘                   │                   │
│                                         │                   │
│  ┌─────────────────────────────────────┴────────┐          │
│  │ Context-Test Agent (真实 LLM)                │          │
│  │ - LLM Logger 输出到 JSONL 文件               │          │
│  │ - 配置 MaxTokens 或 MaxMessages              │          │
│  └──────────────────────────────────────────────┘          │
│                                                             │
│  工作目录: $E2E_HOME (mktemp -d)                            │
│  LLM 日志文件: ./llm-logs-e2e/llm-calls.log (通过 volume 挂载)                   │
└─────────────────────────────────────────────────────────────┘
```

---

## 3. 前置条件

### 3.1 构建二进制

```bash
cd /path/to/xyncra-server
make build
```

### 3.2 启动 Docker E2E 环境

```bash
docker compose -f docker-compose.e2e.yml build --no-cache && \
docker compose -f docker-compose.e2e.yml up -d
```

### 3.3 健康检查

```bash
redis-cli -p 16379 ping
# 预期: PONG

curl -s http://localhost:18080/health
# 预期: {"status":"ok"}
```

### 3.4 创建测试工作目录

```bash
export E2E_HOME=$(mktemp -d /tmp/xe2e-XXXXXX)
echo "E2E_HOME=$E2E_HOME"
mkdir -p $E2E_HOME/agent-logs
```

### 3.5 配置真实 LLM（.env.test）

确保 `.env.test` 已配置（参考 `.env.test.example`）：

```bash
# 验证 .env.test 存在
test -f .env.test && echo "✓ .env.test exists" || echo "✗ .env.test missing"
```

### 3.6 配置上下文测试 Agent

创建 `agents/context-test-bot.md`：

```markdown
---
id: context-test-bot
name: 上下文测试助手
description: 用于测试上下文管理的 Agent
model: qwen3.7-plus
api_key_env: XYNCRA_TEST_REAL_API_KEY
base_url: https://coding.dashscope.aliyuncs.com/v1
parameters:
  temperature: 0.7
  max_tokens: 2000
context:
  max_tokens: 4000      # 测试场景 A
  max_messages: 0
---

你是一个测试助手。请根据对话历史回答用户问题。

如果用户问你"你看到了多少条消息？"，请统计对话历史中用户消息的数量并回答。
如果用户问你"最早的对话是什么？"，请回顾对话历史并回答最早的消息内容。
```

**说明**：
- `max_tokens: 4000`：启用 token 裁剪
- 可以修改为 `max_messages: 10` 测试消息数裁剪
- LLM 日志会自动写入容器内 /app/llm-logs/llm-calls.log 文件

> ⚠️ **重要**：Docker 容器内的 `agents/` 目录是在镜像构建时通过 `COPY` 指令导入的，**并非运行时挂载**。
> 创建新 Agent 配置后，必须使用 `docker cp` 将文件拷贝进容器，然后重启服务器：
>
> ```bash
> docker cp agents/context-test-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/context-test-bot.md
> docker compose -f docker-compose.e2e.yml restart xyncra-server-e2e
> sleep 5
> curl -s http://localhost:18080/health
> # 预期: {"status":"ok"}
> ```

---

## 4. 测试数据字典

| 变量 | 值 | 说明 |
|------|-----|------|
| `$SERVER_URL` | `ws://localhost:18080/ws` | E2E 服务器地址 |
| `$REDIS_ADDR` | `localhost:16379` | E2E Redis |
| `$E2E_HOME` | `/tmp/xe2e-*` | 测试工作目录 |
| `$AGENT_ID` | `context-test-bot` | 测试 Agent ID |
| `$CONV_ID` | 运行时生成 | 测试会话 ID |
| `MaxTokens` | 4000 | Agent 配置的 token 限制 |
| `MaxMessages` | 0 | 禁用消息数裁剪 |

---

## 5. 完整流程图

```mermaid
flowchart TD
    Start([开始]) --> Prep[🟢 环境准备<br/>构建、启动 Docker<br/>配置 Agent]

    Prep --> ScenarioA[🔵 场景 A: Token 裁剪测试]
    ScenarioA --> A1[创建会话<br/>alice ↔ context-test-bot]
    A1 --> A2[发送 20 条长消息<br/>每条约 300 tokens]
    A2 --> A3[发送触发消息<br/>"你看到了多少条消息？"]
    A3 --> A4[🟡 收集 LLM 日志<br/>从 volume 读取 JSONL 文件]
    A4 --> A5[🔴 人工评审<br/>检查 request 阶段的 messages]
    A5 --> A6{验证通过？}
    A6 -->|✅| ScenarioB
    A6 -->|❌| FixA[调整配置<br/>或修复问题]
    FixA --> A2

    ScenarioB --> ScenarioB[🔵 场景 B: 消息数裁剪测试]
    ScenarioB --> B1[修改 Agent 配置<br/>max_messages: 10]
    B1 --> B2[重启服务器]
    B2 --> B3[创建新会话<br/>发送 30 条短消息]
    B3 --> B4[发送触发消息]
    B4 --> B5[🟡 收集 LLM 日志]
    B5 --> B6[🔴 人工评审<br/>验证只包含最新 10 条]
    B6 --> B7{验证通过？}
    B7 -->|✅| ScenarioC
    B7 -->|❌| FixB[调整配置]
    FixB --> B3

    ScenarioC --> ScenarioC[🔵 场景 C: 缓存有效性测试]
    ScenarioC --> C1[在同一会话<br/>连续发送 3 条消息]
    C1 --> C2[🟡 检查 LLM 日志<br/>统计 request 数量]
    C2 --> C3[🔴 人工评审<br/>验证缓存命中<br/>DB 查询减少]
    C3 --> C4{验证通过？}
    C4 -->|✅| ScenarioD
    C4 -->|❌| FixC[检查缓存逻辑]
    FixC --> C1

    ScenarioD --> ScenarioD[🔵 场景 D: 空会话测试]
    ScenarioD --> D1[创建新会话<br/>不发送历史消息]
    D1 --> D2[直接发送触发消息]
    D2 --> D3[🟡 收集 LLM 日志]
    D3 --> D4[🔴 人工评审<br/>验证只有系统消息和用户消息]
    D4 --> D5{验证通过？}
    D5 -->|✅| Summary
    D5 -->|❌| FixD[检查空会话处理]
    FixD --> D1

    Summary[📊 测试总结] --> Cleanup[⚪ 环境清理]
    Cleanup --> End([结束])
```

---

## 6. 分步执行指南

### 场景 A: Token 裁剪测试

**目标**：验证长对话在超过 `MaxTokens` 时正确裁剪

#### 步骤 A1: 创建测试会话

```bash
# 启动 alice 客户端
bin/xyncra-client --user alice --home $E2E_HOME/alice daemon

# 创建会话
CONV_INFO=$(bin/xyncra-client --user alice --home $E2E_HOME/alice rpc create_conversation \
  --params '{"user_id2":"agent/context-test-bot","type":"1-on-1","title":"Context Test"}')
echo "$CONV_INFO"

# 提取会话 ID
CONV_ID=$(echo "$CONV_INFO" | python3 -c "import sys, json; print(json.load(sys.stdin)['id'])")
echo "CONV_ID=$CONV_ID"
```

**预期输出**：
```
CONV_ID=conv-xxxxx-xxxxx
```

**验证**：
```bash
# 数据库验证
docker exec xyncra-server-xyncra-server-e2e-1 sqlite3 /app/xyncra-e2e.db \
  "SELECT id, user_id1, user_id2 FROM conversations WHERE id='$CONV_ID';"
# 预期: conv-xxxxx|alice|agent/context-test-bot
```

#### 步骤 A2: 发送 20 条长消息

```bash
# 生成 20 条长消息（每条约 300 tokens = 1200 字符）
for i in {1..20}; do
  MSG_CONTENT="这是第 $i 条测试消息。$(python3 -c "print('测试内容' * 150)")"
  bin/xyncra-client --user alice --home $E2E_HOME/alice rpc send_message \
    --params "{\"conversation_id\":\"$CONV_ID\",\"content\":\"$MSG_CONTENT\",\"type\":\"text\"}"
  sleep 0.5
done
```

**预期**：20 条消息成功发送

**验证**：
```bash
# 数据库验证消息数
docker exec xyncra-server-xyncra-server-e2e-1 sqlite3 /app/xyncra-e2e.db \
  "SELECT COUNT(*) FROM messages WHERE conversation_id='$CONV_ID' AND sender_id='alice';"
# 预期: 20

# 计算总 token 数（启发式：字符数/4）
docker exec xyncra-server-xyncra-server-e2e-1 sqlite3 /app/xyncra-e2e.db \
  "SELECT SUM(LENGTH(content))/4 FROM messages WHERE conversation_id='$CONV_ID' AND sender_id='alice';"
# 预期: 约 6000 tokens（超过 MaxTokens=4000）
```

#### 步骤 A3: 发送触发消息

```bash
bin/xyncra-client --user alice --home $E2E_HOME/alice rpc send_message \
  --params "{\"conversation_id\":\"$CONV_ID\",\"content\":\"你看到了多少条消息？请统计一下。\",\"type\":\"text\"}"
```

**预期**：Agent 收到消息并开始处理

#### 步骤 A4: 收集 LLM 日志

```bash
# 等待 Agent 完成处理（约 10-30 秒）
sleep 15

# LLM 日志以 JSONL 格式写入容器内 /app/llm-logs/llm-calls.log
# 通过 docker-compose volume 挂载到宿主机 ./llm-logs-e2e/ 目录
# 方法 1: 直接从宿主机 volume 目录复制（推荐）
cp llm-logs-e2e/llm-calls.log $E2E_HOME/llm-records-a.jsonl 2>/dev/null || true

# 方法 2: 使用 docker exec 从容器内读取
docker exec xyncra-server-xyncra-server-e2e-1 cat /app/llm-logs/llm-calls.log > $E2E_HOME/llm-records-a.jsonl 2>/dev/null || true

# 提取 request 阶段的记录
grep '"phase":"request"' $E2E_HOME/llm-records-a.jsonl > $E2E_HOME/llm-request-records-a.jsonl

# 查看记录数量
wc -l $E2E_HOME/llm-records-a.jsonl
# 预期: 约 10-20 条记录（agent_start, request, response, agent_end 等）
```

#### 步骤 A5: 人工评审 LLM 日志

```bash
# 查看最后一个 request 阶段的记录
cat $E2E_HOME/llm-records-a.jsonl | \
  python3 -c "
import sys, json
records = [json.loads(line) for line in sys.stdin if '\"phase\":\"request\"' in line]
if records:
    last_req = records[-1]
    print('=== 最后一次 request ===')
    print(f'Agent ID: {last_req[\"agent_id\"]}')
    print(f'Iteration: {last_req[\"iteration\"]}')
    print(f'Messages count: {len(last_req[\"messages\"])}')
    print(f'Tools count: {len(last_req.get(\"tools\", []))}')
    print()
    print('=== 消息列表（前 5 条和最后 5 条）===')
    msgs = last_req['messages']
    for i, msg in enumerate(msgs[:5]):
        print(f'{i+1}. [{msg[\"role\"]}] {msg[\"content\"][:100]}...')
    print('...')
    for i, msg in enumerate(msgs[-5:], len(msgs)-4):
        print(f'{i}. [{msg[\"role\"]}] {msg[\"content\"][:100]}...')
"
```

**评审标准**（人工判断）：

| 检查项 | 预期 | 通过标志 |
|--------|------|----------|
| Messages 数量 | 10-15 条（裁剪后） | ✅ 如果 < 20 条 |
| 最早消息 | 不是第 1 条消息 | ✅ 如果是第 5-10 条 |
| 最新消息 | 包含触发消息 | ✅ 如果最后一条是"你看到了多少条消息？" |
| 消息顺序 | 时间顺序（旧→新） | ✅ 如果 MessageID 递增 |
| Token 估算 | 总 tokens ≈ 4000 | ✅ 如果在 3500-4500 范围 |

**记录评审结果**：
```
场景 A 评审:
- Messages 数量: ___
- 最早消息编号: ___
- 最新消息内容: ___
- 消息顺序: ✅/❌
- Token 估算: ___
- 总体判定: ✅ PASS / ❌ FAIL
```

---

### 场景 B: 消息数裁剪测试

**目标**：验证 `MaxMessages` 配置正确裁剪

#### 步骤 B1: 修改 Agent 配置

```bash
# 编辑 agents/context-test-bot.md
# 修改 context 部分:
# context:
#   max_tokens: 0
#   max_messages: 10
```

#### 步骤 B2: 重启服务器

```bash
docker compose -f docker-compose.e2e.yml restart xyncra-server-e2e
sleep 5
curl -s http://localhost:18080/health
# 预期: {"status":"ok"}
```

#### 步骤 B3: 创建新会话并发送 30 条消息

```bash
# 创建新会话
CONV_INFO=$(bin/xyncra-client --user alice --home $E2E_HOME/alice rpc create_conversation \
  --params '{"user_id2":"agent/context-test-bot","type":"1-on-1","title":"Message Count Test"}')
CONV_ID_B=$(echo "$CONV_INFO" | python3 -c "import sys, json; print(json.load(sys.stdin)['id'])")

# 发送 30 条短消息
for i in {1..30}; do
  bin/xyncra-client --user alice --home $E2E_HOME/alice rpc send_message \
    --params "{\"conversation_id\":\"$CONV_ID_B\",\"content\":\"消息 $i\",\"type\":\"text\"}"
  sleep 0.3
done
```

#### 步骤 B4: 发送触发消息

```bash
bin/xyncra-client --user alice --home $E2E_HOME/alice rpc send_message \
  --params "{\"conversation_id\":\"$CONV_ID_B\",\"content\":\"你看到了多少条消息？\",\"type\":\"text\"}"
```

#### 步骤 B5-B6: 收集并评审 LLM 日志

```bash
sleep 15

# 从 volume 挂载目录读取 JSONL 日志文件
cp llm-logs-e2e/llm-calls.log $E2E_HOME/llm-records-b.jsonl 2>/dev/null || true
# 或使用 docker exec
docker exec xyncra-server-xyncra-server-e2e-1 cat /app/llm-logs/llm-calls.log > $E2E_HOME/llm-records-b.jsonl 2>/dev/null || true

# 提取 request 阶段记录用于评审
grep '"phase":"request"' $E2E_HOME/llm-records-b.jsonl > $E2E_HOME/llm-request-records-b.jsonl

# 评审
cat $E2E_HOME/llm-request-records-b.jsonl | \
  python3 -c "
import sys, json
records = [json.loads(line) for line in sys.stdin if '\"phase\":\"request\"' in line]
if records:
    last_req = records[-1]
    msgs = [m for m in last_req['messages'] if m['role'] == 'user']
    print(f'用户消息数量: {len(msgs)}')
    print(f'预期: 10 条（最新 10 条）')
    print(f'通过: {\"✅\" if len(msgs) == 10 else \"❌\"}')
"
```

**评审标准**：
- 用户消息数量 = 10（最新 10 条）
- 最早消息 = "消息 21"（不是"消息 1"）
- 最新消息 = 触发消息

---

### 场景 C: 缓存有效性测试

**目标**：验证缓存减少数据库查询

#### 步骤 C1: 在同一会话连续发送消息

```bash
# 使用场景 A 的会话
for i in {1..3}; do
  bin/xyncra-client --user alice --home $E2E_HOME/alice rpc send_message \
    --params "{\"conversation_id\":\"$CONV_ID\",\"content\":\"缓存测试消息 $i\",\"type\":\"text\"}"
  sleep 2  # 间隔 2 秒（小于缓存 TTL 30s）
done
```

#### 步骤 C2-C3: 检查 LLM 日志

```bash
sleep 10

# 从 volume 挂载目录读取 JSONL 日志文件
cp llm-logs-e2e/llm-calls.log $E2E_HOME/llm-records-c.jsonl 2>/dev/null || true
# 或使用 docker exec
docker exec xyncra-server-xyncra-server-e2e-1 cat /app/llm-logs/llm-calls.log > $E2E_HOME/llm-records-c.jsonl 2>/dev/null || true

# 提取 request 阶段记录
grep '"phase":"request"' $E2E_HOME/llm-records-c.jsonl > $E2E_HOME/llm-request-records-c.jsonl

# 分析缓存行为
cat $E2E_HOME/llm-request-records-c.jsonl | \
  python3 -c "
import sys, json
from collections import Counter

records = [json.loads(line) for line in sys.stdin if '\"phase\":\"request\"' in line]
iterations = [r['iteration'] for r in records]
print(f'Request 数量: {len(records)}')
print(f'Iterations: {iterations}')
print()
print('缓存分析:')
print(f'- 如果 iteration 从 1 开始递增，说明每次都是新请求（缓存未命中）')
print(f'- 如果 iteration 保持不变，说明复用了之前的上下文（缓存命中）')
"
```

**评审标准**：

- 3 条消息快速发送（间隔 2s），由于 Conversation Lock 机制：
  - 第 1 条消息先获取锁并执行 Agent
  - 第 2、3 条消息发现锁已被持有，排队等待（日志中有 "conversation lock: already held, requeueing" 记录）
  - 第 1 条完成后，第 2 条重试成功，但由于幂等性检查发现已被处理，跳过执行
  - 第 3 条同理
- 最终只有 1 次实际的 Agent 执行（1 个 `request` 记录）
- 3 条消息均正确落库（通过数据库验证）

**注意**：缓存是内存级别的，无法直接从数据库验证。需要通过 LLM 日志的 `messages` 内容判断。

---

### 场景 D: 空会话测试

**目标**：验证空会话正确处理

#### 步骤 D1-D2: 创建空会话并直接触发

```bash
# 创建新会话
CONV_INFO=$(bin/xyncra-client --user alice --home $E2E_HOME/alice rpc create_conversation \
  --params '{"user_id2":"agent/context-test-bot","type":"1-on-1","title":"Empty Test"}')
CONV_ID_D=$(echo "$CONV_INFO" | python3 -c "import sys, json; print(json.load(sys.stdin)['id'])")

# 直接发送消息（无历史）
bin/xyncra-client --user alice --home $E2E_HOME/alice rpc send_message \
  --params "{\"conversation_id\":\"$CONV_ID_D\",\"content\":\"这是第一条消息\",\"type\":\"text\"}"
```

#### 步骤 D3-D4: 收集并评审

```bash
sleep 15

# 从 volume 挂载目录读取 JSONL 日志文件
cp llm-logs-e2e/llm-calls.log $E2E_HOME/llm-records-d.jsonl 2>/dev/null || true
# 或使用 docker exec
docker exec xyncra-server-xyncra-server-e2e-1 cat /app/llm-logs/llm-calls.log > $E2E_HOME/llm-records-d.jsonl 2>/dev/null || true

# 提取 request 阶段记录
grep '"phase":"request"' $E2E_HOME/llm-records-d.jsonl > $E2E_HOME/llm-request-records-d.jsonl

# 评审
cat $E2E_HOME/llm-request-records-d.jsonl | \
  python3 -c "
import sys, json
records = [json.loads(line) for line in sys.stdin if '\"phase\":\"request\"' in line]
if records:
    last_req = records[-1]
    msgs = last_req['messages']
    user_msgs = [m for m in msgs if m['role'] == 'user']
    print(f'总消息数: {len(msgs)}')
    print(f'用户消息数: {len(user_msgs)}')
    print(f'预期: 1 条用户消息（\"这是第一条消息\"）')
    print(f'通过: {\"✅\" if len(user_msgs) == 1 else \"❌\"}')
"
```

**评审标准**：
- 只有 1 条用户消息
- 没有历史消息
- Agent 正常响应

---

## 7. 数据库验证汇总

### Server DB 验证命令速查

```bash
# 查看会话
docker exec xyncra-server-xyncra-server-e2e-1 sqlite3 /app/xyncra-e2e.db \
  "SELECT id, user_id1, user_id2, title FROM conversations WHERE id='$CONV_ID';"

# 统计消息数
docker exec xyncra-server-xyncra-server-e2e-1 sqlite3 /app/xyncra-e2e.db \
  "SELECT COUNT(*), sender_id FROM messages WHERE conversation_id='$CONV_ID' GROUP BY sender_id;"

# 估算总 token 数
docker exec xyncra-server-xyncra-server-e2e-1 sqlite3 /app/xyncra-e2e.db \
  "SELECT SUM(LENGTH(content))/4 as estimated_tokens FROM messages WHERE conversation_id='$CONV_ID';"

# 查看最新消息
docker exec xyncra-server-xyncra-server-e2e-1 sqlite3 /app/xyncra-e2e.db \
  "SELECT message_id, sender_id, SUBSTR(content, 1, 50) FROM messages WHERE conversation_id='$CONV_ID' ORDER BY message_id DESC LIMIT 5;"
```

### Server Redis 验证命令速查

```bash
# 检查 Agent 锁（应该已释放）
redis-cli -p 16379 -n 15 KEYS "agent:lock:*"

# 检查幂等性 key
redis-cli -p 16379 -n 15 KEYS "agent:idempotent:*"

# 检查 checkpoint（HITL 场景）
redis-cli -p 16379 -n 15 KEYS "agent:checkpoint:*"
```

### Client DB 验证命令速查

```bash
# 查看客户端本地消息
sqlite3 $E2E_HOME/alice/*/xyncra.db \
  "SELECT COUNT(*) FROM messages WHERE conversation_id='$CONV_ID';"
```

---

## 8. 通过/失败判定标准

### 场景 A: Token 裁剪

| 判定条件 | 通过标志 | 失败处理 |
|---------|----------|----------|
| LLM 日志包含 `request` 记录 | ✅ | 检查 Agent 配置和 LLM Logger |
| `messages` 数组长度 < 20 | ✅ | 检查 MaxTokens 配置 |
| 最早消息不是第 1 条 | ✅ | 检查 token 裁剪逻辑 |
| 最新消息是触发消息 | ✅ | 检查消息加载顺序 |
| 消息按时间顺序排列 | ✅ | 检查 `reverseMessages` 逻辑 |

### 场景 B: 消息数裁剪

| 判定条件 | 通过标志 | 失败处理 |
|---------|----------|----------|
| 用户消息数量 = 10 | ✅ | 检查 MaxMessages 配置 |
| 最早消息是"消息 21" | ✅ | 检查 `trimByMessages` 逻辑 |
| 消息按时间顺序排列 | ✅ | 检查消息排序 |

### 场景 C: 缓存有效性（排队 + 去重）

| 判定条件 | 通过标志 | 失败处理 |
|---------|----------|----------|
| 3 条消息均成功落库 | ✅ | 检查消息发送链路 |
| 日志包含 "conversation lock: already held, requeueing" | ✅ | 检查 Conversation Lock 机制 |
| 只有 1 次实际 Agent 执行（1 个 `request` 记录） | ✅ | 检查幂等性和去重逻辑 |
| 无错误或异常 | ✅ | 检查服务器日志 |

### 场景 D: 空会话

| 判定条件 | 通过标志 | 失败处理 |
|---------|----------|----------|
| 只有 1 条用户消息 | ✅ | 检查空会话处理 |
| Agent 正常响应 | ✅ | 检查 LLM 调用 |
| 无错误日志 | ✅ | 检查服务器日志 |

---

## 9. 故障排查指南

| 症状 | 可能原因 | 解决方法 |
|------|---------|---------|
| LLM 日志为空 | Agent 未触发或 Logger 未配置 | 检查 Agent 配置、查看服务器日志 |
| Messages 数量异常 | Token/消息裁剪配置错误 | 检查 `agents/*.md` 的 `context` 部分 |
| 消息顺序混乱 | `reverseMessages` 逻辑错误 | 检查 `db_context_manager.go` |
| 缓存未生效 | TTL 配置过短或缓存 key 错误 | 检查 `WithCacheTTL` 配置 |
| Agent 响应超时 | LLM API 限流或网络问题 | 检查 `.env.test` 配置、查看服务器日志 |
| 数据库查询失败 | SQLite 连接问题 | 检查 Docker 容器状态、数据库文件权限 |

---

## 10. 环境清理

```bash
# 停止客户端
bin/xyncra-client --user alice --home $E2E_HOME/alice kill

# 停止 Docker 环境
docker compose -f docker-compose.e2e.yml down

# 清理临时目录
rm -rf $E2E_HOME

# 清理测试 Agent 配置（可选）
rm -f agents/context-test-bot.md

# 清理 Redis（可选）
redis-cli -p 16379 -n 15 FLUSHDB
```

---

## 11. 真实 LLM 测试配置（.env.test）

本测试需要真实 LLM 调用，依赖 `.env.test` 配置：

```bash
# .env.test 示例
XYNCRA_TEST_REAL_API_KEY=your_api_key_here
XYNCRA_TEST_REAL_MODEL=qwen3.7-plus
XYNCRA_TEST_REAL_BASE_URL=https://coding.dashscope.aliyuncs.com/v1
```

**安全提示**：
- ❌ 不要提交 `.env.test` 到 git
- ✅ 使用 `.env.test.example` 作为模板
- ✅ 定期轮换 API Key

**成本控制**（D-090）：
- 每个场景约消耗 10k-50k tokens
- 完整测试约消耗 100k-200k tokens
- 建议在低峰期执行（避免限流）

---

## 12. 依赖关系说明

| 测试阶段 | 可独立执行 | 依赖 |
|---------|-----------|------|
| 场景 A: Token 裁剪 | ✅ | 无 |
| 场景 B: 消息数裁剪 | ✅ | 需要重启服务器 |
| 场景 C: 缓存有效性 | ✅ | 可复用场景 A 的会话 |
| 场景 D: 空会话 | ✅ | 无 |

**执行顺序建议**：A → D → C → B（从简单到复杂）

---

## 13. 测试执行记录模板

```markdown
# TC-004 测试执行记录

**日期**: 2026-07-15
**Git Commit**: abc123
**测试者**: [姓名]
**环境**: Docker E2E + 真实 LLM (qwen3.7-plus)

## 场景 A: Token 裁剪
- [ ] 环境准备完成
- [ ] 20 条长消息发送成功
- [ ] LLM 日志收集成功
- [ ] 人工评审通过
- Messages 数量: ___
- 最早消息编号: ___
- 总体判定: ✅ PASS / ❌ FAIL

## 场景 B: 消息数裁剪
- [ ] Agent 配置修改完成
- [ ] 服务器重启成功
- [ ] 30 条短消息发送成功
- [ ] LLM 日志收集成功
- [ ] 人工评审通过
- 用户消息数量: ___
- 最早消息内容: ___
- 总体判定: ✅ PASS / ❌ FAIL

## 场景 C: 缓存有效性
- [ ] 连续 3 次消息发送成功
- [ ] LLM 日志收集成功
- [ ] 缓存行为分析完成
- Request 数量: ___
- Iterations: ___
- 总体判定: ✅ PASS / ❌ FAIL

## 场景 D: 空会话
- [ ] 空会话创建成功
- [ ] Agent 正常响应
- [ ] LLM 日志收集成功
- [ ] 人工评审通过
- 用户消息数量: ___
- 总体判定: ✅ PASS / ❌ FAIL

## 发现的问题
1. [问题描述]
2. [问题描述]

## 最终结论
- [ ] 全部通过
- [ ] 部分通过（见上方）
- [ ] 测试失败

## 备注
[任何其他观察或建议]
```

---

## 14. 关键评审要点总结

由于 LLM 输出不确定，本测试的核心是 **通过 LLM 日志验证上下文管理行为**：

### 必须验证的定性标准

1. **上下文加载正确性**
   - 消息从数据库正确加载
   - 消息按时间顺序排列（旧→新）
   - 最新消息包含触发消息

2. **裁剪行为正确性**
   - Token 裁剪：总 tokens 接近但不超过 MaxTokens
   - 消息数裁剪：用户消息数量 = MaxMessages
   - 裁剪保留最新消息，丢弃最旧消息

3. **缓存行为正确性**
   - 同一会话的连续请求复用上下文
   - 缓存 TTL 内不重复查询数据库

4. **边界情况处理**
   - 空会话正确响应
   - 单条消息正确响应
   - 超长消息正确截断

### 人工评审的检查清单

对于每个场景，评审者应该检查：

- [ ] LLM 日志包含完整的 `request` → `response` 记录
- [ ] `request.messages` 数组符合预期
- [ ] 消息顺序正确（按 MessageID 递增）
- [ ] 裁剪逻辑正确（检查最早/最新消息）
- [ ] Token 估算合理（在预期范围内）
- [ ] 无错误或异常日志
- [ ] Agent 正常响应（即使内容不确定）

### 判定原则

- ✅ **PASS**：上下文管理行为符合预期，LLM 日志显示正确的消息加载和裁剪
- ❌ **FAIL**：上下文管理行为异常，LLM 日志显示消息加载错误、裁剪失效或顺序混乱
- ⚠️ **INCONCLUSIVE**：LLM 日志不完整或无法判断，需要重新测试

---

## 15. 参考文档

- [PRODUCT_DECISIONS.md](../../../docs/PRODUCT_DECISIONS.md) — D-060, D-001
- [TC-000-完整链路测试.md](../../../docs/manual-test-cases/TC-000-完整链路测试.md) — Agent 基础交互
- [internal/agent/db_context_manager.go](../../../internal/agent/db_context_manager.go) — 上下文管理器实现
- [internal/agent/llm_logger.go](../../../internal/agent/llm_logger.go) — LLM 日志记录器
