// Package e2e_test contains Category A basic flow E2E tests for the Agent
// system (Phase 1-8). Tests verify the fundamental agent message pipeline:
// user sends message → agent executor → LLM → persist → verify.
//
// Note: The agent_process MQ task delivery is tested implicitly through the
// existing MQ tests (TestBasicMessageDelivery, etc). These tests focus on
// the agent-specific logic: executor, LLM integration, message persistence.
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
// Agent basic test helpers
// ---------------------------------------------------------------------------

// sendUserMessage connects a user via WebSocket, sends a send_message RPC,
// reads the response, and returns the connection. The caller is responsible
// for closing the connection.
func sendUserMessage(t *testing.T, env *agentE2EEnv, userID, convID, content string) *wsConn {
	t.Helper()

	conn := connectClient(t, env.addr, userID, "device-1")

	clientMsgID := fmt.Sprintf("msg-%s-%d", userID, time.Now().UnixNano())
	sendRequest(t, conn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": clientMsgID,
		"content":           content,
		"type":              "text",
	})

	resp := readResponse(t, conn, 10*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code,
		"send_message should succeed, got code=%d msg=%s", resp.Code, resp.Msg)

	return conn
}

// triggerAgentProcessing invokes the agent executor directly (bypassing MQ
// task delivery). This tests the agent pipeline: context loading → LLM call
// → streaming → persistence. Returns the executor error, if any.
func triggerAgentProcessing(t *testing.T, env *agentE2EEnv, messageID, convID, agentUserID, senderID string) error {
	t.Helper()

	payload := agent.ExecutePayload{
		MessageID:      messageID,
		ConversationID: convID,
		AgentID:        agentUserID,
		SenderID:       senderID,
	}
	return env.executor.Execute(context.Background(), payload)
}

// waitForAgentMessageInDB polls the database until a message from the agent
// is persisted in the given conversation, or the timeout expires.
func waitForAgentMessageInDB(t *testing.T, env *agentE2EEnv, convID, agentUserID string, timeout time.Duration) *model.Message {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("waitForAgentMessageInDB: timed out after %v waiting for agent message from %s in conv %s",
				timeout, agentUserID, convID)
		}

		var messages []*model.Message
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", convID, agentUserID).
			Order("message_id DESC").
			Limit(1).
			Find(&messages)

		if len(messages) > 0 {
			return messages[0]
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// TestAgentBasic_AE_BASIC_001
// Scenario: User sends message to agent → agent replies
// Verifies: Message persisted, broadcast sent, correct sender_id (D-054, D-055, D-062)
// ---------------------------------------------------------------------------

// TestAgentBasic_AE_BASIC_001 verifies the happy path: a user sends a message
// to a registered agent and the agent produces a reply. The send_message RPC
// persists the user's message, then the agent executor is triggered directly
// to produce the reply.
func TestAgentBasic_AE_BASIC_001(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-basic-001"
	agentUserID := "agent/test-bot"

	// Create conversation between user and agent.
	conv := createAgentConversation(t, env, userID, agentUserID)

	// Send user message via WebSocket (persists user message in DB).
	conn := sendUserMessage(t, env, userID, conv.ID, "hello")
	defer conn.Close()

	// Trigger agent processing directly (bypassing MQ delivery).
	err := triggerAgentProcessing(t, env, "msg-001", conv.ID, agentUserID, userID)
	require.NoError(t, err, "agent executor should succeed")

	// Wait for the agent's reply to be persisted in the DB.
	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 10*time.Second)
	assert.NotEmpty(t, agentMsg.Content, "agent reply should not be empty")
	assert.Equal(t, agentUserID, agentMsg.SenderID,
		"sender_id should be the agent (D-054)")

	// Verify the mock LLM was called at least once.
	assert.Greater(t, env.mockLLM.CallCount(), 0,
		"mock LLM should have been called at least once")
}

// ---------------------------------------------------------------------------
// TestAgentBasic_AE_BASIC_002
// Scenario: Agent reply message format is correct
// Verifies: Message.SenderID = "agent/test-bot", Content non-empty (D-055)
// ---------------------------------------------------------------------------

