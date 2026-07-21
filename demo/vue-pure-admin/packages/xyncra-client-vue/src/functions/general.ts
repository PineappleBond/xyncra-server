import { buildFunctionEntry } from '../factory'
import { matchPageDescription } from './page-descriptions'

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
      const router = window.__vue_router
      if (router) {
        await router.push(path)
      } else {
        window.location.href = path
      }
      return { success: true }
    },
  ),

  // Page info
  buildFunctionEntry(
    {
      name: 'get_current_page',
      description: '获取当前页面信息（URL、标题、pathname）',
      parameters: { type: 'object', properties: {} },
      tags: ['type:info', 'page:general'],
    },
    async () => {
      const router = window.__vue_router
      if (router && router.currentRoute) {
        const route = router.currentRoute.value
        return {
          success: true,
          path: route.path,
          name: route.name,
          title: route.meta?.title ?? '',
          fullPath: route.fullPath,
        }
      }
      return {
        success: true,
        path: window.location.pathname,
        title: document.title,
      }
    },
  ),

  // Page description - critical for agent to understand page context
  buildFunctionEntry(
    {
      name: 'get_page_description',
      description: '获取当前页面的业务语义描述：页面是什么、有哪些业务区块、每个区块关联哪些 pg_ 函数。调用具体 pg_ 函数前先调此函数建立页面认知',
      parameters: { type: 'object', properties: {} },
      tags: ['type:info', 'page:general'],
      timeout_ms: 10000,
    },
    async () => {
      // In hash-based routers, the route is in the hash (e.g. #/account/settings)
      const hash = window.location.hash || ''
      const pathname = hash.startsWith('#') ? hash.slice(1) : window.location.pathname
      const desc = matchPageDescription(pathname)
      if (!desc) {
        return {
          success: true,
          data: {
            page_id: 'unknown',
            route: pathname,
            title: document.title,
            summary: '未知页面，请使用通用 DOM 函数操作',
            business_goal: '',
            regions: [],
          },
        }
      }
      return { success: true, data: desc }
    },
  ),

  // Page structure - returns interactive elements on the page
  buildFunctionEntry(
    {
      name: 'get_page_structure',
      description: '获取当前页面所有可交互元素的结构化摘要（selector/label/type）',
      parameters: {
        type: 'object',
        properties: {
          region_type: {
            type: 'string',
            description: '可选过滤：只返回指定类型的区域（form/table/card-list/filter-bar）',
          },
        },
      },
      tags: ['type:info', 'page:general'],
      timeout_ms: 10000,
    },
    async (params) => {
      const regionType = (params as { region_type?: string })?.region_type
      const elements = extractInteractiveElements(regionType)
      return { success: true, data: elements }
    },
  ),

  // Form data
  buildFunctionEntry(
    {
      name: 'get_form_data',
      description: '获取页面表单的字段值和校验状态',
      parameters: { type: 'object', properties: {} },
      tags: ['type:info', 'page:general'],
      timeout_ms: 10000,
    },
    async () => {
      const forms = document.querySelectorAll('form, .el-form')
      const results: Array<Record<string, unknown>> = []

      forms.forEach((form) => {
        const fields: Record<string, unknown> = {}
        const inputs = form.querySelectorAll('input, textarea, select, .el-input, .el-select, .el-textarea')
        inputs.forEach((input) => {
          const name = input.getAttribute('name') || input.id || ''
          const value = (input as HTMLInputElement).value || ''
          if (name) {
            fields[name] = value
          }
        })
        if (Object.keys(fields).length > 0) {
          results.push(fields)
        }
      })

      return { success: true, data: results }
    },
  ),

  // Table data
  buildFunctionEntry(
    {
      name: 'get_table_data',
      description: '获取页面表格的行数据和分页信息',
      parameters: { type: 'object', properties: {} },
      tags: ['type:info', 'page:general'],
      timeout_ms: 10000,
    },
    async () => {
      const tables = document.querySelectorAll('.el-table, table')
      const results: Array<Record<string, unknown>> = []

      tables.forEach((table) => {
        const headers: string[] = []
        const headerCells = table.querySelectorAll('thead th, .el-table__header th')
        headerCells.forEach((th) => {
          const text = th.textContent?.trim() || ''
          if (text) headers.push(text)
        })

        const rows: string[][] = []
        const bodyRows = table.querySelectorAll('tbody tr, .el-table__body tr')
        bodyRows.forEach((tr) => {
          const cells: string[] = []
          tr.querySelectorAll('td, .el-table__cell').forEach((td) => {
            cells.push(td.textContent?.trim() || '')
          })
          rows.push(cells)
        })

        // Get pagination info
        const pagination = table.closest('.el-card, div')?.querySelector('.el-pagination')
        let total: number | undefined
        if (pagination) {
          const totalText = pagination.querySelector('.el-pagination__total')
          if (totalText) {
            const match = totalText.textContent?.match(/\d+/)
            if (match) total = parseInt(match[0], 10)
          }
        }

        if (headers.length > 0 || rows.length > 0) {
          results.push({ headers, rows, row_count: rows.length, total })
        }
      })

      return { success: true, data: results }
    },
  ),

  // Click element
  buildFunctionEntry(
    {
      name: 'click_element',
      description: '点击指定 CSS 选择器的元素',
      parameters: {
        type: 'object',
        properties: {
          selector: { type: 'string', description: 'CSS 选择器' },
        },
        required: ['selector'],
      },
      tags: ['type:action', 'page:general'],
      timeout_ms: 15000,
    },
    async (params) => {
      const { selector } = params as { selector: string }
      const el = document.querySelector(selector)
      if (!el) {
        return { success: false, error: `元素未找到: ${selector}` }
      }
      ;(el as HTMLElement).click()
      return { success: true }
    },
  ),

  // Type text
  buildFunctionEntry(
    {
      name: 'type_text',
      description: '在输入框中填值（先清空后输入）',
      parameters: {
        type: 'object',
        properties: {
          selector: { type: 'string', description: '输入框 CSS 选择器' },
          value: { type: 'string', description: '要填入的文本内容' },
        },
        required: ['selector', 'value'],
      },
      tags: ['type:action', 'page:general'],
      timeout_ms: 10000,
    },
    async (params) => {
      const { selector, value } = params as { selector: string; value: string }
      const el = document.querySelector(selector) as HTMLInputElement | null
      if (!el) {
        return { success: false, error: `输入框未找到: ${selector}` }
      }

      // Set value using native input setter to trigger Vue reactivity
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype, 'value'
      )?.set
      if (nativeInputValueSetter) {
        nativeInputValueSetter.call(el, value)
      } else {
        el.value = value
      }
      el.dispatchEvent(new Event('input', { bubbles: true }))
      el.dispatchEvent(new Event('change', { bubbles: true }))

      return { success: true }
    },
  ),

  // Show notification
  buildFunctionEntry(
    {
      name: 'show_notification',
      description: '显示通知消息',
      parameters: {
        type: 'object',
        properties: {
          message: { type: 'string', description: '通知内容' },
          type: { type: 'string', description: '通知类型（success/error/warning/info）' },
        },
        required: ['message'],
      },
      tags: ['type:action', 'page:general'],
      timeout_ms: 5000,
    },
    async (params) => {
      const { message, type = 'info' } = params as { message: string; type?: string }
      // Use Element Plus notification if available
      const el = document.createElement('div')
      el.className = `el-notification el-notification--${type}`
      el.style.cssText = 'position:fixed;top:20px;right:20px;z-index:99999;padding:16px;background:#fff;box-shadow:0 2px 12px rgba(0,0,0,.1);border-radius:4px;min-width:300px;'
      el.textContent = message
      document.body.appendChild(el)
      setTimeout(() => el.remove(), 3000)
      return { success: true }
    },
  ),

  // Highlight element
  buildFunctionEntry(
    {
      name: 'highlight_element',
      description: '高亮指定元素',
      parameters: {
        type: 'object',
        properties: {
          selector: { type: 'string', description: 'CSS 选择器' },
        },
        required: ['selector'],
      },
      tags: ['type:action', 'page:general'],
      timeout_ms: 5000,
    },
    async (params) => {
      const { selector } = params as { selector: string }
      const el = document.querySelector(selector) as HTMLElement | null
      if (!el) {
        return { success: false, error: `元素未找到: ${selector}` }
      }
      const originalOutline = el.style.outline
      const originalTransition = el.style.transition
      el.style.transition = 'outline 0.3s'
      el.style.outline = '3px solid #409eff'
      setTimeout(() => {
        el.style.outline = originalOutline
        el.style.transition = originalTransition
      }, 3000)
      return { success: true }
    },
  ),

  // Scroll to
  buildFunctionEntry(
    {
      name: 'scroll_to',
      description: '滚动到指定位置',
      parameters: {
        type: 'object',
        properties: {
          selector: { type: 'string', description: '目标元素 CSS 选择器' },
        },
        required: ['selector'],
      },
      tags: ['type:action', 'page:general'],
      timeout_ms: 5000,
    },
    async (params) => {
      const { selector } = params as { selector: string }
      const el = document.querySelector(selector)
      if (!el) {
        return { success: false, error: `元素未找到: ${selector}` }
      }
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      return { success: true }
    },
  ),

  // Wait for element
  buildFunctionEntry(
    {
      name: 'wait_for_element',
      description: '等待元素出现（处理 loading 状态）',
      parameters: {
        type: 'object',
        properties: {
          selector: { type: 'string', description: 'CSS 选择器' },
          timeout: { type: 'number', description: '超时时间（毫秒）' },
        },
        required: ['selector'],
      },
      tags: ['type:action', 'page:general'],
      timeout_ms: 15000,
    },
    async (params) => {
      const { selector, timeout = 10000 } = params as { selector: string; timeout?: number }
      const deadline = Date.now() + timeout

      return new Promise((resolve) => {
        function check() {
          const el = document.querySelector(selector)
          if (el) {
            resolve({ success: true })
            return
          }
          if (Date.now() >= deadline) {
            resolve({ success: false, error: `等待超时: ${selector}` })
            return
          }
          requestAnimationFrame(check)
        }
        check()
      })
    },
  ),

  // Confirm action
  buildFunctionEntry(
    {
      name: 'confirm_action',
      description: '操作确认弹窗（confirm/cancel）',
      parameters: {
        type: 'object',
        properties: {
          action: { type: 'string', description: '操作类型（confirm/cancel）' },
        },
        required: ['action'],
      },
      tags: ['type:action', 'page:general'],
      timeout_ms: 5000,
    },
    async (params) => {
      const { action } = params as { action: string }
      const confirmBtn = document.querySelector('.el-message-box__btns .el-button--primary, .el-dialog__footer .el-button--primary')
      const cancelBtn = document.querySelector('.el-message-box__btns .el-button:not(.el-button--primary), .el-dialog__footer .el-button:not(.el-button--primary)')

      if (action === 'confirm' && confirmBtn) {
        (confirmBtn as HTMLElement).click()
        return { success: true }
      }
      if (action === 'cancel' && cancelBtn) {
        (cancelBtn as HTMLElement).click()
        return { success: true }
      }
      return { success: false, error: `未找到确认弹窗或操作按钮` }
    },
  ),
]

