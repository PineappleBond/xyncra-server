# Step 1 — CLI 行为基线与 web 端清单

> 基线来源：`demo/web/packages/xyncra-client-cli`（已验证通过）
> web 端：`demo/web/packages/xyncra-client-web`
> 集成点：`demo/web/src/`
> 架构约束：`demo/web/docs/decisions/PRODUCT_DECISIONS.md`（TS-D-001/003/007/008/012）

---

## ① CLI 行为基线清单（按功能点）

### 1. 消息收发
- **onMessage**（`update-handler.ts:34`）：输入 `Message`（camelCase，`message.id` 即 seq）。CLI 仅打印 `seq/from/conv/content`。Web 的 `ReactUpdateHandler.onMessage`（`ReactUpdateHandler.ts:56`）改为 emit `message:added`（字段 `id/conversationId/senderId/content/clientMessageId/replyToId/createdAt/updatedAt/deletedAt`）。
  - ⚠️ 字段命名：CLI 日志称 `seq=${message.id}`；web event 用 `message.id`，但 sync-manager 传入的 `last_read_message_id` 是**数字**（见下）。

### 2. 删除消息
- **onDeleteMessage**（`update-handler.ts:40`）：输入 `(messageId: string, conversationId: string)`。Web `ReactUpdateHandler.onDeleteMessage`（`ReactUpdateHandler.ts:62`）emit `message:removed`（`messageId, conversationId`）—— 字段一致。

### 3. 标记已读 ⚠️ 关键偏差点
- **onMarkRead**（`update-handler.ts:44`）：输入 `(conversationId: string, messageId: string)`。
- 真实数据来源：`sync-manager.ts:960` `case 'mark_read'` payload = `{ conversation_id, last_read_message_id }`，其中 `last_read_message_id` 为**数字**，被强转成 `string`（`last_read_message_id as unknown as string`，`sync-manager.ts:967`）。
- Web `ReactUpdateHandler.onMarkRead`（`ReactUpdateHandler.ts:69`）emit `read:updated`（`{ conversationId, lastReadMessageId }`）。
- ⚠️ **注意**：web 的 `read:updated` 事件**无任何 hook 订阅**（grep 确认 `useMessages`/`useConversations` 均未订阅 `read:updated`，只有 `EventEmitter.ts` 定义了类型）。CLI 仅打印；web 端发出事件但 UI 未消费 → 已读状态不反映到界面（潜在 bug）。

### 4. 会话更新
- **onConversation**（`update-handler.ts:48`）：输入 `Conversation`，打印 `id/title`。
- 触发逻辑（`sync-manager.ts:971`）：`action==='create'` 传完整 Conversation；`delete/restore/update` 仅传 `{ id }` 形式的最小对象。
- ⚠️ Web `ReactUpdateHandler.onConversation`（`ReactUpdateHandler.ts:76`）只 emit `conversation:added`，**从不 emit `conversation:updated` / `conversation:removed`**（`EventEmitter.ts:17-19` 虽定义了这两个事件，但 handler 未发出）。
  - 后果：`useConversations` 订阅了 `conversation:updated`（`:107`）和 `conversation:removed`（`:116`）但永不触发；delete/restore 后 UI 不刷新（除非调用 `deleteConversation` 本地乐观更新）。

### 5. Gap
- **onGap**（`update-handler.ts:54`）：输入 `seq: number`，CLI 打印。
- Web `ReactUpdateHandler.onGap`（`ReactUpdateHandler.ts:82`）**空实现**，注释称“由 core sync pipeline 处理，UI 无需响应”。✅ 符合约束（sync-manager 已内部处理 gap）。

### 6. Typing
- **onTyping**（`update-handler.ts:59`）：输入 `(userId, conversationId, isTyping)`。CLI 用 `isAgentUser()` 区分 `typing`/`thinking` 文案。
- Web `ReactUpdateHandler.onTyping`（`ReactUpdateHandler.ts:89`）emit `agent:thinking`（`{userId, conversationId, isTyping}`）—— **不区分 agent/user**，统一事件名 `agent:thinking`。与 CLI 语义有差异（CLI 对普通用户发 `typing` 文案），但 web 统一归为 agent 思考态，可接受。⚠️ 字段名一致，事件名映射到 `agent:thinking`。

