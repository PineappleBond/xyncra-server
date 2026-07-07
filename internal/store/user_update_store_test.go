package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers specific to user update tests
// ---------------------------------------------------------------------------

// createUserUpdates inserts count user updates for the given userID with
// sequential Seq values starting at 1. Each update has a unique ID derived
// from the prefix and index.
func createUserUpdates(t *testing.T, s *Store, ctx context.Context, userID, prefix string, count int) {
	t.Helper()
	updates := make([]model.UserUpdate, count)
	for i := 0; i < count; i++ {
		updates[i] = model.UserUpdate{
			ID:        fmt.Sprintf("%s-%d", prefix, i+1),
			UserID:    userID,
			Seq:       uint32(i + 1),
			Payload:   []byte(fmt.Sprintf(`{"idx":%d}`, i+1)),
			CreatedAt: testNow,
		}
	}
	require.NoError(t, s.UserUpdates.Create(ctx, updates),
		"creating %d user updates should succeed", count)
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// TestUserUpdateStore_Create_HappyPath verifies that Create inserts a batch
// of user updates and that all records are retrievable afterwards.
func TestUserUpdateStore_Create_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		updates := []model.UserUpdate{
			{ID: "uu-hp-1", UserID: "alice", Seq: 1, Payload: []byte(`{"type":"msg"}`), CreatedAt: testNow},
			{ID: "uu-hp-2", UserID: "alice", Seq: 2, Payload: []byte(`{"type":"read"}`), CreatedAt: testNow},
			{ID: "uu-hp-3", UserID: "bob", Seq: 1, Payload: []byte(`{"type":"msg"}`), CreatedAt: testNow},
		}

		err := s.UserUpdates.Create(ctx, updates)
		require.NoError(t, err, "Create should succeed for a valid batch")

		// Verify alice has 2 updates.
		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 100)
		require.NoError(t, err, "ListByUser should succeed after Create")
		assert.Len(t, got, 2, "alice should have 2 updates")

		// Verify bob has 1 update.
		gotBob, err := s.UserUpdates.ListByUser(ctx, "bob", 0, 100)
		require.NoError(t, err, "ListByUser for bob should succeed")
		assert.Len(t, gotBob, 1, "bob should have 1 update")
	})
}

// TestUserUpdateStore_Create_EmptySlice verifies that Create with an empty
// slice returns nil without executing a database operation.
func TestUserUpdateStore_Create_EmptySlice(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		err := s.UserUpdates.Create(ctx, []model.UserUpdate{})
		require.NoError(t, err, "Create with empty slice should return nil")

		// Also verify with nil slice.
		err = s.UserUpdates.Create(ctx, nil)
		require.NoError(t, err, "Create with nil slice should return nil")
	})
}

// TestUserUpdateStore_Create_LargeBatch verifies that Create correctly handles
// a batch larger than the internal batch size of 100, exercising the
// CreateInBatches code path with multiple chunks.
func TestUserUpdateStore_Create_LargeBatch(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		const total = 250 // > 100, will require 3 batches (100 + 100 + 50)
		updates := make([]model.UserUpdate, total)
		for i := 0; i < total; i++ {
			updates[i] = model.UserUpdate{
				ID:        fmt.Sprintf("uu-lb-%d", i+1),
				UserID:    "alice",
				Seq:       uint32(i + 1),
				Payload:   []byte(`{}`),
				CreatedAt: testNow,
			}
		}

		err := s.UserUpdates.Create(ctx, updates)
		require.NoError(t, err, "Create with %d records should succeed", total)

		// Verify all records were inserted.
		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, total+10)
		require.NoError(t, err, "ListByUser should succeed after large batch Create")
		assert.Len(t, got, total, "all %d records should be retrievable", total)

		// Verify ordering: first record should have Seq=1, last should have Seq=total.
		assert.Equal(t, uint32(1), got[0].Seq, "first record should have Seq=1")
		assert.Equal(t, uint32(total), got[len(got)-1].Seq, "last record should have Seq=%d", total)
	})
}

// ---------------------------------------------------------------------------
// ListByUser
// ---------------------------------------------------------------------------

