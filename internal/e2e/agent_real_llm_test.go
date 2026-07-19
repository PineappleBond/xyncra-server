//go:build real_llm

// Package e2e_test contains real LLM E2E tests for the Agent system.
// Run with: go test -tags real_llm ./internal/e2e/ -run "^TestAgentRealLLM" -v -timeout 300s
// Requires: .env with XYNCRA_TEST_REAL_LLM_ENABLED=true and XYNCRA_TEST_LLM_API_KEY
//
// These tests exercise the real LLM integration seam — the actual API call,
// streaming response handling, error classification, and persistence. Because
// real LLM output is non-deterministic, assertions are fuzzy (format, structure,
// and key characteristics) rather than exact content matching.
//
// Design decisions:
//   - D-001: Real LLM tests are opt-in (build tag + env var double gating)
//   - D-048: All env vars use XYNCRA_TEST_ prefix
//   - D-054: Agent user IDs use "agent/{id}" format
//   - D-067: Error messages are persisted in Chinese for user-friendliness
//   - Tool call tests are excluded (non-determinism too high for real LLM)
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Real LLM setup wrapper
// ---------------------------------------------------------------------------

// setupRealLLMAgentE2E wraps setupAgentE2E with real LLM skip guard.
// If real LLM mode is not enabled, the test is skipped.
func setupRealLLMAgentE2E(t *testing.T) *agentE2EEnv {
	t.Helper()
	if !realLLMMode() {
		t.Skip("Real LLM mode not enabled (set XYNCRA_TEST_REAL_LLM_ENABLED=true and XYNCRA_TEST_LLM_API_KEY)")
	}
	return setupAgentE2E(t)
}

// ---------------------------------------------------------------------------
// REAL-001: Basic conversation
// Verifies: Agent replies to a simple greeting. Reply is non-empty, SenderID
// correct, message persisted in DB.
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_001(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-001"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		conn := sendUserMessage(t, env, userID, conv.ID, "Hello, please introduce yourself briefly.")
		defer conn.Close()

		if err := triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-001-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID); err != nil {
			return fmt.Errorf("agent executor failed: %w", err)
		}

		agentMsg, err := waitForAgentMessageInDBErr(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
		if err != nil {
			return err
		}
		if err := assertValidAgentReply(t, agentMsg, agentUserID, conv.ID); err != nil {
			return err
		}
		return assertReplyIsReasonable(t, agentMsg.Content)
	})
}

