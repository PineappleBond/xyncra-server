package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestCLIContext returns a CLIContext backed by a temp directory.
// Uses a short path to avoid Unix socket path length limits on macOS (104 bytes).
func newTestCLIContext(t *testing.T) *CLIContext {
	t.Helper()
	tmpDir, err := os.MkdirTemp("/tmp", "xyncra-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	return &CLIContext{
		UserID:   "testuser",
		DeviceID: "testdevice",
		UserDir:  tmpDir,
		DBPath:   filepath.Join(tmpDir, "xyncra.db"),
	}
}

// seedConversation inserts a conversation directly into the store.
func seedConversation(t *testing.T, db *store.ClientDB, conv *model.Conversation) {
	t.Helper()
	if err := db.Conversations.Create(context.Background(), conv); err != nil {
		t.Fatalf("seedConversation: %v", err)
	}
}

// seedMessage inserts a message directly into the store.
func seedMessage(t *testing.T, db *store.ClientDB, msg *model.Message) {
	t.Helper()
	if err := db.Messages.Create(context.Background(), msg); err != nil {
		t.Fatalf("seedMessage: %v", err)
	}
}

// seedDraft inserts a draft directly into the store.
func seedDraft(t *testing.T, db *store.ClientDB, draft *model.Draft) {
	t.Helper()
	if err := db.Drafts.Save(context.Background(), draft); err != nil {
		t.Fatalf("seedDraft: %v", err)
	}
}

// openTestDB opens a SQLite DB at cliCtx.DBPath for seeding/querying.
func openTestDB(t *testing.T, cliCtx *CLIContext) *store.ClientDB {
	t.Helper()
	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// startIPCServer starts a mock IPC server at the given socket path.
func startIPCServer(t *testing.T, sockPath string, handlers map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error)) {
	t.Helper()
	srv := NewIPCServer(sockPath)
	for method, handler := range handlers {
		srv.Register(method, handler)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("IPC server start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
}

// ---------------------------------------------------------------------------
// createConversationViaIPC
// ---------------------------------------------------------------------------

func TestCreateConversationViaIPC_Success(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"create_conversation": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			result := client.CreateConversationResult{
				Conversation: &model.Conversation{ID: "conv-new", UserID2: "peer1", Title: "Chat"},
				Duplicate:    false,
			}
			return NewIPCResponse(req.ID, result)
		},
	})

	result, err := createConversationViaIPC(context.Background(), cliCtx, "peer1", "Chat")
	if err != nil {
		t.Fatalf("createConversationViaIPC() error: %v", err)
	}
	if result.Conversation.ID != "conv-new" {
		t.Errorf("ID = %q, want %q", result.Conversation.ID, "conv-new")
	}
	if result.Duplicate {
		t.Error("expected Duplicate=false")
	}
}

func TestCreateConversationViaIPC_Duplicate(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"create_conversation": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			result := client.CreateConversationResult{
				Conversation: &model.Conversation{ID: "conv-existing"},
				Duplicate:    true,
			}
			return NewIPCResponse(req.ID, result)
		},
	})

	result, err := createConversationViaIPC(context.Background(), cliCtx, "peer1", "")
	if err != nil {
		t.Fatalf("createConversationViaIPC() error: %v", err)
	}
	if !result.Duplicate {
		t.Error("expected Duplicate=true")
	}
}

func TestCreateConversationViaIPC_NoDaemon(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	_, err := createConversationViaIPC(context.Background(), cliCtx, "peer1", "Chat")
	if err == nil {
		t.Fatal("expected error when no daemon is running")
	}
}

// ---------------------------------------------------------------------------
// createConversationStandalone
// ---------------------------------------------------------------------------

