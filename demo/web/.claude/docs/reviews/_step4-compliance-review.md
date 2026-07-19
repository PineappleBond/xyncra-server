# 步骤 6 合规性审查 — 子代理 4（产品经理）

**子代理**：4（产品经理）
**项目根**：/Users/leichujun/go/src/github.com/PineappleBond/xyncra-server
**审查输入**：_step2-architecture-plan.md（架构方案）、docs/decisions/PRODUCT_DECISIONS.md
**方法**：逐决策 D-117/118/124/126/130/131/132 合规核对 + DX/UX 影响评估 + 新增决策建议

---

## 6.1 逐决策合规核对表

| 决策 | 架构方案结论 | 合规判定 | 说明 |
|------|--------------|----------|------|
| **D-117** Conversation 状态机（agent_status 驱动 UI） | 未触碰 agent_status 流转；仅修正连接状态机 `syncing`/`connected` | ✅ 一致 | D-117 规范的是 conversation 业务状态机（asking_user 等），连接状态机属 D-130 范畴。二者正交：D-130 改回 `syncing` 不涉及 conversation agent_status，无冲突。架构方案维持 AgentSelector/useAgentStatus 现有逻辑正确。 |
| **D-118** Pull-on-Notification | 无影响 | ✅ 无影响 | 架构方案未改动通知/拉取流程，CON-1/2 等修复均不涉及 D-118 的"轻量通知→客户端拉取"路径。注明：无影响。 |
| **D-124** Conversation 同步优化（updated_at 广播） | 无影响 | ✅ 无影响 | 未改动 broadcast/conversation 同步逻辑，方案仅修连接兜底与测试。注明：无影响。 |
| **D-126** 消息按需拉取（FetchMoreMessages） | 无影响 | ✅ 无影响 | 未触碰 FetchMoreMessages。注明：无影响。 |
| **D-130** Web 连接状态机（2s 空库兜底显示 syncing） | **改**：`connected`→`syncing`（P0） | ✅ 完全一致 | 见 §6.1.1 专项裁定。 |
| **D-131** deviceInfo 自动填充 | EXP-3 不修（Web 不适用 builtin 函数） | ✅ 兼容 | EXP-3 结论是"Web 不注册 builtin 函数（含 get_device_info）"，而 D-131 要求的是 Web 自动填充 `deviceInfo:{platform:'web',userAgent}`——这是连接握手时由 XyncraProvider 自动注入的元数据，与"是否注册 get_device_info 函数"是两回事。架构方案未取消自动填充，故与 D-131 兼容，注明：不修 EXP-3 不影响 D-131。 |
| **D-132** error:rpc 分层（暴露 {method,message,code}） | EXP-1 不修（不新增 useErrorRpc） | ✅ 一致 | 见 §6.1.2 专项裁定。 |

### 6.1.1 D-130 专项裁定（关键）

**D-130 原文**：`idle→connecting→syncing→connected→disconnected(+reconnecting)，2s 空库兜底显示 syncing 而非假 connected`

**架构方案**：XyncraProvider.tsx:313-317 将 2s 空库兜底 `setConnectionStatus('connected')` 改为 `setConnectionStatus('syncing')`，并保留"首数据到达"作为唯一转 `connected` 信号（行 295-307）。

**裁定**：方案与 D-130 原始描述**逐字一致**——"2s 空库兜底显示 syncing 而非假 connected"被直接落实。
- `ConnectionStatus.tsx:23` STATUS_MAP 已含 `syncing:{processing,'同步中...'}`，改回后零新增未处理分支，UI 安全。
- 状态流转闭合：connecting →（首数据）connected /（无数据 2s）syncing（D-130 语义：已握手但空库）。
- **结论：无需改动 D-130 文档描述**。076 报告的"BUG-4"实为对 D-130 的偏离，本轮 P0 修复即回归 D-130，属根因修复。

### 6.1.2 D-132 与 EXP-1 专项裁定

**D-132 目标**：`error:rpc` 事件暴露 `{method,message,code}` 公共通道，UI 映射友好提示，不泄露内部堆栈。

**现状核实**（架构方案 §3.4）：`XyncraProvider.tsx:218` 内部 emit `error:rpc` 并携带 `{method,message,code}`，经 antd `messageApi.error` 友好展示。`ErrorBoundary` 是 React render 级崩溃边界，与 `error:rpc`（RPC 失败通道）职责不重叠。

**裁定**：当前 `error:rpc` 事件**已满足 D-132 的"暴露 {method,message,code} 公共通道 + UI 友好提示"目标**。EXP-1 不新增 `useErrorRpc` 仅意味着"暂无编程式订阅 hook"，但不影响 D-132 已达成——D-132 未要求提供 hook，仅要求通道与结构。故 EXP-1 不修与 D-132 完全兼容。

