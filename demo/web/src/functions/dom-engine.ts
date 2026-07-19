export interface PageElement {
  type: string
  label: string
  selector: string
  disabled?: boolean
  value?: string
  checked?: boolean
}

export interface Region {
  id: string
  type: string
  label: string
  elements: PageElement[]
  columns?: string[]
  row_count?: number
  total?: number
}

export interface PageStructure {
  title: string
  url: string
  regions: Region[]
}

export function isHidden(el: Element): boolean {
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

const MEANINGFUL_CLASS_PREFIXES = ['ant-', 'xyncra-']

function getMeaningfulClasses(el: Element): string[] {
  return Array.from(el.classList).filter((c) =>
    MEANINGFUL_CLASS_PREFIXES.some((p) => c.startsWith(p)) ||
    /^[A-Z][a-zA-Z0-9-]*$/.test(c) ||
    c === 'active' || c === 'selected' || c === 'disabled'
  )
}

function getNthOfTypeIndex(el: Element): number {
  const parent = el.parentElement
  if (!parent) return 1
  const tag = el.tagName.toLowerCase()
  const siblings = parent.querySelectorAll(`:scope > ${tag}`)
  let index = 1
  for (const sibling of siblings) {
    if (sibling === el) break
    index++
  }
  return index
}

function buildTagClassSelector(el: Element, includeNth: boolean): string {
  const tag = el.tagName.toLowerCase()
  const classes = getMeaningfulClasses(el)
  let sel = tag
  if (classes.length > 0) {
    sel += classes.map((c) => `.${CSS.escape(c)}`).join('')
  }
  if (includeNth) {
    sel += `:nth-of-type(${getNthOfTypeIndex(el)})`
  }
  return sel
}

function buildPathSelector(el: Element): string {
  const parts: string[] = []
  let current: Element | null = el
  while (current && current !== document.body && current !== document.documentElement) {
    const sel = buildTagClassSelector(current, true)
    parts.unshift(sel)
    current = current.parentElement
  }
  return parts.join(' > ')
}

function buildUniqueClassSelector(el: Element): string {
  const simple = buildTagClassSelector(el, false)
  const candidates: string[] = []

  if (el.classList.length > 0) {
    const classStr = getMeaningfulClasses(el).map((c) => `.${CSS.escape(c)}`).join('')
    if (classStr) {
      const tag = el.tagName.toLowerCase()
      candidates.push(`${tag}${classStr}`)
    }
  }

  candidates.push(simple, buildTagClassSelector(el, true))

  for (const sel of candidates) {
    try {
      const matches = document.querySelectorAll(sel)
      if (matches.length === 1 && matches[0] === el) return sel
    } catch {
      continue
    }
  }

  const pathSel = buildPathSelector(el)
  try {
    const matches = document.querySelectorAll(pathSel)
    if (matches.length >= 1) return pathSel
  } catch {
    // fallback below
  }

  return buildPathSelector(el)
}

export function generateSelector(el: Element): string {
  if (el.id) {
    const sel = `#${CSS.escape(el.id)}`
    try {
      if (document.querySelectorAll(sel).length === 1) return sel
    } catch {
      // fall through
    }
  }

  const testId = el.getAttribute('data-cy') || el.getAttribute('data-testid')
  if (testId) {
    const attr = el.hasAttribute('data-cy') ? 'data-cy' : 'data-testid'
    const sel = `[${attr}="${CSS.escape(testId)}"]`
    try {
      if (document.querySelectorAll(sel).length === 1) return sel
    } catch {
      // fall through
    }
  }

  const name = el.getAttribute('name')
  if (name) {
    const sel = `[name="${CSS.escape(name)}"]`
    try {
      if (document.querySelectorAll(sel).length === 1) return sel
    } catch {
      // fall through
    }
  }

  const ariaLabel = el.getAttribute('aria-label')
  if (ariaLabel) {
    const sel = `[aria-label="${CSS.escape(ariaLabel)}"]`
    try {
      if (document.querySelectorAll(sel).length === 1) return sel
    } catch {
      // fall through
    }
  }

  const classSel = buildUniqueClassSelector(el)
  try {
    const matches = document.querySelectorAll(classSel)
    if (matches.length > 1) {
      console.warn(`[dom-engine] Selector not unique: "${classSel}" (${matches.length} matches)`)
    }
  } catch {
    // fallback below
  }

  return classSel
}

function getElementType(el: Element): string {
  const tag = el.tagName.toLowerCase()
  const role = el.getAttribute('role')
  const cls = Array.from(el.classList)

  if (cls.includes('ant-btn') || tag === 'button') return 'button'
  if (tag === 'a' && el.getAttribute('href')) return 'link'
  if (tag === 'input') {
    const t = (el as HTMLInputElement).type
    if (t === 'checkbox' || cls.includes('ant-checkbox-input')) return 'checkbox'
    if (t === 'radio' || cls.includes('ant-radio-input')) return 'radio'
    return 'input'
  }
  if (tag === 'select' || cls.includes('ant-select')) return 'select'
  if (tag === 'textarea') return 'textarea'
  if (role === 'button' || role === 'tab' || role === 'switch' || role === 'menuitem' || role === 'option') return role || tag
  if (cls.includes('ant-switch')) return 'switch'
  if (cls.includes('ant-picker')) return 'datepicker'
  if (cls.includes('ant-menu-item')) return 'menu-item'
  if (cls.includes('ant-tabs-tab')) return 'tab'
  if (cls.includes('ant-upload')) return 'upload'
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

  const alt = el.getAttribute('alt')
  if (alt) return alt

  if (el.tagName === 'INPUT' && el.id) {
    const labelEl = document.querySelector(`label[for="${CSS.escape(el.id)}"]`)
    if (labelEl?.textContent?.trim()) return labelEl.textContent.trim()
  }

  return ''
}

const INTERACTIVE_SELECTORS = [
  'button', 'a[href]', 'input', 'select', 'textarea',
  '[role="button"]', '[role="tab"]', '[role="switch"]', '[role="menuitem"]', '[role="option"]',
  '.ant-btn', '.ant-checkbox', '.ant-radio', '.ant-switch',
  '.ant-picker', '.ant-menu-item', '.ant-tabs-tab',
  '.ant-select', '.ant-upload', '.ant-modal',
]

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
      disabled: el.hasAttribute('disabled') || el.classList.contains('ant-btn-disabled') || undefined,
      value: inputEl.value !== undefined ? inputEl.value : undefined,
      checked: inputEl.checked !== undefined ? inputEl.checked : undefined,
    })
  }

  return results
}

