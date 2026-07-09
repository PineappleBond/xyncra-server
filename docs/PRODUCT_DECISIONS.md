# Xyncra Server 产品决策文档

本文档记录了 Xyncra 消息系统服务器的核心架构决策。所有开发者和子代理在实现功能时必须遵守这些决策。

---

## 决策概览

| 编号 | 决策 | 原因 |
|------|------|------|
| D-001 | 开箱即用，零配置启动 | 降低部署门槛 |
| D-002 | 认证由业务服务器负责，服务器本身不做鉴权 | 职责分离 |
| D-003 | 内网部署模型，通过反向代理暴露服务 | 安全边界外置 |
| D-004 | 默认接受任意 Origin | 内网部署无需 CSRF 防护 |
| D-005 | 简单的 user_id 查询参数认证作为默认 | 开发友好 |
| D-018 | 多节点消息路由，Redis Pub/Sub 实现跨节点推送 | 水平扩展能力 |
| D-027 | 客户端扩展错误码 -400 到 -402（ConnectionError、TimeoutError、SyncError） | 客户端错误分类与服务器错误码体系统一 |
| D-028 | UserUpdate 类型字段 | 支持多种 Update 类型的分类处理和查询 |
| D-029 | sync_updates 补空策略 | 服务器运行时生成 gap 占位 Update，不持久化 |
| D-030 | CLI 进程间通信协议 | Unix Socket + JSON-RPC 2.0，换行符分隔 |
| D-031 | CLI 进程锁实现 | github.com/gofrs/flock + stale lock 检测 |
| D-032 | CLI IPC Fallback 策略 | IPC 优先，失败 fallback 到 WebSocket 短连接 |
| D-033 | CLI 设备 ID 生成 | 主机名 SHA256 前 8 位十六进制，匿名化 |
| D-034 | CLI 环境变量命名规范 | XYNCRA_ 前缀，flag > 环境变量 > 默认值 |
| D-035 | CLI 查询命令使用本地数据库读取 | list-conversations/get-conversation/get-messages/search-messages 直接读本地 SQLite |
| D-036 | sync-updates 命令为 IPC-only | 触发守护进程的 FullSync，无 standalone fallback（D-032 例外） |
| D-037 | CLI flag 不遮蔽全局 flag | create-conversation 使用 --peer-id 而非 --user-id |
| D-038 | CLI 消息 ID flag 类型区分 | delete-message 用 --message-id (string UUID)，mark-as-read 用 --message-id (uint32) |
| D-039 | CLI kill 命令行为规范 | 默认 SIGTERM，--force 升级 SIGKILL，进程退出后清理文件 |
| D-040 | CLI logs 数据保留策略 | 默认保留 7 天，同时清理 RPCLogs 和 NotificationLogs |
| D-041 | CLI 输出格式标准 | 标准库 tabwriter，不引入第三方依赖 |
| D-042 | CLI 退出码标准 | 0=成功, 1=通用错误, 2=前置条件不满足, 3=超时退出 |
| D-043 | E2E 测试端口约定 | Redis 16379, Server 18080, DB 15 |
| D-044 | listen daemon 连接韧性策略 | 无限重试 WS 连接，IPC 始终可用 |
| D-045 | create_conversation 实时通知 | 创建会话时推送 UserUpdate + MQ 广播 |
| D-046 | CLI send --client-msg-id 可选 flag | 默认自动生成 UUID，用于调试和测试 D-006 幂等性 |
| D-047 | mark-as-read 显示 server 实际游标 | CLI 显示 MAX 语义后的实际 last_read_message_id |

---

## D-001: 开箱即用，零配置启动

### 决策

Xyncra Server 设计为开箱即用的消息服务器。开发者可以直接下载、构建、启动，无需复杂配置即可运行。

### 原因

1. **降低准入门槛**：让开发者能快速试用和评估系统
2. **减少运维负担**：不需要复杂的配置文件和环境变量
3. **快速迭代**：开发阶段不需要处理配置管理

### 实现

- 使用合理的默认值（端口、缓冲区大小、超时时间等）
- 通过 Functional Options 模式提供可选配置覆盖
- 核心依赖（Redis）使用标准默认地址 `localhost:6379`

### 示例

```go
// 零配置启动
srv, _ := server.NewWebSocketServer(
    server.WSWithAddr(":8080"),
    server.WSWithConnectionStore(connStore),
)
```

---

## D-002: 认证由业务服务器负责，服务器本身不做鉴权

### 决策

Xyncra Server **不实现任何认证/鉴权机制**。认证逻辑由部署方的业务服务器实现，通过反向代理传递认证后的用户信息。

### 原因

1. **职责分离**：Xyncra 专注于消息推送，认证是业务逻辑
2. **灵活性**：不同业务场景有不同的认证需求（JWT、OAuth、Session 等）
3. **简化核心**：避免在核心服务中引入认证相关的复杂性
4. **安全性**：认证策略由业务方根据具体需求定制，更可靠

### 架构模型

```
┌──────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   客户端     │────▶│  业务服务器      │────▶│  Xyncra Server  │
│  (Browser)   │     │ (认证+反向代理)  │     │  (消息推送)     │
└──────────────┘     └──────────────────┘     └─────────────────┘
                            │
                            ├─ 验证用户身份
                            ├─ 签发 token/session
                            └─ 反向代理时注入 user_id
```

### 默认行为

- `defaultAuthenticate` 从 URL 查询参数 `user_id` 提取用户 ID
- 这只是一个开发便利的默认实现，**不是安全机制**
- 生产环境中，业务服务器应在反向代理层重写请求，注入已认证的 `user_id`

### 开发者指南

如果你要在生产环境使用 Xyncra Server：

1. 在你的业务服务器中实现认证（JWT、OAuth 等）
2. 认证通过后，在反向代理请求中注入 `user_id` 参数或 Header
3. 如果需要自定义认证逻辑，使用 `WSWithAuthenticate(fn)` 覆盖默认行为

