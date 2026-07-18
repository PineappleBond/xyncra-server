package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/agent"
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
	handler := NewSendMessageHandler(s, broker, nil, nil)
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

	// Verify wire-format: JSON keys must be snake_case (TS client compatibility).
	var rawMsgMap map[string]any
	require.NoError(t, json.Unmarshal(data, &rawMsgMap))
	wrapperMap := rawMsgMap["message"].(map[string]any)
	for _, key := range []string{"id", "conversation_id", "sender_id", "client_message_id", "message_id", "content", "type", "reply_to", "status", "created_at"} {
		assert.Contains(t, wrapperMap, key, "message JSON should contain snake_case key %q", key)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Idempotent duplicate (D-006)
// ---------------------------------------------------------------------------

func TestSendMessage_IdempotentDuplicate(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker, nil, nil)
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
	handler := NewSendMessageHandler(s, broker, nil, nil)
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
	handler := NewSendMessageHandler(s, broker, nil, nil)
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
	handler := NewSendMessageHandler(s, broker, nil, nil)
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
		// D-091: empty/missing content is intentionally allowed at the handler
		// level; the CLI layer enforces --content presence, and the Agent
		// returns a user-friendly error for empty content.
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
	handler := NewSendMessageHandler(s, broker, nil, nil)
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
	handler := NewSendMessageHandler(s, broker, nil, nil)
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
	handler := NewSendMessageHandler(s, broker, nil, nil)
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
	handler := NewSendMessageHandler(s, broker, nil, nil)
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
	handler := NewSendMessageHandler(s, broker, nil, nil)
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

// ---------------------------------------------------------------------------
// Bug #7: Concurrent idempotency — ErrDuplicateKey catch
// ---------------------------------------------------------------------------

// TestSendMessage_ConcurrentIdempotency verifies that two concurrent sends
// with the same client_message_id result in exactly one message in the
// database, with one returning duplicate=false and the other duplicate=true
// (or both seeing the same persisted message via ErrDuplicateKey catch).
func TestSendMessage_ConcurrentIdempotency(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker, nil, nil)
	ctx := context.Background()

	convID := "conv-concurrent-1"
	createTestConversation(t, s, convID, "alice", "bob")

	clientMsgID := uuid.New().String()
	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": clientMsgID,
		"content":           "Concurrent send",
	}

	client := server.NewTestClient("alice")

	// Launch two concurrent sends with the same client_message_id.
	type result struct {
		data json.RawMessage
		err  error
	}
	results := make(chan result, 2)

	for i := 0; i < 2; i++ {
		go func() {
			req := newTestRequest("req-concurrent", "send_message", params)
			data, err := handler.HandleRequest(ctx, client, req)
			results <- result{data, err}
		}()
	}

	var msgs []*model.Message
	var duplicates []bool
	for i := 0; i < 2; i++ {
		r := <-results
		require.NoError(t, r.err)
		msg, dup := parseSendMessageResponse(t, r.data)
		msgs = append(msgs, msg)
		duplicates = append(duplicates, dup)
	}

	// Both should return the same message ID.
	assert.Equal(t, msgs[0].ID, msgs[1].ID, "concurrent sends should return the same message ID")

	// Exactly one should be duplicate=false, the other duplicate=true.
	dupCount := 0
	nonDupCount := 0
	for _, d := range duplicates {
		if d {
			dupCount++
		} else {
			nonDupCount++
		}
	}
	assert.Equal(t, 1, nonDupCount, "exactly one send should return duplicate=false")
	assert.Equal(t, 1, dupCount, "exactly one send should return duplicate=true")

	// Verify only one message in DB.
	dbMsgs, err := s.MessageStore().ListByConversation(ctx, convID, 0, 100)
	require.NoError(t, err)
	assert.Len(t, dbMsgs, 1, "should only have one message in database after concurrent sends")
}

// TestSendMessage_EnqueueError_DuplicateNotAffected verifies that when a
// duplicate is detected via ErrDuplicateKey, the handler returns the existing
// message regardless of whether the broker succeeds or fails.
func TestSendMessage_EnqueueError_DuplicateNotAffected(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &failingBroker{} // always fails
	handler := NewSendMessageHandler(s, broker, nil, nil)
	ctx := context.Background()

	convID := "conv-dup-fail-1"
	createTestConversation(t, s, convID, "alice", "bob")

	clientMsgID := uuid.New().String()

	// First send — succeeds despite broker failure (D-007).
	params1 := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": clientMsgID,
		"content":           "First send",
	}
	client := server.NewTestClient("alice")
	req1 := newTestRequest("req-1", "send_message", params1)
	data1, err1 := handler.HandleRequest(ctx, client, req1)
	require.NoError(t, err1)
	msg1, dup1 := parseSendMessageResponse(t, data1)
	assert.False(t, dup1)

	// Second send — duplicate, should return same message, broker still fails
	// but that's OK (fire-and-forget).
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
}

