// Package e2e_test contains Phase 5 reconnect handshake + request replay E2E tests.
//
// These tests verify that Phase 5 (system.reconnect handler + ReplayRequest)
// correctly replays pending requests when a client reconnects.
//
// Scenarios covered:
//  1. Full reconnect flow (timeout -> persist -> reconnect -> replay -> respond -> removed)
//  2. Partial replay with last_seen_seq filter
//  3. IdempotencyKey preserved across replay
//  4. Empty pending store returns ok with replayed:0, total:0
package e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/handler"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Phase 5 E2E setup (extends phase4Env with system.reconnect handler wired)
// ---------------------------------------------------------------------------

// phase5Env extends phase4Env with the system.reconnect handler registered.
type phase5Env struct {
	*phase4Env
}

// setupPhase5E2ETest creates a Phase 4 environment and registers the
// system.reconnect handler with access to ReverseRPC and Logger.
func setupPhase5E2ETest(t *testing.T) *phase5Env {
	t.Helper()
	env := setupPhase4E2ETest(t)
	// Register system.reconnect handler (Phase 5, D-108).
	env.msgHandler.RegisterMethod("system.reconnect",
		handler.NewReconnectHandler(env.srv.ReverseRPC(), env.srv.Logger()))
	return &phase5Env{phase4Env: env}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// persistPendingRequest writes a PendingRequest directly to Redis for test setup.
func persistPendingRequest(t *testing.T, env *phase5Env, preq *server.PendingRequest) {
	t.Helper()
	key := pendingDeviceKey(preq.UserID, preq.DeviceID)
	data, err := json.Marshal(preq)
	require.NoError(t, err, "marshal pending request should succeed")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, env.redisClient.RPush(ctx, key, data).Err(),
		"RPush pending request should succeed")
}

// readIncomingRequest reads messages from the client connection until a
// PackageTypeRequest (reverse-RPC / replay) is found. Non-request packages
// are silently skipped. Returns the request and the raw package.
func readIncomingRequest(t *testing.T, conn *wsConn, timeout time.Duration) *protocol.PackageDataRequest {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("readIncomingRequest: timed out after %v waiting for PackageTypeRequest", timeout)
		}
		_, data, err := conn.recv(remaining)
		require.NoError(t, err, "read message should succeed")

		first, rest := firstJSON(data)
		if len(rest) > 0 {
			conn.msgCh <- msgResult{messageType: 1, data: rest}
		}

		var pkg protocol.Package
		require.NoError(t, json.Unmarshal(first, &pkg), "unmarshal package should succeed")
		if pkg.Type == protocol.PackageTypeRequest {
			var req protocol.PackageDataRequest
			require.NoError(t, json.Unmarshal(pkg.Data, &req), "unmarshal request should succeed")
			return &req
		}
	}
}

// respondToRequest sends a response package back to the server for the given request.
func respondToRequest(t *testing.T, conn *wsConn, reqID string, code protocol.ResponseCode, data interface{}) {
	t.Helper()
	var respData json.RawMessage
	if data != nil {
		var err error
		respData, err = json.Marshal(data)
		require.NoError(t, err, "marshal response data should succeed")
	}
	resp := protocol.PackageDataResponse{
		ID:   reqID,
		Code: code,
		Data: respData,
	}
	respJSON, err := json.Marshal(resp)
	require.NoError(t, err, "marshal response should succeed")
	pkg := protocol.Package{
		Type: protocol.PackageTypeResponse,
		Data: respJSON,
	}
	pkgData, err := json.Marshal(pkg)
	require.NoError(t, err, "marshal package should succeed")
	err = conn.WriteMessage(websocket.TextMessage, pkgData)
	require.NoError(t, err, "write response should succeed")
}

// ---------------------------------------------------------------------------
// Test 1: Full Reconnect Flow
// ---------------------------------------------------------------------------

