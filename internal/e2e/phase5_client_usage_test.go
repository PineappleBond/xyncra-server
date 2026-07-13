// Package e2e_test contains Phase 5 client-usage E2E tests.
//
// These tests verify that Phase 5 (system.reconnect handler + ReplayRequest)
// does not break any client-facing business scenarios. Tests are written from
// the client usage perspective, exercising the same flows that xyncra-client
// would perform.
//
// Scenarios covered:
//  1. Normal message flow still works after Phase 5 changes
//  2. Device reconnection with sync_updates (no data loss)
//  3. ReverseRPC + reconnect integration (timeout -> persist -> reconnect -> replay -> respond)
//  4. Multi-device with reconnect (only the reconnecting device's pending requests replayed)
//  5. system.register_functions still works after reconnect
//  6. Reconnect with no pending requests (empty response + subsequent messaging)
package e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Scenario 1: Normal message flow still works
// ---------------------------------------------------------------------------

// TestPhase5Client_NormalMessageFlow verifies that basic messaging (create
// conversation, send message, sync_updates, mark_as_read) is unaffected by
// Phase 5 changes to Dependencies and RegisterAll.
func TestPhase5Client_NormalMessageFlow(t *testing.T) {
	env := setupPhase5E2ETest(t)

	// Step 1: Two clients connect.
	aliceConn := connectClient(t, env.addr, "alice-p5c-s1", "device-alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob-p5c-s1", "device-bob")
	defer bobConn.Close()

	// Step 2: Create conversation (directly in DB, as in Phase 4 tests).
	conv := createTestConversation(t, env.store, "alice-p5c-s1", "bob-p5c-s1")

	// Drain any startup push updates.
	drainPushUpdates(t, aliceConn)
	drainPushUpdates(t, bobConn)

	// Step 3: Alice sends a message.
	clientMsgID := uuid.New().String()
	sendRequest(t, aliceConn, "send-1", "send_message", map[string]any{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Hello from Phase 5 client test!",
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
	assert.Equal(t, "Hello from Phase 5 client test!", sendData.Message.Content)
	assert.Equal(t, "alice-p5c-s1", sendData.Message.SenderID)
	assert.False(t, sendData.Duplicate)
	assert.True(t, sendData.Message.MessageID > 0, "message ID should be > 0")

	// Drain push updates.
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// Step 4: Bob syncs updates.
	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]any{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync_updates should succeed")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))
	assert.NotEmpty(t, syncData.Updates, "sync should return updates")
	assert.True(t, syncData.LatestSeq > 0, "latest_seq should be > 0")

	// Verify the message update is present.
	foundMessage := false
	for _, u := range syncData.Updates {
		if u.Type == protocol.UpdateTypeMessage {
			var payload model.Message
			require.NoError(t, json.Unmarshal(u.Payload, &payload))
			if payload.Content == "Hello from Phase 5 client test!" {
				foundMessage = true
			}
		}
	}
	assert.True(t, foundMessage, "sync_updates should contain the sent message")

	// Step 5: Bob marks as read.
	sendRequest(t, bobConn, "mark-1", "mark_as_read", map[string]any{
		"conversation_id": conv.ID,
	})
	markResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, markResp.Code, "mark_as_read should succeed")

	var markData struct {
		Status      string `json:"status"`
		UnreadCount int64  `json:"unread_count"`
	}
	require.NoError(t, json.Unmarshal(markResp.Data, &markData))
	assert.Equal(t, "ok", markData.Status)
	assert.Equal(t, int64(0), markData.UnreadCount,
		"unread_count should be 0 after mark_as_read")
}

// ---------------------------------------------------------------------------
// Scenario 2: Device reconnection with data sync
// ---------------------------------------------------------------------------

// TestPhase5Client_ReconnectWithSync verifies that a client that disconnects
// and reconnects can sync updates without data loss.
func TestPhase5Client_ReconnectWithSync(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "alice-p5c-s2"
	deviceID := "device-alice-p5c-s2"

	// Step 1: Client connects and sends messages.
	conn1 := connectClient(t, env.addr, userID, deviceID)

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	conv := createTestConversation(t, env.store, userID, "bob-p5c-s2")

	// Send 2 messages.
	for i := 0; i < 2; i++ {
		sendRequest(t, conn1, "send-"+string(rune('1'+i)), "send_message", map[string]any{
			"conversation_id":   conv.ID,
			"client_message_id": uuid.New().String(),
			"content":           "Message before disconnect",
			"type":              "text",
		})
		resp := readResponse(t, conn1, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send_message should succeed")
	}

	// Drain push updates on conn1.
	drainPushUpdates(t, conn1)

	// Step 2: Disconnect.
	conn1.Close()

	// Wait for cleanup.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 0
	}, 5*time.Second, 50*time.Millisecond, "client should be disconnected")

	// Step 3: Reconnect with same device_id.
	conn2 := connectClient(t, env.addr, userID, deviceID)
	defer conn2.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "reconnected client should be registered")

	// Step 4: Call sync_updates to catch up.
	sendRequest(t, conn2, "sync-1", "sync_updates", map[string]any{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, conn2, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync_updates should succeed after reconnect")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))
	assert.NotEmpty(t, syncData.Updates, "sync should return updates after reconnect")
	assert.True(t, syncData.LatestSeq > 0, "latest_seq should be > 0")

	// Verify we got the messages that were sent before disconnect.
	messageCount := 0
	for _, u := range syncData.Updates {
		if u.Type == protocol.UpdateTypeMessage {
			messageCount++
		}
	}
	assert.Equal(t, 2, messageCount, "should have 2 messages in sync_updates (no data loss)")
}

// ---------------------------------------------------------------------------
// Scenario 3: ReverseRPC + reconnect integration
// ---------------------------------------------------------------------------

// TestPhase5Client_ReverseRPCReconnectIntegration verifies the full client
// lifecycle for reverse RPC + reconnect:
//  1. Client connects and registers functions
//  2. Server sends a ReverseRPC request that times out
//  3. Request persists to Redis
//  4. Client disconnects and reconnects
//  5. Client calls system.reconnect with last_seen_seq=0
//  6. Client receives the replayed request and responds
//  7. Pending request is removed
func TestPhase5Client_ReverseRPCReconnectIntegration(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "alice-p5c-s3"
	deviceID := "device-alice-p5c-s3"

	// Step 1: Client connects.
	conn1 := connectClient(t, env.addr, userID, deviceID)

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Step 2: Register functions (client capability registration).
	sendRequest(t, conn1, "reg-1", "system.register_functions", map[string]any{
		"device_id":   deviceID,
		"device_name": "Test Device",
		"device_type": "desktop",
		"functions": []map[string]any{
			{
				"name":        "test.get_location",
				"description": "Get current GPS location",
				"parameters":  map[string]any{},
			},
		},
	})
	regResp := readResponse(t, conn1, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, regResp.Code,
		"register_functions should succeed, got code %d: %s", regResp.Code, regResp.Msg)

	// Step 3: Drain incoming on conn1 so the reverse-RPC request times out.
	go func() {
		for {
			_, _, err := conn1.recv(5 * time.Second)
			if err != nil {
				return
			}
		}
	}()

	// Step 4: Trigger a reverse-RPC request that will timeout.
	testParams := json.RawMessage(`{"action":"get_location"}`)
	_, err := env.srv.ServerRequest(context.Background(), userID, deviceID, "test.get_location", testParams, 200*time.Millisecond)
	require.Error(t, err, "ServerRequest should timeout")

	// Step 5: Wait for async persist to Redis.
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

	// Step 6: Disconnect client.
	conn1.Close()

	// Wait for cleanup.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 0
	}, 5*time.Second, 50*time.Millisecond, "old connection should be cleaned up")

	// Step 7: Reconnect with same device_id.
	conn2 := connectClient(t, env.addr, userID, deviceID)
	defer conn2.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "reconnected client should be registered")

	// Step 8: Call system.reconnect with last_seen_seq=0.
	sendRequest(t, conn2, "reconnect-1", "system.reconnect", map[string]any{
		"last_seen_seq": 0,
	})
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

	// Step 9: Read the replayed request.
	replayedReq := readIncomingRequest(t, conn2, 5*time.Second)
	assert.Equal(t, "test.get_location", replayedReq.Method,
		"replayed method should match original")
	assert.Equal(t, pendingID, replayedReq.IdempotencyKey,
		"replayed idempotency_key should match original pending request ID")

	// Step 10: Respond to the replayed request.
	respondToRequest(t, conn2, replayedReq.ID, protocol.ResponseCodeOK,
		map[string]any{"location": "test-location"})

	// Step 11: Verify pending request removed from Redis.
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		n, _ := env.redisClient.LLen(ctx, key).Result()
		return n == 0
	}, 5*time.Second, 50*time.Millisecond, "pending request should be removed after successful replay")
}