// ---------------------------------------------------------------------------
// Agent routing test mocks
// ---------------------------------------------------------------------------

// recordingBroker is a mock broker that records all Enqueue calls for test assertions.
type recordingBroker struct {
	mq.Broker // embed to satisfy the full interface; only Enqueue is overridden
	mu        sync.Mutex
	tasks     []*mq.Task
}

func (b *recordingBroker) Enqueue(ctx context.Context, task *mq.Task, opts ...mq.EnqueueOption) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tasks = append(b.tasks, task)
	return fmt.Sprintf("task-%d", len(b.tasks)), nil
}

func (b *recordingBroker) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.tasks)
}

func (b *recordingBroker) taskAt(index int) *mq.Task {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tasks[index]
}

// nthFailBroker fails on the Nth Enqueue call, succeeding on all others.
type nthFailBroker struct {
	mq.Broker
	mu         sync.Mutex
	failOnCall int
	callCount  int
}

func (b *nthFailBroker) Enqueue(ctx context.Context, task *mq.Task, opts ...mq.EnqueueOption) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.callCount++
	if b.callCount == b.failOnCall {
		return "", fmt.Errorf("simulated enqueue failure on call %d", b.failOnCall)
	}
	return fmt.Sprintf("task-%d", b.callCount), nil
}

// ---------------------------------------------------------------------------
// HP-01: Non-agent peer unchanged — only 1 Enqueue (TypeSendMessage)
// ---------------------------------------------------------------------------

func TestSendMessage_AgentDetection_NonAgentUnchanged(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &recordingBroker{}
	handler := NewSendMessageHandler(s, broker, nil, nil)
	ctx := context.Background()

	convID := "conv-nonagent-1"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Hello Bob",
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-hp01", "send_message", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	msg, dup := parseSendMessageResponse(t, data)
	assert.False(t, dup)
	assert.NotNil(t, msg)

	// Only 1 Enqueue call (TypeSendMessage) because peer "bob" is not an agent.
	assert.Equal(t, 1, broker.callCount(), "expected exactly 1 Enqueue call for non-agent peer")
	assert.Equal(t, mq.TypeSendMessage, broker.taskAt(0).Type)
}

// ---------------------------------------------------------------------------
// HP-02: Registered agent peer — 2 Enqueues (TypeSendMessage + TypeAgentProcess)
// ---------------------------------------------------------------------------

func TestSendMessage_AgentDetection_EnqueuesAgentTask(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &recordingBroker{}
	registry := agent.NewRegistry()
	registry.Register(&agent.AgentConfig{ID: "assistant", Name: "Test Agent", Model: "gpt-4", APIKeyEnv: "TEST_KEY"})
	handler := NewSendMessageHandler(s, broker, registry, nil)
	ctx := context.Background()

	convID := "conv-agent-1"
	createTestConversation(t, s, convID, "alice", "agent/assistant")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Hello Agent",
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-hp02", "send_message", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	msg, dup := parseSendMessageResponse(t, data)
	assert.False(t, dup)
	assert.NotNil(t, msg)

	// 2 Enqueue calls: TypeSendMessage + TypeAgentProcess
	assert.Equal(t, 2, broker.callCount(), "expected 2 Enqueue calls for registered agent peer")
	assert.Equal(t, mq.TypeSendMessage, broker.taskAt(0).Type)
	assert.Equal(t, mq.TypeAgentProcess, broker.taskAt(1).Type)
}

// ---------------------------------------------------------------------------
// HP-03: Agent process payload correctness
// ---------------------------------------------------------------------------

func TestSendMessage_AgentDetection_PayloadCorrectness(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &recordingBroker{}
	registry := agent.NewRegistry()
	registry.Register(&agent.AgentConfig{ID: "assistant", Name: "Test Agent", Model: "gpt-4", APIKeyEnv: "TEST_KEY"})
	handler := NewSendMessageHandler(s, broker, registry, nil)
	ctx := context.Background()

	convID := "conv-agent-payload-1"
	createTestConversation(t, s, convID, "alice", "agent/assistant")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Hello Agent",
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-hp03", "send_message", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	msg, _ := parseSendMessageResponse(t, data)
	require.Equal(t, 2, broker.callCount())

	// Unmarshal the second Enqueue payload as agentProcessPayload.
	var payload agentProcessPayload
	require.NoError(t, json.Unmarshal(broker.taskAt(1).Payload, &payload))

	assert.Equal(t, msg.ID, payload.MessageID, "payload message_id should match persisted message")
	assert.Equal(t, convID, payload.ConversationID, "payload conversation_id should match conversation")
	assert.Equal(t, "agent/assistant", payload.AgentID, "payload agent_id should be full agent userID")
	assert.Equal(t, "alice", payload.SenderID, "payload sender_id should be the human sender")
}

