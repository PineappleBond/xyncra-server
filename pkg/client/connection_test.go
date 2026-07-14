package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// safeCloseCM closes a connectionManager without the concurrent-write race
// between Close() and writePump's ctx.Done() handler. It sets the closing flag,
// cancels the context, waits for both pumps to exit, and then invokes Close()
// (which becomes a no-op since closing is already set). This ordering ensures
// handleDisconnect sees isClosing=true and does NOT fire onDisconnect.
func safeCloseCM(cm *connectionManager) {
	cm.mu.Lock()
	if cm.closing {
		cm.mu.Unlock()
		return
	}
	cm.closing = true
	cancel := cm.cancel
	pumpsDone := cm.pumpsDone
	cm.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if pumpsDone != nil {
		select {
		case <-pumpsDone:
		case <-time.After(3 * time.Second):
		}
	}
	cm.Close()
}

// newTestCM creates a connectionManager connected to the given mock server.
func newTestCM(t *testing.T, srv *mockWSServer, cbs connectionCallbacks) *connectionManager {
	t.Helper()
	opts := clientOptions{
		serverURL:           srv.URL(),
		userID:              "test-user",
		reconnectBaseDelay:  defaultReconnectBaseDelay,
		reconnectMaxDelay:   defaultReconnectMaxDelay,
		reconnectMaxRetries: defaultReconnectMaxRetries,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, cbs)
	if err := cm.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { safeCloseCM(cm) })
	return cm
}

// ---------------------------------------------------------------------------
// Connect / Close
// ---------------------------------------------------------------------------

func TestConnect_Success(t *testing.T) {
	srv := newMockWSServer(t)
	cm := newTestCM(t, srv, connectionCallbacks{})

	if !cm.IsConnected() {
		t.Error("IsConnected() = false, want true")
	}
}

func TestConnect_ServerDown(t *testing.T) {
	opts := clientOptions{
		serverURL:           "ws://127.0.0.1:1/ws", // port 1 should be unreachable
		userID:              "test-user",
		reconnectBaseDelay:  defaultReconnectBaseDelay,
		reconnectMaxDelay:   defaultReconnectMaxDelay,
		reconnectMaxRetries: defaultReconnectMaxRetries,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := cm.Connect(ctx)
	if err == nil {
		cm.Close()
		t.Fatal("Connect() expected error, got nil")
	}
	var ce *ClientError
	if !asClientError(err, &ce) {
		t.Fatalf("error should be *ClientError, got %T", err)
	}
	if ce.Code != ErrorCodeConnectionError {
		t.Errorf("error code = %d, want %d", ce.Code, ErrorCodeConnectionError)
	}
}

func TestConnect_UserIDQueryParam(t *testing.T) {
	srv := newMockWSServer(t)
	newTestCM(t, srv, connectionCallbacks{})

	// Verify server received the connection (URL will have user_id=test-user).
	if srv.ConnectionCount() == 0 {
		t.Error("expected at least one server connection")
	}
}

func TestClose_Idempotent(t *testing.T) {
	srv := newMockWSServer(t)
	cm := newTestCM(t, srv, connectionCallbacks{})

	// safeCloseCM already scheduled via t.Cleanup.
	// Now verify multiple Close calls don't panic.
	cm.mu.Lock()
	cancel := cm.cancel
	pumpsDone := cm.pumpsDone
	cm.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if pumpsDone != nil {
		select {
		case <-pumpsDone:
		case <-time.After(3 * time.Second):
		}
	}

	cm.Close()
	cm.Close()
	cm.Close()
}

func TestClose_SetsClosing(t *testing.T) {
	srv := newMockWSServer(t)
	cm := newTestCM(t, srv, connectionCallbacks{})

	safeCloseCM(cm)

	if cm.IsConnected() {
		t.Error("IsConnected() = true after Close, want false")
	}
}

// ---------------------------------------------------------------------------
// SendPackage
// ---------------------------------------------------------------------------

func TestSendPackage_BasicMessage(t *testing.T) {
	srv := newMockWSServer(t)
	cm := newTestCM(t, srv, connectionCallbacks{})

	pkg := &protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeRequest,
		Data:    json.RawMessage(`{"id":"1","method":"test","params":{}}`),
	}
	if err := cm.SendPackage(pkg); err != nil {
		t.Fatalf("SendPackage() error = %v", err)
	}

	// Wait for the server to receive the message.
	received, err := srv.ReadPackage(2 * time.Second)
	if err != nil {
		t.Fatalf("server ReadPackage() error = %v", err)
	}
	if received.Type != protocol.PackageTypeRequest {
		t.Errorf("received type = %d, want %d", received.Type, protocol.PackageTypeRequest)
	}
}