// TestUserUpdateStore_ListByUser_HappyPath verifies that ListByUser returns
// updates for the specified user ordered by Seq ascending.
func TestUserUpdateStore_ListByUser_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Insert updates with non-sequential Seq values to confirm ASC ordering.
		updates := []model.UserUpdate{
			{ID: "uu-list-1", UserID: "alice", Seq: 5, Payload: []byte(`{}`), CreatedAt: testNow},
			{ID: "uu-list-2", UserID: "alice", Seq: 1, Payload: []byte(`{}`), CreatedAt: testNow},
			{ID: "uu-list-3", UserID: "alice", Seq: 3, Payload: []byte(`{}`), CreatedAt: testNow},
			{ID: "uu-list-4", UserID: "bob", Seq: 1, Payload: []byte(`{}`), CreatedAt: testNow},
		}
		require.NoError(t, s.UserUpdates.Create(ctx, updates), "creating updates should succeed")

		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 100)
		require.NoError(t, err, "ListByUser should succeed")
		require.Len(t, got, 3, "alice should have 3 updates")

		// Verify Seq ASC ordering.
		assert.Equal(t, uint32(1), got[0].Seq, "first update should have Seq=1")
		assert.Equal(t, uint32(3), got[1].Seq, "second update should have Seq=3")
		assert.Equal(t, uint32(5), got[2].Seq, "third update should have Seq=5")
	})
}

// TestUserUpdateStore_ListByUser_AfterSeq verifies that the afterSeq cursor
// correctly filters out updates with Seq <= afterSeq.
func TestUserUpdateStore_ListByUser_AfterSeq(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createUserUpdates(t, s, ctx, "alice", "uu-as", 10)

		// afterSeq=5 should return only updates with Seq > 5 (i.e. Seq 6..10).
		got, err := s.UserUpdates.ListByUser(ctx, "alice", 5, 100)
		require.NoError(t, err, "ListByUser with afterSeq=5 should succeed")
		require.Len(t, got, 5, "should return 5 updates with Seq > 5")
		assert.Equal(t, uint32(6), got[0].Seq, "first returned update should have Seq=6")
		assert.Equal(t, uint32(10), got[4].Seq, "last returned update should have Seq=10")

		// afterSeq=0 should return all updates.
		gotAll, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 100)
		require.NoError(t, err, "ListByUser with afterSeq=0 should succeed")
		assert.Len(t, gotAll, 10, "afterSeq=0 should return all updates")

		// afterSeq beyond the max Seq should return empty.
		gotNone, err := s.UserUpdates.ListByUser(ctx, "alice", 100, 100)
		require.NoError(t, err, "ListByUser with afterSeq beyond max should succeed")
		assert.Empty(t, gotNone, "afterSeq beyond max should return empty slice")
	})
}

// TestUserUpdateStore_ListByUser_NonExistentUser verifies that ListByUser
// returns an empty slice (not an error) when the user has no updates.
func TestUserUpdateStore_ListByUser_NonExistentUser(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		got, err := s.UserUpdates.ListByUser(ctx, "nonexistent-user", 0, 100)
		require.NoError(t, err, "ListByUser for non-existent user should not return an error")
		assert.Empty(t, got, "non-existent user should have no updates")
	})
}

// TestUserUpdateStore_ListByUser_LimitZero verifies that limit=0 falls back
// to the default limit of 100.
func TestUserUpdateStore_ListByUser_LimitZero(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create 110 updates (more than the default limit of 100).
		createUserUpdates(t, s, ctx, "alice", "uu-lz", 110)

		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 0)
		require.NoError(t, err, "ListByUser with limit=0 should succeed")
		assert.Len(t, got, 100, "limit=0 should fall back to default of 100")
	})
}

// TestUserUpdateStore_ListByUser_LimitTooLarge verifies that a limit exceeding
// 1000 is capped at the default of 100.
func TestUserUpdateStore_ListByUser_LimitTooLarge(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create 150 updates.
		createUserUpdates(t, s, ctx, "alice", "uu-lt", 150)

		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 5000)
		require.NoError(t, err, "ListByUser with limit>1000 should succeed")
		assert.Len(t, got, 100, "limit>1000 should be capped at default 100")
	})
}

