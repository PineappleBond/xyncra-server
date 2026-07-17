# TC-006: Agent 错误消息持久化测试

> **测试编号**: TC-006
> **测试类型**: 端到端集成测试
> **覆盖范围**: Agent 错误分类 (D-067)、用户友好错误消息 (D-082)、错误消息持久化、部分响应持久化、HITL 中断不产生错误消息
> **环境**: Docker E2E (D-043)
> **最后更新**: 2026-07-15

---

## 1. 概述

本测试用例验证 Agent 执行失败时，系统能否将错误映射为**用户友好的中文消息**并持久化到会话中，让用户通过 `get-messages` 看到明确的失败提示，而不是静默失败或看到原始错误堆栈。

**测试目标**：
- 验证每种错误类型映射到正确的用户友好消息
- 验证错误消息作为 Agent 发送的消息持久化到数据库
- 验证用户通过客户端命令能看到错误消息
- 验证 HITL 中断**不产生**错误消息（受控暂停，非错误）
- 验证错误后会话恢复正常（后续消息可正常处理）

**覆盖的关键决策**：
- D-067: Agent 错误消息持久化 — 所有 Agent 执行错误都应以用户友好消息落库
- D-082: 错误分类与映射 — 不同错误类型对应不同的中文提示
- D-052: 流式中断时部分文本持久化
- HITL 中断（ErrHITLInterrupted）不是错误 — 不发送错误消息

**错误分类映射表**（来自 `executor.go`）：

| 错误类型 | 触发条件 | 用户看到的消息 |
|---------|---------|--------------|
| 配置错误 | API Key 环境变量未设置 / 不支持的模型 | "抱歉，我的配置有误，请联系管理员检查设置。" |
| LLM 超时 / 限流 | LLM 请求超时 / 429 / 5xx | "抱歉，我暂时无法回复，请稍后重试。" |
| 上下文加载失败 | 数据库查询历史消息失败 | "抱歉，我无法读取对话历史，请重新发送消息。" |
| Checkpoint 失败 | HITL checkpoint 存储失败 | "抱歉，等待时间过长，请重新发送消息。" |
| MCP 不可达 | MCP 外部服务连接失败 | "抱歉，外部工具服务不可用，请稍后重试。" |
| 通用错误 | 其他未分类错误 | "抱歉，处理遇到问题，请稍后重试。" |

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
│  ┌─────────────────┐                                     │
│  │ xyncra-client   │                                     │
│  │ User: alice     │                                     │
│  │ Daemon (IPC)    │                                     │
│  └─────────────────┘                                     │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ 错误测试 Agent 配置（每次测试动态修改）               │   │
│  │ - Phase 1: api_key_env 指向不存在的环境变量           │   │
│  │ - Phase 2: base_url 指向不可达地址                   │   │
│  │ - Phase 3: 无效 API Key（格式错误的 key）            │   │
│  │ - Phase 4: HITL Agent（验证无错误消息）               │   │
│  └─────────────────────────────────────────────────────┘   │
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
# 预期输出: PONG

curl -s http://localhost:18080/health
# 预期输出: {"status":"ok"}
```

### 3.4 创建测试工作目录

```bash
export E2E_HOME=$(mktemp -d /tmp/xe2e-XXXXXX)
echo "E2E_HOME=$E2E_HOME"
```

### 3.5 备份原始 Agent 配置

```bash
# 备份所有 agent 配置文件，测试结束后恢复
cp -r agents/ "$E2E_HOME/agents-backup/"
```

### 3.6 ⚠️ Agent 配置文件同步注意事项

> **重要**：Docker 容器内的 `agents/` 目录是在镜像构建时通过 `COPY` 指令导入的，**并非运行时挂载**。
> 因此，在测试过程中修改宿主机 `agents/` 目录下的文件后，**必须使用 `docker cp` 将文件拷贝进容器**，然后再重启服务器：
>
> ```bash
> # 修改宿主机文件后
> docker cp agents/error-test-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/error-test-bot.md
> docker compose -f deploy/docker-compose.e2e.yml restart xyncra-server-e2e
> ```
>
> 仅执行 `docker compose -f deploy/docker-compose.yml restart` **不会**加载宿主机上的新配置。

---

## 4. 测试数据字典

| 变量 | 值 | 说明 |
|------|-----|------|
| `$SERVER_URL` | `ws://localhost:18080/ws` | E2E 服务器 WebSocket 地址 |
| `$REDIS_ADDR` | `localhost:16379` | E2E Redis 地址 |
| `$REDIS_DB` | `15` | E2E Redis DB 编号 |
| `$ALICE` | `alice` | 测试用户 Alice |
| `$E2E_HOME` | `/tmp/xe2e-XXXXXX` | 临时测试目录 |
| `$AGENT_ID` | `error-test-bot` | 错误测试 Agent ID |
| `$CONV_ID` | (运行时获取) | 测试会话 ID |

