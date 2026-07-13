package client

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// makeResp is a helper that builds a PackageDataResponse with the given ID.
func makeResp(id string) *protocol.PackageDataResponse {
	return &protocol.PackageDataResponse{
		ID:   id,
		Code: 0,
		Msg:  "ok",
		Data: nil,
	}
}

// TestResponseRetryQueue_Enqueue_Dequeue verifies basic enqueue then Drain.
func TestResponseRetryQueue_Enqueue_Dequeue(t *testing.T) {
	q := NewResponseRetryQueue(10, 3, &testLogger{t: t})

	resp := makeResp("resp-1")
	q.Enqueue(resp)

	if q.Len() != 1 {
		t.Fatalf("Len after Enqueue: got %d, want 1", q.Len())
	}

	entries := q.Drain(time.Now().Add(1 * time.Second))
	if len(entries) != 1 {
		t.Fatalf("Drain returned %d entries, want 1", len(entries))
	}
	if entries[0].Response().ID != "resp-1" {
		t.Errorf("Response ID: got %q, want %q", entries[0].Response().ID, "resp-1")
	}
	if entries[0].Attempts() != 0 {
		t.Errorf("Attempts: got %d, want 0", entries[0].Attempts())
	}
}

// TestResponseRetryQueue_Max_Size verifies that exceeding maxSize discards the oldest entry.
func TestResponseRetryQueue_Max_Size(t *testing.T) {
	q := NewResponseRetryQueue(100, 3, &testLogger{t: t})

	// Enqueue 101 items (IDs 0..100).
	for i := 0; i <= 100; i++ {
		q.Enqueue(makeResp(fmt.Sprintf("resp-%d", i)))
	}

	if q.Len() != 100 {
		t.Fatalf("Len after 101 enqueues: got %d, want 100", q.Len())
	}

	// Drain — oldest (resp-0) should have been discarded.
	entries := q.Drain(time.Now().Add(1 * time.Second))
	if len(entries) != 100 {
		t.Fatalf("Drain returned %d entries, want 100", len(entries))
	}
	if entries[0].Response().ID != "resp-1" {
		t.Errorf("first drained ID: got %q, want %q (oldest should be discarded)", entries[0].Response().ID, "resp-1")
	}
}

// TestResponseRetryQueue_Max_Retries_Exceeded verifies that entries with attempts >= maxRetry
// are discarded during Drain.
func TestResponseRetryQueue_Max_Retries_Exceeded(t *testing.T) {
	q := NewResponseRetryQueue(10, 3, &testLogger{t: t})

	// Manually insert an entry with attempts >= maxRetry.
	q.mu.Lock()
	q.entries = append(q.entries, &responseRetryEntry{
		resp:      makeResp("expired"),
		attempts:  3, // equals maxRetry
		maxRetry:  3,
		nextRetry: time.Now().Add(-1 * time.Second),
	})
	q.mu.Unlock()

	if q.Len() != 1 {
		t.Fatalf("Len: got %d, want 1", q.Len())
	}

	entries := q.Drain(time.Now())
	if len(entries) != 0 {
		t.Errorf("Drain returned %d entries, want 0 (expired entry should be discarded)", len(entries))
	}
	if q.Len() != 0 {
		t.Errorf("Len after Drain: got %d, want 0", q.Len())
	}
}

// TestResponseRetryQueue_Backoff_Timing verifies that entries with nextRetry in the future
// are not returned by Drain.
func TestResponseRetryQueue_Backoff_Timing(t *testing.T) {
	q := NewResponseRetryQueue(10, 3, &testLogger{t: t})

	// Insert an entry scheduled 10 seconds in the future.
	q.mu.Lock()
	q.entries = append(q.entries, &responseRetryEntry{
		resp:      makeResp("future"),
		attempts:  0,
		maxRetry:  3,
		nextRetry: time.Now().Add(10 * time.Second),
	})
	q.mu.Unlock()

	entries := q.Drain(time.Now())
	if len(entries) != 0 {
		t.Errorf("Drain returned %d entries, want 0 (entry should be in the future)", len(entries))
	}
	if q.Len() != 1 {
		t.Errorf("Len after Drain: got %d, want 1 (entry should still be queued)", q.Len())
	}
}

