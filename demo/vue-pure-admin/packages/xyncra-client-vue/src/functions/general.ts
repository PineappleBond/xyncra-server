import { buildFunctionEntry } from '../factory'
import { openAskUserDialog } from '../composables/useAskUserState'

export const generalFunctions = [
  // Navigation
  buildFunctionEntry(
    {
      name: 'navigate_to',
      description: '导航到指定页面',
      parameters: {
        type: 'object',
        properties: {
          path: { type: 'string', description: '目标路径' },
        },
        required: ['path'],
      },
      tags: ['type:navigate', 'page:general'],
    },
    async (params) => {
      const { path } = params as { path: string }
      // Use plugin injection to get router (avoids window global)
      const { getRouter } = await import('../plugin')
      const router = getRouter()
      if (router) {
        await router.push(path)
      } else {
        window.location.href = path
      }
      return { success: true }
    },
  ),

  // Show notification - uses Element Plus ElNotification
  buildFunctionEntry(
    {
      name: 'show_notification',
      description: '显示通知消息',
      parameters: {
        type: 'object',
        properties: {
          message: { type: 'string', description: '通知内容' },
          type: { type: 'string', description: '通知类型（success/error/warning/info）' },
          duration: { type: 'number', description: '显示时长（毫秒），默认 3000' },
        },
        required: ['message'],
      },
      tags: ['type:action', 'page:general'],
      timeout_ms: 5000,
    },
    async (params) => {
      const { message, type = 'info', duration = 3000 } = params as {
        message: string
        type?: string
        duration?: number
      }
      // Dynamic import to avoid hard dependency on element-plus at package level
      const { ElNotification } = await import('element-plus')
      ElNotification({
        message,
        type: type as 'success' | 'warning' | 'info' | 'error',
        duration,
      })
      return { success: true }
    },
  ),

  // Confirm action - uses Element Plus ElMessageBox.confirm
  buildFunctionEntry(
    {
      name: 'confirm_action',
      description: '显示确认弹窗并等待用户操作',
      parameters: {
        type: 'object',
        properties: {
          message: { type: 'string', description: '确认消息内容' },
          title: { type: 'string', description: '弹窗标题（可选）' },
        },
        required: ['message'],
      },
      tags: ['type:action', 'page:general'],
      timeout_ms: 30000,
    },
    async (params) => {
      const { message, title } = params as { message: string; title?: string }
      // Dynamic import to avoid hard dependency on element-plus at package level
      const { ElMessageBox } = await import('element-plus')
      try {
        await ElMessageBox.confirm(message, title ?? '确认')
        return { success: true, confirmed: true }
      } catch {
        // User cancelled - this is normal flow, not an error
        return { success: true, confirmed: false }
      }
    },
  ),

  // Ask user - HITL (Human-In-The-Loop)
  buildFunctionEntry(
    {
      name: 'ask_user',
      description: '向用户提问并等待回答',
      parameters: {
        type: 'object',
        properties: {
          question: { type: 'string', description: '要向用户提出的问题' },
        },
        required: ['question'],
      },
      tags: ['type:hitl', 'page:general'],
    },
    async (params) => {
      const { question, __callingId } = params as { question: string; __callingId?: string }
      const answer = await openAskUserDialog(question, __callingId ?? '')
      return { success: true, answer }
    },
  ),
]
