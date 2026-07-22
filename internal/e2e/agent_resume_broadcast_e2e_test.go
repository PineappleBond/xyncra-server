// Package e2e_test -- agent_resume broadcast fix verification.
//
// This test verifies that after agent_resume, the broadcast works correctly
// without "user ID is required" errors. It exercises the full HITL flow:
// message -> interrupt -> resume -> broadcast -> message persistence.
//
// The E2E test environment bypasses MQ delivery (D-110): agent processing
// is triggered directly via executor.Execute and taskHandler.ProcessTask.
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
// HITL broadcast bot config
// ---------------------------------------------------------------------------

func hitlBroadcastBotConfig(mockURL string) *agent.AgentConfig {
	return &agent.AgentConfig{
		ID:          "agent/hitl-broadcast-bot",
		Name:        "HITL Broadcast Bot",
		Description: "Agent for testing HITL resume broadcast",
		Model:       "gpt-4",
		APIKeyEnv:   "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:     mockURL + "/v1",
		Parameters: agent.AgentParameters{
			Temperature: 0.7,
			MaxTokens:   2000,
		},
		Context: agent.AgentContext{
			MaxTokens:   4000,
			MaxMessages: 20,
		},
		Tools:        []string{"ask_user_question"},
		SystemPrompt: "You are a test assistant. Use ask_user_question to confirm each step before proceeding.",
	}
}

func writeHitlBroadcastBotConfig(t *testing.T, env *agentE2EEnv) {
	t.Helper()
	cfg := hitlBroadcastBotConfig(env.mockLLM.URL())
	env.registry.Register(cfg)
}

