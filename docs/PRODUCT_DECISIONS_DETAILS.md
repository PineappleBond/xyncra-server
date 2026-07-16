# Xyncra Server 产品决策文档（详细版）

本文档包含 Xyncra Server 所有产品决策的详细说明，包括实现细节、代码示例、约束条件和设计原因。

> 决策概览请参阅 [PRODUCT_DECISIONS.md](./PRODUCT_DECISIONS.md)

---

## 相关文档

- [决策概览](./PRODUCT_DECISIONS.md) - 所有决策的快速参考表
- [API 文档](./API.md) - WebSocket 协议说明

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
              └──────▼──────┐
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

（文档继续，包含 D-027 到 D-115 的所有详细决策...）

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
- `XYNCRA_AGENTS_DIR` → `--agents-dir`（Agent 配置目录路径，默认 `"agents"`）

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

## D-036: 部分 CLI 命令为 IPC-only

### 决策

部分 CLI 命令仅通过 IPC 与守护进程交互，不提供 standalone WebSocket fallback。当守护进程未运行时，返回错误并提示用户启动 `listen`。

当前适用命令：

- `sync-updates`：触发守护进程的 `FullSync` 流程
- `set-typing`：发送瞬时 typing 指示器（daemon 离线时发送无意义）
- `stream-text`：发送瞬时流式文本（daemon 离线时发送无意义，D-051）
- `agent-resume`：HITL resume 操作（D-114）

### 原因

**sync-updates**：

1. **状态一致性**：守护进程持有 `localMaxSeq` 和 WebSocket 连接。独立的 WebSocket 连接直接调用 `sync_updates` RPC 会与守护进程的同步状态竞争 SQLite 写入
2. **去重安全**：守护进程的 `syncManager` 通过 NotificationLog 表去重。独立连接可能导致重复处理
3. **FullSync 是守护进程的职责**：它管理分页、防抖拉取、和 ApplyUpdate 链

**set-typing**：

1. **瞬时操作**：typing 指示器是 ephemeral (D-050)，不持久化。daemon 离线时发送没有接收者，毫无意义
2. **广播依赖连接**：typing 需要通过守护进程的 WebSocket 连接广播给其他成员，独立连接无法复用

**stream-text**：

1. **瞬时操作**：流式文本是 ephemeral (D-050, D-051)，不持久化。daemon 离线时发送没有接收者，毫无意义
2. **广播依赖连接**：stream-text 需要通过守护进程的 WebSocket 连接广播给其他成员，独立连接无法复用

**agent-resume**（D-114）：

1. **HITL 状态存在于 daemon 进程**：interruptIDs 映射（D-113）和 WebSocket 连接均在 daemon 内存中，resume 需要通过 daemon 的 WebSocket 连接发送到服务端
2. **独立连接无法 resume**：resume 需要精确的 (userID, deviceID) 路由，只有 daemon 持有此状态

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

### 例外

- **4001 Close Frame**：收到服务器发送的 Close Frame（code: 4001）时不触发重连（被新实例替换，参见 D-111）
- daemon 进入休眠状态，IPC 仍然可用
- 可通过 `XyncraClient.Reconnect()` 从休眠状态恢复

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

## D-050: Ephemeral Push 模式 (Seq=0)

### 决策

某些业务场景（如 typing 指示器、在线状态、呼叫信令）的数据是瞬时的——丢失可接受，持久化无意义。这类业务使用 **ephemeral push** 模式：

- 使用 `Seq=0` 作为 ephemeral 标识，与持久化更新（Seq ≥ 1）区分
- **不持久化**：不写入 Message、UserUpdate 或任何 store
- **不分配 seq**：不参与用户级 seq 空间（D-028 的例外）
- **不入 MQ**：不进入重试队列
- **直接广播**：Handler 直接调用 `BroadcastUpdates`，绕过 DB 事务和 MQ
- **离线不投递**：用户离线时静默丢弃
- **上线不补拉**：`sync_updates` 永远不会返回 ephemeral 事件

Client 端在 `ApplyUpdate` 入口检查 `Seq==0`，命中时跳过 seq 连续性检查和本地入库，直接回调上层 handler 驱动 UI。

### 原因

1. **与 D-007 互补**：D-007 处理"数据重要但推送可失败"的场景；D-050 处理"数据和推送都可丢失"的场景
2. **协议复用**：复用 `PackageDataUpdates` 信封，无需新增 PackageType，最小化协议改动
3. **扩展性**：同一机制可承载 typing、presence、call signal、read receipt preview 等瞬时业务
4. **客户端简化**：统一的 Seq=0 约定让客户端只需一个分支即可处理所有 ephemeral 类型

### 约束

- Ephemeral 类型必须在 `pkg/protocol/protocol.go` 中显式声明（注释标注 `ephemeral: Seq=0`）
- Handler 必须验证调用者是会话成员（防止向非成员广播）
- Handler 必须实现 rate limiting（建议 1 次/秒/会话/用户），防止广播风暴
- 旧版客户端收到未识别的 ephemeral type 时应静默忽略，不报错
- Ephemeral 事件广播给对话中的**所有成员**（包括发送者自己的其他设备），确保多设备体验一致

### 与现有决策的关系

- **D-028 例外**：ephemeral 类型不参与共享 seq 空间
- **D-007 延伸**：从"MQ fire-and-forget"延伸为"整个操作 fire-and-forget"
- **D-018 复用**：跨节点推送复用 Redis Pub/Sub 路径

---

## D-051: 流式文本 Ephemeral Push

### 决策

`stream_text` RPC 使用 D-050 的 ephemeral push 机制广播实时输入文本。采用**累积文本模式**（每帧包含完整文本快照，非 delta），客户端直接替换显示内容。

### 原因

1. **累积模式简化接收端**：接收方无需维护 delta 拼接状态，任何一帧都是完整的当前文本
2. **丢帧不影响正确性**：ephemeral 推送可丢失（D-050），但累积模式下后续帧自然覆盖
3. **与 LLM 输出匹配**：LLM token 输出场景下，发送方维护的 buffer 本身就是累积的

### 实现要点

- `stream_id` 由客户端生成（建议 UUID），服务端不解析，仅透传
- Rate limit: 20次/秒/会话/用户（50ms 间隔），防止广播风暴
- 流式事件不持久化，最终消息通过 `send_message` 持久化
- 离线用户不投递，重连后通过 `send_message` 的 FullSync 兜底

### 与现有决策的关系

- **D-050 延伸**：第二个 ephemeral 业务场景
- **D-007 延伸**：流式推送是 fire-and-forget
- **D-028 例外**：ephemeral 类型不参与共享 seq 空间

---

## D-052: stream_text 与 send_message 协作模型

### 决策

流式文本的最终持久化通过独立的 `send_message` RPC 完成。采用**两步结束流程**：

1. 发送方调用 `stream_text(is_done=true, text=最终文本)` 广播结束信号
2. 发送方调用 `send_message(content=最终文本)` 持久化消息

### 原因

1. **职责分离**：`stream_text` 负责实时广播（ephemeral），`send_message` 负责持久化
2. **平滑过渡**：接收方收到 `is_done` 后知道流式结束，等待 `send_message` 的持久化消息替换流式 buffer
3. **`send_message` 新增可选 `stream_id` 字段**（omitempty），用于关联流式事件和最终消息

### 实现要点

- 两步顺序必须遵守：先 `is_done` broadcast，再 `send_message` persist
- `stream_id` 与 `client_message_id` 独立——前者关联流式事件，后者保证幂等性
- 如果发送方跳过 `stream_text` 直接 `send_message`，功能不受影响（无流式效果，消息正常持久化）

---

## D-053: Ephemeral 广播给所有成员（已合并入 D-050）

> 此决策已合并入 D-050 约束。Ephemeral 事件广播给对话中的**所有成员**（包括发送者自己的其他设备），作为 D-050 的核心约束而非例外。

---

## D-054: Agent UserID 命名约定

### 决策

Agent 使用 `agent/{id}` 格式的 UserID（如 `agent/assistant-001`）。`agent/` 前缀为系统保留命名空间，服务端通过 `AgentRegistry.IsAgent(userID)` 判断，客户端通过 `strings.HasPrefix(userID, "agent/")` 识别。Agent 在协议层与普通用户完全等价，不新增 User 类型。

### 原因

1. **命名空间隔离**：`agent/` 前缀明确区分 Agent 与普通用户，避免 ID 冲突
2. **协议复用**：不新增 User 类型或字段，复用现有 UserID 机制
3. **可识别性**：客户端和服务端都能通过前缀快速判断身份类型
4. **向后兼容**：现有协议和数据结构无需修改

### 实现

- `AgentRegistry.IsAgent(userID)` 方法检查 `agent/` 前缀并提取 ID
- `AgentRegistry.Get(id)` 方法查询已注册的 agent 配置
- 客户端可通过 `strings.HasPrefix(userID, "agent/")` 识别 agent

---

## D-055: Agent 消息格式复用

### 决策

Agent 的消息与普通用户消息格式完全相同。不新增 Message.Type 枚举值，不新增 Package 类型。Agent 通过 `agent/` 前缀的 UserID 标识（D-054），消息流转、存储、推送复用现有基础设施。

### 原因

1. **协议简洁性**：不引入新的消息类型，保持协议层稳定
2. **复用现有基础设施**：Message Store、MQ、广播机制全部复用，零额外开发
3. **客户端无需改动**：客户端处理 Agent 消息与处理普通用户消息逻辑一致
4. **与 D-054 配合**：通过 UserID 前缀区分，协议层无感知

### 实现

- Agent 消息复用现有 `Message` 结构体，不新增字段
- Agent 发送的消息通过 `agent/{id}` 格式的 `UserID` 标识
- 客户端通过 UserID 前缀区分人类用户和 agent

---

## D-058: Agent 配置格式

### 决策

Agent 通过 Markdown 文件定义，采用 YAML Front Matter + Markdown body 的单文件格式。YAML 部分定义元数据（id、name、description、model、api_key_env、base_url、parameters、context、tools），Markdown body 作为 system prompt。配置文件存放于 `agents/` 目录（可通过 `--agents-dir` 覆盖），server 启动时从磁盘扫描加载，支持运行时通过 `reload_agents` RPC 热更新（D-076）。

配置文件示例：

```markdown
---
id: assistant-001
name: 智能助手
description: 通用对话助手
model: gpt-4
api_key_env: OPENAI_API_KEY
base_url: https://api.openai.com/v1
parameters:
  temperature: 0.7
  max_tokens: 2000
  top_p: 0.9
context:
  max_tokens: 4000
  max_messages: 20
tools:
  - search
  - calculator
---

你是一个智能助手，负责回答用户问题。
```

### 原因

1. **单文件格式简洁**：所有配置集中在一个文件，易于管理和版本控制
2. **Markdown body 天然适合 system prompt**：Markdown 格式人类可读，易于编写和维护
3. **磁盘加载支持热更新**：运行时修改配置文件后，通过 `reload_agents` RPC 即可生效，无需重新编译（D-076）
4. **Docker 部署友好**：Docker 部署时可通过 volume 映射 `agents/` 目录，方便管理
5. **可扩展**：新增 Agent 只需添加新的 Markdown 文件，无需修改代码

### 变更历史

- **2026-07-11**: 从 `go:embed` 改为磁盘目录加载（D-077），支持运行时热更新

---

## D-060: Agent 上下文管理策略

### 决策

Agent 上下文采用 DB 存储 + 内存缓存（sync.Map，TTL 30s），Token 计数裁剪优先，MaxMessages 为 fallback。

### 实现要点

1. **DB 存储**：通过 `MessageStore.ListRecentByConversation` 加载对话历史，确保服务重启不丢失
2. **sync.Map 内存缓存**：减少 DB 查询开销，适合读多写少的 Agent 上下文场景，默认 TTL 30 秒
3. **Token 裁剪优先**：`trimByTokens` 从最新消息向最旧累积 token，超出 `MaxTokens` 时裁剪最旧消息
4. **消息数 fallback**：当 `MaxTokens == 0` 时，使用 `trimByMessages` 按 `MaxMessages` 固定数量裁剪
5. **HeuristicTokenCounter**：默认使用 `len(text)/4` 作为 token 估算，无外部依赖（与 D-001 一致）
6. **消息类型过滤**：Phase 2 为 passthrough（D-055），不过滤消息类型

### 原因