function extractTableMeta(container: Element): { columns?: string[]; row_count?: number; total?: number } {
  const tableWrappers = container.querySelectorAll('.ant-table-wrapper, .ant-table')
  if (tableWrappers.length === 0) return {}

  const tableWrapper = tableWrappers[0]
  const result: { columns?: string[]; row_count?: number; total?: number } = {}

  const headerCells = tableWrapper.querySelectorAll('.ant-table-thead th, .ant-table-cell.ant-table-column-has-filters')
  if (headerCells.length > 0) {
    result.columns = Array.from(headerCells)
      .map((th) => th.textContent?.trim())
      .filter(Boolean) as string[]
  }

  const rows = tableWrapper.querySelectorAll('.ant-table-tbody tr.ant-table-row, .ant-table-tbody > tr')
  result.row_count = rows.length

  const pagination = tableWrapper.querySelector('.ant-pagination')
  if (pagination) {
    const totalEl = pagination.querySelector('.ant-pagination-total-text')
    if (totalEl) {
      const match = totalEl.textContent?.match(/\d+/g)
      if (match) result.total = parseInt(match[match.length - 1], 10)
    }
  }

  return result
}

const REGION_DETECTORS: Array<{
  test: (el: Element) => boolean
  type: Region['type']
  label: (el: Element) => string
}> = [
  {
    test: (el) =>
      el.querySelector('.ant-input-search') !== null ||
      (el.matches('.ant-form') &&
        el.querySelector('input') !== null &&
        el.querySelector('button[type="submit"]') !== null),
    type: 'filter-bar',
    label: (el) =>
      el.querySelector('input')?.getAttribute('placeholder') || 'Filter',
  },
  {
    test: (el) => el.querySelector('.ant-table-wrapper, .ant-table') !== null,
    type: 'table',
    label: (el) =>
      el.querySelector('.ant-table-title')?.textContent?.trim() ||
      el.querySelector('h1, h2, h3, h4')?.textContent?.trim() ||
      'Table',
  },
  {
    test: (el) => el.matches('form, .ant-form'),
    type: 'form',
    label: (el) =>
      (el as HTMLElement).getAttribute('aria-label') ||
      el.querySelector('legend')?.textContent?.trim() ||
      'Form',
  },
  {
    test: (el) => el.querySelectorAll(':scope > .ant-card').length >= 2,
    type: 'card-list',
    label: (el) =>
      el.querySelector('h1, h2, h3, h4')?.textContent?.trim() || 'Cards',
  },
]

