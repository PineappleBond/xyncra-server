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

// getMessagesResult is the parsed response from the get_messages handler.
type getMessagesResult struct {
	Messages []*model.Message `json:"messages"`
	HasMore  bool             `json:"has_more"`
}

// parseGetMessagesResponse unmarshals the handler's response data.
func parseGetMessagesResponse(t *testing.T, data json.RawMessage) getMessagesResult {
	t.Helper()
	var result getMessagesResult
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

// callGetMessages is a convenience that builds a request, calls the handler,
// and parses the response. It fails the test on error.
func callGetMessages(t *testing.T, h *getMessagesHandler, userID string, params interface{}) getMessagesResult {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient(userID)
	req := newTestRequest("req-get-msgs", "get_messages", params)
	data, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)
	return parseGetMessagesResponse(t, data)
}

// callGetMessagesExpectError is a convenience that builds a request, calls the
// handler, and asserts that an error is returned containing the given substring.
func callGetMessagesExpectError(t *testing.T, h *getMessagesHandler, userID string, params interface{}, errContains string) {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient(userID)
	req := newTestRequest("req-get-msgs", "get_messages", params)
	_, err := h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), errContains, "error should contain expected substring")
}

// seedTestMessages inserts count messages into the given conversation with
// sequential MessageIDs starting at startMessageID. Each message has a unique
// ID and ClientMessageID.
func seedTestMessages(t *testing.T, s *testSQLiteStore, convID, senderID string, count int, startMessageID uint32) {
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
// GM-01: NormalList_5Messages_HasMoreFalse
// D-009: cursor-based pagination with after_message_id + limit
// ---------------------------------------------------------------------------

func TestGetMessages_NormalList_HasMoreFalse(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 5, 1)

	result := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"limit":           10,
	})

	require.Len(t, result.Messages, 5, "should return all 5 messages")
	assert.False(t, result.HasMore, "has_more should be false when all messages fit in limit (D-009)")
}

// ---------------------------------------------------------------------------
// GM-02: EmptyMessages_ConversationExistsNoMessages
// ---------------------------------------------------------------------------

func TestGetMessages_EmptyMessages(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")

	result := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
	})

	assert.NotNil(t, result.Messages, "messages should not be null")
	assert.Empty(t, result.Messages, "messages should be empty array")
	assert.False(t, result.HasMore, "has_more should be false for empty list")
}

// ---------------------------------------------------------------------------
// GM-03: PaginationHasMore_10Messages_Limit3
// D-009: limit+1 probe technique for has_more detection
// ---------------------------------------------------------------------------

func TestGetMessages_PaginationHasMore(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 10, 1)

	result := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"limit":           3,
	})

	assert.Len(t, result.Messages, 3, "should return exactly 3 messages (limit)")
	assert.True(t, result.HasMore, "has_more should be true when more messages exist beyond limit (D-009)")
}

// ---------------------------------------------------------------------------
// GM-04: AfterMessageID_FetchFromCursor
// D-008: MessageID is monotonically increasing within a conversation
// D-009: after_message_id is exclusive lower bound for pagination
// ---------------------------------------------------------------------------

func TestGetMessages_AfterMessageID(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 5, 1)

	result := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id":  convID,
		"after_message_id": 3,
	})

	require.Len(t, result.Messages, 2, "should return 2 messages (MessageID > 3)")
	assert.Equal(t, uint32(4), result.Messages[0].MessageID, "first message should have MessageID=4 (D-008, D-009)")
	assert.Equal(t, uint32(5), result.Messages[1].MessageID, "second message should have MessageID=5 (D-008, D-009)")
	assert.False(t, result.HasMore, "has_more should be false")
}

// ---------------------------------------------------------------------------
// GM-05: MissingConversationID_Error
// ---------------------------------------------------------------------------