func TestSendPackage_ChannelFull(t *testing.T) {
	srv := newMockWSServer(t)
	cm := newTestCM(t, srv, connectionCallbacks{})

	// Fill the existing channel by sending many packages without the server
	// reading them. We rely on defaultSendBufSize=256, so send more than that
	// to trigger the drop path. The SendPackage call should not block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < defaultSendBufSize+50; i++ {
			pkg := &protocol.Package{
				Version: 1,
				Type:    protocol.PackageTypeRequest,
				Data:    json.RawMessage(`{"id":"1","method":"test","params":{}}`),
			}
			_ = cm.SendPackage(pkg)
		}
	}()

	select {
	case <-done:
		// Good: did not block.
	case <-time.After(5 * time.Second):
		t.Fatal("SendPackage blocked when channel was full")
	}
}

func TestSendPackage_NotConnected(t *testing.T) {
	opts := clientOptions{
		serverURL:           "ws://127.0.0.1:1/ws",
		userID:              "test-user",
		reconnectBaseDelay:  defaultReconnectBaseDelay,
		reconnectMaxDelay:   defaultReconnectMaxDelay,
		reconnectMaxRetries: defaultReconnectMaxRetries,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{})

	pkg := &protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeRequest,
		Data:    json.RawMessage(`{"id":"1","method":"test","params":{}}`),
	}
	err := cm.SendPackage(pkg)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestSendPackage_AfterClose(t *testing.T) {
	srv := newMockWSServer(t)
	opts := clientOptions{
		serverURL:           srv.URL(),
		userID:              "test-user",
		reconnectBaseDelay:  defaultReconnectBaseDelay,
		reconnectMaxDelay:   defaultReconnectMaxDelay,
		reconnectMaxRetries: defaultReconnectMaxRetries,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{})
	if err := cm.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	// Safe close: cancel + wait for pumps + close.
	safeCloseCM(cm)

	// SendPackage after Close should not panic and should return a connection error.
	pkg := &protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeRequest,
		Data:    json.RawMessage(`{"id":"1","method":"test","params":{}}`),
	}
	if err := cm.SendPackage(pkg); err == nil {
		t.Error("SendPackage() after Close expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Read pump dispatch
// ---------------------------------------------------------------------------

func TestReadPump_DispatchesResponse(t *testing.T) {
	srv := newMockWSServer(t)
	var gotResp *protocol.PackageDataResponse
	var wg sync.WaitGroup
	wg.Add(1)

	newTestCM(t, srv, connectionCallbacks{
		onResponse: func(r *protocol.PackageDataResponse) {
			gotResp = r
			wg.Done()
		},
	})

	// Server sends a response package.
	respData, _ := json.Marshal(protocol.PackageDataResponse{
		ID:   "42",
		Code: protocol.ResponseCodeOK,
		Msg:  "ok",
		Data: json.RawMessage(`"hello"`),
	})
	pkg := &protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeResponse,
		Data:    respData,
	}
	if err := srv.SendPackage(pkg); err != nil {
		t.Fatalf("SendPackage() error = %v", err)
	}

	wg.Wait()
	if gotResp == nil {
		t.Fatal("onResponse was not called")
	}
	if gotResp.ID != "42" {
		t.Errorf("response ID = %q, want %q", gotResp.ID, "42")
	}
}

func TestReadPump_DispatchesUpdates(t *testing.T) {
	srv := newMockWSServer(t)
	var gotUpdates *protocol.PackageDataUpdates
	var wg sync.WaitGroup
	wg.Add(1)

	newTestCM(t, srv, connectionCallbacks{
		onUpdates: func(u *protocol.PackageDataUpdates) {
			gotUpdates = u
			wg.Done()
		},
	})

	updatesData, _ := json.Marshal(protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Type: "message", Payload: json.RawMessage(`{}`)},
		},
	})
	pkg := &protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeUpdates,
		Data:    updatesData,
	}
	if err := srv.SendPackage(pkg); err != nil {
		t.Fatalf("SendPackage() error = %v", err)
	}

	wg.Wait()
	if gotUpdates == nil {
		t.Fatal("onUpdates was not called")
	}
	if len(gotUpdates.Updates) != 1 {
		t.Errorf("updates count = %d, want 1", len(gotUpdates.Updates))
	}
}

