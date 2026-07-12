package e2e_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// RRPC-001: TestReverseRPC_E2E_RRPC_001_BasicRoundTrip
// Verifies: Server sends "ping" -> client responds -> server receives reply
// ---------------------------------------------------------------------------

// TestReverseRPC_E2E_RRPC_001_BasicRoundTrip verifies the happy path of the
// Reverse RPC flow: the server sends a request to a connected client, the
// client manually constructs a response, and the server receives it.
func TestReverseRPC_E2E_RRPC_001_BasicRoundTrip(t *testing.T) {
	env := setupE2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	userID := "user-rrpc-001"
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()

	// Wait for connection registration.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Goroutine reads the server Request and manually responds.
	go func() {
		mt, data, err := conn.recv(5 * time.Second)
		require.NoError(t, err)
		require.Equal(t, websocket.TextMessage, mt, "message type should be TextMessage")

		// Parse the outer Package.
		var pkg protocol.Package
		require.NoError(t, json.Unmarshal(data, &pkg), "unmarshal package should succeed")
		require.Equal(t, protocol.PackageTypeRequest, pkg.Type, "package type should be Request")

		// Parse the inner PackageDataRequest.
		var req protocol.PackageDataRequest
		require.NoError(t, json.Unmarshal(pkg.Data, &req), "unmarshal request should succeed")
		assert.Equal(t, "ping", req.Method, "method should be 'ping'")
		assert.True(t, strings.HasPrefix(req.ID, "s-"), "request ID should have 's-' prefix")

		// Construct a response with the same ID.
		respPayload, err := json.Marshal(map[string]string{"reply": "pong"})
		require.NoError(t, err, "marshal response payload should succeed")

		resp := protocol.PackageDataResponse{
			ID:   req.ID,
			Code: protocol.ResponseCodeOK,
			Data: respPayload,
		}
		respData, err := json.Marshal(resp)
		require.NoError(t, err, "marshal response should succeed")

		respPkg := protocol.Package{
			Type: protocol.PackageTypeResponse,
			Data: respData,
		}
		respPkgData, err := json.Marshal(respPkg)
		require.NoError(t, err, "marshal response package should succeed")

		err = conn.WriteMessage(websocket.TextMessage, respPkgData)
		require.NoError(t, err, "write response should succeed")
	}()

	// Server sends request via ServerRequest.
	resp, err := env.srv.ServerRequest(context.Background(), userID, "device-1", "ping", nil, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	var result map[string]string
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.Equal(t, "pong", result["reply"])
}

// ---------------------------------------------------------------------------
// RRPC-002: TestReverseRPC_E2E_RRPC_002_Timeout
// Verifies: Client does not respond -> server times out
// ---------------------------------------------------------------------------

// TestReverseRPC_E2E_RRPC_002_Timeout verifies that when the server sends a
// request but the client does not respond, the ServerRequest call returns a
// timeout error (context.DeadlineExceeded).
func TestReverseRPC_E2E_RRPC_002_Timeout(t *testing.T) {
	env := setupE2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	userID := "user-rrpc-002"
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()

	// Wait for connection registration.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Drain the incoming request from the client side so it does not block
	// the reader goroutine, but do NOT send a response.
	go func() {
		_, _, _ = conn.recv(5 * time.Second)
	}()

	// Server sends request with short timeout; client does NOT respond.
	_, err := env.srv.ServerRequest(context.Background(), userID, "device-1", "ping", nil, 200*time.Millisecond)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded,
		"timeout error should be context.DeadlineExceeded")
}

// ---------------------------------------------------------------------------
// RRPC-003: TestReverseRPC_E2E_RRPC_003_NoConnection
// Verifies: Request to a non-existent user returns "offline" error
// ---------------------------------------------------------------------------

// TestReverseRPC_E2E_RRPC_003_NoConnection verifies that sending a request to
// a user with no active connections returns an error containing "offline"
// immediately, without waiting for the timeout.
func TestReverseRPC_E2E_RRPC_003_NoConnection(t *testing.T) {
	env := setupE2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	// No client connected for this user.
	start := time.Now()
	_, err := env.srv.ServerRequest(context.Background(), "nonexistent-user", "device-1", "ping", nil, 5*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "offline",
		"error should mention 'offline' (device offline when user has no connections)")

	// Should return almost immediately, not wait for the 5s timeout.
	assert.Less(t, elapsed, 2*time.Second,
		"should return quickly when user is offline, not wait for timeout")
}
