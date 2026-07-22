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

// newTestRemoteCalling creates a remote calling linked to the given conversation.
func newTestRemoteCalling(id, conversationID, checkpointID, agentID, method string) *model.RemoteCalling {
	return &model.RemoteCalling{
		ID:             id,
		ConversationID: conversationID,
		CheckpointID:   checkpointID,
		AgentID:        agentID,
		Method:         method,
		Status:         model.RemoteCallingStatusPending,
		CreatedAt:      testNow,
	}
}

// createTestConversation is a helper that creates a conversation and returns it.
func createTestConversation(t *testing.T, s *Store, ctx context.Context, id string) *model.Conversation {
	t.Helper()
	conv := newTestConv(id, "user-1", "user-2", "direct", "HITL Test")
	require.NoError(t, s.Conversations.Create(ctx, conv))
	return conv
}

// createTestConversationWithUsers creates a conversation with specific user IDs.
func createTestConversationWithUsers(t *testing.T, s *Store, ctx context.Context, id, uid1, uid2 string) *model.Conversation {
	t.Helper()
	conv := newTestConv(id, uid1, uid2, "direct", "HITL Test")
	require.NoError(t, s.Conversations.Create(ctx, conv))
	return conv
}

// TestRemoteCallingStore_Create_HappyPath verifies that Create persists a remote calling
// and that the DB record matches the input.
func TestRemoteCallingStore_Create_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")
		rc := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")

		require.NoError(t, s.RemoteCallings.Create(ctx, rc))

		got, err := s.RemoteCallings.GetByID(ctx, "rc-1")
		require.NoError(t, err)
		assert.Equal(t, "rc-1", got.ID)
		assert.Equal(t, "conv-1", got.ConversationID)
		assert.Equal(t, "cp-1", got.CheckpointID)
		assert.Equal(t, "agent/bot", got.AgentID)
		assert.Equal(t, "ask_user", got.Method)
		assert.Equal(t, model.RemoteCallingStatusPending, got.Status)
	})
}

// TestRemoteCallingStore_GetByID verifies that GetByID returns the correct record.
func TestRemoteCallingStore_GetByID(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")
		rc := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc))

		got, err := s.RemoteCallings.GetByID(ctx, "rc-1")
		require.NoError(t, err)
		assert.Equal(t, "rc-1", got.ID)
	})
}

// TestRemoteCallingStore_GetByID_NotFound verifies that GetByID returns ErrNotFound
// for a non-existent record.
func TestRemoteCallingStore_GetByID_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		_, err := s.RemoteCallings.GetByID(ctx, "non-existent-id")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got: %v", err)
	})
}

// TestRemoteCallingStore_GetPendingByConversation verifies that GetPendingByConversation returns
// all pending remote callings for a conversation, ordered by created_at ASC.
func TestRemoteCallingStore_GetPendingByConversation(t *testing.T) {
	t.Run("returns multiple remote callings ordered by created_at ASC", func(t *testing.T) {
		runOnAllDatabases(t, func(t *testing.T, s *Store) {
			ctx := context.Background()
			cleanAll(t, s, ctx)

			createTestConversation(t, s, ctx, "conv-1")

			rc1 := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
			rc1.CreatedAt = testNow
			require.NoError(t, s.RemoteCallings.Create(ctx, rc1))

			rc2 := newTestRemoteCalling("rc-2", "conv-1", "cp-1", "agent/bot", "ask_user")
			rc2.CreatedAt = testNow.Add(time.Second)
			require.NoError(t, s.RemoteCallings.Create(ctx, rc2))

			// Resolved one
			rc3 := newTestRemoteCalling("rc-3", "conv-1", "cp-2", "agent/bot", "ask_user")
			rc3.CreatedAt = testNow.Add(2 * time.Second)
			require.NoError(t, s.RemoteCallings.Create(ctx, rc3))
			require.NoError(t, s.RemoteCallings.ResolveResult(ctx, "rc-3", "done"))

			got, err := s.RemoteCallings.GetPendingByConversation(ctx, "conv-1")
			require.NoError(t, err)
			require.Len(t, got, 2)
			assert.Equal(t, "rc-1", got[0].ID, "first should be rc-1")
			assert.Equal(t, "rc-2", got[1].ID, "second should be rc-2")
		})
	})

	t.Run("empty conversation returns empty slice", func(t *testing.T) {
		runOnAllDatabases(t, func(t *testing.T, s *Store) {
			ctx := context.Background()
			cleanAll(t, s, ctx)

			got, err := s.RemoteCallings.GetPendingByConversation(ctx, "non-existent-conv")
			require.NoError(t, err)
			assert.Empty(t, got, "should return empty slice for non-existent conversation")
		})
	})
}

