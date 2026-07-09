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

// TestListConversations_CorrectParams verifies that ListConversations sends the
// correct method name and parameters.
func TestListConversations_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("list_conversations", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{"conversations":[],"has_more":false}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.ListConversations(context.Background(), 10, 25)
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if receivedReq.Method != "list_conversations" {
		t.Errorf("expected method list_conversations, got %s", receivedReq.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if offset, _ := params["offset"].(float64); int(offset) != 10 {
		t.Errorf("expected offset=10, got %v", params["offset"])
	}
	if limit, _ := params["limit"].(float64); int(limit) != 25 {
		t.Errorf("expected limit=25, got %v", params["limit"])
	}
}

// TestGetMessages_CorrectParams verifies that GetMessages sends the correct
// method name and parameters.
func TestGetMessages_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("get_messages", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{"messages":[],"has_more":false}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.GetMessages(context.Background(), "conv-42", 100, 50)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if receivedReq.Method != "get_messages" {
		t.Errorf("expected method get_messages, got %s", receivedReq.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["conversation_id"] != "conv-42" {
		t.Errorf("expected conversation_id=conv-42, got %v", params["conversation_id"])
	}
	if afterMsg, _ := params["after_msg_id"].(float64); uint32(afterMsg) != 100 {
		t.Errorf("expected after_msg_id=100, got %v", params["after_msg_id"])
	}
	if limit, _ := params["limit"].(float64); int(limit) != 50 {
		t.Errorf("expected limit=50, got %v", params["limit"])
	}
}

// TestSearchMessages_CorrectParams verifies that SearchMessages sends the
// correct method name and parameters.
func TestSearchMessages_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("search_messages", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{"messages":[],"has_more":false}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.SearchMessages(context.Background(), "conv-7", "hello world", 5, 20)
	if err != nil {
		t.Fatalf("SearchMessages failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if receivedReq.Method != "search_messages" {
		t.Errorf("expected method search_messages, got %s", receivedReq.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["conversation_id"] != "conv-7" {
		t.Errorf("expected conversation_id=conv-7, got %v", params["conversation_id"])
	}
	if params["query"] != "hello world" {
		t.Errorf("expected query='hello world', got %v", params["query"])
	}
	if afterMsg, _ := params["after_msg_id"].(float64); uint32(afterMsg) != 5 {
		t.Errorf("expected after_msg_id=5, got %v", params["after_msg_id"])
	}
	if limit, _ := params["limit"].(float64); int(limit) != 20 {
		t.Errorf("expected limit=20, got %v", params["limit"])
	}
}

// TestGetConversation_CorrectParams verifies that GetConversation sends the
// correct method name and parameters.
func TestGetConversation_CorrectParams(t *testing.T) {
	server := newMockWSServer(t)
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})

	var receivedReq protocol.PackageDataRequest
	server.SetRPCHandler("get_conversation", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		receivedReq = *req
		return json.RawMessage(`{"conversation":{"id":"conv-99"},"unread_count":5}`), nil
	})

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = c.Start(ctx) }()
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("server did not accept connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result, err := c.GetConversation(context.Background(), "conv-99")
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if receivedReq.Method != "get_conversation" {
		t.Errorf("expected method get_conversation, got %s", receivedReq.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(receivedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["conversation_id"] != "conv-99" {
		t.Errorf("expected conversation_id=conv-99, got %v", params["conversation_id"])
	}
}

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

	err := c.DeleteConversation(context.Background(), "conv-del")
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

	err := c.RestoreConversation(context.Background(), "conv-restore")
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

	var syncCalls int
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		syncCalls++
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

	callsBefore := syncCalls
	if err := c.FullSync(context.Background()); err != nil {
		t.Fatalf("FullSync() error: %v", err)
	}
	// FullSync should have made at least one additional sync_updates call.
	if syncCalls <= callsBefore {
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
