import {
  isHidden,
  generateSelector,
  waitForSelector,
  waitForLoadingComplete,
  extractInteractiveElements,
} from '../../functions/dom-engine'

if (typeof CSS === 'undefined') {
  ;(globalThis as any).CSS = {}
}
if (!CSS.escape) {
  ;(CSS as any).escape = (v: string) => String(v)
}

const origOffsetParent = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'offsetParent',
)
Object.defineProperty(HTMLElement.prototype, 'offsetParent', {
  get() {
    if (!this.isConnected) return null
    return this.parentElement || null
  },
  configurable: true,
})

afterAll(() => {
  if (origOffsetParent) {
    Object.defineProperty(HTMLElement.prototype, 'offsetParent', origOffsetParent)
  }
})

function syncRAF() {
  jest.spyOn(window, 'requestAnimationFrame').mockImplementation(
    (cb: FrameRequestCallback) => {
      cb(performance.now() + 100)
      return 1
    },
  )
}

describe('isHidden', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('returns false for normal div', () => {
    const div = document.createElement('div')
    document.body.appendChild(div)
    expect(isHidden(div)).toBe(false)
  })

  it('returns true for element with hidden attribute', () => {
    const div = document.createElement('div')
    div.setAttribute('hidden', '')
    document.body.appendChild(div)
    expect(isHidden(div)).toBe(true)
  })

  it('returns true for display:none', () => {
    const div = document.createElement('div')
    div.style.display = 'none'
    document.body.appendChild(div)
    expect(isHidden(div)).toBe(true)
  })

  it('returns true for visibility:hidden', () => {
    const div = document.createElement('div')
    div.style.visibility = 'hidden'
    document.body.appendChild(div)
    expect(isHidden(div)).toBe(true)
  })

  it('returns true for input type="hidden"', () => {
    const input = document.createElement('input')
    input.type = 'hidden'
    document.body.appendChild(input)
    expect(isHidden(input)).toBe(true)
  })
})

describe('generateSelector', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('uses id for element with unique id', () => {
    const div = document.createElement('div')
    div.id = 'my-unique-id'
    document.body.appendChild(div)
    expect(generateSelector(div)).toBe('#my-unique-id')
  })

  it('uses data-cy attribute', () => {
    const div = document.createElement('div')
    div.setAttribute('data-cy', 'test-button')
    document.body.appendChild(div)
    expect(generateSelector(div)).toBe('[data-cy="test-button"]')
  })

  it('uses data-testid attribute', () => {
    const div = document.createElement('div')
    div.setAttribute('data-testid', 'test-el')
    document.body.appendChild(div)
    expect(generateSelector(div)).toBe('[data-testid="test-el"]')
  })

  it('uses name attribute', () => {
    const input = document.createElement('input')
    input.setAttribute('name', 'email')
    document.body.appendChild(input)
    expect(generateSelector(input)).toBe('[name="email"]')
  })

  it('uses aria-label', () => {
    const btn = document.createElement('button')
    btn.setAttribute('aria-label', 'close')
    document.body.appendChild(btn)
    expect(generateSelector(btn)).toBe('[aria-label="close"]')
  })

  it('falls back to tag+class for plain button', () => {
    const btn = document.createElement('button')
    btn.className = 'ant-btn ant-btn-primary'
    document.body.appendChild(btn)
    const sel = generateSelector(btn)
    expect(sel).toContain('button')
    expect(sel).toContain('ant-btn')
  })

  it('does not warn for unique selectors', () => {
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation()
    const btn = document.createElement('button')
    btn.className = 'ant-btn ant-btn-primary'
    document.body.appendChild(btn)
    generateSelector(btn)
    expect(warnSpy).not.toHaveBeenCalled()
    warnSpy.mockRestore()
  })
})

describe('waitForSelector', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  afterEach(() => {
    jest.restoreAllMocks()
  })

  it('resolves element when it already exists', async () => {
    syncRAF()
    document.body.innerHTML = '<div class="my-el">hello</div>'
    const result = await waitForSelector('.my-el', 5000)
    expect(result).not.toBeNull()
    expect(result!.textContent).toBe('hello')
  })

  it('resolves null on timeout', async () => {
    syncRAF()
    const result = await waitForSelector('.never-exists', 0)
    expect(result).toBeNull()
  })

  it('finds element that appears later', async () => {
    let rAF: FrameRequestCallback | null = null
    jest.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rAF = cb
      return 1
    })

    const promise = waitForSelector('.late', 50000)

    rAF!(100)

    const el = document.createElement('div')
    el.className = 'late'
    document.body.appendChild(el)

    rAF!(200)

    const result = await promise
    expect(result).not.toBeNull()
    expect(result!.className).toBe('late')
  })
})

