package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// startWSServer creates a WebSocketServer with the given ConnectionStore and
// starts it on a random port. It returns the server, its listen address, and
// a cleanup function that cancels the server context and waits for it to
// shut down. The caller should invoke the cleanup function (typically via
// defer) when done.
func startWSServer(t *testing.T, cs ConnectionStore, opts ...WebSocketServerOption) (*WebSocketServer, string, func()) {
	t.Helper()

	baseOpts := []WebSocketServerOption{
		WSWithAddr(":0"),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
		WSWithConnectionStore(cs),
		// Use short ping/pong periods so that heartbeat tests don't block forever.
		WSWithPingPeriod(100 * time.Millisecond),
		WSWithPongWait(2 * time.Second),
		WSWithWriteWait(2 * time.Second),
	}
	baseOpts = append(baseOpts, opts...)

	srv, err := NewWebSocketServer(baseOpts...)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Wait for the server to start accepting connections.
	require.Eventually(t, func() bool {
		addr := srv.Addr()
		return addr != "" && addr != ":0"
	}, 2*time.Second, 10*time.Millisecond, "server did not start in time")

	addr := srv.Addr()

	cleanup := func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Log("server did not shut down in time during cleanup")
		}
	}

	return srv, addr, cleanup
}

// connectWS connects a WebSocket client to the given server address with the
// specified user_id query parameter. It returns the established connection.
func connectWS(t *testing.T, addr, userID string) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s/ws?user_id=%s", addr, userID)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	return conn
}

// connectWSPath connects a WebSocket client to a custom path.
func connectWSPath(t *testing.T, addr, path, userID string) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s%s?user_id=%s", addr, path, userID)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	return conn
}

// readPackage reads a single protocol.Package from the connection with a timeout.
func readPackage(t *testing.T, conn *websocket.Conn, timeout time.Duration) *protocol.Package {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var pkg protocol.Package
	require.NoError(t, json.Unmarshal(msg, &pkg))
	return &pkg
}

// readResponsePackage reads a response package and decodes the inner data into
// a PackageDataResponse.
func readResponsePackage(t *testing.T, conn *websocket.Conn, timeout time.Duration) *protocol.PackageDataResponse {
	t.Helper()
	pkg := readPackage(t, conn, timeout)
	require.Equal(t, protocol.PackageTypeResponse, pkg.Type)

	var resp protocol.PackageDataResponse
	require.NoError(t, json.Unmarshal(pkg.Data, &resp))
	return &resp
}

// sendRequestPackage marshals and sends a request package to the connection.
func sendRequestPackage(t *testing.T, conn *websocket.Conn, id, method string, params json.RawMessage) {
	t.Helper()
	req := protocol.PackageDataRequest{
		ID:     id,
		Method: method,
		Params: params,
	}
	reqData, err := json.Marshal(req)
	require.NoError(t, err)

	pkg := protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: reqData,
	}
	data, err := json.Marshal(pkg)
	require.NoError(t, err)

	err = conn.WriteMessage(websocket.TextMessage, data)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// WebSocketServer - Construction tests
// ---------------------------------------------------------------------------

// TestNewWebSocketServer_AllOptions verifies that the server is created
// successfully when all required and optional options are provided.
func TestNewWebSocketServer_AllOptions(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, err := NewWebSocketServer(
		WSWithAddr(":0"),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
		WSWithConnectionStore(cs),
		WSWithPath("/custom-ws"),
		WSWithReadBufferSize(4096),
		WSWithWriteBufferSize(4096),
		WSWithCompression(),
		WSWithWriteWait(5*time.Second),
		WSWithPongWait(30*time.Second),
		WSWithPingPeriod(20*time.Second),
		WSWithMaxMessageSize(1024*1024),
	)
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.Equal(t, "/custom-ws", srv.path)
	assert.NotNil(t, srv.handler)
}

