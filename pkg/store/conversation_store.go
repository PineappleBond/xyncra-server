package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ConversationStore provides data access operations for the Conversation model.
type ConversationStore struct {
	db *gorm.DB
}

// Create inserts a new conversation record into the database.
func (cs *ConversationStore) Create(ctx context.Context, conv *model.Conversation) error {
	if err := cs.db.WithContext(ctx).Create(conv).Error; err != nil {
		return classifyError(fmt.Errorf("store: create conversation: %w", err))
	}
	return nil
}

// Get retrieves a conversation by its primary key. Returns ErrNotFound if no
// record exists.
func (cs *ConversationStore) Get(ctx context.Context, id string) (*model.Conversation, error) {
	var conv model.Conversation
	err := cs.db.WithContext(ctx).
		Where("id = ?", id).
		First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get conversation: %w", err))
	}
	return &conv, nil
}

// GetByUsers returns the 1-on-1 conversation between user1 and user2.
// It checks both (user1, user2) and (user2, user1) orderings.
// Returns ErrNotFound if no matching conversation exists.
func (cs *ConversationStore) GetByUsers(ctx context.Context, user1, user2 string) (*model.Conversation, error) {
	var conv model.Conversation
	err := cs.db.WithContext(ctx).
		Where("(user_id1 = ? AND user_id2 = ?) OR (user_id1 = ? AND user_id2 = ?)", user1, user2, user2, user1).
		First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get conversation by users: %w", err))
	}
	return &conv, nil
}

// GetByUser returns conversations where the given user is either UserID1 or
// UserID2, ordered by LastMessageAt descending, with offset/limit pagination.
// Soft-deleted records are excluded automatically by GORM's soft-delete plugin.
func (cs *ConversationStore) GetByUser(ctx context.Context, userID string, offset, limit int) ([]*model.Conversation, error) {
	if limit <= 0 || limit > 101 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var convs []*model.Conversation
	if err := cs.db.WithContext(ctx).
		Where("(user_id1 = ? OR user_id2 = ?) AND user_id2 != ''", userID, userID).
		Order("last_message_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&convs).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get conversations by user: %w", err))
	}
	return convs, nil
}

// Update saves all fields of the conversation back to the database.
// It uses Unscoped() so that soft-deleted records can be updated (including
// clearing deleted_at to NULL when restoring a conversation).
func (cs *ConversationStore) Update(ctx context.Context, conv *model.Conversation) error {
	if err := cs.db.WithContext(ctx).Unscoped().Save(conv).Error; err != nil {
		return classifyError(err)
	}
	return nil
}

// Upsert creates the conversation if it does not exist, or saves (overwrites)
// it if it already exists. This is used by the client sync pipeline to apply
// conversation create events idempotently (D-045).
// It uses Unscoped() to also find soft-deleted records, so that restoring a
// previously deleted conversation correctly transitions it back to active.
// If a concurrent insert causes a duplicate key error, the operation retries
// as an update to handle the TOCTOU race between SELECT and INSERT.
func (cs *ConversationStore) Upsert(ctx context.Context, conv *model.Conversation) error {
	var existing model.Conversation
	err := cs.db.WithContext(ctx).Unscoped().Where("id = ?", conv.ID).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if createErr := cs.db.WithContext(ctx).Create(conv).Error; createErr != nil {
				// TOCTOU: another goroutine may have inserted between SELECT and INSERT.
				if errors.Is(classifyError(createErr), ErrDuplicateKey) {
					return cs.Update(ctx, conv)
				}
				return classifyError(fmt.Errorf("store: upsert conversation: %w", createErr))
			}
			return nil
		}
		return classifyError(fmt.Errorf("store: upsert conversation: %w", err))
	}
	return cs.Update(ctx, conv)
}

// Delete performs a cascading soft delete: the conversation and all its messages
// are soft-deleted within a single transaction (D-013).
func (cs *ConversationStore) Delete(ctx context.Context, id string) error {
	return cs.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&model.Conversation{}, "id = ?", id)
		if result.Error != nil {
			return classifyError(fmt.Errorf("store: delete conversation: %w", result.Error))
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}

		// Cascade soft-delete all messages in this conversation (D-013).
		if err := tx.Where("conversation_id = ?", id).Delete(&model.Message{}).Error; err != nil {
			return classifyError(fmt.Errorf("store: cascade delete messages: %w", err))
		}
		return nil
	})
}