// TestResponseRetryQueue_Backoff_Ready verifies that entries with nextRetry in the past
// are returned by Drain.
func TestResponseRetryQueue_Backoff_Ready(t *testing.T) {
	q := NewResponseRetryQueue(10, 3, &testLogger{t: t})

	q.mu.Lock()
	q.entries = append(q.entries, &responseRetryEntry{
		resp:      makeResp("past"),
		attempts:  1,
		maxRetry:  3,
		nextRetry: time.Now().Add(-5 * time.Second),
	})
	q.mu.Unlock()

	entries := q.Drain(time.Now())
	if len(entries) != 1 {
		t.Fatalf("Drain returned %d entries, want 1", len(entries))
	}
	if entries[0].Response().ID != "past" {
		t.Errorf("Response ID: got %q, want %q", entries[0].Response().ID, "past")
	}
	if entries[0].Attempts() != 1 {
		t.Errorf("Attempts: got %d, want 1", entries[0].Attempts())
	}
}

// TestResponseRetryQueue_Empty_Drain verifies that Drain on an empty queue does not panic.
func TestResponseRetryQueue_Empty_Drain(t *testing.T) {
	q := NewResponseRetryQueue(10, 3, &testLogger{t: t})

	entries := q.Drain(time.Now())
	if len(entries) != 0 {
		t.Errorf("Drain on empty queue returned %d entries, want 0", len(entries))
	}
	if q.Len() != 0 {
		t.Errorf("Len: got %d, want 0", q.Len())
	}
}

// TestResponseRetryQueue_Concurrent_Enqueue verifies thread safety under concurrent Enqueue.
func TestResponseRetryQueue_Concurrent_Enqueue(t *testing.T) {
	q := NewResponseRetryQueue(1000, 3, &testLogger{t: t})

	const goroutines = 10
	const perGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				q.Enqueue(makeResp(fmt.Sprintf("g%d-%d", id, i)))
			}
		}(g)
	}

	wg.Wait()

	want := goroutines * perGoroutine // 500, well within maxSize=1000
	if q.Len() != want {
		t.Errorf("Len after concurrent Enqueue: got %d, want %d", q.Len(), want)
	}

	// Drain should return all entries without panic.
	entries := q.Drain(time.Now().Add(1 * time.Second))
	if len(entries) != want {
		t.Errorf("Drain after concurrent Enqueue returned %d entries, want %d", len(entries), want)
	}
}

// TestResponseRetryQueue_Ordering verifies FIFO ordering across enqueue and drain.
func TestResponseRetryQueue_Ordering(t *testing.T) {
	q := NewResponseRetryQueue(10, 3, &testLogger{t: t})

	q.Enqueue(makeResp("A"))
	q.Enqueue(makeResp("B"))
	q.Enqueue(makeResp("C"))

	entries := q.Drain(time.Now().Add(1 * time.Second))
	if len(entries) != 3 {
		t.Fatalf("Drain returned %d entries, want 3", len(entries))
	}

	wantIDs := []string{"A", "B", "C"}
	for i, want := range wantIDs {
		if entries[i].Response().ID != want {
			t.Errorf("entries[%d].ID: got %q, want %q", i, entries[i].Response().ID, want)
		}
	}
}

// TestResponseRetryQueue_EnqueueWithBackoff_Basic verifies that an entry
// enqueued with backoff is not drained until its nextRetry time arrives.
func TestResponseRetryQueue_EnqueueWithBackoff_Basic(t *testing.T) {
	q := NewResponseRetryQueue(10, 5, &testLogger{t: t})

	entry := &responseRetryEntry{
		resp:     makeResp("backoff-basic"),
		attempts: 0,
		maxRetry: 5,
	}
	q.EnqueueWithBackoff(entry)

	if q.Len() != 1 {
		t.Fatalf("Len after EnqueueWithBackoff: got %d, want 1", q.Len())
	}

	// Drain immediately — should return nothing (nextRetry is in the future).
	entries := q.Drain(time.Now())
	if len(entries) != 0 {
		t.Errorf("Drain immediately after EnqueueWithBackoff: got %d entries, want 0", len(entries))
	}
	if q.Len() != 1 {
		t.Errorf("Len after immediate Drain: got %d, want 1", q.Len())
	}

	// Wait for the backoff delay (attempts=0 → baseDelay*2^0 = 1s) to elapse.
	time.Sleep(1100 * time.Millisecond)

	entries = q.Drain(time.Now())
	if len(entries) != 1 {
		t.Errorf("Drain after backoff elapsed: got %d entries, want 1", len(entries))
	}
	if q.Len() != 0 {
		t.Errorf("Len after final Drain: got %d, want 0", q.Len())
	}
}