// TestNewWebSocketServer_MissingConnectionStore verifies that creating a
// server without a ConnectionStore returns an error.
func TestNewWebSocketServer_MissingConnectionStore(t *testing.T) {
	_, err := NewWebSocketServer(
		WSWithAddr(":0"),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
		// No WSWithConnectionStore
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection store is required")
}

// TestNewWebSocketServer_Defaults verifies that default values are applied
// when no optional configuration is provided.
func TestNewWebSocketServer_Defaults(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, err := NewWebSocketServer(
		WSWithConnectionStore(cs),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
	)
	require.NoError(t, err)
	assert.Equal(t, "/ws", srv.path)
	assert.NotNil(t, srv.handler)
}

// ---------------------------------------------------------------------------
// WebSocketServer - Lifecycle tests
// ---------------------------------------------------------------------------

// TestWebSocketServer_StartStop verifies that the server starts and stops
// cleanly via context cancellation.
func TestWebSocketServer_StartStop(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, _, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	assert.True(t, srv.IsRunning())
	assert.NotEqual(t, ":0", srv.Addr())
	assert.NotEmpty(t, srv.Addr())
}

// TestWebSocketServer_GracefulStop verifies that GracefulStop shuts the
// server down cleanly and that all active clients are disconnected.
func TestWebSocketServer_GracefulStop(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	// Connect a client before stopping.
	conn := connectWS(t, addr, "user-graceful")
	defer conn.Close()

	// Give the server a moment to register the client.
	time.Sleep(100 * time.Millisecond)
	assert.GreaterOrEqual(t, srv.ClientCount(), 1)

	// GracefulStop should close the server and all clients.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := srv.GracefulStop(ctx)
	require.NoError(t, err)

	// The client read should fail since the server closed the connection.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// WebSocketServer - Default path "/ws"
// ---------------------------------------------------------------------------

// TestWebSocketServer_DefaultPath verifies that the default WebSocket
// endpoint at /ws accepts connections.
func TestWebSocketServer_DefaultPath(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-default-path")
	defer conn.Close()

	// Connection should be alive; just verify we can write a ping.
	err := conn.WriteMessage(websocket.PingMessage, nil)
	require.NoError(t, err)
}

// TestWebSocketServer_CustomPath verifies that a custom path is used when
// configured via WSWithPath.
func TestWebSocketServer_CustomPath(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs, WSWithPath("/my-ws"))
	defer wsCleanup()

	// Connection to the custom path should succeed.
	conn := connectWSPath(t, addr, "/my-ws", "user-custom-path")
	defer conn.Close()

	err := conn.WriteMessage(websocket.PingMessage, nil)
	require.NoError(t, err)
}

// TestWebSocketServer_WrongPath verifies that connecting to a non-existent
// path returns a 404 error.
func TestWebSocketServer_WrongPath(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	url := fmt.Sprintf("ws://%s/wrong-path?user_id=test", addr)
	_, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// WebSocket connection tests (real connections)
// ---------------------------------------------------------------------------

// TestWebSocketConn_MissingUserID verifies that a connection without a
// user_id query parameter is rejected with HTTP 401.
func TestWebSocketConn_MissingUserID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	url := fmt.Sprintf("ws://%s/ws", addr)
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.Error(t, err)
	if resp != nil {
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	}
}

// TestWebSocketConn_EmptyUserID verifies that a connection with an empty
// user_id query parameter is rejected with HTTP 401.
func TestWebSocketConn_EmptyUserID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	url := fmt.Sprintf("ws://%s/ws?user_id=", addr)
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.Error(t, err)
	if resp != nil {
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	}
}

// TestWebSocketConn_ClientCountIncrease verifies that connecting a client
// increases the server's ClientCount.
func TestWebSocketConn_ClientCountIncrease(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	assert.Equal(t, 0, srv.ClientCount())

	conn := connectWS(t, addr, "user-count")
	defer conn.Close()

	// Wait for the server to register the client.
	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)
}

// TestWebSocketConn_ClientCountDecrease verifies that disconnecting a client
// decreases the server's ClientCount.
func TestWebSocketConn_ClientCountDecrease(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-disconnect")

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	// Close the connection from the client side.
	conn.Close()

	// Wait for the server to clean up.
	require.Eventually(t, func() bool {
		return srv.ClientCount() == 0
	}, 3*time.Second, 50*time.Millisecond)
}

// TestWebSocketConn_MultipleConnectionsSameUser verifies that the same user
// can have multiple simultaneous connections and that the server tracks them
// independently.
func TestWebSocketConn_MultipleConnectionsSameUser(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	conn1 := connectWS(t, addr, "user-multi")
	defer conn1.Close()
	conn2 := connectWS(t, addr, "user-multi")
	defer conn2.Close()
	conn3 := connectWS(t, addr, "user-multi")
	defer conn3.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 3
	}, 2*time.Second, 50*time.Millisecond)

	assert.Equal(t, 3, srv.ClientsByUser("user-multi"))

	// Close one connection.
	conn3.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() == 2
	}, 3*time.Second, 50*time.Millisecond)
	assert.Equal(t, 2, srv.ClientsByUser("user-multi"))
}

