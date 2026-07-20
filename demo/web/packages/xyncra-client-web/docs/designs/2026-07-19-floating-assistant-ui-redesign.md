# Floating Assistant UI 重构设计方案

**日期**: 2026-07-19
**状态**: Draft — 审阅通过后实施
**基于**: `docs/plans/2026-07-19-floating-ai-assistant-design.md` 架构决策

---

## 1. 问题陈述

### 1.1 当前 UI 问题

| # | 问题 | 位置 | 严重度 |
|---|------|------|--------|
| P0 | "思考中" (`Think`) 组件不跟随消息流 | `MessageArea.tsx:112` | **Bug** |
| P1 | 三栏布局浪费水平空间（800px 窗口仅 ~360px 给消息） | `ChatWindow.tsx` | 设计 |
| P2 | 浮出按钮纯蓝色圆形无质感 | `FloatingButton.tsx` | 设计 |
| P3 | 展开/折叠硬切换，无过渡动画 | `FloatingAssistant.tsx:41-44` | 设计 |
| P4 | `ConnectionStatus` 在左栏和中栏重复渲染 | `ChatWindow.tsx:53-62` | 设计 |
| P5 | 消息内容为纯文本，无 Markdown 渲染 | `MessageArea.tsx:67-74` | 设计 |
| P6 | 无 Markdown 代码块语法高亮 | `MessageArea.tsx` | 设计 |
| P7 | `ChatWindow` 残留 `console.log` | `ChatWindow.tsx:48` | 代码质量 |

### 1.2 "思考中" Bug 根因分析

```
当前 DOM 结构:
<div class="scroll-container" style="overflow: auto">
  <Bubble.List />         ← 内部管理自己的滚动/渲染
  {isTyping && <Think />} ← 在 Bubble.List 外部追加
</div>
```

`Think` 渲染在 `Bubble.List` **之后**作为 DOM 兄弟节点。当消息列表超出视口高度时，`Bubble.List` 内部滚动与其容器滚动冲突。用户翻到最底部时，`Think` 可能被 `Bubble.List` 的内部滚动容器遮挡或定位在不可见区域。

**修复方向**: 将 `Think` 作为 `Bubble.List` 的最后一个 `items` 条目，或用 `useRef` + `IntersectionObserver` 实现底部锚定。

---

## 2. 设计目标

1. **从"笨重面板"到"优雅助手"** — 不遮挡页面主体内容
2. **修复所有 P0 级 Bug** — "思考中"定位是第一优先级
3. **单栏聚焦** — 去掉三栏布局，Agent/会话切换降级为轻量控件
4. **Markdown 渲染** — AI 回复内容支持完整 Markdown
5. **平滑动画** — 展开/折叠有过渡感
6. **主题感知** — 尊重 Ant Design Pro 的亮/暗主题

---

## 3. 方案: 右侧滑出侧边栏 (IDE Sidebar)

### 3.1 布局总览

```
┌─────────────────────────────────────────────────────┐
│  Ant Design Pro 页面内容     ┌──────── 420px ──────┤│
│                               │                      ││
│                               │  ┌────────────────┐  ││
│                               │  │  Header         │  ││
│                               │  │  ● 已连接  ✕    │  ││
│                               │  └────────────────┘  ││
│                               │  ┌────────────────┐  ││
│                               │  │ Agent Tabs     │  ││
│                               │  │ [🤖 Test Bot]  │  ││
│                               │  │ [🌤 Weather] V │  ││
│                               │  └────────────────┘  ││
│                               │  ┌────────────────┐  ││
│                               │  │ 会话列表(可折叠)│  ││
│                               │  │ [+ 新会话]     │  ││
│                               │  │ ──────────────  │  ││
│                               │  │ 会话 1          │  ││
│                               │  │ 会话 2          │  ││
│                               │  └────────────────┘  ││
│                               │                      ││
│                               │  ┌────────────────┐  ││
│                               │  │ Message Area    │  ││
│                               │  │ ┌──────┐       │  ││
│                               │  │ │ User  │       │  ││
│                               │  │ └──────┘       │  ││
│                               │  │ ┌──────────┐   │  ││
│                               │  │ │ AI回复     │  │  ││
│                               │  │ │ Markdown   │  │  ││
│                               │  │ └──────────┘   │  ││
│                               │  │ ┌──────────┐   │  ││
│                               │  │ │[思考中]   │   │  ││ ← 锚定在消息流底部
│                               │  │ └──────────┘   │  ││
│                               │  └────────────────┘  ││
│                               │  ┌────────────────┐  ││
│                               │  │ Sender Input   │  ││
│                               │  │ [输入消息...🚀]│  ││
│                               │  └────────────────┘  ││
│                               │                      ││
│  [Floating Button]            │                      ││
│                               └──────────────────────┘│
└─────────────────────────────────────────────────────┘
```

### 3.2 组件树

