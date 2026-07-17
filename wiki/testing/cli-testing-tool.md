---
last_updated: 2026-07-17
---

# CLI 测试工具

## 概述

`xyncra-client` 是 Xyncra 项目的官方 CLI 客户端，位于 `cmd/xyncra-client/`。它也是测试团队最常用的功能验证工具，支持：

- 通过 `listen` 命令启动后台守护进程（daemon）
- 发送和接收消息
- 管理会话（创建、查询、删除、恢复）
- 与 AI Agent 交互
- 本地 SQLite 数据库查询（list-conversations, get-messages, search-messages）
- IPC（Unix Domain Socket）与 daemon 通信
- 独立 WebSocket 回退模式（daemon 不可用时）

## CLI 作为测试工具

### 可用命令一览

| 命令 | 说明 | 连接方式 |
|------|------|---------|
| `listen` | 启动守护进程（daemon） | WebSocket |
| `send` | 发送消息 | IPC → WS |
| `create-conversation` | 创建 1-on-1 会话 | IPC → WS |
| `delete-conversation` | 软删除会话 | IPC → WS |
| `restore-conversation` | 恢复已删除会话 | IPC → WS |
| `list-conversations` | 列出会话（本地 DB） | 本地 |
| `get-conversation` | 查看会话详情（本地 DB） | 本地 |
| `delete-message` | 软删除消息 | IPC → WS |
| `mark-as-read` | 标记已读 | IPC → WS |
| `get-messages` | 查看消息（本地 DB） | 本地 |
| `search-messages` | 搜索消息（本地 DB） | 本地 |
| `sync-updates` | 触发全量同步 | IPC-only |
| `set-typing` | 发送正在输入状态 | IPC-only |
| `stream-text` | 发送流式文本 | IPC-only |
| `agent-resume` | 恢复 HITL 中断的 Agent | IPC-only |
| `reload-agents` | 重载 Agent 配置 | IPC-only |
| `draft` | 管理草稿（save/get/delete） | 本地 |
| `logs` | 查看日志（tail/search/stats/export/cleanup） | 本地 |
| `kill` | 停止守护进程 | 信号 |

### 基本用法

```bash
# 启动守护进程
xyncra-client listen --user-id alice --device-id phone

# 发送消息
xyncra-client send --conversation-id conv-xxx --content "Hello"

# 查看消息（本地数据库）
xyncra-client get-messages --conversation-id conv-xxx

# 同步更新（IPC-only，需要 daemon 运行中）
xyncra-client sync-updates

# 查看会话列表（本地数据库）
xyncra-client list-conversations

# 停止守护进程
xyncra-client kill --user-id alice --device-id phone
```

### 模拟多用户

```bash
# 终端 1: Alice
xyncra-client listen --user-id alice --device-id phone

# 终端 2: Bob
xyncra-client listen --user-id bob --device-id laptop

# 终端 3: Eve（观察者）
xyncra-client listen --user-id eve --device-id web
```

每个用户需要独立的终端窗口和 device-id，不同用户的 daemon 独立运行（锁文件不冲突）。

### 守护进程的模式

`listen` 命令启动一个后台守护进程，它：
- 建立 WebSocket 连接到服务器
- 在 `~/.xyncra/{user_id}/{device_id}/xyncra.sock` 上监听 IPC 请求
- 使用 `~/.xyncra/{user_id}/{device_id}/xyncra.lock` 确保单实例
- 将数据同步到本地 SQLite 数据库 `~/.xyncra/{user_id}/{device_id}/xyncra.db`

使用 `xyncra-client kill` 停止守护进程（发送 SIGTERM，支持 `--force` 发送 SIGKILL）。

### Daemon 的 IPC 通信

Daemon 启动后，CLI 子命令通过 Unix Domain Socket 与 daemon 通信：

```
~/.xyncra/{user_id}/{device_id}/xyncra.sock
```

IPC 格式为 JSON-RPC 2.0，换行符分隔：

```json
{"jsonrpc":"2.0","id":"req-001","method":"send_message","params":{"conversation_id":"...","content":"Hello"}}
{"jsonrpc":"2.0","id":"req-001","result":{"message":{"message_id":1,...}}}
```

### 独立 WebSocket 回退

部分命令（send, create-conversation, delete-conversation, restore-conversation, delete-message, mark-as-read）支持 IPC 优先、独立 WebSocket 回退的策略（D-032）。当 daemon 未运行时，这些命令会自动建立临时 WebSocket 连接完成操作：

```bash
# daemon 未运行时，send 自动回退到独立 WebSocket 连接
xyncra-client send --conversation-id conv-xxx --content "hello"
# 输出：Message sent.
```

## 场景模拟

### Agent 交互场景

```bash
# 1. 启动 daemon
xyncra-client listen --user-id alice --device-id phone

# 2. 创建与 peer 的会话
xyncra-client create-conversation --peer-id agent/test-bot

# 3. 发送消息触发 Agent
xyncra-client send --conversation-id conv-xxx --content "帮我查一下北京的天气"

# 4. 观察回复
# Agent 回复会通过 listen 的标准输出自动显示
# 也可以手动拉取
xyncra-client get-messages --conversation-id conv-xxx

# 5. 查看会话详情
xyncra-client get-conversation --conversation-id conv-xxx
# 返回包含 Agent 状态字段（agent_status）
```

### HITL 模拟

当 Agent 状态变为 `asking_user` 时，`listen` 的 daemon 会自动输出 HITL 信息：

