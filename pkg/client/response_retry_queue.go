package client

import (
	"sync"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// responseRetryEntry represents a single response waiting to be retried.
type responseRetryEntry struct {
	resp      *protocol.PackageDataResponse
	attempts  int
	maxRetry  int
	nextRetry time.Time
}

// Response returns the protocol response payload for this retry entry.
func (e *responseRetryEntry) Response() *protocol.PackageDataResponse {
	return e.resp
}

// Attempts returns how many times this entry has already been attempted.
func (e *responseRetryEntry) Attempts() int {
	return e.attempts
}

// ResponseRetryQueue manages retry attempts for failed response sends.
// Thread-safe via sync.Mutex.
type ResponseRetryQueue struct {
	mu       sync.Mutex
	entries  []*responseRetryEntry
	maxSize  int
	maxRetry int
	logger   Logger
}

// NewResponseRetryQueue creates a new retry queue.
func NewResponseRetryQueue(maxSize, maxRetry int, logger Logger) *ResponseRetryQueue {
	return &ResponseRetryQueue{
		entries:  make([]*responseRetryEntry, 0, maxSize),
		maxSize:  maxSize,
		maxRetry: maxRetry,
		logger:   logger,
	}
}

// Enqueue adds a response to the retry queue.
// If the queue is full, the oldest entry is discarded (with a warning log).
func (q *ResponseRetryQueue) Enqueue(resp *protocol.PackageDataResponse) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) >= q.maxSize {
		q.logger.Info("ResponseRetryQueue: queue full (%d), discarding oldest entry", q.maxSize)
		q.entries = q.entries[1:]
	}

	q.entries = append(q.entries, &responseRetryEntry{
		resp:      resp,
		attempts:  0,
		maxRetry:  q.maxRetry,
		nextRetry: time.Now(),
	})
}

// Drain returns all entries whose nextRetry <= now and removes them from the queue.
// Entries that have exceeded maxRetry are discarded.
// Returns entries in FIFO order.
func (q *ResponseRetryQueue) Drain(now time.Time) []*responseRetryEntry {
	q.mu.Lock()
	defer q.mu.Unlock()

	remaining := make([]*responseRetryEntry, 0, len(q.entries))
	drained := make([]*responseRetryEntry, 0)

	for _, e := range q.entries {
		if e.nextRetry.After(now) {
			remaining = append(remaining, e)
			continue
		}
		if e.attempts >= e.maxRetry {
			q.logger.Debug("ResponseRetryQueue: discarding entry %s after %d attempts (max=%d)", e.resp.ID, e.attempts, e.maxRetry)
			continue
		}
		drained = append(drained, e)
	}

	q.entries = remaining
	return drained
}

// Len returns the current queue size.
func (q *ResponseRetryQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.entries)
}

// EnqueueWithBackoff adds a previously-drained entry back into the queue with
// exponential backoff: nextRetry = now + baseDelay * 2^attempts, capped at 16s.
func (q *ResponseRetryQueue) EnqueueWithBackoff(e *responseRetryEntry) {
	const baseDelay = 1 * time.Second
	const maxBackoff = 16 * time.Second

	delay := baseDelay * (1 << e.attempts)
	if delay > maxBackoff {
		delay = maxBackoff
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) >= q.maxSize {
		q.logger.Info("ResponseRetryQueue: queue full (%d), discarding oldest entry", q.maxSize)
		q.entries = q.entries[1:]
	}

	e.nextRetry = time.Now().Add(delay)
	q.entries = append(q.entries, e)
}
