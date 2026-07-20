# M5: 生产级增强

**里程碑**: M5
**目标**: 系统在生产环境稳定运行——权限控制、审计日志、性能优化、多框架支持。
**前置依赖**: M1-M3 核心功能完成
**预计工作量**: 中（按需迭代，非一次性完成）

---

## 理想状态

一个企业级应用接入 Xyncra 后：

- **安全**：Agent 只能自动执行只读操作，写操作需用户确认，危险操作需二次确认 + 倒计时
- **可追溯**：每次 Agent 操作有完整审计日志，可查询谁在什么时间做了什么
- **高性能**：100 个页面的应用，函数注册延迟 < 200ms，Agent tool 列表始终精简
- **多框架**：Vue 3、Svelte、纯 JS 项目都能接入，不限于 React

---

## Phase 列表

### M5-P1: 操作权限系统

**目标**: 基于权限级别控制 Agent 的自动执行行为。

**交付物**:

1. **三级权限模型**
   ```
   readonly  — 只读操作（查询、搜索、获取状态）
     → Agent 自动执行，无需用户确认
   
   write     — 写操作（创建、修改、提交）
     → Agent 执行前需 HITL 确认
   
   dangerous — 危险操作（删除、取消、支付、批量修改）
     → Agent 执行前需 HITL 确认 + 倒计时（默认 10 秒）
     → 用户可在倒计时内取消
   ```

2. **Schema 中的权限标注**
   - 每个 `ActionSchema` 可声明 `permission` 字段
   - 内置组件模板自动推断默认权限：
     - `get_*_state` → readonly
     - `filter_*`, `sort_*`, `search_*` → readonly
     - `submit_*`, `create_*` → write
     - `delete_*`, `cancel_*` → dangerous
   - 开发者可覆盖默认权限

3. **Agent 权限守卫**
   - Agent 调用工具前，服务端检查权限级别
   - readonly → 直接执行
   - write → 触发 HITL，等待用户确认
   - dangerous → 触发 HITL + 倒计时，倒计时结束未确认则自动取消
   - Agent system prompt 中注入权限策略，让 Agent 理解何时需要确认

4. **全局权限策略覆盖**
   - 服务端配置全局策略（如 `all_write_operations_require_confirmation: true`）
   - 按 Agent 类型配置不同策略
   - 按用户角色配置不同策略

**验证标准**: Agent 调用 `delete_order` 时，客户端弹出确认倒计时，用户 10 秒内可取消。

---

### M5-P2: 操作审计日志

**目标**: 每次 Agent 操作有完整记录，可追溯、可查询。

**交付物**:

1. **审计日志数据模型**
   ```
   AuditLog {
     id: string
     user_id: string
     device_id: string
     agent_id: string
     conversation_id: string
     timestamp: DateTime
     action: string          // 工具名
     params: JSON            // 入参
     result: JSON            // 返回值
     status: 'success' | 'failed' | 'cancelled' | 'timeout'
     duration_ms: number     // 执行耗时
     permission: 'readonly' | 'write' | 'dangerous'
     user_confirmed: boolean // 用户是否确认
     ui_state_before: JSON   // 操作前 UI 状态快照
     ui_state_after: JSON    // 操作后 UI 状态快照
   }
   ```

2. **日志采集**
   - 服务端在工具执行前后自动采集
   - readonly 操作：记录精简日志（不含 ui_state 快照）
   - write/dangerous 操作：记录完整日志
   - 日志写入数据库（与 message 同库，复用现有存储）

3. **日志查询 API**
   - `system.get_audit_logs` RPC
   - 支持按时间范围、操作类型、Agent、用户筛选
   - 分页查询

4. **客户端审计面板**（可选，集成到 M4-P2 调试面板）
   - 展示最近的操作日志
   - 支持回放：重新展示操作前后的 UI 状态

**验证标准**: 能查询某个用户在过去 24 小时内被 Agent 执行的所有写操作，包含完整的入参和结果。

---

### M5-P3: 函数分层注册

