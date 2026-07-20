# M4: 开发者工具链

**里程碑**: M4
**目标**: 新开发者 30 分钟内完成一个页面的 Agent 接入。
**前置依赖**: M1（Schema 引擎），M2/M3 可并行
**预计工作量**: 中

---

## 理想状态

一个新开发者拿到 Xyncra，体验流程：

```
1. npm install @xyncra/schema @xyncra/schema-antd @xyncra/client-web
2. npx xyncra init --scan ./src/pages
   → 自动生成 xyncra.schema.ts 骨架（基于现有路由和组件扫描）
3. 在 app.tsx 中加入 <XyncraProvider> 和 <FloatingAssistant />
4. 打开浏览器，看到悬浮助手
5. 打开调试面板，看到已注册的 12 个工具
6. 对 Agent 说"帮我筛选订单"，看到 Agent 正确调用了 filter_orderTable
7. 调试面板显示：Agent 调用链路、UI state 变化、工具执行结果
```

---

## Phase 列表

### M4-P1: Schema 生成器 CLI

**目标**: 扫描现有前端代码，自动生成初始 schema 骨架。

**交付物**:

1. **`xyncra init` 命令**
   - 扫描项目路由配置（`router.ts` / `routes.ts` / umi 路由）
   - 扫描页面组件目录（`src/pages/`）
   - 识别页面中的常见组件模式：
     - ProTable / Table → 生成 TableComponent schema
     - ProForm / Form → 生成 FormComponent schema
     - Input.Search / Search → 生成 SearchComponent schema
     - Modal / Drawer → 生成 ModalComponent schema
     - Button onClick → 提取可能的 Action
   - 输出 `xyncra.schema.ts` 骨架文件

2. **智能推断**
   - 从 TypeScript 类型推断表格列（如 `interface Order { id: string; status: string }`）
   - 从 ProTable columns 配置提取 filterable / sortable 信息
   - 从 ProForm fields 配置提取表单字段类型和校验规则
   - 从路由配置提取页面名称和嵌套关系

3. **增量更新**
   - `xyncra sync` — 重新扫描，对比已有 schema，只更新变化部分
   - 保留开发者手动添加的 description 和自定义配置

4. **输出格式**
   - 生成的 schema 带有 `// TODO:` 注释标记需要开发者补充的部分
   - 自动生成 description（基于组件名和字段名推断）
   - 不确定的部分标记为 `// REVIEW: 需要确认`

**验证标准**: 对一个典型的 Ant Design Pro 项目运行 `xyncra init`，生成的 schema 能覆盖 60-70% 的页面结构，开发者只需补充业务描述和自定义操作。

---

### M4-P2: Schema 调试面板

**目标**: 浏览器内实时查看 Agent 与 UI 的交互状态。

**交付物**:

1. **调试面板 UI**
   - 浏览器右下角浮层（独立于悬浮助手）
   - 快捷键 `Ctrl+Shift+X` 切换显示
   - 三个 Tab：
     - **Functions** — 当前注册的所有工具列表
       - 工具名、描述、参数 schema
       - 最近一次调用时间和结果
       - 手动测试调用（输入参数，查看返回）
     - **Agent Calls** — Agent 工具调用实时日志
       - 时间线视图：调用顺序、耗时、成功/失败
       - 每次调用的入参和返回值
       - Agent 的推理过程（如果有）
     - **UI State** — 当前 UI 状态快照
       - 各组件的实时状态
       - 状态变化历史（最近 20 次）
       - 手动触发 `report_ui_state`

2. **`@xyncra/devtools` 包**
   - 独立包，开发环境引入，生产环境不打包
   - 通过环境变量控制：`XYNCRA_DEVTOOLS=1`
   - 不增加生产包体积

3. **Agent 调用回放**
   - 记录 Agent 的完整操作序列
   - 支持回放：按步骤重放 Agent 的工具调用
   - 用于调试 Agent 决策问题

**验证标准**: 开发者在调试面板中能看到 Agent 调用 `filter_orderTable` 时的完整入参、返回值和 UI state 变化。

---

### M4-P3: 模板和脚手架

**目标**: 常见页面类型的 schema 模板，开发者选择模板即可快速接入。

**交付物**:

1. **页面模板库**
   ```
   xyncra template list
   → crud-table    CRUD 表格页（搜索+表格+操作）
   → detail        详情展示页（字段+操作按钮）
   → form          表单页（多步骤表单+提交）
   → dashboard     仪表盘（图表+数据卡片）
   → list          列表页（搜索+列表+加载更多）
   → settings      设置页（多 tab 表单）
   ```

2. **`xyncra template apply` 命令**
   - 选择模板 → 交互式填写参数（页面名、路由、组件 ID、字段列表）
   - 生成完整的 schema 文件 + 示例组件代码
   - 支持自定义模板：开发者可将自己的 schema 保存为模板

3. **模板组合**
   - 一个页面可以组合多个模板片段
   - 例如：crud-table = search-template + table-template + action-template
   - 模板之间通过组件 ID 自动关联

**验证标准**: 开发者运行 `xyncra template apply crud-table`，填写 5 个参数，30 秒内生成一个完整可用的 CRUD 页面 schema。

---

### M4-P4: 文档站

**目标**: 完整的接入文档，覆盖从零到生产的全流程。

**交付物**:

1. **快速开始** — 10 分钟接入指南
   - 安装 SDK
   - 最小配置
   - 第一个页面接入
   - 验证 Agent 能调用工具

2. **概念文档**
   - UI Schema 是什么
   - 组件类型详解
   - Agent 工具调用原理
   - Flow Schema 详解
   - HITL 机制

3. **API 参考**
   - `@xyncra/schema` 类型参考
   - `@xyncra/schema-antd` 模板参考
   - `@xyncra/client-web` Hook 参考
   - 服务端配置参考

4. **最佳实践**
   - Schema 编写规范
   - 组件命名约定
   - 安全操作设计
   - 性能优化建议

5. **示例项目**
   - Ant Design Pro CRUD 示例
   - 自定义组件示例
   - 多页面流程示例
   - 多 Agent 协作示例

**验证标准**: 一个不熟悉 Xyncra 的前端开发者，按照文档能在 30 分钟内完成一个页面的 Agent 接入。