---

## 5. 完整流程图

```mermaid
flowchart TD
    Start([开始]) --> Prep[🟢 环境准备<br/>构建二进制、启动 Docker<br/>备份 Agent 配置]

    Prep --> Phase1

    subgraph Phase1 [阶段 1: 配置错误 — API Key 缺失]
        P1A[🔵 修改 Agent 配置<br/>api_key_env 指向不存在的环境变量] --> P1B[🟢 docker cp + 重启服务器<br/>重新加载 Agent 配置]
        P1B --> P1C[🔵 创建会话<br/>alice ↔ error-test-bot]
        P1C --> P1D[🔵 发送消息<br/>"你好"]
        P1D --> P1E[🟡 等待处理<br/>sleep 10]
        P1E --> P1F[🔴 双重验证<br/>CLI: get-messages<br/>DB: SELECT messages]
        P1F --> P1G{错误消息正确？}
        P1G -->|✅| Phase2
        P1G -->|❌| P1Fix[检查配置和日志]
        P1Fix --> P1A
    end

    Phase2 --> subgraph Phase2 [阶段 2: 通用错误 — LLM 服务不可达]
        P2A[🔵 修改 Agent 配置<br/>base_url 指向不可达地址] --> P2B[🟢 docker cp + 重启服务器]
        P2B --> P2C[🔵 创建新会话]
        P2C --> P2D[🔵 发送消息]
        P2D --> P2E[🟡 等待处理]
        P2E --> P2F[🔴 双重验证<br/>验证通用错误消息]
        P2F --> P2G{错误消息正确？}
        P2G -->|✅| Phase3
        P2G -->|❌| P2Fix[检查日志]
        P2Fix --> P2A
    end

    Phase3 --> subgraph Phase3 [阶段 3: LLM 超时/限流 — 无效 API Key]
        P3A[🔵 修改 Agent 配置<br/>使用无效的 API Key] --> P3B[🟢 docker cp + 重启服务器]
        P3B --> P3C[🔵 创建新会话]
        P3C --> P3D[🔵 发送消息]
        P3D --> P3E[🟡 等待处理<br/>LLM 可能超时或返回错误]
        P3E --> P3F[🔴 双重验证<br/>验证超时/限流错误消息]
        P3F --> P3G{错误消息正确？}
        P3G -->|✅| Phase4
        P3G -->|❌| P3Fix[检查日志]
        P3Fix --> P3A
    end

    Phase4 --> subgraph Phase4 [阶段 4: HITL 中断不产生错误消息]
        P4A[🔵 恢复 hitl-bot 配置<br/>使用正常 API Key] --> P4B[🟢 docker cp + 重启服务器]
        P4B --> P4C[🔵 创建 HITL 会话]
        P4C --> P4D[🔵 发送触发消息<br/>触发 HITL 中断]
        P4D --> P4E[🟡 等待处理]
        P4E --> P4F[🔴 双重验证<br/>验证 HITL 无错误消息<br/>验证 checkpoint 存在]
        P4F --> P4G{无错误消息？}
        P4G -->|✅| Phase5
        P4G -->|❌| P4Fix[检查 HITL 逻辑]
        P4Fix --> P4A
    end

    Phase5 --> subgraph Phase5 [阶段 5: 错误后恢复正常]
        P5A[🔵 恢复正常 Agent 配置<br/>使用有效 API Key] --> P5B[🟢 docker cp + 重启服务器]
        P5B --> P5C[🔵 创建新会话]
        P5C --> P5D[🔵 发送消息]
        P5D --> P5E[🟡 等待处理]
        P5E --> P5F[🔴 双重验证<br/>验证 Agent 正常响应]
        P5F --> P5G{正常响应？}
        P5G -->|✅| Cleanup
        P5G -->|❌| P5Fix[检查配置]
        P5Fix --> P5A
    end

    Cleanup --> subgraph Cleanup [⚪ 环境清理]
        CL1[🔵 停止 daemon] --> CL2[🟢 停止 Docker]
        CL2 --> CL3[🔵 恢复 Agent 配置]
        CL3 --> CL4[🔵 清理临时目录]
    end

    Cleanup --> End([结束])
```

---

## 6. 分步执行指南

### 阶段 1: 配置错误 — API Key 环境变量未设置

> **目标**：验证当 Agent 配置的 `api_key_env` 指向一个未设置的环境变量时，用户看到"配置有误"提示。
>
> **预期错误消息**：`"抱歉，我的配置有误，请联系管理员检查设置。"`
>
> **触发路径**：`LLMClientFactory.Create()` → `os.Getenv()` 返回空 → `ErrAPIKeyMissing` → `classifyError()` → 配置错误消息

