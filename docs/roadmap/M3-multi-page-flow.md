# M3: 多页面流程引擎

**里程碑**: M3
**目标**: Agent 能执行跨页面的多步骤操作，自动编排导航、执行、检查、恢复。
**前置依赖**: M1（Schema 引擎）、M2（UI Context）
**预计工作量**: 大

---

## 理想状态

用户说「帮我下单一台 iPhone 16」，Agent 自动执行：

```
Step 1: 导航到 /products
        → 搜索 "iPhone 16"
        → 找到商品，确认信息
Step 2: 点击"加入购物车"
        → 确认已添加（检查购物车数量）
Step 3: 导航到 /cart
        → 确认 iPhone 16 在购物车中
        → 点击"去结算"
Step 4: 导航到 /checkout
        → 确认收货地址和金额
        → HITL: "确认下单？金额 ¥5999，收货地址: 北京市..."
        → 用户确认
Step 5: 点击"提交订单"
        → 确认订单创建成功
        → 返回订单号
```

任何步骤失败，Agent 能：
- 自动重试（网络抖动）
- 换路径（搜索不到时换个关键词）
- 回退上一步（加购失败时重新选择商品）
- 报告用户（库存不足时告知并询问替代方案）

---

## Phase 列表

### M3-P1: Flow Schema 定义

**目标**: 定义多步骤流程的声明式描述格式。

**交付物**:

1. **`FlowSchema` 类型定义**
   ```
   FlowSchema {
     name: string               // 流程名称
     description: string        // 流程描述，给 Agent 理解用
     params?: JSONSchema        // 流程入参（如商品名、数量）
     steps: FlowStep[]
     onError?: 'abort' | 'retry' | 'ask_user'  // 全局错误策略
     maxRetries?: number        // 全局最大重试次数
   }

   FlowStep {
     id: string
     name: string               // 步骤名称
     page: string               // 目标页面路由
     precondition?: string      // 前置条件描述（自然语言，Agent 判断）
     actions: FlowAction[]      // 本步骤要执行的操作
     waitFor?: WaitCondition    // 等待条件（页面加载、数据出现等）
     onSuccess?: 'next' | 'goto:{stepId}' | 'complete'  // 成功后行为
     onFail?: 'retry' | 'skip' | 'abort' | 'ask_user'   // 失败后行为
     maxRetries?: number        // 步骤级最大重试次数
   }

   FlowAction {
     tool: string               // 要调用的工具名
     params?: object | TemplateString  // 参数，支持引用流程入参或前序步骤结果
     expect?: ExpectCondition   // 期望结果
     confirm?: boolean          // 是否需要用户确认
   }

   WaitCondition {
     type: 'page_ready' | 'component_state' | 'element_visible'
     route?: string             // 期望的路由
     componentId?: string       // 期望的组件 ID
     stateKey?: string          // 期望的状态 key
     stateValue?: any           // 期望的状态值
     timeout?: number           // 超时（毫秒）
   }
   ```

2. **Flow Schema 示例**
   ```typescript
   const checkoutFlow: FlowSchema = {
     name: '商品下单',
     description: '帮用户完成从搜索商品到下单的完整流程',
     params: {
       type: 'object',
       properties: {
         productName: { type: 'string', description: '商品名称' },
         quantity: { type: 'number', description: '数量', default: 1 },
       },
       required: ['productName'],
     },
     steps: [
       {
         id: 'search',
         name: '搜索商品',
         page: '/products',
         actions: [
           { tool: 'search_products', params: { keyword: '{{params.productName}}' } },
         ],
         waitFor: { type: 'component_state', componentId: 'productList', stateKey: 'data_count', stateValue: (v) => v > 0 },
         onFail: 'ask_user',
       },
       {
         id: 'add_to_cart',
         name: '加入购物车',
         page: '/products',
         actions: [
           { tool: 'add_to_cart', params: { productId: '{{steps.search.productId}}', quantity: '{{params.quantity}}' }, confirm: true },
         ],
       },
       {
         id: 'checkout',
         name: '确认下单',
         page: '/checkout',
         actions: [
           { tool: 'submit_order', confirm: true },
         ],
       },
     ],
   }
   ```

3. **`@xyncra/flow-schema` 类型包**

**验证标准**: 能用 FlowSchema 完整描述一个「搜索商品 → 加购 → 下单」的流程，类型定义覆盖正常路径和异常路径。

---

### M3-P2: 页面就绪检测

**目标**: 导航后等待目标页面 schema 注册完成，确保工具可用。

**交付物**:

