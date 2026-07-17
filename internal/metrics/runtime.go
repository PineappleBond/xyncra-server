package metrics

import (
	"context"
	"os"
	"runtime"
	"time"
)

// StartRuntimeCollector starts a goroutine that updates system metrics
// (goroutines, memory, GC, open FDs) every 10 seconds. The goroutine exits
// when ctx is cancelled.
func StartRuntimeCollector(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		// Collect once immediately so metrics are available before the first tick.
		collectRuntime()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				collectRuntime()
			}
		}
	}()
}

// collectRuntime reads runtime statistics and updates the corresponding
// Prometheus gauges and summaries.
func collectRuntime() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	Goroutines.Set(float64(runtime.NumGoroutine()))
	MemoryAlloc.Set(float64(m.Alloc))
	MemoryInuse.Set(float64(m.HeapInuse))
	GCCount.Set(float64(m.NumGC))

	// Record the most recent GC pause as a summary observation.
	// PauseTotalNs is in nanoseconds; convert to seconds.
	// Use per-pause average as a single observation per tick.
	if m.NumGC > 0 {
		GCDuration.Observe(float64(m.PauseTotalNs) / float64(m.NumGC) / 1e9)
	}

	// Open file descriptors: platform-specific.
	if fds := countOpenFDs(); fds >= 0 {
		OpenFDs.Set(float64(fds))
	}
}

// countOpenFDs returns the number of open file descriptors for the current
// process. Returns -1 if the count cannot be determined (e.g. non-Linux
// platform without /proc/self/fd).
func countOpenFDs() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}