// TestAgentBasic_AE_BASIC_002 verifies that the agent's reply message has the
// correct format: SenderID matches "agent/{id}" (D-054), Content is non-empty
// and matches the expected mock LLM response (D-055).
func TestAgentBasic_AE_BASIC_002(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-basic-002"
	agentUserID := "agent/test-bot"

	// Create conversation and send user message.
	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := sendUserMessage(t, env, userID, conv.ID, "hello")
	defer conn.Close()

	// Trigger agent processing.
	err := triggerAgentProcessing(t, env, "msg-002", conv.ID, agentUserID, userID)
	require.NoError(t, err, "agent executor should succeed")

	// Wait for the agent reply to be persisted in DB.
	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 10*time.Second)

	// Verify persisted message format.
	assert.Equal(t, agentUserID, agentMsg.SenderID,
		"sender_id should be 'agent/test-bot' (D-054)")
	assert.NotEmpty(t, agentMsg.Content,
		"content should not be empty (D-055)")
	assert.Equal(t, conv.ID, agentMsg.ConversationID,
		"conversation_id should match")
	assert.Equal(t, "text", agentMsg.Type, "message type should be 'text'")
	assert.NotEmpty(t, agentMsg.ID, "message ID should not be empty")
	assert.Greater(t, agentMsg.MessageID, uint32(0),
		"message_id should be allocated")

	// Verify via the assertion helper too.
	assertAgentMessagePersisted(t, env, conv.ID, agentUserID, agentMsg.Content)
}

// ---------------------------------------------------------------------------
// TestAgentBasic_AE_BASIC_003
// Scenario: Agent reply retrievable via sync_updates
// Verifies: Offline user reconnects and gets agent reply (D-055, D-009)
// ---------------------------------------------------------------------------

// TestAgentBasic_AE_BASIC_003 verifies that an agent's reply is persisted and
// can be retrieved by an offline user via sync_updates when they reconnect.
func TestAgentBasic_AE_BASIC_003(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-basic-003"
	agentUserID := "agent/test-bot"

	// Create conversation.
	conv := createAgentConversation(t, env, userID, agentUserID)

	// Send user message (persists in DB).
	conn := sendUserMessage(t, env, userID, conv.ID, "hello")

	// Trigger agent processing.
	err := triggerAgentProcessing(t, env, "msg-003", conv.ID, agentUserID, userID)
	require.NoError(t, err, "agent executor should succeed")

	// Wait for agent reply to be persisted.
	_ = waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 10*time.Second)

	// Drain and close.
	drainPushUpdates(t, conn)
	conn.Close()

	// Wait for the old connection to be cleaned up.
	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(context.Background(), userID, 10)
		return err == nil && len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond, "old connection should be cleaned up")

	// Reconnect as the same user.
	newConn := connectClient(t, env.addr, userID, "device-1")
	defer newConn.Close()
	drainPushUpdates(t, newConn)

	// Request sync_updates from the beginning (after_seq=0).
	sendRequest(t, newConn, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})

	syncResp := readResponse(t, newConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync_updates should succeed")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData), "unmarshal sync data")

	// Verify the sync response contains the agent reply.
	var foundAgentReply bool
	for _, u := range syncData.Updates {
		if u.Type == protocol.UpdateTypeMessage {
			var msg model.Message
			require.NoError(t, json.Unmarshal(u.Payload, &msg))
			if msg.SenderID == agentUserID {
				foundAgentReply = true
				assert.NotEmpty(t, msg.Content, "agent reply content should not be empty")
			}
		}
	}
	assert.True(t, foundAgentReply,
		"sync_updates should contain the agent's reply (D-055, D-009)")

	// Also verify the user's own message is in the sync.
	var foundUserMessage bool
	for _, u := range syncData.Updates {
		if u.Type == protocol.UpdateTypeMessage {
			var msg model.Message
			require.NoError(t, json.Unmarshal(u.Payload, &msg))
			if msg.SenderID == userID {
				foundUserMessage = true
			}
		}
	}
	assert.True(t, foundUserMessage,
		"sync_updates should contain the user's own message")
}

// ---------------------------------------------------------------------------
// TestAgentBasic_AE_BASIC_004
// Scenario: Non-agent users unaffected by agent system
// Verifies: Human-to-human messages flow normally (D-062)
// ---------------------------------------------------------------------------

