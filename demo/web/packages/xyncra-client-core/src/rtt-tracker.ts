/**
 * @packageDocumentation
 * Round-trip time tracker with adaptive timeout computation.
 *
 * Mirrors Go `pkg/client/rtt_tracker.go`.
 *
 * Maintains a circular buffer of RTT samples and computes a trimmed mean SRTT
 * (Smoothed Round-Trip Time). The adaptive timeout is derived as SRTT * 1.5,
 * clamped to a configurable [min, max] range.
 *
 * Constraint C9:  Trimmed mean — drop top/bottom 10%, require >= 5 samples.
 * Constraint C10: AdaptiveTimeout = SRTT * 1.5, clamped [minTimeout, maxTimeout].
 */

export class RTTTracker {
  private capacity: number;
  private samples: number[]; // circular buffer
  private index: number; // next write position
  private count: number; // actual sample count (<= capacity)

  constructor(capacity: number) {
    this.capacity = capacity;
    this.samples = new Array<number>(capacity).fill(0);
    this.index = 0;
    this.count = 0;
  }

  /** Record a single RTT sample (in milliseconds). */
  record(rtt: number): void {
    this.samples[this.index] = rtt;
    this.index = (this.index + 1) % this.capacity;
    if (this.count < this.capacity) {
      this.count++;
    }
  }

  /**
   * Compute the trimmed mean SRTT.
   *
   * Algorithm (matches Go `srttLocked`):
   * 1. Collect valid samples into a sortable array.
   * 2. Sort ascending.
   * 3. Drop top 10% and bottom 10% (floor division).
   * 4. Average the remaining samples.
   *
   * Returns `null` when fewer than 5 samples exist (cold-start).
   */
  srtt(): number | null {
    if (this.count < 5) {
      return null;
    }

    // Copy valid samples in chronological order
    const valid = this.collectSamples();
    valid.sort((a, b) => a - b);

    // Trimmed mean: remove top/bottom 10% (floor division)
    const trim = Math.floor(this.count / 10);
    let start = 0;
    let end = valid.length;
    if (trim > 0 && this.count - 2 * trim >= 2) {
      start = trim;
      end = this.count - trim;
    }

    let sum = 0;
    for (let i = start; i < end; i++) {
      sum += valid[i];
    }
    return sum / (end - start);
  }

  /**
   * Compute an adaptive timeout based on current SRTT.
   *
   * Returns `defaultTimeout` during cold start (< 5 samples).
   * Otherwise: clamp(SRTT * 1.5, minTimeout, maxTimeout).
   */
  adaptiveTimeout(
    defaultTimeout: number,
    minTimeout: number,
    maxTimeout: number,
  ): number {
    const srttValue = this.srtt();
    if (srttValue === null) {
      return defaultTimeout;
    }
    const timeout = srttValue * 1.5;
    return Math.max(minTimeout, Math.min(maxTimeout, timeout));
  }

  /** Reset all samples. */
  reset(): void {
    this.index = 0;
    this.count = 0;
    this.samples.fill(0);
  }

  /** Return the current number of recorded samples. */
  sampleCount(): number {
    return this.count;
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  /**
   * Collect valid samples from the circular buffer in chronological order.
   * When the buffer has wrapped, samples span [head..end, 0..head-1].
   */
  private collectSamples(): number[] {
    if (this.count === this.capacity) {
      // Buffer is full: oldest sample is at `this.index` (the next write pos)
      const result: number[] = new Array(this.count);
      for (let i = 0; i < this.count; i++) {
        result[i] = this.samples[(this.index + i) % this.capacity];
      }
      return result;
    }
    // Buffer not yet full: samples are in [0..count-1]
    return this.samples.slice(0, this.count);
  }
}
