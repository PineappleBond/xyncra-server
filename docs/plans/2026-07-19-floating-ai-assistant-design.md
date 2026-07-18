# Xyncra Web Floating AI Assistant 设计文档

**日期**: 2026-07-19
**状态**: Draft
**作者**: Xyncra Team

---

## 1. 概述

在 demo web app（Ant Design Pro）中添加全局悬浮 AI 助手，通过第4个 package `@xyncra/client-web` 连接 Xyncra 服务器。用户可以与 Agent 对话，Agent 通过反向 RPC 调用客户端注册的 UI 操作函数来操控页面。

**核心目标**：演示 "AI 操作 UI" — 只要注册的函数足够全面，AI 理论上可以操控一切。

### 1.1 关键决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 包结构 | 新建第4个package `xyncra-client-web` | 可复用，职责清晰 |
| 架构模式 | Browser Adapter + React Hooks | 纯数据层，UI 用 @ant-design/x 自建 |
| 现有 chatbot 页 | 保持独立，共存 | 两者功能不同：chatbot 纯对话，悬浮助手能操作 UI |
| 函数范围 | 最小可行集（4个函数） | 证明链路可行，快速出 demo |
| 会话模型 | 多会话 + Agent 列表 | 用户选择 Agent 创建对话 |
| HITL | 支持 | 利用 xyncra 协议已有的 HITL 机制 |
| Agent 发现 | 前端硬编码 | demo 够用，未来可扩展 |
| 函数清单更新 | 动态更新 | 支持页面级动态注册/反注册 |
| 悬浮窗口 | 不可关闭，只可最小化 | 保证始终可用 |
| UI 组件 | 全部使用 Ant Design Pro 自带组件 | antd + ProComponents + @ant-design/x |

## 2. 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                    Demo Web App (Ant Design Pro)             │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  src/app.tsx layout (childrenRender)                 │   │
│  │  ┌────────────────────────────────────────────────┐  │   │
│  │  │  Page Content (dashboard, forms, lists...)     │  │   │
│  │  └────────────────────────────────────────────────┘  │   │
│  │                                                       │   │
│  │  ┌────────────────────────────────────────────────┐  │   │
│  │  │  <FloatingAssistant />  ← 全局悬浮组件          │  │   │
│  │  │  ┌──────────────────────────────────────────┐  │  │   │
│  │  │  │  @ant-design/x UI                        │  │  │   │
│  │  │  │  - 折叠态: 圆形按钮                       │  │  │   │
│  │  │  │  - 展开态: Agent列表 | 对话内容            │  │  │   │
│  │  │  └──────────────────────────────────────────┘  │  │   │
│  │  └──────────────────────────────────────────────┘  │   │
│  └──────────────────────────────────────────────────────┘   │
│                          ▲ hooks                              │
│                          │                                    │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  packages/xyncra-client-web/                         │   │
│  │  ┌─────────────┐ ┌──────────┐ ┌──────────────────┐  │   │
│  │  │  Adapters   │ │  Hooks   │ │ Function Registry│  │   │
│  │  │ (WS, IDB)   │ │ (React)  │ │  (UI操作函数)    │  │   │
│  │  └──────┬──────┘ └────┬─────┘ └───────┬──────────┘  │   │
│  │         │              │               │              │   │
│  │         └──────────────┴───────────────┘              │   │
│  │                        │                              │   │
│  │              ┌─────────▼──────────┐                   │   │
│  │              │  XyncraClient      │                   │   │
│  │              │  (@xyncra/client-  │                   │   │
│  │              │   core)            │                   │   │
│  │              └─────────┬──────────┘                   │   │
│  └────────────────────────┼──────────────────────────────┘   │
│                           │ WebSocket                        │
└───────────────────────────┼──────────────────────────────────┘
                            │
                    ┌───────▼───────┐
                    │ Xyncra Server  │
                    │ (Go backend)   │
                    │                │
                    │  ┌──────────┐  │
                    │  │  Agent   │  │
                    │  │ (LLM +  │  │
                    │  │  Tools) │  │
                    │  └──────────┘  │
                    └────────────────┘
