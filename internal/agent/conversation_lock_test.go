package agent

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 1. Acquire + Release: basic flow
// ---------------------------------------------------------------------------

func TestRedisConversationLock_AcquireRelease(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	lock := NewRedisConversationLock(client)

	// Acquire should succeed.
	acquired, err := lock.Acquire(context.Background(), "conv-1", 10*time.Second)
	require.NoError(t, err)
	assert.True(t, acquired, "first acquire should succeed")

	// Release should succeed (no error).
	err = lock.Release(context.Background(), "conv-1")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// 2. Second acquire on same key returns false (already held)
// ---------------------------------------------------------------------------

func TestRedisConversationLock_AcquireAlreadyHeld(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	lock := NewRedisConversationLock(client)

	// First acquire should succeed.
	acquired, err := lock.Acquire(context.Background(), "conv-2", 10*time.Second)
	require.NoError(t, err)
	assert.True(t, acquired)

	// Second acquire (same instance, same key) should fail — key already exists.
	acquired2, err := lock.Acquire(context.Background(), "conv-2", 10*time.Second)
	require.NoError(t, err)
	assert.False(t, acquired2, "second acquire on same key should return false")
}

// ---------------------------------------------------------------------------
// 3. Different token cannot release another lock holder's lock
// ---------------------------------------------------------------------------

func TestRedisConversationLock_Release_WrongToken(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	// Two independent lock instances (different tokens).
	lock1 := NewRedisConversationLock(client)
	lock2 := NewRedisConversationLock(client)

	// lock1 acquires the lock.
	acquired, err := lock1.Acquire(context.Background(), "conv-3", 10*time.Second)
	require.NoError(t, err)
	assert.True(t, acquired)

	// lock2 tries to release — should NOT delete because token differs.
	err = lock2.Release(context.Background(), "conv-3")
	require.NoError(t, err) // no error, but key not deleted

	// Verify key still exists.
	exists := mr.Exists(lockKey("conv-3"))
	assert.True(t, exists, "lock key should still exist after wrong-token release")

	// lock1 can release successfully.
	err = lock1.Release(context.Background(), "conv-3")
	require.NoError(t, err)

	// Verify key is now deleted.
	exists = mr.Exists(lockKey("conv-3"))
	assert.False(t, exists, "lock key should be deleted after correct-token release")
}

// ---------------------------------------------------------------------------
// 4. After TTL expiry, lock can be re-acquired
// ---------------------------------------------------------------------------

func TestRedisConversationLock_AcquireAfterTTLExpiry(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	lock := NewRedisConversationLock(client)

	// Acquire with short TTL.
	acquired, err := lock.Acquire(context.Background(), "conv-4", 5*time.Second)
	require.NoError(t, err)
	assert.True(t, acquired)

	// Fast-forward past TTL.
	mr.FastForward(10 * time.Second)

	// Should be able to acquire again.
	acquired2, err := lock.Acquire(context.Background(), "conv-4", 10*time.Second)
	require.NoError(t, err)
	assert.True(t, acquired2, "should be able to acquire after TTL expiry")
}

// ---------------------------------------------------------------------------
// 5. Acquire with Redis error
// ---------------------------------------------------------------------------

func TestRedisConversationLock_AcquireRedisError(t *testing.T) {
	// Use an unreachable address to trigger a connection error.
	client := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	defer client.Close()

	lock := NewRedisConversationLock(client)

	_, err := lock.Acquire(context.Background(), "conv-5", 10*time.Second)
	assert.Error(t, err, "should return error when Redis is unreachable")
}

// ---------------------------------------------------------------------------
// 6. Release on non-existent key does not error
// ---------------------------------------------------------------------------

func TestRedisConversationLock_ReleaseNonExistentKey(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	lock := NewRedisConversationLock(client)

	// Release on a key that doesn't exist — should not error.
	err = lock.Release(context.Background(), "conv-6")
	require.NoError(t, err, "release on non-existent key should not error")
}

// ---------------------------------------------------------------------------
// 7. lockKey format
// ---------------------------------------------------------------------------

func TestLockKey(t *testing.T) {
	assert.Equal(t, "agent:lock:conv-abc", lockKey("conv-abc"))
}
