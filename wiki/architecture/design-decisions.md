# 架构决策记录 (ADR)

本文档以 ADR（Architecture Decision Record）格式记录 Xyncra 项目中的关键架构决策。每个决策包含**背景**、**方案对比**、**决策**和**后果**。

---

## ADR-001：为什么 "先落库后处理"

### 背景

消息发送是即时通讯系统的核心路径。系统需要同时保证：
- **数据可靠性**：消息不丢失
- **实时性**：消息尽快到达接收方
- **解耦**：消息持久化与实时推送互不影响

### 方案对比

| 方案 | 描述 | 优势 | 劣势 |
|------|------|------|------|
| **先处理再落库** | MQ 推送成功后再写 DB | 推送成功后才持久化，减少无用写入 | MQ 故障导致消息丢失，DB 与推送强耦合 |
| **同时落库和处理** | 并行写 DB 和入队 MQ | 延迟最低 | 任一失败需要回滚/补偿，复杂度高 |
| **先落库再处理** | DB 事务提交后再入队 MQ | 数据安全，MQ 故障不影响 | 响应略慢（多一次事务时间） |

### 决策

选择 **先落库再处理**。

```
SendMessage:
    1. DB Transaction: INSERT message + INSERT user_updates + UPDATE conversation
    2. COMMIT
    3. Enqueue MQ task (fire-and-forget)
    4. Return response to sender
```

### 后果

- **正面**：MQ 不可用时消息不丢失，接收方通过 `sync_updates` 最终收到；Handler 与 MQ 解耦
- **负面**：发送者响应时间包含一次 DB 事务（通常 < 5ms）；MQ 任务可能延迟消费

**相关决策**：D-007（Fire-and-Forget）、D-062（Agent 消息路由同样遵循此模式）

---

## ADR-002：为什么选择 WebSocket（而非 HTTP/gRPC）

### 背景

Xyncra 是实时消息 + AI Agent 服务器，核心需求：
- **双向实时通信**：服务端主动推送消息/状态/Agent 流式输出
- **低延迟**：毫秒级消息送达
- **长连接状态**：设备在线状态管理

### 方案对比

| 方案 | 双向通信 | 延迟 | 状态管理 | 编码开销 |
|------|----------|------|----------|----------|
| **WebSocket** | 原生支持 | 低 | 长连接天然带状态 | 中等 |
| **HTTP Long Polling** | 模拟（Client → Server 单向） | 高 | 无状态，需额外存储 | 低 |
| **HTTP SSE** | 仅 Server → Client | 中 | 无状态 | 低 |
| **gRPC (双向流)** | 原生支持 | 低 | 流分片带状态 | 高（protobuf codegen） |
| **gRPC-Web** | 有限支持 | 中 | 需代理层 | 高 |

### 决策

选择 **WebSocket**（`gorilla/websocket`）。

```
URL: ws://server/ws?user_id={userID}&device_id={deviceID}
Protocol: JSON over WebSocket TextMessage
```

关键考量：
1. **双向实时性**：服务端推送 Updates、发起 ReverseRPC，WebSocket 原生支持
2. **简单性**：JSON 协议，无需 proto 编译和 codegen 流程
3. **Agent 流式输出**：stream_text Update 通过 WebSocket 实时推送
4. **生态成熟**：`gorilla/websocket` 是 Go 社区最成熟的 WS 库

### 后果

- **正面**：协议简单可调试（JSON 肉眼可读）；客户端 SDK 支持任何 WebSocket 兼容的语言
- **负面**：JSON 编码开销高于 protobuf；需要自行处理连接管理/重连/心跳

**扩展**：gRPC 在反向代理层使用（Nginx 支持 gRPC），但不作为客户端通信协议。

---

## ADR-003：为什么使用 Eino 作为 Agent 框架

### 背景

