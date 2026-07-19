# 步骤 1 代码考古报告 — xyncra-client-web vs xyncra-client-cli

**子代理**：1（代码考古学家）
**项目根**：/Users/leichujun/go/src/github.com/PineappleBond/xyncra-server
**审查对象**：demo/web/packages/{xyncra-client-core, xyncra-client-cli, xyncra-client-web}
**基准**：xyncra-client-cli（已验证基线，84 测试通过）
**关联文档**：docs/decisions/PRODUCT_DECISIONS.md（D-117/118/124/126/130/131/132）、上一轮 076-web-vs-cli-review.md

> **注（2026-07-19 后续更新）**：本文档中提到的 `isAgentUser` / `AGENT_USER_ID_PREFIX` 已在 Agent 身份判定重构中从代码中删除。Agent 身份判定改为 Registry 精确匹配（D-054）。

---

## 1.1 可复用接口 / 类型 / 函数清单

### 1.1.1 core interfaces.ts handler 接口方法签名
来源：demo/web/packages/xyncra-client-core/src/interfaces.ts

**IUpdateHandler**（行 145-164）
- onMessage(message: Message): Promise<void>
- onDeleteMessage(messageId: string, conversationId: string): Promise<void>
- onMarkRead(conversationId: string, messageId: string): Promise<void>
- onConversation(conversation: Conversation, action: ConversationAction): Promise<void>
- onGap(seq: number): Promise<void>

**ITypingHandler**（行 177-183）
- onTyping(userId: string, conversationId: string, isTyping: boolean): Promise<void>

**IStreamingHandler**（行 191-199）
- onStreaming(userId: string, conversationId: string, streamId: string, text: string, isDone: boolean): Promise<void>

**IAgentStatusHandler**（行 207-213）
- onAgentStatus(userId: string, conversationId: string, status: string): Promise<void>

**IAgentTimeoutHandler**（行 221-227）
- onAgentTimeout(userId: string, conversationId: string, reason: string): Promise<void>

> ConversationAction = 'created' | 'updated' | 'removed'（interfaces.ts 行 19），为上一轮核心接口变更新增（BUG-3 修复）。

### 1.1.2 Web TypedEventEmitter / UpdateHandlerEventMap
来源：demo/web/packages/xyncra-client-web/src/internal/EventEmitter.ts

| 事件名 | payload 类型（行号） |
|--------|----------------------|
| conversation:added | { conversation: ConversationEvent }（行 17） |
| conversation:updated | { conversation: ConversationEvent }（行 18） |
| conversation:removed | { conversationId: string }（行 19） |
| read:updated | { conversationId: string; lastReadMessageId: string }（行 20） |
| message:added | { message: MessageEvent }（行 21） |
| message:updated | { message: MessageEvent }（行 22） |
| message:removed | { messageId: string; conversationId: string }（行 23） |
| stream:text | { userId, conversationId, streamId, text }（行 24-29） |
| stream:done | { userId, conversationId, streamId }（行 30-34） |
| agent:status | { userId, conversationId, status }（行 35-39） |
| agent:thinking | { userId, conversationId, isTyping }（行 40-44） |
| hitl:question | { userId, conversationId, reason, questionId?, checkpointId?, interruptId? }（行 45-53） |
| error:rpc | { method: string; message: string; code: number }（行 54-58） |

> ConversationEvent / MessageEvent 接口见 EventEmitter.ts 行 65-77、83-93（camelCase + 字符串日期，镜像 core Message/Conversation）。

### 1.1.3 isAgentUser 定义与约定
来源：demo/web/packages/xyncra-client-core/src/agent.ts
- AGENT_USER_ID_PREFIX = 'agent/'（行 12）
- isAgentUser(userId: string): boolean => userId.startsWith('agent/')（行 20-22）
逻辑：判断 userId 是否以 "agent/" 前缀开头（D-054）。core 与两侧均统一从 @xyncra/client-core 导入。

### 1.1.4 sync-manager.ts 转换函数清单
来源：demo/web/packages/xyncra-client-core/src/sync-manager.ts
- transformMessageDates(Record<string,unknown>): DBMessage（行 1077-1099）server snake_case+ISO string → DBMessage snake_case+Date。
- transformConversationDates(Record<string,unknown>): DBConversation（行 1105-1144）同上，覆盖全部日期字段 string→Date。
- messageToHandler(DBMessage): Message（行 1154-1177）DBMessage snake_case+Date → handler Message camelCase+ISO string；reply_to→replyToId（number→string）。
- conversationToHandler(DBConversation): Conversation（行 1182-1217）同上；last_processed_message_id→lastMessageId（number→string）。
> snake_case↔camelCase 与 Date↔string 双向均有覆盖，无遗漏字段。