```go
// 自定义认证：从 Header 中提取已认证的用户 ID
srv, _ := server.NewWebSocketServer(
    server.WSWithAuthenticate(func(r *http.Request) (string, error) {
        // 业务服务器已在反向代理时注入此 Header
        userID := r.Header.Get("X-Authenticated-User")
        if userID == "" {
            return "", errors.New("not authenticated")
        }
        return userID, nil
    }),
)
```

---

## D-003: 内网部署模型，通过反向代理暴露服务

### 决策

Xyncra Server 设计为部署在内网环境中，通过业务服务器的反向代理对外暴露服务。

### 原因

1. **安全边界外置**：内网环境天然隔离外部攻击
2. **简化核心代码**：不需要实现 TLS、CORS、Rate Limit 等边缘功能
3. **专注核心价值**：资源集中在消息推送功能上
4. **灵活部署**：可配合 Nginx、Envoy、Traefik 等多种反向代理

### 部署架构

```
                    公网
                     │
              ┌──────▼──────┐
              │   Nginx     │  ← TLS 终止、CORS、Rate Limit
              │  (反向代理) │
              └──────┬──────┘
                     │ 内网
              ┌──────▼──────┐
              │   业务服务器 │  ← 认证、业务逻辑
              └──────┬──────┘
                     │
              ┌──────▼──────┐
              │ Xyncra      │  ← 消息推送
              │ Server      │
              └─────────────┘
```

### 含义

- **不内置 TLS**：由反向代理处理
- **不内置 CORS**：由反向代理处理
- **不内置 Rate Limit**：由反向代理处理
- **不内置 CSRF 防护**：内网部署不需要

---

## D-004: 默认接受任意 Origin

### 决策

WebSocket Upgrader 的 `CheckOrigin` 默认返回 `true`，接受任意来源的连接。

### 原因

1. **内网部署**：不存在跨域攻击的威胁模型
2. **开发友好**：无需配置允许的 Origin 列表即可开始开发
3. **职责分离**：CORS 策略由反向代理统一处理

### 代码实现

```go
upgrader := websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true // 内网部署，无需 CSRF 防护
    },
}
```

### 注意事项

- 这是**有意的设计决策**，不是 TODO
- 如果直接暴露到公网，需要在反向代理层添加 CORS 策略
- 或者使用自定义 Upgrader 覆盖默认行为（不推荐）

---

## D-005: 简单的 user_id 查询参数认证作为默认

### 决策

默认认证函数 `defaultAuthenticate` 从 URL 查询参数 `user_id` 提取用户 ID，无签名验证。

### 原因

1. **开箱即用**：开发者可以立即开始测试，无需配置认证
2. **开发便利**：`ws://localhost:8080/ws?user_id=alice` 即可连接
3. **职责分离**：真正的认证由业务服务器负责

### 安全警告

⚠️ **此默认实现不安全，仅适用于开发环境！**

- 任何知道用户 ID 的人都可以冒充该用户
- user_id 会出现在 URL 中，可能被记录到日志
- 生产环境**必须**通过业务服务器的反向代理注入已认证的 user_id

### 生产环境使用

在生产环境中，业务服务器应该：

1. 验证用户身份（JWT、Session 等）
2. 在反向代理请求中**重写** URL 或添加 Header
3. 确保 Xyncra Server 接收到的 user_id 是可信的

