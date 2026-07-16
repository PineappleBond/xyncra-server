package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// New() — constructor validation
// ---------------------------------------------------------------------------

// TestNew_MissingServerURL verifies that New returns an error when serverURL
// is explicitly set to empty.
func TestNew_MissingServerURL(t *testing.T) {
	db := newTestStore(t)
	_, err := New(
		WithServerURL(""),
		WithUserID("u1"),
		WithDB(db),
	)
	if err == nil {
		t.Fatal("expected error for missing serverURL, got nil")
	}
	if !strings.Contains(err.Error(), "serverURL") {
		t.Fatalf("error should mention serverURL, got: %v", err)
	}
}

// TestNew_MissingUserID verifies that New returns an error when userID is not
// provided.
func TestNew_MissingUserID(t *testing.T) {
	db := newTestStore(t)
	_, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithDB(db),
	)
	if err == nil {
		t.Fatal("expected error for missing userID, got nil")
	}
	if !strings.Contains(err.Error(), "userID") {
		t.Fatalf("error should mention userID, got: %v", err)
	}
}

// TestNew_MissingDB verifies that New returns an error when db is not provided.
func TestNew_MissingDB(t *testing.T) {
	_, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("u1"),
	)
	if err == nil {
		t.Fatal("expected error for missing db, got nil")
	}
	if !strings.Contains(err.Error(), "db") {
		t.Fatalf("error should mention db, got: %v", err)
	}
}