Xyncra 需要 AI Agent 能力：
- 多 LLM 提供商支持（OpenAI/Claude/Qwen/Ollama）
- 流式生成（SSE-like → WebSocket 桥接）
- Tool Calling（客户端注册的函数作为 Agent 工具）
- HITL（Human-in-the-Loop）中断恢复
- 对话上下文管理

### 方案对比

| 方案 | 多 LLM | 流式 | Tool Calling | HITL | Go 生态 |
|------|--------|------|-------------|------|---------|
| **Eino** (CloudWeGo) | ✓ | ✓ | ✓ | ✓（Checkpoint） | 原生 Go |
| **LangChain Go** | ✓ | 有限 | ✓ | ✗ | 社区维护 |
| **自己实现** | 需自研 | 需自研 | 需自研 | 需自研 | - |
| **Python LangChain** | ✓ | ✓ | ✓ | ✓ | 跨语言调用 |

### 决策

选择 **CloudWeGo Eino**（`github.com/cloudwego/eino`）。

```
Agent Pipeline:
    Eino Graph/Chain
        ├── ChatModel (OpenAI/Claude/Ollama/Qwen via LLMProvider)
        ├── Tools (client functions via DynamicToolProvider)
        └── Checkpoint (HITL resume via CheckpointStore)
```

关键考量：
1. **Go 原生**：与 Go 技术栈一致，避免 Python 跨语言调用
2. **完整功能**：Graph/Chain 编排、流式生成、Tool Calling、Checkpoint、Callback
3. **ADK 支持**：`eino/adk` 提供 Agent 开发工具包
4. **可扩展**：LLMProvider 接口支持运行时注册新模型提供商（D-066）

### 后果

- **正面**：Agent 处理管道在单个 Go 进程中完成，无跨进程开销；Eino 的 Graph 编排支持复杂 Agent 工作流
- **负面**：Eino 社区相对较新，文档和示例较少；需依赖 eino-ext 扩展模块

**相关**：D-066（LLMProvider 接口）、D-077（磁盘配置加载）、D-083（HITL CheckpointStore）

---

## ADR-004：为什么客户端使用本地 SQLite 数据库

### 背景

Xyncra 客户端需要：
- 离线消息读取（断网时仍可查看历史消息）
- 减少网络请求（读操作不依赖网络）
- 重试队列持久化（进程重启后继续重试）

### 方案对比

| 方案 | 离线可用 | 读延迟 | 实现复杂度 | 数据一致性 |
|------|----------|--------|----------|------------|
| **本地 SQLite (GORM)** | ✓ | 低（本地读取） | 中等 | 最终一致（sync pull） |
| **纯内存缓存** | ✗ | 最低 | 低 | 进程重启丢失 |
| **仅远程读取** | ✗ | 高（依赖网络） | 最低 | 强一致 |
| **SQLite + 远程回退** | ✓ | 低 | 中 | 最终一致 |

### 决策

选择 **本地 SQLite + 按需远程拉取**。

```
读操作：
    ListConversations → 本地 DB
    GetMessages → 本地 DB（D-035）
    GetConversation → 本地 DB
    SearchMessages → 本地 DB
    
写操作：
    SendMessage → RPC → 服务器（不能本地写入）
    
补充机制：
    FetchMoreMessages → RPC 拉取 → Upsert 本地 DB（D-126）
    FullSync → RPC 批量拉取 → 写入本地 DB
    Pull-on-Notification → 通知 → 按需 RPC 拉取（D-118）
```

### 后果

- **正面**：读操作毫秒级响应、离线可查看、重试队列进程级持久化
- **负面**：数据最终一致性（从服务器写入到客户端拉取之间的延迟）；本地 DB 存储占用

**数据一致性保证**：
1. 写操作总是通过 RPC 到服务器（强一致）
2. 服务器通过 Push + Pull 分发到其他设备（最终一致）
3. 同一设备通过 sync_updates 拉取自己的写操作结果（先收到 response，后收到 push）

---

## ADR-005：为什么消息 ID 使用单调递增而非 UUID