```nginx
# Nginx 反向代理示例
location /ws {
    # 业务服务器验证 JWT 后，设置此变量
    proxy_set_header X-Authenticated-User $authenticated_user;
    
    # 重写 URL，注入已认证的用户 ID
    rewrite ^/ws$ /ws?user_id=$authenticated_user break;
    
    proxy_pass http://xyncra-server:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

---

## 决策的影响

### 对开发者的影响

1. **开发阶段**：可以直接使用默认配置，快速开始开发
2. **生产部署**：需要在业务服务器中实现认证，并配置反向代理
3. **安全责任**：认证和边缘安全由业务方负责

### 对代码实现的影响

1. **不实现**：TLS、CORS、CSRF、Rate Limit、认证
2. **实现**：消息推送、连接管理、心跳、广播
3. **提供扩展点**：`WSWithAuthenticate` 等选项允许覆盖默认行为

### 对测试的影响

1. **不需要**测试认证失败场景（不是我们的职责）
2. **需要**测试消息推送功能
3. **需要**测试连接管理功能

---

---

## D-006: client_message_id 幂等性模型

### 决策

send_message 使用客户端提供的 `client_message_id` 实现幂等性。数据库通过 uniqueIndex 保证唯一性。当检测到重复的 `client_message_id` 时，服务器返回已持久化的消息记录（静默命中），而非报错。

`client_message_id` 的生成策略由客户端决定（建议使用 UUID v4），服务器不做格式校验，仅要求非空且唯一。

### 原因

1. **客户端友好**：客户端只需生成 UUID，重试时无需区分"首次发送"和"重试"
2. **简单可靠**：数据库唯一约束是最终的幂等保证
3. **网络容错**：WebSocket 断开重连后，客户端可以安全地重新发送未确认的消息

### 响应行为

- 首次成功：返回 `code=0`，`duplicate=false`（或不包含此字段）
- 幂等命中：返回 `code=0`，`duplicate=true`，消息内容与首次完全一致

---

## D-007: MQ 入队失败的 fire-and-forget 策略

### 决策

send_message 的核心数据（消息 + UserUpdate + 会话更新）在数据库事务中持久化后，MQ 入队（用于向在线用户实时推送）是异步操作。MQ 入队失败不会导致 send_message 返回错误——消息已经安全存储，推送失败不影响数据完整性。

离线用户通过 `sync_updates` 拉取增量更新，保证最终一致性。

### 原因

1. **数据优先**：消息持久化是第一优先级，实时推送是增强体验
2. **容错性**：MQ 暂时不可用时，消息不丢失
3. **最终一致**：离线用户通过增量同步获得所有更新

---

## D-008: MessageID 分配策略

### 决策

每个会话内的消息使用单调递增的 `uint32` 序号（`MessageID = conv.LastProcessedMessageID + 1`）。序号在数据库事务中分配，保证同一会话内不重复。不同会话的 MessageID 空间独立。

`uint32` 上限约 42 亿条消息/会话，对于可预见的用例绰绰有余。

### 原因

1. **有序性**：uint32 递增序号提供天然的消息排序
2. **增量同步**：客户端可以用 `last_message_id` 拉取增量消息
3. **原子性**：在数据库事务内分配，避免并发冲突

---

## 相关文档

- [API 文档](./API.md) - WebSocket 协议说明

---

## D-009: sync_updates 分页模型

### 决策

sync_updates 使用 `after_seq`（排他性下界）+ `limit` 进行分页，默认 limit=100，上限 500。`after_seq=0` 表示从头开始拉取。

### 原因

1. **确定性分页**：与 D-008（uint32 单调递增 seq）配合，提供确定性的分页游标
2. **避免重复/遗漏**：基于 cursor 的分页比 offset 分页更可靠，不受并发插入影响
3. **客户端友好**：`has_more` 让客户端知道是否需要继续拉取，`latest_seq` 让客户端更新本地游标

### 响应结构

```json
{
  "updates": [/* PackageDataUpdate 数组 */],
  "has_more": true,
  "latest_seq": 12345
}
```

- `has_more=true` 表示还有更多更新，客户端应继续调用 sync_updates
- `latest_seq` 是当前用户的全局最新 seq（非返回结果中最大的 seq）

---

## D-010: 被动续期策略

### 决策

Connection TTL 续期采用被动模式——仅由 heartbeat RPC 触发 `ConnectionStore.Refresh()`，不引入后台定时续期 goroutine。

### 原因

1. **零后台开销**：符合 D-001 的零配置哲学
2. **活跃绑定**：只有真正活跃的客户端才会被续期，死连接自然过期
3. **简单可靠**：无需定时任务、无需扫描所有连接

### 客户端行为

客户端应定期发送 heartbeat RPC（建议间隔 30-60 秒）以保持在线。如果客户端停止发送 heartbeat，连接会在 TTL（默认 30 分钟）后自动过期。

---

## D-011: create_conversation 的 find-or-create 幂等模型

### 决策

`create_conversation` 使用 find-or-create 模式实现幂等性：先通过 `GetByUsers` 查询是否已存在相同用户对的会话，存在则返回已有会话（`duplicate=true`），不存在则创建新会话。幂等性由**用户对唯一性**保证，而非客户端提供的幂等 key。

这与 D-006（`client_message_id` 幂等）机制不同：D-006 依赖客户端生成的唯一 ID，此处依赖业务层面的用户对去重逻辑。

### 原因

1. **简化客户端**：客户端无需生成和管理额外的幂等 key
2. **防止重复会话**：同一对用户之间的会话天然应该是唯一的
3. **幂等重试**：网络超时后客户端可以安全地重新调用 `create_conversation`，不会创建重复会话

### 约束

- 当前仅支持 1-on-1 会话类型，用户对唯一性等价于会话唯一性
- 如果未来支持 group/channel 类型，需要重新评估幂等策略（群组会话可能由相同创建者多次创建）

---

## D-012: mark_as_read 的已读位置模型

### 决策

`mark_as_read` 使用 Conversation 模型上的 `LastReadMessageID1` 和 `LastReadMessageID2` 字段（分别对应 UserID1 和 UserID2 的已读位置）。每个用户的已读游标独立维护，互不影响。

更新语义为 `MAX(current_value, new_value)`，即只向前推进、不后退。当客户端传入的 `message_id` 小于当前已读位置时，服务器静默忽略（不报错），返回当前已读位置。

### 原因

1. **简单高效**：1-on-1 会话只有两个成员，两个字段足够，无需单独的 `read_state` 表
2. **并发安全**：`MAX` 语义防止多设备并发导致已读位置回退
3. **与 D-008 配合**：已读位置可以用 uint32 表示，与 MessageID 单调递增模型一致
4. **查询高效**：未读计数只需 `CountUnread(convID, lastReadMessageID)`，无 JOIN

### 约束

- 当前 1-on-1 模型下两个字段足够。如果未来支持 group/channel，需要迁移到 per-user read state 表
- 已读位置不对对方主动暴露（不实现已读回执）。如需"对方已读"功能，需在后续迭代中扩展

---

## D-013: delete_conversation 级联软删除行为

### 决策

`delete_conversation` 执行级联软删除：先软删除会话，再软删除该会话下的所有消息（调用 `MessageStore.DeleteByConversation`）。两个操作在同一数据库事务中执行。

当前模型下，Conversation 是双方共享记录。一方删除会话，另一方的会话也会消失（GORM soft-delete 对双方生效）。这是当前阶段的有意识简化。

软删除的消息仍然占据 `client_message_id` 的 unique index 命名空间（GORM 软删除不修改 unique index 行为）。因此，如果会话被恢复，原有的 `client_message_id` 仍然有效；如果会话不被恢复，这些 `client_message_id` 将永久不可重用。

### 原因

1. **数据一致性**：级联删除保证不会出现孤立的无主消息
2. **可恢复性**：软删除保留数据，可通过 `restore_conversation` 恢复
3. **实现简单**：当前阶段不需要 per-user 的会话可见性管理
4. **client_message_id 安全**：建议使用 UUID v4，命名空间占用不构成实际问题

### 未来演进

如果未来需要"一方删除不影响另一方"的体验（类似微信），需要引入 `ConversationMember` 表记录 per-user 的 `deleted_at`，替代当前的共享软删除模型。

---

## D-014: delete_message 的权限模型

### 决策

`delete_message` 仅允许消息的发送者（`SenderID == client.UserID()`）删除该消息。非发送者调用返回权限错误。

此权限检查使用反向代理注入的 `user_id`（D-002 模型），服务器不验证身份本身，只执行业务规则。

### 原因

1. **与 D-002 一致**：服务器不做认证，但可以做业务规则检查
2. **最小权限原则**：仅发送者可删是即时通讯的标准行为
3. **简单可靠**：无需引入角色系统或管理员权限

### 未来演进

可通过系统级参数（如 `force=true`）支持管理员删除，但当前版本不需要。

---

## D-015: restore_conversation 级联恢复语义

### 决策

`restore_conversation` 恢复会话记录的同时，级联恢复该会话下所有被软删除的消息。两个操作在同一事务中执行。

恢复后，会话的 `LastProcessedMessageID` 重新计算为所有恢复消息中最大的 `MessageID`。`LastMessageAt` 也相应更新。

对未删除的会话调用 `restore_conversation` 是幂等的——返回当前会话，不报错。

### 原因

1. **避免孤立数据**：恢复消息而不恢复会话会导致孤立消息
2. **避免空会话**：恢复会话而不恢复消息会让用户看到空会话
3. **直觉一致**：级联恢复是最直观的用户体验
4. **幂等友好**：重复恢复不报错，客户端可安全重试

---

## D-016: UserUpdate 数据生命周期管理

### 决策

UserUpdate 记录保留 30 天（`DefaultCleanupRetention`），超过 30 天的记录由后台 goroutine 每小时硬删除（物理删除）。

### 原因

1. **控制存储增长**：UserUpdate 是临时性同步数据，用于 `sync_updates` 增量拉取，无需永久保留
2. **简化运维**：自动清理避免手动维护
3. **明确的同步边界**：离线超过 30 天的客户端需要全量同步而非增量同步

### 实现

- 后台 goroutine 使用 `time.NewTicker`，默认 1 小时间隔
- 调用 `UserUpdateStore.CleanupExpired(ctx)` 执行清理
- 清理失败仅记录日志，不影响服务运行
- 使用共享 `ctx`，SIGINT/SIGTERM 时自动退出

### 约束

- 硬删除不可恢复
- 30 天是离线客户端增量同步的有效期边界
- 如果业务需要长期保留同步数据，应存储在业务数据库中

---

## D-017: 结构化错误码体系

### 决策

API 响应使用结构化错误码，分段分配：

- `-100` 到 `-199`：客户端错误（validation、not_found、duplicate）
- `-200` 到 `-299`：权限错误（permission_denied、forbidden）
- `-300` 到 `-399`：服务端错误（internal、unavailable）

通过 `HandlerError` 类型在 handler 层返回带 code 的错误。`websocket_handler.go` 使用 `errors.As` 提取类型化错误并映射到对应的 ResponseCode。

向后兼容：旧客户端检查 `Code < 0` 仍然有效。未迁移的 handler 继续使用 `ResponseCodeError (-1)`。

### 原因

1. **精细化错误处理**：客户端可以基于具体错误码做针对性处理（如 not_found 时跳转、permission_denied 时提示）
2. **向后兼容**：现有客户端不受影响（`-1` 仍是合法错误码）
3. **可扩展**：新增错误类型不影响现有错误码
4. **类型安全**：通过 `HandlerError` 类型而非字符串匹配传递错误分类

---

## D-018: 多节点消息路由架构

### 决策

使用 Redis Pub/Sub 实现跨节点消息推送。每个节点启动时订阅 `xyncra:broadcast:*` pattern，`BroadcastUpdates` 内部同时执行本地推送和 Redis PUBLISH。通过 `SourceNodeID` 避免源节点重复推送。

### 原因

1. **水平扩展**：支持多实例部署，用户连接到任意节点都能收到实时推送
2. **实时性**：Pub/Sub 保证消息即时推送，不依赖轮询
3. **简单可靠**：Redis 是现有依赖（ConnectionStore + MQ），无需引入新的中间件
4. **签名兼容**：`BroadcastUpdates` 签名不变，调用方无需修改

### 架构

```text
Node A                          Redis                         Node B
  │                               │                             │
  ├─ BroadcastUpdates(userB)      │                             │
  │  ├─ 本地推送 userB ✓          │                             │
  │  └─ PUBLISH xyncra:broadcast:{userB}                        │
  │                               ├─ PSUBSCRIBE 分发 ──────────┤
  │                               │                             ├─ 本地推送 userB ✓
  │                               ├─ PSUBSCRIBE 分发 ──────────┤
  │                               │                             │
  ├─ PSUBSCRIBE 收到              │                             │
  │  └─ SourceNodeID == self → skip                           │
