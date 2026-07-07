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

- [架构设计](./architecture.md) - 系统架构详解
- [部署指南](./deployment.md) - 生产环境部署说明
- [API 文档](./api.md) - WebSocket 协议说明

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

## 版本历史

| 日期       | 版本 | 变更                                                                                |
| ---------- | ---- | ----------------------------------------------------------------------------------- |
| 2026-07-07 | v1.3 | 新增 D-011（create_conversation 幂等模型）                                          |
| 2026-07-07 | v1.2 | 新增 D-009（sync_updates 分页模型）、D-010（被动续期策略）                          |
| 2026-07-07 | v1.1 | 新增 D-006（幂等性模型）、D-007（MQ fire-and-forget）、D-008（MessageID 分配策略） |
| 2026-07-07 | v1.0 | 初始版本，记录核心架构决策                                                          |
