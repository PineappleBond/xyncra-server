// Package e2e_test contains Category E HITL (Human-in-the-Loop) E2E tests
// for the Agent system (Phase 8B). Tests verify checkpoint storage, interrupt
// broadcasting, conversation lock behaviour during HITL, and resume flow.
//
// Because triggering a full Eino Runner interrupt requires the LLM to produce
// a compose.Interrupt response (which the mock LLM cannot do), most tests
// exercise individual HITL components directly: BroadcastHelper, CheckpointStore,
// and ConversationLock. This gives reliable, deterministic coverage of the
// HITL protocol without depending on LLM behaviour.
package e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// TestAgentHITL_AE_HITL_001 — Agent interrupts and asks question (D-085, D-087)
// ---------------------------------------------------------------------------

// TestAgentHITL_AE_HITL_001 verifies that when the agent triggers a HITL
// interrupt, the agent_question ephemeral update is broadcast to the human
// user with the correct question, checkpoint_id, and interrupt_id fields.
func TestAgentHITL_AE_HITL_001(t *testing.T) {
	// Scenario: AE-HITL-001
	// Verifies: agent_question ephemeral update sent to client (D-085, D-087)
	// Strategy: Directly test BroadcastHelper.SendAgentQuestion since triggering
	// a real Eino interrupt via mock LLM is not feasible.
	env := setupAgentE2E(t)
	userID := "user-hitl-001"
	agentUserID := "agent/test-bot"
	convID := "conv-hitl-001"

	// Create a conversation so the user is a valid broadcast target.
	createAgentConversation(t, env, userID, agentUserID)

	// Connect user WebSocket.
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()

	// Drain any initial push updates.
	drainPushUpdates(t, conn)

	// Create a BroadcastHelper using the test WebSocket server.
	broadcaster := agent.NewBroadcastHelper(env.srv, testLogger{})

	// Broadcast an agent_question ephemeral update.
	question := "Which city do you prefer?"
	checkpointID := "ckpt-001"
	interruptID := "intr-001"
	broadcaster.SendAgentQuestion(context.Background(), userID, agentUserID, convID,
		question, checkpointID, interruptID)

	// Wait for the agent_question ephemeral update.
	updates := waitForEphemeral(t, conn, protocol.UpdateTypeAgentQuestion, 30*time.Second)

	// Verify the update contents.
	var found bool
	for _, u := range updates.Updates {
		if u.Type != protocol.UpdateTypeAgentQuestion {
			continue
		}
		found = true
		assert.Equal(t, uint32(0), u.Seq, "agent_question should be ephemeral (Seq=0)")

		var payload struct {
			UserID         string `json:"user_id"`
			ConversationID string `json:"conversation_id"`
			Question       string `json:"question"`
			CheckpointID   string `json:"checkpoint_id"`
			InterruptID    string `json:"interrupt_id"`
		}
		require.NoError(t, json.Unmarshal(u.Payload, &payload))
		assert.Equal(t, agentUserID, payload.UserID, "user_id should be the agent")
		assert.Equal(t, convID, payload.ConversationID)
		assert.Equal(t, question, payload.Question, "question should match")
		assert.Equal(t, checkpointID, payload.CheckpointID, "checkpoint_id should match")
		assert.Equal(t, interruptID, payload.InterruptID, "interrupt_id should match")
	}
	assert.True(t, found, "should find an agent_question update")
}

// ---------------------------------------------------------------------------
// TestAgentHITL_AE_HITL_002 — Checkpoint saved to Redis (D-083)
// ---------------------------------------------------------------------------