```

### 约束

- 多节点部署要求所有实例连接到同一 Redis 实例
- Pub/Sub 消息不持久化——如果所有节点同时不可用，消息丢失（但数据已持久化，可通过 sync_updates 恢复）
- 单节点部署无额外开销（Pub/Sub 消息会被自身消费并跳过）

---

## D-019: 容器化部署模型

### 决策

提供 multi-stage Dockerfile 和 docker-compose 作为推荐的部署方式。Dockerfile 使用 golang:alpine 构建 + alpine 运行（~15-20MB 镜像）。docker-compose 编排 xyncra-server 和 Redis 7。

### 原因

1. **开箱即用**：Docker 是最简单的部署方式，符合 D-001 零配置哲学
2. **环境一致性**：容器化避免环境差异导致的问题
3. **运维友好**：docker-compose 一键启动所有依赖
4. **HEALTHCHECK**：使用现有 `/health` 端点

### 约束

- 推荐但非强制——开发者仍可直接 `go run` 或 `go build` 运行
- 需要 Docker 和 docker-compose 环境
- 默认使用 SQLite（容器内持久化通过 volume 挂载）

---

## D-027: 客户端扩展错误码体系

### 决策

xyncra-client 在服务器错误码体系（-100 到 -399）基础上，新增客户端专属错误码段：

- `-400` ConnectionError：WebSocket 连接失败（网络不可达、服务器未启动等）
- `-401` TimeoutError：RPC 调用超时（请求发出但在超时时间内未收到响应）
- `-402` SyncError：增量同步失败（sync_updates 拉取异常、seq 间隙无法修复等）

客户端错误码与服务端错误码使用同一 `ResponseCode` 类型（`int32`），保持错误码体系统一。客户端在 `pkg/client/errors.go` 中定义这些错误码，并提供与服务器 `HandlerError` 模式一致的类型化错误结构 `ClientError`。

### 原因

1. **统一错误码空间**：-400 段自然延续 -100/-200/-300 的分段模式，语义清晰
2. **类型安全**：通过 `ClientError` 结构体而非字符串匹配传递错误分类
3. **向后兼容**：不修改 `pkg/protocol/errors.go`，客户端错误码仅在客户端包内定义
4. **可调试性**：错误码+日志格式让开发者能快速定位问题层（网络/超时/同步）

### 约束

- 客户端错误码仅在客户端包（`pkg/client`）中使用，服务器不会返回这些码
- 客户端解析服务器响应时，使用 `pkg/protocol` 中已有的错误码（-100 到 -399）
- `ClientError` 支持 `Unwrap()` 以保留原始错误链

---

## D-028: UserUpdate 类型字段

### 决策

`UserUpdate` 模型新增 `Type` 字段（`string` 类型），标识更新的业务类型。`PackageDataUpdate` 协议结构同步新增 `Type` 字段。定义 5 种类型常量：`message`（新消息）、`delete_message`（消息删除）、`mark_read`（已读位置更新）、`conversation`（会话状态变更）、`gap`（补空占位，仅运行时生成，不持久化）。

所有操作（`send_message`、`mark_as_read`、`delete_message`、`delete_conversation`、`restore_conversation`）共享同一个用户级 seq 空间。客户端使用单一的 `local_max_seq` 跟踪所有类型的更新进度。

`mark_as_read` 仅为操作用户创建 UserUpdate（通知其其他设备同步已读状态），不为对方创建——这与 D-012（已读位置不对对方暴露）一致。`delete_message`、`delete_conversation`、`restore_conversation` 为所有会话成员创建 UserUpdate，因为这些操作影响所有成员的数据。

### 原因

1. **客户端分类处理**：客户端根据 Type 字段决定如何处理每条 Update（保存消息、删除本地消息、更新已读位置等）
2. **多设备同步**：所有操作通过 sync_updates 传播到用户的所有设备
3. **数据库可查询性**：Type 字段作为数据库列，支持按类型过滤查询（`WHERE type = 'message'`）
4. **向后兼容**：JSON 中新增 `type` 字段，旧客户端自动忽略

---

## D-029: sync_updates 补空策略

### 决策

`sync_updates` 响应保证返回的 updates 列表 seq 连续。当实际数据库记录存在 seq 间隙时（如事务回滚、并发冲突导致某些 seq 缺失），服务器在 handler 层运行时生成 `type: "gap"` 的空 Update 填充缺失位置。

**gap 填充不持久化到数据库**。原因：gap 代表"此 seq 无实际事件"，不是真实事件；持久化会污染数据库并增加清理复杂度。

`has_more` 判断基于 `after_seq + limit < latest_seq`（而非返回记录数量）。返回的 updates 数量始终等于 `min(limit, latest_seq - after_seq)`。

### 原因

1. **客户端简化**：客户端可以顺序处理 updates 而无需自行检测间隙，减少客户端复杂度
2. **与客户端防抖互补**：即使服务器补空有 bug，客户端的防抖拉取（设计文档 §3.2.4）仍然可以兜底处理间隙
3. **零存储开销**：运行时生成，不增加数据库写入和存储
4. **确定性行为**：相同数据库状态始终产生相同的补空结果

---

## D-030: CLI 进程间通信协议

### 决策

xyncra-client CLI 使用 Unix Socket + JSON-RPC 2.0 协议进行进程间通信（IPC）。Socket 路径为 `~/.xyncra/{user_id}/{device_id}/xyncra.sock`。传输层使用换行符（`\n`）分隔消息，每条消息是一个完整的 JSON 对象。

### 原因

1. **标准协议**：JSON-RPC 2.0 是广泛使用的 RPC 协议，工具链丰富（如 `socat` 可直接调试）
2. **跨语言兼容**：未来如有其他语言的 CLI 实现，可直接复用协议
3. **路径编码身份**：Socket 路径天然编码了 (user_id, device_id)，自动路由到正确的 listen 实例
4. **本地安全**：Unix Socket 仅限本地访问，权限 0600

### 约束

- 仅限本地通信，不支持远程 IPC
- 消息大小受 `bufio.Scanner` buffer 限制（默认 1MB）
- listen 进程退出时需清理 socket 文件

---

## D-031: CLI 进程锁实现

### 决策

使用 `github.com/gofrs/flock`（fcntl 文件锁）实现进程锁，防止同一 (user_id, device_id) 重复启动 listen。锁路径为 `~/.xyncra/{user_id}/{device_id}/xyncra.lock`。支持 stale lock 检测：通过读取锁文件中的 PID 检查进程是否存活，如进程已死则自动清理并重试。

### 原因

1. **内核级锁**：fcntl 锁由内核管理，进程崩溃时自动释放
2. **跨平台**：支持 Linux、macOS、Windows
3. **Stale lock 处理**：进程被 SIGKILL 杀死后 flock 自动释放，但锁文件残留，需要 PID 存活检查来检测 stale 状态
4. **零配置**：无需额外服务（如 Redis）

### 约束

- 锁的粒度是 (user_id, device_id)，不同组合的 listen 互不影响
- stale lock 检测存在 TOCTOU 竞态（概率极低，可接受）

---

## D-032: CLI IPC Fallback 策略

### 决策

CLI 命令（如 send）优先通过 Unix Socket IPC 连接到运行中的 listen 进程。如果 IPC 连接失败（listen 未运行），自动 fallback 到 WebSocket 短连接模式（独立模式），直接建立 WebSocket 连接执行操作后关闭。

### 原因

1. **自动降级**：用户无需手动切换模式
2. **守护进程模式优先**：复用 listen 的连接，避免重复建立 WebSocket
3. **独立模式兜底**：listen 未运行时仍可执行一次性操作

### 约束

- 独立模式下无法接收实时推送
- fallback 超时 5 秒
- 独立模式使用 gorilla/websocket 直接连接，不走 XyncraClient（因为 Start() 是阻塞的）

---

## D-033: CLI 设备 ID 生成策略

### 决策

当 `--device-id` 未指定时，使用主机名的 SHA256 哈希前 8 位十六进制字符串作为默认设备 ID。

### 原因

1. **匿名化**：不暴露真实主机名
2. **确定性**：同一台机器总是生成相同的设备 ID
3. **足够唯一**：8 位十六进制（32 bit）对于单机设备标识绰绰有余
4. **零配置**：符合 D-001 开箱即用原则

---

## D-034: CLI 环境变量命名规范

### 决策

所有 CLI 全局参数支持对应的环境变量，使用 `XYNCRA_` 前缀。flag 名中的 `-` 转为 `_`。优先级：命令行 flag > 环境变量 > 默认值。

支持的环境变量：

- `XYNCRA_USER_ID` → `--user-id`
- `XYNCRA_DEVICE_ID` → `--device-id`
- `XYNCRA_SERVER` → `--server`
- `XYNCRA_DB_PATH` → `--db-path`
- `XYNCRA_LOG_DIR` → `--log-dir`（Phase 2 预留，当前 cliLogger 仅写 stderr）
- `XYNCRA_DEBUG` → 启用 debug 日志（值为 `"1"` 或 `"true"` 时启用）

### 原因

1. **灵活配置**：开发时可用环境变量避免每次输入 flag
2. **容器友好**：Docker 部署时通过环境变量注入配置
3. **命名空间隔离**：`XYNCRA_` 前缀避免与其他工具的环境变量冲突

---

## D-035: CLI 查询命令使用本地数据库读取

### 决策

`list-conversations`、`get-conversation`、`get-messages`、`search-messages` 四个查询命令直接读取本地 SQLite 数据库，不通过 IPC 转发到守护进程。使用 `store.New()` 打开数据库（WAL 模式支持并发读），读取完成后关闭连接。

### 原因

1. **本地优先架构**：数据已由 `listen` 守护进程同步到本地，无需再经网络获取
2. **离线可用**：即使服务器不可达，用户仍可查询已同步的数据
3. **性能**：避免 IPC + WebSocket 的双重开销
4. **与 D-023 一致**：AutoMigrate 保证表结构存在，新命令首次运行自动创建数据库

### 约束

- 查询结果反映的是最后一次同步时的状态，非实时
- 本地 SQLite 使用 WAL 模式 + `busy_timeout(5000)`，支持守护进程写入时并发读取
- `store.New()` 会运行 AutoMigrate（写操作），如果守护进程正在写入可能短暂等待

---

## D-036: sync-updates 命令为 IPC-only

### 决策

`sync-updates` 命令仅通过 IPC 触发守护进程的 `FullSync` 流程，不提供 standalone WebSocket fallback。当守护进程未运行时，返回错误并提示用户启动 `listen`。

### 原因

1. **状态一致性**：守护进程持有 `localMaxSeq` 和 WebSocket 连接。独立的 WebSocket 连接直接调用 `sync_updates` RPC 会与守护进程的同步状态竞争 SQLite 写入
2. **去重安全**：守护进程的 `syncManager` 通过 NotificationLog 表去重。独立连接可能导致重复处理
3. **FullSync 是守护进程的职责**：它管理分页、防抖拉取、和 ApplyUpdate 链

### 错误信息

```text
错误：守护进程未运行
建议：先启动 xyncra-client listen --user-id <user>
```

### 对 D-032 的影响

这是 D-032（IPC Fallback 策略）的例外。D-032 仍然适用于所有其他 CLI 命令。

---

## D-037: CLI flag 不遮蔽全局 flag

### 决策

命令局部 flag 不得使用与全局 persistent flag 相同的名称但语义不同。具体地：
- `--user-id` / `-u` 是全局 flag，表示当前用户身份
- `create-conversation` 使用 `--peer-id` 表示对方用户 ID，不使用 `--user-id`

### 原因

1. **避免混淆**：`xyncra-client --user-id alice create-conversation --user-id bob` 中两个 `--user-id` 含义不同，极度困惑
2. **Cobra 行为**：局部 flag 会遮蔽全局 flag，但用户无法直觉判断哪个生效
3. **一致性**：所有命令的 `--user-id` 始终表示当前用户

---

## D-038: CLI 消息 ID flag 类型区分

### 决策

不同命令中 `--message-id` 的含义通过 flag 描述文本明确区分：
- `delete-message --message-id`：string UUID（Message 表的 primary key ID）
- `mark-as-read --message-id`：uint32（会话内消息序号 MessageID）
- `get-messages --after-message-id`：uint32（分页游标，MessageID）

flag 描述中注明类型，例如：`--message-id string   Message UUID to delete` vs `--message-id uint32   Message sequence number to mark as read`。

### 原因

1. **模型区分**：`Message.ID`（string UUID, primary key）和 `Message.MessageID`（uint32, 会话内序号）是两个不同的标识符
2. **服务器协议一致**：`delete_message` RPC 接受 `message_id`（string UUID），`mark_as_read` RPC 接受 `message_id`（uint32）
3. **自文档化**：flag 帮助文本直接展示类型，减少用户错误

---

## D-039: CLI kill 命令行为规范

### 决策

`kill` 命令通过锁文件（`~/.xyncra/{user_id}/{device_id}/xyncra.lock`）获取 listen 守护进程的 PID，发送操作系统信号终止进程。默认发送 SIGTERM（优雅退出），`--force` 时升级为 SIGKILL。`--timeout`（默认 5 秒）控制等待进程退出的时间。进程确认退出后，清理锁文件和 IPC socket 文件。

`kill` 是 OS 级进程管理命令，不属于 D-030（IPC 协议）和 D-032（IPC Fallback）的范畴。

### 退出码

- `0`: 成功终止（或守护进程已不在运行）
- `1`: 通用错误（参数错误等）
- `3`: 超时退出（`--timeout` 到期但进程未退出，且未使用 `--force`）

### 原因

1. **安全终止**：SIGTERM 允许进程执行 defer 清理（如关闭 WebSocket、释放资源）
2. **强制兜底**：`--force` 处理 SIGTERM 被忽略的异常情况
3. **文件清理**：SIGKILL 不执行 defer，必须由 kill 命令显式清理残留文件

### 约束

- 锁文件和 socket 文件的清理必须在确认进程退出后执行
- 如果进程已死但锁残留（stale lock），直接清理文件并报告

---

## D-040: CLI logs 数据保留策略

### 决策

`logs cleanup` 默认保留 7 天的 RPC 日志和通知日志。同时清理 `rpc_logs` 和 `notification_logs` 两张表。`--type` 参数可指定只清理特定表（`rpc` | `notifications` | `all`），默认 `all`。

### 原因

1. **统一管理**：两种日志都是调试数据，保留策略相同
2. **灵活控制**：`--type` 允许按需只清理一种日志
3. **与 D-016 互补**：服务器端 UserUpdate 保留 30 天，客户端日志保留 7 天（客户端日志更轻量）

---

## D-041: CLI 输出格式标准

### 决策

CLI 输出使用标准库 `text/tabwriter` 进行表格对齐，不引入第三方依赖。数据输出到 stdout，错误和提示信息到 stderr。

### 原因

1. **零依赖**：与 D-001 零配置原则一致
2. **管道友好**：stdout/stderr 分离，支持 `| grep`、`> file` 等管道操作
3. **一致性**：与现有命令的输出模式保持一致

---

## D-042: CLI 退出码标准

### 决策

统一 CLI 退出码规范：
- `0`: 成功
- `1`: 通用错误（参数错误、网络错误、数据库错误等）
- `2`: 前置条件不满足（守护进程未运行、锁冲突等）
- `3`: 超时退出（`kill --timeout` 到期但进程未退出，且未使用 `--force`）

现有 `listen` 命令已使用退出码 2 表示锁冲突，`kill` 命令使用退出码 3 表示超时，与此标准一致。

### 原因

1. **可脚本化**：shell 脚本可基于退出码判断错误类型
2. **与现有行为一致**：listen 的 `os.Exit(2)` 已表示锁冲突，kill 的 `os.Exit(3)` 已表示超时
3. **POSIX 兼容**：0/1/2/3 是 Unix 工具的标准退出码约定

---

## D-043: E2E 测试端口约定

### 决策

E2E 测试基础设施使用与开发环境不同的端口，偏移量 10000：

| 服务 | 开发端口 | E2E 端口 |
|------|---------|---------|
| Redis | 6379 | 16379 |
| Xyncra Server | 8080 | 18080 |

Redis 使用 DB 15（最高编号），避免与开发数据（DB 0）冲突。E2E 测试前会执行 `FlushDB` 清空整个 DB。

### 原因

1. **环境隔离**：开发者可在运行 E2E 测试的同时使用本地 Redis 和 Server 进行开发
2. **安全清理**：`FlushDB` 只影响 E2E 专用的 DB 15，不会误删开发数据
3. **标准化**：所有 E2E 测试使用统一端口，无需额外配置

### 约束

- 如果开发者的本地 Redis 已占用 16379 端口，E2E 测试会失败（需手动释放或修改端口）
- `FlushDB` 会清除 DB 15 中的所有数据——不应在此 DB 上存放非测试数据

---

## D-044: listen daemon 连接韧性策略

### 决策

`listen` 守护进程在初始 WebSocket 连接失败时**不退出**，而是使用指数退避无限重试。IPC 服务器（Unix Socket）在 WS 连接建立之前就已启动，确保本地 CLI 始终可用。守护进程仅在上下文取消（SIGTERM/SIGKILL）或显式 `kill` 命令时退出。

### 原因

1. **离线可用**：开发者可能在无网络环境下启动 daemon，服务器恢复后应自动重连
2. **IPC 始终可用**：本地查询命令（D-035）依赖本地 SQLite，即使 WS 断开也应可用
3. **与 D-001 一致**：零配置、开箱即用——daemon 不应因临时网络问题退出
4. **与 D-032 互补**：IPC fallback 的前提是 IPC 始终可用

### 实现

- `XyncraClient.Start()` 不再因初始 `Connect()` 失败而返回错误
- 新增 `connectionMonitorWithInitialConnect()` 方法，包含初始连接重试 + 后续断线重连
- 重试策略复用已有的指数退避（`reconnectInitialBackoff`），无最大重试次数限制
- 连接状态通过日志输出（`logger.Info`/`logger.Error`），开发者可通过 `logs search --error` 监控

### 约束

- 初始连接重试无超时——daemon 会一直重试直到 context 取消
- 开发者不应依赖 daemon 退出作为连接失败的信号——应检查日志或使用 `logs search --error`

---

## D-045: create_conversation 实时通知

### 决策

`create_conversation` 在成功创建新会话（非幂等命中）时，为**双方用户**创建 `conversation` 类型的 UserUpdate，并通过 MQ 广播（fire-and-forget，D-007）推送给在线设备。幂等命中（`duplicate=true`）不创建 UserUpdate 也不广播。

客户端收到 `conversation` 类型更新后，根据 payload 中的 `action` 字段处理：`"create"` 表示新会话创建，客户端应 fetch 完整会话并 upsert 到本地 SQLite。

### 原因

1. **实时发现**：对方用户无需等待下次 `sync_updates` 轮询即可感知新会话
2. **与 D-028 一致**：`conversation` 是已定义的 UserUpdate 类型之一
3. **与 D-007 一致**：MQ 失败不影响数据完整性，离线用户通过 `sync_updates` 最终一致
4. **与 D-011 互补**：幂等命中不产生重复通知

### 实现

- `create_conversation` handler 新增 `broker mq.Broker` 依赖
- 创建成功后：获取双方 latest seq → 创建 UserUpdate（type=conversation）→ 批量写入 → MQ enqueue
- MQ payload 复用 `sendMessageTaskPayload` / `sendMessageRecipient` 结构（与 `mark_as_read`、`send_message` 一致）
- 客户端 `syncManager` 新增 `"create"` case 处理 conversation update
- 客户端 `ConversationStore` 新增 `Upsert` 方法

### 约束

- 幂等命中（`duplicate=true`）**不**触发 UserUpdate 创建和 MQ 广播
- MQ enqueue 失败仅记录日志，不影响 RPC 响应
- 客户端必须处理 `conversation` 类型更新，否则新会话只能等下次全量同步

---

## D-046: CLI send --client-msg-id 可选 flag

### 决策

CLI `send` 命令新增可选 `--client-msg-id` flag（string 类型，UUID 格式）。当未提供时，CLI 自动生成 UUID v4 作为 `client_message_id`（与 D-006 一致）。当提供时，使用用户指定的值。IPC handler 透传此字段到守护进程。

### 原因

1. **可测试性**：允许手动测试 D-006 幂等性（EXT-003）
2. **调试友好**：开发者可以指定已知的 client_message_id 进行调试
3. **向后兼容**：默认行为不变（自动生成 UUID），现有脚本无需修改
4. **与 D-006 一致**：不改变幂等性机制，只是暴露了控制入口

### 实现

```go
// send.go
var clientMsgID string
cmd.Flags().StringVar(&clientMsgID, "client-msg-id", "", "Client-generated message ID for idempotency (auto UUID if empty)")

