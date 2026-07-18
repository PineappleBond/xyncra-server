# Dexie.js 数据库结构

## 数据库概述

| 属性 | 值 |
|------|-----|
| 库名 | IndexedDB（由 `--db-path` 指定，TS-D-012） |
| 实现 | Dexie.js（IndexedDB 友好封装） |
| Node.js 提供 | `fake-indexeddb`（内存实现） |
| 表数量 | 9 个（比 Go 版多 2 个：`questions`、`userUpdates`） |
| 迁移 | 自动（Dexie version + stores 定义） |
| 并发 | IndexedDB 事务模型（读写并发，无锁竞争） |

**与 Go 版的关键差异**：

| 特性 | Go 版（SQLite） | TS 版（Dexie.js） |
|------|----------------|------------------|
| 存储引擎 | SQLite WAL 模式 | IndexedDB（fake-indexeddb） |
| 并发模型 | WAL 读写并发 | IndexedDB 事务（读写并发，无锁竞争） |
| 文件路径 | `~/.xyncra/{uid}/{did}/xyncra.db` | 内存（Node.js）或 IndexedDB（浏览器） |
| `--db-path` 语义 | SQLite 文件路径 | IndexedDB 数据库名（TS-D-012） |
| 迁移方式 | GORM AutoMigrate | Dexie version + stores |
| 表数量 | 7 个 | 9 个（新增 `questions`、`userUpdates`） |

---

## 表结构

### conversations

会话记录，1-on-1 类型。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | number | 主键 |
| `user_id1` | string | 用户 1 ID（索引） |
| `user_id2` | string | 用户 2 ID（索引） |
| `type` | string | 会话类型（当前仅 `direct`） |
| `title` | string | 会话标题 |
| `last_message_at` | Date | 最后消息时间 |
| `last_processed_message_id` | number | 最后处理的消息序列号（D-008） |
| `last_read_message_id1` | number | 用户 1 已读位置（D-012，MAX 语义） |
| `last_read_message_id2` | number | 用户 2 已读位置（D-012，MAX 语义） |
| `created_at` | Date | 创建时间 |
| `agent_status` | string | Agent 状态（D-117） |
| `deleted_at` | Date? | 软删除时间 |

**索引**：
- 主键：`id`
- `user_id1`、`user_id2`、`&[user_id1+user_id2]`（复合唯一）、`type`、`created_at`、`last_message_at`、`agent_status`

**约束**：
- `[user_id1+user_id2]` 复合唯一（D-011 find-or-create 幂等）
- `deleted_at` 非空表示已软删除（D-013）

---

### messages

消息记录。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | UUID 主键（D-038） |
| `message_id` | number | 会话内序列号（D-008，单调递增） |
| `conversation_id` | number | 外键 -> conversations.id |
| `sender_id` | string | 发送者 ID |
| `content` | string | 消息内容 |
| `type` | string | 消息类型 |
| `reply_to` | string? | 回复的消息 UUID（`messages.id`） |
| `client_message_id` | string | 客户端生成的 UUID（D-006） |
| `created_at` | Date | 创建时间 |
| `deleted_at` | Date? | 软删除时间 |

**索引**：
- 主键：`id`
- `&[client_message_id+sender_id]`（复合唯一）
- `conversation_id`、`[conversation_id+message_id]`（复合）、`sender_id`、`created_at`、`message_id`

**D-038 重点 -- id（string UUID）vs message_id（number）的区别**：

| 字段 | 含义 | 用途 |
|------|------|------|
| `id` (string UUID) | 消息的全局唯一标识符（primary key） | 跨会话引用（如 `reply_to`）、RPC 参数（`delete_message`） |
| `message_id` (number) | 会话内的单调递增序号 | 会话内排序、增量同步（`after_seq`）、已读位置（`mark_as_read`） |

**约束**：
- `[client_message_id+sender_id]` 复合唯一保证幂等性（D-006）
- `message_id` 在同一会话内唯一且单调递增（D-008）

