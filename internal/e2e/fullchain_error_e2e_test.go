// Package e2e_test contains full-chain error scenario E2E tests for the Agent
// system. These tests complement the existing Category I error tests
// (agent_error_test.go) by verifying error handling through the full pipeline
// including Redis locks, three-layer verification, and recovery scenarios.
//
// Tests cover:
//   - LLM rate limiting (HTTP 429) with recovery (D-067, D-073, D-082)
//   - LLM connection failure (black hole) with error persistence (D-067, D-075)
//   - Error recovery — subsequent requests succeed after transient failure
//   - Concurrent lock contention — serial processing of same-conversation tasks
//
// All tests use threeLayerCheck + testStepLogger for structured verification.
// No build tag — uses mock LLM.
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
	"github.com/PineappleBond/xyncra-server/internal/mq"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// triggerAgentViaTaskHandler invokes the agent task handler directly (bypassing
// MQ delivery). This exercises the full pipeline: lock acquire -> idempotency
// check -> executor.ExecuteWithErrorMessage -> lock release (D-073, D-075, D-110).
//
// Note: transient errors (rate limit, timeout) are returned to the caller for
// MQ retry. Permanent errors return nil. This is a D-073 refinement.
func triggerAgentViaTaskHandler(t *testing.T, env *agentE2EEnv, messageID, convID, agentUserID, senderID string) error {
	t.Helper()

	payload := agent.AgentProcessPayload{
		MessageID:      messageID,
		ConversationID: convID,
		AgentID:        agentUserID,
		SenderID:       senderID,
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err, "marshal agent process payload")

	task := &mq.Task{
		Type:    "mq:agent_process",
		Payload: raw,
	}
	return env.taskHandler.ProcessTask(context.Background(), task)
}

// invalidateContextCache replaces the executor's context manager with a fresh
// one that has no cached entries. This is necessary when tests insert messages
// directly into the DB and need the executor to see them (the default
// DBContextManager caches conversation context for 30s, D-060).
func invalidateContextCache(t *testing.T, env *agentE2EEnv) {
	t.Helper()
	env.executor.SetContextManager(agent.NewDBContextManager(env.store.MessageStore()))
}

// requireAgentErrorMessage verifies that an error message from the agent was
// persisted in the Server DB containing the given substring.
func requireAgentErrorMessage(t *testing.T, env *agentE2EEnv, convID, agentUserID, substring string) {
	t.Helper()
	requireServerDBHasMessage(t, env.store, convID, substring)
}