// TestWebSocketConn_RegisteredInConnectionStore verifies that a connected
// client is registered in the Redis ConnectionStore and can be queried.
func TestWebSocketConn_RegisteredInConnectionStore(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-store-check")
	defer conn.Close()

	// Wait for the connection to be registered in Redis.
	require.Eventually(t, func() bool {
		conns, err := cs.ListByUser(context.Background(), "user-store-check", 10)
		if err != nil {
			return false
		}
		return len(conns) >= 1
	}, 3*time.Second, 100*time.Millisecond)

	conns, err := cs.ListByUser(context.Background(), "user-store-check", 10)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, "user-store-check", conns[0].UserID)
	assert.Equal(t, "websocket", conns[0].Protocol)
	assert.Equal(t, "active", conns[0].Status)
}

// TestWebSocketConn_ConnectionRemovedFromStoreOnDisconnect verifies that when
// a client disconnects, its entry is removed from the ConnectionStore.
func TestWebSocketConn_ConnectionRemovedFromStoreOnDisconnect(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-remove-store")

	// Wait for the connection to be registered.
	require.Eventually(t, func() bool {
		conns, err := cs.ListByUser(context.Background(), "user-remove-store", 10)
		if err != nil {
			return false
		}
		return len(conns) >= 1
	}, 3*time.Second, 100*time.Millisecond)

	// Close the connection.
	conn.Close()

	// Wait for the connection to be removed from the store.
	require.Eventually(t, func() bool {
		conns, err := cs.ListByUser(context.Background(), "user-remove-store", 10)
		if err != nil {
			return false
		}
		return len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond)
}

// ---------------------------------------------------------------------------
// Message communication tests
// ---------------------------------------------------------------------------

// TestWebSocketMsg_RequestResponse verifies that sending a request message
// triggers a response from the server (via the default handler's unknown
// method response).
func TestWebSocketMsg_RequestResponse(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-req-resp")
	defer conn.Close()

	// Send a request for an unregistered method.
	sendRequestPackage(t, conn, "req-1", "ping", json.RawMessage(`{}`))

	// Should receive a response (error since "ping" is not registered).
	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "req-1", resp.ID)
	assert.NotEqual(t, protocol.ResponseCode(0), resp.Code)
	assert.Contains(t, resp.Msg, "unknown method")
}

// TestWebSocketMsg_MethodHandler verifies that a registered MethodHandler
// processes requests and returns correct responses.
func TestWebSocketMsg_MethodHandler(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("echo", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		// Echo back the params as the response data.
		return req.Params, nil
	})

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	conn := connectWS(t, addr, "user-handler")
	defer conn.Close()

	sendRequestPackage(t, conn, "echo-1", "echo", json.RawMessage(`{"msg":"hello"}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "echo-1", resp.ID)
	assert.Equal(t, protocol.ResponseCode(0), resp.Code)
	assert.Equal(t, "ok", resp.Msg)

	// Decode the data and verify it matches what we sent.
	var data map[string]string
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, "hello", data["msg"])
}

// TestWebSocketMsg_UnknownMethodReturnsError verifies that a request for an
// unregistered method returns an error response with code != 0.
func TestWebSocketMsg_UnknownMethodReturnsError(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-unknown-method")
	defer conn.Close()

	sendRequestPackage(t, conn, "req-unknown", "nonexistent_method", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "req-unknown", resp.ID)
	assert.Equal(t, protocol.ResponseCode(-1), resp.Code)
	assert.Contains(t, resp.Msg, "unknown method")
}

// TestWebSocketMsg_BroadcastUpdates verifies that BroadcastUpdates sends
// update packages to all connections of a specific user.
func TestWebSocketMsg_BroadcastUpdates(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	// Connect two clients for the target user and one for a different user.
	conn1 := connectWS(t, addr, "user-broadcast")
	defer conn1.Close()
	conn2 := connectWS(t, addr, "user-broadcast")
	defer conn2.Close()
	connOther := connectWS(t, addr, "user-other")
	defer connOther.Close()

	// Wait for all clients to register.
	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 3
	}, 2*time.Second, 50*time.Millisecond)

	// Broadcast updates to user-broadcast.
	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{
				Seq:     1,
				Payload: json.RawMessage(`{"event":"new_message"}`),
			},
		},
	}
	err := srv.BroadcastUpdates("user-broadcast", updates)
	require.NoError(t, err)

	// Both user-broadcast clients should receive the update.
	for _, c := range []*websocket.Conn{conn1, conn2} {
		pkg := readPackage(t, c, 3*time.Second)
		assert.Equal(t, protocol.PackageTypeUpdates, pkg.Type)

		var upd protocol.PackageDataUpdates
		require.NoError(t, json.Unmarshal(pkg.Data, &upd))
		require.Len(t, upd.Updates, 1)
		assert.Equal(t, uint32(1), upd.Updates[0].Seq)
	}

	// user-other should NOT receive any updates. We verify by checking that
	// a read on connOther times out.
	_ = connOther.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = connOther.ReadMessage()
	assert.Error(t, err, "user-other should not receive broadcasts for user-broadcast")
}

// TestWebSocketMsg_BroadcastUpdates_NilUpdates verifies that broadcasting
// nil updates returns an error.
func TestWebSocketMsg_BroadcastUpdates_NilUpdates(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, _, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	err := srv.BroadcastUpdates("user-nil", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "updates is nil")
}

// TestWebSocketMsg_MalformedMessageDoesNotDisconnect verifies that sending a
// malformed message (invalid JSON) does not disconnect the client. The client
// should remain connected and be able to send subsequent valid messages.
func TestWebSocketMsg_MalformedMessageDoesNotDisconnect(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("ping", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"pong":true}`), nil
	})

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	conn := connectWS(t, addr, "user-malformed")
	defer conn.Close()

	// Send malformed JSON.
	err := conn.WriteMessage(websocket.TextMessage, []byte("not valid json {{{"))
	require.NoError(t, err)

	// Give the server a moment to process and log the error (P2-08: use Eventually).
	// Verify by sending a valid request immediately after; if the server is still
	// processing the malformed message, the valid request may interleave.
	// We use a small sleep here because we're testing that the connection is still
	// alive after a malformed message, not testing a specific state transition.
	time.Sleep(200 * time.Millisecond)

	// The connection should still be alive. Send a valid request.
	sendRequestPackage(t, conn, "after-malformed", "ping", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "after-malformed", resp.ID)
	assert.Equal(t, protocol.ResponseCode(0), resp.Code)
}

