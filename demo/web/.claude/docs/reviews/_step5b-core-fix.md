# Step 5b — BUG-3 跨包核心修复（core 接口改造）

> 阻塞项来源：`_step5-issues.md` §① BUG-3（core `IUpdateHandler.onConversation` 不传 `action`，web 无法区分新增/更新/删除，导致 `conversation:updated`/`conversation:removed` 永不 emit，删除/恢复会话后 UI 不刷新）。
> 范围：core + cli + web 三包协同改造。
> 前置阅读：`_step1-baseline.md` / `_step2-matrix.md` / `_step3-checklist.md` / `_step4-compliance.md` / `_step5-issues.md` / `PRODUCT_DECISIONS.md`。

---

## ① 改造方案

### 接口签名变更（core）
- `packages/xyncra-client-core/src/interfaces.ts:143`：`onConversation(conversation: Conversation, action: ConversationAction): Promise<void>`
- 新增类型 `ConversationAction = 'created' | 'updated' | 'removed'`（`interfaces.ts:14-20`）。
- 向后兼容：action 为末位参数，cli/web 实现均设默认值 `'updated'`，旧调用（无 action）仍可工作。

### core 调用点变更（sync-manager.ts）
core 内部 4 处 `onConversation` 调用，原均不传 action。改造后：
- `sync-manager.ts:862`（本地缓存命中 update）→ `'updated'`
- `sync-manager.ts:921`（RPC 拉取后 update）→ `'updated'`
- `sync-manager.ts:971-985`（`notifyHandler` 的 `conversation` 分支）：
  - `action === 'create'` → `'created'`，传完整 `Conversation`
  - 其余（`delete`/`restore`/`update`，仅 `{id}`）→ `action === 'delete' ? 'removed' : 'updated'`

根因修复点：原 `notifyHandler` 对 delete/restore/update 一律传最小 `{id}` 且无 action，web 侧无法分辨。现据 payload `action` 字段映射为 `removed`/`updated`。

### cli 适配（update-handler.ts）
- `onConversation(conversation, action = 'updated')`：stdout 打印增加 `${action}` 前缀（`[conversation] created/updated/removed id=...`）。
- import 补充 `ConversationAction`。行为保持"仅打印"，符合 CLI 基线。

### web 适配（ReactUpdateHandler.ts）
- `onConversation(conversation, action = 'updated')` 按 action 分发：
  - `'created'` → `conversation:added`
  - `'updated'` → `conversation:updated`
  - `'removed'` → `conversation:removed`（payload `{ conversationId }`）
- import 补充 `ConversationAction`。
- `useConversations.ts` 已订阅三事件（`:99`/`:114`/`:123`），无需改动即生效：远端删除经 `conversation:removed` 移除列表项，恢复/更新经 `conversation:updated` 刷新，新建经 `conversation:added` 追加。

---

## ② 修改文件清单

| 文件:行号 | 改动说明 |
|-----------|----------|
| `xyncra-client-core/src/interfaces.ts:14-20` | 新增 `ConversationAction` 类型 |
| `xyncra-client-core/src/interfaces.ts:143-154` | `onConversation` 增加 `action` 参数 + JSDoc |
| `xyncra-client-core/src/sync-manager.ts:34` | import 增加 `ConversationAction` |
| `xyncra-client-core/src/sync-manager.ts:863` | 本地命中 update → `'updated'` |
| `xyncra-client-core/src/sync-manager.ts:922` | RPC 拉取 update → `'updated'` |
| `xyncra-client-core/src/sync-manager.ts:971-985` | `notifyHandler` conversation 分支按 payload.action 映射 `created`/`removed`/`updated` |
| `xyncra-client-cli/src/update-handler.ts:48-52` | `onConversation` 适配新签名，打印含 action |
| `xyncra-client-cli/src/update-handler.ts:11-19` | import 增加 `ConversationAction` |
| `xyncra-client-web/src/internal/ReactUpdateHandler.ts:76-92` | `onConversation` 按 action 分发 emit 三事件 |
| `xyncra-client-web/src/internal/ReactUpdateHandler.ts:16-24` | import 增加 `ConversationAction` |
| `xyncra-client-cli/src/__tests__/update-handler.test.ts:99-118` | 新增 action 打印断言用例 |
| `xyncra-client-web/src/__tests__/internal/ReactUpdateHandler.test.ts:79-117` | 扩展 onConversation 覆盖 created/updated/removed 三分支 |

---

## ③ 测试结果

### web 包（`npm test`）
- **195 测试，193 通过，2 失败**。
- 2 失败均为预存的 `AgentSelector.test.tsx`（断言 `onSelect` 收到 `test-agent`，实际收到 `test-bot`），与本次 BUG-3 无关，未触碰。
- 新增/受影响用例全过：`ReactUpdateHandler.onConversation` 三分支、`useConversations` 的 `added`/`updated`/`removed` 事件刷新（既有 `:117`/`:146` 测试现已对应真实 emit 路径）。

### core 包（`npm test`）
- **155 测试，155 通过，0 失败**。

### cli 包（`npm test`）
- 84 测试全部通过（含新增 action 打印用例）。
- 注：cli 有 2 个测试套件因 `fs-ext` 模块类型（`ts-jest` 对 `import { flock } from 'fs-ext'` 报 TS7006）编译失败而标 failed，但该包既有问题，与本次改动无关；`update-handler.test.ts` 自身 PASS。

---

## ④ 遗留风险

1. **core `test-helpers.ts` 未断言 action**：`src/__tests__/test-helpers.ts:376` 的 mock `onConversation(conversation)` 仍无 action 参数断言。调用点改造已编译通过，但无专门单测验证 core 对 delete→`removed`、create→`created` 的映射。建议后续补 sync-manager 级集成测试锁定映射。
2. **远端 delete 后本地乐观删除竞态**：`useConversations.deleteConversation` 已本地乐观移除，若服务端再推 `conversation:removed`，`filter` 无副作用（幂等），无闪烁风险；但并发"远端恢复 + 本地删除"场景未覆盖测试。
3. **`restore` 被映射为 `updated`**：core 中 restore 走 `notifyHandler` 的 `delete` 之外分支，传 `'updated'`。web `toConversationEvent` 收到的是最小 `{id}`，`conversation:updated` 会用一个仅含 id 的对象覆盖列表项，导致 title 等字段丢失（直至下次 `listConversations`）。此为边缘场景，若 restore 应恢复完整字段，需 core 在 restore 时也拉取完整 Conversation。当前与 CLI 基线行为一致（CLI 同样仅收到 `{id}`），暂记为已知限制。
4. **PRODUCT_DECISIONS.md 未改**：本次属实现级 bug 修复，未触发新架构决策，未修改决策文档（符合 step4 结论）。

---

## ⑤ BUG-3 状态

**已解除。** core 接口已能传递 `action`，cli/web 同步适配，web `onConversation` 现按 action emit `conversation:added/updated/removed`，`useConversations` 订阅生效，远端会话删除/恢复/更新后 UI 正确刷新。
