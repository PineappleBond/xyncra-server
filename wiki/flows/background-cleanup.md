# Background Cleanup 业务流程

本文档描述 Xyncra 服务器和服务端/客户端中的后台清理任务，包括 UserUpdate 过期清理、HITL 超时清理、上下文缓存清理、工具结果清理、Rate Limiter 清理和客户端日志清理。所有服务端清理任务在 `cmd/xyncra-server/main.go` 中启动，通过共享的 `context.Context` 管理生命周期。

---

## 目录

- [概述](#概述)
- [UserUpdate 过期清理](#userupdate-过期清理)
- [HITL 超时清理](#hitl-超时清理)
- [上下文缓存清理](#上下文缓存清理)
- [工具结果清理](#工具结果清理)
- [Rate Limiter 清理](#rate-limiter-清理)
- [客户端日志清理](#客户端日志清理)
- [依赖关系](#依赖关系)
- [关键设计决策](#关键设计决策)

---

## 概述

Xyncra 服务器运行多个后台清理任务，用于维护系统健康状态和防止资源泄漏。这些任务在服务器启动时自动运行，无需手动触发。

### 清理任务列表

| 任务 | 间隔 | 保留期 | 说明 |
|------|------|--------|------|
| UserUpdate 过期清理 | 1 小时 | 30 天 | 清理过期的 UserUpdate 记录 |
| HITL 超时清理 | 5 分钟 | 24 小时 | 清理超时的 HITL 会话（可配置） |
| 上下文缓存清理 | 5 分钟 | 30 秒 | 清理过期的对话上下文缓存（TTL 可配置） |
| 工具结果清理 | 调用方配置 | 1 小时 | 清理过期的工具执行结果（TTL 可配置） |
| Rate Limiter 清理（stream_text） | 5 分钟 | 10 分钟 | 清理未使用的 stream_text rate limiter 条目 |
| Rate Limiter 清理（set_typing） | 5 分钟 | 10 分钟 | 清理未使用的 set_typing rate limiter 条目 |
| 客户端日志清理 | 1 小时 | 7 天 | 清理客户端 RPC 日志和通知日志（客户端侧） |

---

## UserUpdate 过期清理

### 概述

定期清理过期的 UserUpdate 记录，防止数据库无限增长。过期记录定义为 `created_at` 超过 30 天的记录。

### 流程图

```mermaid
sequenceDiagram
    participant BG as 后台 Goroutine
    participant Store as UserUpdateStore
    participant DB as 数据库

    loop 每小时
        BG->>Store: CleanupExpired(ctx)
        Store->>DB: DELETE FROM user_updates WHERE created_at < NOW() - 30 days
        DB-->>Store: 返回删除行数
        Store-->>BG: 返回删除行数

        alt 删除行数 > 0
            BG->>BG: 记录日志
        end
    end
```

### 详细步骤

1. **定时触发**：每小时触发一次
2. **执行清理**：调用 `UserUpdateStore.CleanupExpired(ctx)`
3. **SQL 操作**：`DELETE FROM user_updates WHERE created_at < NOW() - INTERVAL 30 DAY`
4. **记录日志**：如果删除行数 > 0，记录日志
5. **错误处理**：清理失败仅记录日志，不中断循环

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| 数据库不可达 | 记录错误日志，下次重试 |
| 无过期记录 | 静默跳过，不记录日志 |
| 清理期间 panic | recover 后继续运行 |

---

## HITL 超时清理

### 概述

定期扫描处于 `asking_user` 状态超过 24 小时的会话，清理其 HITL 相关资源（checkpoint、questions），发送超时消息通知用户，并广播 `agent_timeout` 事件。

### 流程图

```mermaid
sequenceDiagram
    participant BG as 后台 Goroutine
    participant CS as ConversationStore
    participant QS as QuestionStore
    participant CP as CheckPointStore
    participant Lock as Redis (SETNX)
    participant DS as DataStore (SendMessage)
    participant BC as BroadcastHelper
    participant DB as 数据库

    loop 每 5 分钟
        BG->>CS: ListStaleHITLConversations(maxAge=24h, batchSize=100)
        CS->>DB: SELECT ... WHERE agent_status='asking_user' AND agent_last_activity < cutoff
        CS-->>BG: 返回会话列表

        loop 每个超时会话
            BG->>Lock: SETNX hitl:cleanup:{convID} TTL=30s
            Lock-->>BG: 获取锁结果

            alt 获取锁失败
                BG->>BG: 跳过 (其他节点正在处理)
            else 获取锁成功
                BG->>CS: Get(convID) — 重新检查状态
                CS-->>BG: 返回最新会话

                alt 状态已不是 asking_user
                    BG->>BG: 跳过 (已被其他操作解决)
                else 仍为 asking_user
                    BG->>BG: 确定 humanUserID（排除 AgentID）

                    BG->>CS: ClearAgentStatus(convID)
                    CS->>DB: UPDATE conversations SET agent_status='idle', agent_id='', checkpoint_id=''
                    DB-->>CS: 返回 updated_at

                    alt checkpointID 非空
                        BG->>QS: DeleteByCheckpoint(checkpointID) [软删除]
                        QS->>DB: UPDATE questions SET deleted_at=NOW() WHERE checkpoint_id=?
                    end

                    alt checkpointID 非空
                        BG->>CP: Delete(checkpointID)
                        CP->>Redis: DEL checkpoint key
                    end

                    BG->>DS: SendMessage(超时提示消息, recipients=[humanUserID, agentID])
                    DS->>DB: INSERT message ("抱歉，等待时间过长，会话已超时。请重新发送消息。")

                    BG->>BC: SendAgentTimeout(humanUserID, agentID, convID, "hitl_timeout")
                    BG->>BC: SendConversationUpdate(humanUserID, convID, updated_at)
                end
            end
            Note over Lock: 锁通过 TTL 自动释放，无需显式 DEL
        end
    end
```

### 详细步骤

1. **定时触发**：每 5 分钟触发一次（默认值，可通过 `HITLCleanupConfig.Interval` 配置）
2. **查询超时会话**：调用 `ListStaleHITLConversations`，获取所有 `agent_status='asking_user'` 且 `agent_last_activity < NOW() - 24h` 的会话，限制最多 100 条（`BatchSize` 可配置）
3. **分布式锁**：对每个会话使用 Redis `SETNX` 获取分布式锁（key: `hitl:cleanup:{conversationID}`，TTL: 30 秒），防止多节点重复处理。获取锁失败或 Redis 不可达时跳过该会话
4. **重新检查状态**：获取锁后重新查询会话最新状态（通过 `ConversationStore.Get`，自动排除软删除记录），确认仍为 `asking_user`（其他节点或用户操作可能已解决）
5. **确定 humanUserID**：从会话的 `UserID1`/`UserID2` 中排除 `AgentID`，得到人类用户 ID
6. **清理操作**（所有步骤非致命，失败仅记录日志）：
   - 清除会话的 agent 状态：`ClearAgentStatus` 执行 `UPDATE conversations SET agent_status='idle', agent_id='', checkpoint_id='', agent_last_activity=NOW(), updated_at=NOW()`，返回 `updated_at`
   - 如果 `checkpointID` 非空且 `questionStore != nil`，软删除该 checkpoint 的所有 questions（GORM `Delete`，设置 `deleted_at`）
   - 如果 `checkpointID` 非空且 `checkpointStore != nil`，删除 Redis 中的 checkpoint key
7. **发送超时消息**：通过 `DataStore.SendMessage` 向 humanUserID 和 agentID 双方发送超时提示消息（"抱歉，等待时间过长，会话已超时。请重新发送消息。"），消息类型为 text，status 为 sent
8. **广播通知**：通过 `BroadcastHelper` 发送 `agent_timeout` 临时事件通知（reason: `"hitl_timeout"`）和 `conversation_update` 通知客户端刷新状态（仅在 `broadcaster != nil` 时）
9. **锁释放**：通过 Redis TTL（30 秒）自动释放，无需显式 DEL

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| 获取锁失败 | 跳过该会话，其他节点正在处理 |
| 获取锁后状态已变更 | 重新检查后跳过，避免误清理 |
| 清理期间 panic | 每个会话独立 recover，不影响其他会话（双层 recover：per-cycle + per-conversation） |
| 数据库不可达 | 记录错误日志，下次重试 |
| Redis 不可达 | 锁获取失败（SetNX 返回 error），跳过该会话 |
| questionStore 为 nil | 跳过 questions 清理（nil-safe, D-063） |
| checkpointStore 为 nil | 跳过 checkpoint 清理（nil-safe） |
| broadcaster 为 nil | 跳过广播通知（nil-safe） |
| 发送超时消息失败 | 记录错误日志，不影响其他清理步骤 |
| 批量处理上限 | 每次最多处理 100 条（BatchSize 可配置），剩余下次处理 |
| AgentID 与 UserID1 相同 | humanUserID 回退为 UserID2 |

---

## 上下文缓存清理

### 概述

定期清理过期的对话上下文缓存，防止内存泄漏。上下文缓存（`DBContextManager` 的 `sync.Map`）用于加速 Agent 执行，避免每次都从数据库加载消息。默认 TTL 为 30 秒（可通过 `WithCacheTTL` 配置）。

### 流程图

```mermaid
sequenceDiagram
    participant BG as 后台 Goroutine
    participant CM as ContextManager
    participant Cache as 内存缓存

    loop 每 5 分钟
        BG->>CM: StartCleanup(ctx, interval)
        CM->>Cache: 遍历所有缓存条目

        loop 每个缓存条目
            Cache->>Cache: 检查是否过期
            alt 已过期
                Cache->>Cache: 删除条目
            end
        end
    end
```

### 详细步骤

1. **定时触发**：每 5 分钟触发一次（默认值，可通过 `interval` 参数配置）
2. **遍历缓存**：使用 `sync.Map.Range` 遍历所有缓存条目
3. **检查过期**：检查每个条目的 `fetchedAt` 时间，与当前时间比较是否超过 TTL（默认 30 秒）
4. **删除过期条目**：删除超过 TTL 的条目，同时删除类型不正确的损坏条目
5. **Panic Recovery**：`cleanupExpired` 方法有 `defer recover()` 保护，防止清理 panic 终止 goroutine。注意：recover 后静默继续（不记录 panic 信息，因为没有 logger 引用）

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| 缓存为空 | 静默跳过 |
| 并发访问 | `sync.Map` 支持并发读写，无需额外锁 |
| 清理期间有新请求 | 新请求正常处理，不受影响 |
| 条目类型损坏 | 检测后删除损坏条目（`value.(*cachedContext)` 类型断言失败） |
| 清理 panic | `defer recover()` 保护，静默继续（无 logger） |

---

## 工具结果清理

### 概述

定期清理过期的工具执行结果，防止内存泄漏。工具结果存储（`ToolResultStore`）用于 `retrieve_tool_result` 工具，允许 Agent 异步获取被截断的工具执行结果。默认 TTL 为 1 小时（`DefaultTTL`），最大条目数 10000（`DefaultMaxSize`）。

### 流程图

```mermaid
sequenceDiagram
    participant BG as 后台 Goroutine
    participant TRS as ToolResultStore
    participant Store as 内存存储

    loop 每 5 分钟
        BG->>TRS: StartCleanup(ctx, interval)
        TRS->>Store: 遍历所有结果条目

        loop 每个结果条目
            Store->>Store: 检查是否过期
            alt 已过期
                Store->>Store: 删除条目
            end
        end
    end
```

### 详细步骤

1. **定时触发**：由调用方通过 `StartCleanup(ctx, interval)` 启动，服务端在 `main.go` 中以 5 分钟间隔启动（`DefaultToolResultStore.StartCleanup(ctx, 5*time.Minute)`）
2. **遍历结果**：持有写锁（`sync.RWMutex.Lock`），遍历 `map[string]storedResult` 中的所有条目
3. **检查过期**：检查每个结果的 `createdAt` 时间，与当前时间比较是否超过 TTL（默认 1 小时）
4. **删除过期结果**：删除超过 TTL 的结果
5. **容量驱逐**：当条目数超过 `maxSize`（默认 10000）时，`Store` 方法会主动驱逐最旧的条目（`evictOldest` 遍历全部条目找 oldest）

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| 存储为空 | 静默跳过 |
| 并发访问 | `sync.RWMutex` 保护（Cleanup 持有写锁，Retrieve/Store 使用读锁/写锁） |
| 清理期间有新请求 | Cleanup 持有写锁期间新请求会阻塞，完成后正常处理 |
| 超过最大容量 | 写入时主动驱逐最旧条目 |
| panic recovery | **无 recovery**，Cleanup 方法无 `defer recover()`，依赖调用方的 goroutine 管理 |

---

## Rate Limiter 清理

### 概述

定期清理未使用的 rate limiter 条目，防止内存泄漏。`set_typing` 和 `stream_text` handler 各自维护独立的 per-user-per-conversation rate limiter（`sync.Map`），各自运行独立的清理 goroutine。

- `stream_text`：50ms 间隔限制（20/s），使用 `streamingRateLimiter`
- `set_typing`：1 秒间隔限制，使用 `typingRateLimiter`

### 流程图

```mermaid
sequenceDiagram
    participant BG1 as stream_text 清理 Goroutine
    participant BG2 as set_typing 清理 Goroutine
    participant Map1 as stream_text limiters (sync.Map)
    participant Map2 as set_typing limiters (sync.Map)

    par stream_text 清理 (每 5 分钟)
        BG1->>BG1: cleanupStaleLimiters()
        BG1->>Map1: Range(遍历所有条目)
        loop 每个条目
            Map1->>BG1: key, value
            BG1->>BG1: 检查 lastAccess
            alt lastAccess < cutoff (10分钟前)
                BG1->>Map1: Delete(key)
            end
        end
    and set_typing 清理 (每 5 分钟)
        BG2->>BG2: cleanupStaleLimiters()
        BG2->>Map2: Range(遍历所有条目)
        loop 每个条目
            Map2->>BG2: key, value
            BG2->>BG2: 检查 lastAccess
            alt lastAccess < cutoff (10分钟前)
                BG2->>Map2: Delete(key)
            end
        end
    end
```

### 详细步骤

1. **定时触发**：每 5 分钟触发一次（两个独立 goroutine，分别在 `NewStreamTextHandler` 和 `NewSetTypingHandler` 时启动）
2. **遍历条目**：遍历 `sync.Map` 中的所有条目
3. **检查访问时间**：获取每个条目的 `lastAccess` 时间（加锁读取后立即释放）
4. **删除过期条目**：删除 10 分钟未访问的条目
5. **注意**：`cleanupLoop` 没有 panic recovery，也没有 context 取消机制（`for range ticker.C` 无限循环），goroutine 依赖进程退出终止。如果 `cleanupStaleLimiters` panic，goroutine 将终止，rate limiter 条目将不再被清理

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| Map 为空 | 静默跳过 |
| 并发访问 | `sync.Map` 支持并发读写 |
| 清理期间有新请求 | 新请求正常处理，`LoadOrStore` 原子操作 |
| 条目被访问时清理 | 读取 `lastAccess` 时加锁（`rl.mu.Lock`），获取值后立即释放，避免数据竞争 |
| cleanupLoop panic | **无 recovery**，goroutine 终止，rate limiter 条目不再被清理直到进程重启 |
| context 取消 | **不响应 ctx.Done()**，goroutine 持续运行直到进程退出 |

---

## 客户端日志清理

### 概述

客户端 daemon（`xyncra listen`）定期清理本地 SQLite 数据库中的过期 RPC 日志和通知日志，防止数据库无限增长。这是唯一运行在客户端侧的清理任务（D-040）。

### 流程图

```mermaid
sequenceDiagram
    participant BG as 客户端清理 Goroutine
    participant DB as 客户端 SQLite
    participant Logger as cliLogger

    loop 每 1 小时
        BG->>BG: runCleanup(retention=7天)
        BG->>DB: BEGIN TRANSACTION (db.Transaction)

        BG->>DB: DELETE FROM rpc_logs WHERE created_at < before (GORM model delete)
        DB-->>BG: 删除行数

        BG->>DB: DELETE FROM notification_logs WHERE created_at < before (GORM model delete)
        DB-->>BG: 删除行数

        alt 任一删除失败
            BG->>DB: ROLLBACK (自动)
            BG->>Logger: Error("auto-cleanup", error)
        else 全部成功
            BG->>DB: COMMIT (自动)
        end
    end
```

### 详细步骤

1. **定时触发**：每 1 小时触发一次（`defaultCleanupInterval`），通过 `startLogCleanup` goroutine 运行，响应 `ctx.Done()` 退出
2. **事务执行**：在单个 GORM 事务（`db.Transaction`）中执行两个 DELETE 操作（L-1 原子性）。`runCleanup` 内部创建 `context.Background()`
3. **清理 RPC 日志**：GORM model delete `&model.RPCLog{}` 条件 `created_at < before`
4. **清理通知日志**：GORM model delete `&model.NotificationLog{}` 条件 `created_at < before`
5. **错误处理**：清理失败通过 `cliLogger.Error` 记录日志，不终止 daemon

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| 数据库不可达 | 记录错误日志，下次重试 |
| 无过期记录 | 事务正常提交，无副作用 |
| 部分删除失败 | 事务回滚，两个表保持一致 |

---

## 依赖关系

### 服务端内部依赖

| 组件 | 包路径 | 用途 |
|------|--------|------|
| `UserUpdateStore` | `internal/store` | UserUpdate 过期清理 |
| `UserUpdateCleaner` | `internal/cleanup` | UserUpdate 清理调度器 |
| `ConversationStore` | `internal/store` | HITL 超时清理（ListStaleHITLConversations, ClearAgentStatus, Get） |
| `QuestionStore` | `internal/store` | HITL 超时清理（DeleteByCheckpoint，可选） |
| `DeletableCheckPointStore` | `internal/agent` | HITL 超时清理（Delete，可选） |
| `BroadcastHelper` | `internal/agent` | HITL 清理后广播通知（可选，nil-safe） |
| `StoreAPI` (DataStore) | `internal/store` | HITL 超时消息发送（SendMessage） |
| `redisClient` | `internal/agent` | HITL 分布式锁（SetNX） |
| `DBContextManager` | `internal/agent` | 上下文缓存清理 |
| `ToolResultStore` | `internal/agent/tools` | 工具结果清理 |
| `streamTextHandler` | `internal/handler` | stream_text rate limiter 清理 |
| `setTypingHandler` | `internal/handler` | set_typing rate limiter 清理 |

### 客户端内部依赖

| 组件 | 包路径 | 用途 |
|------|--------|------|
| `ClientDB` | `pkg/store` | 客户端日志清理（RPCLog, NotificationLog） |
| `cliLogger` | `internal/cli` | 清理日志输出 |

### 外部依赖

| 组件 | 用途 |
|------|------|
| Database (GORM) | UserUpdate、Conversation、Question、RPCLog、NotificationLog 表 |
| Redis | CheckPoint 存储、分布式锁（SETNX + TTL） |

---

## 关键设计决策

### 1. Fire-and-Forget

清理任务采用 fire-and-forget 模式：
- **原因**：清理失败不影响业务逻辑
- **行为**：失败仅记录日志，下次重试
- **优点**：避免清理任务阻塞业务流程

### 2. Distributed Lock

HITL 清理使用分布式锁：
- **原因**：多节点部署时避免重复清理
- **实现**：Redis SETNX（key: `hitl:cleanup:{conversationID}`）
- **TTL**：30 秒（通过 TTL 自动释放，无需显式 DEL）

### 3. Panic Recovery（部分覆盖）

部分清理任务有 panic recovery：
- **有 recovery**：UserUpdateCleaner（per-cycle，记录日志）、HITLCleanupTask（per-cycle + per-conversation，记录日志）、DBContextManager.cleanupExpired（per-cycle，**静默** recover 无日志）
- **无 recovery**：ToolResultStore.StartCleanup、streamTextHandler.cleanupLoop、setTypingHandler.cleanupLoop、startLogCleanup
- **原因**：防止清理任务崩溃导致 goroutine 泄漏
- **行为**：recover 后继续运行
- **日志**：UserUpdateCleaner 和 HITLCleanupTask 记录 panic 信息用于调试；DBContextManager 静默继续（无 logger 引用）

### 4. Graceful Degradation

部分清理失败不影响其他清理：
- **原因**：提高系统容错性
- **行为**：每个会话的清理独立进行（HITL 清理中每个会话有独立的 recover）
- **日志**：记录具体失败信息

### 5. 客户端日志清理事务原子性（L-1）

客户端日志清理使用数据库事务：
- **原因**：RPC 日志和通知日志应在同一事务中清理
- **行为**：任一删除失败则回滚，两个表保持一致
- **优点**：避免部分清理导致的数据不一致

---

## 相关文档

- [Agent 执行流程](agent-execution.md)
- [Agent Resume (HITL)](agent-resume.md)
- [WebSocket 连接管理](websocket-connection.md)
- [存储层](storage.md)
