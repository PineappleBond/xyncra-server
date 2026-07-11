// Package e2e_test — Category H: Concurrency & Idempotency E2E tests (Phase 8).
//
// These tests verify the distributed concurrency primitives (ConversationLock,
// IdempotencyStore, Semaphore) that guard agent task execution. Each test
// exercises a single component against the E2E Redis instance to keep
// assertions deterministic and fast.
package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
)

// ---------------------------------------------------------------------------
// TestAgentConc_AE_CONC_001 — Same conversation processed serially (D-075)
// ---------------------------------------------------------------------------

// TestAgentConc_AE_CONC_001 verifies that the ConversationLock provides
// per-conversation serialisation: a second Acquire for the same conversation
// returns false while the lock is held, and succeeds after Release.
func TestAgentConc_AE_CONC_001(t *testing.T) {
	// Scenario: AE-CONC-001
	// Verifies: concurrent messages to same conv, second waits for first (D-075)
	env := setupAgentE2E(t)
	ctx := context.Background()
	convID := "conv-conc-001"

	// Step 1: Acquire lock for conv-1 — should succeed.
	acquired, err := env.lock.Acquire(ctx, convID, 30*time.Second)
	require.NoError(t, err, "first Acquire should not error")
	assert.True(t, acquired, "first Acquire should succeed")

	// Step 2: Try to Acquire again for the same conversation — should fail.
	acquired2, err := env.lock.Acquire(ctx, convID, 30*time.Second)
	require.NoError(t, err, "second Acquire should not error")
	assert.False(t, acquired2, "second Acquire should fail (lock already held)")

	// Step 3: Release the lock.
	err = env.lock.Release(ctx, convID)
	require.NoError(t, err, "Release should succeed")

	// Step 4: Acquire again after Release — should succeed.
	acquired3, err := env.lock.Acquire(ctx, convID, 30*time.Second)
	require.NoError(t, err, "Acquire after Release should not error")
	assert.True(t, acquired3, "Acquire after Release should succeed")
}

// ---------------------------------------------------------------------------
// TestAgentConc_AE_CONC_002 — Different conversations processed in parallel (D-075)
// ---------------------------------------------------------------------------

// TestAgentConc_AE_CONC_002 verifies that ConversationLocks for different
// conversations are independent: acquiring one does not block the other.
func TestAgentConc_AE_CONC_002(t *testing.T) {
	// Scenario: AE-CONC-002
	// Verifies: messages to different conversations processed concurrently (D-075)
	env := setupAgentE2E(t)
	ctx := context.Background()
	conv1 := "conv-conc-002-a"
	conv2 := "conv-conc-002-b"

	// Acquire lock for conv1 — should succeed.
	acquired1, err := env.lock.Acquire(ctx, conv1, 30*time.Second)
	require.NoError(t, err, "Acquire for conv1 should not error")
	assert.True(t, acquired1, "Acquire for conv1 should succeed")

	// Acquire lock for conv2 (different conversation) — should also succeed.
	acquired2, err := env.lock.Acquire(ctx, conv2, 30*time.Second)
	require.NoError(t, err, "Acquire for conv2 should not error")
	assert.True(t, acquired2, "Acquire for conv2 should succeed (independent lock)")

	// Release both — neither should error.
	err = env.lock.Release(ctx, conv1)
	require.NoError(t, err, "Release conv1 should succeed")
	err = env.lock.Release(ctx, conv2)
	require.NoError(t, err, "Release conv2 should succeed")
}

// ---------------------------------------------------------------------------
// TestAgentConc_AE_CONC_003 — Idempotency prevents duplicate execution (D-071)
// ---------------------------------------------------------------------------

