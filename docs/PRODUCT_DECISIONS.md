# Xyncra Server 产品决策文档

本文档记录了 Xyncra 消息系统服务器的核心架构决策概览。所有开发者和子代理在实现功能时必须遵守这些决策。

> 详细说明（实现细节、代码示例、约束条件）请参阅 [PRODUCT_DECISIONS_DETAILS.md](./PRODUCT_DECISIONS_DETAILS.md)

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
| D-036 | 部分 CLI 命令为 IPC-only | sync-updates（状态一致性）、set-typing/stream-text（瞬时操作，daemon 离线无意义）、agent-resume（HITL 状态在 daemon 进程） |
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
| D-048 | 测试环境变量使用 XYNCRA_TEST_ 前缀 | 仅测试环境读取，生产代码不读取 |
| D-049 | 弱网韧性测试策略 | 内联 mock WS server，不依赖外部工具 |
| D-050 | Ephemeral Push 模式 (Seq=0) | typing/presence 等瞬时业务不持久化、不入 MQ、离线不投递 |
| D-051 | 流式文本 Ephemeral Push | stream_text RPC 使用累积文本模式，每帧完整快照，丢帧不影响正确性 |
| D-052 | stream_text 与 send_message 协作 | 流式结束后两步协议：先 broadcast is_done，再 send_message 持久化 |
| D-053 | （已合并入 D-050） | Ephemeral 广播给所有成员已纳入 D-050 核心约束 |
| D-054 | Agent UserID 命名约定 | `agent/{id}` 格式，命名空间隔离 |
| D-055 | Agent 消息格式复用 | 不新增 Message 类型，复用现有协议 |
| D-058 | Agent 配置格式 | YAML Front Matter + Markdown body 单文件格式 |
| D-060 | Agent 上下文管理策略 | DB 存储 + 内存缓存，Token 裁剪优先，消息数 fallback |
| D-062 | Agent 消息路由触发模型 | 消息先持久化再异步入队 MQ，fire-and-forget |
| D-063 | AgentRegistry 可选注入 | nil-safe 设计，Agent 功能为可选模块 |
| D-064 | LLM 提供商默认 BaseURL 映射 | 减少配置负担，开箱即用 |
| D-065 | Agent 思考状态展示 | typing indicator 提升用户感知响应速度 |
| D-066 | LLMProvider 接口抽象 | 支持运行时注册新提供商，扩展性 |
| D-067 | Agent 错误消息策略 | 失败时持久化错误消息，避免用户困惑 |
| D-070 | AgentTaskHandler 放置于 `internal/agent/` 包 | Task handler 直接依赖 agent 包内部组件，避免反向依赖 |
| D-071 | Agent 幂等性使用 Redis SETNX + 24h TTL | 零新依赖，原子操作适合分布式幂等性（参见 D-121 两阶段演进） |
| D-072 | Agent 幂等性 fail-open 策略 | Redis 不可用时跳过检查继续执行 |
| D-073 | AgentTaskHandler MQ 返回策略 | 永久错误返回 nil（D-067），transient 错误返回 error 允许 Asynq 重试（参见 D-121） |
| D-074 | Agent 幂等性使用独立 redis.Client | Pub/Sub 连接不能共享，独立客户端允许独立配置 |
| D-075 | Agent 会话级并发锁（Per-Conversation Lock） | Redis SETNX 分布式锁，保证同一会话串行处理 |
| D-076 | reload_agents RPC 管理接口 | 无鉴权热更新 Agent 配置，内网部署模型 |
| D-077 | Agent 配置从磁盘目录加载 | 删除 go:embed，支持运行时热更新和 Docker 目录映射 |
| D-078 | Agent 自定义工具注册表 | 代码注册 + 配置引用，未知工具名跳过（fail-open） |
| D-079 | Agent Middleware 配置格式 | YAML front matter 中 `middleware` 段，可选启用 |
| D-080 | 工具结果截取存储策略 | 内存存储（sync.Map + TTL），不持久化到消息表 |
| D-081 | Sub-agent 声明方式 | 父 Agent YAML 中 `sub_agents` 引用已注册 Agent ID |
| D-082 | Agent 错误消息扩展分类 | 扩展 D-067 覆盖工具/MCP/子Agent/HITL/中间件失败 |
| D-083 | HITL CheckpointStore 失败策略 | 非 fail-open：checkpoint 失败时中止 HITL 并报错 |
| D-084 | HITL Resume 与并发锁协调 | HITL 中断期间保持会话锁，防止新任务冲突 |
| D-085 | agent_resume RPC 规范 | 新 RPC + MQ task type，复用现有锁和幂等机制；支持 partial answer，幂等检查 Question.status |
| D-086 | MCP Server 配置格式 | YAML `mcp_servers` 段，支持 SSE 和 stdio 传输 |
| D-087 | Agent Ephemeral Update 类型扩展 | 新增 agent_status/agent_question/agent_checkpoint_created/agent_timeout（Seq=0） |
| D-088 | 真实 LLM 测试分离 | 构建标签 `real_llm` + 环境变量双重门控，mock 测试与真实 LLM 测试分离 |
| D-089 | 真实 LLM 测试环境变量 | `XYNCRA_TEST_` 前缀，`.env.test` 存储，配置模板可提交 |
| D-090 | 真实 LLM 测试成本控制 | 14 个核心场景、最便宜模型、短对话、构建标签防意外运行 |
| D-091 | Agent 输入边界定义 | 标准化 Agent 系统对各种极端输入的处理行为 |
| D-092 | ReverseRPC 双向请求能力 | 服务端可向指定用户发起 RPC 请求并等待响应，用于 HITL 等场景 |
| D-093 | 连接模型扩展为 (userID, deviceID, connID) | 设备级定向发送，Agent Tool 基础设施 |
| D-094 | 空 device_id 自动生成 UUID | 向后兼容，零配置迁移 |
| D-095 | 设备替换策略：Close Frame 4001 | 同设备多连接时新替旧，pending 请求立即 fail |
| D-096 | ReverseRPC sendFunc 签名扩展 | 空 deviceID=广播，非空=定向发送 |
| D-097 | reqID 格式：UUID 替代原子计数器 | 避免服务器重启后 ID 冲突 |
| D-098 | `system.` 命名空间用于系统级 RPC 方法 | 系统方法与业务方法分离，职责清晰 |
| D-099 | 客户端函数清单使用 JSON Schema 描述参数 | 标准化函数清单格式，客户端和服务端一致理解 |
| D-100 | 客户端工具错误返回给 LLM 自主处理 | LLM 灵活决策，与 D-067 互补而非替代 |
| D-101 | ClientFunctionProvider/ClientCaller 接口定义在 agent 包 | 避免循环依赖，与 D-070 一致 |
| D-102 | DeviceID 通过 MQ payload 传播到 Agent context | 与 D-062 一致，向后兼容 |
| D-103 | ReverseRPC Pending Store（Redis 持久化） | 超时请求持久化到 Redis，支持重连补发，fail-open |
| D-104 | ReverseRPC 幂等键与 Seq 协议扩展 | PackageDataRequest 新增 IdempotencyKey + Seq，omitempty 向后兼容 |
| D-105 | CancelDevice 不清理 Redis Pending | 调用方立即 fail（D-095），但数据保留待 Phase 5 补发 |
| D-106 | Per-device Seq 计数器策略 | Phase 4 内存计数器，Phase 5 可升级为 Redis INCR |
| D-107 | Replay 请求 ID 使用 `s-replay-{uuid}` 格式 | 扩展 D-097 命名空间，日志中区分原始请求与补发请求 |
| D-108 | system.reconnect RPC 规范 | 客户端重连后自动补发断连期间的超时请求，fail-open |
| D-109 | 补发并发与超时策略 | 每请求一 goroutine，10s 超时，超过 MaxRetries 放弃 |
| D-110 | E2E 测试 MQ 异步任务直接调用策略 | 测试应控制其依赖，MQ 可靠性非 E2E 测试目标 |
| D-111 | 客户端 4001 语义感知 | 收到 4001 不重连，daemon 休眠，IPC 保持可用 |
| D-112 | Checkpoint 清理策略 | resume 成功后立即 DEL，TTL 24h 安全网，Delete 失败不阻塞 |
| D-113 | ~~interruptIDs 内存存储策略~~ → 已被 D-116 替代 | Question 表持久化替代 sync.Map，interrupt_id 从 DB 查询 |
| D-114 | agent-resume 为 IPC-only 命令 | HITL 状态在 daemon 进程，不提供 WebSocket fallback |
| D-115 | Daemon 内置函数自动注册 | 消除 register-functions 独立进程与 daemon 的设备替换冲突，单连接架构 |
| D-116 | Question 持久化表（HITL 韧性） | 新增 Question 表，answer 先写 DB 再入队 MQ，task payload 只含 checkpoint_id |
| D-117 | Conversation 状态机 | 新增 agent_status 字段（idle/thinking/tool_calling/generating/asking_user/timeout） |
| D-118 | Pull-on-Notification 模式 | Update 事件为轻量通知（只含 conversation_id），客户端拉取 Conversation 获取最新状态 |
| D-121 | 两阶段幂等性 (Two-Phase Idempotency) | processing key (130s) 防并发 + processed key (24h) 防回放，崩溃后可重试 |
| D-122 | Resume 永久失败清理策略 | ClearAgentStatus + DeleteByCheckpoint + checkpoint DEL，避免 conversation 卡在 asking_user |

