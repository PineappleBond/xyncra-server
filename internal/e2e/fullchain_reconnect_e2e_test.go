// Package e2e_test contains reconnection resilience E2E tests for the Xyncra
// system. These tests verify that clients can disconnect and reconnect without
// losing messages, using sync_updates to retrieve offline messages.
//
// Tests cover:
//  1. Agent processing completes during client disconnect - reconnect pulls reply
//  2. Offline messages delivered via sync_updates after reconnect
//  3. HITL during disconnect (soft assertion)
//
// Verifies: D-044 (connection resilience), D-050 (ephemeral push),
//
//	D-108 (system.reconnect)
//
// No build tag - uses mock LLM.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Test 1: Agent processing completes during client disconnect
// ---------------------------------------------------------------------------

// TestFullChainReconnect_DuringAgentProcessing verifies that when a client
// disconnects while the agent is processing a message, the agent's reply is
// persisted to the database and the client can retrieve it via sync_updates
// after reconnecting.
//
// Flow:
//  1. User connects, sends message to agent
//  2. Agent starts processing (mock LLM with delay)
//  3. Client disconnects (conn.Close)
//  4. Agent finishes processing (poll Server DB)
//  5. Client reconnects (new connection, same userID)
//  6. Client calls sync_updates to retrieve agent reply
//
// Verifies: D-044 (resilience), message persistence during disconnect.
func TestFullChainReconnect_DuringAgentProcessing(t *testing.T) {
	env := setupAgentE2E(t, agent.WithTotalTimeout(30*time.Second))
	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)

	userID := "reconnect-agent-user"
	agentUserID := "agent/test-bot"

	// Step 1: Create conversation and connect user.
	logger.Step("Connect user and create conversation")
	conv := createAgentConversation(t, env, userID, agentUserID)
	conn1 := connectClient(t, env.addr, userID, "device-reconnect-1")

	// Step 2: User sends a message to the agent via send_message RPC.
	logger.Step("User sends message to agent")
	clientMsgID := fmt.Sprintf("msg-reconnect-%d", time.Now().UnixNano())
	sendRequest(t, conn1, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "hello",
		"type":              "text",
	})
	resp := readResponse(t, conn1, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code,
		"send_message should succeed, got code=%d msg=%s", resp.Code, resp.Msg)

	// Consume the sender's own message push (C-10).
	drainPushUpdates(t, conn1)

	// Step 3: Trigger agent processing in background.
	logger.Step("Trigger agent processing (background)")
	doneCh := make(chan error, 1)
	go func() {
		err := triggerAgentProcessing(t, env, clientMsgID, conv.ID, agentUserID, userID)
		doneCh <- err
	}()

	// Step 4: Disconnect client immediately while agent is processing.
	logger.Step("Disconnect client during agent processing")
	conn1.Close()

	// Wait for connection store cleanup.
	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(context.Background(), userID, 10)
		return err == nil && len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond, "old connection should be cleaned up")

	// Step 5: Wait for agent to finish processing (poll Server DB).
	logger.Step("Wait for agent processing to complete")
	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, agentTimeout)
	require.NotEmpty(t, agentMsg.Content, "agent should have produced a reply")
	t.Logf("Agent reply: %s", agentMsg.Content)

	// Also wait for the goroutine to finish (best effort).
	select {
	case err := <-doneCh:
		if err != nil {
			t.Logf("Agent processing returned error (expected in some cases): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Log("Agent processing goroutine did not finish within 5s (continuing)")
	}

	// Step 6: Verify Server DB has the agent message.
	logger.Step("Verify Server DB has agent message")
	check.VerifyServerDB("agent-msg-persisted", func() error {
		requireServerDBHasMessage(t, env.store, conv.ID, agentMsg.Content)
		return nil
	})

	// Step 7: Reconnect with the same userID (new connection).
	logger.Step("Reconnect client")
	conn2 := connectClient(t, env.addr, userID, "device-reconnect-1")
	defer conn2.Close()

	// Wait for new connection to register.
	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userID) > 0
	}, 3*time.Second, 50*time.Millisecond, "reconnected client should be registered")

	// Step 8: sync_updates to retrieve offline messages.
	logger.Step("Sync updates after reconnect")
	sendRequest(t, conn2, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, conn2, normalTimeout)
	require.Equal(t, "sync-1", syncResp.ID, "sync response ID should match")
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code,
		"sync_updates should succeed after reconnect")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData), "unmarshal sync data")

	// Verify agent message is in the sync_updates response.
	foundAgentMsg := false
	for _, u := range syncData.Updates {
		if u.Type == protocol.UpdateTypeMessage {
			var msg model.Message
			require.NoError(t, json.Unmarshal(u.Payload, &msg))
			if msg.SenderID == agentUserID {
				foundAgentMsg = true
				assert.NotEmpty(t, msg.Content, "agent message content should not be empty")
				t.Logf("Found agent message in sync_updates: %s", msg.Content)
			}
		}
	}
	assert.True(t, foundAgentMsg, "sync_updates should contain agent's reply after reconnect")
	assert.GreaterOrEqual(t, syncData.LatestSeq, uint32(1),
		"latest_seq should be at least 1 (user msg + agent msg)")
}

