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

## 版本历史

| 日期       | 版本 | 变更                                                                                |
| ---------- | ---- | ----------------------------------------------------------------------------------- |
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
