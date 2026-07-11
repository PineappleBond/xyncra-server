package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock BroadcastServer
// ---------------------------------------------------------------------------

type broadcastCall struct {
	userID  string
	updates *protocol.PackageDataUpdates
}

type mockBroadcastServer struct {
	calls []broadcastCall
	err   error // if set, BroadcastUpdates always returns this
}

func (m *mockBroadcastServer) BroadcastUpdates(userID string, updates *protocol.PackageDataUpdates) error {
	m.calls = append(m.calls, broadcastCall{userID: userID, updates: updates})
	return m.err
}

// ---------------------------------------------------------------------------
// SendStreamUpdate tests
// ---------------------------------------------------------------------------

func TestSendStreamUpdate_BroadcastsToBothUsers(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendStreamUpdate(context.Background(), "user/alice", "agent/bot1", "conv-1", "stream-1", "hello", false)

	require.Len(t, mock.calls, 2, "should broadcast to both human and agent user")
	assert.Equal(t, "user/alice", mock.calls[0].userID)
	assert.Equal(t, "agent/bot1", mock.calls[1].userID)
}

func TestSendStreamUpdate_SeqIsZero(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendStreamUpdate(context.Background(), "user/alice", "agent/bot1", "conv-1", "stream-1", "hello", false)

	for _, call := range mock.calls {
		require.Len(t, call.updates.Updates, 1)
		assert.Equal(t, uint32(0), call.updates.Updates[0].Seq, "ephemeral updates must use Seq=0 (D-050)")
	}
}

func TestSendStreamUpdate_TypeIsStreaming(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendStreamUpdate(context.Background(), "user/alice", "agent/bot1", "conv-1", "stream-1", "hello", false)

	for _, call := range mock.calls {
		assert.Equal(t, protocol.UpdateTypeStreaming, call.updates.Updates[0].Type)
	}
}

func TestSendStreamUpdate_PayloadContainsCorrectFields(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendStreamUpdate(context.Background(), "user/alice", "agent/bot1", "conv-123", "stream-456", "hello world", true)

	// Verify payload on one of the calls.
	require.Len(t, mock.calls, 2)
	raw := mock.calls[0].updates.Updates[0].Payload

	var payload StreamingPayload
	err := json.Unmarshal(raw, &payload)
	require.NoError(t, err)

	assert.Equal(t, "conv-123", payload.ConversationID)
	assert.Equal(t, "stream-456", payload.StreamID)
	assert.Equal(t, "hello world", payload.Text)
	assert.True(t, payload.IsDone)
}

func TestSendStreamUpdate_BroadcastError_NoPanic(t *testing.T) {
	mock := &mockBroadcastServer{err: fmt.Errorf("broadcast failed")}
	bh := NewBroadcastHelper(mock, noopLogger{})

	// Should not panic even when broadcast fails.
	assert.NotPanics(t, func() {
		bh.SendStreamUpdate(context.Background(), "user/alice", "agent/bot1", "conv-1", "stream-1", "hello", false)
	})

	// Both calls were still attempted.
	assert.Len(t, mock.calls, 2)
}

func TestSendStreamUpdate_EmptyUserIDs(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	// Call with empty user IDs — should still broadcast to both (empty string) slots.
	bh.SendStreamUpdate(context.Background(), "", "", "conv-1", "stream-1", "hello", false)

	require.Len(t, mock.calls, 2, "should still broadcast twice even with empty user IDs")
	assert.Equal(t, "", mock.calls[0].userID)
	assert.Equal(t, "", mock.calls[1].userID)
}

func TestSendStreamUpdate_IsDoneFalse(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendStreamUpdate(context.Background(), "user/alice", "agent/bot1", "conv-1", "stream-1", "partial text", false)

	for _, call := range mock.calls {
		var payload StreamingPayload
		err := json.Unmarshal(call.updates.Updates[0].Payload, &payload)
		require.NoError(t, err)
		assert.False(t, payload.IsDone)
	}
}

// ---------------------------------------------------------------------------
// SendTyping tests
// ---------------------------------------------------------------------------

func TestSendTyping_BroadcastsToTargetUser(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendTyping(context.Background(), "agent/bot1", "user/alice", "conv-1", true)

	require.Len(t, mock.calls, 1, "should broadcast to target user only")
	assert.Equal(t, "user/alice", mock.calls[0].userID)
}

func TestSendTyping_SeqIsZero(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendTyping(context.Background(), "agent/bot1", "user/alice", "conv-1", true)

	require.Len(t, mock.calls, 1)
	require.Len(t, mock.calls[0].updates.Updates, 1)
	assert.Equal(t, uint32(0), mock.calls[0].updates.Updates[0].Seq, "ephemeral typing must use Seq=0 (D-050)")
}

func TestSendTyping_TypeIsTyping(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendTyping(context.Background(), "agent/bot1", "user/alice", "conv-1", true)

	require.Len(t, mock.calls, 1)
	assert.Equal(t, protocol.UpdateTypeTyping, mock.calls[0].updates.Updates[0].Type)
}

