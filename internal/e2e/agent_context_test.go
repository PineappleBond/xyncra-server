// Package e2e_test contains Category D context management E2E tests for the
// Agent system (Phase 1-8). Tests verify multi-turn context retention,
// token-based trimming, message-count fallback, and the sync.Map cache with
// TTL-based expiry (D-060).
package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// ---------------------------------------------------------------------------
// Context test helpers
// ---------------------------------------------------------------------------

// insertContextMsg inserts a message into the database via SendMessage, which
// atomically allocates a MessageID and creates per-member UserUpdate records.
// The caller supplies memberIDs to avoid cross-test coupling with agent IDs.
func insertContextMsg(t *testing.T, env *agentE2EEnv, convID, senderID, content string, memberIDs []string) *model.Message {
	t.Helper()
	msg := &model.Message{
		ID:              fmt.Sprintf("msg-ctx-%d", time.Now().UnixNano()),
		ClientMessageID: fmt.Sprintf("cmid-ctx-%d", time.Now().UnixNano()),
		ConversationID:  convID,
		SenderID:        senderID,
		Content:         content,
		Type:            "text",
		Status:          "sent",
		CreatedAt:       time.Now(),
	}
	result, err := env.store.SendMessage(context.Background(), msg, memberIDs)
	require.NoError(t, err, "insertContextMsg should succeed")
	return result.Message
}

// ---------------------------------------------------------------------------
// TestAgentContext_AE_CTX_001 — Multi-turn conversation maintains context
// Scenario: Second message's reply references first message content (D-060)
// Verifies: Mock LLM receives accumulated conversation history on subsequent
//
//	calls, proving that the context manager loads prior messages from DB.
//
// ---------------------------------------------------------------------------
func TestAgentContext_AE_CTX_001(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ctx-001"
	agentUserID := "agent/test-bot"
	members := []string{userID, agentUserID}

	// Create conversation between user and agent.
	conv := createAgentConversation(t, env, userID, agentUserID)

	// Pre-insert conversation history: one user message and one agent reply.
	// This simulates a prior turn without relying on the executor (which would
	// populate the cache and prevent the second GetContext from seeing new DB
	// data within the 30s TTL).
	_ = insertContextMsg(t, env, conv.ID, userID, "hello from turn one", members)
	_ = insertContextMsg(t, env, conv.ID, agentUserID, "I remember turn one.", members)

	// Now send a second user message via WebSocket.
	conn := sendUserMessage(t, env, userID, conv.ID, "do you remember the context?")
	defer conn.Close()

	// Trigger agent processing. The context manager's first GetContext call
	// loads all three messages from DB: [user1, agent1, user2].
	err := triggerAgentProcessing(t, env, "msg-ctx-001", conv.ID, agentUserID, userID)
	require.NoError(t, err, "agent executor should succeed")

	// Wait for the agent's reply to be persisted.
	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 30*time.Second)
	assert.NotEmpty(t, agentMsg.Content, "agent reply should not be empty")

	// Verify the mock LLM received accumulated history. The Eino ChatModel
	// prepends the system prompt, so the mock should see at least 4 messages:
	// [system, user1, agent1, user2].
	counts := env.mockLLM.RequestMessageCounts()
	require.GreaterOrEqual(t, len(counts), 1,
		"mock LLM should have been called at least once")
	assert.GreaterOrEqual(t, counts[0], 4,
		"mock LLM should receive system + 3 conversation messages (user1, agent1, user2)")
}

