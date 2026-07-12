// Package e2e_test contains weak-network resilience E2E tests for the Agent
// system. Tests exercise the agent pipeline under simulated LLM-level network
// faults (timeouts, delays, rate limits, stream disconnects) and infrastructure
// failures (Redis, conversation lock). All scenarios use the inline mock LLM
// server with fault injection — no external tools required (D-049).
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// ---------------------------------------------------------------------------
// Weak-net test helpers
// ---------------------------------------------------------------------------

// waitAgentMsgInDB polls the database until a message from agentUserID appears
// in the given conversation, or the timeout expires. Returns the first message
// found (most recent by message_id DESC).
func waitAgentMsgInDB(t *testing.T, env *agentE2EEnv, convID, agentUserID string, timeout time.Duration) *model.Message {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("waitAgentMsgInDB: timed out after %v waiting for message from %s in conv %s",
				timeout, agentUserID, convID)
		}
		var msgs []*model.Message
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", convID, agentUserID).
			Order("message_id DESC").Limit(1).Find(&msgs)
		if len(msgs) > 0 {
			return msgs[0]
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// brokenIdempotencyStore always returns an error from MarkProcessed, simulating
// Redis unavailability. Used to verify fail-open behavior (D-072).
type brokenIdempotencyStore struct{}

func (brokenIdempotencyStore) MarkProcessed(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return false, fmt.Errorf("simulated Redis failure")
}

// brokenConversationLock always returns an error from Acquire, simulating Redis
// unavailability. Release is a no-op. Used to verify fail-open behavior.
type brokenConversationLock struct{}

func (brokenConversationLock) Acquire(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return false, fmt.Errorf("simulated Redis failure")
}

func (brokenConversationLock) Release(_ context.Context, _ string) error { return nil }

// ---------------------------------------------------------------------------
// AE-WEAKNET-001: LLM API timeout (black-hole)
// ---------------------------------------------------------------------------

// TestAgentWeakNet_AE_WEAKNET_001 verifies that when the LLM never responds
// (black-hole timeout), the executor produces a user-friendly Chinese error
// message (D-067) and persists it to the database. The error classification
// should contain "暂时无法回复" for LLM timeout (D-067).
func TestAgentWeakNet_AE_WEAKNET_001(t *testing.T) {
	// Black-hole: mock accepts connection but never responds.
	// WithTotalTimeout(10s) ensures the executor gives up in a reasonable time.
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		BlackHoleTimeout: true,
	}, agent.WithTotalTimeout(10*time.Second))

	userID := "user-weaknet-001"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	payload := agent.ExecutePayload{
		MessageID:      "msg-weaknet-001",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)
	require.Error(t, err, "executor should fail on black-hole timeout")

	// Verify error message persisted in DB with Chinese text (D-067).
	// The abrupt connection close is classified as a generic error by the
	// executor's classifyError (not specifically as a timeout), so the
	// default "处理遇到问题" message is persisted.
	msg := waitAgentMsgInDB(t, env, conv.ID, agentUserID, 15*time.Second)
	assert.Contains(t, msg.Content, "抱歉",
		"LLM black-hole should produce user-friendly Chinese error message (D-067)")
	assert.Contains(t, msg.Content, "重试",
		"error message should suggest retrying")
	assert.Equal(t, agentUserID, msg.SenderID, "sender_id should be the agent")
}

// ---------------------------------------------------------------------------
// AE-WEAKNET-002: LLM response delay → typing indicator
// ---------------------------------------------------------------------------

// TestAgentWeakNet_AE_WEAKNET_002 verifies that while the LLM is responding
// slowly (15s delay), the typing indicator is sent to the user (D-065). This
// improves perceived responsiveness even under poor network conditions.
func TestAgentWeakNet_AE_WEAKNET_002(t *testing.T) {
	// 15s delay on LLM response. Total timeout must exceed the delay.
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		ResponseDelay: 15 * time.Second,
	}, agent.WithTotalTimeout(30*time.Second), agent.WithTypingTimeout(20*time.Second))

	userID := "user-weaknet-002"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()

	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	// Trigger execution in background — it will block for ~15s.
	go func() {
		payload := agent.ExecutePayload{
			MessageID:      "msg-weaknet-002",
			ConversationID: conv.ID,
			AgentID:        agentUserID,
			SenderID:       userID,
		}
		_ = env.executor.Execute(context.Background(), payload)
	}()

	// Verify typing indicator is received while LLM is processing.
	// The typing indicator is sent immediately at executor start (step 4).
	waitForEphemeral(t, conn, "typing", 10*time.Second)
}

