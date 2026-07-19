# Step 3 — QA 检查单（CLI 基线 vs web 端偏差验证）

> 审查对象：`demo/web/packages/xyncra-client-web`
> 基线：`demo/web/packages/xyncra-client-cli`
> 源矩阵：`_step2-matrix.md`（BUG-1/2/3、CON-1/2、EXP-1/2/3）
> 测试根：`src/__tests__/`

符号：`✅已覆盖` `⚠️部分` `❌缺失`

---

## ① review 检查单

| # | 偏差点 | 文件:行号 | 验证方法 | 验收标准（对照 CLI） | 需补测试 | 边界覆盖 |
|---|--------|-----------|----------|----------------------|----------|----------|
| **BUG-1a** | `hitl:question` 事件缺 `checkpoint_id/interrupt_id/agent_id` | `ReactUpdateHandler.ts:134` | 现有 `ReactUpdateHandler.test.ts:198` 仅断言 `{userId,conversationId,reason}`，**锁定错误契约**，无法复现缺失 | 事件须携带恢复所需 `question_id/checkpoint_id/interrupt_id/agent_id`（CLI `agent-resume.ts:59`） | ✅ 需改 `ReactUpdateHandler.test.ts` + 新增 `hitl-event-payload.test.ts` 断言完整字段 | ❌ 边界：timeout 未必有 question（CON-2），空字段未处理 |
| **BUG-1b** | `useHITL.answer()` 仅发 `{question_id, answer}` | `useHITL.ts:79` | `useHITL.test.ts:85`、`hitl-flow.test.ts:85` **断言错误契约**，固化 bug | `agent_resume` 须含 `conversation_id+checkpoint_id+interrupt_id+agent_id+answer` | ✅ 需改这两处 + 新增断言完整 5 字段 | ❌ `answer` 调用后未校验 server 回执；并发双 question 时第二个覆盖第一个（`pendingQuestion` 单值） |
| **BUG-1c** | `HITLDialog` 用 `userId` 作 questionID | `HITLDialog.tsx:27` | `HITLDialog.test.tsx` mock 了 `useHITL`，**未传真实 questionID**，无法复现误用 | `answer()` 收到真实 question 的 `id`，非 `userId` | ✅ 需改该测试，断言传入 `question.id` | ❌ 无 question.id 时调用直接崩溃 |
| **BUG-1d** | `useHITL` 订阅后未保留恢复字段 | `useHITL.ts:62` | 现有断言 `{userId,conversationId,question}`（`useHITL.test.ts:61`） | `pendingQuestion` 须含 `id/checkpoint_id/interrupt_id/agent_id` | ✅ 同 BUG-1a 测试 | ❌ 见 BUG-1b 并发覆盖 |
| **BUG-2** | `read:updated` 无订阅方 | `ReactUpdateHandler.ts:69`；`useMessages.ts`/`useConversations.ts` 未订阅 | 现有测试**无一处**对该事件做 UI 级断言（`ReactUpdateHandler.test.ts:106` 仅测 emit，未测消费） | `useConversations` 须订阅并更新 `lastReadMessageId`/未读计数（CLI 仅打印，web 须反映 UI） | ✅ 需新增 `useConversations.read.test.ts` 断言订阅后列表未读态变更 | ⚠️ 类型陷阱：`lastReadMessageId` 为字符串化数字（`sync-manager.ts:963`），比较需类型对齐——**现有无测试** |
| **BUG-3** | `conversation:updated`/`removed` 永不 emit | `ReactUpdateHandler.ts:76` | `useConversations.test.ts:117/146` 测了订阅者逻辑，但**无测试验证 handler 实际 emit**（handler 永远只发 `added`） | handler 须对 update/delete/restore 分别 emit 对应事件（CLI `onConversation` 仍被调用 `sync-manager.ts:971`） | ✅ 需改 `ReactUpdateHandler.test.ts` 增 `onConversation` 区分 action 用例 + `useConversations.remote-update.test.ts` | ❌ 远端 delete 后本地乐观更新的重复/竞态未覆盖 |
| **BUG-4** | 状态机空库卡 `syncing` | `XyncraProvider.tsx:311-315` | `ConnectionStatus.test.tsx:57` 仅渲染 `syncing` 文案，**无生命周期测试**触发 2s 空库路径 | 空库 2s 后须转 `connected`（CLI 由 daemon 维持，无此问题；web 需自愈） | ✅ 需新增 `XyncraProvider.lifecycle.test.tsx` 用 fake timers 走 2s 空库→syncing→connected | ❌ 重连后再次空库、网络中断恢复路径未覆盖 |
| **CON-1** | typing 不区分 agent/user | `ReactUpdateHandler.ts:89` | `ReactUpdateHandler.test.ts:126` 断言统一 `agent:thinking` | CLI 用 `isAgentUser` 区分文案；web 归并可接受（P2） | ❌ 不强制 | ⚠️ 普通用户 typing 误显为"思考态"——无测试 |
| **CON-2** | timeout 直接当 question | `ReactUpdateHandler.ts:134` | 同 BUG-1a | timeout 未必有可答 question，应区分信号 | ❌ 关联 BUG-1 | ❌ 无 question 的纯 timeout 未处理 |
| **EXP-1** | `error:rpc` 无订阅方 | `XyncraProvider.tsx:218` | 无测试消费该事件 | 至少应有 hook 可编程监听（P2，API 完整性） | ⚠️ 建议补 `useErrorRpc` 或文档 | ❌ retry 逻辑无测试 |
| **EXP-2** | 测试锁定错误 HITL 契约 | `useHITL.test.ts:85`、`hitl-flow.test.ts:85` | 即上述断言本身 | 修复 BUG-1 时同步改测试 | ✅ 必改 | — |
| **EXP-3** | builtin 函数未注册 | web 无 `registerBuiltinHandlers` | grep 确认，无测试 | TS-D-007 下 `get_device_info` 依赖 `node:os` 不可移植；待决策 | ❌ 不强制 | ❌ server 反向 RPC `ping` 无 handler（fail-open） |

