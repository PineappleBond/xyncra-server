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

// newTestQuestion creates a question linked to the given conversation.
func newTestQuestion(id, conversationID, checkpointID, interruptID, text string) *model.Question {
	return &model.Question{
		ID:             id,
		ConversationID: conversationID,
		CheckpointID:   checkpointID,
		InterruptID:    interruptID,
		QuestionText:   text,
		Status:         model.QuestionStatusPending,
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

// TestQuestionStore_Create_HappyPath verifies that Create persists a question
// and that the DB record matches the input.
func TestQuestionStore_Create_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")
		q := newTestQuestion("q-1", "conv-1", "cp-1", "int-1", "What color do you prefer?")

		require.NoError(t, s.Questions.Create(ctx, q))

		got, err := s.Questions.GetByConversation(ctx, "conv-1")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "q-1", got[0].ID)
		assert.Equal(t, "conv-1", got[0].ConversationID)
		assert.Equal(t, "cp-1", got[0].CheckpointID)
		assert.Equal(t, "int-1", got[0].InterruptID)
		assert.Equal(t, "What color do you prefer?", got[0].QuestionText)
		assert.Equal(t, model.QuestionStatusPending, got[0].Status)
	})
}

// TestQuestionStore_GetByConversation verifies that GetByConversation returns
// all questions for a conversation, ordered by created_at ASC.
func TestQuestionStore_GetByConversation(t *testing.T) {
	t.Run("returns multiple questions ordered by created_at ASC", func(t *testing.T) {
		runOnAllDatabases(t, func(t *testing.T, s *Store) {
			ctx := context.Background()
			cleanAll(t, s, ctx)

			createTestConversation(t, s, ctx, "conv-1")

			q1 := newTestQuestion("q-1", "conv-1", "cp-1", "int-1", "First question")
			q1.CreatedAt = testNow
			require.NoError(t, s.Questions.Create(ctx, q1))

			q2 := newTestQuestion("q-2", "conv-1", "cp-1", "int-2", "Second question")
			q2.CreatedAt = testNow.Add(time.Second)
			require.NoError(t, s.Questions.Create(ctx, q2))

			q3 := newTestQuestion("q-3", "conv-1", "cp-2", "int-3", "Third question")
			q3.CreatedAt = testNow.Add(2 * time.Second)
			require.NoError(t, s.Questions.Create(ctx, q3))

			got, err := s.Questions.GetByConversation(ctx, "conv-1")
			require.NoError(t, err)
			require.Len(t, got, 3)
			assert.Equal(t, "q-1", got[0].ID, "first question should be q-1")
			assert.Equal(t, "q-2", got[1].ID, "second question should be q-2")
			assert.Equal(t, "q-3", got[2].ID, "third question should be q-3")
		})
	})

	t.Run("empty conversation returns empty slice", func(t *testing.T) {
		runOnAllDatabases(t, func(t *testing.T, s *Store) {
			ctx := context.Background()
			cleanAll(t, s, ctx)

			got, err := s.Questions.GetByConversation(ctx, "non-existent-conv")
			require.NoError(t, err)
			assert.Empty(t, got, "should return empty slice for non-existent conversation")
		})
	})
}

// TestQuestionStore_GetPendingByCheckpoint verifies that GetPendingByCheckpoint
// returns only questions with status "pending" for the given checkpoint.
func TestQuestionStore_GetPendingByCheckpoint(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")

		// Create a pending question.
		qPending := newTestQuestion("q-pending", "conv-1", "cp-1", "int-1", "Pending question")
		require.NoError(t, s.Questions.Create(ctx, qPending))

		// Create and answer another question.
		qAnswered := newTestQuestion("q-answered", "conv-1", "cp-1", "int-2", "Already answered")
		require.NoError(t, s.Questions.Create(ctx, qAnswered))
		require.NoError(t, s.Questions.UpdateAnswer(ctx, "q-answered", "Blue", "user-1", "device-1"))

		// Create a pending question for a different checkpoint.
		qOther := newTestQuestion("q-other", "conv-1", "cp-2", "int-3", "Other checkpoint")
		require.NoError(t, s.Questions.Create(ctx, qOther))

		got, err := s.Questions.GetPendingByCheckpoint(ctx, "cp-1")
		require.NoError(t, err)
		require.Len(t, got, 1, "should return only 1 pending question for cp-1")
		assert.Equal(t, "q-pending", got[0].ID)
		assert.Equal(t, model.QuestionStatusPending, got[0].Status)
	})
}

