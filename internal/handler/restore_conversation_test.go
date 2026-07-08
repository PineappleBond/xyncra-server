package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test 1: Happy path - delete then restore conversation with messages
// ---------------------------------------------------------------------------

func TestRestoreConversation_HappyPath(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewRestoreConversationHandler(s)
	ctx := context.Background()

	convID := "conv-restore-happy-1"
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 3, 1)

	// Delete the conversation first (cascade delete).
	err := s.ConversationStore().Delete(ctx, convID)
	require.NoError(t, err)
	err = s.MessageStore().DeleteByConversation(ctx, convID)
	require.NoError(t, err)

	// Verify deletion.
	_, err = s.ConversationStore().Get(ctx, convID)
	require.Error(t, err, "conversation should be deleted")

	// Restore.
	params := map[string]interface{}{
		"conversation_id": convID,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "restore_conversation", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	var resp restoreConversationResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	assert.NotNil(t, resp.Conversation)
	assert.Equal(t, convID, resp.Conversation.ID)
	assert.Equal(t, int64(3), resp.RestoredMessageCount, "should have restored 3 messages")
}

// ---------------------------------------------------------------------------
// Test 2: Idempotent - non-deleted conversation returns without error
// ---------------------------------------------------------------------------

func TestRestoreConversation_Idempotent(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewRestoreConversationHandler(s)
	ctx := context.Background()

	convID := "conv-restore-idempotent-1"
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 2, 1)

	params := map[string]interface{}{
		"conversation_id": convID,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "restore_conversation", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err, "restoring a non-deleted conversation should not error")

	var resp restoreConversationResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	assert.NotNil(t, resp.Conversation)
	assert.Equal(t, convID, resp.Conversation.ID)
	assert.Equal(t, int64(0), resp.RestoredMessageCount, "non-deleted conv should have 0 restored messages")
}

// ---------------------------------------------------------------------------
// Test 3: Missing conversation_id
// ---------------------------------------------------------------------------

func TestRestoreConversation_MissingConversationID(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewRestoreConversationHandler(s)
	ctx := context.Background()

	params := map[string]interface{}{
		"conversation_id": "",
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "restore_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conversation_id")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 4: Conversation not found
// ---------------------------------------------------------------------------

func TestRestoreConversation_NotFound(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewRestoreConversationHandler(s)
	ctx := context.Background()

	params := map[string]interface{}{
		"conversation_id": "nonexistent-conv",
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "restore_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 5: User not a member
// ---------------------------------------------------------------------------

func TestRestoreConversation_NotMember(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewRestoreConversationHandler(s)
	ctx := context.Background()

	convID := "conv-restore-notmember-1"
	createTestConversation(t, s, convID, "alice", "bob")

	// Delete so restore logic is exercised.
	require.NoError(t, s.ConversationStore().Delete(ctx, convID))

	params := map[string]interface{}{
		"conversation_id": convID,
	}
	client := server.NewTestClient("charlie") // not a member
	req := newTestRequest("req-1", "restore_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a member")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 6: Messages visible after restore via ListByConversation
// ---------------------------------------------------------------------------

func TestRestoreConversation_MessagesVisibleAfterRestore(t *testing.T) {
	s := setupTestSQLite(t)
	ctx := context.Background()

	convID := "conv-restore-visible-1"
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 5, 1)

	// Delete cascade.
	require.NoError(t, s.ConversationStore().Delete(ctx, convID))
	require.NoError(t, s.MessageStore().DeleteByConversation(ctx, convID))

	// Verify messages are not visible.
	msgs, err := s.MessageStore().ListByConversation(ctx, convID, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, msgs, "messages should be hidden after delete")

	// Restore via handler.
	handler := NewRestoreConversationHandler(s)
	params := map[string]interface{}{
		"conversation_id": convID,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "restore_conversation", params)

	data, handleErr := handler.HandleRequest(ctx, client, req)
	require.NoError(t, handleErr)

	var resp restoreConversationResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	assert.Equal(t, int64(5), resp.RestoredMessageCount)

	// Verify all messages are visible again.
	msgs, err = s.MessageStore().ListByConversation(ctx, convID, 0, 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 5, "all 5 messages should be visible after restore")

	// Verify conversation is accessible via normal Get.
	restoredConv, err := s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.NotNil(t, restoredConv)
	assert.Equal(t, convID, restoredConv.ID)

	// Verify LastProcessedMessageID was recalculated.
	assert.Equal(t, uint32(5), restoredConv.LastProcessedMessageID,
		"LastProcessedMessageID should be recalculated to the max message ID")
}
