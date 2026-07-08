package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncStateStore_Get_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	_, err := db.SyncStates.Get(ctx, "nonexistent-key")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSyncStateStore_Set_NewKey(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	require.NoError(t, db.SyncStates.Set(ctx, "my_key", "my_value"))

	val, err := db.SyncStates.Get(ctx, "my_key")
	require.NoError(t, err)
	assert.Equal(t, "my_value", val)
}

func TestSyncStateStore_Set_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	require.NoError(t, db.SyncStates.Set(ctx, "counter", "1"))
	val, err := db.SyncStates.Get(ctx, "counter")
	require.NoError(t, err)
	assert.Equal(t, "1", val)

	// UPSERT: update the value.
	require.NoError(t, db.SyncStates.Set(ctx, "counter", "2"))
	val, err = db.SyncStates.Get(ctx, "counter")
	require.NoError(t, err)
	assert.Equal(t, "2", val)

	// Update again.
	require.NoError(t, db.SyncStates.Set(ctx, "counter", "100"))
	val, err = db.SyncStates.Get(ctx, "counter")
	require.NoError(t, err)
	assert.Equal(t, "100", val)
}

func TestSyncStateStore_GetLocalMaxSeq_Initialize(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	// Before any Set, GetLocalMaxSeq should return 0.
	seq, err := db.SyncStates.GetLocalMaxSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), seq)
}

func TestSyncStateStore_SetLocalMaxSeq_GetLocalMaxSeq(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	require.NoError(t, db.SyncStates.SetLocalMaxSeq(ctx, 42))
	seq, err := db.SyncStates.GetLocalMaxSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), seq)

	// Update to a higher value.
	require.NoError(t, db.SyncStates.SetLocalMaxSeq(ctx, 100))
	seq, err = db.SyncStates.GetLocalMaxSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(100), seq)
}

func TestSyncStateStore_SetLatestSeq_GetLatestSeq(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	// Before any Set, should return 0.
	seq, err := db.SyncStates.GetLatestSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), seq)

	require.NoError(t, db.SyncStates.SetLatestSeq(ctx, 55))
	seq, err = db.SyncStates.GetLatestSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(55), seq)

	// Update.
	require.NoError(t, db.SyncStates.SetLatestSeq(ctx, 200))
	seq, err = db.SyncStates.GetLatestSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(200), seq)
}

func TestSyncStateStore_Set_MultipleKeys(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	require.NoError(t, db.SyncStates.Set(ctx, "key1", "val1"))
	require.NoError(t, db.SyncStates.Set(ctx, "key2", "val2"))
	require.NoError(t, db.SyncStates.Set(ctx, "key3", "val3"))

	val1, err := db.SyncStates.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "val1", val1)

	val2, err := db.SyncStates.Get(ctx, "key2")
	require.NoError(t, err)
	assert.Equal(t, "val2", val2)

	val3, err := db.SyncStates.Get(ctx, "key3")
	require.NoError(t, err)
	assert.Equal(t, "val3", val3)
}