// ---------------------------------------------------------------------------
// EC-01: peerUserID helper function
// ---------------------------------------------------------------------------

func TestSendMessage_PeerUserID(t *testing.T) {
	conv := &model.Conversation{UserID1: "alice", UserID2: "bob"}

	assert.Equal(t, "bob", peerUserID(conv, "alice"), "sender=UserID1 should return UserID2")
	assert.Equal(t, "alice", peerUserID(conv, "bob"), "sender=UserID2 should return UserID1")
	assert.Equal(t, "", peerUserID(conv, "charlie"), "sender not in conversation should return empty string")
}

// ---------------------------------------------------------------------------
// EC-03: Nil registry — no panic, only 1 Enqueue (D-063)
// ---------------------------------------------------------------------------

func TestSendMessage_AgentDetection_NilRegistry(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &recordingBroker{}
	// Pass nil registry: agent detection disabled (D-063).
	handler := NewSendMessageHandler(s, broker, nil, nil)
	ctx := context.Background()

	convID := "conv-nilreg-1"
	createTestConversation(t, s, convID, "alice", "agent/assistant")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Hello Agent",
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-ec03", "send_message", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	msg, dup := parseSendMessageResponse(t, data)
	assert.False(t, dup)
	assert.NotNil(t, msg)

	// With nil registry, only 1 Enqueue (TypeSendMessage), no agent task.
	assert.Equal(t, 1, broker.callCount(), "nil registry should result in only 1 Enqueue call")
	assert.Equal(t, mq.TypeSendMessage, broker.taskAt(0).Type)
}

// ---------------------------------------------------------------------------
// EC-04: Empty agent ID — peerID = "agent/" (no ID suffix)
// ---------------------------------------------------------------------------

func TestSendMessage_AgentDetection_EmptyAgentID(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &recordingBroker{}
	registry := agent.NewRegistry()
	// Do NOT register any agent with empty ID.
	handler := NewSendMessageHandler(s, broker, registry, nil)
	ctx := context.Background()

	convID := "conv-emptyid-1"
	// peerID will be "agent/" — IsAgent returns false for empty ID suffix.
	createTestConversation(t, s, convID, "alice", "agent/")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Hello",
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-ec04", "send_message", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	msg, dup := parseSendMessageResponse(t, data)
	assert.False(t, dup)
	assert.NotNil(t, msg)

	// "agent/" has no ID suffix, so IsAgent returns false; only 1 Enqueue.
	assert.Equal(t, 1, broker.callCount(), "empty agent ID should result in only 1 Enqueue call")
}

// ---------------------------------------------------------------------------
// EC-05: Unregistered agent peer — only 1 Enqueue
// ---------------------------------------------------------------------------

func TestSendMessage_AgentDetection_UnregisteredAgent(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &recordingBroker{}
	registry := agent.NewRegistry()
	// Registry exists but "nonexistent" is NOT registered.
	handler := NewSendMessageHandler(s, broker, registry, nil)
	ctx := context.Background()

	convID := "conv-unreg-1"
	createTestConversation(t, s, convID, "alice", "agent/nonexistent")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Hello",
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-ec05", "send_message", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	msg, dup := parseSendMessageResponse(t, data)
	assert.False(t, dup)
	assert.NotNil(t, msg)

	// Agent "nonexistent" is not registered; only 1 Enqueue.
	assert.Equal(t, 1, broker.callCount(), "unregistered agent should result in only 1 Enqueue call")
}

// ---------------------------------------------------------------------------
// EC-06: Agent sender — anti-recursion guard (D-062)
// ---------------------------------------------------------------------------

func TestSendMessage_AgentDetection_AgentSenderNoRecursion(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &recordingBroker{}
	registry := agent.NewRegistry()
	registry.Register(&agent.AgentConfig{ID: "assistant", Name: "Test Agent", Model: "gpt-4", APIKeyEnv: "TEST_KEY"})
	handler := NewSendMessageHandler(s, broker, registry, nil)
	ctx := context.Background()

	convID := "conv-recursion-1"
	createTestConversation(t, s, convID, "agent/assistant", "alice")

	// Agent is the sender; anti-recursion guard must prevent agent task enqueue.
	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Agent reply",
	}
	client := server.NewTestClient("agent/assistant")
	req := newTestRequest("req-ec06", "send_message", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	msg, dup := parseSendMessageResponse(t, data)
	assert.False(t, dup)
	assert.NotNil(t, msg)

	// Sender has "agent/" prefix; anti-recursion guard fires, only 1 Enqueue.
	assert.Equal(t, 1, broker.callCount(), "agent sender should not trigger agent task (anti-recursion)")
	assert.Equal(t, mq.TypeSendMessage, broker.taskAt(0).Type)
}