// ---------------------------------------------------------------------------
// TestAgentContext_AE_CTX_002 — Token truncation works correctly
// Scenario: Long conversation exceeding max_tokens has old messages trimmed (D-060)
// Verifies: DBContextManager.trimByTokens removes oldest messages when total
//
//	token count exceeds MaxTokens (heuristic: len/4).
//
// ---------------------------------------------------------------------------
func TestAgentContext_AE_CTX_002(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ctx-002"
	agentUserID := "agent/test-bot"
	members := []string{userID, agentUserID}

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Insert 3 long messages. Each is 200 chars → 50 tokens (len/4).
	// Total: 150 tokens.
	longContent := "A"
	for i := 0; i < 199; i++ {
		longContent += "x"
	}
	_ = insertContextMsg(t, env, conv.ID, userID, longContent, members)
	_ = insertContextMsg(t, env, conv.ID, agentUserID, longContent, members)
	_ = insertContextMsg(t, env, conv.ID, userID, longContent, members)

	// Create a context manager with a small max_tokens limit.
	// max_tokens=100 allows at most 2 messages (2×50=100 ≤ 100).
	// 3 messages (3×50=150) would exceed the limit.
	cm := agent.NewDBContextManager(env.store.MessageStore())
	cfg := &agent.AgentConfig{
		Context: agent.AgentContext{
			MaxTokens:   100,
			MaxMessages: 0, // unused when MaxTokens > 0
		},
	}

	msgs, err := cm.GetContext(context.Background(), conv.ID, cfg)
	require.NoError(t, err, "GetContext should succeed")

	// trimByTokens accumulates from newest to oldest. With 50 tokens per
	// message and maxTokens=100:
	//   - msg3 (newest): 50 tokens, total=50 ≤ 100 → keep
	//   - msg2: 50 tokens, total=100 ≤ 100 → keep
	//   - msg1: 50 tokens, total=150 > 100 → trim
	require.Len(t, msgs, 2, "should keep 2 newest messages (100 tokens max)")

	// Verify the kept messages are the newest ones (msg2 and msg3).
	assert.Equal(t, agentUserID, msgs[0].SenderID,
		"first kept message should be the agent's reply (msg2)")
	assert.Equal(t, userID, msgs[1].SenderID,
		"second kept message should be the user's latest (msg3)")
}

// ---------------------------------------------------------------------------
// TestAgentContext_AE_CTX_003 — Message count fallback
// Scenario: max_tokens=0 falls back to max_messages trimming (D-060)
// Verifies: DBContextManager uses trimByMessages when MaxTokens is zero.
// ---------------------------------------------------------------------------
func TestAgentContext_AE_CTX_003(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ctx-003"
	agentUserID := "agent/test-bot"
	members := []string{userID, agentUserID}

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Insert 10 messages (5 turns of user+agent).
	for i := 0; i < 5; i++ {
		_ = insertContextMsg(t, env, conv.ID, userID, fmt.Sprintf("user message %d", i), members)
		_ = insertContextMsg(t, env, conv.ID, agentUserID, fmt.Sprintf("agent reply %d", i), members)
	}

	// Create a context manager with max_tokens=0 and max_messages=3.
	cm := agent.NewDBContextManager(env.store.MessageStore())
	cfg := &agent.AgentConfig{
		Context: agent.AgentContext{
			MaxTokens:   0, // triggers message-count fallback
			MaxMessages: 3,
		},
	}

	msgs, err := cm.GetContext(context.Background(), conv.ID, cfg)
	require.NoError(t, err, "GetContext should succeed")

	// trimByMessages returns at most the 3 newest messages.
	require.Len(t, msgs, 3, "should return at most max_messages=3 messages")

	// The 3 newest are: agent reply 4 (msg10), user message 4 (msg9),
	// agent reply 3 (msg8). Verify chronological order (oldest first).
	assert.Equal(t, agentUserID, msgs[0].SenderID,
		"oldest of the 3 kept messages should be agent reply 3")
	assert.Contains(t, msgs[0].Content, "3",
		"first kept message should contain '3' (agent reply 3)")
	assert.Equal(t, userID, msgs[1].SenderID,
		"second kept message should be user message 4")
	assert.Equal(t, agentUserID, msgs[2].SenderID,
		"third (newest) kept message should be agent reply 4")
}

