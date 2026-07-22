# TC-011-Vue 测试执行报告

| 字段 | 值 |
|------|-----|
| 日期 | 2026-07-22 |
| Git Commit | (working tree, uncommitted fixes) |
| 测试者 | Claude (automated) |
| 环境 | Docker E2E + Vue Dev Server (port 8855) + Playwright |
| Vue Dev Server | http://localhost:8855 |
| Device ID | (per-run generated) |

---

## 测试结果总览

| 阶段 | 步骤 | 结果 | 备注 |
|------|------|------|------|
| **阶段 1: 连接与注册** | | | |
| 1.1 | WebSocket 连接 | PASS | FloatingAssistant 绿色，syncStates 有记录 |
| 1.2 | 函数注册 | PASS | 13-17 个函数注册成功 |
| **阶段 2: RemoteCalling 流程** | | | |
| 2.1 | 收到 RemoteCalling | FAIL | RemoteCalling 存在于 IndexedDB，但 Dialog 未弹出 |
| 2.2 | Dialog 显示 | FAIL | 依赖 2.1 |
| 2.3 | 上报结果 | FAIL | 依赖 2.1 |
| 2.4 | Agent 恢复 | FAIL | 依赖 2.1 |
| **阶段 3: HITL 交互** | | | |
| 3.1 | ask_user | SKIP | 依赖 RemoteCalling 流程正常 |
| 3.2 | ask_user_choice | SKIP | 依赖 RemoteCalling 流程正常 |
| 3.3 | 取消操作 | SKIP | 依赖 RemoteCalling 流程正常 |
| **阶段 4: DeviceID 与 IndexedDB** | | | |
| 4.1 | DeviceID 过滤 | SKIP | 依赖 RemoteCalling 流程正常 |
| 4.2 | IndexedDB 持久化 | PASS | RemoteCalling 记录存在于 IndexedDB |
| **阶段 5: 边缘场景** | | | |
| 5.1 | Update 触发同步 | PASS | syncStates 有记录 |
| 5.2 | 断线重连 | SKIP | 未测试 |
| 5.3 | 超时过期 | SKIP | 需要服务端 DB 操作 |

**总计**: 12 步骤 | 通过: 4 | 失败: 4 | 跳过: 4

**结论**: FAIL — RemoteCalling Dialog 未弹出，核心流程不通。

---

## 发现的 Bug

### Bug 1: `connection-manager.ts` 重复变量声明（构建错误）

**严重程度**: Critical
**文件**: `demo/vue-pure-admin/packages/xyncra-client-core/src/connection-manager.ts:289,301`
**描述**: `sendPackage` 方法中 `const data` 被声明两次（line 289 和 301），导致 Vite 依赖扫描失败，XyncraTestHelpers 无法加载。

**修复**: 将第二个 `const data` 重命名为 `const jsonData`。

**状态**: 已修复（本地）

---

### Bug 2: `DynamicToolProvider` 使用完整 AgentID 查找客户端函数

**严重程度**: Critical
**文件**: `internal/agent/dynamic_tool_provider.go:77`
**描述**: `DynamicToolProvider.BeforeAgent` 使用 `agentID`（如 `agent/ui-assistant`）调用 `GetFunctionsByUser`，但客户端函数注册在基础 `userID`（`agent`）下。导致 Agent 看不到任何客户端注册的函数。

**根因**: `AgentIDFromContext` 返回完整的 agent ID（如 `agent/ui-assistant`），但 `register_functions` 将函数注册在基础 userID（`agent`）下。

**修复**: 在 `GetFunctionsByUser` 调用前提取基础 userID：
```go
baseUserID := agentID
if idx := strings.Index(agentID, "/"); idx > 0 {
    baseUserID = agentID[:idx]
}
deviceFuncs, err := d.funcRegistry.GetFunctionsByUser(ctx, baseUserID)
```

**状态**: 已修复（本地）

---

### Bug 3: `DynamicToolProvider` 工具去重缺失

**严重程度**: Critical
**文件**: `internal/agent/dynamic_tool_provider.go:126-132`
**描述**: 客户端注册的通用函数（如 `type_text`、`click_element`）与 Agent 内置工具同名，导致 LLM API 返回 400 错误：`tools contains duplicate names: type_text`。

**根因**: `merged` 数组包含客户端函数和 registry 工具，两者有重叠；`runCtx.Tools` 也包含同名工具。

**修复**: 在合并工具时按名称去重，客户端函数优先覆盖内置工具。

**状态**: 已修复（本地）

---

### Bug 4: `sync-manager.ts` 在 `tool_calling` 状态下删除 RemoteCallings

