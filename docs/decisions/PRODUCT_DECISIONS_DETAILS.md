# Xyncra Server 产品决策文档（详细版）

本文档包含 Xyncra Server 核心产品决策的详细说明。

> 决策概览请参阅 [PRODUCT_DECISIONS.md](./PRODUCT_DECISIONS.md)

---

## 相关文档

- [决策概览](./PRODUCT_DECISIONS.md) - 所有决策的快速参考表
- [API 文档](../API.md) - WebSocket 协议说明

---

## D-002: 认证由业务服务器负责

**决策**：Xyncra Server 不实现任何认证/鉴权机制，认证由部署方的业务服务器实现，通过反向代理传递认证后的用户信息。

**原因**：职责分离（Xyncra 专注消息推送）、灵活性（适应 JWT/OAuth/Session 等需求）、简化核心、安全性（业务方定制更可靠）。

---

## D-003: 内网部署模型

**决策**：Xyncra Server 部署在内网，通过业务服务器的反向代理对外暴露。

**原因**：安全边界外置（内网隔离攻击）、简化核心（不实现 TLS/CORS/Rate Limit）、专注消息推送、灵活部署（配合 Nginx/Envoy/Traefik 等）。

---

## D-006: client_message_id 幂等性模型

**决策**：send_message 使用客户端提供的 client_message_id 实现幂等性，数据库 uniqueIndex 保证唯一性。重复时返回已持久化的消息记录（静默命中），不报错。

**原因**：客户端只需生成 UUID 即可安全重试、简单可靠（数据库唯一约束是最终保证）、网络容错（断线重连后安全重发）。

---

## D-007: MQ 入队 fire-and-forget

**决策**：send_message 在数据库事务中持久化后，MQ 入队（实时推送）是异步操作，失败不导致 send_message 返回错误。离线用户通过 sync_updates 增量拉取。

**原因**：数据优先（持久化是第一优先级）、容错性（MQ 暂不可用时消息不丢失）、最终一致（离线用户通过增量同步恢复）。

---

## D-011: create_conversation 的 find-or-create 幂等模型

**决策**：create_conversation 先通过 GetByUsers 查询已有会话，存在则返回（duplicate=true），不存在则创建。幂等性由用户对唯一性保证，而非客户端幂等 key。

**原因**：简化客户端（无需额外幂等 key）、防止重复会话、幂等重试安全。与 D-006（client_message_id 幂等）机制不同。

---

## D-018: 多节点消息路由

**决策**：使用 Redis Pub/Sub 实现跨节点消息推送。每个节点订阅 xyncra:broadcast:* pattern，通过 SourceNodeID 避免源节点重复推送。

**原因**：水平扩展（多实例部署）、实时性（不依赖轮询）、简单可靠（复用现有 Redis 依赖）、签名兼容（BroadcastUpdates 签名不变）。

---

## D-028: UserUpdate 类型字段

**决策**：UserUpdate 新增 Type 字段，定义 5 种类型：message、delete_message、mark_read、conversation、gap。所有操作共享用户级 seq 空间。

**原因**：客户端分类处理、多设备同步、数据库可查询性、向后兼容。mark_as_read 仅为操作用户创建 UserUpdate（不暴露已读给对方）。

---

## D-029: sync_updates 补空策略

**决策**：sync_updates 保证返回的 updates 列表 seq 连续。数据库存在 seq 间隙时，运行时生成 type: "gap" 的空 Update 填充，不持久化到数据库。

**原因**：客户端简化（无需自行检测间隙）、零存储开销、确定性行为、与客户端防抖互补。

---

## D-030: CLI 进程间通信协议

**决策**：xyncra-client CLI 使用 Unix Socket + JSON-RPC 2.0 进行 IPC。Socket 路径为 ~/.xyncra/{user_id}/{device_id}/xyncra.sock，换行符分隔消息。

**原因**：标准协议（工具链丰富）、跨语言兼容、路径编码身份（自动路由到正确实例）、本地安全（权限 0600）。

---

## D-032: CLI IPC Fallback 策略

**决策**：CLI 命令优先通过 IPC 连接到守护进程。IPC 失败时（listen 未运行），自动 fallback 到 WebSocket 短连接模式执行操作后关闭。

**原因**：自动降级（用户无需手动切换）、守护进程模式优先（复用连接）、独立模式兜底（未运行时仍可执行一次性操作）。

---

## D-036: 部分 CLI 命令为 IPC-only

