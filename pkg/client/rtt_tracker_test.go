package client

import (
	"sync"
	"testing"
	"time"
)

func TestRTTTracker_Cold_Start(t *testing.T) {
	tracker := NewRTTTracker(50)

	// Record fewer than 5 samples.
	for i := 0; i < 4; i++ {
		tracker.Record(200 * time.Millisecond)
	}

	got := tracker.AdaptiveTimeout(30*time.Second, 5*time.Second, 120*time.Second)
	if got != 30*time.Second {
		t.Errorf("AdaptiveTimeout with 4 samples = %v, want 30s (defaultTimeout)", got)
	}

	if srtt := tracker.SRTT(); srtt != 200*time.Millisecond {
		t.Errorf("SRTT with 4 samples = %v, want 200ms", srtt)
	}
}

func TestRTTTracker_5_Samples_Activates(t *testing.T) {
	tracker := NewRTTTracker(50)

	for i := 0; i < 5; i++ {
		tracker.Record(200 * time.Millisecond)
	}

	srtt := tracker.SRTT()
	if diff := srtt - 200*time.Millisecond; absDuration(diff) > 20*time.Millisecond {
		t.Errorf("SRTT with 5x200ms samples = %v, want ~200ms", srtt)
	}

	timeout := tracker.AdaptiveTimeout(30*time.Second, 100*time.Millisecond, 120*time.Second)
	expected := 300 * time.Millisecond // 200ms * 1.5
	if diff := timeout - expected; absDuration(diff) > 30*time.Millisecond {
		t.Errorf("AdaptiveTimeout = %v, want ~300ms", timeout)
	}
}

func TestRTTTracker_SRTT_Calculation(t *testing.T) {
	tracker := NewRTTTracker(50)

	for i := 0; i < 10; i++ {
		tracker.Record(200 * time.Millisecond)
	}

	srtt := tracker.SRTT()
	if diff := srtt - 200*time.Millisecond; absDuration(diff) > 20*time.Millisecond {
		t.Errorf("SRTT with 10x200ms samples = %v, want ~200ms", srtt)
	}
}

func TestRTTTracker_Outlier_Rejection(t *testing.T) {
	tracker := NewRTTTracker(50)

	// 49 samples of 100ms + 1 sample of 10s.
	for i := 0; i < 49; i++ {
		tracker.Record(100 * time.Millisecond)
	}
	tracker.Record(10 * time.Second)

	srtt := tracker.SRTT()
	// With 50 samples, trim = 50/10 = 5. Bottom 5 and top 5 removed.
	// The 10s outlier is in the top 5 and gets trimmed.
	// Remaining 40 samples are all 100ms.
	if diff := srtt - 100*time.Millisecond; absDuration(diff) > 20*time.Millisecond {
		t.Errorf("SRTT with outlier = %v, want ~100ms (outlier should be trimmed)", srtt)
	}
}

func TestRTTTracker_Window_Overflow(t *testing.T) {
	tracker := NewRTTTracker(50)

	// First 10 samples at 999ms (should be overwritten).
	for i := 0; i < 10; i++ {
		tracker.Record(999 * time.Millisecond)
	}
	// Then 50 samples at 200ms — only the last 50 remain in the window.
	for i := 0; i < 50; i++ {
		tracker.Record(200 * time.Millisecond)
	}

	srtt := tracker.SRTT()
	if diff := srtt - 200*time.Millisecond; absDuration(diff) > 20*time.Millisecond {
		t.Errorf("SRTT after window overflow = %v, want ~200ms (old samples should be evicted)", srtt)
	}
}

func TestRTTTracker_AdaptiveTimeout_Clamp_Low(t *testing.T) {
	tracker := NewRTTTracker(50)

	for i := 0; i < 10; i++ {
		tracker.Record(1 * time.Millisecond)
	}

	// SRTT ~1ms, SRTT*1.5 ~1.5ms, clamped up to minTimeout=5s.
	timeout := tracker.AdaptiveTimeout(30*time.Second, 5*time.Second, 120*time.Second)
	if timeout != 5*time.Second {
		t.Errorf("AdaptiveTimeout clamped low = %v, want 5s (minTimeout)", timeout)
	}
}