// ---------------------------------------------------------------------------
// GetLatestSeq
// ---------------------------------------------------------------------------

// TestUserUpdateStore_GetLatestSeq_HappyPath verifies that GetLatestSeq
// returns the maximum Seq value for a user that has updates.
func TestUserUpdateStore_GetLatestSeq_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		updates := []model.UserUpdate{
			{ID: "uu-seq-1", UserID: "alice", Seq: 3, Payload: []byte(`{}`), CreatedAt: testNow},
			{ID: "uu-seq-2", UserID: "alice", Seq: 7, Payload: []byte(`{}`), CreatedAt: testNow},
			{ID: "uu-seq-3", UserID: "alice", Seq: 5, Payload: []byte(`{}`), CreatedAt: testNow},
			{ID: "uu-seq-4", UserID: "bob", Seq: 42, Payload: []byte(`{}`), CreatedAt: testNow},
		}
		require.NoError(t, s.UserUpdates.Create(ctx, updates), "creating updates should succeed")

		seq, err := s.UserUpdates.GetLatestSeq(ctx, "alice")
		require.NoError(t, err, "GetLatestSeq should succeed for existing user")
		assert.Equal(t, uint32(7), seq, "alice's latest seq should be 7 (the maximum)")

		// Verify bob's seq is isolated.
		seqBob, err := s.UserUpdates.GetLatestSeq(ctx, "bob")
		require.NoError(t, err, "GetLatestSeq should succeed for bob")
		assert.Equal(t, uint32(42), seqBob, "bob's latest seq should be 42")
	})
}

// TestUserUpdateStore_GetLatestSeq_NoRecords verifies that GetLatestSeq
// returns 0 (not an error) when the user has no update records.
func TestUserUpdateStore_GetLatestSeq_NoRecords(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		seq, err := s.UserUpdates.GetLatestSeq(ctx, "nobody")
		require.NoError(t, err, "GetLatestSeq should not return an error for non-existent user")
		assert.Equal(t, uint32(0), seq, "non-existent user should have latest seq 0")
	})
}

// ---------------------------------------------------------------------------
// CleanupExpiredBefore
// ---------------------------------------------------------------------------

// TestUserUpdateStore_CleanupExpiredBefore_HappyPath verifies that
// CleanupExpiredBefore hard-deletes records with CreatedAt strictly before the
// cutoff and leaves the rest intact.
func TestUserUpdateStore_CleanupExpiredBefore_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		oldTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		newTime := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

		updates := []model.UserUpdate{
			{ID: "clean-hp-1", UserID: "alice", Seq: 1, Payload: []byte(`{}`), CreatedAt: oldTime},
			{ID: "clean-hp-2", UserID: "alice", Seq: 2, Payload: []byte(`{}`), CreatedAt: oldTime},
			{ID: "clean-hp-3", UserID: "alice", Seq: 3, Payload: []byte(`{}`), CreatedAt: newTime},
		}
		require.NoError(t, s.UserUpdates.Create(ctx, updates), "creating updates should succeed")

		cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		deleted, err := s.UserUpdates.CleanupExpiredBefore(ctx, cutoff)
		require.NoError(t, err, "CleanupExpiredBefore should succeed")
		assert.Equal(t, int64(2), deleted, "should delete 2 records with CreatedAt < cutoff")

		// Verify the new record survives.
		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 100)
		require.NoError(t, err, "ListByUser after cleanup should succeed")
		require.Len(t, got, 1, "only 1 record should remain")
		assert.Equal(t, uint32(3), got[0].Seq, "remaining record should be the one with Seq=3")
	})
}

