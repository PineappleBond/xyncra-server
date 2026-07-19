# 步骤 2 架构师深度差异分析与修复方案 — xyncra-client-web vs xyncra-client-cli

**子代理**：2（后端架构师）
**项目根**：/Users/leichujun/go/src/github.com/PineappleBond/xyncra-server
**审查对象**：demo/web/packages/{xyncra-client-core, xyncra-client-cli, xyncra-client-web}
**基准**：xyncra-client-cli（已验证基线，84 测试通过）
**输入**：_step1-code-archaeology.md、docs/decisions/PRODUCT_DECISIONS.md（D-117/118/124/126/130/131/132）
**方法**：步骤 3（深度差异分析）+ 步骤 4（设计修复方案），每项基于代码事实给出 `[修/不修/改]` 结论。

---

## 3.0 关键代码事实（已核实，避免臆测）

- **存储层 delete 路径已完整实现**：`sync-manager.ts`(Go) → `handleDeleteMessageTx` → `Messages.SoftDeleteTx`；`handleConversationDeleteTx` → `Conversations.SoftDeleteTx`（D-013 级联）。Web 端 `ReactUpdateHandler.onDeleteMessage`(行 63-68) 与 `onConversation(removed)`(行 87-89) 正确 emit `message:removed` / `conversation:removed`，`useMessages`(行 120-127) 与 `useConversations`(行 122-127) 均按 conversationId 过滤后从本地 state 移除。**IndexedDB 软删已由 core 完成，UI 仅做内存视图更新，路径完整闭合。**
- **HITL 恢复字段已正确保留**：`useHITL.ts:69-77` 消费 `questionId/checkpointId/interruptId`；`useHITL.ts:88-115` 实现 075 修复的恢复逻辑（当事件缺 checkpoint/interrupt 时，回查 `conv.questions` 补全）。`ReactUpdateHandler.onAgentTimeout` 当前 emit `hitl:question` 时**未传** checkpointId/interruptId（仅传 reason），但 useHITL 的回查兜底可补偿——这点影响 CON-2 决策（见 §3.2）。
- **Web 端无任何 builtin 函数自动注册**：全文检索 `ping|get_device_info|get_time` 在 web 包中**仅作为用户示例出现**（`useRegisterFunctions.ts:41` 注释示例 `get_time`），无 `registerFunction` 调用内置 ping/get_device_info/get_time。CLI 侧 builtin 由 Daemon 内置函数自动注册（D-115）。
- **ConnectionStatus 已处理 `syncing`**：`ConnectionStatus.tsx:23` STATUS_MAP 含 `syncing: {processing, '同步中...'}`，改为 `setConnectionStatus('syncing')` 不会引入未处理分支。
- **XyncraProvider 状态机**：`connecting`(行 285) → 首数据 `connected`(行 295-307) / 2s 兜底 `connected`(行 313-317)。全程无 `syncing` 赋值（D-130 死状态）。

---

## 3.1 CON-1：onTyping 不区分 agent/user

**现状**：`ReactUpdateHandler.onTyping`(行 106-116) 对所有 userId 统一 emit `agent:thinking`。CLI 用 `isAgentUser` 区分 `typing`/`thinking`（update-handler.ts:63-78）。`useAgentStatus`(行 66-75) 订阅 `agent:thinking` 将 isTyping 映射为 thinking/idle。

**决策**：**[不修]**

**理由**：
- Web 选择"事件名中立 + 消费者自行判断 agent"的架构（见 _step1 NEW-3）。`agent:thinking` 事件 payload 含 `userId`，消费者本应据 `isAgentUser(userId)` 分流；事件名前缀 `agent:` 是历史命名惯性，非强制语义。
- 当前所有消费方（`useAgentStatus`、`MessageArea` 的 typing 指示）均将 `agent:thinking` 视为"agent 思考/输入中"提示。若拆分为 `typing:user`+`typing:agent` 双事件，需同步改动 `EventEmitter.ts` 事件表、`useAgentStatus`、`MessageArea`、`useStreaming` 无关分支，且 CLI 基线仍是文本输出无结构化事件——**对齐 CLI 的"区分"价值在 Web 结构化事件模型下收益极低**。
- 权衡陷阱：拆分事件名会破坏现有 53+ 处测试对 `agent:thinking` 的断言（useAgentStatus.test、ReactUpdateHandler.test、hitl-flow.test 等），且需新增长事件类型，DX 成本高于收益。
- 若未来确需真实用户 typing 指示，应在 consumer 端用 `isAgentUser` 分流，而非改动 handler 事件契约。

**优先级**：P2（记录待观察，不纳入本轮修复集）

---

## 3.2 CON-2：onAgentTimeout 映射为 hitl:question