describe('waitForLoadingComplete', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  afterEach(() => {
    jest.restoreAllMocks()
  })

  it('resolves immediately when no spinner', async () => {
    syncRAF()
    await waitForLoadingComplete()
  })

  it('resolves after spinner disappears', async () => {
    let rAF: FrameRequestCallback | null = null
    jest.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rAF = cb
      return 1
    })

    document.body.innerHTML = '<div class="ant-spin-spinning">loading</div>'
    const promise = waitForLoadingComplete()

    rAF!(performance.now() + 100)

    document.querySelector('.ant-spin-spinning')?.remove()

    rAF!(performance.now() + 100)

    await promise
  })

  it('resolves on timeout even if spinner persists', async () => {
    let rAF: FrameRequestCallback | null = null
    jest.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rAF = cb
      return 1
    })

    const dateNowSpy = jest.spyOn(Date, 'now').mockReturnValue(0)

    document.body.innerHTML = '<div class="ant-spin-spinning">loading</div>'
    const promise = waitForLoadingComplete(document.body, 100)

    rAF!(performance.now() + 100)

    dateNowSpy.mockReturnValue(500)

    rAF!(performance.now() + 100)

    await promise

    dateNowSpy.mockRestore()
  })
})

describe('extractInteractiveElements', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    document.title = ''
  })

  it('returns structure with title, url and at least one region for empty body', () => {
    const result = extractInteractiveElements()
    expect(result).toHaveProperty('title')
    expect(result).toHaveProperty('url')
    expect(result.regions.length).toBeGreaterThanOrEqual(1)
    const unknown = result.regions.find((r) => r.type === 'unknown')
    expect(unknown).toBeDefined()
    expect(unknown!.elements).toEqual([])
  })

  it('detects buttons on the page', () => {
    document.body.innerHTML = '<button>Click Me</button>'
    const result = extractInteractiveElements()
    const allElements = result.regions.flatMap((r) => r.elements)
    expect(
      allElements.some((e) => e.type === 'button' && e.label === 'Click Me'),
    ).toBe(true)
  })

  it('detects input fields with label in a form', () => {
    document.body.innerHTML = `
      <div class="ant-form">
        <label for="uname">Username</label>
        <input id="uname" type="text" />
      </div>
    `
    const result = extractInteractiveElements()
    const formRegion = result.regions.find((r) => r.type === 'form')
    expect(formRegion).toBeDefined()
    const inputEl = formRegion!.elements.find((e) => e.type === 'input')
    expect(inputEl).toBeDefined()
    expect(inputEl!.label).toBe('Username')
  })

  it('identifies table region with columns, rows and pagination', () => {
    document.body.innerHTML = `
      <div class="ant-table-wrapper">
        <div class="ant-table">
          <div class="ant-table-title">Users</div>
          <table>
            <thead class="ant-table-thead">
              <tr>
                <th>Name</th>
                <th>Email</th>
              </tr>
            </thead>
            <tbody class="ant-table-tbody">
              <tr class="ant-table-row"><td>Alice</td><td>a@b.com</td></tr>
              <tr class="ant-table-row"><td>Bob</td><td>b@b.com</td></tr>
            </tbody>
          </table>
          <div class="ant-pagination">
            <li class="ant-pagination-total-text">1-2 of 20</li>
          </div>
        </div>
      </div>
    `
    const result = extractInteractiveElements()
    const tableRegion = result.regions.find((r) => r.type === 'table')
    expect(tableRegion).toBeDefined()
    expect(tableRegion!.label).toBe('Users')
    expect(tableRegion!.columns).toEqual(['Name', 'Email'])
    expect(tableRegion!.row_count).toBe(2)
    expect(tableRegion!.total).toBe(20)
  })

  it('detects modal region with content', () => {
    document.body.innerHTML = `
      <div class="ant-modal-content">
        <div class="ant-modal-title">Confirm Delete</div>
        <button class="ant-btn">Yes</button>
        <button class="ant-btn">No</button>
      </div>
    `
    const result = extractInteractiveElements()
    const modalRegion = result.regions.find((r) => r.type === 'modal')
    expect(modalRegion).toBeDefined()
    expect(modalRegion!.label).toBe('Confirm Delete')
    expect(modalRegion!.elements.length).toBe(2)
    expect(modalRegion!.elements.every((e) => e.type === 'button')).toBe(true)
  })

  it('filters by region type', () => {
    document.body.innerHTML = `
      <button>Standalone</button>
      <div class="ant-table-wrapper">
        <div class="ant-table">
          <div class="ant-table-tbody"></div>
        </div>
      </div>
    `
    const tables = extractInteractiveElements('table')
    expect(tables.regions.length).toBe(1)
    expect(tables.regions[0].type).toBe('table')
  })
})
