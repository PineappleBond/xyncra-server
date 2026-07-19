# 步骤 3 QA 验证策略 — D-130（P0）与 AgentSelector 测试（P1）

**子代理**：3（QA 工程师）
**项目根**：/Users/leichujun/go/src/github.com/PineappleBond/xyncra-server
**审查对象**：demo/web/packages/xyncra-client-web
**上游输入**：_step2-architecture-plan.md（最终修复集）、_step1-code-archaeology.md

---

## 进度跟踪（TodoWrite）

| # | 任务 | 状态 |
|---|------|------|
| T1 | D-130 测试场景设计 | ✅ 完成 |
| T2 | AgentSelector 测试场景设计 | ✅ 完成 |
| T3 | 回归保护用例梳理 | ✅ 完成 |
| T4 | 环境要求确认 | ✅ 完成 |
| T5 | 验收标准制定 | ✅ 完成 |

---

## 5.1 测试场景清单（按修复项分组）

### A. D-130（P0）— 2s 空库兜底 `connected` → `syncing`

**改动文件**：`src/context/XyncraProvider.tsx:313-317`（兜底 `setConnectionStatus('syncing')`）、`:309-312` 注释、`:70` 类型文档注释。

**A.1 正常路径**
- A.1.1 空库场景：挂载 Provider 后用 `jest.useFakeTimers()` 冻结定时器，`start()` 返回 pending Promise（无数据）。advanceTimersByTime(2000) 后断言 `connectionStatus === 'syncing'`。
- A.1.2 非空库场景：挂载后 emit `message:added`（或 `conversation:added`），断言状态在定时器触发前即转为 `'connected'`，且 2s 兜底被 clearTimeout（不再翻转为 syncing）。
- A.1.3 UI 展示：向 `ConnectionStatus` 注入 `connectionStatus='syncing'`，断言渲染文本 `'同步中...'` 且 Badge status=`'processing'`（`ConnectionStatus.test.tsx` 已含该用例，需保持绿）。

**A.2 边界**
- A.2.1 首数据恰好在第 1999ms 到达：setTimeout 前 1ms 触发 message 事件，断言最终 `'connected'`，且 2000ms 兜底触发时不覆盖（firstDataReceived 守卫）。
- A.2.2 2s 兜底后状态持久：advanceTimersByTime(2000) 后再次 advanceTimersByTime(5000)，断言仍为 `'syncing'`（无二次回调重置）。
- A.2.3 空库永不收消息：状态停留 `'syncing'`，符合 D-130 语义（已握手但无本地数据），非卡死。

**A.3 错误路径**
- A.3.1 `start()` reject：断言状态为 `'disconnected'`（既有逻辑不被 D-130 改动影响）。
- A.3.2 兜底期间卸载：unmount 后 advanceTimersByTime(2000)，断言无内存泄漏 / 无对已卸载组件 setState 警告（React 警告监控）。

### B. AgentSelector（P1）— 过期测试修正

**改动文件**：`src/__tests__/components/AgentSelector.test.tsx:56, :88, :95`。

**B.1 正常路径**
- B.1.1 渲染 header：断言 `'Agents'` 存在（保持通过）。
- B.1.2 展示默认 agents：断言 `'Test Bot'` `'Weather Bot'` `'HITL 测试助手'` `'HITL Parent'` 四个 DEFAULT_AGENTS.name 均渲染。
- B.1.3 点击首个 agent：点击 `list-item[0]`，断言 `onSelect` 被调用且参数为 `'test-bot'`。

**B.2 边界**
- B.2.1 默认列表非空边界：移除测试 setup 中的 `agentID='test-agent'` 依赖（组件忽略 context.agentID，改用 DEFAULT_AGENTS），断言组件不依赖 context.agentID 渲染。
- B.2.2 selectedAgentID 高亮：传入 `selectedAgentID='test-bot'`，断言对应 `list-item` 背景样式为 `'#e6f7ff'`。
- B.2.3 空列表边界（防御性）：若未来 DEFAULT_AGENTS 为空，断言仅渲染 header 且不抛错（当前列表非空，本条为回归护栏，可选）。

**B.3 错误路径**
- B.3.1 onSelect 未提供：不传 onSelect，点击不抛 TypeError（守卫）。
- B.3.2 点击非首个 agent：点击 `list-item[2]`，断言 `onSelect('hitl-bot')`。

### C. 回归保护（D-130 改动不得破坏现有状态机测试）

**需保护的现有用例**（来源 `src/__tests__/context/XyncraProvider.branches.test.tsx`）：
- C.1 `should set connecting status on mount`（:82）— 断言 `'connecting'|'disconnected'`，不受兜底改动影响。
- C.2 `should set disconnected when client.start() resolves`（:97）— 注意：该用例 `start` resolve 后断言 `'disconnected'`，**与 D-130 的 2s 兜底逻辑独立**（resolve 路径不触发 setTimeout 兜底）。回归时需确认 D-130 改动仅作用于兜底分支，不触达 resolve 后状态。
- C.3 `should set disconnected when client.start() rejects`（:115）— 同上，reject 不走兜底。
- C.4 `should set connected when first message event arrives`（:133）— 验证 message:added 仍触发 `'connected'`（A.1.2 强化）。
- C.5 `should call stop on unmount`（:184）— unmount 行为不变。

