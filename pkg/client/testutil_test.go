package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// newTestStore creates an in-memory SQLite database for testing.
// The returned store is closed automatically when the test completes.
func newTestStore(t *testing.T) *store.ClientDB {
	t.Helper()
	name := fmt.Sprintf("client_test_%d", time.Now().UnixNano())
	db, err := store.NewInMemory(name)
	if err != nil {
		t.Fatalf("newTestStore: failed to create in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestClient creates a XyncraClient connected to the given mock server.
// Additional options can override or supplement the defaults.
// The client is stopped automatically when the test completes.
func newTestClient(t *testing.T, server *mockWSServer, opts ...ClientOption) *XyncraClient {
	t.Helper()
	db := newTestStore(t)
	allOpts := []ClientOption{
		WithServerURL(server.URL()),
		WithUserID("test-user"),
		WithDB(db),
		WithLogger(&testLogger{t: t}),
		WithReconnectMaxRetries(1), // avoid infinite loops in tests
		WithReconnectBaseDelay(10 * time.Millisecond),
		WithReconnectMaxDelay(50 * time.Millisecond),
		WithHeartbeatInterval(1 * time.Hour), // effectively disable heartbeat in tests
		WithPullDebounce(10 * time.Millisecond),
	}
	allOpts = append(allOpts, opts...)
	c, err := New(allOpts...)
	if err != nil {
		t.Fatalf("newTestClient: failed to create client: %v", err)
	}
	t.Cleanup(func() { c.Stop() })
	return c
}

// newTestUpdate creates a PackageDataUpdate with the given seq, type and payload.
func newTestUpdate(seq uint32, typ string, payload json.RawMessage) protocol.PackageDataUpdate {
	return protocol.PackageDataUpdate{
		Seq:     seq,
		Type:    typ,
		Payload: payload,
	}
}

// testLogger wraps testing.T as a Logger so that all client log output is
// visible in test output.
type testLogger struct {
	t *testing.T
}

// Info logs an informational message.
func (l *testLogger) Info(msg string, args ...any) { l.t.Logf("[INFO] "+msg, args...) }

// Error logs an error message.
func (l *testLogger) Error(msg string, args ...any) { l.t.Logf("[ERROR] "+msg, args...) }

// Debug logs a debug-level message.
func (l *testLogger) Debug(msg string, args ...any) { l.t.Logf("[DEBUG] "+msg, args...) }

// mockUpdateHandler is a mock UpdateHandler that records all invocations for
// later inspection in tests.
type mockUpdateHandler struct {
	mu            sync.Mutex
	messages      []*model.Message
	deletes       []deleteRecord
	markReads     []markReadRecord
	conversations []*model.Conversation
	gaps          []uint32
}

// deleteRecord holds the arguments passed to OnDeleteMessage.
type deleteRecord struct {
	MessageID      string
	ConversationID string
}

// markReadRecord holds the arguments passed to OnMarkRead.
type markReadRecord struct {
	ConversationID string
	MessageID      uint32
}

// OnMessage records the message.
func (h *mockUpdateHandler) OnMessage(ctx context.Context, msg *model.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msg)
	return nil
}

// OnDeleteMessage records the delete parameters.
func (h *mockUpdateHandler) OnDeleteMessage(ctx context.Context, messageID string, conversationID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.deletes = append(h.deletes, deleteRecord{MessageID: messageID, ConversationID: conversationID})
	return nil
}

// OnMarkRead records the mark-read parameters.
func (h *mockUpdateHandler) OnMarkRead(ctx context.Context, conversationID string, messageID uint32) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.markReads = append(h.markReads, markReadRecord{ConversationID: conversationID, MessageID: messageID})
	return nil
}

// OnConversation records the conversation.
func (h *mockUpdateHandler) OnConversation(ctx context.Context, conv *model.Conversation) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conversations = append(h.conversations, conv)
	return nil
}

// OnGap records the gap sequence.
func (h *mockUpdateHandler) OnGap(ctx context.Context, seq uint32) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gaps = append(h.gaps, seq)
	return nil
}

// assertSyncState asserts that the sync state key holds the expected uint32
// value. If the key does not exist, expected must be 0.
func assertSyncState(t *testing.T, db *store.ClientDB, key string, expected uint32) {
	t.Helper()
	ctx := context.Background()
	var got uint32
	var err error
	switch key {
	case "local_max_seq":
		got, err = db.SyncStates.GetLocalMaxSeq(ctx)
	case "latest_seq":
		got, err = db.SyncStates.GetLatestSeq(ctx)
	default:
		t.Fatalf("assertSyncState: unknown key %q", key)
	}
	if err != nil {
		t.Fatalf("assertSyncState: get %q: %v", key, err)
	}
	if got != expected {
		t.Errorf("assertSyncState: key=%q: got=%d want=%d", key, got, expected)
	}
}
