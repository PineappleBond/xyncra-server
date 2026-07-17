package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test 1: Happy path - sender deletes own message
// ---------------------------------------------------------------------------

func TestDeleteMessage_HappyPath(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteMessageHandler(s, nil, nil)
	ctx := context.Background()

	convID := "conv-del-msg-happy-1"
	createTestConversation(t, s, convID, "alice", "bob")

	msgID := uuid.New().String()
	msg := &model.Message{
		ID:              msgID,
		ClientMessageID: uuid.New().String(),
		ConversationID:  convID,
		MessageID:       1,
		SenderID:        "alice",
		Content:         "Hello",
		CreatedAt:       time.Now(),
	}
	require.NoError(t, s.MessageStore().Create(ctx, msg))

	params := map[string]interface{}{
		"message_id": msgID,
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "delete_message", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	var resp deleteMessageResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	assert.Equal(t, "ok", resp.Status)

	// Verify UserUpdates created for ALL conversation members.
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates, 1, "alice should have 1 UserUpdate")
	assert.Equal(t, protocol.UpdateTypeDeleteMessage, aliceUpdates[0].Type, "Type should be 'delete_message'")

	// Verify payload contains message_id, conversation_id, and message_id_seq.
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(aliceUpdates[0].Payload, &payload))
	assert.Equal(t, msgID, payload["message_id"])
	assert.Equal(t, convID, payload["conversation_id"])
	assert.Equal(t, float64(1), payload["message_id_seq"])

	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Len(t, bobUpdates, 1, "bob should also have 1 UserUpdate (all members)")
	assert.Equal(t, protocol.UpdateTypeDeleteMessage, bobUpdates[0].Type, "Type should be 'delete_message'")
}

// ---------------------------------------------------------------------------
// Test 2: Missing message_id
// ---------------------------------------------------------------------------

func TestDeleteMessage_MissingMessageID(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteMessageHandler(s, nil, nil)
	ctx := context.Background()

	params := map[string]interface{}{
		"message_id": "",
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "delete_message", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "message_id")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 3: Message not found
// ---------------------------------------------------------------------------

func TestDeleteMessage_NotFound(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteMessageHandler(s, nil, nil)
	ctx := context.Background()

	params := map[string]interface{}{
		"message_id": uuid.New().String(),
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "delete_message", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "message not found")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 4: Non-member tries to delete
// ---------------------------------------------------------------------------

func TestDeleteMessage_NotMember(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteMessageHandler(s, nil, nil)
	ctx := context.Background()

	convID := "conv-del-msg-notmember-1"
	createTestConversation(t, s, convID, "alice", "bob")

	msgID := uuid.New().String()
	msg := &model.Message{
		ID:              msgID,
		ClientMessageID: uuid.New().String(),
		ConversationID:  convID,
		MessageID:       1,
		SenderID:        "alice",
		Content:         "Hello",
		CreatedAt:       time.Now(),
	}
	require.NoError(t, s.MessageStore().Create(ctx, msg))

	params := map[string]interface{}{
		"message_id": msgID,
	}

	client := server.NewTestClient("charlie") // not a member
	req := newTestRequest("req-1", "delete_message", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a member")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 5: Non-sender (other member) tries to delete (D-014)
// ---------------------------------------------------------------------------

func TestDeleteMessage_NotSender(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteMessageHandler(s, nil, nil)
	ctx := context.Background()

	convID := "conv-del-msg-notsender-1"
	createTestConversation(t, s, convID, "alice", "bob")

	msgID := uuid.New().String()
	msg := &model.Message{
		ID:              msgID,
		ClientMessageID: uuid.New().String(),
		ConversationID:  convID,
		MessageID:       1,
		SenderID:        "alice",
		Content:         "Hello",
		CreatedAt:       time.Now(),
	}
	require.NoError(t, s.MessageStore().Create(ctx, msg))

	params := map[string]interface{}{
		"message_id": msgID,
	}

	// bob is a member but not the sender
	client := server.NewTestClient("bob")
	req := newTestRequest("req-1", "delete_message", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only the sender")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 6: After delete, message is no longer visible via Get
// ---------------------------------------------------------------------------

func TestDeleteMessage_DeletedNotVisible(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteMessageHandler(s, nil, nil)
	ctx := context.Background()

	convID := "conv-del-msg-notvisible-1"
	createTestConversation(t, s, convID, "alice", "bob")

	msgID := uuid.New().String()
	msg := &model.Message{
		ID:              msgID,
		ClientMessageID: uuid.New().String(),
		ConversationID:  convID,
		MessageID:       1,
		SenderID:        "alice",
		Content:         "Hello",
		CreatedAt:       time.Now(),
	}
	require.NoError(t, s.MessageStore().Create(ctx, msg))

	params := map[string]interface{}{
		"message_id": msgID,
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "delete_message", params)

	// Delete should succeed
	_, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	// Verify message is no longer visible via Get (soft-deleted)
	_, err = s.MessageStore().Get(ctx, msgID)
	require.ErrorIs(t, err, store.ErrNotFound, "deleted message should not be visible via Get")
}
