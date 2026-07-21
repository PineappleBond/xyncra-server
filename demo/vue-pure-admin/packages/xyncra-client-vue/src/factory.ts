import type { FunctionInfo } from '@xyncra/protocol'
import { callComponentMethod } from './utils/component-accessor'

export interface FunctionEntry {
  info: FunctionInfo
  handler: (params: Record<string, unknown>) => Promise<{ success: boolean; error?: string }>
}

export function buildFunctionEntry(
  info: FunctionInfo,
  handler: (params: Record<string, unknown>) => Promise<{ success: boolean; error?: string }>,
): FunctionEntry {
  return { info, handler }
}

export function createClickFunction(
  name: string,
  description: string,
  componentKey: string,
  methodName: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: { type: 'object', properties: {} },
      tags: [...tags, 'type:click'],
      timeout_ms: 15000,
    },
    async () => {
      callComponentMethod(componentKey, methodName)
      return { success: true }
    },
  )
}

export function createInputFunction(
  name: string,
  description: string,
  componentKey: string,
  fieldName: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          value: { type: 'string', description: '要填入的文本内容' },
        },
        required: ['value'],
      },
      tags: [...tags, 'type:input'],
      timeout_ms: 10000,
    },
    async (params) => {
      const value = params.value as string
      callComponentMethod(componentKey, 'setFieldValue', fieldName, value)
      return { success: true }
    },
  )
}

export function createSelectFunction(
  name: string,
  description: string,
  componentKey: string,
  fieldName: string,
  methodName: string = 'setFieldValue',
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          value: { type: 'string', description: '要选择的值' },
        },
        required: ['value'],
      },
      tags: [...tags, 'type:select'],
      timeout_ms: 10000,
    },
    async (params) => {
      const value = params.value as string
      callComponentMethod(componentKey, methodName, fieldName, value)
      return { success: true }
    },
  )
}

export function createSubmitFunction(
  name: string,
  description: string,
  componentKey: string,
  methodName: string = 'submit',
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: { type: 'object', properties: {} },
      tags: [...tags, 'type:submit'],
      timeout_ms: 20000,
    },
    async () => {
      callComponentMethod(componentKey, methodName)
      return { success: true }
    },
  )
}

export function createTabFunction(
  name: string,
  description: string,
  componentKey: string,
  methodName: string = 'switchTab',
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          tab: { type: 'string', description: '要切换到的标签页名称' },
        },
        required: ['tab'],
      },
      tags: [...tags, 'type:tab'],
      timeout_ms: 10000,
    },
    async (params) => {
      const tab = params.tab as string
      callComponentMethod(componentKey, methodName, tab)
      return { success: true }
    },
  )
}

export function createSearchFunction(
  name: string,
  description: string,
  componentKey: string,
  methodName: string = 'search',
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          keyword: { type: 'string', description: '搜索关键词' },
        },
      },
      tags: [...tags, 'type:search'],
      timeout_ms: 15000,
    },
    async (params) => {
      const keyword = params.keyword as string
      callComponentMethod(componentKey, methodName, keyword)
      return { success: true }
    },
  )
}

export function createResetFunction(
  name: string,
  description: string,
  componentKey: string,
  methodName: string = 'reset',
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: { type: 'object', properties: {} },
      tags: [...tags, 'type:reset'],
      timeout_ms: 10000,
    },
    async () => {
      callComponentMethod(componentKey, methodName)
      return { success: true }
    },
  )
}