```

## 3. Package 复用分析

### 3.1 完全复用

- **`@xyncra/protocol`** — 100% 复用。纯类型包，零依赖。
  - `Package`, `PackageType`, `PackageDataRequest/Response/Updates` — WebSocket 消息信封
  - `FunctionInfo`, `ReturnInfo` — 函数注册格式
  - `UpdateType` — 9 种更新类型
  - `ErrorCode`, `HandlerError` — 错误处理

- **`@xyncra/client-core`** — 100% 复用。环境无关的核心，所有环境依赖通过接口注入。

| 需要注入的接口 | CLI 做法 | 浏览器做法 |
|---|---|---|
| `IWebSocketFactory` | `ws` 库 | 原生 `WebSocket` |
| `IIndexedDBProvider` | `fake-indexeddb` | 原生 `indexedDB`（Dexie） |
| `ILogger` | console | console |
| `IUpdateHandler` | CLIUpdateHandler（stdout） | ReactUpdateHandler（React state） |

核心能力全部复用：
- `ConnectionManager` — WebSocket 连接/重连/缓冲
- `SyncManager` — 数据同步、gap 恢复
- `RetryManager` — RPC 重试
- `registerRequestHandler()` — 反向 RPC，AI 调用客户端函数的核心机制
- 所有便利方法：`sendMessage`, `createConversation`, `listConversations`, `getMessages` 等
- HITL 支持：`Question` 模型

### 3.2 不复用

- **`@xyncra/client-cli`** — 不可复用。Node.js 特有（Unix Socket IPC、`fs-ext`、`fake-indexeddb`、`commander`）。但函数注册模式和 UpdateHandler 模式可作为参考。

## 4. `@xyncra/client-web` Package 设计

### 4.1 浏览器适配器

```typescript
// adapters/websocket.ts
export class BrowserWebSocketFactory implements IWebSocketFactory {
  create(url: string): IWebSocket {
    return new WebSocket(url);  // 浏览器原生
  }
}

// adapters/indexeddb.ts
// Dexie 已能在浏览器运行，包装成 IIndexedDBProvider
export class BrowserIndexedDBProvider implements IIndexedDBProvider { ... }
```

### 4.2 React Context Provider

```typescript
interface XyncraContextValue {
  client: XyncraClient | null;
  connectionState: 'connecting' | 'connected' | 'disconnected' | 'reconnecting';
  userID: string;
  deviceID: string;
}

export const XyncraProvider: React.FC<{
  serverURL: string;
  userID: string;
  deviceID?: string;  // 默认生成 UUID 存 localStorage
  children: React.ReactNode;
}>;
```

职责：
- 创建并管理 `XyncraClient` 单例生命周期
- 注入浏览器适配器
- 维护连接状态
- `deviceID` 未提供时生成并持久化到 localStorage

### 4.3 React Hooks

| Hook | 功能 |
|------|------|
| `useXyncra()` | 连接状态、client 实例 |
| `useConversations()` | 会话列表（从 IndexedDB 响应式读取） |
| `useMessages(conversationId)` | 指定会话的消息列表 + 发送方法 |
| `useStreaming(conversationId)` | 流式文本（streaming update 拼接） |
| `useAgentStatus(conversationId)` | Agent 状态（thinking/tool_calling/generating 等） |
| `useHITL()` | 待回答的 HITL 问题 + 回答方法 |
| `useRegisterFunction(name, config)` | 注册单个 UI 操作函数 |
| `useRegisterFunctions(configs)` | 批量注册函数 |

### 4.4 函数注册 API

核心设计原则：**包消费者能以最少的代码、最直观的方式注册函数。**

```typescript
// 单个注册 — 适合页面级动态注册
useRegisterFunction('navigate_to', {
  description: 'Navigate to a page by path',
  parameters: {
    type: 'object',
    properties: {
      path: { type: 'string', description: 'Route path' }
    },
    required: ['path']
  },
  handler: async ({path}) => {
    history.push(path);
    return { success: true };
  }
});

