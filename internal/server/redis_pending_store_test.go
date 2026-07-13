package server

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRedisPendingStore creates a RedisPendingStore backed by miniredis
// for testing. MaxPendingPerDevice is set to 3 to exercise trim logic.
func setupTestRedisPendingStore(t *testing.T) (*RedisPendingStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	store := NewRedisPendingStore(rdb, PendingStoreConfig{
		MaxPendingPerDevice: 3,
		RequestTTL:          24 * time.Hour,
	})
	return store, mr
}

// makePendingReq creates a PendingRequest with the given ID for testing.
func makePendingReq(id, userID, deviceID, method string) *PendingRequest {
	return &PendingRequest{
		ID:             id,
		UserID:         userID,
		DeviceID:       deviceID,
		Method:         method,
		Params:         json.RawMessage(`{"k":"v"}`),
		IdempotencyKey: id,
		Seq:            1,
		RetryCount:     0,
		MaxRetries:     3,
		CreatedAt:      time.Now(),
	}
}

// TestRedisPendingStore_SaveAndList verifies a basic Save + List round-trip.
func TestRedisPendingStore_SaveAndList(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	req := makePendingReq("req-1", "user-1", "device-1", "ping")
	require.NoError(t, store.Save(ctx, req))

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 1)

	got := list[0]
	assert.Equal(t, "req-1", got.ID)
	assert.Equal(t, "user-1", got.UserID)
	assert.Equal(t, "device-1", got.DeviceID)
	assert.Equal(t, "ping", got.Method)
	assert.Equal(t, "req-1", got.IdempotencyKey)
	assert.Equal(t, uint64(1), got.Seq)
	assert.Equal(t, 0, got.RetryCount)
	assert.Equal(t, 3, got.MaxRetries)
	assert.False(t, got.CreatedAt.IsZero())
}

// TestRedisPendingStore_SaveMultiple verifies that multiple Saves preserve
// insertion order.
func TestRedisPendingStore_SaveMultiple(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		req := makePendingReq("req-"+string(rune('0'+i)), "user-1", "device-1", "method")
		req.Seq = uint64(i)
		require.NoError(t, store.Save(ctx, req))
	}

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, "req-1", list[0].ID)
	assert.Equal(t, "req-2", list[1].ID)
	assert.Equal(t, "req-3", list[2].ID)
}

// TestRedisPendingStore_Save_TrimOldest verifies that adding beyond
// MaxPendingPerDevice trims the oldest entries.
func TestRedisPendingStore_Save_TrimOldest(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	// MaxPendingPerDevice is 3; add 4 entries.
	for i := 1; i <= 4; i++ {
		req := makePendingReq("req-"+string(rune('0'+i)), "user-1", "device-1", "method")
		req.Seq = uint64(i)
		require.NoError(t, store.Save(ctx, req))
	}

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 3, "should have exactly 3 entries after trim")

	// The oldest (req-1) should be trimmed; req-2, req-3, req-4 remain.
	assert.Equal(t, "req-2", list[0].ID)
	assert.Equal(t, "req-3", list[1].ID)
	assert.Equal(t, "req-4", list[2].ID)
}

// TestRedisPendingStore_Remove verifies removing a specific pending request.
func TestRedisPendingStore_Remove(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	for _, id := range []string{"req-1", "req-2", "req-3"} {
		require.NoError(t, store.Save(ctx, makePendingReq(id, "user-1", "device-1", "method")))
	}

	require.NoError(t, store.Remove(ctx, "user-1", "device-1", "req-2"))

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "req-1", list[0].ID)
	assert.Equal(t, "req-3", list[1].ID)
}

// TestRedisPendingStore_RemoveUnknown verifies that removing a non-existent
// request is a no-op (idempotent, no error).
func TestRedisPendingStore_RemoveUnknown(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, makePendingReq("req-1", "user-1", "device-1", "method")))

	assert.NoError(t, store.Remove(ctx, "user-1", "device-1", "nonexistent"))

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 1, "removing unknown ID should not affect existing entries")
}

