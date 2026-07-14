# TC-003: HITL 完整流程测试

> **测试编号**: TC-003
> **测试类型**: 端到端集成测试
> **覆盖范围**: Human-in-the-Loop (HITL) 完整流程、Checkpoint (D-083)、Resume (D-084)、agent_resume RPC (D-085)
> **环境**: Docker E2E (D-043)
> **最后更新**: 2026-07-14

---

## 1. 概述

本测试用例覆盖 Xyncra 消息系统的 Human-in-the-Loop (HITL) 完整流程：Agent 遇到需要用户确认的场景时，保存 checkpoint 并暂停执行，等待用户响应后通过 `agent_resume` RPC 恢复执行。

**测试目标**：验证 HITL 流程的完整性，包括 checkpoint 创建、用户通知、resume 机制、并发锁协调。

**覆盖的关键决策**：
- D-083: HITL CheckpointStore 失败策略（非 fail-open）
- D-084: HITL Resume 与并发锁协调（锁不释放）
- D-085: agent_resume RPC 规范
- D-087: Agent Ephemeral Update 类型扩展（agent_question, agent_checkpoint_created）
- D-092: ReverseRPC 双向请求能力（用于获取用户响应）

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
│  └──────────────┘         └──────────────────────┘         │
│         ▲                        ▲                         │
│         │ 16379                  │ 18080                   │
└─────────┼────────────────────────┼─────────────────────────┘
          │                        │
┌─────────┼────────────────────────┼─────────────────────────┐
│         ▼                        ▼                         │
│  ┌─────────────────┐    ┌─────────────────┐               │
│  │ xyncra-client   │    │ HITL Agent      │               │
│  │ User: alice     │    │ (需要用户确认)  │               │
│  │ Daemon (IPC)    │    │                 │               │
│  └─────────────────┘    └─────────────────┘               │
│                                                             │
│  工作目录: $E2E_HOME (mktemp -d)                            │
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
```

### 3.5 配置 HITL Agent

创建或修改 `agents/hitl-bot.md`：

```markdown
---
id: hitl-bot
name: HITL 测试助手
description: 需要用户确认的测试 Agent
model: qwen3.7-plus
api_key_env: XYNCRA_TEST_REAL_API_KEY
base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
parameters:
  temperature: 0.3
  max_tokens: 500
context:
  max_tokens: 4000
  max_messages: 10
middleware:
  enable_client_tools: false
---

你是一个需要用户确认的助手。当用户询问敏感操作时，你应该：
1. 解释操作的影响
2. 询问用户是否确认
3. 等待用户回复"确认"或"取消"

示例场景：
- 用户: "删除所有数据"
- 你: "这个操作不可逆，会影响 100 条记录。请确认是否继续？(回复'确认'或'取消')"
```

---

## 4. 测试数据字典

| 变量 | 值 | 说明 |
|------|-----|------|
| `$SERVER_URL` | `ws://localhost:18080/ws` | E2E 服务器 WebSocket 地址 |
| `$REDIS_ADDR` | `localhost:16379` | E2E Redis 地址 |
| `$REDIS_DB` | `15` | E2E Redis DB 编号 |
| `$ALICE` | `alice` | 测试用户 Alice |
| `$E2E_HOME` | `/tmp/xe2e-XXXXXX` | 临时测试目录 |
| `$HITL_CONV_ID` | (运行时获取) | HITL 会话 ID |
| `$CHECKPOINT_ID` | (运行时获取) | HITL Checkpoint ID |

---

## 5. 完整流程图

