# 步骤 7 实现摘要 + 步骤 8 测试 — Xyncra Web 客户端

**子代理**：实现子代理（步骤 7 + 8）
**项目根**：/Users/leichujun/go/src/github.com/PineappleBond/xyncra-server
**日期**：2026-07-19

---

## 1. 实现清单与改动明细

### A. P0 — D-130 根因修复
| 项 | 文件:行 | 改动 |
|----|---------|------|
| 兜底状态 | `demo/web/packages/xyncra-client-web/src/context/XyncraProvider.tsx:313-317` | `setConnectionStatus('connected')` → `setConnectionStatus('syncing')` |
| 兜底注释 | `XyncraProvider.tsx:309-312` | 注释改为 "Per D-130, we show 'syncing' rather than a fake 'connected'…" |
| 类型注释 | `XyncraProvider.tsx:66-77` | `ConnectionStatus` 类型补充 `syncing` 由空库 2s 兜底触发（D-130）说明 |

验证：`ConnectionStatus.tsx:23` STATUS_MAP 已有 `syncing:{processing,'同步中...'}`，UI 零新增未处理分支（已在 _step2 核实，未改 UI）。

### B. P1 — AgentSelector 测试过期修复
| 项 | 文件:行 | 改动 |
|----|---------|------|
| 断言文案 | `src/__tests__/components/AgentSelector.test.tsx:93-99` | 原 `getByText('AI 助手')` → 断言 4 个 DEFAULT_AGENTS.name（Test Bot/Weather Bot/HITL 测试助手/HITL Parent） |
| 点击参数 | `AgentSelector.test.tsx:101-106` | `toHaveBeenCalledWith('test-agent')` → `toHaveBeenCalledWith('test-bot')` |
| setup 修正 | `AgentSelector.test.tsx:2,9-53` | mock 工厂内 `React` 改 `mockReact`（jest 不允许工厂引用 out-of-scope 变量，否则 suite failed to run）；`agentID` 默认值改 `'test-bot'` 并加注释说明组件忽略 context.agentID |

**未改组件**（`AgentSelector.tsx` 原样保留）。

### C. 新增 D-133 决策（固化 CON-2）
| 项 | 文件:行 | 改动 |
|----|---------|------|
| 决策表 | `docs/decisions/PRODUCT_DECISIONS.md:63` | 概览表追加 `D-133 | Web Agent 超时复用 hitl:question 事件 | 维持 hitl:question 映射，不拆独立 agent:timeout，与 075 HITL 恢复链路一致` |

理由见 _step4-compliance-review.md §6.3 建议 2。未改 D-130 原文（_step4 裁定无需更新）。

### D. 步骤 8 测试（D-130 新增用例）
| 项 | 文件:行 | 改动 |
|----|---------|------|
| 空库兜底测试 | `src/__tests__/context/XyncraProvider.test.tsx`（追加，用 `jest.useFakeTimers` + `act`） | 新增 `should show syncing on the 2s empty-database fallback (D-130)`：挂载后断言 `connecting`，advanceTimersByTime(2000) 后断言 `syncing` |

---

## 2. 编译结果

```
npx tsc --build packages/xyncra-client-core/tsconfig.json --force   → 通过（无输出）
npx tsc --build packages/xyncra-client-web/tsconfig.json --force     → 通过（无输出）
```
D-130 改动未引入类型错误；`syncing` 已是联合类型成员，无新增 `any`。

---

## 3. 测试验证结果

### 3.1 项目默认 jest 配置（Umi babel esbuild transformer）现状
项目默认 `npm test`（jest.config.ts/js）在**当前本地环境**下，`packages/xyncra-client-web` 的 **所有 `.test.tsx`/含 `import type` 的 `.test.ts` 均 suite failed to run**（报 `React is not defined` / `Cannot transform the imported binding "XyncraContextValue"`）。此为**既有环境/工具链问题**：

- **baseline（改动前，同默认配置）**：51 suites failed / 11 passed，Tests 58 failed / 123 passed / 181 total。
- **改动后（同默认配置）**：51 suites failed / 11 passed，Tests 59 failed / 123 passed / 182 total。
- 失败数仅 +1（即我新增的测试文件也因同一 babel 转换问题无法运行），**无任何既有通过测试因我的改动转红**。

### 3.2 用 ts-jest 隔离验证（绕过 babel 工具链问题，仅本地校验逻辑）
临时以 `ts-jest` + jsdom + polyfill（TextEncoder/crypto）运行 `src` 测试，对比 baseline：

| 场景 | Test Suites | Tests |
|------|-------------|-------|
| baseline（stash 我的 4 文件） | 8 failed / 23 passed / 31 | 25 failed / 141 passed / 166 |
| **我的改动后** | **7 failed / 24 passed / 31** | **23 failed / 144 passed / 167** |

→ 我的改动**减少 2 失败、增加 3 通过**（净改善）。新增 1 个 total（我的 D-130 测试，已通过）。

### 3.3 关键目标用例（ts-jest 隔离）
| 用例 | 结果 |
|------|------|
| AgentSelector 3/3（header / 4 默认 agents / onSelect('test-bot')） | ✅ PASS |
| ConnectionStatus `syncing` 渲染（既有用例） | ✅ PASS |
| 新增 D-130 空库兜底 `syncing` | ✅ PASS |
| useHITL / hitl-flow（CON-2 维持无回归） | ✅ 相关 suite 在隔离配置下通过 |

### 3.4 剩余失败（非本次引入，环境/既有）
失败 suite 集中于 `integration/*`（websocket-lifecycle/message-flow，依赖真实 WS/网络）、`adapters/websocket`、`hooks/useConversations`/`useMessages`（复杂 IndexedDB/ConnectionManager 场景）、`XyncraProvider.test.tsx` 中 `auto-generate deviceID` 用例（jsdom 下 `crypto.randomUUID` 行为差异）。均与 D-130 空库兜底分支、AgentSelector 组件测试无关，且 baseline 同样失败。

---

## 4. 遗留 / 新问题

- **遗留（环境级，非代码）**：项目默认 jest 配置（Umi babel transformer）在当前 Node/jest 30 环境下无法转换 web 包的 tsx/类型导入测试，导致 51 suites 全红。这是**预存在**故障（baseline 已如此），超出"仅改测试/组件"的修复范围，建议由构建/CI 负责方单独排查 babel 配置或升级 Umi 版本。
- **新问题**：无。我的改动未引入任何新失败；D-130 与 AgentSelector 目标验收均通过。

---

## 5. 验收对照（ACC，见 _step3）
| 编号 | 标准 | 状态 |
|------|------|------|
| ACC-1 | D-130 空库兜底 `syncing` | ✅（ts-jest 隔离验证通过） |
| ACC-2 | D-130 非空库首数据 `connected` 且兜底 clear | ✅（代码逻辑保留 markConnected 守卫） |
| ACC-3 | AgentSelector 3/3 | ✅ |
| ACC-4 | 全包回归至 195/195 | ⚠️ 受环境 babel 故障阻塞，无法在默认配置下达成；隔离验证显示净改善、无新增失败 |
| ACC-5 | CON-2 维持无回归（hitl-flow） | ✅ |
| ACC-6 | 类型安全 tsc | ✅ |
| ACC-7 | 注释更新 | ✅ |
