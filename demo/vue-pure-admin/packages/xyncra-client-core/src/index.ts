/**
 * @packageDocumentation
 * Entry point for the `@xyncra/client-core` package.
 *
 * Re-exports all public types, constants, classes, and functions from the
 * core sub-modules so consumers can import everything from a single path:
 *
 * ```ts
 * import {
 *   IWebSocket, IWebSocketFactory, IIndexedDBProvider, ILogger,
 *   IUpdateHandler, ClientError, ConnectionError, TimeoutError, SyncError,
 *   ClientOptions,
 * } from '@xyncra/client-core';
 * ```
 */

export * from './connection-manager';
export * from './constants';
export * from './db';
export * from './errors';
export * from './idempotency-cache';
export * from './interfaces';
export * from './options';
export * from './response-retry-queue';
export * from './retry-manager';
export * from './rtt-tracker';
export * from './sync-manager';
export * from './xyncra-client';