```mermaid
flowchart TD
    Start([开始]) --> EnvSetup[环境准备]

    subgraph EnvSetup [环境准备]
        EnvSetup --> BuildBin[构建二进制]
        BuildBin --> DockerUp[启动 Docker E2E]
        DockerUp --> HealthCheck[健康检查]
        HealthCheck --> CreateDir[创建测试目录]
        CreateDir --> ConfigAgent[配置 HITL Agent]
    end

    ConfigAgent --> Phase1

    subgraph Phase1 [阶段 1: 启动 Daemon 并创建会话]
        P1A[启动 Alice daemon] --> P1B[创建与 hitl-bot 的会话]
        P1B --> P1C[记录 HITL_CONV_ID]
    end

    P1C --> Phase2

    subgraph Phase2 [阶段 2: 触发 HITL 中断]
        P2A[Alice 发送敏感操作请求\n"删除所有数据"] --> P2B[Agent 处理并触发 HITL]
        P2B --> P2C[验证 agent_question 推送\nEphemeral Seq=0]
        P2C --> P2D[验证 agent_checkpoint_created 推送]
        P2D --> P2E[验证 Redis Checkpoint\nagent:checkpoint:*]
    end

    P2E --> Phase3

    subgraph Phase3 [阶段 3: 用户响应并 Resume]
        P3A[Alice 发送确认\n"确认"] --> P3B[客户端调用 agent_resume RPC\ncheckpoint_id + answer]
        P3B --> P3C[验证 MQ 任务入队\nTypeAgentResume]
        P3C --> P3D[验证 Agent 恢复执行]
        P3D --> P3E[验证最终消息\n"操作已确认..."]
    end

    P3E --> Phase4

    subgraph Phase4 [阶段 4: 并发锁验证]
        P4A[HITL pending 期间\n发送新消息] --> P4B[验证会话锁被持有\nD-084]
        P4B --> P4C[验证新消息排队等待]
    end

    P4C --> Phase5

    subgraph Phase5 [阶段 5: 超时处理]
        P5A[等待 checkpoint TTL 过期\n或手动清理] --> P5B[验证锁自动释放]
        P5B --> P5C[验证会话恢复正常]
    end

    P5C --> Cleanup

    subgraph Cleanup [环境清理]
        CL1[停止 daemon] --> CL2[停止 Docker]
        CL2 --> CL3[清理临时目录]
    end

    Cleanup --> End([结束])
```

---

## 6. 分步执行指南

### 阶段 1: 启动 Daemon 并创建会话

#### 步骤 1.1: 启动 Alice daemon

```bash
./bin/xyncra-client listen \
  --user-id alice \
  --server ws://localhost:18080/ws \
  > "$E2E_HOME/alice-daemon.log" 2>&1 &
ALICE_PID=$!
sleep 2
```

#### 步骤 1.2: 创建与 HITL Agent 的会话

```bash
HITL_CONV_ID=$(./bin/xyncra-client create-conversation \
  --user-id alice \
  --server ws://localhost:18080/ws \
  --peer-id "agent/hitl-bot" | grep "ID:" | awk '{print $2}')
echo "HITL_CONV_ID=$HITL_CONV_ID"
```

---

### 阶段 2: 触发 HITL 中断

#### 步骤 2.1: 发送敏感操作请求

```bash
./bin/xyncra-client send \
  --user-id alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$HITL_CONV_ID" \
  --content "删除所有数据"
```

#### 步骤 2.2: 等待 Agent 处理

```bash
sleep 10  # 等待 Agent 处理并触发 HITL
```

#### 步骤 2.3: 验证 agent_question 推送 (D-087)

```bash
# 检查 Alice daemon 日志，确认收到 ephemeral 推送
cat "$E2E_HOME/alice-daemon.log" | grep -i "agent_question\|agent_checkpoint" | tail -5
# 预期: 看到 agent_question 或 agent_checkpoint_created 事件
```

#### 步骤 2.4: 验证 Redis Checkpoint (D-083)

