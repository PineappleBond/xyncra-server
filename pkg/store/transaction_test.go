package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// TX-01: SaveTx rollback — NotificationLog should NOT persist
// ---------------------------------------------------------------------------

func TestNotificationLogSaveTx_Rollback(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	nLog := &model.NotificationLog{
		ID:        uuid.New().String(),
		Seq:       100,
		Type:      "message",
		Payload:   []byte(`{}`),
		CreatedAt: time.Now(),
	}

	// Wrap SaveTx in a transaction that returns an error to trigger rollback.
	err := db.Transaction(ctx, func(tx *gorm.DB) error {
		if err := db.NotificationLogs.SaveTx(ctx, tx, nLog); err != nil {
			return fmt.Errorf("save tx: %w", err)
		}
		// Return error to force rollback.
		return fmt.Errorf("intentional rollback")
	})
	require.Error(t, err)

	// Verify the notification log was NOT persisted.
	logs, listErr := db.NotificationLogs.ListBySeqRange(ctx, 100, 100)
	require.NoError(t, listErr)
	assert.Empty(t, logs, "notification log should not be persisted after rollback")
}

// ---------------------------------------------------------------------------
// TX-02: CreateTx commit — Message should persist
// ---------------------------------------------------------------------------

func TestMessageCreateTx_Commit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	msg := &model.Message{
		ID:              uuid.New().String(),
		ClientMessageID: uuid.New().String(),
		ConversationID:  "conv-tx02",
		MessageID:       42,
		SenderID:        "sender-1",
		Content:         "hello from tx",
		Type:            "text",
		Status:          "sent",
	}

	err := db.Transaction(ctx, func(tx *gorm.DB) error {
		return db.Messages.CreateTx(ctx, tx, msg)
	})
	require.NoError(t, err)

	// Verify the message was persisted.
	got, getErr := db.Messages.Get(ctx, msg.ID)
	require.NoError(t, getErr)
	assert.Equal(t, msg.ID, got.ID)
	assert.Equal(t, "hello from tx", got.Content)
	assert.Equal(t, uint32(42), got.MessageID)
}

// ---------------------------------------------------------------------------
// TX-03: SoftDeleteTx cascade — Conversation + Messages are soft-deleted
// ---------------------------------------------------------------------------

func TestConversationSoftDeleteTx_Cascade(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := "conv-tx03"
	conv := newTestConv(convID, "u1", "u2", "direct", "cascade test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// Insert a message in this conversation.
	msgID := "msg-tx03"
	msg := newTestMsg(msgID, "cmid-tx03", convID, 1, "u1", "cascade msg")
	require.NoError(t, db.Messages.Create(ctx, msg))

	// Cascade soft-delete within a transaction.
	err := db.Transaction(ctx, func(tx *gorm.DB) error {
		return db.Conversations.SoftDeleteTx(ctx, tx, convID)
	})
	require.NoError(t, err)

	// Conversation should be soft-deleted (Get returns ErrNotFound).
	_, err = db.Conversations.Get(ctx, convID)
	assert.ErrorIs(t, err, ErrNotFound, "conversation should be soft-deleted")

	// Message should also be soft-deleted.
	_, err = db.Messages.Get(ctx, msgID)
	assert.ErrorIs(t, err, ErrNotFound, "message should be cascade soft-deleted")
}

// ---------------------------------------------------------------------------
// TX-04: RestoreTx cascade — Conversation + Messages are restored
// ---------------------------------------------------------------------------

func TestConversationRestoreTx_Cascade(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := "conv-tx04"
	conv := newTestConv(convID, "u1", "u2", "direct", "restore test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	msgID := "msg-tx04"
	msg := newTestMsg(msgID, "cmid-tx04", convID, 1, "u1", "restore msg")
	require.NoError(t, db.Messages.Create(ctx, msg))

	// First, soft-delete.
	require.NoError(t, db.Conversations.Delete(ctx, convID))

	// Verify both are soft-deleted.
	_, err := db.Conversations.Get(ctx, convID)
	require.ErrorIs(t, err, ErrNotFound)

	// Cascade restore within a transaction.
	err = db.Transaction(ctx, func(tx *gorm.DB) error {
		return db.Conversations.RestoreTx(ctx, tx, convID)
	})
	require.NoError(t, err)

	// Conversation should be visible again.
	got, err := db.Conversations.Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, convID, got.ID)
	assert.Equal(t, "restore test", got.Title)

	// Message should also be restored.
	gotMsg, err := db.Messages.Get(ctx, msgID)
	require.NoError(t, err)
	assert.Equal(t, msgID, gotMsg.ID)
	assert.Equal(t, "restore msg", gotMsg.Content)
}

// ---------------------------------------------------------------------------
// TX-05: UpdateLastReadTx MAX semantics — cursor only advances, never retreats
// ---------------------------------------------------------------------------

func TestConversationUpdateLastReadTx_MAX(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := "conv-tx05"
	conv := newTestConv(convID, "u1", "u2", "direct", "max test")
	require.NoError(t, db.Conversations.Create(ctx, conv))

	// First, set cursor to 100.
	err := db.Transaction(ctx, func(tx *gorm.DB) error {
		return db.Conversations.UpdateLastReadTx(ctx, tx, convID, "u1", 100)
	})
	require.NoError(t, err)

	got, err := db.Conversations.Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(100), got.LastReadMessageID1, "cursor should be 100 after first update")

	// Try to set cursor to 50 (lower than current). Should NOT go backward.
	err = db.Transaction(ctx, func(tx *gorm.DB) error {
		return db.Conversations.UpdateLastReadTx(ctx, tx, convID, "u1", 50)
	})
	require.NoError(t, err)

	got, err = db.Conversations.Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(100), got.LastReadMessageID1, "cursor should remain 100 (MAX semantics)")

	// Advance to 200 — should succeed.
	err = db.Transaction(ctx, func(tx *gorm.DB) error {
		return db.Conversations.UpdateLastReadTx(ctx, tx, convID, "u1", 200)
	})
	require.NoError(t, err)

	got, err = db.Conversations.Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(200), got.LastReadMessageID1, "cursor should advance to 200")
}

// ---------------------------------------------------------------------------
// TX-06: SetLocalMaxSeqTx — seq is correctly updated
// ---------------------------------------------------------------------------

func TestSyncStateSetLocalMaxSeqTx(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	// Set seq to 42 inside a transaction.
	err := db.Transaction(ctx, func(tx *gorm.DB) error {
		return db.SyncStates.SetLocalMaxSeqTx(ctx, tx, 42)
	})
	require.NoError(t, err)

	got, err := db.SyncStates.GetLocalMaxSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), got, "local_max_seq should be 42")

	// Update to 100 inside a rolled-back transaction.
	err = db.Transaction(ctx, func(tx *gorm.DB) error {
		if err := db.SyncStates.SetLocalMaxSeqTx(ctx, tx, 100); err != nil {
			return err
		}
		return fmt.Errorf("rollback")
	})
	require.Error(t, err)

	// Should still be 42 since the second transaction was rolled back.
	got, err = db.SyncStates.GetLocalMaxSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), got, "local_max_seq should remain 42 after rollback")

	// Update to 100 in a successful transaction.
	err = db.Transaction(ctx, func(tx *gorm.DB) error {
		return db.SyncStates.SetLocalMaxSeqTx(ctx, tx, 100)
	})
	require.NoError(t, err)

	got, err = db.SyncStates.GetLocalMaxSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(100), got, "local_max_seq should be 100 after successful update")
}