// TestQuestionStore_GetByCheckpoint verifies that GetByCheckpoint returns all
// questions (both pending and answered) for a given checkpoint.
func TestQuestionStore_GetByCheckpoint(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")

		qPending := newTestQuestion("q-pending", "conv-1", "cp-1", "int-1", "Pending question")
		require.NoError(t, s.Questions.Create(ctx, qPending))

		qAnswered := newTestQuestion("q-answered", "conv-1", "cp-1", "int-2", "Answered question")
		require.NoError(t, s.Questions.Create(ctx, qAnswered))
		require.NoError(t, s.Questions.UpdateAnswer(ctx, "q-answered", "Red", "user-1", "device-1"))

		got, err := s.Questions.GetByCheckpoint(ctx, "cp-1")
		require.NoError(t, err)
		require.Len(t, got, 2, "should return both pending and answered questions")
	})
}

// TestQuestionStore_UpdateAnswer_HappyPath verifies that UpdateAnswer correctly
// updates a pending question and sets all answer-related fields.
func TestQuestionStore_UpdateAnswer_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")
		q := newTestQuestion("q-1", "conv-1", "cp-1", "int-1", "What is your name?")
		require.NoError(t, s.Questions.Create(ctx, q))

		require.NoError(t, s.Questions.UpdateAnswer(ctx, "q-1", "Alice", "user-1", "device-abc"))

		all, err := s.Questions.GetByConversation(ctx, "conv-1")
		require.NoError(t, err)
		require.Len(t, all, 1)
		got := all[0]

		assert.Equal(t, model.QuestionStatusAnswered, got.Status, "status should be answered")
		assert.Equal(t, "Alice", got.Answer, "answer should match")
		assert.Equal(t, "user-1", got.AnsweredBy, "answered_by should match")
		assert.Equal(t, "device-abc", got.AnsweredDeviceID, "answered_device_id should match")
		assert.NotNil(t, got.AnsweredAt, "answered_at should be set")
	})
}

// TestQuestionStore_UpdateAnswer_NotFound verifies that updating a non-existent
// question returns ErrNotFound.
func TestQuestionStore_UpdateAnswer_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		err := s.Questions.UpdateAnswer(ctx, "non-existent-id", "answer", "user-1", "device-1")
		require.Error(t, err, "expected error for non-existent question")
		assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got: %v", err)
	})
}

// TestQuestionStore_UpdateAnswer_AlreadyAnswered verifies that updating a
// question that has already been answered returns ErrConflict.
func TestQuestionStore_UpdateAnswer_AlreadyAnswered(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversation(t, s, ctx, "conv-1")
		q := newTestQuestion("q-1", "conv-1", "cp-1", "int-1", "What is your name?")
		require.NoError(t, s.Questions.Create(ctx, q))

		// First answer should succeed.
		require.NoError(t, s.Questions.UpdateAnswer(ctx, "q-1", "Alice", "user-1", "device-1"))

		// Second answer should return ErrConflict (idempotency check).
		err := s.Questions.UpdateAnswer(ctx, "q-1", "Bob", "user-2", "device-2")
		require.Error(t, err, "expected error for already answered question")
		assert.True(t, errors.Is(err, ErrConflict), "expected ErrConflict, got: %v", err)
	})
}

// TestQuestionStore_DeleteByConversation verifies that DeleteByConversation
// removes all questions associated with the given conversation.
func TestQuestionStore_DeleteByConversation(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		createTestConversationWithUsers(t, s, ctx, "conv-1", "user-1", "user-2")
		createTestConversationWithUsers(t, s, ctx, "conv-2", "user-3", "user-4")

		q1 := newTestQuestion("q-1", "conv-1", "cp-1", "int-1", "Q1")
		q2 := newTestQuestion("q-2", "conv-1", "cp-1", "int-2", "Q2")
		q3 := newTestQuestion("q-3", "conv-2", "cp-2", "int-3", "Q3")
		require.NoError(t, s.Questions.Create(ctx, q1))
		require.NoError(t, s.Questions.Create(ctx, q2))
		require.NoError(t, s.Questions.Create(ctx, q3))

		require.NoError(t, s.Questions.DeleteByConversation(ctx, "conv-1"))

		// Questions for conv-1 should be gone.
		got1, err := s.Questions.GetByConversation(ctx, "conv-1")
		require.NoError(t, err)
		assert.Empty(t, got1, "questions for conv-1 should be deleted")

		// Questions for conv-2 should remain.
		got2, err := s.Questions.GetByConversation(ctx, "conv-2")
		require.NoError(t, err)
		require.Len(t, got2, 1, "questions for conv-2 should remain")
		assert.Equal(t, "q-3", got2[0].ID)
	})
}
