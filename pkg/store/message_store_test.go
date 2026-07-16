package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageStore_Create_Success(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg := newTestMsg(uid(), uid(), convID, 1, "sender1", "Hello, world!")
	require.NoError(t, db.Messages.Create(ctx, msg))

	got, err := db.Messages.Get(ctx, msg.ID)
	require.NoError(t, err)
	assert.Equal(t, msg.ID, got.ID)
	assert.Equal(t, msg.Content, got.Content)
	assert.Equal(t, msg.MessageID, got.MessageID)
	assert.Equal(t, msg.ConversationID, got.ConversationID)
}

func TestMessageStore_Create_DuplicateClientMessageID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	clientMsgID := uid()
	msg1 := newTestMsg(uid(), clientMsgID, convID, 1, "sender", "first")
	require.NoError(t, db.Messages.Create(ctx, msg1))

	msg2 := newTestMsg(uid(), clientMsgID, convID, 2, "sender", "second")
	err := db.Messages.Create(ctx, msg2)
	assert.ErrorIs(t, err, ErrDuplicateKey)
}

func TestMessageStore_Get_Found(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg := newTestMsg(uid(), uid(), convID, 1, "sender", "content")
	require.NoError(t, db.Messages.Create(ctx, msg))

	got, err := db.Messages.Get(ctx, msg.ID)
	require.NoError(t, err)
	assert.Equal(t, msg.ID, got.ID)
	assert.Equal(t, "content", got.Content)
}

func TestMessageStore_Get_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	_, err := db.Messages.Get(ctx, "nonexistent-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMessageStore_GetByClientMessageID_Found(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	clientMsgID := uid()
	msg := newTestMsg(uid(), clientMsgID, convID, 1, "sender", "content")
	require.NoError(t, db.Messages.Create(ctx, msg))

	got, err := db.Messages.GetByClientMessageID(ctx, clientMsgID, "sender")
	require.NoError(t, err)
	assert.Equal(t, msg.ID, got.ID)
	assert.Equal(t, clientMsgID, got.ClientMessageID)
}

func TestMessageStore_GetByClientMessageID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	_, err := db.Messages.GetByClientMessageID(ctx, "nonexistent-client-id", "sender")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMessageStore_ListByConversation_IncrementalFetch(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// Create 5 messages with MessageIDs 1..5.
	for i := uint32(1); i <= 5; i++ {
		msg := newTestMsg(uid(), uid(), convID, i, "sender", "msg")
		require.NoError(t, db.Messages.Create(ctx, msg))
	}

	// Fetch all (afterMessageID=0).
	msgs, err := db.Messages.ListByConversation(ctx, convID, 0, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 5)
	// Should be ordered by MessageID ASC.
	assert.Equal(t, uint32(1), msgs[0].MessageID)
	assert.Equal(t, uint32(5), msgs[4].MessageID)

	// Incremental fetch: afterMessageID=3, should get 4, 5.
	msgs, err = db.Messages.ListByConversation(ctx, convID, 3, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, uint32(4), msgs[0].MessageID)
	assert.Equal(t, uint32(5), msgs[1].MessageID)

	// Limit test.
	msgs, err = db.Messages.ListByConversation(ctx, convID, 0, 2)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, uint32(1), msgs[0].MessageID)
	assert.Equal(t, uint32(2), msgs[1].MessageID)
}