1. **页面就绪信号协议**
   - 客户端页面 SchemaProvider 挂载完成后，发送 `system.page_ready` RPC
   - 参数：`{ route: '/orders', schema_id: 'orderPage', tool_count: 8 }`
   - 服务端缓存每个 (userID, deviceID) 的当前页面就绪状态

2. **Agent 等待机制**
   - Agent 调用 `navigate_to` 后，不立即执行下一步操作
   - 等待目标页面的 `page_ready` 信号
   - 超时处理：默认 10 秒，可配置
   - 超时后：重试导航 / 报告用户

3. **客户端就绪检测增强**
   - SchemaProvider 不仅注册函数，还验证所有 collector 是否就绪
   - 确保 `get_{id}_state` 工具在页面就绪时能返回有效数据
   - 对于异步加载的组件（如表格数据），等待首次数据加载完成后再报告就绪

**验证标准**: Agent 执行 `navigate_to('/orders')` 后，能可靠地等到订单页面的工具注册完成再执行下一步。

---

### M3-P3: 流程执行器

**目标**: Agent 有 `start_flow` 工具，能自动编排多步骤操作。

**交付物**:

1. **服务端 Flow 执行器**
   - Agent 配置新增 `flow_executor` 工具
   - 工具入参：`{ flow: FlowSchema | flowName, params: object }`
   - 执行逻辑：
     ```
     for step in flow.steps:
       1. 导航到 step.page（如果不在目标页面）
       2. 等待 page_ready
       3. 检查 precondition（如果有）
       4. 依次执行 step.actions:
          - 调用工具，传入解析后的 params
          - 检查 expect 条件
          - 如果 confirm=true，触发 HITL 询问用户
       5. 根据 onSuccess 决定下一步
       6. 如果失败，根据 onFail 决定重试/跳过/中止/询问用户
     ```

2. **模板变量系统**
   - `{{params.xxx}}` — 引用流程入参
   - `{{steps.xxx.yyy}}` — 引用前序步骤的结果
   - `{{ui.componentId.stateKey}}` — 引用当前 UI 状态
   - 模板解析在服务端执行，解析后作为工具参数传递给客户端

3. **执行状态追踪**
   - 流程执行过程中，通过 ephemeral push 向客户端推送执行状态
   - 客户端可展示流程进度条：
     ```
     [✓] 搜索商品 → iPhone 16 找到
     [✓] 加入购物车 → 已添加
     [→] 确认下单 → 等待用户确认...
     ```
   - HITL 问题通过现有 `hitl:question` 机制推送

4. **流程执行上下文**
   - 执行器维护一个 `FlowContext` 对象
   - 包含：流程入参、每步的结果、当前步骤索引、重试次数、开始时间
   - Agent 可以通过 `get_flow_context` 工具读取当前执行状态

**验证标准**: Agent 调用 `start_flow(checkoutFlow, { productName: 'iPhone 16' })` 后，自动完成搜索→加购→下单全流程，每步有状态反馈。

---

### M3-P4: 错误恢复策略

**目标**: 流程中任何步骤失败时，系统能自动恢复或优雅降级。

**交付物**:

1. **错误分类与策略**
   ```
   错误类型:
   - network_error (网络抖动) → 自动重试，指数退避
   - tool_not_found (工具不存在) → 等待页面就绪后重试
   - precondition_fail (前置条件不满足) → 回退上一步重试
   - expect_fail (结果不符合预期) → 重试或 ask_user
   - user_reject (用户拒绝确认) → 中止流程
   - timeout (超时) → 重试或 ask_user
   - unknown → ask_user
   ```

2. **步骤断点恢复**
   - 流程执行过程中，每步完成后持久化执行状态到 Redis
   - 流程中断后（如用户关闭页面、网络断开），重新打开页面时可恢复
   - Agent 有 `resume_flow` 工具，从断点继续执行
   - 恢复时检查前置条件是否仍然满足

3. **HITL 介入点**
   - `onFail: 'ask_user'` 时触发 HITL
   - HITL 问题包含：失败步骤、失败原因、建议的恢复操作
   - 用户可选择：重试 / 跳过 / 换参数 / 中止
   - 用户的回答通过现有 HITL 机制回传给 Agent

4. **超时与死锁检测**
   - 单步超时：默认 30 秒
   - 流程总超时：默认 5 分钟
   - 死锁检测：如果同一步骤重试超过 N 次，强制 ask_user
   - 流程超时后：自动中止 + 通知用户

**验证标准**: 流程执行中模拟网络断开，用户重新连接后能从断点恢复继续执行。
