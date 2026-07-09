package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

func TestPrintSendResult(t *testing.T) {
	result := &client.SendMessageResult{
		Message: &model.Message{
			ID:              "msg-uuid-1",
			MessageID:       42,
			ConversationID:  "conv-1",
			ClientMessageID: "client-msg-1",
		},
		Duplicate: false,
	}

	output := captureStdout(func() {
		printSendResult(result)
	})

	if !strings.Contains(output, "Message sent.") {
		t.Errorf("output should contain 'Message sent.', got %q", output)
	}
	if !strings.Contains(output, "Message ID: 42") {
		t.Errorf("output should contain 'Message ID: 42', got %q", output)
	}
	if !strings.Contains(output, "UUID: msg-uuid-1") {
		t.Errorf("output should contain 'UUID: msg-uuid-1', got %q", output)
	}
	if !strings.Contains(output, "Conversation: conv-1") {
		t.Errorf("output should contain 'Conversation: conv-1', got %q", output)
	}
	if !strings.Contains(output, "Client Msg ID: client-msg-1") {
		t.Errorf("output should contain 'Client Msg ID: client-msg-1', got %q", output)
	}
	if !strings.Contains(output, "Duplicate: false") {
		t.Errorf("output should contain 'Duplicate: false', got %q", output)
	}
}

func TestPrintSendResult_NilMessage(t *testing.T) {
	result := &client.SendMessageResult{
		Message:   nil,
		Duplicate: true,
	}

	output := captureStdout(func() {
		printSendResult(result)
	})

	if !strings.Contains(output, "Message sent.") {
		t.Errorf("output should contain 'Message sent.', got %q", output)
	}
	if !strings.Contains(output, "Duplicate: true") {
		t.Errorf("output should contain 'Duplicate: true', got %q", output)
	}
	// Should not crash with nil message.
	if strings.Contains(output, "Message ID:") {
		t.Errorf("output should not contain 'Message ID:' when message is nil, got %q", output)
	}
}

