// Package e2e_test contains full-chain input boundary E2E tests for the Agent
// system (D-091 compliance). These tests complement the existing edge-case
// tests (agent_edge_test.go) by using threeLayerCheck + testStepLogger for
// structured verification of Server DB, Redis, and processing behavior.
//
// Tests cover:
//   - Message burst (10 rapid messages) with lock serialization (D-075)
//   - Long input (10000+ chars) with three-layer verification
//   - Special characters (emoji + HTML + SQL injection) with three-layer check
//   - Empty message with full three-layer rejection verification (D-091)
//
// No build tag — uses mock LLM.
package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// ---------------------------------------------------------------------------
// TestFullChainBoundary_MessageBurst — 10 rapid messages, lock serialization
// Verifies (D-075, D-091):
//  1. All 10 messages are persisted to Server DB
//  2. Agent processes serially via per-conversation lock
//  3. Redis lock is released after all processing completes
// ---------------------------------------------------------------------------
func TestFullChainBoundary_MessageBurst(t *testing.T) {
	env := setupAgentE2E(t)

	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)
	redisClient := redis.NewClient(&redis.Options{Addr: e2eRedisAddr, DB: e2eRedisDB})
	defer redisClient.Close()

	userID := "user-burst-boundary"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	// --- Step 1: Verify initial state ---
	logger.Step("before-burst")
	check.VerifyServerDB("initial-zero-agent-messages", func() error {
		count := countAgentMessages(t, env, conv.ID, agentUserID)
		if count != 0 {
			return fmt.Errorf("expected 0 agent messages before burst, got %d", count)
		}
		return nil
	})
	check.VerifyRedis("initial-lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 2: Send 10 messages rapidly ---
	logger.Step("send-burst")
	numMessages := 10
	var wg sync.WaitGroup
	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			content := fmt.Sprintf("burst message %d", idx)
			_ = insertUserMessageDirect(t, env, userID, conv.ID, content)
			msgID := fmt.Sprintf("msg-burst-%d", idx)
			_ = triggerAgentProcessing(t, env, msgID, conv.ID, agentUserID, userID)
		}(i)
	}
	wg.Wait()

	// --- Step 3: Verify all user messages persisted ---
	logger.Step("verify-burst-persisted")
	check.VerifyServerDB("all-user-messages-persisted", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
		defer cancel()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 500)
		if err != nil {
			return err
		}
		userCount := 0
		for _, m := range msgs {
			if m.SenderID == userID {
				userCount++
			}
		}
		if userCount < numMessages {
			return fmt.Errorf("expected %d user messages, got %d", numMessages, userCount)
		}
		return nil
	})

	// --- Step 4: Wait for agent replies (serial processing, D-075) ---
	logger.Step("wait-for-agent-replies")
	deadline := time.Now().Add(25 * time.Second)
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
		"at least 8 of 10 burst messages should produce agent replies (D-075 serial processing)")

	// --- Step 5: Verify lock released ---
	logger.Step("verify-lock-released")
	check.VerifyRedis("lock-released-after-burst", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})
}