1. **持久化 + 缓存兼顾**：DB 存储保证可靠性，内存缓存保证性能
2. **Token 裁剪优先于消息数**：LLM 的约束是 token 窗口，按 token 裁剪更精确
3. **消息数作为 fallback**：当无法估算 token 时（MaxTokens=0），固定数量是简单可靠的替代
4. **启发式 token 计数**：满足 D-001 零配置原则，后续可通过 `WithTokenCounter` 替换为精确 tokenizer

**实现**：`internal/agent/db_context_manager.go` 中的 `DBContextManager`

---

## D-062: Agent 消息路由触发模型

### 决策

当用户向 Agent 发送消息时，消息先正常持久化（与所有消息相同的路径），然后通过 MQ 入队一个 `TypeAgentProcess` 异步任务触发 Agent 处理。Agent 处理是 fire-and-forget 的——MQ 入队失败不影响消息的持久化和用户收到的响应。

仅当消息的发送者是**人类用户**（非 `agent/` 前缀）且接收者是**已注册 Agent**（`AgentRegistry.IsAgent()` 返回 true）时，才触发 `TypeAgentProcess` 入队。Agent 发送的消息（即使是发给另一个 Agent）不触发 Agent 处理，防止无限递归。

### 原因

1. **与 D-007 一致**：D-007 规定 MQ 入队是 fire-and-forget，Agent 处理属于"增强体验"而非"数据完整性"范畴
2. **与 D-055 一致**：不新增 Message 类型，Agent 消息路由在 MQ 层实现，对协议层透明
3. **防递归保护**：限制只有人类用户发给 Agent 才触发处理，避免 Agent 回复触发二次处理的无限循环
4. **零侵入**：非 Agent 消息路径完全不受影响（仅多一次前缀检查）

### 实现

- 在 `send_message` handler 中，消息持久化后检查 peer userID 是否已注册 Agent
- `agentProcessPayload` 包含 `MessageID`（string UUID）、`ConversationID`、`AgentID`（完整 `agent/xxx` userID）、`SenderID`
- Guard 条件：`!strings.HasPrefix(senderID, "agent/")` 时才入队

### 约束

- Agent 处理失败通过 MQ 重试机制保障（Asynq MaxRetry），但 Agent 回复消息的幂等性由后续 Phase 的 AgentTaskHandler 负责
- `agent/` 前缀的 userID 未在 AgentRegistry 中注册时，`IsAgent()` 返回 false，消息不触发 Agent 处理（graceful degradation）

---

## D-063: AgentRegistry 可选注入（nil-safe）

### 决策

`AgentRegistry` 通过 `Dependencies` struct 注入到 handler 层，允许为 nil。当 `AgentRegistry` 为 nil 时，agent 检测路径被完全跳过。这使得 Agent 功能可以作为可选模块引入，不影响不关心 Agent 功能的部署场景。

### 原因

1. **与 D-001 一致**：不需要 Agent 功能的用户无需关心此模块的存在
2. **向后兼容**：现有代码传入 nil 即可保持 Phase 2 的行为不变
3. **测试友好**：单元测试中传 nil 即可禁用 agent 路径，不需要构造完整的 AgentRegistry
4. **渐进引入**：Agent 系统可以在不影响核心消息功能的前提下逐步开发和部署

### 实现

- `Dependencies` struct 中 `AgentRegistry *agent.AgentRegistry` 为指针类型（零值为 nil）
- `sendMessageHandler` 中 `if h.agentRegistry != nil` guard 保护 agent 检测逻辑
- `main.go` 中在 AgentRegistry 初始化成功后传入 Dependencies

### 约束

- `AgentRegistry` 为 nil 时，所有消息走正常路径（等价于无 Agent 系统）
- 不应在运行时动态修改 `AgentRegistry` 指针（reload 通过 Registry 内部方法实现）

---

## D-064: LLM 提供商默认 BaseURL 映射

### 决策

每个 LLM 提供商有默认 BaseURL，当 AgentConfig 中 `base_url` 为空时使用默认值：

| Provider | 默认 BaseURL |
|----------|-------------|
| OpenAI | `https://api.openai.com/v1` |
| Claude | `https://api.anthropic.com` |
| Ollama | `http://localhost:11434` |
| Qwen | `https://dashscope.aliyuncs.com/compatible-mode/v1` |

`LLMClientFactory` 在创建 ChatModel 时，如果 `AgentConfig.BaseURL` 为空，自动使用对应提供商的默认 URL。

### 原因

1. **减少配置负担**：大多数用户使用官方 API 时无需指定 BaseURL，与 D-001（开箱即用）一致
2. **明确预期**：默认 URL 记录了各提供商的标准端点，便于开发者理解
3. **灵活性保留**：用户仍可通过 `base_url` 覆盖默认值（自托管、代理等场景）

### 实现

```go
func (f *LLMClientFactory) createOpenAI(config *AgentConfig, apiKey string) (model.ChatModel, error) {
    baseURL := config.BaseURL
    if baseURL == "" {
        baseURL = "https://api.openai.com/v1" // 默认值
    }
    // ... 使用 baseURL 创建 ChatModel
}
```

### 约束

- 默认 URL 硬编码在 factory 中，如果需要支持自定义提供商，用户必须显式指定 `base_url`
- 默认 URL 不包含 trailing slash

---

## D-065: Agent 思考状态展示

### 决策

Agent 处理用户消息时，在首个流式 token 到达前，发送 typing indicator（ephemeral push, Seq=0）给会话中的其他成员。首个 token 到达后停止 typing。如果 LLM 无响应超过 60 秒，停止 typing 并发送兜底错误消息（D-067）。

### 原因

1. **用户感知**：避免用户长时间等待无反馈，提升感知响应速度
2. **与 D-050 一致**：typing indicator 是 ephemeral push（Seq=0），不持久化
3. **与 D-057 一致**：Agent 思考状态复用 `set_typing` RPC 的 ephemeral push 机制

### 实现

```go
// AgentExecutor.Execute 流程
func (e *AgentExecutor) Execute(ctx context.Context, payload ExecutePayload) error {
    // 1. 发送 typing=true（ephemeral, Seq=0）
    e.broadcastHelper.SendTyping(ctx, payload.AgentID, payload.ConversationID, true)

    // 2. 加载 context、构建 agent、调用 LLM
    // ...

    // 3. 首个流式 token 到达时，发送 typing=false
    e.broadcastHelper.SendTyping(ctx, payload.AgentID, payload.ConversationID, false)

    // 4. 继续流式广播...
}
```

### 约束

- typing indicator 仅发送给会话中的其他成员（不包括 Agent 自己）
- typing 状态最多持续 60 秒，超时后自动停止
- 客户端通过 `user_id` 前缀（`agent/`）区分 "thinking" 和 "typing" 的 UI 展示

---

## D-066: LLMProvider 接口抽象

### 决策

定义 `LLMProvider` 接口，各提供商实现此接口。`LLMClientFactory` 维护提供商注册表（`map[LLMProvider]ProviderFactory`），支持运行时注册新提供商。

### 原因

1. **扩展性**：添加新提供商只需实现接口并注册，无需修改 factory 核心逻辑
2. **可测试性**：测试时可注入 mock provider
3. **与 D-001 一致**：零配置添加新提供商只需注册

### 实现

```go
// LLMProvider 接口
type LLMProvider interface {
    // CreateChatModel 根据 AgentConfig 创建 ChatModel
    CreateChatModel(config *AgentConfig, apiKey string) (model.ChatModel, error)
    // DefaultBaseURL 返回该提供商的默认 BaseURL
    DefaultBaseURL() string
}

// ProviderFactory 是创建 LLMProvider 的工厂函数
type ProviderFactory func() LLMProvider

// LLMClientFactory 维护提供商注册表
type LLMClientFactory struct {
    providers map[string]LLMProvider
}

// RegisterProvider 注册新的 LLM 提供商
func (f *LLMClientFactory) RegisterProvider(name string, provider LLMProvider) {
    f.providers[name] = provider
}

// 默认注册 OpenAI/Claude/Ollama/Qwen
func NewLLMClientFactory() *LLMClientFactory {
    f := &LLMClientFactory{providers: make(map[string]LLMProvider)}
    f.RegisterProvider("openai", &OpenAIProvider{})
    f.RegisterProvider("claude", &ClaudeProvider{})
    f.RegisterProvider("ollama", &OllamaProvider{})
    f.RegisterProvider("qwen", &QwenProvider{})
    return f
}
```

### 约束

- 提供商名称（如 "openai"、"claude"）与 `detectProvider` 返回值一致
- 新提供商必须在 `NewLLMClientFactory` 时或之后显式注册，不支持自动发现

---

## D-067: Agent 错误消息策略

### 决策

Agent 执行失败时（配置错误、API 错误重试耗尽），通过 `send_message` 持久化一条错误消息。SenderID 为 `agent/{id}`，Content 为预定义错误文本。错误消息也遵循 D-055（复用现有消息格式）。

### 原因

1. **用户感知**：静默失败导致用户困惑，持久化错误消息确保用户能在 sync_updates 中看到失败原因
2. **与 D-055 一致**：不新增 Message 类型，复用现有消息格式
3. **可追溯**：错误消息作为对话历史的一部分，便于调试和排查

### 错误消息文本

| 错误类型 | 消息文本 |
|----------|---------|
| 配置错误（API Key 缺失、model 不支持） | "抱歉，我的配置有误，请联系管理员检查设置。" |
| API 重试耗尽（超时、限流、网络错误） | "抱歉，我暂时无法回复，请稍后重试。" |
| 上下文加载失败 | "抱歉，我无法读取对话历史，请重新发送消息。" |
| 未知错误 | "抱歉，处理遇到问题，请稍后重试。" |

### 实现

```go
// AgentExecutor.Execute 错误处理
func (e *AgentExecutor) Execute(ctx context.Context, payload ExecutePayload) error {
    err := e.executeInternal(ctx, payload)
    if err != nil {
        // 发送错误消息
        errorMsg := e.classifyError(err)
        e.sendErrorMessage(ctx, payload, errorMsg)
        return err
    }
    return nil
}

func (e *AgentExecutor) classifyError(err error) string {
    switch {
    case errors.Is(err, ErrAPIKeyMissing), errors.Is(err, ErrUnsupportedModel):
        return "抱歉，我的配置有误，请联系管理员检查设置。"
    case errors.Is(err, ErrLLMTimeout), errors.Is(err, ErrLLMRateLimited):
        return "抱歉，我暂时无法回复，请稍后重试。"
    case errors.Is(err, ErrContextLoad):
        return "抱歉，我无法读取对话历史，请重新发送消息。"
    default:
        return "抱歉，处理遇到问题，请稍后重试。"
    }
}
```

### 约束

- 错误消息通过 `Store.SendMessage` 持久化，确保离线用户也能通过 sync_updates 获取
- 错误消息不触发新的 Agent 处理（避免无限循环）
- 客户端通过 SenderID 前缀（`agent/`）识别错误消息来源

---

## D-070: AgentTaskHandler 放置于 `internal/agent/` 包

### 决策

`AgentTaskHandler`（MQ 任务处理函数）放置于 `internal/agent/` 包，而非 `internal/handler/` 包。

### 原因

1. **依赖方向**：Task handler 直接依赖 `AgentExecutor`、`IdempotencyStore` 等 agent 包内部组件。放在 handler 包会引入 `handler → agent` 的反向依赖，破坏分层。
2. **所有权边界**：`internal/agent/` 包是 agent 功能的所有权边界——配置解析、上下文管理、LLM 调用、任务处理全部内聚在此包中。
3. **与 D-063 一致**：Agent 功能作为可选模块，其全部实现集中在 `internal/agent/`，handler 层仅通过 `AgentRegistry` 指针耦合（nil-safe）。

### 约束

- handler 层通过 `mq.Broker` 入队 agent 任务，但不直接依赖 agent 包的 handler 实现
- `main.go` 负责组装：创建 `AgentExecutor` → 创建 task handler → 注册到 MQ broker

---

## D-071: Agent 幂等性使用 Redis SETNX + 24h TTL

### 决策

Agent 任务幂等性使用 Redis `SETNX` 原子操作，TTL 为 24 小时。Key 格式：`agent:processed:{messageID}`。

### 原因

1. **零新依赖**：Redis 是现有基础设施（ConnectionStore、MQ、Pub/Sub），无需引入新的中间件
2. **原子操作**：`SETNX` 天然适合分布式幂等性——check-and-set 是原子的，无需额外锁
3. **24h TTL**：覆盖 MQ 重试窗口（Asynq 默认重试 25 次，最长约 23 天），同时避免永久占用 Redis 内存
4. **与 D-006 互补**：D-006 的 `client_message_id` 幂等性是客户端到服务器的幂等性；D-071 是 MQ 层面的幂等性，防止 Asynq 重试导致重复执行

