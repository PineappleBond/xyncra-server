/**
 * @packageDocumentation
 * RemoteCallingRetryManager — infinite retry for agent_resume RPC calls (D-137).
 *
 * When a client executes a RemoteCalling and calls agent_resume to report the
 * result, the RPC may fail due to server errors or network issues. The result
 * must not be lost, so this manager persists failed agent_resume calls to the
 * RetryQueueStore and retries them with exponential backoff until the server
 * confirms success.
 *
 * Design principles (from remote-calling-design.md):
 *   - Infinite retry: no max attempts — the result cannot be lost.
 *   - Exponential backoff: 1s, 2s, 4s, 8s, 16s (cap).
 *   - Persistence: retry queue is stored in IndexedDB, survives page reload.
 *   - Dedup: if the server returns "already processed", the item is removed.
 */

import type { XyncraDatabase } from './db';
import type { RetryQueueItem } from './db/models';
import type { ILogger } from './interfaces';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Default polling interval in milliseconds. */
const DefaultPollInterval = 2000;

/** Maximum backoff delay in milliseconds (16 seconds). */
const MaxBackoffMs = 16_000;

// ---------------------------------------------------------------------------
// RemoteCallingRetryManagerOptions
// ---------------------------------------------------------------------------

/**
 * Configuration options for RemoteCallingRetryManager.
 */
export interface RemoteCallingRetryManagerOptions {
  /** Database instance for accessing the retry queue. */
  db: XyncraDatabase;
  /** RPC function to execute agent_resume calls. */
  rpcFn: (method: string, params: unknown) => Promise<unknown>;
  /** Structured logger. */
  logger: ILogger;
  /** Polling interval in milliseconds. Default: 2000. */
  pollInterval?: number;
}

// ---------------------------------------------------------------------------
// RemoteCallingRetryManager
// ---------------------------------------------------------------------------

/**
 * RemoteCallingRetryManager polls the RetryQueueStore for items ready to retry,
 * calls agent_resume for each, and handles success/failure with exponential backoff.
 *
 * This manager runs an infinite retry loop — items are never marked as "failed".
 * The only removal paths are:
 *   1. Server returns success (code=0).
 *   2. Server returns "already processed" (idempotency).
 *   3. Explicit deletion by the user (e.g., cancel_remote_calls).
 */
export class RemoteCallingRetryManager {
  private readonly db: XyncraDatabase;
  private readonly rpcFn: (method: string, params: unknown) => Promise<unknown>;
  private readonly logger: ILogger;
  private readonly pollInterval: number;

  private running = false;
  private pollTimer: ReturnType<typeof setTimeout> | null = null;

  /** Retry statistics for observability (D-137). */
  private stats = {
    totalRetries: 0,
    successfulRetries: 0,
    failedRetries: 0,
    lastPollAt: 0,
    lastQueueSize: 0,
    /** Total number of items ever enqueued (monotonically increasing counter). */
    totalEnqueued: 0,
  };

  constructor(options: RemoteCallingRetryManagerOptions) {
    this.db = options.db;
    this.rpcFn = options.rpcFn;
    this.logger = options.logger;
    this.pollInterval = options.pollInterval ?? DefaultPollInterval;
  }

  // ---------------------------------------------------------------------------
  // Public API
  // ---------------------------------------------------------------------------

  /**
   * Starts the retry polling loop.
   */
  start(): void {
    if (this.running) return;
    this.running = true;
    void this.pollLoop();
  }

  /**
   * Stops the retry polling loop and clears any pending timer.
   */
  stop(): void {
    this.running = false;
    if (this.pollTimer !== null) {
      clearTimeout(this.pollTimer);
      this.pollTimer = null;
    }
  }

  /**
   * Returns current retry statistics for observability.
   */
  getStats(): Readonly<typeof this.stats> {
    return { ...this.stats };
  }

  /**
   * Enqueues a failed agent_resume call for retry.
   *
   * @param remoteCallingId - The RemoteCalling ID.
   * @param success - Whether the original call succeeded.
   * @param result - Result on success.
   * @param errorMessage - Error message on failure.
   * @param agentId - Agent ID for the resume call.
   */
  async enqueue(
    remoteCallingId: string,
    success: boolean,
    result: string,
    errorMessage: string,
    agentId: string,
  ): Promise<void> {
    // Dedup check: skip if an item with the same remote_calling_id already exists.
    // This prevents duplicate retry entries when agent_resume is called multiple
    // times for the same RemoteCalling (e.g., due to rapid retries or race conditions).
    const existing = await this.db.retryQueueStore.getByRemoteCallingId(remoteCallingId);
    if (existing.length > 0) {
      this.logger.debug('Skipping duplicate enqueue for remote calling', {
        remote_calling_id: remoteCallingId,
      });
      return;
    }

    const item: Omit<RetryQueueItem, 'id'> = {
      remote_calling_id: remoteCallingId,
      success,
      result,
      error_message: errorMessage,
      agent_id: agentId,
      retry_count: 0,
      next_retry_at: new Date(), // Ready for immediate first retry.
      created_at: new Date(),
    };
    await this.db.retryQueueStore.enqueue(item);
    this.stats.totalEnqueued++;
    this.logger.info('Enqueued agent_resume for retry', {
      remote_calling_id: remoteCallingId,
    });
  }

  // ---------------------------------------------------------------------------
  // Internal methods (private)
  // ---------------------------------------------------------------------------

  /**
   * Continuously polls for retry items that are ready.
   */
  private async pollLoop(): Promise<void> {
    while (this.running) {
      try {
        const items = await this.db.retryQueueStore.getReady();
        this.stats.lastPollAt = Date.now();
        this.stats.lastQueueSize = items.length;

        for (const item of items) {
          if (!this.running) break;
          await this.executeRetry(item);
        }
      } catch (error) {
        this.logger.error('RemoteCalling retry poll loop failed', error);
      }

      // Wait for pollInterval, honouring the running flag.
      if (!this.running) break;
      await new Promise<void>((resolve) => {
        this.pollTimer = setTimeout(() => {
          this.pollTimer = null;
          resolve();
        }, this.pollInterval);
      });
    }
  }

  /**
   * Attempts to retry a single agent_resume call.
   * On success: removes from queue.
   * On failure: increments retry count with exponential backoff (capped at 16s).
   * Never marks as "failed" — infinite retry.
   */
  private async executeRetry(item: RetryQueueItem): Promise<void> {
    const itemId = item.id;
    if (itemId === undefined) {
      this.logger.error('RetryQueueItem has no id, skipping', item);
      return;
    }

    this.stats.totalRetries++;

    try {
      const params = {
        id: item.remote_calling_id,
        success: item.success,
        result: item.result,
        error_message: item.error_message,
        agent_id: item.agent_id,
      };

      await this.rpcFn('agent_resume', params);

      // Success: remove from queue.
      await this.db.retryQueueStore.remove(itemId);
      this.stats.successfulRetries++;
      this.logger.info('Agent resume retry succeeded', {
        remote_calling_id: item.remote_calling_id,
        retry_count: item.retry_count,
      });
    } catch (error) {
      this.stats.failedRetries++;
      const errorMsg = error instanceof Error ? error.message : String(error);
      this.logger.warn('Agent resume retry failed', {
        remote_calling_id: item.remote_calling_id,
        retry_count: item.retry_count,
        error: errorMsg,
      });

      // Update retry count with exponential backoff (capped at 16s).
      // No max attempts — infinite retry per design doc.
      await this.db.retryQueueStore.incrementRetry(itemId);
    }
  }
}