// TestWebSocketMsg_HandlerErrorReturnsErrorResponse verifies that when a
// MethodHandler returns an error, the client receives an error response.
func TestWebSocketMsg_HandlerErrorReturnsErrorResponse(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("fail", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return nil, fmt.Errorf("handler error: something went wrong")
	})

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	conn := connectWS(t, addr, "user-handler-err")
	defer conn.Close()

	sendRequestPackage(t, conn, "req-fail", "fail", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "req-fail", resp.ID)
	assert.Equal(t, protocol.ResponseCode(-1), resp.Code)
	assert.Contains(t, resp.Msg, "handler error: something went wrong")
}

// ---------------------------------------------------------------------------
// DefaultMessageHandler tests
// ---------------------------------------------------------------------------

// TestDefaultMessageHandler_RegisterMethod verifies that methods can be
// registered and invoked.
func TestDefaultMessageHandler_RegisterMethod(t *testing.T) {
	handler := NewDefaultMessageHandler()

	called := false
	handler.RegisterMethod("test_method", MethodHandlerFunc(
		func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
			called = true
			return json.RawMessage(`{"ok":true}`), nil
		},
	))

	// Verify the method is registered by invoking HandleMessage.
	// We need a fake client for this.
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	conn := connectWS(t, addr, "user-register-method")
	defer conn.Close()

	sendRequestPackage(t, conn, "reg-1", "test_method", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "reg-1", resp.ID)
	assert.Equal(t, protocol.ResponseCode(0), resp.Code)
	assert.True(t, called)
}