// ---------------------------------------------------------------------------
// Scenario 4: Multi-device with reconnect
// ---------------------------------------------------------------------------

// TestPhase5Client_MultiDeviceReconnect verifies that when two devices are
// connected for the same user, and one disconnects and reconnects, only the
// reconnecting device's pending requests are replayed.
func TestPhase5Client_MultiDeviceReconnect(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "user-p5c-s4"
	device1 := "device-1-p5c-s4"
	device2 := "device-2-p5c-s4"

	// Step 1: Both devices connect.
	conn1 := connectClient(t, env.addr, userID, device1)

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "device-1 should be registered")

	conn2 := connectClient(t, env.addr, userID, device2)
	defer conn2.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 2
	}, 3*time.Second, 50*time.Millisecond, "both devices should be registered")

	// Step 2: Drain incoming on device-1 (will timeout).
	go func() {
		for {
			_, _, err := conn1.recv(5 * time.Second)
			if err != nil {
				return
			}
		}
	}()

	// Step 3: Drain incoming on device-2 (will also timeout).
	go func() {
		for {
			_, _, err := conn2.recv(5 * time.Second)
			if err != nil {
				return
			}
		}
	}()

	// Step 4: Trigger 1 timeout request for device-1.
	_, err := env.srv.ServerRequest(context.Background(), userID, device1, "ping.device1", nil, 200*time.Millisecond)
	require.Error(t, err, "device-1 ServerRequest should timeout")

	// Step 5: Trigger 1 timeout request for device-2.
	_, err = env.srv.ServerRequest(context.Background(), userID, device2, "ping.device2", nil, 200*time.Millisecond)
	require.Error(t, err, "device-2 ServerRequest should timeout")

	// Step 6: Wait for both to persist.
	key1 := pendingDeviceKey(userID, device1)
	key2 := pendingDeviceKey(userID, device2)
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		n1, _ := env.redisClient.LLen(ctx, key1).Result()
		n2, _ := env.redisClient.LLen(ctx, key2).Result()
		return n1 == 1 && n2 == 1
	}, 5*time.Second, 20*time.Millisecond, "both devices should have 1 pending request each")

	// Step 7: Disconnect device-1.
	conn1.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 1
	}, 5*time.Second, 50*time.Millisecond, "only device-2 should remain")

	// Step 8: Device-1 reconnects.
	conn1New := connectClient(t, env.addr, userID, device1)
	defer conn1New.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 2
	}, 3*time.Second, 50*time.Millisecond, "both devices should be registered again")

	// Step 9: Device-1 calls system.reconnect.
	sendRequest(t, conn1New, "reconnect-d1", "system.reconnect", map[string]any{
		"last_seen_seq": 0,
	})
	reconnResp := readResponse(t, conn1New, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, reconnResp.Code,
		"system.reconnect for device-1 should succeed")

	var reconnData struct {
		Status   string `json:"status"`
		Replayed int    `json:"replayed"`
		Total    int    `json:"total"`
	}
	require.NoError(t, json.Unmarshal(reconnResp.Data, &reconnData))
	assert.Equal(t, "ok", reconnData.Status)
	assert.Equal(t, 1, reconnData.Replayed, "device-1 should replay 1 request")
	assert.Equal(t, 1, reconnData.Total, "total should be 1")

	// Step 10: Read the replayed request and verify it's device-1's.
	replayedReq := readIncomingRequest(t, conn1New, 5*time.Second)
	assert.Equal(t, "ping.device1", replayedReq.Method,
		"replayed method should be device-1's request, not device-2's")

	// Respond to clean up.
	respondToRequest(t, conn1New, replayedReq.ID, protocol.ResponseCodeOK, nil)

	// Step 11: Verify device-1's pending store is cleared.
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		n, _ := env.redisClient.LLen(ctx, key1).Result()
		return n == 0
	}, 5*time.Second, 50*time.Millisecond, "device-1 pending should be cleared")

	// Step 12: Verify device-2's pending store is UNTOUCHED.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n2, err := env.redisClient.LLen(ctx, key2).Result()
	require.NoError(t, err, "LLen should succeed for device-2")
	assert.Equal(t, int64(1), n2, "device-2 should still have 1 pending request (untouched)")
}