func TestSendViaIPC_Success(t *testing.T) {
	// Start a real IPCServer with a mock send_message handler.
	sockPath := t.TempDir() + "/test.sock"
	srv := NewIPCServer(sockPath)

	srv.Register("send_message", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
			Content        string `json:"content"`
			ReplyTo        uint32 `json:"reply_to"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, err.Error()), nil
		}

		result := client.SendMessageResult{
			Message: &model.Message{
				MessageID:       100,
				ConversationID:  params.ConversationID,
				ClientMessageID: "mock-client-msg-id",
				Content:         params.Content,
			},
			Duplicate: false,
		}
		return NewIPCResponse(req.ID, result)
	})

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("IPCServer.Start() error: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	cliCtx := &CLIContext{
		UserID:   "testuser",
		DeviceID: "testdevice",
		UserDir:  t.TempDir(),
	}
	// Point SocketPath to our test socket.
	// CLIContext.SocketPath() returns filepath.Join(c.UserDir, "xyncra.sock"),
	// so we need UserDir to be the directory containing the socket.
	// Actually, let's just set UserDir to the temp dir and use the sockPath directly.
	// We need to override SocketPath() output. Since we can't, let's create the socket
	// in UserDir.
	_ = sockPath // not used directly
	cliCtx.UserDir = t.TempDir()

	// Start a second server at the path CLIContext will use.
	cliSockPath := cliCtx.SocketPath()
	srv2 := NewIPCServer(cliSockPath)
	srv2.Register("send_message", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		result := client.SendMessageResult{
			Message: &model.Message{
				MessageID:       200,
				ConversationID:  "conv-1",
				ClientMessageID: "mock-id",
				Content:         "hello",
			},
			Duplicate: false,
		}
		return NewIPCResponse(req.ID, result)
	})
	if err := srv2.Start(context.Background()); err != nil {
		t.Fatalf("IPCServer.Start() error: %v", err)
	}
	defer func() { _ = srv2.Stop() }()

	res, err := sendViaIPC(context.Background(), cliCtx, "conv-1", "hello", 0)
	if err != nil {
		t.Fatalf("sendViaIPC() error: %v", err)
	}
	if res.Message == nil {
		t.Fatal("result.Message should not be nil")
	}
	if res.Message.MessageID != 200 {
		t.Errorf("MessageID = %d, want 200", res.Message.MessageID)
	}
	if res.Message.ConversationID != "conv-1" {
		t.Errorf("ConversationID = %q, want %q", res.Message.ConversationID, "conv-1")
	}
}

func TestSendViaIPC_NoDaemon(t *testing.T) {
	cliCtx := &CLIContext{
		UserDir: t.TempDir(), // No IPC server running here.
	}

	_, err := sendViaIPC(context.Background(), cliCtx, "conv-1", "hello", 0)
	if err == nil {
		t.Fatal("sendViaIPC() should fail when no daemon is running")
	}
}

func TestSendViaIPC_HandlerError(t *testing.T) {
	cliCtx := &CLIContext{
		UserDir: t.TempDir(),
	}
	cliSockPath := cliCtx.SocketPath()

	srv := NewIPCServer(cliSockPath)
	srv.Register("send_message", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		return NewIPCErrorResponse(req.ID, -100, "validation failed"), nil
	})
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("IPCServer.Start() error: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	_, err := sendViaIPC(context.Background(), cliCtx, "conv-1", "hello", 0)
	if err == nil {
		t.Fatal("sendViaIPC() should fail when handler returns error")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error = %q, want it to contain 'validation failed'", err.Error())
	}
}

func TestSendStandalone_Success(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// Start a mock WebSocket server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Read the request package.
		var pkg protocol.Package
		if err := conn.ReadJSON(&pkg); err != nil {
			t.Logf("read error: %v", err)
			return
		}

		// Build a successful response.
		result := client.SendMessageResult{
			Message: &model.Message{
				MessageID:       300,
				ConversationID:  "conv-1",
				ClientMessageID: "standalone-msg-id",
				Content:         "hello",
			},
			Duplicate: false,
		}
		resultData, _ := json.Marshal(result)

		respData, _ := json.Marshal(protocol.PackageDataResponse{
			ID:   "1",
			Code: protocol.ResponseCodeOK,
			Msg:  "ok",
			Data: resultData,
		})

		respPkg := protocol.Package{
			Version: 1,
			Type:    protocol.PackageTypeResponse,
			Data:    json.RawMessage(respData),
		}

		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(respPkg); err != nil {
			t.Logf("write error: %v", err)
		}
	}))
	defer ts.Close()

	// Convert http:// URL to ws:// URL.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	cliCtx := &CLIContext{
		UserID:    "testuser",
		DeviceID:  "testdevice",
		ServerURL: wsURL,
		UserDir:   t.TempDir(),
	}

	res, err := sendStandalone(context.Background(), cliCtx, "conv-1", "hello", 0)
	if err != nil {
		t.Fatalf("sendStandalone() error: %v", err)
	}
	if res.Message == nil {
		t.Fatal("result.Message should not be nil")
	}
	if res.Message.MessageID != 300 {
		t.Errorf("MessageID = %d, want 300", res.Message.MessageID)
	}
}

func TestSendStandalone_ServerError(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		var pkg protocol.Package
		if err := conn.ReadJSON(&pkg); err != nil {
			return
		}

		respData, _ := json.Marshal(protocol.PackageDataResponse{
			ID:   "1",
			Code: protocol.ResponseCodeValidationError,
			Msg:  "invalid content",
		})

		respPkg := protocol.Package{
			Version: 1,
			Type:    protocol.PackageTypeResponse,
			Data:    json.RawMessage(respData),
		}

		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(respPkg); err != nil {
			return
		}
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	cliCtx := &CLIContext{
		UserID:    "testuser",
		ServerURL: wsURL,
		UserDir:   t.TempDir(),
	}

	_, err := sendStandalone(context.Background(), cliCtx, "conv-1", "hello", 0)
	if err == nil {
		t.Fatal("sendStandalone() should fail on server error response")
	}
}

func TestSendStandalone_NoServer(t *testing.T) {
	cliCtx := &CLIContext{
		UserID:    "testuser",
		ServerURL: "ws://127.0.0.1:1", // No server here.
		UserDir:   t.TempDir(),
	}

	_, err := sendStandalone(context.Background(), cliCtx, "conv-1", "hello", 0)
	if err == nil {
		t.Fatal("sendStandalone() should fail when server is unreachable")
	}
}

func TestNewSendCommand_RequiredFlags(t *testing.T) {
	cmd := newSendCommand()

	// Verify required flags are present.
	convFlag := cmd.Flags().Lookup("conversation-id")
	if convFlag == nil {
		t.Fatal("missing --conversation-id flag")
	}

	contentFlag := cmd.Flags().Lookup("content")
	if contentFlag == nil {
		t.Fatal("missing --content flag")
	}

	replyToFlag := cmd.Flags().Lookup("reply-to")
	if replyToFlag == nil {
		t.Fatal("missing --reply-to flag")
	}
}
