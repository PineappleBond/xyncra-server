package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// QuestionStore provides client-side Question persistence (D-125).
type QuestionStore struct {
	db *gorm.DB
}

// Upsert creates or updates a question (idempotent by ID).
func (qs *QuestionStore) Upsert(ctx context.Context, q *model.Question) error {
	if err := qs.db.WithContext(ctx).Save(q).Error; err != nil {
		return classifyError(fmt.Errorf("store: upsert question: %w", err))
	}
	return nil
}

// GetByConversation returns all questions for a conversation, ordered by creation time.
func (qs *QuestionStore) GetByConversation(ctx context.Context, convID string) ([]*model.Question, error) {
	var questions []*model.Question
	if err := qs.db.WithContext(ctx).
		Where("conversation_id = ?", convID).
		Order("created_at ASC").
		Find(&questions).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get questions by conversation: %w", err))
	}
	return questions, nil
}

// DeleteByConversation removes all questions for a conversation.
func (qs *QuestionStore) DeleteByConversation(ctx context.Context, convID string) error {
	if err := qs.db.WithContext(ctx).
		Where("conversation_id = ?", convID).
		Delete(&model.Question{}).Error; err != nil {
		return classifyError(fmt.Errorf("store: delete questions by conversation: %w", err))
	}
	return nil
}

// DeleteByConversationTx removes all questions for a conversation within the
// given transaction.
func (qs *QuestionStore) DeleteByConversationTx(ctx context.Context, tx *gorm.DB, convID string) error {
	if err := tx.WithContext(ctx).
		Where("conversation_id = ?", convID).
		Delete(&model.Question{}).Error; err != nil {
		return classifyError(fmt.Errorf("store: delete questions by conversation: %w", err))
	}
	return nil
}