// DOM engine for extracting interactive elements
interface PageElement {
  type: string
  label: string
  selector: string
  disabled?: boolean
  value?: string
}

interface Region {
  id: string
  type: string
  label: string
  elements: PageElement[]
}

interface PageStructure {
  title: string
  url: string
  regions: Region[]
}

function isHidden(el: Element): boolean {
  if (el.hasAttribute('hidden')) return true
  if (el.tagName === 'INPUT' && (el as HTMLInputElement).type === 'hidden') return true
  const htmlEl = el as HTMLElement
  const style = getComputedStyle(htmlEl)
  if (style.display === 'none') return true
  if (style.visibility === 'hidden') return true
  if (htmlEl.offsetParent === null) {
    if (style.position === 'fixed' || style.position === 'sticky') {
      return style.display === 'none' || style.visibility === 'hidden'
    }
    return true
  }
  return false
}

function generateSelector(el: Element): string {
  if (el.id) {
    return `#${CSS.escape(el.id)}`
  }

  const testId = el.getAttribute('data-cy') || el.getAttribute('data-testid')
  if (testId) {
    const attr = el.hasAttribute('data-cy') ? 'data-cy' : 'data-testid'
    return `[${attr}="${CSS.escape(testId)}"]`
  }

  const name = el.getAttribute('name')
  if (name) {
    return `[name="${CSS.escape(name)}"]`
  }

  // Build path selector
  const parts: string[] = []
  let current: Element | null = el
  while (current && current !== document.body && current !== document.documentElement) {
    const tag = current.tagName.toLowerCase()
    const classes = Array.from(current.classList)
      .filter(c => c.startsWith('el-') || /^[A-Z]/.test(c))
      .slice(0, 2)
      .map(c => `.${CSS.escape(c)}`)
      .join('')
    parts.unshift(`${tag}${classes}`)
    current = current.parentElement
  }
  return parts.join(' > ')
}