**决策**：sync-updates、set-typing、stream-text、agent-resume、reload-agents 仅通过 IPC 与守护进程交互，不提供 WebSocket fallback。守护进程未运行时返回错误。

**原因**：sync-updates 需守护进程状态一致性；set-typing/stream-text 是瞬时操作需复用 daemon 连接；agent-resume 依赖 daemon 内存中的 HITL 状态；reload-agents 操作 daemon 内 AgentRegistry。

---

## D-044: listen daemon 连接韧性

**决策**：守护进程在初始 WebSocket 连接失败时不退出，使用指数退避无限重试（4001 设备替换除外）。IPC 服务器在 WS 连接建立前已启动。仅在 context 取消或 kill 命令时退出。4001 Close Frame 不触发重连，daemon 优雅退出进程（见 D-111）。

**原因**：离线可用（服务器恢复后自动重连）、IPC 始终可用（本地查询独立于 WS）、零配置（不因临时网络问题退出）、4001 设备替换是无限重试的唯一例外（D-095 新替旧策略下重连无意义）。

---

## D-050: Ephemeral Push 模式 (Seq=0)

**决策**：瞬时数据（typing、在线状态、呼叫信令）使用 Seq=0 标识，不持久化、不分配 seq、不入 MQ、离线不投递、上线不补拉。直接通过 BroadcastUpdates 广播。

**原因**：与 D-007 互补（D-007 是数据重要但推送可失败，D-050 是数据和推送都可丢失）、协议复用（复用 PackageDataUpdates 信封）、扩展性（统一机制承载多种瞬时业务）。

---

## D-054: Agent UserID 命名约定

**决策**：Agent 使用 agent/{id} 格式的 UserID，agent/ 前缀为系统保留命名空间。Agent 在协议层与普通用户完全等价，不新增 User 类型或字段。

**原因**：命名空间隔离、协议复用（复用用户机制）、可识别性（前缀快速判断身份类型）、向后兼容。

---

## D-055: Agent 消息格式复用

**决策**：Agent 消息与普通用户消息格式完全相同，不新增 Message.Type 或 Package 类型。通过 agent/ 前缀的 UserID 标识。

**原因**：协议简洁性、复用现有基础设施（Store/MQ/广播全部复用）、客户端无需改动、与 D-054 配合。

---

## D-060: Agent 上下文管理

**决策**：Agent 上下文采用 DB 存储 + sync.Map 内存缓存（TTL 30s）。Token 裁剪优先，MaxMessages 为 fallback。使用 HeuristicTokenCounter（len/4），无外部依赖。

**原因**：持久化+缓存兼顾、Token 裁剪比消息数更精确（LLM 约束是 token 窗口）、消息数 fallback（无法估算 token 时）、启发式计数满足零配置。

---

## D-062: Agent 消息路由触发模型

**决策**：用户向 Agent 发消息时，消息正常持久化，然后通过 MQ 入队 TypeAgentProcess 异步任务触发 Agent 处理。仅当发送者是人类用户（非 agent/ 前缀）且接收者是已注册 Agent 时才触发。

**原因**：与 D-007（MQ fire-and-forget）一致、与 D-055 一致（路由在 MQ 层对协议透明）、防递归保护（Agent 回复不触发二次处理）、零侵入非 Agent 路径。

---

## D-063: AgentRegistry 可选注入（nil-safe）

**决策**：AgentRegistry 通过 Dependencies struct 注入 handler 层，允许为 nil。为 nil 时 agent 检测路径完全跳过。

**原因**：Agent 作为可选模块、向后兼容（传入 nil 保持现有行为）、测试友好（传 nil 禁用 agent 路径）、渐进引入。

---

## D-066: LLMProvider 接口抽象

**决策**：定义 LLMProvider 接口，LLMClientFactory 维护提供商注册表（map[LLMProvider]ProviderFactory），支持运行时注册新提供商。

**原因**：扩展性（新提供商只需实现接口并注册）、可测试性（注入 mock provider）、零配置添加新提供商。

---

## D-067: Agent 错误消息策略

**决策**：Agent 执行失败时，通过 send_message 持久化一条中文错误消息（SenderID 为 agent/{id}），复用现有消息格式。错误消息不触发新的 Agent 处理。

**原因**：用户感知（静默失败导致困惑）、可追溯（错误作为对话历史）、与 D-055 一致（复用现有消息格式）。

---

## D-071: Agent 幂等性使用 Redis SETNX

**决策**：Agent 任务幂等性使用 Redis SETNX 原子操作，key 格式 agent:processed:{messageID}，TTL 24 小时。

