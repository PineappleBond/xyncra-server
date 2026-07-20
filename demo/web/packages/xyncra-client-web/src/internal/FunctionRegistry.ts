/**
 * @packageDocumentation
 * FunctionRegistry — client-side function registration center.
 *
 * Design decision D-3: Function registration is a two-phase process:
 *   1. functionRegistry.register(info, handler) stores the handler locally.
 *   2. XyncraProvider listens to functionRegistry.onChange and calls
 *      client.call('system.register_functions', ...) to sync to the server.
 *
 * The registry tracks a monotonically increasing version number. Each
 * register/unregister call increments the version, allowing XyncraProvider
 * to detect changes and avoid redundant server syncs.
 *
 * @module
 */

import type { FunctionInfo, PackageDataRequest } from '@xyncra/protocol';

/**
 * FunctionHandler processes a server-initiated function call and returns
 * the result. The result is serialized and sent back to the server.
 */
export type FunctionHandler = (
  params: Record<string, unknown>,
) => Promise<unknown>;

/**
 * InternalEntry stores a registered function's info and handler.
 */
interface InternalEntry {
  info: FunctionInfo;
  handler: FunctionHandler;
}

/**
 * FunctionRegistry manages client-side function registrations.
 *
 * Functions registered here can be invoked by the server via reverse RPC.
 * The registry provides change notification so that XyncraProvider can
 * synchronize the full function list to the server after each mutation.
 */
export class FunctionRegistry {
  private entries = new Map<string, InternalEntry>();
  private version = 0;
  private changeCallbacks = new Set<() => void>();

  /**
   * Register a function with its metadata and handler.
   *
   * If a function with the same name already exists, it is replaced.
   * Increments the version and notifies change listeners.
   *
   * @param info - Function metadata (name, description, parameters, etc.).
   * @param handler - Async function invoked when the server calls this function.
   * @throws If the function name is empty.
   */
  register(info: FunctionInfo, handler: FunctionHandler): void {
    if (!info.name || info.name.length === 0) {
      throw new Error('Function name must be non-empty');
    }
    this.entries.set(info.name, { info, handler });
    this.version++;
    this.notifyChange();
  }

  /**
   * Unregister a function by name.
   *
   * If the function does not exist, this is a no-op.
   * Increments the version and notifies change listeners only if a
   * function was actually removed.
   *
   * @param name - The function name to remove.
   */
  unregister(name: string): void {
    if (!this.entries.has(name)) return;
    this.entries.delete(name);
    this.version++;
    this.notifyChange();
  }

  /**
   * Unregister multiple functions by name in a single batch operation.
   *
   * This is more efficient than calling unregister() in a loop because it
   * only increments the version once and triggers a single change
   * notification. Use this during component unmount to avoid intermediate
   * states where the server receives partially-updated function lists.
   *
   * @param names - Array of function names to remove.
   */
  batchUnregister(names: string[]): void {
    let changed = false;
    for (const name of names) {
      if (this.entries.has(name)) {
        this.entries.delete(name);
        changed = true;
      }
    }
    if (changed) {
      this.version++;
      this.notifyChange();
    }
  }

  /**
   * Get the handler for a function by name.
   *
   * @param name - The function name to look up.
   * @returns The handler function, or undefined if not registered.
   */
  getHandler(name: string): FunctionHandler | undefined {
    return this.entries.get(name)?.handler;
  }

  /**
   * Get all registered FunctionInfo objects.
   * Returns a new array each time to prevent external mutation.
   */
  getFunctionInfos(): FunctionInfo[] {
    return Array.from(this.entries.values(), (entry) => entry.info);
  }

  /**
   * Get the current version number.
   *
   * The version starts at 0 and increments on every register/unregister call.
   * XyncraProvider can compare versions to avoid redundant server syncs.
   */
  getVersion(): number {
    return this.version;
  }

  /**
   * Subscribe to registry changes.
   *
   * The callback is invoked after every successful register/unregister call.
   *
   * @param callback - Function called when the registry changes.
   * @returns A function that removes this subscription when called.
   */
  onChange(callback: () => void): () => void {
    this.changeCallbacks.add(callback);
    return () => {
      this.changeCallbacks.delete(callback);
    };
  }

  /**
   * Create a request handler function suitable for passing to
   * client.registerRequestHandler(). The returned handler looks up the
   * function by name in this registry and invokes its handler.
   *
   * @param name - The function name to create a handler for.
   * @returns A RequestHandlerFunc, or undefined if the function is not registered.
   */
  createRequestHandler(
    name: string,
  ): ((req: PackageDataRequest) => Promise<unknown>) | undefined {
    const handler = this.getHandler(name);
    if (!handler) return undefined;

    return async (req: PackageDataRequest): Promise<unknown> => {
      const params = (req.params as Record<string, unknown>) ?? {};
      return handler(params);
    };
  }

  /**
   * Get the number of registered functions.
   */
  get size(): number {
    return this.entries.size;
  }

  /**
   * Remove all registrations and reset the version.
   * Notifies change listeners.
   */
  clear(): void {
    if (this.entries.size === 0) return;
    this.entries.clear();
    this.version++;
    this.notifyChange();
  }

  private notifyChange(): void {
    for (const cb of this.changeCallbacks) {
      try {
        cb();
      } catch {
        // Swallow listener errors to prevent one bad callback from
        // breaking others.
      }
    }
  }
}