// streamingPayloadTest mirrors the agent package's StreamingPayload for assertions.
type streamingPayloadTest struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	StreamID       string `json:"stream_id"`
	Text           string `json:"text"`
	IsDone         bool   `json:"is_done"`
	IsAgent        bool   `json:"is_agent"`
	Timestamp      int64  `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Test 1: Single HITL round -- full broadcast pipeline
// ---------------------------------------------------------------------------

func TestAgentResumeBroadcast_SingleHITL(t *testing.T) {
	registerMultiHITLTool()
	env := setupAgentE2E(t)
	writeHitlBroadcastBotConfig(t, env)

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "ask_user_question", Arguments: `{"question":"Please confirm to proceed."}`},
		{Text: "Confirmed! Task completed successfully."},
	})

	userID := "user-hitl-bcast"
	agentUserID := "agent/hitl-broadcast-bot"
	deviceID := "device-hitl-bcast"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "Please confirm first.")

	// Trigger agent -- HITL interrupt returns ErrHITLInterrupted.
	msgID := fmt.Sprintf("msg-hb-%d", time.Now().UnixNano())
	err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
	require.Error(t, err)
	require.ErrorIs(t, err, agent.ErrHITLInterrupted)

	checkpointID, method := waitForHITLRemoteCalling(t, env, conv.ID, 30*time.Second)
	require.NotEmpty(t, checkpointID)
	assert.Equal(t, "ask_user", method)
	t.Logf("HITL interrupt: method=%q checkpoint=%s", method, checkpointID)

	rdb := newAgentRedisClient(t)
	require.NoError(t, verifyRedisCheckpointExists(rdb, checkpointID))

	// Resume the agent.
	err = triggerAgentResume(t, env, conv.ID, checkpointID, "", agentUserID, userID, deviceID, "Yes, proceed.")
	require.NoError(t, err, "triggerAgentResume should succeed")

	// Wait for agent reply via broadcast. If broadcast fails with
	// "user ID is required", the reply will not arrive and this times out.
	agentReply := waitForAgentReply(t, conn, "hitl-broadcast-bot", 30*time.Second)
	assert.NotEmpty(t, agentReply, "agent should produce a reply after resume")
	t.Logf("Agent reply: %s", agentReply)

	// Verify message persisted in DB.
	msgs, err := env.store.MessageStore().ListRecentByConversation(context.Background(), conv.ID, 50)
	require.NoError(t, err)
	var foundAgentMsg bool
	for _, msg := range msgs {
		if msg.SenderID == agentUserID && msg.Content != "" {
			foundAgentMsg = true
			break
		}
	}
	assert.True(t, foundAgentMsg, "agent message should be persisted after resume")

	// Verify lock released and no pending RemoteCallings.
	require.NoError(t, verifyRedisLockNotHeld(rdb, conv.ID))
	pendingRCs, err := env.store.RemoteCallingStore().GetPendingByCheckpoint(context.Background(), checkpointID)
	require.NoError(t, err)
	assert.Empty(t, pendingRCs, "no pending remote callings should remain")
}

// ---------------------------------------------------------------------------
// Test 2: Streaming updates verification
// ---------------------------------------------------------------------------

func TestAgentResumeBroadcast_StreamingUpdates(t *testing.T) {
	registerMultiHITLTool()
	env := setupAgentE2E(t)
	writeHitlBroadcastBotConfig(t, env)

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "ask_user_question", Arguments: `{"question":"Ready?"}`},
		{Text: "Great! The task has been completed. Here is a detailed summary."},
	})

	userID := "user-stream-bcast"
	agentUserID := "agent/hitl-broadcast-bot"
	deviceID := "device-stream-bcast"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "Start the task.")

	msgID := fmt.Sprintf("msg-sb-%d", time.Now().UnixNano())
	err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
	require.Error(t, err)
	require.ErrorIs(t, err, agent.ErrHITLInterrupted)

	checkpointID, _ := waitForHITLRemoteCalling(t, env, conv.ID, 30*time.Second)

	err = triggerAgentResume(t, env, conv.ID, checkpointID, "", agentUserID, userID, deviceID, "Ready!")
	require.NoError(t, err)

	var streamingCount int
	var foundStreamDone bool
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		pkg := readPackage(t, conn, remaining)
		if pkg.Type != protocol.PackageTypeUpdates {
			continue
		}
		var updates protocol.PackageDataUpdates
		require.NoError(t, json.Unmarshal(pkg.Data, &updates))
		for _, u := range updates.Updates {
			if u.Type == protocol.UpdateTypeStreaming && u.Seq == 0 {
				streamingCount++
				var p streamingPayloadTest
				if err := json.Unmarshal(u.Payload, &p); err == nil && p.IsDone {
					foundStreamDone = true
				}
			}
			if u.Type == protocol.UpdateTypeMessage && u.Seq > 0 {
				goto done
			}
		}
	}
done:

	assert.Greater(t, streamingCount, 0, "should receive streaming updates after resume")
	assert.True(t, foundStreamDone, "should receive streaming is_done signal")
	t.Logf("Streaming updates: %d, done: %v", streamingCount, foundStreamDone)
}

// ---------------------------------------------------------------------------
// Test 3: Conversation update after cleanup
// ---------------------------------------------------------------------------

func TestAgentResumeBroadcast_ConversationUpdate(t *testing.T) {
	registerMultiHITLTool()
	env := setupAgentE2E(t)
	writeHitlBroadcastBotConfig(t, env)

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "ask_user_question", Arguments: `{"question":"Confirm?"}`},
		{Text: "Done!"},
	})

	userID := "user-conv-bcast"
	agentUserID := "agent/hitl-broadcast-bot"
	deviceID := "device-conv-bcast"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "Go ahead.")

	msgID := fmt.Sprintf("msg-cb-%d", time.Now().UnixNano())
	err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
	require.Error(t, err)
	require.ErrorIs(t, err, agent.ErrHITLInterrupted)

	checkpointID, _ := waitForHITLRemoteCalling(t, env, conv.ID, 30*time.Second)

	err = triggerAgentResume(t, env, conv.ID, checkpointID, "", agentUserID, userID, deviceID, "Confirmed")
	require.NoError(t, err)

	agentReply := waitForAgentReply(t, conn, "hitl-broadcast-bot", 30*time.Second)
	assert.NotEmpty(t, agentReply)

	// Look for conversation update after the final message.
	convUpdateFound := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		pkg := readPackage(t, conn, remaining)
		if pkg.Type != protocol.PackageTypeUpdates {
			continue
		}
		var updates protocol.PackageDataUpdates
		if err := json.Unmarshal(pkg.Data, &updates); err != nil {
			continue
		}
		for _, u := range updates.Updates {
			if u.Type == protocol.UpdateTypeConversation {
				convUpdateFound = true
				goto doneConv
			}
		}
	}
doneConv:

	assert.True(t, convUpdateFound, "should receive conversation update after resume cleanup")
}

// ---------------------------------------------------------------------------
// Test 4: Error path -- checkpoint deleted before resume
// ---------------------------------------------------------------------------

func TestAgentResumeBroadcast_ErrorMessage(t *testing.T) {
	registerMultiHITLTool()
	env := setupAgentE2E(t)
	writeHitlBroadcastBotConfig(t, env)

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "ask_user_question", Arguments: `{"question":"Confirm?"}`},
		{Text: "Done!"},
	})

	userID := "user-err-bcast"
	agentUserID := "agent/hitl-broadcast-bot"
	deviceID := "device-err-bcast"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "Test error broadcast.")

	msgID := fmt.Sprintf("msg-eb-%d", time.Now().UnixNano())
	err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
	require.Error(t, err)
	require.ErrorIs(t, err, agent.ErrHITLInterrupted)

	checkpointID, _ := waitForHITLRemoteCalling(t, env, conv.ID, 30*time.Second)

	// Delete the checkpoint to simulate expiration.
	rdb := newAgentRedisClient(t)
	key := fmt.Sprintf("agent:checkpoint:%s", checkpointID)
	require.NoError(t, rdb.Del(context.Background(), key).Err())

	// Resume -- should fail but error broadcast should work.
	err = triggerAgentResume(t, env, conv.ID, checkpointID, "", agentUserID, userID, deviceID, "Too late")
	require.NoError(t, err, "resume handler returns nil on checkpoint-not-found")

	// Wait for error broadcast.
	deadline := time.Now().Add(15 * time.Second)
	var broadcastFound bool
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		pkg := readPackage(t, conn, remaining)
		if pkg.Type != protocol.PackageTypeUpdates {
			continue
		}
		var updates protocol.PackageDataUpdates
		if err := json.Unmarshal(pkg.Data, &updates); err != nil {
			continue
		}
		for _, u := range updates.Updates {
			if u.Type == protocol.UpdateTypeMessage && u.Seq > 0 {
				var msg model.Message
				if err := json.Unmarshal(u.Payload, &msg); err == nil && msg.SenderID == agentUserID {
					broadcastFound = true
					t.Logf("Error message: %s", msg.Content)
					goto doneErr
				}
			}
			if u.Type == protocol.UpdateTypeConversation {
				broadcastFound = true
				t.Log("Conversation update received (cleanup path)")
				goto doneErr
			}
		}
	}
doneErr:

	assert.True(t, broadcastFound, "should receive broadcast after resume failure (no 'user ID is required')")
}