// ---------------------------------------------------------------------------
// Test 2: Offline messages delivered via sync_updates after reconnect
// ---------------------------------------------------------------------------

// TestFullChainReconnect_OfflineMessages verifies that when a client (A) is
// disconnected and another client (B) sends messages to A, client A can
// retrieve those messages via sync_updates after reconnecting.
//
// Flow:
//  1. A and B connect
//  2. A disconnects
//  3. B sends 2 messages to A
//  4. A reconnects
//  5. A calls sync_updates and receives the offline messages
//
// Verifies: D-009 (sync_updates pagination), D-044 (connection resilience).
func TestFullChainReconnect_OfflineMessages(t *testing.T) {
	env := setupE2ETest(t)
	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)

	userA := "reconn-offline-a"
	userB := "reconn-offline-b"

	// Step 1: Connect both A and B, create conversation.
	logger.Step("Connect A and B")
	connA1 := connectClient(t, env.addr, userA, "dev-a")
	connB := connectClient(t, env.addr, userB, "dev-b")
	defer connB.Close()

	conv := createTestConversation(t, env.store, userA, userB)

	// Step 2: Disconnect A.
	logger.Step("Disconnect A")
	connA1.Close()

	// Wait for connection store cleanup.
	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(context.Background(), userA, 10)
		return err == nil && len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond, "A's old connection should be cleaned up")

	// Step 3: B sends 2 messages to A while A is offline.
	logger.Step("B sends messages while A is offline")
	var bMsgIDs []uint32
	for i := 0; i < 2; i++ {
		clientMsgID := fmt.Sprintf("msg-offline-%d-%d", i, time.Now().UnixNano())
		sendRequest(t, connB, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": clientMsgID,
			"content":           fmt.Sprintf("Offline message %d", i+1),
			"type":              "text",
		})
		resp := readResponse(t, connB, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code,
			"B's send_message %d should succeed", i+1)

		var respData struct {
			Message model.Message `json:"message"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &respData))
		bMsgIDs = append(bMsgIDs, respData.Message.MessageID)
	}

	// Drain B's push updates.
	drainPushUpdates(t, connB)

	// Step 4: Verify Server DB has both messages.
	logger.Step("Verify Server DB has offline messages")
	check.VerifyServerDB("offline-msgs-persisted", func() error {
		requireServerDBMessageCount(t, env.store, conv.ID, 2)
		return nil
	})

	// Step 5: A reconnects.
	logger.Step("Reconnect A")
	connA2 := connectClient(t, env.addr, userA, "dev-a")
	defer connA2.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userA) > 0
	}, 3*time.Second, 50*time.Millisecond, "reconnected A should be registered")

	// Step 6: A calls sync_updates to retrieve offline messages.
	logger.Step("A syncs updates after reconnect")
	sendRequest(t, connA2, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, connA2, normalTimeout)
	require.Equal(t, "sync-1", syncResp.ID, "sync response ID should match")
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code,
		"sync_updates should succeed after reconnect")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData), "unmarshal sync data")

	// Step 7: Verify A received both offline messages.
	assert.Len(t, syncData.Updates, 2, "A should receive 2 offline messages via sync_updates")
	assert.False(t, syncData.HasMore, "has_more should be false")
	assert.Equal(t, uint32(2), syncData.LatestSeq, "latest_seq should be 2")

	// Verify message contents.
	for i, u := range syncData.Updates {
		assert.Equal(t, protocol.UpdateTypeMessage, u.Type,
			"update %d type should be message", i)
		var msg model.Message
		require.NoError(t, json.Unmarshal(u.Payload, &msg))
		assert.Equal(t, userB, msg.SenderID, "sender should be B")
		assert.Equal(t, fmt.Sprintf("Offline message %d", i+1), msg.Content,
			"message %d content should match", i+1)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Multiple disconnect/reconnect cycles
// ---------------------------------------------------------------------------

// TestFullChainReconnect_MultipleReconnects verifies that a client can
// disconnect and reconnect multiple times, each time retrieving missed
// messages via sync_updates. This tests the resilience of the connection
// store and update tracking under repeated disconnect/reconnect cycles.
//
// Flow:
//  1. A connects, B connects
//  2. A disconnects, B sends msg1
//  3. A reconnects, syncs msg1
//  4. A disconnects again, B sends msg2
//  5. A reconnects again, syncs msg1 and msg2
//
// Verifies: D-009 (sync_updates), D-044 (resilience).
func TestFullChainReconnect_MultipleReconnects(t *testing.T) {
	env := setupE2ETest(t)
	logger := newTestStepLogger(t)

	userA := "reconn-multi-a"
	userB := "reconn-multi-b"

	// Step 1: Connect B, create conversation.
	logger.Step("Connect B and create conversation")
	connB := connectClient(t, env.addr, userB, "dev-b-multi")
	defer connB.Close()
	conv := createTestConversation(t, env.store, userA, userB)

	// --- Cycle 1: A offline, B sends msg1 ---
	logger.Step("Cycle 1: A offline, B sends msg1")

	// A is not connected. B sends a message.
	clientMsgID1 := fmt.Sprintf("msg-multi-1-%d", time.Now().UnixNano())
	sendRequest(t, connB, "send-cycle1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID1,
		"content":           "Message during first offline",
		"type":              "text",
	})
	resp1 := readResponse(t, connB, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp1.Code, "B's first send should succeed")
	drainPushUpdates(t, connB)

	// A reconnects for the first time.
	logger.Step("A reconnects (cycle 1)")
	connA1 := connectClient(t, env.addr, userA, "dev-a-multi")

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userA) > 0
	}, 3*time.Second, 50*time.Millisecond, "A should be registered (cycle 1)")

	sendRequest(t, connA1, "sync-c1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp1 := readResponse(t, connA1, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, syncResp1.Code, "sync should succeed (cycle 1)")

	var syncData1 struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp1.Data, &syncData1))
	assert.Len(t, syncData1.Updates, 1, "A should receive 1 message in cycle 1")
	assert.Equal(t, uint32(1), syncData1.LatestSeq, "latest_seq should be 1 after cycle 1")

	// A disconnects again.
	connA1.Close()
	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(context.Background(), userA, 10)
		return err == nil && len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond, "A should be disconnected (cycle 2)")

	// --- Cycle 2: A offline again, B sends msg2 ---
	logger.Step("Cycle 2: A offline again, B sends msg2")

	clientMsgID2 := fmt.Sprintf("msg-multi-2-%d", time.Now().UnixNano())
	sendRequest(t, connB, "send-cycle2", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID2,
		"content":           "Message during second offline",
		"type":              "text",
	})
	resp2 := readResponse(t, connB, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp2.Code, "B's second send should succeed")
	drainPushUpdates(t, connB)

	// A reconnects for the second time.
	logger.Step("A reconnects (cycle 2)")
	connA2 := connectClient(t, env.addr, userA, "dev-a-multi")
	defer connA2.Close()

	require.Eventually(t, func() bool {
		return env.srv.ClientsByUser(userA) > 0
	}, 3*time.Second, 50*time.Millisecond, "A should be registered (cycle 2)")

	sendRequest(t, connA2, "sync-c2", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp2 := readResponse(t, connA2, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, syncResp2.Code, "sync should succeed (cycle 2)")

	var syncData2 struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp2.Data, &syncData2))

	// A should see both messages now.
	assert.Len(t, syncData2.Updates, 2, "A should receive both messages in cycle 2")
	assert.False(t, syncData2.HasMore, "has_more should be false")
	assert.Equal(t, uint32(2), syncData2.LatestSeq, "latest_seq should be 2 after cycle 2")

	// Verify message contents and ordering.
	for i, u := range syncData2.Updates {
		assert.Equal(t, protocol.UpdateTypeMessage, u.Type,
			"update %d type should be message", i)
		var msg model.Message
		require.NoError(t, json.Unmarshal(u.Payload, &msg))
		assert.Equal(t, userB, msg.SenderID, "sender should be B")
	}

	logger.Checkpoint("multi-reconnect-complete", "FullChain",
		"Multiple reconnect cycles passed")
}
