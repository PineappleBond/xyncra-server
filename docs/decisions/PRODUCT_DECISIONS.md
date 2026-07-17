# Xyncra Server 产品决策文档

记录非常规的复杂架构决策、影响全局的约束、以及后续开发必须知晓的约定。显而易见的常识性设计不记录。

> 详细说明（含原因背景、约束条件）请参阅 [PRODUCT_DECISIONS_DETAILS.md](./PRODUCT_DECISIONS_DETAILS.md)

---

## 决策概览

| 编号 | 决策 | 原因 |
|------|------|------|
| D-002 | 认证由业务服务器负责，服务器本身不做鉴权 | 职责分离 |
| D-003 | 内网部署模型，通过反向代理暴露服务 | 安全边界外置 |
| D-006 | client_message_id 幂等性模型 | 客户端友好重试 |
| D-007 | MQ 入队失败 fire-and-forget | 数据优先，推送是增强 |
| D-010 | 被动 TTL 续期 | 仅 heartbeat 触发 TTL 续期，user SET 与 info key TTL 对齐（MAX 语义） |
| D-011 | create_conversation find-or-create 幂等模型 | 防止重复会话 |
| D-018 | 多节点消息路由，Redis Pub/Sub 实现跨节点推送 | 水平扩展能力 |
| D-028 | UserUpdate 类型字段 | 支持多种 Update 类型的分类处理和查询 |
| D-029 | sync_updates 补空策略 | 运行时生成 gap 占位 Update，不持久化 |
| D-030 | CLI 进程间通信协议 | Unix Socket + JSON-RPC 2.0 |
| D-032 | CLI IPC Fallback 策略 | IPC 优先，失败 fallback 到 WebSocket 短连接 |
| D-036 | 部分 CLI 命令为 IPC-only | sync-updates/agent-resume/reload-agents 等依赖 daemon 状态 |
| D-044 | listen daemon 连接韧性策略 | 无限重试 WS 连接（4001 设备替换除外），IPC 始终可用 |
| D-050 | Ephemeral Push 模式 (Seq=0) | typing/presence 等瞬时业务零持久化 |
| D-054 | Agent UserID 命名约定 | `agent/{id}` 格式，命名空间隔离 |
| D-055 | Agent 消息格式复用 | 不新增 Message 类型，复用现有协议 |
| D-060 | Agent 上下文管理策略 | DB 存储 + 内存缓存，Token 裁剪 |
| D-062 | Agent 消息路由触发模型 | 消息先持久化再异步入队 MQ，fire-and-forget |
| D-063 | AgentRegistry 可选注入（nil-safe） | Agent 功能为可选模块 |
| D-066 | LLMProvider 接口抽象 | 支持运行时注册新提供商 |
| D-067 | Agent 错误消息策略 | 失败时持久化错误消息 |
| D-071 | Agent 幂等性使用 Redis SETNX + 24h TTL | 零新依赖，分布式幂等性 |
| D-072 | Agent 幂等性 fail-open 策略 | Redis 不可用时跳过检查继续执行 |
| D-073 | AgentTaskHandler 总是返回 nil 给 MQ | 错误已转化为友好消息，防止重试风暴 |
| D-075 | Agent 会话级并发锁 | 保证同一会话串行处理 |
| D-076 | reload-agents 热加载 | IPC-only 命令，热加载 Agent 配置无需重启 daemon |
| D-077 | Agent 配置从磁盘目录加载 | 支持运行时热更新和 Docker 目录映射 |
| D-083 | HITL CheckpointStore 失败策略 | 非 fail-open：checkpoint 丢失不可恢复 |
| D-084 | HITL Resume 与并发锁协调 | HITL 中断期间保持会话锁 |
| D-091 | Agent 输入边界 | CLI 允许空消息通过，由 Agent 端处理错误消息持久化 |
| D-092 | ReverseRPC 双向请求能力 | 服务端向指定用户发起 RPC |
| D-093 | 连接模型扩展为 (userID, deviceID, connID) | 设备级定向发送 |
| D-095 | 设备替换策略 | 同设备新连接替换旧连接 |
| D-098 | `system.` 命名空间用于系统级 RPC 方法 | 系统方法与业务方法分离 |
| D-099 | 客户端函数清单使用 JSON Schema 描述参数 | 标准化函数清单格式 |
| D-103 | ReverseRPC Pending Store（Redis 持久化） | 超时请求持久化，支持重连补发 |
| D-108 | system.reconnect RPC 规范 | 客户端重连后自动补发超时请求 |
| D-111 | 客户端 4001 语义感知 | 收到 4001 不重连，daemon 优雅退出进程（清理 sock/lock 文件），退出码 0 |
| D-115 | Daemon 内置函数自动注册 | 消除独立进程的设备替换冲突 |
| D-116 | Question 持久化表（HITL 韧性） | 替代内存存储，支持多设备/并行 HITL |
| D-117 | Conversation 状态机 | agent_status 字段驱动客户端 UI |
| D-118 | Pull-on-Notification 模式 | Update 为轻量通知，客户端拉取最新状态 |
| D-121 | 两阶段幂等性 | processing key (130s) + processed key (24h) |
| D-123 | HITL 超时自动清理 | 后台定期清理卡在 asking_user 的会话 |
| D-124 | Conversation 同步优化（updated_at 广播） | 减少不必要的 get_conversation RPC |
| D-126 | 消息按需拉取（FetchMoreMessages） | 本地消息不足时从服务器拉取 |
| D-127 | 手动业务级追踪，而非自动基础设施追踪 | Jaeger Operation 列表保持高信噪比 |

---

## 相关文档

- [详细决策文档](./PRODUCT_DECISIONS_DETAILS.md)
- [API 文档](../API.md)
