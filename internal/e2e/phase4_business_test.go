// Package e2e_test contains Phase 4 business scenario E2E tests.
//
// These tests verify that Phase 4 (ReverseRPC idempotency key + Redis
// persistence) does not break existing business flows and that its core
// features work correctly end-to-end.
//
// Scenarios covered:
//  1. Protocol compatibility (old/new format PackageDataRequest)
//  2. Normal message flow (create_conversation -> send_message -> push -> sync -> mark_as_read)
//  3. Device replacement (D-095: old connection cancelled, new connection works)
//  4. ReverseRPC pending store integration (timeout -> persist to Redis -> verify fields)
//  5. Multi-device seq isolation (per-device monotonic seq counters)
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/handler"
	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Phase 4 E2E setup (extends setupE2ETest with PendingStore)
// ---------------------------------------------------------------------------

// phase4Env extends e2eEnv with Phase 4 components.
type phase4Env struct {
	*e2eEnv
	pendingStore *server.RedisPendingStore
	redisClient  *redis.Client // direct access for assertions
}

// setupPhase4E2ETest creates a full E2E environment with a RedisPendingStore
// wired into the WebSocketServer. The PendingStore uses the same Redis
// instance as the connection store (localhost:16379, DB 15).
func setupPhase4E2ETest(t *testing.T) *phase4Env {
	t.Helper()

	// 1. Check Redis connectivity.
	redisClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer pingCancel()
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		_ = redisClient.Close()
		t.Skipf("Redis not available at %s (DB %d): %v -- skipping Phase 4 E2E test", e2eRedisAddr, e2eRedisDB, err)
	}

	// 2. FlushDB to ensure a clean slate.
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer flushCancel()
	require.NoError(t, redisClient.FlushDB(flushCtx).Err(), "FlushDB should succeed")
	_ = redisClient.Close()

	// 3. SQLite in-memory database.
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)", t.Name())
	db, err := store.NewDatabase(store.DatabaseConfig{
		Driver: "sqlite",
		DSN:    dsn,
	})
	require.NoError(t, err, "NewDatabase should succeed")

	dataStore := store.NewFromDatabase(db)
	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer migrateCancel()
	require.NoError(t, dataStore.AutoMigrate(migrateCtx), "AutoMigrate should succeed")

	// 4. Redis connection store.
	keyPrefix := fmt.Sprintf("e2e:%s:", t.Name())
	connStore, err := server.NewRedisConnectionStore(server.RedisConnectionStoreConfig{
		Addr:       e2eRedisAddr,
		DB:         e2eRedisDB,
		KeyPrefix:  keyPrefix,
		DefaultTTL: e2eDefaultTTL,
	})
	require.NoError(t, err, "NewRedisConnectionStore should succeed")

	// 5. AsynqBroker.
	broker, err := mq.NewAsynqBroker(mq.AsynqConfig{
		RedisAddr:     e2eRedisAddr,
		RedisPassword: "",
		RedisDB:       e2eRedisDB,
	})
	require.NoError(t, err, "NewAsynqBroker should succeed")

	// 6. Message handler.
	msgHandler := server.NewDefaultMessageHandler()

	// 7. Function registry.
	funcRegistry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{})

	// 8. PendingStore (Phase 4).
	psRedisClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	pendingStore := server.NewRedisPendingStore(psRedisClient, server.PendingStoreConfig{})

	// 9. WebSocket server with PendingStore.
	srv, err := server.NewWebSocketServer(
		server.WSWithAddr(":0"),
		server.WSWithConnectionStore(connStore),
		server.WSWithStore(dataStore),
		server.WSWithBroker(broker),
		server.WSWithMessageHandler(msgHandler),
		server.WSWithFunctionRegistry(funcRegistry),
		server.WSWithPendingStore(pendingStore),
		server.WSWithPingPeriod(500*time.Millisecond),
		server.WSWithPongWait(3*time.Second),
		server.WSWithWriteWait(3*time.Second),
	)
	require.NoError(t, err, "NewWebSocketServer should succeed")

	// 10. RegisterAll with BroadcastFn.
	handler.RegisterAll(msgHandler, handler.Dependencies{
		ConnStore:        connStore,
		Store:            dataStore,
		Broker:           broker,
		BroadcastFn:      srv.BroadcastUpdates,
		FunctionRegistry: funcRegistry,
	})

	// 11. Task handler.
	taskHandler := mq.NewTaskHandler()
	taskHandler.Register(mq.TypeSendMessage,
		handler.NewSendMessageTaskHandler(srv.BroadcastUpdates, srv.Logger()))

	// 12. Start broker and server.
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := broker.Start(ctx, taskHandler); err != nil {
			if ctx.Err() == nil {
				t.Logf("broker error: %v", err)
			}
		}
	}()

	go func() {
		if err := srv.Start(ctx); err != nil {
			if ctx.Err() == nil {
				t.Logf("server error: %v", err)
			}
		}
	}()

	// 13. Wait for server to be ready.
	require.Eventually(t, func() bool {
		addr := srv.Addr()
		return addr != ":0" && addr != ""
	}, 3*time.Second, 20*time.Millisecond, "server should bind to a real address")

	addr := srv.Addr()

	// 14. Cleanup.
	t.Cleanup(func() {
		cancel()

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = srv.GracefulStop(stopCtx)

		_ = broker.Close()
		_ = connStore.Close()
		_ = psRedisClient.Close()
		_ = db.Close()

		// Final FlushDB.
		cleanupClient := redis.NewClient(&redis.Options{
			Addr: e2eRedisAddr,
			DB:   e2eRedisDB,
		})
		defer func() { _ = cleanupClient.Close() }()
		flushCtx2, flushCancel2 := context.WithTimeout(context.Background(), 2*time.Second)
		defer flushCancel2()
		_ = cleanupClient.FlushDB(flushCtx2).Err()
	})

	// 15. Create a direct Redis client for assertions.
	assertRedisClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})

	baseEnv := &e2eEnv{
		db:           db,
		store:        dataStore,
		connStore:    connStore,
		broker:       broker,
		srv:          srv,
		addr:         addr,
		cancel:       cancel,
		redisKey:     keyPrefix,
		taskHandler:  taskHandler,
		msgHandler:   msgHandler,
		funcRegistry: funcRegistry,
	}

	return &phase4Env{
		e2eEnv:       baseEnv,
		pendingStore: pendingStore,
		redisClient:  assertRedisClient,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sendRequestRaw marshals a PackageDataRequest from raw fields and sends it.
// This allows setting Phase 4 fields (idempotency_key, seq) explicitly.
func sendRequestRaw(t *testing.T, conn *wsConn, reqID, method string, params json.RawMessage, idempotencyKey string, seq uint64) {
	t.Helper()

	req := protocol.PackageDataRequest{
		ID:             reqID,
		Method:         method,
		Params:         params,
		IdempotencyKey: idempotencyKey,
		Seq:            seq,
	}
	reqData, err := json.Marshal(req)
	require.NoError(t, err, "marshal request should succeed")

	pkg := protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: reqData,
	}
	pkgData, err := json.Marshal(pkg)
	require.NoError(t, err, "marshal package should succeed")

	err = conn.WriteMessage(websocket.TextMessage, pkgData)
	require.NoError(t, err, "write message should succeed")
}