**现状**：`ReactUpdateHandler.onAgentTimeout`(行 151-157) 直接 emit `hitl:question {userId, conversationId, reason}`。CLI 是独立 `agent_timeout` 文本（update-handler.ts:107-115）。Go 端 `broadcaster.SendAgentTimeout`（hitl_cleanup.go:230）发送 `UpdateTypeAgentTimeout`，core `notifyHandler`(sync.go:610-616) 调 `OnAgentTimeout`。

**决策**：**[改] — 维持 hitl:question 映射，不改独立事件**

**理由**：
- **不可改独立 `agent:timeout` 事件**：075 修复已将 `reason`→`hitl:question` 的 HITL 恢复映射落地于 `useHITL.ts`，且服务端 `SendAgentTimeout` 的 reason 为 `"hitl_timeout"`（hitl_cleanup.go:230），语义即"HITL 超时中断"。拆分为独立事件需新增 `EventEmitter` 类型、`useAgentStatus`/`useHITL` 双订阅、且 Web HITL 对话框依赖 `hitl:question` 触发——**改独立事件会破坏 075 已验证的 HITL 恢复链路，回归风险 P0**。
- **改进点（低优先级，可选）**：当前 `onAgentTimeout` 未携带 `checkpointId/interruptId`，依赖 `useHITL` 回查兜底。若 timeout 场景服务端能补全这两个字段，应在 `onAgentTimeout` emit 时一并传入（与 `onConversation` 的 HITL question 对齐），消除回查依赖。但这是**服务端/契约增强**，非 Web handler 必须改动；本轮标记为 P2 建议，不阻塞。
- 决策对齐：CLI 的 `agent_timeout` 仅文本打印，Web 的 `hitl:question` 是功能性等价（触发 HITL 恢复 UI），属**合理差异**而非缺陷。

**优先级**：P2（维持现状 + 文档注释说明等价性，不改动代码）

---

## 3.3 D-130 偏差（高优先）

**现状**：`XyncraProvider.tsx:313-317` 2s 空库兜底设为 `connected`；`ConnectionStatus` 联合类型含 `syncing`(行 75) 但全程未赋值（死状态）。D-130 明确要求"2s 空库兜底显示 syncing 而非假 connected"。

**裁决**：**[改] — 统一为 D-130 定义的 `syncing` 行为**

**最终裁决**：**改回 `syncing`，并更新 BUG-4 认知为"根因修复"而非"表面修复"**。

**理由**：
- D-130 是书面产品决策，优先级高于 076 报告的临时绕行。空库时显示"已连接"是**假象**，违反 D-130 意图，且使 `syncing` 枚举沦为死代码（违反最小状态机原则）。
- `ConnectionStatus.tsx:23` 已正确渲染 `syncing`（'同步中...'），改为 `setConnectionStatus('syncing')` **零新增未处理分支**，UI 安全。
- 状态流转修正：`connecting` → 首数据到达 → `connected`；无数据 2s 兜底 → `syncing`（表示已握手但空库/同步中）。`connected` 仅在收到真实数据（message:added/conversation:added）或 fullSync 完成后设置。
- 权衡陷阱：需确认"首数据到达"仍作为转 `connected` 的唯一信号（行 295-307 保留）。2s 兜底改为 `syncing` 后，若用户空库且永不发消息，状态停留 `syncing`——这符合 D-130 语义（已连接服务器但无本地数据），UI 显示"同步中"合理，非卡死。

**文件:函数 变更清单**：
- `demo/web/packages/xyncra-client-web/src/context/XyncraProvider.tsx:313-317` — 将 `setConnectionStatus('connected')` 改为 `setConnectionStatus('syncing')`；同步更新行 309-312 注释（"stuck in 'syncing' forever" → "shows 'syncing' per D-130"）。
- `demo/web/packages/xyncra-client-web/src/context/XyncraProvider.tsx:70` 附近文档注释 — 更新 ConnectionStatus 类型说明，标注 `syncing` 由空库兜底触发（D-130）。

**优先级**：P0

---

## 3.4 EXP-1：error:rpc 无可编程订阅 hook

**现状**：`error:rpc` 仅在 `XyncraProvider.tsx:218` 内部 emit + antd `messageApi.error` 展示。`ErrorBoundary` 是 React 错误边界（render 级崩溃），与 `error:rpc`（RPC 失败通道，D-132）无关，二者不冲突。

**决策**：**[不修]**