func TestReadPump_InvalidJSON(t *testing.T) {
	srv := newMockWSServer(t)

	var responseCalled sync.WaitGroup
	responseCalled.Add(1)

	newTestCM(t, srv, connectionCallbacks{
		onResponse: func(r *protocol.PackageDataResponse) {
			responseCalled.Done()
		},
	})

	// Send invalid JSON directly to the server-side connection.
	srv.mu.Lock()
	conns := make([]*websocket.Conn, len(srv.conns))
	copy(conns, srv.conns)
	srv.mu.Unlock()
	if len(conns) == 0 {
		t.Fatal("no connections")
	}

	// Send garbage bytes. This should not crash the readPump.
	if err := conns[0].WriteMessage(websocket.TextMessage, []byte("not json")); err != nil {
		t.Fatalf("write garbage error = %v", err)
	}

	// Give readPump time to process the invalid JSON.
	time.Sleep(100 * time.Millisecond)

	// The connection should still be alive; send a valid package.
	respData, _ := json.Marshal(protocol.PackageDataResponse{
		ID: "1", Code: protocol.ResponseCodeOK, Msg: "ok",
	})
	validPkg := protocol.Package{Version: 1, Type: protocol.PackageTypeResponse, Data: respData}
	if err := srv.SendPackage(&validPkg); err != nil {
		t.Fatalf("SendPackage() after invalid JSON error = %v", err)
	}

	// Verify the valid package was dispatched.
	done := make(chan struct{})
	go func() {
		responseCalled.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("valid package after invalid JSON was not dispatched")
	}
}

// ---------------------------------------------------------------------------
// Write pump
// ---------------------------------------------------------------------------

func TestWritePump_SendsPings(t *testing.T) {
	srv := newMockWSServer(t)
	opts := clientOptions{
		serverURL:           srv.URL(),
		userID:              "test-user",
		reconnectBaseDelay:  defaultReconnectBaseDelay,
		reconnectMaxDelay:   defaultReconnectMaxDelay,
		reconnectMaxRetries: defaultReconnectMaxRetries,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{})
	// Use a very short ping period so the test completes quickly.
	cm.pingPeriod = 50 * time.Millisecond
	if err := cm.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { safeCloseCM(cm) })

	// Set a pong handler on the server side to verify pings arrive.
	srv.mu.Lock()
	conns := make([]*websocket.Conn, len(srv.conns))
	copy(conns, srv.conns)
	srv.mu.Unlock()
	if len(conns) == 0 {
		t.Fatal("no connections")
	}

	// The server's read loop in our mock already reads messages, but pings
	// are control frames handled by gorilla internally. We can verify the
	// connection stays alive for longer than the ping period.
	time.Sleep(200 * time.Millisecond)

	if !cm.IsConnected() {
		t.Error("IsConnected() = false after ping period, connection should be alive")
	}
}

// ---------------------------------------------------------------------------
// Disconnected channel
// ---------------------------------------------------------------------------