### 背景

消息需要具有有序性和可比较性。客户端需要按消息顺序渲染、分页查询。

### 方案对比

| 方案 | 有序性 | 并发安全 | 依赖 |
|------|--------|----------|------|
| **自增 MessageID** (uint32) | 按插入顺序递增 | 事务内分配（D-008） | conversation.LastProcessedMessageID |
| **UUID** | 无序 | 无冲突 | 无 |
| **UUIDv7（时间排序）** | 按时间排序 | 无冲突 | 新标准，支持有限 |

### 决策

选择 **单调递增 uint32 MessageID**（`conversation.LastProcessedMessageID + 1`）。

```
// store.go:SendMessage
msg.MessageID = conv.LastProcessedMessageID + 1
```

关键设计：
- `MessageID` 在 DB 事务内分配（D-008）
- 每次消息发送时读取 `conversation.LastProcessedMessageID`，加 1 后写入
- 同事务中更新 `conversation.last_processed_message_id`

### 后果

- **正面**：消息天然有序，分页查询简单高效（`after_message_id` 即可）
- **负面**：消息 ID 分配依赖 DB 事务，扩缩容需要妥善处理（当前使用共享 DB，无此问题）

---

## ADR-006：Seq = 0 瞬时通道设计

### 背景

打字指示、流式文本、Agent 状态等业务：
- **不可靠可容忍**：丢失一帧打字指示不影响功能
- **不需要持久化**：写入 DB 无意义（打字状态秒级过期）
- **不需要拉取**：断线重连后不再需要历史打字状态

### 方案对比

| 方案 | 持久化 | 存储开销 | 实现复杂度 |
|------|--------|----------|----------|
| **Seq = 0 特殊标记** | 否 | 无 | 低（检查 seq 即可区分） |
| **独立更新类型通道** | 否 | 无 | 中（需新推送通道） |
| **统一持久化** | 是 | 高（大量无意义写入） | 低 |

### 决策

选择 **Seq = 0 特殊标记**（D-050）。

```
// 服务端发送
update = {seq: 0, type: "typing", payload: {...}}
// 或
update = {seq: 0, type: "streaming", payload: {...}}
// 或
update = {seq: 0, type: "agent_status", payload: {...}}

// 客户端处理（sync.go:ApplyUpdate）
if update.Seq == 0:
    // 跳过持久化、跳过序列检查、跳过 NotificationLog
    handler.OnTyping / OnStreaming / OnAgentStatus / OnAgentTimeout
    return nil
```

### 后果

- **正面**：零存储开销、零拉取开销、代码改动最小化
- **负面**：断线期间瞬时事件完全丢失（可接受业务损失）

**相关决策**：
- D-050：Ephemeral Push 模式
- D-087：Agent 瞬时 Update 类型
- D-124：Conversation update 使用 Seq=0 触发 pull-on-notification

---

## ADR-007：设备替换策略（4001 Close Frame）

### 背景

同一用户在多个设备登录，或同一设备重新连接时，需要正确处理连接替换。

### 方案对比

| 方案 | 冲突检测 | 旧连接处理 | 复杂度 |
|------|----------|----------|--------|
| **4001 Close Frame** | device_id 精确匹配 | 发送 4001 → 客户端不重连 | 中等 |
| **并发连接** | 同设备允许多连接 | 无操作 | 低 |
| **服务端踢下线** | device_id 匹配 | 直接 Close | 低 |

### 决策

选择 **4001 Close Frame 优雅替换**（D-093/D-095）。

```
New Connection:
    1. CancelDevice (fail pending reverse-RPC for old device)
    2. HTTP Upgrade
    3. Register new connection in clientsByDevice
    4. Asynchronously: clean up old connections
       a. Write 4001 Close Frame
       b. 10ms pause (TCP buffer flush)
       c. Close old client
       d. Wait for pump goroutines (500ms timeout)
       e. removeClient

Old Client (receives 4001):
    readPump detects 4001 → cm.replaced = true
    handleDisconnect → onDisconnect(replaced=true)
    connectionMonitor: if replaced → cancel context → graceful exit (exit 0)
```

