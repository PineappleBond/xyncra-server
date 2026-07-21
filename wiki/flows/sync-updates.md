# Sync Updates 业务流程

本文档描述 `sync_updates` RPC 方法的完整业务流程，包括主流程、边缘场景和依赖关系。

---

## 目录

- [概述](#概述)
- [主流程](#主流程)
- [边缘场景](#边缘场景)
- [依赖关系](#依赖关系)
- [关键设计决策](#关键设计决策)

---

## 概述

`sync_updates` 是客户端增量拉取事件流的核心方法。客户端发送 `after_seq`（最后看到的序列号）和 `limit`，服务端返回 `after_seq` 之后的更新列表，包含 `has_more` 标志和 `latest_seq` 用于检测间隙。

### 触发条件

- 客户端重连后同步错过的更新
- 客户端定期轮询获取新更新
- 客户端检测到序列号间隙时补全

### 关键特性

- **Cursor-based pagination**：基于 `after_seq` 的游标分页
- **Gap filling**：自动填充缺失的序列号位置
- **Sequence detection**：返回 `latest_seq` 供客户端检测间隙
- **Limit capping**：默认 100，上限 500

---

## 主流程

```mermaid
sequenceDiagram
    participant C as 客户端
    participant WS as WebSocket Server
    participant H as SyncUpdatesHandler
    participant S as Store

    C->>WS: sync_updates {after_seq, limit}
    WS->>H: HandleRequest(ctx, client, req)

    H->>H: 解析参数
    H->>H: 规范化 limit (默认 100, 上限 500)

    H->>S: UserUpdateStore.GetLatestSeq(userID)
    S-->>H: 返回 latestSeq

    alt latestSeq <= afterSeq
        H-->>WS: 返回空 updates, has_more=false
        WS-->>C: 成功响应
    end

    H->>H: 计算 expectedEnd = min(afterSeq + limit, latestSeq)
    H->>S: UserUpdateStore.ListByUserRange(userID, afterSeq, expectedEnd)
    S-->>H: 返回 actualUpdates

    H->>H: 构建 seq -> update lookup map
    H->>H: 遍历 [afterSeq+1, expectedEnd], 填充 gaps

    H->>H: 计算 has_more = afterSeq + limit < latestSeq

    H-->>WS: 返回 {updates, has_more, latest_seq}
    WS-->>C: 成功响应
```

### 详细步骤

1. **解析参数**：提取 `after_seq` 和 `limit`
2. **规范化 limit**：
   - 默认值：100
   - 上限：500
   - 下限：1（<=0 时设为 100）
3. **获取 latestSeq**：查询用户的最新序列号
4. **早期返回**：如果没有新更新（`latestSeq <= afterSeq`），返回空结果
5. **计算范围**：`expectedEnd = min(afterSeq + limit, latestSeq)`
6. **查询实际更新**：获取 `(afterSeq, expectedEnd]` 范围内的更新
7. **构建 lookup map**：将实际更新按 seq 索引
8. **填充 gaps**：遍历 `[afterSeq+1, expectedEnd]`，缺失的位置用 `UpdateTypeGap` 填充
9. **计算 has_more**：`afterSeq + limit < latestSeq`
10. **返回结果**：返回 `{updates, has_more, latest_seq}`

---

## 边缘场景

### 1. 参数校验

```mermaid
flowchart TD
    A[解析参数] --> B{解析成功?}
    B -->|失败| C[返回 ValidationError]
    B -->|成功| D[规范化 limit]
    D --> E{limit <= 0?}
    E -->|是| F[设为 100]
    E -->|否| G{limit > 500?}
    G -->|是| H[设为 500]
    G -->|否| I[保持原值]
```

| 场景 | 处理方式 |
|------|----------|
| JSON 解析失败 | 返回 `ValidationError('invalid params')` |
| `limit <= 0` | 设为默认值 100 |
| `limit > 500` | 设为上限 500 |
| `after_seq = 0` | 从头开始拉取 |

### 2. 无新更新

```mermaid
flowchart TD
    A[获取 latestSeq] --> B{latestSeq <= afterSeq?}
    B -->|是| C[返回空 updates, has_more=false]
    B -->|否| D[继续处理]
```

| 场景 | 处理方式 |
|------|----------|
| `latestSeq = 0` | 用户没有任何更新 |
| `afterSeq >= latestSeq` | 客户端已是最新状态 |

### 3. Gap Filling

```mermaid
flowchart TD
    A[遍历 seq 范围] --> B{seq 在 actualMap 中?}
    B -->|是| C[使用实际更新]
    B -->|否| D[创建 Gap 更新]
    D --> E[Type=gap, Seq=seq, Payload=nil]
```

| 场景 | 处理方式 |
|------|----------|
| 序列号间隙 | 自动填充 `UpdateTypeGap` 占位符 |
| 间隙原因 | 并发写入、事务回滚、数据清理 |
| 客户端处理 | 收到 gap 更新后可决定是否补全 |

### 4. 分页

```mermaid
flowchart TD
    A[计算 has_more] --> B{afterSeq + limit < latestSeq?}
    B -->|是| C[has_more = true]
    B -->|否| D[has_more = false]
```

| 场景 | 处理方式 |
|------|----------|
| 还有更多数据 | `has_more = true`，客户端应继续拉取 |
| 已拉取完毕 | `has_more = false` |
| 刚好拉完 | `has_more = false` |

### 5. Store 错误

```mermaid
flowchart TD
    A[查询 Store] --> B{查询成功?}
    B -->|失败| C[返回 InternalError]
    B -->|成功| D[继续处理]
```

| 场景 | 处理方式 |
|------|----------|
| `GetLatestSeq` 失败 | 返回 `InternalError` |
| `ListByUserRange` 失败 | 返回 `InternalError` |

### 6. uint32 溢出

| 场景 | 处理方式 |
|------|----------|
| `afterSeq + limit` 溢出 uint32 上限 | `expectedEnd` 计算可能回绕，导致查询范围错误。在约 43 亿序列号后实际可能发生 |
| `latestSeq <= afterSeq` 在回绕后 | 当 `afterSeq` 接近 uint32 最大值而 `latestSeq` 已回绕时，比较结果不正确 |

---

## 依赖关系

### 内部依赖

| 组件 | 用途 |
|------|------|
| `store.StoreAPI` | 查询 UserUpdate 数据 |

### 外部依赖

| 组件 | 用途 |
|------|------|
| Database | UserUpdate 表 |

### 数据库操作

| 操作 | 表 | 说明 |
|------|-----|------|
| SELECT MAX(seq) | user_updates | 获取用户最新序列号 |
| SELECT | user_updates | 查询指定范围内的更新 |

---

## 关键设计决策

### 1. Cursor-based Pagination

使用 `after_seq` 作为游标：
- **优点**：客户端只需记住最后看到的 seq
- **优点**：支持断点续传
- **优点**：避免 offset-based 分页的数据偏移问题

### 2. Gap Filling

自动填充缺失的序列号：
- **原因**：客户端需要连续的序列号来检测间隙
- **实现**：使用 `UpdateTypeGap` 占位符
- **Payload**：nil，不携带实际数据

### 3. Limit Capping

限制单次拉取数量：
- **默认值**：100（平衡网络开销和响应时间）
- **上限**：500（防止过大的响应）
- **下限**：1（至少返回 1 条）

### 4. LatestSeq 返回

返回用户的最新序列号：
- **用途**：客户端可以检测是否还有未拉取的更新
- **实现**：在查询实际更新之前获取，用于早期返回判断

---

## 客户端实现建议

### 拉取策略

```mermaid
flowchart TD
    A[开始同步] --> B[发送 sync_updates afterSeq=localMaxSeq]
    B --> C{has_more?}
    C -->|是| D[更新 afterSeq=latestSeq]
    D --> B
    C -->|否| E[同步完成]
```

### 间隙检测

```mermaid
flowchart TD
    A[收到 updates] --> B[遍历 updates]
    B --> C{seq == expectedSeq?}
    C -->|否| D[检测到间隙]
    D --> E[决定是否补全]
    C -->|是| F[更新 expectedSeq]
```

### 错误处理

```mermaid
flowchart TD
    A[发送 sync_updates] --> B{响应成功?}
    B -->|是| C[处理 updates]
    B -->|否| D{错误类型?}
    D -->|网络错误| E[重试]
    D -->|其他错误| F[记录日志, 延迟重试]
```

---

## 与其他流程的关系

### 重连后同步

```mermaid
sequenceDiagram
    participant C as 客户端
    participant WS as WebSocket Server

    C->>WS: system.reconnect {last_seen_seq}
    WS-->>C: 返回 replayed count

    C->>WS: sync_updates {after_seq: localMaxSeq}
    WS-->>C: 返回 updates

    Note over C: 应用 updates 到本地
```

### 实时推送 + 增量拉取

```mermaid
flowchart TD
    A[实时推送] --> B[WebSocket Updates]
    C[增量拉取] --> D[sync_updates]

    B --> E[客户端处理]
    D --> E

    E --> F[更新 localMaxSeq]
```

---

## 相关文档

- [断线重连](reconnection.md)
- [消息处理](message.md)
- [WebSocket 连接管理](websocket-connection.md)
- [UserUpdate 存储](../architecture/user-update.md)