// ---------------------------------------------------------------------------
// Scenario 5: system.register_functions after reconnect
// ---------------------------------------------------------------------------

// TestPhase5Client_RegisterFunctionsAfterReconnect verifies that the function
// registry still works after a client reconnects and calls system.reconnect.
func TestPhase5Client_RegisterFunctionsAfterReconnect(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "user-p5c-s5"
	deviceID := "device-p5c-s5"

	// Step 1: Client connects and registers functions.
	conn1 := connectClient(t, env.addr, userID, deviceID)

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	sendRequest(t, conn1, "reg-1", "system.register_functions", map[string]any{
		"device_id":   deviceID,
		"device_name": "Test Device",
		"device_type": "desktop",
		"functions": []map[string]any{
			{
				"name":        "test.func_before",
				"description": "Function registered before reconnect",
				"parameters":  map[string]any{},
			},
		},
	})
	regResp := readResponse(t, conn1, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, regResp.Code,
		"register_functions before reconnect should succeed")

	// Step 2: Disconnect.
	conn1.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 0
	}, 5*time.Second, 50*time.Millisecond, "client should be disconnected")

	// Step 3: Reconnect.
	conn2 := connectClient(t, env.addr, userID, deviceID)
	defer conn2.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "reconnected client should be registered")

	// Step 4: Call system.reconnect (empty pending store).
	sendRequest(t, conn2, "reconnect-1", "system.reconnect", map[string]any{
		"last_seen_seq": 0,
	})
	reconnResp := readResponse(t, conn2, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, reconnResp.Code,
		"system.reconnect should succeed")

	// Step 5: Register NEW functions after reconnect.
	sendRequest(t, conn2, "reg-2", "system.register_functions", map[string]any{
		"device_id":   deviceID,
		"device_name": "Test Device",
		"device_type": "desktop",
		"functions": []map[string]any{
			{
				"name":        "test.func_after",
				"description": "Function registered after reconnect",
				"parameters":  map[string]any{},
			},
		},
	})
	regResp2 := readResponse(t, conn2, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, regResp2.Code,
		"register_functions after reconnect should succeed, got code %d: %s",
		regResp2.Code, regResp2.Msg)

	// Step 6: Verify the function is registered by checking the registry.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	funcs, err := env.funcRegistry.GetFunctions(ctx, userID, deviceID)
	require.NoError(t, err, "GetFunctions should succeed")
	foundAfter := false
	for _, f := range funcs {
		if f.Name == "test.func_after" {
			foundAfter = true
		}
	}
	assert.True(t, foundAfter, "function registered after reconnect should be in registry")
}