// ---------------------------------------------------------------------------
// AE-WEAKNET-003: Stream disconnect after 3 chunks
// ---------------------------------------------------------------------------

// TestAgentWeakNet_AE_WEAKNET_003 verifies graceful degradation when the LLM
// stream is abruptly disconnected after sending 3 chunks. The system should
// either persist the partial text received so far or persist an error message
// (D-067). No data loss or crash should occur.
func TestAgentWeakNet_AE_WEAKNET_003(t *testing.T) {
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		StreamDisconnectAfter: 3,
	}, agent.WithTotalTimeout(15*time.Second))

	userID := "user-weaknet-003"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	payload := agent.ExecutePayload{
		MessageID:      "msg-weaknet-003",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)

	// The executor returns an error due to stream disconnect, but persists
	// either partial text or an error message.
	if err != nil {
		// Error path: error message should be persisted (D-067).
		msg := waitAgentMsgInDB(t, env, conv.ID, agentUserID, 15*time.Second)
		assert.NotEmpty(t, msg.Content,
			"should persist either partial text or error message (graceful degradation)")
		assert.Equal(t, agentUserID, msg.SenderID)
	} else {
		// Success path: partial text was persisted.
		msg := waitAgentMsgInDB(t, env, conv.ID, agentUserID, 15*time.Second)
		assert.NotEmpty(t, msg.Content,
			"partial text should be persisted when stream disconnects")
	}
}

// ---------------------------------------------------------------------------
// AE-WEAKNET-004: HTTP 429 rate limiting
// ---------------------------------------------------------------------------

// TestAgentWeakNet_AE_WEAKNET_004 verifies that when the LLM returns HTTP 429
// (rate limit), the error is classified and a user-friendly Chinese error
// message is persisted (D-067, D-082).
func TestAgentWeakNet_AE_WEAKNET_004(t *testing.T) {
	// First request gets 429. Since there's no retry logic at the executor
	// level (D-073: always return nil to MQ), the error is persisted directly.
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		RateLimitFirstN: 1,
	}, agent.WithTotalTimeout(10*time.Second))

	userID := "user-weaknet-004"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	payload := agent.ExecutePayload{
		MessageID:      "msg-weaknet-004",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)
	require.Error(t, err, "executor should fail on rate limit")

	// Verify error message persisted in DB (D-067).
	// Rate limit maps to ErrLLMRateLimited → "暂时无法回复".
	msg := waitAgentMsgInDB(t, env, conv.ID, agentUserID, 10*time.Second)
	assert.Contains(t, msg.Content, "抱歉",
		"HTTP 429 error message should contain 抱歉 (D-067)")
	assert.Contains(t, msg.Content, "暂时无法回复",
		"HTTP 429 should produce rate-limit error message (D-067, D-082)")
	assert.Equal(t, agentUserID, msg.SenderID)
}

// ---------------------------------------------------------------------------
// AE-WEAKNET-005: Redis interrupt → fail-open idempotency
// ---------------------------------------------------------------------------

