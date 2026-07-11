package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/mq"
)

// ---------------------------------------------------------------------------
// Mock IdempotencyStore
// ---------------------------------------------------------------------------

// mockIdempotencyStore implements IdempotencyStore for testing.
type mockIdempotencyStore struct {
	markProcessedFn func(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

func (m *mockIdempotencyStore) MarkProcessed(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if m.markProcessedFn != nil {
		return m.markProcessedFn(ctx, key, ttl)
	}
	return false, nil
}

// newTestHandler creates an AgentExecutor (with mocks) and a task handler for testing.
// The executor uses a mockStoreAPI and mockBroadcastServer so we can observe calls.
func newTestHandler(
	idempotency IdempotencyStore,
) (
	handler func(ctx context.Context, task *mq.Task) error,
	mockStore *mockStoreAPI,
	mockBS *mockBroadcastServer,
) {
	registry := NewRegistry()
	registry.Register(&AgentConfig{
		ID:        "test-agent",
		Name:      "Test",
		Model:     "gpt-4",
		APIKeyEnv: "",
	})

	mockStore = &mockStoreAPI{}
	mockBS = &mockBroadcastServer{}
	// Use a context manager that returns an error so the executor fails at
	// context loading (before reaching the nil agentBuilder). This lets us
	// verify executor invocation via typing broadcasts and error message
	// persistence without needing a full LLM mock stack.
	mockCtxMgr := &mockContextManager{err: ErrContextLoad}
	bh := NewBroadcastHelper(mockBS)
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0)
	logger := log.New(io.Discard, "", 0)

	handler = NewAgentTaskHandler(executor, idempotency, logger)
	return handler, mockStore, mockBS
}

// ---------------------------------------------------------------------------
// 1. Nil task → return nil, no panic
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_NilTask(t *testing.T) {
	handler, _, _ := newTestHandler(nil)
	assert.NotPanics(t, func() {
		err := handler(context.Background(), nil)
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// 2. Invalid payload → return nil
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_InvalidPayload(t *testing.T) {
	handler, _, _ := newTestHandler(nil)
	task := &mq.Task{
		Type:    mq.TypeAgentProcess,
		Payload: json.RawMessage(`{invalid json`),
	}
	err := handler(context.Background(), task)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// 3. Missing required fields → return nil
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		payload AgentProcessPayload
	}{
		{"missing MessageID", AgentProcessPayload{ConversationID: "c", AgentID: "a"}},
		{"missing ConversationID", AgentProcessPayload{MessageID: "m", AgentID: "a"}},
		{"missing AgentID", AgentProcessPayload{MessageID: "m", ConversationID: "c"}},
		{"all empty", AgentProcessPayload{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler, mockStore, _ := newTestHandler(nil)
			payloadBytes, _ := json.Marshal(tc.payload)
			task := &mq.Task{Type: mq.TypeAgentProcess, Payload: payloadBytes}

			err := handler(context.Background(), task)
			assert.NoError(t, err)
			// Executor should NOT have been called → no SendMessage calls.
			assert.Empty(t, mockStore.sendMessageCalls)
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Idempotency duplicate → executor NOT called
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_IdempotencyDuplicate(t *testing.T) {
	idem := &mockIdempotencyStore{
		markProcessedFn: func(_ context.Context, _ string, _ time.Duration) (bool, error) {
			return true, nil // duplicate
		},
	}
	handler, mockStore, mockBS := newTestHandler(idem)

	payload := AgentProcessPayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test-agent",
		SenderID:       "user/alice",
	}
	payloadBytes, _ := json.Marshal(payload)
	task := &mq.Task{Type: mq.TypeAgentProcess, Payload: payloadBytes}

	err := handler(context.Background(), task)
	assert.NoError(t, err)

	// Executor should NOT have been called.
	assert.Empty(t, mockStore.sendMessageCalls, "executor should not be called on duplicate")
	assert.Empty(t, mockBS.calls, "no broadcasts should occur on duplicate")
}

// ---------------------------------------------------------------------------
// 5. Idempotency first time → executor SHOULD be called
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_IdempotencyFirstTime(t *testing.T) {
	var capturedKey string
	idem := &mockIdempotencyStore{
		markProcessedFn: func(_ context.Context, key string, _ time.Duration) (bool, error) {
			capturedKey = key
			return false, nil // first time
		},
	}
	handler, _, mockBS := newTestHandler(idem)

	payload := AgentProcessPayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test-agent",
		SenderID:       "user/alice",
	}
	payloadBytes, _ := json.Marshal(payload)
	task := &mq.Task{Type: mq.TypeAgentProcess, Payload: payloadBytes}

	err := handler(context.Background(), task)
	assert.NoError(t, err)

	// Idempotency key should contain the message ID.
	assert.Equal(t, "agent:processed:msg-1", capturedKey)

	// Executor SHOULD have been called → typing broadcast sent.
	assert.NotEmpty(t, mockBS.calls, "executor should have been called")
}

// ---------------------------------------------------------------------------
// 6. Idempotency error → fail-open, executor SHOULD still be called
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_IdempotencyError_FailOpen(t *testing.T) {
	idem := &mockIdempotencyStore{
		markProcessedFn: func(_ context.Context, _ string, _ time.Duration) (bool, error) {
			return false, fmt.Errorf("redis connection refused")
		},
	}
	handler, _, mockBS := newTestHandler(idem)

	payload := AgentProcessPayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test-agent",
		SenderID:       "user/alice",
	}
	payloadBytes, _ := json.Marshal(payload)
	task := &mq.Task{Type: mq.TypeAgentProcess, Payload: payloadBytes}

	err := handler(context.Background(), task)
	assert.NoError(t, err)

	// Executor SHOULD have been called despite idempotency error.
	assert.NotEmpty(t, mockBS.calls, "executor should be called when idempotency fails (fail-open)")
}

// ---------------------------------------------------------------------------
// 7. Nil IdempotencyStore → skip check, executor called
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_NilIdempotency(t *testing.T) {
	handler, _, mockBS := newTestHandler(nil)

	payload := AgentProcessPayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test-agent",
		SenderID:       "user/alice",
	}
	payloadBytes, _ := json.Marshal(payload)
	task := &mq.Task{Type: mq.TypeAgentProcess, Payload: payloadBytes}

	err := handler(context.Background(), task)
	assert.NoError(t, err)

	// Executor SHOULD have been called (no idempotency check).
	assert.NotEmpty(t, mockBS.calls, "executor should be called when idempotency is nil")
}

// ---------------------------------------------------------------------------
// 8. Executor success → handler returns nil
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_ExecutorSuccess(t *testing.T) {
	// The mockContextManager returns ErrContextLoad, so the executor fails
	// at context loading. The handler still returns nil (its contract).
	handler, _, _ := newTestHandler(nil)

	payload := AgentProcessPayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/nonexistent-agent",
		SenderID:       "user/alice",
	}
	payloadBytes, _ := json.Marshal(payload)
	task := &mq.Task{Type: mq.TypeAgentProcess, Payload: payloadBytes}

	err := handler(context.Background(), task)
	assert.NoError(t, err, "handler always returns nil regardless of executor outcome")
}

// ---------------------------------------------------------------------------
// 9. Executor error → handler still returns nil
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_ExecutorError(t *testing.T) {
	handler, mockStore, _ := newTestHandler(nil)

	// The mockContextManager returns ErrContextLoad, triggering
	// ExecuteWithErrorMessage which persists the error message (D-067).
	payload := AgentProcessPayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/nonexistent-agent",
		SenderID:       "user/alice",
	}
	payloadBytes, _ := json.Marshal(payload)
	task := &mq.Task{Type: mq.TypeAgentProcess, Payload: payloadBytes}

	err := handler(context.Background(), task)
	assert.NoError(t, err, "handler must return nil even when executor fails")

	// Error message should have been persisted via ExecuteWithErrorMessage (D-067).
	require.Len(t, mockStore.sendMessageCalls, 1, "error message should be persisted")
	assert.Equal(t, "conv-1", mockStore.sendMessageCalls[0].msg.ConversationID)
	assert.Equal(t, "agent/nonexistent-agent", mockStore.sendMessageCalls[0].msg.SenderID)
}

// ---------------------------------------------------------------------------
// 10. Correct payload mapping: AgentProcessPayload → ExecutePayload
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_CorrectPayloadMapping(t *testing.T) {
	// The executor proceeds past registry lookup (test-agent is registered),
	// then fails at context loading (mockContextManager returns ErrContextLoad).
	// ExecuteWithErrorMessage persists the error message, confirming the
	// payload mapping was correct.
	handler, mockStore, _ := newTestHandler(nil)

	payload := AgentProcessPayload{
		MessageID:      "msg-unique-123",
		ConversationID: "conv-456",
		AgentID:        "agent/test-agent",
		SenderID:       "user/bob",
	}
	payloadBytes, _ := json.Marshal(payload)
	task := &mq.Task{Type: mq.TypeAgentProcess, Payload: payloadBytes}

	err := handler(context.Background(), task)
	assert.NoError(t, err)

	// The executor should have been invoked. The mockContextManager returns
	// ErrContextLoad, triggering ExecuteWithErrorMessage which persists the
	// error message. The persisted error message uses the AgentID from the
	// payload, confirming the payload mapping was correct.
	require.GreaterOrEqual(t, len(mockStore.sendMessageCalls), 1)
	assert.Equal(t, "conv-456", mockStore.sendMessageCalls[0].msg.ConversationID)
	assert.Equal(t, "agent/test-agent", mockStore.sendMessageCalls[0].msg.SenderID)
	assert.Contains(t, mockStore.sendMessageCalls[0].memberIDs, "user/bob")
}

// ---------------------------------------------------------------------------
// 11. RedisIdempotencyStore: first call returns false (not duplicate)
// ---------------------------------------------------------------------------

func TestRedisIdempotencyStore_MarkProcessed_FirstTime(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	store := NewRedisIdempotencyStore(client)

	dup, err := store.MarkProcessed(context.Background(), "test:key1", time.Hour)
	require.NoError(t, err)
	assert.False(t, dup, "first call should not be a duplicate")
}

// ---------------------------------------------------------------------------
// 12. RedisIdempotencyStore: second call returns true (duplicate)
// ---------------------------------------------------------------------------

func TestRedisIdempotencyStore_MarkProcessed_Duplicate(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	store := NewRedisIdempotencyStore(client)

	_, err = store.MarkProcessed(context.Background(), "test:key2", time.Hour)
	require.NoError(t, err)

	dup, err := store.MarkProcessed(context.Background(), "test:key2", time.Hour)
	require.NoError(t, err)
	assert.True(t, dup, "second call with same key should be a duplicate")
}

// ---------------------------------------------------------------------------
// 13. RedisIdempotencyStore: after TTL expiry, returns false again
// ---------------------------------------------------------------------------

func TestRedisIdempotencyStore_MarkProcessed_TTLExpiry(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	store := NewRedisIdempotencyStore(client)

	_, err = store.MarkProcessed(context.Background(), "test:key3", 5*time.Second)
	require.NoError(t, err)

	// Fast-forward miniredis time past the TTL.
	mr.FastForward(10 * time.Second)

	dup, err := store.MarkProcessed(context.Background(), "test:key3", 5*time.Second)
	require.NoError(t, err)
	assert.False(t, dup, "after TTL expiry, key should no longer be a duplicate")
}

// ---------------------------------------------------------------------------
// RedisIdempotencyStore: Redis error propagated
// ---------------------------------------------------------------------------

func TestRedisIdempotencyStore_MarkProcessed_RedisError(t *testing.T) {
	// Use a closed client to trigger a connection error.
	client := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	defer client.Close()

	store := NewRedisIdempotencyStore(client)

	_, err := store.MarkProcessed(context.Background(), "test:key4", time.Hour)
	assert.Error(t, err, "should return error when Redis is unreachable")
}

// ---------------------------------------------------------------------------
// 14. Empty payload → return nil, no panic
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_EmptyPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload json.RawMessage
	}{
		{"empty string", json.RawMessage("")},
		{"nil", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler, mockStore, _ := newTestHandler(nil)
			task := &mq.Task{Type: mq.TypeAgentProcess, Payload: tc.payload}

			assert.NotPanics(t, func() {
				err := handler(context.Background(), task)
				assert.NoError(t, err, "handler returns nil for empty payload")
			})
			// Executor should NOT have been called.
			assert.Empty(t, mockStore.sendMessageCalls)
		})
	}
}

