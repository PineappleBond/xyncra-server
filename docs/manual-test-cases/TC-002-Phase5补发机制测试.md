# TC-002: Phase 5 补发机制测试（双版本 Client）

> **测试编号**: TC-002
> **测试类型**: 端到端集成测试
> **覆盖范围**: ReverseRPC Pending Store (D-103)、system.reconnect (D-108)、补发机制 (D-109)、设备替换 (D-095)
> **环境**: Docker E2E (D-043)
> **客户端版本**: Go Client (`./bin/xyncra-client`) + TypeScript Client (`$CLIENT_TS`)
> **最后更新**: 2026-07-18

---

## 1. 概述

本测试用例覆盖 Xyncra 消息系统 Phase 5 的补发机制：当 ReverseRPC 请求超时后，请求被持久化到 Redis Pending Store，客户端重连后通过 `system.reconnect` RPC 触发补发。

### 1.0.1 双版本 Client 测试策略

本测试同时覆盖 **Go Client** 和 **TypeScript Client** 两个版本。TS Client 是 Go Client 的 1:1 功能复刻（TS-D-005），CLI 命令、参数和输出格式完全一致。

**测试方法**：
- 阶段 1-4 使用 Go Client 作为主测试路径（文档中的默认命令）
- 每个阶段末尾标注 TS Client 的适配要点
- 两个版本应表现出相同的补发机制行为

**关键差异**（影响测试验证步骤）：

| 差异项 | Go Client | TS Client | 决策编号 |
| --- | --- | --- | --- |
| 二进制路径 | `./bin/xyncra-client` | `xyncra-client`（npm link 后） | — |
| 本地存储 | SQLite 文件 (`xyncra.db`) | IndexedDB (Dexie.js / fake-indexeddb) | TS-D-012 |
| `--device-id` | 必需参数 | 可选（默认 SHA256(hostname)[:8]） | D-033 |
| 进程锁实现 | fcntl | fs-ext | D-031 |

> **源码位置**：TS Client 源码在 `demo/web/packages/xyncra-client-cli/`
> **维护提醒**：如果 TS Client 代码更新，需同步更新本文档。

**测试目标**：验证断连期间的 ReverseRPC 请求能够在重连后正确补发，确保 HITL 等场景的可靠性。验证 Go/TS 两个版本 Client 的补发机制行为一致性。

**覆盖的关键决策**：

- D-095: 设备替换策略（Close Frame 4001）
- D-103: ReverseRPC Pending Store（Redis 持久化）
- D-104: 幂等键与 Seq 协议扩展
- D-105: CancelDevice 不清理 Redis Pending
- D-106: Per-device Seq 计数器策略
- D-107: Replay 请求 ID 格式（s-replay-{uuid}）
- D-108: system.reconnect RPC 规范
- D-109: 补发并发与超时策略

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
│  │ xyncra-client   │    │ 模拟 HITL 场景  │               │
│  │ User: alice     │    │ (Agent 触发      │               │
│  │ Daemon (IPC)    │    │  ReverseRPC)     │               │
│  └─────────────────┘    └─────────────────┘               │
│                                                             │
│  工作目录: $E2E_HOME (mktemp -d)                            │
└─────────────────────────────────────────────────────────────┘
```

---

## 3. 前置条件

### 3.1 构建二进制

#### 3.1.1 Go Client & Server

```bash
cd /path/to/xyncra-server
make build
```

#### 3.1.2 TypeScript Client

> **源码位置**：`demo/web/packages/xyncra-client-cli/`
> **编译入口**：`demo/web/packages/xyncra-client-cli/dist/bin/xyncra-client.js`
> **维护提醒**：如果 TS Client 代码更新，需同步更新本文档。

```bash
# 构建 TS Client
cd demo/web
npm install
cd packages/xyncra-client-cli
npm run build

# 方式 1: npm link（推荐，使命令全局可用）
npm link

# 方式 2: 直接 node 调用（从项目根目录）
# node demo/web/packages/xyncra-client-cli/dist/bin/xyncra-client.js
```

验证：

```bash
xyncra-client --version
# 预期: 0.1.0
```

> **变量定义**：
> - `$CLIENT_GO` = `./bin/xyncra-client`（从项目根目录执行）
> - `$CLIENT_TS` = `xyncra-client`（npm link 后）或 `node demo/web/packages/xyncra-client-cli/dist/bin/xyncra-client.js`

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
# 预期: {"status":"ok"}
```

### 3.4 创建测试工作目录

```bash
export E2E_HOME=$(mktemp -d /tmp/xe2e-XXXXXX)
echo "E2E_HOME=$E2E_HOME"
```

### 3.5 配置 Agent（用于触发 ReverseRPC）

确保 `agents/weather-bot.md` 配置了 `middleware.enable_client_tools: true`，以启用 DynamicToolProvider。