// ---------------------------------------------------------------------------
// Scenario 6: Reconnect with no pending requests + subsequent messaging
// ---------------------------------------------------------------------------

// TestPhase5Client_ReconnectEmptyThenMessage verifies that:
//  1. A fresh client (no pending requests) calls system.reconnect
//  2. Response is {status:"ok", replayed:0, total:0}
//  3. Subsequent messaging still works normally
func TestPhase5Client_ReconnectEmptyThenMessage(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "alice-p5c-s6"
	deviceID := "device-alice-p5c-s6"

	// Step 1: Client connects fresh (no prior timed-out requests).
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "client should be registered")

	// Step 2: Call system.reconnect with no pending requests.
	sendRequest(t, conn, "reconnect-1", "system.reconnect", map[string]any{
		"last_seen_seq": 0,
	})
	resp := readResponse(t, conn, 5*time.Second)
	require.Equal(t, "reconnect-1", resp.ID, "response ID should match")
	require.Equal(t, protocol.ResponseCodeOK, resp.Code,
		"system.reconnect should succeed even with empty pending store")

	var reconnData struct {
		Status   string `json:"status"`
		Replayed int    `json:"replayed"`
		Total    int    `json:"total"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &reconnData))
	assert.Equal(t, "ok", reconnData.Status)
	assert.Equal(t, 0, reconnData.Replayed, "replayed should be 0")
	assert.Equal(t, 0, reconnData.Total, "total should be 0")

	// Step 3: Subsequent messaging still works.
	// Create a conversation and send a message.
	bobConn := connectClient(t, env.addr, "bob-p5c-s6", "device-bob-p5c-s6")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, userID, "bob-p5c-s6")

	drainPushUpdates(t, conn)
	drainPushUpdates(t, bobConn)

	clientMsgID := uuid.New().String()
	sendRequest(t, conn, "send-1", "send_message", map[string]any{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Message after reconnect",
		"type":              "text",
	})
	sendResp := readResponse(t, conn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, sendResp.Code,
		"send_message after reconnect should succeed, got code %d: %s",
		sendResp.Code, sendResp.Msg)

	var sendData struct {
		Message model.Message `json:"message"`
	}
	require.NoError(t, json.Unmarshal(sendResp.Data, &sendData))
	assert.Equal(t, "Message after reconnect", sendData.Message.Content)
	assert.Equal(t, userID, sendData.Message.SenderID)

	// Step 4: Bob can sync and see the message.
	drainPushUpdates(t, bobConn)

	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]any{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync_updates should succeed")

	var syncData struct {
		Updates []protocol.PackageDataUpdate `json:"updates"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))

	foundMessage := false
	for _, u := range syncData.Updates {
		if u.Type == protocol.UpdateTypeMessage {
			var payload model.Message
			require.NoError(t, json.Unmarshal(u.Payload, &payload))
			if payload.Content == "Message after reconnect" {
				foundMessage = true
			}
		}
	}
	assert.True(t, foundMessage, "bob should see the message sent after alice's reconnect")
}