// TestRemoteCallingStore_GetPendingByCheckpoint verifies that GetPendingByCheckpoint
// returns only pending remote callings for the given checkpoint.
func TestRemoteCallingStore_GetPendingByCheckpoint(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")

		// Create a pending remote calling.
		rcPending := newTestRemoteCalling("rc-pending", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rcPending))

		// Create and resolve another remote calling.
		rcResolved := newTestRemoteCalling("rc-resolved", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rcResolved))
		require.NoError(t, s.RemoteCallings.ResolveResult(ctx, "rc-resolved", "done"))

		// Create a pending remote calling for a different checkpoint.
		rcOther := newTestRemoteCalling("rc-other", "conv-1", "cp-2", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rcOther))

		got, err := s.RemoteCallings.GetPendingByCheckpoint(ctx, "cp-1")
		require.NoError(t, err)
		require.Len(t, got, 1, "should return only 1 pending remote calling for cp-1")
		assert.Equal(t, "rc-pending", got[0].ID)
		assert.Equal(t, model.RemoteCallingStatusPending, got[0].Status)
	})
}

// TestRemoteCallingStore_GetByCheckpoint verifies that GetByCheckpoint returns all
// remote callings (all statuses) for a given checkpoint.
func TestRemoteCallingStore_GetByCheckpoint(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")

		rcPending := newTestRemoteCalling("rc-pending", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rcPending))

		rcResolved := newTestRemoteCalling("rc-resolved", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rcResolved))
		require.NoError(t, s.RemoteCallings.ResolveResult(ctx, "rc-resolved", "done"))

		got, err := s.RemoteCallings.GetByCheckpoint(ctx, "cp-1")
		require.NoError(t, err)
		require.Len(t, got, 2, "should return both pending and resolved remote callings")
	})
}

// TestRemoteCallingStore_ResolveResult_HappyPath verifies that ResolveResult correctly
// marks a pending remote calling as resolved with success.
func TestRemoteCallingStore_ResolveResult_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")
		rc := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc))

		require.NoError(t, s.RemoteCallings.ResolveResult(ctx, "rc-1", "Alice"))

		got, err := s.RemoteCallings.GetByID(ctx, "rc-1")
		require.NoError(t, err)

		assert.Equal(t, model.RemoteCallingStatusResolved, got.Status, "status should be resolved")
		assert.Equal(t, "Alice", got.Result, "result should match")
		assert.True(t, got.Success, "success should be true")
		assert.NotNil(t, got.ResolvedAt, "resolved_at should be set")
	})
}

// TestRemoteCallingStore_ResolveResult_NotFound verifies that resolving a non-existent
// remote calling returns ErrNotFound.
func TestRemoteCallingStore_ResolveResult_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		err := s.RemoteCallings.ResolveResult(ctx, "non-existent-id", "result")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got: %v", err)
	})
}

// TestRemoteCallingStore_ResolveResult_AlreadyResolved verifies that resolving a
// remote calling that has already been resolved returns ErrConflict.
func TestRemoteCallingStore_ResolveResult_AlreadyResolved(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")
		rc := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc))

		// First resolve should succeed.
		require.NoError(t, s.RemoteCallings.ResolveResult(ctx, "rc-1", "Alice"))

		// Second resolve should return ErrConflict (idempotency check).
		err := s.RemoteCallings.ResolveResult(ctx, "rc-1", "Bob")
		require.Error(t, err, "expected error for already resolved remote calling")
		assert.True(t, errors.Is(err, ErrConflict), "expected ErrConflict, got: %v", err)
	})
}

// TestRemoteCallingStore_ResolveError_HappyPath verifies that ResolveError correctly
// marks a pending remote calling as resolved with failure.
func TestRemoteCallingStore_ResolveError_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")
		rc := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc))

		require.NoError(t, s.RemoteCallings.ResolveError(ctx, "rc-1", "timeout"))

		got, err := s.RemoteCallings.GetByID(ctx, "rc-1")
		require.NoError(t, err)

		assert.Equal(t, model.RemoteCallingStatusResolved, got.Status, "status should be resolved")
		assert.Equal(t, "timeout", got.ErrorMessage, "error_message should match")
		assert.False(t, got.Success, "success should be false")
		assert.NotNil(t, got.ResolvedAt, "resolved_at should be set")
	})
}

