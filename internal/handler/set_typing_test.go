package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers (mocks)
// ---------------------------------------------------------------------------

// recordingBroadcaster records all broadcast calls for assertion.
type recordingBroadcaster struct {
	mu    sync.Mutex
	calls []broadcastCall
}

type broadcastCall struct {
	userID  string
	updates *protocol.PackageDataUpdates
}

func (rb *recordingBroadcaster) broadcast(userID string, updates *protocol.PackageDataUpdates) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.calls = append(rb.calls, broadcastCall{userID: userID, updates: updates})
	return nil
}

// failingBroadcaster always returns an error.
type failingBroadcaster struct{}

func (fb *failingBroadcaster) broadcast(userID string, updates *protocol.PackageDataUpdates) error {
	return fmt.Errorf("simulated broadcast failure")
}

// callSetTyping is a convenience helper that builds a request, invokes the
// handler, and returns the raw response data.
func callSetTyping(
	t *testing.T,
	h *setTypingHandler,
	userID string,
	params map[string]interface{},
	reqID string,
) (json.RawMessage, error) {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient(userID)
	req := newTestRequest(reqID, "set_typing", params)
	return h.HandleRequest(ctx, client, req)
}

// parseSetTypingResponse unmarshals the set_typing success response payload.
func parseSetTypingResponse(t *testing.T, data json.RawMessage) map[string]string {
	t.Helper()
	var resp map[string]string
	require.NoError(t, json.Unmarshal(data, &resp))
	return resp
}

// ---------------------------------------------------------------------------
// Test 1: BasicFlow — happy path with is_typing=true
// ---------------------------------------------------------------------------

func TestSetTyping_BasicFlow(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)

	convID := "conv-typing-1"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"is_typing":       true,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "set_typing", params)

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	resp := parseSetTypingResponse(t, data)
	assert.Equal(t, "ok", resp["status"])

	// Verify broadcast went to both alice and bob (D-050: all members including caller).
	rb.mu.Lock()
	defer rb.mu.Unlock()
	require.Len(t, rb.calls, 2, "broadcast should be called exactly twice (alice + bob)")
	broadcastTargets := []string{rb.calls[0].userID, rb.calls[1].userID}
	assert.ElementsMatch(t, []string{"alice", "bob"}, broadcastTargets,
		"broadcast should target both alice and bob")

	// Verify payload contents.
	require.Len(t, rb.calls[0].updates.Updates, 1)
	update := rb.calls[0].updates.Updates[0]
	assert.Equal(t, uint32(0), update.Seq, "ephemeral update must have Seq=0")
	assert.Equal(t, protocol.UpdateTypeTyping, update.Type)

	var payload typingBroadcastPayload
	require.NoError(t, json.Unmarshal(update.Payload, &payload))
	assert.Equal(t, "alice", payload.UserID)
	assert.Equal(t, convID, payload.ConversationID)
	assert.True(t, payload.IsTyping)
	assert.NotZero(t, payload.Timestamp)
}

// ---------------------------------------------------------------------------
// Test 2: IsTypingFalse — stop typing
// ---------------------------------------------------------------------------

func TestSetTyping_IsTypingFalse(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)

	convID := "conv-typing-2"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"is_typing":       false,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "set_typing", params)

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	resp := parseSetTypingResponse(t, data)
	assert.Equal(t, "ok", resp["status"])

	rb.mu.Lock()
	defer rb.mu.Unlock()
	require.Len(t, rb.calls, 2, "broadcast should be called twice (alice + bob)")

	var payload typingBroadcastPayload
	require.NoError(t, json.Unmarshal(rb.calls[0].updates.Updates[0].Payload, &payload))
	assert.False(t, payload.IsTyping, "is_typing should be false")
}

// ---------------------------------------------------------------------------
// Test 3: MissingConversationID — validation error
// ---------------------------------------------------------------------------

func TestSetTyping_MissingConversationID(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)

	params := map[string]interface{}{
		"is_typing": true,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "set_typing", params)

	_, err := h.HandleRequest(context.Background(), client, req)
	require.Error(t, err)

	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr), "error should be a HandlerError")
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code,
		"missing conversation_id should return code -100")
}

// ---------------------------------------------------------------------------
// Test 4: ConversationNotFound — non-existent conversation
// ---------------------------------------------------------------------------

