package server

import (
	"bytes"
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

// TestClient_Send_BufferFull_ReturnsErrSendBufferFull verifies that when the
// send channel buffer is full, Send returns ErrSendBufferFull rather than
// blocking or silently dropping.
func TestClient_Send_BufferFull_ReturnsErrSendBufferFull(t *testing.T) {
	// Use a client with a small buffer (size=1) so we can deterministically
	// fill it and verify the error return.
	c := newClientWithSendBuf(t, "u-buf-full", 1)

	// Fill the buffer.
	err := c.Send([]byte("first"))
	require.NoError(t, err, "first Send should succeed")

	// Buffer is now full; the next Send should return ErrSendBufferFull.
	err = c.Send([]byte("second"))
	assert.ErrorIs(t, err, ErrSendBufferFull, "Send should return ErrSendBufferFull when buffer is full")
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
	targetClient := findClient(srv, "user-close-idem")
	require.NotNil(t, targetClient)

	assert.NotPanics(t, func() {
		targetClient.Close()
		targetClient.Close()
		targetClient.Close()
	})
}

// TestClient_Send_Closed_ReturnsErrClientClosed verifies that Send returns
// ErrClientClosed after the client has been closed.
func TestClient_Send_Closed_ReturnsErrClientClosed(t *testing.T) {
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

	targetClient := findClient(srv, "user-send-after-close")
	require.NotNil(t, targetClient)

	targetClient.Close()

	// Wait for Close to take effect (replace time.Sleep with Eventually, P2-08).
	require.Eventually(t, func() bool {
		targetClient.mu.Lock()
		defer targetClient.mu.Unlock()
		return targetClient.closed
	}, 2*time.Second, 50*time.Millisecond)

	// Send after Close should return ErrClientClosed (not panic, not block).
	assert.NotPanics(t, func() {
		err := targetClient.Send([]byte("after-close"))
		assert.ErrorIs(t, err, ErrClientClosed, "Send should return ErrClientClosed after Close")
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

	targetClient := findClient(srv, "user-42")
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

// ---------------------------------------------------------------------------
// Helper: find a server-side Client by userID
// ---------------------------------------------------------------------------

// findClient returns the first server-side Client matching the given userID,
// or nil if not found. The caller should typically wrap this in
// require.NotNil.
func findClient(srv *WebSocketServer, userID string) *Client {
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	for _, c := range srv.clients {
		if c.UserID() == userID {
			return c
		}
	}
	return nil
}

// newClientWithSendBuf creates a Client with a send buffer but nil conn.
// Only for testing sendResponse/sendSuccessResponse/sendErrorResponse and
// other methods that do not touch the underlying WebSocket connection.
func newClientWithSendBuf(t *testing.T, userID string, bufSize int) *Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		userID: userID,
		connID: "test-conn",
		send:   make(chan []byte, bufSize),
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

// ---------------------------------------------------------------------------
// P0: NewClient default constants (D-001: zero-config defaults)
// ---------------------------------------------------------------------------

// TestNewClient_Defaults verifies that NewClient applies the documented
// default constants when no options are provided.
func TestNewClient_Defaults(t *testing.T) {
	c := NewClient(nil, "u1", "device-1", "c1")

	// D-001: zero-config defaults must match the documented constants.
	assert.Equal(t, defaultWriteWait, c.writeWait, "writeWait default (D-001)")
	assert.Equal(t, 10*time.Second, c.writeWait, "writeWait = 10s (D-001)")
	assert.Equal(t, defaultPongWait, c.pongWait, "pongWait default (D-001)")
	assert.Equal(t, 60*time.Second, c.pongWait, "pongWait = 60s (D-001)")
	assert.Equal(t, defaultPingPeriod, c.pingPeriod, "pingPeriod default (D-001)")
	assert.Equal(t, 54*time.Second, c.pingPeriod, "pingPeriod = 54s (D-001)")
	assert.Equal(t, defaultMaxMessageSize, int(c.maxMessageSize), "maxMessageSize default (D-001)")
	assert.Equal(t, int64(64*1024), c.maxMessageSize, "maxMessageSize = 64 KiB (D-001)")

	// The send channel buffer size must equal defaultSendBufSize.
	assert.Equal(t, defaultSendBufSize, cap(c.send), "send buffer capacity (D-001)")
	assert.Equal(t, 256, cap(c.send), "send buffer = 256 (D-001)")

	// Other fields.
	assert.Equal(t, "u1", c.userID)
	assert.Equal(t, "c1", c.connID)
	assert.NotNil(t, c.ctx)
	assert.NotNil(t, c.cancel)
	assert.NotNil(t, c.done)
	assert.False(t, c.closed)
	assert.Nil(t, c.handler)
}

// ---------------------------------------------------------------------------
// P0: NewClient option overrides
// ---------------------------------------------------------------------------

// TestNewClient_WithOptions verifies that each ClientOption correctly
// overrides the corresponding default value.
func TestNewClient_WithOptions(t *testing.T) {
	customHandler := MessageHandlerFunc(func(ctx context.Context, client *Client, pkg *protocol.Package) {})

	c := NewClient(nil, "u2", "device-1", "c2",
		WithWriteWait(5*time.Second),
		WithPongWait(30*time.Second),
		WithPingPeriod(25*time.Second),
		WithMaxMessageSize(1024*1024),
		WithSendBufSize(512),
		WithMessageHandler(customHandler),
	)

	assert.Equal(t, 5*time.Second, c.writeWait, "WithWriteWait override")
	assert.Equal(t, 30*time.Second, c.pongWait, "WithPongWait override")
	assert.Equal(t, 25*time.Second, c.pingPeriod, "WithPingPeriod override")
	assert.Equal(t, int64(1024*1024), c.maxMessageSize, "WithMaxMessageSize override")
	assert.Equal(t, 512, cap(c.send), "WithSendBufSize override")
	// Function values cannot be compared with == in Go; verify the handler
	// was set by checking it is non-nil.
	assert.NotNil(t, c.handler, "WithMessageHandler override")
}

// TestNewClient_WithSendBufSize_Invalid verifies that WithSendBufSize with
// zero or negative values preserves the default buffer size (D-001).
func TestNewClient_WithSendBufSize_Invalid(t *testing.T) {
	// n=0 → keep default
	c0 := NewClient(nil, "u", "device-1", "c", WithSendBufSize(0))
	assert.Equal(t, defaultSendBufSize, cap(c0.send), "WithSendBufSize(0) keeps default (D-001)")

	// n=-1 → keep default
	c1 := NewClient(nil, "u", "device-1", "c", WithSendBufSize(-1))
	assert.Equal(t, defaultSendBufSize, cap(c1.send), "WithSendBufSize(-1) keeps default (D-001)")

	// n=-100 → keep default
	c2 := NewClient(nil, "u", "device-1", "c", WithSendBufSize(-100))
	assert.Equal(t, defaultSendBufSize, cap(c2.send), "WithSendBufSize(-100) keeps default (D-001)")
}

// ---------------------------------------------------------------------------
// P0: marshalPackage / unmarshalPackage round-trip
// ---------------------------------------------------------------------------

// TestMarshalUnmarshalPackage verifies that marshalPackage and
// unmarshalPackage correctly encode and decode all PackageType values, and
// that invalid JSON returns an error.
func TestMarshalUnmarshalPackage(t *testing.T) {
	// Round-trip each PackageType.
	tests := []struct {
		name string
		pkg  *protocol.Package
	}{
		{
			name: "Request",
			pkg: &protocol.Package{
				Type: protocol.PackageTypeRequest,
				Data: json.RawMessage(`{"id":"r1","method":"ping","params":{}}`),
			},
		},
		{
			name: "Response",
			pkg: &protocol.Package{
				Type: protocol.PackageTypeResponse,
				Data: json.RawMessage(`{"id":"r1","code":0,"msg":"ok","data":null}`),
			},
		},
		{
			name: "Updates",
			pkg: &protocol.Package{
				Type: protocol.PackageTypeUpdates,
				Data: json.RawMessage(`{"updates":[{"seq":1,"payload":{}}]}`),
			},
		},
		{
			name: "NilData",
			pkg: &protocol.Package{
				Type: protocol.PackageTypeResponse,
				Data: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := marshalPackage(tt.pkg)
			require.NoError(t, err, "marshalPackage should not fail")
			require.NotEmpty(t, data)

			got, err := unmarshalPackage(data)
			require.NoError(t, err, "unmarshalPackage should not fail")
			assert.Equal(t, tt.pkg.Type, got.Type, "type must match")
			// Nil Data marshals as JSON "null" and unmarshals back as the
			// literal bytes []byte("null"), not as Go nil.
			if tt.pkg.Data == nil {
				assert.Equal(t, json.RawMessage("null"), got.Data, "nil data must round-trip as JSON null")
			} else {
				assert.JSONEq(t, string(tt.pkg.Data), string(got.Data), "data must match")
			}
		})
	}

	// Invalid JSON must return an error.
	t.Run("InvalidJSON", func(t *testing.T) {
		_, err := unmarshalPackage([]byte("not-json{{{"))
		require.Error(t, err, "unmarshalPackage should return error for invalid JSON")
	})
}

// ---------------------------------------------------------------------------
// P0: Run blocks until Close
// ---------------------------------------------------------------------------

// TestClient_Run_BlocksUntilClose verifies that Run blocks the calling
// goroutine until Close is called, and that Done() is closed after Run
// returns.
func TestClient_Run_BlocksUntilClose(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
	)
	defer cleanup()

	wsConn := connectWS(t, addr, "user-run-blocks")
	defer wsConn.Close()

	require.Eventually(t, func() bool {
		return findClient(srv, "user-run-blocks") != nil
	}, 2*time.Second, 50*time.Millisecond)

	client := findClient(srv, "user-run-blocks")
	require.NotNil(t, client)

	// Done must not be closed while the client is alive.
	select {
	case <-client.Done():
		t.Fatal("Done() should not be closed while client is running")
	default:
		// expected
	}

	// Close the client.
	client.Close()

	// Done must close within a reasonable time.
	select {
	case <-client.Done():
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Done() should be closed after Close()")
	}
}

// ---------------------------------------------------------------------------
// P0: Done is closed after both pumps exit
// ---------------------------------------------------------------------------

// TestClient_Done_ClosedAfterBothPumps verifies that the Done channel is
// closed only after both readPump and writePump goroutines have exited.
func TestClient_Done_ClosedAfterBothPumps(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
	)
	defer cleanup()

	wsConn := connectWS(t, addr, "user-done-pumps")
	defer wsConn.Close()

	require.Eventually(t, func() bool {
		return findClient(srv, "user-done-pumps") != nil
	}, 2*time.Second, 50*time.Millisecond)

	client := findClient(srv, "user-done-pumps")
	require.NotNil(t, client)

	// Close the client: this cancels ctx, causing writePump to send a close
	// frame and exit, and readPump to get an error and exit.
	client.Close()

	// Wait for Done. It must close, proving both pumps have exited.
	select {
	case <-client.Done():
		// success: both pumps exited
	case <-time.After(5 * time.Second):
		t.Fatal("Done() should close after both pumps exit")
	}

	// After Done is closed, the client must be marked as closed.
	client.mu.Lock()
	assert.True(t, client.closed, "client must be marked closed after Done")
	client.mu.Unlock()
}

// ---------------------------------------------------------------------------
// P0: readPump calls handler
// ---------------------------------------------------------------------------

// TestClient_ReadPump_HandlerCalled verifies that the message handler is
// invoked for every valid incoming package.
func TestClient_ReadPump_HandlerCalled(t *testing.T) {
	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("echo", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return req.Params, nil
	})

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	wsConn := connectWS(t, addr, "user-handler-called")
	defer wsConn.Close()

	// Send a valid request; the handler should be called and produce a
	// response.
	sendRequestPackage(t, wsConn, "hc-1", "echo", json.RawMessage(`{"v":1}`))

	resp := readResponsePackage(t, wsConn, 3*time.Second)
	assert.Equal(t, "hc-1", resp.ID)
	assert.Equal(t, protocol.ResponseCodeOK, resp.Code)

	var data map[string]int
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, 1, data["v"])
}