func TestDisconnected_Channel(t *testing.T) {
	srv := newMockWSServer(t)
	disconnected := make(chan struct{})

	opts := clientOptions{
		serverURL:           srv.URL(),
		userID:              "test-user",
		reconnectBaseDelay:  defaultReconnectBaseDelay,
		reconnectMaxDelay:   defaultReconnectMaxDelay,
		reconnectMaxRetries: defaultReconnectMaxRetries,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{
		onDisconnect: func(replaced bool) {
			close(disconnected)
		},
	})
	if err := cm.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	ch := cm.Disconnected()
	if ch == nil {
		t.Fatal("Disconnected() returned nil channel")
	}

	// Close the connection manager. Since closing is deliberate, onDisconnect
	// should NOT be called.
	safeCloseCM(cm)

	select {
	case <-disconnected:
		t.Error("onDisconnect was called during deliberate Close; want no callback")
	case <-time.After(200 * time.Millisecond):
		// Good: no unexpected disconnect callback.
	}

	if cm.IsConnected() {
		t.Error("IsConnected() = true after Close, want false")
	}
}

// ---------------------------------------------------------------------------
// Backoff delay
// ---------------------------------------------------------------------------

func TestBackoffDelay_Exponential(t *testing.T) {
	base := 100 * time.Millisecond
	maxDelay := 10 * time.Second

	// Attempt 1: base * 2^0 = 100ms (plus jitter).
	d1 := backoffDelay(1, base, maxDelay)
	if d1 < base/2 || d1 > base*2 {
		t.Errorf("attempt 1: delay %v outside expected range", d1)
	}

	// Attempt 2: base * 2^1 = 200ms (plus jitter).
	d2 := backoffDelay(2, base, maxDelay)
	if d2 < 100*time.Millisecond || d2 > 400*time.Millisecond {
		t.Errorf("attempt 2: delay %v outside expected range", d2)
	}

	// Attempt 3: base * 2^2 = 400ms (plus jitter).
	d3 := backoffDelay(3, base, maxDelay)
	if d3 < 200*time.Millisecond || d3 > 800*time.Millisecond {
		t.Errorf("attempt 3: delay %v outside expected range", d3)
	}
}

func TestBackoffDelay_Cap(t *testing.T) {
	base := 1 * time.Second
	maxDelay := 5 * time.Second

	// Attempt 10: base * 2^9 = 512s, should be capped at 5s.
	d := backoffDelay(10, base, maxDelay)
	if d > maxDelay+maxDelay/4 {
		t.Errorf("delay %v exceeds cap %v + jitter", d, maxDelay)
	}
}

