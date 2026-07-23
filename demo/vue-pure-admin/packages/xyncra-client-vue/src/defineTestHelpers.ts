import type { FunctionInfo } from '@xyncra/protocol'
import type { FunctionEntry } from './composables/useRegisterFunctions'
import { useRegisterFunctions } from './composables/useRegisterFunctions'
import { registerComponent } from './utils/component-accessor'

/** HelperDef defines a single test helper function. */
export interface HelperDef {
  /** Helper function name (used in pg_* naming). */
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
 * @param routePath - Optional route path for the page (e.g. "/guide"). Added as a `route:` tag.
 */
export function defineTestHelpers(
  pageKey: string,
  helpers: Helpers,
  routePath: string = '',
): void {
  // 1. Extract helpers map: name -> handler function.
  const helpersMap: Record<string, (args: any) => any> = {}
  for (const [, def] of Object.entries(helpers)) {
    helpersMap[def.name] = (args: any) => def.handler(args)
  }

  // 2. Register component with helpers.
  try {
    registerComponent(pageKey, helpersMap)
  } catch (error) {
    console.warn(`[defineTestHelpers] Failed to register component for "${pageKey}":`, error)
  }

  // 3. Generate FunctionEntry array with pg_* naming.
  const underscoredKey = pageKey.replace(/-/g, '_')
  const functionEntries: FunctionEntry[] = Object.values(helpers).map((def) => ({
    info: {
      name: `pg_${underscoredKey}_${def.name}`,
      description: def.description,
      parameters: def.parameters,
      tags: [`page:${pageKey}`, 'type:helper', ...(routePath ? [`route:${routePath}`] : []), ...(def.tags ?? [])],
      timeout_ms: def.timeout_ms ?? 10000,
    },
    handler: async (params: Record<string, unknown>) => {
      await def.handler(params)
      return { success: true }
    },
  }))

  // 4. Register page functions (lifecycle managed by useRegisterFunctions).
  try {
    useRegisterFunctions(functionEntries)
  } catch (error) {
    console.warn(`[defineTestHelpers] Failed to register functions for "${pageKey}":`, error)
  }
}