function getElementType(el: Element): string {
  const tag = el.tagName.toLowerCase()
  const role = el.getAttribute('role')
  const cls = Array.from(el.classList)

  if (tag === 'button' || cls.some(c => c.includes('btn'))) return 'button'
  if (tag === 'a' && el.getAttribute('href')) return 'link'
  if (tag === 'input') {
    const t = (el as HTMLInputElement).type
    if (t === 'checkbox') return 'checkbox'
    if (t === 'radio') return 'radio'
    return 'input'
  }
  if (tag === 'select') return 'select'
  if (tag === 'textarea') return 'textarea'
  if (role) return role
  if (cls.some(c => c.includes('switch'))) return 'switch'
  if (cls.some(c => c.includes('picker'))) return 'datepicker'
  if (cls.some(c => c.includes('tabs-tab'))) return 'tab'
  return tag
}

function getElementLabel(el: Element): string {
  const ariaLabel = el.getAttribute('aria-label')
  if (ariaLabel) return ariaLabel

  const text = el.textContent?.trim()
  if (text && text.length < 100) return text

  const inputEl = el as HTMLInputElement
  if (inputEl.placeholder) return inputEl.placeholder

  const title = el.getAttribute('title')
  if (title) return title

  return ''
}

const INTERACTIVE_SELECTORS = [
  'button', 'a[href]', 'input', 'select', 'textarea',
  '[role="button"]', '[role="tab"]', '[role="switch"]', '[role="menuitem"]',
  '.el-button', '.el-checkbox', '.el-radio', '.el-switch',
  '.el-date-editor', '.el-menu-item', '.el-tabs__item',
  '.el-select', '.el-upload',
]