---

## 4. 测试数据字典

| 变量 | 值 | 说明 |
|------|-----|------|
| `$SERVER_URL` | `ws://localhost:18080/ws` | E2E 服务器 WebSocket 地址 |
| `$REDIS_ADDR` | `localhost:16379` | E2E Redis 地址 |
| `$REDIS_DB` | `15` | E2E Redis DB 编号 |
| `$ALICE` | `alice` | 测试用户 Alice |
| `$E2E_HOME` | `/tmp/xe2e-XXXXXX` | 临时测试目录 |
| `$DEVICE_ID` | (运行时获取) | Alice 的设备 ID |
| `$PENDING_KEY` | `pending:alice\x00$DEVICE_ID` | Redis Pending Key |
| `$CLIENT_GO` | `./bin/xyncra-client` | Go Client 二进制路径 |
| `$CLIENT_TS` | `xyncra-client` | TS Client 命令（npm link 后） |

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
    end

    CreateDir --> Phase1

    subgraph Phase1 [阶段 1: 启动 Daemon 并注册函数]
        P1A[启动 Alice daemon] --> P1B[验证 device_id 生成]
        P1B --> P1C[验证 system.register_functions 调用]
    end

    P1C --> Phase2

    subgraph Phase2 [阶段 2: 触发 ReverseRPC 并模拟超时]
        P2A[Agent 调用客户端工具\n触发 ReverseRPC] --> P2B[客户端不响应\n模拟超时]
        P2B --> P2C[验证 Redis Pending Store\npending:{userID}\x00{deviceID}]
    end

    P2C --> Phase3

    subgraph Phase3 [阶段 3: 客户端重连并补发]
        P3A[客户端调用 system.reconnect\nlast_seen_seq=0] --> P3B[验证服务器补发请求\ns-replay-{uuid}]
        P3B --> P3C[客户端响应补发请求]
        P3C --> P3D[验证 Redis Pending 已清理]
    end

    P3D --> Phase4

    subgraph Phase4 [阶段 4: 设备替换测试]
        P4A[新连接使用相同 device_id] --> P4B[验证旧连接收到 Close 4001]
        P4B --> P4C[验证旧连接 pending 保留\n不清理 D-105]
    end

    P4C --> Cleanup

    subgraph Cleanup [环境清理]
        CL1[停止 daemon] --> CL2[停止 Docker]
        CL2 --> CL3[清理临时目录]
    end

    Cleanup --> End([结束])
```

---

## 6. 分步执行指南

### 阶段 1: 启动 Daemon 并注册函数

#### 步骤 1.1: 启动 Alice daemon

```bash
./bin/xyncra-client listen \
  --user-id alice --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  > "$E2E_HOME/alice-daemon.log" 2>&1 &
ALICE_PID=$!
sleep 2
```

#### 步骤 1.2: 获取 device_id

```bash
# 从日志或 Redis 中获取 device_id
DEVICE_ID=$(redis-cli -p 16379 -n 15 KEYS "xyncra:conn:info:*" | head -1 | sed 's/xyncra:conn:info://')
echo "DEVICE_ID=$DEVICE_ID"

# 或从 daemon 日志获取
grep "device_id" "$E2E_HOME/alice-daemon.log" | head -1
```

#### 步骤 1.3: 验证函数注册

```bash
# 检查服务器日志，确认 system.register_functions 被调用
cat "$E2E_HOME/server.log" | grep "system.register_functions" | tail -3
# 预期: 看到函数注册日志
```

#### 步骤 1.4: 📘 TS Client 适配要点（阶段 1）

> TS Client 的 daemon 启动行为与 Go Client 完全一致，仅二进制路径和 device-id 默认值不同。

```bash
# TS Client 启动（使用不同的 device-id 避免冲突）
xyncra-client listen \
  --user-id alice --device-id test-device-alice-ts \
  --server ws://localhost:18080/ws \
  > "$E2E_HOME/alice-ts-daemon.log" 2>&1 &
ALICE_TS_PID=$!
sleep 2

# 验证函数注册
cat "$E2E_HOME/server.log" | grep "system.register_functions" | tail -3
# 预期: 看到 TS Client 的函数注册日志
```

**差异点**：
- `--device-id` 可选（TS 默认 SHA256(hostname)[:8]）
- 状态文件无 `xyncra.db`（TS 使用 IndexedDB 内存存储）

---

### 阶段 2: 触发 ReverseRPC 并模拟超时

#### 步骤 2.1: 创建 Agent 会话

```bash
CONV_ID=$(./bin/xyncra-client create-conversation \
  --user-id alice --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --peer-id "agent/weather-bot" | grep "ID:" | awk '{print $2}')
