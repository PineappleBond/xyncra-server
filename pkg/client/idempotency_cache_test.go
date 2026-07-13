package client

import (
	"fmt"
	"sync"
	"testing"
)

func TestIdempotencyCache_Put_Contains(t *testing.T) {
	c := NewIdempotencyCache(10)
	c.Put("key-1")

	if !c.Contains("key-1") {
		t.Error("Contains(\"key-1\") = false, want true after Put")
	}
}

func TestIdempotencyCache_Miss(t *testing.T) {
	c := NewIdempotencyCache(10)

	if c.Contains("nonexistent") {
		t.Error("Contains(\"nonexistent\") = true, want false")
	}

	// Also miss after another key was inserted.
	c.Put("other")
	if c.Contains("nonexistent") {
		t.Error("Contains(\"nonexistent\") = true after inserting other key, want false")
	}
}

func TestIdempotencyCache_Len(t *testing.T) {
	c := NewIdempotencyCache(10)

	if got := c.Len(); got != 0 {
		t.Errorf("Len() on empty cache = %d, want 0", got)
	}

	c.Put("a")
	if got := c.Len(); got != 1 {
		t.Errorf("Len() after 1 Put = %d, want 1", got)
	}

	c.Put("b")
	c.Put("c")
	if got := c.Len(); got != 3 {
		t.Errorf("Len() after 3 Puts = %d, want 3", got)
	}
}

func TestIdempotencyCache_Duplicate_Put(t *testing.T) {
	c := NewIdempotencyCache(10)
	c.Put("dup")
	c.Put("dup")
	c.Put("dup")

	if got := c.Len(); got != 1 {
		t.Errorf("Len() after 3 duplicate Puts = %d, want 1", got)
	}
}

func TestIdempotencyCache_LRU_Eviction(t *testing.T) {
	c := NewIdempotencyCache(3)
	c.Put("A")
	c.Put("B")
	c.Put("C")
	c.Put("D") // should evict A (LRU)

	if c.Contains("A") {
		t.Error("Contains(\"A\") = true, want false (should have been evicted)")
	}
	if !c.Contains("B") {
		t.Error("Contains(\"B\") = false, want true")
	}
	if !c.Contains("C") {
		t.Error("Contains(\"C\") = false, want true")
	}
	if !c.Contains("D") {
		t.Error("Contains(\"D\") = false, want true")
	}
	if got := c.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
}

func TestIdempotencyCache_LRU_Access_Order(t *testing.T) {
	c := NewIdempotencyCache(3)
	c.Put("A")
	c.Put("B")
	c.Put("C")

	// Access A, promoting it to MRU. Order is now: A(mru) -> C -> B(lru)
	c.Contains("A")

	// Insert D; should evict B (the current LRU, not A).
	c.Put("D")

	if !c.Contains("A") {
		t.Error("Contains(\"A\") = false, want true (was accessed, should not be evicted)")
	}
	if c.Contains("B") {
		t.Error("Contains(\"B\") = true, want false (should have been evicted as LRU)")
	}
	if !c.Contains("C") {
		t.Error("Contains(\"C\") = false, want true")
	}
	if !c.Contains("D") {
		t.Error("Contains(\"D\") = false, want true")
	}
}

func TestIdempotencyCache_Capacity_One(t *testing.T) {
	c := NewIdempotencyCache(1)
	c.Put("first")

	if got := c.Len(); got != 1 {
		t.Errorf("Len() = %d, want 1", got)
	}
	if !c.Contains("first") {
		t.Error("Contains(\"first\") = false, want true")
	}

	// Inserting second key evicts the first.
	c.Put("second")
	if got := c.Len(); got != 1 {
		t.Errorf("Len() after second Put = %d, want 1", got)
	}
	if c.Contains("first") {
		t.Error("Contains(\"first\") = true, want false (should have been evicted)")
	}
	if !c.Contains("second") {
		t.Error("Contains(\"second\") = false, want true")
	}
}

func TestIdempotencyCache_Empty_Key(t *testing.T) {
	c := NewIdempotencyCache(5)
	c.Put("")

	if !c.Contains("") {
		t.Error("Contains(\"\") = false, want true (empty key is legal)")
	}
	if got := c.Len(); got != 1 {
		t.Errorf("Len() = %d, want 1", got)
	}

	// Empty key counts as existing for duplicate Put.
	c.Put("")
	if got := c.Len(); got != 1 {
		t.Errorf("Len() after duplicate Put(\"\") = %d, want 1", got)
	}
}

func TestIdempotencyCache_Concurrent(t *testing.T) {
	c := NewIdempotencyCache(100)
	const goroutines = 10
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("g%d-k%d", id, i)
				c.Put(key)
				c.Contains(key)
				c.Len()
			}
		}(g)
	}
	wg.Wait()

	// Cache should be at capacity, not exceed it.
	if got := c.Len(); got > 100 {
		t.Errorf("Len() = %d, want <= 100 (capacity)", got)
	}
}
