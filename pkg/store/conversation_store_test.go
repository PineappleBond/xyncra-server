package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationStore_Create_Success(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "Test Conv")
	err := db.Conversations.Create(ctx, conv)
	require.NoError(t, err)

	got, err := db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, got.ID)
	assert.Equal(t, conv.Title, got.Title)
	assert.Equal(t, conv.UserID1, got.UserID1)
	assert.Equal(t, conv.UserID2, got.UserID2)
}

func TestConversationStore_Create_DuplicateKey(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv1 := newTestConv(uid(), "userA", "userB", "direct", "Conv 1")
	require.NoError(t, db.Conversations.Create(ctx, conv1))

	// Same unique (user_id1, user_id2) pair → unique constraint violation.
	conv2 := newTestConv(uid(), "userA", "userB", "direct", "Conv 2")
	err := db.Conversations.Create(ctx, conv2)
	assert.ErrorIs(t, err, ErrDuplicateKey)
}

func TestConversationStore_Get_Found(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "Hello")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	got, err := db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, got.ID)
	assert.Equal(t, "Hello", got.Title)
}

func TestConversationStore_Get_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	_, err := db.Conversations.Get(ctx, "nonexistent-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestConversationStore_GetByUsers_BothOrderings(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), "alice", "bob", "direct", "Alice-Bob")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// Query with (alice, bob).
	got, err := db.Conversations.GetByUsers(ctx, "alice", "bob")
	require.NoError(t, err)
	assert.Equal(t, conv.ID, got.ID)

	// Query with (bob, alice) — reversed order.
	got, err = db.Conversations.GetByUsers(ctx, "bob", "alice")
	require.NoError(t, err)
	assert.Equal(t, conv.ID, got.ID)
}

func TestConversationStore_GetByUser_OrderAndPagination(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	userA := uid()
	userB := uid()

	now := time.Now()
	// Create 3 conversations with different LastMessageAt.
	conv1 := newTestConv(uid(), userA, userB, "direct", "Old")
	conv1.LastMessageAt = now.Add(-3 * time.Hour)
	conv2 := newTestConv(uid(), userA, uid(), "direct", "Mid")
	conv2.LastMessageAt = now.Add(-1 * time.Hour)
	conv3 := newTestConv(uid(), uid(), userA, "direct", "Recent")
	conv3.LastMessageAt = now

	require.NoError(t, db.Conversations.Create(ctx, conv1))
	require.NoError(t, db.Conversations.Create(ctx, conv2))
	require.NoError(t, db.Conversations.Create(ctx, conv3))

	// Get first page: limit=2.
	results, err := db.Conversations.GetByUser(ctx, userA, 0, 2)
	require.NoError(t, err)
	require.Len(t, results, 2)
	// Should be ordered by LastMessageAt DESC: Recent, Mid.
	assert.Equal(t, conv3.ID, results[0].ID)
	assert.Equal(t, conv2.ID, results[1].ID)

	// Get second page: offset=2, limit=2.
	results, err = db.Conversations.GetByUser(ctx, userA, 2, 2)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, conv1.ID, results[0].ID)

	// Soft-delete conv2 and verify it's excluded.
	require.NoError(t, db.Conversations.Delete(ctx, conv2.ID))
	results, err = db.Conversations.GetByUser(ctx, userA, 0, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, r := range results {
		assert.NotEqual(t, conv2.ID, r.ID)
	}
}

func TestConversationStore_Update_Success(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "Original")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	conv.Title = "Updated"
	conv.Pinned = true
	require.NoError(t, db.Conversations.Update(ctx, conv))

	got, err := db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", got.Title)
	assert.True(t, got.Pinned)
}

