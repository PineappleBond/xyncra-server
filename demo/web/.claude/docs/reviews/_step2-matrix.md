# Step 2 — CLI 基线 vs web 端功能对照矩阵

> 基线：`demo/web/packages/xyncra-client-cli`（已验证通过）
> 审查对象：`demo/web/packages/xyncra-client-web`
> 集成点：`demo/web/src/`
> 本人已亲自核对所有 step1 标注的重大偏差点（HITL / read:updated / conversation:updated/removed），结论见各备注。

## 判定图例
- `一致`：web 与 CLI 协议语义/字段/错误处理一致
- `偏差`：有差异，需进一步判定是否 bug
- `web 独有(合理)`：因浏览器 React 环境差异（事件驱动 UI vs stdout、无 IPC 层）产生的合理差异
- `web 缺失(需补)`：CLI 有而 web 明显遗漏的必要行为

---

## ① 对照矩阵

| # | 功能点 | CLI 行为 | web 行为 | 判定 | 备注 / 文件:行号 |
|---|--------|----------|----------|------|------------------|
| 1 | 消息收发 | `onMessage` 打印 `seq/from/conv/content` (`update-handler.ts:34`) | emit `message:added`（`id/conversationId/senderId/content/clientMessageId/replyToId/createdAt/updatedAt/deletedAt`）(`ReactUpdateHandler.ts:56`) | web 独有(合理) | web 转为事件供 UI 订阅，字段一致；`message.id` 即 seq。 |
| 2 | 删除消息 | `onDeleteMessage(messageId, conversationId)` 打印 (`update-handler.ts:40`) | emit `message:removed`（`messageId, conversationId`）(`ReactUpdateHandler.ts:62`) | 一致 | 字段一致。 |
| 3 | 标记已读 | `onMarkRead(conv, msg)` 打印 (`update-handler.ts:44`) | emit `read:updated`（`conversationId, lastReadMessageId`）(`ReactUpdateHandler.ts:69`) | **偏差** | ⚠️ **已验证 BUG**：`read:updated` 无 hook 订阅（`useMessages`/`useConversations` 均未订阅），仅 `EventEmitter.ts` 定义类型。已读状态不反映到 UI。 |
| 4 | 会话更新 | `onConversation` 打印；`create` 传完整对象，`delete/restore/update` 仅 `{id}` (`update-handler.ts:48`, `sync-manager.ts:971`) | emit `conversation:added`（`ReactUpdateHandler.ts:76`） | **偏差** | ⚠️ **已验证 BUG**：handler 只发 `added`，永不发 `conversation:updated`/`conversation:removed`；但 `useConversations` 订阅了这两个事件（`:107`/`:116`）。delete/restore 后 UI 不刷新（仅本地乐观更新掩盖）。 |
| 5 | Gap | `onGap(seq)` 打印 (`update-handler.ts:54`) | 空实现（`ReactUpdateHandler.ts:82`） | web 独有(合理) | ✅ 符合约束（core sync pipeline 处理 gap，UI 无需响应）。 |
| 6 | Typing | `onTyping` 用 `isAgentUser` 区分 `typing`/`thinking` 文案 (`update-handler.ts:59`) | emit `agent:thinking`（`userId, conversationId, isTyping`）(`ReactUpdateHandler.ts:89`)，不区分 agent/user | web 独有(合理) | 事件名统一为 `agent:thinking`，字段一致；CLI 对普通用户发 `typing` 文案，web 归为 agent 思考态，可接受。 |
| 7 | Streaming | `onStreaming` 区分 `agent`/`streaming` 前缀 (`update-handler.ts:77`) | `isDone`→`stream:done`；否则 `stream:text`（`userId, conversationId, streamId, text`）(`ReactUpdateHandler.ts:103`) | 一致 | 字段一致。 |
| 8 | Agent 状态 | `onAgentStatus` 打印 (`update-handler.ts:92`) | emit `agent:status`（`userId, conversationId, status`）(`ReactUpdateHandler.ts:124`) | 一致 | 字段一致，状态常量同 `useAgentStatus.ts:29`。 |
| 9 | Agent 超时 / HITL | `onAgentTimeout(conv, reason)` 打印 (`update-handler.ts:103`)；恢复需 `agent_resume` RPC：`conversation_id+checkpoint_id+interrupt_id+agent_id+answer` (`commands/agent-resume.ts:59`) | emit `hitl:question`（`userId, conversationId, reason`）(`ReactUpdateHandler.ts:134`)；`useHITL.answer` 只发 `{question_id, answer}` (`useHITL.ts:79`)；`HITLDialog` 用 `pendingQuestion.userId` 作 questionID (`HITLDialog.tsx:27`) | **偏差** | ⚠️ **已验证 BUG（P0）**：三重不一致，HITL 完全无法恢复。详见 §②-1。 |
| 10 | RPC 调用 | IPC（Unix socket）主路径 + WebSocket fallback（`rpc-helper.ts`，10s 超时）；`isMutationMethod` 区分变更类 (`rpc-helper.ts:107`) | 无 IPC，全走浏览器原生 WebSocket（`BrowserWebSocketFactory`/`CoreWebSocketBridge`，`websocket.ts:168`）；`XyncraProvider.onError` emit `error:rpc` + antd `message.error`（`XyncraProvider.tsx:216`） | web 独有(合理) | TS-D-007 约束，无 IPC 层合理。 |
| 11 | 存储 | IndexedDB + fake-indexeddb polyfill（`--db-path` 为库名，TS-D-012） | `BrowserIndexedDBProvider.getIDBFactory()` 返回原生 `indexedDB`（`indexeddb.ts:24`），无 CRUD 方法 | 一致 | ✅ 仅暴露 factory，符合设计。 |
| 12 | WebSocket 连接/重连 | daemon 维持连接；CLI 无需重连状态机 | 状态机 `connecting→syncing→connected→disconnected`（`XyncraProvider.tsx:73`）；`syncing` 仅 2s 无数据 fallback 触发（`:311`）；真正 `connected` 依赖首个 `message:added`/`conversation:added`（`:306`） | **偏差** | ⚠️ **已验证 BUG（P2）**：空库场景下无 `message:added`，2s 后进入 `syncing` 后无机制再转 `connected`，状态机卡死。 |
| 13 | 函数注册 | `builtin-functions.ts` 注册 3 个诊断函数 `ping`/`get_device_info`/`get_time`（`registerBuiltinHandlers`，`:65`） | 未注册任何 builtin（grep 确认无 `registerBuiltinHandlers` 引用）；仅注册 `src/functions/*.tsx` 4 个 demo 函数 | web 缺失(需补)？ | 见 §②-4：web 环境无 Node `node:os`，`get_device_info` 依赖 `hostname()` 无法移植；是否需等价实现待定（当前判为合理差异/体验）。 |