// TestAgentConc_AE_CONC_003 verifies that RedisIdempotencyStore.MarkProcessed
// returns false (not duplicate) on the first call and true (duplicate) on
// subsequent calls for the same key, preventing duplicate task execution.
func TestAgentConc_AE_CONC_003(t *testing.T) {
	// Scenario: AE-CONC-003
	// Verifies: same MessageID task executes only once (D-071)
	redisClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	defer redisClient.Close()

	ctx := context.Background()
	require.NoError(t, redisClient.FlushDB(ctx).Err(), "flush Redis before test")

	store := agent.NewRedisIdempotencyStore(redisClient)

	// First call for msg-123 — should NOT be a duplicate.
	dup, err := store.MarkProcessed(ctx, "agent:processed:msg-123", 24*time.Hour)
	require.NoError(t, err, "first MarkProcessed should not error")
	assert.False(t, dup, "first MarkProcessed should return false (not duplicate)")

	// Second call for same key — should be a duplicate.
	dup2, err := store.MarkProcessed(ctx, "agent:processed:msg-123", 24*time.Hour)
	require.NoError(t, err, "second MarkProcessed should not error")
	assert.True(t, dup2, "second MarkProcessed should return true (duplicate)")

	// Different key — should NOT be a duplicate.
	dup3, err := store.MarkProcessed(ctx, "agent:processed:msg-456", 24*time.Hour)
	require.NoError(t, err, "MarkProcessed for different key should not error")
	assert.False(t, dup3, "MarkProcessed for different key should return false")
}

// ---------------------------------------------------------------------------
// TestAgentConc_AE_CONC_004 — Idempotency fail-open (D-072)
// ---------------------------------------------------------------------------