### 实现

```go
dup, err := idempotency.MarkProcessed(ctx, "agent:processed:"+payload.MessageID, 24*time.Hour)
if err != nil {
    // fail-open (D-072)
} else if dup {
    return nil // skip duplicate
}
```

### 约束

- Key 格式 `agent:processed:{messageID}` 使用 `MessageID`（UUID），不是 `client_message_id`——因为 MQ 层面只有 `MessageID`
- TTL 到期后，同一 `MessageID` 可能被重新处理——这在 24h 后是安全的（MQ 重试早已结束）

---

## D-072: Agent 幂等性 fail-open 策略

### 决策

当 Redis 不可用时，跳过幂等性检查继续执行任务（fail-open），而非拒绝执行（fail-close）。

### 原因

1. **幂等性是优化而非安全机制**：Agent 执行重复不会造成数据损坏——重复的消息回复会被去重（D-006 `client_message_id`），不会导致数据不一致。
2. **阻塞执行比重复执行更糟糕**：用户等待 Agent 回复，Redis 临时故障不应导致 Agent 完全不可用。
3. **与 D-007 一致**：MQ 入队失败不阻塞核心流程，Redis 故障也不应阻塞 Agent 执行。

### 实现

```go
if idempotency != nil {
    dup, err := idempotency.MarkProcessed(ctx, key, 24*time.Hour)
    if err != nil {
        logger.Printf("agent task: idempotency check: %v", err)
        // Continue processing — fail-open for idempotency.
    } else if dup {
        return nil // skip duplicate
    }
}
```

### 约束

- fail-open 意味着 Redis 故障期间可能重复执行——但重复执行的后果是重复发送一条回复消息，可接受
- 日志记录 Redis 故障，运维可通过监控发现

---

## D-073: AgentTaskHandler 总是返回 nil 给 MQ

### 决策

`AgentTaskHandler` 总是返回 `nil` 给 MQ（Asynq），即使执行失败。

### 原因

1. **ExecuteWithErrorMessage 已处理所有错误**：成功则持久化回复消息，失败则持久化友好错误消息（D-067）。错误已经转化为对用户有意义的输出。
2. **MQ 重试导致重复执行和重复错误消息**：如果返回 error，Asynq 会重试，每次重试都会调用 `ExecuteWithErrorMessage`，每次都会持久化一条错误消息——用户会收到多条相同的错误消息。
3. **与 `mq_send_message.go` 模式一致**：send_message 的 MQ handler 也总是返回 nil（D-007 模型）。

### 实现

```go
if err := executor.ExecuteWithErrorMessage(ctx, execPayload); err != nil {
    // Error already persisted as user-friendly message (D-067).
    // Return nil to prevent MQ retry — the error is terminal.
    logger.Printf("agent task: execution failed: %v", err)
}
return nil
```

### 约束

- 这意味着 Agent 执行失败是不可恢复的——如果需要重试，必须由用户重新发送消息触发
- 日志记录失败，运维可通过监控发现

---

## D-074: Agent 幂等性使用独立 redis.Client

### 决策

Agent 幂等性使用独立的 `redis.Client`，不复用 `nodeBroadcasterClient`（Pub/Sub 专用连接）。

### 原因

1. **Pub/Sub 连接不能共享**：`nodeBroadcasterClient` 专用于 Pub/Sub（D-018），其连接状态被 `PSUBSCRIBE` 占用，不能用于常规命令。
2. **独立配置**：独立客户端允许独立配置连接池大小、超时时间等参数，不受 Pub/Sub 连接的影响。
3. **开销极小**：额外一个 Redis 连接（连接池默认 10 个）的内存和 CPU 开销可忽略不计。

### 实现

```go
// main.go 中创建独立的 Redis 客户端
agentRedisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
idempotencyStore := agent.NewRedisIdempotencyStore(agentRedisClient)
```

### 约束

- 新增一个 Redis 连接到连接池——如果 Redis 服务端连接数受限，需要考虑
- 与 `nodeBroadcasterClient` 使用相同的 Redis 实例，网络延迟相同
- 此独立 `redis.Client` 同时被 Agent 幂等性（D-071）、会话锁（D-075）、CheckpointStore（D-083）和 ReverseRPC PendingStore（D-103）共享

---

## D-075: Agent 会话级并发锁（Per-Conversation Lock）

### 决策

Agent 执行使用 Redis `SETNX` 分布式锁，key 格式 `agent:lock:{conversationID}`，TTL 130s（略高于 120s 总超时）。同一会话同一时间只允许一个 Agent 任务执行。如果锁已被占用，新任务直接跳过（不重试），因为已运行的任务会处理最新上下文。Fail-open 策略：Redis 不可用时跳过锁检查继续执行（与 D-072 一致）。使用 Lua 脚本确保只释放自己持有的锁，防止误删其他实例的锁。

### 原因

1. **上下文一致性**：同一对话的多个消息并行执行可能导致上下文加载不一致、重复响应
2. **分布式安全**：Redis 锁跨节点生效，支持多实例部署（与 D-018 多节点路由一致）
3. **与 D-072 一致**：fail-open 策略保证可用性优先
4. **与 D-071 互补**：D-071 的幂等性防止重复执行，D-075 的锁防止并发执行
5. **复用 D-074 的独立 redis.Client**：不引入额外 Redis 连接

### 约束

- 锁 TTL 130s 覆盖了总超时 120s + 10s buffer
- Lua 脚本检查锁值（unique token）后才删除，防止释放他人的锁
- 锁获取失败时记录日志但不阻塞执行（fail-open）

---

## D-076: reload_agents RPC 管理接口

### 决策

新增 `reload_agents` RPC 方法，调用 `AgentRegistry.Reload()` 重新扫描磁盘 `agents/` 目录并加载配置。无鉴权（与 D-002 一致）。响应格式：`{"count": N}`，N 为重新加载后的 Agent 数量。解析失败的配置文件被跳过并记录日志（与启动行为一致）。

### 原因

1. **热更新能力**：修改 Agent 配置后无需重启服务器
2. **与 D-002 一致**：内网部署模型下，能访问此 RPC 的只有内网服务或反向代理后的管理员
3. **与 D-077 配合**：磁盘加载模式使得 reload 有实际意义
4. **最坏情况可接受**：误触发 reload 的后果仅为短暂延迟，不会造成数据损坏

### 约束

- reload 期间，正在执行的 Agent 任务不受影响（它们持有旧 AgentConfig 指针的引用）
- reload 使用 `sync.RWMutex` 保护，与 `Get`/`IsAgent` 等读操作并发安全
- 解析失败的配置文件不阻止其他有效配置的加载

---

## D-077: Agent 配置从磁盘目录加载

### 决策

Agent 配置文件从磁盘目录加载（默认 `agents/`），替代原来的 `go:embed` 方案。通过 `--agents-dir` 命令行 flag 可覆盖默认路径。`AgentRegistry` 启动时扫描目录加载所有 `.md` 文件，`Reload()` 方法重新扫描同一目录。

### 原因

1. **热更新前提**：`go:embed` 将文件嵌入二进制，运行时内容不可变，reload 无实际意义
2. **Docker 部署友好**：Docker 部署时可通过 volume 映射 `agents/` 目录，运维人员可直接修改配置
3. **开发效率**：修改 Agent prompt 后无需重新编译，reload 即可生效
4. **与 D-001 兼容**：默认 `agents/` 目录开箱即用，无需额外配置

### 变更历史

- **2026-07-11**: 从 `go:embed` 改为磁盘目录加载。删除 `internal/agent/embed.go`，修改 `AgentRegistry.Load()` 接受目录路径参数

### 约束

- 默认路径 `agents/` 相对于工作目录
- 目录不存在时记录警告但不报错（Agent 功能为可选模块，D-063）
- `.md` 文件以外的文件被忽略
- 配置文件变更不会自动触发 reload，需要显式调用 `reload_agents` RPC

---

## D-078: Agent 自定义工具注册表

### 决策

工具通过代码注册到 `ToolRegistry`（类似 `LLMClientFactory` 的 provider 注册表）。内置工具在启动时预注册。Agent 配置通过名称引用工具（`tools: [search, calculator]`）。配置中未注册的工具名被记录日志并跳过（fail-open，与 D-001/D-072 一致）。

### 原因

1. **与 D-066 模式一致**：`LLMClientFactory` 的 provider 注册表已是同样模式
2. **零配置**：内置工具开箱即用
3. **可扩展**：添加新工具只需实现函数 + 注册
4. **向后兼容**：`tools: []` 是默认值

### 约束

- 自定义工具注册发生在 `main.go`，运行时添加需重新编译
- 未知工具名跳过并记录警告，不阻塞 Agent 构建

---

## D-079: Agent Middleware 配置格式

### 决策

Middleware 在 Agent YAML front matter 的 `middleware` 段中配置，所有字段可选：

```yaml
middleware:
  enable_client_tools: true
  client_tools:
    function_tags: []          # 空 = 所有函数
    excluded_functions: []     # 排除特定函数名
    call_timeout: 30s          # 工具调用超时
  enable_patch_tool_calls: true
  enable_summarization: true
  summarization_tokens: 160000
  enable_tool_reduction: true
  tool_reduction_max_chars: 50000
```

当 `middleware` 段缺失时，不应用任何中间件（与 Phase 1-7 向后兼容）。中间件顺序固定为：DynamicToolProvider → PatchToolCalls → Summarization → ToolReduction。其中 DynamicToolProvider 仅在 `enable_client_tools: true` 时存在。

各中间件说明：

- **DynamicToolProvider**：从客户端设备函数注册表中动态注入工具。需要 `enable_client_tools: true`。通过 `client_tools.function_tags` 按标签过滤（OR 语义，空列表 = 所有函数），通过 `client_tools.excluded_functions` 排除特定函数，通过 `client_tools.call_timeout` 控制调用超时（默认 30s）。过滤顺序：`excluded_functions` 先于 `function_tags` 执行。
- **PatchToolCalls**：修复 LLM 产生的格式不合法的工具调用（如参数类型不匹配）。
- **Summarization**：上下文过长时自动压缩历史消息。
- **ToolReduction**：工具返回结果过大时截取，通过 `retrieve_tool_result` 按 ID 找回完整结果。

### 原因

1. **Per-agent 控制**：不同 Agent 可有不同压缩策略
2. **向后兼容**：无 `middleware` 段的配置行为不变
3. **顺序固定**：避免错误配置导致的问题
4. **DynamicToolProvider 必须在首位**：它注入的工具需要被后续中间件（PatchToolCalls、Summarization、ToolReduction）看到

### 约束

- DynamicToolProvider 需要 `clientFunctionProvider` 和 `clientCaller` 已注入（`nil` 时跳过并记录警告）
- Summarization 的压缩模型默认使用主模型，可选覆盖
- 中间件创建失败时跳过（fail-open），不阻塞 Agent 构建

---

## D-080: 工具结果截取存储策略

### 决策

截取的工具结果存储在内存中（`sync.Map` + TTL），不持久化到消息表或文件系统。`retrieve_tool_result` 工具通过 ID 查找完整结果。如果结果已过期（TTL 到期），工具返回"结果已过期"消息。

### 原因

1. **与 D-001 一致**：文件系统存储需要路径配置、清理机制、Docker volumes
2. **与 D-060 模式一致**：`sync.Map` 缓存是已有模式
3. **TTL 1 小时**：足够覆盖单次 Agent 执行
4. **与 D-055 一致**：截取元数据不持久化到 messages 表

### 约束

- 服务重启后截取结果丢失（可接受，工具结果可重新获取）
- 截取元数据仅存在于 Agent 执行上下文中

---

## D-081: Sub-agent 声明方式

### 决策

Sub-agent 在父 Agent 的 YAML 配置中通过 `sub_agents` 段声明，引用已注册的 Agent ID：

```yaml
id: planner
sub_agents:
  - researcher
  - writer
```

Sub-agent 不是独立的 `agent/` 用户，而是通过 Eino DeepAgent 的 TaskTool 在父 Agent 上下文中执行。Sub-agent 的输出流回父 Agent，只有父 Agent 向会话发送消息。

### 原因

1. **复用现有配置**：无需单独的 sub-agent 配置格式
2. **与 D-054/D-055 一致**：不创建新 UserID，消息格式不变
3. **简单组合**：通过组合已有 Agent 定义实现复杂层次

