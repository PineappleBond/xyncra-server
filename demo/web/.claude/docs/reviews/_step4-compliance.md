# Step 4 — 产品经理合规审查（CLI 基线对照）

> 审查对象：`demo/web/packages/xyncra-client-web` + 集成点 `demo/web/src/`
> 基线：`demo/web/packages/xyncra-client-cli`
> 前置依据：`_step1-baseline.md`、`_step2-matrix.md`、`_step3-checklist.md`
> 判定标准：`demo/web/docs/decisions/PRODUCT_DECISIONS.md`

---

## ① 合规性结论表

| 决策条目 | 合规判定 | 证据 / 文件:行号 | 备注 |
|---------|---------|------------------|------|
| **TS-D-001** 多包架构 / 浏览器不引入 Node 代码 | ✅ 合规 | `adapters/*.ts`、`context/XyncraProvider.tsx`、`hooks/*`、`components/*` 均无 `ws`/`node:`/`fs`/`path`/`os`/`child_process`/`net`/`daemon`/`ipc`/`flock` 引入（grep 全文 0 命中）；仅使用浏览器全局 `window`/`document`/`globalThis.localStorage`/`navigator`。`app.tsx:155` 的 `process.env.XYNCRA_WS_URL` 属 demo 前端项目（非 web 包），不违反。 | 无 Node 代码泄漏，架构边界清晰。 |
| **TS-D-003** Dexie.js + fake-indexeddb 存储层（web 用原生 IndexedDB） | ✅ 合规 | `adapters/indexeddb.ts:23-27` `BrowserIndexedDBProvider.getIDBFactory()` 仅返回浏览器原生 `indexedDB`，无 CRUD、无 Dexie 实例、无 Node polyfill。 | web 端按设计只暴露 factory，落地符合。 |
| **TS-D-007** 浏览器内嵌模式 / 无 IPC 层 | ✅ 合规 | `adapters/websocket.ts:168-246` `CoreWebSocketBridge` 用浏览器原生 `new WebSocket(url)` 实现 core `IWebSocket`；`BrowserWebSocketFactory.create` 注入（`:259`）。全包无 IPC socket / daemon 代码（grep 0 命中）。 | 与 CLI 的 Unix socket 主路径形成合理差异（matrix §①-10 OK-3）。 |
| **TS-D-008** npm workspace 子包 | ✅ 合规 | web 包位于 `packages/xyncra-client-web`，经 `@xyncra/client-web` scope 在 `app.tsx:13` 被 demo 前端引用。 | — |
| **TS-D-012** `--db-path` 语义为 IndexedDB 库名（web 无该 flag） | ✅ 合规 | web 端无 `--db-path` flag；库名由 deviceID 派生（`XyncraProvider.tsx:130` `resolveDeviceID` 用 `crypto.randomUUID`+`globalThis.localStorage` 持久化，D-5）。 | web 库名通过注入的 `BrowserIndexedDBProvider` 间接确定，无 flag 暴露，语义不冲突。 |
| **TS-D-002** 环境无关核心（core 零环境假设） | ✅ 合规 | web 包仅实现并注入接口（`IWebSocketFactory`/`IIndexedDBProvider`/`IUpdateHandler`/`ILogger`），未改动 core。 | 抽查未触及 core，但 web 侧注入方式符合 D-2 约束。 |
| **TS-D-005/006/009/010/011** 其余条目 | ➖ 不适用 | 均为 CLI/协议层/Go 兼容相关，web 端不涉及 IPC/fs-ext/scope 等，无需核验。 | — |

**结论：未发现违反 PRODUCT_DECISIONS 的硬性违规。** 全部已核条目（TS-D-001/002/003/007/008/012）均合规，web 端正确保持了"浏览器进程内运行、无 IPC、无 Node 代码、存储仅暴露 IndexedDB factory"的架构边界。

---

## ② 开发者体验问题清单

