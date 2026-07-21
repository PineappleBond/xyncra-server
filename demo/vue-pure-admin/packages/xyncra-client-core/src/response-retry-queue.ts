/**
 * @packageDocumentation
 * FIFO queue with exponential backoff for failed response sends.
 *
 * Mirrors Go `pkg/client/response_retry_queue.go`.
 *
 * When the client fails to send a response back to the server (e.g. connection
 * lost), the response is enqueued here. A retry loop periodically drains ready
 * entries (those whose backoff has elapsed) and attempts to re-send. Failed
 * entries are re-enqueued with exponential backoff.
 *
 * Constraint C3: Backoff = base * 2^(attempt-1), capped at max, exponent
 * clamped to 30, with +/-25% random jitter.
 */

import type { PackageDataResponse } from '@xyncra/protocol';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/** A single entry in the response retry queue. */
export interface ResponseRetryEntry {
  /** The protocol response to retry sending. */
  response: PackageDataResponse;
  /** Absolute timestamp (ms, `Date.now()` epoch) after which the entry is ready. */
  nextRetryAt: number;
  /** How many send attempts have been made so far (starts at 1). */
  attempt: number;
}

// ---------------------------------------------------------------------------
// ResponseRetryQueue
// ---------------------------------------------------------------------------

export class ResponseRetryQueue {
  private entries: ResponseRetryEntry[];
  private baseDelay: number;
  private maxDelay: number;
  private maxSize: number;

  /**
   * @param baseDelay  Initial backoff delay in milliseconds.
   * @param maxDelay   Maximum backoff cap in milliseconds.
   * @param maxSize    Maximum queue capacity (oldest discarded on overflow).
   *                   Defaults to 100 (mirrors Go `DefaultResponseRetryMaxSize`).
   */
  constructor(baseDelay: number, maxDelay: number, maxSize: number = 100) {
    this.entries = [];
    this.baseDelay = baseDelay;
    this.maxDelay = maxDelay;
    this.maxSize = maxSize;
  }

  /**
   * Add a response to the queue for the first time.
   * Sets attempt=1 and nextRetryAt = now + baseDelay.
   */
  enqueue(response: PackageDataResponse): void {
    this.evictIfNeeded();
    this.entries.push({
      response,
      nextRetryAt: Date.now() + this.baseDelay,
      attempt: 1,
    });
  }

  /**
   * Re-enqueue a previously drained entry after a failed retry.
   * Increments the attempt counter and computes a new nextRetryAt using
   * exponential backoff with jitter (constraint C3).
   *
   * If the response ID is not found in the internal queue (e.g. it was evicted),
   * a fresh entry is created.
   */
  enqueueWithBackoff(entry: ResponseRetryEntry, now: number): void {
    // Try to find the existing internal entry to preserve attempt count
    const existing = this.entries.find(
      (e) => e.response.id === entry.response.id,
    );
    const attempt = existing !== undefined ? existing.attempt + 1 : 1;

    // Remove stale internal tracking entry if present
    if (existing !== undefined) {
      this.entries = this.entries.filter(
        (e) => e.response.id !== entry.response.id,
      );
    }

    this.evictIfNeeded();
    this.entries.push({
      response: entry.response,
      nextRetryAt: this.calculateBackoff(attempt, now),
      attempt,
    });
  }

  /**
   * Remove and return all entries whose nextRetryAt <= now.
   * Entries that have exceeded maxRetry are silently discarded.
   * Remaining (not-yet-ready) entries stay in the queue.
   *
   * Returns responses in FIFO order.
   *
   * @param now      Current timestamp in milliseconds.
   * @param maxRetry Maximum number of attempts before discarding (default 3).
   */
  drain(now: number, maxRetry: number = 3): PackageDataResponse[] {
    const ready: PackageDataResponse[] = [];
    const remaining: ResponseRetryEntry[] = [];

    for (const entry of this.entries) {
      if (entry.nextRetryAt > now) {
        remaining.push(entry);
        continue;
      }
      // Entry is ready — check if it has exceeded its retry budget
      if (entry.attempt > maxRetry) {
        // Discard: too many attempts (logged by caller if needed)
        continue;
      }
      ready.push(entry.response);
    }

    this.entries = remaining;
    return ready;
  }

  /** Return the current number of entries in the queue. */
  len(): number {
    return this.entries.length;
  }

  /** Remove all entries from the queue. */
  clear(): void {
    this.entries = [];
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  /**
   * Compute the next retry timestamp using exponential backoff with jitter.
   *
   * Formula (constraint C3):
   *   delay = baseDelay * 2^(attempt-1)
   *   delay = min(delay, maxDelay)
   *   exponent capped at 30 to prevent overflow
   *   jitter = +/- 25% of delay
   *   result = now + delay + jitter
   */
  private calculateBackoff(attempt: number, now: number): number {
    let exp = attempt - 1;
    if (exp > 30) exp = 30; // exponent cap (C3)

    let delay = this.baseDelay * 2 ** exp;
    if (delay > this.maxDelay) {
      delay = this.maxDelay;
    }

    // +/- 25% random jitter
    const jitterRange = delay * 0.5; // total range = 50% of delay
    const jitter = Math.random() * jitterRange - delay * 0.25;

    return now + delay + jitter;
  }

  /**
   * If the queue is at capacity, discard the oldest entry (FIFO front).
   * Mirrors Go behaviour: log warning and drop head.
   */
  private evictIfNeeded(): void {
    if (this.entries.length >= this.maxSize) {
      this.entries.shift(); // discard oldest
    }
  }
}
