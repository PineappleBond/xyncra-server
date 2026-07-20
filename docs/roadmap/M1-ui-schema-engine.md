# M1: UI Schema 引擎

**里程碑**: M1
**目标**: 开发者用声明式 schema 描述页面结构，SDK 自动生成 Agent 可调用的工具。
**前置依赖**: 无（可立即开始）
**预计工作量**: 大（核心突破，后续所有里程碑的基础）

---

## 理想状态

一个 Ant Design Pro 的 CRUD 页面，开发者只写一个 schema 声明：

```typescript
const orderPage: PageSchema = {
  route: '/orders',
  name: '订单管理',
  components: [
    {
      type: 'table', id: 'orderTable',
      columns: [
        { key: 'id', label: '订单号' },
        { key: 'status', label: '状态', filterable: true },
        { key: 'amount', label: '金额', sortable: true },
      ],
      actions: [
        { id: 'view', label: '查看详情', type: 'navigate', target: '/orders/:id' },
        { id: 'cancel', label: '取消订单', type: 'api', confirm: true },
      ],
    },
    {
      type: 'search', id: 'orderSearch',
      fields: [
        { key: 'status', label: '状态', type: 'select', options: [...] },
        { key: 'dateRange', label: '日期范围', type: 'daterange' },
      ],
    },
  ],
}
```

Agent 自动获得以下工具并能正确调用：

| 工具名 | 来源 | 说明 |
|--------|------|------|
| `get_current_page` | 全局自动生成 | 返回当前路由和页面名称 |
| `get_orderTable_state` | table 组件自动生成 | 返回表格数据、分页、筛选条件 |
| `filter_orderTable` | table.filterable 列自动生成 | 按列筛选表格 |
| `sort_orderTable` | table.sortable 列自动生成 | 按列排序 |
| `cancel_order` | table.actions 自动生成 | 取消订单（带 confirm） |
| `navigate_to_order_view` | table.actions 自动生成 | 跳转到订单详情 |
| `search_orders` | search 组件自动生成 | 按搜索条件查询 |
| `reset_search` | search 组件自动生成 | 重置搜索条件 |

**开发者写了 1 个 schema，Agent 获得了 8 个工具。** 无需手写任何函数。

---

## Phase 列表

### M1-P1: Schema 类型系统

**目标**: 定义核心类型，建立 schema 的类型基础。

**交付物**:

1. **`PageSchema`** — 页面级描述
   ```
   PageSchema {
     route: string              // 路由路径，如 '/orders'
     name: string               // 页面中文名称，如 '订单管理'
     description?: string       // 页面描述，给 Agent 理解用
     components: ComponentSchema[]
     globalActions?: ActionSchema[]  // 页面级操作（如"新建订单"按钮）
   }
   ```

2. **`ComponentSchema`** — 组件级描述
   - 基础字段：`type`, `id`, `label`, `description`
   - 按 type 区分子类型：
     - `TableComponent` — 列定义（columns）、行操作（rowActions）、批量操作（batchActions）
     - `FormComponent` — 字段定义（fields）、提交/重置操作
     - `SearchComponent` — 搜索字段、搜索/重置操作
     - `ListComponent` — 列表项操作
     - `ModalComponent` — 弹窗内容、确认/取消操作
     - `DetailComponent` — 详情字段展示
     - `NavigationComponent` — 导航菜单项
   - 通用扩展：`custom` 类型支持任意组件

3. **`ActionSchema`** — 操作描述
   ```
   ActionSchema {
     id: string
     label: string              // 操作名称，给 Agent 看
     description?: string       // 操作详细描述
     type: 'navigate' | 'api' | 'form' | 'confirm' | 'custom'
     params?: JSONSchema        // 参数定义
     confirm?: boolean          // 是否需要用户确认
     permission?: 'readonly' | 'write' | 'dangerous'  // 权限级别
   }
   ```

4. **类型包发布** — `@xyncra/schema` 独立包，纯类型定义，零依赖

**验证标准**: 类型定义能完整描述一个 Ant Design Pro CRUD 页面的所有组件和操作。

---

### M1-P2: Schema → Tool 转换器

**目标**: 将 schema 自动转换为 `FunctionInfo[]` 并注册到服务端。

**交付物**:

1. **`SchemaCompiler`** — 核心转换器
   - 输入：`PageSchema`
   - 输出：`FunctionInfo[]`
   - 转换规则：
     - 每个 `ComponentSchema` → 一个 `get_{id}_state` 工具（读取状态）
     - 每个 `ComponentSchema` 中的 `ActionSchema` → 一个 `{action_id}` 工具（执行操作）
     - `TableComponent` 的 filterable 列 → 一个 `filter_{id}` 工具
     - `TableComponent` 的 sortable 列 → 一个 `sort_{id}` 工具
     - `SearchComponent` → 一个 `search_{id}` 工具 + `reset_{id}` 工具
     - `FormComponent` → 一个 `submit_{id}` 工具 + `reset_{id}` 工具
     - `globalActions` → 直接生成工具