### 后果

- **正面**：设备精确替换、干净退出（清理 sock/lock 文件）
- **负面**：额外的 TCP flush 延时（10ms）、顺序性要求严格

---

## ADR-008：HITL 中断恢复模型

### 背景

Agent 在执行过程中可能需要向用户提问（Human-in-the-Loop），用户回复后需要从断点恢复 Agent 执行。

### 决策摘要

Question 持久化 + Agent Checkpoint 恢复。

详细设计参见 `docs/design/DESIGN_HITL_RESILIENCE.md`。

```
Agent 询问用户（HITL 中断）:
    1. Eino Checkpoint → CheckpointStore (Redis)
    2. Question → QuestionStore (DB)
    3. Conversation status → "asking_user"

用户回复:
    1. send_message → 服务器
    2. 服务器识别为 HITL 回复 → enqueue mq:agent_resume
    3. MQ Consumer → ResumeHandler
       a. 获取会话级锁
       b. 读取 Checkpoint
       c. 恢复 Eino Graph 执行
       d. 清理 Checkpoint（D-112）
       e. 释放会话锁

HITL 超时清理（D-123）:
    cleanup goroutine:
        SELECT conversations WHERE agent_status = "asking_user" AND updated_at < now - 30min
        FOR EACH: 发送超时通知，重置状态
```

**相关决策**：
- D-083：HITL CheckpointStore 非 fail-open
- D-084：HITL Resume 与并发锁协调
- D-112：Checkpoint 删除策略
- D-113：会话状态机（agent_status 字段）
- D-116：Question 持久化表
- D-123：HITL 超时自动清理

---

## ADR-009：多节点消息路由策略

### 背景

水平扩展时，消息需要从生产者节点路由到消费者节点的目标设备。

### 方案对比

| 方案 | 实现 | 延迟 | 复杂度 |
|------|------|------|--------|
| **Redis Pub/Sub** | NodeBroadcaster 接口 | 低 | 低 |
| **MQ 消息路由** | 通过 Asynq 队列转发 | 中 | 中 |
| **gRPC 节点间通信** | 节点间 gRPC 流 | 低 | 高 |

### 决策

选择 **Redis Pub/Sub** + **NoopBroadcaster 默认实现**（D-018）。

```
单节点部署：
    NodeBroadcaster = NoopBroadcaster (no-op)

多节点部署：
    NodeBroadcaster = RedisNodeBroadcaster
    
    广播流程：
        1. broadcastLocal: 直接推送本地连接
        2. nodeBroadcaster.Publish: Redis PUBLISH xyncra:broadcast:{userID}
        3. 其他节点 SUBSCRIBE → handleRemoteBroadcast
        4. 源节点 ID 跳过（sourceNodeID == s.nodeID → skip）
```

### 后果

- **正面**：单节点零开销、多节点自动启用；Redis 是已有依赖，无新增基础设施
- **负面**：Pub/Sub 消息不持久化（节点宕机丢失推送消息，数据通过 sync_updates 最终一致）

---

## ADR-010：接口隔离和可选模块

### 背景

系统功能模块化需要清晰的接口边界，部分功能（Agent、FunctionRegistry）是可选的，不能因为可选模块的缺失影响核心功能。

### 决策

```
接口设计原则：
    1. 所有模块通过接口依赖（ServerDeps / StoreAPI / Broker / AgentRegistry）
    2. 可选模块始终 nil-safe（D-063）
    3. 接口尽量小（Interface Segregation Principle）

具体实现：
    AgentRegistry: 可 nil，nil 时跳过 Agent 检测
    FunctionRegistry: 可 nil，nil 时不注册 system.register_functions
    ReverseRPC.PendingStore: 可 nil，nil 时不持久化超时请求
    QuestionStore: 可 nil，nil 时不持久化 HITL 问题
    NodeBroadcaster: 默认为 NoopBroadcaster（D-018）
    CheckpointStore: 可 nil，nil 时 HITL 恢复不可用
```

