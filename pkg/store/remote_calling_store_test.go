package store

import (
	"context"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// RemoteCallingStore tests (D-137)
// ---------------------------------------------------------------------------

// TestRemoteCallingStore_Upsert verifies that Upsert creates a new remote calling and
// updates an existing one (idempotent by ID).
func TestRemoteCallingStore_Upsert(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()

	// Create a new remote calling via Upsert.
	rc := &model.RemoteCalling{
		ID:             uid(),
		ConversationID: convID,
		CheckpointID:   "cp-1",
		AgentID:        "agent/bot",
		Method:         "ask_user",
		Status:         "pending",
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, db.RemoteCallings.Upsert(ctx, rc))

	// Verify the remote calling was persisted.
	got, err := db.RemoteCallings.GetByConversation(ctx, convID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, rc.ID, got[0].ID)
	assert.Equal(t, "ask_user", got[0].Method)
	assert.Equal(t, "pending", got[0].Status)

	// Update the same remote calling via Upsert (change status).
	rc.Status = "resolved"
	rc.Result = "done"
	require.NoError(t, db.RemoteCallings.Upsert(ctx, rc))

	// Verify the update.
	got2, err := db.RemoteCallings.GetByConversation(ctx, convID)
	require.NoError(t, err)
	require.Len(t, got2, 1)
	assert.Equal(t, "resolved", got2[0].Status)
	assert.Equal(t, "done", got2[0].Result)
}

// TestRemoteCallingStore_GetByConversation verifies that remote callings are returned
// for the correct conversation, ordered by creation time.
func TestRemoteCallingStore_GetByConversation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convA := uid()
	convB := uid()
	baseTime := time.Now().UTC().Truncate(time.Second)

	// Insert remote callings for two different conversations.
	rcs := []*model.RemoteCalling{
		{ID: uid(), ConversationID: convA, CheckpointID: "cp-1", AgentID: "agent/bot", Method: "ask_user", Status: "pending", CreatedAt: baseTime},
		{ID: uid(), ConversationID: convA, CheckpointID: "cp-2", AgentID: "agent/bot", Method: "ask_user", Status: "pending", CreatedAt: baseTime.Add(time.Second)},
		{ID: uid(), ConversationID: convB, CheckpointID: "cp-3", AgentID: "agent/bot", Method: "ask_user", Status: "pending", CreatedAt: baseTime},
	}
	for _, rc := range rcs {
		require.NoError(t, db.RemoteCallings.Upsert(ctx, rc))
	}

	// Query convA: should return 2 remote callings.
	gotA, err := db.RemoteCallings.GetByConversation(ctx, convA)
	require.NoError(t, err)
	require.Len(t, gotA, 2)
	assert.Equal(t, "cp-1", gotA[0].CheckpointID, "should be ordered by created_at ASC")
	assert.Equal(t, "cp-2", gotA[1].CheckpointID)

	// Query convB: should return 1 remote calling.
	gotB, err := db.RemoteCallings.GetByConversation(ctx, convB)
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	assert.Equal(t, "cp-3", gotB[0].CheckpointID)

	// Query non-existent conversation: should return empty slice.
	gotC, err := db.RemoteCallings.GetByConversation(ctx, "non-existent")
	require.NoError(t, err)
	assert.Empty(t, gotC)
}

// TestRemoteCallingStore_GetPendingByConversation verifies that only pending remote callings
// are returned for the correct conversation.
func TestRemoteCallingStore_GetPendingByConversation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	baseTime := time.Now().UTC().Truncate(time.Second)

	// Insert pending and resolved remote callings.
	require.NoError(t, db.RemoteCallings.Upsert(ctx, &model.RemoteCalling{
		ID: uid(), ConversationID: convID, CheckpointID: "cp-1",
		AgentID: "agent/bot", Method: "ask_user", Status: "pending", CreatedAt: baseTime,
	}))
	require.NoError(t, db.RemoteCallings.Upsert(ctx, &model.RemoteCalling{
		ID: uid(), ConversationID: convID, CheckpointID: "cp-2",
		AgentID: "agent/bot", Method: "ask_user", Status: "resolved", CreatedAt: baseTime.Add(time.Second),
	}))

	got, err := db.RemoteCallings.GetPendingByConversation(ctx, convID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "pending", got[0].Status)
}

// TestRemoteCallingStore_DeleteByConversation verifies that all remote callings for a
// conversation are deleted.
func TestRemoteCallingStore_DeleteByConversation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convA := uid()
	convB := uid()

	// Insert remote callings for two conversations.
	for i := 0; i < 3; i++ {
		require.NoError(t, db.RemoteCallings.Upsert(ctx, &model.RemoteCalling{
			ID: uid(), ConversationID: convA, CheckpointID: "cp-a",
			AgentID: "agent/bot", Method: "ask_user", Status: "pending",
		}))
	}
	require.NoError(t, db.RemoteCallings.Upsert(ctx, &model.RemoteCalling{
		ID: uid(), ConversationID: convB, CheckpointID: "cp-b",
		AgentID: "agent/bot", Method: "ask_user", Status: "pending",
	}))

	// Delete remote callings for convA.
	require.NoError(t, db.RemoteCallings.DeleteByConversation(ctx, convA))

	// convA should have no remote callings.
	gotA, err := db.RemoteCallings.GetByConversation(ctx, convA)
	require.NoError(t, err)
	assert.Empty(t, gotA, "all remote callings for convA should be deleted")

	// convB should still have its remote calling.
	gotB, err := db.RemoteCallings.GetByConversation(ctx, convB)
	require.NoError(t, err)
	require.Len(t, gotB, 1, "convB remote callings should not be affected")

	// Deleting again should be a no-op (no error).
	require.NoError(t, db.RemoteCallings.DeleteByConversation(ctx, convA))
}