---

## 6.2 DX/UX 影响评估

### 6.2.1 D-130 改 `syncing` 的 UI 用户感知
- **正向**：消除"空库却显示已连接"的假象，连接状态与真实数据到达对齐，用户可区分"已握手但无数据"与"已就绪"。
- **误导风险（需提示）**：若用户空库且永不收发消息，状态**长期停留 `syncing`**（显示"同步中..."）。这虽符合 D-130 语义，但 UX 上可能被解读为"卡在同步"。
  - **缓解建议（非阻塞）**：可在 `syncing` 状态下补充副文案（如"同步中…（空会话）"）或超时阈值（如 `syncing` 持续 > N 秒且无任何数据，降级为中性提示）。属体验增强，不强制进本轮修复集。
  - 权衡：不改 D-130，仅 UI 文案层优化，避免引入新状态破坏最小状态机。

### 6.2.2 CON-1/2 维持现状的 UI 影响
- **CON-1（不修）**：`agent:thinking` 对所有 userId 统一显示"思考/输入中"。已知副作用——**真实用户 typing 也会被渲染为 agent 思考指示**。在 Web 单用户对话场景下影响有限（多数会话仅有 agent 活动）；若未来引入多人类用户同屏，会误显。当前维持属合理（事件契约重构成本 > 收益）。
- **CON-2（改=维持 hitl:question）**：HITL 超时复用 `hitl:question` 触发恢复 UI，与 075 已验证链路一致，UX 上用户看到的是统一的"需要回答"对话框，无感知断裂。维持正确，无负面影响。

---

## 6.3 新增/更新产品决策建议

> 标准：仅"非常规复杂架构 / 影响全局 / 改变外部行为"才记 D 编号。常识性内容不记。

### 建议 1（更新，非新增）：D-130 是否需要更新描述？
**裁定：无需更新。**
D-130 原文字面即"2s 空库兜底显示 syncing 而非假 connected"，与代码历史无歧义——076 的 BUG-4 是**实现偏离决策**，非决策本身歧义。更新描述反而会模糊"实现违反已写明决策"的事实。建议在 D-130 详情（PRODUCT_DECISIONS_DETAILS.md）补一条"实现备注：076 曾偏离此决策以 connected 兜底，已回归"，属注释级，不升级编号。

### 建议 2（新增编号）：CON-2 维持 `hitl:question` 映射——反向固化决策
**裁定：建议新增 D 编号（固化"Web 端 Agent 超时复用 hitl:question 事件"为正式契约）。**

理由：CON-2 选择"维持 `hitl:question` 映射，不拆独立 `agent:timeout` 事件"是一个**改变外部行为契约**的非常规决策（与 CLI 的 `agent_timeout` 文本分支形成结构性差异，且依赖 075 的 useHITL 回查兜底）。若不固化为决策，未来重构者可能误以为"应拆分为独立事件"而破坏 075 已验证的 HITL 恢复链路（回归风险 P0）。符合"影响全局 / 改变外部行为"标准。

**建议编号**：D-133（接 D-132 之后）
**草稿文本**：

> **D-133** Web 端 Agent 超时复用 hitl:question 事件
> Web 客户端 `ReactUpdateHandler.onAgentTimeout` 将 Agent 超时（服务端 reason=`hitl_timeout`）映射为 `hitl:question` 事件而非独立 `agent:timeout` 事件，以复用 075 已验证的 HITL 恢复链路（`useHITL` 据 questionId/checkpointId 恢复对话框）。与 CLI 的 `agent_timeout` 文本分支为合理差异（Web 为功能性等价）。**约束**：不得拆分为独立事件，避免破坏 HITL 恢复；可选增强为 emit 时补传 checkpointId/interruptId 消除回查依赖（服务端契约增强，非 Web handler 强制）。

### 建议 3（记录级，不新增编号）：CON-1 维持现状待观察
CON-1 维持 `agent:thinking` 统一事件属 P2 待观察，未改变外部契约（仅 consumer 端用 `isAgentUser` 分流的既有设计），按文档质量标准不记 D 编号，维持架构方案"记录待观察"即可。

---

## 6.4 合规审查结论

- **8 项决策全部合规**：D-117/118/124/126/130/131/132 均被架构方案满足或无冲突；D-130 为完全一致（P0 修复即回归书面决策）。
- **建议动作**：
  1. 不更新 D-130 描述（仅补详情注释级备注）。
  2. **建议新增 D-133**，固化 CON-2 的 `hitl:question` 映射契约，防未来回归。
  3. EXP-1/EXP-3/CON-1/并发HITL/远程删除 维持现状，均兼容现有决策，无需新编号。