func TestSendTyping_IsTypingPayload(t *testing.T) {
	tests := []struct {
		name     string
		isTyping bool
	}{
		{"typing true", true},
		{"typing false", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockBroadcastServer{}
			bh := NewBroadcastHelper(mock, noopLogger{})

			bh.SendTyping(context.Background(), "agent/bot1", "user/alice", "conv-42", tc.isTyping)

			require.Len(t, mock.calls, 1)
			raw := mock.calls[0].updates.Updates[0].Payload

			var payload TypingPayload
			err := json.Unmarshal(raw, &payload)
			require.NoError(t, err)

			assert.Equal(t, "conv-42", payload.ConversationID)
			assert.Equal(t, tc.isTyping, payload.IsTyping)
		})
	}
}

func TestSendTyping_BroadcastError_NoPanic(t *testing.T) {
	mock := &mockBroadcastServer{err: fmt.Errorf("broadcast failed")}
	bh := NewBroadcastHelper(mock, noopLogger{})

	assert.NotPanics(t, func() {
		bh.SendTyping(context.Background(), "agent/bot1", "user/alice", "conv-1", true)
	})

	assert.Len(t, mock.calls, 1)
}

// ---------------------------------------------------------------------------
// NewBroadcastHelper
// ---------------------------------------------------------------------------

func TestNewBroadcastHelper(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	assert.NotNil(t, bh)
	assert.NotNil(t, bh.logger)
	assert.Equal(t, mock, bh.wsServer)
}

// ---------------------------------------------------------------------------
// Payload format verification (Phase 6: client-agent integration)
// ---------------------------------------------------------------------------

func TestSendStreamUpdate_PayloadIncludesUserIDAndTimestamp(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendStreamUpdate(context.Background(), "user/alice", "agent/bot1", "conv-1", "stream-1", "hello", false)

	require.Len(t, mock.calls, 2)
	raw := mock.calls[0].updates.Updates[0].Payload

	var payload StreamingPayload
	err := json.Unmarshal(raw, &payload)
	require.NoError(t, err)

	// UserID should be the agent (the entity doing the streaming).
	assert.Equal(t, "agent/bot1", payload.UserID, "streaming payload user_id should be the agent")
	assert.NotZero(t, payload.Timestamp, "timestamp should be non-zero")
	assert.Greater(t, payload.Timestamp, int64(0), "timestamp should be positive")
}

func TestSendTyping_PayloadIncludesUserIDAndTimestamp(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendTyping(context.Background(), "agent/bot1", "user/alice", "conv-1", true)

	require.Len(t, mock.calls, 1)
	raw := mock.calls[0].updates.Updates[0].Payload

	var payload TypingPayload
	err := json.Unmarshal(raw, &payload)
	require.NoError(t, err)

	// UserID should be the agent (the entity doing the typing/thinking).
	assert.Equal(t, "agent/bot1", payload.UserID, "typing payload user_id should be the agent")
	assert.NotZero(t, payload.Timestamp, "timestamp should be non-zero")
}

func TestSendStreamUpdate_JSONFieldNames(t *testing.T) {
	// Verify JSON field names match client expectations (pkg/client/sync.go streamingUpdatePayload).
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendStreamUpdate(context.Background(), "user/alice", "agent/bot1", "conv-1", "stream-1", "text", true)

	raw := mock.calls[0].updates.Updates[0].Payload
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	// Verify expected JSON keys exist.
	assert.Contains(t, m, "user_id")
	assert.Contains(t, m, "conversation_id")
	assert.Contains(t, m, "stream_id")
	assert.Contains(t, m, "text") // not "content"
	assert.Contains(t, m, "is_done")
	assert.Contains(t, m, "timestamp")
	// "content" should NOT be a key (renamed to "text").
	assert.NotContains(t, m, "content")
}

func TestSendTyping_JSONFieldNames(t *testing.T) {
	// Verify JSON field names match client expectations (pkg/client/sync.go typingUpdatePayload).
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	bh.SendTyping(context.Background(), "agent/bot1", "user/alice", "conv-1", true)

	raw := mock.calls[0].updates.Updates[0].Payload
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	assert.Contains(t, m, "user_id")
	assert.Contains(t, m, "conversation_id")
	assert.Contains(t, m, "is_typing")
	assert.Contains(t, m, "timestamp")
}

// ---------------------------------------------------------------------------
// Unicode and special-character round-trip (Phase 6: client-agent integration)
// ---------------------------------------------------------------------------

// TestSendStreamUpdate_UnicodeAndSpecialChars verifies that streaming payloads
// containing Unicode characters, emojis, quotes, backslashes, and control
// characters survive JSON round-tripping without corruption.
func TestSendStreamUpdate_UnicodeAndSpecialChars(t *testing.T) {
	mock := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mock, noopLogger{})

	text := "Hello 世界 🌍 \"quoted\" \\backslash \n\tnewline"
	bh.SendStreamUpdate(context.Background(), "user/a", "agent/b", "conv", "s", text, false)

	require.Len(t, mock.calls, 2)
	var payload StreamingPayload
	require.NoError(t, json.Unmarshal(mock.calls[0].updates.Updates[0].Payload, &payload))
	assert.Equal(t, text, payload.Text)
}