#### 步骤 1.1: 创建错误测试 Agent 配置

```bash
cat > agents/error-test-bot.md << 'EOF'
---
id: error-test-bot
name: 错误测试助手
description: "用于测试 Agent 错误消息持久化"
model: qwen3.7-plus
api_key_env: XYNCRA_NONEXISTENT_API_KEY_XXXXX
base_url: "https://coding.dashscope.aliyuncs.com/v1"
parameters:
  temperature: 0.7
  max_tokens: 500
context:
  max_tokens: 4000
  max_messages: 10
---

你是一个测试助手。
EOF
```

**验证**：
```bash
cat agents/error-test-bot.md
# 预期: 显示刚创建的配置文件
grep "api_key_env:" agents/error-test-bot.md
# 预期: api_key_env: XYNCRA_NONEXISTENT_API_KEY_XXXXX
```

#### 步骤 1.2: 同步配置到容器并重启服务器

```bash
# 必须 docker cp，因为容器内 agents/ 是构建时 COPY 的（见 3.6）
docker cp agents/error-test-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/error-test-bot.md
docker compose -f deploy/docker-compose.e2e.yml restart xyncra-server-e2e
sleep 5

curl -s http://localhost:18080/health
# 预期: {"status":"ok"}
```

#### 步骤 1.3: 启动 Alice daemon

```bash
./bin/xyncra-client listen \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  > "$E2E_HOME/alice-daemon.log" 2>&1 &
ALICE_PID=$!
sleep 2

ps -p $ALICE_PID
# 预期: 进程存在
```

#### 步骤 1.4: 创建与 error-test-bot 的会话

```bash
./bin/xyncra-client create-conversation \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --peer-id "agent/error-test-bot"
```

**预期输出**（类似）：
```
Conversation created.
ID:       <conv-uuid>
Peer:     agent/error-test-bot
Type:     1-on-1
```

**操作**：
```bash
CONV_ID="<从输出中获取>"
echo "CONV_ID=$CONV_ID"
```

#### 步骤 1.5: 发送消息触发 Agent 处理

```bash
./bin/xyncra-client send \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "你好，请介绍一下自己"
```

**预期**：消息发送成功，记录消息 ID。

#### 步骤 1.6: 等待 Agent 处理

```bash
sleep 10
```

> Agent 会尝试 Build → 发现 API Key 环境变量不存在 → 失败 → 持久化错误消息。

#### 步骤 1.7: 验证 — 客户端命令

```bash
./bin/xyncra-client sync-updates \
  --user-id alice \
  --device-id test-device-alice

./bin/xyncra-client get-messages \
  --user-id alice \
  --device-id test-device-alice \
  --conversation-id "$CONV_ID"
```

**预期**：
- 输出包含 Alice 的提问消息（"你好，请介绍一下自己"）
- 输出包含 Agent 的错误消息：**"抱歉，我的配置有误，请联系管理员检查设置。"**
- Agent 消息的 sender 显示为 `agent/error-test-bot`

#### 步骤 1.8: 验证 — 服务器 DB 直接查询

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db \
  "SELECT sender_id, content FROM messages WHERE conversation_id = '$CONV_ID' ORDER BY message_id ASC;"
```

**预期**：
```
alice|你好，请介绍一下自己
agent/error-test-bot|抱歉，我的配置有误，请联系管理员检查设置。
```

- 第一条消息：Alice 的提问
- 第二条消息：Agent 的**用户友好错误消息**（不是原始错误堆栈）

#### 步骤 1.9: 验证 — 服务器日志

```bash
docker compose -f deploy/docker-compose.e2e.yml logs xyncra-server-e2e 2>&1 | grep -i "error\|API key\|配置" | tail -5
```

**预期**：日志中包含 `API key environment variable not set` 或类似错误信息（原始错误，用于开发调试）。

#### 步骤 1.10: 记录 CONV_ID

```bash
PHASE1_CONV_ID="$CONV_ID"
echo "Phase 1 CONV_ID=$PHASE1_CONV_ID"
```

---

### 阶段 2: 通用错误 — LLM 服务不可达

> **目标**：验证当 `base_url` 指向不可达地址时，用户看到"处理遇到问题"提示。
>
> **预期错误消息**：`"抱歉，处理遇到问题，请稍后重试。"`
>
> **触发路径**：`Provider.CreateChatModel()` → 连接失败 → `ErrAgentBuild` → `classifyError()` → 通用错误消息

#### 步骤 2.1: 修改 Agent 配置为不可达地址

```bash
cat > agents/error-test-bot.md << 'EOF'
---
id: error-test-bot
name: 错误测试助手
description: "用于测试 Agent 错误消息持久化"
model: qwen3.7-plus
api_key_env: DASHSCOPE_API_KEY
base_url: "http://192.0.2.1:9999/v1"
parameters:
  temperature: 0.7
  max_tokens: 500
