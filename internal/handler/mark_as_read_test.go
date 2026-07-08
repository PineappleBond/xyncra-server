package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseMarkAsReadResponse unmarshals the handler's response data.
func parseMarkAsReadResponse(t *testing.T, data json.RawMessage) (string, int64, uint32) {
	t.Helper()
	var resp markAsReadResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	return resp.Status, resp.UnreadCount, resp.LastReadMessageID
}

// seedMessages inserts count messages into the conversation and updates
// the conversation's LastProcessedMessageID.
func seedMessages(t *testing.T, s *testSQLiteStore, convID string, count int) {
	t.Helper()
	ctx := context.Background()
	for i := uint32(1); i <= uint32(count); i++ {
		err := s.MessageStore().Create(ctx, &model.Message{
			ID:              uuid.New().String(),
			ClientMessageID: uuid.New().String(),
			ConversationID:  convID,
			MessageID:       i,
			SenderID:        "alice",
			Content:         fmt.Sprintf("msg %d", i),
			CreatedAt:       time.Now(),
		})
		require.NoError(t, err)
	}
	// Update conversation's LastProcessedMessageID.
	require.NoError(t, s.ConversationStore().UpdateLastMessage(ctx, convID, time.Now(), uint32(count)))
}

// ---------------------------------------------------------------------------
// Test 1: Happy path — mark specific message as read
// ---------------------------------------------------------------------------

func TestMarkAsRead_HappyPath(t *testing.T) {
	s := setupTestSQLite(t)
	h := NewMarkAsReadHandler(s, nil)
	ctx := context.Background()

	convID := "conv-mark-happy"
	createTestConversation(t, s, convID, "alice", "bob")
	seedMessages(t, s, convID, 5)

	// Mark message 3 as read.
	params := map[string]interface{}{
		"conversation_id": convID,
		"message_id":      3,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "mark_as_read", params)

	data, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	status, unreadCount, lastReadMessageID := parseMarkAsReadResponse(t, data)
	assert.Equal(t, "ok", status)
	assert.Equal(t, uint32(3), lastReadMessageID)
	assert.Equal(t, int64(2), unreadCount, "messages 4 and 5 are unread")

	// Verify the conversation's read cursor updated.
	conv, err := s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), conv.LastReadMessageID1, "alice is UserID1, should have LastReadMessageID1=3")

	// Verify UserUpdate was created for alice (the operating user) only.
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates, 1, "alice should have 1 UserUpdate after mark_as_read")
	assert.Equal(t, protocol.UpdateTypeMarkRead, aliceUpdates[0].Type, "UserUpdate Type should be 'mark_read'")

	// Verify payload contains conversation_id and last_read_message_id.
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(aliceUpdates[0].Payload, &payload))
	assert.Equal(t, convID, payload["conversation_id"], "payload should contain conversation_id")
	assert.Equal(t, float64(3), payload["last_read_message_id"], "payload should contain last_read_message_id")

	// Verify NO UserUpdate was created for bob (D-012: mark_read not exposed to other party).
	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Empty(t, bobUpdates, "bob should NOT have any UserUpdate from mark_as_read (D-012)")
}

// ---------------------------------------------------------------------------
// Test 2: Omit message_id — uses LastProcessedMessageID (all read)
// ---------------------------------------------------------------------------

func TestMarkAsRead_OmitMessageID(t *testing.T) {
	s := setupTestSQLite(t)
	h := NewMarkAsReadHandler(s, nil)
	ctx := context.Background()

	convID := "conv-mark-omit"
	createTestConversation(t, s, convID, "alice", "bob")
	seedMessages(t, s, convID, 5)

	// Omit message_id — should mark all as read.
	params := map[string]interface{}{
		"conversation_id": convID,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "mark_as_read", params)

	data, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	status, unreadCount, lastReadMessageID := parseMarkAsReadResponse(t, data)
	assert.Equal(t, "ok", status)
	assert.Equal(t, uint32(5), lastReadMessageID, "should use LastProcessedMessageID=5")
	assert.Equal(t, int64(0), unreadCount, "all messages are read, unread_count should be 0")

	// Verify the conversation's read cursor updated.
	conv, err := s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), conv.LastReadMessageID1)
}

