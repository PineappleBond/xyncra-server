/**
 * RetryQueueStore — client-side retry queue persistence for RemoteCalling (D-137).
 *
 * Persists failed agent_resume calls for retry with exponential backoff.
 */

import type { XyncraDatabase } from './index';
import type { RetryQueueItem } from './models';

/**
 * RetryQueueStore provides client-side retry queue persistence (D-137).
 */
export class RetryQueueStore {
  constructor(private readonly db: XyncraDatabase) {}

  /**
   * Enqueues a retry item.
   */
  async enqueue(item: Omit<RetryQueueItem, 'id'>): Promise<number> {
    return await this.db.retryQueue.add(item as RetryQueueItem);
  }

  /**
   * Dequeues (removes) a retry item by ID.
   */
  async remove(id: number): Promise<void> {
    await this.db.retryQueue.delete(id);
  }

  /**
   * Returns all retry items that are ready for retry (next_retry_at < now).
   */
  async getReady(): Promise<RetryQueueItem[]> {
    const now = new Date();
    return await this.db.retryQueue
      .where('next_retry_at')
      .below(now)
      .toArray();
  }

  /**
   * Increments the retry count and updates next_retry_at with exponential backoff.
   * Backoff formula: min(2^(retryCount-1), 16) seconds.
   * retry_count starts from 0 when enqueued and is incremented before backoff calculation,
   * so after the first increment retry_count=1, yielding 2^0=1s backoff.
   * Sequence: 1s, 2s, 4s, 8s, 16s, 16s, ... (capped at 16s).
   */
  async incrementRetry(id: number): Promise<void> {
    const item = await this.db.retryQueue.get(id);
    if (!item) return;

    const newRetryCount = item.retry_count + 1;
    const backoffMs = Math.min(Math.pow(2, newRetryCount - 1), 16) * 1000;
    const nextRetryAt = new Date(Date.now() + backoffMs);

    await this.db.retryQueue.update(id, {
      retry_count: newRetryCount,
      next_retry_at: nextRetryAt,
    });
  }

  /**
   * Returns all retry items for a given remote calling ID.
   */
  async getByRemoteCallingId(rcId: string): Promise<RetryQueueItem[]> {
    return await this.db.retryQueue
      .where('remote_calling_id')
      .equals(rcId)
      .toArray();
  }

  /**
   * Removes all retry items for a given remote calling ID.
   */
  async deleteByRemoteCallingId(rcId: string): Promise<void> {
    await this.db.retryQueue
      .where('remote_calling_id')
      .equals(rcId)
      .delete();
  }
}