context:
  max_tokens: 4000
  max_messages: 10
---

你是一个测试助手。
EOF
```

> **说明**：`192.0.2.1` 是 TEST-NET-1（RFC 5737），在公网不可达，连接会超时。
>
> 使用容器中实际存在的环境变量名（`DASHSCOPE_API_KEY`），确保 API Key 存在但服务不可达，从而触发连接错误而非 API Key 缺失错误。如果使用容器中不存在的环境变量名，错误会在 Build 阶段提前终止，返回配置错误消息而非通用错误消息。

**验证**：
```bash
grep "base_url:" agents/error-test-bot.md
# 预期: base_url: "http://192.0.2.1:9999/v1"
```

#### 步骤 2.2: 同步配置到容器并重启服务器

```bash
docker cp agents/error-test-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/error-test-bot.md
docker compose -f deploy/docker-compose.e2e.yml restart xyncra-server-e2e
sleep 5

curl -s http://localhost:18080/health
# 预期: {"status":"ok"}
```

#### 步骤 2.3: 创建新会话

```bash
CONV_INFO=$(./bin/xyncra-client create-conversation \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --peer-id "agent/error-test-bot")

CONV_ID=$(echo "$CONV_INFO" | grep "ID:" | awk '{print $2}')
echo "CONV_ID=$CONV_ID"
```

#### 步骤 2.4: 发送消息

```bash
./bin/xyncra-client send \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "测试不可达服务"
```

#### 步骤 2.5: 等待处理（连接超时可能需要更长时间）

```bash
sleep 15
```

#### 步骤 2.6: 验证 — 客户端命令

```bash
./bin/xyncra-client sync-updates \
  --user-id alice \
  --device-id test-device-alice

./bin/xyncra-client get-messages \
  --user-id alice \
  --device-id test-device-alice \
  --conversation-id "$CONV_ID"
```

**预期**：
- 包含 Alice 的提问消息
- 包含 Agent 的错误消息：**"抱歉，处理遇到问题，请稍后重试。"** 或 **"抱歉，我暂时无法回复，请稍后重试。"**（取决于错误是在 Build 阶段还是 Run 阶段发生）

#### 步骤 2.7: 验证 — 服务器 DB 直接查询

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db \
  "SELECT sender_id, content FROM messages WHERE conversation_id = '$CONV_ID' AND sender_id = 'agent/error-test-bot';"
```

**预期**：返回一行，content 为用户友好的错误消息（中文）

#### 步骤 2.8: 验证 — 服务器日志

```bash
docker compose -f deploy/docker-compose.e2e.yml logs xyncra-server-e2e 2>&1 | grep -i "timeout\|unreachable\|connect\|build" | tail -5
```

**预期**：日志中包含连接超时或拒绝相关的错误信息。

---

### 阶段 3: LLM 超时/限流 — 无效 API Key

> **目标**：验证当 API Key 格式无效导致 LLM 返回认证错误或超时时，用户看到"暂时无法回复"提示。
>
> **预期错误消息**：`"抱歉，我暂时无法回复，请稍后重试。"`
>
> **触发路径**：LLM API 返回 401/429/5xx → `classifyError()` → 超时/限流消息

#### 步骤 3.1: 修改 Agent 配置为无效 API Key

```bash
cat > agents/error-test-bot.md << 'EOF'
---
id: error-test-bot
name: 错误测试助手
description: "用于测试 Agent 错误消息持久化"
model: qwen3.7-plus
api_key_env: XYNCRA_INVALID_KEY_FOR_TEST
base_url: "https://coding.dashscope.aliyuncs.com/v1"
parameters:
  temperature: 0.7
  max_tokens: 500
context:
  max_tokens: 4000
  max_messages: 10
---

你是一个测试助手。
EOF
```

#### 步骤 3.2: 设置无效 API Key 环境变量并重启服务器

```bash
# 在 deploy/docker-compose.e2e.yml 中临时添加环境变量
# 或直接通过 docker compose -f deploy/docker-compose.yml 传递
docker compose -f deploy/docker-compose.e2e.yml down
XYNCRA_INVALID_KEY_FOR_TEST="sk-invalid-test-key-12345" \
  docker compose -f deploy/docker-compose.e2e.yml up -d
sleep 5

# 必须 docker cp 重新同步 agent 配置（容器被重建）
docker cp agents/error-test-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/error-test-bot.md
docker compose -f deploy/docker-compose.e2e.yml restart xyncra-server-e2e
sleep 5

curl -s http://localhost:18080/health
# 预期: {"status":"ok"}
```