```
FloatingAssistant (顶层编排)
├── FloatingButton ← 56px 圆形，点击展开
└── SidebarPanel (新增，动画容器)
    ├── Header
    │   ├── ConnectionStatus (单例)
    │   ├── AgentDisplay (当前 Agent 名称)
    │   └── CloseButton
    ├── AgentSelector (紧凑模式: tabs 或 dropdown)
    ├── ConversationPanel (可折叠区域)
    │   ├── NewConversationButton
    │   └── ConversationList
    └── ChatPanel
        ├── MessageArea
        │   ├── Bubble.List (含 Think 作为末项)
        │   └── AutoScrollAnchor (ref 锚点)
        └── Sender
```

### 3.3 关键变更: 从三栏到单栏

| 变更 | 当前 | 新方案 |
|------|------|--------|
| 布局 | 左(200) + 中(240) + 右(flex) | 单列全宽 420px |
| Agent 选择 | 左栏全高度列表 | 页面顶部紧凑 tabs/dropdown |
| 会话列表 | 中栏全高度列表 | 可折叠面板(默认展开，可收起) |
| 消息区 | flex:1 填充剩余 | 占据侧边栏主体空间 |
| 窗口尺寸 | 800x600 固定 | 420px x 100vh |
| 定位 | fixed bottom-right | fixed right-0 top-0 |

### 3.4 动画方案

```
折叠 → 展开:
  SidebarPanel: transform: translateX(100%) → translateX(0)
                transition: transform 250ms cubic-bezier(0.4, 0, 0.2, 1)
  FloatingButton: opacity: 1 → 0 (或 scale 缩小)
  
展开 → 折叠:
  反向动画，FloatingButton 在 SidebarPanel 完全退出后淡入
```

使用 CSS `@keyframes` 或 `framer-motion`。优先纯 CSS / Ant Design 内置动画，最低依赖。

---

## 4. 组件详细设计

### 4.1 FloatingButton (重建)

```tsx
// 全新设计，不再只是蓝色圆形
// - 显示当前 Agent 的小图标/头像
// - 有轻微脉动动画 (有未读时可提醒)
// - hover 时显示提示 "打开 AI 助手"
// - 过渡到新位置: bottom-24 → right-24 (窗口展开后隐藏)
```

### 4.2 Header (新增)

- 左侧: `ConnectionStatus` 小圆点 + "已连接" / "未连接"
- 中间: 当前 Agent 名称 (如 "Test Bot")
- 右侧: 关闭按钮 `✕` (或 Ant Design `CloseOutlined`)
- 背景: 轻微半透明毛玻璃 (`backdrop-filter: blur`)

### 4.3 AgentSelector (重构)

从固定列表改为紧凑式选择器:
- **方案 A (推荐)**: Ant Design `Segmented` 组件横向排列 Agent 名称
- **方案 B**: Ant Design `Select` 下拉选择器
- 选中项高亮，切换时重置会话选择

### 4.4 ConversationPanel (重构)

- 折叠面板: Ant Design `Collapse` 组件，带"新建会话"按钮
- 会话列表用 `Conversations` (保持现有)
- 默认展开，用户可收起腾出消息区域空间
- 收起后显示为 2 行摘要 + 会话计数

### 4.5 MessageArea + "思考中" 修复 (核心)

**修复方案**: 将 `Think` 内联到 `Bubble.List` 的 items 中，作为最后一项渲染:

```tsx
const bubbleItems = useMemo(() => {
  const items = messages.map(msg => ({
    key: msg.id,
    content: msg.content,
    role: msg.senderId === 'user' ? 'user' : 'ai',
  }));

  // Think indicator as last bubble item
  if (isTyping) {
    items.push({
      key: '__thinking__',
      content: '',
      role: 'ai',
      loading: true,  // Bubble.List 自带的 loading 态
    });
  }
  return items;
}, [messages, isTyping]);
```

同时使用 `useRef` + `useEffect` 实现**自动滚动到底部**:

```tsx
const bottomRef = useRef<HTMLDivElement>(null);

useEffect(() => {
  bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
}, [messages, isStreaming, isTyping]);

// 在 Bubble.List 下方
<Bubble.List items={bubbleItems} role={ROLE_CONFIG} />
<div ref={bottomRef} />  // 锚点
```

### 4.6 Markdown 渲染 (新增)

**方案**: 使用 `react-markdown` + `react-syntax-highlighter` 实现:

```tsx
import ReactMarkdown from 'react-markdown';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism';

// 在 Bubble content 中渲染 Markdown:
content: <ReactMarkdown
  components={{
    code({ className, children }) {
      const match = /language-(\w+)/.exec(className || '');
      return match ? (
        <SyntaxHighlighter style={oneDark} language={match[1]}>
          {String(children).replace(/\n$/, '')}
        </SyntaxHighlighter>
      ) : (
        <code className={className}>{children}</code>
      );
    }
  }}
>{msg.content}</ReactMarkdown>
```

