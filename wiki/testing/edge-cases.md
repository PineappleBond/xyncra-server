# 边缘场景测试

## 概述

本文档记录 Xyncra 系统的边缘场景测试覆盖，包括网络异常、服务故障、并发竞态、用户异常行为等情况。

边缘场景通过以下方式覆盖：

1. **自动化 E2E 测试**（`internal/e2e/`）— 弱网、断连、并发
2. **Mock LLM 故障注入**（`llmWeakNetConfig`）— LLM 超时、中断、限流
3. **手动测试**（`docs/testing/manual/`）— 真实网络环境、长时间运行

## 场景目录

| 场景 | 自动化覆盖 | 手动覆盖 | 优先级 |
|------|:---------:|:---------:|:------:|
| 网络断连/重连 | ✓ | ✓ | P0 |
| 服务重启 | 部分 | ✓ | P0 |
| 消息突发 | ✓ | — | P1 |
| HITL 超时 | ✓ | ✓ | P0 |
| HITL 用户离线 | ✓ | — | P1 |
| 多设备竞争 | ✓ | ✓ | P1 |
| 子 Agent HITL 失败 | ✓ | — | P1 |
| Agent 执行超时 | ✓ | — | P1 |
| LLM 返回错误 | ✓ | — | P0 |
| LLM 流式中断 | ✓ | — | P1 |
| LLM 限流 | ✓ | — | P2 |
| LLM 黑盒超时 | ✓ | — | P1 |

## 场景 1: 网络断连/重连

### 场景描述

客户端与服务端之间的 WebSocket 连接由于网络波动断开，客户端在恢复网络后重新连接，需要确保消息不丢失。

### 自动化测试覆盖

**文件**: `internal/e2e/fullchain_reconnect_e2e_test.go`

**测试 1: Agent 处理中断连**

```go
func TestFullChainReconnect_DuringAgentProcessing(t *testing.T) {
    // 1. 用户连接，发送消息到 Agent
    // 2. Agent 开始处理（Mock LLM 带 2s 延迟）
    // 3. 客户端断连（conn.Close）
    // 4. Agent 完成处理，消息持久化到 DB
    // 5. 客户端重连（新连接，相同 userID）
    // 6. sync_updates 拉取 Agent 回复
    // 验证：断连期间 Agent 照常处理
    // 验证：重连后通过 sync 获取所有消息
}
```

**测试 2: 多轮断连/重连**

```go
func TestFullChainReconnect_MultipleCycles(t *testing.T) {
    // 5 轮断连-重连，每轮发送并验证 3 条消息
    // 使用 threeLayerCheck 验证 Server DB + Redis
    // 验证：消息不丢失，seq 连续
}
```

**测试 3: 断连期间消息送达**

```go
// 用户 A 与用户 B 对话
// A 在线，B 断连
// A 发送 5 条消息
// B 重连，sync_updates 拉取全部 5 条
```

### 实现机制

```
客户端断连 → Server 检测到 ConnError
  → RedisConnectionStore.Remove(connID)
  → 30s TTL 后自动清理（如未主动 Remove）

客户端重连 → 建立新 WebSocket
  → 新 connID 注册到 ConnectionStore
  → client.send_message → 正常流程
  → sync_updates 拉取离线消息
```

### 手动测试步骤

```bash
# 终端 1: Alice 连接
xyncra-client connect --user-id alice --device-id phone

# 终端 2: Bob 连接
xyncra-client connect --user-id bob --device-id laptop

# 模拟断连：暂停服务器进程
kill -STOP $(pgrep xyncra-server)

# Alice 发消息（此时会缓存或失败）
xyncra-client send --conversation-id conv-xxx --content "你在吗？"

# 恢复服务器
kill -CONT $(pgrep xyncra-server)

# Bob 重连，检查消息
xyncra-client sync-updates --after-seq 0
```

### 已知限制

- 断连期间客户端发送的消息可能因 write buffer 满而丢失
- `wsConn.recv` 超时不损坏连接（使用 channel 包装）
- Server 端 writePump 会在断连后丢弃无法投递的消息

## 场景 2: 服务重启

### 场景描述

Xyncra Server 在运行中崩溃或重启，验证：
- 持久化数据不丢失（SQLite 文件）
- 连接信息丢失可重建
- Agent 处理中的任务状态