func TestCreateConversationStandalone_Success(t *testing.T) {
	ts := startMockWSServer(t, func(t *testing.T, pkg protocol.Package) (protocol.Package, bool) {
		respData, _ := json.Marshal(protocol.PackageDataResponse{
			ID:   "1",
			Code: protocol.ResponseCodeOK,
			Data: json.RawMessage(`{"conversation":{"id":"conv-standalone","user_id2":"peer1","title":"Chat"},"duplicate":false}`),
		})
		return protocol.Package{Version: 1, Type: protocol.PackageTypeResponse, Data: json.RawMessage(respData)}, true
	})

	cliCtx := newTestCLIContext(t)
	cliCtx.ServerURL = wsURL(ts)

	result, err := createConversationStandalone(context.Background(), cliCtx, "peer1", "Chat")
	if err != nil {
		t.Fatalf("createConversationStandalone() error: %v", err)
	}
	if result.Conversation.ID != "conv-standalone" {
		t.Errorf("ID = %q, want %q", result.Conversation.ID, "conv-standalone")
	}
}

// ---------------------------------------------------------------------------
// deleteConversationViaIPC / standalone
// ---------------------------------------------------------------------------

func TestDeleteConversationViaIPC_Success(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"delete_conversation": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			return NewIPCResponse(req.ID, nil)
		},
	})

	err := deleteConversationViaIPC(context.Background(), cliCtx, "conv-1")
	if err != nil {
		t.Fatalf("deleteConversationViaIPC() error: %v", err)
	}
}

func TestDeleteConversationStandalone_Success(t *testing.T) {
	ts := startMockWSServer(t, func(t *testing.T, pkg protocol.Package) (protocol.Package, bool) {
		respData, _ := json.Marshal(protocol.PackageDataResponse{
			ID:   "1",
			Code: protocol.ResponseCodeOK,
			Data: json.RawMessage(`{}`),
		})
		return protocol.Package{Version: 1, Type: protocol.PackageTypeResponse, Data: json.RawMessage(respData)}, true
	})

	cliCtx := newTestCLIContext(t)
	cliCtx.ServerURL = wsURL(ts)

	err := deleteConversationStandalone(context.Background(), cliCtx, "conv-1")
	if err != nil {
		t.Fatalf("deleteConversationStandalone() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// restoreConversationViaIPC / standalone
// ---------------------------------------------------------------------------

func TestRestoreConversationViaIPC_Success(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"restore_conversation": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			return NewIPCResponse(req.ID, nil)
		},
	})

	err := restoreConversationViaIPC(context.Background(), cliCtx, "conv-1")
	if err != nil {
		t.Fatalf("restoreConversationViaIPC() error: %v", err)
	}
}

