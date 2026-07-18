package store

import (
	"context"
	"errors"
	"fmt"
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

			beforeUpdate := time.Now().Truncate(time.Second)
			_, err := s.Conversations.UpdateAgentStatus(ctx, "conv-hitl-1", model.AgentStatusAskingUser, "agent/test-agent", "cp-123")
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
			_, err := s.Conversations.UpdateAgentStatus(ctx, "conv-hitl-2", model.AgentStatusThinking, "agent/a1", "cp-1")
			require.NoError(t, err)
			got, err := s.Conversations.Get(ctx, "conv-hitl-2")
			require.NoError(t, err)
			assert.Equal(t, model.AgentStatusThinking, got.AgentStatus)

			// thinking -> tool_calling
			_, err = s.Conversations.UpdateAgentStatus(ctx, "conv-hitl-2", model.AgentStatusToolCalling, "agent/a1", "cp-1")
			require.NoError(t, err)
			got, err = s.Conversations.Get(ctx, "conv-hitl-2")
			require.NoError(t, err)
			assert.Equal(t, model.AgentStatusToolCalling, got.AgentStatus)

			// tool_calling -> asking_user
			_, err = s.Conversations.UpdateAgentStatus(ctx, "conv-hitl-2", model.AgentStatusAskingUser, "agent/a1", "cp-2")
			require.NoError(t, err)
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

		_, err := s.Conversations.UpdateAgentStatus(ctx, "non-existent-id", model.AgentStatusThinking, "agent/a1", "cp-1")
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
			_, err := s.Conversations.UpdateAgentStatus(ctx, "conv-clear-1", model.AgentStatusAskingUser, "agent/test-agent", "cp-abc")
			require.NoError(t, err)

			// Now clear it.
			beforeClear := time.Now().Truncate(time.Second)
			_, err = s.Conversations.ClearAgentStatus(ctx, "conv-clear-1")
			require.NoError(t, err)

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
			_, err := s.Conversations.ClearAgentStatus(ctx, "conv-clear-2")
			require.NoError(t, err)

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

		_, err := s.Conversations.ClearAgentStatus(ctx, "non-existent-id")
		require.Error(t, err, "expected error for non-existent conversation")
		assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got: %v", err)
	})
}

// ---------------------------------------------------------------------------
// A1: UpdateAgentStatus returns correct timestamp and updates updated_at
// ---------------------------------------------------------------------------

// TestConversationStore_UpdateAgentStatus_ReturnsTimestamp verifies that
// UpdateAgentStatus returns a non-zero timestamp close to the current time,
// and that the conversation's updated_at field is updated accordingly (D-124).
func TestConversationStore_UpdateAgentStatus_ReturnsTimestamp(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-ts-1", "user-1", "user-2", "direct", "Timestamp Test")
		require.NoError(t, s.Conversations.Create(ctx, conv))

		beforeUpdate := time.Now().Add(-time.Second)
		ts, err := s.Conversations.UpdateAgentStatus(ctx, "conv-ts-1", model.AgentStatusThinking, "agent/a1", "cp-1")
		require.NoError(t, err)

		// Timestamp should be non-zero and close to now.
		assert.False(t, ts.IsZero(), "returned timestamp should not be zero")
		assert.True(t, ts.After(beforeUpdate), "returned timestamp should be after the call time")
		assert.True(t, ts.Before(time.Now().Add(time.Second)), "returned timestamp should be before now+1s")

		// The conversation's updated_at should match the returned timestamp.
		got, err := s.Conversations.Get(ctx, "conv-ts-1")
		require.NoError(t, err)
		assert.Equal(t, ts.Unix(), got.UpdatedAt.Unix(),
			"conversation updated_at should match returned timestamp (second precision)")
	})
}

// ---------------------------------------------------------------------------
// A2: ClearAgentStatus returns correct timestamp and updates updated_at
// ---------------------------------------------------------------------------