---

## ② 测试覆盖结论

| 偏差点 | 覆盖度 | 说明 |
|--------|--------|------|
| BUG-1a/1b/1c/1d | ❌ 缺失（且测试反向固化 bug） | `useHITL.test.ts`、`hitl-flow.test.ts`、`ReactUpdateHandler.test.ts` 断言的是**错误契约** |
| BUG-2 | ❌ 缺失 | 仅测 emit，无 UI 消费测试；`useMessages`/`useConversations` 未订阅 |
| BUG-3 | ⚠️ 部分 | 订阅者逻辑有测试，但 handler emit 路径无测试（handler 永不发 updated/removed） |
| BUG-4 | ❌ 缺失 | 仅静态渲染 `syncing` 文案，无 2s 空库→connected 生命周期测试 |
| CON-1/2 | ❌ 缺失 | 无测试，但 P2 可接受 |
| EXP-1/3 | ❌ 缺失 | 无测试，P2/待决策 |

**测试覆盖缺口数量：9 个偏差点中 7 个缺失/反向固化（BUG-1×4、BUG-2、BUG-3 emit 路径、BUG-4、EXP-2 固化），仅 BUG-3 订阅者侧部分覆盖。**

建议新增/修改测试文件清单：
1. `src/__tests__/internal/ReactUpdateHandler.test.ts`（改：增 `onConversation` action 分支、改 `onAgentTimeout` 断言完整字段）
2. `src/__tests__/hooks/useHITL.test.ts`（改：断言 `agent_resume` 完整 5 字段 + `pendingQuestion` 含恢复字段）
3. `src/__tests__/integration/hitl-flow.test.ts`（改：同上）
4. `src/__tests__/components/HITLDialog.test.tsx`（改：传入真实 `question.id` 并断言）
5. `src/__tests__/hooks/useConversations.read.test.ts`（新：订阅 `read:updated` 后未读态变更 + 数字型 id 对齐）
6. `src/__tests__/hooks/useConversations.remote-update.test.ts`（新：远端 update/delete/restore 经事件刷新）
7. `src/__tests__/context/XyncraProvider.lifecycle.test.tsx`（新：2s 空库→syncing→connected 自愈）
8. `src/__tests__/integration/hitl-event-payload.test.ts`（新：端到端 `hitl:question` 携带恢复字段）

---

## ③ 边界遗漏清单（web 相对 CLI 错误/边界处理）

| 类别 | 遗漏点 | 对照 CLI 行为 |
|------|--------|---------------|
| HITL 并发 | 多个顺序 question 时 `pendingQuestion` 单值覆盖，第二个 question 丢失恢复上下文（BUG-1b/d） | CLI `agent_resume` 按 question 独立处理，无单值限制 |
| HITL 空数据 | `onAgentTimeout` 无 question 时仍弹 HITLDialog，server 无对应 question 记录（CON-2） | CLI 仅打印 `[agent_timeout]`，不强制弹窗 |
| 已读类型 | `lastReadMessageId` 为字符串化数字，UI 比较可能不等（BUG-2 备注） | CLI 仅打印，无比较 |
| 状态机空库 | 空库 2s 后卡 `syncing`，无机制再转 `connected`（BUG-4） | CLI 由 daemon 维持，无 UI 状态机 |
| 重连/网络中断 | 无测试覆盖 `syncing` 期间断网、重连后再次空库 | CLI daemon 自动重连，web 无验证 |
| 远端删除竞态 | 远端 `conversation:removed` 后本地乐观 delete 的重复/闪烁（BUG-3） | CLI 单一 stdout，无乐观更新竞态 |
| RPC 错误 | `error:rpc` 仅 antd toast，无 hook 订阅，retry 不可编程（EXP-1） | CLI IPC 有显式错误返回 |
| 反向 RPC | builtin `ping`/`get_device_info`/`get_time` 未注册，server 调用无 handler（EXP-3） | CLI 自动注册（TS-D-007 下 web 可豁免，但无失败可见性） |

---

## ④ 修复验证门禁（供步骤 4）

- BUG-1：新增测试断言 `hitl:question` 含 `checkpoint_id/interrupt_id/agent_id` 且 `answer()` 发完整 5 字段；`HITLDialog` 传 `question.id`。
- BUG-2：`useConversations` 订阅 `read:updated` 后列表未读计数更新；数字型 id 比较通过。
- BUG-3：`ReactUpdateHandler.onConversation` 按 action emit `added/updated/removed`，远端删除后 UI 列表长度变化。
- BUG-4：2s 空库场景最终 `connectionStatus==='connected'`。
- EXP-2：旧错误断言全部移除/改正。
