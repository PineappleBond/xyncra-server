// Package e2e_test contains Category K ephemeral updates E2E tests for the
// Agent system (Phase 8C). Tests verify that agent_status, agent_timeout, and
// other ephemeral updates (Seq=0) are correctly broadcast, not persisted to
// sync_updates, and backward-compatible with older clients (D-050, D-087).
package e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// TestAgentEph_AE_EPH_001 — agent_status thinking (D-087)
// ---------------------------------------------------------------------------

// TestAgentEph_AE_EPH_001 verifies that when the agent begins processing,
// an agent_status ephemeral update with status="thinking" is broadcast to
// the human user with Seq=0.
func TestAgentEph_AE_EPH_001(t *testing.T) {
	// Scenario: AE-EPH-001
	// Verifies: agent_status="thinking" ephemeral update sent (D-087)
	// Strategy: Directly call BroadcastHelper.SendAgentStatus since the full
	// agent pipeline may not emit agent_status in a predictable order.
	env := setupAgentE2E(t)
	userID := "user-eph-001"
	agentUserID := "agent/test-bot"
	convID := "conv-eph-001"

	// Create conversation so broadcast targets a valid member.
	createAgentConversation(t, env, userID, agentUserID)

	// Connect user WebSocket client.
	conn := connectClient(t, env.addr, userID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	// Create broadcaster and emit agent_status="thinking".
	broadcaster := agent.NewBroadcastHelper(env.srv, testLogger{})
	broadcaster.SendAgentStatus(context.Background(), userID, agentUserID, convID, "thinking")

	// Wait for the ephemeral agent_status update.
	updates := waitForEphemeral(t, conn, protocol.UpdateTypeAgentStatus, 30*time.Second)

	// Verify payload contents.
	var found bool
	for _, u := range updates.Updates {
		if u.Type != protocol.UpdateTypeAgentStatus {
			continue
		}
		found = true
		assert.Equal(t, uint32(0), u.Seq, "agent_status must be ephemeral (Seq=0, D-050)")

		var payload agent.AgentStatusPayload
		require.NoError(t, json.Unmarshal(u.Payload, &payload))
		assert.Equal(t, agentUserID, payload.UserID, "user_id should be the agent")
		assert.Equal(t, convID, payload.ConversationID)
		assert.Equal(t, "thinking", payload.Status, "status should be 'thinking'")
		assert.NotZero(t, payload.Timestamp, "timestamp should be set")
	}
	assert.True(t, found, "should receive an agent_status update")
}

// ---------------------------------------------------------------------------
// TestAgentEph_AE_EPH_002 — agent_status tool_calling (D-087)
// ---------------------------------------------------------------------------

// TestAgentEph_AE_EPH_002 verifies that when the agent invokes a tool,
// an agent_status ephemeral update with status="tool_calling" is broadcast
// to the human user with Seq=0.
func TestAgentEph_AE_EPH_002(t *testing.T) {
	// Scenario: AE-EPH-002
	// Verifies: agent_status="tool_calling" ephemeral update sent (D-087)
	env := setupAgentE2E(t)
	userID := "user-eph-002"
	agentUserID := "agent/test-bot"
	convID := "conv-eph-002"

	createAgentConversation(t, env, userID, agentUserID)

	conn := connectClient(t, env.addr, userID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	broadcaster := agent.NewBroadcastHelper(env.srv, testLogger{})
	broadcaster.SendAgentStatus(context.Background(), userID, agentUserID, convID, "tool_calling")

	updates := waitForEphemeral(t, conn, protocol.UpdateTypeAgentStatus, 30*time.Second)

	var found bool
	for _, u := range updates.Updates {
		if u.Type != protocol.UpdateTypeAgentStatus {
			continue
		}
		found = true
		assert.Equal(t, uint32(0), u.Seq, "agent_status must be ephemeral (Seq=0, D-050)")

		var payload agent.AgentStatusPayload
		require.NoError(t, json.Unmarshal(u.Payload, &payload))
		assert.Equal(t, agentUserID, payload.UserID)
		assert.Equal(t, convID, payload.ConversationID)
		assert.Equal(t, "tool_calling", payload.Status, "status should be 'tool_calling'")
	}
	assert.True(t, found, "should receive an agent_status update")
}

// ---------------------------------------------------------------------------
// TestAgentEph_AE_EPH_003 — agent_timeout notification (D-087)
// ---------------------------------------------------------------------------

// TestAgentEph_AE_EPH_003 verifies that when agent processing times out,
// an agent_timeout ephemeral update is broadcast to the human user with Seq=0
// and the correct reason field.
func TestAgentEph_AE_EPH_003(t *testing.T) {
	// Scenario: AE-EPH-003
	// Verifies: agent_timeout ephemeral update sent on timeout (D-087)
	env := setupAgentE2E(t)
	userID := "user-eph-003"
	agentUserID := "agent/test-bot"
	convID := "conv-eph-003"

	createAgentConversation(t, env, userID, agentUserID)

	conn := connectClient(t, env.addr, userID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	broadcaster := agent.NewBroadcastHelper(env.srv, testLogger{})
	reason := "LLM request timed out after 120s"
	broadcaster.SendAgentTimeout(context.Background(), userID, agentUserID, convID, reason)

	updates := waitForEphemeral(t, conn, protocol.UpdateTypeAgentTimeout, 30*time.Second)

	var found bool
	for _, u := range updates.Updates {
		if u.Type != protocol.UpdateTypeAgentTimeout {
			continue
		}
		found = true
		assert.Equal(t, uint32(0), u.Seq, "agent_timeout must be ephemeral (Seq=0, D-050)")

		var payload agent.AgentTimeoutPayload
		require.NoError(t, json.Unmarshal(u.Payload, &payload))
		assert.Equal(t, agentUserID, payload.UserID, "user_id should be the agent")
		assert.Equal(t, convID, payload.ConversationID)
		assert.Equal(t, reason, payload.Reason, "reason should match the broadcast value")
		assert.NotZero(t, payload.Timestamp)
	}
	assert.True(t, found, "should receive an agent_timeout update")
}

// ---------------------------------------------------------------------------
// TestAgentEph_AE_EPH_004 — Ephemeral updates not persisted (D-050, D-087)
// ---------------------------------------------------------------------------

// TestAgentEph_AE_EPH_004 verifies that agent ephemeral updates (typing,
// streaming, agent_status) are never included in sync_updates responses.
// Per D-050, ephemeral updates have Seq=0 and are not persisted; sync_updates
// must only return persisted (Seq>0) updates.
func TestAgentEph_AE_EPH_004(t *testing.T) {
	// Scenario: AE-EPH-004
	// Verifies: ephemeral updates excluded from sync_updates (D-050, D-087)
	// Strategy: broadcast several ephemeral updates, then reconnect and call
	// sync_updates to verify none of them appear.
	env := setupAgentE2E(t)
	userID := "user-eph-004"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	convID := conv.ID

	// Connect and receive ephemeral updates in real time.
	conn := connectClient(t, env.addr, userID)

	// Drain initial connection setup updates.
	drainPushUpdates(t, conn)

	broadcaster := agent.NewBroadcastHelper(env.srv, testLogger{})

	// Broadcast several ephemeral updates of different types.
	broadcaster.SendTyping(context.Background(), agentUserID, userID, convID, true)
	broadcaster.SendStreamUpdate(context.Background(), userID, agentUserID, convID, "stream-1", "Hello ", false)
	broadcaster.SendAgentStatus(context.Background(), userID, agentUserID, convID, "thinking")
	broadcaster.SendAgentStatus(context.Background(), userID, agentUserID, convID, "tool_calling")

	// Wait for the agent_status updates to arrive (confirms broadcasts succeeded).
	_ = waitForEphemeral(t, conn, protocol.UpdateTypeAgentStatus, 30*time.Second)

	// Insert a persisted message so sync_updates has at least one real update.
	insertUserMessageDirect(t, env, userID, convID, "hello")

	// Drain remaining updates and close connection.
	drainPushUpdates(t, conn)
	conn.Close()

	// Wait for the connection to fully close.
	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(context.Background(), userID, 10)
		return err == nil && len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond, "connection should be fully closed")

	// Reconnect and call sync_updates.
	newConn := connectClient(t, env.addr, userID)
	defer newConn.Close()
	drainPushUpdates(t, newConn)

	sendRequest(t, newConn, "sync-eph-004", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     500,
	})
	syncResp := readResponse(t, newConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code,
		"sync_updates should succeed, got code=%d msg=%s", syncResp.Code, syncResp.Msg)

	var syncData struct {
		Updates []protocol.PackageDataUpdate `json:"updates"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))

	// Verify that no ephemeral types appear in sync_updates.
	ephemeralTypes := []string{
		protocol.UpdateTypeTyping,
		protocol.UpdateTypeStreaming,
		protocol.UpdateTypeAgentStatus,
		protocol.UpdateTypeAgentQuestion,
		protocol.UpdateTypeAgentCheckpointCreated,
		protocol.UpdateTypeAgentTimeout,
	}
	for _, u := range syncData.Updates {
		for _, ephType := range ephemeralTypes {
			assert.NotEqual(t, ephType, u.Type,
				"ephemeral type %q must not appear in sync_updates (D-050)", ephType)
		}
		// All persisted updates must have Seq>0.
		assert.Greater(t, u.Seq, uint32(0),
			"all sync_updates entries should have Seq>0 (D-050)")
	}

	// Verify the persisted message IS present.
	var foundMsg bool
	for _, u := range syncData.Updates {
		if u.Type == protocol.UpdateTypeMessage {
			var msg model.Message
			if json.Unmarshal(u.Payload, &msg) == nil && msg.SenderID == userID {
				foundMsg = true
			}
		}
	}
	assert.True(t, foundMsg, "sync_updates should contain the persisted user message")
}

// ---------------------------------------------------------------------------
// TestAgentEph_AE_EPH_005 — Old client ignores unknown types (D-087)
// ---------------------------------------------------------------------------

// TestAgentEph_AE_EPH_005 verifies that the agent_status update payload is
// valid JSON with the standard envelope structure (type + payload fields),
// ensuring backward compatibility: older clients that do not recognise the
// agent_status type will silently ignore it per the D-050 constraint, without
// raising errors or crashing.
func TestAgentEph_AE_EPH_005(t *testing.T) {
	// Scenario: AE-EPH-005
	// Verifies: agent_status payload is well-formed JSON that older clients
	// can safely ignore (D-050, D-087).
	env := setupAgentE2E(t)
	userID := "user-eph-005"
	agentUserID := "agent/test-bot"
	convID := "conv-eph-005"

	createAgentConversation(t, env, userID, agentUserID)

	conn := connectClient(t, env.addr, userID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	broadcaster := agent.NewBroadcastHelper(env.srv, testLogger{})
	broadcaster.SendAgentStatus(context.Background(), userID, agentUserID, convID, "idle")

	// Wait for the agent_status update.
	updates := waitForEphemeral(t, conn, protocol.UpdateTypeAgentStatus, 30*time.Second)

	// Verify the update envelope is well-formed. Old clients parse the outer
	// PackageDataUpdate structure and silently skip unknown types.
	var found bool
	for _, u := range updates.Updates {
		if u.Type != protocol.UpdateTypeAgentStatus {
			continue
		}
		found = true

		// Verify the outer envelope has expected fields.
		assert.Equal(t, protocol.UpdateTypeAgentStatus, u.Type,
			"type field should be 'agent_status'")
		assert.Equal(t, uint32(0), u.Seq, "Seq must be 0 for ephemeral (D-050)")

		// Verify the payload is valid JSON and contains the expected fields.
		// Old clients will unmarshal into a generic map and ignore unknown keys.
		var payloadMap map[string]interface{}
		err := json.Unmarshal(u.Payload, &payloadMap)
		require.NoError(t, err, "payload must be valid JSON (backward compat)")

		// Required fields that old clients expect in any update payload.
		assert.Contains(t, payloadMap, "user_id",
			"payload should contain 'user_id' for backward compatibility")
		assert.Contains(t, payloadMap, "conversation_id",
			"payload should contain 'conversation_id' for backward compatibility")
		assert.Contains(t, payloadMap, "timestamp",
			"payload should contain 'timestamp' for backward compatibility")

		// Verify the type-specific field is present and correctly typed.
		assert.Equal(t, "idle", payloadMap["status"],
			"status field should be present in payload")
	}
	assert.True(t, found, "should receive an agent_status update")
}