// TestDefaultMessageHandler_RegisterMethodFunc verifies the convenience
// wrapper RegisterMethodFunc works identically to RegisterMethod.
func TestDefaultMessageHandler_RegisterMethodFunc(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("add", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"result":42}`), nil
	})

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	conn := connectWS(t, addr, "user-method-func")
	defer conn.Close()

	sendRequestPackage(t, conn, "add-1", "add", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "add-1", resp.ID)
	assert.Equal(t, protocol.ResponseCode(0), resp.Code)

	var data map[string]int
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, 42, data["result"])
}

// TestDefaultMessageHandler_SetFallback verifies that a fallback handler is
// invoked when a request method is not registered.
func TestDefaultMessageHandler_SetFallback(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()
	fallbackCalled := false
	handler.SetFallback(MethodHandlerFunc(
		func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
			fallbackCalled = true
			return json.RawMessage(`{"fallback":true}`), nil
		},
	))

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	conn := connectWS(t, addr, "user-fallback")
	defer conn.Close()

	sendRequestPackage(t, conn, "fb-1", "unknown_method", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "fb-1", resp.ID)
	assert.Equal(t, protocol.ResponseCode(0), resp.Code)
	assert.True(t, fallbackCalled)
}

// TestDefaultMessageHandler_ConcurrentRegisterAndCall verifies that method
// registration and invocation are safe for concurrent use.
func TestDefaultMessageHandler_ConcurrentRegisterAndCall(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()

	// Pre-register a method.
	var mu sync.Mutex
	callCount := 0
	handler.RegisterMethodFunc("concurrent", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		return json.RawMessage(`{}`), nil
	})

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	const numClients = 5
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = connectWS(t, addr, fmt.Sprintf("user-concurrent-%d", i))
		defer conns[i].Close()
	}

	// Each client sends a request concurrently.
	var wg sync.WaitGroup
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sendRequestPackage(t, conns[i], fmt.Sprintf("concurrent-%d", i), "concurrent", json.RawMessage(`{}`))
		}(i)
	}
	wg.Wait()

	// Each client should receive a response.
	for i := 0; i < numClients; i++ {
		resp := readResponsePackage(t, conns[i], 3*time.Second)
		assert.Equal(t, fmt.Sprintf("concurrent-%d", i), resp.ID)
		assert.Equal(t, protocol.ResponseCode(0), resp.Code)
	}

	mu.Lock()
	assert.Equal(t, numClients, callCount)
	mu.Unlock()

	// Concurrently register new methods while calling existing ones.
	wg.Add(numClients)
	for i := 0; i < numClients; i++ {
		go func(i int) {
			defer wg.Done()
			methodName := fmt.Sprintf("dynamic_%d", i)
			handler.RegisterMethodFunc(methodName, func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			})
		}(i)
	}
	wg.Wait()

	// No panic or race condition means success.
}

// TestDefaultMessageHandler_MethodOverwrite verifies that re-registering a
// method overwrites the previous handler.
func TestDefaultMessageHandler_MethodOverwrite(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("overwrite_me", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"version":"old"}`), nil
	})

	// Overwrite the handler.
	handler.RegisterMethodFunc("overwrite_me", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"version":"new"}`), nil
	})

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	conn := connectWS(t, addr, "user-overwrite")
	defer conn.Close()

	sendRequestPackage(t, conn, "ow-1", "overwrite_me", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, protocol.ResponseCode(0), resp.Code)

	var data map[string]string
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, "new", data["version"])
}

// ---------------------------------------------------------------------------
// Client tests
// ---------------------------------------------------------------------------

// TestClient_SendAndReceive verifies that messages sent via Send/SendPackage
// are delivered to the WebSocket peer. We register a handler that uses
// SendPackage to push a custom message, then returns a normal response.
func TestClient_SendAndReceive(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("reply", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		// Return a normal response. The SendPackage path is exercised by
		// sendSuccessResponse internally (which calls client.SendPackage).
		return json.RawMessage(`{"echo":"pong"}`), nil
	})

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	conn := connectWS(t, addr, "user-send")
	defer conn.Close()

	sendRequestPackage(t, conn, "reply-1", "reply", json.RawMessage(`{}`))

	// Read the response (produced by sendSuccessResponse via client.SendPackage).
	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "reply-1", resp.ID)
	assert.Equal(t, protocol.ResponseCode(0), resp.Code)

	var data map[string]string
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, "pong", data["echo"])
}

// TestClient_SendBufferFullDropsMessage verifies that when the send channel
// buffer is full, messages are silently dropped rather than blocking.
func TestClient_SendBufferFullDropsMessage(t *testing.T) {
	// This test creates a client connected to a real WebSocket server, but
	// the handler never reads from the client. We fill the send buffer by
	// pushing messages faster than they are drained.
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs,
		WSWithPingPeriod(1*time.Hour), // disable pings
	)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-buffer-full")
	defer conn.Close()

	// Wait for the server to register the client.
	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	// Find the client in the server.
	srv.mu.RLock()
	var targetClient *Client
	for _, c := range srv.clients {
		if c.UserID() == "user-buffer-full" {
			targetClient = c
			break
		}
	}
	srv.mu.RUnlock()
	require.NotNil(t, targetClient)

	// Send many messages rapidly. Since no one is reading the client's send
	// channel fast enough (and the buffer is defaultSendBufSize=256), many
	// will be dropped but Send must not block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			targetClient.Send([]byte(fmt.Sprintf("msg-%d", i)))
		}
	}()

	select {
	case <-done:
		// Success: Send did not block.
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked when buffer was full; expected silent drop")
	}
}

// TestClient_CloseIdempotent verifies that calling Close multiple times on a
// Client does not panic or cause errors.
func TestClient_CloseIdempotent(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs,
		WSWithPingPeriod(1*time.Hour),
	)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-close-idem")
	defer conn.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	// Find the client and close it multiple times.
	srv.mu.RLock()
	var targetClient *Client
	for _, c := range srv.clients {
		if c.UserID() == "user-close-idem" {
			targetClient = c
			break
		}
	}
	srv.mu.RUnlock()
	require.NotNil(t, targetClient)

	assert.NotPanics(t, func() {
		targetClient.Close()
		targetClient.Close()
		targetClient.Close()
	})
}

// TestClient_SendAfterClose verifies that Send is a no-op after Close.
func TestClient_SendAfterClose(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs,
		WSWithPingPeriod(1*time.Hour),
	)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-send-after-close")
	defer conn.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	srv.mu.RLock()
	var targetClient *Client
	for _, c := range srv.clients {
		if c.UserID() == "user-send-after-close" {
			targetClient = c
			break
		}
	}
	srv.mu.RUnlock()
	require.NotNil(t, targetClient)

	targetClient.Close()

	// Wait for Close to take effect (replace time.Sleep with Eventually, P2-08).
	require.Eventually(t, func() bool {
		targetClient.mu.Lock()
		defer targetClient.mu.Unlock()
		return targetClient.closed
	}, 2*time.Second, 50*time.Millisecond)

	// Send after Close should be a no-op (not panic, not block).
	assert.NotPanics(t, func() {
		targetClient.Send([]byte("after-close"))
	})
}

// TestClient_Accessors verifies that UserID and ConnID return the expected
// values.
func TestClient_Accessors(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs,
		WSWithPingPeriod(1*time.Hour),
	)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-42")
	defer conn.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	srv.mu.RLock()
	var targetClient *Client
	for _, c := range srv.clients {
		if c.UserID() == "user-42" {
			targetClient = c
			break
		}
	}
	srv.mu.RUnlock()
	require.NotNil(t, targetClient)

	assert.Equal(t, "user-42", targetClient.UserID())
	assert.NotEmpty(t, targetClient.ConnID()) // ConnID is server-assigned UUID
}

// TestClient_PingPong verifies that the server sends periodic pings and the
// client responds with pongs automatically (gorilla/websocket default).
func TestClient_PingPong(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs,
		WSWithPingPeriod(200*time.Millisecond),
		WSWithPongWait(2*time.Second),
	)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-ping-pong")
	defer conn.Close()

	// Wait for the server to register the client.
	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	// Set up a pong handler on the client side. gorilla/websocket by default
	// logs pings; we just verify the connection stays alive for several ping
	// cycles.
	pongReceived := make(chan struct{}, 10)
	conn.SetPingHandler(func(appData string) error {
		select {
		case pongReceived <- struct{}{}:
		default:
		}
		// Write a pong response.
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
	})

	// Read messages in the background to keep the connection alive.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// Wait for at least one ping to be received.
	select {
	case <-pongReceived:
		// Success: server is sending pings.
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive a ping from the server within 3 seconds")
	}

	conn.Close()
	<-done
}

// ---------------------------------------------------------------------------
// WebSocketServer - Custom authentication
// ---------------------------------------------------------------------------

// TestWebSocketServer_CustomAuthenticate verifies that a custom authenticate
// function is used during the WebSocket upgrade.
func TestWebSocketServer_CustomAuthenticate(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs,
		WSWithAuthenticate(func(r *http.Request) (string, error) {
			token := r.URL.Query().Get("token")
			if token == "valid-token" {
				return "user-from-token", nil
			}
			return "", ErrAuthenticationFailed
		}),
	)
	defer wsCleanup()

	// Connect with a valid token.
	url := fmt.Sprintf("ws://%s/ws?token=valid-token", addr)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Connect with an invalid token should fail.
	urlInvalid := fmt.Sprintf("ws://%s/ws?token=bad-token", addr)
	_, _, err = websocket.DefaultDialer.Dial(urlInvalid, nil)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// WebSocketServer - BroadcastUpdates to no clients
// ---------------------------------------------------------------------------

// TestWebSocketMsg_BroadcastUpdates_NoClients verifies that broadcasting to
// a user with no active connections does not error.
func TestWebSocketMsg_BroadcastUpdates_NoClients(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, _, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{}`)},
		},
	}
	err := srv.BroadcastUpdates("no-such-user", updates)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// WebSocketServer - ClientCount and ClientsByUser
