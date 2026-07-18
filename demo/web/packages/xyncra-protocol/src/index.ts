/**
 * @packageDocumentation
 * Entry point for the `@xyncra/protocol` package.
 *
 * Re-exports all public types, constants, classes, and functions from the
 * protocol sub-modules so consumers can import everything from a single path:
 *
 * ```ts
 * import { Package, UpdateType, HandlerError, FunctionInfo } from '@xyncra/protocol';
 * ```
 */

export * from './errors';
export * from './function';
export * from './package';
