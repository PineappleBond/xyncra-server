package store

import (
	"context"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserUpdateStore_Create_BatchInsert(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	userID := uid()
	updates := []model.UserUpdate{
		{ID: uid(), UserID: userID, Seq: 1, Type: "message", CreatedAt: time.Now()},
		{ID: uid(), UserID: userID, Seq: 2, Type: "message", CreatedAt: time.Now()},
		{ID: uid(), UserID: userID, Seq: 3, Type: "presence", CreatedAt: time.Now()},
	}

	require.NoError(t, db.UserUpdates.Create(ctx, updates))

	got, err := db.UserUpdates.ListByUser(ctx, userID, 0, 10)
	require.NoError(t, err)
	require.Len(t, got, 3)
	// Should be ordered by Seq ASC.
	assert.Equal(t, uint32(1), got[0].Seq)
	assert.Equal(t, uint32(2), got[1].Seq)
	assert.Equal(t, uint32(3), got[2].Seq)
}

func TestUserUpdateStore_Create_EmptySlice(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	// Empty slice should be a no-op.
	require.NoError(t, db.UserUpdates.Create(ctx, []model.UserUpdate{}))

	// Nil slice should also be a no-op.
	require.NoError(t, db.UserUpdates.Create(ctx, nil))
}

func TestUserUpdateStore_ListByUser_OrderAndLimit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	userID := uid()
	updates := make([]model.UserUpdate, 10)
	for i := 0; i < 10; i++ {
		updates[i] = model.UserUpdate{
			ID:        uid(),
			UserID:    userID,
			Seq:       uint32(i + 1),
			Type:      "message",
			CreatedAt: time.Now(),
		}
	}
	require.NoError(t, db.UserUpdates.Create(ctx, updates))

	// List all after seq=0.
	got, err := db.UserUpdates.ListByUser(ctx, userID, 0, 100)
	require.NoError(t, err)
	require.Len(t, got, 10)
	assert.Equal(t, uint32(1), got[0].Seq)
	assert.Equal(t, uint32(10), got[9].Seq)

	// List after seq=5 → should get seqs 6..10.
	got, err = db.UserUpdates.ListByUser(ctx, userID, 5, 100)
	require.NoError(t, err)
	require.Len(t, got, 5)
	assert.Equal(t, uint32(6), got[0].Seq)
	assert.Equal(t, uint32(10), got[4].Seq)

	// Limit=3 after seq=0 → should get seqs 1..3.
	got, err = db.UserUpdates.ListByUser(ctx, userID, 0, 3)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, uint32(1), got[0].Seq)
	assert.Equal(t, uint32(3), got[2].Seq)

	// Other user should get nothing.
	got, err = db.UserUpdates.ListByUser(ctx, "other-user", 0, 100)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestUserUpdateStore_ListByUserRange(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	userID := uid()
	updates := make([]model.UserUpdate, 10)
	for i := 0; i < 10; i++ {
		updates[i] = model.UserUpdate{
			ID:        uid(),
			UserID:    userID,
			Seq:       uint32(i + 1),
			Type:      "message",
			CreatedAt: time.Now(),
		}
	}
	require.NoError(t, db.UserUpdates.Create(ctx, updates))

	// Range (3, 7] → seqs 4, 5, 6, 7.
	got, err := db.UserUpdates.ListByUserRange(ctx, userID, 3, 7)
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, uint32(4), got[0].Seq)
	assert.Equal(t, uint32(7), got[3].Seq)

	// Range (0, 2] → seqs 1, 2.
	got, err = db.UserUpdates.ListByUserRange(ctx, userID, 0, 2)
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Range where maxSeq <= afterSeq → nil.
	got, err = db.UserUpdates.ListByUserRange(ctx, userID, 5, 3)
	require.NoError(t, err)
	assert.Nil(t, got)

	// Range where maxSeq == afterSeq → nil.
	got, err = db.UserUpdates.ListByUserRange(ctx, userID, 5, 5)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestUserUpdateStore_GetLatestSeq(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	userID := uid()
	updates := []model.UserUpdate{
		{ID: uid(), UserID: userID, Seq: 5, Type: "message", CreatedAt: time.Now()},
		{ID: uid(), UserID: userID, Seq: 10, Type: "message", CreatedAt: time.Now()},
		{ID: uid(), UserID: userID, Seq: 3, Type: "message", CreatedAt: time.Now()},
	}
	require.NoError(t, db.UserUpdates.Create(ctx, updates))

	seq, err := db.UserUpdates.GetLatestSeq(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, uint32(10), seq)
}

func TestUserUpdateStore_GetLatestSeq_Empty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	seq, err := db.UserUpdates.GetLatestSeq(ctx, "nonexistent-user")
	require.NoError(t, err)
	assert.Equal(t, uint32(0), seq)
}

func TestUserUpdateStore_CleanupExpiredBefore(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	userID := uid()
	now := time.Now()
	updates := []model.UserUpdate{
		{ID: uid(), UserID: userID, Seq: 1, Type: "message", CreatedAt: now.Add(-48 * time.Hour)},
		{ID: uid(), UserID: userID, Seq: 2, Type: "message", CreatedAt: now.Add(-24 * time.Hour)},
		{ID: uid(), UserID: userID, Seq: 3, Type: "message", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: uid(), UserID: userID, Seq: 4, Type: "message", CreatedAt: now},
	}
	require.NoError(t, db.UserUpdates.Create(ctx, updates))

	// Delete everything older than 25h ago → seqs 1 should be deleted.
	before := now.Add(-25 * time.Hour)
	deleted, err := db.UserUpdates.CleanupExpiredBefore(ctx, before)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Remaining should be seqs 2, 3, 4.
	remaining, err := db.UserUpdates.ListByUser(ctx, userID, 0, 100)
	require.NoError(t, err)
	require.Len(t, remaining, 3)
	assert.Equal(t, uint32(2), remaining[0].Seq)
}

func TestUserUpdateStore_CleanupExpired(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	userID := uid()
	now := time.Now()

	// Create one record that's 31 days old (should be cleaned up by default 30-day retention).
	oldUpdate := model.UserUpdate{
		ID:        uid(),
		UserID:    userID,
		Seq:       1,
		Type:      "message",
		CreatedAt: now.Add(-31 * 24 * time.Hour),
	}
	// Create one recent record.
	recentUpdate := model.UserUpdate{
		ID:        uid(),
		UserID:    userID,
		Seq:       2,
		Type:      "message",
		CreatedAt: now,
	}
	require.NoError(t, db.UserUpdates.Create(ctx, []model.UserUpdate{oldUpdate, recentUpdate}))

	deleted, err := db.UserUpdates.CleanupExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Only the recent one should remain.
	remaining, err := db.UserUpdates.ListByUser(ctx, userID, 0, 100)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, uint32(2), remaining[0].Seq)
}
