package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// DraftStore provides data access operations for message drafts.
// Each conversation can have at most one draft (one-draft-per-conversation).
type DraftStore struct {
	db *gorm.DB
}

// Save performs an UPSERT for a draft. If a draft for the conversation already
// exists (by ConversationID uniqueIndex), it is updated; otherwise a new
// record is inserted.
func (ds *DraftStore) Save(ctx context.Context, draft *model.Draft) error {
	err := ds.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "conversation_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"content", "updated_at"}),
		}).
		Create(draft).Error
	if err != nil {
		return classifyError(fmt.Errorf("store: save draft: %w", err))
	}
	return nil
}

// GetByConversation retrieves the draft for the given conversation.
// Returns ErrNotFound if no draft exists.
func (ds *DraftStore) GetByConversation(ctx context.Context, conversationID string) (*model.Draft, error) {
	var draft model.Draft
	err := ds.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		First(&draft).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get draft: %w", err))
	}
	return &draft, nil
}

// Delete removes a draft by its primary key. Returns ErrNotFound if not found.
func (ds *DraftStore) Delete(ctx context.Context, id string) error {
	result := ds.db.WithContext(ctx).Delete(&model.Draft{}, "id = ?", id)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete draft: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByConversation removes the draft for the given conversation.
// Returns ErrNotFound if no draft exists.
func (ds *DraftStore) DeleteByConversation(ctx context.Context, conversationID string) error {
	result := ds.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Delete(&model.Draft{})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete draft by conversation: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns all drafts ordered by UpdatedAt descending.
func (ds *DraftStore) List(ctx context.Context) ([]*model.Draft, error) {
	var drafts []*model.Draft
	err := ds.db.WithContext(ctx).
		Order("updated_at DESC").
		Find(&drafts).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list drafts: %w", err))
	}
	return drafts, nil
}