> **说明**：使用一个格式正确但无效的 API Key（`sk-invalid-test-key-12345`），LLM 提供商会返回 401 Unauthorized 或 429 错误。

#### 步骤 3.3: 创建新会话

```bash
CONV_INFO=$(./bin/xyncra-client create-conversation \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --peer-id "agent/error-test-bot")

CONV_ID=$(echo "$CONV_INFO" | grep "ID:" | awk '{print $2}')
echo "CONV_ID=$CONV_ID"
```

#### 步骤 3.4: 发送消息

```bash
./bin/xyncra-client send \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "请回复我"
```

#### 步骤 3.5: 等待处理

```bash
sleep 15
```

#### 步骤 3.6: 验证 — 客户端命令

```bash
./bin/xyncra-client sync-updates \
  --user-id alice \
  --device-id test-device-alice

./bin/xyncra-client get-messages \
  --user-id alice \
  --device-id test-device-alice \
  --conversation-id "$CONV_ID"
```

**预期**：

- 包含 Agent 的错误消息：**"抱歉，处理遇到问题，请稍后重试。"**
- 说明：LLM 返回 401 Unauthorized（无效 API Key）时，`classifyError()` 将其归类为通用错误（generic），而非超时/限流。如需触发"暂时无法回复"消息，需构造 HTTP 429/5xx 或 context deadline exceeded 场景。

#### 步骤 3.7: 验证 — 服务器 DB 直接查询

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db \
  "SELECT sender_id, content FROM messages WHERE conversation_id = '$CONV_ID' AND sender_id = 'agent/error-test-bot';"
```

**预期**：返回一行，content 为用户友好的错误消息

#### 步骤 3.8: 验证 — 服务器日志

```bash
docker compose -f deploy/docker-compose.e2e.yml logs xyncra-server-e2e 2>&1 | grep -i "rate\|429\|401\|unauthorized\|timeout\|stream" | tail -5
```

**预期**：日志中包含 LLM 返回的错误码或超时信息。

---

### 阶段 4: HITL 中断不产生错误消息

> **目标**：验证 HITL 中断是受控暂停，**不会**产生错误消息。
>
> **预期**：会话中只有 Alice 的提问消息，没有 Agent 错误消息。Checkpoint 存在于 Redis 中。
>
> **触发路径**：Agent 调用 `ask_user` 工具 → HITL 中断 → `ErrHITLInterrupted` → `ExecuteWithErrorMessage()` 检测到 HITL → **跳过**错误消息

#### 步骤 4.1: 恢复 hitl-bot 配置

```bash
# 确认 hitl-bot.md 存在且配置正确
cat agents/hitl-bot.md
# 预期: 包含 HITL Agent 配置，api_key_env 指向有效的环境变量
```

> 如果 `hitl-bot.md` 不存在或需要修改，参考 TC-003 中的配置。关键是确保 Agent 在用户发送敏感操作请求时会触发 HITL（调用 `ask_user` 工具）。

#### 步骤 4.2: 同步配置到容器并重启服务器

```bash
docker cp agents/hitl-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/hitl-bot.md
docker compose -f deploy/docker-compose.e2e.yml restart xyncra-server-e2e
sleep 5

curl -s http://localhost:18080/health
# 预期: {"status":"ok"}
```

#### 步骤 4.3: 创建 HITL 会话

```bash
CONV_INFO=$(./bin/xyncra-client create-conversation \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --peer-id "agent/hitl-bot")

HITL_CONV_ID=$(echo "$CONV_INFO" | grep "ID:" | awk '{print $2}')
echo "HITL_CONV_ID=$HITL_CONV_ID"
```

#### 步骤 4.4: 发送触发 HITL 的消息

```bash
./bin/xyncra-client send \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$HITL_CONV_ID" \
  --content "删除所有数据"
```

#### 步骤 4.5: 等待 Agent 处理并触发 HITL

```bash
sleep 15
```

#### 步骤 4.6: 验证 — 客户端命令（无错误消息）

```bash
./bin/xyncra-client sync-updates \
  --user-id alice \
  --device-id test-device-alice

./bin/xyncra-client get-messages \
  --user-id alice \
  --device-id test-device-alice \
  --conversation-id "$HITL_CONV_ID"
```

**预期**：
- 包含 Alice 的提问消息（"删除所有数据"）
- **不包含**任何 Agent 错误消息（如"抱歉…"）
- 可能包含 Agent 在 HITL 中断前生成的部分文本（如"这个操作不可逆，请确认…"）

#### 步骤 4.7: 验证 — 服务器 DB（无错误消息）

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db \
  "SELECT sender_id, content FROM messages WHERE conversation_id = '$HITL_CONV_ID' ORDER BY message_id ASC;"
```