// ---------------------------------------------------------------------------
// Test 3: MAX semantics (D-012) — read cursor cannot go backward
// ---------------------------------------------------------------------------

func TestMarkAsRead_MAXSemantics(t *testing.T) {
	s := setupTestSQLite(t)
	h := NewMarkAsReadHandler(s, nil)
	ctx := context.Background()

	convID := "conv-mark-max"
	createTestConversation(t, s, convID, "alice", "bob")
	seedMessages(t, s, convID, 5)

	// First, set read cursor to message 5.
	params1 := map[string]interface{}{
		"conversation_id": convID,
		"message_id":      5,
	}
	client := server.NewTestClient("alice")
	req1 := newTestRequest("req-1", "mark_as_read", params1)
	_, err := h.HandleRequest(ctx, client, req1)
	require.NoError(t, err)

	conv, err := s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), conv.LastReadMessageID1)

	// Now try to set read cursor to message 2 (backward).
	// Should stay at 5 due to MAX semantics (D-012).
	params2 := map[string]interface{}{
		"conversation_id": convID,
		"message_id":      2,
	}
	req2 := newTestRequest("req-2", "mark_as_read", params2)
	data, err := h.HandleRequest(ctx, client, req2)
	require.NoError(t, err)

	status, _, lastReadMessageID := parseMarkAsReadResponse(t, data)
	assert.Equal(t, "ok", status)
	// The handler still returns the requested messageID in the response,
	// but the store enforces MAX semantics so the actual cursor stays at 5.
	assert.Equal(t, uint32(2), lastReadMessageID, "handler returns requested messageID")

	// Verify the actual cursor in the database stayed at 5 (MAX semantics).
	conv, err = s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), conv.LastReadMessageID1, "MAX semantics: cursor should stay at 5, not go back to 2")
}

// ---------------------------------------------------------------------------
// Test 4: Missing conversation_id
// ---------------------------------------------------------------------------

func TestMarkAsRead_MissingConversationID(t *testing.T) {
	s := setupTestSQLite(t)
	h := NewMarkAsReadHandler(s, nil)
	ctx := context.Background()

	params := map[string]interface{}{
		"message_id": 1,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "mark_as_read", params)

	_, err := h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conversation_id")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 5: Conversation not found
// ---------------------------------------------------------------------------

func TestMarkAsRead_NotFound(t *testing.T) {
	s := setupTestSQLite(t)
	h := NewMarkAsReadHandler(s, nil)
	ctx := context.Background()

	params := map[string]interface{}{
		"conversation_id": "nonexistent-conv",
		"message_id":      1,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "mark_as_read", params)

	_, err := h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 6: User is not a member
// ---------------------------------------------------------------------------

func TestMarkAsRead_NotMember(t *testing.T) {
	s := setupTestSQLite(t)
	h := NewMarkAsReadHandler(s, nil)
	ctx := context.Background()

	convID := "conv-mark-notmember"
	createTestConversation(t, s, convID, "alice", "bob")
	seedMessages(t, s, convID, 5)

	params := map[string]interface{}{
		"conversation_id": convID,
		"message_id":      1,
	}
	client := server.NewTestClient("charlie") // not a member
	req := newTestRequest("req-1", "mark_as_read", params)

	_, err := h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a member")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 7: UserID1 and UserID2 update different fields independently
// ---------------------------------------------------------------------------

func TestMarkAsRead_UserID1AndUserID2Separate(t *testing.T) {
	s := setupTestSQLite(t)
	h := NewMarkAsReadHandler(s, nil)
	ctx := context.Background()

	convID := "conv-mark-separate"
	createTestConversation(t, s, convID, "alice", "bob")
	seedMessages(t, s, convID, 5)

	// Alice marks message 3 as read.
	params1 := map[string]interface{}{
		"conversation_id": convID,
		"message_id":      3,
	}
	client1 := server.NewTestClient("alice")
	req1 := newTestRequest("req-1", "mark_as_read", params1)
	_, err := h.HandleRequest(ctx, client1, req1)
	require.NoError(t, err)

	// Bob marks message 1 as read.
	params2 := map[string]interface{}{
		"conversation_id": convID,
		"message_id":      1,
	}
	client2 := server.NewTestClient("bob")
	req2 := newTestRequest("req-2", "mark_as_read", params2)
	_, err = h.HandleRequest(ctx, client2, req2)
	require.NoError(t, err)

	// Verify each user's read cursor is independent.
	conv, err := s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), conv.LastReadMessageID1, "alice's cursor should be 3")
	assert.Equal(t, uint32(1), conv.LastReadMessageID2, "bob's cursor should be 1")
}

// ---------------------------------------------------------------------------
// Test 8: Unread count is correct
// ---------------------------------------------------------------------------

func TestMarkAsRead_UnreadCountCorrect(t *testing.T) {
	s := setupTestSQLite(t)
	h := NewMarkAsReadHandler(s, nil)
	ctx := context.Background()

	convID := "conv-mark-unread"
	createTestConversation(t, s, convID, "alice", "bob")
	seedMessages(t, s, convID, 5)

	tests := []struct {
		name          string
		messageID     uint32
		expectedCount int64
	}{
		{"mark message 1", 1, 4}, // messages 2,3,4,5 unread
		{"mark message 3", 3, 2}, // messages 4,5 unread
		{"mark message 5", 5, 0}, // all read
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{
				"conversation_id": convID,
				"message_id":      tt.messageID,
			}
			client := server.NewTestClient("alice")
			req := newTestRequest("req-"+tt.name, "mark_as_read", params)

			data, err := h.HandleRequest(ctx, client, req)
			require.NoError(t, err)

			_, unreadCount, lastReadMessageID := parseMarkAsReadResponse(t, data)
			assert.Equal(t, tt.messageID, lastReadMessageID)
			assert.Equal(t, tt.expectedCount, unreadCount)
		})
	}
}