function extractInteractiveElements(regionType?: string): PageStructure {
  const mainContent = document.querySelector('.el-main, main, [role="main"]') || document.body
  const regions: Region[] = []

  // Detect form regions
  const forms = mainContent.querySelectorAll('form, .el-form')
  forms.forEach((form, i) => {
    if (isHidden(form)) return
    if (regionType && regionType !== 'form') return
    const elements = queryInteractiveElements(form)
    if (elements.length > 0) {
      regions.push({
        id: `form-${i + 1}`,
        type: 'form',
        label: form.getAttribute('aria-label') || `表单 ${i + 1}`,
        elements,
      })
    }
  })

  // Detect table regions
  const tables = mainContent.querySelectorAll('.el-table, table')
  tables.forEach((table, i) => {
    if (isHidden(table)) return
    if (regionType && regionType !== 'table') return
    const elements = queryInteractiveElements(table)
    if (elements.length > 0) {
      regions.push({
        id: `table-${i + 1}`,
        type: 'table',
        label: `表格 ${i + 1}`,
        elements,
      })
    }
  })

  // If no specific regions found, create a general region
  if (regions.length === 0) {
    const elements = queryInteractiveElements(mainContent)
    if (elements.length > 0) {
      regions.push({
        id: 'general',
        type: 'general',
        label: document.title,
        elements,
      })
    }
  }

  return {
    title: document.title,
    url: window.location.href,
    regions,
  }
}

function queryInteractiveElements(container: Element): PageElement[] {
  const seen = new Set<Element>()
  const results: PageElement[] = []

  let allElements: NodeListOf<Element>
  try {
    allElements = container.querySelectorAll(INTERACTIVE_SELECTORS.join(','))
  } catch {
    return results
  }

  for (const el of allElements) {
    if (seen.has(el) || isHidden(el)) continue
    seen.add(el)

    const inputEl = el as HTMLInputElement
    results.push({
      type: getElementType(el),
      label: getElementLabel(el),
      selector: generateSelector(el),
      disabled: el.hasAttribute('disabled') || undefined,
      value: inputEl.value !== undefined ? inputEl.value : undefined,
    })
  }

  return results
}