// TestRemoteCallingStore_CancelByCheckpoint verifies that CancelByCheckpoint
// cancels all pending remote callings for a checkpoint.
func TestRemoteCallingStore_CancelByCheckpoint(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")

		rc1 := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc1))

		rc2 := newTestRemoteCalling("rc-2", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc2))

		// Already resolved one
		rc3 := newTestRemoteCalling("rc-3", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc3))
		require.NoError(t, s.RemoteCallings.ResolveResult(ctx, "rc-3", "done"))

		count, _, _, err := s.RemoteCallings.CancelByCheckpoint(ctx, "cp-1", "test-user", "user cancelled")
		require.NoError(t, err)
		assert.Equal(t, int64(2), count, "should cancel 2 pending remote callings")

		// Verify pending count is 0
		pending, err := s.RemoteCallings.CountPendingByCheckpoint(ctx, "cp-1")
		require.NoError(t, err)
		assert.Equal(t, int64(0), pending)
	})
}

// TestRemoteCallingStore_DeleteByConversation verifies that DeleteByConversation
// removes all remote callings associated with the given conversation.
func TestRemoteCallingStore_DeleteByConversation(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversationWithUsers(t, s, ctx, "conv-1", "user-1", "user-2")
		createTestConversationWithUsers(t, s, ctx, "conv-2", "user-3", "user-4")

		rc1 := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
		rc2 := newTestRemoteCalling("rc-2", "conv-1", "cp-1", "agent/bot", "ask_user")
		rc3 := newTestRemoteCalling("rc-3", "conv-2", "cp-2", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc1))
		require.NoError(t, s.RemoteCallings.Create(ctx, rc2))
		require.NoError(t, s.RemoteCallings.Create(ctx, rc3))

		require.NoError(t, s.RemoteCallings.DeleteByConversation(ctx, "conv-1"))

		// Remote callings for conv-1 should be gone.
		got1, err := s.RemoteCallings.GetPendingByConversation(ctx, "conv-1")
		require.NoError(t, err)
		assert.Empty(t, got1, "remote callings for conv-1 should be deleted")

		// Remote callings for conv-2 should remain.
		got2, err := s.RemoteCallings.GetPendingByConversation(ctx, "conv-2")
		require.NoError(t, err)
		require.Len(t, got2, 1, "remote callings for conv-2 should remain")
		assert.Equal(t, "rc-3", got2[0].ID)
	})
}

// TestRemoteCallingStore_CountPendingByCheckpoint verifies that CountPendingByCheckpoint
// returns the correct count of pending remote callings.
func TestRemoteCallingStore_CountPendingByCheckpoint(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")

		rc1 := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc1))

		rc2 := newTestRemoteCalling("rc-2", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc2))

		// Resolved one
		rc3 := newTestRemoteCalling("rc-3", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc3))
		require.NoError(t, s.RemoteCallings.ResolveResult(ctx, "rc-3", "done"))

		count, err := s.RemoteCallings.CountPendingByCheckpoint(ctx, "cp-1")
		require.NoError(t, err)
		assert.Equal(t, int64(2), count, "should count 2 pending remote callings")
	})
}

// TestRemoteCallingStore_ListExpired verifies that ListExpired returns
// only pending remote callings that have passed their expiration time.
func TestRemoteCallingStore_ListExpired(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")

		// Expired one
		expiredAt := time.Now().Add(-1 * time.Hour)
		rc1 := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
		rc1.ExpiresAt = &expiredAt
		require.NoError(t, s.RemoteCallings.Create(ctx, rc1))

		// Not expired
		futureAt := time.Now().Add(1 * time.Hour)
		rc2 := newTestRemoteCalling("rc-2", "conv-1", "cp-1", "agent/bot", "ask_user")
		rc2.ExpiresAt = &futureAt
		require.NoError(t, s.RemoteCallings.Create(ctx, rc2))

		// No expiration
		rc3 := newTestRemoteCalling("rc-3", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc3))

		got, err := s.RemoteCallings.ListExpired(ctx, 100, time.Now())
		require.NoError(t, err)
		require.Len(t, got, 1, "should return only 1 expired remote calling")
		assert.Equal(t, "rc-1", got[0].ID)
	})
}

// TestRemoteCallingStore_MarkExpired verifies that MarkExpired
// marks a pending remote calling as expired.
func TestRemoteCallingStore_MarkExpired(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")

		rc := newTestRemoteCalling("rc-1", "conv-1", "cp-1", "agent/bot", "ask_user")
		require.NoError(t, s.RemoteCallings.Create(ctx, rc))

		require.NoError(t, s.RemoteCallings.MarkExpired(ctx, "rc-1"))

		got, err := s.RemoteCallings.GetByID(ctx, "rc-1")
		require.NoError(t, err)
		assert.Equal(t, model.RemoteCallingStatusExpired, got.Status, "status should be expired")
	})
}