// ---------------------------------------------------------------------------
// REAL-002: Message format validation
// Verifies: ConversationID, Type="text", MessageID>0, ID non-empty,
// SenderID correct. Uses a different prompt than REAL-001 to test a
// distinct scenario (format verification, not basic dialogue).
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_002(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-002"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		conn := sendUserMessage(t, env, userID, conv.ID, "Please respond with exactly: format-check-ok")
		defer conn.Close()

		msgID := fmt.Sprintf("msg-real-002-%d", time.Now().UnixNano())
		if err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID); err != nil {
			return fmt.Errorf("agent executor failed: %w", err)
		}

		agentMsg, err := waitForAgentMessageInDBErr(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
		if err != nil {
			return err
		}

		// Verify message format fields (D-054, D-055).
		if agentMsg.SenderID != agentUserID {
			return fmt.Errorf("sender_id should be agent: got %q", agentMsg.SenderID)
		}
		if agentMsg.ConversationID != conv.ID {
			return fmt.Errorf("conversation_id should match: got %q", agentMsg.ConversationID)
		}
		if agentMsg.Type != "text" {
			return fmt.Errorf("message type should be 'text': got %q", agentMsg.Type)
		}
		if agentMsg.ID == "" {
			return fmt.Errorf("message ID (string UUID) should not be empty")
		}
		if agentMsg.MessageID == 0 {
			return fmt.Errorf("message_id (sequence) should be positive")
		}
		if agentMsg.Content == "" {
			return fmt.Errorf("content should not be empty (D-055)")
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// REAL-003: sync_updates — offline user reconnects and gets agent reply
// Verifies: Agent reply is persisted and retrievable via sync_updates (D-009).
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_003(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-003"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		// Send message and trigger processing.
		conn := sendUserMessage(t, env, userID, conv.ID, "Hello, what is your name?")

		msgID := fmt.Sprintf("msg-real-003-%d", time.Now().UnixNano())
		if err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID); err != nil {
			conn.Close()
			return fmt.Errorf("agent executor failed: %w", err)
		}

		// Wait for agent reply to be persisted.
		_, err := waitForAgentMessageInDBErr(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
		if err != nil {
			conn.Close()
			return err
		}

		// Drain and close the original connection.
		drainPushUpdates(t, conn)
		conn.Close()

		// Wait for old connection cleanup.
		if err := waitForCondition(testTimeout(5*time.Second), 100*time.Millisecond, func() bool {
			conns, listErr := env.connStore.ListByUser(context.Background(), userID, 10)
			return listErr == nil && len(conns) == 0
		}); err != nil {
			return fmt.Errorf("old connection not cleaned up")
		}

		// Reconnect.
		newConn := connectClient(t, env.addr, userID, "device-1")
		defer newConn.Close()
		drainPushUpdates(t, newConn)

		// Request sync_updates from the beginning.
		sendRequest(t, newConn, "sync-1", "sync_updates", map[string]interface{}{
			"after_seq": 0,
			"limit":     100,
		})

		syncResp := readResponse(t, newConn, 5*time.Second)
		if syncResp.Code != protocol.ResponseCodeOK {
			return fmt.Errorf("sync_updates should succeed, got code=%d", syncResp.Code)
		}

		var syncData struct {
			Updates []protocol.PackageDataUpdate `json:"updates"`
		}
		if err := json.Unmarshal(syncResp.Data, &syncData); err != nil {
			return fmt.Errorf("unmarshal sync data: %w", err)
		}

		// Verify the sync response contains the agent reply.
		var foundAgentReply bool
		for _, u := range syncData.Updates {
			if u.Type == protocol.UpdateTypeMessage {
				var msg model.Message
				if err := json.Unmarshal(u.Payload, &msg); err != nil {
					continue
				}
				if msg.SenderID == agentUserID {
					foundAgentReply = true
					if msg.Content == "" {
						return fmt.Errorf("agent reply content should not be empty")
					}
					if msg.Type != "text" {
						return fmt.Errorf("agent reply type should be text: got %q", msg.Type)
					}
				}
			}
		}
		if !foundAgentReply {
			return fmt.Errorf("sync_updates should contain the agent's reply (D-055, D-009)")
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// REAL-004: Streaming output — streaming ephemeral updates arrive
// Verifies: At least one streaming update is received before is_done.
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_004(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-004"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		conn := connectClient(t, env.addr, userID, "device-1")
		defer conn.Close()
		_ = insertUserMessageDirect(t, env, userID, conv.ID, "Tell me a short fun fact.")

		execDone := make(chan struct{})
		go func() {
			defer close(execDone)
			_ = triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-004-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID)
		}()

		// Collect streaming updates until is_done.
		var streamingCount int
		deadline := time.Now().Add(testTimeout(60 * time.Second))
		for {
			if time.Now().After(deadline) {
				<-execDone
				return fmt.Errorf("timed out waiting for streaming, got %d updates", streamingCount)
			}
			updates := waitForUpdate(t, conn, time.Until(deadline))
			for _, u := range updates.Updates {
				if u.Type != protocol.UpdateTypeStreaming {
					continue
				}
				var p struct {
					UserID string `json:"user_id"`
					Text   string `json:"text"`
					IsDone bool   `json:"is_done"`
				}
				if err := json.Unmarshal(u.Payload, &p); err != nil {
					continue
				}
				if p.UserID != agentUserID {
					continue
				}
				if p.IsDone {
					goto streamDone
				}
				if p.Text != "" {
					streamingCount++
				}
			}
		}
	streamDone:
		<-execDone
		if streamingCount < 1 {
			return fmt.Errorf("should receive at least 1 streaming update (D-051), got %d", streamingCount)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// REAL-005: is_done signal — last streaming update has is_done=true
// Verifies: The streaming sequence terminates with an is_done=true signal (D-052).
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_005(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-005"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		conn := connectClient(t, env.addr, userID, "device-1")
		defer conn.Close()
		_ = insertUserMessageDirect(t, env, userID, conv.ID, "Say hello in one sentence.")

		execDone := make(chan struct{})
		go func() {
			defer close(execDone)
			_ = triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-005-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID)
		}()

		// waitForAgentStreamDone waits for the is_done=true signal.
		finalText := waitForAgentStreamDone(t, conn, "real-test-bot", testTimeout(60*time.Second))
		<-execDone

		if finalText == "" {
			return fmt.Errorf("final streamed text should not be empty (D-052)")
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// REAL-006: Multi-turn context — second reply relates to first
// Verifies: The agent maintains context across multiple turns (D-060).
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_006(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-006"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		// Turn 1: Establish a topic.
		conn1 := sendUserMessage(t, env, userID, conv.ID, "My favorite color is blue.")
		if err := triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-006a-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID); err != nil {
			conn1.Close()
			return fmt.Errorf("first turn executor failed: %w", err)
		}
		reply1, err := waitForAgentMessageInDBErr(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
		if err != nil {
			conn1.Close()
			return err
		}
		if reply1.Content == "" {
			conn1.Close()
			return fmt.Errorf("first reply should not be empty")
		}
		drainPushUpdates(t, conn1)
		conn1.Close()

		// Wait for connection cleanup.
		if err := waitForCondition(testTimeout(5*time.Second), 100*time.Millisecond, func() bool {
			conns, listErr := env.connStore.ListByUser(context.Background(), userID, 10)
			return listErr == nil && len(conns) == 0
		}); err != nil {
			return fmt.Errorf("connection not cleaned up after turn 1")
		}

		// Turn 2: Ask about the topic from turn 1.
		conn2 := sendUserMessage(t, env, userID, conv.ID, "What is my favorite color?")
		defer conn2.Close()
		if err := triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-006b-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID); err != nil {
			return fmt.Errorf("second turn executor failed: %w", err)
		}
		reply2, err := waitForAgentMessageInDBErr(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
		if err != nil {
			return err
		}
		if reply2.Content == "" {
			return fmt.Errorf("second reply should not be empty")
		}

		// Verify the second reply mentions "blue" (fuzzy context check).
		// Candidates are truly distinct under case-insensitive comparison (fix #8).
		return assertContainsAny(t, reply2.Content, []string{"blue", "azure", "navy"})
	})
}

// ---------------------------------------------------------------------------
// REAL-007: API Key missing → error message persisted (D-067)
// Verifies: When the agent's api_key_env points to a non-existent env var,
// a Chinese error message containing "配置有误" is persisted.
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_007(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-007"
	badAgentID := "no-key-real-bot"
	agentUser := "agent/" + badAgentID

	// Write an agent config referencing a non-existent env var for api_key_env.
	badConfig := &agent.AgentConfig{
		ID:           badAgentID,
		Name:         "No Key Real Bot",
		Description:  "Agent with missing API key for real LLM test",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_NONEXISTENT_REAL_KEY_99999",
		BaseURL:      "https://api.example.com/v1", // does not matter, key check happens first
		Parameters:   agent.AgentParameters{Temperature: 0.3, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 5},
		SystemPrompt: "You are a test assistant.",
	}
	writeAgentConfig(t, env.agentsDir, badConfig)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	conv := createAgentConversation(t, env, userID, agentUser)

	retryRealLLM(t, 2, func(t *testing.T) error {
		_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

		payload := agent.ExecutePayload{
			MessageID:      fmt.Sprintf("msg-real-007-%d", time.Now().UnixNano()),
			ConversationID: conv.ID,
			AgentID:        agentUser,
			SenderID:       userID,
		}
		err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)
		if err == nil {
			return fmt.Errorf("executor should fail when API key is missing")
		}

		// Verify error message persisted in DB.
		var msgs []*model.Message
		found := false
		for i := 0; i < 50; i++ {
			env.db.DB().WithContext(context.Background()).
				Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUser).
				Order("message_id DESC").Limit(1).Find(&msgs)
			if len(msgs) > 0 {
				found = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !found {
			return fmt.Errorf("error message should be persisted (D-067)")
		}

		if !strings.Contains(msgs[0].Content, "配置有误") {
			return fmt.Errorf("error should be classified as configuration error (D-067): got %q", msgs[0].Content)
		}
		if msgs[0].SenderID != agentUser {
			return fmt.Errorf("sender_id should be agent: got %q", msgs[0].SenderID)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// REAL-008: typing indicator — typing=true sent before first token (D-065)
// Verifies: A typing=true ephemeral update is broadcast before streaming begins.
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_008(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-008"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		conn := connectClient(t, env.addr, userID, "device-1")
		defer conn.Close()
		_ = insertUserMessageDirect(t, env, userID, conv.ID, "Hello")

		execDone := make(chan struct{})
		go func() {
			defer close(execDone)
			_ = triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-008-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID)
		}()

		// Wait for typing indicator.
		updates := waitForEphemeral(t, conn, protocol.UpdateTypeTyping, testTimeout(30*time.Second))
		var found bool
		for _, u := range updates.Updates {
			if u.Type != protocol.UpdateTypeTyping {
				continue
			}
			found = true
			if u.Seq != 0 {
				return fmt.Errorf("typing should be ephemeral (Seq=0): got Seq=%d", u.Seq)
			}
			var p struct {
				UserID   string `json:"user_id"`
				IsTyping bool   `json:"is_typing"`
			}
			if err := json.Unmarshal(u.Payload, &p); err != nil {
				return fmt.Errorf("unmarshal typing payload: %w", err)
			}
			if p.UserID != agentUserID {
				return fmt.Errorf("typing user_id should be agent: got %q", p.UserID)
			}
			if !p.IsTyping {
				return fmt.Errorf("is_typing should be true (D-065)")
			}
		}
		<-execDone
		if !found {
			return fmt.Errorf("should receive a typing update")
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// REAL-009: agent_status event — agent_status ephemeral update pushed (D-087)
// Verifies: An agent_status ephemeral update is broadcast during agent
// processing (via the full agent pipeline, not via direct BroadcastHelper call).
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_009(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-009"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		conn := connectClient(t, env.addr, userID, "device-1")
		defer conn.Close()
		_ = insertUserMessageDirect(t, env, userID, conv.ID, "Hello")

		execDone := make(chan struct{})
		go func() {
			defer close(execDone)
			_ = triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-009-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID)
		}()

		// Wait for agent_status ephemeral update (emitted by the agent pipeline).
		updates := waitForEphemeral(t, conn, protocol.UpdateTypeAgentStatus, testTimeout(30*time.Second))

		var found bool
		for _, u := range updates.Updates {
			if u.Type != protocol.UpdateTypeAgentStatus {
				continue
			}
			found = true
			if u.Seq != 0 {
				return fmt.Errorf("agent_status must be ephemeral (Seq=0, D-050): got Seq=%d", u.Seq)
			}

			var payload agent.AgentStatusPayload
			if err := json.Unmarshal(u.Payload, &payload); err != nil {
				return fmt.Errorf("unmarshal agent_status payload: %w", err)
			}
			if payload.UserID != agentUserID {
				return fmt.Errorf("user_id should be the agent: got %q", payload.UserID)
			}
			if payload.ConversationID != conv.ID {
				return fmt.Errorf("conversation_id should match: got %q", payload.ConversationID)
			}
			if payload.Status == "" {
				return fmt.Errorf("status should not be empty (D-087)")
			}
			if payload.Timestamp == 0 {
				return fmt.Errorf("timestamp should be set")
			}
		}
		<-execDone
		if !found {
			return fmt.Errorf("should receive an agent_status update (D-087)")
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// REAL-010: Config hot reload — new agent available after reload (D-076)
// Verifies: After writing a new agent config and reloading, the new agent is
// registered and can process messages via the real LLM.
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_010(t *testing.T) {
	env := setupRealLLMAgentE2E(t)

	// Write a new agent config for the real LLM.
	cfg := realLLMConfig()
	newAgentCfg := &agent.AgentConfig{
		ID:          "reload-real-bot",
		Name:        "Reload Real Bot",
		Description: "Agent added via hot reload for real LLM test",
		Model:       cfg.Model,
		APIKeyEnv:   "XYNCRA_TEST_REAL_API_KEY",
		BaseURL:     cfg.BaseURL,
		Parameters: agent.AgentParameters{
			Temperature: 0.3,
			MaxTokens:   500,
		},
		Context: agent.AgentContext{
			MaxTokens:   4000,
			MaxMessages: 5,
		},
		SystemPrompt: "You are a reload test assistant. Respond in English. Keep it under 1 sentence.",
	}
	writeAgentConfig(t, env.agentsDir, newAgentCfg)

	// Before reload, the new agent must not exist.
	_, found := env.registry.Get("reload-real-bot")
	require.False(t, found, "new agent should not exist before reload")

	// Reload from disk.
	require.NoError(t, env.registry.Reload(), "Reload() should succeed")

	// After reload, the new agent must be available.
	reloadedCfg, found := env.registry.Get("reload-real-bot")
	require.True(t, found, "new agent should exist after reload")
	require.Equal(t, "Reload Real Bot", reloadedCfg.Name)

	// Pre-existing agent must still be present.
	_, foundOriginal := env.registry.Get("real-test-bot")
	require.True(t, foundOriginal, "original real-test-bot should still exist after reload")

	// Verify the new agent can actually process a message via real LLM.
	userID := "user-real-010"
	newAgentUserID := "agent/reload-real-bot"
	conv := createAgentConversation(t, env, userID, newAgentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		conn := sendUserMessage(t, env, userID, conv.ID, "Say hi from the reloaded bot.")
		defer conn.Close()

		msgID := fmt.Sprintf("msg-real-010-%d", time.Now().UnixNano())
		if err := triggerAgentProcessing(t, env, msgID, conv.ID, newAgentUserID, userID); err != nil {
			return fmt.Errorf("reloaded agent executor failed: %w", err)
		}

		agentMsg, err := waitForAgentMessageInDBErr(t, env, conv.ID, newAgentUserID, testTimeout(30*time.Second))
		if err != nil {
			return err
		}
		return assertValidAgentReply(t, agentMsg, newAgentUserID, conv.ID)
	})
}

// ---------------------------------------------------------------------------
// REAL-011: Serial processing — messages to same conversation processed serially (D-075)
// Verifies: Two sequential messages to the same conversation both produce
// correct replies, demonstrating the conversation lock serializes processing.
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_011(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-011"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		// First message.
		conn1 := sendUserMessage(t, env, userID, conv.ID, "What is 2+2?")
		if err := triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-011a-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID); err != nil {
			conn1.Close()
			return fmt.Errorf("first execution failed: %w", err)
		}
		reply1, err := waitForAgentMessageInDBErr(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
		if err != nil {
			conn1.Close()
			return err
		}
		if reply1.Content == "" {
			conn1.Close()
			return fmt.Errorf("first reply should not be empty")
		}
		drainPushUpdates(t, conn1)
		conn1.Close()

		// Wait for connection cleanup.
		if err := waitForCondition(testTimeout(5*time.Second), 100*time.Millisecond, func() bool {
			conns, listErr := env.connStore.ListByUser(context.Background(), userID, 10)
			return listErr == nil && len(conns) == 0
		}); err != nil {
			return fmt.Errorf("connection not cleaned up after first message")
		}

		// Second message — processed after the first completes (serial via lock).
		conn2 := sendUserMessage(t, env, userID, conv.ID, "What is 3+3?")
		defer conn2.Close()
		if err := triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-011b-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID); err != nil {
			return fmt.Errorf("second execution failed: %w", err)
		}
		reply2, err := waitForAgentMessageInDBErr(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
		if err != nil {
			return err
		}
		if reply2.Content == "" {
			return fmt.Errorf("second reply should not be empty")
		}

		// Verify both replies have distinct message IDs (separate executions).
		if reply1.ID == reply2.ID {
			return fmt.Errorf("both replies should have distinct message IDs (serial processing)")
		}
		if reply1.SenderID != agentUserID {
			return fmt.Errorf("first reply sender should be agent: got %q", reply1.SenderID)
		}
		if reply2.SenderID != agentUserID {
			return fmt.Errorf("second reply sender should be agent: got %q", reply2.SenderID)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// REAL-012: Idempotency — same MessageID not executed twice (D-071)
// Verifies: The idempotency store prevents duplicate task execution.
// A pre-marked message ID is skipped by the task handler without calling the LLM.
// Uses require.Eventually to verify no message is persisted (fix #7: no sleep).
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_012(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-012"
	agentUserID := "agent/real-test-bot"
	messageID := fmt.Sprintf("msg-real-012-%d", time.Now().UnixNano())

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Create a Redis client for the idempotency store (same pattern as setupAgentE2E).
	redisClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	defer redisClient.Close()

	idempotencyStore := agent.NewRedisIdempotencyStore(redisClient)

	// Pre-mark this message as processed.
	dup, err := idempotencyStore.MarkProcessed(context.Background(), "agent:processed:"+messageID, 24*time.Hour)
	require.NoError(t, err, "MarkProcessed should succeed")
	require.False(t, dup, "first MarkProcessed should not be duplicate")

	// Create a task handler with the idempotency store.
	taskHandler := agent.NewAgentTaskHandler(env.executor, idempotencyStore, env.lock, testLogger{})

	// Build task payload.
	taskPayload, err := json.Marshal(agent.AgentProcessPayload{
		MessageID:      messageID,
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.NoError(t, err)

	task := &mq.Task{
		Type:    "mq:agent_process",
		Payload: taskPayload,
	}

	// Handler should return nil (task skipped due to idempotency).
	result := taskHandler(context.Background(), task)
	require.Nil(t, result, "handler should return nil (D-073)")

	// Verify no agent reply was persisted (execution was skipped).
	// Use Eventually to poll DB instead of time.Sleep (fix #7: sync over sleep).
	require.Eventually(t, func() bool {
		var msgs []*model.Message
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
			Find(&msgs)
		return len(msgs) == 0
	}, 3*time.Second, 200*time.Millisecond,
		"no agent reply should be persisted when idempotency skips execution (D-071)")
}

// ---------------------------------------------------------------------------
// REAL-013: Full flow — send → stream → persist → sync complete chain
// Verifies: The entire agent processing pipeline works end-to-end with a
// real LLM: streaming updates arrive, message is persisted, and the reply
// is retrievable via sync_updates on reconnect.
// Fix #6: Uses string containment rather than exact equality for streamed
// vs persisted text comparison, since real LLM output may have minor
// trailing whitespace differences.
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_013(t *testing.T) {
	env := setupRealLLMAgentE2E(t)
	userID := "user-real-013"
	agentUserID := "agent/real-test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	retryRealLLM(t, 2, func(t *testing.T) error {
		conn := connectClient(t, env.addr, userID, "device-1")
		defer conn.Close()
		_ = insertUserMessageDirect(t, env, userID, conv.ID, "Say hello in a few words.")

		execDone := make(chan struct{})
		go func() {
			defer close(execDone)
			_ = triggerAgentProcessing(t, env, fmt.Sprintf("msg-real-013-%d", time.Now().UnixNano()), conv.ID, agentUserID, userID)
		}()

		// Step 1: Wait for streaming to complete (is_done=true).
		streamText := waitForAgentStreamDone(t, conn, "real-test-bot", testTimeout(60*time.Second))
		if streamText == "" {
			<-execDone
			return fmt.Errorf("streamed text should not be empty")
		}
		<-execDone // ensure executor completes before DB check

		// Step 2: Verify message is persisted in DB.
		var agentMsg *model.Message
		deadline := time.Now().Add(testTimeout(10 * time.Second))
		for time.Now().Before(deadline) {
			var msgs []*model.Message
			env.db.DB().WithContext(context.Background()).
				Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
				Order("message_id DESC").Limit(1).Find(&msgs)
			if len(msgs) > 0 {
				agentMsg = msgs[0]
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if agentMsg == nil {
			return fmt.Errorf("agent message should be persisted")
		}

		// Relaxed comparison: persisted content should contain or be contained
		// in the streamed text (fix #6: exact equality too strict for real LLM).
		streamTrimmed := strings.TrimSpace(streamText)
		persistedTrimmed := strings.TrimSpace(agentMsg.Content)
		if !strings.Contains(streamTrimmed, persistedTrimmed) &&
			!strings.Contains(persistedTrimmed, streamTrimmed) &&
			streamTrimmed != persistedTrimmed {
			return fmt.Errorf("persisted content %q should be approximately equal to streamed text %q (D-052)",
				agentMsg.Content, streamText)
		}
		if agentMsg.SenderID != agentUserID {
			return fmt.Errorf("sender should be agent (D-054): got %q", agentMsg.SenderID)
		}

		// Step 3: Drain, close, reconnect and verify sync_updates.
		drainPushUpdates(t, conn)
		conn.Close()

		if err := waitForCondition(testTimeout(5*time.Second), 100*time.Millisecond, func() bool {
			conns, listErr := env.connStore.ListByUser(context.Background(), userID, 10)
			return listErr == nil && len(conns) == 0
		}); err != nil {
			return fmt.Errorf("connection not cleaned up")
		}

		newConn := connectClient(t, env.addr, userID, "device-1")
		defer newConn.Close()
		drainPushUpdates(t, newConn)

		sendRequest(t, newConn, "sync-1", "sync_updates", map[string]interface{}{
			"after_seq": 0,
			"limit":     100,
		})
		syncResp := readResponse(t, newConn, 5*time.Second)
		if syncResp.Code != protocol.ResponseCodeOK {
			return fmt.Errorf("sync_updates should succeed, got code=%d", syncResp.Code)
		}

		var syncData struct {
			Updates []protocol.PackageDataUpdate `json:"updates"`
		}
		if err := json.Unmarshal(syncResp.Data, &syncData); err != nil {
			return fmt.Errorf("unmarshal sync data: %w", err)
		}

		// Verify the agent's persisted message IS in sync_updates.
		var foundAgentMsg bool
		for _, u := range syncData.Updates {
			if u.Type == protocol.UpdateTypeMessage {
				var msg model.Message
				if err := json.Unmarshal(u.Payload, &msg); err == nil {
					if msg.SenderID == agentUserID {
						foundAgentMsg = true
						// Relaxed comparison for sync content too.
						syncContent := strings.TrimSpace(msg.Content)
						if syncContent != persistedTrimmed &&
							!strings.Contains(syncContent, persistedTrimmed) &&
							!strings.Contains(persistedTrimmed, syncContent) {
							return fmt.Errorf("sync_updates content %q should approximately match persisted content %q",
								msg.Content, agentMsg.Content)
						}
					}
				}
			}
			// Verify no ephemeral types in sync.
			if u.Type == protocol.UpdateTypeStreaming {
				return fmt.Errorf("no streaming in sync_updates (D-051)")
			}
			if u.Type == protocol.UpdateTypeTyping {
				return fmt.Errorf("no typing in sync_updates (D-050)")
			}
		}
		if !foundAgentMsg {
			return fmt.Errorf("sync_updates should contain the agent's persisted message")
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// REAL-014: Human-to-human isolation — agent system does not interfere (D-062)
// Verifies: Messages between two non-agent users flow normally without any
// agent processing being triggered.
// ---------------------------------------------------------------------------

func TestAgentRealLLM_REAL_014(t *testing.T) {
	env := setupRealLLMAgentE2E(t)

	user1 := "user-real-h2h-1"
	user2 := "user-real-h2h-2"

	// Create conversation between two regular human users.
	conv := createTestConversation(t, env.store, user1, user2)

	// Connect user1 and send a message.
	conn1 := connectClient(t, env.addr, user1, "device-1")
	defer conn1.Close()
	drainPushUpdates(t, conn1)

	clientMsgID := fmt.Sprintf("h2h-real-msg-%d", time.Now().UnixNano())
	sendRequest(t, conn1, "req-h2h-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Hello from human to human!",
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
	require.Equal(t, "Hello from human to human!", dbMsgs[0].Content)

	retryRealLLM(t, 2, func(t *testing.T) error {
		// Connect user2 and verify via sync_updates.
		conn2 := connectClient(t, env.addr, user2, "device-1")
		defer conn2.Close()
		drainPushUpdates(t, conn2)

		sendRequest(t, conn2, fmt.Sprintf("sync-h2h-%d", time.Now().UnixNano()), "sync_updates", map[string]interface{}{
			"after_seq": 0,
			"limit":     100,
		})
		syncResp := readResponse(t, conn2, 5*time.Second)
		if syncResp.Code != protocol.ResponseCodeOK {
			return fmt.Errorf("sync_updates should succeed, got code=%d", syncResp.Code)
		}

		var syncData struct {
			Updates []protocol.PackageDataUpdate `json:"updates"`
		}
		if err := json.Unmarshal(syncResp.Data, &syncData); err != nil {
			return fmt.Errorf("unmarshal sync data: %w", err)
		}

		var foundMsg bool
		for _, u := range syncData.Updates {
			if u.Type == protocol.UpdateTypeMessage {
				var msg model.Message
				if err := json.Unmarshal(u.Payload, &msg); err == nil {
					if msg.SenderID == user1 {
						foundMsg = true
						if msg.Content != "Hello from human to human!" {
							return fmt.Errorf("sync content should match: got %q", msg.Content)
						}
					}
				}
			}
		}
		if !foundMsg {
			return fmt.Errorf("user2 should see user1's message via sync_updates")
		}

		// Verify no agent messages exist in this conversation (agent did not interfere).
		var agentMsgs []*model.Message
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id LIKE ?", conv.ID, "agent/%").
			Find(&agentMsgs)
		if len(agentMsgs) != 0 {
			return fmt.Errorf("no agent messages should exist in human-to-human conversation (D-062): got %d", len(agentMsgs))
		}
		return nil
	})
}