func TestSetTyping_ConversationNotFound(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)

	params := map[string]interface{}{
		"conversation_id": "nonexistent-conv",
		"is_typing":       true,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-1", "set_typing", params)

	_, err := h.HandleRequest(context.Background(), client, req)
	require.Error(t, err)

	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code,
		"non-existent conversation should return code -101")
}

// ---------------------------------------------------------------------------
// Test 5: CallerNotMember — non-member caller
// ---------------------------------------------------------------------------

func TestSetTyping_CallerNotMember(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)

	convID := "conv-typing-5"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"is_typing":       true,
	}
	client := server.NewTestClient("eve") // eve is not a member
	req := newTestRequest("req-1", "set_typing", params)

	_, err := h.HandleRequest(context.Background(), client, req)
	require.Error(t, err)

	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code,
		"non-member caller should return code -200")
}

// ---------------------------------------------------------------------------
// Test 6: SenderAlsoReceivesBroadcast — caller also receives own typing (D-050)
// ---------------------------------------------------------------------------

func TestSetTyping_SenderAlsoReceivesBroadcast(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)

	convID := "conv-typing-6"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"is_typing":       true,
	}

	data, err := callSetTyping(t, h, "alice", params, "req-1")
	require.NoError(t, err)
	assert.Equal(t, "ok", parseSetTypingResponse(t, data)["status"])

	rb.mu.Lock()
	defer rb.mu.Unlock()
	require.Len(t, rb.calls, 2, "broadcast should be called twice (alice + bob)")
	broadcastTargets := []string{rb.calls[0].userID, rb.calls[1].userID}
	assert.ElementsMatch(t, []string{"alice", "bob"}, broadcastTargets,
		"broadcast should target both alice and bob (D-050)")
}

// ---------------------------------------------------------------------------
// Test 7: NoUserUpdatesCreated — typing events are not persisted
// ---------------------------------------------------------------------------

func TestSetTyping_NoUserUpdatesCreated(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)

	convID := "conv-typing-7"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"is_typing":       true,
	}

	// Record baseline: zero updates for both users before the call.
	ctx := context.Background()
	beforeAlice, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	beforeBob, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 100)
	require.NoError(t, err)

	data, err := callSetTyping(t, h, "alice", params, "req-1")
	require.NoError(t, err)
	assert.Equal(t, "ok", parseSetTypingResponse(t, data)["status"])

	// After the call: no new UserUpdate records for either user.
	afterAlice, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	afterBob, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 100)
	require.NoError(t, err)

	assert.Len(t, afterAlice, len(beforeAlice),
		"set_typing must not create UserUpdate records for alice")
	assert.Len(t, afterBob, len(beforeBob),
		"set_typing must not create UserUpdate records for bob")
}

// ---------------------------------------------------------------------------
// Test 8: BroadcastError_Tolerated — broadcast failure does not affect response
// ---------------------------------------------------------------------------

func TestSetTyping_BroadcastError_Tolerated(t *testing.T) {
	s := setupTestSQLite(t)
	fb := &failingBroadcaster{}
	h := NewSetTypingHandler(s, fb.broadcast)

	convID := "conv-typing-8"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"is_typing":       true,
	}

	// Handler should still return success even though broadcast fails.
	data, err := callSetTyping(t, h, "alice", params, "req-1")
	require.NoError(t, err, "handler should succeed even when broadcast fails (fire-and-forget)")

	resp := parseSetTypingResponse(t, data)
	assert.Equal(t, "ok", resp["status"],
		"response should be status=ok despite broadcast failure")
}

// ---------------------------------------------------------------------------
// Test 9: DeletedConversation — soft-deleted conversation returns NotFound
// ---------------------------------------------------------------------------

func TestSetTyping_DeletedConversation(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)

	convID := "conv-typing-9"
	createTestConversation(t, s, convID, "alice", "bob")

	// Soft-delete the conversation. GORM's scoped queries will then treat it
	// as non-existent, causing ConversationStore.Get to return ErrNotFound.
	ctx := context.Background()
	require.NoError(t, s.ConversationStore().Delete(ctx, convID))

	params := map[string]interface{}{
		"conversation_id": convID,
		"is_typing":       true,
	}

	_, err := callSetTyping(t, h, "alice", params, "req-1")
	require.Error(t, err)

	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code,
		"soft-deleted conversation should return code -101")

	// No broadcast should have happened.
	rb.mu.Lock()
	defer rb.mu.Unlock()
	assert.Empty(t, rb.calls, "no broadcast should occur for a deleted conversation")
}

// ---------------------------------------------------------------------------
// Test 10: RateLimit — rapid successive calls are throttled
// ---------------------------------------------------------------------------

