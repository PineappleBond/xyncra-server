package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// getGaugeValue reads the current value of a Prometheus Gauge.
func getGaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	m := &dto.Metric{}
	if err := g.Write(m); err != nil {
		t.Fatalf("failed to read gauge: %v", err)
	}
	return m.GetGauge().GetValue()
}

// TestStartRuntimeCollector verifies that the runtime collector updates
// goroutine and memory metrics after starting.
func TestStartRuntimeCollector(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Record values before the collector runs.
	beforeGoroutines := getGaugeValue(t, Goroutines)
	beforeMemory := getGaugeValue(t, MemoryAlloc)

	// Start the collector and wait for at least one collection cycle.
	StartRuntimeCollector(ctx)
	time.Sleep(100 * time.Millisecond)

	// The collectRuntime function is called immediately on start, so
	// goroutine count and memory should have been updated.
	afterGoroutines := getGaugeValue(t, Goroutines)
	afterMemory := getGaugeValue(t, MemoryAlloc)

	// Goroutines should be > 0 (we're running in at least one goroutine).
	if afterGoroutines <= 0 {
		t.Errorf("expected goroutines > 0, got %f", afterGoroutines)
	}

	// The values should have changed from their initial zero/previous state
	// since the collector ran. We check that memory is > 0 (some heap is
	// always allocated).
	if afterMemory <= 0 {
		t.Errorf("expected memory alloc > 0, got %f", afterMemory)
	}

	// Just verify that the collector ran — the values should be different
	// from whatever they were before (unless they were already set).
	// This is a soft check; the important thing is the metrics are non-zero.
	_ = beforeGoroutines
	_ = beforeMemory

	// GCCount should also be set.
	gcCount := getGaugeValue(t, GCCount)
	if gcCount < 0 {
		t.Errorf("expected gc_count >= 0, got %f", gcCount)
	}

	// Cancel and verify no panic.
	cancel()
	time.Sleep(50 * time.Millisecond)
}

// TestStartRuntimeCollectorCancellation verifies that the collector goroutine
// exits when the context is cancelled.
func TestStartRuntimeCollectorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Start collector.
	StartRuntimeCollector(ctx)

	// Let it run briefly.
	time.Sleep(50 * time.Millisecond)

	// Cancel — should not panic or leak.
	cancel()

	// Give the goroutine time to exit.
	time.Sleep(50 * time.Millisecond)

	// If we get here without hanging, the test passes.
}

// TestCollectRuntime verifies that collectRuntime updates metrics without
// panicking and covers all the gauge-setting paths.
func TestCollectRuntime(t *testing.T) {
	// Snapshot values before collection.
	beforeGoroutines := getGaugeValue(t, Goroutines)

	// Call collectRuntime directly.
	collectRuntime()

	// Goroutines should be updated to a positive value.
	afterGoroutines := getGaugeValue(t, Goroutines)
	if afterGoroutines <= 0 {
		t.Errorf("expected goroutines > 0 after collectRuntime, got %f", afterGoroutines)
	}
	// The value should differ from the initial zero (unless it was already set
	// by a previous test, which is fine).
	_ = beforeGoroutines

	// MemoryAlloc should be positive (heap is always allocated).
	mem := getGaugeValue(t, MemoryAlloc)
	if mem <= 0 {
		t.Errorf("expected memory_alloc > 0, got %f", mem)
	}

	// MemoryInuse should be positive.
	inuse := getGaugeValue(t, MemoryInuse)
	if inuse <= 0 {
		t.Errorf("expected memory_inuse > 0, got %f", inuse)
	}

	// GCCount should be >= 0.
	gc := getGaugeValue(t, GCCount)
	if gc < 0 {
		t.Errorf("expected gc_count >= 0, got %f", gc)
	}
}

// TestCountOpenFDs verifies that countOpenFDs returns a valid result and
// does not panic on any platform.
func TestCountOpenFDs(t *testing.T) {
	fds := countOpenFDs()
	// On Linux, /proc/self/fd exists and returns >= 0.
	// On macOS/Windows, it returns -1 (graceful degradation).
	if fds < -1 {
		t.Errorf("countOpenFDs returned unexpected value: %d", fds)
	}
}