// ---------------------------------------------------------------------------
// P0: readPump continues after decode error
// ---------------------------------------------------------------------------

// TestClient_ReadPump_DecodeErrorContinues verifies that a decode error
// (invalid JSON) does not disconnect the client; subsequent valid messages
// are still processed.
func TestClient_ReadPump_DecodeErrorContinues(t *testing.T) {
	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("ping", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"pong":true}`), nil
	})

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	wsConn := connectWS(t, addr, "user-decode-cont")
	defer wsConn.Close()

	// Send invalid JSON (will fail to decode as a protocol.Package).
	err := wsConn.WriteMessage(websocket.TextMessage, []byte("{broken json"))
	require.NoError(t, err)

	// Give the server a moment to process the bad message and log the error.
	time.Sleep(200 * time.Millisecond)

	// Connection must still be alive: send a valid request.
	sendRequestPackage(t, wsConn, "after-bad", "ping", json.RawMessage(`{}`))

	resp := readResponsePackage(t, wsConn, 3*time.Second)
	assert.Equal(t, "after-bad", resp.ID)
	assert.Equal(t, protocol.ResponseCodeOK, resp.Code)
}

// ---------------------------------------------------------------------------
// P0: writePump sends CloseMessage on ctx cancel
// ---------------------------------------------------------------------------

// TestClient_WritePump_CtxCancelSendsCloseFrame verifies that when the
// client context is cancelled (via Close), the writePump exits and the
// underlying connection is closed. Note: Close() cancels ctx and then
// immediately closes the underlying TCP conn, which races with the
// writePump's attempt to send a close frame. The peer may see either a
// proper close frame (1000/1001) or an abnormal closure (1006) depending
// on timing.
func TestClient_WritePump_CtxCancelSendsCloseFrame(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
	)
	defer cleanup()

	wsConn := connectWS(t, addr, "user-close-frame")
	defer wsConn.Close()

	require.Eventually(t, func() bool {
		return findClient(srv, "user-close-frame") != nil
	}, 2*time.Second, 50*time.Millisecond)

	client := findClient(srv, "user-close-frame")
	require.NotNil(t, client)

	// Close the server-side client. This cancels ctx (triggering writePump
	// exit) and closes the underlying conn.
	client.Close()

	// The WS client should see the connection close. Set a read deadline and
	// try to read; the read must fail.
	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err := wsConn.ReadMessage()
	require.Error(t, err, "client should see connection close after server Close()")

	// Verify Done is closed (both pumps exited).
	select {
	case <-client.Done():
		// success: both pumps exited
	case <-time.After(5 * time.Second):
		t.Fatal("Done() should close after Close()")
	}
}

// ---------------------------------------------------------------------------
// P0: Send is safe for concurrent use with Close (-race)
// ---------------------------------------------------------------------------

// TestClient_Send_ConcurrentWithClose verifies that concurrent Send and
// Close calls do not cause data races or panics under the race detector.
// Send errors (ErrSendBufferFull or ErrClientClosed) are expected when
// concurrent Sends race with Close.
func TestClient_Send_ConcurrentWithClose(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
	)
	defer cleanup()

	wsConn := connectWS(t, addr, "user-race-send")
	defer wsConn.Close()

	require.Eventually(t, func() bool {
		return findClient(srv, "user-race-send") != nil
	}, 2*time.Second, 50*time.Millisecond)

	client := findClient(srv, "user-race-send")
	require.NotNil(t, client)

	// Launch multiple goroutines calling Send concurrently.
	const numSenders = 10
	var wg sync.WaitGroup
	for i := 0; i < numSenders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Send may return ErrSendBufferFull or ErrClientClosed during
				// the race with Close; that is expected behaviour.
				_ = client.Send([]byte(fmt.Sprintf("sender-%d-msg-%d", id, j)))
			}
		}(i)
	}

	// Concurrently close the client.
	time.Sleep(1 * time.Millisecond) // let some Sends go through
	assert.NotPanics(t, func() {
		client.Close()
	})

	// Wait for all senders to finish.
	wg.Wait()

	// After Close, Send must return ErrClientClosed (not panic).
	assert.NotPanics(t, func() {
		err := client.Send([]byte("after-close"))
		assert.ErrorIs(t, err, ErrClientClosed, "Send after Close should return ErrClientClosed")
	})
}

// ---------------------------------------------------------------------------
// Client.Send / Client.SendPackage unit tests (Phase 3 error propagation)
// ---------------------------------------------------------------------------

// TestClient_Send_Success verifies that Send returns nil and enqueues the
// message into the send channel when the client is open and the buffer is
// not full.
func TestClient_Send_Success(t *testing.T) {
	c := newClientWithSendBuf(t, "u-send-ok", 10)

	err := c.Send([]byte("hello"))
	assert.NoError(t, err, "Send should succeed on an open client with buffer space")

	// Verify the message was enqueued.
	select {
	case msg := <-c.send:
		assert.Equal(t, []byte("hello"), msg, "enqueued message should match what was sent")
	case <-time.After(2 * time.Second):
		t.Fatal("expected a message in the send channel")
	}
}

// TestClient_SendPackage_Success verifies that SendPackage marshals a
// protocol.Package and enqueues it successfully, returning nil.
func TestClient_SendPackage_Success(t *testing.T) {
	c := newClientWithSendBuf(t, "u-sendpkg-ok", 10)

	pkg := &protocol.Package{
		Type: protocol.PackageTypeUpdates,
		Data: json.RawMessage(`{"seq":42}`),
	}
	err := c.SendPackage(pkg)
	assert.NoError(t, err, "SendPackage should succeed on an open client")

	// Verify the message was enqueued and is valid JSON.
	select {
	case msg := <-c.send:
		var decoded protocol.Package
		require.NoError(t, json.Unmarshal(msg, &decoded))
		assert.Equal(t, protocol.PackageTypeUpdates, decoded.Type)
	case <-time.After(2 * time.Second):
		t.Fatal("expected a message in the send channel")
	}
}

// TestClient_SendPackage_SendFails_ReturnsError verifies that SendPackage
// returns an error when the underlying Send fails (buffer full or client
// closed).
func TestClient_SendPackage_SendFails_ReturnsError(t *testing.T) {
	t.Run("ClientClosed", func(t *testing.T) {
		c := newClientWithSendBuf(t, "u-sendpkg-closed", 10)

		// Close the client.
		c.mu.Lock()
		c.closed = true
		c.cancel()
		c.mu.Unlock()

		pkg := &protocol.Package{
			Type: protocol.PackageTypeUpdates,
			Data: json.RawMessage(`{"seq":1}`),
		}
		err := c.SendPackage(pkg)
		assert.Error(t, err, "SendPackage should return error on closed client")
		assert.ErrorIs(t, err, ErrClientClosed)
	})

	t.Run("BufferFull", func(t *testing.T) {
		// Create a client with buffer size 1.
		c := newClientWithSendBuf(t, "u-sendpkg-full", 1)

		// Fill the buffer.
		pkg1 := &protocol.Package{
			Type: protocol.PackageTypeUpdates,
			Data: json.RawMessage(`{"seq":1}`),
		}
		err := c.SendPackage(pkg1)
		require.NoError(t, err, "first SendPackage should succeed")

		// Second SendPackage should fail because the buffer is full.
		pkg2 := &protocol.Package{
			Type: protocol.PackageTypeUpdates,
			Data: json.RawMessage(`{"seq":2}`),
		}
		err = c.SendPackage(pkg2)
		assert.Error(t, err, "second SendPackage should fail when buffer is full")
		assert.ErrorIs(t, err, ErrSendBufferFull)
	})
}

// TestClient_Send_ConcurrentSendAndClose_RaceFree verifies that concurrent
// Send and Close calls do not panic and are safe under the race detector.
func TestClient_Send_ConcurrentSendAndClose_RaceFree(t *testing.T) {
	c := newClientWithSendBuf(t, "u-race-free", 256)

	const numSenders = 20
	var wg sync.WaitGroup

	// Launch many goroutines that Send concurrently.
	for i := 0; i < numSenders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.Send([]byte(fmt.Sprintf("s-%d-m-%d", id, j)))
			}
		}(i)
	}

	// Concurrently mark client as closed (simulating Close without touching
	// the nil conn).
	time.Sleep(500 * time.Microsecond)
	assert.NotPanics(t, func() {
		c.mu.Lock()
		c.closed = true
		c.cancel()
		c.mu.Unlock()
	})

	wg.Wait()

	// After Close, Send should return ErrClientClosed, not panic.
	assert.NotPanics(t, func() {
		err := c.Send([]byte("post-close"))
		assert.ErrorIs(t, err, ErrClientClosed)
	})
}

// ---------------------------------------------------------------------------
// P1: readPump with nil handler does not panic
// ---------------------------------------------------------------------------

// TestClient_ReadPump_NilHandlerSilentDiscard verifies that a Client with
// handler=nil silently discards incoming messages without panicking.
func TestClient_ReadPump_NilHandlerSilentDiscard(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
	)
	defer cleanup()

	wsConn := connectWS(t, addr, "user-nil-handler")
	defer wsConn.Close()

	require.Eventually(t, func() bool {
		return findClient(srv, "user-nil-handler") != nil
	}, 2*time.Second, 50*time.Millisecond)

	client := findClient(srv, "user-nil-handler")
	require.NotNil(t, client)

	// Set the handler to nil to test the nil-handler code path.
	client.handler = nil

	// Send a valid request. The server should not panic, and the connection
	// should stay alive.
	sendRequestPackage(t, wsConn, "nil-1", "anything", json.RawMessage(`{}`))

	// Give the server a moment to process the message.
	time.Sleep(200 * time.Millisecond)

	// The connection must still be alive — no response expected (handler is
	// nil), but the server must not have closed the connection.
	// Send another valid request to verify.
	sendRequestPackage(t, wsConn, "nil-2", "anything", json.RawMessage(`{}`))
	time.Sleep(200 * time.Millisecond)

	// Verify connection is alive by writing a ping.
	err := wsConn.WriteMessage(websocket.PingMessage, nil)
	assert.NoError(t, err, "connection should still be alive after nil-handler discard")
}

// ---------------------------------------------------------------------------
// P1: writePump batch drain
// ---------------------------------------------------------------------------

// TestClient_WritePump_BatchDrain verifies that when multiple messages are
// queued in the send channel, writePump drains them efficiently. Due to
// the batch drain optimization, multiple JSON objects may be concatenated
// into a single WebSocket text frame. This test verifies all messages
// eventually arrive by reading raw frames and counting JSON objects.
func TestClient_WritePump_BatchDrain(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
		WSWithPongWait(30*time.Second), // prevent readPump timeout
	)
	defer cleanup()

	wsConn := connectWS(t, addr, "user-batch-drain")
	defer wsConn.Close()

	require.Eventually(t, func() bool {
		return findClient(srv, "user-batch-drain") != nil
	}, 2*time.Second, 50*time.Millisecond)

	client := findClient(srv, "user-batch-drain")
	require.NotNil(t, client)

	// Enqueue multiple messages rapidly so writePump batches them.
	const numMsgs = 5
	for i := 0; i < numMsgs; i++ {
		pkg := &protocol.Package{
			Type: protocol.PackageTypeUpdates,
			Data: json.RawMessage(fmt.Sprintf(`{"seq":%d}`, i+1)),
		}
		err := client.SendPackage(pkg)
		require.NoError(t, err)
	}

	// Read raw WS frames and count the total number of JSON objects (and
	// verify each has the correct type). The batch drain may concatenate
	// multiple JSON objects into one frame.
	receivedCount := 0
	_ = wsConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for receivedCount < numMsgs {
		_, rawMsg, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		// Parse concatenated JSON objects from this frame.
		dec := json.NewDecoder(bytes.NewReader(rawMsg))
		for dec.More() {
			var pkg protocol.Package
			if err := dec.Decode(&pkg); err != nil {
				break
			}
			assert.Equal(t, protocol.PackageTypeUpdates, pkg.Type)
			receivedCount++
		}
	}
	assert.Equal(t, numMsgs, receivedCount, "all batched messages should be received")
}

// ---------------------------------------------------------------------------
// P1: Close idempotency — flag and context behaviour
// ---------------------------------------------------------------------------

// TestClient_Close_IdempotentUnit verifies the internal state transitions
// that Close triggers: the closed flag is set and the context is cancelled.
// Calling Close on a Client with a nil conn would panic (conn.Close), so
// this test directly sets the closed flag and cancels the context to
// exercise the guard logic, verifying that the second "close" would be a
// no-op. The full idempotency test with a real connection is covered by
// TestClient_CloseIdempotent.
func TestClient_Close_IdempotentUnit(t *testing.T) {
	c := NewClient(nil, "u", "device-1", "c")

	// Simulate what Close does internally (without touching conn).
	c.mu.Lock()
	require.False(t, c.closed, "client should not be closed initially")
	c.closed = true
	c.cancel()
	c.mu.Unlock()

	// Verify state after first "close".
	c.mu.Lock()
	assert.True(t, c.closed, "client must be marked closed")
	c.mu.Unlock()
	select {
	case <-c.ctx.Done():
		// expected
	default:
		t.Fatal("context should be cancelled after close")
	}

	// Simulate second Close: the guard check (c.closed == true) should
	// cause an early return. We verify this by checking that the closed
	// flag is still true and ctx is still cancelled — nothing changed.
	c.mu.Lock()
	wasClosed := c.closed
	c.mu.Unlock()
	assert.True(t, wasClosed, "idempotent: second close sees closed=true")

	// Context should still be cancelled (no re-cancel needed).
	select {
	case <-c.ctx.Done():
		// expected
	default:
		t.Fatal("context should still be cancelled")
	}
}

// ---------------------------------------------------------------------------
// P1: SendPackage returns marshal error
// ---------------------------------------------------------------------------

// TestClient_SendPackage_Enqueue verifies that SendPackage marshals a
// protocol.Package and enqueues it into the client's send channel. Since
// protocol.Package uses json.RawMessage for Data (which is []byte and always
// marshals successfully), a true marshal error is essentially unreachable
// with the current struct layout. This test verifies the happy path and
// confirms the message is correctly enqueued.
func TestClient_SendPackage_Enqueue(t *testing.T) {
	c := newClientWithSendBuf(t, "u", 10)

	// Valid package — should marshal and enqueue successfully.
	validPkg := &protocol.Package{
		Type: protocol.PackageTypeUpdates,
		Data: json.RawMessage(`{"seq":1}`),
	}
	err := c.SendPackage(validPkg)
	assert.NoError(t, err, "SendPackage should succeed for valid package")

	// Verify the message was enqueued.
	select {
	case msg := <-c.send:
		assert.NotEmpty(t, msg)
		// Verify the enqueued data is valid JSON containing the package.
		var decoded protocol.Package
		require.NoError(t, json.Unmarshal(msg, &decoded))
		assert.Equal(t, protocol.PackageTypeUpdates, decoded.Type)
	default:
		t.Fatal("SendPackage should have enqueued a message")
	}
}

// ---------------------------------------------------------------------------
// P1: readPump closes connection on oversized message
// ---------------------------------------------------------------------------

// TestClient_ReadPump_MessageExceedsLimit verifies that an incoming message
// exceeding MaxMessageSize causes the connection to be closed.
func TestClient_ReadPump_MessageExceedsLimit(t *testing.T) {
	const smallLimit = 256 // 256 bytes

	_, addr, cleanup := startWSServerMem(t,
		WSWithMaxMessageSize(smallLimit),
		WSWithPingPeriod(1*time.Hour),
	)
	defer cleanup()

	wsConn := connectWS(t, addr, "user-oversize")
	defer wsConn.Close()

	// Send a message that exceeds the limit.
	bigMsg := make([]byte, smallLimit+1024)
	for i := range bigMsg {
		bigMsg[i] = 'A'
	}

	// Wrap it in a valid protocol.Package JSON envelope so it's a text
	// message that exceeds the limit after JSON parsing.
	envelope := fmt.Sprintf(`{"type":0,"data":"%s"}`, string(bigMsg))
	err := wsConn.WriteMessage(websocket.TextMessage, []byte(envelope))
	require.NoError(t, err)

	// The server should close the connection because the message exceeds
	// MaxMessageSize. The client read should fail.
	_ = wsConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = wsConn.ReadMessage()
	assert.Error(t, err, "connection should be closed after oversized message")
}

// ---------------------------------------------------------------------------
// websocket_handler.go tests — P0
// ---------------------------------------------------------------------------

// TestHandleRequest_InvalidJSON verifies that sending a PackageTypeRequest
// with invalid JSON in Data produces an error response with code=-1 and an
// empty ID (D-017: the request ID is unavailable when parsing fails).
func TestHandleRequest_InvalidJSON(t *testing.T) {
	handler := NewDefaultMessageHandler()

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-invalid-json")
	defer conn.Close()

	// Send a Package with PackageTypeRequest but Data that is not valid JSON
	// for PackageDataRequest. The outer JSON must be valid so readPump can
	// decode the Package; only the inner Data must fail to unmarshal.
	rawWS := `{"type":0,"data":"{not valid json"}`
	err := conn.WriteMessage(websocket.TextMessage, []byte(rawWS))
	require.NoError(t, err)

	resp := readResponsePackage(t, conn, 3*time.Second)
	// (D-017) ID must be empty string when request cannot be parsed.
	assert.Equal(t, "", resp.ID, "ID must be empty string when JSON is invalid (D-017)")
	assert.Equal(t, protocol.ResponseCodeError, resp.Code, "code must be -1 (D-017)")
	assert.Equal(t, "invalid request data", resp.Msg)
}

// TestHandleRequest_UnknownMethod verifies that a request for an unregistered
// method returns an error response containing the original request ID (D-017).
func TestHandleRequest_UnknownMethod(t *testing.T) {
	handler := NewDefaultMessageHandler()
	// No methods registered.

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-unknown-meth")
	defer conn.Close()

	sendRequestPackage(t, conn, "req-unk-1", "nonexistent_method", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	// (D-017) req.ID must be echoed back even for unknown method errors.
	assert.Equal(t, "req-unk-1", resp.ID, "request ID must be preserved (D-017)")
	assert.Equal(t, protocol.ResponseCodeError, resp.Code, "code must be -1 (D-017)")
	assert.Contains(t, resp.Msg, "unknown method: nonexistent_method")
}

// TestHandleRequest_HandlerReturnsHandlerError verifies that a HandlerError
// returned by a MethodHandler has its Code and Message transparently passed
// through to the client response (D-017).
func TestHandleRequest_HandlerReturnsHandlerError(t *testing.T) {
	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("fail_typed", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return nil, protocol.NewHandlerError(protocol.ResponseCodeNotFound, "conversation not found")
	})

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-handler-err-typed")
	defer conn.Close()

	sendRequestPackage(t, conn, "req-te-1", "fail_typed", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	// (D-017) HandlerError Code and Message must be transparently forwarded.
	assert.Equal(t, "req-te-1", resp.ID, "request ID must be preserved (D-017)")
	assert.Equal(t, protocol.ResponseCodeNotFound, resp.Code, "HandlerError.Code must be forwarded (D-017)")
	assert.Equal(t, "conversation not found", resp.Msg, "HandlerError.Message must be forwarded (D-017)")
}

// TestHandleRequest_HandlerError_WithWrappedInnerError verifies that
// errors.As correctly extracts the HandlerError Code even when the error is
// wrapped with fmt.Errorf (D-017).
func TestHandleRequest_HandlerError_WithWrappedInnerError(t *testing.T) {
	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("fail_wrapped", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		inner := protocol.NewHandlerError(protocol.ResponseCodePermissionDenied, "access denied")
		// Wrap the HandlerError: errors.As should still extract it.
		return nil, fmt.Errorf("operation failed: %w", inner)
	})

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-wrapped-err")
	defer conn.Close()

	sendRequestPackage(t, conn, "req-we-1", "fail_wrapped", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	// (D-017) errors.As must extract HandlerError.Code from wrapped errors.
	assert.Equal(t, "req-we-1", resp.ID, "request ID must be preserved (D-017)")
	assert.Equal(t, protocol.ResponseCodePermissionDenied, resp.Code, "wrapped HandlerError.Code must be extracted via errors.As (D-017)")
	assert.Contains(t, resp.Msg, "access denied", "wrapped HandlerError.Message must be extracted (D-017)")
}

// TestDefaultMessageHandler_UnknownPackageType verifies that a Package with
// a type value that is not Request/Response/Updates hits the default branch
// (logged) and does not produce any response or panic.
func TestDefaultMessageHandler_UnknownPackageType(t *testing.T) {
	handler := NewDefaultMessageHandler()

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-unknown-pkg-type")
	defer conn.Close()

	// Send a package with an invalid type (99).
	pkg := protocol.Package{
		Type: protocol.PackageType(99),
		Data: json.RawMessage(`{}`),
	}
	data, err := json.Marshal(pkg)
	require.NoError(t, err)
	err = conn.WriteMessage(websocket.TextMessage, data)
	require.NoError(t, err)

	// The server must not send any response for unknown package types.
	// Verify the connection is still alive by sending a valid request
	// afterwards. The unknown-method response proves the connection survived.
	time.Sleep(200 * time.Millisecond)

	sendRequestPackage(t, conn, "still-alive-unknown-type", "anything", json.RawMessage(`{}`))
	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "still-alive-unknown-type", resp.ID, "connection must survive unknown package type")
}

// ---------------------------------------------------------------------------
// websocket_handler.go tests — P1
// ---------------------------------------------------------------------------

// TestDefaultMessageHandler_ResponsePackageType_Ignored verifies that a
// PackageTypeResponse received from the client is logged and ignored (no
// handler invocation, no response sent back).
func TestDefaultMessageHandler_ResponsePackageType_Ignored(t *testing.T) {
	handler := NewDefaultMessageHandler()
	handlerCalled := false
	handler.RegisterMethodFunc("should_not_be_called", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		handlerCalled = true
		return json.RawMessage(`{}`), nil
	})

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-resp-ignored")
	defer conn.Close()

	// Send a PackageTypeResponse to the server.
	respPkg := protocol.Package{
		Type: protocol.PackageTypeResponse,
		Data: json.RawMessage(`{"id":"x","code":0,"msg":"ok","data":null}`),
	}
	data, err := json.Marshal(respPkg)
	require.NoError(t, err)
	err = conn.WriteMessage(websocket.TextMessage, data)
	require.NoError(t, err)

	// Wait for processing.
	time.Sleep(200 * time.Millisecond)

	// The registered method handler must NOT have been called.
	assert.False(t, handlerCalled, "method handler must not be invoked for PackageTypeResponse")

	// Connection must still be alive.
	sendRequestPackage(t, conn, "after-resp-pkg", "should_not_be_called", json.RawMessage(`{}`))
	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "after-resp-pkg", resp.ID)
	// The method handler should be called now (from the valid request).
	assert.True(t, handlerCalled, "method handler must be called for a valid request after ignored response package")
}

// TestDefaultMessageHandler_UpdatesPackageType_Ignored verifies that a
// PackageTypeUpdates received from the client is logged and ignored (no
// handler invocation, no response sent back).
func TestDefaultMessageHandler_UpdatesPackageType_Ignored(t *testing.T) {
	handler := NewDefaultMessageHandler()
	handlerCalled := false
	handler.RegisterMethodFunc("should_not_be_called", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		handlerCalled = true
		return json.RawMessage(`{}`), nil
	})

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-updates-ignored")
	defer conn.Close()

	// Send a PackageTypeUpdates to the server.
	updatesPkg := protocol.Package{
		Type: protocol.PackageTypeUpdates,
		Data: json.RawMessage(`{"updates":[]}`),
	}
	data, err := json.Marshal(updatesPkg)
	require.NoError(t, err)
	err = conn.WriteMessage(websocket.TextMessage, data)
	require.NoError(t, err)

	// Wait for processing.
	time.Sleep(200 * time.Millisecond)

	// The registered method handler must NOT have been called.
	assert.False(t, handlerCalled, "method handler must not be invoked for PackageTypeUpdates")

	// Connection must still be alive.
	sendRequestPackage(t, conn, "after-updates-pkg", "should_not_be_called", json.RawMessage(`{}`))
	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "after-updates-pkg", resp.ID)
	assert.True(t, handlerCalled, "method handler must be called for a valid request after ignored updates package")
}

// TestHandleRequest_HandlerReturnsPlainError verifies that a plain error
// (not a HandlerError) from a MethodHandler results in code=-1 with the
// error message as the Msg field.
func TestHandleRequest_HandlerReturnsPlainError(t *testing.T) {
	handler := NewDefaultMessageHandler()
	handler.RegisterMethodFunc("fail_plain", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return nil, fmt.Errorf("something went wrong")
	})

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-plain-err")
	defer conn.Close()

	sendRequestPackage(t, conn, "req-pe-1", "fail_plain", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, "req-pe-1", resp.ID)
	assert.Equal(t, protocol.ResponseCodeError, resp.Code, "plain error must use generic code -1")
	assert.Equal(t, "something went wrong", resp.Msg, "plain error message must be forwarded verbatim")
}

// TestDefaultMessageHandler_Fallback_InvokedOnUnknown verifies that a
// fallback handler set via SetFallback is invoked when a request method is
// not registered (unit-level, using startWSServerMem).
func TestDefaultMessageHandler_Fallback_InvokedOnUnknown(t *testing.T) {
	handler := NewDefaultMessageHandler()
	fallbackCalled := false
	handler.SetFallback(MethodHandlerFunc(
		func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
			fallbackCalled = true
			return json.RawMessage(`{"fallback":"yes"}`), nil
		},
	))

	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-fallback-unit")
	defer conn.Close()

	sendRequestPackage(t, conn, "fb-u-1", "completely_unknown", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.True(t, fallbackCalled, "fallback handler must be invoked for unknown method")
	assert.Equal(t, "fb-u-1", resp.ID)
	assert.Equal(t, protocol.ResponseCodeOK, resp.Code)
}

// TestSendSuccessResponse verifies that sendSuccessResponse marshals and
// sends a correct PackageTypeResponse with code=0 via the client's send
// channel, using a Client with no real WebSocket connection.
func TestSendSuccessResponse(t *testing.T) {
	c := newClientWithSendBuf(t, "u-success", 10)

	respData := json.RawMessage(`{"key":"value"}`)
	err := sendSuccessResponse(c, "id-1", respData)
	require.NoError(t, err)

	// Read the enqueued message from the send channel.
	select {
	case raw := <-c.send:
		var pkg protocol.Package
		require.NoError(t, json.Unmarshal(raw, &pkg))
		assert.Equal(t, protocol.PackageTypeResponse, pkg.Type)

		var resp protocol.PackageDataResponse
		require.NoError(t, json.Unmarshal(pkg.Data, &resp))
		assert.Equal(t, "id-1", resp.ID)
		assert.Equal(t, protocol.ResponseCodeOK, resp.Code)
		assert.Equal(t, "ok", resp.Msg)
		assert.JSONEq(t, `{"key":"value"}`, string(resp.Data))
	case <-time.After(2 * time.Second):
		t.Fatal("expected a message in the send channel")
	}
}

// TestSendErrorResponse verifies that sendErrorResponse marshals and sends a
// correct PackageTypeResponse with the given code and message.
func TestSendErrorResponse(t *testing.T) {
	c := newClientWithSendBuf(t, "u-error", 10)

	err := sendErrorResponse(c, "id-err", protocol.ResponseCodeNotFound, "not found")
	require.NoError(t, err)

	select {
	case raw := <-c.send:
		var pkg protocol.Package
		require.NoError(t, json.Unmarshal(raw, &pkg))
		assert.Equal(t, protocol.PackageTypeResponse, pkg.Type)

		var resp protocol.PackageDataResponse
		require.NoError(t, json.Unmarshal(pkg.Data, &resp))
		assert.Equal(t, "id-err", resp.ID)
		assert.Equal(t, protocol.ResponseCodeNotFound, resp.Code, "(D-017) structured error code must be forwarded")
		assert.Equal(t, "not found", resp.Msg)
	case <-time.After(2 * time.Second):
		t.Fatal("expected a message in the send channel")
	}
}

// TestSendResponse_AfterClientClose verifies that calling sendResponse on a
// closed client does not panic and returns ErrClientClosed (propagated from
// Send through SendPackage).
func TestSendResponse_AfterClientClose(t *testing.T) {
	c := newClientWithSendBuf(t, "u-closed", 10)

	// Close the client by setting the closed flag and cancelling context.
	c.mu.Lock()
	c.closed = true
	c.cancel()
	c.mu.Unlock()

	// sendResponse after close should not panic and should return an error.
	assert.NotPanics(t, func() {
		err := sendSuccessResponse(c, "id-after-close", json.RawMessage(`{}`))
		assert.Error(t, err, "sendSuccessResponse after close should return an error")
	})

	assert.NotPanics(t, func() {
		err := sendErrorResponse(c, "id-after-close", protocol.ResponseCodeError, "test")
		assert.Error(t, err, "sendErrorResponse after close should return an error")
	})
}

// ---------------------------------------------------------------------------
// websocket_handler.go tests — P2
// ---------------------------------------------------------------------------

// TestMessageHandlerFunc_Adapter verifies that MessageHandlerFunc adapts an
// ordinary function into a MessageHandler.
func TestMessageHandlerFunc_Adapter(t *testing.T) {
	called := false
	var gotClient *Client
	var gotPkg *protocol.Package

	fn := MessageHandlerFunc(func(ctx context.Context, client *Client, pkg *protocol.Package) {
		called = true
		gotClient = client
		gotPkg = pkg
	})

	c := newClientWithSendBuf(t, "u-adapter", 1)
	pkg := &protocol.Package{Type: protocol.PackageTypeRequest, Data: json.RawMessage(`{}`)}
	fn.HandleMessage(context.Background(), c, pkg)

	assert.True(t, called, "adapter function must be called")
	assert.Equal(t, c, gotClient, "client must be forwarded")
	assert.Equal(t, pkg, gotPkg, "package must be forwarded")
}

// TestMethodHandlerFunc_Adapter verifies that MethodHandlerFunc adapts an
// ordinary function into a MethodHandler.
func TestMethodHandlerFunc_Adapter(t *testing.T) {
	called := false
	expectedData := json.RawMessage(`{"result":true}`)

	fn := MethodHandlerFunc(func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		called = true
		return expectedData, nil
	})

	c := newClientWithSendBuf(t, "u-method-adapter", 1)
	req := &protocol.PackageDataRequest{ID: "a1", Method: "test"}
	result, err := fn.HandleRequest(context.Background(), c, req)

	assert.True(t, called, "adapter function must be called")
	require.NoError(t, err)
	assert.Equal(t, expectedData, result)
}

// TestDefaultMessageHandler_RegisterMethod_Overwrite verifies at the unit
// level that re-registering a method name replaces the previous handler.
func TestDefaultMessageHandler_RegisterMethod_Overwrite(t *testing.T) {
	handler := NewDefaultMessageHandler()

	// Register the initial handler.
	handler.RegisterMethodFunc("ow", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"v":"old"}`), nil
	})

	// Overwrite.
	handler.RegisterMethodFunc("ow", func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"v":"new"}`), nil
	})

	// Verify by invoking HandleMessage through a real WS connection.
	_, addr, cleanup := startWSServerMem(t, WSWithMessageHandler(handler))
	defer cleanup()

	conn := connectWS(t, addr, "user-overwrite-unit")
	defer conn.Close()

	sendRequestPackage(t, conn, "ow-1", "ow", json.RawMessage(`{}`))

	resp := readResponsePackage(t, conn, 3*time.Second)
	assert.Equal(t, protocol.ResponseCodeOK, resp.Code)

	var data map[string]string
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, "new", data["v"], "overwritten handler must be invoked")
}

// ---------------------------------------------------------------------------
// D-018: NodeBroadcaster integration tests
// ---------------------------------------------------------------------------

// mockNodeBroadcaster is a test double that records Publish calls and allows
// injecting a Subscribe callback for testing handleRemoteBroadcast.
type mockNodeBroadcaster struct {
	mu            sync.Mutex
	publishCalls  []mockPublishCall
	subscribeFunc func(ctx context.Context, callback func(string, *protocol.PackageDataUpdates, string)) error
	closeCalled   bool
}

type mockPublishCall struct {
	UserID       string
	Updates      *protocol.PackageDataUpdates
	SourceNodeID string
}

func (m *mockNodeBroadcaster) Publish(ctx context.Context, userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishCalls = append(m.publishCalls, mockPublishCall{
		UserID:       userID,
		Updates:      updates,
		SourceNodeID: sourceNodeID,
	})
	return nil
}

func (m *mockNodeBroadcaster) Subscribe(ctx context.Context, callback func(string, *protocol.PackageDataUpdates, string)) error {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, callback)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockNodeBroadcaster) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return nil
}

func (m *mockNodeBroadcaster) getPublishCalls() []mockPublishCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mockPublishCall(nil), m.publishCalls...)
}

// TestBroadcastUpdates_CallsNodeBroadcaster verifies that BroadcastUpdates
// calls NodeBroadcaster.Publish after local broadcast (D-018).
func TestBroadcastUpdates_CallsNodeBroadcaster(t *testing.T) {
	mockNB := &mockNodeBroadcaster{}
	cs := NewMemoryConnectionStore(0)

	srv, addr, cleanup := startWSServer(t, cs, WSWithNodeBroadcaster(mockNB))
	defer cleanup()

	conn := connectWS(t, addr, "user-nb-publish")
	defer conn.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{"event":"test"}`)},
		},
	}
	err := srv.BroadcastUpdates("user-nb-publish", updates)
	require.NoError(t, err)

	// Verify NodeBroadcaster.Publish was called.
	calls := mockNB.getPublishCalls()
	require.Len(t, calls, 1, "NodeBroadcaster.Publish should be called once (D-018)")
	assert.Equal(t, "user-nb-publish", calls[0].UserID)
	assert.Equal(t, updates, calls[0].Updates)
	// SourceNodeID should be the server's own node ID.
	assert.NotEmpty(t, calls[0].SourceNodeID, "sourceNodeID must be non-empty (D-018)")
	assert.Equal(t, srv.nodeID, calls[0].SourceNodeID, "sourceNodeID must match server nodeID (D-018)")

	// The local client should also receive the update.
	pkg := readPackage(t, conn, 3*time.Second)
	assert.Equal(t, protocol.PackageTypeUpdates, pkg.Type)
}