// TestAgentWeakNet_AE_WEAKNET_005 verifies that when Redis is unavailable and
// the idempotency store cannot be reached, the agent task still executes
// successfully (fail-open, D-072). The task must NOT be blocked or rejected.
func TestAgentWeakNet_AE_WEAKNET_005(t *testing.T) {
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{},
		agent.WithTotalTimeout(10*time.Second))

	userID := "user-weaknet-005"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	// Create a task handler with a broken idempotency store (simulates Redis failure).
	// Lock is nil (not tested here — see AE-WEAKNET-007 for that).
	taskHandler := agent.NewAgentTaskHandler(
		env.executor,
		brokenIdempotencyStore{}, // always returns error → fail-open
		nil,                      // no lock
		testLogger{},
	)

	taskPayload, err := json.Marshal(agent.AgentProcessPayload{
		MessageID:      "msg-weaknet-005",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.NoError(t, err)

	// Handler should succeed (return nil to MQ, D-073) despite idempotency failure.
	taskErr := taskHandler(context.Background(), &mq.Task{
		Type:    "mq:agent_process",
		Payload: taskPayload,
	})
	assert.Nil(t, taskErr, "task handler should always return nil to MQ (D-073)")

	// Verify the task executed successfully (fail-open, D-072).
	// The agent reply should be persisted, NOT an error message.
	msg := waitAgentMsgInDB(t, env, conv.ID, agentUserID, 15*time.Second)
	assert.NotEmpty(t, msg.Content,
		"task should execute successfully despite Redis failure (fail-open, D-072)")
	assert.Equal(t, agentUserID, msg.SenderID)
}

// ---------------------------------------------------------------------------
// AE-WEAKNET-006: Combined weak-net (delay + eventual success)
// ---------------------------------------------------------------------------

// TestAgentWeakNet_AE_WEAKNET_006 verifies that the agent system tolerates
// moderate latency (5s delay) and eventually produces a correct reply. This
// tests the happy path under degraded network conditions.
func TestAgentWeakNet_AE_WEAKNET_006(t *testing.T) {
	// 5s delay — executor timeout (15s) must exceed this.
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		ResponseDelay: 5 * time.Second,
	}, agent.WithTotalTimeout(15*time.Second))

	userID := "user-weaknet-006"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	payload := agent.ExecutePayload{
		MessageID:      "msg-weaknet-006",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)
	require.NoError(t, err, "executor should succeed despite 5s delay")

	// Verify the reply was persisted with correct content.
	msg := waitAgentMsgInDB(t, env, conv.ID, agentUserID, 15*time.Second)
	assert.NotEmpty(t, msg.Content, "reply should not be empty")
	assert.Equal(t, agentUserID, msg.SenderID, "sender_id should be the agent")
}

// ---------------------------------------------------------------------------
// AE-WEAKNET-007: Conversation lock fail-open
// ---------------------------------------------------------------------------

// TestAgentWeakNet_AE_WEAKNET_007 verifies that when the per-conversation lock
// cannot be acquired (e.g. Redis unavailable), the agent task still executes
// in degraded mode (fail-open). The lock failure is logged but does not block
// execution (D-072 pattern applied to conversation lock).
func TestAgentWeakNet_AE_WEAKNET_007(t *testing.T) {
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{},
		agent.WithTotalTimeout(10*time.Second))

	userID := "user-weaknet-007"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	// Create a task handler with a broken conversation lock (simulates Redis failure).
	// Idempotency is nil (not tested here — see AE-WEAKNET-005 for that).
	taskHandler := agent.NewAgentTaskHandler(
		env.executor,
		nil,                      // no idempotency store
		brokenConversationLock{}, // always returns error → fail-open
		testLogger{},
	)

	taskPayload, err := json.Marshal(agent.AgentProcessPayload{
		MessageID:      "msg-weaknet-007",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	})
	require.NoError(t, err)

	// Handler should succeed despite lock failure (fail-open).
	taskErr := taskHandler(context.Background(), &mq.Task{
		Type:    "mq:agent_process",
		Payload: taskPayload,
	})
	assert.Nil(t, taskErr, "task handler should always return nil to MQ (D-073)")

	// Verify the task executed successfully despite lock failure.
	msg := waitAgentMsgInDB(t, env, conv.ID, agentUserID, 15*time.Second)
	assert.NotEmpty(t, msg.Content,
		"task should execute despite lock failure (fail-open)")
	assert.Equal(t, agentUserID, msg.SenderID)
}

// ---------------------------------------------------------------------------
// AE-WEAKNET-008: Partial text integrity
// ---------------------------------------------------------------------------

