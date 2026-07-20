import type { CSSProperties } from 'react';

/**
 * 新的样式系统 — 适配右侧滑出侧边栏形态
 * 
 * 关键变化:
 * - 侧边栏 420px × 100vh，贴右
 * - 单栏布局（不再三栏）
 * - 主题感知（使用 CSS 变量，兼容亮暗）
 * - 动画相关样式
 */
export const FLOATING_ASSISTANT_STYLES: Record<string, CSSProperties> = {
  // 顶层容器: fixed 定位, right-0 top-0, zIndex 1000
  container: {
    position: 'fixed',
    top: 0,
    right: 0,
    bottom: 0,
    zIndex: 1000,
    pointerEvents: 'none', // 让点击穿透，子容器单独控制
  },

  // 侧边栏面板: 420px 宽, 全高, 毛玻璃背景
  sidebar: {
    width: 420,
    height: '100vh',
    backgroundColor: 'var(--color-bg-elevated, #fff)',
    boxShadow: '-4px 0 24px rgba(0, 0, 0, 0.1)',
    display: 'flex',
    flexDirection: 'column',
    pointerEvents: 'auto',
    position: 'relative',
  },

  // 响应式: 小屏全宽
  sidebarMobile: {
    width: '100vw',
  },

  // Header 区域: 连接状态 + Agent 名 + 关闭按钮
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '12px 16px',
    borderBottom: '1px solid var(--color-border-secondary, #f0f0f0)',
    flexShrink: 0,
  },

  // Agent 选择区: 紧凑横向 tabs
  agentSelector: {
    padding: '8px 12px',
    borderBottom: '1px solid var(--color-border-secondary, #f0f0f0)',
    flexShrink: 0,
  },

  // 会话面板: 可折叠区域
  conversationPanel: {
    flexShrink: 0,
    borderBottom: '1px solid var(--color-border-secondary, #f0f0f0)',
  },

  // 消息区域: flex:1 填充剩余空间
  messageArea: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    minHeight: 0, // flex 子项收缩兼容
    backgroundColor: 'var(--color-bg-layout, #f5f5f5)',
  },

  // 消息列表容器: 可滚动
  messageList: {
    flex: 1,
    overflow: 'auto',
    padding: 16,
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },

  // Sender 输入区
  senderArea: {
    padding: '12px 16px',
    borderTop: '1px solid var(--color-border-secondary, #f0f0f0)',
    flexShrink: 0,
    backgroundColor: 'var(--color-bg-elevated, #fff)',
  },

  // 折叠浮出按钮: 56px 圆形
  floatingButton: {
    width: 56,
    height: 56,
    borderRadius: '50%',
    backgroundColor: '#1677ff',
    color: '#fff',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
    boxShadow: '0 4px 14px rgba(22, 119, 255, 0.4)',
    border: 'none',
    outline: 'none',
    transition: 'transform 0.2s, box-shadow 0.2s',
    position: 'fixed',
    bottom: 24,
    right: 24,
    zIndex: 999,
  },

  // 浮出按钮 hover 状态 (用 transform scale)
  floatingButtonHover: {
    transform: 'scale(1.08)',
    boxShadow: '0 6px 20px rgba(22, 119, 255, 0.5)',
  },

  // 用户消息气泡
  userBubble: {
    alignSelf: 'flex-end',
    maxWidth: '80%',
  },

  // AI 消息气泡
  aiBubble: {
    alignSelf: 'flex-start',
    maxWidth: '100%',
  },

  // "思考中" 气泡
  thinkingBubble: {
    alignSelf: 'flex-start',
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '8px 16px',
    backgroundColor: 'var(--color-bg-container, #fff)',
    borderRadius: '12px 12px 12px 0',
    boxShadow: '0 1px 4px rgba(0,0,0,0.06)',
  },

  // 自动滚动锚点
  scrollAnchor: {
    height: 1,
    flexShrink: 0,
  },

  // 空状态容器
  emptyState: {
    flex: 1,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },

  // "加载更多消息" 按钮容器
  loadMoreTrigger: {
    display: 'flex',
    justifyContent: 'center',
    padding: '4px 0',
  },

  // 时间戳 — AI 消息左对齐
  timestampLeft: {
    fontSize: 11,
    color: 'var(--color-text-tertiary, #999)',
    marginTop: 4,
    paddingLeft: 4,
  },

  // 时间戳 — 用户消息右对齐
  timestampRight: {
    fontSize: 11,
    color: 'var(--color-text-tertiary, #999)',
    marginTop: 4,
    paddingRight: 4,
    textAlign: 'right' as const,
  },

  // HITL dialog override z-index (需高于侧边栏)
  hitlDialog: {
    zIndex: 1100,
  },
};