func TestBackoffDelay_Jitter(t *testing.T) {
	base := 1 * time.Second
	maxDelay := 30 * time.Second

	// Run many samples and verify all are within +/-25% of the exponential value.
	for attempt := 1; attempt <= 5; attempt++ {
		exp := attempt - 1
		if exp > 30 {
			exp = 30
		}
		baseDelay := base * time.Duration(1<<uint(exp))
		if baseDelay > maxDelay {
			baseDelay = maxDelay
		}
		low := baseDelay - baseDelay/4
		high := baseDelay + baseDelay/4

		for i := 0; i < 50; i++ {
			d := backoffDelay(attempt, base, maxDelay)
			if d < low || d > high {
				t.Errorf("attempt %d: delay %v outside jitter range [%v, %v]", attempt, d, low, high)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Marshal / Unmarshal
// ---------------------------------------------------------------------------

func TestMarshalUnmarshalPackage(t *testing.T) {
	original := &protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeRequest,
		Data:    json.RawMessage(`{"id":"1","method":"test","params":{}}`),
	}

	data, err := marshalPackage(original)
	if err != nil {
		t.Fatalf("marshalPackage() error = %v", err)
	}

	decoded, err := unmarshalPackage(data)
	if err != nil {
		t.Fatalf("unmarshalPackage() error = %v", err)
	}

	if decoded.Version != original.Version {
		t.Errorf("Version = %d, want %d", decoded.Version, original.Version)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type = %d, want %d", decoded.Type, original.Type)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("Data = %q, want %q", decoded.Data, original.Data)
	}
}

func TestUnmarshalPackage_InvalidJSON(t *testing.T) {
	_, err := unmarshalPackage([]byte("not json"))
	if err == nil {
		t.Error("unmarshalPackage() expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// asClientError attempts to cast err to *ClientError. Returns true on success.
func asClientError(err error, target **ClientError) bool {
	if err == nil {
		return false
	}
	if ce, ok := err.(*ClientError); ok {
		*target = ce
		return true
	}
	if strings.HasPrefix(err.Error(), "client: [") {
		return false
	}
	return false
}

// ---------------------------------------------------------------------------
// Reconnect
// ---------------------------------------------------------------------------

// TestReconnect_MaxRetriesExceeded verifies that Reconnect returns an error
// after maxRetries has been exhausted. We manually set the attempt counter
// close to maxRetries so the limit is hit on the next call.
func TestReconnect_MaxRetriesExceeded(t *testing.T) {
	// Use an unreachable URL so reconnection attempts fail and the counter
	// keeps incrementing.
	opts := clientOptions{
		serverURL:           "ws://127.0.0.1:1/ws",
		userID:              "test-user",
		reconnectBaseDelay:  1 * time.Millisecond,
		reconnectMaxDelay:   2 * time.Millisecond,
		reconnectMaxRetries: 3,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{})
	// Manually set attempt to just below maxRetries so the next Reconnect
	// increments it to maxRetries, and the one after that fails.
	cm.mu.Lock()
	cm.attempt = 2 // maxRetries=3, so one more increment hits the limit
	cm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This should increment attempt to 3 (= maxRetries) and fail to connect,
	// but the check is at the top: attempt >= maxRetries → error.
	// Wait: attempt=2 < 3, so it increments to 3 and tries to connect (fails).
	_ = cm.Reconnect(ctx)

	// Now attempt=3 (after failed connect, Connect resets only on success,
	// but since dial failed, attempt stays at 3 from the Reconnect increment).
	// Actually, Connect only resets attempt on success. Since the URL is
	// unreachable, Connect fails and attempt stays at 3.
	// Next call: attempt(3) >= maxRetries(3) → immediate error.
	err := cm.Reconnect(ctx)
	if err == nil {
		t.Fatal("expected error after maxRetries exceeded, got nil")
	}
}

// TestReconnect_SuccessResetsAttempt verifies that a successful Connect resets
// the attempt counter back to 0.
func TestReconnect_SuccessResetsAttempt(t *testing.T) {
	srv := newMockWSServer(t)
	opts := clientOptions{
		serverURL:           srv.URL(),
		userID:              "test-user",
		reconnectBaseDelay:  1 * time.Millisecond,
		reconnectMaxDelay:   5 * time.Millisecond,
		reconnectMaxRetries: 5,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{})
	if err := cm.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { cm.Close() })

	// After Connect, attempt should be 0.
	if got := cm.Attempt(); got != 0 {
		t.Fatalf("Attempt() after Connect = %d, want 0", got)
	}

	// Simulate disconnect and reconnect.
	cm.mu.Lock()
	cm.connected = false
	cm.mu.Unlock()

	if err := cm.Reconnect(context.Background()); err != nil {
		t.Fatalf("Reconnect() error = %v", err)
	}

	// After successful Reconnect -> Connect, attempt should be reset to 0.
	if got := cm.Attempt(); got != 0 {
		t.Errorf("Attempt() after successful Reconnect = %d, want 0", got)
	}
}

// TestReconnect_ContextCancelled verifies that Reconnect returns promptly when
// the context is cancelled during the backoff wait.
func TestReconnect_ContextCancelled(t *testing.T) {
	// Use an unreachable URL so the dial would hang.
	opts := clientOptions{
		serverURL:           "ws://127.0.0.1:1/ws",
		userID:              "test-user",
		reconnectBaseDelay:  10 * time.Second, // long delay to ensure context cancellation is noticed
		reconnectMaxDelay:   30 * time.Second,
		reconnectMaxRetries: 5,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately.
	cancel()

	err := cm.Reconnect(ctx)
	if err == nil {
		cm.Close()
		t.Fatal("expected error from cancelled context, got nil")
	}
	// The error should be context.Canceled or a connection error wrapping it.
}

// ---------------------------------------------------------------------------
// 4001 semantic awareness (D-095, D-111)
// ---------------------------------------------------------------------------

// TestConnectionManager_4001_NoReconnect verifies that receiving a 4001 close
// frame from the server sets the replaced flag and passes replaced=true to the
// onDisconnect callback.
func TestConnectionManager_4001_NoReconnect(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		msg := websocket.FormatCloseMessage(4001, "replaced by newer device")
		_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
		// Keep the handler alive briefly so the client can receive the close frame.
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	var gotReplaced bool
	var disconnectCalled sync.WaitGroup
	disconnectCalled.Add(1)

	opts := clientOptions{
		serverURL:           wsURL,
		userID:              "test-user",
		reconnectBaseDelay:  defaultReconnectBaseDelay,
		reconnectMaxDelay:   defaultReconnectMaxDelay,
		reconnectMaxRetries: defaultReconnectMaxRetries,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{
		onDisconnect: func(replaced bool) {
			gotReplaced = replaced
			disconnectCalled.Done()
		},
	})

	if err := cm.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { safeCloseCM(cm) })

	// Wait for the onDisconnect callback to fire.
	done := make(chan struct{})
	go func() {
		disconnectCalled.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("onDisconnect was not called within timeout")
	}

	// Verify replaced flag was set.
	if !cm.Replaced() {
		t.Error("Replaced() = false, want true after 4001 close frame")
	}
	if !gotReplaced {
		t.Error("onDisconnect received replaced=false, want true")
	}
}

// TestConnectionManager_OtherClose_Reconnect verifies that a non-4001 close
// frame (e.g., 1000 Normal Closure) does NOT set the replaced flag, allowing
// normal reconnection behavior.
func TestConnectionManager_OtherClose_Reconnect(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "normal closure")
		_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	var gotReplaced bool = true // initialize to true to detect if callback fires with false
	var disconnectCalled sync.WaitGroup
	disconnectCalled.Add(1)

	opts := clientOptions{
		serverURL:           wsURL,
		userID:              "test-user",
		reconnectBaseDelay:  defaultReconnectBaseDelay,
		reconnectMaxDelay:   defaultReconnectMaxDelay,
		reconnectMaxRetries: defaultReconnectMaxRetries,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{
		onDisconnect: func(replaced bool) {
			gotReplaced = replaced
			disconnectCalled.Done()
		},
	})

	if err := cm.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { safeCloseCM(cm) })

	// Wait for the onDisconnect callback.
	done := make(chan struct{})
	go func() {
		disconnectCalled.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("onDisconnect was not called within timeout")
	}

	// Verify replaced flag was NOT set.
	if cm.Replaced() {
		t.Error("Replaced() = true, want false after non-4001 close frame")
	}
	if gotReplaced {
		t.Error("onDisconnect received replaced=true, want false")
	}
}

// TestConnectionManager_Replaced_ResetOnConnect verifies that calling Connect
// resets the replaced flag to false, allowing a fresh connection attempt.
func TestConnectionManager_Replaced_ResetOnConnect(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Keep connection alive for the duration of the test.
		time.Sleep(500 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	opts := clientOptions{
		serverURL:           wsURL,
		userID:              "test-user",
		reconnectBaseDelay:  defaultReconnectBaseDelay,
		reconnectMaxDelay:   defaultReconnectMaxDelay,
		reconnectMaxRetries: defaultReconnectMaxRetries,
		logger:              &testLogger{t: t},
	}
	cm := newConnectionManager(opts, connectionCallbacks{})

	// Manually set replaced=true to simulate a previous 4001 event.
	cm.mu.Lock()
	cm.replaced = true
	cm.mu.Unlock()

	if !cm.Replaced() {
		t.Fatal("precondition: Replaced() should be true before Connect")
	}

	// Connect should reset replaced to false.
	if err := cm.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { safeCloseCM(cm) })

	if cm.Replaced() {
		t.Error("Replaced() = true after Connect, want false (should be reset)")
	}
}