// TestAgentHITL_AE_HITL_002 verifies that the RedisCheckPointStore correctly
// persists and retrieves checkpoint data in Redis.
func TestAgentHITL_AE_HITL_002(t *testing.T) {
	// Scenario: AE-HITL-002
	// Verifies: checkpoint creation → Redis contains corresponding key (D-083)
	// Strategy: Directly test RedisCheckPointStore.Set and .Get against the
	// test Redis instance.

	// Flush Redis DB to ensure clean state.
	redisClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	defer redisClient.Close()

	ctx := context.Background()
	require.NoError(t, redisClient.FlushDB(ctx).Err())

	// Create a checkpoint store with test defaults.
	store := agent.NewRedisCheckPointStore(redisClient, "test:ckpt:", 1*time.Hour)

	// Save a checkpoint.
	checkpointID := "ckpt-test-002"
	data := []byte(`{"state":"interrupted","answer_pending":true}`)
	err := store.Set(ctx, checkpointID, data)
	require.NoError(t, err, "Set should succeed")

	// Verify the key exists in Redis.
	redisKey := "test:ckpt:" + checkpointID
	val, err := redisClient.Get(ctx, redisKey).Result()
	require.NoError(t, err, "Redis GET should succeed after Set")
	assert.Equal(t, string(data), val, "stored data should match")

	// Verify Get returns the correct data.
	loaded, found, err := store.Get(ctx, checkpointID)
	require.NoError(t, err, "Get should succeed")
	assert.True(t, found, "Get should find the checkpoint")
	assert.Equal(t, data, loaded, "loaded data should match what was stored")

	// Verify Get for non-existent key returns (nil, false, nil).
	_, found2, err2 := store.Get(ctx, "non-existent-key")
	require.NoError(t, err2, "Get for missing key should not error")
	assert.False(t, found2, "Get for missing key should return found=false")
}

// ---------------------------------------------------------------------------
// TestAgentHITL_AE_HITL_003 — Agent continues after user answers (D-084, D-085)
// ---------------------------------------------------------------------------

// TestAgentHITL_AE_HITL_003 verifies that the AgentResumePayload can be
// correctly serialized and deserialized, and that the resume handler can be
// constructed with the expected dependencies.
//
// A full E2E resume flow (agent_resume RPC → MQ → executor → Runner.ResumeWithParams)
// requires the Eino Runner to have previously saved a checkpoint during an
// interrupt, which the mock LLM cannot trigger. Therefore this test validates
// the payload contract and handler wiring at the component level.
func TestAgentHITL_AE_HITL_003(t *testing.T) {
	// Scenario: AE-HITL-003
	// Verifies: agent_resume RPC → TypeAgentResume MQ → execution resumes (D-084, D-085)
	// Strategy: Component test — verify AgentResumePayload serialization and
	// that the resume handler can be constructed (wiring check).

	// Test AgentResumePayload serialization round-trip.
	original := agent.AgentResumePayload{
		ConversationID: "conv-resume-003",
		CheckpointID:   "ckpt-003",
		SenderID:       "user-003",
		AgentID:        "agent/test-bot",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err, "marshal AgentResumePayload should succeed")

	var decoded agent.AgentResumePayload
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "unmarshal AgentResumePayload should succeed")

	assert.Equal(t, original.ConversationID, decoded.ConversationID)
	assert.Equal(t, original.CheckpointID, decoded.CheckpointID)
	assert.Equal(t, original.SenderID, decoded.SenderID)
	assert.Equal(t, original.AgentID, decoded.AgentID)

	// Verify the resume handler can be constructed with the test environment.
	env := setupAgentE2E(t)
	handler := agent.NewAgentResumeHandler(env.executor, env.registry, env.lock, testLogger{}, nil)
	assert.NotNil(t, handler, "resume handler should be constructable")

	// Verify that calling the handler with nil task is a no-op.
	err = handler(context.Background(), nil)
	assert.NoError(t, err, "nil task should be handled gracefully")
}

// ---------------------------------------------------------------------------
// TestAgentHITL_AE_HITL_004 — Conversation lock held during HITL (D-084)
// ---------------------------------------------------------------------------

// TestAgentHITL_AE_HITL_004 verifies that the conversation lock remains held
// during a HITL interrupt, preventing new messages from triggering a new agent
// processing run on the same conversation.
func TestAgentHITL_AE_HITL_004(t *testing.T) {
	// Scenario: AE-HITL-004
	// Verifies: during interrupt, new messages don't trigger new agent processing (D-084)
	// Strategy: Directly test ConversationLock.Acquire/Release to verify the
	// lock-holding semantics that HITL depends on.
	env := setupAgentE2E(t)
	ctx := context.Background()
	convID := "conv-hitl-004"

	// Step 1: Acquire the lock (simulating task handler acquiring before execution).
	acquired, err := env.lock.Acquire(ctx, convID, 130*time.Second)
	require.NoError(t, err, "first Acquire should succeed")
	assert.True(t, acquired, "first Acquire should return true")

	// Step 2: Simulate HITL interrupt — lock is NOT released (D-084).
	// A second Acquire attempt should fail (lock already held).
	acquired2, err2 := env.lock.Acquire(ctx, convID, 130*time.Second)
	require.NoError(t, err2, "second Acquire should not error")
	assert.False(t, acquired2, "second Acquire should return false (lock held during HITL)")

	// Step 3: Release the lock (simulating resume handler completing).
	err = env.lock.Release(ctx, convID)
	require.NoError(t, err, "Release should succeed")

	// Step 4: After release, a new Acquire should succeed.
	acquired3, err3 := env.lock.Acquire(ctx, convID, 130*time.Second)
	require.NoError(t, err3, "Acquire after Release should succeed")
	assert.True(t, acquired3, "Acquire after Release should return true")
}