// TestRedisPendingStore_RemoveByDevice verifies removing all pending requests
// for a device.
func TestRedisPendingStore_RemoveByDevice(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	for _, id := range []string{"req-1", "req-2"} {
		require.NoError(t, store.Save(ctx, makePendingReq(id, "user-1", "device-1", "method")))
	}

	require.NoError(t, store.RemoveByDevice(ctx, "user-1", "device-1"))

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	assert.Empty(t, list, "all entries for the device should be removed")
}

// TestRedisPendingStore_TTL verifies that the Redis key expires after the
// configured TTL using miniredis FastForward.
func TestRedisPendingStore_TTL(t *testing.T) {
	store, mr := setupTestRedisPendingStore(t)
	ctx := context.Background()

	req := makePendingReq("req-1", "user-1", "device-1", "ping")
	require.NoError(t, store.Save(ctx, req))

	key := "pending:user-1\x00device-1"
	assert.True(t, mr.Exists(key), "key should exist after Save")

	// FastForward past the 24h TTL.
	mr.FastForward(25 * time.Hour)

	assert.False(t, mr.Exists(key), "key should have expired after TTL")

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	assert.Empty(t, list, "List should return empty after TTL expiry")
}

// TestRedisPendingStore_TTL_RefreshOnSave verifies that each Save refreshes
// the TTL on the key.
func TestRedisPendingStore_TTL_RefreshOnSave(t *testing.T) {
	store, mr := setupTestRedisPendingStore(t)
	ctx := context.Background()

	// Save first entry.
	req1 := makePendingReq("req-1", "user-1", "device-1", "ping")
	require.NoError(t, store.Save(ctx, req1))

	key := "pending:user-1\x00device-1"

	// Advance 23 hours (just under 24h TTL).
	mr.FastForward(23 * time.Hour)
	assert.True(t, mr.Exists(key), "key should still exist before TTL")

	// Save second entry: should refresh TTL.
	req2 := makePendingReq("req-2", "user-1", "device-1", "ping")
	require.NoError(t, store.Save(ctx, req2))

	// Advance another 23 hours (total 46h from first save, 23h from second).
	mr.FastForward(23 * time.Hour)
	assert.True(t, mr.Exists(key), "key should still exist because second Save refreshed TTL")

	// Advance 2 more hours (total 48h from first save, 25h from second save).
	mr.FastForward(2 * time.Hour)
	assert.False(t, mr.Exists(key), "key should have expired after refreshed TTL")
}

// TestRedisPendingStore_KeyFormat verifies the Redis key format is
// "pending:{userID}\x00{deviceID}".
func TestRedisPendingStore_KeyFormat(t *testing.T) {
	store, mr := setupTestRedisPendingStore(t)
	ctx := context.Background()

	req := makePendingReq("req-1", "alice", "phone-42", "ping")
	require.NoError(t, store.Save(ctx, req))

	keys := mr.Keys()
	expectedKey := "pending:alice\x00phone-42"
	assert.True(t, slices.Contains(keys, expectedKey), "expected key %q not found in Redis keys: %v", expectedKey, keys)
}

// TestRedisPendingStore_RedisDown verifies that Save returns an error when
// Redis is unavailable.
func TestRedisPendingStore_RedisDown(t *testing.T) {
	store, mr := setupTestRedisPendingStore(t)
	ctx := context.Background()

	// Shut down miniredis.
	mr.Close()

	req := makePendingReq("req-1", "user-1", "device-1", "ping")
	err := store.Save(ctx, req)
	require.Error(t, err, "Save should fail when Redis is down")
	assert.Contains(t, err.Error(), "pending store")
}