### 导出契约（`index.ts`）清晰度
- ✅ 导出分层清晰：Adapters / UI Components / React Context / Hooks / Internal 分组，并附 `@packageDocumentation` 用法示例。
- ⚠️ **陷阱点 1（P2）**：`ConnectionStatus` 被导出两次且语义混淆——`index.ts:45` 以别名 `ConnectionStatusBadge` 导出组件，而 `index.ts:54` 又导出类型 `ConnectionStatus`。开发者易把组件名与状态枚举类型名（`'connecting'|'syncing'|'connected'|'disconnected'`）混淆，且类型 `ConnectionStatus` 未提供对应值常量导出，需开发者自行硬编码字符串。
- ⚠️ **陷阱点 2（P2）**：Internal 模块（`ReactUpdateHandler`/`FunctionRegistry`/`TypedEventEmitter`/`isAgentUser`/`FunctionHandler`）被公开导出（`index.ts:88-91`），但属实现细节；未标记 `@internal`，第三方使用者可能误依赖，未来重构风险高。
- ⚠️ **陷阱点 3（P2，EXP-1 关联）**：`error:rpc` 事件已在 `XyncraProvider.tsx:218` emit，但 `index.ts` 未导出任何可订阅该错误的 hook（仅有 `EventEmitter` 内部类型）。开发者无法以编程方式监听 RPC 失败，只能被动接收 antd toast。

### `useRegisterFunction` 一致性结论
- ✅ **4 个 demo 函数用法完全一致**：均为 `export function XxxFunction() { useRegisterFunction(info, async (params) => {...}); return null; }` 形态（`getCurrentPage.tsx:23`、`highlightElement.tsx:33`、`showNotification.tsx:39`、`navigateTo.tsx:30`）。
- ✅ 参数签名一致：均先传 `FunctionInfo` 元数据（含 `name`/`description`/`parameters`），再传 async handler；`required` 字段声明规范。
- ⚠️ **踩坑点（P2，非一致性问题，属约定缺失）**：handler 内对 `params` 一律用 `as` 强转（如 `params.selector as string`、`params.type as ...`），web 包未对 `FunctionInfo.parameters` 做运行时校验或类型推导。server 传入多余/缺失字段时，类型安全仅停留在编译期，运行期无保护（属实现级发现，见 §④）。
- ✅ 集成点正确：`functions/index.tsx:15` `DemoFunctions` 统一挂载 4 个函数组件，且置于 `XyncraProvider` 内部（`app.tsx:158-161`），满足 hook 必须在 Provider 树下调用的约束。

---

## ③ 新增决策建议

**无新增决策建议。**

本次审查发现的全部问题（HITL 数据流向、read/conversation 事件缺失、连接状态机卡死、内置函数未注册、导出契约陷阱）均属实现级缺陷或已有待定项（step2 §④ 已列出需架构师裁定的 3 点：内置函数注册范围、`onConversation` 是否携带 action、HITL 数据源），未达到"非常规复杂架构 / 影响全局 / 改变外部协议"的新决策标准。其中：
- EXP-3（内置函数注册范围）已在 step2 §④-1 作为待确认决策列出，由架构师裁定，无需在 PRODUCT_DECISIONS 新增条目。
- BUG-1/2/3/4 均为 web 包内部实现 bug，不改变对外协议或全局架构，修复即可，不应写入决策文档。

---

## ④ 实现级发现记录（不改 PRODUCT_DECISIONS）

> 以下仅作归档，供后续修复轮次参考；不构成产品决策。

1. **`index.ts` 导出 `ConnectionStatus` 组件/类型同名歧义**（§② 陷阱点 1）：建议后续将类型导出改名为 `ConnectionStatusValue` 或导出状态常量枚举，避免与 `ConnectionStatusBadge` 组件混淆。
2. **Internal 模块过度暴露**（§② 陷阱点 2）：`ReactUpdateHandler`/`FunctionRegistry`/`TypedEventEmitter`/`isAgentUser`/`FunctionHandler` 应标记 `@internal` 或移出公开导出，降低第三方误依赖风险。
3. **`error:rpc` 无编程订阅入口**（§② 陷阱点 3 / EXP-1）：建议新增 `useErrorRpc` hook 或导出事件订阅方法，提升 API 完整性。
4. **`useRegisterFunction` handler 参数无运行时校验**（§② 踩坑点）：`params` 经 `as` 强转，建议对 `FunctionInfo.parameters.required` 做运行期校验或在文档中明确约定"server 保证字段类型"。
5. **deviceID SSR 容错已实现**（核实项）：`resolveDeviceID`（`XyncraProvider.tsx:130-160`）对 `globalThis.localStorage`/`crypto.randomUUID` 缺失均做了 fail-through，仅在完全不可用时抛错——符合 D-5 的"SSR 必须显式传 deviceID"约束，无问题。
6. **`navigateTo` 依赖 `@umijs/max` history**（核实项）：`navigateTo.tsx:11,32` 使用 Umi 的 `history.push`，属 demo 前端耦合，非 web 包通用能力；若作为通用示例对外，应改为 `window.history`/`window.location`，避免框架绑定（P3，仅提示）。
