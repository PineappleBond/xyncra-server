/**
 * QueueStore — data access operations for the retry task queue.
 *
 * Mirrors Go QueueStore (pkg/store/queue_store.go).
 */

import { ErrNotFound } from '../errors';
import type { XyncraDatabase } from './index';
import type { RetryTask } from './models';

/**
 * QueueStore provides data access operations for the retry task queue.
 */
export class QueueStore {
  constructor(private readonly db: XyncraDatabase) {}

  /**
   * Inserts a new retry task into the queue.
   */
  async save(task: RetryTask): Promise<void> {
    await this.db.retryTasks.add(task);
  }

  /**
   * Returns retry tasks with status "pending" and next_retry <= now,
   * ordered by next_retry ascending (soonest first).
   */
  async listPending(limit: number): Promise<RetryTask[]> {
    if (limit <= 0) limit = 50;

    const now = new Date();

    const tasks = await this.db.retryTasks
      .where('status')
      .equals('pending')
      .toArray();

    // Filter by next_retry <= now.
    const due = tasks.filter((t) => new Date(t.next_retry) <= now);

    // Sort by next_retry ascending.
    due.sort(
      (a, b) =>
        new Date(a.next_retry).getTime() - new Date(b.next_retry).getTime(),
    );

    return due.slice(0, limit);
  }

  /**
   * Saves changes to a retry task (attempt count, next retry time,
   * last error, etc.).
   */
  async update(task: RetryTask): Promise<void> {
    await this.db.retryTasks.put(task);
  }

  /**
   * Sets the task's status to "failed" so it no longer appears in
   * listPending results.
   * Throws ErrNotFound if the task does not exist.
   */
  async markFailed(id: string, lastError: string): Promise<void> {
    const updated = await this.db.retryTasks
      .where('id')
      .equals(id)
      .modify((task) => {
        task.status = 'failed';
        task.last_error = lastError;
      });
    if (updated === 0) {
      throw ErrNotFound;
    }
  }

  /**
   * Removes a retry task by its primary key.
   * Throws ErrNotFound if not found.
   */
  async delete(id: string): Promise<void> {
    const existing = await this.db.retryTasks.get(id);
    if (!existing) {
      throw ErrNotFound;
    }
    await this.db.retryTasks.delete(id);
  }

  /**
   * Returns the total number of retry tasks with the given status.
   */
  async count(status: string): Promise<number> {
    return this.db.retryTasks.where('status').equals(status).count();
  }
}
