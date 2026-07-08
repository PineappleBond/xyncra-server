package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/mq"
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

// mockBroker is a minimal mock of mq.Broker used for send_message tests.
type mockBroker struct {
	mq.Broker
}

func (m *mockBroker) Enqueue(ctx context.Context, task *mq.Task, opts ...mq.EnqueueOption) (string, error) {
	return "task-id", nil
}

// createTestConversation inserts a 1-on-1 conversation between user1 and user2.
func createTestConversation(t *testing.T, s *testSQLiteStore, id, user1, user2 string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	conv := &model.Conversation{
		ID:            id,
		UserID1:       user1,
		UserID2:       user2,
		Type:          "1-on-1",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastMessageAt: now,
	}
	require.NoError(t, s.ConversationStore().Create(ctx, conv))
}

// failingBroker always returns an error from Enqueue (used for D-007 tests).
type failingBroker struct {
	mq.Broker
}

func (b *failingBroker) Enqueue(ctx context.Context, task *mq.Task, opts ...mq.EnqueueOption) (string, error) {
	return "", fmt.Errorf("simulated enqueue failure")
}

// parseSendMessageResponse unmarshals the handler's response data.
func parseSendMessageResponse(t *testing.T, data json.RawMessage) (*model.Message, bool) {
	t.Helper()
	var resp struct {
		Message   *model.Message `json:"message"`
		Duplicate bool           `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(data, &resp))
	return resp.Message, resp.Duplicate
}

// ---------------------------------------------------------------------------
// Test 1: Happy path
// ---------------------------------------------------------------------------

func TestSendMessage_HappyPath(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	convID := "conv-happy-1"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Hello, Bob!",
		"type":              "text",
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "send_message", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	msg, duplicate := parseSendMessageResponse(t, data)
	assert.False(t, duplicate)
	assert.NotNil(t, msg)
	assert.Equal(t, "Hello, Bob!", msg.Content)
	assert.Equal(t, "text", msg.Type)
	assert.Equal(t, "alice", msg.SenderID)
	assert.Equal(t, convID, msg.ConversationID)
	assert.Equal(t, uint32(1), msg.MessageID)
	assert.Equal(t, params["client_message_id"], msg.ClientMessageID)
	assert.NotEmpty(t, msg.ID)

	// Verify persistence
	persisted, err := s.MessageStore().Get(ctx, msg.ID)
	require.NoError(t, err)
	assert.Equal(t, msg.ID, persisted.ID)
	assert.Equal(t, msg.Content, persisted.Content)

	// Verify conversation updated
	conv, err := s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), conv.LastProcessedMessageID)

	// Verify user updates created for both members
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates, 1)
	assert.Equal(t, protocol.UpdateTypeMessage, aliceUpdates[0].Type, "UserUpdate Type should be 'message'")

	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Len(t, bobUpdates, 1)
	assert.Equal(t, protocol.UpdateTypeMessage, bobUpdates[0].Type, "UserUpdate Type should be 'message'")
}

// ---------------------------------------------------------------------------
// Test 2: Idempotent duplicate (D-006)
// ---------------------------------------------------------------------------

func TestSendMessage_IdempotentDuplicate(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	convID := "conv-idempotent-1"
	createTestConversation(t, s, convID, "alice", "bob")

	clientMsgID := uuid.New().String()
	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": clientMsgID,
		"content":           "First send",
	}

	client := server.NewTestClient("alice")

	// First send
	req1 := newTestRequest("req-1", "send_message", params)
	data1, err1 := handler.HandleRequest(ctx, client, req1)
	require.NoError(t, err1)

	msg1, dup1 := parseSendMessageResponse(t, data1)
	assert.False(t, dup1)
	assert.Equal(t, "First send", msg1.Content)

	// Second send with same client_message_id
	params2 := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": clientMsgID,
		"content":           "Duplicate attempt",
	}
	req2 := newTestRequest("req-2", "send_message", params2)
	data2, err2 := handler.HandleRequest(ctx, client, req2)
	require.NoError(t, err2)

	msg2, dup2 := parseSendMessageResponse(t, data2)
	assert.True(t, dup2, "second send should return duplicate=true")
	assert.Equal(t, msg1.ID, msg2.ID, "duplicate should return same message ID")
	assert.Equal(t, "First send", msg2.Content, "duplicate should return original content")

	// Verify only one message in DB
	msgs, err := s.MessageStore().ListByConversation(ctx, convID, 0, 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 1, "should only have one message in database")

	// Verify idempotent hit did NOT create additional UserUpdates (QA-High-3).
	// After the first send, alice and bob each had exactly 1 UserUpdate.
	// The duplicate send must not create any more.
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates, 1, "idempotent hit should not create additional UserUpdates for alice")

	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Len(t, bobUpdates, 1, "idempotent hit should not create additional UserUpdates for bob")
}

// ---------------------------------------------------------------------------
// Test 3: Conversation not found
// ---------------------------------------------------------------------------

func TestSendMessage_ConversationNotFound(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	params := map[string]interface{}{
		"conversation_id":   "nonexistent-conv",
		"client_message_id": uuid.New().String(),
		"content":           "Test",
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "send_message", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conversation not found")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 4: Sender not a member
// ---------------------------------------------------------------------------

func TestSendMessage_SenderNotMember(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	convID := "conv-member-1"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "I'm not a member",
	}

	client := server.NewTestClient("charlie") // not a member
	req := newTestRequest("req-1", "send_message", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a member")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 5: Invalid parameters
// ---------------------------------------------------------------------------

func TestSendMessage_InvalidParams(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	client := server.NewTestClient("alice")

	tests := []struct {
		name      string
		params    map[string]interface{}
		errSubstr string
	}{
		{
			name: "missing conversation_id",
			params: map[string]interface{}{
				"client_message_id": uuid.New().String(),
				"content":           "Test",
			},
			errSubstr: "conversation_id",
		},
		{
			name: "missing client_message_id",
			params: map[string]interface{}{
				"conversation_id": "conv-1",
				"content":         "Test",
			},
			errSubstr: "client_message_id",
		},
		{
			name: "missing content",
			params: map[string]interface{}{
				"conversation_id":   "conv-1",
				"client_message_id": uuid.New().String(),
			},
			errSubstr: "content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newTestRequest("req-"+tt.name, "send_message", tt.params)
			_, err := handler.HandleRequest(ctx, client, req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errSubstr)
			var handlerErr *protocol.HandlerError
			require.True(t, errors.As(err, &handlerErr))
			assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Test 6: MessageID increment (D-008)
// ---------------------------------------------------------------------------

func TestSendMessage_MessageIDIncrement(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	convID := "conv-msgid-1"
	createTestConversation(t, s, convID, "alice", "bob")

	// Send first message
	params1 := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Message 1",
	}
	req1 := newTestRequest("req-1", "send_message", params1)
	data1, err1 := handler.HandleRequest(ctx, server.NewTestClient("alice"), req1)
	require.NoError(t, err1)

	msg1, _ := parseSendMessageResponse(t, data1)
	assert.Equal(t, uint32(1), msg1.MessageID, "first message should have MessageID=1")

	// Send second message
	params2 := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Message 2",
	}
	req2 := newTestRequest("req-2", "send_message", params2)
	data2, err2 := handler.HandleRequest(ctx, server.NewTestClient("alice"), req2)
	require.NoError(t, err2)

	msg2, _ := parseSendMessageResponse(t, data2)
	assert.Equal(t, uint32(2), msg2.MessageID, "second message should have MessageID=2")

	// Verify conversation LastProcessedMessageID updated
	conv, err := s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), conv.LastProcessedMessageID)
}

// ---------------------------------------------------------------------------
// Test 7: UserUpdate Seq increment per user
// ---------------------------------------------------------------------------

func TestSendMessage_UserUpdateSeqIncrement(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	convID := "conv-seq-1"
	createTestConversation(t, s, convID, "alice", "bob")

	// Send first message
	params1 := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Message 1",
	}
	req1 := newTestRequest("req-1", "send_message", params1)
	_, err1 := handler.HandleRequest(ctx, server.NewTestClient("alice"), req1)
	require.NoError(t, err1)

	// Check first update seq for both users
	aliceUpdates1, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates1, 1)
	assert.Equal(t, uint32(1), aliceUpdates1[0].Seq)
	assert.Equal(t, protocol.UpdateTypeMessage, aliceUpdates1[0].Type, "UserUpdate Type should be 'message'")

	bobUpdates1, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Len(t, bobUpdates1, 1)
	assert.Equal(t, uint32(1), bobUpdates1[0].Seq)
	assert.Equal(t, protocol.UpdateTypeMessage, bobUpdates1[0].Type, "UserUpdate Type should be 'message'")

	// Send second message
	params2 := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Message 2",
	}
	req2 := newTestRequest("req-2", "send_message", params2)
	_, err2 := handler.HandleRequest(ctx, server.NewTestClient("alice"), req2)
	require.NoError(t, err2)

	// Check second update seq for both users
	aliceUpdates2, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates2, 2)
	assert.Equal(t, uint32(1), aliceUpdates2[0].Seq)
	assert.Equal(t, uint32(2), aliceUpdates2[1].Seq)
	assert.Equal(t, protocol.UpdateTypeMessage, aliceUpdates2[0].Type)
	assert.Equal(t, protocol.UpdateTypeMessage, aliceUpdates2[1].Type)

	bobUpdates2, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Len(t, bobUpdates2, 2)
	assert.Equal(t, uint32(1), bobUpdates2[0].Seq)
	assert.Equal(t, uint32(2), bobUpdates2[1].Seq)
	assert.Equal(t, protocol.UpdateTypeMessage, bobUpdates2[0].Type)
	assert.Equal(t, protocol.UpdateTypeMessage, bobUpdates2[1].Type)
}

// ---------------------------------------------------------------------------
// Test 8: Enqueue error does not affect response (D-007)
// ---------------------------------------------------------------------------

func TestSendMessage_EnqueueError(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &failingBroker{} // always fails
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	convID := "conv-enqueue-err-1"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Test fire-and-forget",
	}

	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "send_message", params)

	// Should succeed despite enqueue failure
	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err, "handler should succeed even when enqueue fails")

	msg, duplicate := parseSendMessageResponse(t, data)
	assert.False(t, duplicate)
	assert.NotNil(t, msg)
	assert.Equal(t, "Test fire-and-forget", msg.Content)

	// Verify message was persisted
	persisted, err := s.MessageStore().Get(ctx, msg.ID)
	require.NoError(t, err)
	assert.Equal(t, msg.ID, persisted.ID)
}

// ---------------------------------------------------------------------------
// Test 9: With reply_to
// ---------------------------------------------------------------------------

func TestSendMessage_WithReplyTo(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	convID := "conv-reply-1"
	createTestConversation(t, s, convID, "alice", "bob")

	// Send original message
	params1 := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Original message",
	}
	req1 := newTestRequest("req-1", "send_message", params1)
	data1, err1 := handler.HandleRequest(ctx, server.NewTestClient("alice"), req1)
	require.NoError(t, err1)

	originalMsg, _ := parseSendMessageResponse(t, data1)
	assert.Equal(t, uint32(1), originalMsg.MessageID)

	// Send reply
	params2 := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Reply message",
		"reply_to":          originalMsg.MessageID,
	}
	req2 := newTestRequest("req-2", "send_message", params2)
	data2, err2 := handler.HandleRequest(ctx, server.NewTestClient("bob"), req2)
	require.NoError(t, err2)

	replyMsg, _ := parseSendMessageResponse(t, data2)
	assert.Equal(t, originalMsg.MessageID, replyMsg.ReplyTo, "reply_to should match original message ID")
	assert.Equal(t, "Reply message", replyMsg.Content)
	assert.Equal(t, uint32(2), replyMsg.MessageID)
}

// ---------------------------------------------------------------------------
// Test 10: Multiple messages
// ---------------------------------------------------------------------------

func TestSendMessage_MultipleMessages(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker)
	ctx := context.Background()

	convID := "conv-multi-1"
	createTestConversation(t, s, convID, "alice", "bob")

	// Send 5 messages
	var messages []*model.Message
	for i := 0; i < 5; i++ {
		params := map[string]interface{}{
			"conversation_id":   convID,
			"client_message_id": uuid.New().String(),
			"content":           fmt.Sprintf("Message %d", i+1),
		}
		req := newTestRequest(fmt.Sprintf("req-%d", i+1), "send_message", params)
		data, err := handler.HandleRequest(ctx, server.NewTestClient("alice"), req)
		require.NoError(t, err)

		msg, _ := parseSendMessageResponse(t, data)
		messages = append(messages, msg)
	}

	// Verify MessageID increments
	for i, msg := range messages {
		expectedID := uint32(i + 1)
		assert.Equal(t, expectedID, msg.MessageID, "message %d should have MessageID=%d", i+1, expectedID)
		assert.Equal(t, fmt.Sprintf("Message %d", i+1), msg.Content)
	}

	// Verify all messages persisted
	msgs, err := s.MessageStore().ListByConversation(ctx, convID, 0, 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 5)

	// Verify conversation updated
	conv, err := s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), conv.LastProcessedMessageID)

	// Verify user updates (alice and bob each have 5 updates)
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates, 5)

	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 100)
	require.NoError(t, err)
	assert.Len(t, bobUpdates, 5)

	// Verify seq increments correctly
	for i, update := range aliceUpdates {
		assert.Equal(t, uint32(i+1), update.Seq)
		assert.Equal(t, protocol.UpdateTypeMessage, update.Type, "UserUpdate Type should be 'message'")
	}
	for i, update := range bobUpdates {
		assert.Equal(t, uint32(i+1), update.Seq)
		assert.Equal(t, protocol.UpdateTypeMessage, update.Type, "UserUpdate Type should be 'message'")
	}
}