// TestRedisPendingStore_ConcurrentSave verifies that concurrent Saves from
// multiple goroutines are safe and do not race.
func TestRedisPendingStore_ConcurrentSave(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	const n = 10
	var wg sync.WaitGroup

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := makePendingReq("req-concurrent", "user-1", "device-1", "ping")
			req.ID = "req-" + string(rune('A'+idx))
			req.Seq = uint64(idx + 1)
			assert.NoError(t, store.Save(ctx, req))
		}(i)
	}

	wg.Wait()

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	// MaxPendingPerDevice is 3, so at most 3 entries remain.
	assert.LessOrEqual(t, len(list), 3, "should not exceed MaxPendingPerDevice")
	assert.Greater(t, len(list), 0, "should have at least some entries")
}

// TestRedisPendingStore_ListEmpty verifies that List on a non-existent key
// returns an empty slice without error.
func TestRedisPendingStore_ListEmpty(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	list, err := store.List(ctx, "nonexistent-user", "nonexistent-device")
	require.NoError(t, err)
	assert.Empty(t, list, "List on non-existent key should return empty slice")
}

// TestRedisPendingStore_ListSkipsCorrupted verifies that List skips entries
// with corrupted JSON rather than failing the entire operation.
func TestRedisPendingStore_ListSkipsCorrupted(t *testing.T) {
	store, mr := setupTestRedisPendingStore(t)
	ctx := context.Background()

	key := "pending:user-1\x00device-1"

	// Push one valid entry and one corrupted entry directly into Redis.
	goodReq := makePendingReq("req-good", "user-1", "device-1", "ping")
	goodData, err := json.Marshal(goodReq)
	require.NoError(t, err)

	mr.RPush(key, string(goodData))
	mr.RPush(key, "{corrupted-json")

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 1, "should skip corrupted entry")
	assert.Equal(t, "req-good", list[0].ID)
}

// TestRedisPendingStore_SaveNilParams verifies that Save does not error
// when Params is json.RawMessage(nil).
func TestRedisPendingStore_SaveNilParams(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	req := makePendingReq("req-nil", "user-1", "device-1", "ping")
	req.Params = json.RawMessage(nil)

	err := store.Save(ctx, req)
	require.NoError(t, err, "Save with nil Params should not error")

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "req-nil", list[0].ID)
}

// ---------------------------------------------------------------------------
// Update tests (Phase 5)
// ---------------------------------------------------------------------------

// TestRedisPendingStore_Update_RetryCount verifies updating RetryCount.
func TestRedisPendingStore_Update_RetryCount(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	req := makePendingReq("req-1", "user-1", "device-1", "ping")
	require.NoError(t, store.Save(ctx, req))

	// Update RetryCount from 0 to 1.
	req.RetryCount = 1
	require.NoError(t, store.Update(ctx, req))

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, 1, list[0].RetryCount)
	assert.Equal(t, "req-1", list[0].ID)
}

// TestRedisPendingStore_Update_MultipleFields verifies other fields are preserved.
func TestRedisPendingStore_Update_MultipleFields(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	req := makePendingReq("req-1", "user-1", "device-1", "ping")
	req.Seq = 42
	require.NoError(t, store.Save(ctx, req))

	req.RetryCount = 2
	require.NoError(t, store.Update(ctx, req))

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, 2, list[0].RetryCount)
	assert.Equal(t, uint64(42), list[0].Seq)
	assert.Equal(t, "ping", list[0].Method)
	assert.Equal(t, "user-1", list[0].UserID)
	assert.Equal(t, "device-1", list[0].DeviceID)
}

// TestRedisPendingStore_Update_NonExistent verifies no-op for unknown ID.
func TestRedisPendingStore_Update_NonExistent(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, makePendingReq("req-1", "user-1", "device-1", "ping")))

	// Update a non-existent request — should be no-op.
	unknown := makePendingReq("req-unknown", "user-1", "device-1", "ping")
	assert.NoError(t, store.Update(ctx, unknown))

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "req-1", list[0].ID)
}