2. **工具描述自动生成**
   - `name`：从 schema id + action id 派生，snake_case
   - `description`：从 schema label + description 组合生成自然语言描述
   - `parameters`：从 schema 的 fields/columns/params 自动生成 JSON Schema
   - 结构化语义：包含组件类型、操作类型、是否需要确认等元数据

3. **注册管理器**
   - 与现有 `system.register_functions` 协议对接
   - 支持增量更新：只注册变化的函数，不全量替换
   - 页面切换时自动清理旧页面的函数，注册新页面的函数

**验证标准**: 给定一个 PageSchema，Compiler 输出的 FunctionInfo[] 能被服务端正确接收并注入到 Agent tools 中。

---

### M1-P3: React SchemaProvider

**目标**: 开发者传入 schema，组件自动处理注册/反注册/路由感知。

**交付物**:

1. **`<SchemaProvider>` 组件**
   ```tsx
   <SchemaProvider schema={orderPage}>
     <OrderTable />
     <OrderSearch />
   </SchemaProvider>
   ```
   - 挂载时自动编译 schema 并注册函数
   - 卸载时自动反注册
   - schema 变化时自动重新编译和注册

2. **`usePageSchema` Hook**
   ```tsx
   const { register, unregister, getRegisteredTools } = usePageSchema()
   ```
   - 手动控制 schema 注册/反注册
   - 获取当前已注册的工具列表

3. **路由感知**
   - 与 `react-router` / `umi` 集成
   - 自动检测当前路由，只注册匹配当前路由的 schema
   - 路由切换时自动切换注册的函数集合

4. **嵌套 Schema 支持**
   - 页面级 schema + 组件级 schema 可组合
   - Modal、Drawer 等浮层组件支持动态 schema（打开时注册，关闭时反注册）

**验证标准**: 开发者在页面组件中放入 `<SchemaProvider schema={...}>`，页面加载时函数自动注册到服务端，页面卸载时自动清理。

---

### M1-P4: 组件状态采集器

**目标**: 自动采集组件当前状态，生成结构化 JSON 供 Agent 读取。

**交付物**:

1. **`ComponentStateCollector` 接口**
   ```
   interface ComponentStateCollector {
     collect(): ComponentState  // 返回当前组件的结构化状态
   }
   ```

2. **内置采集器**
   - `TableCollector`：采集当前数据、分页信息、排序状态、筛选条件、选中行
   - `FormCollector`：采集表单当前值、校验状态、是否已修改
   - `SearchCollector`：采集当前搜索条件、是否激活
   - `ListCollector`：采集列表数据、加载状态
   - `ModalCollector`：采集弹窗打开状态、内容摘要

3. **状态注册与 `get_{id}_state` 工具对接**
   - SchemaCompiler 生成的 `get_{id}_state` 工具自动调用对应 collector
   - collector 返回的 JSON 直接作为工具返回值

4. **自定义 collector 支持**
   - 开发者可为自定义组件编写自己的 collector
   - 通过 `<ComponentState id="xxx" collector={myCollector}>` 注入

**验证标准**: Agent 调用 `get_orderTable_state` 时，返回当前表格的真实数据（分页、筛选、排序状态），而非静态配置。

---

### M1-P5: 内置组件库

**目标**: 预置常见组件的 schema 模板，开发者开箱即用。

**交付物**:

1. **`@xyncra/schema-antd` 包** — Ant Design 组件的 schema 模板
   ```
   createTableSchema({
     id: 'orderTable',
     columns: [...],
     rowActions: [...],
   })
   → 自动生成完整的 TableComponent schema
   ```
   - `createTableSchema` — ProTable / Table
   - `createFormSchema` — ProForm / Form
   - `createSearchSchema` — ProTable 的搜索区
   - `createListSchema` — List / ProList
   - `createDetailSchema` — Descriptions
   - `createModalSchema` — Modal / Drawer
   - `createPageSchema` — 组合以上，生成完整页面 schema

2. **与 Ant Design Pro 组件的深度集成**
   - 自动从 ProTable 的 columns 定义提取 filterable / sortable 信息
   - 自动从 ProForm 的 fields 定义提取参数 schema
   - 自动识别 ProTable 的 toolBarRender 中的操作按钮

3. **通用组件模板** — 不依赖特定 UI 库
   - `createGenericTableSchema` — 任意表格组件
   - `createGenericFormSchema` — 任意表单组件
   - 开发者只需传入 columns / fields 定义

**验证标准**: 使用内置模板，一个标准的 Ant Design Pro CRUD 页面只需 20-30 行 schema 代码即可完成 Agent 接入。