// ---------------------------------------------------------------------------

// TestWebSocketServer_ClientCountAndClientsByUser verifies the accuracy of
// ClientCount and ClientsByUser with multiple users and connections.
func TestWebSocketServer_ClientCountAndClientsByUser(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	srv, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	// Connect 3 users with different numbers of connections.
	connA1 := connectWS(t, addr, "userA")
	defer connA1.Close()
	connA2 := connectWS(t, addr, "userA")
	defer connA2.Close()
	connB1 := connectWS(t, addr, "userB")
	defer connB1.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 3
	}, 2*time.Second, 50*time.Millisecond)

	assert.Equal(t, 3, srv.ClientCount())
	assert.Equal(t, 2, srv.ClientsByUser("userA"))
	assert.Equal(t, 1, srv.ClientsByUser("userB"))
	assert.Equal(t, 0, srv.ClientsByUser("userC"))
}

// ---------------------------------------------------------------------------
// WebSocketServer - MessageHandlerInstance
// ---------------------------------------------------------------------------

// TestWebSocketServer_MessageHandlerInstance verifies that
// MessageHandlerInstance returns the configured handler.
func TestWebSocketServer_MessageHandlerInstance(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()
	srv, err := NewWebSocketServer(
		WSWithAddr(":0"),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
		WSWithConnectionStore(cs),
		WSWithMessageHandler(handler),
	)
	require.NoError(t, err)
	assert.Equal(t, handler, srv.MessageHandlerInstance())
}

