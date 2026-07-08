package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// getConversationResult is the parsed response from the get_conversation handler.
type getConversationResult struct {
	Conversation *model.Conversation `json:"conversation"`
	UnreadCount  int64               `json:"unread_count"`
}

// parseGetConversationResponse unmarshals the handler's response data.
func parseGetConversationResponse(t *testing.T, data json.RawMessage) getConversationResult {
	t.Helper()
	var result getConversationResult
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

// callGetConversation is a convenience that builds a request, calls the handler,
// and parses the response. It fails the test on error.
func callGetConversation(t *testing.T, h *getConversationHandler, userID string, params interface{}) getConversationResult {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient(userID)
	req := newTestRequest("req-get-conv", "get_conversation", params)
	data, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)
	return parseGetConversationResponse(t, data)
}

// callGetConversationExpectError is a convenience that builds a request, calls
// the handler, and asserts that an error is returned containing the given substring.
func callGetConversationExpectError(t *testing.T, h *getConversationHandler, userID string, params interface{}, errContains string) {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient(userID)
	req := newTestRequest("req-get-conv", "get_conversation", params)
	_, err := h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), errContains, "error should contain expected substring")
}

// seedTestMessagesFrom inserts count messages into the given conversation with
// sequential MessageIDs starting at startMessageID, optionally setting the
// sender to alternate between the given senderID and "other-sender".
func seedTestMessagesFrom(t *testing.T, s *testSQLiteStore, convID, senderID string, count int, startMessageID uint32) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < count; i++ {
		msgID := startMessageID + uint32(i)
		msg := &model.Message{
			ID:              uuid.New().String(),
			ClientMessageID: uuid.New().String(),
			ConversationID:  convID,
			MessageID:       msgID,
			SenderID:        senderID,
			Content:         "message-" + uuid.New().String()[:8],
			Type:            "text",
			Status:          "sent",
			CreatedAt:       now.Add(time.Duration(i) * time.Millisecond),
		}
		require.NoError(t, s.MessageStore().Create(ctx, msg))
	}
}

// ---------------------------------------------------------------------------
// GC-01: HappyPath
// ---------------------------------------------------------------------------

func TestGetConversation_HappyPath(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetConversationHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")

	// Seed some messages so unread_count is non-zero.
	seedTestMessagesFrom(t, s, convID, "bob", 5, 1)

	// Update conversation LastProcessedMessageID to reflect the seeded messages.
	ctx := context.Background()
	require.NoError(t, s.ConversationStore().UpdateLastMessage(ctx, convID, time.Now(), 5))

	result := callGetConversation(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
	})

	assert.NotNil(t, result.Conversation, "conversation should not be nil")
	assert.Equal(t, convID, result.Conversation.ID, "conversation ID should match")
	assert.Equal(t, "alice", result.Conversation.UserID1)
	assert.Equal(t, "bob", result.Conversation.UserID2)
	assert.Equal(t, int64(5), result.UnreadCount, "alice has not read any messages, so all 5 are unread")
}

// ---------------------------------------------------------------------------
// GC-02: MissingConversationID
// ---------------------------------------------------------------------------

func TestGetConversation_MissingConversationID(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetConversationHandler(s)

	ctx := context.Background()
	client := server.NewTestClient("alice")
	req := newTestRequest("req-get-conv", "get_conversation", map[string]interface{}{})
	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conversation_id")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// GC-03: NotFound
// ---------------------------------------------------------------------------

func TestGetConversation_NotFound(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetConversationHandler(s)

	ctx := context.Background()
	client := server.NewTestClient("alice")
	req := newTestRequest("req-get-conv", "get_conversation", map[string]interface{}{
		"conversation_id": "nonexistent-conv-id",
	})
	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// GC-04: NotMember
// ---------------------------------------------------------------------------

func TestGetConversation_NotMember(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetConversationHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")

	ctx := context.Background()
	client := server.NewTestClient("charlie")
	req := newTestRequest("req-get-conv", "get_conversation", map[string]interface{}{
		"conversation_id": convID,
	})
	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a member")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// GC-05: SoftDeleted (D-013)
// ---------------------------------------------------------------------------

func TestGetConversation_SoftDeleted(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetConversationHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")

	// Soft-delete the conversation.
	ctx := context.Background()
	require.NoError(t, s.ConversationStore().Delete(ctx, convID))

	client := server.NewTestClient("alice")
	req := newTestRequest("req-get-conv", "get_conversation", map[string]interface{}{
		"conversation_id": convID,
	})
	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// GC-06: UnreadCountCorrect (D-012)
// ---------------------------------------------------------------------------

func TestGetConversation_UnreadCountCorrect(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetConversationHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")

	ctx := context.Background()

	// Seed 10 messages from bob (MessageIDs 1..10).
	seedTestMessagesFrom(t, s, convID, "bob", 10, 1)

	// Update conversation metadata.
	require.NoError(t, s.ConversationStore().UpdateLastMessage(ctx, convID, time.Now(), 10))

	// Set alice's read cursor to 7 → messages 8, 9, 10 are unread (3 unread).
	require.NoError(t, s.ConversationStore().UpdateLastRead(ctx, convID, "alice", 7))

	result := callGetConversation(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
	})

	assert.Equal(t, int64(3), result.UnreadCount,
		"alice has read up to MessageID=7, so messages 8-10 (3 messages) are unread (D-012)")

	// Verify bob's view: bob's read cursor is 0, so all 10 are unread for bob.
	bobResult := callGetConversation(t, handler, "bob", map[string]interface{}{
		"conversation_id": convID,
	})
	assert.Equal(t, int64(10), bobResult.UnreadCount,
		"bob has not read any messages, so all 10 are unread")

	// Now advance bob's read cursor past all messages.
	require.NoError(t, s.ConversationStore().UpdateLastRead(ctx, convID, "bob", 10))

	bobResult2 := callGetConversation(t, handler, "bob", map[string]interface{}{
		"conversation_id": convID,
	})
	assert.Equal(t, int64(0), bobResult2.UnreadCount,
		"bob has read all messages, so unread count should be 0")
}