**目标**: 解决 Agent tool 爆炸问题——Agent 只看到当前相关的 tools。

**交付物**:

1. **按页面路由过滤**
   - 客户端只注册当前页面的 schema 对应的 tools
   - 页面切换时：反注册旧页面 tools，注册新页面 tools
   - 全局工具（navigate_to、get_current_page）始终注册

2. **按场景过滤**
   - Schema 中的 `context` 字段定义工具的适用场景
   - 例如：搜索场景下只加载 search_* 和 filter_* tools
   - Agent 配置中可指定 `active_context`

3. **动态工具分组**
   - 每个 PageSchema 的 tools 归为一个 `tool_group`
   - Agent system prompt 中按 group 组织工具描述
   - Agent 可以通过 `list_tool_groups` 工具查看所有可用工具组
   - Agent 可以通过 `activate_tool_group` 工具切换活跃工具组

4. **工具数量监控**
   - 服务端监控每个 Agent 的活跃 tool 数量
   - 超过阈值（如 30）时发出告警
   - 自动建议开发者拆分页面或使用场景过滤

**验证标准**: 一个有 50 个页面的应用，Agent 在任何时刻的活跃 tool 数量不超过 20-30 个。

---

### M5-P4: 性能优化

**目标**: 大规模应用下系统保持流畅。

**交付物**:

1. **批量注册与增量更新**
   - 当前：每次 schema 变化全量替换所有 functions
   - 优化：计算 diff，只发送变化的 functions（新增/修改/删除）
   - 协议扩展：`system.register_functions` 支持 `mode: 'patch'`

2. **UI State 上报节流**
   - 高频状态变化（如鼠标移动、连续输入）不上报
   - 有意义的变化才上报：数据变化、筛选变化、页面切换
   - 可配置节流间隔（默认 1 秒）

3. **Schema 缓存**
   - 客户端：schema 编译结果缓存，相同 schema 不重复编译
   - 服务端：FunctionInfo 缓存，相同 schema 不重复处理
   - 缓存失效：schema 变化时自动失效

4. **WebSocket 消息压缩**
   - UI state 上报消息可能较大（100 个组件的状态）
   - 启用 WebSocket permessage-deflate 压缩
   - 或只上报变化的组件状态（delta 更新）

**验证标准**: 100 个页面的应用，函数注册延迟 < 200ms，WebSocket 消息量 < 10 msg/s（正常操作下）。

---

### M5-P5: 多框架支持

**目标**: 不限于 React，Vue 3、Svelte、纯 JS 项目都能接入。

**交付物**:

1. **`@xyncra/client-vue` 包** — Vue 3 adapter
   ```
   <template>
     <XyncraProvider :server-url="url" :user-id="userId">
       <SchemaProvider :schema="orderPage">
         <OrderTable />
       </SchemaProvider>
       <FloatingAssistant />
     </XyncraProvider>
   </template>

   <script setup>
   import { useRegisterFunction, usePageSchema } from '@xyncra/client-vue'
   // Vue 3 composables 版本的 hooks
   </script>
   ```

2. **`@xyncra/client-svelte` 包** — Svelte adapter
   - Svelte stores 版本的状态管理
   - Svelte components 版本的 Provider

3. **`@xyncra/client-vanilla` 包** — 纯 JS adapter
   - 无框架依赖，直接操作 DOM
   - 提供命令式 API：
     ```javascript
     import { createXyncraClient, registerSchema } from '@xyncra/client-vanilla'
     
     const client = createXyncraClient({ serverURL, userID })
     registerSchema(client, orderPage)
     ```
   - 适用于任何框架或无框架项目

4. **统一的核心层**
   - 所有框架 adapter 共享 `@xyncra/client-core`
   - 框架差异仅在适配层：状态管理、组件生命周期、DOM 操作
   - Schema 定义和 Tool 注册逻辑完全复用

**验证标准**: 同一个 PageSchema 可以在 React、Vue 3、Svelte 和纯 JS 项目中使用，行为一致。