```
[conversation] id=conv-xxx title="..."
[hitl] conv=conv-xxx agent=agent/test-bot checkpoint_id=cp-xxx
  [1] interrupt_id=int-xxx question="我需要在发送邮件之前获得您的确认" (pending)
```

手动测试时，可以使用以下命令恢复 Agent 执行：

```bash
# 批准：向 Agent 提供回答以恢复执行
xyncra-client agent-resume \
    --conversation-id conv-xxx \
    --checkpoint-id cp-xxx \
    --interrupt-id int-xxx \
    --answer "yes, please proceed" \
    --agent-id agent/test-bot
```

`agent-resume` 是 IPC-only 命令（D-085, D-114），需要 daemon 运行中。

### 弱网场景模拟

```bash
# 使用 tc 命令模拟网络延迟（需要 root）
sudo tc qdisc add dev lo root netem delay 200ms 50ms loss 10%

# 运行测试
xyncra-client listen --user-id alice --device-id phone

# 清理
sudo tc qdisc del dev lo root

# 模拟 Server 下线
kill -STOP $(pgrep xyncra-server)  # 暂停 Server
# ... 测试断连行为（daemon 应自动重连）...
kill -CONT $(pgrep xyncra-server)  # 恢复 Server
```

### 并发场景

```bash
# 批量启动多个 daemon 实例（不同 user-id / device-id）
for i in $(seq 1 5); do
    xyncra-client listen --user-id "user-$i" --device-id "dev-$i" &
done

# 批量发送消息
for i in $(seq 1 10); do
    xyncra-client send --conversation-id conv-xxx --content "Message $i"
done
```

## 自动化测试使用

CLI 测试工具也用于自动化 E2E 测试。测试位于 `internal/cli/e2e/`：

```bash
# 运行 CLI E2E 测试（Go 测试框架）
go test -v -count=1 ./internal/cli/e2e/ -timeout 120s

# 运行指定优先级的测试
go test -v -count=1 -run 'TestListenDaemon|TestKillNormal' ./internal/cli/e2e/ -timeout 120s
```

### E2E 测试文件结构

| 文件 | 说明 |
|------|------|
| `internal/cli/e2e/cli_e2e_test.go` | P0 基础场景（启动、停止、IPC、同步） |
| `internal/cli/e2e/cli_e2e_p0_test.go` | P0 进阶场景（断连、kill、多设备、跨实例同步） |
| `internal/cli/e2e/cli_e2e_p1_test.go` | P1 场景 |
| `internal/cli/e2e/cli_e2e_p2_test.go` | P2 场景 |
| `internal/cli/e2e/agent_e2e_test.go` | Agent 交互场景 |
| `internal/cli/e2e/weaknet_test.go` | 弱网场景 |
| `internal/cli/e2e/helpers_test.go` | 测试辅助工具 |

### E2E 测试的 IPC 调用模式

自动化测试中，通过 Unix Socket 向 daemon 发送 JSON-RPC 2.0 请求并使用辅助函数验证响应：

```go
// 启动 daemon
dp := startDaemon(t, env, userID, deviceID)
defer requireStopDaemon(t, dp)

// 通过 runCLI 执行命令
result := runCLI(t, env,
    "--user-id", userID,
    "--device-id", deviceID,
    "send",
    "--conversation-id", convID,
    "--content", "test message",
)
requireExitCode(t, result, 0)

// 直接通过 IPC 调用
resp := ipcCall(t, dp.socketPath, "sync_updates", nil)
require.Nil(t, resp.Error)
```

辅助函数位于 `internal/cli/e2e/helpers_test.go`：

- `startDaemon(t, env, userID, deviceID)` — 启动 daemon 并等待 socket 就绪
- `requireStopDaemon(t, dp)` — 发送 SIGTERM 等待退出
- `runCLI(t, env, args...)` — 执行 CLI 命令并返回 stdout/stderr/exit code
- `ipcCall(t, socketPath, method, params)` — 通过 IPC 发送 JSON-RPC 请求
- `waitForSocket(ctx, path)` — 轮询等待 Unix socket 可连接
- `seedLocalDBFull(t, dbPath, convs, msgs)` — 直接写本地数据库准备数据

## 安全注意事项

### IPC 通道安全

| 项目 | 说明 |
|------|------|
| Socket 权限 | Unix socket 权限 0600，仅所有者可访问 |
| 锁文件 | 使用 flock 确保同一 (user, device) 只能有一个 daemon |
| 数据隔离 | 不同 user/device 使用独立的目录和数据库 |

### 生产环境禁止

```
xyncra-client listen        # 正常连接不受影响
xyncra-client kill          # 正常停止 daemon
xyncra-client agent-resume  # 正常恢复 Agent
```

### 审计

所有 RPC 调用和通知日志记录到本地 SQLite 数据库中，可通过 `xyncra-client logs` 命令查看：

```bash
# 查看最近 RPC 调用
xyncra-client logs tail --type rpc --limit 20

# 查看最近通知
xyncra-client logs tail --type notifications --limit 20

# 搜索 RPC 错误
xyncra-client logs search --type rpc --error --from 1h

# 统计 RPC 调用量
xyncra-client logs stats --since 24h

# 导出日志
xyncra-client logs export --type rpc --format csv -o /tmp/rpc-logs.csv

# 清理旧日志
xyncra-client logs cleanup --retain 7d
```