func TestRestoreConversationStandalone_Success(t *testing.T) {
	ts := startMockWSServer(t, func(t *testing.T, pkg protocol.Package) (protocol.Package, bool) {
		respData, _ := json.Marshal(protocol.PackageDataResponse{
			ID:   "1",
			Code: protocol.ResponseCodeOK,
			Data: json.RawMessage(`{}`),
		})
		return protocol.Package{Version: 1, Type: protocol.PackageTypeResponse, Data: json.RawMessage(respData)}, true
	})

	cliCtx := newTestCLIContext(t)
	cliCtx.ServerURL = wsURL(ts)

	err := restoreConversationStandalone(context.Background(), cliCtx, "conv-1")
	if err != nil {
		t.Fatalf("restoreConversationStandalone() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// printConversationList
// ---------------------------------------------------------------------------

func TestPrintConversationList_Empty(t *testing.T) {
	output := captureStdout(func() {
		printConversationList(nil, "testuser", false)
	})
	if !strings.Contains(output, "No conversations found") {
		t.Errorf("output = %q, want 'No conversations found'", output)
	}
}

func TestPrintConversationList_WithData(t *testing.T) {
	convs := []*model.Conversation{
		{
			ID:            "conv-1",
			UserID1:       "testuser",
			UserID2:       "peer1",
			Title:         "Chat with peer",
			LastMessageAt: time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
	}
	output := captureStdout(func() {
		printConversationList(convs, "testuser", false)
	})
	if !strings.Contains(output, "conv-1") {
		t.Errorf("output = %q, want to contain conv-1", output)
	}
	if !strings.Contains(output, "peer1") {
		t.Errorf("output = %q, want to contain peer1", output)
	}
	if !strings.Contains(output, "Chat with peer") {
		t.Errorf("output = %q, want to contain title", output)
	}
}

func TestPrintConversationList_HasMore(t *testing.T) {
	convs := []*model.Conversation{
		{ID: "conv-1", UserID1: "testuser", UserID2: "peer1", LastMessageAt: time.Now()},
	}
	output := captureStdout(func() {
		printConversationList(convs, "testuser", true)
	})
	if !strings.Contains(output, "more conversations available") {
		t.Errorf("output = %q, want to contain pagination hint", output)
	}
}

// TestPrintConversationList_ReversePeer verifies that when the current user is
// UserID2, the Peer column correctly displays UserID1 instead of UserID2.
func TestPrintConversationList_ReversePeer(t *testing.T) {
	convs := []*model.Conversation{
		{
			ID:            "conv-reverse",
			UserID1:       "other-user",
			UserID2:       "testuser",
			Title:         "Reverse",
			LastMessageAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		},
	}
	output := captureStdout(func() {
		printConversationList(convs, "testuser", false)
	})
	if !strings.Contains(output, "other-user") {
		t.Errorf("peer column should show UserID1 when current user is UserID2, got %q", output)
	}
	// Make sure we don't show the current user as their own peer
	// (the header row contains "Peer" so we check for the data row specifically)
}

// TestPrintConversationList_ZeroLastMessageAt verifies that a zero-value
// LastMessageAt is displayed as "-" rather than the raw "0001-01-01" timestamp.
func TestPrintConversationList_ZeroLastMessageAt(t *testing.T) {
	convs := []*model.Conversation{
		{
			ID:      "conv-zero",
			UserID1: "testuser",
			UserID2: "peer1",
			// LastMessageAt is zero value
		},
	}
	output := captureStdout(func() {
		printConversationList(convs, "testuser", false)
	})
	if strings.Contains(output, "0001") {
		t.Errorf("zero LastMessageAt should not show year 0001, got %q", output)
	}
	if !strings.Contains(output, "-") {
		t.Errorf("zero LastMessageAt should show '-', got %q", output)
	}
}

// ---------------------------------------------------------------------------
// printConversationDetail
// ---------------------------------------------------------------------------

func TestPrintConversationDetail(t *testing.T) {
	conv := &model.Conversation{
		ID:            "conv-detail",
		UserID1:       "testuser",
		UserID2:       "peer1",
		Type:          "1-on-1",
		Title:         "My Chat",
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastMessageAt: time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
	}
	output := captureStdout(func() {
		printConversationDetail(conv, "testuser", 5)
	})
	if !strings.Contains(output, "conv-detail") {
		t.Errorf("output = %q, want to contain conv ID", output)
	}
	if !strings.Contains(output, "1-on-1") {
		t.Errorf("output = %q, want to contain type", output)
	}
	if !strings.Contains(output, "My Chat") {
		t.Errorf("output = %q, want to contain title", output)
	}
	if !strings.Contains(output, "Unread:       5") {
		t.Errorf("output = %q, want to contain unread count", output)
	}
}

// ---------------------------------------------------------------------------
// list-conversations (local DB, D-035)
// ---------------------------------------------------------------------------

func TestRunListConversations_EmptyDB(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)
	_ = db

	// No conversations seeded. Use the printConversationList directly since
	// runListConversations requires cobra command setup. We verify the DB path.
	convs, err := db.Conversations.GetByUser(context.Background(), "testuser", 0, 21)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	if len(convs) != 0 {
		t.Errorf("expected 0 conversations, got %d", len(convs))
	}
}

func TestRunListConversations_WithData(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	seedConversation(t, db, &model.Conversation{
		ID:            "conv-1",
		UserID1:       "testuser",
		UserID2:       "peer1",
		LastMessageAt: time.Now(),
	})
	seedConversation(t, db, &model.Conversation{
		ID:            "conv-2",
		UserID1:       "testuser",
		UserID2:       "peer2",
		LastMessageAt: time.Now(),
	})

	convs, err := db.Conversations.GetByUser(context.Background(), "testuser", 0, 21)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	if len(convs) != 2 {
		t.Errorf("expected 2 conversations, got %d", len(convs))
	}
}

func TestRunListConversations_HasMore(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	// Seed 3 conversations, query with limit=2.
	for i := 0; i < 3; i++ {
		seedConversation(t, db, &model.Conversation{
			ID:            "conv-" + string(rune('a'+i)),
			UserID1:       "testuser",
			UserID2:       "peer" + string(rune('1'+i)),
			LastMessageAt: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	// Fetch limit+1 to detect hasMore (matching runListConversations logic).
	limit := 2
	convs, err := db.Conversations.GetByUser(context.Background(), "testuser", 0, limit+1)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	hasMore := len(convs) > limit
	if !hasMore {
		t.Error("expected hasMore=true")
	}
}

// ---------------------------------------------------------------------------
// get-conversation (local DB, D-035)
// ---------------------------------------------------------------------------

func TestGetConversation_Found(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	seedConversation(t, db, &model.Conversation{
		ID:                     "conv-get",
		UserID1:                "testuser",
		UserID2:                "peer1",
		Type:                   "1-on-1",
		Title:                  "Test Chat",
		LastReadMessageID1:     10,
		LastProcessedMessageID: 15,
	})
	// Seed messages: 5 unread (MessageID 11..15).
	for i := uint32(11); i <= 15; i++ {
		seedMessage(t, db, &model.Message{
			ID:              fmt.Sprintf("msg-get-%d", i),
			ClientMessageID: fmt.Sprintf("cid-get-%d", i),
			ConversationID:  "conv-get",
			MessageID:       i,
			SenderID:        "peer1",
			Content:         "hello",
			CreatedAt:       time.Now(),
		})
	}

	conv, err := db.Conversations.Get(context.Background(), "conv-get")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	unreadCount, err := db.Messages.CountUnread(context.Background(), "conv-get", conv.LastReadMessageID1)
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	if unreadCount != 5 {
		t.Errorf("unreadCount = %d, want 5", unreadCount)
	}
}

func TestGetConversation_NotFound(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	_, err := db.Conversations.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent conversation")
	}
	if !strings.Contains(err.Error(), "not found") {
		// Check it's ErrNotFound.
		if err != store.ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Command flag validation
// ---------------------------------------------------------------------------

func TestNewCreateConversationCommand_RequiredFlags(t *testing.T) {
	cmd := newCreateConversationCommand()
	if cmd.Flags().Lookup("peer-id") == nil {
		t.Error("missing --peer-id flag")
	}
	if cmd.Flags().Lookup("title") == nil {
		t.Error("missing --title flag")
	}
}

func TestNewDeleteConversationCommand_RequiredFlags(t *testing.T) {
	cmd := newDeleteConversationCommand()
	if cmd.Flags().Lookup("conversation-id") == nil {
		t.Error("missing --conversation-id flag")
	}
}

func TestNewRestoreConversationCommand_RequiredFlags(t *testing.T) {
	cmd := newRestoreConversationCommand()
	if cmd.Flags().Lookup("conversation-id") == nil {
		t.Error("missing --conversation-id flag")
	}
}

func TestNewListConversationsCommand_Flags(t *testing.T) {
	cmd := newListConversationsCommand()
	if cmd.Flags().Lookup("offset") == nil {
		t.Error("missing --offset flag")
	}
	if cmd.Flags().Lookup("limit") == nil {
		t.Error("missing --limit flag")
	}
}

func TestNewGetConversationCommand_RequiredFlags(t *testing.T) {
	cmd := newGetConversationCommand()
	if cmd.Flags().Lookup("conversation-id") == nil {
		t.Error("missing --conversation-id flag")
	}
}