---

## 1.2 约束列表

### 1.2.1 未文档化行为

1. **onGap 由 core 内部处理**：sync-manager.ts 行 1055-1057，notifyHandler 对 gap 类型直接调 handler.onGap(update.seq)。但 Web ReactUpdateHandler.onGap（ReactUpdateHandler.ts 行 99-102）为空实现（UI 无需响应），CLI 同名方法打印 [gap]（update-handler.ts 行 58-60）。gap 已在 core 内部通过 scheduleDebouncedPull（行 359）处理，handler 层 onGap 仅为可选通知钩子。

2. **ConnectionManager WS 文本协议**：connection-manager.ts 行 294-296，sendPackage 通过 JSON.stringify(pkg) 以**文本**帧发送（注释"Send as text JSON for easier debugging"，server 同时支持 text 与 binary）。读取侧 setupReadPump（行 440-442）用 JSON.parse 还原 Package。Web 适配器 CoreWebSocketBridge.send（websocket.ts 行 207-214）对 Uint8Array 走 .buffer、对 string 透传——core 始终发 string，故走 string 分支。

3. **连接状态机转换触发条件（D-130）**：
   - 初始 connectionStatus 为 'disconnected'（XyncraProvider.tsx 行 213）。
   - useEffect 启动即设 'connecting'（行 285）。
   - 监听首条 message:added / conversation:added → 'connected'（行 306-307、295-304）。
   - **2s 兜底**：若无数据到达，定时器将状态设为 'connected'（行 313-317）。
   - 关键偏差：state 类型含 'syncing'（行 75）但**代码从未赋值 syncing**；D-130 明确要求"2s 空库兜底显示 syncing 而非假 connected"，当前实现兜底设为 connected，与 D-130 直接冲突（见 §2 与 [NEW-1]）。

4. **handleEphemeralConversationUpdate 的 pull-on-notification**（sync-manager 行 832-935，D-118/D-124）：仅 action==='update' 走 pull；其他 action 直接 notifyHandler。空库/缓存最新时仍会 onConversation(...,'updated') 通知 handler（行 863-866）。

### 1.2.2 测试中隐含假设 — AgentSelector 2 失败根因
来源：测试 demo/web/packages/xyncra-client-web/src/__tests__/components/AgentSelector.test.tsx，组件 demo/web/packages/xyncra-client-web/src/components/FloatingAssistant/AgentSelector.tsx。

**实测结果**（运行 npx jest AgentSelector.test.tsx）：3 用例，2 失败、1 通过。

| 失败用例 | 断言（测试:行） | 实际组件行为 | 根因判定 |
|----------|----------------|--------------|----------|
| should show the AI assistant agent | expect(getByText('AI 助手')).toBeTruthy()（行 88） | 组件渲染 DEFAULT_AGENTS（test-bot / weather-bot / hitl-bot / hitl-parent），无 'AI 助手' 文案（AgentSelector.tsx 行 31-52） | **测试自身过期**：组件已从"单一 AI 助手"重构为"默认 agent 列表"，测试仍断言旧文案。非组件 bug。 |
| should call onSelect when agent is clicked | expect(onSelect).toHaveBeenCalledWith('test-agent')（行 95） | 点击 list-item[0] 触发 onSelect(agents[0].id) = 'test-bot'（AgentSelector.tsx 行 87） | **测试自身过期**：测试传入 agentID='test-agent'（行 56）但组件完全忽略 context 的 agentID，用内部 DEFAULT_AGENTS。断言的 'test-agent' 与组件实际值 'test-bot' 不符。非组件 bug。 |

> 通过用例为 should render the agents header（断言 'Agents'，行 83）——该文案仍存在（AgentSelector.tsx 行 81）。
> 结论：2 个失败均为**测试过期（lock 旧契约）**，与 076 报告"pre-existing AgentSelector.test.tsx only"一致，建议后续修正测试以匹配 DEFAULT_AGENTS 语义，而非改组件。

---

## 1.3 CLI vs Web 完整差异表
逐 handler 方法对比（core 共享，两侧各自实现 IUpdateHandler + 可选接口）。