// TestNew_Success verifies that New returns a non-nil client when all required
// options are supplied.
func TestNew_Success(t *testing.T) {
	db := newTestStore(t)
	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("u1"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	// Verify default logger was assigned (no panic on logger access).
	if c.logger == nil {
		t.Fatal("expected non-nil logger (default)")
	}
}

// ---------------------------------------------------------------------------
// Start / Stop lifecycle
// ---------------------------------------------------------------------------

// startAndStopClient is a helper that starts the client in a goroutine,
// waits for the connection to be established, and then stops it.
func startAndStopClient(t *testing.T, c *XyncraClient, server *mockWSServer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Start(ctx)
	}()

	// Wait for the connection to be established.
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	// Give goroutines a moment to start.
	time.Sleep(200 * time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
}

// TestStartStop_Lifecycle verifies that a client can be started, connected,
// and stopped cleanly.
func TestStartStop_Lifecycle(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	c := newTestClient(t, server)
	startAndStopClient(t, c, server)
}

// TestStop_Idempotent verifies that calling Stop multiple times does not panic.
func TestStop_Idempotent(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Cancel and stop — multiple Stop calls must not panic.
	cancel()
	time.Sleep(100 * time.Millisecond)
	c.Stop()
	c.Stop()
	c.Close()
}

// ---------------------------------------------------------------------------
// Local DB query methods (D-035)
// ---------------------------------------------------------------------------

// TestListConversations_LocalDB verifies that ListConversations reads from the
// local database and returns the correct conversations.
func TestListConversations_LocalDB(t *testing.T) {
	db := newTestStore(t)

	// Seed conversations directly into the local DB.
	conv1 := &model.Conversation{
		ID:            "conv-1",
		UserID1:       "test-user",
		UserID2:       "peer1",
		Title:         "Chat 1",
		LastMessageAt: time.Now().Add(-1 * time.Hour),
	}
	conv2 := &model.Conversation{
		ID:            "conv-2",
		UserID1:       "test-user",
		UserID2:       "peer2",
		Title:         "Chat 2",
		LastMessageAt: time.Now(),
	}
	if err := db.Conversations.Create(context.Background(), conv1); err != nil {
		t.Fatalf("seed conv1: %v", err)
	}
	if err := db.Conversations.Create(context.Background(), conv2); err != nil {
		t.Fatalf("seed conv2: %v", err)
	}

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	result, err := c.ListConversations(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("ListConversations() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Conversations) != 2 {
		t.Errorf("expected 2 conversations, got %d", len(result.Conversations))
	}
	// Verify ordering by LastMessageAt DESC.
	if result.Conversations[0].ID != "conv-2" {
		t.Errorf("expected first conversation to be conv-2 (most recent), got %s", result.Conversations[0].ID)
	}
}

// TestListConversations_Empty verifies that ListConversations returns an empty
// list when the local database has no conversations.
func TestListConversations_Empty(t *testing.T) {
	db := newTestStore(t)

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	result, err := c.ListConversations(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("ListConversations() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Conversations) != 0 {
		t.Errorf("expected 0 conversations, got %d", len(result.Conversations))
	}
	if result.HasMore {
		t.Error("expected HasMore=false for empty list")
	}
}

// TestListConversations_HasMore verifies that ListConversations correctly
// detects when there are more conversations available (limit+1 probe).
func TestListConversations_HasMore(t *testing.T) {
	db := newTestStore(t)

	// Seed 3 conversations.
	for i := 0; i < 3; i++ {
		conv := &model.Conversation{
			ID:            fmt.Sprintf("conv-%d", i),
			UserID1:       "test-user",
			UserID2:       fmt.Sprintf("peer%d", i),
			LastMessageAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := db.Conversations.Create(context.Background(), conv); err != nil {
			t.Fatalf("seed conv: %v", err)
		}
	}

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	// Query with limit=2, should return 2 and HasMore=true.
	result, err := c.ListConversations(context.Background(), 0, 2)
	if err != nil {
		t.Fatalf("ListConversations() error: %v", err)
	}
	if len(result.Conversations) != 2 {
		t.Errorf("expected 2 conversations, got %d", len(result.Conversations))
	}
	if !result.HasMore {
		t.Error("expected HasMore=true")
	}
}

// TestGetConversation_LocalDB verifies that GetConversation reads from the local
// database and returns the conversation with unread count and questions.
func TestGetConversation_LocalDB(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()

	// Seed a conversation.
	conv := &model.Conversation{
		ID:                 "conv-get",
		UserID1:            "test-user",
		UserID2:            "peer1",
		Type:               "1-on-1",
		Title:              "Test Chat",
		LastReadMessageID1: 10,
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conv: %v", err)
	}

	// Seed messages: 5 unread (MessageID 11..15).
	for i := uint32(11); i <= 15; i++ {
		msg := &model.Message{
			ID:              fmt.Sprintf("msg-%d", i),
			ClientMessageID: fmt.Sprintf("cid-%d", i),
			ConversationID:  "conv-get",
			MessageID:       i,
			SenderID:        "peer1",
			Content:         "hello",
			CreatedAt:       time.Now(),
		}
		if err := db.Messages.Create(ctx, msg); err != nil {
			t.Fatalf("seed msg: %v", err)
		}
	}

	// Seed a HITL question.
	q := &model.Question{
		ID:             "q-1",
		ConversationID: "conv-get",
		CheckpointID:   "ckpt-1",
		InterruptID:    "int-1",
		QuestionText:   "Are you sure?",
		Status:         "pending",
		CreatedAt:      time.Now(),
	}
	if err := db.Questions.Upsert(ctx, q); err != nil {
		t.Fatalf("seed question: %v", err)
	}

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	result, err := c.GetConversation(ctx, "conv-get")
	if err != nil {
		t.Fatalf("GetConversation() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Conversation.ID != "conv-get" {
		t.Errorf("expected ID=conv-get, got %s", result.Conversation.ID)
	}
	if result.UnreadCount != 5 {
		t.Errorf("expected UnreadCount=5, got %d", result.UnreadCount)
	}
	if len(result.Questions) != 1 {
		t.Errorf("expected 1 question, got %d", len(result.Questions))
	}
	if result.Questions[0].QuestionText != "Are you sure?" {
		t.Errorf("expected question text 'Are you sure?', got %s", result.Questions[0].QuestionText)
	}
}

// TestGetConversation_NotFound verifies that GetConversation returns an error
// when the conversation is not found in the local database.
func TestGetConversation_NotFound(t *testing.T) {
	db := newTestStore(t)

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	_, err = c.GetConversation(context.Background(), "nonexistent-conv")
	if err == nil {
		t.Fatal("expected error for nonexistent conversation, got nil")
	}
}

// TestGetMessages_LocalDB verifies that GetMessages reads from the local
// database and returns messages in the correct order.
func TestGetMessages_LocalDB(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()

	// Seed a conversation.
	conv := &model.Conversation{
		ID:      "conv-msgs",
		UserID1: "test-user",
		UserID2: "peer1",
		Title:   "Test",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conv: %v", err)
	}

	// Seed messages with MessageID 1..5.
	for i := uint32(1); i <= 5; i++ {
		msg := &model.Message{
			ID:              fmt.Sprintf("msg-%d", i),
			ClientMessageID: fmt.Sprintf("cid-%d", i),
			ConversationID:  "conv-msgs",
			MessageID:       i,
			SenderID:        "peer1",
			Content:         fmt.Sprintf("Message %d", i),
			CreatedAt:       time.Now(),
		}
		if err := db.Messages.Create(ctx, msg); err != nil {
			t.Fatalf("seed msg: %v", err)
		}
	}

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	result, err := c.GetMessages(ctx, "conv-msgs", 0, 10)
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(result.Messages) != 5 {
		t.Errorf("expected 5 messages, got %d", len(result.Messages))
	}
	// Verify ordering by MessageID ASC.
	if result.Messages[0].MessageID != 1 {
		t.Errorf("expected first message MessageID=1, got %d", result.Messages[0].MessageID)
	}
	if result.Messages[4].MessageID != 5 {
		t.Errorf("expected last message MessageID=5, got %d", result.Messages[4].MessageID)
	}
}

// TestGetMessages_AfterMsgID verifies that GetMessages respects the afterMsgID
// parameter for pagination.
func TestGetMessages_AfterMsgID(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()

	conv := &model.Conversation{
		ID:      "conv-after",
		UserID1: "test-user",
		UserID2: "peer1",
		Title:   "Test",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conv: %v", err)
	}

	for i := uint32(1); i <= 5; i++ {
		msg := &model.Message{
			ID:              fmt.Sprintf("msg-%d", i),
			ClientMessageID: fmt.Sprintf("cid-%d", i),
			ConversationID:  "conv-after",
			MessageID:       i,
			SenderID:        "peer1",
			Content:         fmt.Sprintf("Message %d", i),
			CreatedAt:       time.Now(),
		}
		if err := db.Messages.Create(ctx, msg); err != nil {
			t.Fatalf("seed msg: %v", err)
		}
	}

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	// Fetch messages after MessageID=3, should get 4 and 5.
	result, err := c.GetMessages(ctx, "conv-after", 3, 10)
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result.Messages))
	}
	if result.Messages[0].MessageID != 4 {
		t.Errorf("expected first message MessageID=4, got %d", result.Messages[0].MessageID)
	}
}

// TestGetMessages_HasMore verifies that GetMessages correctly detects when
// there are more messages available (limit+1 probe).
func TestGetMessages_HasMore(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()

	conv := &model.Conversation{
		ID:      "conv-hasmore",
		UserID1: "test-user",
		UserID2: "peer1",
		Title:   "Test",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conv: %v", err)
	}

	for i := uint32(1); i <= 5; i++ {
		msg := &model.Message{
			ID:              fmt.Sprintf("msg-%d", i),
			ClientMessageID: fmt.Sprintf("cid-%d", i),
			ConversationID:  "conv-hasmore",
			MessageID:       i,
			SenderID:        "peer1",
			Content:         fmt.Sprintf("Message %d", i),
			CreatedAt:       time.Now(),
		}
		if err := db.Messages.Create(ctx, msg); err != nil {
			t.Fatalf("seed msg: %v", err)
		}
	}

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	// Query with limit=3, should return 3 and HasMore=true.
	result, err := c.GetMessages(ctx, "conv-hasmore", 0, 3)
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(result.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result.Messages))
	}
	if !result.HasMore {
		t.Error("expected HasMore=true")
	}
}