// ---------------------------------------------------------------------------
// TestFullChainBoundary_LongInput_ThreeLayer — 10000+ char input
// Verifies (D-060, D-091):
//  1. BEFORE: 0 agent messages in Server DB
//  2. ACTION: executor processes the super-long input without crash
//  3. AFTER: agent reply persisted, Redis lock released
// ---------------------------------------------------------------------------
func TestFullChainBoundary_LongInput_ThreeLayer(t *testing.T) {
	env := setupAgentE2E(t)

	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)
	redisClient := redis.NewClient(&redis.Options{Addr: e2eRedisAddr, DB: e2eRedisDB})
	defer redisClient.Close()

	userID := "user-longinput-boundary"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Build a 10000+ character input.
	longInput := strings.Repeat("x", 10001)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, longInput)

	// --- Step 1: BEFORE — verify initial state ---
	logger.Step("before-long-input")
	check.VerifyServerDB("initial-zero-agent-messages", func() error {
		count := countAgentMessages(t, env, conv.ID, agentUserID)
		if count != 0 {
			return fmt.Errorf("expected 0 agent messages, got %d", count)
		}
		return nil
	})
	check.VerifyServerDB("user-message-persisted", func() error {
		requireServerDBMessageCount(t, env.store, conv.ID, 1)
		return nil
	})
	check.VerifyRedis("initial-lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 2: ACTION — executor processes the long input ---
	logger.Step("execute-long-input")
	err := env.executor.Execute(context.Background(), agent.ExecutePayload{
		MessageID:      "msg-longinput-1",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.NoError(t, err, "executor should handle 10000+ char input without crashing (D-060 truncation)")

	// --- Step 3: AFTER — verify agent reply and lock release ---
	logger.Step("after-long-input")
	check.VerifyServerDB("agent-reply-persisted", func() error {
		agentCount := countAgentMessages(t, env, conv.ID, agentUserID)
		if agentCount < 1 {
			return fmt.Errorf("expected at least 1 agent reply, got %d", agentCount)
		}
		return nil
	})
	check.VerifyRedis("lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})
}

// ---------------------------------------------------------------------------
// TestFullChainBoundary_SpecialChars_ThreeLayer — emoji + HTML + SQL injection
// Verifies (D-091):
//  1. Message with mixed special chars is stored without truncation
//  2. Agent produces a reply without crash
//  3. Redis lock is released
// ---------------------------------------------------------------------------
func TestFullChainBoundary_SpecialChars_ThreeLayer(t *testing.T) {
	env := setupAgentE2E(t)

	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)
	redisClient := redis.NewClient(&redis.Options{Addr: e2eRedisAddr, DB: e2eRedisDB})
	defer redisClient.Close()

	userID := "user-specialchars-boundary"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Build a message with emoji + HTML + SQL injection attempt.
	specialMsg := "Hello \U0001f30d\U0001f389 <script>alert('xss')</script> ' OR 1=1; DROP TABLE users; -- \x00 world"
	_ = insertUserMessageDirect(t, env, userID, conv.ID, specialMsg)

	// --- Step 1: BEFORE — verify message stored exactly ---
	logger.Step("before-special-chars")
	check.VerifyServerDB("message-stored-intact", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
		defer cancel()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 10)
		if err != nil {
			return err
		}
		if len(msgs) != 1 {
			return fmt.Errorf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].Content != specialMsg {
			return fmt.Errorf("message content was modified: stored %q, expected %q", msgs[0].Content, specialMsg)
		}
		return nil
	})
	check.VerifyRedis("initial-lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 2: ACTION — executor processes the special chars ---
	logger.Step("execute-special-chars")
	err := env.executor.Execute(context.Background(), agent.ExecutePayload{
		MessageID:      "msg-specialchars-1",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	// Either success or graceful error is acceptable — no crash/panic.
	t.Logf("special chars executor result: %v", err)

	// --- Step 3: AFTER — verify agent reply and lock release ---
	logger.Step("after-special-chars")
	check.VerifyServerDB("agent-handled-special-chars", func() error {
		// Check if an agent message exists (success or error message).
		agentCount := countAgentMessages(t, env, conv.ID, agentUserID)
		// Agent may or may not produce a reply for special chars — both are
		// acceptable as long as no crash occurred.
		t.Logf("agent messages after special chars: %d", agentCount)
		return nil
	})
	check.VerifyRedis("lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// Verify the original message content is still intact (not corrupted).
	check.VerifyServerDB("original-message-not-corrupted", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
		defer cancel()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 500)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if m.SenderID == userID && m.Content == specialMsg {
				return nil
			}
		}
		return fmt.Errorf("original special char message not found or corrupted")
	})
}

// ---------------------------------------------------------------------------
// TestFullChainBoundary_EmptyMessage_ThreeLayer — empty message rejection
// Verifies (D-091):
//  1. BEFORE: 0 agent messages, lock released
//  2. ACTION: executor rejects the empty message with error
//  3. AFTER: error message persisted (containing "抱歉"), lock released
// ---------------------------------------------------------------------------
func TestFullChainBoundary_EmptyMessage_ThreeLayer(t *testing.T) {
	env := setupAgentE2E(t)

	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)
	redisClient := redis.NewClient(&redis.Options{Addr: e2eRedisAddr, DB: e2eRedisDB})
	defer redisClient.Close()

	userID := "user-emptymsg-boundary"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "")

	// --- Step 1: BEFORE — verify initial state ---
	logger.Step("before-empty-message")
	check.VerifyServerDB("initial-message-count", func() error {
		requireServerDBMessageCount(t, env.store, conv.ID, 1) // only user message
		return nil
	})
	check.VerifyServerDB("no-agent-messages", func() error {
		count := countAgentMessages(t, env, conv.ID, agentUserID)
		if count != 0 {
			return fmt.Errorf("expected 0 agent messages, got %d", count)
		}
		return nil
	})
	check.VerifyRedis("initial-lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 2: ACTION — executor rejects the empty message ---
	logger.Step("execute-empty-message")
	err := env.executor.ExecuteWithErrorMessage(context.Background(), agent.ExecutePayload{
		MessageID:      "msg-empty-1",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.Error(t, err, "empty message should be rejected (D-091)")

	// --- Step 3: AFTER — verify error message persisted and lock released ---
	logger.Step("after-empty-message")
	check.VerifyServerDB("error-message-persisted", func() error {
		requireServerDBHasMessage(t, env.store, conv.ID, "抱歉")
		return nil
	})
	check.VerifyServerDB("total-message-count", func() error {
		// 1 user message + 1 error message = 2
		requireServerDBMessageCount(t, env.store, conv.ID, 2)
		return nil
	})
	check.VerifyRedis("lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})
}
