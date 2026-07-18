/**
 * @packageDocumentation
 * RetryManager — manages the retry queue for failed RPC calls.
 *
 * Mirrors Go retryManager (pkg/client/retry.go):
 *   - Persists failed RPC calls to the retry queue (IndexedDB via QueueStore).
 *   - Polls for pending tasks at a configurable interval.
 *   - Executes tasks via the injected RPC function.
 *   - On success: deletes the task.
 *   - On failure: calculates exponential backoff (C3) and updates next_retry,
 *     or marks the task as "failed" if max attempts are exhausted.
 *
 * Concurrency model differences from Go:
 *   Go goroutine + time.Ticker  → TS while(running) + setTimeout + await Promise
 *   Go context.Context cancel   → TS running flag + clearTimeout
 */

import { backoffDelay } from './connection-manager';
import {
  DefaultReconnectMaxDelay,
  DefaultRetryBaseDelay,
  DefaultRetryMaxAttempts,
  DefaultRetryPollInterval,
} from './constants';
import type { XyncraDatabase } from './db';
import type { RetryTask } from './db/models';
import type { ILogger } from './interfaces';

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

/**
 * Configuration options for the RetryManager.
 */
export interface RetryManagerOptions {
  /** Database instance for persisting retry tasks. */
  db: XyncraDatabase;
  /** RPC function to execute retry calls. */
  rpcFn: (method: string, params: unknown) => Promise<unknown>;
  /** Structured logger. */
  logger: ILogger;
  /** Polling interval in milliseconds. Defaults to DefaultRetryPollInterval. */
  pollInterval?: number;
  /** Base delay for exponential backoff in milliseconds. Defaults to DefaultRetryBaseDelay. */
  baseDelay?: number;
  /** Maximum delay cap for exponential backoff in milliseconds. Defaults to DefaultReconnectMaxDelay. */
  maxDelay?: number;
  /** Maximum number of retry attempts. Defaults to DefaultRetryMaxAttempts. */
  maxAttempts?: number;
}

// ---------------------------------------------------------------------------
// Text encoding helpers
// ---------------------------------------------------------------------------

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

/**
 * Encodes a JSON-serialisable value into a Uint8Array for storage.
 */
function encodeParams(params: unknown): Uint8Array {
  return textEncoder.encode(JSON.stringify(params));
}

/**
 * Decodes a Uint8Array back into a JSON-serialisable value.
 */
function decodeParams(data: Uint8Array): unknown {
  return JSON.parse(textDecoder.decode(data));
}

// ---------------------------------------------------------------------------
// RetryManager
// ---------------------------------------------------------------------------

/**
 * RetryManager manages the retry queue for failed RPC calls.
 *
 * Mirrors Go retryManager (pkg/client/retry.go).
 */
export class RetryManager {
  private readonly db: XyncraDatabase;
  private readonly rpcFn: (method: string, params: unknown) => Promise<unknown>;
  private readonly logger: ILogger;
  private readonly pollInterval: number;
  private readonly baseDelay: number;
  private readonly maxDelay: number;
  private readonly maxAttempts: number;

  private running = false;
  private pollTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(options: RetryManagerOptions) {
    this.db = options.db;
    this.rpcFn = options.rpcFn;
    this.logger = options.logger;
    this.pollInterval = options.pollInterval ?? DefaultRetryPollInterval;
    this.baseDelay = options.baseDelay ?? DefaultRetryBaseDelay;
    this.maxDelay = options.maxDelay ?? DefaultReconnectMaxDelay;
    this.maxAttempts = options.maxAttempts ?? DefaultRetryMaxAttempts;
  }

  // -----------------------------------------------------------------------
  // Public API
  // -----------------------------------------------------------------------

  /**
   * Starts the retry polling loop.
   *
   * Mirrors Go retryManager.Start().
   */
  start(): void {
    if (this.running) return;
    this.running = true;
    // Fire-and-forget: pollLoop manages its own error handling.
    void this.pollLoop();
  }

  /**
   * Stops the retry polling loop and clears any pending timer.
   *
   * Mirrors Go retryManager.Stop().
   */
  stop(): void {
    this.running = false;
    if (this.pollTimer !== null) {
      clearTimeout(this.pollTimer);
      this.pollTimer = null;
    }
  }

  /**
   * Enqueues a failed RPC call for retry.
   *
   * Mirrors Go retryManager.Enqueue().
   *
   * @param method - RPC method name.
   * @param params - RPC parameters (will be JSON-serialised and stored as Uint8Array).
   */
  async enqueue(method: string, params: unknown): Promise<void> {
    const task: RetryTask = {
      id: crypto.randomUUID(),
      method,
      params: encodeParams(params),
      attempt: 0,
      max_attempts: this.maxAttempts,
      next_retry: new Date(),
      status: 'pending',
      last_error: '',
      created_at: new Date(),
    };
    await this.db.queueStore.save(task);
  }

  // -----------------------------------------------------------------------
  // Internal methods (private)
  // -----------------------------------------------------------------------

  /**
   * Continuously polls for pending retry tasks at the configured interval.
   *
   * Mirrors Go retryManager.pollLoop().
   */
  private async pollLoop(): Promise<void> {
    while (this.running) {
      try {
        const tasks = await this.db.queueStore.listPending(50);

        for (const task of tasks) {
          if (!this.running) break;
          await this.executeTask(task);
        }
      } catch (error) {
        this.logger.error('Retry poll loop failed', error);
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
   * Attempts to retry a single task and updates its state based on the result.
   *
   * Mirrors Go retryManager.executeTask().
   */
  private async executeTask(task: RetryTask): Promise<void> {
    // Increment attempt count (mirrors Go: task.Attempt++).
    task.attempt++;

    try {
      const params = decodeParams(task.params);
      await this.rpcFn(task.method, params);

      // Success: delete task (mirrors Go: rm.db.Queue.Delete(ctx, task.ID)).
      await this.db.queueStore.delete(task.id);
      this.logger.debug(`Retry task succeeded: ${task.method}`);
    } catch (error) {
      const errorMsg = error instanceof Error ? error.message : String(error);
      this.logger.warn(`Retry task failed: ${task.method}`, error);

      // Failed: update attempt count and error message.
      task.last_error = errorMsg;

      if (task.attempt >= task.max_attempts) {
        // Max attempts reached: mark as failed permanently.
        task.status = 'failed';
        try {
          await this.db.queueStore.update(task);
        } catch (updateError) {
          this.logger.error('Failed to mark task as failed', updateError);
        }
        this.logger.error(`Retry task exhausted: ${task.method}`);
      } else {
        // Calculate next retry time with exponential backoff (C3).
        const delay = backoffDelay(task.attempt, this.baseDelay, this.maxDelay);
        task.next_retry = new Date(Date.now() + delay);

        try {
          await this.db.queueStore.update(task);
        } catch (updateError) {
          this.logger.error('Failed to update task retry state', updateError);
        }
      }
    }
  }
}
