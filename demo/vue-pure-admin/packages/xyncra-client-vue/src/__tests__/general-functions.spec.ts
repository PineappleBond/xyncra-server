import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock element-plus
const mockElNotification = vi.fn()
const mockElMessageBoxConfirm = vi.fn()

vi.mock('element-plus', () => ({
  ElNotification: mockElNotification,
  ElMessageBox: {
    confirm: mockElMessageBoxConfirm,
  },
}))

// Mock plugin module for navigate_to
const mockGetRouter = vi.fn()
vi.mock('../plugin', () => ({
  getRouter: mockGetRouter,
}))

// Mock useAskUserState for ask_user
vi.mock('../composables/useAskUserState', () => ({
  openAskUserDialog: vi.fn().mockResolvedValue('test-answer'),
}))

import { generalFunctions } from '../functions/general'

describe('generalFunctions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // -------------------------------------------------------
  // Function count assertion
  // -------------------------------------------------------
  it('should contain exactly 4 functions', () => {
    expect(generalFunctions).toHaveLength(4)
  })

  it('should contain only the expected function names', () => {
    const names = generalFunctions.map(fn => fn.info.name)
    expect(names).toEqual(['navigate_to', 'show_notification', 'confirm_action', 'ask_user'])
  })

  // -------------------------------------------------------
  // Negative assertions - deleted functions should not exist
  // -------------------------------------------------------
  it('should not contain deleted DOM functions', () => {
    const names = generalFunctions.map(fn => fn.info.name)
    const deletedFunctions = [
      'get_form_data',
      'get_table_data',
      'click_element',
      'type_text',
      'highlight_element',
      'scroll_to',
      'get_current_page',
      'get_page_description',
      'get_page_structure',
      'wait_for_element'
    ]

    for (const fn of deletedFunctions) {
      expect(names).not.toContain(fn)
    }
  })

  // -------------------------------------------------------
  // navigate_to
  // -------------------------------------------------------
  describe('navigate_to', () => {
    it('should use router.push when router is available', async () => {
      const mockPush = vi.fn()
      mockGetRouter.mockReturnValue({ push: mockPush })

      const navigateTo = generalFunctions.find(fn => fn.info.name === 'navigate_to')!
      await navigateTo.handler({ path: '/test-path' })

      expect(mockGetRouter).toHaveBeenCalled()
      expect(mockPush).toHaveBeenCalledWith('/test-path')
    })

    it('should fallback to window.location.href when router is not available', async () => {
      mockGetRouter.mockReturnValue(undefined)

      const navigateTo = generalFunctions.find(fn => fn.info.name === 'navigate_to')!

      // Spy on window.location.href assignment
      const locationSpy = vi.spyOn(window, 'location', 'get').mockReturnValue({
        ...window.location,
        href: 'http://localhost/'
      } as Location)

      // We can't directly mock href setter in jsdom, so we just verify the handler completes
      await expect(navigateTo.handler({ path: '/fallback-path' })).resolves.toEqual({ success: true })

      locationSpy.mockRestore()
    })

    it('should have correct function info', () => {
      const navigateTo = generalFunctions.find(fn => fn.info.name === 'navigate_to')!

      expect(navigateTo.info.description).toBe('导航到指定页面')
      expect(navigateTo.info.parameters.required).toContain('path')
      expect(navigateTo.info.tags).toContain('type:navigate')
      expect(navigateTo.info.tags).toContain('page:general')
    })
  })

  // -------------------------------------------------------
  // show_notification
  // -------------------------------------------------------
  describe('show_notification', () => {
    it('should call ElNotification with correct parameters', async () => {
      const showNotification = generalFunctions.find(fn => fn.info.name === 'show_notification')!

      await showNotification.handler({
        message: 'Test message',
        type: 'success',
        duration: 5000
      })

      expect(mockElNotification).toHaveBeenCalledWith({
        message: 'Test message',
        type: 'success',
        duration: 5000
      })
    })

    it('should use default type and duration when not provided', async () => {
      const showNotification = generalFunctions.find(fn => fn.info.name === 'show_notification')!

      await showNotification.handler({ message: 'Default params' })

      expect(mockElNotification).toHaveBeenCalledWith({
        message: 'Default params',
        type: 'info',
        duration: 3000
      })
    })

    it('should have correct function info', () => {
      const showNotification = generalFunctions.find(fn => fn.info.name === 'show_notification')!

      expect(showNotification.info.description).toBe('显示通知消息')
      expect(showNotification.info.parameters.required).toContain('message')
      expect(showNotification.info.tags).toContain('type:action')
      expect(showNotification.info.timeout_ms).toBe(5000)
    })
  })

  // -------------------------------------------------------
  // confirm_action
  // -------------------------------------------------------
  describe('confirm_action', () => {
    it('should return confirmed: true when user confirms', async () => {
      mockElMessageBoxConfirm.mockResolvedValue('confirm')

      const confirmAction = generalFunctions.find(fn => fn.info.name === 'confirm_action')!
      const result = await confirmAction.handler({
        message: 'Are you sure?',
        title: 'Confirm'
      })

      expect(result).toEqual({ success: true, confirmed: true })
      expect(mockElMessageBoxConfirm).toHaveBeenCalledWith('Are you sure?', 'Confirm')
    })

    it('should return confirmed: false when user cancels', async () => {
      mockElMessageBoxConfirm.mockRejectedValue(new Error('cancel'))

      const confirmAction = generalFunctions.find(fn => fn.info.name === 'confirm_action')!
      const result = await confirmAction.handler({
        message: 'Are you sure?'
      })

      expect(result).toEqual({ success: true, confirmed: false })
    })

    it('should use default title when not provided', async () => {
      mockElMessageBoxConfirm.mockResolvedValue('confirm')

      const confirmAction = generalFunctions.find(fn => fn.info.name === 'confirm_action')!
      await confirmAction.handler({ message: 'Confirm this' })

      expect(mockElMessageBoxConfirm).toHaveBeenCalledWith('Confirm this', '确认')
    })

    it('should have correct function info', () => {
      const confirmAction = generalFunctions.find(fn => fn.info.name === 'confirm_action')!

      expect(confirmAction.info.description).toBe('显示确认弹窗并等待用户操作')
      expect(confirmAction.info.parameters.required).toContain('message')
      expect(confirmAction.info.tags).toContain('type:action')
      expect(confirmAction.info.timeout_ms).toBe(30000)
    })
  })

  // -------------------------------------------------------
  // ask_user
  // -------------------------------------------------------
  describe('ask_user', () => {
    it('should call openAskUserDialog and return answer', async () => {
      const { openAskUserDialog } = await import('../composables/useAskUserState')

      const askUser = generalFunctions.find(fn => fn.info.name === 'ask_user')!
      const result = await askUser.handler({
        question: 'What is your name?',
        __callingId: 'test-id'
      })

      expect(openAskUserDialog).toHaveBeenCalledWith('What is your name?', 'test-id')
      expect(result).toEqual({ success: true, answer: 'test-answer' })
    })

    it('should handle missing __callingId', async () => {
      const { openAskUserDialog } = await import('../composables/useAskUserState')

      const askUser = generalFunctions.find(fn => fn.info.name === 'ask_user')!
      await askUser.handler({ question: 'Test question' })

      expect(openAskUserDialog).toHaveBeenCalledWith('Test question', '')
    })

    it('should have correct function info', () => {
      const askUser = generalFunctions.find(fn => fn.info.name === 'ask_user')!

      expect(askUser.info.description).toBe('向用户提问并等待回答')
      expect(askUser.info.parameters.required).toContain('question')
      expect(askUser.info.tags).toContain('type:hitl')
      expect(askUser.info.tags).toContain('page:general')
    })
  })
})