### 约束

- Sub-agent 深度限制为 2 层（parent → child → grandchild），防止无限递归
- Sub-agent 必须在同一 AgentRegistry 中注册

---

## D-082: Agent 错误消息扩展分类

### 决策

D-067 错误分类扩展为覆盖 Phase 8 的新失败模式：

| 错误类型 | 消息文本 |
|---------|---------|
| 工具执行失败 | "抱歉，工具调用失败，请稍后重试。" |
| MCP 服务不可达 | "抱歉，外部工具服务不可用，请稍后重试。" |
| 子 Agent 委派失败 | "抱歉，子任务执行失败，请稍后重试。" |
| Checkpoint 过期（HITL） | "抱歉，等待时间过长，请重新发送消息。" |
| 中间件（Summarization）失败 | "抱歉，上下文压缩失败，正在使用完整历史继续。" |

### 原因

1. **与 D-067 一致**：用户友好的中文消息
2. **优雅降级**：中间件失败应继续执行（不中断）

---

## D-083: HITL CheckpointStore 失败策略

### 决策

当 CheckpointStore（Redis）在 checkpoint 保存时不可用，HITL 流程中止并持久化用户友好的错误消息（D-067 模式）。这**不是** fail-open — HITL 无法在没有 checkpoint 的情况下工作。但是，resume 任务的幂等性检查仍然使用 fail-open（与 D-072 一致）。

### 原因

1. **Checkpoint 丢失不可恢复**：与幂等性不同，checkpoint 丢失意味着 Agent 执行状态无法恢复
2. **明确错误优于静默损坏**：中止并报错比重复执行更好

---

## D-084: HITL Resume 与并发锁协调

### 决策

当 Agent 遇到 HITL 中断时，per-conversation 锁**不释放**。锁的 TTL 延长到覆盖 checkpoint TTL（如 24h + buffer）。这防止其他任务在 HITL 流程 pending 期间处理同一会话。`agent_resume` 任务复用同一锁（已被持有）。如果锁 TTL 过期（用户 24h 内未响应），会话解锁，正常处理恢复。

### 原因

1. **防止冲突**：没有协调，用户的普通消息可能触发新 Agent 执行，与 pending 的 HITL resume 冲突
2. **与 D-075 一致**：复用现有锁机制
3. **自然过期**：24h 后自动解锁，无需额外清理

---

## D-085: agent_resume RPC 规范

### 决策

新增 `agent_resume` RPC 方法。参数：

| 参数 | 必需 | 说明 |
|------|------|------|
| `conversation_id` | 是 | 会话 ID |
| `checkpoint_id` | 是 | Checkpoint ID |
| `question_id` | 是 | Question ID（D-116） |
| `answer` | 是 | 用户对 agent 问题的回答 |
| `agent_id` | 是 | 多 Agent 场景标识 |

**更新语义（D-116 引入后）**：

1. Handler 接收 `answer`，写入 Question 表（`status=answered, answer=<answer>`）
2. 幂等检查：如果 `Question.status != pending`，返回 `{code: 409, msg: "already_answered"}`
3. 检查同一 checkpoint 下是否所有 Questions 都已 answered：
   - **未全部回答**：返回 `{status: "partial", answered: N, total: M}`，不入队 MQ
   - **全部回答**：入队 `TypeAgentResume{checkpoint_id}` MQ 任务（payload 不含 answer），返回 `{status: "complete"}`
4. MQ worker 从 DB 读取所有 Questions，组装 `Targets map[interrupt_id]answer`，调用 `ResumeWithParams`

遵循 D-073（总是返回 nil 给 MQ）和 D-002（无鉴权）。

### 原因

1. **复用现有基础设施**：MQ 确保锁和幂等机制适用
2. **Thin RPC**：RPC 只是入队，实际处理在 MQ worker 中
3. **Answer 先写 DB**：确保服务器重启后答案不丢失（D-116 韧性设计）
4. **Partial answer 支持**：多 Question 场景下，用户可逐个回答，全部回答后自动 resume
5. **幂等保证**：Question.status 检查防止重复回答（多设备竞态安全）

---

## D-086: MCP Server 配置格式

### 决策

MCP Server 在 Agent YAML 配置的 `mcp_servers` 段中配置：

```yaml
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
  - name: github
    transport: sse
    url: https://mcp.github.io/sse
```

MCP 工具与自定义工具合并为 Agent 的单一工具集。MCP Server 连接失败时跳过并记录警告（fail-open，D-001 精神）。

### 原因

1. **Per-agent MCP**：不同 Agent 可连接不同工具生态
2. **双传输**：支持 stdio（本地进程）和 SSE（远程服务）

---

## D-087: Agent Ephemeral Update 类型扩展

### 决策

新增 2 个 ephemeral Update 类型到 `pkg/protocol/protocol.go`：

```go
UpdateTypeAgentStatus            = "agent_status"              // ephemeral: Seq=0
UpdateTypeAgentTimeout           = "agent_timeout"             // ephemeral: Seq=0
```

扩展现有 ephemeral 类型族（`typing`、`streaming`）。不持久化，不被 `sync_updates` 拉取，直接通过 `BroadcastUpdates` 广播。旧客户端静默忽略未知 ephemeral 类型（D-050 约束）。

### 原因

1. **与 D-050 一致**：所有 Agent 状态信号都是 ephemeral
2. **向后兼容**：旧客户端通过 default case 忽略未知类型

---

## D-088: 真实 LLM 测试分离（Real LLM Test Separation）

### 决策

真实 LLM 测试使用构建标签 `//go:build real_llm` 与 mock 测试分离。两种运行模式：

- **Quick 模式（默认）**：`go test ./internal/e2e/ -run TestAgent` — 59 个 mock 测试，~60 秒，零成本，零外部依赖
- **Full 模式（opt-in）**：`go test -tags real_llm ./internal/e2e/ -run TestAgentRealLLM` — 14 个真实 LLM 测试，~5-10 分钟，需要 API 密钥

### 原因

1. **Mock 测试用于日常开发和 CI**：快速反馈，零外部依赖，与 D-001（开箱即用）一致
2. **真实 LLM 测试用于集成验证**：确保系统实际调用外部 API 并处理真实响应
3. **构建标签是 Go 惯例**：`go test ./...` 默认不包含 `real_llm` 标签，不会意外运行
4. **环境变量作为第二道防线**：防止有人手动加了 `-tags real_llm` 但没配 API key 时运行
5. **双重保护**：构建标签 + 环境变量，任一缺失则跳过

### 实现

```go
// internal/e2e/agent_real_llm_test.go
//go:build real_llm

func TestAgentRealLLM_BasicChat(t *testing.T) {
    if os.Getenv("XYNCRA_TEST_REAL_LLM_ENABLED") != "true" {
        t.Skip("XYNCRA_TEST_REAL_LLM_ENABLED != true, skipping real LLM test")
    }
    // ... 真实 LLM 测试逻辑
}
```

### 约束

- `go test ./...` 默认只运行 mock 测试（59 个），不包含真实 LLM 测试
- 真实 LLM 测试需要同时满足两个条件：`-tags real_llm` 和环境变量 `XYNCRA_TEST_REAL_LLM_ENABLED=true`
- 真实 LLM 测试不测试工具调用（非确定性太高，由 mock 测试覆盖）

---

## D-089: 真实 LLM 测试环境变量（Real LLM Test Environment Variables）

### 决策

真实 LLM 测试使用以下环境变量配置：

| 变量 | 用途 | 必需 |
|------|------|------|
| `XYNCRA_TEST_LLM_API_KEY` | LLM API 密钥（同时作为运行时门控） | 是 |
| `XYNCRA_TEST_REAL_LLM_ENABLED` | 显式启用开关（`true`/其他值跳过） | 是 |
| `XYNCRA_TEST_REAL_API_KEY` | 运行时 API Key 环境变量（由 `setupAgentE2E` 自动从 `XYNCRA_TEST_LLM_API_KEY` 复制设置，Agent 配置通过 `api_key_env: XYNCRA_TEST_REAL_API_KEY` 读取） | 否（内部自动设置） |
| `XYNCRA_TEST_LLM_BASE_URL` | LLM API 端点 | 否（默认 `https://dashscope.aliyuncs.com/compatible-mode/v1`） |
| `XYNCRA_TEST_LLM_MODEL` | 模型名称 | 否（默认 `qwen3.7-plus`） |
| `XYNCRA_TEST_LLM_PROVIDER` | 提供商名称 | 否（默认 `qwen`） |
| `XYNCRA_TEST_REAL_LLM_TIMEOUT` | 单次请求超时（覆盖 `testTimeout` 的默认缩放值） | 否（默认按 base*6 缩放） |
| `XYNCRA_TEST_REAL_LLM_MAX_TOKENS` | 最大 token 数（覆盖 `realLLMAgentConfig` 的默认值） | 否（默认 `500`） |

所有变量使用 `XYNCRA_TEST_` 前缀（符合 D-048）。存储于 `.env.test`（gitignored，不提交）。配置模板 `.env.test.example` 可提交到 git。

### 原因

1. **与 D-048 一致**：测试环境变量统一使用 `XYNCRA_TEST_` 前缀
2. **双重门控**：`XYNCRA_TEST_LLM_API_KEY` 和 `XYNCRA_TEST_REAL_LLM_ENABLED` 同时要求，防止意外运行
3. **合理默认值**：默认模型、端点、超时等使用最经济实用的配置，与 D-090（成本控制）一致
4. **安全存储**：`.env.test` 包含 API 密钥，必须 gitignored；`.env.test.example` 不含敏感信息，可提交

### 约束

- `.env.test` 不得提交到 git（已在 `.gitignore` 中）
- `.env.test.example` 作为模板可提交，开发者复制后填入自己的 API 密钥
- 环境变量缺失时测试自动跳过（`t.Skip`），不报错

---

## D-090: 真实 LLM 测试成本控制（Real LLM Test Cost Control）

### 决策

真实 LLM 测试采用以下成本控制策略：

- 限制 14 个测试场景（从 59 个 mock 场景中选择核心子集，跳过工具调用测试）
- 默认使用最便宜的模型（`qwen3.7-plus`）
- `max_tokens` 限制为 500
- `context.max_messages` 限制为 5（短对话）
- `temperature` 设为 0.3（更确定性的输出）
- 构建标签 + 环境变量双重门控防止意外运行（D-088）
- 每个测试最多重试 2 次（处理 API 超时/限流）
- 不测试工具调用（真实 LLM 的工具调用非确定性太高，由 mock 测试覆盖）

### 原因

1. **成本可控**：14 个场景 x 500 tokens x 低成本模型，单次 Full 运行成本极低
2. **与 D-088 互补**：双重门控确保只有有意运行时才产生费用
3. **确定性优先**：`temperature=0.3` 减少输出随机性，提高测试稳定性
4. **短对话限制**：`max_messages=5` 避免长上下文带来的 token 成本增长
5. **工具调用排除**：真实 LLM 的工具调用格式不保证一致，mock 测试已充分覆盖此场景

### 约束

- 测试场景选择应覆盖核心功能：基础对话、多轮对话、上下文管理、错误处理等
- 不覆盖工具调用、MCP、子 Agent 等高级功能（由 mock 测试负责）
- 重试次数限制为 2 次，避免 API 限流导致测试长时间挂起

---

## D-091: Agent 输入边界定义

### 决策

Agent 系统对各类输入边界情况采用以下标准化处理策略，确保在各种极端输入下不崩溃、行为可预期：

| 输入类型 | 阈值 | 行为 |
|----------|------|------|
| 空消息 | `content == ""` 或纯空白 | 拒绝处理，返回错误消息 |
| 超长输入 | `len(content) > 10000` 字符 | Token 截断 (D-060)，正常处理 |
| 消息 burst | 同一会话 > 5 条/秒 | 串行处理 (D-075)，排队等待 |
| 超大上下文 | 超过 `context.max_tokens` | Token 裁剪优先，消息数 fallback |
| 特殊字符 | emoji, CJK, RTL, null bytes | 不崩溃，尽力处理 |

### 原因

1. **与 D-049 互补**：D-049 定义了弱网韧性测试策略（内联 mock server，周期断连），D-091 定义输入层面的韧性边界，两者共同覆盖 Agent 系统的韧性维度
2. **与 D-060 一致**：上下文管理已有 Token 裁剪优先、消息数 fallback 策略，D-091 将其明确扩展为输入边界规范
3. **与 D-067 一致**：空消息等无效输入通过持久化中文错误消息反馈给用户，与现有错误消息策略保持一致
4. **与 D-072 一致**：fail-open 精神——极端输入不应导致 Agent 完全不可用，应尽力处理或给出友好反馈
5. **与 D-075 一致**：消息 burst 场景复用 per-conversation 分布式锁的串行处理机制，无需引入额外限流层

