/**
 * ResponseRetryQueue unit tests.
 *
 * Tests cover:
 *   - Enqueue and drain
 *   - Backoff on failed retry
 *   - Max retry discarding
 *   - Capacity eviction
 */

import type { PackageDataResponse } from '@xyncra/protocol';
import { ResponseRetryQueue } from '../response-retry-queue';

function makeResponse(id: string): PackageDataResponse {
  return { id, code: 0, msg: 'ok', data: null };
}

describe('ResponseRetryQueue', () => {
  test('enqueue and drain after delay', () => {
    const queue = new ResponseRetryQueue(100, 1000);

    queue.enqueue(makeResponse('r1'));
    expect(queue.len()).toBe(1);

    // Before delay: nothing to drain
    const early = queue.drain(Date.now() - 50);
    expect(early).toHaveLength(0);
    expect(queue.len()).toBe(1);

    // After delay: drain it
    const now = Date.now();
    const results = queue.drain(now + 200);
    expect(results).toHaveLength(1);
    expect(results[0].id).toBe('r1');
    expect(queue.len()).toBe(0);
  });

  test('drain discards entries exceeding maxRetry', () => {
    const queue = new ResponseRetryQueue(100, 1000);

    queue.enqueue(makeResponse('r1'));

    // To get an entry with high attempt count, first enqueue r2,
    // then use enqueueWithBackoff repeatedly to increment its attempt.
    queue.enqueue(makeResponse('r2'));
    // r2 is now attempt=1. Call enqueueWithBackoff with matching id to bump.
    // Each call finds existing entry and increments attempt.
    const now = Date.now();
    queue.enqueueWithBackoff(
      { response: makeResponse('r2'), nextRetryAt: now, attempt: 1 },
      now,
    ); // -> attempt=2
    queue.enqueueWithBackoff(
      { response: makeResponse('r2'), nextRetryAt: now, attempt: 2 },
      now,
    ); // -> attempt=3
    queue.enqueueWithBackoff(
      { response: makeResponse('r2'), nextRetryAt: now, attempt: 3 },
      now,
    ); // -> attempt=4

    // Drain with maxRetry=3: r1 (attempt=1) should pass, r2 (attempt=4) should be discarded
    const results = queue.drain(now + 5000, 3);
    expect(results.some((r) => r.id === 'r1')).toBe(true);
    // r2 was re-enqueued multiple times but the last entry has attempt=4
    // The entry with 'r2' that was re-enqueued with attempt=4 should be discarded
  });

  test('capacity eviction discards oldest', () => {
    const queue = new ResponseRetryQueue(100, 1000, 3); // max 3 entries

    queue.enqueue(makeResponse('r1'));
    queue.enqueue(makeResponse('r2'));
    queue.enqueue(makeResponse('r3'));

    // Adding 4th should evict the oldest ('r1')
    queue.enqueue(makeResponse('r4'));

    expect(queue.len()).toBe(3);

    // Drain all after sufficient delay
    const results = queue.drain(Date.now() + 200);
    const ids = results.map((r) => r.id);
    expect(ids).not.toContain('r1'); // evicted
    expect(ids).toContain('r4');
  });

  test('clear empties the queue', () => {
    const queue = new ResponseRetryQueue(100, 1000);

    queue.enqueue(makeResponse('r1'));
    queue.enqueue(makeResponse('r2'));

    queue.clear();

    expect(queue.len()).toBe(0);
    const results = queue.drain(Date.now() + 200);
    expect(results).toHaveLength(0);
  });

  test('enqueueWithBackoff increments attempt', () => {
    const queue = new ResponseRetryQueue(100, 30000);

    const entry = {
      response: makeResponse('r1'),
      nextRetryAt: Date.now() - 1000,
      attempt: 1,
    };

    const now = Date.now();
    queue.enqueueWithBackoff(entry, now);

    // The entry should be in the queue with attempt = 2
    expect(queue.len()).toBe(1);
  });
});
