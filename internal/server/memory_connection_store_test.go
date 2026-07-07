package server

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// MemoryConnectionStore - CRUD tests
// ---------------------------------------------------------------------------

func TestMemoryConnectionStore_Add(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	err := cs.Add(ctx, info)
	require.NoError(t, err)
	assert.False(t, info.CreatedAt.IsZero())
	assert.False(t, info.UpdatedAt.IsZero())
}

func TestMemoryConnectionStore_Add_NilInfo(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	err := cs.Add(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection info is nil")
}

func TestMemoryConnectionStore_Add_EmptyID(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	err := cs.Add(context.Background(), &ConnectionInfo{UserID: "u1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection ID is required")
}

func TestMemoryConnectionStore_Add_EmptyUserID(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	err := cs.Add(context.Background(), &ConnectionInfo{ID: "c1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

func TestMemoryConnectionStore_Add_OverwritesExisting(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	info.Metadata = map[string]string{"version": "1"}
	require.NoError(t, cs.Add(ctx, info))

	info2 := newTestConnection("conn-1", "user-1")
	info2.Metadata = map[string]string{"version": "2"}
	require.NoError(t, cs.Add(ctx, info2))

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "2", got.Metadata["version"])
}

func TestMemoryConnectionStore_Add_MaxConnectionsExceeded(t *testing.T) {
	cs := NewMemoryConnectionStore(2)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))

	err := cs.Add(ctx, newTestConnection("conn-3", "user-1"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMaxConnectionsExceeded)

	// Overwriting should still work.
	info := newTestConnection("conn-1", "user-1")
	info.Metadata = map[string]string{"updated": "true"}
	err = cs.Add(ctx, info)
	assert.NoError(t, err)
}

func TestMemoryConnectionStore_Add_UserIDChange_CleansOldUserSet(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-A")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-B")))

	// Overwrite conn-1 with user-B.
	info := newTestConnection("conn-1", "user-B")
	require.NoError(t, cs.Add(ctx, info))

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "user-B", got.UserID)

	// user-A should have 0 connections.
	count, _ := cs.CountByUser(ctx, "user-A")
	assert.Equal(t, int64(0), count)
}

func TestMemoryConnectionStore_Get(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	info.Metadata = map[string]string{"ip": "127.0.0.1"}
	require.NoError(t, cs.Add(ctx, info))

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "conn-1", got.ID)
	assert.Equal(t, "127.0.0.1", got.Metadata["ip"])
}

func TestMemoryConnectionStore_Get_NotFound(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	_, err := cs.Get(context.Background(), "nonexistent")
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestMemoryConnectionStore_Remove(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Remove(ctx, "conn-1"))

	_, err := cs.Get(ctx, "conn-1")
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestMemoryConnectionStore_Remove_Nonexistent(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	err := cs.Remove(context.Background(), "nonexistent")
	require.NoError(t, err)
}

func TestMemoryConnectionStore_Exists(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	exists, err := cs.Exists(ctx, "conn-1")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = cs.Exists(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestMemoryConnectionStore_Update(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	info.Metadata = map[string]string{"platform": "ios"}
	require.NoError(t, cs.Add(ctx, info))

	require.NoError(t, cs.Update(ctx, "conn-1", map[string]string{"platform": "android"}))

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "android", got.Metadata["platform"])
}

func TestMemoryConnectionStore_Update_NotFound(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	err := cs.Update(context.Background(), "nonexistent", nil)
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestMemoryConnectionStore_Patch(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))

	err := cs.Patch(ctx, "conn-1", func(ci *ConnectionInfo) {
		ci.Status = "idle"
		ci.Metadata["patched"] = "true"
	})
	require.NoError(t, err)

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "idle", got.Status)
	assert.Equal(t, "true", got.Metadata["patched"])
}

func TestMemoryConnectionStore_Patch_NilUpdater(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))

	err := cs.Patch(ctx, "conn-1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "updater function is nil")
}

func TestMemoryConnectionStore_Refresh(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Refresh(ctx, "conn-1"))

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestMemoryConnectionStore_Refresh_NotFound(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	err := cs.Refresh(context.Background(), "nonexistent")
	assert.Equal(t, ErrConnectionNotFound, err)
}

// ---------------------------------------------------------------------------
// MemoryConnectionStore - user-level operations
// ---------------------------------------------------------------------------

func TestMemoryConnectionStore_ListByUser(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-3", "user-2")))

	conns, err := cs.ListByUser(ctx, "user-1", -1)
	require.NoError(t, err)
	assert.Len(t, conns, 2)
}

func TestMemoryConnectionStore_ListByUser_WithLimit(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, cs.Add(ctx, newTestConnection(fmt.Sprintf("conn-%d", i), "user-1")))
	}

	conns, err := cs.ListByUser(ctx, "user-1", 3)
	require.NoError(t, err)
	assert.Len(t, conns, 3)
}

func TestMemoryConnectionStore_ListByUser_NoConnections(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	conns, err := cs.ListByUser(context.Background(), "no-such-user", -1)
	require.NoError(t, err)
	assert.Empty(t, conns)
}

func TestMemoryConnectionStore_CountByUser(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))

	count, err := cs.CountByUser(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestMemoryConnectionStore_CountAll(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-2")))

	count, err := cs.CountAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestMemoryConnectionStore_RemoveByUser(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-3", "user-2")))

	removed, err := cs.RemoveByUser(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), removed)

	count, _ := cs.CountByUser(ctx, "user-1")
	assert.Equal(t, int64(0), count)

	// user-2 should be unaffected.
	count, _ = cs.CountByUser(ctx, "user-2")
	assert.Equal(t, int64(1), count)
}

func TestMemoryConnectionStore_Ping(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	assert.NoError(t, cs.Ping(context.Background()))
}

func TestMemoryConnectionStore_Close(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	assert.NoError(t, cs.Close())
}

// ---------------------------------------------------------------------------
// MemoryConnectionStore - Concurrency
// ---------------------------------------------------------------------------

func TestMemoryConnectionStore_ConcurrentAddGet(t *testing.T) {
	cs := NewMemoryConnectionStore(0)
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			connID := fmt.Sprintf("conn-%d", i)
			userID := fmt.Sprintf("user-%d", i%5)
			assert.NoError(t, cs.Add(ctx, newTestConnection(connID, userID)))
		}(i)
	}
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			connID := fmt.Sprintf("conn-%d", i)
			got, err := cs.Get(ctx, connID)
			assert.NoError(t, err)
			assert.Equal(t, connID, got.ID)
		}(i)
	}
	wg.Wait()

	for u := 0; u < 5; u++ {
		count, err := cs.CountByUser(ctx, fmt.Sprintf("user-%d", u))
		require.NoError(t, err)
		assert.Equal(t, int64(4), count)
	}
}