**集成/单元回归**（grep 关联）：
- `integration/hitl-flow.test.ts` — CON-2 维持后 HITL 恢复链路仍全绿（useHITL 回查逻辑）。
- `components/ConnectionStatus.test.tsx` — `'syncing'` 渲染用例（:57）必须保持通过（A.1.3 依赖）。
- `hooks/useAgentStatus.test.ts`、`ReactUpdateHandler` 相关测试 — CON-1 不修，断言 `agent:thinking` 不变。

---

## 5.2 环境要求

| 项目 | 要求 | 备注 |
|------|------|------|
| 测试框架 | Jest（Umi Max 配置，`target:'browser'`，jsdom） | 运行 `npx jest packages/xyncra-client-web/` 或 `npx jest AgentSelector.test.tsx` |
| DOM 环境 | jsdom（URL `http://localhost:8000`） | 由 `jest.config.ts` baseConfig 提供 |
| 定时器 mock | `jest.useFakeTimers()` + `jest.advanceTimersByTime` | A 系列必须；注意 React 18 `act()` 包裹 |
| WebSocket mock | 复用 `XyncraProvider.test.tsx` 的 `jest.mock('../../adapters/websocket', ...)` | `BrowserWebSocketFactory.create` 返回 stub |
| IndexedDB mock | 复用 `jest.mock('../../adapters/indexeddb', ...)` | `getIDBFactory` 返回 stub；空库场景无需真实 DB 数据 |
| antd mock | AgentSelector 测试已局部 mock antd（Avatar/List/Typography） | ConnectionStatus 已 mock 为 `'status'` 占位 |
| localStorage | `tests/setupTests.jsx` 提供 localStorageMock | 全局 setupFiles |
| 关键约束 | **不使用真定时器**：D-130 2s 兜底必须 fake timers，否则测试超时 | 现有 C 系列用 `setTimeout(r,10)` 真实等待，D-130 新用例改用 fake |

**环境风险识别**（见 §5.4）：
1. **fake timers 与 React 18 act 交互**：`advanceTimersByTime` 需包在 `act()` 内，否则状态更新不 flush，断言读到旧值。建议封装 `await act(async () => jest.advanceTimersByTime(2000))`。
2. **Provider 内部 `useEffect` 中 setTimeout 在 unmount 时是否 clear**：现有代码未对 connectionTimeout 做 cleanup（仅 markConnected 内 clear），卸载后兜底仍会触发 setState → React 警告。A.3.2 需监控 console.error。
3. **jsdom 无真实 WebSocket**：依赖 mock 工厂，不可在 D-130 测试中实例化真 WS。

---

## 5.3 验收标准

| 编号 | 标准 | 通过条件 | 覆盖率期望 |
|------|------|----------|-----------|
| ACC-1 | D-130 空库兜底 | `npx jest XyncraProvider` 含新用例：2s 后 `'syncing'`，UI 显示 `'同步中...'` | `XyncraProvider.tsx` 状态机分支 100%（connecting/syncing/connected/disconnected 全断言） |
| ACC-2 | D-130 非空库 | 首数据到达即 `'connected'`，2000ms 兜底被 clear 不覆盖 | 同上 |
| ACC-3 | AgentSelector 绿态 | `npx jest AgentSelector.test.tsx` **3/3 通过**（Test Bot / test-bot / Agents） | `AgentSelector.tsx` 行覆盖 ≥95% |
| ACC-4 | 全包回归 | `npx jest packages/xyncra-client-web/` 目标 **195/195**（修复 2 过期用例） | 维持现有覆盖率不下降 |
| ACC-5 | CON-2 维持无回归 | `hitl-flow.test.ts` 全绿，HITL 恢复链路未被破坏 | 维持 |
| ACC-6 | 类型安全 | `tsc --noEmit` 通过，`syncing` 已是联合类型成员（ConnectionStatus 已处理），无新增 `any` | 0 类型错误 |
| ACC-7 | 注释/文档 | `:309-312` 与 `:70` 注释更新为 D-130 语义（"shows 'syncing' per D-130"），不残留 "stuck in 'syncing' forever" | 人工 review |

**交付门槛**：ACC-1~ACC-4 为强制（P0+P1 验收），ACC-5~ACC-7 为质量护栏。

---

## 5.4 环境风险小结

| 风险 | 等级 | 缓解 |
|------|------|------|
| fake timers + act 不 flush | 中 | 所有 advanceTimersByTime 包 act() |
| unmount 后兜底 setState 警告 | 低（现有缺陷，非本次引入） | A.3.2 监控 console.error，建议子代理 4 顺带补 cleanupEffect |
| jsdom 无 WS | 低 | 全程 mock 工厂，不实例化真 WS |
| 真实定时器导致超时 | 中 | D-130 用例强制 useFakeTimers，禁止真实等待 |

> 结论：环境风险可控，无阻塞性风险；唯一需在 D-130 修复时同步注意的是 unmount 清理（已在 T4 标注，建议作为 P2 后续项）。