// TestResponseRetryQueue_EnqueueWithBackoff_ExponentialDelay verifies that the
// backoff delay increases exponentially: attempts=0→1s, attempts=1→2s, attempts=2→4s.
func TestResponseRetryQueue_EnqueueWithBackoff_ExponentialDelay(t *testing.T) {
	q := NewResponseRetryQueue(10, 5, &testLogger{t: t})

	for attempts, wantMinDelay := range []time.Duration{
		1 * time.Second, // 2^0 * 1s
		2 * time.Second, // 2^1 * 1s
		4 * time.Second, // 2^2 * 1s
	} {
		entry := &responseRetryEntry{
			resp:     makeResp(fmt.Sprintf("exp-%d", attempts)),
			attempts: attempts,
			maxRetry: 5,
		}
		q.EnqueueWithBackoff(entry)

		// Drain just before expected delay — should return nothing.
		earlyDrain := q.Drain(time.Now().Add(wantMinDelay - 200*time.Millisecond))
		if len(earlyDrain) != 0 {
			t.Errorf("attempts=%d: Drain before delay returned %d entries, want 0", attempts, len(earlyDrain))
		}

		// Drain at/after expected delay — should return the entry.
		lateDrain := q.Drain(time.Now().Add(wantMinDelay + 100*time.Millisecond))
		if len(lateDrain) != 1 {
			t.Errorf("attempts=%d: Drain after delay returned %d entries, want 1", attempts, len(lateDrain))
		}
	}
}

// TestResponseRetryQueue_EnqueueWithBackoff_Cap verifies that the backoff
// delay is capped at 16s regardless of attempts.
func TestResponseRetryQueue_EnqueueWithBackoff_Cap(t *testing.T) {
	q := NewResponseRetryQueue(10, 100, &testLogger{t: t})

	// attempts=10 would give 2^10 * 1s = 1024s, but should be capped at 16s.
	entry := &responseRetryEntry{
		resp:     makeResp("capped"),
		attempts: 10,
		maxRetry: 100,
	}
	q.EnqueueWithBackoff(entry)

	// Drain at 15s — should return nothing (still capped at 16s).
	entries := q.Drain(time.Now().Add(15 * time.Second))
	if len(entries) != 0 {
		t.Errorf("Drain at 15s: got %d entries, want 0 (capped at 16s)", len(entries))
	}

	// Drain at 17s — should return the entry.
	entries = q.Drain(time.Now().Add(17 * time.Second))
	if len(entries) != 1 {
		t.Errorf("Drain at 17s: got %d entries, want 1", len(entries))
	}
}

// TestResponseRetryQueue_EnqueueWithBackoff_DrainInteraction verifies the full
// cycle: enqueue with backoff, wait, drain returns it, re-enqueue with higher
// attempts, verify longer delay.
func TestResponseRetryQueue_EnqueueWithBackoff_DrainInteraction(t *testing.T) {
	q := NewResponseRetryQueue(10, 5, &testLogger{t: t})

	// Enqueue with attempts=0 (delay=1s).
	entry := &responseRetryEntry{
		resp:     makeResp("drain-interaction"),
		attempts: 0,
		maxRetry: 5,
	}
	q.EnqueueWithBackoff(entry)

	// Wait for delay to elapse.
	time.Sleep(1100 * time.Millisecond)

	// Drain should return the entry.
	entries := q.Drain(time.Now())
	if len(entries) != 1 {
		t.Fatalf("Drain: got %d entries, want 1", len(entries))
	}

	// Simulate failed send: increment attempts and re-enqueue.
	entries[0].attempts++ // now attempts=1 (delay=2s)
	q.EnqueueWithBackoff(entries[0])

	if q.Len() != 1 {
		t.Fatalf("Len after re-enqueue: got %d, want 1", q.Len())
	}

	// Drain at 1s — should return nothing (new delay is 2s).
	earlyEntries := q.Drain(time.Now().Add(1 * time.Second))
	if len(earlyEntries) != 0 {
		t.Errorf("Drain at 1s after re-enqueue: got %d entries, want 0", len(earlyEntries))
	}

	// Drain at 2.5s — should return the entry.
	time.Sleep(2600 * time.Millisecond)
	lateEntries := q.Drain(time.Now())
	if len(lateEntries) != 1 {
		t.Errorf("Drain after 2s delay: got %d entries, want 1", len(lateEntries))
	}
}
