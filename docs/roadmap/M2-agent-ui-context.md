# M2: Agent UI Context 注入

**里程碑**: M2
**目标**: Agent 能实时感知客户端 UI 状态，基于上下文做决策。
**前置依赖**: M1（需要 Schema 引擎提供的组件状态）
**预计工作量**: 中

---

## 理想状态

Agent 的 system prompt 中自动注入当前 UI 上下文：

```
=== 用户当前 UI 状态 ===
页面: 订单管理 (/orders)
组件状态:
  - orderTable: 表格，共 150 条数据，当前第 1 页，按"状态"筛选为"待处理"
  - orderSearch: 搜索框，已输入关键词 "iPhone"
  - 全局: 用户已登录，角色=管理员

可执行操作:
  - filter_orderTable: 按条件筛选表格
  - sort_orderTable: 按列排序
  - cancel_order: 取消指定订单（需要确认，dangerous）
  - search_orders: 搜索订单
  - navigate_to: 跳转页面
```

用户说「帮我把金额超过 1000 的订单找出来」，Agent 看到当前已有筛选条件（status=pending），决定追加金额筛选而非重置：

```
Agent 调用: filter_orderTable({ field: 'amount', operator: 'gt', value: 1000 })
→ 表格刷新，状态更新
→ Agent 确认: "已筛选出金额超过 1000 的待处理订单，共 23 条"
```

---

## Phase 列表

### M2-P1: UI State 协议扩展

**目标**: 定义客户端上报 UI 状态的协议，新增 RPC 方法。

**交付物**:

1. **新增 `system.report_ui_state` RPC 方法**
   - 客户端 → 服务端
   - 参数：
     ```json
     {
       "route": "/orders",
       "page_name": "订单管理",
       "components": [
         {
           "id": "orderTable",
           "type": "table",
           "state": {
             "data_count": 150,
             "page": 1,
             "page_size": 20,
             "filters": { "status": "pending" },
             "sort": { "field": "created_at", "order": "desc" },
             "selected_rows": []
           }
         },
         {
           "id": "orderSearch",
           "type": "search",
           "state": {
             "keyword": "iPhone",
             "active_filters": {}
           }
         }
       ],
       "timestamp": "2026-07-20T12:00:00Z"
     }
     ```
   - 响应：`{ "status": "ok" }`

2. **服务端处理逻辑**
   - 接收并缓存每个 (userID, deviceID) 的最新 UI state
   - 存储在内存中（TTL 30 秒，过期自动清理）
   - 不持久化（UI state 是瞬时的，类似 typing indicator）

3. **协议类型扩展** — `@xyncra/protocol` 新增 UI state 相关类型

**验证标准**: 客户端调用 `system.report_ui_state` 后，服务端能正确解析并缓存 UI state。

---

### M2-P2: Agent System Prompt 注入

**目标**: 服务端将缓存的 UI state 注入到 Agent 的 system prompt 中。

**交付物**:

1. **UI State → System Prompt 模板**
   - 定义结构化模板，将 UI state 转换为自然语言描述
   - 包含：当前页面、各组件状态、可用操作列表
   - 组件状态格式化：
     - Table: "表格，共 N 条数据，当前第 M 页，筛选条件: X=Y"
     - Form: "表单，已填写字段: [a, b, c]，未填写: [d, e]"
     - Search: "搜索框，关键词: X，筛选条件: Y"
     - Modal: "弹窗已打开，标题: X"

2. **Agent 上下文注入点**
   - 在 `AgentBuilder` 或 `eino_agent.go` 中增加 UI context 注入
   - 每次 Agent 收到用户消息时，先查询该用户的 UI state 缓存
   - 如果有有效 UI state，追加到 system prompt 末尾
   - 如果没有（用户未打开页面或 state 已过期），不注入

3. **UI Context 与现有 Agent Config 集成**
   - Agent YAML 配置新增 `enable_ui_context: true` 选项
   - 仅对 UI 助手类 Agent 启用，编程 Agent 等不需要

**验证标准**: Agent 在回复用户消息时，能引用当前页面的具体信息（如"我看到你的表格当前筛选了待处理订单"）。

---

### M2-P3: 状态变化驱动更新

**目标**: UI 状态变化时自动重新上报，Agent 始终看到最新状态。

**交付物**:

1. **客户端状态变化检测**
   - `SchemaProvider` 监听组件状态变化
   - 变化来源：用户操作（翻页、筛选、输入）、Agent 操作（调用工具后 UI 更新）
   - 去抖策略：100ms 内的多次变化合并为一次上报
   - 节流策略：最多每秒上报 1 次

2. **Agent 工具调用后的状态刷新**
   - Agent 调用客户端工具（如 `filter_orderTable`）→ 客户端执行 → UI 更新 → 自动触发 `report_ui_state`
   - Agent 在工具返回后能看到更新后的 UI state
   - 通过 system prompt 中的 UI context 变化来感知（无需额外协议）

3. **页面切换时的状态上报**
   - 路由变化 → 新页面的 SchemaProvider 挂载 → 自动上报新页面的 UI state
   - 旧页面的 SchemaProvider 卸载 → 自动反注册旧页面的函数
   - 保证 Agent 在任何时刻看到的都是当前页面的真实状态

**验证标准**: 用户手动翻页后，Agent 在下一次交互中能感知到分页变化（如"我看到你现在在第 2 页"）。

---

### M2-P4: Agent 决策链路优化

**目标**: Agent 基于 UI context 做出更智能的决策。

**交付物**:

1. **Agent 思维链优化**
   - 在 Agent system prompt 中增加决策指引：
     - "调用工具前，先通过 get_{id}_state 确认组件当前状态"
     - "如果用户的需求涉及筛选，先检查当前是否有已有筛选条件"
     - "执行写操作前，向用户确认"
   - Agent 先理解 context → 再选择 tool → 执行 → 读取新 context → 确认结果

2. **工具调用结果增强**
   - 客户端工具执行后，除了返回操作结果，还返回操作后的 UI state 变化摘要
   - 例如 `filter_orderTable` 返回：
     ```json
     {
       "success": true,
       "message": "已筛选",
       "state_change": {
         "data_count": { "from": 150, "to": 23 },
         "filters": { "added": { "amount_gt": 1000 } }
       }
     }
     ```
   - Agent 可以直接读取变化摘要，无需再调用 get_state

3. **多轮操作的上下文保持**
   - Agent 连续调用多个工具时，UI context 自动更新
   - 保证每一步决策都基于最新状态
   - 避免 Agent 基于过期信息重复操作

**验证标准**: 用户说「帮我筛选金额超过 1000 的订单，然后取消第 1 条」，Agent 能正确执行两步操作，且第二步基于筛选后的结果执行。