**原因**：零新依赖（Redis 是现有基础设施）、原子操作（无需额外锁）、24h TTL（覆盖 MQ 重试窗口）、与 D-006 互补（D-006 是客户端到服务器，D-071 是 MQ 层面）。

---

## D-072: Agent 幂等性 fail-open 策略

**决策**：Redis 不可用时跳过幂等性检查继续执行（fail-open），而非拒绝执行（fail-close）。

**原因**：幂等性是优化而非安全机制（重复执行不损坏数据）、阻塞比重复更糟糕（Redis 故障不应使 Agent 完全不可用）、与 D-007（MQ fire-and-forget）一致。

---

## D-073: AgentTaskHandler 总是返回 nil 给 MQ

**决策**：AgentTaskHandler 总是返回 nil 给 Asynq，即使执行失败。错误已由 ExecuteWithErrorMessage 转化为用户友好的错误消息。

**原因**：MQ 重试导致重复执行和重复错误消息、与 send_message MQ handler 模式一致（D-007 模型）。

---

## D-075: Agent 会话级并发锁

**决策**：Agent 执行使用 Redis SETNX 分布式锁，key agent:lock:{conversationID}，TTL 130s。同一会话同一时间只允许一个 Agent 任务执行。锁被占用时新任务跳过。Fail-open。

**原因**：上下文一致性（并行执行导致重复响应）、分布式安全（跨节点生效）、与 D-072 fail-open 一致。

---

## D-077: Agent 配置从磁盘目录加载

**决策**：Agent 配置文件从磁盘目录加载（默认 agents/），替代 go:embed 方案。支持 --agents-dir flag 覆盖路径，Reload() 重新扫描目录。

**原因**：热更新前提（go:embed 嵌入二进制后不可变）、Docker 部署友好（volume 映射）、开发效率（修改 prompt 后 reload 即可生效）、与 D-001 兼容。

---

## D-083: HITL CheckpointStore 失败策略

**决策**：CheckpointStore（Redis）在 checkpoint 保存时不可用，HITL 流程中止并持久化错误消息。这不属于 fail-open——HITL 无法在没有 checkpoint 的情况下工作。但 resume 的幂等性检查仍 fail-open。

**原因**：Checkpoint 丢失不可恢复、明确错误优于静默损坏。与 D-072 区分：幂等性跳过可接受，checkpoint 丢失不可恢复。

---

## D-084: HITL Resume 与并发锁协调

**决策**：Agent 遇到 HITL 中断时 per-conversation 锁不释放，锁 TTL 延长到 24h + buffer。agent_resume 任务复用同一锁。锁 TTL 过期后会话解锁。

**原因**：防止冲突（用户普通消息与 pending resume 冲突）、复用 D-075 锁机制、自然过期无需额外清理。

---

## D-092: ReverseRPC 双向请求能力

**决策**：服务端通过 ReverseRPC 向用户所有活跃连接发起 RPC 请求并同步等待响应。可选组件（nil-safe）。Request ID 使用 "s-" 前缀 + UUID。用户级广播，第一个到达的响应被接受，后续静默丢弃。

**原因**：HITL 基础设施需要服务端主动获取结构化响应、复用 WebSocket 双向通道、与 D-050 互补（不替代 ephemeral push）。超时后请求持久化到 Redis PendingStore（D-103）。

---

## D-093: 连接模型扩展为 (userID, deviceID, connID)

**决策**：连接管理索引从 (userID, connID) 扩展为 (userID, deviceID, connID)。新增 sendToDevice 定向方法。device_id 通过 WebSocket URL query parameter 传递。

**原因**：Agent Tool 基础设施需要定向调用发起会话的设备、向后兼容（空 device_id 自动生成 UUID）、无跨设备聚合（Agent 只调用发起设备）。

---

## D-095: 设备替换策略

**决策**：同 (userID, deviceID) 新连接到来时，先 Upgrade 新连接并原子注册，异步向旧连接发送 Close Frame（code: 4001）并清理。旧连接 pending ReverseRPC 立即 fail。

**原因**：防止消息重复投递（同设备多连接导致路由不确定）、快速失败（pending 请求立即 fail 不等超时）、确定性行为（新替旧无歧义）。

---

## D-098: system. 命名空间用于系统级 RPC

**决策**：引入 system. 前缀作为系统级 RPC 方法命名空间，与业务方法区分。系统方法用于元数据交换和能力声明，不参与业务逻辑。当前定义 system.register_functions。