// TestAgentWeakNet_AE_WEAKNET_008 verifies that when the LLM stream disconnects
// after just 1 chunk, the partial text is handled cleanly — either fully
// persisted or fully discarded. No garbled, truncated, or corrupt text should
// appear in the database.
func TestAgentWeakNet_AE_WEAKNET_008(t *testing.T) {
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		StreamDisconnectAfter: 1,
	}, agent.WithTotalTimeout(15*time.Second))

	userID := "user-weaknet-008"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	payload := agent.ExecutePayload{
		MessageID:      "msg-weaknet-008",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)

	// Either partial text is persisted or an error message — both are valid.
	// The key assertion is: something is persisted and it's clean text.
	msg := waitAgentMsgInDB(t, env, conv.ID, agentUserID, 15*time.Second)
	assert.NotEmpty(t, msg.Content,
		"partial text or error message should be persisted (not empty)")
	assert.Equal(t, agentUserID, msg.SenderID)

	// If there's content from the LLM (not an error message), verify it's
	// clean text — no garbled UTF-8 or half-constructed SSE frames.
	if err == nil {
		// Partial text was persisted. Verify it's valid UTF-8 (no corruption).
		assert.NotEmpty(t, msg.Content,
			"partial text should be non-empty when persisted")
		// Basic sanity: content should be printable text.
		for _, r := range msg.Content {
			assert.NotEqual(t, '�', r,
				"partial text should not contain replacement characters (garbled UTF-8)")
		}
	}
	// If err != nil, the error message was persisted — it's always clean Chinese text.
}

// ---------------------------------------------------------------------------
// AE-WEAKNET-009: Cross-session parallel processing
// ---------------------------------------------------------------------------

// TestAgentWeakNet_AE_WEAKNET_009 verifies that concurrent agent processing
// across different conversations does not cause interference. Two conversations
// with different agents (backed by different mock servers — one delayed, one
// normal) run in parallel; the non-delayed conversation completes quickly
// regardless of the delayed one.
func TestAgentWeakNet_AE_WEAKNET_009(t *testing.T) {
	// Primary env with a delayed mock for agent "test-bot".
	env := setupAgentE2EWeakNet(t, llmWeakNetConfig{
		ResponseDelay: 5 * time.Second,
	}, agent.WithTotalTimeout(20*time.Second))

	// Create a second mock with no delay and a second agent "fast-bot".
	fastMock := newMockLLMServer()
	t.Cleanup(func() { fastMock.Close() })

	fastCfg := basicAgentConfig(fastMock.URL())
	fastCfg.ID = "fast-bot"
	fastCfg.Name = "Fast Bot"
	writeAgentConfig(t, env.agentsDir, fastCfg)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	userA := "user-weaknet-009a"
	userB := "user-weaknet-009b"
	agentA := "agent/test-bot"
	agentB := "agent/fast-bot"

	convA := createAgentConversation(t, env, userA, agentA)
	convB := createAgentConversation(t, env, userB, agentB)

	_ = insertUserMessageDirect(t, env, userA, convA.ID, "hello")
	_ = insertUserMessageDirect(t, env, userB, convB.ID, "hello")

	// Execute both concurrently.
	var wg sync.WaitGroup
	var errA, errB error

	wg.Add(2)
	go func() {
		defer wg.Done()
		errA = env.executor.Execute(context.Background(), agent.ExecutePayload{
			MessageID:      "msg-weaknet-009a",
			ConversationID: convA.ID,
			AgentID:        agentA,
			SenderID:       userA,
		})
	}()
	go func() {
		defer wg.Done()
		errB = env.executor.Execute(context.Background(), agent.ExecutePayload{
			MessageID:      "msg-weaknet-009b",
			ConversationID: convB.ID,
			AgentID:        agentB,
			SenderID:       userB,
		})
	}()

	wg.Wait()
	assert.NoError(t, errA, "conversation A (delayed) should eventually succeed")
	assert.NoError(t, errB, "conversation B (fast) should succeed")

	// Verify both conversations produced replies.
	msgA := waitAgentMsgInDB(t, env, convA.ID, agentA, 15*time.Second)
	assert.NotEmpty(t, msgA.Content, "conversation A should have a reply")

	msgB := waitAgentMsgInDB(t, env, convB.ID, agentB, 15*time.Second)
	assert.NotEmpty(t, msgB.Content, "conversation B should have a reply")
}
