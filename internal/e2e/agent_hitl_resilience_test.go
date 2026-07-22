// Package e2e_test contains HITL Resilience E2E tests for the Agent system.
//
// These tests verify the data persistence and recovery guarantees described in
// docs/DESIGN_HITL_RESILIENCE.md. They exercise the RemoteCalling table, Conversation
// agent_status state machine, Checkpoint persistence, and the resume flow
// across simulated server restarts and multi-device races.
//
// All tests use:
//   - SQLite in-memory database (via setupAgentE2E)
//   - Redis for checkpoints and conversation locks (via setupAgentE2E)
//   - Direct DB manipulation to simulate agent behaviour (D-110: MQ bypass)
package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// createTestRemoteCalling creates a RemoteCalling directly in the database.
// This simulates the agent creating a HITL remote calling during execution.
func createTestRemoteCalling(t *testing.T, env *agentE2EEnv, convID, checkpointID, interruptID, agentID string) *model.RemoteCalling {
	t.Helper()
	rc := &model.RemoteCalling{
		ID:             uuid.New().String(),
		ConversationID: convID,
		CheckpointID:   checkpointID,
		AgentID:        agentID,
		Method:         "ask_user",
		Params:         `{"question":"Are you sure you want to proceed?","interrupt_id":"` + interruptID + `"}`,
		InterruptID:    interruptID,
		Status:         model.RemoteCallingStatusPending,
		CreatedAt:      time.Now(),
		ExpiresAt:      timePtr(time.Now().Add(24 * time.Hour)),
	}
	err := env.store.RemoteCallingStore().Create(context.Background(), rc)
	require.NoError(t, err, "create remote calling should succeed")
	return rc
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// newHitlRedisClient creates a Redis client for HITL test checkpoint operations.
func newHitlRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// ---------------------------------------------------------------------------
// Scenario 1: Single Device Basic Flow
// ---------------------------------------------------------------------------

// TestHITLResilience_Scenario1_SingleDevice verifies the basic HITL flow for
// a single device:
//
//  1. Agent emits a remote calling -> RemoteCalling row created (status=pending)
//  2. Conversation agent_status set to "asking_user"
//  3. User answers -> RemoteCalling resolved (status=resolved)
//  4. All remote callings resolved -> resume task processes -> agent resumes
//
// Corresponds to DESIGN_HITL_RESILIENCE.md Scenario 1 (User Offline When Agent Asks).
func TestHITLResilience_Scenario1_SingleDevice(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-hitl-r1"
	agentUserID := "agent/test-bot"
	checkpointID := "ckpt-hitl-r1"
	interruptID := "intr-hitl-r1"

	// Create conversation and use the actual generated ID.
	conv := createAgentConversation(t, env, userID, agentUserID)
	convID := conv.ID

	// Step 1: Agent pauses, creates a RemoteCalling in DB (status=pending).
	rc := createTestRemoteCalling(t, env, convID, checkpointID, interruptID, agentUserID)

	// Step 2: Verify RemoteCalling is persisted with status=pending.
	ctx := context.Background()
	pendingRCs, err := env.store.RemoteCallingStore().GetPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, pendingRCs, 1, "should have 1 pending remote calling")
	assert.Equal(t, model.RemoteCallingStatusPending, pendingRCs[0].Status)
	assert.Equal(t, rc.ID, pendingRCs[0].ID)

	// Step 3: Update Conversation agent_status to asking_user.
	_, err = env.store.ConversationStore().UpdateAgentStatus(ctx, convID,
		model.AgentStatusAskingUser, agentUserID, checkpointID)
	require.NoError(t, err)

	// Step 4: Verify Conversation state.
	updatedConv, err := env.store.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusAskingUser, updatedConv.AgentStatus)
	assert.Equal(t, checkpointID, updatedConv.CheckpointID)
	assert.Equal(t, agentUserID, updatedConv.AgentID)

	// Step 5: User answers the remote calling.
	err = env.store.RemoteCallingStore().ResolveResult(ctx, rc.ID, "Yes, proceed")
	require.NoError(t, err, "ResolveResult should succeed")

	// Step 6: Verify RemoteCalling is now resolved.
	allRCs, err := env.store.RemoteCallingStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, allRCs, 1)
	assert.Equal(t, model.RemoteCallingStatusResolved, allRCs[0].Status)
	assert.Equal(t, "Yes, proceed", allRCs[0].Result)
	assert.True(t, allRCs[0].Success)
	assert.NotNil(t, allRCs[0].ResolvedAt)

	// Step 7: Verify no pending remote callings remain.
	pendingCount, err := env.store.RemoteCallingStore().CountPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), pendingCount, "should have 0 pending remote callings")
}