// connectClientPhase4 is a convenience wrapper around connectClient.
func connectClientPhase4(t *testing.T, env *phase4Env, userID, deviceID string) *wsConn {
	t.Helper()
	return connectClient(t, env.addr, userID, deviceID)
}

// pendingDeviceKey returns the Redis key for a device's pending requests.
func pendingDeviceKey(userID, deviceID string) string {
	return "pending:" + userID + "\x00" + deviceID
}

// readPendingRequests reads all pending requests from Redis for a device.
func readPendingRequests(t *testing.T, rc *redis.Client, userID, deviceID string) []*server.PendingRequest {
	t.Helper()
	key := pendingDeviceKey(userID, deviceID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	strs, err := rc.LRange(ctx, key, 0, -1).Result()
	require.NoError(t, err, "LRange should succeed")

	result := make([]*server.PendingRequest, 0, len(strs))
	for _, raw := range strs {
		var req server.PendingRequest
		require.NoError(t, json.Unmarshal([]byte(raw), &req), "unmarshal pending request should succeed")
		result = append(result, &req)
	}
	return result
}

// ---------------------------------------------------------------------------
// Scenario 1: Protocol Compatibility
// ---------------------------------------------------------------------------

// TestPhase4_Scenario1_ProtocolCompatibility verifies that:
//   - Old-format PackageDataRequest (no idempotency_key, no seq) is handled correctly
//   - New-format PackageDataRequest (with idempotency_key and seq) is handled correctly
//   - Server response format is backward compatible (no unexpected new fields)
func TestPhase4_Scenario1_ProtocolCompatibility(t *testing.T) {
	env := setupPhase4E2ETest(t)

	aliceConn := connectClientPhase4(t, env, "alice-p4s1", "device-alice")
	defer aliceConn.Close()
	bobConn := connectClientPhase4(t, env, "bob-p4s1", "device-bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice-p4s1", "bob-p4s1")

	// -----------------------------------------------------------------------
	// 1a. Old-format request (no idempotency_key, no seq).
	// -----------------------------------------------------------------------
	clientMsgID1 := uuid.New().String()
	sendRequest(t, aliceConn, "req-old-1", "send_message", map[string]any{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID1,
		"content":           "Old format message",
		"type":              "text",
	})

	resp1 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-old-1", resp1.ID, "response ID should match")
	require.Equal(t, protocol.ResponseCodeOK, resp1.Code,
		"old-format request should succeed, got code %d: %s", resp1.Code, resp1.Msg)

	var respData1 struct {
		Message model.Message `json:"message"`
	}
	require.NoError(t, json.Unmarshal(resp1.Data, &respData1))
	assert.Equal(t, "Old format message", respData1.Message.Content)

	// Consume push updates.
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// -----------------------------------------------------------------------
	// 1b. New-format request (with idempotency_key and seq).
	// -----------------------------------------------------------------------
	clientMsgID2 := uuid.New().String()
	params2, _ := json.Marshal(map[string]any{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID2,
		"content":           "New format message",
		"type":              "text",
	})
	sendRequestRaw(t, aliceConn, "req-new-1", "send_message", params2, "idem-key-123", 42)

	resp2 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-new-1", resp2.ID, "response ID should match")
	require.Equal(t, protocol.ResponseCodeOK, resp2.Code,
		"new-format request should succeed, got code %d: %s", resp2.Code, resp2.Msg)

	var respData2 struct {
		Message model.Message `json:"message"`
	}
	require.NoError(t, json.Unmarshal(resp2.Data, &respData2))
	assert.Equal(t, "New format message", respData2.Message.Content)

	// -----------------------------------------------------------------------
	// 1c. Response format backward compatibility.
	// Verify that the server response does not contain unexpected new fields.
	// -----------------------------------------------------------------------
	var rawResp map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp2.Data, &rawResp),
		"response data should be valid JSON")

	// The response data should have "message" and optionally "duplicate" fields.
	// It should NOT have "idempotency_key" or "seq" in the response data.
	_, hasIdempotencyKey := rawResp["idempotency_key"]
	_, hasSeq := rawResp["seq"]
	assert.False(t, hasIdempotencyKey,
		"response data should not contain idempotency_key (backward compatibility)")
	assert.False(t, hasSeq,
		"response data should not contain seq (backward compatibility)")

	// Consume push updates.
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)
}

