import { getCurrentInstance } from 'vue'

/**
 * ComponentEntry holds a component proxy and its associated test helpers.
 */
export interface ComponentEntry {
  proxy: any
  helpers: Record<string, (args: any) => any>
}

let componentRegistry: Map<string, ComponentEntry> | null = null

/**
 * initComponentRegistry initializes the global component registry.
 * Must be called once during plugin installation.
 */
export function initComponentRegistry(): void {
  componentRegistry = new Map()
}

/**
 * registerComponent registers the current component instance under the given key.
 * Optionally accepts a helpers map for test helper functions.
 */
export function registerComponent(key: string, helpers?: Record<string, (args: any) => any>): void {
  if (!componentRegistry) {
    console.warn('[xyncra] Component registry not initialized. Plugin not installed?')
    return
  }

  const instance = getCurrentInstance()
  if (!instance) return

  componentRegistry.set(key, {
    proxy: instance.proxy,
    helpers: helpers ?? {},
  })
}

/**
 * callComponentMethod calls a method on a registered component.
 * Lookup order: proxy method first, then helpers map.
 * When falling back to helpers, args[0] is passed as the single object argument.
 */
export function callComponentMethod(
  componentKey: string,
  method: string,
  ...args: any[]
): any {
  if (!componentRegistry) {
    console.warn('[xyncra] Component registry not initialized')
    return undefined
  }

  const entry = componentRegistry.get(componentKey)
  if (!entry) {
    console.warn('[xyncra] Component "' + componentKey + '" not registered. Developer did not expose this component.')
    return undefined
  }

  // Try proxy method first.
  if (typeof entry.proxy[method] === 'function') {
    return entry.proxy[method](...args)
  }

  // Fall back to helpers map.
  const helper = entry.helpers[method]
  if (typeof helper === 'function') {
    return helper(args[0])
  }

  console.warn('[xyncra] Method "' + method + '" not exposed by component "' + componentKey + '"')
  return undefined
}

/**
 * getComponentData returns the proxy of a registered component.
 * Kept for backward compatibility.
 */
export function getComponentData(componentKey: string): any {
  if (!componentRegistry) return undefined
  const entry = componentRegistry.get(componentKey)
  return entry?.proxy
}

/**
 * getComponentEntry returns the full ComponentEntry (proxy + helpers)
 * for a registered component, or undefined if not found.
 */
export function getComponentEntry(componentKey: string): ComponentEntry | undefined {
  if (!componentRegistry) return undefined
  return componentRegistry.get(componentKey)
}
