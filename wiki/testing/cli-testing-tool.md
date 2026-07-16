# CLI 测试工具

## 概述

`xyncra-client` 是 Xyncra 项目的官方 CLI 客户端，位于 `cmd/xyncra-client/`。它也是测试团队最常用的功能验证工具，支持：

- 连接 Xyncra Server（WebSocket）
- 发送和接收消息
- 管理会话（创建、查询、删除）
- 与 AI Agent 交互
- 离线模式（daemon 后台运行）
- 测试后门（仅开发模式可用）

## CLI 作为测试工具

### 基本用法

```bash
# 直接连接模式
xyncra-client connect --user-id alice --device-id phone

# Daemon 模式（后台守护进程）
xyncra-client daemon start
xyncra-client daemon status

# 发送消息
xyncra-client send --conversation-id conv-xxx --content "Hello"

# 查看消息
xyncra-client get-messages --conversation-id conv-xxx

# 同步更新
xyncra-client sync-updates --after-seq 0 --limit 100

# 查看会话列表
xyncra-client list-conversations
```

### 模拟多用户

```bash
# 终端 1: Alice
xyncra-client connect --user-id alice --device-id phone

# 终端 2: Bob
xyncra-client connect --user-id bob --device-id laptop

# 终端 3: Eve（观察者）
xyncra-client connect --user-id eve --device-id web
```

每个用户需要独立的终端窗口，或者使用 daemon 模式在后台运行。

### Daemon 模式的 IPC 通信

Daemon 模式启动后，CLI 与 daemon 通过 Unix socket (Linux/macOS) 或命名管道 (Windows) 通信：

```
/var/run/xyncra-cli.sock  (默认路径)
```

IPC 格式为 JSON 行协议：

```json
{"type":"request","id":"req-001","method":"send_message","params":{"conversation_id":"...","content":"Hello"}}
{"type":"response","id":"req-001","code":0,"data":{...}}
{"type":"push","data":{"updates":[...]}}
```

## 场景模拟

### Agent 交互场景

```bash
# 1. 启动 daemon
xyncra-client daemon start

# 2. 创建与 Agent 的会话
xyncra-client create-conversation --user-id alice --with-agent test-bot

# 3. 发送消息触发 Agent
xyncra-client send --conversation-id conv-xxx --content "帮我查一下北京的天气"

# 4. 观察回复
# Agent 回复会通过 push 自动显示
# 也可以手动拉取
xyncra-client get-messages --conversation-id conv-xxx

# 5. 查看 Agent 状态
xyncra-client get-conversation --conversation-id conv-xxx
# 返回包含 agent_status 字段
```

### HITL 模拟

当 Agent 发送 `hitl.request_approval` 请求时，CLI 会自动显示：

```
🤖 Agent: 我需要在发送邮件之前获得您的确认
   动作: send_email
   详情: 发送确认邮件到 user@example.com
   ┌─────────────────────────────────┐
   │  [Y] 批准    [N] 拒绝    [T] 超时│
   └─────────────────────────────────┘
```

手动测试时，可以使用以下命令模拟 HITL 响应：

```bash
# 批准
xyncra-client hitl-respond --checkpoint-id cp-xxx --approved true

# 拒绝
xyncra-client hitl-respond --checkpoint-id cp-xxx --approved false
```

### 弱网场景模拟

```bash
# 使用 tc 命令模拟网络延迟（需要 root）
sudo tc qdisc add dev lo root netem delay 200ms 50ms loss 10%

# 运行测试
xyncra-client connect --user-id alice --device-id phone

# 清理
sudo tc qdisc del dev lo root

# 模拟 Server 下线
kill -STOP $(pgrep xyncra-server)  # 暂停 Server
# ... 测试断连行为 ...
kill -CONT $(pgrep xyncra-server)  # 恢复 Server
```

### 并发场景

```bash
# 批量启动多个 daemon 实例
for i in $(seq 1 5); do
    xyncra-client daemon start --port $((18080 + i)) --user-id "user-$i" &
done

# 批量发送消息
for i in $(seq 1 10); do
    xyncra-client send --conversation-id conv-xxx --content "Message $i"
done
```

## 测试后门

### 设计原则

测试后门遵循以下安全原则：

1. **编译时禁用**：测试后门通过 `debug` 构建标签控制，生产构建不含后门代码
2. **最简权限**：后门只能读取/操作测试数据，不能影响生产数据
3. **审计日志**：所有后门操作记录到审计日志
4. **环境检查**：运行时会检查 `XYNCRA_DEBUG` 环境变量，未设置时拒绝执行

### 构建标签

```bash
# 生产构建（不含测试后门）
go build -o xyncra-client ./cmd/xyncra-client/

# 开发/测试构建（含测试后门）
go build -tags debug -o xyncra-client-debug ./cmd/xyncra-client/
```

### 可用的测试后门命令

以下命令仅在 `debug` 构建标签下可用：

| 命令 | 用途 | 生产环境可用？ |
|------|------|:------------:|
| `xyncra-client debug flush-redis` | 清空 Redis 测试数据 | ❌ |
| `xyncra-client debug inject-message` | 注入模拟消息到 DB | ❌ |
| `xyncra-client debug simulate-disconnect` | 触发 Agent 感知的断连事件 | ❌ |
| `xyncra-client debug force-agent-process` | 强制触发 Agent 处理 | ❌ |
| `xyncra-client debug mock-llm-response` | 设置 Mock LLM 的响应内容 | ❌ |
| `xyncra-client debug list-agent-configs` | 列出当前加载的 Agent 配置 | ❌ |
| `xyncra-client debug reload-agents` | 重新加载 Agent 配置 | ❌ |