// TestPhase5_FullReconnectFlow verifies that:
//  1. A ReverseRPC request times out and is persisted to Redis
//  2. Client disconnects and reconnects with same deviceID
//  3. Client sends system.reconnect with last_seen_seq=0
//  4. Server replays the pending request (client receives it)
//  5. Client responds to the replayed request
//  6. Pending request is removed from Redis
func TestPhase5_FullReconnectFlow(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "alice-p5"
	deviceID := "device-alice-p5"

	// Step 1: Connect client.
	conn1 := connectClient(t, env.addr, userID, deviceID)

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Step 2: Drain incoming on conn1 so the reverse-RPC request is delivered
	// but never responded to, causing a timeout.
	go func() {
		for {
			_, _, err := conn1.recv(5 * time.Second)
			if err != nil {
				return
			}
		}
	}()

	// Step 3: Trigger a reverse-RPC request with a short timeout.
	testParams := json.RawMessage(`{"action":"ping"}`)
	_, err := env.srv.ServerRequest(context.Background(), userID, deviceID, "test.ping", testParams, 200*time.Millisecond)
	require.Error(t, err, "ServerRequest should timeout")

	// Step 4: Wait for async persist to Redis.
	key := pendingDeviceKey(userID, deviceID)
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		n, _ := env.redisClient.LLen(ctx, key).Result()
		return n > 0
	}, 3*time.Second, 20*time.Millisecond, "pending request should be persisted to Redis")

	pendingBefore := readPendingRequests(t, env.redisClient, userID, deviceID)
	require.Len(t, pendingBefore, 1, "should have exactly 1 pending request")
	pendingID := pendingBefore[0].ID

	// Step 5: Disconnect client.
	conn1.Close()

	// Wait for the old connection to be cleaned up.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 0
	}, 5*time.Second, 50*time.Millisecond, "old connection should be cleaned up")

	// Step 6: Reconnect with same deviceID.
	conn2 := connectClient(t, env.addr, userID, deviceID)
	defer conn2.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "reconnected client should be registered")

	// Step 7: Send system.reconnect with last_seen_seq=0.
	sendRequest(t, conn2, "reconnect-1", "system.reconnect", map[string]any{
		"last_seen_seq": 0,
	})

	// Step 8: Read the reconnect response.
	reconnResp := readResponse(t, conn2, 5*time.Second)
	require.Equal(t, "reconnect-1", reconnResp.ID, "reconnect response ID should match")
	require.Equal(t, protocol.ResponseCodeOK, reconnResp.Code,
		"system.reconnect should succeed, got code %d: %s", reconnResp.Code, reconnResp.Msg)

	var reconnData struct {
		Status   string `json:"status"`
		Replayed int    `json:"replayed"`
		Total    int    `json:"total"`
	}
	require.NoError(t, json.Unmarshal(reconnResp.Data, &reconnData))
	assert.Equal(t, "ok", reconnData.Status)
	assert.Equal(t, 1, reconnData.Replayed, "should replay 1 request")
	assert.Equal(t, 1, reconnData.Total, "total should be 1")

	// Step 9: Read the replayed request (arrives as a new incoming request).
	replayedReq := readIncomingRequest(t, conn2, 5*time.Second)
	assert.Equal(t, "test.ping", replayedReq.Method, "replayed method should match original")
	assert.Equal(t, pendingID, replayedReq.IdempotencyKey,
		"replayed idempotency_key should match original pending request ID")

	// Step 10: Respond to the replayed request.
	respondToRequest(t, conn2, replayedReq.ID, protocol.ResponseCodeOK, map[string]any{"pong": true})

	// Step 11: Verify pending request removed from Redis.
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		n, _ := env.redisClient.LLen(ctx, key).Result()
		return n == 0
	}, 5*time.Second, 50*time.Millisecond, "pending request should be removed after successful replay")
}

// ---------------------------------------------------------------------------
// Test 2: Partial Replay with last_seen_seq
// ---------------------------------------------------------------------------