// TestConversationStore_ClearAgentStatus_ReturnsTimestamp verifies that
// ClearAgentStatus returns a non-zero timestamp close to the current time,
// and that the conversation's updated_at field is updated accordingly (D-124).
func TestConversationStore_ClearAgentStatus_ReturnsTimestamp(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-ts-2", "user-1", "user-2", "direct", "Clear Timestamp")
		require.NoError(t, s.Conversations.Create(ctx, conv))

		// Set agent to active first.
		_, err := s.Conversations.UpdateAgentStatus(ctx, "conv-ts-2", model.AgentStatusAskingUser, "agent/a1", "cp-2")
		require.NoError(t, err)

		beforeClear := time.Now().Add(-time.Second)
		ts, err := s.Conversations.ClearAgentStatus(ctx, "conv-ts-2")
		require.NoError(t, err)

		// Timestamp should be non-zero and close to now.
		assert.False(t, ts.IsZero(), "returned timestamp should not be zero")
		assert.True(t, ts.After(beforeClear), "returned timestamp should be after the call time")

		// The conversation's updated_at should match the returned timestamp.
		got, err := s.Conversations.Get(ctx, "conv-ts-2")
		require.NoError(t, err)
		assert.Equal(t, ts.Unix(), got.UpdatedAt.Unix(),
			"conversation updated_at should match returned timestamp (second precision)")
	})
}

// ---------------------------------------------------------------------------
// A3-A7: ListStaleHITLConversations tests (D-123)
// ---------------------------------------------------------------------------

// setConversationAgentLastActivity sets the agent_last_activity field directly,
// bypassing GORM's auto-update. Used to simulate stale conversations
// for testing stale conversation queries (D-123).
func setConversationAgentLastActivity(t *testing.T, s *Store, convID string, lastActivity time.Time) {
	t.Helper()
	ctx := context.Background()
	result := s.db.WithContext(ctx).
		Model(&model.Conversation{}).
		Where("id = ?", convID).
		Update("agent_last_activity", lastActivity)
	require.NoError(t, result.Error)
	require.Equal(t, int64(1), result.RowsAffected, "should update exactly one row")
}

// TestListStaleHITLConversations_ReturnsStaleAskingUser verifies that
// ListStaleHITLConversations returns conversations in asking_user status
// whose agent_last_activity is older than the maxAge cutoff (D-123).
func TestListStaleHITLConversations_ReturnsStaleAskingUser(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create a conversation in asking_user status.
		conv := newTestConv("conv-stale-1", "user-1", "user-2", "direct", "Stale HITL")
		require.NoError(t, s.Conversations.Create(ctx, conv))
		_, err := s.Conversations.UpdateAgentStatus(ctx, "conv-stale-1", model.AgentStatusAskingUser, "agent/bot", "cp-stale")
		require.NoError(t, err)

		// Set agent_last_activity to 2 days ago (stale, beyond the 24h default).
		setConversationAgentLastActivity(t, s, "conv-stale-1", time.Now().Add(-48*time.Hour))

		results, err := s.Conversations.ListStaleHITLConversations(ctx, 24*time.Hour, 100)
		require.NoError(t, err)
		assert.Len(t, results, 1, "should find 1 stale conversation")
		assert.Equal(t, "conv-stale-1", results[0].ID)
	})
}

// TestListStaleHITLConversations_ExcludesNonStale verifies that
// ListStaleHITLConversations does not return conversations whose agent_last_activity
// is within the maxAge window (not yet stale).
func TestListStaleHITLConversations_ExcludesNonStale(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create a conversation in asking_user status but recently updated.
		conv := newTestConv("conv-fresh-1", "user-1", "user-2", "direct", "Fresh HITL")
		require.NoError(t, s.Conversations.Create(ctx, conv))
		_, err := s.Conversations.UpdateAgentStatus(ctx, "conv-fresh-1", model.AgentStatusAskingUser, "agent/bot", "cp-fresh")
		require.NoError(t, err)

		// agent_last_activity is now (not stale).
		results, err := s.Conversations.ListStaleHITLConversations(ctx, 24*time.Hour, 100)
		require.NoError(t, err)
		assert.Empty(t, results, "should not return non-stale conversations")
	})
}

