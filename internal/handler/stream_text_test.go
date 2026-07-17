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
// Test helpers
// ---------------------------------------------------------------------------

// callStreamText is a convenience helper that builds a request, invokes the
// handler, and returns the raw response data.
func callStreamText(
	t *testing.T,
	h *streamTextHandler,
	userID string,
	params map[string]interface{},
	reqID string,
) (json.RawMessage, error) {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient(userID)
	req := newTestRequest(reqID, "stream_text", params)
	return h.HandleRequest(ctx, client, req)
}

// parseStreamTextResponse unmarshals the stream_text success response payload.
func parseStreamTextResponse(t *testing.T, data json.RawMessage) map[string]string {
	t.Helper()
	var resp map[string]string
	require.NoError(t, json.Unmarshal(data, &resp))
	return resp
}

// ---------------------------------------------------------------------------
// Test 1: BasicFlow — happy path, first streaming frame
// ---------------------------------------------------------------------------

func TestStreamText_BasicFlow(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	convID := "conv-stream-1"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"stream_id":       "stream-1",
		"text":            "hello",
		"is_done":         false,
	}

	data, err := callStreamText(t, h, "alice", params, "req-1")
	require.NoError(t, err)

	resp := parseStreamTextResponse(t, data)
	assert.Equal(t, "ok", resp["status"])

	// Verify broadcast went to ALL members (including caller, D-050).
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
	assert.Equal(t, protocol.UpdateTypeStreaming, update.Type)

	var payload streamingBroadcastPayload
	require.NoError(t, json.Unmarshal(update.Payload, &payload))
	assert.Equal(t, "alice", payload.UserID)
	assert.Equal(t, convID, payload.ConversationID)
	assert.Equal(t, "stream-1", payload.StreamID)
	assert.Equal(t, "hello", payload.Text)
	assert.False(t, payload.IsDone)
	assert.NotZero(t, payload.Timestamp)
}

// ---------------------------------------------------------------------------
// Test 2: MiddleFrames — consecutive frames accumulate text
// ---------------------------------------------------------------------------

func TestStreamText_MiddleFrames(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	convID := "conv-stream-2"
	createTestConversation(t, s, convID, "alice", "bob")

	texts := []string{"h", "he", "hel"}
	for i, text := range texts {
		// Sleep >50ms between calls to avoid rate limiting.
		if i > 0 {
			time.Sleep(80 * time.Millisecond)
		}
		params := map[string]interface{}{
			"conversation_id": convID,
			"stream_id":       "stream-2",
			"text":            text,
			"is_done":         false,
		}
		data, err := callStreamText(t, h, "alice", params, fmt.Sprintf("req-%d", i+1))
		require.NoError(t, err)
		assert.Equal(t, "ok", parseStreamTextResponse(t, data)["status"])
	}

	// Each call broadcasts to 2 members, so total calls = 3 * 2 = 6.
	rb.mu.Lock()
	defer rb.mu.Unlock()
	require.Len(t, rb.calls, 6, "each frame should broadcast to 2 members (3 frames * 2)")

	// Verify each broadcast contains the full text for that frame.
	for i, text := range texts {
		idx := i * 2 // first member of each pair
		var payload streamingBroadcastPayload
		require.NoError(t, json.Unmarshal(rb.calls[idx].updates.Updates[0].Payload, &payload))
		assert.Equal(t, text, payload.Text,
			"frame %d should contain cumulative text %q", i+1, text)
	}
}

// ---------------------------------------------------------------------------
// Test 3: IsDoneFrame — is_done=true frame
// ---------------------------------------------------------------------------

func TestStreamText_IsDoneFrame(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	convID := "conv-stream-3"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"stream_id":       "stream-3",
		"text":            "hello world",
		"is_done":         true,
	}

	data, err := callStreamText(t, h, "alice", params, "req-done")
	require.NoError(t, err)
	assert.Equal(t, "ok", parseStreamTextResponse(t, data)["status"])

	rb.mu.Lock()
	defer rb.mu.Unlock()
	require.Len(t, rb.calls, 2)

	var payload streamingBroadcastPayload
	require.NoError(t, json.Unmarshal(rb.calls[0].updates.Updates[0].Payload, &payload))
	assert.True(t, payload.IsDone, "is_done should be true in the final frame")
	assert.Equal(t, "hello world", payload.Text)
}

