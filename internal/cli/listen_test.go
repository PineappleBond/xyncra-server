package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

func TestCLIUpdateHandler_OnMessage(t *testing.T) {
	h := newCLIUpdateHandler(nil)
	msg := &model.Message{
		MessageID:      42,
		SenderID:       "alice",
		ConversationID: "conv-1",
		Content:        "hello world",
	}

	output := captureStdout(func() {
		if err := h.OnMessage(context.Background(), msg); err != nil {
			t.Fatalf("OnMessage() error: %v", err)
		}
	})

	if !strings.Contains(output, "[new message]") {
		t.Errorf("output should contain [new message], got %q", output)
	}
	if !strings.Contains(output, "seq=42") {
		t.Errorf("output should contain seq=42, got %q", output)
	}
	if !strings.Contains(output, "from=alice") {
		t.Errorf("output should contain from=alice, got %q", output)
	}
	if !strings.Contains(output, "conv=conv-1") {
		t.Errorf("output should contain conv=conv-1, got %q", output)
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("output should contain message content, got %q", output)
	}
}

func TestCLIUpdateHandler_OnMessage_Nil(t *testing.T) {
	h := newCLIUpdateHandler(nil)
	// Should not panic.
	if err := h.OnMessage(context.Background(), nil); err != nil {
		t.Fatalf("OnMessage(nil) error: %v", err)
	}
}

func TestCLIUpdateHandler_OnDeleteMessage(t *testing.T) {
	h := newCLIUpdateHandler(nil)

	output := captureStdout(func() {
		if err := h.OnDeleteMessage(context.Background(), "msg-123", "conv-456"); err != nil {
			t.Fatalf("OnDeleteMessage() error: %v", err)
		}
	})

	if !strings.Contains(output, "[delete message]") {
		t.Errorf("output should contain [delete message], got %q", output)
	}
	if !strings.Contains(output, "conv=conv-456") {
		t.Errorf("output should contain conv=conv-456, got %q", output)
	}
	if !strings.Contains(output, "msg=msg-123") {
		t.Errorf("output should contain msg=msg-123, got %q", output)
	}
}

func TestCLIUpdateHandler_OnMarkRead(t *testing.T) {
	h := newCLIUpdateHandler(nil)

	output := captureStdout(func() {
		if err := h.OnMarkRead(context.Background(), "conv-789", 100); err != nil {
			t.Fatalf("OnMarkRead() error: %v", err)
		}
	})

	if !strings.Contains(output, "[mark read]") {
		t.Errorf("output should contain [mark read], got %q", output)
	}
	if !strings.Contains(output, "conv=conv-789") {
		t.Errorf("output should contain conv=conv-789, got %q", output)
	}
	if !strings.Contains(output, "msg_id=100") {
		t.Errorf("output should contain msg_id=100, got %q", output)
	}
}

func TestCLIUpdateHandler_OnConversation(t *testing.T) {
	h := newCLIUpdateHandler(nil)
	conv := &model.Conversation{
		ID:    "conv-abc",
		Title: "My Chat",
	}

	output := captureStdout(func() {
		if err := h.OnConversation(context.Background(), conv); err != nil {
			t.Fatalf("OnConversation() error: %v", err)
		}
	})

	if !strings.Contains(output, "[conversation]") {
		t.Errorf("output should contain [conversation], got %q", output)
	}
	if !strings.Contains(output, "id=conv-abc") {
		t.Errorf("output should contain id=conv-abc, got %q", output)
	}
	if !strings.Contains(output, "My Chat") {
		t.Errorf("output should contain title, got %q", output)
	}
}

func TestCLIUpdateHandler_OnConversation_Nil(t *testing.T) {
	h := newCLIUpdateHandler(nil)
	// Should not panic.
	if err := h.OnConversation(context.Background(), nil); err != nil {
		t.Fatalf("OnConversation(nil) error: %v", err)
	}
}

func TestCLIUpdateHandler_OnGap(t *testing.T) {
	h := newCLIUpdateHandler(nil)

	output := captureStdout(func() {
		if err := h.OnGap(context.Background(), 999); err != nil {
			t.Fatalf("OnGap() error: %v", err)
		}
	})

	if !strings.Contains(output, "[gap]") {
		t.Errorf("output should contain [gap], got %q", output)
	}
	if !strings.Contains(output, "seq=999") {
		t.Errorf("output should contain seq=999, got %q", output)
	}
}

func TestCLILogger_Info(t *testing.T) {
	l := &cliLogger{debug: false}

	output := captureStderr(func() {
		l.Info("server started", "port", 8080)
	})

	if !strings.Contains(output, "[INFO]") {
		t.Errorf("output should contain [INFO], got %q", output)
	}
	if !strings.Contains(output, "server started") {
		t.Errorf("output should contain message, got %q", output)
	}
	if !strings.Contains(output, "port=8080") {
		t.Errorf("output should contain port=8080, got %q", output)
	}

	// Timestamp format check: should start with [YYYY-MM-DD HH:MM:SS]
	tsPattern := `^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\]`
	matched, _ := regexp.MatchString(tsPattern, output)
	if !matched {
		t.Errorf("output should start with timestamp, got %q", output)
	}
}

