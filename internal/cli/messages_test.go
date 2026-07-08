package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// deleteMessageViaIPC / standalone
// ---------------------------------------------------------------------------

func TestDeleteMessageViaIPC_Success(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"delete_message": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			return NewIPCResponse(req.ID, nil)
		},
	})

	err := deleteMessageViaIPC(context.Background(), cliCtx, "msg-123")
	if err != nil {
		t.Fatalf("deleteMessageViaIPC() error: %v", err)
	}
}

func TestDeleteMessageViaIPC_HandlerError(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"delete_message": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			return NewIPCErrorResponse(req.ID, -100, "not allowed"), nil
		},
	})

	err := deleteMessageViaIPC(context.Background(), cliCtx, "msg-123")
	if err == nil {
		t.Fatal("expected error from IPC handler error")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error = %q, want it to contain 'not allowed'", err.Error())
	}
}

func TestDeleteMessageStandalone_Success(t *testing.T) {
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

	err := deleteMessageStandalone(context.Background(), cliCtx, "msg-123")
	if err != nil {
		t.Fatalf("deleteMessageStandalone() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// markAsReadViaIPC / standalone
// ---------------------------------------------------------------------------

func TestMarkAsReadViaIPC_Success(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"mark_as_read": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			return NewIPCResponse(req.ID, nil)
		},
	})

	err := markAsReadViaIPC(context.Background(), cliCtx, "conv-1", 42)
	if err != nil {
		t.Fatalf("markAsReadViaIPC() error: %v", err)
	}
}