// TestSearchMessages_LocalDB verifies that SearchMessages reads from the local
// database and returns messages in DESC order (newest first).
func TestSearchMessages_LocalDB(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()

	conv := &model.Conversation{
		ID:      "conv-search",
		UserID1: "test-user",
		UserID2: "peer1",
		Title:   "Test",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conv: %v", err)
	}

	msg1 := &model.Message{
		ID:              "msg-1",
		ClientMessageID: "cid-1",
		ConversationID:  "conv-search",
		MessageID:       1,
		SenderID:        "peer1",
		Content:         "hello world",
		CreatedAt:       time.Now(),
	}
	msg2 := &model.Message{
		ID:              "msg-2",
		ClientMessageID: "cid-2",
		ConversationID:  "conv-search",
		MessageID:       2,
		SenderID:        "peer1",
		Content:         "goodbye world",
		CreatedAt:       time.Now(),
	}
	msg3 := &model.Message{
		ID:              "msg-3",
		ClientMessageID: "cid-3",
		ConversationID:  "conv-search",
		MessageID:       3,
		SenderID:        "peer1",
		Content:         "hello there",
		CreatedAt:       time.Now(),
	}
	if err := db.Messages.Create(ctx, msg1); err != nil {
		t.Fatalf("seed msg1: %v", err)
	}
	if err := db.Messages.Create(ctx, msg2); err != nil {
		t.Fatalf("seed msg2: %v", err)
	}
	if err := db.Messages.Create(ctx, msg3); err != nil {
		t.Fatalf("seed msg3: %v", err)
	}

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	result, err := c.SearchMessages(ctx, "conv-search", "hello", 0, 10)
	if err != nil {
		t.Fatalf("SearchMessages() error: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Errorf("expected 2 messages matching 'hello', got %d", len(result.Messages))
	}
	// Verify DESC order: msg-3 (newest) should come before msg-1.
	if result.Messages[0].ID != "msg-3" {
		t.Errorf("expected first result to be msg-3 (DESC order), got %s", result.Messages[0].ID)
	}
	if result.Messages[1].ID != "msg-1" {
		t.Errorf("expected second result to be msg-1, got %s", result.Messages[1].ID)
	}
}

// TestSearchMessages_NoResult verifies that SearchMessages returns an empty
// list when no messages match the query.
func TestSearchMessages_NoResult(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()

	conv := &model.Conversation{
		ID:      "conv-noresult",
		UserID1: "test-user",
		UserID2: "peer1",
		Title:   "Test",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conv: %v", err)
	}

	msg := &model.Message{
		ID:              "msg-1",
		ClientMessageID: "cid-1",
		ConversationID:  "conv-noresult",
		MessageID:       1,
		SenderID:        "peer1",
		Content:         "hello world",
		CreatedAt:       time.Now(),
	}
	if err := db.Messages.Create(ctx, msg); err != nil {
		t.Fatalf("seed msg: %v", err)
	}

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"),
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	result, err := c.SearchMessages(ctx, "conv-noresult", "nonexistent", 0, 10)
	if err != nil {
		t.Fatalf("SearchMessages() error: %v", err)
	}
	if len(result.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result.Messages))
	}
	if result.HasMore {
		t.Error("expected HasMore=false for empty result")
	}
}