**理由**：
- D-132 设计意图即为"统一错误通道 + UI 友好提示"，当前 antd message 直接展示已满足 90% 场景。
- 新增 `useErrorRpc` hook 属**渐进增强**，但：
  - 破坏面：需新增 `EventEmitter` 订阅 API、hook 文件、`XyncraContextValue` 导出或独立 hook，且现有 `messageApi.error` 已消费该事件——若 hook 也订阅会**重复消费**（需改为 emitter 多订阅，本身支持，但需明确"展示"与"编程处理"的职责切分）。
  - DX 收益有限：当前无业务代码需要编程式响应特定 RPC 错误码。
- 权衡陷阱：若新增 hook 而不移除 `messageApi.error`，会出现"错误既弹 toast 又被业务捕获"的双重行为，需设计优先级（hook 消费后是否抑制 toast）。现阶段过度设计。

**优先级**：P2（记录为后续增强，不纳入修复集）

---

## 3.5 EXP-3 / builtin 函数

**现状**：Web 端无任何 builtin 函数注册（ping/get_device_info/get_time）。CLI 由 Daemon 内置函数自动注册（D-115）。Web 的 `registerFunction` 仅为用户自定义函数通道（XyncraProvider.tsx:378、useRegisterFunctions.ts）。

**决策**：**[不修]**

**理由**：
- **ping**：浏览器无 ICMP/网络 ping 语义，且 Web 连接活性已由 `ConnectionManager` 心跳（D-010）覆盖，无需客户端 ping 函数。
- **get_device_info**：D-131 已要求 Web 自动填充 `deviceInfo: {platform:'web', userAgent}`，Ag网页运行环境天然具备该信息，无需作为"函数"暴露给 Agent。
- **get_time**：Agent 侧 Go 端 `tools/registry.go:113` 已有 `get_current_time` 内置工具，Web 作为客户端无需重复提供。
- Web 是**客户端消费方**，不是 Agent 执行方；builtin 函数应由 Agent 服务端（Go）提供，Web 仅注册"反向 RPC 处理函数"（用户自定义）。架构上 Web 不需要 builtin 函数。
- 权衡陷阱：若强行在 Web 注册伪 builtin 函数，会与 D-115（Daemon 自动注册）语义冲突，且浏览器环境无法真正执行 ping 等系统级操作。

**优先级**：P2（不修，明确记录为"Web 架构不适用"）

---

## 3.6 并发 HITL 竞态

**现状**：多设备同时回答同一 HITL question。服务端 `hitl_cleanup.go` 用 Redis `SetNX` 分布式锁（行 161-170）+ `questionStore.DeleteByCheckpoint` 保证幂等（D-083 非 fail-open、D-084 会话锁）。Web 端 `useHITL.ts:88-121` `answer()` 调 `client.call('agent_resume', {...})`，由服务端裁决"先到先得"。

**决策**：**[不修] — Web 侧无额外改动**

**理由**：
- HITL 并发最终一致性由**服务端分布式锁 + Question 持久化表（D-116）**保证，Web 作为客户端仅发起 resume 请求，竞态处理在服务端。
- `pendingQuestion` 恢复字段已正确（useHITL.ts:63-77 存储、88-115 回查补全），多设备场景：设备 A 回答后服务端标记 question 已解决，设备 B 的 `pendingQuestion` 仍本地显示，但再次 answer 会被服务端拒绝（question 已消）——UI 层可优化为"监听 conversation:updated 的 agent_status 变化清除 pendingQuestion"，但属体验增强非竞态缺陷。
- 权衡陷阱：Web 端无法单凭本地状态解决多设备竞态，强行加本地锁会与服务端锁语义重复且不一致。正确做法是依赖服务端裁决 + UI 订阅 agent_status 退出 asking_user 时清除 pending。

**优先级**：P2（建议：useHITL 订阅 `agent:status`/`conversation:updated`，当 agent_status != asking_user 时清除 pendingQuestion；记录为后续增强，不阻塞本轮）

---

## 3.7 远程删除竞态

**现状**：服务端 `delete_message.go` / `delete_conversation`（broadcastDelete*Updates）广播 `UpdateTypeDeleteMessage` / `UpdateTypeConversation(delete)`。core `sync-manager.ts`(Go) `handleDeleteMessageTx`→`Messages.SoftDeleteTx`、`handleConversationDeleteTx`→`Conversations.SoftDeleteTx`（D-013 级联）。Web `ReactUpdateHandler.onDeleteMessage`(63-68) emit `message:removed`、`onConversation(removed)`(87-89) emit `conversation:removed`，`useMessages`(120-127)/`useConversations`(122-127) 按 conversationId 过滤移除本地视图。

**决策**：**[不修] — 本地存储清理路径已完整**

