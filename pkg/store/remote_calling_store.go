package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// RemoteCallingStore handles RemoteCalling persistence for the client (D-137).
type RemoteCallingStore struct {
	db *gorm.DB
}

// NewRemoteCallingStore creates a new RemoteCallingStore.
func NewRemoteCallingStore(db *gorm.DB) *RemoteCallingStore {
	return &RemoteCallingStore{db: db}
}

// Upsert creates or updates a RemoteCalling (idempotent by ID).
func (s *RemoteCallingStore) Upsert(ctx context.Context, rc *model.RemoteCalling) error {
	if err := s.db.WithContext(ctx).Save(rc).Error; err != nil {
		return fmt.Errorf("store: upsert remote calling: %w", err)
	}
	return nil
}

// GetByConversation returns all RemoteCallings for a conversation.
func (s *RemoteCallingStore) GetByConversation(ctx context.Context, conversationID string) ([]*model.RemoteCalling, error) {
	var rcs []*model.RemoteCalling
	if err := s.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at ASC").
		Find(&rcs).Error; err != nil {
		return nil, fmt.Errorf("store: get remote callings by conversation: %w", err)
	}
	return rcs, nil
}

// GetPendingByConversation returns all pending RemoteCallings for a conversation.
func (s *RemoteCallingStore) GetPendingByConversation(ctx context.Context, conversationID string) ([]*model.RemoteCalling, error) {
	var rcs []*model.RemoteCalling
	if err := s.db.WithContext(ctx).
		Where("conversation_id = ? AND status = ?", conversationID, model.RemoteCallingStatusPending).
		Order("created_at ASC").
		Find(&rcs).Error; err != nil {
		return nil, fmt.Errorf("store: get pending remote callings by conversation: %w", err)
	}
	return rcs, nil
}

// DeleteByConversation deletes all RemoteCallings for a conversation.
func (s *RemoteCallingStore) DeleteByConversation(ctx context.Context, conversationID string) error {
	if err := s.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Delete(&model.RemoteCalling{}).Error; err != nil {
		return fmt.Errorf("store: delete remote callings by conversation: %w", err)
	}
	return nil
}

// DeleteByConversationTx deletes all RemoteCallings for a conversation within a transaction.
func (s *RemoteCallingStore) DeleteByConversationTx(tx *gorm.DB, conversationID string) error {
	if err := tx.
		Where("conversation_id = ?", conversationID).
		Delete(&model.RemoteCalling{}).Error; err != nil {
		return fmt.Errorf("store: delete remote callings by conversation (tx): %w", err)
	}
	return nil
}