**依赖评估**:
- `react-markdown` — ~25KB gzip, 纯 React 渲染
- `react-syntax-highlighter` — ~15KB gzip (按需加载语言)
- 合计 ~40KB gzip，可接受

### 4.7 HITLDialog (保持)

- 保持不变，Modal 形态已经合适
- 唯一修改: 适配新侧边栏的 z-index

---

## 5. 样式系统

### 5.1 色板

| 用途 | 亮色主题 | 暗色主题 | Ant Design Token |
|------|----------|----------|------------------|
| 侧边栏背景 | `#ffffff` | `#1f1f1f` | `colorBgElevated` |
| 消息区背景 | `#f5f5f5` | `#141414` | `colorBgLayout` |
| 用户气泡 | `#1677ff` | `#1677ff` | `colorPrimary` |
| AI 气泡 | `#ffffff` | `#2a2a2a` | `colorBgContainer` |
| 边框分割 | `#f0f0f0` | `#303030` | `colorBorderSecondary` |
| 毛玻璃效果 | `rgba(255,255,255,0.85)` | `rgba(0,0,0,0.75)` | — |

### 5.2 圆角 & 阴影

| 元素 | 值 |
|------|-----|
| 侧边栏 | `border-radius: 0` (从右侧贴边滑出) |
| 消息气泡 | `border-radius: 12px` |
| AI 气泡 | `border-radius: 12px 12px 12px 0` |
| 浮出按钮 | `border-radius: 50%` |
| 侧边栏阴影 | `box-shadow: -4px 0 24px rgba(0,0,0,0.10)` |

### 5.3 响应式

| 视口宽度 | 侧边栏宽度 |
|-----------|-----------|
| ≥1200px | 420px |
| 768-1199px | 380px |
| <768px | 100vw (全屏覆盖) |

---

## 6. 迁移计划

| 步骤 | 文件 | 工作量 |
|------|------|--------|
| 1. 新增 `dependencies` (react-markdown + syntax-highlighter) | `package.json` | ~2 min |
| 2. 重写 `FloatingAssistant.tsx` — 添加动画容器状态管理 | `FloatingAssistant.tsx` | ~10 min |
| 3. 新建 `SidebarPanel.tsx` — 侧边栏骨架 + Header + 布局 | (新增文件) | ~15 min |
| 4. 重构 `AgentSelector.tsx` — 从 List 改为 Segmented/Select | `AgentSelector.tsx` | ~10 min |
| 5. 重构 `ConversationList.tsx` — 改为可折叠区 | `ConversationList.tsx` | ~10 min |
| 6. 重写 `MessageArea.tsx` — 修复 Think 定位 + auto-scroll | `MessageArea.tsx` | ~15 min |
| 7. 新增 `MarkdownRenderer.tsx` — Markdown 渲染组件 | (新增文件) | ~10 min |
| 8. 重写 `styles.ts` — 新式样系统 | `styles.ts` | ~10 min |
| 9. 清理 `ConnectionStatus` 重复渲染 | `ChatWindow.tsx` → 移除 | ~2 min |
| 10. 清理 `console.log` | `ChatWindow.tsx` | ~1 min |
| **合计** | | **~85 min (CC)** |

### 文件变更清单

```
新增:
  SidebarPanel.tsx
  MarkdownRenderer.tsx

修改:
  FloatingAssistant.tsx    (重写: 添加动画逻辑)
  FloatingButton.tsx       (重写: 新样式 + 动画)
  MessageArea.tsx          (重写: Think 修复 + Markdown + auto-scroll)
  AgentSelector.tsx        (重构: 紧凑模式)
  ConversationList.tsx     (重构: 可折叠)
  ChatWindow.tsx           (移除: 解构到 SidebarPanel)
  styles.ts                (重写: 新式样系统)
  index.tsx                (添加新导出)

删除:
  ConnectionStatus.tsx     (合并到 Header)
  AgentDetail.tsx          (暂不启用)
```

---

## 7. 未覆盖 & 未来

| 内容 | 说明 |
|------|------|
| Markdown 的 LaTeX 数学公式 | 后续需要时添加 `remark-math` / `rehype-katex` |
| 代码块复制按钮 | 后续增强 |
| 文件/图片上传 | 协议层尚未支持 |
| 语音输入 | 超出当前范围 |
| 多窗口拖拽 | 超出当前范围 |

---

## 审阅清单

请确认以下决策:

1. **形态**: 右侧 420px 滑出侧边栏，全屏高度 — ✅ 已确认
2. **Markdown 渲染**: 引入 `react-markdown` + `react-syntax-highlighter` — 同意/拒绝?
3. **Agent 选择器**: 使用 `Segmented` 横向排列 — 同意/拒绝/用 `Select` 替代?
4. **会话列表**: 可折叠面板 (Collapse) — 同意/拒绝?
5. **FloatingButton**: 保留圆形但改为带 Agent 图标 + 脉动动画 — 同意/有其他想法?
6. **迁移策略**: 直接重写文件 (85min CC 估算) — 启动实施/先审阅设计稿?