func TestConversationStore_Delete_CascadeSoftDelete(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "ToDelete")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// Create messages in this conversation.
	msg1 := newTestMsg(uid(), uid(), conv.ID, 1, "sender", "msg1")
	msg2 := newTestMsg(uid(), uid(), conv.ID, 2, "sender", "msg2")
	require.NoError(t, db.Messages.Create(ctx, msg1))
	require.NoError(t, db.Messages.Create(ctx, msg2))

	// Delete the conversation (D-013: cascade soft-delete).
	require.NoError(t, db.Conversations.Delete(ctx, conv.ID))

	// Conversation should not be found via regular Get.
	_, err := db.Conversations.Get(ctx, conv.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	// But should be found via GetUnscoped.
	unscoped, err := db.Conversations.GetUnscoped(ctx, conv.ID)
	require.NoError(t, err)
	assert.NotNil(t, unscoped.DeletedAt)

	// Messages should also be soft-deleted.
	_, err = db.Messages.Get(ctx, msg1.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = db.Messages.Get(ctx, msg2.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestConversationStore_Delete_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	err := db.Conversations.Delete(ctx, "nonexistent-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestConversationStore_Restore_CascadeRestore(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "ToRestore")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg1 := newTestMsg(uid(), uid(), conv.ID, 1, "sender", "msg1")
	msg2 := newTestMsg(uid(), uid(), conv.ID, 2, "sender", "msg2")
	require.NoError(t, db.Messages.Create(ctx, msg1))
	require.NoError(t, db.Messages.Create(ctx, msg2))

	// Delete then restore (D-015: cascade restore).
	require.NoError(t, db.Conversations.Delete(ctx, conv.ID))
	require.NoError(t, db.Conversations.Restore(ctx, conv.ID))

	// Conversation should be back.
	got, err := db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, got.ID)
	assert.True(t, got.DeletedAt.Time.IsZero())

	// Messages should also be restored.
	gotMsg1, err := db.Messages.Get(ctx, msg1.ID)
	require.NoError(t, err)
	assert.True(t, gotMsg1.DeletedAt.Time.IsZero())

	gotMsg2, err := db.Messages.Get(ctx, msg2.ID)
	require.NoError(t, err)
	assert.True(t, gotMsg2.DeletedAt.Time.IsZero())
}

func TestConversationStore_Restore_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	err := db.Conversations.Restore(ctx, "nonexistent-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestConversationStore_UpdateLastRead_MaxSemantics(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	uid1 := uid()
	uid2 := uid()
	conv := newTestConv(uid(), uid1, uid2, "direct", "ReadTest")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// Set last read to 5 for uid1.
	require.NoError(t, db.Conversations.UpdateLastRead(ctx, conv.ID, uid1, 5))

	got, err := db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), got.LastReadMessageID1)

	// Try to set to 3 — should stay at 5 (MAX semantics, D-012).
	require.NoError(t, db.Conversations.UpdateLastRead(ctx, conv.ID, uid1, 3))

	got, err = db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), got.LastReadMessageID1)

	// Set to 10 — should advance to 10.
	require.NoError(t, db.Conversations.UpdateLastRead(ctx, conv.ID, uid1, 10))

	got, err = db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, uint32(10), got.LastReadMessageID1)

	// Test uid2 column.
	require.NoError(t, db.Conversations.UpdateLastRead(ctx, conv.ID, uid2, 7))
	got, err = db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, uint32(7), got.LastReadMessageID2)
}

