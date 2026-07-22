// Package e2e_test — agent_resume broadcast fix verification.
//
// This test verifies that after agent_resume, the broadcast works correctly
// without "user ID is required" errors. It exercises the full HITL flow:
// message -> interrupt -> resume -> broadcast -> message persistence.
//
// The E2E test environment bypasses MQ delivery (D-110): agent processing
// is triggered directly via executor.Execute and taskHandler.ProcessTask.
// This ensures the broadcast path is exercised even without a running MQ worker.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// HITL broadcast bot config
// ---------------------------------------------------------------------------

// hitlBroadcastBotConfig returns an AgentConfig that uses ask_user_question
// tool for HITL, followed by a final text response.
func hitlBroadcastBotConfig(mockURL string) *agent.AgentConfig {
	return &agent.AgentConfig{
		ID:          "hitl-broadcast-bot",
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

// writeHitlBroadcastBotConfig writes the HITL broadcast bot agent config to
// the given directory and reloads the registry.
func writeHitlBroadcastBotConfig(t *testing.T, env *agentE2EEnv) {
	t.Helper()
	writeAgentConfig(t, env.agentsDir, hitlBroadcastBotConfig(env.mockLLM.URL()))
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")
	_, found := env.registry.Get("hitl-broadcast-bot")
	require.True(t, found, "hitl-broadcast-bot should be registered")
}

// insertUserMessageDirect inserts a user message directly into the database
// (bypasses WebSocket send_message RPC). Used when the test only needs the
// message to exist in DB for agent context loading.
func insertUserMessageDirect(t *testing.T, env *agentE2EEnv, userID, convID, content string) string {
	t.Helper()
	clientMsgID := uuid.New().String()
	msg := &model.Message{
		ID:              uuid.New().String(),
		ClientMessageID: clientMsgID,
		ConversationID:  convID,
		SenderID:        userID,
		Content:         content,
		Type:            "text",
		Status:          "sent",
		CreatedAt:       time.Now(),
	}
	_, err := env.store.SendMessage(context.Background(), msg, []string{userID})
	require.NoError(t, err, "insert user message should succeed")
	return clientMsgID
}

// StreamingPayload mirrors the agent package's StreamingPayload for test assertions.
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
// Test: agent_resume broadcast fix -- single HITL round with full broadcast
// ---------------------------------------------------------------------------

// TestAgentResumeBroadcast_SingleHITL verifies the complete HITL resume
// broadcast pipeline:
//
//  1. User message -> agent processes -> ask_user_question -> interrupt
//     -> checkpoint saved -> RemoteCalling created with pending status.
//  2. Resume by resolving RemoteCalling -> agent resumes -> generates text
//     -> streaming broadcast -> message persist -> BroadcastRaw.
//
// This test verifies all broadcast calls after resume use valid (non-empty)
// user IDs (no "user ID is required" errors).
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

	// 1. Create conversation and connect WebSocket for receiving broadcasts.
	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	// 2. Insert user message into DB (bypasses WebSocket send_message).
	insertUserMessageDirect(t, env, userID, conv.ID, "Please do something and confirm with me first.")

	// 3. Trigger agent processing directly (bypasses MQ, D-110).
	msgID := fmt.Sprintf("msg-hb-%d", time.Now().UnixNano())
	err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
	// The executor returns nil when the agent interrupts (HITL).
	require.NoError(t, err, "agent executor should succeed (interrupt returns nil)")

	// 4. Wait for HITL interrupt -- RemoteCalling created with pending status.
	checkpointID, question := waitForHITLRemoteCalling(t, env, conv.ID, 30*time.Second)
	require.NotEmpty(t, checkpointID, "checkpoint_id should not be empty")
	assert.Contains(t, question, "confirm", "question should mention confirm")
	t.Logf("HITL interrupt: question=%q checkpoint=%s", question, checkpointID)

	// 5. Verify checkpoint exists in Redis.
	rdb := newAgentRedisClient(t)
	require.NoError(t, verifyRedisCheckpointExists(rdb, checkpointID),
		"checkpoint should exist in Redis after HITL")

	// 6. Verify lock is held during HITL.
	require.NoError(t, verifyRedisLockHeld(rdb, conv.ID),
		"conversation lock should be held during HITL")

	// 7. Resume the agent (bypasses MQ, simulates agent_resume RPC).
	err = triggerAgentResume(t, env, conv.ID, checkpointID, "", agentUserID, userID, deviceID, "Yes, proceed.")
	require.NoError(t, err, "triggerAgentResume should succeed")

	// 8. Wait for agent's final reply via broadcast.
	// This is the critical assertion: if any broadcast call fails with
	// "user ID is required", the reply will not be delivered.
	agentReply := waitForAgentReply(t, conn, "hitl-broadcast-bot", 30*time.Second)
	assert.NotEmpty(t, agentReply, "agent should produce a reply after resume")
	t.Logf("Agent reply after resume: %s", agentReply)

	// 9. Verify message is persisted in server DB.
	msgs, err := env.store.MessageStore().ListRecentByConversation(context.Background(), conv.ID, 50)
	require.NoError(t, err, "query messages should succeed")
	var foundAgentMsg bool
	for _, msg := range msgs {
		if msg.SenderID == agentUserID && msg.Content != "" {
			foundAgentMsg = true
			t.Logf("Agent message persisted: %s", msg.Content)
			break
		}
	}
	assert.True(t, foundAgentMsg, "agent message should be persisted in DB after resume")

	// 10. Verify conversation lock is released after completion.
	require.NoError(t, verifyRedisLockNotHeld(rdb, conv.ID),
		"conversation lock should be released after completion")

	// 11. Verify no pending RemoteCallings remain.
	pendingRCs, err := env.store.RemoteCallingStore().GetPendingByCheckpoint(context.Background(), checkpointID)
	require.NoError(t, err, "query pending remote callings should succeed")
	assert.Empty(t, pendingRCs, "no pending remote callings should remain after resume")
}

// ---------------------------------------------------------------------------
// Test: agent_resume broadcast fix -- streaming updates verification
// ---------------------------------------------------------------------------

// TestAgentResumeBroadcast_StreamingUpdates verifies that after resume,
// the streaming updates (Seq=0) are broadcast to the user without errors.
func TestAgentResumeBroadcast_StreamingUpdates(t *testing.T) {
	registerMultiHITLTool()
	env := setupAgentE2E(t)
	writeHitlBroadcastBotConfig(t, env)

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "ask_user_question", Arguments: `{"question":"Ready to continue?"}`},
		{Text: "Great! The task has been completed. Here is a detailed summary of what was done."},
	})

	userID := "user-stream-bcast"
	agentUserID := "agent/hitl-broadcast-bot"
	deviceID := "device-stream-bcast"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, deviceID)
	defer conn.Close()
	drainPushUpdates(t, conn)

	insertUserMessageDirect(t, env, userID, conv.ID, "Start the task.")

	msgID := fmt.Sprintf("msg-sb-%d", time.Now().UnixNano())
	err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
	require.NoError(t, err)

	checkpointID, _ := waitForHITLRemoteCalling(t, env, conv.ID, 30*time.Second)
	require.NotEmpty(t, checkpointID)

	// Resume.
	err = triggerAgentResume(t, env, conv.ID, checkpointID, "", agentUserID, userID, deviceID, "Ready!")
	require.NoError(t, err)

	// Collect streaming updates (Seq=0) and the final message (Seq>0).
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
				var payload streamingPayloadTest
				if err := json.Unmarshal(u.Payload, &payload); err == nil && payload.IsDone {
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
	t.Logf("Received %d streaming updates, stream done: %v", streamingCount, foundStreamDone)
}