---

### questions

HITL 问题记录（D-116）。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | UUID 主键 |
| `conversation_id` | number | 外键 -> conversations.id |
| `content` | string | 问题内容 |
| `status` | string | 状态：`pending` / `answered` / `expired` |
| `created_at` | Date | 创建时间 |

**索引**：
- 主键：`id`
- `conversation_id`、`created_at`

---

### syncStates

同步状态键值存储。

| 字段 | 类型 | 说明 |
|------|------|------|
| `key` | string | 状态键（主键） |
| `value` | string | 状态值 |

**预定义键**：

| Key | 说明 |
|-----|------|
| `local_max_seq` | 本地已处理的最大序列号（所有类型共享，D-028） |
| `latest_seq` | 服务器全局最新序列号 |

---

### drafts

消息草稿（本地，不上传到服务器）。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | number | 主键 |
| `conversation_id` | number | 外键 -> conversations.id |
| `content` | string | 草稿内容 |

**索引**：
- 主键：`id`
- `&conversation_id`（唯一）

**约束**：
- `conversation_id` 唯一：每个会话最多一个草稿
- Upsert 语义：`draft save` 覆盖已有草稿

---

### retryTasks

重试任务队列。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | number | 主键 |
| `method` | string | RPC 方法名 |
| `params` | string | JSON 参数 |
| `attempt` | number | 已重试次数 |
| `status` | string | 状态：`pending` / `done` / `failed` |
| `next_retry` | Date | 下次重试时间 |
| `created_at` | Date | 创建时间 |

**索引**：
- 主键：`id`
- `method`、`status`、`next_retry`、`created_at`

---

### rpcLogs

RPC 调用日志。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | number | 主键 |
| `type` | string | 日志类型 |
| `request_id` | string | 请求 ID（UUID，用于追踪） |
| `method` | string | RPC 方法名（如 `send_message`） |
| `status_code` | number | 状态码（0=成功，-1=错误，D-027 客户端错误码） |
| `duration` | number | 耗时（毫秒） |
| `conversation_id` | number? | 关联会话 ID（可选） |
| `error` | string? | 错误信息（status_code < 0 时） |
| `created_at` | Date | 创建时间 |

**索引**：
- 主键：`id`
- `type`、`request_id`、`method`、`status_code`、`conversation_id`、`created_at`

---

### notificationLogs

推送通知日志。

| 字段 | 类型 | 说明 |
|------|------|------|
| `seq` | number | 序列号（与 UserUpdate.Seq 对应，主键） |
| `type` | string | 通知类型（`message`、`delete_message`、`mark_read`、`conversation`、`gap`，D-028） |
| `payload` | string | JSON 负载 |
| `created_at` | Date | 创建时间 |

**索引**：
- 主键：`seq`
- `type`、`created_at`

**约束**：
- `seq` 用于去重（syncManager 基于此表避免重复处理）

---

### userUpdates

本地缓存的 UserUpdate 记录（从服务器同步）。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | number | 主键 |
| `user_id` | string | 用户 ID |
| `seq` | number | 序列号 |
| `type` | string | 更新类型（D-028：`message`、`delete_message`、`mark_read`、`conversation`、`gap`） |
| `payload` | string | JSON 负载 |
| `created_at` | Date | 创建时间 |

**索引**：
- 主键：`id`
- `user_id`、`[user_id+seq]`（复合）、`type`、`created_at`

**约束**：
- 服务器端 UserUpdate 保留 30 天（D-016），客户端本地缓存对应同步到的记录

---

## 并发特性

| 特性 | 说明 |
|------|------|
| IndexedDB 事务 | 读写并发，无锁竞争（不同于 SQLite WAL 的 busy_timeout） |
| Dexie 事务 | `db.transaction('rw', tables, () => { ... })` 自动管理事务边界 |
| daemon 写入 | 只有 listen 守护进程写入数据库 |
| CLI 读取 | 查询命令通过 IPC 访问 daemon 中的 Dexie.js |
| 自动迁移 | Dexie version 升级时自动更新表结构 |