### 各边界行为详解

**空消息**：当用户发送 `content == ""` 或纯空白（空格、制表符、换行符组合）消息时，Agent 不触发 LLM 调用，直接通过 `ExecuteWithErrorMessage` 持久化一条友好错误消息（如 "消息内容为空，请重新发送。"），避免无意义的 API 调用。

**超长输入**：当输入超过 10000 字符时，不拒绝处理，而是交由 D-060 的 `DBContextManager` 进行 Token 裁剪。`HeuristicTokenCounter`（`len(text)/4`）负责估算 token 数，超出 `context.max_tokens` 的消息被裁剪。这样既保证功能不中断，又防止 LLM 上下文溢出。

**消息 burst**：当同一会话短时间内收到多条消息（> 5 条/秒）时，D-075 的 per-conversation 分布式锁（Redis `SETNX`，key `agent:lock:{conversationID}`）确保同一时间只有一个 Agent 任务执行。后续消息的任务排队等待当前任务完成后处理最新上下文。

**超大上下文**：当对话历史的 token 总量超过 Agent 配置的 `context.max_tokens` 时，`DBContextManager` 先尝试 Token 裁剪（`trimByTokens`），从最旧消息开始移除直至总 token 数在限制内。当 `MaxTokens == 0` 时 fallback 到消息数裁剪（`trimByMessages`），按 `context.max_messages` 固定数量裁剪。

**特殊字符**：emoji、CJK（中日韩）字符、RTL（从右到左书写系统如阿拉伯语）字符、null bytes（`\x00`）等特殊输入不应导致 Agent 崩溃或 panic。系统应尽力处理这些字符——LLM API 通常能正确处理 Unicode，服务器侧只需保证 JSON 编解码不 panic、字符串操作不因特殊字符截断产生无效 UTF-8。

### 约束

- 空消息和超长输入的处理在 `AgentExecutor.Execute` 入口处检查，在 LLM 调用之前
- 特殊字符处理不做主动清洗（不删除 emoji 或 null bytes），而是确保编解码链路能正确处理
- 消息 burst 的处理完全依赖 D-075 的并发锁，不引入独立的 rate limiter
- 超大上下文的裁剪策略已在 `DBContextManager` 中实现（D-060），D-091 仅规范行为边界

### 与现有决策的关系

- **D-049**：弱网韧性（网络层）与本决策（输入层）互补
- **D-060**：上下文裁剪策略是"超大上下文"和"超长输入"边界的具体实现
- **D-067**：空消息拒绝时通过持久化错误消息反馈用户
- **D-072**：fail-open 精神——极端输入尽量处理而非拒绝
- **D-075**：消息 burst 场景直接复用 per-conversation 分布式锁

---

## D-092: ReverseRPC 双向请求能力

### 决策

服务端可通过 `ReverseRPC` 组件向指定用户的所有活跃连接发起 RPC 请求并同步等待响应。ReverseRPC 为可选组件（nil-safe，与 D-063 模式一致），为 nil 时退化为当前行为（服务端不处理客户端的 `PackageTypeResponse`）。

Request ID 使用 `"s-"` 前缀 + 原子计数器（`"s-1"`, `"s-2"`, ...），与客户端纯数字 ID（`"1"`, `"2"`, ...）命名空间隔离，避免双向通信中的 ID 冲突。

请求发送给目标用户的所有活跃本地连接（用户级广播），第一个到达的响应被接受，后续响应被静默丢弃。使用非阻塞发送（复用 `Client.Send` 的 channel 模式），超时兜底。

### 原因

1. **HITL 基础设施**：HITL 流程中需要服务端主动向客户端获取结构化响应（如用户选择），当前 ephemeral push + 客户端主动 `agent_resume` RPC 的模式在 E2E 测试中难以编排
2. **复用现有通道**：复用 WebSocket 双向通道，不引入新的传输层
3. **与 D-050 互补**：ReverseRPC 不替代 ephemeral push，而是补充。HITL 通知由 conversation update（D-118 Pull-on-Notification）承载，ReverseRPC 用于结构化地获取用户回答
4. **幂等响应**："第一个响应被接受"的语义与客户端 `dispatchResponse` 的幂等删除模式一致

### 实现

- `internal/server/reverse_rpc.go`：`ReverseRPC` 组件，pending map + channel 模式（与客户端 `XyncraClient` 对称）
- `internal/server/websocket_server.go`：集成 ReverseRPC，暴露 `ServerRequest` 便捷方法
- `internal/server/websocket_handler.go`：`PackageTypeResponse` 分支 dispatch 到 ReverseRPC
- `pkg/client/connection.go`：`readPump` 新增 `PackageTypeRequest` 分支
- `pkg/client/client.go`：新增 `RegisterRequestHandler` + `handleIncomingRequest`

### 约束

- 不持久化离线的反向请求：用户无连接时立即返回错误。ReverseRPC 请求超时（`DeadlineExceeded`）后，通过 `PendingStore` 持久化到 Redis（D-103），支持设备重连后补发（Phase 5）
- 初始实现仅支持单节点（本地连接广播），不跨节点路由
- 与 `agent_resume` RPC（D-085）互补而非替代：`agent_resume` 保持不变，ReverseRPC 是独立的基础设施
- Send 返回 error（`ErrSendBufferFull` 或 `ErrClientClosed`），`ServerRequest` 收到发送错误后立即返回（无需等待超时），调用方可快速响应
- 正常断连时（非设备替换），该设备的所有 pending ReverseRPC 请求通过 `CancelDeviceWithReason` 立即 fail，错误消息为 "device disconnected"

### 与现有决策的关系

- **D-002/D-003**：内网部署模型，ReverseRPC 不新增鉴权层
- **D-050**：ReverseRPC 补充 ephemeral push，不替代
- **D-063**：nil-safe 可选注入模式
- **D-085**：`agent_resume` RPC 保持不变，两者互补
- **D-018**：初始不跨节点路由，后续可扩展

---

## D-093: 连接模型扩展为 (userID, deviceID, connID)

### 决策

连接管理索引从 `(userID, connID)` 扩展为 `(userID, deviceID, connID)`。服务端维护两级索引：`clientsByUser`（userID → connID → Client）用于广播，`clientsByDevice`（"userID\x00deviceID" → connID → Client）用于定向发送。新增 `sendToDevice(userID, deviceID, pkg)` 方法实现定向发送，与 `sendToUser(userID, pkg)` 广播方法共存。`device_id` 通过 WebSocket 连接 URL 的 query parameter 传递（与 `user_id` 一致）。

### 原因

1. **Agent Tool 基础设施**: Client Function Agent Tools 功能需要服务端定向调用发起会话的特定设备（D-092 延伸）
2. **与 D-005 一致**: device_id 通过 query param 传递，开发友好
3. **向后兼容**: 空 device_id 自动生成 UUID（D-094），旧客户端无感迁移
4. **无跨设备聚合**: Agent 只调用发起会话的设备，不需要设备选择逻辑

### 约束

- `sendToDevice` 当前仅支持单节点本地索引，跨节点定向发送为未来演进方向（与 D-018 的关系）
- `sendFunc` 签名扩展为 `func(userID, deviceID string, pkg *protocol.Package) error`，空 `deviceID` 表示广播（兼容 D-092 现有行为）
- 客户端 `device_id` 由客户端生成（CLI 使用 D-033 策略，其他客户端可自定义）

---

## D-094: 空 device_id 自动生成 UUID

### 决策

当客户端未提供 `device_id` 查询参数时，服务端自动生成 UUID v4 作为该连接的 device_id。每连接成为独立"设备"，行为与当前一致（每连接独立存在，不触发设备替换）。自动生成时输出 `INFO` 级别日志。

### 原因

1. **与 D-001 一致**: 零配置，旧客户端无需任何改动即可工作
2. **安全降级**: 不发送 device_id 的客户端失去定向发送能力，但基础功能不受影响
3. **调试友好**: INFO 日志帮助开发者识别哪些客户端尚未迁移

### 约束

- 自动生成的 UUID 确保每连接独立，不会意外触发设备替换
- CLI 客户端使用 D-033（主机名 SHA256 前 8 位）生成的 device_id 不受此影响——CLI 总是发送 device_id

---

## D-095: 设备替换策略

### 决策

同 `(userID, deviceID)` 新连接到来时，先 Upgrade 新连接并原子注册，然后在 goroutine 中异步向旧连接发送 Close Frame（code: 4001, reason: "replaced by new connection from same device"）并清理旧连接（HTTP handler 零阻塞）。旧连接的 pending ReverseRPC 请求通过 `CancelDevice(userID, deviceID)` 立即 fail（不等超时）。

### 原因

1. **防止消息重复投递**: 同一设备 ID 的多连接共存会导致 sendToDevice 不确定路由
2. **快速失败**: 旧连接的 pending 请求立即 fail 而非等待超时，调用方可快速响应
3. **确定性行为**: 新连接总是替换旧连接，没有"两个连接共存"的歧义

### 实现要点

- Close Frame code 4001 为应用自定义码（在 4000-4999 范围内）
- 替换时序：先 Upgrade 新连接，然后在 goroutine 中异步清理旧连接（HTTP handler 零阻塞）
- 原子注册：新连接在单次锁操作内完成注册
- 旧连接已关闭时（竞态），跳过 Close Frame 发送，正常注册新连接

### 约束

- 设备替换的清理逻辑已异步化，HTTP handler 零阻塞
- 设备替换是节点内行为，不涉及跨节点协调
- 与 D-031 互补：CLI 的进程锁防止同一 (user_id, device_id) 重复启动 listen，避免意外替换震荡
- 设备替换时，调用方立即收到 fail 错误（行为不变）。但请求数据已持久化到 Redis PendingStore（D-103），不被 CancelDevice 清理，由 Phase 5 补发机制在新连接建立后重发（D-105）

---

## D-096: ReverseRPC sendFunc 签名扩展

### 决策

`ReverseRPC.sendFunc` 签名从 `func(userID string, pkg *protocol.Package) error` 扩展为 `func(userID, deviceID string, pkg *protocol.Package) error`。`ServerRequest()` 签名相应新增 `deviceID` 参数。空 `deviceID` 表示广播到用户所有连接（保持向后兼容），非空 `deviceID` 表示定向发送到指定设备。

### 原因

1. **向后兼容**: 空 deviceID 的广播行为与现有 `sendToUser` 语义一致
2. **统一接口**: 一个 sendFunc 同时支持广播和定向，调用方通过 deviceID 参数控制
3. **与 D-092 兼容**: D-092 的用户级广播行为通过空 deviceID 保持

---

## D-097: reqID 格式：UUID 替代原子计数器

### 决策

ReverseRPC 的 reqID 从 `fmt.Sprintf("s-%d", atomic.AddUint64(...))` 改为 `fmt.Sprintf("s-%s", uuid.New().String())`。保留 `"s-"` 前缀（与客户端纯数字 ID 命名空间隔离）。同时移除 `nextReqID uint64` 字段和 `sync/atomic` 依赖。客户端 `XyncraClient.Call()` 的 reqID 也改为 UUID（无前缀）。

### 原因

1. **避免重启冲突**: 原子计数器重启后从 0 递增，可能与 Redis 中旧实例的 pending 请求 ID 冲突
2. **UUID 全局唯一**: 122 bit 随机性，碰撞概率可忽略
3. **保留 "s-" 前缀**: 维持与客户端 ID 的命名空间隔离，现有测试断言 `HasPrefix("s-")` 不受影响

### 约束

- reqID 长度从短字符串变为 ~38 字符（`"s-"` + 36 字符 UUID），日志空间开销略增
- 客户端 reqID 也改为 UUID，与服务端命名空间隔离（客户端无前缀，服务端有 `"s-"` 前缀）

---

## D-098: `system.` 命名空间用于系统级 RPC 方法

### 决策

引入 `system.` 前缀作为系统级 RPC 方法的命名空间，与业务方法（如 `send_message`, `create_conversation`）区分。系统方法不直接参与业务逻辑，而是用于客户端与服务端之间的元数据交换和能力声明。

当前定义的系统方法：
- `system.register_functions`: 客户端声明设备函数能力

### 原因

