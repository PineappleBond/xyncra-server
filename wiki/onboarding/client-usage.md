# 客户端使用指南

> `xyncra-client` 是一个全功能的 CLI 客户端，支持守护进程模式（IPC）和独立模式，
> 可在不编写任何代码的情况下与 Xyncra 服务器交互。

---

## 目录

- [架构概览](#架构概览)
- [守护进程模式 vs 独立模式](#守护进程模式-vs-独立模式)
- [命令参考](#命令参考)
- [消息发送与接收](#消息发送与接收)
- [Agent 交互](#agent-交互)
- [测试与调试](#测试与调试)
- [本地数据查询](#本地数据查询)
- [内置函数（ReverseRPC）](#内置函数-reverserpc)
- [草稿功能](#草稿功能)
- [故障排查](#故障排查)

---

## 架构概览

```
                  ┌──────────────────────────────────────────┐
                  │            xyncra-client                  │
                  │                                            │
  ┌─────────┐     │  ┌──────────────────┐  ┌──────────────┐  │     ┌──────────┐
  │  CLI    │─────│─▶│   IPC Server     │  │ XyncraClient │──│────▶│  Xyncra  │
  │ Command │     │  │  (Unix Socket)   │  │  (Go SDK)    │  │     │  Server   │
  └─────────┘     │  └──────────────────┘  └──────┬───────┘  │     └──────────┘
                  │                                │           │
                  │                         ┌──────▼───────┐  │
                  │                         │  WebSocket   │  │
                  │                         │  Connection  │  │
                  │                         └──────────────┘  │
                  │                                            │
                  │  ┌──────────────────┐  ┌──────────────┐   │
                  │  │   Local SQLite   │  │  Update      │   │
                  │  │   (offline DB)   │  │  Handler     │   │
                  │  └──────────────────┘  └──────────────┘   │
                  └──────────────────────────────────────────┘
```

守护进程架构：

- **CLI Command**：用户输入的命令，通过 Unix Socket IPC 转发给守护进程
- **IPC Server**：监听 Unix Socket，处理命令转发和结果返回
- **XyncraClient**：Go SDK，管理 WebSocket 连接、数据同步、RPC 调用
- **Local SQLite**：客户端本地数据库，支持离线数据查询
- **Update Handler**：处理服务器推送的实时更新

---

## 守护进程模式 vs 独立模式

### 守护进程模式（推荐）

守护进程维护一个持久的 WebSocket 连接，后台自动同步数据。

```bash
# 启动守护进程
xyncra-client listen --user-id alice --device-id macbook
```

其他命令通过 IPC 与守护进程通信：

```bash
xyncra-client send --conversation-id xxx --content "Hello"
xyncra-client list-conversations
xyncra-client get-messages --conversation-id xxx
```

### 独立模式（无守护进程）

当守护进程未运行时，部分命令会自动降级为独立模式——直接建立一个临时
WebSocket 连接执行操作后立即断开（D-032）：

```bash
# 即使没有提前启动 listen，send 命令也会尝试独立连接
xyncra-client send --user-id alice --device-id macbook \
  --conversation-id xxx --content "Hello"
```

**注意**：独立模式下不会收到实时推送，也不会自动同步。推荐始终运行守护进程。

---

## 命令参考

### 守护进程命令

#### listen

启动守护进程，维护持久的 WebSocket 连接。

```bash
xyncra-client listen [flags]
```

| 标志 | 说明 |
|------|------|
| `--user-id` / `-u` | 用户 ID（必填） |
| `--device-id` | 设备 ID（必填） |
| `--server` / `-s` | 服务器 URL（默认 `ws://localhost:8080/ws`） |
| `--db-path` | 本地数据库路径 |
| `--log-dir` | 日志目录 |
| `--device-info` | 设备元信息 JSON（如 `'{"name":"MacBook","os":"darwin"}'`） |

守护进程启动后：

- 建立 WebSocket 连接并注册内置函数
- 启动 IPC 服务器监听 Unix Socket
- 自动处理重连（无限重试，D-044）
- 接收实时推送更新并显示在终端
- 在 `~/.xyncra/{user}/{device}/` 下创建进程锁文件

```bash
# 启动示例
xyncra-client listen \
  --user-id alice \
  --device-id macbook-pro \
  --server ws://10.0.0.5:8080/ws \
  --device-info '{"name":"My MacBook","os":"darwin","version":"14.5"}'
```

### 会话命令

#### create-conversation

创建 1-on-1 会话（find-or-create 幂等）。

```bash
xyncra-client create-conversation --user-id alice --peer-id bob
```

| 标志 | 说明 |
|------|------|
| `--peer-id` | 对方用户 ID |
| `--title` | 会话标题（可选） |

#### list-conversations

列出所有会话（读取本地数据库）。

```bash
xyncra-client list-conversations [flags]
```

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--offset` | 0 | 分页偏移 |
| `--limit` | 20 | 每页数量 |

输出示例：

```
ID             PEER    TITLE     LAST MESSAGE
--             ----    -----     ------------
conv-abc123    bob     -         2026-07-16 12:00:00
conv-def456    charlie Chat      2026-07-15 10:00:00
```

#### get-conversation

获取单个会话详情。

```bash
xyncra-client get-conversation --conversation-id conv-abc123
```

#### delete-conversation

软删除会话。

```bash
xyncra-client delete-conversation --conversation-id conv-abc123
```

#### restore-conversation

恢复软删除的会话。

```bash
xyncra-client restore-conversation --conversation-id conv-abc123
```

### 消息命令

#### send

发送消息。

```bash
xyncra-client send --conversation-id conv-abc123 --content "Hello!"
```

| 标志 | 说明 |
|------|------|
| `--conversation-id` / `-c` | 会话 ID（必填） |
| `--content` / `-m` | 消息内容（必填，允许空字符串） |
| `--reply-to` | 回复的消息序号 |
| `--client-msg-id` | 客户端消息 ID（自动生成 UUID） |

#### get-messages

获取消息历史（读取本地数据库）。

```bash
xyncra-client get-messages --conversation-id conv-abc123
```

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--conversation-id` / `-c` | - | 会话 ID |
| `--after-message-id` | 0 | 分页游标（消息序号） |
| `--limit` | 50 | 每页数量 |

#### search-messages

在会话中搜索消息。

```bash
xyncra-client search-messages --conversation-id conv-abc123 --query "hello"
```

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--conversation-id` / `-c` | - | 会话 ID |
| `--query` / `-q` | - | 搜索关键词 |
| `--after-message-id` | 0 | 分页游标 |
| `--limit` | 50 | 每页数量 |

#### delete-message

删除消息。

```bash
xyncra-client delete-message --message-id msg-uuid-xxx
```

#### mark-as-read

标记会话已读。

```bash
xyncra-client mark-as-read --conversation-id conv-abc123
```

| 标志 | 说明 |
|------|------|
| `--message-id` | 已读到哪条消息序号（0 = 标记全部已读） |

### 同步命令

#### sync-updates

触发完整数据同步。IPC-only 命令，需要守护进程运行（D-036）。

```bash
xyncra-client sync-updates
```

### Agent 命令

#### agent-resume

恢复 HITL 中断的 Agent。IPC-only 命令（D-085、D-114）。

```bash
xyncra-client agent-resume \
  --conversation-id conv-abc123 \
  --agent-id agent:weather-bot \
  --checkpoint-id cp-xxx \
  --answer "查询北京的天气"
```

| 标志 | 说明 |
|------|------|
| `--conversation-id` | 会话 ID（必填） |
| `--agent-id` | Agent ID（必填，如 `agent:weather-bot`） |
| `--checkpoint-id` | HITL 通知中的检查点 ID（必填） |
| `--interrupt-id` | HITL 通知中的中断 ID（可选） |
| `--answer` | 用户的回答内容（必填） |

#### reload-agents

热加载 Agent 配置。IPC-only 命令（D-076）。

```bash
xyncra-client reload-agents
```

### 日志命令

`logs` 是包含多个子命令的父命令，用于管理客户端本地 RPC 和通知日志。

```bash
xyncra-client logs tail [flags]
xyncra-client logs search [flags]
xyncra-client logs stats [flags]
xyncra-client logs export [flags]
xyncra-client logs cleanup [flags]
```

#### logs tail

显示最近的日志条目。

```bash
xyncra-client logs tail [flags]
```

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--type` | `rpc` | 日志类型：`rpc` 或 `notifications` |
| `--limit` | 50 | 显示条数 |
| `--since` | `1h` | 时间范围（如 `1h`、`30m`、`7d`） |

#### logs search

搜索日志条目。

```bash
xyncra-client logs search [flags]
```

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--type` | `rpc` | 日志类型：`rpc` 或 `notifications` |
| `--method` | - | 按 RPC 方法过滤 |
| `--error` | false | 仅显示错误 |
| `--from` | - | 开始时间 |
| `--to` | - | 结束时间 |
| `--conversation-id` | - | 按会话 ID 过滤 |
| `--request-id` | - | 按请求 ID 查询 |
| `--limit` | 100 | 返回条数 |

#### logs stats

显示 RPC 日志统计。

```bash
xyncra-client logs stats [flags]
```

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--since` | `24h` | 统计时间窗口 |
| `--interval` | - | 按间隔分组：`1m`、`5m`、`15m`、`1h`、`1d` |

#### logs export

导出日志到 CSV 或 JSON。

```bash
xyncra-client logs export [flags]
```

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--type` | `rpc` | 日志类型 |
| `--format` | `csv` | 导出格式：`csv` 或 `json` |
| `--output` / `-o` | stdout | 输出文件路径 |
| `--method` | - | 按方法过滤 |
| `--from` | - | 开始时间 |
| `--to` | - | 结束时间 |
| `--limit` | 1000 | 最大导出条数（上限 10000） |

#### logs cleanup

清理过期日志。

```bash
xyncra-client logs cleanup [flags]
```

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--retain` | `168h` | 保留时长 |
| `--dry-run` | false | 试运行（仅显示将删除数量） |
| `--type` | `all` | 清理类型：`rpc`、`notifications`、`all` |

### 其他命令

#### draft

管理消息草稿（本地 SQLite，不依赖网络）。

```bash
# 保存草稿
xyncra-client draft save --conversation-id conv-abc123 --content "未完成的消息"

# 读取草稿
xyncra-client draft get --conversation-id conv-abc123

# 删除草稿
xyncra-client draft delete --conversation-id conv-abc123
```

#### set-typing

发送正在输入指示器。IPC-only 命令（D-050）。

```bash
xyncra-client set-typing --conversation-id conv-abc123 [--stop]
```

| 标志 | 说明 |
|------|------|
| `--conversation-id` / `-c` | 会话 ID（必填） |
| `--stop` | 停止输入（不指定则发送开始输入） |

#### stream-text

发送流式文本。IPC-only 命令（D-051）。

```bash
xyncra-client stream-text \
  --conversation-id conv-abc123 \
  --stream-id stream-xxx \
  --text "Hello" \
  [--done]
```

| 标志 | 说明 |
|------|------|
| `--conversation-id` / `-c` | 会话 ID（必填） |
| `--stream-id` | 流 ID（必填，客户端生成 UUID） |
| `--text` | 累积文本内容（必填） |
| `--done` | 标记流结束 |

#### logs

查看客户端日志（参见[日志命令](#日志命令)子命令）。

#### kill

停止守护进程。

```bash
xyncra-client kill [flags]
```

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--force` | false | 使用 SIGKILL 强制终止 |
| `--timeout` | `5s` | 等待进程退出的超时时间 |

---

## 消息发送与接收

### 发送消息流程

```bash
# 1. 启动守护进程
xyncra-client listen --user-id alice --device-id laptop

# 2. 创建会话
xyncra-client create-conversation --peer-id bob
# 输出：Conversation created.
#   Conversation ID: conv-abc123
#   Peer: bob

# 3. 发送消息
xyncra-client send --conversation-id conv-abc123 --content "Hello Bob!"
# 输出：
#   Message sent.
#     Message ID: 1
#     UUID: msg-xxx
#     Conversation: conv-abc123
#     Client Msg ID: <uuid>
#   Duplicate: false

# 4. 接收回复（自动显示在守护进程终端）
# [new message] seq=2 from=bob conv=conv-abc123 "Hi Alice!"
```

### 消息幂等性

`send` 命令通过 `client_message_id` 实现幂等。如果守护进程检测到相同
`client_message_id` 的消息已被服务器处理，会返回 `Duplicate: true` 和已持久化的消息内容。

```bash
# 使用自定义 client-msg-id
xyncra-client send \
  --conversation-id conv-abc123 \
  --content "Hello" \
  --client-msg-id "my-unique-id-001"
```

如果未指定，客户端会自动生成 UUID v4 作为 `client_message_id`。

### 接收消息

守护进程启动后，所有实时推送会显示在终端：

```
[new message] seq=1 from=alice conv=conv-abc123 "Hello"
[delete message] conv=conv-abc123 msg=msg-xxx
[mark read] conv=conv-abc123 msg_id=42
[conversation] id=conv-abc123 title="Chat"
[gap] seq=100
[typing] user=bob conv=conv-abc123 started typing
[typing] user=bob conv=conv-abc123 stopped typing
[agent] user=agent:weather-bot conv=conv-abc123 stream=s-xxx status=streaming text="..."
[agent_status] agent=agent:weather-bot conv=conv-abc123 status=thinking
[agent_timeout] agent=agent:weather-bot conv=conv-abc123 reason="execution timeout"
[hitl] conv=conv-abc123 agent=weather-bot checkpoint_id=cp-xxx
  [1] interrupt_id=int-xxx question="确认删除这条消息？" (pending)
```

### 离线数据访问

即使守护进程离线，也可以查看本地数据：

```bash
# 这些命令不依赖网络，直接读取本地 SQLite
xyncra-client list-conversations
xyncra-client get-messages --conversation-id conv-abc123
xyncra-client search-messages --conversation-id conv-abc123 --query "hello"
xyncra-client get-conversation --conversation-id conv-abc123
```

---

## Agent 交互

### 与 Agent 对话

Agent 被表示为系统用户（user_id 格式为 `agent:{agent_id}`）。创建与 Agent 的会话、
发送消息的方式与普通用户相同。

```bash
# 1. 创建与天气 Bot 的会话
xyncra-client create-conversation --peer-id agent:weather-bot

# 2. 发送消息
xyncra-client send --conversation-id conv-agent-xxx --content "北京的天气怎么样？"
```

### 观察 Agent 实时输出

Agent 处理消息时会推送一系列实时事件：

```
[agent_status] agent=agent:weather-bot conv=conv-xxx status=thinking
[agent] user=agent:weather-bot conv=conv-xxx stream=s-1 status=streaming text="北京今天..."
[agent] user=agent:weather-bot conv=conv-xxx stream=s-1 status=streaming text="北京今天晴..."
[agent] user=agent:weather-bot conv=conv-xxx stream=s-1 status=done text="北京今天晴..."
[new message] seq=3 from=agent:weather-bot conv=conv-xxx "北京今天晴..."
[agent_status] agent=agent:weather-bot conv=conv-xxx status=idle
```

### HITL（Human-in-the-Loop）

当 Agent 需要用户确认时：

```
[conversation] id=conv-xxx title=""
[hitl] conv=conv-xxx agent=weather-bot checkpoint_id=cp-xxx
  [1] interrupt_id=int-xxx question="您确定要删除这些数据吗？" (pending)
```

使用 `agent-resume` 命令回答：

```bash
xyncra-client agent-resume \
  --conversation-id conv-xxx \
  --agent-id agent:weather-bot \
  --checkpoint-id cp-xxx \
  --answer "确认删除"
```

### 热加载 Agent

修改或新增 Agent 配置文件后：

```bash
xyncra-client reload-agents
```

---

## 测试与调试

### 调试模式

```bash
export XYNCRA_DEBUG=1
xyncra-client listen --user-id alice --device-id laptop
```

`XYNCRA_DEBUG` 接受 `1` 或 `true`。启用后，调试日志（连接、重连、RPC 调用等）
会输出到 stderr。

### 健康检查

```bash
# 检查守护进程是否运行
curl http://localhost:8080/health

# 检查守护进程的 IPC Socket
ls -la ~/.xyncra/alice/laptop/xyncra.sock
```

### 查看客户端日志

```bash
# 查看最近 RPC 日志
xyncra-client logs tail

# 查看最近通知日志
xyncra-client logs tail --type notifications
```

### RPC 日志

客户端会自动记录所有 RPC 调用和结果到本地数据库。日志保留 7 天，自动清理。

### 常用调试命令

```bash
# 启用调试模式查看重连行为
XYNCRA_DEBUG=1 xyncra-client listen --user-id alice --device-id laptop 2>&1 | grep -i reconnect

# 检查连接状态
lsof -i :8080

# 检查进程锁
cat ~/.xyncra/alice/laptop/xyncra.lock
```

---

## 本地数据查询

所有本地数据读取命令都使用本地 SQLite 数据库，不依赖网络：

```bash
# 会话数据
xyncra-client list-conversations
xyncra-client get-conversation --conversation-id conv-abc123

# 消息数据
xyncra-client get-messages --conversation-id conv-abc123
xyncra-client search-messages --conversation-id conv-abc123 --query "关键字"

# 草稿数据
xyncra-client draft get --conversation-id conv-abc123
```

本地数据库位置：`~/.xyncra/{user_id}/{device_id}/xyncra.db`

---

## 内置函数（ReverseRPC）

守护进程自动注册三个内置函数，Agent 可以通过 ReverseRPC 调用设备端功能（D-092、D-098）：

### ping

```json
// Agent 请求
{"method": "ping", "params": {"message": "hello"}}

// 设备响应
{"echo": "hello", "timestamp": "2026-07-16T12:00:00.123456789Z"}
```

### get_device_info

```json
// Agent 请求
{"method": "get_device_info", "params": {}}

// 设备响应
{"hostname": "MacBook-Pro.local", "os": "darwin", "arch": "arm64", "pid": 12345}
```

### get_time

```json
// Agent 请求
{"method": "get_time", "params": {}}

// 设备响应
{"utc": "2026-07-16T12:00:00.123456789Z", "unix": 1781683200, "timezone": "Local"}
```

三个函数的 tags 均为 `diagnostic`，用于服务器端诊断设备状态。

---

## 草稿功能

`draft` 命令管理消息草稿，支持离线使用（D-038、M-4）：

```bash
# 保存草稿
xyncra-client draft save --conversation-id conv-abc123 --content "未完成的回复"

# 发送消息后自动清除草稿
xyncra-client send --conversation-id conv-abc123 --content "最终的回复"
```

---

## 故障排查

| 问题 | 解决方法 |
|------|---------|
| `address already in use` | 守护进程已在运行，或上次未正常退出。用 `xyncra-client kill` 停止 |
| `connection refused` | 服务器未运行。先 `./bin/xyncra-server` 或 `docker compose up -d` |
| `user-id is required` | 通过 `--user-id` 或 `XYNCRA_USER_ID` 环境变量设置 |
| 看不到实时推送 | `listen` 命令未运行。先启动守护进程 |
| 消息发送失败 | 检查会话 ID 是否正确，或尝试 `kill` 后重启守护进程 |
| `4001 close frame` | 同一设备 ID 的新连接替换了当前连接。换一个 device-id 或这是预期行为 |