func TestMessageStore_SearchByConversation_ContentMatch(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg1 := newTestMsg(uid(), uid(), convID, 1, "sender", "hello world")
	msg2 := newTestMsg(uid(), uid(), convID, 2, "sender", "goodbye world")
	msg3 := newTestMsg(uid(), uid(), convID, 3, "sender", "hello there")
	require.NoError(t, db.Messages.Create(ctx, msg1))
	require.NoError(t, db.Messages.Create(ctx, msg2))
	require.NoError(t, db.Messages.Create(ctx, msg3))

	// Search for "hello" — should find msg1 and msg3, ordered by MessageID DESC.
	msgs, err := db.Messages.SearchByConversation(ctx, convID, "hello", 0, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, msg3.ID, msgs[0].ID) // DESC order: newest first.
	assert.Equal(t, msg1.ID, msgs[1].ID)

	// Search with afterMessageID: "hello" before msgID 3.
	msgs, err = db.Messages.SearchByConversation(ctx, convID, "hello", 3, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, msg1.ID, msgs[0].ID)

	// Empty content returns empty.
	msgs, err = db.Messages.SearchByConversation(ctx, convID, "", 0, 10)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestMessageStore_ListByTimeRange_Filter(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	now := time.Now()
	msg1 := newTestMsg(uid(), uid(), convID, 1, "sender", "old")
	msg1.CreatedAt = now.Add(-3 * time.Hour)
	msg2 := newTestMsg(uid(), uid(), convID, 2, "sender", "mid")
	msg2.CreatedAt = now.Add(-1 * time.Hour)
	msg3 := newTestMsg(uid(), uid(), convID, 3, "sender", "new")
	msg3.CreatedAt = now

	require.NoError(t, db.Messages.Create(ctx, msg1))
	require.NoError(t, db.Messages.Create(ctx, msg2))
	require.NoError(t, db.Messages.Create(ctx, msg3))

	// Query range: from -2h to now — should include msg2 and msg3.
	startTime := now.Add(-2 * time.Hour)
	endTime := now.Add(1 * time.Minute)
	msgs, err := db.Messages.ListByTimeRange(ctx, convID, startTime, endTime, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, msg2.ID, msgs[0].ID)
	assert.Equal(t, msg3.ID, msgs[1].ID)

	// Query range that includes all.
	startTime = now.Add(-4 * time.Hour)
	msgs, err = db.Messages.ListByTimeRange(ctx, convID, startTime, endTime, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
}

func TestMessageStore_Delete_SoftDelete(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg := newTestMsg(uid(), uid(), convID, 1, "sender", "to-delete")
	require.NoError(t, db.Messages.Create(ctx, msg))

	require.NoError(t, db.Messages.Delete(ctx, msg.ID))

	// Should not be found via regular Get.
	_, err := db.Messages.Get(ctx, msg.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMessageStore_Delete_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	err := db.Messages.Delete(ctx, "nonexistent-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMessageStore_Restore(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg := newTestMsg(uid(), uid(), convID, 1, "sender", "to-restore")
	require.NoError(t, db.Messages.Create(ctx, msg))
	require.NoError(t, db.Messages.Delete(ctx, msg.ID))

	require.NoError(t, db.Messages.Restore(ctx, msg.ID))

	got, err := db.Messages.Get(ctx, msg.ID)
	require.NoError(t, err)
	assert.Equal(t, msg.ID, got.ID)
	assert.True(t, got.DeletedAt.Time.IsZero())
}

func TestMessageStore_Restore_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	err := db.Messages.Restore(ctx, "nonexistent-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMessageStore_DeleteByConversation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg1 := newTestMsg(uid(), uid(), convID, 1, "sender", "msg1")
	msg2 := newTestMsg(uid(), uid(), convID, 2, "sender", "msg2")
	require.NoError(t, db.Messages.Create(ctx, msg1))
	require.NoError(t, db.Messages.Create(ctx, msg2))

	require.NoError(t, db.Messages.DeleteByConversation(ctx, convID))

	// Both messages should be soft-deleted.
	_, err := db.Messages.Get(ctx, msg1.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = db.Messages.Get(ctx, msg2.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMessageStore_RestoreByConversation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg1 := newTestMsg(uid(), uid(), convID, 1, "sender", "msg1")
	msg2 := newTestMsg(uid(), uid(), convID, 2, "sender", "msg2")
	require.NoError(t, db.Messages.Create(ctx, msg1))
	require.NoError(t, db.Messages.Create(ctx, msg2))

	require.NoError(t, db.Messages.DeleteByConversation(ctx, convID))

	restored, err := db.Messages.RestoreByConversation(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), restored)

	// Both messages should be visible again.
	got1, err := db.Messages.Get(ctx, msg1.ID)
	require.NoError(t, err)
	assert.True(t, got1.DeletedAt.Time.IsZero())

	got2, err := db.Messages.Get(ctx, msg2.ID)
	require.NoError(t, err)
	assert.True(t, got2.DeletedAt.Time.IsZero())
}

func TestMessageStore_RestoreByConversation_NothingToRestore(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg := newTestMsg(uid(), uid(), convID, 1, "sender", "msg1")
	require.NoError(t, db.Messages.Create(ctx, msg))

	// No soft-deleted messages yet — should return 0.
	restored, err := db.Messages.RestoreByConversation(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), restored)
}

func TestMessageStore_Restore_NotDeleted(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msg := newTestMsg(uid(), uid(), convID, 1, "sender", "not-deleted")
	require.NoError(t, db.Messages.Create(ctx, msg))

	// Restore on a message that is NOT deleted should return ErrNotFound
	// (the WHERE clause requires deleted_at IS NOT NULL).
	err := db.Messages.Restore(ctx, msg.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMessageStore_SearchByConversation_LikeEscaping(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// Create messages with special LIKE characters in content.
	msg100pct := newTestMsg(uid(), uid(), convID, 1, "sender", "100% done")
	msg1001 := newTestMsg(uid(), uid(), convID, 2, "sender", "1001 items")
	msgA_B := newTestMsg(uid(), uid(), convID, 3, "sender", "a_b test")
	msgACB := newTestMsg(uid(), uid(), convID, 4, "sender", "acb test")
	msgPipe := newTestMsg(uid(), uid(), convID, 5, "sender", "pipe|char")

	require.NoError(t, db.Messages.Create(ctx, msg100pct))
	require.NoError(t, db.Messages.Create(ctx, msg1001))
	require.NoError(t, db.Messages.Create(ctx, msgA_B))
	require.NoError(t, db.Messages.Create(ctx, msgACB))
	require.NoError(t, db.Messages.Create(ctx, msgPipe))

	// Search for "100%" — should only match "100% done", not "1001 items".
	msgs, err := db.Messages.SearchByConversation(ctx, convID, "100%", 0, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, msg100pct.ID, msgs[0].ID)

	// Search for "a_b" — should only match "a_b test", not "acb test".
	msgs, err = db.Messages.SearchByConversation(ctx, convID, "a_b", 0, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, msgA_B.ID, msgs[0].ID)

	// Search for "pipe|char" — should match the pipe message.
	msgs, err = db.Messages.SearchByConversation(ctx, convID, "pipe|char", 0, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, msgPipe.ID, msgs[0].ID)

	// Empty content returns empty.
	msgs, err = db.Messages.SearchByConversation(ctx, convID, "", 0, 10)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestMessageStore_CountUnread_ExcludesSoftDeleted(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	conv := newTestConv(convID, uid(), uid(), "direct", "Test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// Create 5 messages with IDs 1..5.
	for i := uint32(1); i <= 5; i++ {
		msg := newTestMsg(uid(), uid(), convID, i, "sender", "msg")
		require.NoError(t, db.Messages.Create(ctx, msg))
	}

	// Count all messages after ID 2 → should be 3 (IDs 3, 4, 5).
	count, err := db.Messages.CountUnread(ctx, convID, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	// Count all messages after ID 0 → should be 5.
	count, err = db.Messages.CountUnread(ctx, convID, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)

	// Soft-delete message with ID 4.
	msgs, err := db.Messages.ListByConversation(ctx, convID, 3, 1)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.NoError(t, db.Messages.Delete(ctx, msgs[0].ID))

	// Count after ID 2 → should now be 2 (IDs 3, 5; ID 4 soft-deleted).
	count, err = db.Messages.CountUnread(ctx, convID, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// Count after ID 5 → should be 0.
	count, err = db.Messages.CountUnread(ctx, convID, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

// ---------------------------------------------------------------------------
// ListRecentByConversation
// ---------------------------------------------------------------------------

func TestMessageStore_ListRecentByConversation_DescOrder(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	require.NoError(t, db.Conversations.Create(ctx, newTestConv(convID, uid(), uid(), "direct", "Test")))

	for i := uint32(1); i <= 5; i++ {
		msg := newTestMsg(uid(), uid(), convID, i, "sender", "content")
		require.NoError(t, db.Messages.Create(ctx, msg))
	}

	msgs, err := db.Messages.ListRecentByConversation(ctx, convID, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 5)

	// Verify MessageID DESC ordering.
	for i := range msgs {
		assert.Equal(t, uint32(5-i), msgs[i].MessageID)
	}
}

func TestMessageStore_ListRecentByConversation_Limit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	require.NoError(t, db.Conversations.Create(ctx, newTestConv(convID, uid(), uid(), "direct", "Test")))

	for i := uint32(1); i <= 10; i++ {
		msg := newTestMsg(uid(), uid(), convID, i, "sender", "content")
		require.NoError(t, db.Messages.Create(ctx, msg))
	}

	msgs, err := db.Messages.ListRecentByConversation(ctx, convID, 3)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	assert.Equal(t, uint32(10), msgs[0].MessageID)
	assert.Equal(t, uint32(8), msgs[2].MessageID)
}

func TestMessageStore_ListRecentByConversation_InvalidLimit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	require.NoError(t, db.Conversations.Create(ctx, newTestConv(convID, uid(), uid(), "direct", "Test")))

	for i := uint32(1); i <= 5; i++ {
		msg := newTestMsg(uid(), uid(), convID, i, "sender", "content")
		require.NoError(t, db.Messages.Create(ctx, msg))
	}

	// limit=0 should fallback to 50.
	msgs, err := db.Messages.ListRecentByConversation(ctx, convID, 0)
	require.NoError(t, err)
	assert.Len(t, msgs, 5)
}

func TestMessageStore_ListRecentByConversation_Empty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	require.NoError(t, db.Conversations.Create(ctx, newTestConv(convID, uid(), uid(), "direct", "Test")))

	msgs, err := db.Messages.ListRecentByConversation(ctx, convID, 10)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

// ---------------------------------------------------------------------------
// Upsert tests
// ---------------------------------------------------------------------------

func TestMessageStore_Upsert_Create(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	require.NoError(t, db.Conversations.Create(ctx, newTestConv(convID, uid(), uid(), "direct", "Test")))

	clientMsgID := uid()
	msg := newTestMsg(uid(), clientMsgID, convID, 1, "sender1", "Hello")

	// Upsert on a non-existing record should create it.
	err := db.Messages.Upsert(ctx, msg)
	require.NoError(t, err)

	got, err := db.Messages.Get(ctx, msg.ID)
	require.NoError(t, err)
	assert.Equal(t, msg.ID, got.ID)
	assert.Equal(t, clientMsgID, got.ClientMessageID)
	assert.Equal(t, "sender1", got.SenderID)
	assert.Equal(t, "Hello", got.Content)
}

func TestMessageStore_Upsert_Update(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	require.NoError(t, db.Conversations.Create(ctx, newTestConv(convID, uid(), uid(), "direct", "Test")))

	clientMsgID := uid()
	senderID := "sender1"
	msg := newTestMsg(uid(), clientMsgID, convID, 1, senderID, "Original content")
	require.NoError(t, db.Messages.Create(ctx, msg))

	// Upsert on an existing record should update it.
	msg.Content = "Updated content"
	msg.Status = "delivered"
	err := db.Messages.Upsert(ctx, msg)
	require.NoError(t, err)

	got, err := db.Messages.Get(ctx, msg.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated content", got.Content)
	assert.Equal(t, "delivered", got.Status)
}

func TestMessageStore_Upsert_TOCTOU(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	require.NoError(t, db.Conversations.Create(ctx, newTestConv(convID, uid(), uid(), "direct", "Test")))

	clientMsgID := uid()
	senderID := "sender1"
	msgID := uid() // Use the same ID for both messages

	// Concurrent inserts with the same (client_message_id, sender_id) and same ID.
	// This simulates the TOCTOU race where both goroutines SELECT, find nothing,
	// then both INSERT. One succeeds, the other hits duplicate key and retries
	// as UPDATE via updateByCompositeKey.
	msg1 := newTestMsg(msgID, clientMsgID, convID, 1, senderID, "First attempt")
	msg2 := newTestMsg(msgID, clientMsgID, convID, 1, senderID, "Second attempt")

	var wg sync.WaitGroup
	errs := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = db.Messages.Upsert(ctx, msg1)
	}()
	go func() {
		defer wg.Done()
		errs[1] = db.Messages.Upsert(ctx, msg2)
	}()

	wg.Wait()

	// Both should succeed (TOCTOU retry handles the race).
	assert.NoError(t, errs[0])
	assert.NoError(t, errs[1])

	// Verify only one record exists with the composite key.
	got, err := db.Messages.GetByClientMessageID(ctx, clientMsgID, senderID)
	require.NoError(t, err)
	assert.NotNil(t, got)
	// Content should be whichever write won — either is acceptable.
	assert.Contains(t, []string{"First attempt", "Second attempt"}, got.Content)
}

func TestMessageStore_Upsert_CompositeKey(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	require.NoError(t, db.Conversations.Create(ctx, newTestConv(convID, uid(), uid(), "direct", "Test")))

	clientMsgID := uid()
	// Same client_message_id, different sender_ids → should NOT conflict.
	msg1 := newTestMsg(uid(), clientMsgID, convID, 1, "senderA", "From A")
	msg2 := newTestMsg(uid(), clientMsgID, convID, 2, "senderB", "From B")

	require.NoError(t, db.Messages.Upsert(ctx, msg1))
	require.NoError(t, db.Messages.Upsert(ctx, msg2))

	// Both records should exist.
	got1, err := db.Messages.GetByClientMessageID(ctx, clientMsgID, "senderA")
	require.NoError(t, err)
	assert.Equal(t, "From A", got1.Content)

	got2, err := db.Messages.GetByClientMessageID(ctx, clientMsgID, "senderB")
	require.NoError(t, err)
	assert.Equal(t, "From B", got2.Content)
}