### 7. Streaming
- **onStreaming**（`update-handler.ts:77`）：输入 `(userId, conversationId, streamId, text, isDone)`。CLI 区分 `agent`/`streaming` 前缀。
- Web `ReactUpdateHandler.onStreaming`（`ReactUpdateHandler.ts:103`）：`isDone` → emit `stream:done`（`{userId, conversationId, streamId}`）；否则 emit `stream:text`（`{userId, conversationId, streamId, text}`）。✅ 字段一致。

### 8. Agent 状态
- **onAgentStatus**（`update-handler.ts:92`）：输入 `(userId, conversationId, status)`。常量值见 `useAgentStatus.ts:29`：`idle|thinking|tool_calling|generating|asking_user|timeout`。
- Web `ReactUpdateHandler.onAgentStatus`（`ReactUpdateHandler.ts:124`）emit `agent:status`（`{userId, conversationId, status}`）。✅ 字段一致。

### 9. Agent 超时 / HITL ⚠️ 最大偏差点
- **onAgentTimeout**（`update-handler.ts:103`）：输入 `(userId, conversationId, reason)`，CLI 打印 `[agent_timeout]`。
- Web `ReactUpdateHandler.onAgentTimeout`（`ReactUpdateHandler.ts:134`）emit `hitl:question`（`{userId, conversationId, reason}`）。⚠️ 事件名 `hitl:question`，但 **payload 不含 `question_id`/`checkpoint_id`/`interrupt_id`/`agent_id`**。
- **正确恢复契约**（`commands/agent-resume.ts:59` + `xyncra-client.ts:128-129`）：`agent_resume` RPC 需要 `conversation_id`+`checkpoint_id`+`interrupt_id`+`agent_id`+`answer`。
- Web `useHITL.answer()`（`useHITL.ts:79`）只发 `{ question_id, answer }`；`HITLDialog.handleOk`（`HITLDialog.tsx:27`）实际传 `pendingQuestion.userId` 作为 questionID。
- ⚠️ **三重不一致**：(1) 事件无 checkpoint/interrupt/agent_id；(2) RPC 缺字段；(3) answer 实参误用 userId。
- 服务端真正数据源：`GetConversationResult.questions`（D-125，`xyncra-client.ts:121-134`）含 `id/checkpoint_id/interrupt_id`，web 端未接入该查询。

### 10. RPC 调用
- CLI 主路径：IPC（Unix socket）→ JSON-RPC 2.0（`rpc-helper.ts` 为 daemon 不可用时的 WebSocket fallback，10s 超时）。
- `isMutationMethod`（`rpc-helper.ts:107`）：`send_message/delete_message/mark_as_read/create_conversation/delete_conversation/restore_conversation` 为变更类。
- Web：无 IPC，**全部走 `client.call()` / 注入的 `BrowserWebSocketFactory`**（TS-D-007）。`XyncraProvider` 通过 `onError` 回调 emit `error:rpc` + antd `message.error`（`XyncraProvider.tsx:216`）。
- 函数注册同步：`system.register_functions`（full-replacement 模型，`XyncraProvider.tsx:332`），fail-open（D-072）。

### 11. 存储
- CLI：IndexedDB（TS-D-003，fake-indexeddb polyfill）+ `--db-path` 语义已变更（TS-D-012）。
- Web：`BrowserIndexedDBProvider.getIDBFactory()`（`indexeddb.ts:24`）返回浏览器原生 `indexedDB`。✅ 仅暴露 `getIDBFactory`，无 CRUD 方法。