// TestRedisPendingStore_Update_PreservesOrder verifies order is maintained.
func TestRedisPendingStore_Update_PreservesOrder(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	for _, id := range []string{"req-1", "req-2", "req-3"} {
		require.NoError(t, store.Save(ctx, makePendingReq(id, "user-1", "device-1", "method")))
	}

	// Update the middle entry.
	req2 := makePendingReq("req-2", "user-1", "device-1", "method")
	req2.RetryCount = 5
	require.NoError(t, store.Update(ctx, req2))

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, "req-1", list[0].ID)
	assert.Equal(t, "req-2", list[1].ID)
	assert.Equal(t, 5, list[1].RetryCount)
	assert.Equal(t, "req-3", list[2].ID)
}

// TestRedisPendingStore_Update_RedisDown verifies error when Redis is down.
func TestRedisPendingStore_Update_RedisDown(t *testing.T) {
	store, mr := setupTestRedisPendingStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, makePendingReq("req-1", "user-1", "device-1", "ping")))

	mr.Close()

	req := makePendingReq("req-1", "user-1", "device-1", "ping")
	req.RetryCount = 1
	err := store.Update(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pending store")
}

// TestRedisPendingStore_Update_Concurrent verifies race safety.
// Note: Update uses a non-atomic read-filter-rewrite pattern (same as Remove),
// so concurrent Updates may lose data. This test verifies no panics or races
// occur under -race, not strict data integrity (which is acceptable per D-103 fail-open).
func TestRedisPendingStore_Update_Concurrent(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	ctx := context.Background()

	for i := range 3 {
		id := fmt.Sprintf("req-%d", i+1)
		require.NoError(t, store.Save(ctx, makePendingReq(id, "user-1", "device-1", "method")))
	}

	const n = 10
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := makePendingReq("req-2", "user-1", "device-1", "method")
			req.RetryCount = idx
			// May or may not succeed depending on concurrent rewrites.
			_ = store.Update(ctx, req)
		}(i)
	}
	wg.Wait()

	// Verify no panic occurred and the store is still usable.
	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	assert.NotEmpty(t, list, "store should still have entries after concurrent updates")
}

// TestRedisPendingStore_Update_SkipsCorrupted verifies corrupted entries are skipped.
func TestRedisPendingStore_Update_SkipsCorrupted(t *testing.T) {
	store, mr := setupTestRedisPendingStore(t)
	ctx := context.Background()

	key := "pending:user-1\x00device-1"

	goodReq := makePendingReq("req-good", "user-1", "device-1", "ping")
	goodData, err := json.Marshal(goodReq)
	require.NoError(t, err)

	mr.RPush(key, string(goodData))
	mr.RPush(key, "{corrupted-json")

	// Update the good entry.
	goodReq.RetryCount = 3
	require.NoError(t, store.Update(ctx, goodReq))

	list, err := store.List(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.Len(t, list, 1, "corrupted entry should be removed during rewrite")
	assert.Equal(t, "req-good", list[0].ID)
	assert.Equal(t, 3, list[0].RetryCount)
}

// TestRedisPendingStore_Update_TTLRefresh verifies TTL is refreshed on Update.
func TestRedisPendingStore_Update_TTLRefresh(t *testing.T) {
	store, mr := setupTestRedisPendingStore(t)
	ctx := context.Background()

	req := makePendingReq("req-1", "user-1", "device-1", "ping")
	require.NoError(t, store.Save(ctx, req))

	key := "pending:user-1\x00device-1"

	// Advance 23 hours (just under 24h TTL).
	mr.FastForward(23 * time.Hour)
	assert.True(t, mr.Exists(key))

	// Update should refresh TTL.
	req.RetryCount = 1
	require.NoError(t, store.Update(ctx, req))

	// Advance another 23 hours (total 46h from save, 23h from update).
	mr.FastForward(23 * time.Hour)
	assert.True(t, mr.Exists(key), "key should still exist because Update refreshed TTL")

	// Advance 2 more hours (total 48h from save, 25h from update).
	mr.FastForward(2 * time.Hour)
	assert.False(t, mr.Exists(key), "key should have expired after refreshed TTL")
}
