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
// QuestionStore tests (D-125)
// ---------------------------------------------------------------------------

// TestQuestionStore_Upsert verifies that Upsert creates a new question and
// updates an existing one (idempotent by ID).
func TestQuestionStore_Upsert(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convID := uid()

	// Create a new question via Upsert.
	q := &model.Question{
		ID:             uid(),
		ConversationID: convID,
		CheckpointID:   "cp-1",
		InterruptID:    "int-1",
		QuestionText:   "What color?",
		Status:         "pending",
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, db.Questions.Upsert(ctx, q))

	// Verify the question was persisted.
	got, err := db.Questions.GetByConversation(ctx, convID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, q.ID, got[0].ID)
	assert.Equal(t, "What color?", got[0].QuestionText)
	assert.Equal(t, "pending", got[0].Status)

	// Update the same question via Upsert (change question text).
	q.QuestionText = "What size?"
	q.Status = "answered"
	require.NoError(t, db.Questions.Upsert(ctx, q))

	// Verify the update.
	got2, err := db.Questions.GetByConversation(ctx, convID)
	require.NoError(t, err)
	require.Len(t, got2, 1)
	assert.Equal(t, "What size?", got2[0].QuestionText)
	assert.Equal(t, "answered", got2[0].Status)
}

// TestQuestionStore_GetByConversation verifies that questions are returned
// for the correct conversation, ordered by creation time.
func TestQuestionStore_GetByConversation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convA := uid()
	convB := uid()
	baseTime := time.Now().UTC().Truncate(time.Second)

	// Insert questions for two different conversations.
	questions := []*model.Question{
		{ID: uid(), ConversationID: convA, CheckpointID: "cp-1", InterruptID: "int-1", QuestionText: "Q1", Status: "pending", CreatedAt: baseTime},
		{ID: uid(), ConversationID: convA, CheckpointID: "cp-2", InterruptID: "int-2", QuestionText: "Q2", Status: "pending", CreatedAt: baseTime.Add(time.Second)},
		{ID: uid(), ConversationID: convB, CheckpointID: "cp-3", InterruptID: "int-3", QuestionText: "Q3", Status: "pending", CreatedAt: baseTime},
	}
	for _, q := range questions {
		require.NoError(t, db.Questions.Upsert(ctx, q))
	}

	// Query convA: should return 2 questions.
	gotA, err := db.Questions.GetByConversation(ctx, convA)
	require.NoError(t, err)
	require.Len(t, gotA, 2)
	assert.Equal(t, "Q1", gotA[0].QuestionText, "should be ordered by created_at ASC")
	assert.Equal(t, "Q2", gotA[1].QuestionText)

	// Query convB: should return 1 question.
	gotB, err := db.Questions.GetByConversation(ctx, convB)
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	assert.Equal(t, "Q3", gotB[0].QuestionText)

	// Query non-existent conversation: should return empty slice.
	gotC, err := db.Questions.GetByConversation(ctx, "non-existent")
	require.NoError(t, err)
	assert.Empty(t, gotC)
}

// TestQuestionStore_DeleteByConversation verifies that all questions for a
// conversation are deleted.
func TestQuestionStore_DeleteByConversation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	convA := uid()
	convB := uid()

	// Insert questions for two conversations.
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Questions.Upsert(ctx, &model.Question{
			ID:             uid(),
			ConversationID: convA,
			CheckpointID:   "cp-a",
			QuestionText:   "Q for A",
			Status:         "pending",
		}))
	}
	require.NoError(t, db.Questions.Upsert(ctx, &model.Question{
		ID:             uid(),
		ConversationID: convB,
		CheckpointID:   "cp-b",
		QuestionText:   "Q for B",
		Status:         "pending",
	}))

	// Delete questions for convA.
	require.NoError(t, db.Questions.DeleteByConversation(ctx, convA))

	// convA should have no questions.
	gotA, err := db.Questions.GetByConversation(ctx, convA)
	require.NoError(t, err)
	assert.Empty(t, gotA, "all questions for convA should be deleted")

	// convB should still have its question.
	gotB, err := db.Questions.GetByConversation(ctx, convB)
	require.NoError(t, err)
	require.Len(t, gotB, 1, "convB questions should not be affected")

	// Deleting again should be a no-op (no error).
	require.NoError(t, db.Questions.DeleteByConversation(ctx, convA))
}