```bash
# 检查 Redis 中的 checkpoint
redis-cli -p 16379 -n 15 KEYS "agent:checkpoint:*"
# 预期: 包含 agent:checkpoint:$HITL_CONV_ID 或类似 key

# 获取 checkpoint 详情
CHECKPOINT_KEY=$(redis-cli -p 16379 -n 15 KEYS "agent:checkpoint:*" | head -1)
redis-cli -p 16379 -n 15 GET "$CHECKPOINT_KEY"
# 预期: JSON 包含 checkpoint_id, conversation_id, agent_state 等
```

#### 步骤 2.5: 记录 Checkpoint ID

```bash
CHECKPOINT_ID=$(redis-cli -p 16379 -n 15 GET "$CHECKPOINT_KEY" | jq -r '.id')
echo "CHECKPOINT_ID=$CHECKPOINT_ID"
```

---

### 阶段 3: 用户响应并 Resume

#### 步骤 3.1: Alice 发送确认消息

```bash
./bin/xyncra-client send \
  --user-id alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$HITL_CONV_ID" \
  --content "确认"
```

#### 步骤 3.2: 调用 agent_resume RPC (D-085)

```bash
# 通过 IPC 或 curl 调用 agent_resume
# 注意：实际实现中，客户端应在收到用户确认消息后自动调用

# 手动调用（用于测试）
curl -X POST http://localhost:18080/rpc \
  -H "Content-Type: application/json" \
  -d "{
    \"jsonrpc\": \"2.0\",
    \"method\": \"agent_resume\",
    \"params\": {
      \"conversation_id\": \"$HITL_CONV_ID\",
      \"checkpoint_id\": \"$CHECKPOINT_ID\",
      \"answer\": \"确认\"
    },
    \"id\": 1
  }"
# 预期: {"result":{"status":"queued"},"error":null}
```

#### 步骤 3.3: 验证 MQ 任务入队

```bash
# 检查服务器日志
cat "$E2E_HOME/server.log" | grep "agent_resume\|TypeAgentResume" | tail -3
# 预期: 看到 agent_resume 任务入队日志
```

#### 步骤 3.4: 等待 Agent 恢复执行

```bash
sleep 10  # 等待 Agent 恢复执行并生成响应
```

#### 步骤 3.5: 验证最终消息

```bash
./bin/xyncra-client sync-updates --user-id alice

./bin/xyncra-client get-messages \
  --user-id alice \
  --conversation-id "$HITL_CONV_ID" \
  --limit 5
# 预期: 包含 Agent 的确认消息，如 "操作已确认，正在执行..."
```

---

### 阶段 4: 并发锁验证 (D-084)

#### 步骤 4.1: 触发新的 HITL 流程

```bash
./bin/xyncra-client send \
  --user-id alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$HITL_CONV_ID" \
  --content "再次删除所有数据"

sleep 5  # 等待 Agent 处理并触发 HITL
```

#### 步骤 4.2: 验证会话锁被持有

```bash
# 检查 Redis 中的锁
redis-cli -p 16379 -n 15 KEYS "agent:lock:*"
# 预期: 包含 agent:lock:$HITL_CONV_ID

# 获取锁信息
LOCK_KEY="agent:lock:$HITL_CONV_ID"
redis-cli -p 16379 -n 15 GET "$LOCK_KEY"
# 预期: 锁的值（unique token）
```

#### 步骤 4.3: HITL pending 期间发送新消息

```bash
./bin/xyncra-client send \
  --user-id alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$HITL_CONV_ID" \
  --content "这是一条新消息"

# 检查服务器日志，确认新消息的 Agent 处理被跳过或排队
cat "$E2E_HOME/server.log" | grep "lock.*held\|skip.*agent" | tail -3
# 预期: 看到锁被持有、跳过 Agent 处理的日志
```

---

### 阶段 5: 超时处理

#### 步骤 5.1: 手动清理 checkpoint（模拟超时）