// ---------------------------------------------------------------------------
// Scenario 7: Heartbeat still works with Phase 5
// ---------------------------------------------------------------------------

// TestPhase5Client_HeartbeatStillWorks verifies that heartbeat (D-010) is
// unaffected by Phase 5 changes.
func TestPhase5Client_HeartbeatStillWorks(t *testing.T) {
	env := setupPhase5E2ETest(t)

	conn := connectClient(t, env.addr, "alice-p5c-hb", "device-hb-p5c")
	defer conn.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser("alice-p5c-hb") > 0
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
// Scenario 8: Reconnect does not affect other devices' push delivery
// ---------------------------------------------------------------------------

// TestPhase5Client_ReconnectDoesNotAffectOtherDevicePush verifies that when
// device-1 reconnects, device-2 (still connected) continues to receive push
// updates normally.
func TestPhase5Client_ReconnectDoesNotAffectOtherDevicePush(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "alice-p5c-s8"
	device1 := "device-1-p5c-s8"
	device2 := "device-2-p5c-s8"

	// Step 1: Two devices connect for same user.
	conn1 := connectClient(t, env.addr, userID, device1)

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "device-1 should be registered")

	conn2 := connectClient(t, env.addr, userID, device2)
	defer conn2.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 2
	}, 3*time.Second, 50*time.Millisecond, "both devices should be registered")

	// Create conversation with bob.
	conv := createTestConversation(t, env.store, userID, "bob-p5c-s8")
	bobConn := connectClient(t, env.addr, "bob-p5c-s8", "device-bob-p5c-s8")
	defer bobConn.Close()

	drainPushUpdates(t, conn1)
	drainPushUpdates(t, conn2)
	drainPushUpdates(t, bobConn)

	// Step 2: Disconnect device-1.
	conn1.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 1
	}, 5*time.Second, 50*time.Millisecond, "only device-2 should remain")

	// Step 3: Device-1 reconnects and calls system.reconnect.
	conn1New := connectClient(t, env.addr, userID, device1)
	defer conn1New.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) == 2
	}, 3*time.Second, 50*time.Millisecond, "both devices should be registered again")

	sendRequest(t, conn1New, "reconnect-1", "system.reconnect", map[string]any{
		"last_seen_seq": 0,
	})
	reconnResp := readResponse(t, conn1New, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, reconnResp.Code,
		"system.reconnect should succeed")

	drainPushUpdates(t, conn1New)

	// Step 4: Bob sends a message -- both device-1 (new) and device-2 should
	// receive push updates.
	sendRequest(t, bobConn, "send-1", "send_message", map[string]any{
		"conversation_id":   conv.ID,
		"client_message_id": uuid.New().String(),
		"content":           "Test push after reconnect",
		"type":              "text",
	})
	bobSendResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, bobSendResp.Code,
		"bob's send_message should succeed")

	// Step 5: Device-2 (which was never disconnected) should receive push.
	drainPushUpdates(t, conn2)

	// Device-2 should still be functional -- verify by sending a heartbeat.
	sendRequest(t, conn2, "hb-1", "heartbeat", map[string]any{})
	hbResp := readResponse(t, conn2, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, hbResp.Code,
		"device-2 heartbeat should still work after device-1 reconnect")

	// Step 6: Device-1 (reconnected) should also be functional.
	sendRequest(t, conn1New, "hb-2", "heartbeat", map[string]any{})
	hbResp2 := readResponse(t, conn1New, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, hbResp2.Code,
		"device-1 (reconnected) heartbeat should work")
}