// ---------------------------------------------------------------------------
// Test 4: MissingConversationID — validation error
// ---------------------------------------------------------------------------

func TestStreamText_MissingConversationID(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	params := map[string]interface{}{
		"stream_id": "stream-4",
		"text":      "hello",
	}

	_, err := callStreamText(t, h, "alice", params, "req-1")
	require.Error(t, err)

	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr), "error should be a HandlerError")
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code,
		"missing conversation_id should return code -100")
}

// ---------------------------------------------------------------------------
// Test 5: MissingStreamID — validation error
// ---------------------------------------------------------------------------

func TestStreamText_MissingStreamID(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	params := map[string]interface{}{
		"conversation_id": "conv-stream-5",
		"text":            "hello",
	}

	_, err := callStreamText(t, h, "alice", params, "req-1")
	require.Error(t, err)

	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeValidationError, handlerErr.Code,
		"missing stream_id should return code -100")
}

// ---------------------------------------------------------------------------
// Test 6: ConversationNotFound — non-existent conversation
// ---------------------------------------------------------------------------

func TestStreamText_ConversationNotFound(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	params := map[string]interface{}{
		"conversation_id": "nonexistent-conv",
		"stream_id":       "stream-6",
		"text":            "hello",
	}

	_, err := callStreamText(t, h, "alice", params, "req-1")
	require.Error(t, err)

	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code,
		"non-existent conversation should return code -101")
}

// ---------------------------------------------------------------------------
// Test 7: CallerNotMember — non-member caller
// ---------------------------------------------------------------------------

func TestStreamText_CallerNotMember(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	convID := "conv-stream-7"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"stream_id":       "stream-7",
		"text":            "hello",
	}

	_, err := callStreamText(t, h, "eve", params, "req-1")
	require.Error(t, err)

	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodePermissionDenied, handlerErr.Code,
		"non-member caller should return code -200")
}

// ---------------------------------------------------------------------------
// Test 8: SenderAlsoReceivesBroadcast — caller also receives own stream (D-050)
// ---------------------------------------------------------------------------

func TestStreamText_SenderAlsoReceivesBroadcast(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	convID := "conv-stream-8"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"stream_id":       "stream-8",
		"text":            "hello",
		"is_done":         false,
	}

	data, err := callStreamText(t, h, "alice", params, "req-1")
	require.NoError(t, err)
	assert.Equal(t, "ok", parseStreamTextResponse(t, data)["status"])

	rb.mu.Lock()
	defer rb.mu.Unlock()
	require.Len(t, rb.calls, 2, "broadcast should be called twice (alice + bob)")
	broadcastTargets := []string{rb.calls[0].userID, rb.calls[1].userID}
	assert.ElementsMatch(t, []string{"alice", "bob"}, broadcastTargets,
		"broadcast should target both alice and bob (D-050)")
}

// ---------------------------------------------------------------------------
// Test 9: RateLimit — rapid successive calls are throttled (50ms window)
// ---------------------------------------------------------------------------

func TestStreamText_RateLimit(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	convID := "conv-stream-9"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"stream_id":       "stream-9",
		"text":            "hello",
		"is_done":         false,
	}

	// Phase 1: rapid-fire 3 calls within 50ms. Only the first should pass;
	// subsequent calls are silently throttled but still return status=ok.
	for i := 0; i < 3; i++ {
		data, err := callStreamText(t, h, "alice", params, fmt.Sprintf("req-%d", i+1))
		require.NoError(t, err)
		assert.Equal(t, "ok", parseStreamTextResponse(t, data)["status"],
			"rate-limited call should still return status=ok")
	}

	// Inspect broadcasts.
	func() {
		rb.mu.Lock()
		defer rb.mu.Unlock()
		require.Equal(t, 2, len(rb.calls),
			"only the first call should trigger broadcasts (to both members); subsequent calls should be rate-limited")
		broadcastTargets := []string{rb.calls[0].userID, rb.calls[1].userID}
		assert.ElementsMatch(t, []string{"alice", "bob"}, broadcastTargets,
			"broadcast should target both alice and bob")
	}()

	// Phase 2: wait for the rate limiter window (50ms) to elapse.
	time.Sleep(80 * time.Millisecond)
	data, err := callStreamText(t, h, "alice", params, "req-after-wait")
	require.NoError(t, err)
	assert.Equal(t, "ok", parseStreamTextResponse(t, data)["status"])

	rb.mu.Lock()
	assert.Equal(t, 4, len(rb.calls),
		"after rate-limit window elapses, the next call should trigger another round of broadcasts (2 members)")
	rb.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Test 10: NoUserUpdatesCreated — streaming events are not persisted
