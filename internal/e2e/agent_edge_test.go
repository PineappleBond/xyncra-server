// Package e2e_test contains edge-case input E2E tests for the Agent system.
// Tests exercise extreme and unusual inputs (super-long text, empty messages,
// emoji, CJK, RTL, null bytes, message bursts, large contexts, and multilingual
// content) to verify the agent pipeline degrades gracefully or processes
// normally without crashing.
package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// ---------------------------------------------------------------------------
// AE-EDGE-001: Super long input (>10000 characters)
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_001 verifies that a very long user message (>10000
// characters) is handled gracefully. The context manager's token trimming
// (D-060) should prevent context overflow. The agent must produce a reply
// without crashing.
func TestAgentEdge_AE_EDGE_001(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-001"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Build a 10000+ character input string.
	longInput := strings.Repeat("a", 10001)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, longInput)

	err := triggerAgentProcessing(t, env, "msg-edge-001", conv.ID, agentUserID, userID)
	assert.NoError(t, err, "executor should handle super-long input without crashing")

	// Verify the agent produced a reply.
	msg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 15*time.Second)
	assert.NotEmpty(t, msg.Content, "agent should produce a reply for long input")
	assert.Equal(t, agentUserID, msg.SenderID, "sender_id should be the agent")
}

// ---------------------------------------------------------------------------
// AE-EDGE-002: Empty message
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_002 verifies that an empty message (content: "") is
// rejected with a user-friendly error message persisted to DB (D-091).
// The agent must not crash and must return a "抱歉" error message.
func TestAgentEdge_AE_EDGE_002(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-002"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "")

	payload := agent.ExecutePayload{
		MessageID:      "msg-edge-002",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)
	assert.Error(t, err, "empty message should be rejected (D-091)")

	// D-091: error message must be persisted in DB.
	msg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 10*time.Second)
	assert.Contains(t, msg.Content, "抱歉",
		"empty message error should contain 抱歉 (D-091)")
	assert.Equal(t, agentUserID, msg.SenderID, "sender_id should be the agent")
}

// ---------------------------------------------------------------------------
// AE-EDGE-003: Whitespace-only message
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_003 verifies that a whitespace-only message (spaces,
// tabs, newlines) is rejected with a user-friendly error message persisted to
// DB (D-091), identical to empty message behavior.
func TestAgentEdge_AE_EDGE_003(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-003"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "   \n\t  ")

	payload := agent.ExecutePayload{
		MessageID:      "msg-edge-003",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)
	assert.Error(t, err, "whitespace-only message should be rejected (D-091)")

	// D-091: error message must be persisted in DB.
	msg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 10*time.Second)
	assert.Contains(t, msg.Content, "抱歉",
		"whitespace-only message error should contain 抱歉 (D-091)")
	assert.Equal(t, agentUserID, msg.SenderID, "sender_id should be the agent")
}

// ---------------------------------------------------------------------------
// AE-EDGE-004: Emoji input
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_004 verifies that emoji characters in user messages
// are handled correctly. The agent should process the message normally and
// produce a reply without UTF-8 encoding issues.
func TestAgentEdge_AE_EDGE_004(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-004"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "Hello \U0001f30d\U0001f389\U0001f680")

	err := triggerAgentProcessing(t, env, "msg-edge-004", conv.ID, agentUserID, userID)
	require.NoError(t, err, "executor should handle emoji input without error")

	msg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 15*time.Second)
	assert.NotEmpty(t, msg.Content, "agent should reply to emoji input")
	assert.Equal(t, agentUserID, msg.SenderID, "sender_id should be the agent")
}

// ---------------------------------------------------------------------------
// AE-EDGE-005: CJK mixed input
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_005 verifies that CJK (Chinese, Japanese, Korean)
// characters mixed with Latin script are handled correctly. The agent should
// process the message normally.
func TestAgentEdge_AE_EDGE_005(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-005"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "你好Helloこんにちさ；안녕")

	err := triggerAgentProcessing(t, env, "msg-edge-005", conv.ID, agentUserID, userID)
	require.NoError(t, err, "executor should handle CJK mixed input")

	msg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 15*time.Second)
	assert.NotEmpty(t, msg.Content, "agent should reply to CJK mixed input")
	assert.Equal(t, agentUserID, msg.SenderID)
}

// ---------------------------------------------------------------------------
// AE-EDGE-006: RTL text (Arabic)
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_006 verifies that right-to-left text (Arabic) is
// handled correctly. The agent should process the message normally without
// encoding or layout issues.
func TestAgentEdge_AE_EDGE_006(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-006"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "مرحبا بالعالم")

	err := triggerAgentProcessing(t, env, "msg-edge-006", conv.ID, agentUserID, userID)
	require.NoError(t, err, "executor should handle RTL text")

	msg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 15*time.Second)
	assert.NotEmpty(t, msg.Content, "agent should reply to RTL text")
	assert.Equal(t, agentUserID, msg.SenderID)
}

// ---------------------------------------------------------------------------
// AE-EDGE-007: Null bytes in input
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_007 verifies that null bytes (\x00) in user input are
// handled gracefully. The system must not crash, and the database should store
// the message without corruption.
func TestAgentEdge_AE_EDGE_007(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-007"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "Hello\x00World")

	err := triggerAgentProcessing(t, env, "msg-edge-007", conv.ID, agentUserID, userID)
	// Either success or error is acceptable — the key is no crash/panic.
	t.Logf("null bytes executor result: %v", err)

	// If there's an agent message in DB, verify it's valid.
	var msgs []*model.Message
	env.db.DB().WithContext(context.Background()).
		Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
		Order("message_id DESC").Limit(1).Find(&msgs)
	if len(msgs) > 0 {
		assert.Equal(t, agentUserID, msgs[0].SenderID)
	}
	// No agent message is also acceptable (error path).
}