// ---------------------------------------------------------------------------
// FetchMoreMessages (D-126)
// ---------------------------------------------------------------------------

// TestFetchMoreMessages_Success verifies that FetchMoreMessages fetches from
// the server via RPC, persists to local DB, and returns the results.
func TestFetchMoreMessages_Success(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("get_messages", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		msgs := []model.Message{
			{
				ID:              "msg-remote-1",
				ClientMessageID: "cid-remote-1",
				ConversationID:  "conv-fetch",
				MessageID:       1,
				SenderID:        "peer1",
				Content:         "Remote message 1",
				CreatedAt:       time.Now(),
			},
			{
				ID:              "msg-remote-2",
				ClientMessageID: "cid-remote-2",
				ConversationID:  "conv-fetch",
				MessageID:       2,
				SenderID:        "peer1",
				Content:         "Remote message 2",
				CreatedAt:       time.Now(),
			},
		}
		return json.Marshal(GetMessagesResult{Messages: msgs, HasMore: false})
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.FetchMoreMessages(context.Background(), "conv-fetch", 0, 10)
	if err != nil {
		t.Fatalf("FetchMoreMessages() error: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result.Messages))
	}

	// Verify messages were persisted to local DB.
	localMsgs, err := c.db.Messages.ListByConversation(context.Background(), "conv-fetch", 0, 10)
	if err != nil {
		t.Fatalf("ListByConversation() error: %v", err)
	}
	if len(localMsgs) != 2 {
		t.Errorf("expected 2 messages in local DB, got %d", len(localMsgs))
	}
}

// TestFetchMoreMessages_RPCFail verifies that FetchMoreMessages returns an
// error when the RPC call fails.
func TestFetchMoreMessages_RPCFail(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("get_messages", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return nil, fmt.Errorf("server error")
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	_, err := c.FetchMoreMessages(context.Background(), "conv-fetch", 0, 10)
	if err == nil {
		t.Fatal("expected error from FetchMoreMessages, got nil")
	}
}

// TestFetchMoreMessages_UpsertConflict verifies that FetchMoreMessages handles
// duplicate key errors gracefully (best-effort persistence).
func TestFetchMoreMessages_UpsertConflict(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("get_messages", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		msgs := []model.Message{
			{
				ID:              "msg-existing",
				ClientMessageID: "cid-existing",
				ConversationID:  "conv-conflict",
				MessageID:       1,
				SenderID:        "peer1",
				Content:         "Existing message",
				CreatedAt:       time.Now(),
			},
		}
		return json.Marshal(GetMessagesResult{Messages: msgs, HasMore: false})
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Pre-seed the message in local DB.
	existingMsg := &model.Message{
		ID:              "msg-existing",
		ClientMessageID: "cid-existing",
		ConversationID:  "conv-conflict",
		MessageID:       1,
		SenderID:        "peer1",
		Content:         "Existing message",
		CreatedAt:       time.Now(),
	}
	if err := c.db.Messages.Create(context.Background(), existingMsg); err != nil {
		t.Fatalf("seed msg: %v", err)
	}

	// FetchMoreMessages should succeed even though the message already exists.
	result, err := c.FetchMoreMessages(context.Background(), "conv-conflict", 0, 10)
	if err != nil {
		t.Fatalf("FetchMoreMessages() error: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(result.Messages))
	}
}

// TestFetchMoreMessages_EmptyResult verifies that FetchMoreMessages handles
// an empty result from the server correctly.
func TestFetchMoreMessages_EmptyResult(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("get_messages", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(GetMessagesResult{Messages: []model.Message{}, HasMore: false})
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.FetchMoreMessages(context.Background(), "conv-empty", 0, 10)
	if err != nil {
		t.Fatalf("FetchMoreMessages() error: %v", err)
	}
	if len(result.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result.Messages))
	}
}

// ---------------------------------------------------------------------------
// Call() — request/response matching and error handling
// ---------------------------------------------------------------------------

// TestCall_BasicRequestResponse verifies that a simple RPC call returns the
// data sent back by the mock server.
func TestCall_BasicRequestResponse(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("echo", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"hello":"world"}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	data, err := c.Call(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["hello"] != "world" {
		t.Fatalf("expected hello=world, got: %v", result)
	}
}

// TestCall_RequestIDMatching verifies that concurrent RPC calls are correctly
// matched to their responses via request IDs.
func TestCall_RequestIDMatching(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("echo", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return req.Params, nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	results := make([]string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			payload := fmt.Sprintf(`{"idx":%d}`, idx)
			data, err := c.Call(context.Background(), "echo", json.RawMessage(payload))
			errs[idx] = err
			if err == nil {
				var m map[string]int
				_ = json.Unmarshal(data, &m)
				results[idx] = fmt.Sprintf("%d", m["idx"])
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Errorf("call %d failed: %v", i, errs[i])
			continue
		}
		expected := fmt.Sprintf("%d", i)
		if results[i] != expected {
			t.Errorf("call %d: expected idx=%s, got idx=%s", i, expected, results[i])
		}
	}
}

// TestCall_Timeout verifies that a Call that exceeds the RPC timeout returns a
// TimeoutError with code -401.
func TestCall_Timeout(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	// Register a handler that never responds (we don't register it, so the mock
	// server won't send a response).
	// The mock server only sends a response if a handler is registered, so
	// omitting the handler will cause the request to hang.

	c := newTestClient(t, server, WithRPCTimeout(200*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	_, err := c.Call(context.Background(), "nonexistent_method", nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	clientErr, ok := err.(*ClientError)
	if !ok {
		t.Fatalf("expected *ClientError, got %T: %v", err, err)
	}
	if clientErr.Code != ErrorCodeTimeoutError {
		t.Fatalf("expected code %d, got %d", ErrorCodeTimeoutError, clientErr.Code)
	}
}

// TestCall_ContextCancelled verifies that a Call with a cancelled context
// returns an error.
func TestCall_ContextCancelled(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Create a context that we cancel immediately.
	callCtx, callCancel := context.WithCancel(context.Background())
	callCancel() // cancel before calling

	_, err := c.Call(callCtx, "slow_method", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// TestCall_ServerError verifies that a server-returned error code is propagated
// as a ClientError.
func TestCall_ServerError(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("failing_method", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return nil, fmt.Errorf("something went wrong")
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	_, err := c.Call(context.Background(), "failing_method", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	clientErr, ok := err.(*ClientError)
	if !ok {
		t.Fatalf("expected *ClientError, got %T: %v", err, err)
	}
	// The mock server returns ResponseCodeError (-1) for handler errors.
	if clientErr.Code != protocol.ResponseCodeError {
		t.Fatalf("expected code %d, got %d", protocol.ResponseCodeError, clientErr.Code)
	}
}

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

// TestHeartbeat_PeriodicSend verifies that the client sends periodic heartbeat
// requests to the server.
func TestHeartbeat_PeriodicSend(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("heartbeat", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})

	var heartbeatCount int64
	server.SetRPCHandler("heartbeat", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		atomic.AddInt64(&heartbeatCount, 1)
		return json.RawMessage(`{}`), nil
	})

	c := newTestClient(t, server, WithHeartbeatInterval(100*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}

	// Wait for at least 2 heartbeats.
	time.Sleep(350 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	count := atomic.LoadInt64(&heartbeatCount)
	if count < 2 {
		t.Fatalf("expected at least 2 heartbeats, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// RPC convenience methods — verify correct params are sent
// ---------------------------------------------------------------------------

// TestSendMessage_CorrectParams verifies that SendMessage sends the correct
// method name and parameters.
func TestSendMessage_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("send_message", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{"message":{"id":"msg-1"},"duplicate":false}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.SendMessage(context.Background(), "conv-1", "hello", "client-msg-1", 0)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Duplicate {
		t.Error("expected duplicate=false")
	}

	// Verify method name.
	if receivedReq.Method != "send_message" {
		t.Errorf("expected method send_message, got %s", receivedReq.Method)
	}

	// Verify params.
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["conversation_id"] != "conv-1" {
		t.Errorf("expected conversation_id=conv-1, got %v", params["conversation_id"])
	}
	if params["content"] != "hello" {
		t.Errorf("expected content=hello, got %v", params["content"])
	}
	if params["client_message_id"] != "client-msg-1" {
		t.Errorf("expected client_message_id=client-msg-1, got %v", params["client_message_id"])
	}
}

// TestCreateConversation_CorrectParams verifies that CreateConversation sends
// the correct method name and parameters.
func TestCreateConversation_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("create_conversation", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{"conversation":{"id":"conv-1"},"duplicate":false}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.CreateConversation(context.Background(), "user-2", "Test Chat")
	if err != nil {
		t.Fatalf("CreateConversation failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if receivedReq.Method != "create_conversation" {
		t.Errorf("expected method create_conversation, got %s", receivedReq.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["user_id"] != "user-2" {
		t.Errorf("expected user_id=user-2, got %v", params["user_id"])
	}
	if params["title"] != "Test Chat" {
		t.Errorf("expected title=Test Chat, got %v", params["title"])
	}
}

// TestMarkAsRead_CorrectParams verifies that MarkAsRead sends the correct method
// name and parameters.
func TestMarkAsRead_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("mark_as_read", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	err := c.MarkAsRead(context.Background(), "conv-1", 42)
	if err != nil {
		t.Fatalf("MarkAsRead failed: %v", err)
	}

	if receivedReq.Method != "mark_as_read" {
		t.Errorf("expected method mark_as_read, got %s", receivedReq.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["conversation_id"] != "conv-1" {
		t.Errorf("expected conversation_id=conv-1, got %v", params["conversation_id"])
	}
	// JSON numbers are float64 by default.
	if msgID, ok := params["message_id"].(float64); !ok || uint32(msgID) != 42 {
		t.Errorf("expected message_id=42, got %v", params["message_id"])
	}
}

// TestSyncUpdates_CorrectParams verifies that SyncUpdates sends the correct
// method name and parameters.
func TestSyncUpdates_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReqs []protocol.PackageDataRequest
	var mu sync.Mutex
	// Override the handler to capture all calls (including the FullSync one).
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		mu.Lock()
		receivedReqs = append(receivedReqs, *req)
		mu.Unlock()
		resp, err := json.Marshal(SyncUpdatesResult{
			Updates:   nil,
			HasMore:   false,
			LatestSeq: 10,
		})
		return resp, err
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.SyncUpdates(context.Background(), 5, 50)
	if err != nil {
		t.Fatalf("SyncUpdates failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Find the call with after_seq=5 (skip the FullSync call with after_seq=0).
	mu.Lock()
	found := false
	for _, req := range receivedReqs {
		var params map[string]any
		_ = json.Unmarshal(req.Params, &params)
		afterSeq, _ := params["after_seq"].(float64)
		if uint32(afterSeq) == 5 {
			found = true
			if req.Method != "sync_updates" {
				t.Errorf("expected method sync_updates, got %s", req.Method)
			}
			limit, _ := params["limit"].(float64)
			if int(limit) != 50 {
				t.Errorf("expected limit=50, got %v", params["limit"])
			}
			break
		}
	}
	mu.Unlock()
	if !found {
		t.Error("did not find sync_updates call with after_seq=5")
	}
}

// ---------------------------------------------------------------------------
// Additional RPC convenience methods — parameter validation
// ---------------------------------------------------------------------------

// TestDeleteConversation_CorrectParams verifies that DeleteConversation sends
// the correct method name and parameters.
func TestDeleteConversation_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("delete_conversation", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	_, err := c.DeleteConversation(context.Background(), "conv-del")
	if err != nil {
		t.Fatalf("DeleteConversation failed: %v", err)
	}

	if receivedReq.Method != "delete_conversation" {
		t.Errorf("expected method delete_conversation, got %s", receivedReq.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["conversation_id"] != "conv-del" {
		t.Errorf("expected conversation_id=conv-del, got %v", params["conversation_id"])
	}
}

// TestRestoreConversation_CorrectParams verifies that RestoreConversation sends
// the correct method name and parameters.
func TestRestoreConversation_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("restore_conversation", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	_, err := c.RestoreConversation(context.Background(), "conv-restore")
	if err != nil {
		t.Fatalf("RestoreConversation failed: %v", err)
	}

	if receivedReq.Method != "restore_conversation" {
		t.Errorf("expected method restore_conversation, got %s", receivedReq.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["conversation_id"] != "conv-restore" {
		t.Errorf("expected conversation_id=conv-restore, got %v", params["conversation_id"])
	}
}

// TestDeleteConversation_ReturnsResult verifies that DeleteConversation correctly
// parses the server response including the deleted_message_count field (P3 fix).
func TestDeleteConversation_ReturnsResult(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	server.SetRPCHandler("delete_conversation", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"status":"ok","deleted_message_count":5}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.DeleteConversation(context.Background(), "conv-del-result")
	if err != nil {
		t.Fatalf("DeleteConversation failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != "ok" {
		t.Errorf("Status: got=%q want=%q", result.Status, "ok")
	}
	if result.DeletedMessageCount != 5 {
		t.Errorf("DeletedMessageCount: got=%d want=5", result.DeletedMessageCount)
	}
}

// TestRestoreConversation_ReturnsResult verifies that RestoreConversation
// correctly parses the server response including the restored_message_count
// field (P3 fix).
func TestRestoreConversation_ReturnsResult(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	server.SetRPCHandler("restore_conversation", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"conversation":{"id":"conv-restore-result"},"restored_message_count":3}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.RestoreConversation(context.Background(), "conv-restore-result")
	if err != nil {
		t.Fatalf("RestoreConversation failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Conversation == nil {
		t.Fatal("expected non-nil Conversation")
	}
	if result.Conversation.ID != "conv-restore-result" {
		t.Errorf("Conversation.ID: got=%q want=%q", result.Conversation.ID, "conv-restore-result")
	}
	if result.RestoredMessageCount != 3 {
		t.Errorf("RestoredMessageCount: got=%d want=3", result.RestoredMessageCount)
	}
}

// TestDeleteConversation_ZeroDeletedCount verifies that DeleteConversation
// correctly parses a server response with deleted_message_count=0 (empty
// conversation or already-deleted scenario).
func TestDeleteConversation_ZeroDeletedCount(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	server.SetRPCHandler("delete_conversation", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"status":"ok","deleted_message_count":0}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.DeleteConversation(context.Background(), "conv-empty")
	if err != nil {
		t.Fatalf("DeleteConversation failed: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status: got=%q want=%q", result.Status, "ok")
	}
	if result.DeletedMessageCount != 0 {
		t.Errorf("DeletedMessageCount: got=%d want=0", result.DeletedMessageCount)
	}
}

// TestRestoreConversation_ZeroRestoredCount verifies that RestoreConversation
// correctly parses a server response with restored_message_count=0 (empty
// conversation or idempotent restore scenario).
func TestRestoreConversation_ZeroRestoredCount(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	server.SetRPCHandler("restore_conversation", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"conversation":{"id":"conv-empty"},"restored_message_count":0}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.RestoreConversation(context.Background(), "conv-empty")
	if err != nil {
		t.Fatalf("RestoreConversation failed: %v", err)
	}
	if result.Conversation == nil {
		t.Fatal("expected non-nil Conversation")
	}
	if result.RestoredMessageCount != 0 {
		t.Errorf("RestoredMessageCount: got=%d want=0", result.RestoredMessageCount)
	}
}

// TestDeleteMessage_CorrectParams verifies that DeleteMessage sends the correct
// method name and parameters.
func TestDeleteMessage_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("delete_message", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	err := c.DeleteMessage(context.Background(), "msg-abc")
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	if receivedReq.Method != "delete_message" {
		t.Errorf("expected method delete_message, got %s", receivedReq.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["message_id"] != "msg-abc" {
		t.Errorf("expected message_id=msg-abc, got %v", params["message_id"])
	}
}

// ---------------------------------------------------------------------------
// FullSync — delegates to syncManager.FullSync
// ---------------------------------------------------------------------------

// TestFullSync_Success verifies that FullSync completes successfully when the
// mock server returns an empty update batch (has_more=false).
func TestFullSync_Success(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if err := c.FullSync(context.Background()); err != nil {
		t.Fatalf("FullSync() error: %v", err)
	}
}

// TestFullSync_WithError verifies that FullSync propagates errors from the
// underlying sync_updates RPC.
func TestFullSync_WithError(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return nil, fmt.Errorf("server unavailable")
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	err := c.FullSync(context.Background())
	if err == nil {
		t.Fatal("FullSync() should fail when sync_updates returns an error")
	}
	clientErr, ok := err.(*ClientError)
	if !ok {
		t.Fatalf("expected *ClientError, got %T: %v", err, err)
	}
	if clientErr.Code != ErrorCodeSyncError {
		t.Fatalf("expected code %d, got %d", ErrorCodeSyncError, clientErr.Code)
	}
}

// TestFullSync_DelegatesToSyncManager verifies that FullSync delegates to the
// syncManager by checking the correct RPC method is called.
func TestFullSync_DelegatesToSyncManager(t *testing.T) {
	server := newMockWSServer(t)

	var syncCalls atomic.Int64
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		syncCalls.Add(1)
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	callsBefore := syncCalls.Load()
	if err := c.FullSync(context.Background()); err != nil {
		t.Fatalf("FullSync() error: %v", err)
	}
	// FullSync should have made at least one additional sync_updates call.
	if syncCalls.Load() <= callsBefore {
		t.Error("FullSync did not call sync_updates")
	}
}

// ---------------------------------------------------------------------------
// Bug #6: conversation_id extraction from params
// ---------------------------------------------------------------------------

// TestCall_ConversationIDExtraction_Normal verifies that a normal
// conversation_id string param is correctly extracted for RPC logging.
func TestCall_ConversationIDExtraction_Normal(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedConvID string
	server.SetRPCHandler("test_method", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		// Verify the request has the conversation_id in params.
		var params map[string]any
		_ = json.Unmarshal(req.Params, &params)
		if cid, ok := params["conversation_id"].(string); ok {
			receivedConvID = cid
		}
		return json.RawMessage(`{}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	params := map[string]any{
		"conversation_id": "conv-extract-1",
		"content":         "test",
	}
	_, err := c.Call(context.Background(), "test_method", params)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if receivedConvID != "conv-extract-1" {
		t.Errorf("expected conversation_id=conv-extract-1, got %q", receivedConvID)
	}
}

// TestCall_ConversationIDExtraction_Missing verifies that a missing
// conversation_id does not cause an error (graceful degradation).
func TestCall_ConversationIDExtraction_Missing(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("test_method", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// No conversation_id in params — should succeed without error.
	params := map[string]any{"content": "test"}
	_, err := c.Call(context.Background(), "test_method", params)
	if err != nil {
		t.Fatalf("Call should succeed without conversation_id: %v", err)
	}
}

// TestCall_ConversationIDExtraction_NonString verifies that a non-string
// conversation_id (e.g. integer) does not crash the client.
func TestCall_ConversationIDExtraction_NonString(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
	server.SetRPCHandler("test_method", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// conversation_id as integer — extraction should gracefully handle this.
	params := map[string]any{"conversation_id": 12345}
	_, err := c.Call(context.Background(), "test_method", params)
	if err != nil {
		t.Fatalf("Call should succeed with non-string conversation_id: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bug #8: Initial connection failure retry, context cancel clean exit
// ---------------------------------------------------------------------------

// TestConnectionMonitor_InitialConnectRetry verifies that the client retries
// the initial connection when the server is unreachable and exits cleanly
// when the context is cancelled.
func TestConnectionMonitor_InitialConnectRetry(t *testing.T) {
	// Create a client pointing to a non-existent server.
	db := newTestStore(t)
	c, err := New(
		WithServerURL("ws://127.0.0.1:1/no-server"),
		WithUserID("test-user"),
		WithDB(db),
		WithLogger(&testLogger{t: t}),
		WithReconnectBaseDelay(50*time.Millisecond),
		WithReconnectMaxDelay(100*time.Millisecond),
		WithReconnectMaxRetries(1),
		WithHeartbeatInterval(1*time.Hour),
		WithPullDebounce(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Start(ctx)
	}()

	// Wait for the client to attempt at least one connection.
	time.Sleep(200 * time.Millisecond)

	// Cancel context — should exit cleanly.
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() should return nil after context cancel, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after context cancellation")
	}
}

// TestStop_CleanExit verifies that Stop() causes Start() to return promptly.
func TestStop_CleanExit(t *testing.T) {
	db := newTestStore(t)
	c, err := New(
		WithServerURL("ws://127.0.0.1:1/no-server"),
		WithUserID("test-user"),
		WithDB(db),
		WithLogger(&testLogger{t: t}),
		WithReconnectBaseDelay(50*time.Millisecond),
		WithReconnectMaxDelay(100*time.Millisecond),
		WithHeartbeatInterval(1*time.Hour),
		WithPullDebounce(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	ctx := context.Background()
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Start(ctx)
	}()

	// Wait a moment for goroutines to start.
	time.Sleep(100 * time.Millisecond)

	// Stop should cause Start to return.
	c.Stop()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() should return nil after Stop(), got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after Stop()")
	}
}

// TestGetConversation_UserID2 verifies that GetConversation correctly uses
// LastReadMessageID2 when the current user is UserID2 (not UserID1).
func TestGetConversation_UserID2(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()

	// Seed a conversation where the test user is UserID2.
	conv := &model.Conversation{
		ID:                 "conv-user2",
		UserID1:            "peer1",
		UserID2:            "test-user",
		Type:               "1-on-1",
		Title:              "User2 Chat",
		LastReadMessageID2: 10, // user2 has read up to message 10
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conv: %v", err)
	}

	// Seed messages: 5 unread (MessageID 11..15).
	for i := uint32(11); i <= 15; i++ {
		msg := &model.Message{
			ID:              fmt.Sprintf("msg-u2-%d", i),
			ClientMessageID: fmt.Sprintf("cid-u2-%d", i),
			ConversationID:  "conv-user2",
			MessageID:       i,
			SenderID:        "peer1",
			Content:         "hello from peer1",
			CreatedAt:       time.Now(),
		}
		if err := db.Messages.Create(ctx, msg); err != nil {
			t.Fatalf("seed msg: %v", err)
		}
	}

	c, err := New(
		WithServerURL("ws://localhost:8080/ws"),
		WithUserID("test-user"), // user is UserID2
		WithDB(db),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer c.Stop()

	result, err := c.GetConversation(ctx, "conv-user2")
	if err != nil {
		t.Fatalf("GetConversation() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Conversation.ID != "conv-user2" {
		t.Errorf("expected ID=conv-user2, got %s", result.Conversation.ID)
	}
	// Verify unread count is based on LastReadMessageID2 (10), so messages 11..15 = 5 unread.
	if result.UnreadCount != 5 {
		t.Errorf("expected UnreadCount=5 (using LastReadMessageID2), got %d", result.UnreadCount)
	}
}
