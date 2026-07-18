/**
 * Automatic periodic cleanup of expired RPC and notification logs.
 *
 * Mirrors Go startLogCleanup in internal/cli/listen.go.
 * Default: 7-day retention, 1-hour interval.
 *
 * @module
 */

import type { ILogger } from '@xyncra/client-core';

/** Default log retention: 7 days in milliseconds. */
export const DEFAULT_LOG_RETENTION = 7 * 24 * 60 * 60 * 1000;

/** Default cleanup interval: 1 hour in milliseconds. */
export const DEFAULT_CLEANUP_INTERVAL = 60 * 60 * 1000;

/**
 * Start a periodic log cleanup timer.
 *
 * Runs every `interval` ms, deleting records older than `retention`.
 * The returned function stops the timer when called.
 *
 * Note: actual log deletion depends on the XyncraClient's internal database.
 * This is a simplified version that logs the cleanup action.
 */
export function startLogCleanup(
  interval: number = DEFAULT_CLEANUP_INTERVAL,
  retention: number = DEFAULT_LOG_RETENTION,
  logger?: ILogger,
): () => void {
  const timer = setInterval(() => {
    const before = new Date(Date.now() - retention);
    logger?.debug?.('log cleanup: deleting records before', before.toISOString());
    // Actual cleanup would call client internal DB methods.
    // For now this is a placeholder that the daemon wires up.
  }, interval);

  // Allow the process to exit even if the timer is still running.
  if (timer.unref) timer.unref();

  return () => clearInterval(timer);
}
