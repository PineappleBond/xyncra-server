import type { FunctionInfo } from '@xyncra/protocol'
import type { FunctionEntry } from './composables/useRegisterFunctions'
import { useRegisterFunctions } from './composables/useRegisterFunctions'
import { registerComponent } from './utils/component-accessor'

/** HelperDef defines a single test helper function. */
export interface HelperDef {
  /** Helper function name (used in pg_* naming and window mount). */
  name: string
  /** Human-readable description for Agent. */
  description: string
  /** JSON Schema parameters (reuses FunctionInfo.parameters format). */
  parameters: FunctionInfo['parameters']
  /** Handler function receiving a single object argument. */
  handler: (args: Record<string, unknown>) => any
  /** Optional tags for categorization. */
  tags?: string[]
  /** Optional timeout in milliseconds, default 10000. */
  timeout_ms?: number
}

/** Helpers maps helper names to their definitions. */
export type Helpers = Record<string, HelperDef>

/** DefineTestHelpersOptions configures defineTestHelpers behavior. */
export interface DefineTestHelpersOptions {
  /** Whether to mount helpers on window.XyncraTestHelpers. Default true. */
  exposeToWindow?: boolean
}

/**
 * defineTestHelpers registers page-level test helpers in a single call.
 *
 * This composable replaces the three-step boilerplate of
 * registerComponent + useXxxFunctions + defineExpose. It must be called
 * synchronously at the top level of <script setup> because it depends on
 * getCurrentInstance (via registerComponent) and inject (via useXyncra).
 *
 * @param pageKey - Unique page identifier (e.g. "login", "table-demo").
 * @param helpers - Map of helper definitions to register.
 * @param options - Optional configuration for window exposure.
 */
export function defineTestHelpers(
  pageKey: string,
  helpers: Helpers,
  options?: DefineTestHelpersOptions,
): void {
  // 1. Mount to window FIRST (before any composable that might fail).
  const exposeToWindow = options?.exposeToWindow ?? true
  if (exposeToWindow && typeof window !== 'undefined') {
    if (!window.XyncraTestHelpers) {
      window.XyncraTestHelpers = {}
    }
    window.XyncraTestHelpers[pageKey] = {}
    for (const def of Object.values(helpers)) {
      window.XyncraTestHelpers[pageKey][def.name] = (args: Record<string, unknown>) => def.handler(args)
    }
    console.log(`[defineTestHelpers] Mounted ${Object.keys(helpers).length} helpers for "${pageKey}" on window.XyncraTestHelpers`)
  }

  // 2. Extract helpers map: name -> handler function.
  const helpersMap: Record<string, (args: any) => any> = {}
  for (const [, def] of Object.entries(helpers)) {
    helpersMap[def.name] = (args: any) => def.handler(args)
  }

  // 3. Register component with helpers.
  try {
    registerComponent(pageKey, helpersMap)
  } catch (error) {
    console.warn(`[defineTestHelpers] Failed to register component for "${pageKey}":`, error)
  }

  // 4. Generate FunctionEntry array with pg_* naming.
  const underscoredKey = pageKey.replace(/-/g, '_')
  const functionEntries: FunctionEntry[] = Object.values(helpers).map((def) => ({
    info: {
      name: `pg_${underscoredKey}_${def.name}`,
      description: def.description,
      parameters: def.parameters,
      tags: [`page:${pageKey}`, 'type:helper', ...(def.tags ?? [])],
      timeout_ms: def.timeout_ms ?? 10000,
    },
    handler: async (params: Record<string, unknown>) => {
      await def.handler(params)
      return { success: true }
    },
  }))

  // 5. Register page functions (lifecycle managed by useRegisterFunctions).
  try {
    useRegisterFunctions(functionEntries)
  } catch (error) {
    console.warn(`[defineTestHelpers] Failed to register functions for "${pageKey}":`, error)
  }
}

declare global {
  interface Window {
    XyncraTestHelpers?: Record<string, Record<string, (args: Record<string, unknown>) => any>>
  }
}