func TestCLILogger_Error(t *testing.T) {
	l := &cliLogger{debug: false}

	output := captureStderr(func() {
		l.Error("connection failed", "error", "timeout")
	})

	if !strings.Contains(output, "[ERROR]") {
		t.Errorf("output should contain [ERROR], got %q", output)
	}
	if !strings.Contains(output, "connection failed") {
		t.Errorf("output should contain message, got %q", output)
	}
	if !strings.Contains(output, "error=timeout") {
		t.Errorf("output should contain error=timeout, got %q", output)
	}
}

func TestCLILogger_DebugSuppressed(t *testing.T) {
	l := &cliLogger{debug: false}

	output := captureStderr(func() {
		l.Debug("should not appear")
	})

	if output != "" {
		t.Errorf("Debug should be suppressed when debug=false, got %q", output)
	}
}

func TestCLILogger_DebugEnabled(t *testing.T) {
	l := &cliLogger{debug: true}

	output := captureStderr(func() {
		l.Debug("debug info", "key", "value")
	})

	if !strings.Contains(output, "[DEBUG]") {
		t.Errorf("output should contain [DEBUG], got %q", output)
	}
	if !strings.Contains(output, "debug info") {
		t.Errorf("output should contain message, got %q", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("output should contain key=value, got %q", output)
	}
}

func TestNewCLILogger_DebugEnv(t *testing.T) {
	t.Run("XYNCRA_DEBUG=1", func(t *testing.T) {
		t.Setenv("XYNCRA_DEBUG", "1")
		l := newCLILogger()
		if !l.debug {
			t.Error("debug should be true when XYNCRA_DEBUG=1")
		}
	})

	t.Run("XYNCRA_DEBUG=true", func(t *testing.T) {
		t.Setenv("XYNCRA_DEBUG", "true")
		l := newCLILogger()
		if !l.debug {
			t.Error("debug should be true when XYNCRA_DEBUG=true")
		}
	})

	t.Run("XYNCRA_DEBUG=0", func(t *testing.T) {
		t.Setenv("XYNCRA_DEBUG", "0")
		l := newCLILogger()
		if l.debug {
			t.Error("debug should be false when XYNCRA_DEBUG=0")
		}
	})

	t.Run("XYNCRA_DEBUG not set", func(t *testing.T) {
		t.Setenv("XYNCRA_DEBUG", "")
		l := newCLILogger()
		if l.debug {
			t.Error("debug should be false when XYNCRA_DEBUG is empty")
		}
	})
}

func TestFormatLogArgs(t *testing.T) {
	tests := []struct {
		name string
		args []any
		want string
	}{
		{"empty", nil, ""},
		{"single key", []any{"key"}, " key=MISSING"},
		{"key-value", []any{"key", "value"}, " key=value"},
		{"multiple pairs", []any{"a", 1, "b", 2}, " a=1 b=2"},
		{"odd number", []any{"a", 1, "b"}, " a=1 b=MISSING"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLogArgs(tt.args)
			if got != tt.want {
				t.Errorf("formatLogArgs(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestLogTimestamp(t *testing.T) {
	ts := logTimestamp()
	// Should match YYYY-MM-DD HH:MM:SS format.
	matched, err := regexp.MatchString(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`, ts)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Errorf("logTimestamp() = %q, want format YYYY-MM-DD HH:MM:SS", ts)
	}
}

// ---------------------------------------------------------------------------
// IPC handler tests — verify registerIPCHandlers dispatches correctly
// ---------------------------------------------------------------------------

// setupIPCWithClient creates a mock WebSocket server + XyncraClient + IPCServer
// with handlers registered via registerIPCHandlers. Returns the IPC socket path
// and the underlying ClientDB for test assertions.
func setupIPCWithClient(t *testing.T) (sockPath string, db *store.ClientDB, cleanup func()) {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var pkg protocol.Package
			if err := conn.ReadJSON(&pkg); err != nil {
				return
			}
			var req protocol.PackageDataRequest
			_ = json.Unmarshal(pkg.Data, &req)

			var respJSON []byte
			switch req.Method {
			case "sync_updates":
				data, _ := json.Marshal(client.SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
				respData, _ := json.Marshal(protocol.PackageDataResponse{ID: req.ID, Code: protocol.ResponseCodeOK, Msg: "ok", Data: data})
				respJSON = respData
			case "send_message":
				result := client.SendMessageResult{
					Message: &model.Message{MessageID: 100, ConversationID: "conv-1", ClientMessageID: "cid-1", Content: "hello"},
				}
				data, _ := json.Marshal(result)
				respData, _ := json.Marshal(protocol.PackageDataResponse{ID: req.ID, Code: protocol.ResponseCodeOK, Data: data})
				respJSON = respData
			case "create_conversation":
				result := client.CreateConversationResult{
					Conversation: &model.Conversation{ID: "conv-new", UserID2: "peer1"},
					Duplicate:    false,
				}
				data, _ := json.Marshal(result)
				respData, _ := json.Marshal(protocol.PackageDataResponse{ID: req.ID, Code: protocol.ResponseCodeOK, Data: data})
				respJSON = respData
			case "delete_conversation", "restore_conversation", "delete_message":
				respData, _ := json.Marshal(protocol.PackageDataResponse{ID: req.ID, Code: protocol.ResponseCodeOK, Data: json.RawMessage(`{}`)})
				respJSON = respData
			case "mark_as_read":
				// Return a server-confirmed cursor value (D-012, D-047).
				marResult := struct {
					LastReadMessageID uint32 `json:"last_read_message_id"`
				}{LastReadMessageID: 42}
				marData, _ := json.Marshal(marResult)
				respData, _ := json.Marshal(protocol.PackageDataResponse{ID: req.ID, Code: protocol.ResponseCodeOK, Data: marData})
				respJSON = respData
			default:
				respData, _ := json.Marshal(protocol.PackageDataResponse{ID: req.ID, Code: protocol.ResponseCodeOK, Data: json.RawMessage(`{}`)})
				respJSON = respData
			}

			respPkg := protocol.Package{Version: 1, Type: protocol.PackageTypeResponse, Data: json.RawMessage(respJSON)}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = conn.WriteJSON(respPkg)
		}
	}))

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	tmpDir, err := os.MkdirTemp("/tmp", "xyncra-ipc-*")
	if err != nil {
		ts.Close()
		t.Fatalf("MkdirTemp: %v", err)
	}

	db, err = store.New(tmpDir + "/test.db")
	if err != nil {
		ts.Close()
		t.Fatalf("open db: %v", err)
	}

	xc, err := client.New(
		client.WithServerURL(wsURL),
		client.WithUserID("testuser"),
		client.WithDB(db),
	)
	if err != nil {
		_ = db.Close()
		ts.Close()
		t.Fatalf("create client: %v", err)
	}

	// Start client in background so it connects.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = xc.Start(ctx) }()

	// Give it a moment to connect.
	time.Sleep(500 * time.Millisecond)

	sockPath = tmpDir + "/xyncra.sock"
	ipcServer := NewIPCServer(sockPath)
	registerIPCHandlers(ipcServer, xc, db, "testuser")
	if err := ipcServer.Start(context.Background()); err != nil {
		cancel()
		xc.Stop()
		_ = db.Close()
		ts.Close()
		t.Fatalf("IPC server start: %v", err)
	}

	cleanup = func() {
		cancel()
		xc.Stop()
		_ = ipcServer.Stop()
		_ = db.Close()
		ts.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return sockPath, db, cleanup
}

func TestIPCHandler_CreateConversation(t *testing.T) {
	sockPath, _, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "create_conversation", map[string]any{
		"user_id2": "peer1",
		"title":    "Test",
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}
}

func TestIPCHandler_DeleteConversation(t *testing.T) {
	sockPath, _, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "delete_conversation", map[string]any{
		"conversation_id": "conv-1",
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}
}

func TestIPCHandler_RestoreConversation(t *testing.T) {
	sockPath, _, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "restore_conversation", map[string]any{
		"conversation_id": "conv-1",
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}
}

func TestIPCHandler_DeleteMessage(t *testing.T) {
	sockPath, _, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "delete_message", map[string]any{
		"message_id": "msg-123",
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}
}

func TestIPCHandler_MarkAsRead(t *testing.T) {
	sockPath, _, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "mark_as_read", map[string]any{
		"conversation_id": "conv-1",
		"message_id":      uint32(42),
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}
}

func TestIPCHandler_SyncUpdates(t *testing.T) {
	sockPath, _, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "sync_updates", nil)
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}
}

func TestIPCHandler_InvalidParams(t *testing.T) {
	sockPath, _, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	// Send invalid params (a string instead of an object) for create_conversation.
	resp, err := ipcClient.Call(context.Background(), "create_conversation", "invalid")
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	// Handler should return an error response for invalid params.
	if resp.Error == nil {
		t.Fatal("expected error response for invalid params")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestIPCHandler_UnknownMethod(t *testing.T) {
	sockPath, _, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "nonexistent_method", nil)
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// Bug 2 verification: automatic log cleanup goroutine (D-040)
// ---------------------------------------------------------------------------

// TestRunCleanup_DeletesExpiredLogs verifies that runCleanup hard-deletes RPC
// logs and notification logs older than the retention period.
func TestRunCleanup_DeletesExpiredLogs(t *testing.T) {
	db, err := store.NewInMemory("cleanup_test")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Insert an old RPC log (10 days ago) and a recent one (1 hour ago).
	oldRPC := &model.RPCLog{
		ID:        "rpc-old",
		Type:      "request",
		RequestID: "req-1",
		Method:    "send_message",
		CreatedAt: now.Add(-10 * 24 * time.Hour),
	}
	recentRPC := &model.RPCLog{
		ID:        "rpc-recent",
		Type:      "request",
		RequestID: "req-2",
		Method:    "heartbeat",
		CreatedAt: now.Add(-1 * time.Hour),
	}
	if err := db.RPCLogs.Save(ctx, oldRPC); err != nil {
		t.Fatalf("save old rpc: %v", err)
	}
	if err := db.RPCLogs.Save(ctx, recentRPC); err != nil {
		t.Fatalf("save recent rpc: %v", err)
	}

	// Insert an old notification log and a recent one.
	oldNotif := &model.NotificationLog{
		ID:        "notif-old",
		Seq:       1,
		Type:      "message",
		CreatedAt: now.Add(-10 * 24 * time.Hour),
	}
	recentNotif := &model.NotificationLog{
		ID:        "notif-recent",
		Seq:       2,
		Type:      "message",
		CreatedAt: now.Add(-1 * time.Hour),
	}
	if err := db.NotificationLogs.Save(ctx, oldNotif); err != nil {
		t.Fatalf("save old notif: %v", err)
	}
	if err := db.NotificationLogs.Save(ctx, recentNotif); err != nil {
		t.Fatalf("save recent notif: %v", err)
	}

	// Run cleanup with 7-day retention.
	logger := newCLILogger()
	runCleanup(db, 7*24*time.Hour, logger)

	// Old records should be gone.
	before := now.Add(-7 * 24 * time.Hour)
	rpcRemaining, err := db.RPCLogs.CountBefore(ctx, before)
	if err != nil {
		t.Fatalf("count rpc: %v", err)
	}
	// No RPC logs should be older than 7 days now.
	if rpcRemaining != 0 {
		t.Errorf("rpc logs older than 7d remaining: got=%d want=0", rpcRemaining)
	}

	notifRemaining, err := db.NotificationLogs.CountBefore(ctx, before)
	if err != nil {
		t.Fatalf("count notif: %v", err)
	}
	if notifRemaining != 0 {
		t.Errorf("notification logs older than 7d remaining: got=%d want=0", notifRemaining)
	}

	// Recent records should still exist. We can verify by counting all records.
	// Use a very old cutoff to ensure we only count recent records.
	veryOld := now.Add(-2 * 24 * time.Hour)
	rpcCount, err := db.RPCLogs.CountBefore(ctx, veryOld)
	if err != nil {
		t.Fatalf("count recent rpc: %v", err)
	}
	// recentRPC is 1 hour old, well within 2 days.
	if rpcCount != 0 {
		t.Errorf("recent rpc logs should not be deleted: got count before 2d ago=%d", rpcCount)
	}
}

// TestStartLogCleanup_ExitsOnCancel verifies that the cleanup goroutine exits
// cleanly when the context is cancelled.
func TestStartLogCleanup_ExitsOnCancel(t *testing.T) {
	db, err := store.NewInMemory("cleanup_cancel_test")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	logger := newCLILogger()
	done := make(chan struct{})

	go func() {
		startLogCleanup(ctx, db, 50*time.Millisecond, 7*24*time.Hour, logger)
		close(done)
	}()

	// Let it tick at least once.
	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// OK: goroutine exited.
	case <-time.After(2 * time.Second):
		t.Fatal("startLogCleanup did not exit after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// Bug 3 verification: create_conversation should persist to local DB (D-035)
// ---------------------------------------------------------------------------

// TestIPCHandler_CreateConversation_PersistsLocally verifies that after a
// successful create_conversation IPC call, the new conversation is written to
// the local database so that list-conversations (D-035) can read it
// immediately.
func TestIPCHandler_CreateConversation_PersistsLocally(t *testing.T) {
	sockPath, db, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "create_conversation", map[string]any{
		"user_id2": "peer1",
		"title":    "Test",
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}

	// The conversation should now be in the local DB.
	ctx := context.Background()
	conv, err := db.Conversations.Get(ctx, "conv-new")
	if err != nil {
		t.Fatalf("conversation not persisted locally: %v", err)
	}
	if conv.ID != "conv-new" {
		t.Errorf("persisted conversation ID: got=%q want=%q", conv.ID, "conv-new")
	}
	if conv.UserID2 != "peer1" {
		t.Errorf("persisted conversation UserID2: got=%q want=%q", conv.UserID2, "peer1")
	}
}

// ---------------------------------------------------------------------------
// IPC-01: send_message persists to local DB immediately (H-4 fix)
// ---------------------------------------------------------------------------

// TestIPCHandler_SendMessage_PersistsLocally verifies that after a successful
// send_message IPC call, the sent message is written to the local database so
// that it is immediately visible (H-4 fix).
func TestIPCHandler_SendMessage_PersistsLocally(t *testing.T) {
	sockPath, db, cleanup := setupIPCWithClient(t)
	defer cleanup()

	// Pre-create the conversation so UpdateLastMessage succeeds.
	ctx := context.Background()
	conv := &model.Conversation{
		ID:      "conv-1",
		UserID1: "testuser",
		UserID2: "peer1",
		Type:    "1-on-1",
		Title:   "Test Conv",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "send_message", map[string]any{
		"conversation_id": "conv-1",
		"content":         "hello",
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}

	// The message should now be in the local DB.
	// The mock returns MessageID:100, ClientMessageID:"cid-1", ConversationID:"conv-1".
	msgs, err := db.Messages.ListByConversation(ctx, "conv-1", 0, 100)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected message to be persisted locally after send_message, found none")
	}
	found := false
	for _, m := range msgs {
		if m.ClientMessageID == "cid-1" && m.Content == "hello" {
			found = true
			break
		}
	}
	if !found {
		t.Error("sent message (cid-1) not found in local DB after send_message")
	}
}

// ---------------------------------------------------------------------------
// IPC-02: delete_message soft-deletes in local DB
// ---------------------------------------------------------------------------

// TestIPCHandler_DeleteMessage_SoftDeletesLocally verifies that after a
// successful delete_message IPC call, the message is soft-deleted in the local
// database.
func TestIPCHandler_DeleteMessage_SoftDeletesLocally(t *testing.T) {
	sockPath, db, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ctx := context.Background()

	// Seed a message in the local DB.
	msg := &model.Message{
		ID:              "msg-to-delete",
		ClientMessageID: "cmid-del",
		ConversationID:  "conv-1",
		MessageID:       10,
		SenderID:        "testuser",
		Content:         "to be deleted",
		Type:            "text",
		Status:          "sent",
	}
	if err := db.Messages.Create(ctx, msg); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "delete_message", map[string]any{
		"message_id": "msg-to-delete",
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}

	// Message should be soft-deleted (Get returns ErrNotFound).
	_, err = db.Messages.Get(ctx, "msg-to-delete")
	if err == nil {
		t.Fatal("expected message to be soft-deleted in local DB, but Get succeeded")
	}
	if err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// IPC-03: mark_as_read uses server-returned cursor (D-012, D-047)
// ---------------------------------------------------------------------------

// TestIPCHandler_MarkAsRead_UsesServerCursor verifies that after a successful
// mark_as_read IPC call, the local read cursor is set to the server-returned
// value (not the client-requested value).
func TestIPCHandler_MarkAsRead_UsesServerCursor(t *testing.T) {
	sockPath, db, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ctx := context.Background()

	// Pre-create the conversation so UpdateLastRead succeeds.
	conv := &model.Conversation{
		ID:      "conv-read",
		UserID1: "testuser",
		UserID2: "peer1",
		Type:    "1-on-1",
		Title:   "Read Test",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	// Request cursor at 10, but the mock server returns 42 (D-047).
	resp, err := ipcClient.Call(context.Background(), "mark_as_read", map[string]any{
		"conversation_id": "conv-read",
		"message_id":      uint32(10),
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}

	// The local cursor should be 42 (server-returned), not 10 (client-requested).
	gotConv, err := db.Conversations.Get(ctx, "conv-read")
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if gotConv.LastReadMessageID1 != 42 {
		t.Errorf("LastReadMessageID1: got=%d want=42 (server-returned value)", gotConv.LastReadMessageID1)
	}
}

// ---------------------------------------------------------------------------
// IPC-04: delete_conversation soft-deletes in local DB
// ---------------------------------------------------------------------------

// TestIPCHandler_DeleteConversation_SoftDeletesLocally verifies that after a
// successful delete_conversation IPC call, the conversation is soft-deleted in
// the local database.
func TestIPCHandler_DeleteConversation_SoftDeletesLocally(t *testing.T) {
	sockPath, db, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ctx := context.Background()

	// Seed a conversation in the local DB.
	conv := &model.Conversation{
		ID:      "conv-del",
		UserID1: "testuser",
		UserID2: "peer1",
		Type:    "1-on-1",
		Title:   "To Delete",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "delete_conversation", map[string]any{
		"conversation_id": "conv-del",
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}

	// Conversation should be soft-deleted (Get returns ErrNotFound).
	_, err = db.Conversations.Get(ctx, "conv-del")
	if err == nil {
		t.Fatal("expected conversation to be soft-deleted in local DB, but Get succeeded")
	}
	if err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// IPC-05: restore_conversation restores in local DB
// ---------------------------------------------------------------------------

// TestIPCHandler_RestoreConversation_RestoresLocally verifies that after a
// successful restore_conversation IPC call, the conversation is restored in the
// local database.
func TestIPCHandler_RestoreConversation_RestoresLocally(t *testing.T) {
	sockPath, db, cleanup := setupIPCWithClient(t)
	defer cleanup()

	ctx := context.Background()

	// Seed and then soft-delete a conversation.
	conv := &model.Conversation{
		ID:      "conv-restore",
		UserID1: "testuser",
		UserID2: "peer1",
		Type:    "1-on-1",
		Title:   "To Restore",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	if err := db.Conversations.Delete(ctx, "conv-restore"); err != nil {
		t.Fatalf("seed delete: %v", err)
	}

	// Verify it's deleted.
	_, err := db.Conversations.Get(ctx, "conv-restore")
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound after seed delete, got: %v", err)
	}

	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "restore_conversation", map[string]any{
		"conversation_id": "conv-restore",
	})
	if err != nil {
		t.Fatalf("IPC call: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("handler error: %v", resp.Error)
	}

	// Conversation should be restored.
	gotConv, err := db.Conversations.Get(ctx, "conv-restore")
	if err != nil {
		t.Fatalf("conversation not restored in local DB: %v", err)
	}
	if gotConv.Title != "To Restore" {
		t.Errorf("restored conversation title: got=%q want=%q", gotConv.Title, "To Restore")
	}
}

// ---------------------------------------------------------------------------
// Agent-aware display tests (D-065)
// ---------------------------------------------------------------------------

// TestCLIUpdateHandler_OnTyping_Matrix covers all 4 combinations of
// (agent|human) x (started|stopped) for the typing indicator display (D-065).
func TestCLIUpdateHandler_OnTyping_Matrix(t *testing.T) {
	tests := []struct {
		name       string
		userID     string
		isTyping   bool
		isAgent    bool
		wantLabel  string // "[typing]" or "[thinking]"
		wantAction string
	}{
		{"agent started typing", "agent/bot1", true, true, "[thinking]", "started typing"},
		{"agent stopped typing", "agent/bot1", false, true, "[thinking]", "stopped thinking"},
		{"human started typing", "alice", true, false, "[typing]", "started typing"},
		{"human stopped typing", "alice", false, false, "[typing]", "stopped typing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newCLIUpdateHandler(nil)
			output := captureStdout(func() {
				require.NoError(t, h.OnTyping(context.Background(), tc.userID, "conv-1", tc.isTyping, tc.isAgent))
			})
			assert.Contains(t, output, tc.wantLabel)
			assert.Contains(t, output, tc.wantAction)
			assert.Contains(t, output, tc.userID)
		})
	}
}

// TestCLIUpdateHandler_OnStreaming_Matrix covers all 4 combinations of
// (agent|human) x (streaming|done) for the streaming display.
func TestCLIUpdateHandler_OnStreaming_Matrix(t *testing.T) {
	tests := []struct {
		name       string
		userID     string
		isDone     bool
		isAgent    bool
		wantLabel  string // "[streaming]" or "[agent]"
		wantStatus string
	}{
		{"agent streaming", "agent/bot1", false, true, "[agent]", "streaming"},
		{"agent done", "agent/bot1", true, true, "[agent]", "done"},
		{"human streaming", "alice", false, false, "[streaming]", "streaming"},
		{"human done", "alice", true, false, "[streaming]", "done"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newCLIUpdateHandler(nil)
			output := captureStdout(func() {
				require.NoError(t, h.OnStreaming(context.Background(), tc.userID, "conv-1", "stream-1", "hello", tc.isDone, tc.isAgent))
			})
			assert.Contains(t, output, tc.wantLabel)
			assert.Contains(t, output, tc.wantStatus)
			assert.Contains(t, output, tc.userID)
		})
	}
}

// ---------------------------------------------------------------------------
// HITL handler output tests (D-087)
// ---------------------------------------------------------------------------

// TestCLIUpdateHandler_OnAgentStatus verifies the output format of the
// OnAgentStatus handler.
func TestCLIUpdateHandler_OnAgentStatus(t *testing.T) {
	tests := []struct {
		name       string
		userID     string
		convID     string
		status     string
		wantFields []string
	}{
		{
			name:   "asking_user",
			userID: "agent/bot1", convID: "conv-1", status: "asking_user",
			wantFields: []string{"[agent_status]", "agent=agent/bot1", "conv=conv-1", "status=asking_user"},
		},
		{
			name:   "running",
			userID: "agent/bot2", convID: "conv-2", status: "running",
			wantFields: []string{"[agent_status]", "agent=agent/bot2", "status=running"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newCLIUpdateHandler(nil)
			output := captureStdout(func() {
				require.NoError(t, h.OnAgentStatus(context.Background(), tc.userID, tc.convID, tc.status))
			})
			for _, field := range tc.wantFields {
				assert.Contains(t, output, field)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HITL display via OnConversation (D-125)
// ---------------------------------------------------------------------------

// TestCLIUpdateHandler_OnConversation_HITL verifies that when a conversation
// has agent_status="asking_user" and pending questions in the local DB,
// OnConversation outputs HITL information including checkpoint_id,
// interrupt_id, question text, and status (D-125).
func TestCLIUpdateHandler_OnConversation_HITL(t *testing.T) {
	// Create a ClientDB with a pending question.
	db, err := store.NewInMemory("cli-hitl-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	convID := "conv-hitl-cli"
	ctx := context.Background()

	// Insert a pending question into the question store.
	err = db.Questions.Upsert(ctx, &model.Question{
		ID:             "q-hitl-1",
		ConversationID: convID,
		CheckpointID:   "cp-42",
		InterruptID:    "int-99",
		QuestionText:   "Are you sure?",
		Status:         "pending",
	})
	require.NoError(t, err)

	// Create a handler wired to the ClientDB.
	h := newCLIUpdateHandler(db)

	conv := &model.Conversation{
		ID:           convID,
		Title:        "Test HITL",
		AgentID:      "agent/bot1",
		AgentStatus:  model.AgentStatusAskingUser,
		CheckpointID: "cp-42",
	}

	output := captureStdout(func() {
		require.NoError(t, h.OnConversation(ctx, conv))
	})

	// Verify basic conversation info is present.
	assert.Contains(t, output, "[conversation]")
	assert.Contains(t, output, "id="+convID)

	// Verify HITL info is displayed (D-125).
	assert.Contains(t, output, "[hitl]")
	assert.Contains(t, output, "agent=agent/bot1")
	assert.Contains(t, output, "checkpoint_id=cp-42")
	assert.Contains(t, output, "interrupt_id=int-99")
	assert.Contains(t, output, "Are you sure?")
	assert.Contains(t, output, "pending")
}

// TestCLIUpdateHandler_OnConversation_HITL_NoQuestions verifies that when a
// conversation has agent_status="asking_user" but no questions in the DB,
// no HITL block is displayed (D-125).
func TestCLIUpdateHandler_OnConversation_HITL_NoQuestions(t *testing.T) {
	db, err := store.NewInMemory("cli-hitl-empty-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	h := newCLIUpdateHandler(db)

	conv := &model.Conversation{
		ID:          "conv-no-q",
		Title:       "No Questions",
		AgentID:     "agent/bot2",
		AgentStatus: model.AgentStatusAskingUser,
	}

	output := captureStdout(func() {
		require.NoError(t, h.OnConversation(context.Background(), conv))
	})

	// Should have basic conversation info.
	assert.Contains(t, output, "[conversation]")
	// Should NOT have HITL block since no questions exist.
	assert.NotContains(t, output, "[hitl]")
}

// TestCLIUpdateHandler_OnConversation_HITL_NonAskingUser verifies that when
// agent_status is not "asking_user", no HITL block is displayed even if
// questions exist in the DB (D-125).
func TestCLIUpdateHandler_OnConversation_HITL_NonAskingUser(t *testing.T) {
	db, err := store.NewInMemory("cli-hitl-idle-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	// Insert a question even though status is idle.
	require.NoError(t, db.Questions.Upsert(ctx, &model.Question{
		ID:             "q-idle",
		ConversationID: "conv-idle",
		CheckpointID:   "cp-1",
		QuestionText:   "Should not appear",
		Status:         "pending",
	}))

	h := newCLIUpdateHandler(db)

	conv := &model.Conversation{
		ID:          "conv-idle",
		Title:       "Idle",
		AgentID:     "agent/bot3",
		AgentStatus: model.AgentStatusIdle, // not asking_user
	}

	output := captureStdout(func() {
		require.NoError(t, h.OnConversation(ctx, conv))
	})

	assert.Contains(t, output, "[conversation]")
	assert.NotContains(t, output, "[hitl]")
}

// TestCLIUpdateHandler_OnAgentTimeout verifies the output format of the
// OnAgentTimeout handler.
func TestCLIUpdateHandler_OnAgentTimeout(t *testing.T) {
	h := newCLIUpdateHandler(nil)

	output := captureStdout(func() {
		err := h.OnAgentTimeout(context.Background(),
			"agent/weather-bot", "conv-123", "LLM timeout after 120s")
		if err != nil {
			t.Fatalf("OnAgentTimeout() error: %v", err)
		}
	})

	assert.Contains(t, output, "[agent_timeout]")
	assert.Contains(t, output, "agent=agent/weather-bot")
	assert.Contains(t, output, "conv=conv-123")
	assert.Contains(t, output, "LLM timeout after 120s")
}

// ---------------------------------------------------------------------------
// Restore conversation fallback tests (D-015)
// ---------------------------------------------------------------------------

// setupTestIPCWithMock creates a mock WebSocket server with custom response
// handlers and returns the IPC socket path, ClientDB, and cleanup function.
// The responseHandler allows tests to control mock server responses per method.
func setupTestIPCWithMock(t *testing.T, responseHandler func(req protocol.PackageDataRequest) json.RawMessage) (sockPath string, db *store.ClientDB, cleanup func()) {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var pkg protocol.Package
			if err := conn.ReadJSON(&pkg); err != nil {
				return
			}
			var req protocol.PackageDataRequest
			_ = json.Unmarshal(pkg.Data, &req)

			var respData []byte
			if responseHandler != nil {
				data := responseHandler(req)
				respData, _ = json.Marshal(protocol.PackageDataResponse{ID: req.ID, Code: protocol.ResponseCodeOK, Data: data})
			} else {
				respData, _ = json.Marshal(protocol.PackageDataResponse{ID: req.ID, Code: protocol.ResponseCodeOK, Data: json.RawMessage(`{}`)})
			}

			respPkg := protocol.Package{Version: 1, Type: protocol.PackageTypeResponse, Data: json.RawMessage(respData)}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = conn.WriteJSON(respPkg)
		}
	}))

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	tmpDir, err := os.MkdirTemp("/tmp", "xyncra-ipc-fallback-*")
	if err != nil {
		ts.Close()
		t.Fatalf("MkdirTemp: %v", err)
	}

	db, err = store.New(tmpDir + "/test.db")
	if err != nil {
		ts.Close()
		t.Fatalf("open db: %v", err)
	}

	xc, err := client.New(
		client.WithServerURL(wsURL),
		client.WithUserID("testuser"),
		client.WithDB(db),
	)
	if err != nil {
		_ = db.Close()
		ts.Close()
		t.Fatalf("create client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = xc.Start(ctx) }()
	time.Sleep(500 * time.Millisecond)

	// Set up IPC server.
	sockPath = tmpDir + "/xyncra.sock"
	ipcServer := NewIPCServer(sockPath)
	registerIPCHandlers(ipcServer, xc, db, "testuser")
	go func() { _ = ipcServer.Start(context.Background()) }()
	time.Sleep(200 * time.Millisecond)

	cleanup = func() {
		cancel()
		xc.Stop()
		_ = ipcServer.Stop()
		_ = db.Close()
		ts.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return sockPath, db, cleanup
}

// TestRestoreConversation_FallbackToRPC verifies that when the local DB returns
// ErrNotFound during restore_conversation, the IPC handler falls back to fetching
// the conversation from the server via RPC and upserts it to the local DB.
func TestRestoreConversation_FallbackToRPC(t *testing.T) {
	// Custom mock server: restore_conversation returns empty (OK),
	// get_conversation returns a conversation fetched from "server".
	responseHandler := func(req protocol.PackageDataRequest) json.RawMessage {
		switch req.Method {
		case "restore_conversation":
			data, _ := json.Marshal(client.RestoreConversationResult{
				Conversation:         &model.Conversation{ID: "conv-fallback", UserID1: "other", UserID2: "testuser", Type: "1-on-1", Title: "Fallback Conv"},
				RestoredMessageCount: 3,
			})
			return data
		case "get_conversation":
			data, _ := json.Marshal(client.GetConversationResult{
				Conversation: &model.Conversation{ID: "conv-fallback", UserID1: "other", UserID2: "testuser", Type: "1-on-1", Title: "Fallback Conv"},
				UnreadCount:  2,
			})
			return data
		default:
			return json.RawMessage(`{}`)
		}
	}

	sockPath, db, cleanup := setupTestIPCWithMock(t, responseHandler)
	defer cleanup()

	ctx := context.Background()

	// Do NOT seed the conversation in local DB — this triggers the ErrNotFound path.
	// Verify the conversation does not exist.
	_, err := db.Conversations.Get(ctx, "conv-fallback")
	require.ErrorIs(t, err, store.ErrNotFound, "conversation should not exist before test")

	// Call restore_conversation via IPC.
	ipcClient := NewIPCClient(sockPath, 5*time.Second)
	resp, err := ipcClient.Call(context.Background(), "restore_conversation", map[string]any{
		"conversation_id": "conv-fallback",
	})
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	// After fallback, the conversation should be upserted into local DB.
	gotConv, err := db.Conversations.Get(ctx, "conv-fallback")
	require.NoError(t, err, "conversation should exist in local DB after fallback upsert")
	assert.Equal(t, "Fallback Conv", gotConv.Title)
	assert.Equal(t, "other", gotConv.UserID1)
	assert.Equal(t, "testuser", gotConv.UserID2)
}