// TestPhase5_PartialReplay verifies that when the client sends
// system.reconnect with last_seen_seq=1, only requests with Seq > 1 are
// replayed (Seq 2 and 3, but not Seq 1).
func TestPhase5_PartialReplay(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "user-p5pr"
	deviceID := "device-p5pr"

	// Step 1: Persist 3 pending requests with Seq 1, 2, 3.
	for i := uint64(1); i <= 3; i++ {
		preq := &server.PendingRequest{
			ID:             "req-seq-" + string(rune('0'+i)),
			UserID:         userID,
			DeviceID:       deviceID,
			Method:         "test.method",
			Params:         json.RawMessage(`{"seq":` + json.Number(string(rune('0'+i))).String() + `}`),
			IdempotencyKey: "idem-seq-" + string(rune('0'+i)),
			Seq:            i,
			RetryCount:     0,
			MaxRetries:     3,
			CreatedAt:      time.Now(),
		}
		persistPendingRequest(t, env, preq)
	}

	// Step 2: Connect client.
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Step 3: Send system.reconnect with last_seen_seq=1.
	sendRequest(t, conn, "reconnect-partial", "system.reconnect", map[string]any{
		"last_seen_seq": 1,
	})

	// Step 4: Read reconnect response.
	reconnResp := readResponse(t, conn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, reconnResp.Code)

	var reconnData struct {
		Status   string `json:"status"`
		Replayed int    `json:"replayed"`
		Total    int    `json:"total"`
	}
	require.NoError(t, json.Unmarshal(reconnResp.Data, &reconnData))
	assert.Equal(t, "ok", reconnData.Status)
	assert.Equal(t, 2, reconnData.Replayed, "should replay 2 requests (Seq 2 and 3)")
	assert.Equal(t, 3, reconnData.Total, "total should be 3")

	// Step 5: Read 2 replayed requests from the client connection.
	replayedMethods := make(map[string]bool)
	for i := 0; i < 2; i++ {
		req := readIncomingRequest(t, conn, 5*time.Second)
		replayedMethods[req.IdempotencyKey] = true
		// Respond to each to prevent retry tracking side-effects.
		respondToRequest(t, conn, req.ID, protocol.ResponseCodeOK, nil)
	}

	// Verify Seq 1 was NOT replayed, but Seq 2 and 3 were.
	assert.True(t, replayedMethods["idem-seq-2"], "Seq 2 should be replayed")
	assert.True(t, replayedMethods["idem-seq-3"], "Seq 3 should be replayed")
	assert.False(t, replayedMethods["idem-seq-1"], "Seq 1 should NOT be replayed (last_seen_seq=1)")
}

// ---------------------------------------------------------------------------
// Test 3: IdempotencyKey Preserved
// ---------------------------------------------------------------------------

// TestPhase5_IdempotencyKeyPreserved verifies that the replayed request
// carries the original IdempotencyKey from the pending store.
func TestPhase5_IdempotencyKeyPreserved(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "user-p5idem"
	deviceID := "device-p5idem"
	knownIdemKey := "known-idempotency-key-12345"

	// Step 1: Persist a pending request with a known IdempotencyKey.
	preq := &server.PendingRequest{
		ID:             "req-idem-original",
		UserID:         userID,
		DeviceID:       deviceID,
		Method:         "test.echo",
		Params:         json.RawMessage(`{"msg":"hello"}`),
		IdempotencyKey: knownIdemKey,
		Seq:            1,
		RetryCount:     0,
		MaxRetries:     3,
		CreatedAt:      time.Now(),
	}
	persistPendingRequest(t, env, preq)

	// Step 2: Connect client.
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Step 3: Send system.reconnect.
	sendRequest(t, conn, "reconnect-idem", "system.reconnect", map[string]any{
		"last_seen_seq": 0,
	})
	reconnResp := readResponse(t, conn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, reconnResp.Code)

	// Step 4: Read the replayed request and verify IdempotencyKey.
	replayedReq := readIncomingRequest(t, conn, 5*time.Second)
	assert.Equal(t, knownIdemKey, replayedReq.IdempotencyKey,
		"replayed request should preserve original IdempotencyKey")
	assert.Equal(t, "test.echo", replayedReq.Method, "replayed method should match original")

	// Clean up: respond so the goroutine finishes.
	respondToRequest(t, conn, replayedReq.ID, protocol.ResponseCodeOK, nil)
}

// ---------------------------------------------------------------------------
// Test 4: Empty Pending Store
// ---------------------------------------------------------------------------

// TestPhase5_EmptyPendingStore verifies that when there are no pending
// requests, system.reconnect returns {status:"ok", replayed:0, total:0}.
func TestPhase5_EmptyPendingStore(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "user-p5empty"
	deviceID := "device-p5empty"

	// Step 1: Connect client (no pending requests in Redis).
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Step 2: Send system.reconnect.
	sendRequest(t, conn, "reconnect-empty", "system.reconnect", map[string]any{
		"last_seen_seq": 0,
	})

	// Step 3: Read response and verify.
	resp := readResponse(t, conn, 5*time.Second)
	require.Equal(t, "reconnect-empty", resp.ID, "response ID should match")
	require.Equal(t, protocol.ResponseCodeOK, resp.Code,
		"system.reconnect should succeed even with empty pending store")

	var data struct {
		Status   string `json:"status"`
		Replayed int    `json:"replayed"`
		Total    int    `json:"total"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, "ok", data.Status)
	assert.Equal(t, 0, data.Replayed, "replayed should be 0")
	assert.Equal(t, 0, data.Total, "total should be 0")
}
