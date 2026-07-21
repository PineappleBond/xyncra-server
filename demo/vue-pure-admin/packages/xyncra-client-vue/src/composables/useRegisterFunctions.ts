import { onMounted, onUnmounted } from 'vue'
import type { FunctionInfo } from '@xyncra/protocol'
import type { FunctionHandler } from '../internal/FunctionRegistry'
import { useXyncra } from './useXyncra'

export interface FunctionEntry {
  info: FunctionInfo
  handler: FunctionHandler
}

export function useRegisterFunctions(functions: FunctionEntry[]): void {
  const { registry } = useXyncra()

  onMounted(() => {
    for (const { info, handler } of functions) {
      registry.register(info, handler)
    }
  })

  onUnmounted(() => {
    // Use batchUnregister to avoid triggering multiple intermediate syncs.
    // This prevents the server from receiving partially-updated function
    // lists during page navigation.
    registry.batchUnregister(functions.map((f) => f.info.name))
  })
}
