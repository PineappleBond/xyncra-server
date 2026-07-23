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

## D-054: Agent 身份判定：Registry 精确匹配

**决策**：Agent 身份判定通过检查 userID 是否存在于 `AgentRegistry` 中（精确匹配），不再依赖任何命名前缀。Agent `.md` 配置文件的 `id` 字段使用完整 userID（如 `agent/weather-bot`），其中 `agent/` 为示例约定而非系统强制。Agent 在协议层与普通用户完全等价，不新增 User 类型或字段。

**原因**：精确匹配消除前缀硬编码、支持任意格式的 Agent ID（如 `my-project/bot-1`）、`agent/` 约定保留兼容性、向后兼容（现有 `agent/xxx` 配置无需修改）。

---

## D-055: Agent 消息格式复用

**决策**：Agent 消息与普通用户消息格式完全相同，不新增 Message.Type 或 Package 类型。Agent 通过注册在 `AgentRegistry` 中的 UserID 标识。

**原因**：协议简洁性、复用现有基础设施（Store/MQ/广播全部复用）、客户端无需改动、与 D-054 配合。

---

## D-060: Agent 上下文管理

**决策**：Agent 上下文采用 DB 存储 + sync.Map 内存缓存（TTL 30s）。Token 裁剪优先，MaxMessages 为 fallback。使用 HeuristicTokenCounter（len/4），无外部依赖。

**原因**：持久化+缓存兼顾、Token 裁剪比消息数更精确（LLM 约束是 token 窗口）、消息数 fallback（无法估算 token 时）、启发式计数满足零配置。

---

## D-062: Agent 消息路由触发模型

**决策**：用户向 Agent 发消息时，消息正常持久化，然后通过 MQ 入队 TypeAgentProcess 异步任务触发 Agent 处理。仅当发送者不在 AgentRegistry 中（即非 Agent 用户）且接收者是已注册 Agent 时才触发。

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

## D-092: ~~ReverseRPC 双向请求能力~~（已废弃）

**废弃原因**：被 D-137 RemoteCalling 统一模型替代。客户端函数调用从同步 RPC 改为 RemoteCalling 异步中断-恢复模式（D-140）。

---

## D-093: 连接模型扩展为 (userID, deviceID, connID)

**决策**：连接管理索引从 (userID, connID) 扩展为 (userID, deviceID, connID)。新增 sendToDevice 定向方法。device_id 通过 WebSocket URL query parameter 传递。

**原因**：Agent Tool 基础设施需要定向调用发起会话的设备、向后兼容（空 device_id 自动生成 UUID）、无跨设备聚合（Agent 只调用发起设备）。

---

## D-095: 设备替换策略

**决策**：同 (userID, deviceID) 新连接到来时，先 Upgrade 新连接并原子注册，异步向旧连接发送 Close Frame（code: 4001）并清理。

**修订**（D-140）：RemoteCalling 模式下设备替换无需特殊处理——设备重连后客户端会重新拉取 pending RemoteCalling，无需取消旧连接的 pending 请求。

---

## D-098: system. 命名空间用于系统级 RPC

**决策**：引入 system. 前缀作为系统级 RPC 方法命名空间，与业务方法区分。系统方法用于元数据交换和能力声明，不参与业务逻辑。当前定义 system.register_functions。

**原因**：命名空间隔离（职责清晰）、可扩展性（未来可新增 system.reconnect 等）、向后兼容、语义明确。

---

## D-099: 客户端函数清单使用 JSON Schema

**决策**：system.register_functions 使用 JSON Schema (draft 7) 描述参数。函数包含 name、description、parameters、returns、tags、timeout_ms 字段。每设备最多 200 个函数。

**原因**：JSON Schema 标准化（工具链丰富）、向后兼容（可选字段渐进扩展）、Agent 友好（LLM 可直接理解）、客户端灵活（任意编程语言）。

---