func TestConversationStore_UpdateLastRead_NotMember(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "ReadTest")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	err := db.Conversations.UpdateLastRead(ctx, conv.ID, "non-member-user", 5)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestConversationStore_SearchByTitle_LikeEscaping(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	user := uid()
	// Create conversations with special LIKE characters in titles.
	conv100pct := newTestConv(uid(), user, uid(), "direct", "100% done")
	conv1001 := newTestConv(uid(), user, uid(), "direct", "1001 items")
	convA_B := newTestConv(uid(), user, uid(), "direct", "a_b test")
	convACB := newTestConv(uid(), user, uid(), "direct", "acb test")
	convPipe := newTestConv(uid(), user, uid(), "direct", "pipe|char")

	require.NoError(t, db.Conversations.Create(ctx, conv100pct))
	require.NoError(t, db.Conversations.Create(ctx, conv1001))
	require.NoError(t, db.Conversations.Create(ctx, convA_B))
	require.NoError(t, db.Conversations.Create(ctx, convACB))
	require.NoError(t, db.Conversations.Create(ctx, convPipe))

	// Search for "100%" — should only match "100% done", not "1001 items".
	results, err := db.Conversations.SearchByTitle(ctx, user, "100%", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, conv100pct.ID, results[0].ID)

	// Search for "a_b" — should only match "a_b test", not "acb test".
	results, err = db.Conversations.SearchByTitle(ctx, user, "a_b", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, convA_B.ID, results[0].ID)

	// Search for "pipe|char" — should match the pipe conversation.
	results, err = db.Conversations.SearchByTitle(ctx, user, "pipe|char", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, convPipe.ID, results[0].ID)

	// Search for empty string returns empty.
	results, err = db.Conversations.SearchByTitle(ctx, user, "", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestConversationStore_GetUnscoped_IncludesSoftDeleted(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "SoftDel")
	require.NoError(t, db.Conversations.Create(ctx, conv))
	require.NoError(t, db.Conversations.Delete(ctx, conv.ID))

	// Regular Get should fail.
	_, err := db.Conversations.Get(ctx, conv.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	// GetUnscoped should succeed and include the soft-deleted record.
	got, err := db.Conversations.GetUnscoped(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, got.ID)
	assert.NotNil(t, got.DeletedAt)
	assert.False(t, got.DeletedAt.Time.IsZero())
}

func TestConversationStore_UpdateLastMessage(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "LastMsg")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	newTime := time.Now().Add(1 * time.Hour)
	require.NoError(t, db.Conversations.UpdateLastMessage(ctx, conv.ID, newTime, 42))

	got, err := db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), got.LastProcessedMessageID)
	// LastMessageAt may have slight precision loss due to SQLite storage.
	assert.WithinDuration(t, newTime, got.LastMessageAt, time.Second)
}

func TestConversationStore_UpdateLastMessage_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	err := db.Conversations.UpdateLastMessage(ctx, "nonexistent", time.Now(), 1)
	assert.ErrorIs(t, err, ErrNotFound)
}

// Verify that ConversationStore errors wrap correctly.
func TestConversationStore_ErrorWrapping(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	_, err := db.Conversations.Get(ctx, "nonexistent")
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestConversationStore_GetByUsers_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	_, err := db.Conversations.GetByUsers(ctx, uid(), uid())
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestConversationStore_UpdateLastRead_ConversationNotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	err := db.Conversations.UpdateLastRead(ctx, "nonexistent-conv-id", uid(), 5)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestConversationStore_Restore_NotDeleted(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "NotDeleted")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// Restore on a conversation that is NOT deleted is idempotent (D-015):
	// returns nil without error.
	err := db.Conversations.Restore(ctx, conv.ID)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Upsert tests (Bug #5 — D-045)
// ---------------------------------------------------------------------------

func TestConversationStore_Upsert_Create(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "UpsertNew")
	// Upsert on a non-existing record should create it.
	err := db.Conversations.Upsert(ctx, conv)
	require.NoError(t, err)

	got, err := db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, got.ID)
	assert.Equal(t, "UpsertNew", got.Title)
}

func TestConversationStore_Upsert_Update(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "Original")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// Upsert on an existing record should update it.
	conv.Title = "Updated"
	conv.Pinned = true
	err := db.Conversations.Upsert(ctx, conv)
	require.NoError(t, err)

	got, err := db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", got.Title)
	assert.True(t, got.Pinned)
}

func TestConversationStore_Upsert_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	conv := newTestConv(uid(), uid(), uid(), "direct", "Idempotent")
	// Call Upsert twice — should succeed both times without error.
	require.NoError(t, db.Conversations.Upsert(ctx, conv))
	require.NoError(t, db.Conversations.Upsert(ctx, conv))

	// Only one record should exist.
	got, err := db.Conversations.Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, got.ID)
}
