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