func TestSetTyping_RateLimit(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)

	convID := "conv-typing-10"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"is_typing":       true,
	}

	// Phase 1: rapid-fire 3 calls. The rate limiter's 1-second window means
	// only the first call should pass through; the next two are silently
	// throttled (but still return status=ok).
	for i := 0; i < 3; i++ {
		data, err := callSetTyping(t, h, "alice", params, fmt.Sprintf("req-%d", i+1))
		require.NoError(t, err)
		assert.Equal(t, "ok", parseSetTypingResponse(t, data)["status"],
			"rate-limited call should still return status=ok")
	}

	// Inspect broadcasts. Lock is held only for the assertion, then dropped
	// BEFORE sleeping — otherwise the handler's subsequent broadcast call
	// would deadlock trying to record into rb.
	func() {
		rb.mu.Lock()
		defer rb.mu.Unlock()
		require.Equal(t, 2, len(rb.calls),
			"only the first call should trigger broadcasts (to both members); subsequent calls should be rate-limited")
		broadcastTargets := []string{rb.calls[0].userID, rb.calls[1].userID}
		assert.ElementsMatch(t, []string{"alice", "bob"}, broadcastTargets,
			"broadcast should target both alice and bob")
	}()

	// Phase 2: wait for the rate limiter window to elapse, then verify a
	// subsequent call does trigger another broadcast.
	time.Sleep(1100 * time.Millisecond)
	data, err := callSetTyping(t, h, "alice", params, "req-after-wait")
	require.NoError(t, err)
	assert.Equal(t, "ok", parseSetTypingResponse(t, data)["status"])

	rb.mu.Lock()
	assert.Equal(t, 4, len(rb.calls),
		"after rate-limit window elapses, the next call should trigger another round of broadcasts (2 members)")
	rb.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Test 11: InvalidJSONParams — malformed JSON returns ValidationError
// ---------------------------------------------------------------------------

func TestSetTyping_InvalidJSONParams(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	req := &protocol.PackageDataRequest{
		ID:     "req-bad-json",
		Method: "set_typing",
		Params: json.RawMessage(`{invalid json}`),
	}

	_, err := h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	var he *protocol.HandlerError
	require.True(t, errors.As(err, &he))
	assert.Equal(t, protocol.ResponseCodeValidationError, he.Code)
}

// ---------------------------------------------------------------------------
// Test 12: IsTypingOmitted_DefaultsFalse — is_typing missing defaults to false
// ---------------------------------------------------------------------------

func TestSetTyping_IsTypingOmitted_DefaultsFalse(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)
	ctx := context.Background()

	convID := "conv-typing-default"
	createTestConversation(t, s, convID, "alice", "bob")

	// Only send conversation_id, omit is_typing.
	params := map[string]interface{}{
		"conversation_id": convID,
	}
	client := server.NewTestClient("alice")
	req := newTestRequest("req-default", "set_typing", params)

	data, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(data, &resp))
	assert.Equal(t, "ok", resp["status"])

	// Verify payload has is_typing=false.
	rb.mu.Lock()
	defer rb.mu.Unlock()
	require.Len(t, rb.calls, 2, "broadcast should be called twice (alice + bob)")
	var payload typingBroadcastPayload
	require.NoError(t, json.Unmarshal(rb.calls[0].updates.Updates[0].Payload, &payload))
	assert.False(t, payload.IsTyping, "omitted is_typing should default to false")
}

// ---------------------------------------------------------------------------
// Test 13: ConcurrentCalls — concurrent calls do not race
// ---------------------------------------------------------------------------

func TestSetTyping_ConcurrentCalls(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewSetTypingHandler(s, rb.broadcast)
	ctx := context.Background()

	convID := "conv-typing-concurrent"
	createTestConversation(t, s, convID, "alice", "bob")

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			params := map[string]interface{}{
				"conversation_id": convID,
				"is_typing":       true,
			}
			client := server.NewTestClient("alice")
			req := newTestRequest("req-concurrent", "set_typing", params)
			_, err := h.HandleRequest(ctx, client, req)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	// Due to rate limiting, only 1-2 of 10 concurrent calls should trigger broadcast.
	// Each broadcast goes to 2 members (alice + bob), so max ~4 calls.
	rb.mu.Lock()
	defer rb.mu.Unlock()
	assert.LessOrEqual(t, len(rb.calls), 4, "rate limiter should allow at most 1-2 broadcast rounds (2 members each)")
}
