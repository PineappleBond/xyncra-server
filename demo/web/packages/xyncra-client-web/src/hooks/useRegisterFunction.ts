/**
 * @packageDocumentation
 * useRegisterFunction — hook for registering a single function with the
 * Xyncra client.
 *
 * Registers the function on mount (or when info.name changes) and
 * automatically unregisters it on unmount.
 *
 * @module
 */

import type { FunctionInfo } from '@xyncra/protocol';
import { useEffect } from 'react';
import type { FunctionHandler } from '../internal/FunctionRegistry';
import { useXyncra } from './useXyncra';

/**
 * Register a single function with the Xyncra client.
 *
 * The function is registered when the component mounts (or when `info.name`
 * changes) and automatically unregistered when the component unmounts.
 *
 * @param info - Function metadata (name, description, parameters, etc.).
 * @param handler - Async function invoked when the server calls this function.
 *
 * @example
 * ```tsx
 * useRegisterFunction(
 *   {
 *     name: 'get_weather',
 *     description: 'Get weather for a location',
 *     parameters: { ... },
 *   },
 *   async (params) => {
 *     return { temp: 22, location: params.location };
 *   },
 * );
 * ```
 */
export function useRegisterFunction(
  info: FunctionInfo,
  handler: FunctionHandler,
): void {
  const { registerFunction, unregisterFunction } = useXyncra();

  useEffect(() => {
    registerFunction(info, handler);
    return () => {
      unregisterFunction(info.name);
    };
    // Re-register only when the function name changes.
    // The handler and info fields are captured via closure.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [info.name]);
}