// ---------------------------------------------------------------------------
// 15. Null JSON payload → unmarshals to zero-value struct, validation fails
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_NullJSONPayload(t *testing.T) {
	handler, mockStore, _ := newTestHandler(nil)
	task := &mq.Task{
		Type:    mq.TypeAgentProcess,
		Payload: json.RawMessage("null"),
	}

	err := handler(context.Background(), task)
	assert.NoError(t, err, "handler returns nil for null JSON payload")
	// null unmarshals to zero-value AgentProcessPayload (all fields empty),
	// which fails validation → executor NOT called.
	assert.Empty(t, mockStore.sendMessageCalls)
}

// ---------------------------------------------------------------------------
// 16. Idempotency TTL is exactly 24 hours
// ---------------------------------------------------------------------------

func TestNewAgentTaskHandler_IdempotencyTTLValue(t *testing.T) {
	var capturedTTL time.Duration
	idem := &mockIdempotencyStore{
		markProcessedFn: func(_ context.Context, _ string, ttl time.Duration) (bool, error) {
			capturedTTL = ttl
			return false, nil // first time, not duplicate
		},
	}
	handler, _, _ := newTestHandler(idem)

	payload := AgentProcessPayload{
		MessageID:      "msg-ttl",
		ConversationID: "conv-ttl",
		AgentID:        "agent/test-agent",
		SenderID:       "user/alice",
	}
	payloadBytes, _ := json.Marshal(payload)
	task := &mq.Task{Type: mq.TypeAgentProcess, Payload: payloadBytes}

	err := handler(context.Background(), task)
	assert.NoError(t, err)
	assert.Equal(t, 24*time.Hour, capturedTTL, "idempotency TTL must be 24 hours")
}