## D-103: ~~ReverseRPC Pending Store~~（已废弃）

**废弃原因**：随 ReverseRPC 一起删除（D-140）。RemoteCalling 使用数据库持久化，不再需要 Redis PendingStore。

---

## D-108: ~~system.reconnect RPC 规范~~（已废弃）

**废弃原因**：随 PendingStore 一起删除（D-140）。RemoteCalling 模式下客户端通过 `get_remote_callings` RPC 拉取 pending 记录，无需 reconnect 补发机制。

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

## D-127: 手动业务级追踪，而非自动基础设施追踪

**决策**：移除所有自动埋点库（otelgorm、redisotel、otelhttp），在 repository/store 方法层手动添加业务级 span。Jaeger Operation 列表只展示触发层操作（ws.*、mq.*、agent.*、system.*），DB/Redis 操作以子 span 形式呈现。

**约束条件**：

1. 所有新 public store/server 方法必须手动埋点
2. Span 命名遵循 `domain.entity.operation` 模式（如 `db.conversation.get`、`redis.connection.add`）
3. 禁止引入 otelgorm/redisotel/otelhttp 等自动埋点库
4. 接受 otelhttp 移除后 outbound HTTP 可见性降低的 trade-off（`agent.llm.call` span 已覆盖业务层信息）

**背景**：otelgorm/redisotel/otelhttp 产生大量低价值 span（`GORM query`、`get`、`ping`），严重污染 Jaeger Operation 列表，使开发者难以快速定位真正的业务操作。手动埋点虽然增加少量代码量，但保证 Operation 列表的信噪比。

---

## D-128: /metrics 端点在同一 HTTP 端口暴露

**决策**：通过 `WSWithExtraRoutes` 功能选项将 `/metrics` 注册到现有 HTTP mux（端口 8080），而非独立管理端口。

**理由：**

- 符合 D-003 内网部署模型（反向代理已控制访问）
- 简化部署（少一个端口配置）
- server 包不依赖 Prometheus（通过 Route 抽象解耦）
- 与 `/health` 端点模式一致

**备选方案（已拒绝）：** 独立管理端口（如 `:9090`）— 增加配置复杂度，内网模型下无安全收益。

**实现：** 在 `internal/server/websocket_server.go` 中新增 `WSWithExtraRoutes` 选项，允许从外部注册额外的 HTTP 路由到服务器的 HTTP mux。`Route` 结构体定义在 server 包中，仅包含 `Pattern string` 和 `Handler http.Handler`，不引入 Prometheus 依赖。

---

## D-134: 双层函数注册策略：页面级 + 通用级 fallback

**决策**：采用双层函数注册策略。通用函数（click_element、type_text 等 20 个）始终常驻注册，作为 fallback 兜底。页面专用函数（pg_ 前缀）在对应页面组件 mount 时按需注册，unmount 时自动注销。

**原因**：
- 精准性：页面函数使用预计算 CSS 选择器，零歧义操作特定元素
- 资源高效：只注册当前页面需要的函数，不浪费 device 函数配额
- 自动生命周期：利用 React useEffect cleanup 机制，组件卸载时自动注销
- 安全降级：当页面结构变化导致 pg_ 函数失效时，Agent 自动回退到通用函数

**权衡与陷阱**：
- 页面切换时存在短暂的函数列表变更窗口（旧页面注销 → 新页面注册），Agent 可能拿到过时的函数列表。通过 `get_current_page` 确认页面后再调用 pg_ 函数可缓解此问题。
- 通用函数作为 fallback 保留了动态选择器生成的不确定性，但这是必要的安全网。
- pg_ 函数的 CSS 选择器是静态硬编码的，Ant Design 版本升级可能导致选择器失效，需要周期性审计更新。

---

## D-135: 每设备最大函数数上调至 500

**决策**：`DefaultMaxFunctionsPerDevice` 从 200 上调至 500。对应环境变量 `XYNCRA_MAX_FUNCTIONS_PER_DEVICE` 默认值同步更新。