// ---------------------------------------------------------------------------
// EC-07: Second Enqueue (agent task) fails — handler still succeeds (D-007)
// ---------------------------------------------------------------------------

func TestSendMessage_AgentDetection_EnqueueAgentFails(t *testing.T) {
	s := setupTestSQLite(t)
	// Fail on call 2 (the agent task enqueue); call 1 (send_message) succeeds.
	broker := &nthFailBroker{failOnCall: 2}
	registry := agent.NewRegistry()
	registry.Register(&agent.AgentConfig{ID: "assistant", Name: "Test Agent", Model: "gpt-4", APIKeyEnv: "TEST_KEY"})
	handler := NewSendMessageHandler(s, broker, registry, nil)
	ctx := context.Background()

	convID := "conv-enqfail-1"
	createTestConversation(t, s, convID, "alice", "agent/assistant")

	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": uuid.New().String(),
		"content":           "Hello Agent",
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-ec07", "send_message", params)

	// Handler must succeed despite agent enqueue failure (fire-and-forget, D-007).
	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err, "handler should succeed even when agent enqueue fails")

	msg, dup := parseSendMessageResponse(t, data)
	assert.False(t, dup)
	assert.NotNil(t, msg)
	assert.Equal(t, "Hello Agent", msg.Content)

	// Verify message was persisted.
	persisted, err := s.MessageStore().Get(ctx, msg.ID)
	require.NoError(t, err)
	assert.Equal(t, msg.ID, persisted.ID)
}

// ---------------------------------------------------------------------------
// EP-01: Persist fails — 0 additional Enqueues (agent detection is after persist)
//
// Strategy: send the same message twice. The second send triggers the
// idempotent duplicate path (ErrDuplicateKey catch in handler), which returns
// before reaching step 5b (agent detection). This proves agent detection is
// placed after persist: the duplicate path short-circuits before it.
// ---------------------------------------------------------------------------

func TestSendMessage_AgentDetection_PersistFailsNoEnqueue(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &recordingBroker{}
	registry := agent.NewRegistry()
	registry.Register(&agent.AgentConfig{ID: "assistant", Name: "Test Agent", Model: "gpt-4", APIKeyEnv: "TEST_KEY"})
	handler := NewSendMessageHandler(s, broker, registry, nil)
	ctx := context.Background()

	convID := "conv-persistfail-1"
	createTestConversation(t, s, convID, "alice", "agent/assistant")

	clientMsgID := uuid.New().String()
	params := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": clientMsgID,
		"content":           "Hello Agent",
	}
	client := server.NewTestClient("alice")

	// First send: succeeds normally.
	req1 := newTestRequest("req-ep01-1", "send_message", params)
	data1, err1 := handler.HandleRequest(ctx, client, req1)
	require.NoError(t, err1)
	msg1, dup1 := parseSendMessageResponse(t, data1)
	assert.False(t, dup1)
	assert.NotNil(t, msg1)

	countAfterFirst := broker.callCount()
	assert.Equal(t, 2, countAfterFirst, "first send should produce 2 Enqueue calls")

	// Second send with same client_message_id: duplicate detected via ErrDuplicateKey.
	// The handler returns the existing message before reaching agent detection (step 5b).
	params2 := map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": clientMsgID,
		"content":           "Hello Agent again",
	}
	req2 := newTestRequest("req-ep01-2", "send_message", params2)
	data2, err2 := handler.HandleRequest(ctx, client, req2)
	require.NoError(t, err2)
	msg2, dup2 := parseSendMessageResponse(t, data2)
	assert.True(t, dup2, "second send should return duplicate=true")
	assert.Equal(t, msg1.ID, msg2.ID, "duplicate should return same message ID")

	// No additional Enqueue calls on the duplicate path (agent detection is after persist).
	assert.Equal(t, countAfterFirst, broker.callCount(),
		"duplicate send should not trigger any additional Enqueue calls (agent detection is after persist)")
}

// ---------------------------------------------------------------------------
// Phase 6: agentProcessPayload DeviceID JSON field (DEV-03)
// ---------------------------------------------------------------------------

func TestAgentProcessPayload_DeviceID(t *testing.T) {
	payload := agentProcessPayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test",
		SenderID:       "alice",
		DeviceID:       "device-1",
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "device-1", decoded["device_id"])
}
