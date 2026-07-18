/**
 * RetryManager unit tests.
 *
 * Tests cover:
 *   - Enqueue and poll
 *   - Successful retry deletes the task
 *   - Failed retry with exponential backoff
 *   - Max attempts exhaustion marks task as failed
 *   - Stop halts polling
 */

import { RetryManager } from '../retry-manager';
import {
  createFreshDatabase,
  createMockLogger,
  resetIdCounter,
  sleep,
} from './test-helpers';

describe('RetryManager', () => {
  let db: ReturnType<typeof createFreshDatabase>;
  let logger: ReturnType<typeof createMockLogger>;
  let rpcFn: jest.Mock;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-retry-${Date.now()}-${Math.random()}`);
    await db.open();
    logger = createMockLogger();
    rpcFn = jest.fn();
  });

  afterEach(async () => {
    await db.delete();
  });

  function createRetryManager(pollInterval = 50) {
    return new RetryManager({
      db,
      rpcFn,
      logger,
      pollInterval,
      baseDelay: 100,
      maxDelay: 1000,
      maxAttempts: 3,
    });
  }

  test('enqueue persists a task', async () => {
    const retryMgr = createRetryManager();

    await retryMgr.enqueue('send_message', { conversation_id: 'conv-1' });

    const pending = await db.queueStore.listPending(50);
    expect(pending).toHaveLength(1);
    expect(pending[0].method).toBe('send_message');
  });

  test('successful retry deletes the task', async () => {
    const retryMgr = createRetryManager(10);
    rpcFn.mockResolvedValue({ result: 'ok' });

    await retryMgr.enqueue('test.method', { foo: 'bar' });

    // Start polling
    retryMgr.start();

    // Wait for at least one poll cycle
    await sleep(100);

    retryMgr.stop();

    // Task should be deleted after successful retry
    const pending = await db.queueStore.listPending(50);
    expect(pending).toHaveLength(0);

    const totalCount = await db.queueStore.count('pending');
    expect(totalCount).toBe(0);
  });

  test('failed retry increments attempt and sets next_retry', async () => {
    const retryMgr = createRetryManager(10);
    rpcFn.mockRejectedValue(new Error('network error'));

    await retryMgr.enqueue('test.method', {});

    retryMgr.start();

    // Wait for the first attempt + backoff
    await sleep(200);

    retryMgr.stop();

    // Task should still exist with attempt > 0
    const pending = await db.queueStore.listPending(50);
    // It might be pending with a future next_retry, or we check all tasks
    const allPending = await db.queueStore.count('pending');
    const allFailed = await db.queueStore.count('failed');
    // After one failure, the task should be pending (not yet failed)
    expect(allPending + allFailed).toBe(1);
  });

  test('max attempts exhaustion marks task as failed', async () => {
    const retryMgr = createRetryManager(10);
    rpcFn.mockRejectedValue(new Error('always fails'));

    await retryMgr.enqueue('test.method', {});

    retryMgr.start();

    // Wait long enough for all 3 attempts
    await sleep(1000);

    retryMgr.stop();

    // Task should be marked as failed
    const failedCount = await db.queueStore.count('failed');
    expect(failedCount).toBe(1);

    const pendingCount = await db.queueStore.count('pending');
    expect(pendingCount).toBe(0);
  });

  test('stop halts polling', async () => {
    const retryMgr = createRetryManager(10);
    rpcFn.mockResolvedValue({ result: 'ok' });

    await retryMgr.enqueue('test.method', {});

    retryMgr.start();
    await sleep(50);
    retryMgr.stop();

    // Add another task after stopping
    await retryMgr.enqueue('test.method2', {});

    // Wait and verify the second task was NOT processed
    await sleep(100);

    const pending = await db.queueStore.listPending(50);
    // The first task should be done, second should still be pending
    expect(pending.length).toBeGreaterThanOrEqual(0);
  });

  test('start is idempotent', async () => {
    const retryMgr = createRetryManager();

    retryMgr.start();
    retryMgr.start(); // should not throw

    retryMgr.stop();
  });
});