**原因**：
- 全量页面适配预计产生 230+ 页面专用函数（28 页面 × 平均 8 个交互元素）
- 加上 20 个常驻通用函数，总计约 250+ 函数
- 200 上限不足，但上调至 500 为未来扩展预留充足空间
- D-134 的双层策略保证同一时刻只注册当前页面的 pg_ 函数，理论上不需要 500 上限，但宽松上限避免 Agent 探测阶段的临时函数注册导致注册失败

**权衡与陷阱**：
- 上调上限增加服务器端注册请求的大小，但 JSON 序列化开销可忽略（500 个函数约 50KB）
- 不影响 `MaxFunctionNameLength`（保持 255 字符）
- 不影响协议层，`FunctionInfo` 结构体不变
- 已部署的服务需要更新环境变量才能利用新的默认值

---

## D-136: 测试辅助函数统一接口（已实现）

**决策**：采用声明式注册模式 `defineTestHelpers(pageKey, helpers)` 一行代码完成组件注册、函数暴露、window 挂载、页面函数生成。替代原有三步样板（registerComponent + useXxxFunctions + defineExpose）。

**实现细节**：

- `defineTestHelpers` composable 在 `<script setup>` 顶层同步调用
- 每个 helper 声明包含 name、description、parameters（JSON Schema）、handler
- 自动生成 `pg_<pageKey下划线化>_<helperName>` 格式的页面函数
- 挂载到 `window.XyncraTestHelpers[pageKey][helperName]`（嵌套结构）
- 组件访问器扩展为同时存储 proxy 和 helpers 映射，callComponentMethod 先 proxy 后 helpers

**原因**：

- 降低样板代码（一行替代三步）
- 统一 Agent 和 Playwright 的调用接口
- 消除 DOM 版 test-helpers 的脆弱选择器
- 显式声明 meta 提高 Agent 可读性

**权衡与陷阱**：

- 迁移需同时改 view + 删 functions 文件，非原子，分批迁移期允许新旧并存
- `defineExpose` 无法在 composable 内调用，helpers map 改为运行时存于 `component-accessor`
- `window.XyncraTestHelpers` 由扁平改为嵌套，属外部行为变更
- `getCurrentInstance` 时序约束：必须在 setup 顶层同步调用
- 全量迁移 89 个页面组件，删除 27 个 functions 文件（保留 general.ts）

---

## D-140: ReverseRPC 废弃，客户端函数调用统一为 RemoteCalling

**决策**：废弃 D-092（ReverseRPC）、D-103（PendingStore）、D-108（system.reconnect）。客户端函数调用从同步 RPC 改为 RemoteCalling 异步中断-恢复模式。

**实现**：
- 客户端函数 tool 使用 `tool.Interrupt(ctx, interruptData)` 触发中断（与 `ask_user` 一致）
- executor 中断处理器解析 interruptData JSON，区分 HITL（method="ask_user"）和客户端函数调用
- 客户端函数中断设置 `agent_status="tool_calling"`（而非 `asking_user`）
- RemoteCalling 记录包含 method、params（原始 JSON）、device_id、timeout_ms
- resume 时 tool 通过 `tool.GetResumeContext[string](ctx)` 检测并直接返回客户端响应结果
- cleanup 任务同时覆盖 `asking_user` 和 `tool_calling` 两种状态

**原因**：
- 统一所有远程调用为单一模型（D-137）
- 消除 ReverseRPC 同步阻塞（异步模式更灵活）
- 简化基础设施（删除 PendingStore、system.reconnect）
- 与 HITL 流程一致（相同的中断-恢复模式）

**删除的代码**：
- `internal/server/reverse_rpc.go`、`reverse_rpc_test.go`
- `internal/server/pending_store.go`、`redis_pending_store.go`、`redis_pending_store_test.go`
- `internal/handler/reconnect.go`、`reconnect_test.go`
- `ClientCaller` 接口、`sendToUser`/`sendToDevice` 方法
- `ErrDeviceOffline`、`WSWithPendingStore`、`ReverseRPC()`、`ServerRequest()`