| Handler 方法 | CLI 行为（文件:行） | Web 行为（文件:行） | 差异 | 影响 |
|--------------|---------------------|---------------------|------|------|
| onMessage | update-handler.ts:35-39 打印 [new message] | ReactUpdateHandler.ts:57-61 发 message:added | 语义等价（CLI 文本 / Web 事件） | 无（合理差异） |
| onDeleteMessage | update-handler.ts:41-43 打印 [delete message] | ReactUpdateHandler.ts:63-68 发 message:removed | 等价 | 无 |
| onMarkRead | update-handler.ts:45-47 打印 [mark read] | ReactUpdateHandler.ts:70-75 发 read:updated（真实事件，非伪造 conversation:updated） | Web 发独立 read:updated（BUG-2 已修）；CLI 无此事件概念 | 无（web 修复正确） |
| onConversation | update-handler.ts:49-56 打印 [conversation] {action} | ReactUpdateHandler.ts:77-97 按 action 分派 conversation:added/updated/removed（BUG-3 已修） | 等价；CLI 打印含 action 前缀，Web 分事件 | 无 |
| onGap | update-handler.ts:58-60 打印 [gap] | ReactUpdateHandler.ts:99-102 空实现 | Web 不响应（core 已内部处理） | 无 |
| onTyping | update-handler.ts:63-78 用 isAgentUser 区分 typing/thinking 标签 | ReactUpdateHandler.ts:106-116 始终发 agent:thinking（不区分 agent/user，CON-1 记录） | **Web 把所有人 typing 标为 agent:thinking**（事件名暗示仅 agent），CLI 区分 | 一致性偏差（CON-1，记录未修）；UI 若据 userID 判断 agent 则仅展示问题，否则非 agent 用户 typing 也被当作 thinking |
| onStreaming | update-handler.ts:81-93 据 isAgentUser 前缀 agent/streaming | ReactUpdateHandler.ts:120-137 发 stream:text/stream:done（无 agent 区分） | Web 事件名中立，CLI 文本区分 | 无实质差异 |
| onAgentStatus | update-handler.ts:96-104 打印 [agent_status] | ReactUpdateHandler.ts:141-147 发 agent:status | 等价 | 无 |
| onAgentTimeout | update-handler.ts:107-115 打印 [agent_timeout] | ReactUpdateHandler.ts:151-157 发 hitl:question | **Web 将 timeout 直接映射为 HITL question**（CON-2 记录） | 一致性偏差（CON-2，记录未修）；reason 字段被当作 HITL，丢失 timeout 语义 |
| error:rpc 路径 | CLI **无** onError/error:rpc（grep 仅 isAgentUser，无 onError） | xyncra-client.ts:546-548 调 onError → XyncraProvider.tsx:216-225 发 error:rpc 事件 + antd message | Web 有独立错误通道（D-132）；CLI 基线无此机制 | 合理差异（EXP-1 记录）；CLI 不缺失，仅形态不同 |

> 以上 handler 行为两侧均通过 core sync-manager.notifyHandler（行 946-1059）统一驱动，action 分支由 core 4 调用点保证（见步骤 2）。

---

## 步骤 2：上一轮（075/076）修复落地确认清单
逐条核实当前代码，给出 [OK / 回归 / 新问题] + 证据。

