# 快速开始

本文档帮助你在 5 分钟内跑通 xyncra-client 的核心流程：构建、启动守护进程、发送消息、查询数据。

## 前置条件

- **Go 1.24+**（编译客户端和服务器）
- **Redis**（服务器需要，默认 `localhost:6379`）

> 服务器和客户端均遵循 **D-001**（零配置启动）原则，使用合理的默认值，无需配置文件。

## 构建

```bash
# 构建服务器
go build -o xyncra-server ./cmd/xyncra-server

# 构建客户端
go build -o xyncra-client ./cmd/xyncra-client
```

## 启动服务器

```bash
./xyncra-server
# 服务器默认监听 :8080，需要 Redis 在 localhost:6379
```

> 服务器不做认证（**D-002**），通过 URL 查询参数 `user_id` 识别用户。生产环境应在反向代理层注入已认证的用户身份。

## 首次运行 listen

`listen` 命令启动守护进程，维护 WebSocket 长连接并提供 IPC 服务。**所有写操作命令都依赖守护进程运行**（直接查询命令除外）。

```bash
./xyncra-client listen --user-id alice
```

预期 stderr 输出：

```
[xyncra] Starting listener daemon...
[xyncra] Device: abc12345
[xyncra] Connecting to ws://localhost:8080/ws?user_id=alice ...
[xyncra] IPC server listening at /Users/alice/.xyncra/alice/abc12345/xyncra.sock
[xyncra] Listening for updates... (Ctrl+C to stop)
```

> **关键概念**：
> - `listen` 是守护进程，必须先启动（**D-030**, **D-031**）
> - 同一 (user_id, device_id) 只允许一个实例，由 fcntl 进程锁保证
> - 退出码 `2` 表示锁冲突（进程已在运行）

打开另一个终端继续下面的操作。

## 发送第一条消息

### 1. 创建会话

```bash
# find-or-create 幂等模型（D-011）：重复执行返回同一会话，不报错
./xyncra-client create-conversation --user-id alice --peer-id bob
```

预期输出：

```
Conversation created.
  Conversation ID: <uuid>
  Peer: bob
  Title:
```

> **D-011**：`create-conversation` 使用 find-or-create 幂等模式。同一用户对之间的会话天然唯一，重复调用安全返回已有会话。
>
> **D-037**：使用 `--peer-id` 而非 `--user-id`，避免遮蔽全局 flag。

### 2. 发送消息

```bash
./xyncra-client send --user-id alice -c <conversation_id> -m "Hello, Bob!"
```

预期输出：

```
Message sent.
  Message ID: <seq>
  Conversation: <conversation_id>
  Client Msg ID: 550e8400-e29b-41d4-a716-446655440000
  Duplicate: false
```

> - `-c` 是 `--conversation-id` 的简写，`-m` 是 `--content` 的简写
> - `Client Msg ID` 是客户端自动生成的 UUID v4（**D-006**），保证幂等性
> - `Duplicate: false` 表示首次发送；重试时变为 `true` 表示幂等命中
> - 执行模式：IPC 优先，失败 fallback 到 WebSocket 短连接（**D-032**）

## 查询数据

查询命令**直接读取本地 SQLite 数据库**（**D-035**），无需守护进程运行，离线可用。

```bash
# 列出所有会话（分页：默认 limit=20）
./xyncra-client list-conversations --user-id alice
```

预期输出：

```
ID                                    Peer                  Title                         Last Message
-------------------------------------------------------------------------------------------
<uuid>                                bob                                                   2026-07-09 12:34:56
```

```bash
# 查看会话详情 + 未读计数
./xyncra-client get-conversation --user-id alice -c <conversation_id>
```

预期输出：

```
Conversation Details
  ID:           <uuid>
  Type:         direct
  User 1:       alice
  User 2:       bob
  Peer:         bob
  Title:
  Created:      2026-07-09 12:34:56
  Last Message: 2026-07-09 12:34:56
  Unread:       0
```

```bash
# 获取消息历史（ASC 顺序，D-035）
./xyncra-client get-messages --user-id alice -c <conversation_id>
```

预期输出：

```
[#1] alice (12:34): Hello, Bob!
```

```bash
# 搜索消息（DESC 顺序，D-035）
./xyncra-client search-messages --user-id alice -c <conversation_id> -q "Hello"
```