// ---------------------------------------------------------------------------
// TestAgentContext_AE_CTX_004 — Cache hit avoids DB query
// Scenario: Same conversation requested multiple times in short period, only
//
//	first hits DB (D-060)
//
// Verifies: Two consecutive GetContext calls return identical results; the
// second call is served from the in-memory cache.
// ---------------------------------------------------------------------------
func TestAgentContext_AE_CTX_004(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ctx-004"
	agentUserID := "agent/test-bot"
	members := []string{userID, agentUserID}

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Insert initial messages.
	_ = insertContextMsg(t, env, conv.ID, userID, "hello cache test", members)
	_ = insertContextMsg(t, env, conv.ID, agentUserID, "cached reply", members)

	// Use default TTL (30s) — both calls will be within the TTL window.
	cm := agent.NewDBContextManager(env.store.MessageStore())
	cfg := &agent.AgentConfig{
		Context: agent.AgentContext{
			MaxTokens:   4000,
			MaxMessages: 10,
		},
	}

	// First call: loads from DB and caches.
	msgs1, err := cm.GetContext(context.Background(), conv.ID, cfg)
	require.NoError(t, err, "first GetContext should succeed")
	require.Len(t, msgs1, 2, "should load 2 messages from DB")

	// Insert a new message AFTER the cache is populated.
	_ = insertContextMsg(t, env, conv.ID, userID, "new message after cache", members)

	// Second call: should hit cache (within 30s TTL) and NOT see the new message.
	msgs2, err := cm.GetContext(context.Background(), conv.ID, cfg)
	require.NoError(t, err, "second GetContext should succeed")

	// Cache hit: same result as first call (2 messages, not 3).
	assert.Len(t, msgs2, 2,
		"cached result should still have 2 messages (new message not visible)")

	// Verify content identity — the cached slice should be the same data.
	assert.Equal(t, msgs1[0].Content, msgs2[0].Content,
		"cached messages should match original")
	assert.Equal(t, msgs1[1].Content, msgs2[1].Content,
		"cached messages should match original")
}

// ---------------------------------------------------------------------------
// TestAgentContext_AE_CTX_005 — Cache TTL expiry triggers reload
// Scenario: After cache expires, next request reloads from DB (D-060)
// Verifies: Once TTL elapses, GetContext fetches fresh data including any
//
//	new messages inserted after the initial cache population.
//
// ---------------------------------------------------------------------------
func TestAgentContext_AE_CTX_005(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ctx-005"
	agentUserID := "agent/test-bot"
	members := []string{userID, agentUserID}

	conv := createAgentConversation(t, env, userID, agentUserID)

	// Insert initial messages.
	_ = insertContextMsg(t, env, conv.ID, userID, "initial message", members)
	_ = insertContextMsg(t, env, conv.ID, agentUserID, "initial reply", members)

	// Use a very short TTL (100ms) so the test completes quickly.
	cm := agent.NewDBContextManager(env.store.MessageStore(), agent.WithCacheTTL(100*time.Millisecond))
	cfg := &agent.AgentConfig{
		Context: agent.AgentContext{
			MaxTokens:   4000,
			MaxMessages: 10,
		},
	}

	// First call: loads from DB and caches with 100ms TTL.
	msgs1, err := cm.GetContext(context.Background(), conv.ID, cfg)
	require.NoError(t, err, "first GetContext should succeed")
	require.Len(t, msgs1, 2, "should load 2 initial messages")

	// Insert a new message after cache population.
	_ = insertContextMsg(t, env, conv.ID, userID, "message after ttl", members)

	// Wait for the cache TTL to expire.
	time.Sleep(200 * time.Millisecond)

	// Second call: cache expired, should reload from DB.
	msgs2, err := cm.GetContext(context.Background(), conv.ID, cfg)
	require.NoError(t, err, "second GetContext after TTL should succeed")

	// After TTL expiry, the new message should be visible.
	assert.Len(t, msgs2, 3,
		"should reload from DB and include the new message (3 total)")

	// Verify the new message is present.
	found := false
	for _, m := range msgs2 {
		if m.Content == "message after ttl" {
			found = true
		}
	}
	assert.True(t, found,
		"reloaded context should include the message inserted after TTL expiry")
}