**Go 编译时检查**：

```go
var _ StoreAPI = (*Store)(nil)      // 确保 Store 实现接口
var _ Server = (*WebSocketServer)(nil) // 确保 WebSocketServer 实现接口
var _ NodeBroadcaster = (*NoopBroadcaster)(nil)
```

### 后果

- **正面**：模块可独立测试、可选功能零配置启动、核心功能不依赖可选模块
- **负面**：接口增多，部分代码路径需要 nil 检查

---

## ADR-011：幂等性模型分层

### 背景

系统涉及多个层面的幂等性保证：消息发送、RPC 调用、Agent 处理、反向 RPC。

### 决策

| 层面 | 机制 | 范围 | 说明 |
|------|------|------|------|
| 消息发送 | DB 唯一索引 `(client_message_id, sender_id)` | 全局 | TOCTOU-safe，事务内去重（D-006） |
| 客户端 RPC | 重试队列 + `pending[reqID]` | 客户端 | 内存去重 + 本地持久化 |
| 客户端反向 RPC | `IdempotencyCache`（LRU 缓存） | 客户端 | 内存去重 |
| Agent 处理 | Redis SETNX `agent:idempotency:{msgID}` | 全局 | 两阶段失效（D-121）：processing (130s) + processed (24h) |
| Agent 幂等性 | fail-open（D-072） | 全局 | Redis 不可用时跳过检查 |
| 反向 RPC 超时 | `IdempotencyKey`（= reqID） | 全局 | 客户端缓存去重（D-107） |

```
// DB 唯一索引（消息幂等）
CREATE UNIQUE INDEX idx_msg_client_id_sender ON messages(client_message_id, sender_id);

// Agent 幂等性（两阶段，D-121）
SET agent:idempotency:{msgID} "processing" EX 130 NX  // 第一阶段
SET agent:idempotency:{msgID} "processed" EX 86400     // 第二阶段
```

---

## ADR-012：Conversation 状态机

### 背景

会话在不同阶段有不同的状态：正常对话、Agent 正在思考、等待用户回复（HITL）等。状态需要持久化并在客户端同步。

### 决策

会话使用 `agent_status` 字段驱动 UI（D-117）。

```
状态机：
    idle ──→ thinking ──→ tool_calling ──→ generating ──→ idle
                            │                            │
                            └── asking_user ──────────────┘
                                         │
                                    user回复 (agent_resume)
                                         │
                                    thinking → ... → idle
```

状态通过 Seq=0 的 `agent_status` Update 实时推送，客户端通过 `get_conversation` 拉取最新状态。

**相关**：D-124（conversation update 优化）、D-125（Question 同步）

---

## 决策索引

| ADR | 决策 | 相关产品决策 |
|-----|------|-------------|
| ADR-001 | 先落库后处理 | D-006, D-007, D-062 |
| ADR-002 | WebSocket 协议 | D-002, D-005 |
| ADR-003 | Eino Agent 框架 | D-063, D-066, D-077 |
| ADR-004 | 本地 SQLite 客户端 | D-035, D-126 |
| ADR-005 | 单调递增消息 ID | D-008 |
| ADR-006 | Seq=0 瞬时通道 | D-050, D-087 |
| ADR-007 | 设备替换 4001 | D-093, D-095, D-111 |
| ADR-008 | HITL 中断恢复 | D-083 ~ D-085, D-112 ~ D-125 |
| ADR-009 | 多节点 Redis Pub/Sub | D-018 |
| ADR-010 | 接口隔离与可选模块 | D-063 |
| ADR-011 | 分层幂等性 | D-006, D-071, D-072, D-121 |
| ADR-012 | 会话状态机 | D-117, D-124, D-125 |