// ---------------------------------------------------------------------------
// Test 9: UserUpdate creation - Type, Payload, and only for operating user
// ---------------------------------------------------------------------------

func TestMarkAsRead_UserUpdateCreation(t *testing.T) {
	s := setupTestSQLite(t)
	h := NewMarkAsReadHandler(s, nil)
	ctx := context.Background()

	convID := "conv-mark-uu-create"
	createTestConversation(t, s, convID, "alice", "bob")
	seedMessages(t, s, convID, 5)

	// Alice marks message 3 as read.
	params := map[string]interface{}{
		"conversation_id": convID,
		"message_id":      3,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "mark_as_read", params)
	_, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	// Verify UserUpdate created for alice with correct Type.
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	require.Len(t, aliceUpdates, 1, "alice should have exactly 1 UserUpdate")
	assert.Equal(t, protocol.UpdateTypeMarkRead, aliceUpdates[0].Type, "Type should be 'mark_read'")
	assert.Equal(t, uint32(1), aliceUpdates[0].Seq, "Seq should be 1 (first update)")

	// Verify Payload contains conversation_id and last_read_message_id.
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(aliceUpdates[0].Payload, &payload))
	assert.Equal(t, convID, payload["conversation_id"])
	assert.Equal(t, float64(3), payload["last_read_message_id"])

	// Verify NO UserUpdate for bob (D-012).
	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Empty(t, bobUpdates, "bob should have 0 UserUpdates (D-012)")

	// Alice marks message 5 as read - should create a second UserUpdate.
	params2 := map[string]interface{}{
		"conversation_id": convID,
		"message_id":      5,
	}
	req2 := newTestRequest("req-2", "mark_as_read", params2)
	_, err = h.HandleRequest(ctx, client, req2)
	require.NoError(t, err)

	aliceUpdates2, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates2, 2, "alice should have 2 UserUpdates after second mark_as_read")
	assert.Equal(t, uint32(2), aliceUpdates2[1].Seq, "second update should have Seq=2")
	assert.Equal(t, protocol.UpdateTypeMarkRead, aliceUpdates2[1].Type)

	// Bob still has 0 UserUpdates.
	bobUpdates2, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Empty(t, bobUpdates2, "bob should still have 0 UserUpdates")
}