| # | 修复项 | 核查结果 | 证据 | 备注 |
|---|--------|----------|------|------|
| 1 | safeISODate 在 useMessages.ts/useConversations.ts 中使用；dbMessageToEvent/dbConversationToEvent 无 Invalid Date 崩溃 | [OK] | useMessages.ts:14,26-38（导入+dbMessageToEvent 全程 safeISODate，createdAt 用 ?? '' 兜底）；useConversations.ts:13,25-42（导入+dbConversationToEvent，createdAt 用 ?? ''）。dateUtils.ts:5-12 对 Invalid Date 返回 undefined | 无崩溃风险，已覆盖 |
| 2 | onMarkRead 发射 read:updated（非伪造 conversation:updated）；useConversations 消费 read:updated | [OK] | ReactUpdateHandler.ts:70-75 emit read:updated；useConversations.ts:129-140 eventEmitter.on('read:updated', ...) 更新 lastReadMessageId1/2 | 修复正确，无伪造事件 |
| 3 | CoreWebSocketBridge.send() 无 data as string 不安全断言 | [OK] | websocket.ts:207-214：send(data) 先判 Uint8Array 走 .buffer，否则透传 string；无 as string 强转 | 安全（core 始终传 string） |
| 4 | BrowserIndexedDBProvider 仅保留 getIDBFactory()，无死代码 CRUD | [OK] | indexeddb.ts:23-27：仅 getIDBFactory() 返回原生 indexedDB；无 CRUD 方法 | 符合 TS-D-003 |
| 5 | onConversation 三分支正确 dispatch；core sync-manager.ts 4 调用点传正确 action | [OK] | Web 分派：ReactUpdateHandler.ts:81-96（created→conversation:added、removed→conversation:removed、updated/default→conversation:updated）。core 4 调用点：(a) handleEphemeralConversationUpdate 行 863-866 传 'updated'；(b) handleEphemeralConversationUpdate 行 923-926 传 'updated'；(c) notifyHandler conversation 分支 行 978-981（create→'created'）；(d) notifyHandler 行 986-989（delete→'removed'，其余→'updated'） | 4 处全部正确 |
| 6 | 空库 2s 兜底状态机自愈逻辑（定位代码，确认当前是 connected 还是 syncing） | [新问题] | XyncraProvider.tsx:213（初始 disconnected）、:285（connecting）、:313-317（**2s 兜底设 connected**）。ConnectionStatus 联合类型含 'syncing'（行 75）但**全文件无任何 setConnectionStatus('syncing')** | **D-130 偏差**：决策要求"2s 空库兜底显示 syncing 而非假 connected"，当前实现兜底设为 connected，且 syncing 状态在建机中从未被赋值（dead state）。此为上一轮未记录的新偏差，详见 [NEW-1] |

---

## 新发现（上一轮未记录）

### [NEW-1] D-130 连接状态机偏差 — 2s 兜底错误设为 connected，syncing 状态成死状态
- **位置**：demo/web/packages/xyncra-client-web/src/context/XyncraProvider.tsx:313-317（兜底设为 connected），联合类型 ConnectionStatus 含 'syncing'（行 75）但全程未赋值。
- **依据**：PRODUCT_DECISIONS.md:61 明确"2s 空库兜底显示 syncing 而非假 connected"。
- **影响**：空库场景下 UI 显示"已连接"假象，违反 D-130 意图；syncing 枚举值为死代码。建议将兜底改为 setConnectionStatus('syncing')，并在 fullSync 真正完成或首数据到达后转 connected（注意：若改为 syncing，需同步确认 ConnectionStatus 组件渲染该态，避免新增未处理分支）。
- **与 BUG-4 关系**：076 报告称 BUG-4（状态机卡 syncing）已修，但实际修法是"兜底直接转 connected"，绕过了 D-130 的本意，故 BUG-4 为**表面修复**，根因（D-130 状态语义）未真正对齐。

### [NEW-2] onTyping 事件名 agent:thinking 对非 agent 用户语义误导（CON-1 的具体化）
- **位置**：ReactUpdateHandler.ts:106-116，所有 typing 统一发 agent:thinking；CLI update-handler.ts:63-78 用 isAgentUser 区分。
- **影响**：Web 侧若后续据事件名 agent:thinking 判断"仅 agent 在思考"，则真实用户（非 agent/ 前缀）typing 会被误显示为思考态。当前 agent:status/agent:thinking 均不区分，属 CON-1 记录项但未具体化。无需立即修，记录待架构决策。

### [NEW-3] isAgentUser 在 web handler 中仅用于重导出，未用于 typing 分流
- **位置**：ReactUpdateHandler.ts:26（import）、:203（re-export），但 onTyping/onStreaming 内并未调用 isAgentUser 做分支（对照 CLI 行 71、89 有调用）。
- 说明：Web 选择事件名中立（agent:thinking/stream:text），将 agent 判断推给消费者；但事件名仍带 agent: 前缀，形成语义张力（见 NEW-2）。

---

## 汇总
- **步骤 1** 已完成 core/cli/web 三角代码考古，产出 handler 接口清单、EventEmitter 事件表、isAgentUser 约定、sync-manager 转换函数清单、未文档化约束、AgentSelector 失败根因（测试过期，非组件 bug）、CLI vs Web 完整差异表。
- **步骤 2** 复查 6 项修复：5 项 [OK]，1 项发现 [新问题]（D-130 偏差，即 NEW-1）。
- **新增问题**：[NEW-1] D-130 状态机偏差（BUG-4 表面修复）、[NEW-2] typing 事件名语义误导、[NEW-3] isAgentUser 在 web handler 未用于分流。
- 当前 web 测试：195 用例，193 通过，2 失败（均为 AgentSelector 过期测试，已确认）。