### 自动化测试覆盖

`internal/e2e/agent_reload_test.go`:

```go
func TestAgentReload_AE_RELOAD_001(t *testing.T) {
    // 验证 Agent 配置热加载
    // 1. 加载初始 Agent 配置
    // 2. 调用 reload_agents RPC
    // 3. 验证新配置生效
}
```

### 服务重启恢复流程

```
Server 重启 → 初始化组件
  └── SQLite（持久化） → 数据完整
  └── Redis（内存） → 连接信息丢失
  └── Agent Registry → 重新加载配置

客户端行为：
  ├── 感知断连 → 重试 connect
  ├── sync_updates 拉取离线消息
  └── agent_execute payload → 从 MQ 重试
```

### 手动测试步骤

```bash
# 1. 准备工作
xyncra-client daemon start

# 2. 发送消息
xyncra-client send --conversation-id conv-xxx --content "消息 1"
xyncra-client send --conversation-id conv-xxx --content "消息 2"

# 3. 重启 Server
pkill xyncra-server && sleep 2 && xyncra-server --port 18080

# 4. 验证数据
xyncra-client get-messages --conversation-id conv-xxx
# 预期：消息 1 和消息 2 都在

# 5. 继续发送
xyncra-client send --conversation-id conv-xxx --content "消息 3"
# 预期：发送成功
```

### Agent 处理中的任务

当 Server 重启时，正在处理的 Agent 任务会丢失。恢复策略：

- **SQLite**: 用户消息已持久化，不会丢失
- **Redis 锁**: 锁自动过期（TTL），后续消息可建立新锁
- **Agent 回复**: 不会恢复（任务丢失），用户需重新发送消息
- **MQ 任务**: Asynq 有重试机制，重启后可能重试

### 数据持久性矩阵

| 数据类型 | 存储 | 重启后 | 恢复方式 |
|---------|------|--------|---------|
| 消息 | SQLite | ✓ 保留 | 直接可用 |
| 会话 | SQLite | ✓ 保留 | 直接可用 |
| 用户更新 | SQLite | ✓ 保留 | sync_updates |
| 连接信息 | Redis | ✗ 丢失 | 客户端重连重建 |
| Agent 锁 | Redis | ✗ 丢失 | TTL 自动过期 |
| Agent 检查点 | Redis | ✗ 丢失 | HITL 重新触发 |
| 待处理任务 | Redis | ✗ 丢失 | Asynq 重试 |
| Agent 配置 | 文件 | ✓ 保留 | 重启重新加载 |

## 场景 3: 并发消息竞态

### 场景描述

多用户或多设备同时发送消息到同一会话，验证消息顺序正确性、幂等性锁、不出现数据损坏。

### 自动化测试覆盖

**文件**: `internal/e2e/fullchain_concurrent_e2e_test.go`

**测试 1: 多用户同 Agent（无锁竞争）**

```go
func TestFullChainConcurrent_MultiUserSameAgent(t *testing.T) {
    // 3 用户, 各自与 agent/test-bot 有独立会话
    // 并发触发 executor.Execute
    // 验证：全部完成，无死锁
    // 验证：Redis 锁全部释放
}
```

**测试 2: 单用户多 Agent（无锁竞争）**

```go
func TestFullChainConcurrent_SingleUserMultiAgent(t *testing.T) {
    // 1 用户, 与 2 个 Agent 有不同会话
    // 并发发送到不同 Agent
    // 验证：两个 Agent 都回复
    // 验证：消息互不干扰
}
```

**测试 3: 压力测试**

```go
func TestFullChainConcurrent_Stress(t *testing.T) {
    // 10 用户 x 10 轮 = 100 次 Agent 调用
    // 验证：全部完成
    // 验证：Server DB 消息数 = 100 用户消息 + 100 Agent 回复
    // 验证：Redis 锁全部释放
    // 验证：无 goroutine 泄漏
}
```

**文件**: `internal/e2e/fullchain_input_boundary_e2e_test.go`

**测试 4: 消息突发**

```go
func TestFullChainBoundary_MessageBurst(t *testing.T) {
    // 10 条消息在 1 秒内发送到同一会话
    // 验证：10 条用户消息全部持久化
    // 验证：Agent 按序处理（锁序列化）
    // 验证：Redis 锁最终释放
}
```