**原因**：命名空间隔离（职责清晰）、可扩展性（未来可新增 system.reconnect 等）、向后兼容、语义明确。

---

## D-099: 客户端函数清单使用 JSON Schema

**决策**：system.register_functions 使用 JSON Schema (draft 7) 描述参数。函数包含 name、description、parameters、returns、tags、timeout_ms 字段。每设备最多 200 个函数。

**原因**：JSON Schema 标准化（工具链丰富）、向后兼容（可选字段渐进扩展）、Agent 友好（LLM 可直接理解）、客户端灵活（任意编程语言）。

---

## D-103: ReverseRPC Pending Store

**决策**：ServerRequest 超时（DeadlineExceeded）后请求异步持久化到 Redis。key 格式 pending:{userID}\x00{deviceID}，使用 Redis List。仅 DeadlineExceeded 触发持久化。Fail-open。

**原因**：超时后的请求数据不应丢失、零新依赖（复用现有 Redis）、Phase 5 补发基础设施。TTL 24h，每设备最多 50 条 pending。

---

## D-108: system.reconnect RPC 规范

**决策**：客户端重连后调用 system.reconnect 触发服务端补发断连期间超时的请求。参数 last_seen_seq，服务端从 PendingStore 查询并过滤 Seq > last_seen_seq 的请求，异步补发。Fail-open、Nil-safe、无鉴权。

**原因**：设备重连后自动恢复断连期间丢失的反向请求。Redis 错误仅记日志，PendingStore 不可用时跳过。

---

## D-111: 客户端 4001 语义感知

**决策**：客户端收到 Close Frame code 4001 时，标记为"被替换"，不触发重连。daemon 调用 `Stop()` 优雅退出进程。退出时通过 defer 链清理 lock file、socket file、DB。退出码 0。旧的休眠/唤醒机制（replacedWake channel）被移除。

**原因**：防止重连死循环（Docker 端口转发场景下的无限循环）、语义正确（4001 表示被替换应安静退出）、与 D-095 一致（新替旧策略下重连无意义）。

---

## D-115: Daemon 内置函数自动注册

**决策**：listen daemon 启动时自动注册内置函数（ping、get_device_info、get_time），不需独立 register-functions 进程。重连后自动重新注册。

**原因**：消除设备替换冲突（独立进程与 daemon 使用相同 deviceID 互相踢掉）、单连接架构（所有功能合并到一个 WS 连接）、开箱即用（不需额外启动第二个进程）。

---

## D-116: Question 持久化表（HITL 韧性）

**决策**：新增 Question 数据库表持久化 HITL 问题与答案，替代 D-113 的 sync.Map 内存方案。Answer 先写 DB，再入队 MQ。支持一个 checkpoint 对应多个 Question（1:N）。

**原因**：服务器重启韧性（答案持久化不丢失）、多设备竞态安全（Question.status 幂等检查）、并行 Sub-Agent HITL、Partial answer 逐个回答后自动 resume。

---

## D-117: Conversation 状态机

**决策**：Conversation 新增 agent_status 字段，定义 idle/thinking/tool_calling/generating/asking_user/timeout 六种状态，遵循有限状态机模型。

**原因**：客户端 UI 驱动（不同状态展示不同 UI）、Pull-on-Notification 基础、状态查询支持、与 D-087（ephemeral agent_status）互补。

---

## D-118: Pull-on-Notification 模式

**决策**：Update 事件只作为轻量通知（payload 只含 conversation_id），客户端收到后阻塞拉取 Conversation 最新状态。不管离线多久，拉取到的永远是此刻的真相。

**原因**：离线恢复、多设备同步（Device A 回答后 Device B 拉取最新状态关闭弹窗）、弱网竞态安全（多设备同时回答时幂等返回 409）、简化协议。

---

## D-121: 两阶段幂等性

**决策**：Agent 任务幂等性分两阶段：processing key（130s TTL）防并发，processed key（24h TTL）防回放。崩溃后可重试（processing key 过期），不会永久阻塞。

**原因**：单 key 模型无法同时满足"崩溃后可重试"和"防重复"。130s 覆盖超时窗口又保证崩溃后 key 自动过期，24h 防止 MQ 重试窗口内的回放。

---

## D-123: HITL 超时自动清理

**决策**：后台 goroutine 定期扫描 asking_user 状态的 Conversation，清理超过 24h 未响应的 HITL 会话。清理步骤：释放会话锁、软删除 Question、DEL Redis checkpoint、发送超时消息、广播 agent_timeout ephemeral。