// countAgentMessages returns the number of messages from the agent in a
// conversation.
func countAgentMessages(t *testing.T, env *agentE2EEnv, convID, agentUserID string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
	defer cancel()
	msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, convID, 500)
	require.NoError(t, err, "list messages for count")
	count := 0
	for _, m := range msgs {
		if m.SenderID == agentUserID {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// TestFullChainError_LLMRateLimited — HTTP 429 → error message → recovery
// Verifies:
//  1. Mock LLM returns HTTP 429 on first request
//  2. Server DB has error message containing "暂时无法回复" (D-067)
//  3. Redis lock is released after error (D-075)
//  4. Second request succeeds (no deadlock)
//
// ---------------------------------------------------------------------------
func TestFullChainError_LLMRateLimited(t *testing.T) {
	// Setup with weak net: first request returns 429.
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		RateLimitFirstN: 1,
	}, agent.WithTotalTimeout(30*time.Second))

	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)
	redisClient := newAgentRedisClient(t)

	userID := "user-fc-ratelimit"
	agentUserID := "agent/test-bot"

	// Create conversation and insert user message.
	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	// --- Step 1: Verify initial state ---
	logger.Step("before-trigger")
	check.VerifyServerDB("initial-message-count", func() error {
		requireServerDBMessageCount(t, env.store, conv.ID, 1) // only user message
		return nil
	})
	check.VerifyRedis("initial-lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 2: Trigger agent processing (first request → 429) ---
	logger.Step("trigger-rate-limit")
	err := env.executor.ExecuteWithErrorMessage(context.Background(), agent.ExecutePayload{
		MessageID:      "msg-ratelimit-1",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.Error(t, err, "executor should fail when LLM returns HTTP 429")

	// --- Step 3: Verify error state ---
	logger.Step("after-rate-limit")
	check.VerifyServerDB("error-message-persisted", func() error {
		requireAgentErrorMessage(t, env, conv.ID, agentUserID, "暂时无法回复")
		return nil
	})
	check.VerifyRedis("lock-not-held", func() error {
		// Executor does not manage locks; the task handler does.
		// Verify no stale lock exists.
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 4: Recovery — reset weak net and send another message ---
	logger.Step("recovery")
	env.mockLLM.ResetWeakNet()
	env.mockLLM.ResetCounters()
	invalidateContextCache(t, env)

	// Insert a new user message and trigger again.
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello again")
	err = env.executor.Execute(context.Background(), agent.ExecutePayload{
		MessageID:      "msg-ratelimit-2",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.NoError(t, err, "second execution should succeed after rate limit recovery")

	// Verify the agent produced a successful reply.
	check.VerifyServerDB("recovery-message", func() error {
		agentCount := countAgentMessages(t, env, conv.ID, agentUserID)
		if agentCount < 2 {
			return fmt.Errorf("expected at least 2 agent messages (error + reply), got %d", agentCount)
		}
		return nil
	})
	check.VerifyRedis("lock-released-after-recovery", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})
}

// ---------------------------------------------------------------------------
// TestFullChainError_LLMTimeout — LLM connection failure → error message
// Verifies:
//  1. Mock LLM accepts connection but immediately closes (black hole)
//  2. Server DB has an error message persisted (D-067)
//  3. ExecuteWithErrorMessage returns an error
//  4. No stale Redis lock (D-075)
//
// ---------------------------------------------------------------------------
func TestFullChainError_LLMTimeout(t *testing.T) {
	// Setup with weak net: black hole (accept connection, close immediately).
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		BlackHoleTimeout: true,
	}, agent.WithTotalTimeout(10*time.Second))

	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)
	redisClient := newAgentRedisClient(t)

	userID := "user-fc-timeout"
	agentUserID := "agent/test-bot"

	// Create conversation and insert user message.
	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	// --- Step 1: Verify initial state ---
	logger.Step("before-trigger")
	check.VerifyServerDB("initial-message-count", func() error {
		requireServerDBMessageCount(t, env.store, conv.ID, 1)
		return nil
	})
	check.VerifyRedis("initial-lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 2: Trigger agent processing (LLM connection fails) ---
	logger.Step("trigger-blackhole")
	err := env.executor.ExecuteWithErrorMessage(context.Background(), agent.ExecutePayload{
		MessageID:      "msg-timeout-1",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.Error(t, err, "executor should fail when LLM connection fails")

	// --- Step 3: Verify error state ---
	logger.Step("after-blackhole")
	check.VerifyServerDB("error-message-persisted", func() error {
		// Black hole causes a connection error → generic error message (D-067).
		// The error is NOT a timeout or rate limit, so classifyError returns
		// the default "处理遇到问题" message.
		requireAgentErrorMessage(t, env, conv.ID, agentUserID, "处理遇到问题")
		return nil
	})
	check.VerifyRedis("lock-not-held", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 4: Verify the mock LLM was actually called ---
	assert.Greater(t, env.mockLLM.CallCount(), 0,
		"mock LLM should have received at least one request")
}

// ---------------------------------------------------------------------------
// TestFullChainError_ThreeLayerVerification — three-layer check for HTTP 500
// Verifies the three-layer checkpoint pattern (Server DB, Redis) for the
// existing AE_ERR_001 scenario (LLM HTTP 500 → "暂时无法回复").
// This test demonstrates the structured verification pattern used across
// all full-chain error tests.
// ---------------------------------------------------------------------------
func TestFullChainError_ThreeLayerVerification(t *testing.T) {
	env := setupAgentE2E(t)

	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)
	redisClient := newAgentRedisClient(t)

	userID := "user-fc-threelayer"
	agentUserID := "agent/test-bot"

	// Create conversation and insert user message with error_trigger.
	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "error_trigger")

	// --- Step 1: Verify initial state (before trigger) ---
	logger.Step("before-trigger")
	check.VerifyServerDB("initial-message-count", func() error {
		requireServerDBMessageCount(t, env.store, conv.ID, 1) // only user message
		return nil
	})
	check.VerifyRedis("initial-lock-released", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 2: Trigger agent processing via executor ---
	logger.Step("trigger-error")
	err := env.executor.ExecuteWithErrorMessage(context.Background(), agent.ExecutePayload{
		MessageID:      "msg-threelayer-1",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.Error(t, err, "executor should fail for HTTP 500")

	// --- Step 3: Verify post-error state (three-layer) ---
	logger.Step("after-error")
	check.VerifyServerDB("error-message-persisted", func() error {
		requireAgentErrorMessage(t, env, conv.ID, agentUserID, "暂时无法回复")
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

// ---------------------------------------------------------------------------
// TestFullChainError_RecoveryAfterError — error then successful recovery
// Verifies:
//  1. First request: mock LLM returns HTTP 500 → error message persisted
//  2. Second request (with cache invalidated): agent produces a successful reply
//  3. Error does not block subsequent processing (D-073, D-075)
//
// ---------------------------------------------------------------------------
func TestFullChainError_RecoveryAfterError(t *testing.T) {
	env := setupAgentE2E(t)

	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)
	redisClient := newAgentRedisClient(t)

	userID := "user-fc-recovery"
	agentUserID := "agent/test-bot"

	// Create conversation.
	conv := createAgentConversation(t, env, userID, agentUserID)

	// --- Step 1: First request — LLM returns HTTP 500 ---
	logger.Step("first-request-error")
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "error_trigger")

	err := env.executor.ExecuteWithErrorMessage(context.Background(), agent.ExecutePayload{
		MessageID:      "msg-recovery-1",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.Error(t, err, "first execution should fail (HTTP 500)")

	check.VerifyServerDB("error-message-persisted", func() error {
		requireAgentErrorMessage(t, env, conv.ID, agentUserID, "暂时无法回复")
		return nil
	})
	check.VerifyRedis("lock-released-after-error", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// --- Step 2: Reset and send a normal message ---
	logger.Step("reset-and-recover")
	// The mock LLM already handles "hello" with a normal response by default.
	env.mockLLM.ResetCounters()

	// Invalidate the context cache so the executor sees the new message.
	// The DBContextManager caches conversation context for 30s (D-060).
	invalidateContextCache(t, env)

	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")
	err = env.executor.Execute(context.Background(), agent.ExecutePayload{
		MessageID:      "msg-recovery-2",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.NoError(t, err, "second execution should succeed after recovery")

	// --- Step 3: Verify successful reply ---
	logger.Step("verify-recovery")
	check.VerifyServerDB("successful-reply", func() error {
		msgs, listErr := env.store.MessageStore().ListRecentByConversation(
			context.Background(), conv.ID, 500)
		if listErr != nil {
			return listErr
		}
		// The latest agent message should be a successful reply (not an error).
		for _, m := range msgs {
			if m.SenderID == agentUserID {
				if m.Content != "抱歉，我暂时无法回复，请稍后重试。" {
					return nil // found a successful reply
				}
			}
		}
		return fmt.Errorf("no successful agent reply found after recovery")
	})
	check.VerifyRedis("lock-released-after-recovery", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// Verify the mock LLM was called for the successful request.
	assert.Greater(t, env.mockLLM.CallCount(), 0,
		"mock LLM should have been called for the recovery request")

	// Total agent messages: 1 error + 1 successful = 2.
	agentMsgCount := countAgentMessages(t, env, conv.ID, agentUserID)
	assert.Equal(t, 2, agentMsgCount,
		"should have exactly 2 agent messages: 1 error + 1 successful reply")
}

// ---------------------------------------------------------------------------
// TestFullChainError_ConcurrentLockContention — serial processing via lock
// Verifies:
//  1. User A's task acquires the lock and processes (with delayed LLM)
//  2. User B's task (same conversation) is skipped while lock is held (D-075)
//  3. After User A completes, lock is released
//  4. User B can be retried successfully after lock release
//
// ---------------------------------------------------------------------------
func TestFullChainError_ConcurrentLockContention(t *testing.T) {
	// Setup with a short delay so User A's task takes some time.
	// The mock handler sleeps for 3s before processing (applied to ALL requests).
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		ResponseDelay: 3 * time.Second,
	}, agent.WithTotalTimeout(30*time.Second))

	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)
	redisClient := newAgentRedisClient(t)

	userID := "user-fc-lock"
	agentUserID := "agent/test-bot"

	// Create conversation.
	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	// --- Step 1: Start User A's task in a goroutine ---
	logger.Step("start-user-a")
	doneA := make(chan error, 1)
	go func() {
		doneA <- triggerAgentViaTaskHandler(t, env,
			"msg-lock-a", conv.ID, agentUserID, userID)
	}()

	// Give User A's task time to acquire the lock.
	time.Sleep(500 * time.Millisecond)

	// --- Step 2: Verify lock is held ---
	logger.Step("verify-lock-held")
	{
		ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
		defer cancel()
		key := fmt.Sprintf("agent:lock:%s", conv.ID)
		exists, err := redisClient.Exists(ctx, key).Result()
		require.NoError(t, err, "check lock key")
		require.Equal(t, int64(1), exists,
			"lock should be held while User A is processing")
	}

	// --- Step 3: Start User B's task (should be skipped) ---
	logger.Step("start-user-b-while-locked")
	errB := triggerAgentViaTaskHandler(t, env,
		"msg-lock-b", conv.ID, agentUserID, userID)
	assert.Nil(t, errB, "task handler returns nil when skipping (lock held)")

	// User B's task was skipped — no new agent message should be persisted
	// beyond what User A will produce.
	msgCountBeforeA := countAgentMessages(t, env, conv.ID, agentUserID)
	t.Logf("agent messages before A completes: %d", msgCountBeforeA)

	// --- Step 4: Wait for User A to complete ---
	logger.Step("wait-user-a")
	errA := <-doneA
	// User A's task goes through the mock with 3s delay but succeeds.
	// The task handler returns nil for successful execution.
	assert.Nil(t, errA, "User A's task handler should return nil on success")

	// --- Step 5: Verify lock is released ---
	logger.Step("after-user-a")
	check.VerifyRedis("lock-released-after-a", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})

	// Verify agent reply was persisted.
	agentCountAfterA := countAgentMessages(t, env, conv.ID, agentUserID)
	assert.GreaterOrEqual(t, agentCountAfterA, 1,
		"User A should have produced at least 1 agent message")

	// --- Step 6: Retry User B's task (should succeed now) ---
	logger.Step("retry-user-b")
	// Reset weak net so the mock responds immediately.
	env.mockLLM.ResetWeakNet()
	env.mockLLM.ResetCounters()
	invalidateContextCache(t, env)

	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello again")
	errB2 := triggerAgentViaTaskHandler(t, env,
		"msg-lock-b-retry", conv.ID, agentUserID, userID)
	assert.Nil(t, errB2, "retry task handler should return nil")

	check.VerifyServerDB("user-b-reply-persisted", func() error {
		agentCount := countAgentMessages(t, env, conv.ID, agentUserID)
		if agentCount < 2 {
			return fmt.Errorf("expected at least 2 agent messages (A + B), got %d", agentCount)
		}
		return nil
	})
	check.VerifyRedis("lock-released-after-b", func() error {
		requireRedisSessionLockReleased(t, redisClient, conv.ID)
		return nil
	})
}