1. **命名空间隔离**：系统方法与业务方法分离，职责清晰
2. **可扩展性**：未来可新增 `system.reconnect`, `system.ping`, `system.get_capabilities` 等
3. **向后兼容**：不影响现有业务方法
4. **语义明确**：客户端开发者一看便知这是系统级操作，非业务操作

### 约束

- 系统方法以 `system.` 开头，业务方法不使用此前缀
- 系统方法的参数和响应格式独立定义，不与业务方法混用
- 系统方法无鉴权（与 D-002 一致，内网部署模型）

---

## D-099: 客户端函数清单使用 JSON Schema 描述参数

### 决策

客户端通过 `system.register_functions` 声明设备函数能力。每个函数使用以下格式：

```json
{
  "name": "read_file",
  "description": "读取本地文件内容",
  "parameters": {
    "type": "object",
    "properties": {
      "path": {"type": "string", "description": "文件路径"}
    },
    "required": ["path"]
  },
  "returns": {"type": "string", "description": "文件内容"},
  "tags": ["filesystem", "read"],
  "timeout_ms": 5000
}
```

字段说明：
- `name` (必填): 函数唯一标识，设备内唯一，最长 255 字符
- `description` (可选): 人类可读的函数描述
- `parameters` (可选): JSON Schema (draft 7) 描述输入参数
- `returns` (可选): 描述返回值类型
- `tags` (可选): 标签数组，用于过滤
- `timeout_ms` (可选): 函数执行超时（毫秒），0 或不填表示使用默认超时

### 原因

1. **JSON Schema 标准化**：广泛使用的参数描述格式，工具链丰富
2. **向后兼容**：可选字段允许渐进式扩展
3. **Agent 友好**：LLM 可直接理解 JSON Schema 生成调用参数
4. **客户端灵活**：客户端可使用任意编程语言，只需生成符合格式的 JSON

### 约束

- `name` 是必填字段，为空时服务端拒绝注册
- `parameters` 必须是合法的 JSON Schema，但服务端不校验 schema 本身（只存储）
- `timeout_ms` 为 0 或不填时，Agent 调用时使用配置的默认超时（如 30s）
- 每个设备最多注册 200 个函数（可通过 `XYNCRA_MAX_FUNCTIONS_PER_DEVICE` 环境变量调整）
- 函数清单格式一旦确定，尽量避免破坏性变更；如需变更，使用版本号（如 `version: 1`）

---

## D-100: 客户端工具错误返回给 LLM 自主处理

### 决策

客户端工具（DynamicToolProvider 注入的工具）调用失败时，错误作为 tool error 返回给 LLM，由 LLM 决定重试策略或向用户说明。不直接触发 D-067 错误消息持久化。

### 原因

1. **LLM 自主决策**：LLM 可能选择重试、换一种方式、或告知用户，比硬编码的错误消息更灵活
2. **与 D-067 互补**：D-067 覆盖的是 Agent 整体执行失败（LLM API 错误、上下文加载失败等），客户端工具失败是工具层面的局部错误
3. **与 Eino 框架一致**：Eino 的 tool error 机制就是让 LLM 看到工具失败并自行处理
4. **D-082 范围澄清**：D-082 的"工具执行失败"分类适用于服务端工具（`ToolRegistry` 注册的静态工具）的致命错误，客户端工具失败走不同的路径

### 约束

- 工具 error 消息应对 LLM 友好（描述性文本，非 Go error 原始格式）
- 如果 LLM 在收到 tool error 后仍然完成了 Agent 执行（即使回复了"工具调用失败"），不触发 D-067
- 仅当 Agent 整体执行异常（panic、LLM API 错误等）时才触发 D-067
- 设备断连时，工具错误消息为 "device disconnected"（正常断连）或 "device replaced"（设备替换），与 "device is offline"（设备从未连接，`ErrDeviceOffline`）区分。LLM 可根据不同错误消息采取不同策略

---

## D-101: ClientFunctionProvider/ClientCaller 接口定义在 agent 包

### 决策

为避免 `agent` 包导入 `server` 包造成循环依赖，在 `agent` 包定义 `ClientFunctionProvider` 和 `ClientCaller` 接口。`server` 包的具体实现（`MemoryFunctionRegistry`、`ReverseRPC`/`WebSocketServer`）通过 Go duck typing 满足接口。

### 原因

1. **与 D-070 一致**：Agent 功能的所有权边界在 `internal/agent/`，依赖方向为 server → agent（接口定义）
2. **可测试性**：测试时可注入 mock 实现
3. **零适配器开销**：Go duck typing 无需额外的 adapter struct

### 约束

- 接口方法签名必须与 server 包的实现精确匹配
- `main.go` 负责组装：将 server 包的具体实例传入 agent 包的 setter

---

## D-102: DeviceID 通过 MQ payload 传播到 Agent context

### 决策

`agentProcessPayload` 新增 `DeviceID` 字段，从 `send_message` handler 通过 MQ 传播到 `ExecutePayload`，再由 executor 写入 context（`ContextWithCallerDevice`）供 DynamicToolProvider 读取。

### 原因

1. **与 D-062 一致**：MQ payload 是 Agent 任务数据的标准传递路径
2. **向后兼容**：JSON 增量字段，旧 payload 反序列化后 DeviceID 为空字符串
3. **与 D-093 配合**：D-093 建立了 (userID, deviceID) 索引，DynamicToolProvider 利用此基础设施
4. **context 不能跨 MQ**：Asynq 反序列化任务时使用新 context，context.Value 无法传递，必须通过 payload 字段

### 约束

- `DeviceID` 字段在 `agentProcessPayload` 和 `ExecutePayload` 中均为 string 类型
- 空字符串表示"无设备信息"（旧版 payload 或无设备连接的场景）
- `agent_resume` 任务同样需要传播 DeviceID

---

## D-103: ReverseRPC Pending Store（Redis 持久化）

### 决策

ReverseRPC `ServerRequest` 超时（`context.DeadlineExceeded`）后，请求通过 `PendingStore` 接口异步持久化到 Redis。Key 格式为 `pending:{userID}\x00{deviceID}`，使用 Redis List 存储 JSON 编码的 `PendingRequest`。仅 `DeadlineExceeded` 触发持久化；`CancelDevice`（设备替换/断连）和 `ContextCancel`（父 context 取消）不持久化。Fail-open 策略：Redis 写入失败仅记录日志，不阻塞 ServerRequest 返回（与 D-072 一致）。

PendingStore 为可选组件（nil-safe，与 D-063 模式一致），nil 时退化为当前行为（超时即丢弃）。

### 原因

1. **与 D-007 一致**：数据持久化是第一优先级，超时后的请求数据不应丢失
2. **与 D-072 一致**：fail-open 策略保证可用性优先
3. **零新依赖**：复用现有 Redis 基础设施和 `redisIdempotencyClient`（D-074）
4. **Phase 5 基础设施**：为后续重连握手和请求补发提供数据基础

### 约束

- 异步持久化（goroutine），不阻塞 ServerRequest 调用方
- TTL 默认 24h，通过 `EXPIRE` 在每次 Save 时刷新
- 每个设备最多存储 50 条 pending 请求（LTRIM 淘汰最旧）
- `PendingStore` 接口支持 Save/List/Remove/RemoveByDevice 操作

---

## D-104: ReverseRPC 幂等键与 Seq 协议扩展

### 决策

`PackageDataRequest` 新增两个字段，均使用 `omitempty` 保持向后兼容：

- `IdempotencyKey string`：等于 reqID（`"s-" + UUID`），用于补发去重
- `Seq uint64`：per-device 单调递增序号，用于请求排序

旧客户端解析时忽略这两个字段（JSON omitempty 语义），新客户端可选择使用。

### 原因

1. **与 D-097 配合**：reqID 已改为 UUID，天然全局唯一，直接复用为幂等键
2. **向后兼容**：omitempty 确保旧客户端无感
3. **Phase 5 基础设施**：Seq 用于重连握手时计算缺失请求范围

### 约束

- `IdempotencyKey` 由服务端生成（= reqID），客户端不需要生成
- `Seq` 由服务端 per-device 分配，客户端只读
- 旧客户端不发送这两个字段时，服务端行为不受影响

---

## D-105: CancelDevice 不清理 Redis Pending

### 决策

`CancelDevice`（设备替换/断连）不清理 Redis PendingStore 中的请求。Pending 请求保留至 TTL 到期（D-103 的 request_ttl）或 Phase 5 成功补发。

这与 D-095（调用方立即 fail）互补——调用方立即收到错误，但数据层面保留补发机会。

### 原因

1. **Phase 5 补发前提**：如果 CancelDevice 清理了 Redis pending，Phase 5 的补发机制就无从谈起
2. **与 D-095 不冲突**：D-095 的"立即 fail"是指调用方（ServerRequest 的 caller）立即收到错误；Redis pending 保留是数据层面的决策，不影响调用方行为
3. **TTL 自动清理**：24h TTL 确保不会无限堆积

### 约束

- 设备替换时，旧连接的 ServerRequest 走 respCh 分支（收到合成响应），不走 ctx.Done() 分支，因此**不会触发持久化**
- 只有真正超时（DeadlineExceeded）的请求才会被持久化到 Redis
- `CancelAll()`（优雅关闭）同样不清理 Redis pending

---

## D-106: Per-device Seq 计数器策略

### 决策

每个 (userID, deviceID) 维护一个内存中的单调递增 seq 计数器，用于 `PackageDataRequest.Seq` 字段分配。Phase 4 使用内存计数器（`sync.Mutex` + `map[string]uint64`），服务器重启后重置为 0。Phase 5 可升级为 Redis INCR（跨重启持久化）。

Seq 仅用于排序（Phase 5 重连握手时计算缺失请求范围），不用于幂等性判断——幂等性由 `IdempotencyKey`（UUID）保证。

### 原因

1. **Phase 4 简化**：内存计数器足够满足单节点场景，无需引入 Redis 依赖
2. **幂等性不依赖 Seq**：即使 seq 重启归零，IdempotencyKey（UUID）仍保证去重正确
3. **Phase 5 升级路径明确**：Redis INCR 是自然演进方向

### 约束

- 服务器重启后 seq 从 0 重新递增
- 如果旧请求仍在 Redis pending 中（seq 较大），新请求的 seq 可能更小——但 seq 仅用于排序，不影响正确性
- 并发安全由 `sync.Mutex` 保证

---

## D-107: Replay 请求 ID 格式

### 决策

Replay 请求使用 `s-replay-{uuid}` 格式（如 `s-replay-550e8400-e29b-41d4-a716-446655440000`）。

### 原因

- 扩展 D-097 的 `s-{uuid}` 格式，添加 `-replay-` 段用于日志区分
- 保留 `s-` 前缀维持服务端发起请求的命名空间隔离
- UUID 保证唯一性，避免服务器重启后 ID 冲突

### 实现

- `ReverseRPC.ReplayRequest` 生成新 reqID 时使用 `fmt.Sprintf("s-replay-%s", uuid.New().String())`
- 新 reqID 避免与原始 pending entry 在 `r.pending` map 中冲突
- 原始 IdempotencyKey 保留不变，用于客户端幂等去重

---

## D-108: system.reconnect RPC 规范

### 决策

新增 `system.reconnect` RPC 方法，客户端重连后调用此方法触发服务端补发断连期间的超时请求。

### 规范

- 客户端发送 `system.reconnect` 请求，params 为 `{last_seen_seq: uint64}`
- 服务端从 PendingStore 查询该设备的 pending 请求
- 过滤条件：`Seq > last_seen_seq` AND `RetryCount < MaxRetries`
- 对每个符合条件的请求启动 goroutine 异步补发
- 立即返回 `{status: "ok", replayed: N, total: M}`

### 设计约束

- Fail-open：Redis 错误仅记录日志，返回 `{replayed: 0, total: 0}`（D-072）
- 无鉴权：属于 `system.` 命名空间，内网部署模型（D-002/D-098）
- Nil-safe：仅在 PendingStore 和 ReverseRPC 均可用时注册（D-063）
- `last_seen_seq` 缺失时默认为 0，即补发全部 pending 请求

### 补发生命周期

- 补发成功（resp.Code == 0）→ 从 PendingStore 移除
- 补发超时/错误 → RetryCount++
- RetryCount > MaxRetries → 从 PendingStore 移除并放弃
- PendingStore 操作均 fail-open，错误仅记录日志

---

## D-109: 补发并发与超时策略

### 决策

每个补发请求一个独立 goroutine，超时 10 秒，超过 MaxRetries 放弃。