// ---------------------------------------------------------------------------
// Test: agent_resume broadcast fix -- conversation update after cleanup
// ---------------------------------------------------------------------------

// TestAgentResumeBroadcast_ConversationUpdate verifies that after resume
// completes, a conversation update is broadcast to notify clients.
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

	insertUserMessageDirect(t, env, userID, conv.ID, "Go ahead.")

	msgID := fmt.Sprintf("msg-cb-%d", time.Now().UnixNano())
	err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
	require.NoError(t, err)

	checkpointID, _ := waitForHITLRemoteCalling(t, env, conv.ID, 30*time.Second)

	err = triggerAgentResume(t, env, conv.ID, checkpointID, "", agentUserID, userID, deviceID, "Confirmed")
	require.NoError(t, err)

	// Wait for the final message first.
	agentReply := waitForAgentReply(t, conn, "hitl-broadcast-bot", 30*time.Second)
	assert.NotEmpty(t, agentReply, "agent should produce a reply")

	// After the message, a conversation update should follow (cleanup broadcast).
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
// Test: agent_resume broadcast fix -- error path broadcast
// ---------------------------------------------------------------------------

// TestAgentResumeBroadcast_ErrorMessage verifies that after a resume failure
// (checkpoint expired/deleted), the error message is broadcast correctly
// without "user ID is required" errors.
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

	insertUserMessageDirect(t, env, userID, conv.ID, "Test error broadcast.")

	msgID := fmt.Sprintf("msg-eb-%d", time.Now().UnixNano())
	err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
	require.NoError(t, err)

	checkpointID, _ := waitForHITLRemoteCalling(t, env, conv.ID, 30*time.Second)

	// Delete the checkpoint from Redis to simulate expiration.
	rdb := newAgentRedisClient(t)
	key := fmt.Sprintf("agent:checkpoint:%s", checkpointID)
	require.NoError(t, rdb.Del(context.Background(), key).Err(), "delete checkpoint should succeed")

	// Resume -- should fail because checkpoint is gone, but error message
	// should still be broadcast without "user ID is required" error.
	err = triggerAgentResume(t, env, conv.ID, checkpointID, "", agentUserID, userID, deviceID, "Too late")
	// The resume handler returns nil for checkpoint-not-found (non-retriable).
	require.NoError(t, err, "resume handler should return nil even on checkpoint-not-found")

	// Wait for the error message broadcast.
	deadline := time.Now().Add(15 * time.Second)
	var errorMsgFound bool
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
				if err := json.Unmarshal(u.Payload, &msg); err == nil {
					if msg.SenderID == agentUserID {
						errorMsgFound = true
						t.Logf("Error message received: %s", msg.Content)
						goto doneErr
					}
				}
			}
			// Conversation update also acceptable (cleanup path).
			if u.Type == protocol.UpdateTypeConversation {
				errorMsgFound = true
				t.Logf("Conversation update received (cleanup path)")
				goto doneErr
			}
		}
	}
doneErr:

	assert.True(t, errorMsgFound, "should receive error broadcast after resume failure (no 'user ID is required')")
}