// Restore undeletes a soft-deleted conversation and cascades the restore to all
// its messages within a single transaction (D-015). Calling Restore on a
// conversation that already exists but is not soft-deleted is idempotent — it
// returns nil without error (D-015). Returns ErrNotFound only if the
// conversation does not exist at all.
func (cs *ConversationStore) Restore(ctx context.Context, id string) error {
	return cs.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Check if conversation exists at all (including soft-deleted).
		var count int64
		if err := tx.Unscoped().Model(&model.Conversation{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return classifyError(fmt.Errorf("store: restore conversation: %w", err))
		}
		if count == 0 {
			return ErrNotFound
		}

		// Restore the conversation if it was soft-deleted.
		result := tx.Unscoped().
			Model(&model.Conversation{}).
			Where("id = ? AND deleted_at IS NOT NULL", id).
			Update("deleted_at", nil)
		if result.Error != nil {
			return classifyError(fmt.Errorf("store: restore conversation: %w", result.Error))
		}

		// Cascade restore all messages in this conversation (D-015).
		// Only runs if the conversation was actually soft-deleted (RowsAffected > 0),
		// making this idempotent for non-deleted conversations.
		if result.RowsAffected > 0 {
			if err := tx.Unscoped().
				Model(&model.Message{}).
				Where("conversation_id = ? AND deleted_at IS NOT NULL", id).
				Update("deleted_at", nil).Error; err != nil {
				return classifyError(fmt.Errorf("store: cascade restore messages: %w", err))
			}
		}
		return nil
	})
}