// 如果未提供，自动生成
if clientMsgID == "" {
    clientMsgID = uuid.New().String()
}
```

### 约束

- flag 值为空字符串时自动生成 UUID
- 不进行格式校验（与 D-006 一致：服务器不做格式校验）
- 仅影响 `send` 命令，其他命令不变

---

## D-047: mark-as-read 显示 server 实际游标

### 决策

CLI `mark-as-read` 命令输出显示 server 返回的 `last_read_message_id`（即 MAX 语义后的实际游标值），而非用户请求的 `--message-id` 值。三层修改：server handler 返回实际游标、IPC handler 捕获并转发 server 响应、CLI 显示 server 确认的值。

### 原因

1. **准确性**：用户看到的是实际状态，而非请求值
2. **与 D-012 一致**：MAX 语义下，请求值可能被忽略（回退场景），显示实际值避免误导
3. **向后兼容**：仅显示层修改，不影响协议或数据结构
4. **调试友好**：开发者可以直接看到 server 端的实际游标位置

### 示例

```bash
# 当前游标在 #3
$ xyncra-client mark-as-read --conversation-id $CONV_ID --message-id 1 --user-id alice
Marked as read up to message #3   # 显示实际游标（MAX 语义保持 #3），而非请求的 #1
```

### 约束

- 仅影响 CLI 输出显示
- IPC 协议中新增 `last_read_message_id` 响应字段
- 旧版 CLI 不受影响（忽略新字段）

---

## 版本历史

| 日期       | 版本 | 变更                                                                                |
| ---------- | ---- | ----------------------------------------------------------------------------------- |
| 2026-07-10 | v2.6 | 新增 D-046（CLI send --client-msg-id flag）、D-047（mark-as-read 显示实际游标）     |
| 2026-07-09 | v2.5 | 新增 D-044（daemon 连接韧性策略）、D-045（create_conversation 实时通知）       |
| 2026-07-09 | v2.4 | 新增 D-043（E2E 测试端口约定），修正 D-042（补充退出码 3）                       |
| 2026-07-09 | v2.3 | 新增 D-039 到 D-042（kill 命令规范、logs 保留策略、输出格式、退出码标准）            |
| 2026-07-09 | v2.2 | 新增 D-035 到 D-038（CLI 查询命令本地读取、sync-updates IPC-only、flag 命名规范）    |
| 2026-07-09 | v2.1 | 新增 D-030 到 D-034（CLI 层产品决策）                                              |
| 2026-07-08 | v2.0 | 新增 D-028（UserUpdate 类型字段）、D-029（sync_updates 补空策略）                   |
| 2026-07-08 | v1.9 | 新增 D-027（客户端扩展错误码体系）                                                 |
| 2026-07-08 | v1.8 | 新增 D-018（多节点消息路由架构）                                                    |
| 2026-07-08 | v1.7 | 新增 D-017（结构化错误码体系）                                                     |
| 2026-07-08 | v1.6 | 新增 D-019（容器化部署模型）                                                      |
| 2026-07-08 | v1.5 | 新增 D-016（UserUpdate 数据生命周期管理）                                           |
| 2026-07-07 | v1.4 | 新增 D-012（已读位置模型）、D-013（级联软删除）、D-014（消息删除权限）、D-015（级联恢复） |
| 2026-07-07 | v1.3 | 新增 D-011（create_conversation 幂等模型）                                          |
| 2026-07-07 | v1.2 | 新增 D-009（sync_updates 分页模型）、D-010（被动续期策略）                          |
| 2026-07-07 | v1.1 | 新增 D-006（幂等性模型）、D-007（MQ fire-and-forget）、D-008（MessageID 分配策略） |
| 2026-07-07 | v1.0 | 初始版本，记录核心架构决策                                                          |