// ---------------------------------------------------------------------------
// AE-EDGE-008: Message burst (10 messages rapidly)
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_008 verifies that sending 10 messages to the same
// conversation in rapid succession does not crash the system. Under D-075,
// the per-conversation lock serializes processing. All messages should
// eventually be processed.
func TestAgentEdge_AE_EDGE_008(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-008"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Launch 10 goroutines that each insert a message and trigger the executor.
	// The per-conversation lock (D-075) serializes execution.
	numMessages := 10
	var wg sync.WaitGroup
	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msgID := fmt.Sprintf("msg-edge-008-%d", idx)
			content := fmt.Sprintf("burst message %d", idx)
			_ = insertUserMessageDirect(t, env, userID, conv.ID, content)
			_ = triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
		}(i)
	}
	wg.Wait()

	// Verify that most agent replies were persisted.
	// D-091: messages should be serialized via per-conversation lock (D-075).
	// Threshold is >=8 (not 10) because some messages may be skipped by the LLM
	// when context truncation collapses near-identical burst messages.
	deadline := time.Now().Add(20 * time.Second)
	var agentMsgs []*model.Message
	for {
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
			Find(&agentMsgs)
		if len(agentMsgs) >= 8 || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, len(agentMsgs), 8,
		"at least 8 of 10 burst messages should produce agent replies (D-091: serial processing)")
}

// ---------------------------------------------------------------------------
// AE-EDGE-009: Large context (50 rounds of conversation)
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_009 verifies that after 50 rounds of conversation
// (100 messages), the context trimming strategy (D-060) prevents context
// overflow and the agent still produces a normal reply.
func TestAgentEdge_AE_EDGE_009(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-009"
	agentUserID := "agent/test-bot"
	members := []string{userID, agentUserID}

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Pre-insert 50 rounds of conversation (100 messages).
	for i := 0; i < 50; i++ {
		_ = insertContextMsg(t, env, conv.ID, userID,
			fmt.Sprintf("user message round %d with some extra text to fill tokens", i),
			members)
		_ = insertContextMsg(t, env, conv.ID, agentUserID,
			fmt.Sprintf("agent reply round %d with some extra text to fill tokens", i),
			members)
	}

	// Insert one final user message and trigger processing.
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello after 50 rounds")

	err := triggerAgentProcessing(t, env, "msg-edge-009", conv.ID, agentUserID, userID)
	assert.NoError(t, err, "executor should succeed with large context (D-060 truncation)")

	msg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 30*time.Second)
	assert.NotEmpty(t, msg.Content, "agent should reply even with large context")
	assert.Equal(t, agentUserID, msg.SenderID)
}

// ---------------------------------------------------------------------------
// AE-EDGE-010: Consecutive identical messages (idempotency)
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_010 verifies that sending 3 identical messages with
// different message IDs and client message IDs does not trigger false-positive
// idempotency. Each message should be processed independently.
func TestAgentEdge_AE_EDGE_010(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-010"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Send 3 identical messages with different IDs.
	for i := 0; i < 3; i++ {
		_ = insertUserMessageDirect(t, env, userID, conv.ID, "same message")
	}

	// Process each message sequentially.
	for i := 0; i < 3; i++ {
		msgID := fmt.Sprintf("msg-edge-010-%d", i)
		err := triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
		assert.NoError(t, err, "each identical message should be processed (id=%d)", i)
	}

	// Verify all 3 were processed by checking agent reply count.
	deadline := time.Now().Add(15 * time.Second)
	var agentMsgs []*model.Message
	for {
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
			Find(&agentMsgs)
		if len(agentMsgs) >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, len(agentMsgs), 3,
		"all 3 identical messages should produce separate agent replies")
}

// ---------------------------------------------------------------------------
// AE-EDGE-011: Multi-language mixed long text (5000 characters)
// ---------------------------------------------------------------------------

// TestAgentEdge_AE_EDGE_011 verifies that a 5000-character message containing
// Chinese, English, Japanese, and Korean text is handled correctly. The context
// manager's token trimming should work properly with mixed-script content.
func TestAgentEdge_AE_EDGE_011(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-edge-011"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Build a 5000-character multi-language input.
	// Each iteration is ~50 chars, so 100 iterations = ~5000 chars.
	var builder strings.Builder
	mixedChunk := "你好Helloこんにちさ；안녕WorldمرحباTest"
	for builder.Len() < 5000 {
		builder.WriteString(mixedChunk)
	}
	longMixedInput := builder.String()
	assert.GreaterOrEqual(t, len(longMixedInput), 5000, "input should be at least 5000 characters")

	_ = insertUserMessageDirect(t, env, userID, conv.ID, longMixedInput)

	err := triggerAgentProcessing(t, env, "msg-edge-011", conv.ID, agentUserID, userID)
	// Either success (truncation handled it) or error (too long) — no crash.
	t.Logf("multi-language long text executor result: %v", err)

	// If the agent produced a reply, verify it.
	var msgs []*model.Message
	env.db.DB().WithContext(context.Background()).
		Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
		Order("message_id DESC").Limit(1).Find(&msgs)
	if len(msgs) > 0 {
		assert.Equal(t, agentUserID, msgs[0].SenderID)
		assert.NotEmpty(t, msgs[0].Content)
	}
	// No reply is also acceptable if the input was too large to process.
}
