# Xyncra Vue Demo 实现工作总结

## 已完成的工作

### 1. 提示词创建 (083)
- **文件**: `.claude/docs/task-planner-prompts/083-vue-demo-full-implementation.md`
- **状态**: 已在另一个进程中执行完成
- **内容**: Vue Demo 完整实现提示词，包含 12 个步骤

### 2. 测试技能创建
- **技能**: `xyncra-vue-demo-test`
- **位置**: `.claude/skills/xyncra-vue-demo-test/SKILL.md`
- **架构**: Playwright + WebSocket 结合
  - Playwright: 启动浏览器、控制 Vue 应用、验证页面状态
  - WebSocket: 发送测试指令给 ui-assistant Agent
  - 验证: Agent 函数调用 + 页面状态 + IndexedDB

### 3. 测试辅助函数
- **文件**: `demo/vue-pure-admin/src/test-helpers/index.ts`
- **状态**: 已创建，包含 9 个函数
- **函数列表**:
  - `fillForm(options)` - 填写基础表单
  - `submitForm()` - 提交表单
  - `resetForm()` - 重置表单
  - `searchTable(keyword)` - 搜索表格
  - `goToPage(pageNum)` - 表格翻页
  - `sortTable(column, order)` - 表格排序
  - `switchTab(tabLabel)` - 切换 Tab
  - `selectDate(selector, date)` - 选择日期
  - `selectOption(selector, option)` - 选择下拉选项

### 4. 架构决策
- **决策编号**: D-136
- **位置**: `docs/decisions/PRODUCT_DECISIONS.md`
- **内容**: 测试辅助函数统一接口（Agent 和 Playwright 共用）
- **未来方向**: 采用声明式注册模式 `defineTestHelpers(pageKey, helpers)`

---

## 待实现的工作

### 优先级 1: 页面组件集成

#### 1.1 修改页面组件
每个需要测试的页面组件需要：
1. 导入测试辅助函数
2. 通过 `defineExpose` 暴露
3. 通过 `registerComponent` 注册

**示例代码**:
```vue
<!-- views/schema-form/index.vue -->
<script setup lang="ts">
import { registerComponent } from '@/xyncra/component-accessor'
import { fillForm, submitForm, resetForm } from '@/test-helpers'

// 注册当前组件
registerComponent('schema-form')

// 暴露测试辅助函数，供 Agent 和 Playwright 调用
defineExpose({
  fillForm,
  submitForm,
  resetForm,
})
</script>
```

#### 1.2 需要集成的页面列表
- **P0 (高优先级)**:
  - Login (`views/login/index.vue`)
  - SchemaForm (`views/schema-form/index.vue`)
  - Table (`views/table/index.vue`)
  - System/User (`views/system/user/index.vue`)

- **P1 (中优先级)**:
  - List/Card (`views/list/card/index.vue`)
  - Tabs (`views/tabs/index.vue`)
  - Account Settings (`views/account-settings/index.vue`)
  - Monitor (`views/monitor/index.vue`)
  - Result (`views/result/index.vue`)

