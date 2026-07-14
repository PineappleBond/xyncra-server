package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// MessageStore provides data access operations for the Message model.
type MessageStore struct {
	db *gorm.DB
}

// Create inserts a new message record into the database.
func (ms *MessageStore) Create(ctx context.Context, msg *model.Message) error {
	if err := ms.db.WithContext(ctx).Create(msg).Error; err != nil {
		return classifyError(fmt.Errorf("store: create message: %w", err))
	}
	return nil
}

// Get retrieves a message by its primary key. Returns ErrNotFound if no record
// exists.
func (ms *MessageStore) Get(ctx context.Context, id string) (*model.Message, error) {
	var msg model.Message
	err := ms.db.WithContext(ctx).
		Where("id = ?", id).
		First(&msg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get message: %w", err))
	}
	return &msg, nil
}

// GetByClientMessageID retrieves a message by its client-generated unique ID
// and sender ID (composite uniqueness). Returns ErrNotFound if no matching record exists.
func (ms *MessageStore) GetByClientMessageID(ctx context.Context, clientMessageID, senderID string) (*model.Message, error) {
	var msg model.Message
	err := ms.db.WithContext(ctx).
		Where("client_message_id = ? AND sender_id = ?", clientMessageID, senderID).
		First(&msg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get message by client_message_id: %w", err))
	}
	return &msg, nil
}

// ListByConversation returns messages for the given conversation with
// MessageID greater than afterMessageID, ordered by MessageID ascending.
func (ms *MessageStore) ListByConversation(ctx context.Context, convID string, afterMessageID uint32, limit int) ([]*model.Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var msgs []*model.Message
	err := ms.db.WithContext(ctx).
		Where("conversation_id = ? AND message_id > ?", convID, afterMessageID).
		Order("message_id ASC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list messages by conversation: %w", err))
	}
	return msgs, nil
}

// SearchByConversation returns messages for the given conversation that contain
// the specified content substring (case-insensitive via LIKE), ordered by
// MessageID descending (newest first).
func (ms *MessageStore) SearchByConversation(ctx context.Context, convID, content string, afterMessageID uint32, limit int) ([]*model.Message, error) {
	if limit <= 0 || limit > 201 {
		limit = 50
	}
	if content == "" {
		return []*model.Message{}, nil
	}

	like := "%" + escapeLikePattern(content) + "%"

	query := ms.db.WithContext(ctx).
		Where("conversation_id = ? AND content LIKE ? ESCAPE '|'", convID, like)

	if afterMessageID > 0 {
		query = query.Where("message_id < ?", afterMessageID)
	}

	var msgs []*model.Message
	err := query.
		Order("message_id DESC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: search messages by content: %w", err))
	}
	return msgs, nil
}

// ListByTimeRange returns messages for the given conversation within the
// specified time range (inclusive), ordered by MessageID ascending.
func (ms *MessageStore) ListByTimeRange(ctx context.Context, convID string, startTime, endTime time.Time, limit int) ([]*model.Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var msgs []*model.Message
	err := ms.db.WithContext(ctx).
		Where("conversation_id = ? AND created_at >= ? AND created_at <= ?", convID, startTime, endTime).
		Order("message_id ASC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list messages by time range: %w", err))
	}
	return msgs, nil
}

// Delete performs a soft delete on the message identified by id.
func (ms *MessageStore) Delete(ctx context.Context, id string) error {
	result := ms.db.WithContext(ctx).Delete(&model.Message{}, "id = ?", id)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete message: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Restore undeletes a soft-deleted message identified by id.
func (ms *MessageStore) Restore(ctx context.Context, id string) error {
	result := ms.db.WithContext(ctx).
		Unscoped().
		Model(&model.Message{}).
		Where("id = ? AND deleted_at IS NOT NULL", id).
		Update("deleted_at", nil)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: restore message: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByConversation performs a soft delete on all messages belonging to the
// given conversation.
func (ms *MessageStore) DeleteByConversation(ctx context.Context, convID string) error {
	result := ms.db.WithContext(ctx).
		Where("conversation_id = ?", convID).
		Delete(&model.Message{})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete messages by conversation: %w", result.Error))
	}
	return nil
}

// RestoreByConversation restores all soft-deleted messages belonging to the
// given conversation. Returns the number of restored rows.
func (ms *MessageStore) RestoreByConversation(ctx context.Context, convID string) (int64, error) {
	result := ms.db.WithContext(ctx).
		Unscoped().
		Model(&model.Message{}).
		Where("conversation_id = ? AND deleted_at IS NOT NULL", convID).
		Update("deleted_at", nil)
	if result.Error != nil {
		return 0, classifyError(fmt.Errorf("store: restore messages by conversation: %w", result.Error))
	}
	return result.RowsAffected, nil
}

// ListRecentByConversation returns the most recent messages for a conversation,
// ordered by MessageID descending (newest first), limited to at most limit rows.
// Soft-deleted messages are excluded automatically by GORM's soft-delete plugin.
// This is used by the Agent context manager to load conversation history.
func (ms *MessageStore) ListRecentByConversation(ctx context.Context, convID string, limit int) ([]*model.Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	var msgs []*model.Message
	err := ms.db.WithContext(ctx).
		Where("conversation_id = ?", convID).
		Order("message_id DESC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list recent messages by conversation: %w", err))
	}
	return msgs, nil
}

// CountUnread returns the number of messages in the given conversation with
// MessageID greater than afterMessageID. Soft-deleted messages are excluded
// automatically by GORM's soft-delete plugin.
func (ms *MessageStore) CountUnread(ctx context.Context, convID string, afterMessageID uint32) (int64, error) {
	var count int64
	err := ms.db.WithContext(ctx).
		Model(&model.Message{}).
		Where("conversation_id = ? AND message_id > ?", convID, afterMessageID).
		Count(&count).Error
	if err != nil {
		return 0, classifyError(fmt.Errorf("store: count unread messages: %w", err))
	}
	if count < 0 {
		count = 0
	}
	return count, nil
}

// CreateTx inserts a message within the given transaction.
func (ms *MessageStore) CreateTx(ctx context.Context, tx *gorm.DB, msg *model.Message) error {
	if err := tx.WithContext(ctx).Create(msg).Error; err != nil {
		return classifyError(fmt.Errorf("store: create message tx: %w", err))
	}
	return nil
}

// SoftDeleteTx performs a soft delete within the given transaction.
func (ms *MessageStore) SoftDeleteTx(ctx context.Context, tx *gorm.DB, id string) error {
	result := tx.WithContext(ctx).Delete(&model.Message{}, "id = ?", id)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: soft delete message tx: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
