# Xyncra Vue Demo 开发需求

## 一、核心理念

**唯一目标：让 AI Agent 能控制 vue-pure-admin Demo 的所有页面、所有功能。**

Agent 可以通过 WebSocket 连接到 Xyncra Server，知道当前页面是什么、有什么可操作的元素，然后填表单、点按钮、切 Tab、翻页、提交数据——完全接管 UI 交互。

这个 Demo 不是为了复刻 React 版 Ant Design Pro 的页面，而是用 vue-pure-admin 的页面来**展示 Agent 控制 UI 的能力**。一旦 Agent 能操作所有页面，Demo 就成功了。

## 二、为什么从 React （~/go/src/github.com/PineappleBond/xyncra-server/demo/web） 迁移到 Vue （~/go/src/github.com/PineappleBond/xyncra-server/demo/vue-pure-admin）

### 核心原因：表单值设置方式

React（Ant Design Pro）中表单组件是**受控组件**，值由 state 管理，修改时必须通过 React 的合成事件系统触发 onChange 才能生效。Agent 要填写表单就得操作 React 内部的 fiber 树来模拟 onChange，这正是 `reactValueSetter.ts` 的职责——复杂、脆弱、与 React 版本耦合。

Vue 的 **v-model** 本质是**双向绑定**，直接修改响应式 ref 即可更新视图。Element Plus 也提供了组件实例的 setter API。Agent 填写表单时只需要：
1. 找到对应的组件 DOM 或组件实例
2. 直接设置值（`ref.value = xxx` 或 `instance.setValue(xxx)`）
3. 视图自动更新

不需要 fiber hack，不需要模拟事件冒泡，代码量更少、更稳定。

### 次要原因
- Demo 只是验证 Agent 控制 UI 的能力，框架本身不是产品
- 用 Vue 降低表单操作的复杂度，减少 Agent 操作出错的概率

## 三、现有可复用资产

以下代码是**框架无关的纯逻辑**，可直接复制到 vue-pure-admin 项目中使用：

| 资产 | 路径 | 说明 |
|------|------|------|
| `@xyncra/protocol` | `packages/xyncra-protocol/` | 类型定义（FunctionInfo、WebSocket 协议等），纯 TS，直接复制 |
| `@xyncra/client-core` | `packages/xyncra-client-core/` | XyncraClient 核心类（连接管理、重连、消息队列、重试、IndexedDB 持久化），纯 TS，直接复制 |
| `@xyncra/client-cli` | `packages/xyncra-client-cli/` | CLI工程 |


## 四、需要实现的核心功能

### 4.1 框架层（一次性建设）

| # | 功能 | 说明 |
|---|------|------|
| 1 | Xyncra Vue Plugin | 初始化 XyncraClient，provide client 实例给全局，页面加载后自动连接 ws://localhost:18080/ws |
| 2 | 函数注册系统 | 提供通用函数注册机制，支持按路由动态注册/注销函数。Vue Router 的 `afterEach` 中监听路由变化，自动切换当前页面的函数集 |
| 3 | DOM 操作引擎 | waitForSelector 轮询（已有）、waitForLoadingComplete（已有）、Vue/Element Plus 值设置（新：利用 Element Plus 的 API 或直接修改 v-model 绑定的 ref） |
| 4 | FloatingAssistant 悬浮助手 | 页面右下角悬浮按钮 + 侧边聊天面板。独立于布局，直接挂载在 App.vue |
| 5 | 连接状态管理 | 浮窗按钮颜色显示连接状态（绿/黄/红） |
| 6 | HITL 审批 | Agent 敏感操作时弹窗审批，支持批量 Tab 式处理 |

### 4.2 页面函数（持续扩展）

对 **vue-pure-admin 的每个页面**，注册对应的 Agent 函数，让 Agent 能操作该页面上的所有功能元素。

```
原则：一个页面一个 .functions.ts 文件，注册该页面所有可交互元素的函数。
函数命名：pg_{pageKey}_{elementType}_{description}
标签标记：tags 带 page:{pageKey} 用于路由匹配
```