**预期**：
- 只有 Alice 的消息，或 Alice 消息 + Agent 的部分文本（HITL 中断前的输出）
- **没有**包含"抱歉"的错误消息

#### 步骤 4.8: 验证 — Redis Checkpoint 存在

```bash
redis-cli -p 16379 -n 15 KEYS "agent:checkpoint:*"
```

**预期**：包含 `agent:checkpoint:$HITL_CONV_ID` 或类似 key — 证明 HITL 中断已正确保存 checkpoint，是受控暂停而非错误。

```bash
redis-cli -p 16379 -n 15 KEYS "agent:lock:*"
```

**预期**：包含 `agent:lock:$HITL_CONV_ID` — 证明会话锁在 HITL 期间被持有（D-084）。

---

### 阶段 5: 错误后恢复正常

> **目标**：验证修复配置后，Agent 能正常处理消息，不被之前的错误影响。

#### 步骤 5.1: 恢复正常的 Agent 配置

```bash
# 使用 .env.test 中配置的有效 API Key
cat > agents/error-test-bot.md << 'EOF'
---
id: error-test-bot
name: 错误测试助手
description: "用于测试 Agent 错误消息持久化"
model: qwen3.7-plus
api_key_env: DASHSCOPE_API_KEY
base_url: "https://coding.dashscope.aliyuncs.com/v1"
parameters:
  temperature: 0.7
  max_tokens: 500
context:
  max_tokens: 4000
  max_messages: 10
---

你是一个测试助手。请用简短的中文回答问题。
EOF
```

#### 步骤 5.2: 同步配置到容器并重启服务器

```bash
docker cp agents/error-test-bot.md xyncra-server-xyncra-server-e2e-1:/app/agents/error-test-bot.md
docker compose -f deploy/docker-compose.e2e.yml restart xyncra-server-e2e
sleep 5

curl -s http://localhost:18080/health
# 预期: {"status":"ok"}
```

#### 步骤 5.3: 创建新会话

```bash
CONV_INFO=$(./bin/xyncra-client create-conversation \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --peer-id "agent/error-test-bot")

CONV_ID=$(echo "$CONV_INFO" | grep "ID:" | awk '{print $2}')
echo "CONV_ID=$CONV_ID"
```

#### 步骤 5.4: 发送消息

```bash
./bin/xyncra-client send \
  --user-id alice \
  --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "你好，请用一句话介绍自己"
```

#### 步骤 5.5: 等待处理

```bash
sleep 15
```

#### 步骤 5.6: 验证 — 客户端命令（正常响应）

```bash
./bin/xyncra-client sync-updates \
  --user-id alice \
  --device-id test-device-alice

./bin/xyncra-client get-messages \
  --user-id alice \
  --device-id test-device-alice \
  --conversation-id "$CONV_ID"
```

**预期**：
- 包含 Alice 的提问消息
- 包含 Agent 的**正常响应**（不是错误消息）
- Agent 响应不包含"抱歉"等错误提示词

#### 步骤 5.7: 验证 — 服务器 DB（正常消息）

```bash
docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e \
  sqlite3 /app/xyncra-e2e.db \
  "SELECT sender_id, SUBSTR(content, 1, 80) FROM messages WHERE conversation_id = '$CONV_ID' ORDER BY message_id ASC;"
```

**预期**：
- Alice 的消息：`你好，请用一句话介绍自己`
- Agent 的消息：正常的回复内容（不含"抱歉"）

---

## 7. 数据库验证汇总

### 7.1 Server DB 验证命令速查

```bash
DB_EXEC="docker compose -f deploy/docker-compose.e2e.yml exec xyncra-server-e2e sqlite3 /app/xyncra-e2e.db"

# 查看指定会话的所有消息（含错误消息）
$DB_EXEC "SELECT sender_id, content FROM messages WHERE conversation_id = '<conv-id>' ORDER BY message_id ASC;"

# 只查看 Agent 发送的错误消息
$DB_EXEC "SELECT content FROM messages WHERE conversation_id = '<conv-id>' AND sender_id LIKE 'agent/%';"

# 统计 Agent 消息数量（验证只有一条错误消息）
$DB_EXEC "SELECT COUNT(*) FROM messages WHERE conversation_id = '<conv-id>' AND sender_id = 'agent/error-test-bot';"

# 查看所有 Agent 相关会话
$DB_EXEC "SELECT id, user_id1, user_id2 FROM conversations WHERE user_id2 LIKE 'agent/%';"
```

### 7.2 Server Redis 验证命令速查

