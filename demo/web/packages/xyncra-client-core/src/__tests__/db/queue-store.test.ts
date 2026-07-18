/**
 * QueueStore unit tests.
 */

import { ErrNotFound } from '../../errors';
import {
  createFreshDatabase,
  createRetryTask,
  resetIdCounter,
} from '../test-helpers';

describe('QueueStore', () => {
  let db: ReturnType<typeof createFreshDatabase>;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-queue-${Date.now()}-${Math.random()}`);
    await db.open();
  });

  afterEach(async () => {
    await db.delete();
  });

  test('save and listPending', async () => {
    const task = createRetryTask({ next_retry: new Date(Date.now() - 1000) });
    await db.queueStore.save(task);

    const pending = await db.queueStore.listPending(50);
    expect(pending).toHaveLength(1);
    expect(pending[0].method).toBe('test.method');
  });

  test('listPending excludes future-due tasks', async () => {
    const task = createRetryTask({ next_retry: new Date(Date.now() + 60000) });
    await db.queueStore.save(task);

    const pending = await db.queueStore.listPending(50);
    expect(pending).toHaveLength(0);
  });

  test('listPending sorts by next_retry ascending', async () => {
    const task1 = createRetryTask({ next_retry: new Date(Date.now() - 3000) });
    const task2 = createRetryTask({ next_retry: new Date(Date.now() - 1000) });
    await db.queueStore.save(task1);
    await db.queueStore.save(task2);

    const pending = await db.queueStore.listPending(50);
    expect(pending).toHaveLength(2);
    // Earliest first
    expect(pending[0].id).toBe(task1.id);
  });

  test('update saves changes', async () => {
    const task = createRetryTask();
    await db.queueStore.save(task);

    task.attempt = 3;
    task.last_error = 'timeout';
    await db.queueStore.update(task);

    const pending = await db.queueStore.listPending(50);
    expect(pending[0].attempt).toBe(3);
    expect(pending[0].last_error).toBe('timeout');
  });

  test('markFailed sets status to failed', async () => {
    const task = createRetryTask();
    await db.queueStore.save(task);

    await db.queueStore.markFailed(task.id, 'max attempts exceeded');

    const count = await db.queueStore.count('pending');
    expect(count).toBe(0);
    const failedCount = await db.queueStore.count('failed');
    expect(failedCount).toBe(1);
  });

  test('markFailed throws ErrNotFound for missing task', async () => {
    await expect(db.queueStore.markFailed('nope', 'err')).rejects.toBe(
      ErrNotFound,
    );
  });

  test('delete removes the task', async () => {
    const task = createRetryTask();
    await db.queueStore.save(task);

    await db.queueStore.delete(task.id);

    const pending = await db.queueStore.listPending(50);
    expect(pending).toHaveLength(0);
  });

  test('delete throws ErrNotFound for missing task', async () => {
    await expect(db.queueStore.delete('nope')).rejects.toBe(ErrNotFound);
  });

  test('count returns correct counts by status', async () => {
    const task1 = createRetryTask({ status: 'pending' });
    const task2 = createRetryTask({ status: 'pending' });
    const task3 = createRetryTask({ status: 'failed' });
    await db.queueStore.save(task1);
    await db.queueStore.save(task2);
    await db.queueStore.save(task3);

    expect(await db.queueStore.count('pending')).toBe(2);
    expect(await db.queueStore.count('failed')).toBe(1);
    expect(await db.queueStore.count('completed')).toBe(0);
  });
});
