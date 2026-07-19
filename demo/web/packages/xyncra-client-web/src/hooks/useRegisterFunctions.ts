/**
 * @packageDocumentation
 * useRegisterFunctions — hook for batch-registering multiple functions with
 * the Xyncra client.
 *
 * Registers all functions on mount (or when the function name list changes)
 * and automatically unregisters them on unmount.
 *
 * @module
 */

import type { FunctionInfo } from '@xyncra/protocol';
import { useEffect } from 'react';
import type { FunctionHandler } from '../internal/FunctionRegistry';
import { useXyncra } from './useXyncra';

/**
 * A function entry for batch registration.
 */
export interface FunctionEntry {
  info: FunctionInfo;
  handler: FunctionHandler;
}

/**
 * Register multiple functions with the Xyncra client.
 *
 * All functions are registered when the component mounts (or when the set
 * of function names changes) and automatically unregistered on unmount.
 *
 * @param functions - Array of function entries, each with `info` and `handler`.
 *
 * @example
 * ```tsx
 * useRegisterFunctions([
 *   {
 *     info: { name: 'get_weather', description: 'Get weather', parameters: { ... } },
 *     handler: async (params) => ({ temp: 22 }),
 *   },
 *   {
 *     info: { name: 'get_time', description: 'Get current time', parameters: { ... } },
 *     handler: async () => ({ time: new Date().toISOString() }),
 *   },
 * ]);
 * ```
 */
export function useRegisterFunctions(functions: FunctionEntry[]): void {
  const { registerFunction, unregisterFunction } = useXyncra();

  useEffect(() => {
    for (const { info, handler } of functions) {
      registerFunction(info, handler);
    }

    return () => {
      for (const { info } of functions) {
        unregisterFunction(info.name);
      }
    };
    // Re-register only when the set of function names changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [functions.map((f) => f.info.name).join(',')]);
}