```bash
# 删除 checkpoint
CHECKPOINT_KEY=$(redis-cli -p 16379 -n 15 KEYS "agent:checkpoint:*" | head -1)
redis-cli -p 16379 -n 15 DEL "$CHECKPOINT_KEY"

# 删除锁
LOCK_KEY="agent:lock:$HITL_CONV_ID"
redis-cli -p 16379 -n 15 DEL "$LOCK_KEY"
```

#### 步骤 5.2: 验证会话恢复正常

```bash
./bin/xyncra-client send \
  --user-id alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$HITL_CONV_ID" \
  --content "现在可以正常处理了吗？"

sleep 10

./bin/xyncra-client sync-updates --user-id alice

./bin/xyncra-client get-messages \
  --user-id alice \
  --conversation-id "$HITL_CONV_ID" \
  --limit 3
# 预期: Agent 正常响应
```

---

## 7. 数据库验证汇总

### 7.1 Redis 验证命令速查

```bash
R="redis-cli -p 16379 -n 15"

# Checkpoint
$R KEYS "agent:checkpoint:*"
$R GET "agent:checkpoint:<conversation-id>"

# 会话锁
$R KEYS "agent:lock:*"
$R GET "agent:lock:<conversation-id>"

# 幂等性
$R KEYS "agent:idempotent:*"

# 清理
$R FLUSHDB
```

---

## 8. 通过/失败判定标准

| 阶段 | 判定条件 |
|------|---------|
| 阶段 1 | daemon 正常启动，HITL 会话创建成功 |
| 阶段 2 | Agent 触发 HITL，checkpoint 保存到 Redis，ephemeral 推送发送 |
| 阶段 3 | agent_resume RPC 成功，Agent 恢复执行，最终消息正确 |
| 阶段 4 | HITL pending 期间会话锁被持有，新消息处理被跳过 |
| 阶段 5 | checkpoint/锁清理后会话恢复正常 |

---

## 9. 故障排查指南

| 症状 | 可能原因 | 解决方法 |
|------|---------|---------|
| Checkpoint 未创建 | Agent 未配置 HITL 或 CheckpointStore 失败 | 检查 Agent 配置和服务器日志 |
| agent_resume 失败 | checkpoint_id 不匹配或已过期 | 检查 Redis 中的 checkpoint |
| 会话锁未释放 | TTL 未到期或手动清理不彻底 | 手动删除 Redis 锁 key |

---

## 10. 环境清理

```bash
./bin/xyncra-client kill --user-id alice

docker compose -f docker-compose.e2e.yml down

rm -rf "$E2E_HOME"
rm -rf ~/.xyncra/alice

redis-cli -p 16379 -n 15 FLUSHDB
```

---

## 11. 依赖关系说明

| 测试阶段 | 可独立执行 | 依赖 |
|---------|-----------|------|
| 阶段 1 (Daemon 启动) | ✅ | 环境准备 |
| 阶段 2 (HITL 触发) | ✅ | 阶段 1 |
| 阶段 3 (Resume) | ✅ | 阶段 2 |
| 阶段 4 (并发锁) | ✅ | 阶段 2 |
| 阶段 5 (超时处理) | ✅ | 阶段 4 |

阶段 3 和阶段 4 可并行执行（使用不同的 HITL 实例）。

---

## 12. 测试执行记录模板

```markdown
### TC-003 测试执行记录

| 字段 | 值 |
|------|-----|
| 日期 | YYYY-MM-DD |
| Git Commit | <sha> |
| 测试者 | <name> |
| 环境 | Docker E2E |

| 阶段 | 结果 | 备注 |
|------|------|------|
| 阶段 1: Daemon 启动 | ✅ / ❌ | |
| 阶段 2: HITL 触发 | ✅ / ❌ | D-083, D-087 |
| 阶段 3: Resume | ✅ / ❌ | D-085 |
| 阶段 4: 并发锁 | ✅ / ❌ | D-084 |
| 阶段 5: 超时处理 | ✅ / ❌ | |

**发现的问题**：
1. (描述)

**结论**：PASS / FAIL
```
