# SQLite 数据库结构

## 数据库文件

| 属性 | 值 |
|------|-----|
| 路径 | `~/.xyncra/{user_id}/{device_id}/xyncra.db` |
| 模式 | WAL（Write-Ahead Logging） |
| 并发 | `busy_timeout(5000)` -- 写冲突等待 5 秒 |
| 迁移 | AutoMigrate（每次运行自动更新表结构，D-035） |
| 打开方式 | `store.New()` 打开数据库，读完后关闭连接 |
| 权限 | 目录 `0700`（D-030） |

**并发模型**：
- WAL 模式支持读写并发（daemon 写入时 CLI 可并发读取，D-035）
- daemon 独占写入，CLI 查询命令只读
- `busy_timeout(5000)` 在写冲突时最多等待 5 秒

---

## 表结构

### conversations

会话记录，1-on-1 类型。

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `uint` | 主键（自增） |
| `Type` | `string` | 会话类型（当前仅 `direct`） |
| `UserID1` | `string` | 用户 1 ID |
| `UserID2` | `string` | 用户 2 ID |
| `Title` | `string` | 会话标题 |
| `LastMessageAt` | `timestamp` | 最后消息时间 |
| `LastProcessedMessageID` | `uint32` | 最后处理的消息序列号（D-008） |
| `LastReadMessageID1` | `uint32` | 用户 1 已读位置（D-012，MAX 语义） |
| `LastReadMessageID2` | `uint32` | 用户 2 已读位置（D-012，MAX 语义） |
| `DeletedAt` | `timestamp` | 软删除时间（GORM soft-delete） |

**约束**：
- `(UserID1, UserID2)` 用户对唯一性（D-011 find-or-create 幂等）
- `DeletedAt` 非空表示已软删除（D-013）

---

### messages

消息记录。

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `string` | UUID 主键（D-038） |
| `MessageID` | `uint32` | 会话内序列号（D-008，单调递增） |
| `ConversationID` | `uint` | 外键 -> conversations.ID |
| `SenderID` | `string` | 发送者 ID |
| `Content` | `string` | 消息内容 |
| `Type` | `string` | 消息类型 |
| `ReplyTo` | `string` | 回复的消息 UUID（`messages.id`） |
| `ClientMessageID` | `string` | 客户端生成的 UUID（D-006，uniqueIndex） |
| `DeletedAt` | `timestamp` | 软删除时间（GORM soft-delete） |

**D-038 重点 -- ID（string UUID）vs MessageID（uint32）的区别**：

| 字段 | 含义 | 用途 |
|------|------|------|
| `ID` (string UUID) | 消息的全局唯一标识符（primary key） | 跨会话引用（如 `ReplyTo`）、RPC 参数（`delete_message`） |
| `MessageID` (uint32) | 会话内的单调递增序号 | 会话内排序、增量同步（`after_seq`）、已读位置（`mark_as_read`） |

**约束**：
- `ClientMessageID` uniqueIndex 保证幂等性（D-006）
- `MessageID` 在同一会话内唯一且单调递增（D-008）

---

### drafts

消息草稿（本地，不上传到服务器）。

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `uint` | 主键（自增） |
| `ConversationID` | `uint` | 外键 -> conversations.ID（unique） |
| `Content` | `string` | 草稿内容 |

**约束**：
- `ConversationID` unique：每个会话最多一个草稿
- Upsert 语义：`draft save` 覆盖已有草稿

---

### rpc_logs

RPC 调用日志。

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `uint` | 主键（自增） |
| `Method` | `string` | RPC 方法名（如 `send_message`） |
| `StatusCode` | `int` | 状态码（0=成功，-1=错误，D-027 客户端错误码） |
| `Duration` | `int64` | 耗时（毫秒） |
| `ConversationID` | `uint` | 关联会话 ID（可选） |
| `RequestID` | `string` | 请求 ID（UUID，用于追踪） |
| `Error` | `string` | 错误信息（StatusCode < 0 时） |
| `CreatedAt` | `timestamp` | 创建时间 |

**查询示例**：
- 按方法过滤：`WHERE method = 'send_message'`
- 仅错误：`WHERE status_code = -1`
- 按时间范围：`WHERE created_at > datetime('now', '-1 day')`

