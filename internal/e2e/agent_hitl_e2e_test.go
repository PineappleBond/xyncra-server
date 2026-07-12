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
// AE_HITL_E2E_001: TestAgentHITL_E2E_AE_HITL_E2E_001_UserApproves
// Verifies: Server sends "hitl.request_approval" -> client responds with
//
//	{approved: true} -> server receives the approval response.
//
// ---------------------------------------------------------------------------

// TestAgentHITL_E2E_AE_HITL_E2E_001_UserApproves verifies the happy path of
// the HITL approval flow: the server sends a "hitl.request_approval" request
// via ReverseRPC, the client reads the request, validates the method and ID
// prefix, and responds with {approved: true}.
func TestAgentHITL_E2E_AE_HITL_E2E_001_UserApproves(t *testing.T) {
	env := setupE2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	userID := "user-hitl-001"
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()

	// Wait for connection registration.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Goroutine reads the server Request and sends back an approval response.
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
		assert.Equal(t, "hitl.request_approval", req.Method, "method should be 'hitl.request_approval'")
		assert.True(t, strings.HasPrefix(req.ID, "s-"), "request ID should have 's-' prefix")

		// Verify the request params contain the expected action field.
		var params map[string]any
		require.NoError(t, json.Unmarshal(req.Params, &params), "unmarshal params should succeed")
		assert.Equal(t, "send_email", params["action"], "params action should be 'send_email'")

		// Construct an approval response with the same ID.
		respPayload, err := json.Marshal(map[string]any{
			"approved": true,
		})
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

	// Server sends the HITL approval request via ServerRequest.
	reqParams, err := json.Marshal(map[string]string{
		"action": "send_email",
		"target": "bob@example.com",
	})
	require.NoError(t, err, "marshal request params should succeed")

	resp, err := env.srv.ServerRequest(context.Background(), userID, "device-1", "hitl.request_approval", reqParams, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Parse and verify the response data.
	var result map[string]any
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.Equal(t, true, result["approved"], "approved should be true")
}

// ---------------------------------------------------------------------------
// AE_HITL_E2E_002: TestAgentHITL_E2E_AE_HITL_E2E_002_UserRejects
// Verifies: Server sends "hitl.request_approval" -> client responds with
//
//	{approved: false, reason: "not comfortable"} -> server receives rejection.
//
// ---------------------------------------------------------------------------

// TestAgentHITL_E2E_AE_HITL_E2E_002_UserRejects verifies the rejection path
// of the HITL approval flow: the server sends a "hitl.request_approval"
// request, the client responds with {approved: false, reason: "not comfortable"},
// and the server correctly receives the rejection.
func TestAgentHITL_E2E_AE_HITL_E2E_002_UserRejects(t *testing.T) {
	env := setupE2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	userID := "user-hitl-002"
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()

	// Wait for connection registration.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Goroutine reads the server Request and sends back a rejection response.
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
		assert.Equal(t, "hitl.request_approval", req.Method, "method should be 'hitl.request_approval'")
		assert.True(t, strings.HasPrefix(req.ID, "s-"), "request ID should have 's-' prefix")

		// Construct a rejection response with the same ID.
		respPayload, err := json.Marshal(map[string]any{
			"approved": false,
			"reason":   "not comfortable",
		})
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

	// Server sends the HITL approval request via ServerRequest.
	reqParams, err := json.Marshal(map[string]string{
		"action": "delete_account",
		"user":   "alice",
	})
	require.NoError(t, err, "marshal request params should succeed")

	resp, err := env.srv.ServerRequest(context.Background(), userID, "device-1", "hitl.request_approval", reqParams, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Parse and verify the response data.
	var result map[string]any
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.Equal(t, false, result["approved"], "approved should be false")
	assert.Equal(t, "not comfortable", result["reason"], "reason should be 'not comfortable'")
}

// ---------------------------------------------------------------------------
// AE_HITL_E2E_003: TestAgentHITL_E2E_AE_HITL_E2E_003_UserOffline
// Verifies: Server sends "hitl.request_approval" to an offline user ->
//
//	returns error containing "offline".
//
// ---------------------------------------------------------------------------

// TestAgentHITL_E2E_AE_HITL_E2E_003_UserOffline verifies that when the server
// sends a HITL approval request to a user with no active connections, the
// ServerRequest call returns an error containing "offline" immediately,
// without waiting for the timeout.
func TestAgentHITL_E2E_AE_HITL_E2E_003_UserOffline(t *testing.T) {
	env := setupE2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	// No client connected for this user.
	reqParams, err := json.Marshal(map[string]string{
		"action": "approve_payment",
		"amount": "1000",
	})
	require.NoError(t, err, "marshal request params should succeed")

	start := time.Now()
	_, err = env.srv.ServerRequest(context.Background(), "offline-user", "device-1", "hitl.request_approval", reqParams, 5*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "offline",
		"error should mention 'offline' (device offline when user has no connections)")

	// Should return almost immediately, not wait for the 5s timeout.
	assert.Less(t, elapsed, 2*time.Second,
		"should return quickly when user is offline, not wait for timeout")
}
