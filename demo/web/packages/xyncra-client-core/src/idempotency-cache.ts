/**
 * @packageDocumentation
 * LRU cache for inbound RPC request deduplication.
 *
 * Mirrors Go `pkg/client/idempotency_cache.go`.
 *
 * Uses a `Map<string, true>` which preserves insertion order in JavaScript,
 * giving O(1) contains / put / delete with automatic LRU eviction when the
 * cache exceeds capacity.
 *
 * Constraint C13: idempotency dedup check happens before handler invocation.
 */

export class IdempotencyCache {
  private capacity: number;
  private items: Map<string, true>;

  constructor(capacity: number) {
    this.capacity = capacity;
    this.items = new Map();
  }

  /**
   * Check whether `key` exists in the cache.
   * If found, the key is promoted to most-recently-used (MRU) position,
   * matching Go's `MoveToFront` semantics.
   */
  contains(key: string): boolean {
    if (!this.items.has(key)) {
      return false;
    }
    // Promote to MRU: delete and re-insert at the end
    this.items.delete(key);
    this.items.set(key, true);
    return true;
  }

  /**
   * Insert a key into the cache.
   * If the key already exists, it is promoted to MRU.
   * If the cache is at capacity, the least-recently-used (oldest) key is evicted.
   */
  put(key: string): void {
    // If key already exists, delete first so re-insert places it at the end (MRU)
    this.items.delete(key);

    // Evict the oldest entry (first key in Map iteration order) if at capacity
    if (this.items.size >= this.capacity) {
      const oldestKey = this.items.keys().next().value;
      if (oldestKey !== undefined) {
        this.items.delete(oldestKey);
      }
    }

    this.items.set(key, true);
  }

  /** Return the current number of entries in the cache. */
  len(): number {
    return this.items.size;
  }

  /** Remove all entries from the cache. */
  clear(): void {
    this.items.clear();
  }
}