// 批量注册 — 适合全局一次性注册
useRegisterFunctions({
  navigate_to: { description: '...', parameters: {...}, handler: async (...) => {...} },
  show_notification: { description: '...', handler: async (...) => {...} },
});
```

关键特性：
- **自动清理**：组件卸载时自动反注册
- **动态响应**：依赖变化时自动重新注册
- **页面级注册**：任何页面组件都可注册自己的函数
- **全局注册**：在 FloatingAssistant 或 app.tsx 层注册通用函数
- **动态清单同步**：每次注册变化时调用 `system.register_functions` 更新全量清单到服务端

## 5. 悬浮助手 UI 设计

### 5.1 放置位置

在 `src/app.tsx` 的 `childrenRender` 中注入：

```typescript
childrenRender: (children) => (
  <XyncraProvider serverURL={WS_URL} userID={currentUser?.userid}>
    {children}
    <FloatingAssistant />
    <SettingDrawer ... />
  </XyncraProvider>
),
```

### 5.2 折叠态

右下角圆形按钮（antd `FloatButton` 或自定义），点击展开。

### 5.3 展开态

```
┌────────────────────────────────────┐
│ 🤖 AI Assistant              [—]   │  ← 只有最小化，没有 ×
├──────┬─────────────────────────────┤
│ 左侧 │ 右侧内容区                   │
│      │                             │
│ 🤖   │  [视图A] Agent 详情          │
│ Agents│  ┌─────────────────────┐   │
│ ───── │  │ UI Demo Agent       │   │
│ 🤖 UI │  │ 一个能操作UI的demo   │   │
│   Demo│  │ agent               │   │
│       │  │                     │   │
│ 📋    │  │ [💬 开始对话]        │   │  ← 点击创建对话
│   Data│  └─────────────────────┘   │
│       │                             │
│ 💬   │  [视图B] 对话中              │
│ 会话  │  ┌─────────────────────┐   │
│ ───── │  │ User: 帮我跳转...    │   │
│ 💬 #1 │  │ AI: 好的...          │   │
│ 💬 #2 │  │ 🔄 navigate_to       │   │  ← 函数调用可视化
│       │  └─────────────────────┘   │
│       │  ┌─────────────────────┐   │
│       │  │ 输入消息...  [发送]  │   │
│       │  └─────────────────────┘   │
└──────┴─────────────────────────────┘
```

### 5.4 UI 组件列表

全部使用 Ant Design Pro 自带组件：

| 组件 | antd/x 来源 | 用途 |
|------|-------------|------|
| 折叠按钮 | `FloatButton` 或 antd `Button` | 右下角悬浮按钮 |
| 展开窗口 | antd `Card` / `Drawer` | 窗口壳 |
| Agent 列表 | `@ant-design/x` `Conversations` | Agent 选择 |
| 会话列表 | `@ant-design/x` `Conversations` | 历史会话 |
| 消息气泡 | `@ant-design/x` `Bubble.List` | 对话消息 |
| 输入框 | `@ant-design/x` `Sender` | 消息输入 |
| Markdown 渲染 | `@ant-design/x-markdown` `XMarkdown` | AI 回复渲染 |
| 思考过程 | `@ant-design/x` `Think` | Agent 思考展示 |
| HITL 弹窗 | antd `Modal` | 确认对话框 |
| 通知 | antd `notification` | show_notification 函数实现 |
| 状态指示 | antd `Badge` + `Tooltip` | 连接状态 |

### 5.5 Agent 配置（硬编码）

```typescript
export interface AgentConfig {
  agentId: string;
  name: string;
  avatar: string;
  description: string;
  capabilities: string[];
}