---

## ② 偏差分类清单

### BUG（确认 bug，需修复）

**BUG-1：HITL 恢复完全不可用（P0）**
- 文件:行号：
  - `ReactUpdateHandler.ts:134`（`onAgentTimeout` emit `hitl:question` 仅含 `userId/conversationId/reason`，缺 `checkpoint_id/interrupt_id/agent_id`）
  - `useHITL.ts:79`（`answer()` 发 `agent_resume` 仅 `{question_id, answer}`）
  - `HITLDialog.tsx:27`（`handleOk` 用 `pendingQuestion.userId` 作 questionID，误用）
  - `useHITL.ts:62`（订阅 `hitl:question` 后仅存 `{userId, conversationId, question}`，未保留恢复所需字段）
- 与 CLI 差异：CLI `agent_resume` 需 `conversation_id+checkpoint_id+interrupt_id+agent_id+answer`（`commands/agent-resume.ts:59`）；web 全部缺失。
- 影响：用户无法在 web 端恢复被中断（asking_user/timeout）的 agent；HITL 流程形同虚设。
- 严重度：**P0（功能级失败）**。
- 修复方向：core 的 `QuestionStore` 已落库 questions（含 `checkpoint_id/interrupt_id`，`sync-manager.ts:809`/`question-store.ts:23`），但 `XyncraClient` 未暴露 `getQuestions` 便捷方法、web `useHITL` 未读取。需 (a) 在 `hitl:question` 事件或 hook 中补充 question 元数据；(b) `answer()` 改为读取真实 question 的 `checkpoint_id/interrupt_id/agent_id/conversation_id` 并发送完整 `agent_resume`；(c) 修正 `HITLDialog` 传 question ID 而非 userId。

**BUG-2：read:updated 事件无消费者（P1）**
- 文件:行号：`ReactUpdateHandler.ts:69`（emit），`useMessages.ts`/`useConversations.ts` 均未订阅 `read:updated`（仅 `EventEmitter.ts` 定义）。
- 与 CLI 差异：CLI 仅打印；web 发出事件但 UI 未消费 → 已读光标/未读计数不刷新。
- 影响：消息已读状态不反映到界面（未读标记残留）。
- 严重度：**P1（UI 状态不一致，非崩溃）**。
- 修复方向：在 `useConversations` 订阅 `read:updated`，更新对应 conversation 的 `lastReadMessageId`/未读计数；注意 `lastReadMessageId` 实为字符串化数字（`sync-manager.ts:963` `as unknown as string`），比较需类型对齐。

**BUG-3：conversation:updated / conversation:removed 永不 emit（P1）**
- 文件:行号：`ReactUpdateHandler.ts:76`（`onConversation` 只 emit `conversation:added`）；`useConversations.ts:107`/`:116`（订阅但永不触发）。
- 与 CLI 差异：CLI `onConversation` 对 `delete/restore/update` 仍被调用（`sync-manager.ts:971`，仅传 `{id}`）；web handler 未区分 action，统一发 `added`。
- 影响：服务端推送的会话更新/删除（非本地发起）后，UI 列表不刷新。本地 `deleteConversation` 乐观更新可掩盖，但远端删除/恢复不更新。
- 严重度：**P1（远端会话变更 UI 不感知）**。
- 修复方向：`ReactUpdateHandler.onConversation` 需携带 action（create/update/delete/restore）并分别 emit `conversation:added`/`conversation:updated`/`conversation:removed`；或 core sync-manager 拆分回调。需核对 `IUpdateHandler.onConversation` 接口是否提供 action 字段。

