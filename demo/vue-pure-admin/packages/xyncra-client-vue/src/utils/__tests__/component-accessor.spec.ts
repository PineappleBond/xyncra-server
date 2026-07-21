import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock vue's getCurrentInstance
vi.mock('vue', () => ({
  getCurrentInstance: vi.fn(),
}))

import { getCurrentInstance } from 'vue'
import {
  initComponentRegistry,
  registerComponent,
  callComponentMethod,
  getComponentData,
  getComponentEntry,
} from '../component-accessor'

describe('component-accessor', () => {
  beforeEach(() => {
    initComponentRegistry()
    vi.clearAllMocks()
  })

  // -------------------------------------------------------
  // "registry not initialized" tests need a fresh module
  // where componentRegistry is still null.
  // We test them in a separate block using vi.resetModules().
  // -------------------------------------------------------
  describe('registry not initialized', () => {
    it('registerComponent should warn and return early', async () => {
      vi.resetModules()
      vi.doMock('vue', () => ({
        getCurrentInstance: vi.fn(),
      }))
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
      const mod = await import('../component-accessor')

      mod.registerComponent('key')

      expect(warnSpy).toHaveBeenCalledWith(
        '[xyncra] Component registry not initialized. Plugin not installed?',
      )
      warnSpy.mockRestore()
    })

    it('callComponentMethod should warn and return undefined', async () => {
      vi.resetModules()
      vi.doMock('vue', () => ({
        getCurrentInstance: vi.fn(),
      }))
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
      const mod = await import('../component-accessor')

      const result = mod.callComponentMethod('key', 'method')

      expect(result).toBeUndefined()
      expect(warnSpy).toHaveBeenCalledWith(
        '[xyncra] Component registry not initialized',
      )
      warnSpy.mockRestore()
    })

    it('getComponentData should return undefined', async () => {
      vi.resetModules()
      vi.doMock('vue', () => ({
        getCurrentInstance: vi.fn(),
      }))
      const mod = await import('../component-accessor')

      expect(mod.getComponentData('key')).toBeUndefined()
    })

    it('getComponentEntry should return undefined', async () => {
      vi.resetModules()
      vi.doMock('vue', () => ({
        getCurrentInstance: vi.fn(),
      }))
      const mod = await import('../component-accessor')

      expect(mod.getComponentEntry('key')).toBeUndefined()
    })
  })

  // -------------------------------------------------------
  // Rest of the tests use the shared beforeEach setup.
  // -------------------------------------------------------

  describe('initComponentRegistry', () => {
    it('should initialize the registry so getComponentData returns undefined for any key', () => {
      // beforeEach already called initComponentRegistry
      expect(getComponentData('any-key')).toBeUndefined()
    })

    it('should allow re-initialization without error', () => {
      // Register something, then re-init — old data should be gone.
      const mockProxy = { foo: vi.fn() }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('old-key')

      initComponentRegistry()

      expect(getComponentData('old-key')).toBeUndefined()
    })
  })

  describe('registerComponent', () => {
    it('should register component with proxy when getCurrentInstance returns instance', () => {
      const mockProxy = { submit: vi.fn() }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)

      registerComponent('test-key')

      expect(getComponentData('test-key')).toBe(mockProxy)
    })

    it('should register component with helpers', () => {
      const mockProxy = { submit: vi.fn() }
      const mockHelpers = { fill: vi.fn() }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)

      registerComponent('test-key', mockHelpers)

      const entry = getComponentEntry('test-key')
      expect(entry?.proxy).toBe(mockProxy)
      expect(entry?.helpers).toBe(mockHelpers)
    })

    it('should default helpers to empty object when not provided', () => {
      const mockProxy = { submit: vi.fn() }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)

      registerComponent('test-key')

      const entry = getComponentEntry('test-key')
      expect(entry?.helpers).toEqual({})
    })

    it('should do nothing if getCurrentInstance returns null', () => {
      vi.mocked(getCurrentInstance).mockReturnValue(null)

      registerComponent('test-key')

      expect(getComponentData('test-key')).toBeUndefined()
    })

    it('should overwrite a previously registered component with the same key', () => {
      const proxy1 = { id: 1 }
      const proxy2 = { id: 2 }
      vi.mocked(getCurrentInstance)
        .mockReturnValueOnce({ proxy: proxy1 } as any)
        .mockReturnValueOnce({ proxy: proxy2 } as any)

      registerComponent('dup-key')
      registerComponent('dup-key')

      expect(getComponentData('dup-key')).toBe(proxy2)
    })
  })

  describe('callComponentMethod', () => {
    it('should call proxy method with all arguments', () => {
      const mockProxy = { submit: vi.fn().mockReturnValue('proxy-result') }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key')

      const result = callComponentMethod('test-key', 'submit', 'arg1', 'arg2')

      expect(result).toBe('proxy-result')
      expect(mockProxy.submit).toHaveBeenCalledWith('arg1', 'arg2')
    })

    it('should fall back to helpers when proxy method not found', () => {
      const mockProxy = {}
      const mockHelper = vi.fn().mockReturnValue('helper-result')
      const mockHelpers = { fill: mockHelper }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key', mockHelpers)

      const result = callComponentMethod('test-key', 'fill', { value: 'test' })

      expect(result).toBe('helper-result')
      expect(mockHelper).toHaveBeenCalledWith({ value: 'test' })
    })

    it('should prefer proxy over helpers when both exist', () => {
      const mockProxy = { fill: vi.fn().mockReturnValue('proxy-result') }
      const mockHelper = vi.fn().mockReturnValue('helper-result')
      const mockHelpers = { fill: mockHelper }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key', mockHelpers)

      const result = callComponentMethod('test-key', 'fill', 'arg1')

      expect(result).toBe('proxy-result')
      expect(mockProxy.fill).toHaveBeenCalledWith('arg1')
      expect(mockHelper).not.toHaveBeenCalled()
    })

    it('should warn and return undefined for unregistered component', () => {
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})

      const result = callComponentMethod('nonexistent', 'method')

      expect(result).toBeUndefined()
      expect(warnSpy).toHaveBeenCalledWith(
        '[xyncra] Component "nonexistent" not registered. Developer did not expose this component.',
      )
      warnSpy.mockRestore()
    })

    it('should warn and return undefined for non-existent method', () => {
      const mockProxy = {}
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key')
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})

      const result = callComponentMethod('test-key', 'nonexistent')

      expect(result).toBeUndefined()
      expect(warnSpy).toHaveBeenCalledWith(
        '[xyncra] Method "nonexistent" not exposed by component "test-key"',
      )
      warnSpy.mockRestore()
    })

    it('should pass only args[0] to helpers (not the full args array)', () => {
      const mockProxy = {}
      const mockHelper = vi.fn().mockReturnValue('ok')
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key', { doStuff: mockHelper })

      callComponentMethod('test-key', 'doStuff', 'first', 'second')

      // helpers receive only args[0]
      expect(mockHelper).toHaveBeenCalledTimes(1)
      expect(mockHelper).toHaveBeenCalledWith('first')
    })

    it('should return undefined when proxy method returns undefined', () => {
      const mockProxy = { noop: vi.fn().mockReturnValue(undefined) }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key')

      const result = callComponentMethod('test-key', 'noop')

      expect(result).toBeUndefined()
      expect(mockProxy.noop).toHaveBeenCalled()
    })

    it('should handle calling a method with no extra args', () => {
      const mockProxy = { reset: vi.fn().mockReturnValue(true) }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key')

      const result = callComponentMethod('test-key', 'reset')

      expect(result).toBe(true)
      expect(mockProxy.reset).toHaveBeenCalledWith()
    })
  })

  describe('getComponentData', () => {
    it('should return proxy for registered component', () => {
      const mockProxy = { submit: vi.fn() }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key')

      expect(getComponentData('test-key')).toBe(mockProxy)
    })

    it('should return undefined for unregistered component', () => {
      expect(getComponentData('nonexistent')).toBeUndefined()
    })

    it('should return undefined for a key that was never registered', () => {
      // Registry is initialized but empty
      expect(getComponentData('never-registered')).toBeUndefined()
    })
  })

  describe('getComponentEntry', () => {
    it('should return full entry with proxy and helpers', () => {
      const mockProxy = { submit: vi.fn() }
      const mockHelpers = { fill: vi.fn() }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key', mockHelpers)

      const entry = getComponentEntry('test-key')
      expect(entry?.proxy).toBe(mockProxy)
      expect(entry?.helpers).toBe(mockHelpers)
    })

    it('should return entry with empty helpers when none provided', () => {
      const mockProxy = { submit: vi.fn() }
      vi.mocked(getCurrentInstance).mockReturnValue({ proxy: mockProxy } as any)
      registerComponent('test-key')

      const entry = getComponentEntry('test-key')
      expect(entry?.proxy).toBe(mockProxy)
      expect(entry?.helpers).toEqual({})
    })

    it('should return undefined for unregistered component', () => {
      expect(getComponentEntry('nonexistent')).toBeUndefined()
    })
  })
})