echo "CONV_ID=$CONV_ID"
```

#### 步骤 2.2: 发送消息触发 Agent 处理

```bash
./bin/xyncra-client send \
  --user-id alice --device-id test-device-alice \
  --server ws://localhost:18080/ws \
  --conversation-id "$CONV_ID" \
  --content "What's the weather? Call a client tool."
```

#### 步骤 2.3: 模拟客户端不响应（暂停 daemon）

```bash
# 暂停 Alice daemon 进程（SIGSTOP）
kill -STOP $ALICE_PID

# 等待 ReverseRPC 超时（默认 30s）
sleep 35
```

#### 步骤 2.4: 验证 Redis Pending Store (D-103)

```bash
PENDING_KEY="pending:alice\x00$DEVICE_ID"
redis-cli -p 16379 -n 15 GET "$PENDING_KEY"
# 预期: JSON 数组，包含超时的请求

# 或使用 KEYS 查找
redis-cli -p 16379 -n 15 KEYS "pending:*"
# 预期: 包含 pending:alice* 的 key
```

#### 步骤 2.5: 📘 TS Client 适配要点（阶段 2）

> TS Client 的 ReverseRPC 超时和 Pending Store 行为与 Go Client 完全一致。

```bash
# TS Client 创建 Agent 会话
TS_CONV_ID=$(xyncra-client create-conversation \
  --user-id alice --device-id test-device-alice-ts \
  --server ws://localhost:18080/ws \
  --peer-id "agent/weather-bot" | grep "ID:" | awk '{print $2}')

# 发送消息触发 Agent 处理
xyncra-client send \
  --user-id alice --device-id test-device-alice-ts \
  --server ws://localhost:18080/ws \
  --conversation-id "$TS_CONV_ID" \
  --content "What's the weather? Call a client tool."

# 暂停 TS daemon 进程
kill -STOP $ALICE_TS_PID
sleep 35  # 等待 ReverseRPC 超时

# 验证 Redis Pending Store
redis-cli -p 16379 -n 15 KEYS "pending:alice*"
# 预期: 包含 TS Client 的 pending 请求
```

---

### 阶段 3: 客户端重连并补发

#### 步骤 3.1: 恢复 daemon 进程

```bash
kill -CONT $ALICE_PID
sleep 2
```

#### 步骤 3.2: 手动触发 system.reconnect (D-108)

```bash
# 通过 IPC 或直接调用
# 注意：实际实现中，daemon 重连后应自动调用 system.reconnect
# 此处手动触发用于测试

# 检查 daemon 日志，确认是否自动调用了 reconnect
cat "$E2E_HOME/alice-daemon.log" | grep "system.reconnect" | tail -3
```

#### 步骤 3.3: 验证补发请求 (D-107)

```bash
# 检查服务器日志，确认补发请求
cat "$E2E_HOME/server.log" | grep "s-replay-" | tail -5
# 预期: 看到 s-replay-{uuid} 格式的请求 ID
```

#### 步骤 3.4: 验证 Redis Pending 已清理

```bash
sleep 5  # 等待补发完成

PENDING_KEY="pending:alice\x00$DEVICE_ID"
redis-cli -p 16379 -n 15 GET "$PENDING_KEY"
# 预期: (nil) 或空数组（请求已被处理并清理）
```

#### 步骤 3.5: 📘 TS Client 适配要点（阶段 3）

> TS Client 的重连和补发行为与 Go Client 完全一致。

```bash
# 恢复 TS daemon 进程
kill -CONT $ALICE_TS_PID
sleep 2

# 检查 TS daemon 日志，确认 system.reconnect
cat "$E2E_HOME/alice-ts-daemon.log" | grep "system.reconnect" | tail -3

# 验证补发请求
cat "$E2E_HOME/server.log" | grep "s-replay-" | tail -5
# 预期: 看到 TS Client 的补发请求

# 验证 Redis Pending 已清理
sleep 5
redis-cli -p 16379 -n 15 KEYS "pending:alice*"
# 预期: TS Client 的 pending 请求已被清理
```

---

### 阶段 4: 设备替换测试 (D-095)

#### 步骤 4.1: 启动第二个连接（相同 device_id）

```bash
# 使用相同的 device_id 启动新连接
./bin/xyncra-client listen \
  --user-id alice --device-id test-device-alice \
  --device-id "$DEVICE_ID" \
  --server ws://localhost:18080/ws \
  > "$E2E_HOME/alice-daemon-2.log" 2>&1 &