func TestGetMessages_MissingConversationID(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)

	ctx := context.Background()
	client := server.NewTestClient("alice")
	req := newTestRequest("req-get-msgs", "get_messages", map[string]interface{}{
		"limit": 10,
	})
	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conversation_id")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// GM-06: ConversationNotFound_Error
// ---------------------------------------------------------------------------

func TestGetMessages_ConversationNotFound(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)

	ctx := context.Background()
	client := server.NewTestClient("alice")
	req := newTestRequest("req-get-msgs", "get_messages", map[string]interface{}{
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
// GM-07: NotAMember_Error
// C-3: member verification required
// ---------------------------------------------------------------------------

func TestGetMessages_NotAMember(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")

	ctx := context.Background()
	client := server.NewTestClient("charlie")
	req := newTestRequest("req-get-msgs", "get_messages", map[string]interface{}{
		"conversation_id": convID,
	})
	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a member of the conversation")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// GM-08: Sorting_MessageID_ASC
// D-008: MessageID is monotonically increasing within a conversation
// ---------------------------------------------------------------------------

func TestGetMessages_Sorting_MessageIDASC(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 5, 1)

	result := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"limit":           100,
	})

	require.Len(t, result.Messages, 5, "should return all 5 messages")
	for i := 1; i < len(result.Messages); i++ {
		assert.Greater(t, result.Messages[i].MessageID, result.Messages[i-1].MessageID,
			"messages should be ordered by MessageID ASC (D-008)")
	}
}

// ---------------------------------------------------------------------------
// GM-09: UserIsolation_Conv1MessagesNotInConv2
// D-008: MessageID space is independent per conversation
// ---------------------------------------------------------------------------

func TestGetMessages_UserIsolation(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)
	conv1ID := uuid.New().String()
	conv2ID := uuid.New().String()

	createTestConversation(t, s, conv1ID, "alice", "bob")
	createTestConversation(t, s, conv2ID, "alice", "charlie")
	seedTestMessages(t, s, conv1ID, "alice", 3, 1)
	seedTestMessages(t, s, conv2ID, "alice", 4, 1)

	result1 := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": conv1ID,
	})
	result2 := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": conv2ID,
	})

	require.Len(t, result1.Messages, 3, "conv1 should have 3 messages")
	require.Len(t, result2.Messages, 4, "conv2 should have 4 messages")

	// Verify isolation: all messages in conv1 belong to conv1, same for conv2.
	for _, msg := range result1.Messages {
		assert.Equal(t, conv1ID, msg.ConversationID, "all conv1 messages should have conv1ID (D-008)")
	}
	for _, msg := range result2.Messages {
		assert.Equal(t, conv2ID, msg.ConversationID, "all conv2 messages should have conv2ID (D-008)")
	}
}

// ---------------------------------------------------------------------------
// GM-10: BoundaryTest_ZeroAndNegativeLimit
// ---------------------------------------------------------------------------

func TestGetMessages_BoundaryZeroNegativeLimit(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 5, 1)

	// limit=0 should reset to default (50).
	result := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"limit":           0,
	})
	assert.Len(t, result.Messages, 5, "limit=0 should reset to default 50; all 5 should be returned")
	assert.False(t, result.HasMore, "has_more should be false when all messages fit in default limit")

	// limit=-1 should also reset to default (50).
	result2 := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"limit":           -1,
	})
	assert.Len(t, result2.Messages, 5, "limit=-1 should reset to default 50; all 5 should be returned")
}

// ---------------------------------------------------------------------------
// GM-11: BoundaryTest_LimitAboveCap
// ---------------------------------------------------------------------------

func TestGetMessages_BoundaryLimitAboveCap(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewGetMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessages(t, s, convID, "alice", 5, 1)

	// limit=999 should be capped to default 50 (since > 200 resets to default).
	result := callGetMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"limit":           999,
	})
	assert.Len(t, result.Messages, 5, "limit=999 should reset to default 50; all 5 should be returned")
}
