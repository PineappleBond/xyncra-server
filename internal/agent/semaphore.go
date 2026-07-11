package agent

import (
	"context"
	"sync"
	"sync/atomic"
)

// Semaphore limits concurrent agent executions with metrics tracking.
type Semaphore struct {
	ch       chan struct{}
	capacity int
	mu       sync.Mutex
	active   int
	peak     int
	totalAcq int64 // accessed atomically
}

// NewSemaphore creates a Semaphore with the given capacity.
// If capacity <= 0, Acquire always succeeds immediately (unlimited).
func NewSemaphore(capacity int) *Semaphore {
	if capacity <= 0 {
		return &Semaphore{capacity: 0} // ch stays nil, Acquire returns immediately
	}
	return &Semaphore{
		ch:       make(chan struct{}, capacity),
		capacity: capacity,
	}
}

// Acquire blocks until a slot is available or ctx is cancelled.
// Returns nil if acquired, ctx.Err() if cancelled.
// For unlimited semaphores (capacity <= 0), always returns nil immediately.
func (s *Semaphore) Acquire(ctx context.Context) error {
	if s == nil || s.capacity <= 0 {
		return nil
	}
	select {
	case s.ch <- struct{}{}:
		s.mu.Lock()
		s.active++
		if s.active > s.peak {
			s.peak = s.active
		}
		s.mu.Unlock()
		atomic.AddInt64(&s.totalAcq, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees a slot. Safe to call on nil receiver.
func (s *Semaphore) Release() {
	if s == nil || s.capacity <= 0 {
		return
	}
	<-s.ch
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
}

// SemaphoreStats contains a point-in-time snapshot of semaphore metrics.
type SemaphoreStats struct {
	Capacity      int
	Active        int
	Peak          int
	TotalAcquired int64
}

// Stats returns current semaphore metrics.
func (s *Semaphore) Stats() SemaphoreStats {
	if s == nil {
		return SemaphoreStats{}
	}
	s.mu.Lock()
	stats := SemaphoreStats{
		Capacity:      s.capacity,
		Active:        s.active,
		Peak:          s.peak,
		TotalAcquired: atomic.LoadInt64(&s.totalAcq),
	}
	s.mu.Unlock()
	return stats
}