**严重程度**: Critical
**文件**: `demo/vue-pure-admin/packages/xyncra-client-core/src/sync-manager.ts:1001`
**描述**: 同步管理器在 agent_status 不是 `asking_user` 时删除 RemoteCallings。但客户端函数调用使用 `tool_calling` 状态，导致 RemoteCallings 被立即删除，Dialog 无法显示。

**根因**: 代码只检查了 `asking_user` 状态（HITL 场景），未考虑 `tool_calling` 状态（客户端函数调用场景）。

**修复**: 同时检查 `asking_user` 和 `tool_calling` 状态：
```typescript
if (result.conversation.agent_status !== 'asking_user' && result.conversation.agent_status !== 'tool_calling') {
  await this.options.db.remoteCallingsStore.deleteByConversation(convID);
}
```

**状态**: 已修复（本地）

---

### Bug 5: `sync-manager.ts` D-124 优化路径不包含 RemoteCallings

**严重程度**: High
**文件**: `demo/vue-pure-admin/packages/xyncra-client-core/src/sync-manager.ts:953`
**描述**: 当本地缓存的 `updated_at` 已是最新时（D-124 优化），`onConversation` 回调使用本地数据，但本地 conversation 对象不包含 `remote_callings`。导致 `VueUpdateHandler` 无法检测到 pending RemoteCallings，`remote_calling` 事件不被触发。

**根因**: D-124 优化跳过了 RPC 调用，但本地 conversation 记录不存储 RemoteCallings。

**修复**: 在 D-124 优化路径中，从 IndexedDB `remoteCallingsStore` 查询 RemoteCallings 并附加到 conversation 对象。

**状态**: 已修复（本地）

---

## 测试用例文档修复

### 问题 1: Vue Dev Server 端口

**文档问题**: SKILL.md 和 TC-011 假设 Vue Dev Server 在 `localhost:5173`，但实际端口可能因冲突而变化（测试中为 8855）。

**修复建议**: 文档应说明使用 `E2E_BASE_URL` 环境变量配置实际端口。

### 问题 2: Hash Router

**文档问题**: SKILL.md 的登录步骤使用 `page.goto('/login')`，但 Vue Demo 使用 hash router，URL 应为 `/#/login`。

**修复建议**: 登录 URL 应为 `${BASE_URL}/#/login`。

### 问题 3: waitForLoadState

**文档问题**: SKILL.md 使用 `waitForLoadState('networkidle')`，但在 Vite dev server 下会超时。

**修复建议**: 使用 `waitForLoadState('domcontentloaded')` 并添加额外等待时间。

---

## 服务端问题分析

### 问题 1: 函数注册与 Agent 查找的 ID 不匹配

**现象**: 客户端注册了 17 个函数，但 Agent 只看到内置工具。

**根因**: 函数注册使用 `userID=agent`，但 Agent 查找使用 `agentID=agent/ui-assistant`。`DynamicToolProvider` 没有正确提取基础 userID。

**影响**: 所有客户端函数对 Agent 不可见，Agent 无法调用 `pg_*` 函数。

### 问题 2: 工具名称冲突导致 LLM 调用失败

**现象**: LLM API 返回 400 错误：`tools contains duplicate names: type_text`。

**根因**: 客户端注册的通用函数（如 `type_text`）与 Agent 内置工具同名，`DynamicToolProvider` 未去重。

**影响**: Agent 执行完全失败，返回"抱歉，处理遇到问题"。

### 问题 3: RemoteCalling 超时过快

**现象**: RemoteCalling 在 10 秒后过期（`expires_at` = `created_at + 10s`），客户端来不及处理。

**根因**: `get_page_description` 等通用函数的 `timeout_ms` 设置过短。

**影响**: 即使 Dialog 弹出，用户也来不及操作。

---

## 修复文件清单

| 文件 | 修复内容 |
|------|---------|
| `demo/vue-pure-admin/packages/xyncra-client-core/src/connection-manager.ts` | 修复重复 `data` 变量声明 |
| `demo/vue-pure-admin/packages/xyncra-client-core/dist/connection-manager.js` | 同步修复（dist） |
| `internal/agent/dynamic_tool_provider.go` | 修复 userID 提取 + 工具去重 |
| `demo/vue-pure-admin/packages/xyncra-client-core/src/sync-manager.ts` | 修复 `tool_calling` 状态下的 RemoteCalling 保留 + D-124 路径的 RemoteCalling 查询 |
| `demo/vue-pure-admin/e2e/tc011-remote-calling-test.ts` | 新增测试脚本 |

---

## 下一步

1. 修复 Bug 4 和 Bug 5 后重新测试
2. 验证 RemoteCalling Dialog 弹出和交互流程
3. 测试 `ask_user` 和 `ask_user_choice` 场景
4. 测试 DeviceID 过滤
5. 测试断线重连后的 RemoteCalling 恢复