```bash
R="redis-cli -p 16379 -n 15"

# Agent 幂等性 key
$R KEYS "agent:idempotent:*"

# Agent checkpoint（HITL 场景）
$R KEYS "agent:checkpoint:*"

# Agent 会话锁
$R KEYS "agent:lock:*"

# Asynq 队列（检查是否有失败的任务）
$R KEYS "asynq:{*}"

# 清理
$R FLUSHDB
```

### 7.3 Client DB 验证命令速查

```bash
CLIENT_DB=$(ls ~/.xyncra/alice/*/xyncra.db 2>/dev/null | head -1)

# 查看客户端本地消息
sqlite3 "$CLIENT_DB" \
  "SELECT sender_id, content FROM messages WHERE conversation_id = '<conv-id>' ORDER BY message_id ASC;"

# 查看客户端同步状态
sqlite3 "$CLIENT_DB" \
  "SELECT key, value FROM sync_states;"
```

---

## 8. 通过/失败判定标准

| 阶段 | 判定条件 | 通过标志 | 失败处理 |
|------|---------|----------|---------|
| 阶段 1 (API Key 缺失) | Agent 消息 content = "抱歉，我的配置有误，请联系管理员检查设置。" | ✅ | 检查 api_key_env 配置、服务器日志 |
| 阶段 2 (LLM 不可达) | Agent 消息 content 包含 "抱歉" 且为错误消息（"处理遇到问题" 或 "暂时无法回复"） | ✅ | 检查 base_url 配置、网络连通性、服务器日志 |
| 阶段 3 (无效 API Key) | Agent 消息 content 包含 "抱歉" 且为错误消息（"暂时无法回复" 或 "处理遇到问题"） | ✅ | 检查环境变量是否正确传递到容器 |
| 阶段 4 (HITL 无错误) | Agent 消息中**不包含**"抱歉"等错误消息；Redis 存在 checkpoint key | ✅ | 检查 hitl-bot 配置、ask_user 工具是否正确触发 |
| 阶段 5 (恢复正常) | Agent 消息为正常回复，不含"抱歉" | ✅ | 检查 .env.test 配置、API Key 有效性 |

### 通用验证标准

- 所有错误消息的 `sender_id` 必须是 Agent（如 `agent/error-test-bot`），不是 alice
- 错误消息必须持久化到服务器 DB 的 `messages` 表中
- 用户通过 `get-messages` 必须能看到错误消息
- 错误消息必须是**用户友好的中文**，不能包含原始错误堆栈或技术细节

---

## 9. 故障排查指南

| 症状 | 可能原因 | 解决方法 |
|------|---------|---------|
| Agent 无响应（无任何消息） | MQ 任务未入队或被跳过 | 检查 `asynq` 队列、服务器日志中是否有 `agent_process` 任务 |
| 错误消息不是中文 | `classifyError()` 走了 default 分支 | 检查服务器日志中的原始错误，确认 sentinel error 匹配 |
| API Key 缺失但无错误消息 | 环境变量意外存在 | `env | grep XYNCRA_NONEXISTENT` 确认环境变量确实不存在 |
| 修改 Agent 配置后未生效 | 容器内 `agents/` 是构建时 COPY 的，非挂载 | 必须 `docker cp` 到容器内再重启，见 §3.6 |
| 错误分类与预期不符 | `api_key_env` 指向容器中不存在的变量 | 确认 `docker compose -f deploy/docker-compose.yml exec ... env` 中变量存在，否则 Build 阶段会提前失败为配置错误 |
| HITL 未触发 | Agent 没有使用 `ask_user` 工具 | 检查 hitl-bot 配置、确认 middleware 中未禁用 HITL |
| 阶段 5 仍然报错 | 服务器未正确重启或 Agent 配置未更新 | `docker cp` + `docker compose -f deploy/docker-compose.yml restart` + `curl /health` 确认 |
| 容器内看不到环境变量 | docker-compose 未传递环境变量 | 检查 `deploy/docker-compose.e2e.yml` 的 `environment` 配置 |
| 消息顺序不对 | sync-updates 未执行 | 先执行 `sync-updates`，再执行 `get-messages` |

---

## 10. 环境清理

```bash
# 停止 Alice daemon
./bin/xyncra-client kill --user-id alice --device-id test-device-alice
./bin/xyncra-client kill --user-id alice --device-id test-device-alice --force 2>/dev/null

# 停止 Docker E2E 环境
docker compose -f deploy/docker-compose.e2e.yml down

# 恢复原始 Agent 配置
rm -f agents/error-test-bot.md
cp -r "$E2E_HOME/agents-backup/"* agents/

# 清理临时目录
rm -rf "$E2E_HOME"

# 清理 ~/.xyncra 测试数据
rm -rf ~/.xyncra/alice

# 清理 Redis（可选）
redis-cli -p 16379 -n 15 FLUSHDB
```