### 12. WebSocket 连接/重连
- Web：`CoreWebSocketBridge`（`websocket.ts:168`）直接 `new WebSocket(url)`（浏览器原生），`binaryType='arraybuffer'`，适配 core 的 `IWebSocket` 回调接口。
- 连接状态机（`XyncraProvider.tsx:73`）：`connecting → syncing → connected → disconnected`。
- ⚠️ `syncing` 仅在“2s 内无数据”fallback（`XyncraProvider.tsx:311`）触发；真正的 `connected` 依赖首次 `message:added`/`conversation:added`（`XyncraProvider.tsx:306`）。空库场景下无 `message:added` 事件，2s 后进入 `syncing` 而非 `connected`，但此后无机制再转 `connected`。⚠️ 状态机可能卡在 `syncing`。

### 13. 函数注册
- CLI：`builtin-functions.ts` 注册 3 个内置诊断函数（`ping`/`get_device_info`/`get_time`），通过 `registerBuiltinHandlers`（`builtin-functions.ts:65`）。
- ⚠️ Web `XyncraProvider` **未调用任何 builtin 注册**（grep 确认 web 包无 `registerBuiltinHandlers` 引用）。Web 仅注册 `demo/web/src/functions/*.tsx` 的四个 demo 函数（`get_current_page`/`highlight_element`/`show_notification`/`navigate_to`）。
- Web 注册机制：两阶段（`FunctionRegistry.ts` D-3）→ `XyncraProvider.onChange` 同步到服务端（full-replacement）。

---

## ② web 端接口/事件清单

### 导出契约（`index.ts`）
- Adapters：`BrowserWebSocketAdapter`/`BrowserWebSocketFactory`/`BrowserIndexedDBProvider`/`ConsoleLogger`/`Logger`/`CloseEvent`/`WebSocketAdapter`
- Components：`FloatingAssistant`/`ConnectionStatus`(别名 `ConnectionStatusBadge`)/`HITLDialog`/`ChatWindow`/`AgentSelector`/`AgentDetail`/`ConversationList`/`FloatingButton`/`FunctionCallDisplay`/`MessageArea`/`FLOATING_ASSISTANT_STYLES`
- Context：`XyncraProvider`/`XyncraContext`/`XyncraProviderProps`/`ConnectionStatus`/`XyncraContextValue`
- Hooks：`useXyncra`/`useConversations`/`useMessages`/`useStreaming`/`useAgentStatus`/`useHITL`/`useRegisterFunction`/`useRegisterFunctions`
- Internal：`ReactUpdateHandler`/`FunctionRegistry`/`TypedEventEmitter`/`EventEmitter`(类型)/`isAgentUser`/`FunctionHandler`

### 内部事件名（`EventEmitter.ts:16-55` UpdateHandlerEventMap）
| 事件 | 触发点 | 订阅方 |
|------|--------|--------|
| `message:added` | `ReactUpdateHandler.ts:57` | `useMessages.ts:102`, `XyncraProvider.tsx:306`(连接态) |
| `message:updated` | （已定义，未 emit） | `useMessages.ts:112` |
| `message:removed` | `ReactUpdateHandler.ts:66` | `useMessages.ts:120` |
| `conversation:added` | `ReactUpdateHandler.ts:77` | `useConversations.ts:92`, `XyncraProvider.tsx:307` |
| `conversation:updated` | （已定义，未 emit） | `useConversations.ts:107` |
| `conversation:removed` | （已定义，未 emit） | `useConversations.ts:116` |
| `read:updated` | `ReactUpdateHandler.ts:70` | **无订阅** |
| `stream:text` | `ReactUpdateHandler.ts:113` | `useStreaming.ts:109` |
| `stream:done` | `ReactUpdateHandler.ts:111` | `useStreaming.ts:131` |
| `agent:status` | `ReactUpdateHandler.ts:129` | `useAgentStatus.ts:58` |
| `agent:thinking` | `ReactUpdateHandler.ts:94` | `useAgentStatus.ts:66` |
| `hitl:question` | `ReactUpdateHandler.ts:139` | `useHITL.ts:62` |
| `error:rpc` | `XyncraProvider.tsx:218` | 仅 `EventEmitter` 定义，无 hook 订阅 |