// TestListStaleHITLConversations_ExcludesNonAskingUser verifies that
// ListStaleHITLConversations only returns conversations in asking_user
// status, not those in other agent statuses.
func TestListStaleHITLConversations_ExcludesNonAskingUser(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create conversations in various statuses (all stale).
		// Each conversation needs a unique user pair (unique index on user_id1, user_id2).
		statuses := []string{
			model.AgentStatusIdle,
			model.AgentStatusThinking,
			model.AgentStatusToolCalling,
			model.AgentStatusGenerating,
			model.AgentStatusTimeout,
		}
		for i, status := range statuses {
			convID := "conv-non-ask-" + status
			uid1 := fmt.Sprintf("user-na-%d", i)
			uid2 := fmt.Sprintf("peer-na-%d", i)
			conv := newTestConv(convID, uid1, uid2, "direct", "Non-asking "+status)
			require.NoError(t, s.Conversations.Create(ctx, conv))
			_, err := s.Conversations.UpdateAgentStatus(ctx, convID, status, "agent/bot", "cp-"+status)
			require.NoError(t, err)
			// Set agent_last_activity to 2 days ago (would be stale if asking_user).
			setConversationAgentLastActivity(t, s, convID, time.Now().Add(-48*time.Hour))
		}

		results, err := s.Conversations.ListStaleHITLConversations(ctx, 24*time.Hour, 100)
		require.NoError(t, err)
		assert.Empty(t, results, "should not return conversations in non-asking_user statuses")
	})
}

// TestListStaleHITLConversations_ExcludesSoftDeleted verifies that
// ListStaleHITLConversations does not return soft-deleted conversations.
func TestListStaleHITLConversations_ExcludesSoftDeleted(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-del-1", "user-1", "user-2", "direct", "Deleted HITL")
		require.NoError(t, s.Conversations.Create(ctx, conv))
		_, err := s.Conversations.UpdateAgentStatus(ctx, "conv-del-1", model.AgentStatusAskingUser, "agent/bot", "cp-del")
		require.NoError(t, err)

		// Set agent_last_activity to 2 days ago.
		setConversationAgentLastActivity(t, s, "conv-del-1", time.Now().Add(-48*time.Hour))

		// Soft-delete the conversation.
		require.NoError(t, s.Conversations.Delete(ctx, "conv-del-1"))

		results, err := s.Conversations.ListStaleHITLConversations(ctx, 24*time.Hour, 100)
		require.NoError(t, err)
		assert.Empty(t, results, "should not return soft-deleted conversations")
	})
}

// TestListStaleHITLConversations_RespectsLimit verifies that
// ListStaleHITLConversations returns at most `limit` results.
func TestListStaleHITLConversations_RespectsLimit(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create 5 stale asking_user conversations.
		for i := 0; i < 5; i++ {
			convID := fmt.Sprintf("conv-limit-%d", i)
			uid1 := fmt.Sprintf("user-lim-%d", i)
			uid2 := fmt.Sprintf("peer-lim-%d", i)
			conv := newTestConv(convID, uid1, uid2, "direct", fmt.Sprintf("Limit %d", i))
			require.NoError(t, s.Conversations.Create(ctx, conv))
			_, err := s.Conversations.UpdateAgentStatus(ctx, convID, model.AgentStatusAskingUser, "agent/bot", fmt.Sprintf("cp-%d", i))
			require.NoError(t, err)
			// Stagger agent_last_activity so ordering is deterministic.
			setConversationAgentLastActivity(t, s, convID, time.Now().Add(-48*time.Hour).Add(time.Duration(i)*time.Minute))
		}

		// Limit to 3.
		results, err := s.Conversations.ListStaleHITLConversations(ctx, 24*time.Hour, 3)
		require.NoError(t, err)
		assert.Len(t, results, 3, "should return at most 3 conversations")

		// Verify ordering: oldest agent_last_activity first (ASC).
		for i := 1; i < len(results); i++ {
			assert.True(t, !results[i].AgentLastActivity.Before(results[i-1].AgentLastActivity),
				"results should be ordered by agent_last_activity ASC")
		}
	})
}
