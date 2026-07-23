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

  // Get current page - query the user's current route
  buildFunctionEntry(
    {
      name: 'get_current_page',
      description: '获取用户当前所在的页面信息，包括路径、路由名称、页面标题、URL参数和查询参数。当需要了解用户当前在哪个页面时调用此函数。',
      parameters: {
        type: 'object',
        properties: {},
      },
      tags: ['type:query', 'page:general'],
    },
    async () => {
      const { getRouter } = await import('../plugin')
      const router = getRouter()

      // Determine current route path
      const currentPath = router
        ? router.currentRoute.value.path
        : window.location.pathname

      // Get registered functions from FunctionRegistry, filtered by relevance:
      //   - general functions (tags include 'page:general')
      //   - page-specific functions (tags include 'route:<currentPath>')
      let functions: Array<{ name: string; description: string }> = []
      try {
        const registry = (window as any).$xyncra?.registry
        if (registry && typeof registry.getFunctionInfos === 'function') {
          const infos = registry.getFunctionInfos()
          const routeTag = `route:${currentPath}`
          functions = infos
            .filter((info: { tags?: string[] }) => {
              const tags = info.tags ?? []
              return tags.includes('page:general') || tags.includes(routeTag)
            })
            .map((info: { name: string; description?: string }) => ({
              name: info.name,
              description: info.description ?? '',
            }))
        }
      } catch {
        // Registry not available, return empty array
      }

      if (router) {
        const route = router.currentRoute.value
        return {
          success: true,
          path: route.path,
          name: route.name ?? null,
          title: route.meta?.title ?? null,
          fullPath: route.fullPath,
          params: route.params,
          query: route.query,
          functions,
        }
      }
      // Fallback when router is not available
      return {
        success: true,
        path: window.location.pathname,
        name: null,
        title: document.title || null,
        fullPath: window.location.pathname + window.location.search,
        params: {},
        query: Object.fromEntries(new URLSearchParams(window.location.search)),
        functions,
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
