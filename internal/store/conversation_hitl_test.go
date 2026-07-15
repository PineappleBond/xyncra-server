package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConversationStore_UpdateAgentStatus verifies that UpdateAgentStatus sets
// agent_status, agent_id, checkpoint_id and updates agent_last_activity.
func TestConversationStore_UpdateAgentStatus(t *testing.T) {
	t.Run("sets agent state fields correctly", func(t *testing.T) {
		runOnAllDatabases(t, func(t *testing.T, s *Store) {
			ctx := context.Background()
			cleanAll(t, s, ctx)

			conv := newTestConv("conv-hitl-1", "user-1", "user-2", "direct", "HITL Test")
			require.NoError(t, s.Conversations.Create(ctx, conv))

			beforeUpdate := time.Now()
			err := s.Conversations.UpdateAgentStatus(ctx, "conv-hitl-1", model.AgentStatusAskingUser, "agent/test-agent", "cp-123")
			require.NoError(t, err)

			got, err := s.Conversations.Get(ctx, "conv-hitl-1")
			require.NoError(t, err)
			assert.Equal(t, model.AgentStatusAskingUser, got.AgentStatus, "agent_status should be asking_user")
			assert.Equal(t, "agent/test-agent", got.AgentID, "agent_id should match")
			assert.Equal(t, "cp-123", got.CheckpointID, "checkpoint_id should match")
			assert.True(t, got.AgentLastActivity.After(beforeUpdate) || got.AgentLastActivity.Equal(beforeUpdate),
				"agent_last_activity should be updated to current time")
		})
	})

	t.Run("transitions through multiple statuses", func(t *testing.T) {
		runOnAllDatabases(t, func(t *testing.T, s *Store) {
			ctx := context.Background()
			cleanAll(t, s, ctx)

			conv := newTestConv("conv-hitl-2", "user-1", "user-2", "direct", "HITL Transitions")
			require.NoError(t, s.Conversations.Create(ctx, conv))

			// idle -> thinking
			require.NoError(t, s.Conversations.UpdateAgentStatus(ctx, "conv-hitl-2", model.AgentStatusThinking, "agent/a1", "cp-1"))
			got, err := s.Conversations.Get(ctx, "conv-hitl-2")
			require.NoError(t, err)
			assert.Equal(t, model.AgentStatusThinking, got.AgentStatus)

			// thinking -> tool_calling
			require.NoError(t, s.Conversations.UpdateAgentStatus(ctx, "conv-hitl-2", model.AgentStatusToolCalling, "agent/a1", "cp-1"))
			got, err = s.Conversations.Get(ctx, "conv-hitl-2")
			require.NoError(t, err)
			assert.Equal(t, model.AgentStatusToolCalling, got.AgentStatus)

			// tool_calling -> asking_user
			require.NoError(t, s.Conversations.UpdateAgentStatus(ctx, "conv-hitl-2", model.AgentStatusAskingUser, "agent/a1", "cp-2"))
			got, err = s.Conversations.Get(ctx, "conv-hitl-2")
			require.NoError(t, err)
			assert.Equal(t, model.AgentStatusAskingUser, got.AgentStatus)
			assert.Equal(t, "cp-2", got.CheckpointID, "checkpoint_id should be updated to cp-2")
		})
	})
}

// TestConversationStore_UpdateAgentStatus_NotFound verifies that updating a
// non-existent conversation returns ErrNotFound.
func TestConversationStore_UpdateAgentStatus_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		err := s.Conversations.UpdateAgentStatus(ctx, "non-existent-id", model.AgentStatusThinking, "agent/a1", "cp-1")
		require.Error(t, err, "expected error for non-existent conversation")
		assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got: %v", err)
	})
}

// TestConversationStore_ClearAgentStatus verifies that ClearAgentStatus resets
// the conversation agent state to idle and clears agent_id and checkpoint_id.
func TestConversationStore_ClearAgentStatus(t *testing.T) {
	t.Run("resets agent state to idle", func(t *testing.T) {
		runOnAllDatabases(t, func(t *testing.T, s *Store) {
			ctx := context.Background()
			cleanAll(t, s, ctx)

			conv := newTestConv("conv-clear-1", "user-1", "user-2", "direct", "Clear Test")
			require.NoError(t, s.Conversations.Create(ctx, conv))

			// Set agent to active state first.
			require.NoError(t, s.Conversations.UpdateAgentStatus(ctx, "conv-clear-1", model.AgentStatusAskingUser, "agent/test-agent", "cp-abc"))

			// Now clear it.
			beforeClear := time.Now()
			require.NoError(t, s.Conversations.ClearAgentStatus(ctx, "conv-clear-1"))

			got, err := s.Conversations.Get(ctx, "conv-clear-1")
			require.NoError(t, err)
			assert.Equal(t, model.AgentStatusIdle, got.AgentStatus, "agent_status should be idle")
			assert.Equal(t, "", got.AgentID, "agent_id should be empty")
			assert.Equal(t, "", got.CheckpointID, "checkpoint_id should be empty")
			assert.True(t, got.AgentLastActivity.After(beforeClear) || got.AgentLastActivity.Equal(beforeClear),
				"agent_last_activity should be updated")
		})
	})

	t.Run("clearing already idle conversation is a no-op", func(t *testing.T) {
		runOnAllDatabases(t, func(t *testing.T, s *Store) {
			ctx := context.Background()
			cleanAll(t, s, ctx)

			conv := newTestConv("conv-clear-2", "user-1", "user-2", "direct", "Already Idle")
			require.NoError(t, s.Conversations.Create(ctx, conv))

			// Clear without ever setting — should still succeed.
			require.NoError(t, s.Conversations.ClearAgentStatus(ctx, "conv-clear-2"))

			got, err := s.Conversations.Get(ctx, "conv-clear-2")
			require.NoError(t, err)
			assert.Equal(t, model.AgentStatusIdle, got.AgentStatus)
		})
	})
}

// TestConversationStore_ClearAgentStatus_NotFound verifies that clearing agent
// status on a non-existent conversation returns ErrNotFound.
func TestConversationStore_ClearAgentStatus_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		err := s.Conversations.ClearAgentStatus(ctx, "non-existent-id")
		require.Error(t, err, "expected error for non-existent conversation")
		assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got: %v", err)
	})
}