### 参数

| 参数 | 值 | 说明 |
|------|-----|------|
| replayTimeout | 10s | 单个补发请求超时时间 |
| 最大并发 | MaxPendingPerDevice (50) | 每设备最大 pending 请求数 |
| MaxRetries | 3（默认） | 最大补发重试次数 |

### 原因

- 每请求一个 goroutine（最多 50 个）≈ 200KB 内存，开销可忽略
- Worker pool 增加复杂度但收益不大
- 10s 超时：原始调用方已超时离开，10s 足够客户端处理
- 保留原始 IdempotencyKey 用于客户端幂等去重

---

## D-110: E2E 测试 MQ 异步任务直接调用策略

### 决策

E2E 测试中，MQ 异步任务（如 `agent_process`、`agent_resume`）的投递通过直接调用 executor/handler 绕过 MQ，测试业务逻辑而非 MQ 基础设施。测试辅助函数 `triggerAgentProcessing` 和 `triggerAgentResume` 分别对应这两个场景。

### 原因

1. **与 D-049 一致**: 测试应控制其依赖（D-049 使用内联 mock server）
2. **MQ 可靠性非测试目标**: Asynq worker 的异步投递在 E2E 环境中不可靠，这不是 Xyncra 的测试目标
3. **代码覆盖等价**: 直接调用路径与 MQ 路径共享相同的 executor/handler 代码
4. **零生产影响**: 生产代码不变

### 约束

- 直接调用必须包含完整 payload（含 DeviceID，D-102）
- 生产 `agent_resume` RPC 仍走 MQ（D-085 不变）
- `triggerAgentResume` 通过 `taskHandler.ProcessTask` 触发（包含锁逻辑，D-084）
- `triggerAgentProcessing` 通过 `executor.Execute` 直接调用（含完整 pipeline）

---

## D-111: 客户端 4001 语义感知

### 决策

当客户端收到服务器发送的 Close Frame（code: 4001）时，标记当前连接为"被替换"状态，不触发重连循环。daemon 进入休眠状态，IPC 保持可用。

### 原因

1. **防止重连死循环**：Docker 端口转发场景下，设备替换逻辑阻塞 HTTP handler，导致客户端超时重连，形成无限循环
2. **语义正确性**：4001 表示"被新连接替换"，旧连接应安静退出，而非重试
3. **与 D-095 一致**：新替旧策略下，旧连接的重连是无意义的（新连接已接管）

### 实现

- 客户端 `readPump` 检测到 4001 CloseError 后设置 `replaced=true`
- `handleDisconnect` 将 `replaced` 标志传递给 `onDisconnect` 回调
- `connectionMonitorWithInitialConnect` 检测 `Replaced()` 后进入休眠（等待 `replacedWake` 通道）
- IPC 服务器继续运行，本地查询命令可用（D-035）
- `XyncraClient.Reconnect()` 可唤醒连接监视器恢复连接

### 约束

- 4001 是唯一触发"不重连"语义的关闭码
- 其他关闭码（如 1000、1006）仍触发重连（D-044）
- daemon 休眠后，`Reconnect()` 方法可恢复 WS 连接
- 设备替换的清理逻辑在服务端异步执行，HTTP handler 零阻塞

---

## D-112: Checkpoint 清理策略

### 决策

resume 成功后立即删除 Redis 中的 checkpoint（Redis DEL）。删除是幂等的（key 不存在不报错）。同时保留 TTL 24h 作为安全网，覆盖异常路径（resume 失败、进程崩溃等）。Delete 失败只记日志，不阻塞 resume 成功状态（非致命）。

### 原因

1. **及时清理**：resume 成功后 checkpoint 不再需要，立即删除释放 Redis 内存
2. **幂等安全**：Redis DEL 对不存在的 key 不报错，重复删除无副作用
3. **TTL 安全网**：24h TTL 覆盖异常路径——如果 resume 成功后的 DEL 因网络等原因失败，TTL 保证 checkpoint 最终被清理
4. **非致命**：Delete 失败不影响 resume 的成功状态，用户已经得到了答案

### 约束

- DEL 操作在 resume 成功后执行，不在 resume 开始前
- Delete 失败仅记录 WARN 级别日志
- TTL 24h 与 D-084（HITL Resume 与并发锁协调）的锁 TTL 一致

---

## D-113: ~~interruptIDs 内存存储策略~~ → 已被 D-116 替代

> **状态**: 已废弃（2026-07-15）
> **替代方案**: D-116（Question 持久化表）

### 原决策

使用 `sync.Map` 存储在 `AgentExecutor` 中，映射 `conversationID → interruptID`。daemon 重启后丢失，不持久化。

### 废弃原因

D-116 引入 Question 持久化表后，`interrupt_id` 存储在 Question 表中，通过 DB 查询获取。`sync.Map` 的内存存储方式存在以下问题：

1. 服务器重启后丢失，导致 HITL 恢复失败
2. 无法支持多设备场景（答案需要先持久化）
3. 无法支持并行 Sub-Agent HITL（多 Question 场景）

### 迁移指南

- 删除 `AgentExecutor.interruptIDs sync.Map` 字段
- `agent_resume` handler 改为从 Question 表查询 `interrupt_id`（通过 `question_id` 参数）
- Task payload 只含 `checkpoint_id`，不含 `interrupt_id` 或 `answer`

---

## D-114: agent-resume 为 IPC-only 命令

### 决策

`agent-resume` CLI 命令不提供 WebSocket fallback，仅通过 IPC 与守护进程交互。需要 daemon 运行中（`xyncra-client listen`）。当守护进程未运行时，返回错误并提示用户启动 `listen`。

### 原因

1. **与 D-036 一致**：HITL 状态存在于 daemon 进程（interruptIDs 映射 D-113、WebSocket 连接），resume 需通过 daemon 的 WebSocket 连接发送到服务端
2. **独立连接无法 resume**：resume 需要精确的 (userID, deviceID) 路由，只有 daemon 持有此状态
3. **与 set-typing/stream-text 同类**：都是依赖 daemon 状态的命令

### 约束

- 守护进程未运行时返回错误（退出码 2，与 D-042 一致）
- 错误信息提示用户启动 `xyncra-client listen`

---

## D-115: Daemon 内置函数自动注册

### 决策

`listen` daemon 启动时自动注册内置函数（ping、get_device_info、get_time），不需要独立的 `register-functions` 进程。函数通过 `system.register_functions` RPC 注册到服务端 FunctionRegistry，ReverseRPC 调用通过 `RegisterRequestHandler` 机制在 daemon 内处理。daemon 重连后（包括 4001 设备替换后 Reconnect 唤醒），`performReconnectHandshake` 自动重新注册函数（D-044 韧性策略）。函数注册失败采用 fail-open 策略（D-072），不阻塞 daemon 启动。

### 原因

1. **消除设备替换冲突**: 原 `register-functions` 独立进程与 daemon 使用相同 (userID, deviceID)，触发 D-095 设备替换互相踢掉
2. **单连接架构**: 所有功能（消息接收、函数注册、ReverseRPC 处理）合并到一个 WS 连接
3. **与 D-044 一致**: daemon 连接韧性——重连后自动恢复函数注册
4. **与 D-001 一致**: 开箱即用，不需要额外启动第二个进程

### 内置函数

| 名称 | 描述 | 标签 |
|------|------|------|
| `ping` | 回声测试，验证 ReverseRPC 通道 | diagnostic |
| `get_device_info` | 设备基本信息（hostname、OS、arch） | diagnostic |
| `get_time` | 设备当前时间（UTC + timezone） | diagnostic |

### 约束

- 内置函数硬编码在 `internal/cli/builtin_functions.go`，当前不支持用户自定义
- 函数注册使用 10 秒独立超时，不阻塞 FullSync 和 daemon 启动
- handler 错误透传给服务端/Agent，由 LLM 自主处理（D-100）
- daemon 通过 `--device-info` flag 接受 JSON 格式设备元数据（如 `'{"name":"MacBook","os":"darwin"}'`），默认为空

---

## D-116: Question 持久化表（HITL 韧性）

### 决策

新增 `Question` 数据库表，用于持久化 HITL 问题与答案。替代原 D-113 的 `interruptIDs sync.Map` 内存存储方案。

**Question 表结构**：

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID | 主键 |
| `conversation_id` | string | FK → Conversation |
| `checkpoint_id` | string | 关联的 Eino checkpoint |
| `interrupt_id` | string | Eino interrupt address ID |
| `question_text` | text | 问题内容 |
| `status` | enum | `pending` / `answered` |
| `answer` | text | 用户回答（nullable） |
| `answered_by` | string | 回答者 user_id |
| `answered_device_id` | string | 回答设备 |
| `created_at` | timestamp | 创建时间 |
| `answered_at` | timestamp | 回答时间（nullable） |

**核心原则**：Answer 先写 DB，再入队 MQ。Task payload 只含 `checkpoint_id`，不含 answer。

### 原因

1. **服务器重启韧性**：答案持久化到 DB，服务器重启后不丢失（D-113 的 sync.Map 重启后丢失）
2. **多设备竞态安全**：Question.status 检查提供幂等保证，防止重复回答
3. **并行 Sub-Agent HITL**：支持一个 checkpoint 对应多个 Question（1:N 关系）
4. **Partial answer 支持**：用户可逐个回答问题，全部回答后自动 resume

### 实现要点

- Agent 调用 `ask_user` 时，为每个 interrupt 创建一条 Question 记录（status=pending）
- 更新 Conversation.agent_status = `asking_user`（D-117）
- 广播 conversation update（D-118 Pull-on-Notification）
- `agent_resume` handler 接收 answer，写入 Question 表（status=answered）
- 检查同一 checkpoint 下所有 Questions 是否都已 answered
- 全部 answered 后，入队 `TypeAgentResume{checkpoint_id}`（不含 answer）
- MQ worker 从 DB 读取所有 Questions，组装 `Targets map[interrupt_id]answer`

### 约束

- Question 表与 Conversation 是 1:N 关系
- Question.status 只能从 `pending` 转为 `answered`，不可逆
- 幂等检查：如果 Question.status != pending，返回 409 错误
- 服务器重启后，Question 状态保留，用户可继续回答

---

## D-117: Conversation 状态机

### 决策

Conversation 模型新增 `agent_status` 字段，表示 Agent 当前执行状态。状态转换遵循有限状态机模型。

**状态定义**：

| 状态 | 含义 |
|------|------|
| `idle` | 空闲，无 Agent 执行 |
| `thinking` | Agent 正在推理（LLM 调用中） |
| `tool_calling` | Agent 正在调用工具 |
| `generating` | Agent 正在生成流式输出 |
| `asking_user` | Agent 暂停，等待用户回答（HITL） |
| `timeout` | Agent 执行超时 |

**状态转换**：

```
idle → thinking: 用户发消息
thinking → tool_calling: 调用工具
tool_calling → generating: 工具返回
tool_calling → thinking: 继续推理
generating → asking_user: ask_user 工具
generating → idle: 执行完成
asking_user → thinking: 全部 questions answered → resume
thinking/tool_calling/generating → timeout: 超时
timeout → idle
```

**Conversation 扩展字段**：

| Field | Type | Description |
|-------|------|-------------|
| `agent_status` | enum | 当前 Agent 执行状态 |
| `agent_id` | string | 当前执行的 agent |
| `checkpoint_id` | string | 当前 HITL checkpoint（nullable） |
| `agent_last_activity` | timestamp | 最后活动时间 |

### 原因

1. **客户端 UI 驱动**：客户端可根据 agent_status 显示不同 UI（如 "正在思考"、"等待回答"）
2. **Pull-on-Notification 基础**：客户端拉取 Conversation 时获取最新状态（D-118）
3. **状态查询**：支持查询"哪些会话正在等待用户回答"等业务场景
4. **与 D-087 互补**：D-087 的 ephemeral agent_status 用于实时推送，本决策的持久化状态用于拉取查询

### 实现要点

- Agent 执行开始时设置 `agent_status = thinking`
- 调用工具时更新为 `tool_calling`
- 流式输出时更新为 `generating`
- `ask_user` 时更新为 `asking_user`，创建 Question 记录（D-116）
- 执行完成时更新为 `idle`
- 超时或错误时更新为 `timeout` 或 `idle`

### 约束

- `agent_status` 更新应通过数据库事务保证一致性
- 状态转换应记录日志，便于调试
- 超时检测可通过 `agent_last_activity` 字段实现

---

## D-118: Pull-on-Notification 模式