---

## 相关文档

- [详细决策文档](./PRODUCT_DECISIONS_DETAILS.md) - 包含所有决策的实现细节、代码示例和约束条件
- [API 文档](./API.md) - WebSocket 协议说明

---

## 版本历史

| 日期       | 版本 | 变更                                                                                                 |
| ---------- | ---- | ---------------------------------------------------------------------------------------------------- |
| 2026-07-16 | v3.20 | 新增 D-121（两阶段幂等性）、D-122（Resume 永久失败清理策略） |
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

---

## D-116: HITL Question 持久化模型

### 决策

HITL 问题持久化为独立的 `Question` 实体，与 `Conversation` 是一对多关系。Question 包含以下字段：

- `id` (UUID): 主键
- `conversation_id` (string): FK → Conversation
- `checkpoint_id` (string): 关联 Eino checkpoint
- `interrupt_id` (string): Eino interrupt address ID
- `question_text` (text): 问题内容
- `status` (enum): `pending` | `answered`
- `answer` (text): 用户回答（nullable）
- `answered_by` (string): 回答者 user_id
- `answered_device_id` (string): 回答设备
- `created_at` (timestamp): 创建时间
- `answered_at` (timestamp): 回答时间（nullable）

### 原因

1. **离线用户支持**: 用户离线时 Question 持久化到 DB，上线后可通过 `sync_updates` 或 `get_conversation` 拉取
2. **多设备竞态保护**: Question 级别幂等，`UpdateAnswer` 使用 `WHERE status = 'pending'` 条件，已回答返回 409
3. **服务器重启韧性**: Answer 先写 DB，MQ payload 不含 answer，重启后答案不丢失
4. **部分 answer 支持**: 多个 Question 可逐个回答，全部 answered 后才入队 resume task

