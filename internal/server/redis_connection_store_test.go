package server

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
// Test helpers
// ---------------------------------------------------------------------------

// setupTestRedisConnectionStore creates a RedisConnectionStore backed by
// miniredis for testing. MaxConnectionsPerUser is set to 0 (unlimited) unless
// overridden.
func setupTestRedisConnectionStore(t *testing.T, maxConns int) (*RedisConnectionStore, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	store := NewRedisConnectionStoreFromClient(RedisConnectionStoreFromClientConfig{
		Client:                rdb,
		DefaultTTL:            30 * time.Minute,
		MaxConnectionsPerUser: maxConns,
	})
	return store, mr, rdb
}

// makeConnInfo creates a ConnectionInfo with the given id and userID for testing.
func makeConnInfo(id, userID string) *ConnectionInfo {
	return &ConnectionInfo{
		ID:        id,
		UserID:    userID,
		DeviceID:  "device-1",
		SessionID: "session-1",
		TTL:       30 * time.Minute,
	}
}

// ---------------------------------------------------------------------------
// D-010: User SET TTL tests
// ---------------------------------------------------------------------------

// TestRedisConnectionStore_AddSetsUserSetTTL verifies that Add() sets a TTL
// on the per-user connection SET (D-010). Without this, the SET would persist
// forever as an orphan key even after all connections expire.
func TestRedisConnectionStore_AddSetsUserSetTTL(t *testing.T) {
	store, _, rdb := setupTestRedisConnectionStore(t, 0)
	ctx := context.Background()

	info := makeConnInfo("conn-1", "user-1")
	require.NoError(t, store.Add(ctx, info))

	// Check that the user SET has a TTL (PTTL > 0).
	userKey := store.userKey("user-1")
	pttl, err := rdb.PTTL(ctx, userKey).Result()
	require.NoError(t, err)
	assert.Greater(t, pttl.Milliseconds(), int64(0), "user SET should have a TTL after Add()")
}

// TestRedisConnectionStore_RefreshRefreshesUserSetTTL verifies that Refresh()
// resets the TTL on both the info key and the per-user SET (D-010).
func TestRedisConnectionStore_RefreshRefreshesUserSetTTL(t *testing.T) {
	store, mr, rdb := setupTestRedisConnectionStore(t, 0)
	ctx := context.Background()

	info := makeConnInfo("conn-1", "user-1")
	info.TTL = 1 * time.Hour
	require.NoError(t, store.Add(ctx, info))

	userKey := store.userKey("user-1")

	// Fast-forward miniredis time to reduce the TTL.
	mr.FastForward(30 * time.Minute)

	// Record the PTTL before refresh.
	pttlBefore, err := rdb.PTTL(ctx, userKey).Result()
	require.NoError(t, err)

	// Refresh should reset the TTL.
	require.NoError(t, store.Refresh(ctx, "conn-1"))

	pttlAfter, err := rdb.PTTL(ctx, userKey).Result()
	require.NoError(t, err)

	assert.Greater(t, pttlAfter.Milliseconds(), pttlBefore.Milliseconds(),
		"Refresh() should reset the user SET TTL to a higher value")
}

// TestRedisConnectionStore_MultipleConnectionsMaxTTL verifies the MAX
// semantics: when a second connection is added for the same user, the user
// SET TTL is the maximum of the existing and new TTLs (D-010).
func TestRedisConnectionStore_MultipleConnectionsMaxTTL(t *testing.T) {
	store, _, rdb := setupTestRedisConnectionStore(t, 0)
	ctx := context.Background()

	// Add first connection with 1-hour TTL.
	info1 := makeConnInfo("conn-1", "user-1")
	info1.TTL = 1 * time.Hour
	require.NoError(t, store.Add(ctx, info1))

	userKey := store.userKey("user-1")
	pttl1, err := rdb.PTTL(ctx, userKey).Result()
	require.NoError(t, err)

	// Add second connection with 30-minute TTL (shorter).
	// The user SET TTL should NOT be shortened.
	info2 := makeConnInfo("conn-2", "user-1")
	info2.TTL = 30 * time.Minute
	require.NoError(t, store.Add(ctx, info2))

	pttl2, err := rdb.PTTL(ctx, userKey).Result()
	require.NoError(t, err)

	// The SET TTL should be >= the first TTL (MAX semantics).
	// Allow a small tolerance for timing.
	assert.GreaterOrEqual(t, pttl2.Milliseconds(), pttl1.Milliseconds()-100,
		"user SET TTL should not be shortened by a second connection with shorter TTL")
}

// TestRedisConnectionStore_ListByUserCleansEmptySet verifies that ListByUser
// cleans up empty user SETs when all connections have expired (D-010).
func TestRedisConnectionStore_ListByUserCleansEmptySet(t *testing.T) {
	store, mr, rdb := setupTestRedisConnectionStore(t, 0)
	ctx := context.Background()

	info := makeConnInfo("conn-1", "user-1")
	info.TTL = 5 * time.Minute
	require.NoError(t, store.Add(ctx, info))

	userKey := store.userKey("user-1")

	// Verify the user SET exists.
	exists, err := rdb.Exists(ctx, userKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists, "user SET should exist after Add()")

	// Expire the connection info key in miniredis (simulate TTL expiry).
	mr.FastForward(10 * time.Minute)

	// ListByUser should clean up the stale entry from the SET.
	conns, err := store.ListByUser(ctx, "user-1", 10)
	require.NoError(t, err)
	assert.Empty(t, conns, "should return no connections after TTL expiry")

	// The user SET itself should be deleted (D-010 cleanup).
	exists, err = rdb.Exists(ctx, userKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists, "empty user SET should be deleted after ListByUser cleanup")
}
