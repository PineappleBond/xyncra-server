package client

import (
	"sort"
	"sync"
	"time"
)

// RTTTracker maintains a sliding window of RTT samples and computes an
// adaptive timeout based on the smoothed RTT plus a safety margin.
// Thread-safe via sync.Mutex.
type RTTTracker struct {
	mu         sync.Mutex
	samples    []time.Duration // circular buffer
	head       int             // next write position
	count      int             // valid sample count (<= len(samples))
	windowSize int
}

// NewRTTTracker creates a new RTT tracker with the given window size.
func NewRTTTracker(windowSize int) *RTTTracker {
	return &RTTTracker{
		samples:    make([]time.Duration, windowSize),
		windowSize: windowSize,
	}
}

// Record adds a new RTT sample to the tracker.
func (t *RTTTracker) Record(rtt time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.samples[t.head] = rtt
	t.head = (t.head + 1) % t.windowSize
	if t.count < t.windowSize {
		t.count++
	}
}

// AdaptiveTimeout computes the adaptive timeout value.
// Uses SRTT * 1.5, clamped to [minTimeout, maxTimeout].
// Returns defaultTimeout when fewer than 5 samples (cold start).
func (t *RTTTracker) AdaptiveTimeout(defaultTimeout, minTimeout, maxTimeout time.Duration) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.count < 5 {
		return defaultTimeout
	}

	srtt := t.srttLocked()
	timeout := srtt * 3 / 2
	return clampDuration(timeout, minTimeout, maxTimeout)
}

// SRTT returns the smoothed RTT (trimmed mean).
// Returns 0 when no samples available.
func (t *RTTTracker) SRTT() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.srttLocked()
}

// srttLocked computes the trimmed mean of valid samples.
// Caller must hold t.mu.
func (t *RTTTracker) srttLocked() time.Duration {
	if t.count == 0 {
		return 0
	}

	// Copy valid samples into a sortable slice.
	valid := make([]time.Duration, t.count)
	if t.count == t.windowSize {
		// Buffer is full: samples are stored from head..end then 0..head-1.
		copy(valid, t.samples[t.head:])
		copy(valid[t.windowSize-t.head:], t.samples[:t.head])
	} else {
		copy(valid, t.samples[:t.count])
	}

	sort.Slice(valid, func(i, j int) bool { return valid[i] < valid[j] })

	// Trimmed mean: remove top/bottom 10% (floor).
	trim := t.count / 10
	if trim > 0 && t.count-2*trim >= 2 {
		valid = valid[trim : t.count-trim]
	}

	var sum time.Duration
	for _, v := range valid {
		sum += v
	}
	return sum / time.Duration(len(valid))
}

// clampDuration clamps dur to [min, max].
func clampDuration(dur, min, max time.Duration) time.Duration {
	if dur < min {
		return min
	}
	if dur > max {
		return max
	}
	return dur
}