**文件**: `internal/e2e/concurrent_test.go`

```go
func TestConcurrentSendMessage(t *testing.T) {
    // 多个用户同时发消息到同一会话
    // 使用 sync.WaitGroup 协调
    // 验证：消息顺序正确
    // 验证：无数据损坏
}
```

### 锁机制

```go
// 每个会话一把 Redis 锁
// Redis SETNX + Lua 释放
// 锁 Key: agent:lock:{conversationID}
// TTL: 默认 30 秒（自动过期防死锁）
// 同一会话串行处理（D-075）
// 不同会话并行处理
```

### 竞态条件识别

| 竞态类型 | 检测方式 | 是否已修复 |
|---------|---------|:---------:|
| send_message TOCTOU | unique constraint violation → -300 而非 duplicate=true | ❌ 已知 Bug |
| 并发心跳 TTL 覆盖 | ConnectionStore.Refresh 原子操作 | ✅ |
| 消息顺序反转 | MessageID 在单 goroutine 分配 | ✅ |
| 会话创建重复 | ConversationStore.Create 唯一索引 | ✅ |

## 场景 4: HITL 超时/用户离线

### 场景描述

Agent 向用户发起 HITL 请求后，用户未在超时时间内响应（或用户已离线），Agent 需要自动取消当前操作。

### 自动化测试覆盖

**文件**: `internal/e2e/agent_hitl_test.go`

**测试 1: HITL 超时自动取消**

```go
func TestAgentHITL_AE_HITL_004_Timeout(t *testing.T) {
    // 1. 用户发送消息触发 HITL
    // 2. Agent 创建检查点
    // 3. 等待 HITL 超时（Mock LLM 短超时）
    // 4. 验证：Agent 自动取消
    // 5. 验证：错误消息持久化
    // 6. 验证：Redis 检查点被清理
}
```

**测试 2: HITL 用户离线**

```go
func TestAgentHITL_AE_HITL_005_UserOffline(t *testing.T) {
    // 1. 用户发送消息触发 HITL
    // 2. 用户断连
    // 3. 等待超时
    // 4. 验证：Agent 取消处理
    // 5. 用户重连后检查错误消息
}
```

### 文件: `internal/e2e/agent_hitl_resilience_test.go`

```go
func TestAgentHITL_Retry(t *testing.T) {
    // 重复调用 triggerAgentResume 验证幂等性
}

func TestAgentHITL_ConcurrentResume(t *testing.T) {
    // 多 goroutine 同时恢复同一检查点
    // 验证：只有一个成功
}
```

### 超时配置

```go
// Agent 默认超时
agent.WithTotalTimeout(30 * time.Second)

// HITL 等待超时（在 Agent 内部配置）
hittl_timeout: 60s  // Agent .md 配置

// Mock LLM 响应延迟
llmWeakNetConfig{
    ResponseDelay: 2 * time.Second,  // 模拟慢响应
    BlackHoleTimeout: true,          // 模拟无响应
}
```

### 手动测试步骤

```bash
# 1. 发送需要 HITL 的消息
xyncra-client send --conversation-id conv-xxx --content "帮我发送一封邮件"

# 2. 等待 HITL 请求出现
# 预期输出：
# 🤖 Agent: 我需要您的确认才能发送邮件
# ┌─────────────────────────────────┐
# │  [Y] 批准    [N] 拒绝    [T] 超时│
# └─────────────────────────────────┘

# 3. 不做任何操作，等待超时
# 预期：收到 Agent 超时取消消息
# "操作已超时取消"

# 4. 重连后验证错误消息
xyncra-client get-messages --conversation-id conv-xxx
```

## 场景 5: 多设备竞争

### 场景描述

同一用户在多台设备上登录，Agent 向所有设备发送 HITL 请求，一台设备响应后其他设备的请求自动取消。验证多设备下的 HITL 一致性。

### 自动化测试覆盖

**文件**: `internal/e2e/agent_hitl_e2e_test.go`

```go
func TestAgentHITL_E2E_AE_HITL_E2E_005_MultipleDevices(t *testing.T) {
    // 1. 同一用户在设备 A、B 上连接
    // 2. 发送触发 HITL 的消息
    // 3. 设备 A 批准，设备 B 拒绝
    // 4. 验证：第一个响应生效
    // 5. 验证：第二个响应被忽略
    // 6. 验证：Agent 根据批准继续处理
}
```

