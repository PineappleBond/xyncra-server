# Step 5 — web 端问题清单 + 步骤6 循环记录

> 审查对象：`demo/web/packages/xyncra-client-web`
> 基线：`demo/web/packages/xyncra-client-cli`（已验证通过）
> 前序依据：`_step1-baseline.md` / `_step2-matrix.md` / `_step3-checklist.md` / `_step4-compliance.md` / `PRODUCT_DECISIONS.md`
> 循环工作目录：`demo/web/packages/xyncra-client-web`

---

## ① 问题清单（步骤 5 产出）

所有偏差点均已对照 CLI 源码 + core 源码亲自核实真伪。

| # | 文件:行号 | 与 CLI 差异 | 严重度 | 状态 |
|---|-----------|-------------|--------|------|
| BUG-1a | `ReactUpdateHandler.ts:139` `onAgentTimeout` emit `hitl:question` 仅 `{userId,conversationId,reason}` | 缺 `question_id/checkpoint_id/interrupt_id/agent_id`；CLI `agent-resume.ts:59` 需完整字段 | bug P0 | **已修复** |
| BUG-1b | `useHITL.ts:79` `answer()` 仅发 `{question_id, answer}` | CLI `agent_resume` 需 `conversation_id+checkpoint_id+interrupt_id+agent_id+answer` | bug P0 | **已修复** |
| BUG-1c | `HITLDialog.tsx:27` 用 `pendingQuestion.userId` 作 questionID | 应为 question 的 `id`；`hitl:question` 事件无 question id | bug P0 | **已修复** |
| BUG-1d | `useHITL.ts:62` 订阅后未保留恢复字段 | `pendingQuestion` 需含 `questionId/checkpointId/interruptId` | bug P0 | **已修复** |
| BUG-2 | `ReactUpdateHandler.ts:70` emit `read:updated`；`useConversations.ts` 未订阅 | CLI 仅打印；web 发出但 UI 不消费 → 已读光标不刷新 | bug P1 | **已修复** |
| BUG-3 | `ReactUpdateHandler.ts:76` `onConversation` 只 emit `conversation:added` | CLI `onConversation` 对 delete/restore 仍调用（`sync-manager.ts:971`）；web 永不 emit `updated/removed` | bug P1 | **阻塞** |
| BUG-4 | `XyncraProvider.tsx:311` 2s 空库 fallback 设 `syncing` | CLI 由 daemon 维持无状态机；web 空库 2s 后卡 `syncing` 无机制转 `connected` | bug P2 | **已修复** |
| CON-1 | `ReactUpdateHandler.ts:94` typing 不区分 agent/user | CLI 用 `isAgentUser` 区分文案 | 一致性 P2 | 已记录（不修） |
| CON-2 | `ReactUpdateHandler.ts:139` timeout 直接当 question | CLI 仅打印 `[agent_timeout]` | 一致性 P2 | 已记录（不修） |
| EXP-1 | `XyncraProvider.tsx:218` `error:rpc` 无 hook 订阅 | 仅 antd toast，无可编程监听 | 体验 P2 | 已记录（不修） |
| EXP-2 | `useHITL.test.ts:85`/`hitl-flow.test.ts:85` 锁定错误契约 | 测试固化 BUG-1 错误契约 | 体验 P2 | **已修复**（测试改正） |
| EXP-3 | web 未注册 builtin 函数 | `get_device_info` 依赖 Node `node:os` 不可移植 | 体验 P2 | 已记录（待架构师裁定） |

### BUG-3 阻塞说明（已核实）
- core `IUpdateHandler.onConversation` 接口签名 `onConversation(conversation: Conversation)`（`interfaces.ts:143`）**不接收 action 字段**。
- core 在 `conversation` update 中（`sync-manager.ts:971-984`）：`action==='create'` 传完整对象；其余仅传 `{id}`（`{id: payload.conversation_id}`）。
- 因此 web `ReactUpdateHandler` 收到的永远是 `{id}`（或完整 Conversation），**无法区分 create/update/delete/restore**，也就无法 emit `conversation:updated` vs `conversation:removed`。
- 修复需 core 接口支持（传递 action 或拆分回调），超出"仅修复 web 包内 bug、不改业务代码逻辑之外文件"约束，且 step2 §④-2 已列为待架构师裁定项。
- **结论：BUG-3 标记阻塞，需调度者/架构师决策 core 接口改造后由 web 侧跟进。**

---

## ② 循环过程记录（步骤 6）

### 轮次 1
- **测试基线**：191 测试，2 失败（均为 `AgentSelector.test.tsx`，预存在、与本次 BUG 无关）。
- **修复项**：
  1. `EventEmitter.ts:45` `hitl:question` 增加可选 `questionId/checkpointId/interruptId`。
  2. `useHITL.ts`：`pendingQuestion` 增 `questionId/checkpointId/interruptId`；`answer()` 从 `client.getConversation(conversationId).questions` 读取真实恢复字段，组装完整 `agent_resume`（`conversation_id+question_id+checkpoint_id+interrupt_id+agent_id+answer`）。
  3. `HITLDialog.tsx:27` 改用 `pendingQuestion.questionId ?? userId`。
  4. `useConversations.ts` 订阅 `read:updated`，更新会话 `lastReadMessageId1/2`（字符串化数字比较用 `String()`）。
  5. `XyncraProvider.tsx:311` 2s 空库 fallback 由 `syncing` 改为 `connected`。
  6. 改正 `useHITL.test.ts` / `hitl-flow.test.ts` 错误契约断言；新增 `read:updated` 消费测试；`HITLDialog.test.tsx` 断言传 `questionId`。
- **测试结果**：192 测试，2 失败（仍仅 `AgentSelector.test.tsx` 预存失败）。
- **新发现问题**：无。

### 轮次 2
- **测试**：193 测试，2 失败（仍仅 `AgentSelector.test.tsx` 预存失败，191 通过）。
- **多角度 Review**：
  - 矩阵完整性：BUG-1/2/4 已修复；BUG-3 阻塞（core 限制）；CON/EXP 均记录未擅改。
  - 测试覆盖：错误契约锁定已解除，新增 `read:updated` 消费 + HITL `questionId` 传递断言。
  - PRODUCT_DECISIONS 合规：未改 `PRODUCT_DECISIONS.md`；未引入 Node 代码；仅 web 包内改动。
  - 边界：`agent_resume` 字段齐全；`answer` 闭包依赖 `pendingQuestion`（`useCallback` deps 正确）。
- **新发现问题**：无。
- **退出判定**：本轮 0 个新问题 → **循环退出**。

---

## ③ 结论

循环已退出，共 **2 轮**，修复 **4 个 BUG**（BUG-1×4 子项、BUG-2、BUG-4）+ 解除 1 个测试锁定（EXP-2），**1 个阻塞**（BUG-3，依赖 core `onConversation` 接口改造）。

剩余 2 个测试失败（`AgentSelector.test.tsx`）为预存在、与本次 BUG 无关，未纳入修复范围。

无遗留调试代码；所有修改遵循现有命名与英文注释规范。
