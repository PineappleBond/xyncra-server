package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestSemaphore_AcquireRelease — basic acquire and release
// ---------------------------------------------------------------------------

func TestSemaphore_AcquireRelease(t *testing.T) {
	sem := NewSemaphore(1)
	require.NotNil(t, sem)

	ctx := context.Background()
	err := sem.Acquire(ctx)
	require.NoError(t, err)

	stats := sem.Stats()
	assert.Equal(t, 1, stats.Active)
	assert.Equal(t, int64(1), stats.TotalAcquired)

	sem.Release()
	stats = sem.Stats()
	assert.Equal(t, 0, stats.Active)
}

// ---------------------------------------------------------------------------
// TestSemaphore_Unlimited — capacity=0 always succeeds immediately
// ---------------------------------------------------------------------------

func TestSemaphore_Unlimited(t *testing.T) {
	sem := NewSemaphore(0)
	require.NotNil(t, sem)

	ctx := context.Background()

	// Multiple acquires should all succeed immediately.
	for i := 0; i < 100; i++ {
		err := sem.Acquire(ctx)
		require.NoError(t, err)
	}

	stats := sem.Stats()
	assert.Equal(t, 0, stats.Capacity)
	// Unlimited semaphore (capacity=0) does not track acquisitions.
	assert.Equal(t, int64(0), stats.TotalAcquired)

	// Release is a no-op on unlimited.
	sem.Release()
}

// ---------------------------------------------------------------------------
// TestSemaphore_Bounds — exceeding capacity blocks
// ---------------------------------------------------------------------------

func TestSemaphore_Bounds(t *testing.T) {
	sem := NewSemaphore(2)
	ctx := context.Background()

	// Acquire two slots.
	require.NoError(t, sem.Acquire(ctx))
	require.NoError(t, sem.Acquire(ctx))

	stats := sem.Stats()
	assert.Equal(t, 2, stats.Active)
	assert.Equal(t, 2, stats.Capacity)

	// Third acquire should block — use a short timeout.
	ctxTimeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	err := sem.Acquire(ctxTimeout)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Release one slot, then acquire should succeed.
	sem.Release()

	err = sem.Acquire(ctx)
	require.NoError(t, err)
	sem.Release()
	sem.Release()
}

// ---------------------------------------------------------------------------
// TestSemaphore_ContextCancellation — ctx cancelled returns error
// ---------------------------------------------------------------------------

func TestSemaphore_ContextCancellation(t *testing.T) {
	sem := NewSemaphore(1)
	ctx := context.Background()

	// Fill the only slot.
	require.NoError(t, sem.Acquire(ctx))

	// Cancel context before trying to acquire.
	ctxCancel, cancel := context.WithCancel(ctx)
	cancel()

	err := sem.Acquire(ctxCancel)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Cleanup.
	sem.Release()
}

// ---------------------------------------------------------------------------
// TestSemaphore_Stats — verify metrics tracking
// ---------------------------------------------------------------------------

func TestSemaphore_Stats(t *testing.T) {
	sem := NewSemaphore(3)
	ctx := context.Background()

	// Initial state.
	stats := sem.Stats()
	assert.Equal(t, 3, stats.Capacity)
	assert.Equal(t, 0, stats.Active)
	assert.Equal(t, 0, stats.Peak)
	assert.Equal(t, int64(0), stats.TotalAcquired)

	// Acquire one.
	require.NoError(t, sem.Acquire(ctx))
	stats = sem.Stats()
	assert.Equal(t, 1, stats.Active)
	assert.Equal(t, 1, stats.Peak)
	assert.Equal(t, int64(1), stats.TotalAcquired)

	// Acquire two more.
	require.NoError(t, sem.Acquire(ctx))
	require.NoError(t, sem.Acquire(ctx))
	stats = sem.Stats()
	assert.Equal(t, 3, stats.Active)
	assert.Equal(t, 3, stats.Peak)
	assert.Equal(t, int64(3), stats.TotalAcquired)

	// Release all.
	sem.Release()
	sem.Release()
	sem.Release()
	stats = sem.Stats()
	assert.Equal(t, 0, stats.Active)
	assert.Equal(t, 3, stats.Peak) // Peak stays at high-water mark.
	assert.Equal(t, int64(3), stats.TotalAcquired)
}