---

## D-141: Tool Calling 消息持久化策略

**决策**：Agent 调用工具时，将 tool calling 的输入参数和输出结果持久化到 messages 表，前端可查看完整的 tool 执行历史。

**设计背景**：
- 原有实现：tool calling 信息仅通过 ephemeral 广播（Seq=0）实时推送给在线用户，不进入 sync_updates 通道
- 问题：前端刷新页面后看不到 tool calling 执行历史，离线用户上线后无法获取记录
- 修订 D-050：tool_calling 执行结果具有独立记录价值，属于 persistent 范畴
- 修订 D-055：tool_calling 生命周期与 text 有本质差异，允许新增 Message 类型

**实现细节**：

1. **消息模型**：
   - Message.Type = "tool_calling"（新增类型）
   - Message.Content = JSON 序列化的 ToolCallingPayload
   - ToolCallingPayload 字段：name, args, status (executing|completed|failed), result, error, duration_ms

2. **生命周期**：
   - 执行前：创建 Message (status=executing)，调用 store.SendMessage 持久化
   - 执行后：事务内更新 Message (status=completed/failed)，创建 UserUpdate fan-out
   - 消耗 MessageID，推进 conversation 序列（与 text 消息一致）

3. **截断策略**：
   - args: 2048 字符
   - result: 4096 字符
   - error: 2048 字符
   - 与现有 truncate 行为一致，防止大 payload

4. **降级策略（D-063 nil-safe）**：
   - context 未注入 StoreAPI 时，降级为原有 ephemeral broadcast
   - SendMessage 失败时记录错误日志，工具仍正常执行（fire-and-forget, D-007）
   - UpdateMessageContentTx 失败时工具结果不丢失（已返回给 Agent）

5. **客户端同步**：
   - 客户端使用 put 语义（upsert）替代 add（insert-only）
   - Go 客户端：CreateOrUpdateTx 方法
   - 前端：Dexie put() 替代 add()

6. **RemoteCalling 关联**：
   - RemoteCalling 新增 MessageID 可选字段（D-141）
   - 初始不自动回填历史数据，后续迭代

**约束条件**：

- UserUpdate.Type 使用 "message"（复用现有 sync_updates 通道）
- UserUpdate.Payload 是 marshal 后的完整 Message 对象
- 所有 DB 操作在同一事务内（UpdateMessageContentTx + UserUpdate fan-out）
- 同一会话的多个 tool call 由 Eino 串行执行，无需并发控制

**影响范围**：

- 后端：internal/store/message.go, internal/agent/llm_logger.go, internal/agent/executor.go
- 客户端 SDK：pkg/store/message_store.go, pkg/client/sync.go
- 前端：sync-manager.ts (add→put), ToolCallingMessage.vue (新增组件)

---

## 版本历史

| 日期       | 版本  | 变更                                                                                           |
| ---------- | ----- | ---------------------------------------------------------------------------------------------- |
| 2026-07-22 | v3.29 | D-140（ReverseRPC 废弃，客户端函数统一为 RemoteCalling）；D-092/D-103/D-108 标记废弃；修订 D-095、D-123 |
| 2026-07-20 | v3.28 | D-136 已实现声明式注册模式 defineTestHelpers，全量迁移 89 个页面组件                              |
| 2026-07-19 | v3.27 | D-054 更新为 Registry 精确匹配模型、D-055/D-062 去除前缀依赖描述                                |
| 2026-07-17 | v3.26 | D-128（/metrics 端点在同一 HTTP 端口暴露，WSWithExtraRoutes）                                   |
| 2026-07-17 | v3.25 | D-127（手动业务级追踪，移除自动埋点库）                                                         |
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