**原因**：避免会话永久卡死（未回答的 HITL 不应无限占用会话锁）、与 D-016 模式一致（后台 goroutine 定期清理）。使用 Redis SETNX 防止多节点重复处理。

---

## D-124: Conversation 同步优化（updated_at 广播）

**决策**：SendConversationUpdate payload 包含 updated_at 时间戳。客户端收到通知后比较时间戳与本地缓存，若小于等于本地则跳过 get_conversation RPC。

**原因**：减少不必要的 RPC（状态频繁更新时不必每次都拉取）、与 D-118 互补（优化拉取决策）、向后兼容（旧客户端忽略此字段）。

---

## D-126: 消息按需拉取（FetchMoreMessages）

**决策**：FetchMoreMessages() 从服务器 RPC 拉取消息并 upsert 到本地 DB。与 D-118（Conversation 层面 Pull-on-Notification）互补——在消息层面提供按需拉取能力。Upsert 失败为 best-effort。

**原因**：消息层面的按需拉取、RPC 拉取后写入本地 DB 保证离线可用（与 D-035 本地优先架构一致）。

---

## 版本历史

| 日期       | 版本  | 变更                                                                                           |
| ---------- | ----- | ---------------------------------------------------------------------------------------------- |
| 2026-07-16 | v3.23 | D-121（两阶段幂等性）、D-036（新增 reload-agents IPC-only）、D-126（FetchMoreMessages）         |
| 2026-07-16 | v3.21 | 新增 D-123（HITL 超时自动清理）、D-124（Conversation 同步优化 - updated_at 广播）               |
| 2026-07-15 | v3.19 | 新增 D-116（Question 持久化表）、D-117（Conversation 状态机）、D-118（Pull-on-Notification 模式） |
| 2026-07-15 | v3.18 | 新增 D-115（Daemon 内置函数自动注册）                                                           |
| 2026-07-14 | v3.17 | D-036（新增 agent-resume IPC-only）                                                             |
| 2026-07-13 | v3.15 | 新增 D-108（system.reconnect RPC 规范）                                                         |
| 2026-07-13 | v3.14 | 新增 D-103（ReverseRPC Pending Store）；更新 D-092 约束、D-095 约束                              |
| 2026-07-12 | v3.13 | D-092 Send 反馈增强（Send 返回 error）+ 正常断连 fail pending ReverseRPC 请求                   |
| 2026-07-12 | v3.11 | 新增 D-098（system. 命名空间）、D-099（函数清单协议格式）                                       |
| 2026-07-12 | v3.10 | 新增 D-093、D-095（设备连接模型）                                                               |
| 2026-07-12 | v3.9  | 新增 D-092（ReverseRPC 双向请求能力）                                                            |
| 2026-07-11 | v3.4  | 新增 D-071（Agent 幂等性 Redis SETNX）、D-072（fail-open 策略）、D-073（返回 nil 给 MQ）        |
| 2026-07-11 | v3.3  | 新增 D-066（LLMProvider 接口）、D-067（Agent 错误消息）                                         |
| 2026-07-11 | v3.2  | 新增 D-062（Agent 消息路由触发模型）、D-063（AgentRegistry 可选注入）                           |
| 2026-07-11 | v3.1  | 新增 D-060（Agent 上下文管理策略）                                                               |
| 2026-07-11 | v3.0  | 新增 D-054（Agent UserID 命名约定）、D-055（Agent 消息格式复用）                                |
| 2026-07-10 | v2.9  | D-036（sync-updates IPC-only）                                                                   |
| 2026-07-10 | v2.8  | 新增 D-050（Ephemeral Push 模式，Seq=0）                                                        |
| 2026-07-09 | v2.5  | 新增 D-044（daemon 连接韧性策略）                                                                |
| 2026-07-09 | v2.2  | D-036（sync-updates IPC-only）                                                                   |
| 2026-07-09 | v2.1  | 新增 D-030（IPC 协议）、D-032（IPC Fallback 策略）                                              |
| 2026-07-08 | v2.0  | 新增 D-028（UserUpdate 类型字段）、D-029（sync_updates 补空策略）                               |
| 2026-07-08 | v1.8  | 新增 D-018（多节点消息路由架构）                                                                  |
| 2026-07-07 | v1.3  | 新增 D-011（create_conversation 幂等模型）                                                      |
| 2026-07-07 | v1.1  | 新增 D-006（幂等性模型）、D-007（MQ fire-and-forget）                                           |
| 2026-07-07 | v1.0  | 初始版本，记录核心架构决策 D-002、D-003                                                          |