function detectRegion(el: Element): { type: Region['type']; label: string } | null {
  for (const detector of REGION_DETECTORS) {
    if (detector.test(el)) {
      return { type: detector.type, label: detector.label(el) }
    }
  }
  return null
}

interface RegionCandidate {
  element: Element
  type: Region['type']
  label: string
}

function collectRegionCandidates(root: Element): RegionCandidate[] {
  const candidates: RegionCandidate[] = []

  const modals = root.querySelectorAll('.ant-modal-confirm, .ant-modal-content')
  for (const modal of modals) {
    if (!isHidden(modal)) {
      candidates.push({
        element: modal,
        type: 'modal',
        label: modal.querySelector('.ant-modal-title, .ant-modal-confirm-title')?.textContent?.trim() || 'Modal',
      })
    }
  }

  const drawers = root.querySelectorAll('.ant-drawer-content-wrapper')
  for (const drawer of drawers) {
    if (!isHidden(drawer)) {
      candidates.push({
        element: drawer,
        type: 'drawer',
        label: drawer.querySelector('.ant-drawer-title')?.textContent?.trim() || 'Drawer',
      })
    }
  }

  const mainContent =
    root.querySelector('.ant-layout-content, main, [role="main"]') || root

  for (const child of Array.from(mainContent.children)) {
    if (isHidden(child)) continue
    const detected = detectRegion(child)
    if (detected) {
      candidates.push({ element: child, ...detected })
    }
  }

  const hasCandidates = candidates.length > 0

  if (!hasCandidates) {
    candidates.push({
      element: root,
      type: 'unknown',
      label: document.title,
    })
  }

  return candidates
}

function buildRegion(candidate: RegionCandidate, id: string): Region {
  const elements = queryInteractiveElements(candidate.element)

  const region: Region = {
    id,
    type: candidate.type,
    label: candidate.label,
    elements,
  }

  if (candidate.type === 'table') {
    Object.assign(region, extractTableMeta(candidate.element))
  }

  return region
}

export function extractInteractiveElements(regionType?: string): PageStructure {
  const regionCandidates = collectRegionCandidates(document.body)
  const regions: Region[] = []

  for (let i = 0; i < regionCandidates.length; i++) {
    const region = buildRegion(regionCandidates[i], `region-${i + 1}`)
    if (!regionType || region.type === regionType) {
      regions.push(region)
    }
  }

  return {
    title: document.title,
    url: window.location.href,
    regions,
  }
}

export function waitForSelector(
  selector: string,
  timeoutMs: number,
  visible: boolean = true,
): Promise<Element | null> {
  const deadline = Date.now() + timeoutMs

  return new Promise((resolve) => {
    let lastCheck = 0

    function poll(timestamp: number) {
      if (timestamp - lastCheck < 50) {
        requestAnimationFrame(poll)
        return
      }
      lastCheck = timestamp

      let el: Element | null = null
      try {
        el = document.querySelector(selector)
      } catch {
        resolve(null); return
      }

      if (el && (!visible || !isHidden(el))) {
        resolve(el)
        return
      }

      if (Date.now() >= deadline) {
        resolve(null)
        return
      }

      requestAnimationFrame(poll)
    }

    requestAnimationFrame(poll)
  })
}

export function waitForLoadingComplete(
  container?: Element,
  timeoutMs: number = 10000,
): Promise<void> {
  const root = container || document.body
  const deadline = Date.now() + timeoutMs

  return new Promise((resolve) => {
    function poll() {
      const spinning = root.querySelector('.ant-spin-spinning, .ant-spin-blur')
      if (!spinning) {
        resolve()
        return
      }
      if (Date.now() >= deadline) {
        resolve()
        return
      }
      requestAnimationFrame(poll)
    }

    requestAnimationFrame(poll)
  })
}