- **P2/P3 (低优先级)**:
  - Components/* (30+ 子页面)
  - Able/* (20+ 子页面)
  - Nested/*
  - Permission/*
  - Editor, Markdown, FlowChart, Ganttastic, VueFlow, Codemirror
  - ChatAI, Guide, Welcome, Empty, Error, About, MenuOverflow

### 优先级 2: Playwright 测试用例

#### 2.1 创建测试文件
- **位置**: `demo/vue-pure-admin/tests/e2e/vue-demo.spec.ts`
- **参考**: `.claude/skills/xyncra-vue-demo-test/SKILL.md` 中的测试运行器代码

#### 2.2 测试场景
1. Agent 基础回复
2. Agent 获取当前页面
3. Agent 导航到登录页
4. Agent 填写表单
5. Agent 提交表单
6. Agent 搜索表格
7. Agent 翻页
8. Agent 切换 Tab
9. Agent 选择日期
10. Agent 复合操作

#### 2.3 测试调用方式
```typescript
// 1. 打开页面
await page.goto('http://localhost:8848/schema-form')

// 2. 等待 FloatingAssistant 连接
await waitForFloatingAssistantReady(page)

// 3. 通过 WebSocket 发送指令
await client.sendMessage('填写标题为"测试任务"')

// 4. 等待 Agent 执行
await page.waitForTimeout(5000)

// 5. 验证页面状态
const titleInput = await page.$('input[name="title"]')
expect(await titleInput?.inputValue()).toBe('测试任务')

// 6. 使用测试辅助函数验证
const result = await page.evaluate(() => {
  return (window as any).XyncraTestHelpers.fillForm({
    title: '测试任务',
  })
})
expect(result.success).toBe(true)
```

### 优先级 3: 声明式注册模式 (D-136)

#### 3.1 当前方式（手动）
```typescript
// 需要 3 步
import { registerComponent } from '@/xyncra/component-accessor'
import { fillForm, submitForm } from '@/test-helpers'

registerComponent('schema-form')
defineExpose({ fillForm, submitForm })
```

#### 3.2 目标方式（声明式）
```typescript
// 一行代码完成所有注册
import { defineTestHelpers } from '@/test-helpers'
import { fillForm, submitForm } from '@/test-helpers/form-helpers'

defineTestHelpers('schema-form', {
  fillForm,
  submitForm,
})
```

#### 3.3 实现步骤
1. 修改 `src/test-helpers/index.ts`，添加 `defineTestHelpers` composable
2. 按功能模块拆分测试辅助函数：
   - `form-helpers.ts`
   - `table-helpers.ts`
   - `tab-helpers.ts`
   - `date-helpers.ts`
3. 实现自动挂载 `window.XyncraTestHelpers`
4. 实现自动生成 `pg_*` 函数定义
5. 更新所有页面组件使用新方式

**注意**: 此重构涉及大量页面组件修改，建议在功能稳定后统一实施。

---

## 关键约束

### 架构约束
1. **开发者 API 唯一，禁止 DOM 操作** (C7)
   - 页面函数只能调用开发者通过 `defineExpose` 暴露的方法
   - 禁止任何 DOM 操作（dispatchEvent、waitForSelector、querySelector 等）

2. **模块级全局组件注册表** (D-136)
   - 使用模块级 `Map` 存储组件注册表
   - Plugin install 时调用 `initComponentRegistry()` 初始化
   - 页面组件调用 `registerComponent(key)` 注册
   - 页面函数调用 `callComponentMethod(key, method)` 调用

3. **测试辅助函数统一接口**
   - Agent 通过 `callComponentMethod` 调用
   - Playwright 通过 `window.XyncraTestHelpers` 调用
   - 页面组件内部也可以直接调用

### 环境约束
1. **Docker E2E 环境**
   - Redis: `localhost:16379`
   - Server: `localhost:18080`
   - Jaeger: `localhost:16687`

2. **Vue Dev Server**
   - URL: `http://localhost:8848` (或配置的端口)
   - 需要确保 FloatingAssistant 连接成功（绿色指示灯）

3. **ui-assistant Agent**
   - 配置文件: `agents/ui-assistant.md`
   - 知道如何调用 `pg_*` 函数和通用 DOM 函数

---

## 文件位置参考

### 核心文件
- **提示词**: `.claude/docs/task-planner-prompts/083-vue-demo-full-implementation.md`
- **测试技能**: `.claude/skills/xyncra-vue-demo-test/SKILL.md`
- **测试辅助函数**: `demo/vue-pure-admin/src/test-helpers/index.ts`
- **产品决策**: `docs/decisions/PRODUCT_DECISIONS.md` (D-136)

### Vue Demo 文件
- **入口**: `demo/vue-pure-admin/src/main.ts`
- **根组件**: `demo/vue-pure-admin/src/App.vue`
- **路由配置**: `demo/vue-pure-admin/src/router/index.ts`
- **页面组件**: `demo/vue-pure-admin/src/views/**/index.vue`

### Agent 配置
- **ui-assistant**: `agents/ui-assistant.md`
- **Docker E2E**: `deploy/docker-compose.e2e.yml`

---

## 验证清单

### 功能验证
- [ ] 每个页面组件已导入并暴露测试辅助函数
- [ ] `registerComponent` 正确注册组件
- [ ] `defineExpose` 正确暴露函数
- [ ] Agent 可以通过 `pg_*` 函数调用
- [ ] Playwright 可以通过 `window.XyncraTestHelpers` 调用

### 测试验证
- [ ] Playwright 测试环境已配置
- [ ] 测试用例已编写（至少 10 个场景）
- [ ] 测试可以通过 WebSocket 发送指令
- [ ] 测试可以验证页面状态
- [ ] 测试可以验证 IndexedDB 数据

### 质量验证
- [ ] 所有测试通过
- [ ] Agent 可以操作所有页面
- [ ] Playwright 可以验证所有功能
- [ ] 没有 DOM 操作绕过权限控制

---

## 下一步行动

### 立即执行
1. 修改 P0 页面组件（Login, SchemaForm, Table, System/User）
2. 导入并暴露测试辅助函数
3. 编写并运行 Playwright 测试

### 后续执行
1. 修改 P1 页面组件
2. 修改 P2/P3 页面组件
3. 实施声明式注册模式 (D-136)

---

## 常见问题

### Q1: 如何验证 FloatingAssistant 已连接？
**A**: 检查右下角悬浮按钮是否为绿色，或通过 Playwright 检查 DOM：
```typescript
await page.waitForFunction(() => {
  const btn = document.querySelector('[data-testid="floating-assistant"]')
  return btn && btn.getAttribute('data-status') === 'connected'
})
```

### Q2: Agent 如何调用页面函数？
**A**: Agent 通过 WebSocket 接收指令，然后调用 `callComponentMethod(key, method, args)`：
```typescript
// Agent 内部调用
callComponentMethod('schema-form', 'fillForm', { title: '测试' })
```

### Q3: Playwright 如何调用测试辅助函数？
**A**: 通过 `page.evaluate` 执行浏览器 JS：
```typescript
const result = await page.evaluate(() => {
  return (window as any).XyncraTestHelpers.fillForm({ title: '测试' })
})
```

### Q4: 何时使用声明式注册模式？
**A**: 当需要修改大量页面组件时，建议先使用手动方式确保功能正确，然后在合适的时机统一重构为声明式模式。

---

## 联系信息

如有问题，请查看：
- 产品决策文档: `docs/decisions/PRODUCT_DECISIONS.md`
- 测试技能文档: `.claude/skills/xyncra-vue-demo-test/SKILL.md`
- 提示词文档: `.claude/docs/task-planner-prompts/083-vue-demo-full-implementation.md`