// ---------------------------------------------------------------------------

func TestStreamText_NoUserUpdatesCreated(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	convID := "conv-stream-10"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"stream_id":       "stream-10",
		"text":            "hello",
		"is_done":         false,
	}

	// Record baseline: zero updates for both users before the call.
	ctx := context.Background()
	beforeAlice, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	beforeBob, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 100)
	require.NoError(t, err)

	data, err := callStreamText(t, h, "alice", params, "req-1")
	require.NoError(t, err)
	assert.Equal(t, "ok", parseStreamTextResponse(t, data)["status"])

	// After the call: no new UserUpdate records for either user.
	afterAlice, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	afterBob, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 100)
	require.NoError(t, err)

	assert.Len(t, afterAlice, len(beforeAlice),
		"stream_text must not create UserUpdate records for alice")
	assert.Len(t, afterBob, len(beforeBob),
		"stream_text must not create UserUpdate records for bob")
}

// ---------------------------------------------------------------------------
// Test 11: BroadcastError_Tolerated — broadcast failure does not affect response
// ---------------------------------------------------------------------------

func TestStreamText_BroadcastError_Tolerated(t *testing.T) {
	s := setupTestSQLite(t)
	fb := &failingBroadcaster{}
	h := NewStreamTextHandler(s, fb.broadcast, nil)

	convID := "conv-stream-11"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"stream_id":       "stream-11",
		"text":            "hello",
		"is_done":         false,
	}

	// Handler should still return success even though broadcast fails.
	data, err := callStreamText(t, h, "alice", params, "req-1")
	require.NoError(t, err, "handler should succeed even when broadcast fails (fire-and-forget)")

	resp := parseStreamTextResponse(t, data)
	assert.Equal(t, "ok", resp["status"],
		"response should be status=ok despite broadcast failure")
}

// ---------------------------------------------------------------------------
// Test 12: InvalidJSONParams — malformed JSON returns ValidationError
// ---------------------------------------------------------------------------

func TestStreamText_InvalidJSONParams(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)
	ctx := context.Background()

	client := server.NewTestClient("alice")
	req := &protocol.PackageDataRequest{
		ID:     "req-bad-json",
		Method: "stream_text",
		Params: json.RawMessage(`{invalid json}`),
	}

	_, err := h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	var he *protocol.HandlerError
	require.True(t, errors.As(err, &he))
	assert.Equal(t, protocol.ResponseCodeValidationError, he.Code)
}

// ---------------------------------------------------------------------------
// Test 13: EmptyText — empty text is valid (text="" does not error)
// ---------------------------------------------------------------------------

func TestStreamText_EmptyText(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)

	convID := "conv-stream-13"
	createTestConversation(t, s, convID, "alice", "bob")

	params := map[string]interface{}{
		"conversation_id": convID,
		"stream_id":       "stream-13",
		"text":            "",
		"is_done":         false,
	}

	data, err := callStreamText(t, h, "alice", params, "req-empty")
	require.NoError(t, err)

	resp := parseStreamTextResponse(t, data)
	assert.Equal(t, "ok", resp["status"])

	rb.mu.Lock()
	defer rb.mu.Unlock()
	require.Len(t, rb.calls, 2)

	var payload streamingBroadcastPayload
	require.NoError(t, json.Unmarshal(rb.calls[0].updates.Updates[0].Payload, &payload))
	assert.Equal(t, "", payload.Text, "empty text should be allowed")
}

// ---------------------------------------------------------------------------
// Test 14: ConcurrentCalls — concurrent calls do not race
// ---------------------------------------------------------------------------

func TestStreamText_ConcurrentCalls(t *testing.T) {
	s := setupTestSQLite(t)
	rb := &recordingBroadcaster{}
	h := NewStreamTextHandler(s, rb.broadcast, nil)
	ctx := context.Background()

	convID := "conv-stream-concurrent"
	createTestConversation(t, s, convID, "alice", "bob")

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			params := map[string]interface{}{
				"conversation_id": convID,
				"stream_id":       "stream-concurrent",
				"text":            "concurrent",
				"is_done":         false,
			}
			client := server.NewTestClient("alice")
			req := newTestRequest("req-concurrent", "stream_text", params)
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