---

## 11. 真实 LLM 测试配置（.env.test）

阶段 3 和阶段 5 需要真实 LLM 服务（阶段 3 需要 LLM 返回错误，阶段 5 需要正常响应）。

### 环境变量说明

| 变量 | 说明 | 默认值 | 必需 |
|------|------|--------|------|
| `XYNCRA_TEST_REAL_LLM_ENABLED` | 启用真实 LLM 测试 | `true` | 是 |
| `XYNCRA_TEST_LLM_API_KEY` | LLM API 密钥 | — | 阶段 5 必需 |
| `XYNCRA_TEST_LLM_BASE_URL` | LLM API 基础 URL | `https://coding.dashscope.aliyuncs.com/v1` | 否 |
| `XYNCRA_TEST_LLM_MODEL` | 模型名称 | `qwen3.7-plus` | 否 |

### 安全提示

> ⚠️ `.env.test` 包含 API 密钥，**已在 `.gitignore` 中排除**，切勿提交到版本控制。

### 成本控制 (D-090)

- 阶段 1-2 不调用 LLM（配置错误/连接错误），无 token 消耗
- 阶段 3 调用 LLM 但会快速失败，消耗极少 token
- 阶段 4 HITL 中断，token 消耗较低
- 阶段 5 一次正常调用，约消耗 1k-3k tokens
- 完整测试总计约 5k tokens 以内

---

## 12. 依赖关系说明

| 测试阶段 | 可独立执行 | 依赖 |
|---------|-----------|------|
| 阶段 1 (API Key 缺失) | ✅ | 环境准备 |
| 阶段 2 (LLM 不可达) | ✅ | 环境准备 |
| 阶段 3 (无效 API Key) | ✅ | 环境准备 + .env.test |
| 阶段 4 (HITL 无错误) | ✅ | 环境准备 + hitl-bot 配置 |
| 阶段 5 (恢复正常) | ✅ | 环境准备 + .env.test |

**执行顺序建议**：1 → 2 → 3 → 4 → 5（从错误到正常，验证渐进恢复）

**可并行执行的阶段**：
- 阶段 1-3 可并行（使用不同的 Agent 配置，但需要不同的服务器重启）
- 阶段 4 独立于阶段 1-3（使用不同的 Agent：hitl-bot）
- 阶段 5 必须在所有错误场景完成后执行（验证恢复）

---

## 13. 测试执行记录模板

```markdown
### TC-006 测试执行记录

| 字段 | 值 |
|------|-----|
| 日期 | YYYY-MM-DD |
| Git Commit | <sha> |
| 测试者 | <name> |
| 环境 | Docker E2E |
| E2E_HOME | /tmp/xe2e-XXXXXX |
| 总耗时 | XXm |

| 阶段 | 结果 | 实际错误消息 | 备注 |
|------|------|------------|------|
| 阶段 1: API Key 缺失 | ✅ / ❌ | | 预期: "抱歉，我的配置有误，请联系管理员检查设置。" |
| 阶段 2: LLM 不可达 | ✅ / ❌ | | 预期: "抱歉，处理遇到问题" 或 "抱歉，我暂时无法回复" |
| 阶段 3: 无效 API Key | ✅ / ❌ | | 预期: "抱歉，处理遇到问题"（401 → 通用错误） |
| 阶段 4: HITL 无错误 | ✅ / ❌ | | 预期: 无 "抱歉" 消息 |
| 阶段 5: 恢复正常 | ✅ / ❌ | | 预期: Agent 正常响应 |

**发现的问题**：
1. (描述)

**错误消息分类准确性**：
- API Key 缺失 → 配置错误: ✅ / ❌
- 连接超时 → 通用错误 / 超时错误: ✅ / ❌
- 无效 Key (401) → 通用错误: ✅ / ❌
- HITL 中断 → 无错误消息: ✅ / ❌

**结论**：PASS / FAIL (X/5 阶段通过)
```

---

## 14. 参考文档

- [PRODUCT_DECISIONS.md](../../../docs/PRODUCT_DECISIONS.md) — D-067, D-082
- [TC-000-完整链路测试.md](../../../docs/manual-test-cases/TC-000-完整链路测试.md) — Agent 基础交互
- [TC-003-HITL完整流程测试.md](../../../docs/manual-test-cases/TC-003-HITL完整流程测试.md) — HITL 完整流程
- [internal/agent/executor.go](../../../internal/agent/executor.go) — 错误分类实现
- [internal/agent/errors.go](../../../internal/agent/errors.go) — Sentinel errors 定义