// ---------------------------------------------------------------------------
// Scenario 9: Reconnect retry count increments on failed replay
// ---------------------------------------------------------------------------

// TestPhase5Client_ReconnectRetryCountIncrement verifies that when a replayed
// request times out again (client doesn't respond), the RetryCount is
// incremented in the pending store.
func TestPhase5Client_ReconnectRetryCountIncrement(t *testing.T) {
	env := setupPhase5E2ETest(t)

	userID := "user-p5c-s9"
	deviceID := "device-p5c-s9"

	// Step 1: Persist a pending request with RetryCount=0, MaxRetries=3.
	preq := &server.PendingRequest{
		ID:             "req-retry-test",
		UserID:         userID,
		DeviceID:       deviceID,
		Method:         "test.retry_method",
		Params:         json.RawMessage(`{"retry":"test"}`),
		IdempotencyKey: "idem-retry-test",
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

	// Step 3: Call system.reconnect FIRST (read response before draining).
	sendRequest(t, conn, "reconnect-1", "system.reconnect", map[string]any{
		"last_seen_seq": 0,
	})
	reconnResp := readResponse(t, conn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, reconnResp.Code,
		"system.reconnect should succeed")

	var reconnData struct {
		Replayed int `json:"replayed"`
	}
	require.NoError(t, json.Unmarshal(reconnResp.Data, &reconnData))
	assert.Equal(t, 1, reconnData.Replayed, "should replay 1 request")

	// Step 4: Drain incoming (don't respond to replayed request, so it times out).
	go func() {
		for {
			_, _, err := conn.recv(15 * time.Second)
			if err != nil {
				return
			}
		}
	}()

	// Step 5: Wait for the replay to timeout (replayTimeout is 10s, but we
	// just wait for the async update to happen).
	key := pendingDeviceKey(userID, deviceID)
	require.Eventually(t, func() bool {
		reqs := readPendingRequests(t, env.redisClient, userID, deviceID)
		if len(reqs) == 0 {
			return false
		}
		// RetryCount should have been incremented from 0 to 1.
		return reqs[0].RetryCount >= 1
	}, 15*time.Second, 200*time.Millisecond,
		"RetryCount should be incremented after failed replay")

	// Step 6: Verify the request is still in the store (not removed).
	n, err := env.redisClient.LLen(context.Background(), key).Result()
	require.NoError(t, err, "LLen should succeed")
	assert.Equal(t, int64(1), n, "pending request should still exist (RetryCount incremented, not removed)")
}
