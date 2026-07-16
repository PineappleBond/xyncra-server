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

func setupTestDB(t *testing.T) *ClientDB {
	t.Helper()
	// NewInMemory already builds the full DSN; we just pass a unique name.
	name := fmt.Sprintf("testdb_%d", time.Now().UnixNano())
	db, err := NewInMemory(name)
	if err != nil {
		t.Fatalf("setupTestDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func cleanAll(t *testing.T, db *ClientDB, ctx context.Context) {
	t.Helper()
	// Delete all records from all tables (in dependency order).
	db.db.Unscoped().Where("1=1").Delete(&model.NotificationLog{})
	db.db.Unscoped().Where("1=1").Delete(&model.RPCLog{})
	db.db.Unscoped().Where("1=1").Delete(&model.RetryTask{})
	db.db.Unscoped().Where("1=1").Delete(&model.Draft{})
	db.db.Unscoped().Where("1=1").Delete(&model.SyncState{})
	db.db.Unscoped().Where("1=1").Delete(&model.UserUpdate{})
	db.db.Unscoped().Where("1=1").Delete(&model.Question{})
	db.db.Unscoped().Where("1=1").Delete(&model.Message{})
	db.db.Unscoped().Where("1=1").Delete(&model.Conversation{})
}

func newTestConv(id, uid1, uid2, typ, title string) *model.Conversation {
	return &model.Conversation{
		ID:            id,
		UserID1:       uid1,
		UserID2:       uid2,
		Type:          typ,
		Title:         title,
		LastMessageAt: time.Now(),
	}
}

func newTestMsg(id, clientID, convID string, msgID uint32, sender, content string) *model.Message {
	return &model.Message{
		ID:              id,
		ClientMessageID: clientID,
		ConversationID:  convID,
		MessageID:       msgID,
		SenderID:        sender,
		Content:         content,
		Type:            "text",
		Status:          "sent",
	}
}

func uid() string {
	return uuid.New().String()
}

func TestClientDB_Transaction_Commit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	var convID string
	err := db.Transaction(ctx, func(tx *gorm.DB) error {
		conv := newTestConv(uid(), uid(), uid(), "direct", "TxConv")
		convID = conv.ID
		if err := tx.Create(conv).Error; err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	// Verify the conversation is visible after commit.
	got, err := db.Conversations.Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, convID, got.ID)
	assert.Equal(t, "TxConv", got.Title)
}

func TestClientDB_Transaction_RollbackOnError(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	var convID string
	err := db.Transaction(ctx, func(tx *gorm.DB) error {
		conv := newTestConv(uid(), uid(), uid(), "direct", "RollbackConv")
		convID = conv.ID
		if err := tx.Create(conv).Error; err != nil {
			return err
		}
		// Return an error to trigger rollback.
		return fmt.Errorf("intentional error")
	})
	require.Error(t, err)

	// Verify the conversation is NOT visible (rolled back).
	_, err = db.Conversations.Get(ctx, convID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestClientDB_Transaction_CancelledContext(t *testing.T) {
	db := setupTestDB(t)
	cleanAll(t, db, context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := db.Transaction(ctx, func(tx *gorm.DB) error {
		return nil
	})
	require.Error(t, err)
}

func TestClientDB_Ping(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	err := db.Ping(ctx)
	require.NoError(t, err)
}
