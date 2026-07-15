package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// QuestionStore handles Question persistence operations
type QuestionStore struct {
	db *gorm.DB
}

// NewQuestionStore creates a new QuestionStore
func NewQuestionStore(db *gorm.DB) *QuestionStore {
	return &QuestionStore{db: db}
}

// Create persists a new Question.
func (qs *QuestionStore) Create(ctx context.Context, q *model.Question) error {
	if err := qs.db.WithContext(ctx).Create(q).Error; err != nil {
		return classifyError(fmt.Errorf("store: create question: %w", err))
	}
	return nil
}

// GetByConversation returns all questions for a conversation.
func (qs *QuestionStore) GetByConversation(ctx context.Context, conversationID string) ([]*model.Question, error) {
	var questions []*model.Question
	if err := qs.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at ASC").
		Find(&questions).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get questions by conversation: %w", err))
	}
	return questions, nil
}

// GetPendingByCheckpoint returns pending questions for a checkpoint.
func (qs *QuestionStore) GetPendingByCheckpoint(ctx context.Context, checkpointID string) ([]*model.Question, error) {
	var questions []*model.Question
	if err := qs.db.WithContext(ctx).
		Where("checkpoint_id = ? AND status = ?", checkpointID, model.QuestionStatusPending).
		Order("created_at ASC").
		Find(&questions).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get pending questions by checkpoint: %w", err))
	}
	return questions, nil
}

// GetByCheckpoint returns all questions for a checkpoint (both pending and answered).
func (qs *QuestionStore) GetByCheckpoint(ctx context.Context, checkpointID string) ([]*model.Question, error) {
	var questions []*model.Question
	if err := qs.db.WithContext(ctx).
		Where("checkpoint_id = ?", checkpointID).
		Order("created_at ASC").
		Find(&questions).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get questions by checkpoint: %w", err))
	}
	return questions, nil
}

// UpdateAnswer updates a question's answer and status.
// Returns ErrNotFound if question doesn't exist.
// Returns ErrConflict if question is already answered (idempotency check).
func (qs *QuestionStore) UpdateAnswer(ctx context.Context, questionID, answer, answeredBy, answeredDeviceID string) error {
	now := time.Now()
	result := qs.db.WithContext(ctx).
		Model(&model.Question{}).
		Where("id = ? AND status = ?", questionID, model.QuestionStatusPending).
		Updates(map[string]interface{}{
			"status":             model.QuestionStatusAnswered,
			"answer":             answer,
			"answered_by":        answeredBy,
			"answered_device_id": answeredDeviceID,
			"answered_at":        now,
		})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: update question answer: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		// Either the question doesn't exist, or it's already answered.
		var existing model.Question
		if err := qs.db.WithContext(ctx).
			Where("id = ?", questionID).
			First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return classifyError(fmt.Errorf("store: get question for answer check: %w", err))
		}
		// Question exists but is already answered — conflict (idempotency).
		return ErrConflict
	}
	return nil
}

// DeleteByConversation deletes all questions for a conversation.
func (qs *QuestionStore) DeleteByConversation(ctx context.Context, conversationID string) error {
	if err := qs.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Delete(&model.Question{}).Error; err != nil {
		return classifyError(fmt.Errorf("store: delete questions by conversation: %w", err))
	}
	return nil
}