func TestMarkAsReadStandalone_Success(t *testing.T) {
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

	err := markAsReadStandalone(context.Background(), cliCtx, "conv-1", 42)
	if err != nil {
		t.Fatalf("markAsReadStandalone() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// resolveLastProcessedMessageID
// ---------------------------------------------------------------------------

func TestResolveLastProcessedMessageID_Found(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	seedConversation(t, db, &model.Conversation{
		ID:                     "conv-resolve",
		UserID1:                "testuser",
		UserID2:                "peer1",
		LastProcessedMessageID: 99,
	})

	got, err := resolveLastProcessedMessageID(context.Background(), cliCtx, "conv-resolve")
	if err != nil {
		t.Fatalf("resolveLastProcessedMessageID() error: %v", err)
	}
	if got != 99 {
		t.Errorf("got = %d, want 99", got)
	}
}

func TestResolveLastProcessedMessageID_NotFound(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	_ = openTestDB(t, cliCtx)

	_, err := resolveLastProcessedMessageID(context.Background(), cliCtx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent conversation")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// printMessageList
// ---------------------------------------------------------------------------

func TestPrintMessageList_Empty(t *testing.T) {
	output := captureStdout(func() {
		printMessageList(nil, false)
	})
	if !strings.Contains(output, "No messages found") {
		t.Errorf("output = %q, want 'No messages found'", output)
	}
}

func TestPrintMessageList_WithData(t *testing.T) {
	msgs := []*model.Message{
		{
			MessageID: 42,
			SenderID:  "alice",
			Content:   "hello world",
			CreatedAt: time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
	}
	output := captureStdout(func() {
		printMessageList(msgs, false)
	})
	if !strings.Contains(output, "[#42]") {
		t.Errorf("output = %q, want [#42]", output)
	}
	if !strings.Contains(output, "alice") {
		t.Errorf("output = %q, want sender", output)
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("output = %q, want content", output)
	}
	if !strings.Contains(output, "10:30") {
		t.Errorf("output = %q, want time", output)
	}
}

func TestPrintMessageList_HasMore(t *testing.T) {
	msgs := []*model.Message{
		{MessageID: 1, SenderID: "alice", Content: "msg", CreatedAt: time.Now()},
	}
	output := captureStdout(func() {
		printMessageList(msgs, true)
	})
	if !strings.Contains(output, "more") {
		t.Errorf("output = %q, want pagination hint", output)
	}
}

// ---------------------------------------------------------------------------
// get-messages (local DB, D-035)
// ---------------------------------------------------------------------------

func TestGetMessages_EmptyDB(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	msgs, err := db.Messages.ListByConversation(context.Background(), "conv-1", 0, 51)
	if err != nil {
		t.Fatalf("ListByConversation: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestGetMessages_WithData(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	for i := uint32(1); i <= 5; i++ {
		seedMessage(t, db, &model.Message{
			ID:              fmt.Sprintf("msg-data-%d", i),
			ClientMessageID: fmt.Sprintf("cid-data-%d", i),
			ConversationID:  "conv-1",
			MessageID:       i,
			SenderID:        "alice",
			Content:         "message",
			CreatedAt:       time.Now(),
		})
	}

	msgs, err := db.Messages.ListByConversation(context.Background(), "conv-1", 0, 51)
	if err != nil {
		t.Fatalf("ListByConversation: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages, got %d", len(msgs))
	}
}

func TestGetMessages_HasMore(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	for i := uint32(1); i <= 5; i++ {
		seedMessage(t, db, &model.Message{
			ID:              fmt.Sprintf("msg-more-%d", i),
			ClientMessageID: fmt.Sprintf("cid-more-%d", i),
			ConversationID:  "conv-more",
			MessageID:       i,
			SenderID:        "bob",
			Content:         "hello",
			CreatedAt:       time.Now(),
		})
	}

	limit := 3
	msgs, err := db.Messages.ListByConversation(context.Background(), "conv-more", 0, limit+1)
	if err != nil {
		t.Fatalf("ListByConversation: %v", err)
	}
	hasMore := len(msgs) > limit
	if !hasMore {
		t.Error("expected hasMore=true")
	}
}

// ---------------------------------------------------------------------------
// search-messages (local DB, D-035)
// ---------------------------------------------------------------------------

func TestSearchMessages_Results(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	seedMessage(t, db, &model.Message{
		ID:              "msg-s1",
		ClientMessageID: "cid-s1",
		ConversationID:  "conv-search",
		MessageID:       1,
		SenderID:        "alice",
		Content:         "hello world",
		CreatedAt:       time.Now(),
	})
	seedMessage(t, db, &model.Message{
		ID:              "msg-s2",
		ClientMessageID: "cid-s2",
		ConversationID:  "conv-search",
		MessageID:       2,
		SenderID:        "bob",
		Content:         "goodbye",
		CreatedAt:       time.Now(),
	})

	msgs, err := db.Messages.SearchByConversation(context.Background(), "conv-search", "hello", 0, 51)
	if err != nil {
		t.Fatalf("SearchByConversation: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 result, got %d", len(msgs))
	}
	if len(msgs) > 0 && msgs[0].MessageID != 1 {
		t.Errorf("expected MessageID=1, got %d", msgs[0].MessageID)
	}
}

func TestSearchMessages_NoResults(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	msgs, err := db.Messages.SearchByConversation(context.Background(), "conv-search", "nonexistent", 0, 51)
	if err != nil {
		t.Fatalf("SearchByConversation: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 results, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Command flag validation
// ---------------------------------------------------------------------------

func TestNewDeleteMessageCommand_RequiredFlags(t *testing.T) {
	cmd := newDeleteMessageCommand()
	if cmd.Flags().Lookup("message-id") == nil {
		t.Error("missing --message-id flag")
	}
}

func TestNewMarkAsReadCommand_Flags(t *testing.T) {
	cmd := newMarkAsReadCommand()
	if cmd.Flags().Lookup("conversation-id") == nil {
		t.Error("missing --conversation-id flag")
	}
	if cmd.Flags().Lookup("message-id") == nil {
		t.Error("missing --message-id flag")
	}
}

func TestNewGetMessagesCommand_Flags(t *testing.T) {
	cmd := newGetMessagesCommand()
	if cmd.Flags().Lookup("conversation-id") == nil {
		t.Error("missing --conversation-id flag")
	}
	if cmd.Flags().Lookup("after-message-id") == nil {
		t.Error("missing --after-message-id flag")
	}
	if cmd.Flags().Lookup("limit") == nil {
		t.Error("missing --limit flag")
	}
}

func TestNewSearchMessagesCommand_Flags(t *testing.T) {
	cmd := newSearchMessagesCommand()
	if cmd.Flags().Lookup("conversation-id") == nil {
		t.Error("missing --conversation-id flag")
	}
	if cmd.Flags().Lookup("query") == nil {
		t.Error("missing --query flag")
	}
}

// Ensure store import is used.
var _ = store.ErrNotFound

// ---------------------------------------------------------------------------
// D-038: flag type validation
// ---------------------------------------------------------------------------

// TestDeleteMessage_MessageID_IsString verifies that the --message-id flag on
// the delete-message command is of type string (Message.ID UUID), not uint32.
func TestDeleteMessage_MessageID_IsString(t *testing.T) {
	cmd := newDeleteMessageCommand()
	f := cmd.Flags().Lookup("message-id")
	if f == nil {
		t.Fatal("missing --message-id flag")
	}
	if f.Value.Type() != "string" {
		t.Errorf("--message-id type = %q, want %q (D-038: delete-message uses Message.ID string UUID)", f.Value.Type(), "string")
	}
}

// TestMarkAsRead_MessageID_IsUint32 verifies that the --message-id flag on
// the mark-as-read command is of type uint32 (Message.MessageID sequence
// number), not string.
func TestMarkAsRead_MessageID_IsUint32(t *testing.T) {
	cmd := newMarkAsReadCommand()
	f := cmd.Flags().Lookup("message-id")
	if f == nil {
		t.Fatal("missing --message-id flag")
	}
	if f.Value.Type() != "uint32" {
		t.Errorf("--message-id type = %q, want %q (D-038: mark-as-read uses Message.MessageID uint32)", f.Value.Type(), "uint32")
	}
}