// TestAgentConc_AE_CONC_004 verifies the fail-open contract: when Redis is
// unreachable, MarkProcessed returns an error. The task handler (D-072) treats
// this error as "proceed anyway" — the caller must not block execution.
func TestAgentConc_AE_CONC_004(t *testing.T) {
	// Scenario: AE-CONC-004
	// Verifies: when Redis unavailable, skip check and continue (D-072)
	badClient := redis.NewClient(&redis.Options{
		Addr: "localhost:1", // unreachable — no Redis on this port
	})
	defer badClient.Close()

	store := agent.NewRedisIdempotencyStore(badClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// MarkProcessed should return an error (Redis unreachable).
	_, err := store.MarkProcessed(ctx, fmt.Sprintf("agent:processed:msg-fail-%d", time.Now().UnixNano()), 24*time.Hour)
	require.Error(t, err, "MarkProcessed with unreachable Redis should return error")

	// The fail-open contract (D-072): the task handler checks
	//   if err != nil { logger.Error(...); /* continue processing */ }
	// so an error means "proceed". We verify the error is non-nil, confirming
	// the handler would take the fail-open path and continue execution.
}

// ---------------------------------------------------------------------------
// TestAgentConc_AE_CONC_005 — Semaphore limits concurrency (D-075)
// ---------------------------------------------------------------------------

// TestAgentConc_AE_CONC_005 verifies that the Semaphore caps the number of
// concurrent acquisitions at its capacity. Excess Acquire calls block until
// a slot is released. Stats (peak, total) are updated correctly.
func TestAgentConc_AE_CONC_005(t *testing.T) {
	// Scenario: AE-CONC-005
	// Verifies: tasks exceeding max concurrency queue (D-075)
	capacity := 2
	sem := agent.NewSemaphore(capacity)

	ctx := context.Background()

	// First Acquire — should succeed immediately.
	err := sem.Acquire(ctx)
	require.NoError(t, err, "first Acquire should succeed")

	// Second Acquire — should succeed (at capacity).
	err = sem.Acquire(ctx)
	require.NoError(t, err, "second Acquire should succeed")

	// Third Acquire with short timeout — should fail (context deadline exceeded).
	ctxTimeout, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	err = sem.Acquire(ctxTimeout)
	require.Error(t, err, "third Acquire should timeout (capacity reached)")
	assert.ErrorIs(t, err, context.DeadlineExceeded,
		"timeout error should be context.DeadlineExceeded")

	// Release one slot.
	sem.Release()

	// Now Acquire should succeed again.
	ctx2 := context.Background()
	err = sem.Acquire(ctx2)
	require.NoError(t, err, "Acquire after Release should succeed")

	// Verify stats: capacity=2, peak=2, totalAcquired=3 (two initial + one after release).
	stats := sem.Stats()
	assert.Equal(t, capacity, stats.Capacity, "capacity should match")
	assert.Equal(t, 2, stats.Peak, "peak concurrent should be 2")
	assert.Equal(t, int64(3), stats.TotalAcquired, "total acquired should be 3")

	// Cleanup: release remaining slots.
	sem.Release()
	sem.Release()
}

// ---------------------------------------------------------------------------
// TestAgentConc_AE_CONC_006 — Lock TTL expiry releases (D-075)
// ---------------------------------------------------------------------------

// TestAgentConc_AE_CONC_006 verifies that a ConversationLock is automatically
// released when its TTL expires, allowing a new Acquire to succeed.
func TestAgentConc_AE_CONC_006(t *testing.T) {
	// Scenario: AE-CONC-006
	// Verifies: after lock timeout, new tasks can acquire (D-075)
	env := setupAgentE2E(t)
	ctx := context.Background()
	convID := "conv-conc-006"

	// Acquire with a short TTL (1 second).
	acquired, err := env.lock.Acquire(ctx, convID, 1*time.Second)
	require.NoError(t, err, "first Acquire should succeed")
	assert.True(t, acquired, "first Acquire should return true")

	// Wait for TTL to expire.
	time.Sleep(1500 * time.Millisecond)

	// Acquire again — should succeed because the old lock has expired.
	acquired2, err := env.lock.Acquire(ctx, convID, 30*time.Second)
	require.NoError(t, err, "Acquire after TTL expiry should not error")
	assert.True(t, acquired2, "Acquire after TTL expiry should succeed (old lock expired)")

	// Cleanup.
	_ = env.lock.Release(ctx, convID)
}

// ---------------------------------------------------------------------------
// TestAgentConc_AE_CONC_007 — Lua script prevents accidental lock deletion (D-075)
// ---------------------------------------------------------------------------

// TestAgentConc_AE_CONC_007 verifies that the Lua release script only deletes
// the lock when the caller's token matches the stored token. A different
// lock instance (different random token) cannot release another's lock.
func TestAgentConc_AE_CONC_007(t *testing.T) {
	// Scenario: AE-CONC-007
	// Verifies: only release locks you own (D-075)
	redisClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	defer redisClient.Close()

	ctx := context.Background()
	require.NoError(t, redisClient.FlushDB(ctx).Err(), "flush Redis before test")

	convID := "conv-conc-007"

	// Create two lock instances — each gets a unique random token.
	lock1 := agent.NewRedisConversationLock(redisClient)
	lock2 := agent.NewRedisConversationLock(redisClient)

	// lock1 acquires the lock.
	acquired, err := lock1.Acquire(ctx, convID, 30*time.Second)
	require.NoError(t, err, "lock1 Acquire should succeed")
	assert.True(t, acquired, "lock1 should acquire the lock")

	// lock2 tries to release — should fail silently (Lua script returns 0
	// without deleting because lock2's token != lock1's token).
	err = lock2.Release(ctx, convID)
	require.NoError(t, err, "lock2 Release should not return error (Lua returns 0)")

	// Verify the lock is still held: lock2 cannot acquire it.
	acquired2, err := lock2.Acquire(ctx, convID, 30*time.Second)
	require.NoError(t, err, "lock2 Acquire should not error")
	assert.False(t, acquired2, "lock2 should NOT acquire (lock still held by lock1)")

	// lock1 releases — should succeed.
	err = lock1.Release(ctx, convID)
	require.NoError(t, err, "lock1 Release should succeed")

	// Now lock2 can acquire the lock.
	acquired3, err := lock2.Acquire(ctx, convID, 30*time.Second)
	require.NoError(t, err, "lock2 Acquire after lock1 Release should not error")
	assert.True(t, acquired3, "lock2 should acquire after lock1 releases")

	// Cleanup.
	_ = lock2.Release(ctx, convID)
}