### 实现

- `internal/store/model/question.go`: Question 模型
- `internal/store/question.go`: QuestionStore（Create, GetByConversation, GetByCheckpoint, UpdateAnswer, CountPendingByCheckpoint, DeleteByCheckpoint）
- AutoMigrate 注册 Question 模型

### 约束

- Question 使用软删除（GORM DeletedAt）
- Question 的生命周期：创建 → pending → answered → resume 后删除

---

## D-117: Answer 先写 DB 策略

### 决策

`agent_resume` RPC handler 先将用户回答写入 Question 表（`status = 'answered'`），然后检查是否所有 Questions 都已回答。全部回答才入队 `TypeAgentResume` MQ 任务。MQ task payload **不含 answer**，只含 `checkpoint_id`。

### 原因

1. **服务器重启韧性**: Answer 在 DB 中，重启后不丢失
2. **部分 answer 支持**: 用户可逐个回答多个 Question
3. **MQ 重试安全**: MQ task 重试时从 DB 读取 answer，不会丢失或重复

### 实现

- `internal/handler/agent_resume.go`: RPC handler
- `QuestionStore.UpdateAnswer`: 更新 answer 和 status
- `QuestionStore.CountPendingByCheckpoint`: 检查是否全部回答

### 约束

- MQ payload 不含 answer，只含 checkpoint_id
- 全部 answered 后才入队 resume task
- 部分 answered 时返回 `{status: "partial", answered: N, total: M}`

---

## D-118: Question 级别幂等保护

### 决策

`agent_resume` RPC 使用 Question 级别幂等保护。`UpdateAnswer` 使用 `WHERE status = 'pending'` 条件更新。已回答的 Question 返回 `ErrConflict`（HTTP 409）。

### 原因

1. **多设备竞态**: Device A 回答后，Device B 收到 409
2. **幂等性**: 同一设备重复回答也返回 409
3. **客户端友好**: 客户端收到 409 后静默关闭弹窗

### 实现

```go
// QuestionStore.UpdateAnswer
result := qs.db.WithContext(ctx).
    Model(&model.Question{}).
    Where("id = ? AND status = ?", questionID, model.QuestionStatusPending).
    Updates(map[string]any{...})
if result.RowsAffected == 0 {
    return ErrConflict // 409
}
```

### 约束

- 409 响应的错误码为 `-409`，消息为 `"question_already_answered"`
- 客户端应静默处理 409

---

## D-119: Resume 从 DB 读取 Questions

### 决策

