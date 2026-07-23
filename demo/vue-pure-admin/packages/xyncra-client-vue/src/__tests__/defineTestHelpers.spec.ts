import { describe, it, expect, vi, beforeEach } from 'vitest'

const { mockRegister, mockBatchUnregister } = vi.hoisted(() => ({
  mockRegister: vi.fn(),
  mockBatchUnregister: vi.fn(),
}))

vi.mock('vue', async () => {
  const actual = await vi.importActual<typeof import('vue')>('vue')
  return {
    ...actual,
    getCurrentInstance: vi.fn().mockReturnValue({ proxy: {} }),
    onMounted: vi.fn((cb: () => void) => cb()),
    onUnmounted: vi.fn(),
    inject: vi.fn().mockReturnValue({
      registry: {
        register: mockRegister,
        batchUnregister: mockBatchUnregister,
      },
    }),
  }
})

vi.mock('../utils/component-accessor', () => ({
  registerComponent: vi.fn(),
}))

import { defineTestHelpers } from '../defineTestHelpers'
import { registerComponent } from '../utils/component-accessor'

describe('defineTestHelpers', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // -------------------------------------------------------
  // pg_* naming
  // -------------------------------------------------------
  describe('pg_* naming', () => {
    it('should generate correct pg_* function names with underscored pageKey', () => {
      const handler = vi.fn()
      const helpers = {
        fill: {
          name: 'fill',
          description: 'Fill form',
          parameters: { type: 'object' as const, properties: {} },
          handler,
        },
      }

      defineTestHelpers('schema-form', helpers)

      // registerComponent called with the pageKey and a helpers map
      expect(registerComponent).toHaveBeenCalledWith(
        'schema-form',
        expect.objectContaining({ fill: expect.any(Function) }),
      )

      // useRegisterFunctions should have been invoked via onMounted;
      // verify register was called with the pg_ prefixed name.
      expect(mockRegister).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'pg_schema_form_fill' }),
        expect.any(Function),
      )
    })

    it('should handle pageKey without hyphens', () => {
      const handler = vi.fn()
      const helpers = {
        submit: {
          name: 'submit',
          description: 'Submit form',
          parameters: { type: 'object' as const, properties: {} },
          handler,
        },
      }

      defineTestHelpers('login', helpers)

      expect(registerComponent).toHaveBeenCalledWith(
        'login',
        expect.objectContaining({ submit: expect.any(Function) }),
      )

      expect(mockRegister).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'pg_login_submit' }),
        expect.any(Function),
      )
    })

    it('should replace all hyphens in pageKey', () => {
      const helpers = {
        action: {
          name: 'action',
          description: 'An action',
          parameters: { type: 'object' as const, properties: {} },
          handler: vi.fn(),
        },
      }

      defineTestHelpers('my-complex-page', helpers)

      expect(mockRegister).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'pg_my_complex_page_action' }),
        expect.any(Function),
      )
    })
  })

  // -------------------------------------------------------
  // parameters passthrough
  // -------------------------------------------------------
  describe('parameters passthrough', () => {
    it('should pass parameters JSON Schema to FunctionInfo', () => {
      const schema = {
        type: 'object' as const,
        properties: {
          value: { type: 'string' as const, description: 'Input value' },
        },
        required: ['value'],
      }
      const helpers = {
        fill: {
          name: 'fill',
          description: 'Fill field',
          parameters: schema,
          handler: vi.fn(),
        },
      }

      defineTestHelpers('test', helpers)

      expect(mockRegister).toHaveBeenCalledWith(
        expect.objectContaining({ parameters: schema }),
        expect.any(Function),
      )
    })

    it('should pass description through to FunctionInfo', () => {
      const helpers = {
        doStuff: {
          name: 'doStuff',
          description: 'Does something important',
          parameters: { type: 'object' as const, properties: {} },
          handler: vi.fn(),
        },
      }

      defineTestHelpers('page', helpers)

      expect(mockRegister).toHaveBeenCalledWith(
        expect.objectContaining({ description: 'Does something important' }),
        expect.any(Function),
      )
    })
  })

  // -------------------------------------------------------
  // tags
  // -------------------------------------------------------
  describe('tags', () => {
    it('should include default tags page:<key> and type:helper', () => {
      const helpers = {
        fill: {
          name: 'fill',
          description: 'Fill',
          parameters: { type: 'object' as const, properties: {} },
          handler: vi.fn(),
        },
      }

      defineTestHelpers('login', helpers)

      expect(mockRegister).toHaveBeenCalledWith(
        expect.objectContaining({
          tags: expect.arrayContaining(['page:login', 'type:helper']),
        }),
        expect.any(Function),
      )
    })

    it('should merge custom tags with default tags', () => {
      const helpers = {
        fill: {
          name: 'fill',
          description: 'Fill',
          parameters: { type: 'object' as const, properties: {} },
          handler: vi.fn(),
          tags: ['form', 'critical'],
        },
      }

      defineTestHelpers('login', helpers)

      expect(mockRegister).toHaveBeenCalledWith(
        expect.objectContaining({
          tags: ['page:login', 'type:helper', 'form', 'critical'],
        }),
        expect.any(Function),
      )
    })
  })

  // -------------------------------------------------------
  // timeout_ms
  // -------------------------------------------------------
  describe('timeout_ms', () => {
    it('should default timeout to 10000', () => {
      const helpers = {
        fill: {
          name: 'fill',
          description: 'Fill',
          parameters: { type: 'object' as const, properties: {} },
          handler: vi.fn(),
        },
      }

      defineTestHelpers('login', helpers)

      expect(mockRegister).toHaveBeenCalledWith(
        expect.objectContaining({ timeout_ms: 10000 }),
        expect.any(Function),
      )
    })

    it('should use custom timeout when provided', () => {
      const helpers = {
        fill: {
          name: 'fill',
          description: 'Fill',
          parameters: { type: 'object' as const, properties: {} },
          handler: vi.fn(),
          timeout_ms: 30000,
        },
      }

      defineTestHelpers('login', helpers)

      expect(mockRegister).toHaveBeenCalledWith(
        expect.objectContaining({ timeout_ms: 30000 }),
        expect.any(Function),
      )
    })
  })

  // -------------------------------------------------------
  // registerComponent integration
  // -------------------------------------------------------
  describe('registerComponent integration', () => {
    it('should call registerComponent with pageKey and helpers map', () => {
      const fillHandler = vi.fn()
      const submitHandler = vi.fn()
      const helpers = {
        fill: {
          name: 'fill',
          description: 'Fill',
          parameters: { type: 'object' as const, properties: {} },
          handler: fillHandler,
        },
        submit: {
          name: 'submit',
          description: 'Submit',
          parameters: { type: 'object' as const, properties: {} },
          handler: submitHandler,
        },
      }

      defineTestHelpers('login', helpers)

      expect(registerComponent).toHaveBeenCalledWith('login', {
        fill: expect.any(Function),
        submit: expect.any(Function),
      })
    })

    it('should pass empty helpers map when no helpers defined', () => {
      defineTestHelpers('login', {})

      expect(registerComponent).toHaveBeenCalledWith('login', {})
    })
  })

  // -------------------------------------------------------
  // helper function behavior
  // -------------------------------------------------------
  describe('helper function behavior', () => {
    it('should call handler when registerComponent helper is invoked', () => {
      const handler = vi.fn()
      const helpers = {
        fill: {
          name: 'fill',
          description: 'Fill',
          parameters: { type: 'object' as const, properties: {} },
          handler,
        },
      }

      defineTestHelpers('login', helpers)

      // Extract the helpers map that was passed to registerComponent
      const registeredMap = vi.mocked(registerComponent).mock.calls[0][1] as Record<
        string,
        (args: any) => any
      >
      registeredMap.fill({ value: 'hello' })

      expect(handler).toHaveBeenCalledWith({ value: 'hello' })
    })

    it('handler registered via useRegisterFunctions should return { success: true }', async () => {
      const handler = vi.fn()
      const helpers = {
        fill: {
          name: 'fill',
          description: 'Fill',
          parameters: { type: 'object' as const, properties: {} },
          handler,
        },
      }

      defineTestHelpers('login', helpers)

      // Extract the handler passed to registerFunction
      const registeredHandler = mockRegister.mock.calls[0][1] as (
        params: Record<string, unknown>,
      ) => Promise<any>
      const result = await registeredHandler({ value: 'test' })

      expect(result).toEqual({ success: true })
    })
  })
})
