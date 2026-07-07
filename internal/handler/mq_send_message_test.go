package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// fakeLogger is a minimal Logger implementation that records calls for tests.
type fakeLogger struct {
	errors []string
	debugs []string
}

func (l *fakeLogger) Info(msg string, args ...any)  {}
func (l *fakeLogger) Error(msg string, args ...any) { l.errors = append(l.errors, msg) }
func (l *fakeLogger) Debug(msg string, args ...any) { l.debugs = append(l.debugs, msg) }

// makeTask builds an mq.Task with an mqSendMessageTaskPayload JSON-encoded.
func makeTask(t *testing.T, payload mqSendMessageTaskPayload) *mq.Task {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return &mq.Task{Type: mq.TypeSendMessage, Payload: data}
}

// ---------------------------------------------------------------------------
// TestSendMessageTaskHandler_HappyPath verifies the happy path: each
// recipient in the payload results in a broadcastFn call.
// ---------------------------------------------------------------------------

func TestSendMessageTaskHandler_HappyPath(t *testing.T) {
	var calls []struct {
		userID  string
		updates *protocol.PackageDataUpdates
	}
	broadcastFn := func(userID string, updates *protocol.PackageDataUpdates) error {
		calls = append(calls, struct {
			userID  string
			updates *protocol.PackageDataUpdates
		}{userID, updates})
		return nil
	}

	payload := mqSendMessageTaskPayload{
		Recipients: []mqSendMessageRecipient{
			{
				UserID: "alice",
				Updates: []protocol.PackageDataUpdate{
					{Seq: 1, Payload: json.RawMessage(`{"msg":"hi"}`), CreatedAt: time.Now()},
				},
			},
			{
				UserID: "bob",
				Updates: []protocol.PackageDataUpdate{
					{Seq: 2, Payload: json.RawMessage(`{"msg":"hi"}`), CreatedAt: time.Now()},
					{Seq: 3, Payload: json.RawMessage(`{"msg":"there"}`), CreatedAt: time.Now()},
				},
			},
		},
	}

	handler := NewSendMessageTaskHandler(broadcastFn, nil)
	err := handler(context.Background(), makeTask(t, payload))
	require.NoError(t, err)

	require.Len(t, calls, 2)
	assert.Equal(t, "alice", calls[0].userID)
	assert.Len(t, calls[0].updates.Updates, 1)
	assert.Equal(t, "bob", calls[1].userID)
	assert.Len(t, calls[1].updates.Updates, 2)
}

// ---------------------------------------------------------------------------
// TestSendMessageTaskHandler_InvalidPayload verifies that malformed JSON does
// not panic and the handler returns nil (no retry, D-007).
// ---------------------------------------------------------------------------

func TestSendMessageTaskHandler_InvalidPayload(t *testing.T) {
	broadcastFn := func(userID string, updates *protocol.PackageDataUpdates) error {
		t.Fatal("broadcastFn should not be called on invalid payload")
		return nil
	}

	handler := NewSendMessageTaskHandler(broadcastFn, nil)
	task := &mq.Task{Type: mq.TypeSendMessage, Payload: json.RawMessage(`{not valid json`)}
	err := handler(context.Background(), task)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestSendMessageTaskHandler_BroadcastError verifies that a broadcastFn
// failure is logged but does not abort processing of remaining recipients
// and does not cause task retry (D-007).
// ---------------------------------------------------------------------------

func TestSendMessageTaskHandler_BroadcastError(t *testing.T) {
	var calls []string
	broadcastFn := func(userID string, updates *protocol.PackageDataUpdates) error {
		calls = append(calls, userID)
		if userID == "alice" {
			return errors.New("connection closed")
		}
		return nil
	}

	logger := &fakeLogger{}
	payload := mqSendMessageTaskPayload{
		Recipients: []mqSendMessageRecipient{
			{UserID: "alice", Updates: []protocol.PackageDataUpdate{{Seq: 1}}},
			{UserID: "bob", Updates: []protocol.PackageDataUpdate{{Seq: 2}}},
			{UserID: "carol", Updates: []protocol.PackageDataUpdate{{Seq: 3}}},
		},
	}

	handler := NewSendMessageTaskHandler(broadcastFn, logger)
	err := handler(context.Background(), makeTask(t, payload))
	require.NoError(t, err)

	// All recipients must be attempted.
	assert.Equal(t, []string{"alice", "bob", "carol"}, calls)
	// Error from alice's broadcast should have been logged.
	assert.NotEmpty(t, logger.errors)
}

// ---------------------------------------------------------------------------
// TestSendMessageTaskHandler_NoRecipients verifies that an empty recipients
// list is handled gracefully.
// ---------------------------------------------------------------------------

func TestSendMessageTaskHandler_NoRecipients(t *testing.T) {
	broadcastFn := func(userID string, updates *protocol.PackageDataUpdates) error {
		t.Fatal("broadcastFn should not be called with no recipients")
		return nil
	}

	handler := NewSendMessageTaskHandler(broadcastFn, nil)
	payload := mqSendMessageTaskPayload{Recipients: []mqSendMessageRecipient{}}
	err := handler(context.Background(), makeTask(t, payload))
	assert.NoError(t, err)
}