// ---------------------------------------------------------------------------
// WebSocketServer - DefaultMessageHandler handles non-Request package types
// ---------------------------------------------------------------------------

// TestDefaultMessageHandler_NonRequestTypes verifies that the handler logs
// and ignores non-Request package types (Response and Updates) from clients.
func TestDefaultMessageHandler_NonRequestTypes(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	_, addr, wsCleanup := startWSServer(t, cs)
	defer wsCleanup()

	conn := connectWS(t, addr, "user-non-request")
	defer conn.Close()

	// Send a PackageTypeResponse to the server. The server should ignore it
	// and the connection should stay alive.
	respPkg := protocol.Package{
		Type: protocol.PackageTypeResponse,
		Data: json.RawMessage(`{"id":"x","code":0,"msg":"ok","data":null}`),
	}
	data, err := json.Marshal(respPkg)
	require.NoError(t, err)
	err = conn.WriteMessage(websocket.TextMessage, data)
	require.NoError(t, err)

	// Wait briefly for the server to process the message (P2-08).
	time.Sleep(200 * time.Millisecond)

	// Connection should still be alive. Verify by sending a valid request
	// (which will get an unknown-method response since nothing is registered).
	sendRequestPackage(t, conn, "still-alive", "anything", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "still-alive", resp.ID)
}

// ---------------------------------------------------------------------------
// WebSocketServer - Multiple concurrent request-response flows
// ---------------------------------------------------------------------------

// TestWebSocketMsg_ConcurrentRequestResponse verifies that multiple
// concurrent request-response flows work correctly on the same connection.
func TestWebSocketMsg_ConcurrentRequestResponse(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()

	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("slow", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		time.Sleep(50 * time.Millisecond)
		return json.RawMessage(fmt.Sprintf(`{"id":"%s"}`, req.ID)), nil
	})

	_, addr, wsCleanup := startWSServer(t, cs, WSWithMessageHandler(handler))
	defer wsCleanup()

	conn := connectWS(t, addr, "user-concurrent-req")
	defer conn.Close()

	// Send multiple requests rapidly.
	const numReqs = 5
	for i := 0; i < numReqs; i++ {
		sendRequestPackage(t, conn, fmt.Sprintf("concurrent-req-%d", i), "slow", json.RawMessage(`{}`))
	}

	// Read all responses.
	receivedIDs := make(map[string]bool)
	for i := 0; i < numReqs; i++ {
		resp := readResponsePackage(t, conn, 5*time.Second)
		assert.Equal(t, protocol.ResponseCode(0), resp.Code)
		receivedIDs[resp.ID] = true
	}

	// All request IDs should have corresponding responses.
	for i := 0; i < numReqs; i++ {
		assert.True(t, receivedIDs[fmt.Sprintf("concurrent-req-%d", i)])
	}
}

