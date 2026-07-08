package store

import (
	"context"
	"testing"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDraftStore_Save_NewDraft(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	draft := &model.Draft{
		ID:             uid(),
		ConversationID: convID,
		Content:        "Hello, this is a draft",
	}
	require.NoError(t, db.Drafts.Save(ctx, draft))

	got, err := db.Drafts.GetByConversation(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, draft.ID, got.ID)
	assert.Equal(t, "Hello, this is a draft", got.Content)
	assert.Equal(t, convID, got.ConversationID)
}

func TestDraftStore_Save_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	draft1 := &model.Draft{
		ID:             uid(),
		ConversationID: convID,
		Content:        "First draft",
	}
	require.NoError(t, db.Drafts.Save(ctx, draft1))

	got, err := db.Drafts.GetByConversation(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, "First draft", got.Content)
	firstID := got.ID

	// Save again with same ConversationID — should UPSERT (update content).
	draft2 := &model.Draft{
		ID:             uid(),
		ConversationID: convID,
		Content:        "Updated draft content",
	}
	require.NoError(t, db.Drafts.Save(ctx, draft2))

	got, err = db.Drafts.GetByConversation(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, "Updated draft content", got.Content)
	// The original record is updated (same ID), not a new one inserted.
	// Note: due to ON CONFLICT, the original row is updated in place.
	assert.Equal(t, firstID, got.ID)
}

func TestDraftStore_GetByConversation_Found(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	draft := &model.Draft{
		ID:             uid(),
		ConversationID: convID,
		Content:        "Some draft",
	}
	require.NoError(t, db.Drafts.Save(ctx, draft))

	got, err := db.Drafts.GetByConversation(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, draft.ID, got.ID)
	assert.Equal(t, "Some draft", got.Content)
}

func TestDraftStore_GetByConversation_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	_, err := db.Drafts.GetByConversation(ctx, "nonexistent-conv")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDraftStore_Delete(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	draft := &model.Draft{
		ID:             uid(),
		ConversationID: convID,
		Content:        "To delete",
	}
	require.NoError(t, db.Drafts.Save(ctx, draft))

	require.NoError(t, db.Drafts.Delete(ctx, draft.ID))

	_, err := db.Drafts.GetByConversation(ctx, convID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDraftStore_Delete_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	err := db.Drafts.Delete(ctx, "nonexistent-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDraftStore_DeleteByConversation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()
	draft := &model.Draft{
		ID:             uid(),
		ConversationID: convID,
		Content:        "Draft to delete by conv",
	}
	require.NoError(t, db.Drafts.Save(ctx, draft))

	require.NoError(t, db.Drafts.DeleteByConversation(ctx, convID))

	_, err := db.Drafts.GetByConversation(ctx, convID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDraftStore_DeleteByConversation_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	err := db.Drafts.DeleteByConversation(ctx, "nonexistent-conv")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDraftStore_List(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	// Create 3 drafts for different conversations.
	d1 := &model.Draft{ID: uid(), ConversationID: uid(), Content: "Draft 1"}
	d2 := &model.Draft{ID: uid(), ConversationID: uid(), Content: "Draft 2"}
	d3 := &model.Draft{ID: uid(), ConversationID: uid(), Content: "Draft 3"}

	require.NoError(t, db.Drafts.Save(ctx, d1))
	require.NoError(t, db.Drafts.Save(ctx, d2))
	require.NoError(t, db.Drafts.Save(ctx, d3))

	drafts, err := db.Drafts.List(ctx)
	require.NoError(t, err)
	require.Len(t, drafts, 3)
	// Ordered by UpdatedAt DESC. Since d3 was saved last, it should be first.
	assert.Equal(t, d3.ID, drafts[0].ID)
	assert.Equal(t, d2.ID, drafts[1].ID)
	assert.Equal(t, d1.ID, drafts[2].ID)
}

func TestDraftStore_List_Empty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	drafts, err := db.Drafts.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, drafts)
}