### 决策

Update 事件（UserUpdate）采用 **Pull-on-Notification** 模式：Update 只作为轻量通知（只含 `conversation_id`），客户端收到通知后阻塞拉取 Conversation 最新状态。

**核心原则**：

1. Update 事件是**通知**，不是**数据**
2. 客户端收到通知后**拉取** Conversation 获取最新状态
3. 不管离线多久，拉取到的永远是**此刻的真相**
4. Question 与 Conversation 是**一对多**关系（D-116）

### 原因

1. **离线恢复**：离线客户端上线后，通过 sync_updates 拉取 conversation update，再拉取 Conversation 获取最新状态（包括 pending Questions）
2. **多设备同步**：Device A 回答问题后，Device B 收到 conversation update，拉取最新状态后关闭弹窗
3. **弱网竞态安全**：Device B 如果也回答了同一个 Question，服务端检查 Question.status 返回 409，B 静默关闭弹窗
4. **简化协议**：Update 只含轻量通知，不包含完整状态，减少带宽和复杂度

### 实现要点

- `ask_user` 触发时：
  1. 创建 Question 记录（D-116）
  2. 更新 Conversation.agent_status = `asking_user`（D-117）
  3. 广播 `{type: "conversation", conv_id: C1}` UserUpdate
- 客户端收到 conversation update 后：
  1. 调用 `get_conversation` 拉取完整 Conversation
  2. 检查 `agent_status` 和 `questions` 列表
  3. 如果有 pending questions，弹窗显示
  4. 如果 questions 都已 answered，关闭弹窗
- 离线客户端上线后：
  1. 调用 `sync_updates` 拉取增量 updates
  2. 对每个 conversation update，拉取 Conversation 最新状态
  3. 根据当前状态决定 UI 行为

### 约束

- Update 事件的 payload 只含 `conversation_id`，不含完整 Conversation 数据
- 客户端必须实现"拉取最新状态"逻辑，不能依赖 Update payload
- 与 D-028（UserUpdate 类型字段）兼容：`type: "conversation"` 是已有类型
- 与 D-045（create_conversation 实时通知）一致：同样使用 conversation update 通知

---

## D-123: HITL 超时自动清理

### 决策

后台 goroutine 定期扫描处于 `asking_user` 状态的 Conversation，清理超过 24h（默认超时阈值）未响应的 HITL 会话。清理步骤：

1. ClearAgentStatus：释放会话锁，将 `agent_status` 重置为 `idle`（D-117 状态机）
2. DeleteByCheckpoint：软删除关联的 Question 记录（D-116）
3. DEL checkpoint from Redis：删除 Eino checkpoint（D-112）
4. 发送用户友好的超时错误消息：持久化消息到会话中（D-067/D-082 模式）
5. 广播 `agent_timeout` ephemeral 通知（D-087）

使用 Redis SETNX 分布式锁防止多节点重复处理。所有清理步骤均为 non-fatal，失败仅记录日志。

### 原因

1. **避免会话永久卡死**：未被回答的 HITL 会话不应无限占用会话锁（D-075）
2. **与 D-016 模式一致**：D-016（UserUpdate 清理）已有后台 goroutine 定期清理模式
3. **与 D-122 模式一致**：D-122（Resume 永久失败清理）已定义清理步骤，超时清理复用相同步骤
4. **用户体验**：发送超时消息告知用户 HITL 已过期，需重新发起

### 实现

- 后台 goroutine 使用 `time.NewTicker`，默认 5 分钟间隔
- 查询条件：`agent_status = 'asking_user' AND agent_last_activity < NOW() - timeout`
- Redis SETNX 分布式锁：`hitl:cleanup:{conversationID}`，per-conversation 粒度，TTL 覆盖单次清理周期
- 清理函数复用 D-122 的 `cleanupAfterResumeFailure` 模式（所有操作 non-fatal）
- 超时消息文本："抱歉，等待时间过长，会话已超时。请重新发送消息。"（与 D-082 checkpoint 过期消息一致）
- 通过 `HITLCleanupConfig` 结构体配置（零值填充默认值）

```go
// 配置选项（零值填充默认值）
type HITLCleanupConfig struct {
    Interval  time.Duration // 清理间隔，默认 5 分钟
    MaxAge    time.Duration // 超时阈值，默认 24h
    BatchSize int           // 单次清理最大会话数，默认 100
    LockTTL   time.Duration // 分布式锁 TTL，默认 30 秒
}
```

### 约束

- 仅清理 `agent_status = 'asking_user'` 且 `agent_last_activity` 超过超时阈值的会话
- 所有清理步骤 non-fatal，失败仅记录日志（与 D-122 一致）
- 超时消息使用 D-067/D-082 模式持久化中文消息
- 状态转换：清理后 `agent_status` 从 `asking_user` 转为 `idle`（而非 `timeout`），简化状态机
- 超时阈值默认 24h，与 D-084（HITL 锁 TTL 24h + buffer）一致

---

## D-124: Conversation 同步优化（updated_at 广播）

### 决策

Conversation 的 `updated_at` 字段在每次状态变更时由数据库自动更新（GORM `UpdatedAt`）。`SendConversationUpdate` 广播时在 payload 中包含 `updated_at`（Unix 秒级时间戳）。客户端收到 conversation update 通知后，比较 `payload.updated_at` 与本地缓存的 `updated_at`：若 payload 时间戳小于等于本地则跳过 `get_conversation` RPC，若大于本地或本地无缓存则执行拉取。

服务端变更：`UpdateAgentStatus` / `ClearAgentStatus` 返回 `(time.Time, error)` 供调用方获取最新时间戳。

### 原因

1. **减少不必要的 RPC**：同一 Conversation 的状态可能频繁更新（如 agent_status 从 thinking → tool_calling → generating），每次 update 都触发 `get_conversation` 拉取是浪费。通过 `updated_at` 比较，客户端可以判断本地缓存是否已是最新
2. **与 D-118 互补**：D-118 定义了 Pull-on-Notification 模式（Update 事件为轻量通知，客户端拉取 Conversation 获取最新状态）。D-124 在此基础上优化拉取决策——不是每次都拉，而是基于时间戳判断是否需要拉
3. **向后兼容**：旧客户端忽略 `updated_at` 字段，仍按 D-118 模式执行拉取；旧服务端不提供 `updated_at` 时，客户端执行 RPC

### 实现

```go
// 数据库层：Conversation.updated_at 由 GORM 自动维护（UpdatedAt）

// 广播层：SendConversationUpdate payload 新增 updated_at
type ConversationUpdatePayload struct {
    ConversationID string `json:"conversation_id"`
    UpdatedAt      int64  `json:"updated_at,omitempty"` // Unix 秒级时间戳，omitempty 向后兼容
}

// 服务端：UpdateAgentStatus / ClearAgentStatus 返回最新时间戳
func (s *ConversationStore) UpdateAgentStatus(ctx context.Context, convID string, status string) (time.Time, error) {
    result := s.db.WithContext(ctx).
        Model(&model.Conversation{}).
        Where("id = ?", convID).
        Update("agent_status", status)
    // 查询更新后的 updated_at
    var conv model.Conversation
    s.db.WithContext(ctx).Select("updated_at").Where("id = ?", convID).First(&conv)
    return conv.UpdatedAt, result.Error
}

// 客户端：比较 updated_at 决定是否拉取
func (c *SyncManager) handleConversationUpdate(payload *ConversationUpdatePayload) {
    if payload.UpdatedAt > 0 {
        localConv := c.conversationCache.Get(payload.ConversationID)
        if localConv != nil && payload.UpdatedAt <= localConv.UpdatedAt.Unix() {
            return // 本地已是最新（小于等于），跳过拉取
        }
    }
    // 执行 get_conversation RPC
    c.fetchConversation(payload.ConversationID)
}
```

### 约束

- `updated_at` 为 Unix 秒级时间戳（`int64`），使用 `omitempty` 向后兼容
- 旧客户端不识别 `updated_at` 字段时，按 D-118 模式执行拉取
- 旧服务端不提供 `updated_at` 时（值为 0 或缺失），客户端执行拉取
- `UpdateAgentStatus` / `ClearAgentStatus` 返回 `(time.Time, error)`，调用方将 `time.Time` 转为 Unix 秒级时间戳填入 payload
- 时间戳精度为秒级，同一秒内的多次更新可能产生相同时间戳（客户端缓存 `updated_at` 比较时仍能正确判断）

---

## 版本历史

| 日期       | 版本 | 变更                                                                                                 |
| ---------- | ---- | ---------------------------------------------------------------------------------------------------- |
| 2026-07-16 | v3.22 | D-125（移除冗余 HITL Ephemeral 事件 agent_question/agent_checkpoint_created）；更新 D-087（缩减为 2 种类型）、D-120（删除向后兼容约束） |
| 2026-07-16 | v3.21 | 新增 D-123（HITL 超时自动清理）、D-124（Conversation 同步优化 - updated_at 广播） |
| 2026-07-15 | v3.19 | 新增 D-116（Question 持久化表）、D-117（Conversation 状态机）、D-118（Pull-on-Notification 模式）；更新 D-085（partial answer + 幂等检查）、D-113（被 D-116 替代） |
| 2026-07-15 | v3.18 | 新增 D-115（Daemon 内置函数自动注册），删除 `register-functions` CLI 命令 |
| 2026-07-14 | v3.17 | 新增 D-112（Checkpoint 清理策略）、D-113（interruptIDs 内存存储）、D-114（agent-resume IPC-only）；更新 D-085（5 参数）、D-036（加入 agent-resume） |
| 2026-07-13 | v3.16 | 新增 D-110（E2E 测试 MQ 异步任务直接调用策略） |
| 2026-07-13 | v3.15 | Phase 5: 新增 D-107（Replay 请求 ID 格式）、D-108（system.reconnect RPC 规范）、D-109（补发并发与超时策略） |
| 2026-07-13 | v3.14 | Phase 4: 新增 D-103（ReverseRPC Pending Store）、D-104（幂等键与 Seq 协议扩展）、D-105（CancelDevice 不清理 Redis Pending）、D-106（Per-device Seq 计数器策略）；更新 D-092 约束、D-095 约束、D-074 约束 |
| 2026-07-12 | v3.13 | Phase 3: Send 反馈增强（Send 返回 error）+ 正常断连 fail pending ReverseRPC 请求（CancelDeviceWithReason）；更新 D-092 约束、D-100 补充 |
| 2026-07-12 | v3.12 | 新增 D-100（客户端工具错误返回 LLM）、D-101（接口定义在 agent 包）、D-102（DeviceID 通过 MQ payload 传播） |
| 2026-07-12 | v3.11 | 新增 D-098（system. 命名空间）、D-099（函数清单协议格式）                                            |
| 2026-07-12 | v3.10 | 新增 D-093..D-097（设备连接模型 + reqID UUID）                                                       |
| 2026-07-12 | v3.9 | 新增 D-092（ReverseRPC 双向请求能力）                                                                 |
| 2026-07-12 | v3.8 | 新增 D-091（Agent 输入边界定义）                                                                     |
| 2026-07-12 | v3.7 | 新增 D-088..D-090（真实 LLM 端到端测试：分离策略、环境变量、成本控制）                               |
| 2026-07-11 | v3.6 | 新增 D-078..D-087（Phase 8: 高级功能产品决策）                                                      |
| 2026-07-11 | v3.4 | 新增 D-070..D-074（Phase 5: AgentTaskHandler 产品决策）                                               |
| 2026-07-11 | v3.3 | 新增 D-064（LLM 默认 BaseURL）、D-065（Agent 思考状态）、D-066（LLMProvider 接口）、D-067（Agent 错误消息） |
| 2026-07-11 | v3.2 | 新增 D-062（Agent 消息路由触发模型）、D-063（AgentRegistry 可选注入）                                |
| 2026-07-11 | v3.1 | 新增 D-060（Agent 上下文管理策略）                                                        |
| 2026-07-11 | v3.0 | 新增 D-054（Agent UserID 命名约定）、D-055（Agent 消息格式复用）、D-058（Agent 配置格式）            |
| 2026-07-10 | v2.9 | 新增 D-051/D-052/D-053（流式文本 Ephemeral Push + 协作模型 + 广播所有成员），更新 D-036               |
| 2026-07-10 | v2.8 | 新增 D-050（Ephemeral Push 模式，Seq=0）                                                             |
| 2026-07-10 | v2.7 | 新增 D-048（测试环境变量命名规范）、D-049（弱网韧性测试策略）                   |
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