ALICE_PID_2=$!
sleep 2
```

#### 步骤 4.2: 验证旧连接收到 Close 4001

```bash
# 检查第一个 daemon 的日志
cat "$E2E_HOME/alice-daemon.log" | grep -i "4001\|replaced" | tail -3
# 预期: 看到 "Close frame 4001" 或 "replaced by new connection"
```

#### 步骤 4.3: 验证旧连接 pending 保留 (D-105)

```bash
# CancelDevice 不应清理 Redis Pending
PENDING_KEY="pending:alice\x00$DEVICE_ID"
redis-cli -p 16379 -n 15 GET "$PENDING_KEY"
# 预期: 如果有 pending 请求，应该仍然存在（不被清理）
```

#### 步骤 4.4: 停止第二个 daemon

```bash
./bin/xyncra-client kill --user-id alice --device-id "$DEVICE_ID"
```

#### 步骤 4.5: 📘 TS Client 适配要点（阶段 4）

> TS Client 的设备替换行为与 Go Client 完全一致。

```bash
# TS Client 设备替换测试
# 使用相同的 device-id 启动新连接
xyncra-client listen \
  --user-id alice --device-id test-device-alice-ts \
  --server ws://localhost:18080/ws \
  > "$E2E_HOME/alice-ts-daemon-2.log" 2>&1 &
ALICE_TS_PID_2=$!
sleep 2

# 验证旧 TS daemon 收到 Close 4001
cat "$E2E_HOME/alice-ts-daemon.log" | grep -i "4001\|replaced" | tail -3
# 预期: 看到 Close 4001 或 replaced 信息

# 停止第二个 TS daemon
xyncra-client kill --user-id alice --device-id test-device-alice-ts
```

---

## 7. 数据库验证汇总

### 7.1 Redis 验证命令速查

```bash
R="redis-cli -p 16379 -n 15"

# Pending 请求
$R KEYS "pending:*"
$R GET "pending:alice\x00<device-id>"

# 连接信息
$R KEYS "xyncra:conn:info:*"
$R KEYS "xyncra:conn:user:*"

# 清理
$R FLUSHDB
```

---

## 8. 通过/失败判定标准

| 阶段 | 判定条件 |
|------|---------|
| 阶段 1 | Go/TS daemon 正常启动，device_id 正确生成，函数注册成功 |
| 阶段 2 | ReverseRPC 超时后，Go/TS 请求都被持久化到 Redis Pending Store |
| 阶段 3 | 重连后 system.reconnect 触发补发，Go/TS 的 Pending 都被清理 |
| 阶段 4 | 设备替换时 Go/TS 旧连接都收到 Close 4001，Pending 不被清理 |

---

## 9. 故障排查指南

| 症状 | 可能原因 | 解决方法 |
|------|---------|---------|
| Pending Store 为空 | ReverseRPC 未超时或 fail-open | 检查服务器日志，确认超时配置 |
| 补发未触发 | daemon 未调用 system.reconnect | 检查 daemon 日志，手动触发 |
| 设备替换未生效 | device_id 不匹配 | 确保两次连接使用相同 device_id |

---

## 10. 环境清理

```bash
# Go Client daemons
./bin/xyncra-client kill --user-id alice --device-id test-device-alice
./bin/xyncra-client kill --user-id alice --device-id test-device-alice --force 2>/dev/null

# TS Client daemons（如果使用）
xyncra-client kill --user-id alice --device-id test-device-alice-ts 2>/dev/null || true
xyncra-client kill --user-id alice --device-id test-device-alice-ts --force 2>/dev/null || true

docker compose -f deploy/docker-compose.e2e.yml down

rm -rf "$E2E_HOME"
rm -rf ~/.xyncra/alice

redis-cli -p 16379 -n 15 FLUSHDB
```

---

## 11. 依赖关系说明

| 测试阶段 | 可独立执行 | 依赖 |
|---------|-----------|------|
| 阶段 1 (Daemon 启动) | ✅ | 环境准备 |
| 阶段 2 (Pending 验证) | ✅ | 阶段 1 |
| 阶段 3 (补发验证) | ✅ | 阶段 2 |
| 阶段 4 (设备替换) | ✅ | 阶段 1 |

阶段 4 可与阶段 2-3 并行执行。

---

## 12. 测试执行记录模板

```markdown
### TC-002 测试执行记录

| 字段 | 值 |
|------|-----|
| 日期 | YYYY-MM-DD |
| Git Commit | <sha> |
| 测试者 | <name> |
| 环境 | Docker E2E |
| 客户端版本 | Go / TS / 双版本 |

| 阶段 | 结果 (Go) | 结果 (TS) | 备注 |
|------|-----------|-----------|------|
| 阶段 1: Daemon 启动 | ✅ / ❌ | ✅ / ❌ | |
| 阶段 2: Pending 验证 | ✅ / ❌ | ✅ / ❌ | D-103 |
| 阶段 3: 补发验证 | ✅ / ❌ | ✅ / ❌ | D-108 |
| 阶段 4: 设备替换 | ✅ / ❌ | ✅ / ❌ | D-095 |

**发现的问题**：
1. (描述)

**结论**：PASS / FAIL
```