**读写分离场景**：
```typescript
// daemon: 写入
await db.messages.add({
  id: uuidv4(),
  conversation_id: 1,
  sender_id: 'alice',
  content: 'Hello!',
  // ...
});

// CLI (via IPC): 并发读取
const messages = await db.messages
  .where('conversation_id').equals(1)
  .sortBy('message_id');
```

---

## Dexie.js API 示例

### 初始化数据库

```typescript
import Dexie from 'dexie';
import { fakeIndexedDB } from 'fake-indexeddb';

// Node.js 环境使用 fake-indexeddb
const db = new Dexie('xyncra.db', { indexedDB: fakeIndexedDB });

db.version(1).stores({
  conversations: 'id, user_id1, user_id2, &[user_id1+user_id2], type, created_at, last_message_at, agent_status',
  messages: 'id, &[client_message_id+sender_id], conversation_id, [conversation_id+message_id], sender_id, created_at, message_id',
  questions: 'id, conversation_id, created_at',
  syncStates: 'key',
  drafts: 'id, &conversation_id',
  retryTasks: 'id, method, status, next_retry, created_at',
  rpcLogs: 'id, type, request_id, method, status_code, conversation_id, created_at',
  notificationLogs: 'seq, type, created_at',
  userUpdates: 'id, user_id, [user_id+seq], type, created_at',
});
```

### 常用查询

```typescript
// 查看所有会话（排除已删除）
const conversations = await db.conversations
  .where('deleted_at').equals(null)
  .reverse()
  .sortBy('last_message_at');

// 查看某会话的消息（按序列号排序）
const messages = await db.messages
  .where('conversation_id').equals(1)
  .and(msg => !msg.deleted_at)
  .sortBy('message_id');

// 查看同步状态
const syncState = await db.syncStates.get('local_max_seq');

// 查看未读消息数（假设当前用户是 user_id2）
const conv = await db.conversations.get(1);
const unreadCount = await db.messages
  .where('[conversation_id+message_id]')
  .between([1, conv.last_read_message_id2 + 1], [1, Infinity])
  .and(msg => msg.sender_id !== 'alice' && !msg.deleted_at)
  .count();

// 查看最近的 RPC 日志
const recentLogs = await db.rpcLogs
  .orderBy('created_at')
  .reverse()
  .limit(10)
  .toArray();

// 查看最近的推送通知
const recentNotifications = await db.notificationLogs
  .orderBy('seq')
  .reverse()
  .limit(10)
  .toArray();

// 查看草稿
const draft = await db.drafts
  .where('conversation_id').equals(1)
  .first();

// 统计 RPC 调用（按方法分组）
const allLogs = await db.rpcLogs
  .where('created_at')
  .above(new Date(Date.now() - 24 * 60 * 60 * 1000))
  .toArray();

const stats = allLogs.reduce((acc, log) => {
  if (!acc[log.method]) acc[log.method] = { count: 0, totalDuration: 0 };
  acc[log.method].count++;
  acc[log.method].totalDuration += log.duration;
  return acc;
}, {});
```

### 事务操作

```typescript
// 读写事务
await db.transaction('rw', db.messages, db.conversations, async () => {
  // 更新会话最后消息时间
  await db.conversations.update(1, { last_message_at: new Date() });

  // 添加新消息
  await db.messages.add({
    id: uuidv4(),
    conversation_id: 1,
    sender_id: 'alice',
    content: 'Hello!',
    message_id: 42,
    // ...
  });
});

// 只读事务
await db.transaction('r', db.messages, async () => {
  const count = await db.messages
    .where('conversation_id').equals(1)
    .count();
  console.log(`Total messages: ${count}`);
});
```

---

## 相关文档

- [系统架构概览](overview.md)
- [IPC 协议规范](ipc.md)