### 一致性（与 CLI 语义不一致但非崩溃级，记录）

**CON-1：Typing 事件不区分 agent/user（P2）**
- 文件:行号：`ReactUpdateHandler.ts:89`（统一 emit `agent:thinking`）。
- 与 CLI 差异：CLI `onTyping` 用 `isAgentUser` 区分 `typing`/`thinking` 文案（`update-handler.ts:59`）。
- 影响：普通用户 typing 也被归为 agent 思考态，UI 展示可能误导。
- 严重度：**P2**。

**CON-2：`hitl:question` 事件名语义偏差（P2）**
- 文件:行号：`ReactUpdateHandler.ts:134`（timeout 事件命名为 `hitl:question`）。
- 与 CLI 差异：CLI `onAgentTimeout` 仅打印 `[agent_timeout]`，timeout 与 HITL question 为两类信号；web 把 timeout 直接当 question。
- 影响：timeout 未必伴随可回答 question，强行弹 HITLDialog 体验偏差。
- 严重度：**P2**（与 BUG-1 关联）。

### 体验（开发者体验/文档问题，记录）

**EXP-1：error:rpc 事件无订阅方（P2）**
- 文件:行号：`XyncraProvider.tsx:218`（emit `error:rpc`），无 hook 消费，仅 antd `message.error` 提示。
- 影响：开发者无法以编程方式监听 RPC 错误（如 retry 逻辑）。
- 严重度：**P2**（文档/API 完整性）。

**EXP-2：测试锁定错误 HITL 契约（P2）**
- 文件:行号：`__tests__/hooks/useHITL.test.ts:85`、`__tests__/integration/hitl-flow.test.ts:85` 断言 `agent_resume` 仅 `{question_id, answer}`。
- 影响：测试固化了 BUG-1 的错误契约，修复 BUG-1 时必须同步改测试。
- 严重度：**P2**。

**EXP-3：内置诊断函数未在 web 注册（P2）**
- 文件:行号：web 端无 `registerBuiltinHandlers` 引用（对比 `builtin-functions.ts:65`）。
- 与 CLI 差异：CLI 自动注册 `ping`/`get_device_info`/`get_time`；web 仅注册 4 个 demo 函数。
- 影响：server 反向 RPC 调用 `ping` 等时 web 端无 handler（fail-open，非崩溃）。
- 严重度：**P2**（待决策：TS-D-007 下 `get_device_info` 依赖 Node `node:os` 无法移植，需浏览器等价实现或明确不注册）。

### 合理差异（环境差异，不算问题）

**OK-1：消息收发转事件（`message:added`）** — web 事件驱动 UI，CLI 走 stdout（§①-1）。
**OK-2：onGap 空实现** — core 已处理 gap，符合约束（§①-5）。
**OK-3：RPC 全走浏览器 WebSocket，无 IPC** — TS-D-007 约束（§①-10）。
**OK-4：存储仅暴露 `getIDBFactory`** — 符合设计，无 CRUD（§①-11）。

---

## ③ 建议修复优先级

| 优先级 | 项 | 说明 |
|--------|----|----|
| **P0** | BUG-1 HITL 恢复不可用 | 功能级失败，agent 中断后无法恢复；需 core+web 协同改。 |
| **P1** | BUG-2 read:updated 无订阅 | UI 已读状态不刷新。 |
| **P1** | BUG-3 conversation:updated/removed 永不 emit | 远端会话变更 UI 不感知。 |
| **P2** | CON-1/2、EXP-1/2/3、BUG-4 状态机卡死 | 一致性/体验问题，可纳入同一轮修复。 |

> 注：§①-12 WebSocket 状态机卡 `syncing` 已归为 BUG（P2），修复方向：空库场景下 `connected` 应由 `client.start()` resolve 或握手完成信号驱动，而非依赖首个数据事件。

---

## ④ 待确认决策（需调度者/架构师裁定）
1. **内置函数注册范围**（EXP-3）：web 是否需要等价实现 `ping`/`get_device_info`/`get_time`？`get_device_info` 的 `hostname()` 需浏览器等价（如 `navigator` 信息）。
2. **IUpdateHandler.onConversation 是否携带 action**：BUG-3 修复前提是 core 接口能提供 create/update/delete/restore 区分；若不能，需在 core 层拆分回调。
3. **HITL 数据源**：BUG-1 修复依赖 web 端读取 `QuestionStore` 中已落库的 questions（含 `checkpoint_id/interrupt_id`），需决定是否在 `XyncraClient` 暴露 `getQuestions(conversationId)` 便捷方法并接入 `useHITL`。