export const AGENTS: AgentConfig[] = [
  {
    agentId: 'agent/ui-demo',
    name: 'UI Demo Agent',
    avatar: '🤖',
    description: '一个能操作UI的demo agent，可以帮你跳转页面、显示通知、高亮元素。',
    capabilities: ['跳转到任意页面', '显示通知消息', '高亮页面元素', '查看当前页面信息'],
  },
];
```

## 6. Demo 函数集

### 6.1 最小可行函数集

| 函数名 | 参数 | 功能 |
|--------|------|------|
| `navigate_to` | `{path: string}` | 路由跳转 |
| `show_notification` | `{type: 'success'\|'error'\|'info'\|'warning', message: string, description?: string}` | antd 通知提示 |
| `highlight_element` | `{selector: string, duration_ms?: number}` | CSS 高亮指定 DOM 元素 |
| `get_current_page` | `{}` | 返回当前路由 path + 页面标题 |

### 6.2 函数注册位置

全局函数在 `FloatingAssistant` 组件中通过 `useDemoFunctions()` 统一注册。页面级函数在各页面组件中注册。

## 7. 实时状态 & HITL

### 7.1 流式响应

`useStreaming(conversationId)` 收集 `streaming` update（seq=0）的文本片段，拼接到当前 AI 回复。`done: true` 时完整文本持久化为 Message。使用 `@ant-design/x` 的 `Bubble` + `XMarkdown` 渲染，支持流式动画。

### 7.2 Agent 状态显示

| Agent Status | UI 显示 |
|---|---|
| `idle` | 无特殊指示 |
| `thinking` | 🧠 "思考中..." |
| `tool_calling` | 🔧 "调用函数: {name}" |
| `generating` | ✍️ "生成回复..." + 流式文本 |
| `asking_user` | HITL 问题弹窗 |
| `timeout` | ⚠️ "Agent 超时" |

### 7.3 HITL 流程

1. Agent 调用函数前需要确认 → Server 创建 Question → 推送 `agent_status: asking_user`
2. `useHITL()` 检测到新 Question → `HITLDialog` 弹窗
3. 用户点击确认/拒绝 → `agent_resume` RPC → Agent 继续或取消

### 7.4 连接状态

| State | UI |
|---|---|
| `connecting` | 悬浮按钮旋转动画 |
| `connected` | 绿色指示 |
| `reconnecting` | 黄色指示 + tooltip |
| `disconnected` | 红色指示 |

## 8. 端到端流程

```
用户在悬浮助手输入: "帮我跳转到dashboard"
         │
         ▼
  FloatingAssistant 调用 sendMessage()
         │
         ▼
  XyncraClient → WebSocket → Xyncra Server
         │
         ▼
  Server 路由到 Agent (UI Demo Agent)
         │
         ▼
  Agent (LLM) 决定调用 navigate_to({path: "/dashboard/analysis"})
         │
         ▼
  Server 发送 Reverse RPC → 客户端
         │
         ▼
  XyncraClient.registerRequestHandler('navigate_to') 触发
         │
         ▼
  handler 执行: history.push('/dashboard/analysis')
         │                                          ← 页面跳转了！
         ▼
  返回 {success: true} → Server → Agent
         │
         ▼
  Agent 回复用户: "已跳转到 Dashboard Analysis 页面"
         │
         ▼
  悬浮助手流式显示回复（streaming update）
  同时 agent_status 显示: thinking → tool_calling → generating
```

## 9. 文件结构

```
packages/xyncra-client-web/              # 第4个 package
├── package.json
├── tsconfig.json
├── src/
│   ├── index.ts
│   ├── adapters/
│   │   ├── websocket.ts                  # BrowserWebSocketFactory
│   │   └── indexeddb.ts                  # BrowserIndexedDBProvider
│   ├── context/
│   │   └── XyncraProvider.tsx            # React Context + Client 生命周期
│   ├── hooks/
│   │   ├── useXyncra.ts
│   │   ├── useConversations.ts
│   │   ├── useMessages.ts
│   │   ├── useStreaming.ts
│   │   ├── useAgentStatus.ts
│   │   ├── useHITL.ts
│   │   ├── useRegisterFunction.ts
│   │   └── useRegisterFunctions.ts
│   └── internal/
│       ├── ReactUpdateHandler.ts         # IUpdateHandler → React state
│       └── FunctionRegistry.ts           # 函数注册中心 + 动态清单同步

demo/web/src/components/FloatingAssistant/
├── index.tsx                              # 主组件
├── FloatingButton.tsx                     # 折叠态按钮
├── ChatWindow.tsx                         # 展开态窗口
├── AgentSelector.tsx                      # 左侧 Agent 列表
├── AgentDetail.tsx                        # Agent 详情页
├── ConversationList.tsx                   # 左侧会话列表
├── MessageArea.tsx                        # 消息区
├── FunctionCallDisplay.tsx                # 函数调用可视化
├── HITLDialog.tsx                         # HITL 确认弹窗
├── useDemoFunctions.ts                    # 注册 4 个 demo 函数
├── agentConfig.ts                         # 硬编码 Agent 列表
└── style.ts                               # 样式
```

## 10. 实现阶段

| 阶段 | 内容 | 目标 |
|------|------|------|
| Phase 1 | `xyncra-client-web` 基础：adapters + XyncraProvider + useXyncra | 连通 WebSocket |
| Phase 2 | 消息相关 hooks：useConversations + useMessages + useStreaming | 能聊天 |
| Phase 3 | 函数注册：useRegisterFunction(s) + FunctionRegistry | 能注册函数 |
| Phase 4 | HITL + Agent Status：useHITL + useAgentStatus | 完整交互 |
| Phase 5 | Floating Assistant UI：全部 UI 组件 | 有界面 |
| Phase 6 | 集成联调：接入真实 xyncra server + agent | 端到端跑通 |

## 11. 不在范围内（YAGNI）

- Agent 动态发现（硬编码替代）
- 函数参数的 TypeScript 自动类型推断
- Web Worker 隔离
- 离线消息缓存 UI
- 多设备同步 UI 状态
- 语音/图片消息