### 多设备 HITL 竞争逻辑

```
Agent 创建检查点 → Broadcast to ALL devices of user
  ├── Device A (phone): 响应 approved: true
  ├── Device B (laptop): 响应 approved: false
  └── 竞争结果 → 先到达的胜出
      ├── Device A 先到 → Agent 继续处理
      └── Device B 先到 → Agent 取消
```

### 手动测试步骤

```bash
# 终端 1: 手机端
xyncra-client connect --user-id alice --device-id phone

# 终端 2: 笔记本端
xyncra-client connect --user-id alice --device-id laptop

# 终端 3: 发送 HITL 触发消息
xyncra-client send --conversation-id conv-xxx --content "帮我操作"

# 手机端批准
# 笔记本端看到 HITL 请求被取消
```

## 场景 6: 子 Agent HITL 失败

### 场景描述

当子 Agent 在处理过程中触发 HITL（需要用户确认），但用户无法响应或响应被拒绝，验证父 Agent 是否能正确处理子 Agent 的 HITL 失败。

### 自动化测试覆盖

**文件**: `internal/e2e/agent_subagent_test.go`

```go
func TestAgentSub_AE_SUB_006_SubAgentHITLFails(t *testing.T) {
    // 1. 父 Agent 委派任务给子 Agent
    // 2. 子 Agent 触发 HITL（需要用户确认）
    // 3. 用户拒绝或超时
    // 4. 验证：子 Agent 错误传播到父 Agent
    // 5. 验证：父 Agent 生成合理错误消息
}
```

### 子 Agent HITL 失败处理

```
用户消息 → 父 Agent → 委派给子 Agent
  → 子 Agent 触发 HITL
  → HITL 超时/拒绝
  → 子 Agent 返回错误
  → 父 Agent 收到错误
  → 父 Agent 回复用户："子任务未能完成：用户未响应确认请求"
```

## 场景 7: Agent 执行超时

### 自动化测试覆盖

**文件**: `internal/e2e/agent_error_test.go`

```go
func TestAgentErr_AE_ERR_006_Timeout(t *testing.T) {
    env := setupAgentE2E(t, agent.WithTotalTimeout(1*time.Second))

    // Mock LLM 带 10s 延迟
    env.mockLLM.SetWeakNetConfig(llmWeakNetConfig{
        ResponseDelay: 10 * time.Second,
    })

    // 发送消息 → Agent 执行超时
    // 验证：错误消息包含"回复超时"
}
```

### 超时层次

```
客户端等待响应（readResponse）: 5s normal / 30s agent
  └── Agent 总超时（WithTotalTimeout）: 30s default / 1s test
      └── LLM 请求超时（HTTP client）: 10s
          └── Token 生成超时（model params）: 60s
```

## 场景 8: LLM 流式中断

### 自动化测试覆盖

**文件**: `internal/e2e/agent_weaknet_test.go`

```go
func TestAgentWeakNet_StreamDisconnect(t *testing.T) {
    env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
        StreamDisconnectAfter: 3,  // 3 个 chunk 后断开
    })

    // 发送消息 → Agent 回复在中途断开
    // 验证：Agent 生成错误消息
    // 验证：StreamBridge 正确重置
}
```

## 边界输入测试

以下边界输入场景在 `internal/e2e/agent_edge_test.go` 中覆盖：

| 输入类型 | 示例 | 预期行为 |
|---------|------|---------|
| 超长文本 | 10000+ 字符 "aaaa..." | Agent 回复（可能被截断） |
| 空文本 | `""` | 拒绝，返回"抱歉无法处理空消息" |
| Emoji | 😀🚀🎉 | 正常处理 |
| CJK 字符 | 中文、日本語、한국어 | 正常处理 |
| RTL 文本 | العربية | 正常处理 |
| Null 字节 | `\x00` | 不截断，正常处理 |
| HTML 注入 | `<script>alert(1)</script>` | 正常处理，不执行 |
| SQL 注入 | `' OR 1=1--` | 正常处理，参数化查询 |
| 混合内容 | 中英文 + emoji + 数字 | 正常处理 |