Resume MQ handler 从 DB 读取 Questions 构建 `Targets` map（`interrupt_id → answer`），不再使用 `interruptIDs sync.Map`（D-113 已移除）。

### 原因

1. **移除 interruptIDs sync.Map**: 改为从 DB 查询
2. **服务器重启韧性**: Questions 在 DB 中，重启后仍可读取
3. **多轮 HITL 支持**: 每轮 HITL 的 Questions 独立，通过 `checkpoint_id` 区分

### 实现

```go
questions, err := executor.store.QuestionStore().GetByCheckpoint(ctx, payload.CheckpointID)
targets := make(map[string]any)
for _, q := range questions {
    if q.Status == model.QuestionStatusAnswered && q.InterruptID != "" {
        targets[q.InterruptID] = q.Answer
    }
}
```

### 约束

- 只构建 `status = 'answered'` 的 Questions 的 Targets
- 如果没有 answered Questions，resume 失败

---

## D-120: HITL Pull-on-Notification 模式

### 决策

HITL 使用 Pull-on-Notification 模式：Agent 中断时广播轻量 conversation update（只含 `conversation_id`），客户端拉取 Conversation 获取完整状态（包括 Questions）。

### 原因

1. **轻量通知**: 广播只含 `conversation_id`
2. **真相在 DB**: 客户端拉取 Conversation 时从 DB 读取最新 Questions
3. **离线用户支持**: 离线用户上线后通过 `sync_updates` 或 `get_conversation` 拉取

### 实现

- `BroadcastHelper.SendConversationUpdate`: 广播轻量 update
- `get_conversation` handler: 响应中包含 `questions` 列表
- 客户端: 收到 update 后拉取 Conversation，弹窗显示 Questions

### 约束

- `UpdateTypeConversation` 为 ephemeral（Seq=0）
- 保留 `agent_question` ephemeral 广播（向后兼容）

---

## D-121: 两阶段幂等性 (Two-Phase Idempotency)

### 决策

Agent 任务执行使用两阶段 Redis key 区分"执行中"和"已完成":

- `agent:processing:{messageID}` (TTL 130s): 执行前 SETNX，防止并发重复执行
- `agent:processed:{messageID}` (TTL 24h): 执行成功后 SET，防止回放重复

Resume task 同理:

- `agent:resume:processing:{checkpointID}` (TTL 130s)
- `agent:resume:{checkpointID}` (TTL 24h)

### 原因

1. **崩溃恢复**: processing key 130s 自然过期后，Asynq 重试可重新执行
2. **并发保护**: processing key 防止同一消息被并发处理
3. **回放保护**: processed key (24h) 防止已完成的消息被重复处理
4. **Fail-open** (D-072): Redis 不可用时跳过两阶段检查

### 实现

- `internal/agent/task_handler.go`: 步骤 5 两阶段检查 + 步骤 7 收尾标记
- `internal/agent/resume_handler.go`: 同样的两阶段模式
- `IdempotencyStore.CheckProcessed`: 只检查不设置（Redis EXISTS）
- `IdempotencyStore.DeleteKey`: 清理 processing key（Redis DEL）

### 约束

- Processing key TTL 与 conversation lock TTL (D-075) 一致 (130s)
- HITL interrupt: 不设置 processed key，让 processing key 自然过期
- Task handler transient error: 不设置 processed key，删除 processing key，返回 error 给 Asynq 自动重试
- Resume handler transient error: 不设置 processed key，删除 processing key，发送用户错误消息，返回 nil（用户需手动重试，因 HITL 场景用户已投入交互成本）

---

## D-122: Resume 永久失败清理策略

### 决策

Resume 永久失败时执行完整清理:

1. ClearAgentStatus (conversation → idle)
2. DeleteByCheckpoint (soft-delete Questions)
3. Delete checkpoint from Redis (D-112)
4. 发送用户友好的错误消息

### 原因

1. **避免永久卡死**: 清理后 conversation 不再卡在 `asking_user`
2. **数据一致性**: Questions 使用 GORM soft-delete，数据可恢复
3. **用户体验**: 发送错误消息告知用户重新发送

### 实现

- `internal/agent/resume_handler.go`: `cleanupAfterResumeFailure` 函数
- 所有操作 non-fatal，失败仅记日志

### 约束

- 仅限"不可恢复"的失败: checkpoint 过期、agent config not found、build 失败、DB 错误
- Transient errors (LLM timeout) 不触发清理 — Questions 保留供重试
- HITL re-interrupt 不触发清理 — conversation 进入新的 asking_user 周期，Questions 和 checkpoint 保留供后续 resume 使用