**需要覆盖的页面范围**（vue-pure-admin 内置页面 + 新增页面）：

| 页面 | 需要 Agent 能做什么 |
|------|-------------------|
| 所有 Form 页面 | 填写所有类型的输入框/选择器/日期/开关/复选框/单选框/文本域，提交表单 |
| 所有 Table 页面 | 搜索、排序、翻页、查看详情、新增/编辑/删除行 |
| 所有 Tab 页面 | 切换 Tab，在对应 Tab 下操作 |
| Dashboard 页面 | 切换分析维度/时间范围，查看指标 |
| Detail 页面 | 查看数据，点击操作按钮 |
| 所有有交互的页面 | 点按钮、开关、链接、菜单项等 |

具体页面列表待确认（vue-pure-admin 内置了哪些页面就覆盖哪些页面），**关键是覆盖所有、不遗漏**。

### 4.3 聊天交互

FloatingAssistant 提供以下能力：

| # | 功能 | 说明 |
|---|------|------|
| 1 | 消息发送/接收 | 用户打字发送，Agent 流式回复（Markdown 渲染） |
| 2 | 会话管理 | 左侧历史会话列表，点击切换，可新建会话 |
| 3 | Agent 选择器 | 切换身份（alice/bob 等） |
| 4 | 函数调用可视化 | 消息流中展示 Agent 调用了什么函数、参数、结果 |
| 5 | 页面感知 | Agent 知道当前页面 URL 和页面描述，自动匹配该页面可用的函数 |

## 五、实施步骤

### Phase 1：基础设施
1. pnpm install 确认 vue-pure-admin 可用
2. vite.config.ts 配置代理（`/api/*` → `localhost:18080`，`/ws` → `ws://localhost:18080`）
3. 复制 `packages/xyncra-protocol/`、`packages/xyncra-client-core/`、`src/functions/dom-engine.ts`、`src/functions/utils/factory.ts`
4. 注册为 pnpm workspace
5. 验证：Network 面板看到 WebSocket 连接

### Phase 2：Vue 适配层
1. 创建 Xyncra Vue Plugin（初始化 client，provide/inject）
2. composables：useXyncra、useRegisterFunctions、useStreaming、useHITL、useConversations
3. 路由监听器：`router.afterEach` 触发函数切换
4. 验证：composable 可用，connect/disconnect 正常

### Phase 3：FloatingAssistant
1. 用 Vue + Element Plus 实现悬浮面板, 要适配现有的UI。不要做的很突兀。
2. ChatWindow + 流式 Markdown 渲染
3. HITLDialog 批量审批
4. 验证：打开发送消息，Agent 回复

### Phase 4：逐个页面覆盖
1. 选择一个 vue-pure-admin 页面，编写其 .functions.ts
2. 注册到路由监听中
3. 验证 Agent 能操作该页面所有交互元素
4. 循环直到所有页面被覆盖

**验收标准**：打开任意 vue-pure-admin 页面，Agent 都能通过自然语言指令操作该页面上所有功能。

## 六、注意事项

1. **不要照搬 React 的 reactValueSetter**。Vue 的 v-model 天然绑定 data ref，直接修改响应式数据即可。Element Plus 也提供了组件实例方法（如 `selectRef.setCurrentValue`）。
2. **覆盖所有页面比复刻旧函数更重要**。旧 Demo 的 21 个页面函数仅供参考，不要花时间 1:1 迁移，优先确保 vue-pure-admin 的每个页面都有函数覆盖。
3. **竞态条件**：`performReconnectHandshake` 必须先 `register_functions` 再发 `reconnect`。XyncraClient 中已经修好了，注意不要改回去。
4. **FloatingAssistant 独立于布局**：不依赖 vue-pure-admin 的 layout 系统，直接挂载到 App.vue，用 position:fixed。
5. **函数命名一致性**：`pg_{pageKey}_{elementType}_{description}`，tags 带 `page:{pageKey}`，方便路由自动匹配。