---

### notification_logs

推送通知日志。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Seq` | `uint64` | 序列号（与 UserUpdate.Seq 对应） |
| `Type` | `string` | 通知类型（`message`、`delete_message`、`mark_read`、`conversation`、`gap`，D-028） |
| `Payload` | `text` | JSON 负载 |
| `CreatedAt` | `timestamp` | 创建时间 |

**约束**：
- `Seq` 用于去重（syncManager 基于此表避免重复处理）

---

### sync_states

同步状态键值存储。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Key` | `string` | 状态键 |
| `Value` | `string` | 状态值 |

**预定义键**：

| Key | 说明 |
|-----|------|
| `local_max_seq` | 本地已处理的最大序列号（所有类型共享，D-028） |
| `latest_seq` | 服务器全局最新序列号 |

---

### retry_tasks

重试任务队列。

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `uint` | 主键（自增） |
| `Method` | `string` | RPC 方法名 |
| `Params` | `text` | JSON 参数 |
| `Attempt` | `int` | 已重试次数 |
| `Status` | `string` | 状态：`pending` / `done` / `failed` |
| `CreatedAt` | `timestamp` | 创建时间 |

---

### user_updates

本地缓存的 UserUpdate 记录（从服务器同步）。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Seq` | `uint64` | 序列号 |
| `Type` | `string` | 更新类型（D-028：`message`、`delete_message`、`mark_read`、`conversation`、`gap`） |
| `Payload` | `text` | JSON 负载 |
| `CreatedAt` | `timestamp` | 创建时间 |

**约束**：
- 服务器端 UserUpdate 保留 30 天（D-016），客户端本地缓存对应同步到的记录

---

## 并发特性

| 特性 | 说明 |
|------|------|
| WAL 模式 | 读写并发 -- 读者不阻塞写者，写者不阻塞读者 |
| `busy_timeout(5000)` | 写冲突时等待最多 5 秒（SQLite 自动重试） |
| daemon 独占写入 | 只有 listen 守护进程写入数据库 |
| CLI 只读 | 查询命令（D-035）只读 SQLite |
| AutoMigrate | `store.New()` 运行 AutoMigrate，可能短暂阻塞（DDL 锁） |

**读写分离场景**：
```
daemon: INSERT INTO messages ...     <- 写入
CLI:    SELECT * FROM messages ...   <- 并发读取（WAL 允许）
```

---

## 常用查询示例

```bash
# 设置路径变量（替换为实际值）
DB=~/.xyncra/alice/abc12345/xyncra.db

# 查看所有会话
sqlite3 $DB "SELECT id, user_id1, user_id2, title, last_message_at FROM conversations WHERE deleted_at IS NULL;"

# 查看某会话的消息（按序列号排序）
sqlite3 $DB "SELECT message_id, sender_id, content, created_at FROM messages WHERE conversation_id = 1 AND deleted_at IS NULL ORDER BY message_id;"

# 查看同步状态
sqlite3 $DB "SELECT * FROM sync_states;"

# 查看未读消息数（假设当前用户是 user_id2）
sqlite3 $DB "SELECT COUNT(*) FROM messages WHERE conversation_id = 1 AND message_id > (SELECT last_read_message_id2 FROM conversations WHERE id = 1) AND sender_id != '当前用户ID' AND deleted_at IS NULL;"

# 查看最近的 RPC 日志
sqlite3 $DB "SELECT created_at, method, status_code, duration, error FROM rpc_logs ORDER BY created_at DESC LIMIT 10;"

# 查看最近的推送通知
sqlite3 $DB "SELECT created_at, seq, type FROM notification_logs ORDER BY created_at DESC LIMIT 10;"

# 查看草稿
sqlite3 $DB "SELECT d.conversation_id, d.content FROM drafts d JOIN conversations c ON d.conversation_id = c.id WHERE c.id = 1;"

# 统计 RPC 调用（按方法分组）
sqlite3 $DB "SELECT method, COUNT(*) as count, AVG(duration) as avg_ms FROM rpc_logs WHERE created_at > datetime('now', '-1 day') GROUP BY method ORDER BY count DESC;"
```

---

## 相关文档

- [系统架构概览](overview.md)
- [IPC 协议规范](ipc.md)