// ---------------------------------------------------------------------------
// Scenario 2: Normal Message Flow E2E
// ---------------------------------------------------------------------------

// TestPhase4_Scenario2_NormalMessageFlow verifies that the normal message flow
// (connect -> send_message -> sync_updates -> mark_as_read) is unaffected by
// Phase 4 changes. Push delivery via MQ is not tested here because it depends
// on the AsynqBroker which may not process tasks in all CI environments.
func TestPhase4_Scenario2_NormalMessageFlow(t *testing.T) {
	env := setupPhase4E2ETest(t)

	// Step 1: Connect.
	aliceConn := connectClientPhase4(t, env, "alice-p4s2", "device-alice")
	defer aliceConn.Close()
	bobConn := connectClientPhase4(t, env, "bob-p4s2", "device-bob")
	defer bobConn.Close()

	// Step 2: Create conversation (directly in DB).
	conv := createTestConversation(t, env.store, "alice-p4s2", "bob-p4s2")

	// Drain any startup push updates.
	drainPushUpdates(t, aliceConn)
	drainPushUpdates(t, bobConn)

	// Step 3: Send message.
	clientMsgID := uuid.New().String()
	sendRequest(t, aliceConn, "send-1", "send_message", map[string]any{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Hello from Alice!",
		"type":              "text",
	})
	sendResp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, sendResp.Code,
		"send_message should succeed, got code %d: %s", sendResp.Code, sendResp.Msg)

	var sendData struct {
		Message   model.Message `json:"message"`
		Duplicate bool          `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(sendResp.Data, &sendData))
	assert.Equal(t, "Hello from Alice!", sendData.Message.Content)
	assert.Equal(t, "alice-p4s2", sendData.Message.SenderID)
	assert.False(t, sendData.Duplicate)
	assert.True(t, sendData.Message.MessageID > 0, "message ID should be > 0")

	// Drain any push updates (may or may not arrive depending on MQ).
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// Step 4: Sync updates from Bob's perspective (reads directly from DB).
	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]any{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code,
		"sync_updates should succeed")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))
	assert.NotEmpty(t, syncData.Updates, "sync should return updates")
	assert.True(t, syncData.LatestSeq > 0, "latest_seq should be > 0")

	// Verify the message update is present with Seq > 0.
	foundMessage := false
	for _, u := range syncData.Updates {
		assert.True(t, u.Seq > 0, "sync_updates should only return persisted updates (seq > 0), got seq=%d", u.Seq)
		if u.Type == protocol.UpdateTypeMessage {
			var payload model.Message
			require.NoError(t, json.Unmarshal(u.Payload, &payload))
			if payload.Content == "Hello from Alice!" {
				foundMessage = true
			}
		}
	}
	assert.True(t, foundMessage, "sync_updates should contain the sent message")

	// Step 5: Mark as read.
	sendRequest(t, bobConn, "mark-1", "mark_as_read", map[string]any{
		"conversation_id": conv.ID,
	})
	markResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, markResp.Code,
		"mark_as_read should succeed")

	var markData struct {
		Status      string `json:"status"`
		UnreadCount int64  `json:"unread_count"`
	}
	require.NoError(t, json.Unmarshal(markResp.Data, &markData))
	assert.Equal(t, "ok", markData.Status)
	assert.Equal(t, int64(0), markData.UnreadCount,
		"unread_count should be 0 after mark_as_read")

	// Step 6: Verify get_conversation shows correct state after mark_as_read.
	sendRequest(t, bobConn, "get-conv-1", "get_conversation", map[string]any{
		"conversation_id": conv.ID,
	})
	gcResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, gcResp.Code,
		"get_conversation should succeed")

	var gcData struct {
		UnreadCount int64 `json:"unread_count"`
	}
	require.NoError(t, json.Unmarshal(gcResp.Data, &gcData))
	assert.Equal(t, int64(0), gcData.UnreadCount,
		"get_conversation unread_count should be 0 after mark_as_read")
}

// ---------------------------------------------------------------------------
// Scenario 3: Device Replacement (D-095)
// ---------------------------------------------------------------------------

// TestPhase4_Scenario3_DeviceReplacement verifies that device replacement
// correctly cancels pending reverse-RPC requests on the old connection and
// the new connection works normally.
func TestPhase4_Scenario3_DeviceReplacement(t *testing.T) {
	env := setupPhase4E2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	userID := "user-p4s3"
	deviceID := "device-p4s3"

	// Step 1: Connect the first device (device-A).
	connA := connectClientPhase4(t, env, userID, deviceID)

	// Wait for connection registration.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "device-A should be registered")

	// Step 2: Start a reverse-RPC request to device-A (will be pending).
	type rpcResult struct {
		resp *protocol.PackageDataResponse
		err  error
	}
	resultCh := make(chan rpcResult, 1)
	go func() {
		resp, err := env.srv.ServerRequest(context.Background(), userID, deviceID, "ping", nil, 10*time.Second)
		resultCh <- rpcResult{resp: resp, err: err}
	}()

	// Wait for the request to be sent (client will receive it).
	// Drain it on the client side so the reader goroutine doesn't block.
	go func() {
		// Read messages until we get the reverse RPC request.
		for {
			_, data, err := connA.recv(5 * time.Second)
			if err != nil {
				return
			}
			first, rest := firstJSON(data)
			if len(rest) > 0 {
				connA.msgCh <- msgResult{messageType: 1, data: rest}
			}
			var pkg protocol.Package
			if err := json.Unmarshal(first, &pkg); err != nil {
				continue
			}
			if pkg.Type == protocol.PackageTypeRequest {
				// Got the reverse RPC request; just drain it (don't respond).
				return
			}
		}
	}()

	// Give the ServerRequest time to send the request.
	time.Sleep(200 * time.Millisecond)

	// Step 3: Connect device-A' (same user_id, same device_id) -- triggers replacement.
	connB := connectClientPhase4(t, env, userID, deviceID)
	defer connB.Close()

	// Step 4: Wait for the old reverse-RPC request to be cancelled.
	select {
	case result := <-resultCh:
		// The request should have been cancelled. The exact reason depends
		// on timing: the old handleWebSocket cleanup may fire "device disconnected"
		// before the new handleWebSocket fires "device replaced" (race between
		// the two cleanup paths). Both are valid cancellation signals.
		if result.err != nil {
			// Rare race: ctx.Done() may win first.
			t.Logf("ServerRequest returned error (acceptable race): %v", result.err)
		} else {
			require.NotNil(t, result.resp)
			assert.Equal(t, protocol.ResponseCode(-1), result.resp.Code,
				"old request should have code=-1 after device replacement")
			// Accept either "device replaced" or "device disconnected" because
			// the old handleWebSocket cleanup may run before the new one registers.
			assert.True(t,
				result.resp.Msg == "device replaced" || result.resp.Msg == "device disconnected",
				"old request msg should be 'device replaced' or 'device disconnected', got: %q",
				result.resp.Msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old reverse-RPC request was not cancelled within 5s after device replacement")
	}

	// Wait a bit for the old connection to be fully cleaned up.
	time.Sleep(300 * time.Millisecond)

	// The old connection's reader should have exited.
	// Verify by checking that the server only has the new connection.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 1
	}, 3*time.Second, 50*time.Millisecond, "only device-B should be registered")

	// Step 5: Verify the new connection works.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 1
	}, 3*time.Second, 50*time.Millisecond, "new connection should be registered")

	// Send a new reverse-RPC request to device-B and verify it works.
	go func() {
		// Read and respond to the reverse RPC request on device-B.
		for {
			_, data, err := connB.recv(5 * time.Second)
			if err != nil {
				return
			}
			first, rest := firstJSON(data)
			if len(rest) > 0 {
				connB.msgCh <- msgResult{messageType: 1, data: rest}
			}
			var pkg protocol.Package
			if err := json.Unmarshal(first, &pkg); err != nil {
				continue
			}
			if pkg.Type == protocol.PackageTypeRequest {
				var req protocol.PackageDataRequest
				if err := json.Unmarshal(pkg.Data, &req); err != nil {
					continue
				}

				// Send response.
				resp := protocol.PackageDataResponse{
					ID:   req.ID,
					Code: protocol.ResponseCodeOK,
					Msg:  "pong-from-B",
				}
				respData, _ := json.Marshal(resp)
				respPkg := protocol.Package{
					Type: protocol.PackageTypeResponse,
					Data: respData,
				}
				pkgData, _ := json.Marshal(respPkg)
				_ = connB.WriteMessage(websocket.TextMessage, pkgData)
				return
			}
		}
	}()

	resp, err := env.srv.ServerRequest(context.Background(), userID, deviceID, "ping", nil, 5*time.Second)
	require.NoError(t, err, "new reverse-RPC request to device-B should succeed")
	require.NotNil(t, resp)
	assert.Equal(t, "pong-from-B", resp.Msg, "device-B should respond correctly")

	// Clean up.
	connA.Close()
}

// ---------------------------------------------------------------------------
// Scenario 4: ReverseRPC Pending Store Integration
// ---------------------------------------------------------------------------

// TestPhase4_Scenario4_PendingStoreIntegration verifies that a timed-out
// reverse-RPC request is persisted to Redis with correct fields.
func TestPhase4_Scenario4_PendingStoreIntegration(t *testing.T) {
	env := setupPhase4E2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	userID := "user-p4s4"
	deviceID := "device-p4s4"

	// Step 1: Connect a client.
	conn := connectClientPhase4(t, env, userID, deviceID)
	defer conn.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Step 2: Drain incoming requests (don't respond) to trigger timeout.
	go func() {
		for {
			_, _, err := conn.recv(5 * time.Second)
			if err != nil {
				return
			}
			// Drain but don't respond.
		}
	}()

	// Step 3: Trigger a reverse-RPC request that will timeout.
	testParams := json.RawMessage(`{"key":"value","num":42}`)
	_, err := env.srv.ServerRequest(context.Background(), userID, deviceID, "test.method", testParams, 200*time.Millisecond)
	require.Error(t, err, "ServerRequest should timeout")
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Step 4: Wait for async persist.
	key := pendingDeviceKey(userID, deviceID)
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		n, _ := env.redisClient.LLen(ctx, key).Result()
		return n > 0
	}, 3*time.Second, 20*time.Millisecond, "pending request should be persisted to Redis")

	// Step 5: Verify the persisted data.
	pendingReqs := readPendingRequests(t, env.redisClient, userID, deviceID)
	require.Len(t, pendingReqs, 1, "should have exactly 1 pending request")

	pr := pendingReqs[0]
	assert.Equal(t, userID, pr.UserID, "UserID should match")
	assert.Equal(t, deviceID, pr.DeviceID, "DeviceID should match")
	assert.Equal(t, "test.method", pr.Method, "Method should match")
	assert.Equal(t, testParams, pr.Params, "Params should match")
	assert.NotEmpty(t, pr.ID, "ID should be non-empty")
	assert.Equal(t, pr.ID, pr.IdempotencyKey, "IdempotencyKey should equal ID (D-097)")
	assert.True(t, pr.Seq > 0, "Seq should be > 0, got %d", pr.Seq)
	assert.Equal(t, 0, pr.RetryCount, "RetryCount should be 0")
	assert.Equal(t, 3, pr.MaxRetries, "MaxRetries should be 3 (default)")
	assert.False(t, pr.CreatedAt.IsZero(), "CreatedAt should be set")

	// Step 6: Verify the Redis key format.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ttl, err := env.redisClient.PTTL(ctx, key).Result()
	require.NoError(t, err, "PTTL should succeed")
	assert.True(t, ttl > 0, "key should have a TTL, got %v", ttl)
}

// ---------------------------------------------------------------------------
// Scenario 5: Multi-device Seq Isolation
// ---------------------------------------------------------------------------

// TestPhase4_Scenario5_MultiDeviceSeqIsolation verifies that per-device seq
// counters are independent. Two devices for the same user should each have
// their own seq sequence starting from 1.
func TestPhase4_Scenario5_MultiDeviceSeqIsolation(t *testing.T) {
	env := setupPhase4E2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	userID := "user-p4s5"
	deviceA := "device-A-p4s5"
	deviceB := "device-B-p4s5"

	// Step 1: Connect two devices for the same user.
	connA := connectClientPhase4(t, env, userID, deviceA)
	defer connA.Close()
	connB := connectClientPhase4(t, env, userID, deviceB)
	defer connB.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 2
	}, 3*time.Second, 50*time.Millisecond, "both devices should be registered")

	// Step 2: Drain incoming requests on both devices (don't respond).
	go func() {
		for {
			_, _, err := connA.recv(5 * time.Second)
			if err != nil {
				return
			}
		}
	}()
	go func() {
		for {
			_, _, err := connB.recv(5 * time.Second)
			if err != nil {
				return
			}
		}
	}()

	// Step 3: Trigger 2 timeout requests for device-A.
	for i := range 2 {
		_, err := env.srv.ServerRequest(context.Background(), userID, deviceA, "ping", nil, 100*time.Millisecond)
		require.ErrorIs(t, err, context.DeadlineExceeded, "device-A request %d should timeout", i+1)
	}

	// Step 4: Trigger 3 timeout requests for device-B.
	for i := range 3 {
		_, err := env.srv.ServerRequest(context.Background(), userID, deviceB, "ping", nil, 100*time.Millisecond)
		require.ErrorIs(t, err, context.DeadlineExceeded, "device-B request %d should timeout", i+1)
	}

	// Step 5: Wait for all async persists.
	keyA := pendingDeviceKey(userID, deviceA)
	keyB := pendingDeviceKey(userID, deviceB)

	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		nA, _ := env.redisClient.LLen(ctx, keyA).Result()
		nB, _ := env.redisClient.LLen(ctx, keyB).Result()
		return nA == 2 && nB == 3
	}, 5*time.Second, 20*time.Millisecond, "device-A should have 2 pending, device-B should have 3")

	// Step 6: Verify seq isolation.
	reqsA := readPendingRequests(t, env.redisClient, userID, deviceA)
	require.Len(t, reqsA, 2, "device-A should have 2 pending requests")
	assert.Equal(t, uint64(1), reqsA[0].Seq, "device-A first seq should be 1")
	assert.Equal(t, uint64(2), reqsA[1].Seq, "device-A second seq should be 2")

	reqsB := readPendingRequests(t, env.redisClient, userID, deviceB)
	require.Len(t, reqsB, 3, "device-B should have 3 pending requests")
	assert.Equal(t, uint64(1), reqsB[0].Seq, "device-B first seq should be 1")
	assert.Equal(t, uint64(2), reqsB[1].Seq, "device-B second seq should be 2")
	assert.Equal(t, uint64(3), reqsB[2].Seq, "device-B third seq should be 3")

	// Step 7: Verify independence -- device-B seq does not overlap with device-A.
	seqsA := make(map[uint64]bool)
	for _, r := range reqsA {
		seqsA[r.Seq] = true
	}
	for _, r := range reqsB {
		assert.True(t, seqsA[r.Seq] || r.Seq <= 3,
			"device-B seq %d should be independent (its own counter)", r.Seq)
	}
}

// ---------------------------------------------------------------------------
// Scenario 1b: Heartbeat still works with Phase 4
// ---------------------------------------------------------------------------

// TestPhase4_HeartbeatCompatibility verifies that heartbeat (D-010) still
// works correctly after Phase 4 changes.
func TestPhase4_HeartbeatCompatibility(t *testing.T) {
	env := setupPhase4E2ETest(t)

	conn := connectClientPhase4(t, env, "alice-p4hb", "device-hb")
	defer conn.Close()

	// Wait for registration.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser("alice-p4hb") > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Send heartbeat.
	sendRequest(t, conn, "hb-1", "heartbeat", map[string]any{})
	resp := readResponse(t, conn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code,
		"heartbeat should succeed, got code %d: %s", resp.Code, resp.Msg)

	var hbData struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &hbData))
	assert.Equal(t, "ok", hbData.Status, "heartbeat status should be 'ok'")
}

// ---------------------------------------------------------------------------
// Scenario 3b: Normal disconnect does NOT persist to PendingStore
// ---------------------------------------------------------------------------

// TestPhase4_NormalDisconnect_NoPersist verifies that a normal client
// disconnect (not a timeout) does NOT trigger pending store persistence.
func TestPhase4_NormalDisconnect_NoPersist(t *testing.T) {
	env := setupPhase4E2ETest(t)
	env.msgHandler.SetReverseRPC(env.srv.ReverseRPC())

	userID := "user-p4nd"
	deviceID := "device-p4nd"

	conn := connectClientPhase4(t, env, userID, deviceID)

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Disconnect normally (no pending reverse-RPC requests).
	conn.Close()

	// Wait for cleanup.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 0
	}, 5*time.Second, 50*time.Millisecond, "client should be disconnected")

	// Verify no pending requests in Redis.
	key := pendingDeviceKey(userID, deviceID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, err := env.redisClient.LLen(ctx, key).Result()
	require.NoError(t, err, "LLen should succeed")
	assert.Equal(t, int64(0), n, "no pending requests should exist after normal disconnect")
}

// ---------------------------------------------------------------------------
// Scenario 2b: sync_updates after Phase 4 does not return ephemeral updates
// ---------------------------------------------------------------------------

// TestPhase4_SyncUpdates_NoEphemeral verifies that sync_updates does not
// return ephemeral updates (seq=0), which is unchanged by Phase 4 but
// important to verify as a regression check.
func TestPhase4_SyncUpdates_NoEphemeral(t *testing.T) {
	env := setupPhase4E2ETest(t)

	aliceConn := connectClientPhase4(t, env, "alice-p4sync", "device-sync")
	defer aliceConn.Close()
	bobConn := connectClientPhase4(t, env, "bob-p4sync", "device-sync-bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice-p4sync", "bob-p4sync")

	// Alice sends a message.
	sendRequest(t, aliceConn, "send-1", "send_message", map[string]any{
		"conversation_id":   conv.ID,
		"client_message_id": uuid.New().String(),
		"content":           "Test sync",
		"type":              "text",
	})
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	drainPushUpdates(t, aliceConn)
	drainPushUpdates(t, bobConn)

	// Bob syncs.
	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]any{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code)

	var syncData struct {
		Updates []protocol.PackageDataUpdate `json:"updates"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))

	// All returned updates should have seq > 0 (no ephemeral updates).
	for _, u := range syncData.Updates {
		assert.True(t, u.Seq > 0,
			"sync_updates should not return ephemeral updates (seq=0), got seq=%d type=%s",
			u.Seq, u.Type)
	}
}
