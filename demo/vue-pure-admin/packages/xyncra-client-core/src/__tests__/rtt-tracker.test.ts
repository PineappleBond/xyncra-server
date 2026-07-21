/**
 * RTTTracker unit tests.
 *
 * Tests cover:
 *   - C9: Trimmed mean requires >= 5 samples
 *   - C9: Trimmed mean drops top/bottom 10%
 *   - C10: AdaptiveTimeout = SRTT * 1.5, clamp [min, max]
 *   - Circular buffer wraparound
 *   - reset
 */

import { RTTTracker } from '../rtt-tracker';

describe('RTTTracker', () => {
  // ---------------------------------------------------------------------------
  // C9: Trimmed mean
  // ---------------------------------------------------------------------------

  test('C9: srtt returns null with fewer than 5 samples', () => {
    const tracker = new RTTTracker(10);

    tracker.record(100);
    tracker.record(200);
    tracker.record(300);
    tracker.record(400);

    expect(tracker.srtt()).toBeNull();
  });

  test('C9: srtt returns a value with exactly 5 samples', () => {
    const tracker = new RTTTracker(10);

    tracker.record(100);
    tracker.record(200);
    tracker.record(300);
    tracker.record(400);
    tracker.record(500);

    expect(tracker.srtt()).not.toBeNull();
  });

  test('C9: trimmed mean drops top/bottom 10%', () => {
    const tracker = new RTTTracker(10);

    // 10 samples: [100, 200, 300, 400, 500, 600, 700, 800, 900, 1000]
    for (let i = 1; i <= 10; i++) {
      tracker.record(i * 100);
    }

    // trim = floor(10/10) = 1
    // Drop bottom 1 (100) and top 1 (1000)
    // Remaining: [200, 300, 400, 500, 600, 700, 800, 900]
    // Mean: (200+300+400+500+600+700+800+900)/8 = 550
    expect(tracker.srtt()).toBeCloseTo(550, 0);
  });

  test('C9: with exactly 5 samples, trim = 0', () => {
    const tracker = new RTTTracker(10);

    tracker.record(100);
    tracker.record(200);
    tracker.record(300);
    tracker.record(400);
    tracker.record(500);

    // trim = floor(5/10) = 0
    // All samples used: mean = (100+200+300+400+500)/5 = 300
    expect(tracker.srtt()).toBeCloseTo(300, 0);
  });

  // ---------------------------------------------------------------------------
  // C10: AdaptiveTimeout
  // ---------------------------------------------------------------------------

  test('C10: adaptiveTimeout returns defaultTimeout during cold start', () => {
    const tracker = new RTTTracker(10);
    tracker.record(100); // only 1 sample

    const timeout = tracker.adaptiveTimeout(5000, 5000, 120000);
    expect(timeout).toBe(5000); // defaultTimeout
  });

  test('C10: adaptiveTimeout = SRTT * 1.5', () => {
    const tracker = new RTTTracker(10);

    // All samples = 1000, so SRTT = 1000
    for (let i = 0; i < 10; i++) {
      tracker.record(1000);
    }

    const timeout = tracker.adaptiveTimeout(5000, 5000, 120000);
    // 1000 * 1.5 = 1500, clamped to min 5000
    expect(timeout).toBe(5000); // clamped to min
  });

  test('C10: adaptiveTimeout clamps to min', () => {
    const tracker = new RTTTracker(10);

    // SRTT = 100, timeout = 150 -> clamped to min 5000
    for (let i = 0; i < 10; i++) {
      tracker.record(100);
    }

    const timeout = tracker.adaptiveTimeout(5000, 5000, 120000);
    expect(timeout).toBe(5000);
  });

  test('C10: adaptiveTimeout clamps to max', () => {
    const tracker = new RTTTracker(10);

    // SRTT = 100000, timeout = 150000 -> clamped to max 120000
    for (let i = 0; i < 10; i++) {
      tracker.record(100000);
    }

    const timeout = tracker.adaptiveTimeout(5000, 5000, 120000);
    expect(timeout).toBe(120000);
  });

  test('C10: adaptiveTimeout within range returns computed value', () => {
    const tracker = new RTTTracker(50);

    // SRTT = 10000, timeout = 15000
    for (let i = 0; i < 50; i++) {
      tracker.record(10000);
    }

    const timeout = tracker.adaptiveTimeout(5000, 5000, 120000);
    expect(timeout).toBeCloseTo(15000, 0);
  });

  // ---------------------------------------------------------------------------
  // Circular buffer
  // ---------------------------------------------------------------------------

  test('circular buffer wraps correctly', () => {
    const tracker = new RTTTracker(5);

    // Fill buffer twice
    for (let i = 1; i <= 10; i++) {
      tracker.record(i * 100);
    }

    // Buffer should contain [600, 700, 800, 900, 1000]
    expect(tracker.sampleCount()).toBe(5);

    const srtt = tracker.srtt();
    expect(srtt).not.toBeNull();
    // trim = floor(5/10) = 0, so all 5 samples: mean = (600+700+800+900+1000)/5 = 800
    expect(srtt).toBeCloseTo(800, 0);
  });

  // ---------------------------------------------------------------------------
  // Reset
  // ---------------------------------------------------------------------------

  test('reset clears all samples', () => {
    const tracker = new RTTTracker(10);

    for (let i = 0; i < 10; i++) {
      tracker.record(100);
    }

    expect(tracker.sampleCount()).toBe(10);
    expect(tracker.srtt()).not.toBeNull();

    tracker.reset();

    expect(tracker.sampleCount()).toBe(0);
    expect(tracker.srtt()).toBeNull();
  });
});