// TestUserUpdateStore_CleanupExpiredBefore_CustomTime verifies that
// CleanupExpiredBefore respects an arbitrary caller-supplied cutoff time and
// returns 0 when no records match.
func TestUserUpdateStore_CleanupExpiredBefore_CustomTime(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		t2 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		t3 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

		updates := []model.UserUpdate{
			{ID: "clean-ct-1", UserID: "alice", Seq: 1, Payload: []byte(`{}`), CreatedAt: t1},
			{ID: "clean-ct-2", UserID: "alice", Seq: 2, Payload: []byte(`{}`), CreatedAt: t2},
			{ID: "clean-ct-3", UserID: "alice", Seq: 3, Payload: []byte(`{}`), CreatedAt: t3},
		}
		require.NoError(t, s.UserUpdates.Create(ctx, updates), "creating updates should succeed")

		// Cutoff at 2026-04-01: should delete t1 and t2 but not t3.
		cutoff := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
		deleted, err := s.UserUpdates.CleanupExpiredBefore(ctx, cutoff)
		require.NoError(t, err, "CleanupExpiredBefore with custom cutoff should succeed")
		assert.Equal(t, int64(2), deleted, "should delete 2 records before 2026-04-01")

		// Verify remaining.
		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 100)
		require.NoError(t, err, "ListByUser after custom cleanup should succeed")
		require.Len(t, got, 1, "only 1 record should remain")
		assert.Equal(t, uint32(3), got[0].Seq, "remaining record should have Seq=3")

		// Cleanup again with a cutoff before all remaining records: should delete 0.
		deleted2, err := s.UserUpdates.CleanupExpiredBefore(ctx, time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err, "second CleanupExpiredBefore should succeed")
		assert.Equal(t, int64(0), deleted2, "no records should match the new cutoff")
	})
}

// ---------------------------------------------------------------------------
// CleanupExpired
// ---------------------------------------------------------------------------

// TestUserUpdateStore_CleanupExpired_Default30Days verifies that CleanupExpired
// uses the default 30-day retention: records older than 30 days are deleted
// while recent records are preserved.
func TestUserUpdateStore_CleanupExpired_Default30Days(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		now := time.Now().UTC()
		expired := now.Add(-60 * 24 * time.Hour)  // 60 days ago: beyond 30-day retention
		recent := now.Add(-10 * 24 * time.Hour)   // 10 days ago: within retention
		boundary := now.Add(-31 * 24 * time.Hour) // 31 days ago: just beyond retention

		updates := []model.UserUpdate{
			{ID: "clean-exp-1", UserID: "alice", Seq: 1, Payload: []byte(`{}`), CreatedAt: expired},
			{ID: "clean-exp-2", UserID: "alice", Seq: 2, Payload: []byte(`{}`), CreatedAt: boundary},
			{ID: "clean-exp-3", UserID: "alice", Seq: 3, Payload: []byte(`{}`), CreatedAt: recent},
		}
		require.NoError(t, s.UserUpdates.Create(ctx, updates), "creating updates should succeed")

		deleted, err := s.UserUpdates.CleanupExpired(ctx)
		require.NoError(t, err, "CleanupExpired should succeed")
		assert.Equal(t, int64(2), deleted, "should delete 2 records older than 30 days")

		// Only the recent record should remain.
		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 100)
		require.NoError(t, err, "ListByUser after CleanupExpired should succeed")
		require.Len(t, got, 1, "only 1 recent record should remain")
		assert.Equal(t, uint32(3), got[0].Seq, "remaining record should have Seq=3")
	})
}

// TestUserUpdateStore_CleanupExpired_NoExpiredRecords verifies that
// CleanupExpired returns 0 and no error when all records are within the
// retention period.
func TestUserUpdateStore_CleanupExpired_NoExpiredRecords(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		now := time.Now().UTC()
		recent := now.Add(-5 * 24 * time.Hour) // 5 days ago: well within 30-day retention

		updates := []model.UserUpdate{
			{ID: "clean-ne-1", UserID: "alice", Seq: 1, Payload: []byte(`{}`), CreatedAt: recent},
			{ID: "clean-ne-2", UserID: "alice", Seq: 2, Payload: []byte(`{}`), CreatedAt: recent},
		}
		require.NoError(t, s.UserUpdates.Create(ctx, updates), "creating updates should succeed")

		deleted, err := s.UserUpdates.CleanupExpired(ctx)
		require.NoError(t, err, "CleanupExpired should succeed when no records are expired")
		assert.Equal(t, int64(0), deleted, "no records should be deleted")

		// All records should still exist.
		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 100)
		require.NoError(t, err, "ListByUser after CleanupExpired should succeed")
		assert.Len(t, got, 2, "all records should still exist")
	})
}