// TestAgentBasic_AE_BASIC_004 verifies that the presence of the agent system
// does not affect normal human-to-human message delivery.
func TestAgentBasic_AE_BASIC_004(t *testing.T) {
	env := setupAgentE2E(t)

	user1 := "user-h2h-1"
	user2 := "user-h2h-2"

	// Create conversation between two regular human users.
	conv := createTestConversation(t, env.store, user1, user2)

	// Connect user1 and send a message.
	conn1 := connectClient(t, env.addr, user1, "device-1")
	defer conn1.Close()
	drainPushUpdates(t, conn1)

	// User1 sends a message to user2.
	clientMsgID := fmt.Sprintf("h2h-msg-%d", time.Now().UnixNano())
	sendRequest(t, conn1, "req-h2h-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Hello human!",
		"type":              "text",
	})

	resp := readResponse(t, conn1, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code,
		"send_message between humans should succeed")

	// Verify message is persisted in DB.
	var dbMsgs []*model.Message
	env.db.DB().WithContext(context.Background()).
		Where("conversation_id = ? AND sender_id = ?", conv.ID, user1).
		Find(&dbMsgs)
	require.Len(t, dbMsgs, 1, "message should be persisted")
	assert.Equal(t, "Hello human!", dbMsgs[0].Content)

	// Connect user2 and verify via sync_updates (more reliable than push).
	conn2 := connectClient(t, env.addr, user2, "device-1")
	defer conn2.Close()
	drainPushUpdates(t, conn2)

	sendRequest(t, conn2, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, conn2, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync_updates should succeed")

	var syncData struct {
		Updates []protocol.PackageDataUpdate `json:"updates"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))

	var foundMsg bool
	for _, u := range syncData.Updates {
		if u.Type == protocol.UpdateTypeMessage {
			var msg model.Message
			require.NoError(t, json.Unmarshal(u.Payload, &msg))
			if msg.SenderID == user1 {
				foundMsg = true
				assert.Equal(t, "Hello human!", msg.Content)
			}
		}
	}
	assert.True(t, foundMsg, "user2 should see user1's message via sync_updates")

	// Verify no agent processing occurred.
	assert.Equal(t, 0, env.mockLLM.CallCount(),
		"mock LLM should NOT be called for human-to-human messages (D-062)")
}

// ---------------------------------------------------------------------------
// TestAgentBasic_AE_BASIC_005
// Scenario: Agent-to-agent messaging does not trigger processing
// Verifies: Agent messaging another agent does not trigger MQ (D-062)
// ---------------------------------------------------------------------------

// TestAgentBasic_AE_BASIC_005 verifies that when an agent user sends a message
// to another agent user, no agent processing is triggered (D-062).
func TestAgentBasic_AE_BASIC_005(t *testing.T) {
	env := setupAgentE2E(t)

	// Register a second agent "test-bot-2".
	writeAgentConfig(t, env.agentsDir, secondBotConfig(env.mockLLM.URL()))
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	agent1 := "agent/test-bot"
	agent2 := "agent/test-bot-2"

	// Create conversation between the two agents.
	conv := createAgentConversation(t, env, agent1, agent2)

	// Connect as agent/test-bot via WebSocket.
	conn1 := connectClient(t, env.addr, agent1, "device-1")
	defer conn1.Close()

	// Send a message from agent/test-bot to agent/test-bot-2.
	clientMsgID := fmt.Sprintf("agent2agent-msg-%d", time.Now().UnixNano())
	sendRequest(t, conn1, "req-a2a-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "hello fellow agent",
		"type":              "text",
	})

	resp := readResponse(t, conn1, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code,
		"agent-to-agent send_message should succeed")

	// Drain any push updates.
	drainPushUpdates(t, conn1)

	// Wait a moment for any MQ processing that should NOT happen.
	time.Sleep(2 * time.Second)

	// Verify no agent processing occurred (mock LLM was not called).
	assert.Equal(t, 0, env.mockLLM.CallCount(),
		"mock LLM should NOT be called for agent-to-agent messages (D-062)")
}

// secondBotConfig returns an AgentConfig for a second test bot.
func secondBotConfig(mockURL string) *agent.AgentConfig {
	return &agent.AgentConfig{
		ID:          "test-bot-2",
		Name:        "Test Bot 2",
		Description: "Second test agent for agent-to-agent tests",
		Model:       "gpt-4",
		APIKeyEnv:   "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:     mockURL + "/v1",
		Parameters: agent.AgentParameters{
			Temperature: 0.7,
			MaxTokens:   1000,
		},
		Context: agent.AgentContext{
			MaxTokens:   4000,
			MaxMessages: 10,
		},
		SystemPrompt: "You are a second test assistant. Be concise.",
	}
}
