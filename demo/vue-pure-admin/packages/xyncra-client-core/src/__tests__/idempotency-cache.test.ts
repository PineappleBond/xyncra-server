/**
 * IdempotencyCache unit tests.
 *
 * Tests cover:
 *   - LRU eviction
 *   - contains promotes to MRU
 *   - put on existing key promotes to MRU
 *   - clear and len
 */

import { IdempotencyCache } from '../idempotency-cache';

describe('IdempotencyCache', () => {
  test('basic put and contains', () => {
    const cache = new IdempotencyCache(10);
    cache.put('a');
    cache.put('b');

    expect(cache.contains('a')).toBe(true);
    expect(cache.contains('b')).toBe(true);
    expect(cache.contains('c')).toBe(false);
    expect(cache.len()).toBe(2);
  });

  test('LRU eviction when capacity exceeded', () => {
    const cache = new IdempotencyCache(3);

    cache.put('a');
    cache.put('b');
    cache.put('c');

    expect(cache.len()).toBe(3);

    // Adding 'd' evicts 'a' (least recently used)
    cache.put('d');

    expect(cache.contains('a')).toBe(false); // 'a' evicted
    expect(cache.contains('b')).toBe(true);
    expect(cache.contains('d')).toBe(true);
    expect(cache.len()).toBe(3);
  });

  test('contains promotes to MRU', () => {
    const cache = new IdempotencyCache(3);

    cache.put('a');
    cache.put('b');
    cache.put('c');

    // Access 'a' — promotes it to MRU
    cache.contains('a');

    // Adding 'd' should evict 'b' (now the LRU)
    cache.put('d');

    expect(cache.contains('a')).toBe(true); // promoted, still here
    expect(cache.contains('b')).toBe(false); // evicted
    expect(cache.contains('c')).toBe(true);
    expect(cache.contains('d')).toBe(true);
  });

  test('put on existing key promotes to MRU', () => {
    const cache = new IdempotencyCache(3);

    cache.put('a');
    cache.put('b');
    cache.put('c');

    // Re-put 'a' — promotes to MRU
    cache.put('a');

    // Adding 'd' should evict 'b' (now the LRU)
    cache.put('d');

    expect(cache.contains('a')).toBe(true);
    expect(cache.contains('b')).toBe(false); // evicted
    expect(cache.len()).toBe(3);
  });

  test('clear empties the cache', () => {
    const cache = new IdempotencyCache(10);
    cache.put('a');
    cache.put('b');
    cache.put('c');

    cache.clear();

    expect(cache.len()).toBe(0);
    expect(cache.contains('a')).toBe(false);
  });

  test('capacity of 1 always keeps only the latest', () => {
    const cache = new IdempotencyCache(1);

    cache.put('a');
    expect(cache.contains('a')).toBe(true);

    cache.put('b');
    expect(cache.contains('a')).toBe(false);
    expect(cache.contains('b')).toBe(true);
    expect(cache.len()).toBe(1);
  });

  test('contains on missing key does not affect eviction order', () => {
    const cache = new IdempotencyCache(3);

    cache.put('a');
    cache.put('b');
    cache.put('c');

    // contains on a missing key — should not affect anything
    expect(cache.contains('z')).toBe(false);

    // 'a' should still be the LRU
    cache.put('d');
    expect(cache.contains('a')).toBe(false); // 'a' evicted
    expect(cache.contains('b')).toBe(true);
  });
});
