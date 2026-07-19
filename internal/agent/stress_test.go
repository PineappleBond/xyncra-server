package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestConcurrentSameConversation — 10 goroutines, same conversationID
//
// Verifies that the per-conversation lock (RedisConversationLock) serializes
// access: at most 1 goroutine holds the lock at any given time.
// ---------------------------------------------------------------------------

func TestConcurrentSameConversation(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	const goroutines = 10

	// Each goroutine gets its own lock instance (unique token) so that
	// token-based release isolation is also tested.
	var active int64
	var maxActive int64
	var processed int64
	var skipped int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			lock := NewRedisConversationLock(client)
			ctx := context.Background()
			convID := "conv-stress-001"

			acquired, err := lock.Acquire(ctx, convID, 10*time.Second)
			require.NoError(t, err)
			if !acquired {
				atomic.AddInt64(&skipped, 1)
				return
			}

			// Track concurrent execution count.
			cur := atomic.AddInt64(&active, 1)
			for {
				old := atomic.LoadInt64(&maxActive)
				if cur <= old || atomic.CompareAndSwapInt64(&maxActive, old, cur) {
					break
				}
			}

			// Simulate work.
			time.Sleep(20 * time.Millisecond)

			atomic.AddInt64(&active, -1)
			atomic.AddInt64(&processed, 1)

			if err := lock.Release(ctx, convID); err != nil {
				t.Errorf("lock release failed (goroutine %d): %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// The lock is exclusive: max concurrent should be exactly 1.
	assert.Equal(t, int64(1), maxActive, "max concurrent should be 1 (exclusive lock)")

	// All goroutines should either process or be skipped (no panic, no hang).
	total := atomic.LoadInt64(&processed) + atomic.LoadInt64(&skipped)
	assert.Equal(t, int64(goroutines), total, "all goroutines should complete")
	t.Logf("processed=%d, skipped=%d", processed, skipped)
}

// ---------------------------------------------------------------------------
// TestSemaphoreBounds — 50 goroutines, semaphore capacity 5
//
// Verifies that the semaphore never allows more than 5 concurrent acquisitions.
// ---------------------------------------------------------------------------

func TestSemaphoreBounds(t *testing.T) {
	sem := NewSemaphore(5)
	var active int64
	var maxActive int64
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sem.Acquire(context.Background()); err != nil {
				return
			}
			defer sem.Release()

			cur := atomic.AddInt64(&active, 1)
			// Update max using CAS loop.
			for {
				old := atomic.LoadInt64(&maxActive)
				if cur <= old || atomic.CompareAndSwapInt64(&maxActive, old, cur) {
					break
				}
			}

			time.Sleep(10 * time.Millisecond) // simulate work
			atomic.AddInt64(&active, -1)
		}()
	}
	wg.Wait()

	stats := sem.Stats()
	if maxActive > 5 {
		t.Errorf("max active %d exceeded capacity 5", maxActive)
	}
	if stats.TotalAcquired != 50 {
		t.Errorf("expected 50 total acquired, got %d", stats.TotalAcquired)
	}
	t.Logf("peak=%d, totalAcquired=%d", stats.Peak, stats.TotalAcquired)
}

// ---------------------------------------------------------------------------
// TestCacheCleanup — 100 cached conversations, verify expired entries removed
//
// Creates a DBContextManager with a very short TTL, manually populates the
// cache, waits for expiry, runs cleanup, and verifies entries are removed.
// ---------------------------------------------------------------------------

func TestCacheCleanup(t *testing.T) {
	cm := &DBContextManager{
		ttl:       50 * time.Millisecond,
		tokenizer: &HeuristicTokenCounter{},
	}

	// Manually inject 100 cache entries.
	for i := 0; i < 100; i++ {
		cm.cache.Store(fmt.Sprintf("conv-%d", i), &cachedContext{
			messages:  nil,
			fetchedAt: time.Now(),
		})
	}

	// Verify all 100 are present.
	countBefore := countCacheEntries(cm)
	assert.Equal(t, 100, countBefore, "should have 100 entries before cleanup")

	// Wait for TTL to expire.
	time.Sleep(80 * time.Millisecond)

	// Run cleanup.
	cm.cleanupExpired()

	// Verify all entries have been removed.
	countAfter := countCacheEntries(cm)
	assert.Equal(t, 0, countAfter, "all expired entries should be cleaned up")
}

// countCacheEntries counts the number of entries in the sync.Map cache.
func countCacheEntries(cm *DBContextManager) int {
	var count int
	cm.cache.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// ---------------------------------------------------------------------------
// TestReloadAgents — concurrent Reload + Get/IsAgent/ListAll
//
// Verifies there are no race conditions when multiple goroutines read and
// reload the registry concurrently. Run with -race to detect data races.
// ---------------------------------------------------------------------------

func TestReloadAgents(t *testing.T) {
	dir := t.TempDir()

	// Write 10 agent .md files.
	for i := 0; i < 10; i++ {
		content := fmt.Sprintf(`---
id: agent/test-%d
name: Test Agent %d
model: gpt-4
api_key_env: TEST_KEY_%d
---
System prompt for agent %d.
`, i, i, i, i)
		require.NoError(t, writeTestFile(dir, fmt.Sprintf("agent-%d.md", i), content))
	}

	registry := NewRegistry()
	require.NoError(t, registry.Load(dir))
	assert.Equal(t, 10, registry.Count())

	var wg sync.WaitGroup

	// 10 goroutines doing concurrent Reload.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = registry.Reload()
		}()
	}

	// 10 goroutines doing concurrent reads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("agent/test-%d", idx%10)
			registry.Get(id)
			registry.IsAgent(id)
			_ = registry.ListAll()
		}(i)
	}

	wg.Wait()

	// After all concurrent operations, registry should still be consistent.
	assert.Equal(t, 10, registry.Count())
}

// writeTestFile writes content to a file in dir. Used only in tests.
func writeTestFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
}
