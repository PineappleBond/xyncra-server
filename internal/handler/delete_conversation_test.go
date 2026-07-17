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
// Test 1: Happy path - delete conversation with messages
// ---------------------------------------------------------------------------

func TestDeleteConversation_HappyPath(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteConversationHandler(s, nil, nil)
	ctx := context.Background()

	convID := "conv-delete-happy-1"
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 3, 1)

	params := map[string]interface{}{
		"conversation_id": convID,
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "delete_conversation", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	var resp deleteConversationResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, int64(3), resp.DeletedMessageCount, "should have deleted 3 messages")

	// Verify UserUpdates created for ALL conversation members.
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates, 1, "alice should have 1 UserUpdate")
	assert.Equal(t, protocol.UpdateTypeConversation, aliceUpdates[0].Type, "Type should be 'conversation'")

	// Verify payload contains conversation_id and action "delete".
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(aliceUpdates[0].Payload, &payload))
	assert.Equal(t, convID, payload["conversation_id"])
	assert.Equal(t, "delete", payload["action"])

	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Len(t, bobUpdates, 1, "bob should also have 1 UserUpdate (all members)")
	assert.Equal(t, protocol.UpdateTypeConversation, bobUpdates[0].Type, "Type should be 'conversation'")
}

// ---------------------------------------------------------------------------
// Test 2: Missing conversation_id
// ---------------------------------------------------------------------------

func TestDeleteConversation_MissingConversationID(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteConversationHandler(s, nil, nil)
	ctx := context.Background()

	params := map[string]interface{}{
		"conversation_id": "",
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "delete_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conversation_id")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 3: Conversation not found
// ---------------------------------------------------------------------------

func TestDeleteConversation_NotFound(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteConversationHandler(s, nil, nil)
	ctx := context.Background()

	params := map[string]interface{}{
		"conversation_id": "nonexistent-conv",
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "delete_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 4: User not a member
// ---------------------------------------------------------------------------

func TestDeleteConversation_NotMember(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteConversationHandler(s, nil, nil)
	ctx := context.Background()

	convID := "conv-delete-notmember-1"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
	}

	client := server.NewTestClient("charlie") // not a member
	req := newTestRequest("req-1", "delete_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a member")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 5: Cascade delete - verify both conversation and messages are soft-deleted
// ---------------------------------------------------------------------------

func TestDeleteConversation_CascadeDelete(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteConversationHandler(s, nil, nil)
	ctx := context.Background()

	convID := "conv-delete-cascade-1"
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 5, 1)

	params := map[string]interface{}{
		"conversation_id": convID,
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "delete_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	// Verify conversation is soft-deleted (Get should return not found)
	_, err = s.ConversationStore().Get(ctx, convID)
	require.Error(t, err, "conversation should be soft-deleted")

	// Verify messages are soft-deleted (ListByConversation should return empty)
	messages, err := s.MessageStore().ListByConversation(ctx, convID, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, messages, "all messages should be soft-deleted")
}

// ---------------------------------------------------------------------------
// Test 6: Double delete - second delete returns not found
// ---------------------------------------------------------------------------

func TestDeleteConversation_DoubleDelete(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewDeleteConversationHandler(s, nil, nil)
	ctx := context.Background()

	convID := "conv-delete-double-1"
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 2, 1)

	params := map[string]interface{}{
		"conversation_id": convID,
	}

	client := server.NewTestClient("alice")

	// First delete - should succeed
	req1 := newTestRequest("req-1", "delete_conversation", params)
	data1, err1 := handler.HandleRequest(ctx, client, req1)
	require.NoError(t, err1)

	var resp1 deleteConversationResponse
	require.NoError(t, json.Unmarshal(data1, &resp1))
	assert.Equal(t, "ok", resp1.Status)
	assert.Equal(t, int64(2), resp1.DeletedMessageCount)

	// Second delete - should return not found
	req2 := newTestRequest("req-2", "delete_conversation", params)
	_, err2 := handler.HandleRequest(ctx, client, req2)
	require.Error(t, err2, "second delete should fail")
	assert.Contains(t, err2.Error(), "not found", "second delete should return 'not found'")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err2, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code)
}