// ---------------------------------------------------------------------------
// MemoryConnectionStore-based tests (no Redis required, P2-07)
//
// These tests use the in-memory ConnectionStore so they can run in any
// environment without a Redis dependency. Redis-backed tests above remain as
// integration tests.
// ---------------------------------------------------------------------------

// startWSServerMem is like startWSServer but uses a MemoryConnectionStore
// instead of Redis.
func startWSServerMem(t *testing.T, opts ...WebSocketServerOption) (*WebSocketServer, string, func()) {
	t.Helper()

	cs := NewMemoryConnectionStore(0)
	return startWSServer(t, cs, opts...)
}

// TestMemoryStore_WebSocketBasic verifies that a basic WebSocket connection
// works with the in-memory ConnectionStore.
func TestMemoryStore_WebSocketBasic(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t)
	defer cleanup()

	conn := connectWS(t, addr, "user-mem-basic")
	defer conn.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	assert.Equal(t, 1, srv.ClientCount())
	assert.Equal(t, 1, srv.ClientsByUser("user-mem-basic"))
}

// TestMemoryStore_BroadcastUpdates verifies that BroadcastUpdates works with
// the in-memory ConnectionStore.
func TestMemoryStore_BroadcastUpdates(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t)
	defer cleanup()

	conn1 := connectWS(t, addr, "user-mem-bcast")
	defer conn1.Close()
	conn2 := connectWS(t, addr, "user-mem-bcast")
	defer conn2.Close()
	connOther := connectWS(t, addr, "user-mem-other")
	defer connOther.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 3
	}, 2*time.Second, 50*time.Millisecond)

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{"event":"test"}`)},
		},
	}
	err := srv.BroadcastUpdates("user-mem-bcast", updates)
	require.NoError(t, err)

	for _, c := range []*websocket.Conn{conn1, conn2} {
		pkg := readPackage(t, c, 3*time.Second)
		assert.Equal(t, protocol.PackageTypeUpdates, pkg.Type)
	}

	_ = connOther.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = connOther.ReadMessage()
	assert.Error(t, err, "user-mem-other should not receive broadcasts")
}

// TestMemoryStore_RequestResponse verifies request-response works with the
// in-memory ConnectionStore.
func TestMemoryStore_RequestResponse(t *testing.T) {
	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("echo", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return req.Params, nil
	})

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-mem-req")
	defer conn.Close()

	sendRequestPackage(t, conn, "echo-1", "echo", json.RawMessage(`{"msg":"hello"}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "echo-1", resp.ID)
	assert.Equal(t, protocol.ResponseCodeOK, resp.Code)
	assert.Equal(t, "ok", resp.Msg)
}

// TestMemoryStore_ClientDisconnect verifies that client disconnection cleanup
// works with the in-memory ConnectionStore.
func TestMemoryStore_ClientDisconnect(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t)
	defer cleanup()

	conn := connectWS(t, addr, "user-mem-disconnect")

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	conn.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() == 0
	}, 3*time.Second, 50*time.Millisecond)

	assert.Equal(t, 0, srv.ClientsByUser("user-mem-disconnect"))
}

// TestMemoryStore_MultipleUsers verifies per-user tracking with the in-memory
// ConnectionStore.
func TestMemoryStore_MultipleUsers(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t)
	defer cleanup()

	connA1 := connectWS(t, addr, "userA")
	defer connA1.Close()
	connA2 := connectWS(t, addr, "userA")
	defer connA2.Close()
	connB1 := connectWS(t, addr, "userB")
	defer connB1.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 3
	}, 2*time.Second, 50*time.Millisecond)

	assert.Equal(t, 3, srv.ClientCount())
	assert.Equal(t, 2, srv.ClientsByUser("userA"))
	assert.Equal(t, 1, srv.ClientsByUser("userB"))
	assert.Equal(t, 0, srv.ClientsByUser("userC"))
}

// TestMemoryStore_HealthEndpoint verifies the /health endpoint works with
// the in-memory ConnectionStore.
func TestMemoryStore_HealthEndpoint(t *testing.T) {
	_, addr, cleanup := startWSServerMem(t)
	defer cleanup()

	// The health endpoint should return HTTP 200 with status "ok".
	url := fmt.Sprintf("http://%s/health", addr)
	require.Eventually(t, func() bool {
		resp, err := http.Get(url)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 50*time.Millisecond)
}