### debug flush-redis

```bash
# 清空 Redis DB 15（测试专用）
xyncra-client debug flush-redis --db 15

# 仅清空指定前缀
xyncra-client debug flush-redis --prefix "e2e:test-"
```

**安全措施**：
- 默认拒绝生产数据库（DB 0-3），除非显式指定 `--force`
- 记录操作到系统日志
- 不自动补全或 Tab 提示

### debug inject-message

```bash
# 向会话注入模拟消息
xyncra-client debug inject-message \
    --conversation-id conv-xxx \
    --sender-id agent/test-bot \
    --content "这是一条测试消息" \
    --type text

# 注入批量消息
xyncra-client debug inject-message \
    --conversation-id conv-xxx \
    --sender-id alice \
    --content-file /tmp/messages.json
```

**安全措施**：
- 仅接受 `sender-id` 以 `test-` 开头的注入
- 记录注入的完整 payload 到审计日志

### debug simulate-disconnect

```bash
# 让 Server 认为某个用户已断连（但实际连接还在）
xyncra-client debug simulate-disconnect --user-id alice --device-id phone
```

**安全措施**：
- 仅对当前进程管理的连接生效
- 在 30 秒后自动恢复连接状态
- 记录到审计日志

### debug force-agent-process

```bash
# 强制触发 Agent 处理指定消息
xyncra-client debug force-agent-process \
    --message-id msg-xxx \
    --conversation-id conv-xxx \
    --agent-id agent/test-bot
```

**安全措施**：
- 要求消息发送者不是 `agent/` 前缀
- 检查 Agent 配置存在性
- 不可跳过幂等性检查

### debug mock-llm-response

```bash
# 设置 Mock LLM 响应
xyncra-client debug mock-llm-response \
    --pattern "hello" \
    --response "Hello from test!"

# 设置 Tool Call 响应
xyncra-client debug mock-llm-response \
    --pattern "tool_weather" \
    --tool-name "get_weather" \
    --tool-args '{"location":"Beijing"}'

# 设置错误响应
xyncra-client debug mock-llm-response \
    --pattern "error_trigger" \
    --http-status 500
```

**安全措施**：
- 仅当 Server 以 `--mock-llm` 模式启动时生效
- 每 5 分钟自动重置为默认配置
- 不持久化到磁盘

### debug list-agent-configs

```bash
# 列出所有 Agent 配置
xyncra-client debug list-agent-configs

# 输出示例：
# ID: test-bot | Name: Test Bot | Model: gpt-4
# ID: tool-bot | Name: Tool Bot | Model: gpt-4 | Tools: [get_weather, get_current_time]
```

**安全措施**：
- 不显示 API Key（显示 `***`）
- 不显示 `BaseURL` 中的密钥参数

### debug reload-agents

```bash
# 从磁盘重新加载 Agent 配置
xyncra-client debug reload-agents

# 从指定目录加载
xyncra-client debug reload-agents --dir /tmp/test-agents
```

## 自动化测试使用

CLI 测试工具也用于自动化 E2E 测试。测试脚本位于 `internal/cli/e2e/`：

```bash
# 运行 CLI E2E 测试（Go 测试框架）
go test -v -count=1 ./internal/cli/e2e/ -timeout 120s

# 运行 CLI 子代理 E2E（手动脚本）
bash docs/testing/cli-e2e-p0-scenarios.sh
```

### CLI 测试的 wsConn 包装器

自动化测试中，CLI 的 WebSocket 连接使用 `wsConn` 包装器（`e2e_test.go`），它提供：

- **channel 消息泵**：后台 goroutine 持续读取 WebSocket
- **超时安全的 recv**：通过 select + timer 实现不损坏连接的读取
- **自动清理**：Close/stop 方法确保 goroutine 退出

```go
conn := connectClient(t, env.addr, userID, deviceID)
defer conn.Close()

// 发送请求
sendRequest(t, conn, "req-1", "send_message", params)

// 等待响应（5 秒超时）
resp := readResponse(t, conn, 5*time.Second)

// 等待推送更新
updates := waitForUpdate(t, conn, 10*time.Second)
```

## 安全注意事项

### 后门安全

| 后门 | 风险 | 缓解措施 |
|------|------|---------|
| flush-redis | 数据丢失 | 仅限 DB 15，默认拒绝生产 DB |
| inject-message | 数据污染 | sender-id 必须以 `test-` 开头 |
| force-agent-process | 资源占用 | 不可跳过幂等性检查 |
| simulate-disconnect | 连接状态不一致 | 30 秒自动恢复 |

### 生产环境禁止

```
xyncra-client debug *           # debug 构建标签不存在
xyncra-client daemon start      # 无 --mock-llm 选项
xyncra-client connect           # 正常连接不受影响
```

### 审计

所有后门操作记录到 `~/.xyncra/debug-audit.log`：

```
2026-07-16T10:30:00 [DEBUG-AUDIT] flush-redis --db 15 --user chujun
2026-07-16T10:35:00 [DEBUG-AUDIT] inject-message --conv conv-xxx --sender test-bot
```
