package handler

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseCreateConversationResponse unmarshals the handler's response data.
func parseCreateConversationResponse(t *testing.T, data json.RawMessage) (*model.Conversation, bool) {
	t.Helper()
	var resp struct {
		Conversation *model.Conversation `json:"conversation"`
		Duplicate    bool                `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(data, &resp))
	return resp.Conversation, resp.Duplicate
}

// ---------------------------------------------------------------------------
// Test 1: Happy path — basic creation (D-011)
// ---------------------------------------------------------------------------

func TestCreateConversation_HappyPath(t *testing.T) {
	// D-011: find-or-create idempotency model for create_conversation.
	s := setupTestSQLite(t)
	handler := NewCreateConversationHandler(s, nil, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
	}
	req := newTestRequest("req-1", "create_conversation", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	conv, duplicate := parseCreateConversationResponse(t, data)
	assert.False(t, duplicate, "first creation should return duplicate=false (D-011)")
	require.NotNil(t, conv)
	assert.NotEmpty(t, conv.ID, "conversation should have an ID")
	assert.Equal(t, "alice", conv.UserID1, "UserID1 should be the caller (D-011)")
	assert.Equal(t, "bob", conv.UserID2, "UserID2 should be the target user (D-011)")
	assert.Equal(t, "1-on-1", conv.Type, "type should be 1-on-1 (D-011)")
	assert.NotZero(t, conv.CreatedAt, "CreatedAt should be set")
	assert.NotZero(t, conv.UpdatedAt, "UpdatedAt should be set")
	assert.NotZero(t, conv.LastMessageAt, "LastMessageAt should be set")

	// Verify persistence.
	persisted, err := s.ConversationStore().Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, persisted.ID)
	assert.Equal(t, "alice", persisted.UserID1)
	assert.Equal(t, "bob", persisted.UserID2)

	// Verify wire-format: JSON keys must be snake_case (TS client compatibility).
	var rawConvMap map[string]any
	require.NoError(t, json.Unmarshal(data, &rawConvMap))
	wrapperMap := rawConvMap["conversation"].(map[string]any)
	for _, key := range []string{"id", "user_id1", "user_id2", "type", "title", "created_at", "updated_at", "last_message_at"} {
		assert.Contains(t, wrapperMap, key, "conversation JSON should contain snake_case key %q", key)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Creation with title (D-011)
// ---------------------------------------------------------------------------

func TestCreateConversation_WithTitle(t *testing.T) {
	// D-011: optional title should be persisted.
	s := setupTestSQLite(t)
	handler := NewCreateConversationHandler(s, nil, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
		"title":   "Project discussion",
	}
	req := newTestRequest("req-2", "create_conversation", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	conv, duplicate := parseCreateConversationResponse(t, data)
	assert.False(t, duplicate, "first creation should return duplicate=false (D-011)")
	assert.Equal(t, "Project discussion", conv.Title, "title should be persisted (D-011)")

	// Verify persistence.
	persisted, err := s.ConversationStore().Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, "Project discussion", persisted.Title)
}

// ---------------------------------------------------------------------------
// Test 3: Idempotent duplicate — same user calls again (D-011)
// ---------------------------------------------------------------------------

func TestCreateConversation_IdempotentDuplicate(t *testing.T) {
	// D-011: repeated call by the same user returns the existing conversation
	// with duplicate=true and the same conversation ID.
	s := setupTestSQLite(t)
	handler := NewCreateConversationHandler(s, nil, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
	}

	// First call — creates new conversation.
	req1 := newTestRequest("req-1", "create_conversation", params)
	data1, err1 := handler.HandleRequest(ctx, client, req1)
	require.NoError(t, err1)

	conv1, dup1 := parseCreateConversationResponse(t, data1)
	assert.False(t, dup1, "first call should return duplicate=false (D-011)")
	require.NotNil(t, conv1)

	// Second call — should return existing conversation.
	req2 := newTestRequest("req-2", "create_conversation", params)
	data2, err2 := handler.HandleRequest(ctx, client, req2)
	require.NoError(t, err2)

	conv2, dup2 := parseCreateConversationResponse(t, data2)
	assert.True(t, dup2, "second call should return duplicate=true (D-011)")
	assert.Equal(t, conv1.ID, conv2.ID, "idempotent call should return the same conversation ID (D-011)")
}

// ---------------------------------------------------------------------------
// Test 4: Reverse idempotent — Bob calls create(Alice) (D-011)
// ---------------------------------------------------------------------------

func TestCreateConversation_ReverseIdempotent(t *testing.T) {
	// D-011: GetByUsers checks both (user1,user2) and (user2,user1) orderings,
	// so a call in the reverse direction should still find the existing
	// conversation and return duplicate=true.
	s := setupTestSQLite(t)
	handler := NewCreateConversationHandler(s, nil, nil)
	ctx := context.Background()

	// Alice creates conversation with Bob.
	aliceClient := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
	}
	req1 := newTestRequest("req-1", "create_conversation", params)
	data1, err1 := handler.HandleRequest(ctx, aliceClient, req1)
	require.NoError(t, err1)

	conv1, dup1 := parseCreateConversationResponse(t, data1)
	assert.False(t, dup1, "first creation by Alice should return duplicate=false (D-011)")

	// Bob calls create_conversation with Alice — should find existing.
	bobClient := server.NewTestClient("bob")
	params2 := map[string]interface{}{
		"user_id": "alice",
	}
	req2 := newTestRequest("req-2", "create_conversation", params2)
	data2, err2 := handler.HandleRequest(ctx, bobClient, req2)
	require.NoError(t, err2)

	conv2, dup2 := parseCreateConversationResponse(t, data2)
	assert.True(t, dup2, "reverse call by Bob should return duplicate=true (D-011)")
	assert.Equal(t, conv1.ID, conv2.ID, "reverse call should return the same conversation ID (D-011)")
}

// ---------------------------------------------------------------------------
// Test 5: Missing user_id
// ---------------------------------------------------------------------------

func TestCreateConversation_MissingUserID(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewCreateConversationHandler(s, nil, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "user_id completely missing",
			params: map[string]interface{}{},
		},
		{
			name: "user_id is empty string",
			params: map[string]interface{}{
				"user_id": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newTestRequest("req-"+tt.name, "create_conversation", tt.params)
			_, err := handler.HandleRequest(ctx, client, req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "user_id",
				"error should mention 'user_id' (D-011)")
			var handlerErr *protocol.HandlerError
			require.True(t, errors.As(err, &handlerErr))
			assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Test 6: Cannot create conversation with yourself
// ---------------------------------------------------------------------------

func TestCreateConversation_CannotCreateWithSelf(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewCreateConversationHandler(s, nil, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "alice", // same as caller
	}
	req := newTestRequest("req-self", "create_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "yourself",
		"error should mention 'yourself' (D-011)")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 7: DB validation — duplicate create, verify single conversation (D-011)
// ---------------------------------------------------------------------------

func TestCreateConversation_DBConsistency(t *testing.T) {
	// D-011: after two create_conversation calls (one new, one duplicate),
	// the database should contain exactly one conversation between the pair,
	// and any messages sent in that conversation should be unaffected.
	s := setupTestSQLite(t)
	handler := NewCreateConversationHandler(s, nil, nil)
	ctx := context.Background()

	aliceClient := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
	}

	// First call — creates.
	req1 := newTestRequest("req-1", "create_conversation", params)
	data1, err1 := handler.HandleRequest(ctx, aliceClient, req1)
	require.NoError(t, err1)
	conv1, dup1 := parseCreateConversationResponse(t, data1)
	assert.False(t, dup1)

	// Second call — returns duplicate.
	req2 := newTestRequest("req-2", "create_conversation", params)
	data2, err2 := handler.HandleRequest(ctx, aliceClient, req2)
	require.NoError(t, err2)
	conv2, dup2 := parseCreateConversationResponse(t, data2)
	assert.True(t, dup2)
	assert.Equal(t, conv1.ID, conv2.ID)

	// Verify only one conversation exists between alice and bob.
	convs, err := s.ConversationStore().GetByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	count := 0
	for _, c := range convs {
		if c.ID == conv1.ID {
			count++
		}
	}
	assert.Equal(t, 1, count, "should only have one conversation between alice and bob (D-011)")

	// Seed a message into the conversation and verify count remains 1 after
	// another idempotent create call.
	sendHandler := NewSendMessageHandler(s, &mockBroker{}, nil, nil)
	msgParams := map[string]interface{}{
		"conversation_id":   conv1.ID,
		"client_message_id": "client-msg-db-check-1",
		"content":           "hello from alice",
	}
	msgReq := newTestRequest("req-msg-1", "send_message", msgParams)
	_, err = sendHandler.HandleRequest(ctx, aliceClient, msgReq)
	require.NoError(t, err)

	msgs, err := s.MessageStore().ListByConversation(ctx, conv1.ID, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, len(msgs), "should have exactly 1 message after send (D-011)")

	// Third idempotent create call — should not affect message count.
	req3 := newTestRequest("req-3", "create_conversation", params)
	data3, err3 := handler.HandleRequest(ctx, aliceClient, req3)
	require.NoError(t, err3)
	_, dup3 := parseCreateConversationResponse(t, data3)
	assert.True(t, dup3, "third call should still return duplicate=true (D-011)")

	msgsAfter, err := s.MessageStore().ListByConversation(ctx, conv1.ID, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, len(msgsAfter), "message count should remain 1 after idempotent create (D-011)")
}

// ---------------------------------------------------------------------------
// Bug #5: UserUpdate creation, MQ enqueue, broker failure, duplicate
// ---------------------------------------------------------------------------

// countingBroker is a mock broker that counts Enqueue calls.
type countingBroker struct {
	mq.Broker
	mu    sync.Mutex
	calls int
}

func (b *countingBroker) Enqueue(ctx context.Context, task *mq.Task, opts ...mq.EnqueueOption) (string, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	return "task-id", nil
}

func (b *countingBroker) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// TestCreateConversation_UserUpdateCreated verifies that creating a new
// conversation creates UserUpdate records for both conversation members (D-045).
func TestCreateConversation_UserUpdateCreated(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &mockBroker{}
	handler := NewCreateConversationHandler(s, broker, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
	}
	req := newTestRequest("req-1", "create_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	// Verify UserUpdate records exist for both alice and bob.
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates, 1, "alice should have 1 UserUpdate from conversation creation (D-045)")
	assert.Equal(t, protocol.UpdateTypeConversation, aliceUpdates[0].Type)

	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 10)
	require.NoError(t, err)
	assert.Len(t, bobUpdates, 1, "bob should have 1 UserUpdate from conversation creation (D-045)")
	assert.Equal(t, protocol.UpdateTypeConversation, bobUpdates[0].Type)

	// Both updates should have seq=1 (first update for each user).
	assert.Equal(t, uint32(1), aliceUpdates[0].Seq)
	assert.Equal(t, uint32(1), bobUpdates[0].Seq)
}

// TestCreateConversation_MQEnqueue verifies that creating a new conversation
// enqueues an MQ task for broadcasting to both members.
func TestCreateConversation_MQEnqueue(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &countingBroker{}
	handler := NewCreateConversationHandler(s, broker, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
	}
	req := newTestRequest("req-1", "create_conversation", params)

	_, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	// Should have enqueued exactly one MQ task.
	assert.Equal(t, 1, broker.count(), "should enqueue exactly one MQ task on new conversation (D-045)")
}

// TestCreateConversation_BrokerFailureStillSucceeds verifies that a broker
// enqueue failure does not affect the main response (D-007 fire-and-forget).
func TestCreateConversation_BrokerFailureStillSucceeds(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &failingBroker{} // always fails
	handler := NewCreateConversationHandler(s, broker, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
	}
	req := newTestRequest("req-1", "create_conversation", params)

	// Should succeed despite broker failure.
	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err, "handler should succeed even when broker fails (D-007)")

	conv, duplicate := parseCreateConversationResponse(t, data)
	assert.False(t, duplicate)
	require.NotNil(t, conv)
	assert.NotEmpty(t, conv.ID)

	// Verify conversation was persisted.
	persisted, err := s.ConversationStore().Get(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, persisted.ID)
}

// TestCreateConversation_DuplicateDoesNotTriggerMQ verifies that an idempotent
// duplicate call does not create additional UserUpdates or enqueue MQ tasks.
func TestCreateConversation_DuplicateDoesNotTriggerMQ(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &countingBroker{}
	handler := NewCreateConversationHandler(s, broker, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
	}

	// First call — creates new conversation.
	req1 := newTestRequest("req-1", "create_conversation", params)
	_, err := handler.HandleRequest(ctx, client, req1)
	require.NoError(t, err)
	assert.Equal(t, 1, broker.count(), "first call should enqueue one MQ task")

	// Check UserUpdates after first call.
	aliceUpdates1, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates1, 1)

	// Second call — duplicate.
	req2 := newTestRequest("req-2", "create_conversation", params)
	data2, err := handler.HandleRequest(ctx, client, req2)
	require.NoError(t, err)

	_, dup2 := parseCreateConversationResponse(t, data2)
	assert.True(t, dup2, "second call should return duplicate=true")

	// Broker should NOT have been called again.
	assert.Equal(t, 1, broker.count(), "duplicate call should not enqueue additional MQ tasks")

	// UserUpdates should remain at 1 for alice.
	aliceUpdates2, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 10)
	require.NoError(t, err)
	assert.Len(t, aliceUpdates2, 1, "duplicate call should not create additional UserUpdates")
}

// TestCreateConversation_NilBroker verifies that passing nil broker does not
// panic (fire-and-forget with nil is safe).
func TestCreateConversation_NilBroker(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewCreateConversationHandler(s, nil, nil) // nil broker
	ctx := context.Background()

	client := server.NewTestClient("alice")
	params := map[string]interface{}{
		"user_id": "bob",
	}
	req := newTestRequest("req-1", "create_conversation", params)

	// Should not panic.
	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	conv, duplicate := parseCreateConversationResponse(t, data)
	assert.False(t, duplicate)
	require.NotNil(t, conv)
}