func TestRTTTracker_AdaptiveTimeout_Clamp_High(t *testing.T) {
	tracker := NewRTTTracker(50)

	for i := 0; i < 10; i++ {
		tracker.Record(100 * time.Second)
	}

	// SRTT ~100s, SRTT*1.5 ~150s, clamped down to maxTimeout=120s.
	timeout := tracker.AdaptiveTimeout(30*time.Second, 5*time.Second, 120*time.Second)
	if timeout != 120*time.Second {
		t.Errorf("AdaptiveTimeout clamped high = %v, want 120s (maxTimeout)", timeout)
	}
}

func TestRTTTracker_Converges_Up(t *testing.T) {
	tracker := NewRTTTracker(50)

	// Fill 50 samples at 50ms.
	for i := 0; i < 50; i++ {
		tracker.Record(50 * time.Millisecond)
	}

	// Use minTimeout=50ms so 75ms (50ms*1.5) is not clamped.
	timeout1 := tracker.AdaptiveTimeout(30*time.Second, 50*time.Millisecond, 120*time.Second)
	expected1 := 75 * time.Millisecond // 50ms * 1.5
	if diff := timeout1 - expected1; absDuration(diff) > 20*time.Millisecond {
		t.Errorf("AdaptiveTimeout step 1 = %v, want ~75ms", timeout1)
	}

	// Now fill 50 samples at 500ms, overwriting the old ones.
	for i := 0; i < 50; i++ {
		tracker.Record(500 * time.Millisecond)
	}

	timeout2 := tracker.AdaptiveTimeout(30*time.Second, 50*time.Millisecond, 120*time.Second)
	expected2 := 750 * time.Millisecond // 500ms * 1.5
	if diff := timeout2 - expected2; absDuration(diff) > 100*time.Millisecond {
		t.Errorf("AdaptiveTimeout after converging up = %v, want ~750ms", timeout2)
	}
}

func TestRTTTracker_Converges_Down(t *testing.T) {
	tracker := NewRTTTracker(50)

	// Fill 50 samples at 5s.
	for i := 0; i < 50; i++ {
		tracker.Record(5 * time.Second)
	}

	timeout1 := tracker.AdaptiveTimeout(30*time.Second, 5*time.Second, 120*time.Second)
	expected1 := 7500 * time.Millisecond // 5s * 1.5
	if diff := timeout1 - expected1; absDuration(diff) > 500*time.Millisecond {
		t.Errorf("AdaptiveTimeout step 1 = %v, want ~7.5s", timeout1)
	}

	// Now fill 50 samples at 50ms, overwriting the old ones.
	for i := 0; i < 50; i++ {
		tracker.Record(50 * time.Millisecond)
	}

	timeout2 := tracker.AdaptiveTimeout(30*time.Second, 5*time.Second, 120*time.Second)
	// 50ms * 1.5 = 75ms, clamped to minTimeout = 5s.
	if timeout2 != 5*time.Second {
		t.Errorf("AdaptiveTimeout after converging down = %v, want 5s (clamped from 75ms)", timeout2)
	}
}

func TestRTTTracker_Concurrent(t *testing.T) {
	tracker := NewRTTTracker(50)

	const goroutines = 10
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				tracker.Record(time.Duration(i+1) * time.Millisecond)
				tracker.SRTT()
				tracker.AdaptiveTimeout(30*time.Second, 5*time.Second, 120*time.Second)
			}
		}()
	}
	wg.Wait()

	// Just verify no panic/race occurred and SRTT is positive.
	if srtt := tracker.SRTT(); srtt <= 0 {
		t.Errorf("SRTT after concurrent writes = %v, want > 0", srtt)
	}
}

// absDuration returns the absolute value of a time.Duration.
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