// ---------------------------------------------------------------------------
// TestAgentHITL_AE_HITL_005 — CheckpointStore failure aborts (D-083)
// ---------------------------------------------------------------------------

// TestAgentHITL_AE_HITL_005 verifies that the RedisCheckPointStore is
// fail-closed: when Redis is unreachable, Set returns an error so the HITL
// flow can abort rather than silently losing the checkpoint.
func TestAgentHITL_AE_HITL_005(t *testing.T) {
	// Scenario: AE-HITL-005
	// Verifies: when Redis unavailable, HITL aborts with error (D-083)
	// Strategy: Create a CheckpointStore with an invalid Redis address and
	// verify that Set returns an error (fail-closed, D-083).

	// Use an invalid Redis address to simulate Redis being unreachable.
	badClient := redis.NewClient(&redis.Options{
		Addr: "localhost:1", // invalid port — Redis won't be here
	})
	defer badClient.Close()

	store := agent.NewRedisCheckPointStore(badClient, "test:ckpt:", 1*time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Set should fail because Redis is unreachable.
	err := store.Set(ctx, "ckpt-fail-005", []byte("data"))
	require.Error(t, err, "Set with unreachable Redis should return error (D-083: fail-closed)")

	// Get should also fail.
	_, _, err = store.Get(ctx, "ckpt-fail-005")
	require.Error(t, err, "Get with unreachable Redis should return error (D-083: fail-closed)")
}

// ---------------------------------------------------------------------------
// TestAgentHITL_AE_HITL_006 — agent_checkpoint_created notification (D-087)
// ---------------------------------------------------------------------------

// TestAgentHITL_AE_HITL_006 verifies that after a checkpoint is created,
// an agent_checkpoint_created ephemeral update is broadcast to the human user.
func TestAgentHITL_AE_HITL_006(t *testing.T) {
	// Scenario: AE-HITL-006
	// Verifies: ephemeral notification sent after checkpoint creation (D-087)
	// Strategy: Directly test BroadcastHelper.SendAgentCheckpointCreated.
	env := setupAgentE2E(t)
	userID := "user-hitl-006"
	agentUserID := "agent/test-bot"
	convID := "conv-hitl-006"

	// Create a conversation so the user is a valid broadcast target.
	createAgentConversation(t, env, userID, agentUserID)

	// Connect user WebSocket.
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()

	// Drain any initial push updates.
	drainPushUpdates(t, conn)

	// Create a BroadcastHelper.
	broadcaster := agent.NewBroadcastHelper(env.srv, testLogger{})

	// Broadcast an agent_checkpoint_created ephemeral update.
	checkpointID := "ckpt-006"
	broadcaster.SendAgentCheckpointCreated(context.Background(), userID, agentUserID, convID, checkpointID)

	// Wait for the agent_checkpoint_created ephemeral update.
	updates := waitForEphemeral(t, conn, protocol.UpdateTypeAgentCheckpointCreated, 30*time.Second)

	// Verify the update contents.
	var found bool
	for _, u := range updates.Updates {
		if u.Type != protocol.UpdateTypeAgentCheckpointCreated {
			continue
		}
		found = true
		assert.Equal(t, uint32(0), u.Seq, "agent_checkpoint_created should be ephemeral (Seq=0)")

		var payload struct {
			UserID         string `json:"user_id"`
			ConversationID string `json:"conversation_id"`
			CheckpointID   string `json:"checkpoint_id"`
		}
		require.NoError(t, json.Unmarshal(u.Payload, &payload))
		assert.Equal(t, agentUserID, payload.UserID, "user_id should be the agent")
		assert.Equal(t, convID, payload.ConversationID)
		assert.Equal(t, checkpointID, payload.CheckpointID, "checkpoint_id should match")
	}
	assert.True(t, found, "should find an agent_checkpoint_created update")
}