// ---------------------------------------------------------------------------
// TestSemaphore_NilReceiver — nil safety
// ---------------------------------------------------------------------------

func TestSemaphore_NilReceiver(t *testing.T) {
	var sem *Semaphore

	// Acquire on nil should return nil (no error).
	err := sem.Acquire(context.Background())
	assert.NoError(t, err)

	// Release on nil should not panic.
	assert.NotPanics(t, func() {
		sem.Release()
	})

	// Stats on nil should return zero stats.
	stats := sem.Stats()
	assert.Equal(t, 0, stats.Capacity)
	assert.Equal(t, 0, stats.Active)
	assert.Equal(t, 0, stats.Peak)
	assert.Equal(t, int64(0), stats.TotalAcquired)
}

// ---------------------------------------------------------------------------
// TestSemaphore_Peak — verify peak tracking
// ---------------------------------------------------------------------------

func TestSemaphore_Peak(t *testing.T) {
	sem := NewSemaphore(5)
	ctx := context.Background()

	// Acquire 3 slots.
	for i := 0; i < 3; i++ {
		require.NoError(t, sem.Acquire(ctx))
	}
	assert.Equal(t, 3, sem.Stats().Peak)

	// Release 2 slots.
	sem.Release()
	sem.Release()
	assert.Equal(t, 1, sem.Stats().Active)
	assert.Equal(t, 3, sem.Stats().Peak) // Peak stays.

	// Acquire 3 more (total active = 4, new peak).
	for i := 0; i < 3; i++ {
		require.NoError(t, sem.Acquire(ctx))
	}
	assert.Equal(t, 4, sem.Stats().Active)
	assert.Equal(t, 4, sem.Stats().Peak)

	// Release all.
	for i := 0; i < 4; i++ {
		sem.Release()
	}
	assert.Equal(t, 0, sem.Stats().Active)
	assert.Equal(t, 4, sem.Stats().Peak) // Peak preserved.
}

// ---------------------------------------------------------------------------
// TestSemaphore_Concurrent — multi-goroutine safety
// ---------------------------------------------------------------------------

func TestSemaphore_Concurrent(t *testing.T) {
	const capacity = 5
	const goroutines = 20
	const iterations = 50

	sem := NewSemaphore(capacity)
	ctx := context.Background()

	var active int64
	var maxActive int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				err := sem.Acquire(ctx)
				require.NoError(t, err)

				current := atomic.AddInt64(&active, 1)
				// Update max active (simple CAS loop).
				for {
					old := atomic.LoadInt64(&maxActive)
					if current <= old || atomic.CompareAndSwapInt64(&maxActive, old, current) {
						break
					}
				}

				// Hold briefly to increase contention.
				time.Sleep(time.Microsecond * 10)

				atomic.AddInt64(&active, -1)
				sem.Release()
			}
		}()
	}

	wg.Wait()

	stats := sem.Stats()
	assert.Equal(t, capacity, stats.Capacity)
	assert.Equal(t, 0, stats.Active, "all slots should be released")
	assert.Equal(t, int64(goroutines*iterations), stats.TotalAcquired)
	assert.LessOrEqual(t, maxActive, int64(capacity),
		"concurrent active count should never exceed capacity")
}

// ---------------------------------------------------------------------------
// TestSemaphore_NegativeCapacity — negative treated as unlimited
// ---------------------------------------------------------------------------

func TestSemaphore_NegativeCapacity(t *testing.T) {
	sem := NewSemaphore(-1)
	require.NotNil(t, sem)

	ctx := context.Background()
	for i := 0; i < 50; i++ {
		err := sem.Acquire(ctx)
		require.NoError(t, err)
	}

	stats := sem.Stats()
	assert.Equal(t, 0, stats.Capacity)
	// Unlimited semaphore (capacity < 0) does not track acquisitions.
	assert.Equal(t, int64(0), stats.TotalAcquired)

	// Release is no-op.
	assert.NotPanics(t, func() {
		sem.Release()
	})
}