// UpdateLastMessage updates the LastMessageAt and LastProcessedMessageID fields
// of the conversation identified by convID.
func (cs *ConversationStore) UpdateLastMessage(ctx context.Context, convID string, lastMessageAt time.Time, lastProcessedMessageID uint32) error {
	result := cs.db.WithContext(ctx).
		Model(&model.Conversation{}).
		Where("id = ?", convID).
		Updates(map[string]any{
			"last_message_at":           lastMessageAt,
			"last_processed_message_id": lastProcessedMessageID,
		})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: update last message: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLastRead updates the last-read message ID for the specified user.
// Uses MAX semantics: only advances forward, never backward (D-012).
// Uses a single SQL statement to avoid TOCTOU races.
func (cs *ConversationStore) UpdateLastRead(ctx context.Context, convID, userID string, messageID uint32) error {
	// Single UPDATE with CASE WHEN for both columns; only the matching user column advances.
	// WHERE clause ensures the user belongs to this conversation and RowsAffected catches missing records.
	result := cs.db.WithContext(ctx).
		Model(&model.Conversation{}).
		Where("id = ? AND (user_id1 = ? OR user_id2 = ?)", convID, userID, userID).
		Updates(map[string]any{
			"last_read_message_id1": gorm.Expr("CASE WHEN user_id1 = ? AND ? > last_read_message_id1 THEN ? ELSE last_read_message_id1 END", userID, messageID, messageID),
			"last_read_message_id2": gorm.Expr("CASE WHEN user_id2 = ? AND ? > last_read_message_id2 THEN ? ELSE last_read_message_id2 END", userID, messageID, messageID),
		})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: update last read: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SearchByTitle searches conversations for the given user that contain the
// specified title substring (case-insensitive via LIKE), ordered by
// LastMessageAt descending.
func (cs *ConversationStore) SearchByTitle(ctx context.Context, userID, title string, limit int) ([]*model.Conversation, error) {
	if limit <= 0 || limit > 101 {
		limit = 20
	}
	if title == "" {
		return []*model.Conversation{}, nil
	}

	like := "%" + escapeLikePattern(title) + "%"

	var convs []*model.Conversation
	err := cs.db.WithContext(ctx).
		Where("(user_id1 = ? OR user_id2 = ?) AND title LIKE ? ESCAPE '|'", userID, userID, like).
		Order("last_message_at DESC").
		Limit(limit).
		Find(&convs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: search conversations by title: %w", err))
	}
	return convs, nil
}

// GetUnscoped retrieves a conversation including soft-deleted records.
// Returns ErrNotFound if no record exists.
func (cs *ConversationStore) GetUnscoped(ctx context.Context, id string) (*model.Conversation, error) {
	var conv model.Conversation
	err := cs.db.WithContext(ctx).
		Unscoped().
		Where("id = ?", id).
		First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get unscoped conversation: %w", err))
	}
	return &conv, nil
}

// UpdateLastMessageTx updates last message fields within the given transaction.
func (cs *ConversationStore) UpdateLastMessageTx(ctx context.Context, tx *gorm.DB, convID string, lastMessageAt time.Time, lastProcessedMessageID uint32) error {
	result := tx.WithContext(ctx).
		Model(&model.Conversation{}).
		Where("id = ?", convID).
		Updates(map[string]any{
			"last_message_at":           lastMessageAt,
			"last_processed_message_id": lastProcessedMessageID,
		})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: update last message tx: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLastReadTx updates read cursor within the given transaction.
// Uses MAX semantics: only advances forward (D-012).
func (cs *ConversationStore) UpdateLastReadTx(ctx context.Context, tx *gorm.DB, convID, userID string, messageID uint32) error {
	result := tx.WithContext(ctx).
		Model(&model.Conversation{}).
		Where("id = ? AND (user_id1 = ? OR user_id2 = ?)", convID, userID, userID).
		Updates(map[string]any{
			"last_read_message_id1": gorm.Expr("CASE WHEN user_id1 = ? AND ? > last_read_message_id1 THEN ? ELSE last_read_message_id1 END", userID, messageID, messageID),
			"last_read_message_id2": gorm.Expr("CASE WHEN user_id2 = ? AND ? > last_read_message_id2 THEN ? ELSE last_read_message_id2 END", userID, messageID, messageID),
		})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: update last read tx: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertTx creates or updates a conversation within the given transaction.
// It uses Unscoped() to also find soft-deleted records, so that restoring a
// previously deleted conversation correctly transitions it back to active.
// If a concurrent insert causes a duplicate key error, the operation retries
// as an update to handle the TOCTOU race between SELECT and INSERT.
func (cs *ConversationStore) UpsertTx(ctx context.Context, tx *gorm.DB, conv *model.Conversation) error {
	var existing model.Conversation
	err := tx.WithContext(ctx).Unscoped().Where("id = ?", conv.ID).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if createErr := tx.WithContext(ctx).Create(conv).Error; createErr != nil {
				// TOCTOU: another goroutine may have inserted between SELECT and INSERT.
				// Use classifyError to detect duplicate key via SQLite error strings.
				if errors.Is(classifyError(createErr), ErrDuplicateKey) {
					return cs.updateExistingTx(ctx, tx, conv)
				}
				return classifyError(fmt.Errorf("store: upsert create tx: %w", createErr))
			}
			return nil
		}
		return classifyError(fmt.Errorf("store: upsert tx: %w", err))
	}
	return cs.updateExistingTx(ctx, tx, conv)
}

// updateExistingTx saves the conversation record, preserving server-controlled fields.
// It uses Unscoped() so that soft-deleted records can be updated (including
// clearing deleted_at to NULL when restoring a conversation).
func (cs *ConversationStore) updateExistingTx(ctx context.Context, tx *gorm.DB, conv *model.Conversation) error {
	if err := tx.WithContext(ctx).Unscoped().Save(conv).Error; err != nil {
		return classifyError(fmt.Errorf("store: upsert update tx: %w", err))
	}
	return nil
}

// SoftDeleteTx performs cascading soft delete within the given transaction (D-013).
func (cs *ConversationStore) SoftDeleteTx(ctx context.Context, tx *gorm.DB, id string) error {
	result := tx.WithContext(ctx).Delete(&model.Conversation{}, "id = ?", id)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete conversation tx: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	// Cascade soft-delete messages (D-013).
	if err := tx.WithContext(ctx).Where("conversation_id = ?", id).Delete(&model.Message{}).Error; err != nil {
		return classifyError(fmt.Errorf("store: cascade delete messages tx: %w", err))
	}
	return nil
}

// RestoreTx performs cascading restore within the given transaction (D-015).
func (cs *ConversationStore) RestoreTx(ctx context.Context, tx *gorm.DB, id string) error {
	// Check existence (including soft-deleted).
	var count int64
	if err := tx.WithContext(ctx).Unscoped().Model(&model.Conversation{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return classifyError(fmt.Errorf("store: restore check tx: %w", err))
	}
	if count == 0 {
		return ErrNotFound
	}
	// Restore conversation if soft-deleted.
	result := tx.WithContext(ctx).Unscoped().
		Model(&model.Conversation{}).
		Where("id = ? AND deleted_at IS NOT NULL", id).
		Update("deleted_at", nil)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: restore conversation tx: %w", result.Error))
	}
	// Cascade restore messages (D-015).
	if result.RowsAffected > 0 {
		if err := tx.WithContext(ctx).Unscoped().
			Model(&model.Message{}).
			Where("conversation_id = ? AND deleted_at IS NOT NULL", id).
			Update("deleted_at", nil).Error; err != nil {
			return classifyError(fmt.Errorf("store: cascade restore messages tx: %w", err))
		}
	}
	return nil
}