预期输出：

```
[#1] alice (12:34): Hello, Bob!
```

> **D-035**：四个查询命令（`list-conversations`、`get-conversation`、`get-messages`、`search-messages`）直接读本地 SQLite，支持离线使用。查询结果反映最后一次同步时的状态。

## 配置

### 环境变量

所有全局 flag 均支持 `XYNCRA_` 前缀的环境变量（**D-034**）：

| 环境变量 | 对应 Flag | 默认值 | 说明 |
|----------|-----------|--------|------|
| `XYNCRA_USER_ID` | `--user-id, -u` | (必填) | 用户 ID |
| `XYNCRA_DEVICE_ID` | `--device-id` | hostname SHA256[:8] | 设备 ID（**D-033**） |
| `XYNCRA_SERVER` | `--server, -s` | `ws://localhost:8080/ws` | 服务器 URL |
| `XYNCRA_DB_PATH` | `--db-path` | `~/.xyncra/{uid}/{did}/xyncra.db` | SQLite 数据库路径 |
| `XYNCRA_LOG_DIR` | `--log-dir` | `~/.xyncra/{uid}/{did}/logs` | 日志目录 |
| `XYNCRA_DEBUG` | - | (空) | 设为 `1` 或 `true` 启用 debug 日志 |

### 优先级规则

```
flag > env var > default
```

示例：

```bash
# 以下三种方式等效
./xyncra-client listen --user-id alice
XYNCRA_USER_ID=alice ./xyncra-client listen
# 或在 shell profile 中 export XYNCRA_USER_ID=alice
```

### device-id 自动生成（D-033）

如果不指定 `--device-id`，客户端使用主机名的 SHA256 哈希前 8 位 hex 作为设备 ID。同一台机器总是生成相同的 device-id。

```bash
# 查看自动生成的 device-id
./xyncra-client listen --user-id alice 2>&1 | grep "Device:"
```

> **D-033**：使用哈希而非真实主机名，兼顾匿名化和确定性。8 位 hex = 32 bit，对于单机设备标识绰绰有余。

## 目录结构

```
~/.xyncra/{user_id}/{device_id}/
├── xyncra.db       # SQLite 数据库（WAL 模式，所有数据）
├── xyncra.lock     # 进程锁文件（fcntl，D-031）
├── xyncra.sock     # Unix Socket（IPC，D-030）
└── logs/           # 日志目录
```

| 路径 | 权限 | 说明 |
|------|------|------|
| 目录 `~/.xyncra/{uid}/{did}/` | `0700` | 仅所有者可访问 |
| `xyncra.lock` | - | fcntl 文件锁（**D-031**），支持 stale lock 检测 |
| `xyncra.sock` | `0600` | IPC Unix Socket（**D-030**），JSON-RPC 2.0 协议 |
| `xyncra.db` | - | SQLite WAL 模式，支持守护进程写入时并发查询 |

## 退出码（D-042）

| 退出码 | 含义 | 场景 |
|--------|------|------|
| `0` | 成功 | 命令正常完成 |
| `1` | 通用错误 | 参数错误、网络错误、数据库错误等 |
| `2` | 前置条件不满足 | 锁冲突（`listen` 重复启动） |
| `3` | 超时退出 | `kill --timeout` 到期且未使用 `--force` |

> **D-042**：POSIX 兼容的退出码约定，方便 shell 脚本基于退出码判断错误类型。

## 典型工作流

```
1. 启动服务器        ./xyncra-server
2. 启动守护进程      ./xyncra-client listen --user-id alice   （终端 1）
3. 创建会话          ./xyncra-client create-conversation --user-id alice --peer-id bob
4. 发送消息          ./xyncra-client send --user-id alice -c <id> -m "Hello!"
5. 查询消息          ./xyncra-client get-messages --user-id alice -c <id>
6. 手动触发同步      ./xyncra-client sync-updates --user-id alice
7. 停止守护进程      ./xyncra-client kill --user-id alice
```

> **注意**：`sync-updates` 是 IPC-only 命令（**D-036**），必须依赖守护进程运行，无 WebSocket fallback。

## 下一步

- [命令详解](commands/) — 所有命令的详细文档
- [架构说明](architecture/overview.md) — 系统架构和数据流
- [使用场景](scenarios/basic-usage.md) — 常见使用场景