⚠️ 4 个事件定义了但永不 emit（`message:updated`/`conversation:updated`/`conversation:removed`），2 个 emit 了但无订阅（`read:updated`/`error:rpc`）。

### 依赖的 core 接口
- `IUpdateHandler` + 可选接口（`ITypingHandler`/`IStreamingHandler`/`IAgentStatusHandler`/`IAgentTimeoutHandler`）：`interfaces.ts:135-209`
- `IWebSocket`/`IWebSocketFactory`/`IIndexedDBProvider`/`ILogger`
- `XyncraClient`（`.call`/`.start`/`.stop`/`.listConversations`/`.getMessages`/`.createConversation`/`.deleteConversation`/`.sendMessage`/`.registerRequestHandler`）

### 测试隐含假设
- `__tests__/hooks/useHITL.test.ts:85`：断言 `agent_resume` 仅带 `{question_id, answer}` → **测试锁定了错误契约**。
- `__tests__/integration/hitl-flow.test.ts:85`：同上错误假设。
- `__tests__/ReactUpdateHandler.test.ts`：应断言 emit `read:updated`（见 memory，已修）。
- `useStreaming` 测试依赖 `requestAnimationFrame`/`setTimeout` 时序（500ms cleanup）。
- `XyncraProvider` 测试依赖 antd `message.useMessage` + `crypto.randomUUID` + `localStorage`。

---

## ③ 隐藏约束列表（review 必须遵守）

1. **TS-D-007 无 IPC 层**：web 端一切走浏览器原生 WebSocket + 注入的 `BrowserWebSocketFactory`，严禁引入 `ipc.ts`/`daemon.ts`/`lock.ts` 等 Node 代码。
2. **TS-D-001 不引入 Node 代码**：`ws`/`node:os`/`node:fs` 等仅限 CLI 包；web 包 `useRegisterFunction` 的 demo 函数（`navigateTo`）使用 `window`/`document`/`history`（浏览器全局），不可在 SSR/Node 环境运行。
3. **TS-D-012 `--db-path` 语义变更**：CLI 的 `--db-path` 是 IndexedDB 库名；web 端无该 flag，由 `deviceID`（`crypto.randomUUID` + localStorage，D-5）派生库名。
4. **onGap 由 core 处理**：web 的 `onGap` 应为空实现（✅ 已遵守），不要尝试在 UI 响应 gap。
5. **D-3 两阶段函数注册**：本地 `FunctionRegistry` 存 handler → `XyncraProvider.onChange` 调 `system.register_functions`（full-replacement）→ fail-open（D-072，注册失败不抛错，仅 emit `error:rpc`）。
6. **D-5 deviceID 自动生成**：`resolveDeviceID`（`XyncraProvider.tsx:130`）+ localStorage 持久化；SSR/crypto 不可用时必须显式传 `deviceID` prop。
7. **D-1 ReactUpdateHandler 不持 React state**：通过 `TypedEventEmitter` 解耦（✅ 已遵守）；hook 在 `useEffect` 订阅。
8. **D-072 函数注册 fail-open**：`system.register_functions` 失败仅 `handleError`，不阻塞。
9. **C11 重连握手**：`client.start()` 内部执行 `system.reconnect` + `system.register_functions`（`xyncra-client.ts:826`）。
10. **已读消息 id 类型陷阱**：`last_read_message_id` 是数字（`sync-manager.ts:963`），经 `as unknown as string` 传入 handler；web 端 `read:updated.lastReadMessageId` 实为字符串化的数字，UI 比较时需警惕类型。
11. **HITL 正确数据契约**：`agent_resume` 需要 `conversation_id`+`checkpoint_id`+`interrupt_id`+`agent_id`+`answer`（asserted by `agent-resume.ts` + `xyncra-client.ts:128`）；正确 question 数据来自 `GetConversationResult.questions`（D-125），而非 `hitl:question` 事件。
12. **内置函数未在 web 注册**：CLI 的 3 个 builtin（`ping`/`get_device_info`/`get_time`）在 web 端未注册（TS-D-007 下可能不需要，但需确认是否符合预期）。