**理由**：
- 用 codegraph 核实：core 存储层 **已实现 delete 路径**（`SoftDeleteTx` 在 messages/conversations store 均存在，D-013 级联软删消息）。Web 端 IndexedDB 由 core `xyncra-client-core` 的 dexie store 管理，delete 通过 sync-manager 事务落库，非 Web handler 职责。
- 远程删除 → 服务端广播 → core 落库软删 → handler emit 事件 → hook 更新内存视图，**全链路闭合**，无遗留清理缺口。
- 权衡陷阱：若担心"软删后本地仍占空间"，属 D-013 设计（软删保留可追溯），非 bug；硬删需另立决策，超出本轮范围。

**优先级**：P2（路径已验证完整，记录确认）

---

## 3.8 AgentSelector.test.tsx 失败根因

**现状**：子代理 1 实测确认 2 失败均为**测试过期**：组件已从"单一 AI 助手"重构为 `DEFAULT_AGENTS`（test-bot/weather-bot/hitl-bot/hitl-parent，AgentSelector.tsx:31-52），测试仍断言旧文案 'AI 助手'(行 88) 与 'test-agent'(行 95)。组件行为正确，非 bug。

**决策**：**[修] — 更新测试以匹配 DEFAULT_AGENTS 语义**

**理由**：
- 测试断言的是旧契约，组件当前行为（DEFAULT_AGENTS + onSelect(agents[0].id)='test-bot'）正确。
- 修复方式：将 `getByText('AI 助手')` 改为断言 `getByText('Test Bot')`（DEFAULT_AGENTS[0].name）；将 `expect(onSelect).toHaveBeenCalledWith('test-agent')` 改为 `expect(onSelect).toHaveBeenCalledWith('test-bot')`。保留通过的 'Agents' header 断言（行 83）。
- 权衡陷阱：不可改组件去适配旧测试（会破坏多 agent 设计意图）；必须改测试。

**文件:函数 变更清单**：
- `demo/web/packages/xyncra-client-web/src/__tests__/components/AgentSelector.test.tsx:88` — `getByText('AI 助手')` → `getByText('Test Bot')`。
- `demo/web/packages/xyncra-client-web/src/__tests__/components/AgentSelector.test.tsx:95` — `toHaveBeenCalledWith('test-agent')` → `toHaveBeenCalledWith('test-bot')`；并确认测试 setup 中 `agents` 注入逻辑（行 56 的 `agentID='test-agent'` 应移除或改为不相关，因组件忽略 context agentID 用 DEFAULT_AGENTS）。

**优先级**：P1（恢复测试绿态，属上一轮遗留）

---

## 4.0 最终修复集汇总表

| 问题ID | 决策 | 优先级 | 涉及文件 | 核心改动 |
|--------|------|--------|----------|----------|
| CON-1 | 不修 | P2 | （无） | 维持 `agent:thinking` 事件；consumer 端用 `isAgentUser` 分流，不拆事件 |
| CON-2 | 改（维持现状） | P2 | ReactUpdateHandler.ts:151-157（注释说明） | 维持 `hitl:question` 映射，不改独立事件；可选增强：emit 时补 checkpointId/interruptId |
| D-130 | 改 | **P0** | XyncraProvider.tsx:313-317, :70 注释 | 2s 空库兜底 `connected`→`syncing`；更新类型注释；BUG-4 升级为根因修复 |
| EXP-1 | 不修 | P2 | （无） | 不新增 `useErrorRpc`；antd message 已满足 D-132 |
| EXP-3 | 不修 | P2 | （无） | Web 架构不适用 builtin 函数（ping/get_device_info/get_time 由服务端/Go Agent 提供） |
| 并发HITL | 不修 | P2 | （可选 useHITL.ts 增强） | 依赖服务端分布式锁；可选订阅 agent_status 清除 pendingQuestion |
| 远程删除 | 不修 | P2 | （无，已验证） | core 存储层 delete 路径完整（SoftDeleteTx + D-013 级联），Web 视图同步闭合 |
| AgentSelector测试 | 修 | **P1** | AgentSelector.test.tsx:88, :95, :56 | 断言改为 DEFAULT_AGENTS（'Test Bot' / 'test-bot'），匹配当前组件 |

**统计**：P0 × 1（D-130）、P1 × 1（AgentSelector 测试）、P2 × 6（CON-1/2、EXP-1/3、并发HITL、远程删除，均不修/维持）。

---

## 4.1 给子代理 3（QA）的验证建议

1. **D-130 修复后**运行空库场景：确认状态显示"同步中..."而非"已连接"；发送/接收首消息后转"已连接"。
2. **AgentSelector 测试**修复后 `npx jest AgentSelector.test.tsx` 应 3/3 通过。
3. 全包 `npx jest` 目标回归至 195/195（修复 2 个过期测试）。
4. 确认 CON-2 维持后 `useHITL` 集成测试（hitl-flow.test.ts）仍全绿——验证 HITL 恢复链路未被破坏。