// TestHandleRemoteBroadcast_SkipsOwnNode verifies that handleRemoteBroadcast
// skips messages originated by this node to avoid duplicate delivery (D-018).
func TestHandleRemoteBroadcast_SkipsOwnNode(t *testing.T) {
	mockNB := &mockNodeBroadcaster{}
	cs := NewMemoryConnectionStore(0)

	srv, addr, cleanup := startWSServer(t, cs, WSWithNodeBroadcaster(mockNB))
	defer cleanup()

	conn := connectWS(t, addr, "user-skip-self")
	defer conn.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{"event":"self"}`)},
		},
	}

	// Call handleRemoteBroadcast with this node's own ID.
	srv.handleRemoteBroadcast("user-skip-self", updates, srv.nodeID)

	// The client should NOT receive any update (message was skipped).
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err := conn.ReadMessage()
	assert.Error(t, err, "client should not receive update from own node (D-018)")
}

// TestHandleRemoteBroadcast_DeliversFromOtherNode verifies that
// handleRemoteBroadcast delivers updates from other nodes (D-018).
func TestHandleRemoteBroadcast_DeliversFromOtherNode(t *testing.T) {
	mockNB := &mockNodeBroadcaster{}
	cs := NewMemoryConnectionStore(0)

	srv, addr, cleanup := startWSServer(t, cs, WSWithNodeBroadcaster(mockNB))
	defer cleanup()

	conn := connectWS(t, addr, "user-other-node")
	defer conn.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 42, Payload: json.RawMessage(`{"event":"remote"}`)},
		},
	}

	// Call handleRemoteBroadcast with a different node ID.
	srv.handleRemoteBroadcast("user-other-node", updates, "different-node-id")

	// The client SHOULD receive the update.
	pkg := readPackage(t, conn, 3*time.Second)
	assert.Equal(t, protocol.PackageTypeUpdates, pkg.Type)

	var upd protocol.PackageDataUpdates
	require.NoError(t, json.Unmarshal(pkg.Data, &upd))
	require.Len(t, upd.Updates, 1)
	assert.Equal(t, uint32(42), upd.Updates[0].Seq)
}

// ---------------------------------------------------------------------------
// Phase 3 integration tests — device replacement does not cancel replacement's
// pending ReverseRPC requests
// ---------------------------------------------------------------------------

// TestHandleWebSocket_DeviceReplacement_DoesNotCancelReplacementPendingRPC
// verifies that when a device replacement occurs:
//  1. The old connection's pending ServerRequest receives "device replaced".
//  2. After old connection cleanup, a new ServerRequest to the replacement
//     connection remains pending (not cancelled by old connection's cleanup).
func TestHandleWebSocket_DeviceReplacement_DoesNotCancelReplacementPendingRPC(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
		WSWithPongWait(30*time.Second),
	)
	defer cleanup()

	const userID = "user-replace"
	const deviceID = "device-replace"

	// Step 1: Connect A with (userID, deviceID).
	connA := connectWSWithDevice(t, addr, userID, deviceID)

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	// Step 2: Launch a ServerRequest to deviceID; client A will NOT respond.
	type requestResult struct {
		resp *protocol.PackageDataResponse
		err  error
	}
	resultChA := make(chan requestResult, 1)
	go func() {
		resp, err := srv.ServerRequest(
			context.Background(), userID, deviceID, "ping", nil, 15*time.Second,
		)
		resultChA <- requestResult{resp: resp, err: err}
	}()

	// Wait for client A to receive the server request.
	_ = connA.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err := connA.ReadMessage()
	require.NoError(t, err, "client A should receive the server request")

	// Step 3: Connect B with the same (userID, deviceID) — triggers device
	// replacement.
	connB := connectWSWithDevice(t, addr, userID, deviceID)

	// Step 4: Old connection A's pending request must be cancelled.
	// Depending on timing, either "device replaced" (from connB's CancelDevice
	// call) or "device disconnected" (from connA's cleanup path, if connA
	// finishes before connB calls CancelDevice) is acceptable.
	select {
	case result := <-resultChA:
		if result.err != nil {
			// Rare race: ctx.Done() may win first.
			assert.ErrorIs(t, result.err, context.Canceled)
		} else {
			require.NotNil(t, result.resp)
			assert.Equal(t, protocol.ResponseCode(-1), result.resp.Code)
			assert.Contains(t, result.resp.Msg, "device",
				"reason should be 'device replaced' or 'device disconnected'")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old connection A's pending ServerRequest was not cancelled after device replacement")
	}

	// Step 5: Launch a second ServerRequest to the new connection B.
	resultChB := make(chan requestResult, 1)
	go func() {
		resp, err := srv.ServerRequest(
			context.Background(), userID, deviceID, "ping2", nil, 15*time.Second,
		)
		resultChB <- requestResult{resp: resp, err: err}
	}()

	// Wait for client B to receive the server request.
	_ = connB.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = connB.ReadMessage()
	require.NoError(t, err, "client B should receive the server request")

	// Step 6: Wait for old connection A cleanup to complete (it should detect
	// hasActiveConn=true because B is already registered, and skip cancelling).
	time.Sleep(500 * time.Millisecond)

	// Step 7: Verify that connection B's ServerRequest is still pending (not
	// cancelled by old connection A's cleanup).
	select {
	case result := <-resultChB:
		t.Fatalf("new connection B's ServerRequest should still be pending, but got: resp=%v, err=%v",
			result.resp, result.err)
	case <-time.After(1 * time.Second):
		// Expected: still pending.
	}

	// Clean up.
	connB.Close()
	connA.Close()
}

// ---------------------------------------------------------------------------
// Phase 3 integration tests — normal disconnect cancels pending ReverseRPC
// ---------------------------------------------------------------------------

// TestHandleWebSocket_NormalDisconnect_CancelsPendingReverseRPC verifies that
// when a client disconnects normally, all pending ReverseRPC requests for that
// device are immediately cancelled with "device disconnected" as the reason.
func TestHandleWebSocket_NormalDisconnect_CancelsPendingReverseRPC(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),  // disable pings to avoid interference
		WSWithPongWait(30*time.Second), // prevent readPump timeout
	)
	defer cleanup()

	const userID = "user-integ"
	const deviceID = "device-integ"

	// Connect a client with a known deviceID.
	conn := connectWSWithDevice(t, addr, userID, deviceID)

	// Wait for the server to register the client.
	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	// Launch a ServerRequest in a goroutine. The client will NOT respond,
	// so this request will remain pending until the client disconnects.
	type requestResult struct {
		resp *protocol.PackageDataResponse
		err  error
	}
	resultCh := make(chan requestResult, 1)
	go func() {
		resp, err := srv.ServerRequest(
			context.Background(), userID, deviceID, "ping", nil, 10*time.Second,
		)
		resultCh <- requestResult{resp: resp, err: err}
	}()

	// Wait for the server request to be sent (the client should receive it).
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err := conn.ReadMessage()
	require.NoError(t, err, "client should receive the server request")

	// Now disconnect the client normally (close the WebSocket connection).
	conn.Close()

	// The pending ServerRequest should return promptly with a cancellation
	// response containing "device disconnected" as the reason.
	select {
	case result := <-resultCh:
		if result.err != nil {
			// Rare race: ctx.Done() may win first.
			assert.ErrorIs(t, result.err, context.Canceled)
		} else {
			require.NotNil(t, result.resp)
			assert.Equal(t, protocol.ResponseCode(-1), result.resp.Code)
			assert.Equal(t, "device disconnected", result.resp.Msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending ServerRequest was not cancelled after client disconnect")
	}
}
