package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

// MessageStore provides data access operations for the Message model.
type MessageStore struct {
	db *gorm.DB
}

// NewMessageStore creates a MessageStore backed by the given database.
func NewMessageStore(db *gorm.DB) *MessageStore {
	return &MessageStore{db: db}
}

// Create inserts a new message record into the database.
func (ms *MessageStore) Create(ctx context.Context, msg *model.Message) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageCreate)
	defer func() { finish(err) }()

	if err = ms.db.WithContext(ctx).Create(msg).Error; err != nil {
		return classifyError(err)
	}
	return nil
}

// Get retrieves a message by its primary key. Returns ErrNotFound if no record
// exists.
func (ms *MessageStore) Get(ctx context.Context, id string) (result *model.Message, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageGet)
	defer func() { finish(err) }()

	var msg model.Message
	err = ms.db.WithContext(ctx).
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

// GetByConversationAndMessageIDTx retrieves a message by conversation_id and
// message_id within the given transaction. Returns ErrNotFound if no record exists.
func (ms *MessageStore) GetByConversationAndMessageIDTx(ctx context.Context, tx *gorm.DB, conversationID string, messageID uint32) (result *model.Message, err error) {
	ctx, finish := startSpan(ctx, "db.message.get_by_conv_msg_id_tx")
	defer func() { finish(err) }()

	if tx == nil {
		tx = ms.db
	}

	var msg model.Message
	err = tx.WithContext(ctx).
		Where("conversation_id = ? AND message_id = ?", conversationID, messageID).
		First(&msg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get message by conversation and message id: %w", err))
	}
	return &msg, nil
}

// ListByConversation returns messages for the given conversation with
// MessageID greater than afterMessageID, ordered by MessageID ascending,
// limited to at most limit rows. This supports incremental message fetching.
func (ms *MessageStore) ListByConversation(ctx context.Context, convID string, afterMessageID uint32, limit int) (result []*model.Message, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageListByConversation,
		attribute.String(tracing.AttrConversationID, convID))
	defer func() { finish(err) }()

	if limit <= 0 || limit > 201 {
		limit = 50
	}

	var msgs []*model.Message
	err = ms.db.WithContext(ctx).
		Where("conversation_id = ? AND message_id > ?", convID, afterMessageID).
		Order("message_id ASC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list messages by conversation: %w", err))
	}
	return msgs, nil
}

// Delete performs a soft delete on the message identified by id.
func (ms *MessageStore) Delete(ctx context.Context, id string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageDelete)
	defer func() { finish(err) }()

	result := ms.db.WithContext(ctx).Delete(&model.Message{}, "id = ?", id)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete message: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// GetByClientMessageID retrieves a message by its client-generated unique ID
// and sender ID (composite uniqueness). This is useful for idempotency checks
// on the send-message path. Returns ErrNotFound if no matching record exists.
func (ms *MessageStore) GetByClientMessageID(ctx context.Context, clientMessageID, senderID string) (result *model.Message, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageGetByClientMessageID)
	defer func() { finish(err) }()

	var msg model.Message
	err = ms.db.WithContext(ctx).
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

// SearchByConversation returns messages for the given conversation that contain
// the specified content substring (case-insensitive via LIKE), ordered by
// MessageID descending (newest first), limited to at most limit rows.
// If afterMessageID is non-zero, only messages with MessageID < afterMessageID
// are returned, enabling cursor-based pagination through search results.
func (ms *MessageStore) SearchByConversation(ctx context.Context, convID, content string, afterMessageID uint32, limit int) (result []*model.Message, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageSearchByConversation,
		attribute.String(tracing.AttrConversationID, convID))
	defer func() { finish(err) }()

	if limit <= 0 || limit > 201 {
		limit = 50
	}
	if content == "" {
		return []*model.Message{}, nil
	}

	like := "%" + escapeLikePattern(content) + "%"

	query := ms.db.WithContext(ctx).
		Where("conversation_id = ? AND content LIKE ? ESCAPE '|'", convID, like)

	// Apply cursor: only messages older (lower MessageID) than the cursor.
	if afterMessageID > 0 {
		query = query.Where("message_id < ?", afterMessageID)
	}

	var msgs []*model.Message
	err = query.
		Order("message_id DESC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: search messages by content: %w", err))
	}
	return msgs, nil
}

// ListByTimeRange returns messages for the given conversation within the
// specified time range (inclusive), ordered by MessageID ascending, limited
// to at most limit rows.
func (ms *MessageStore) ListByTimeRange(ctx context.Context, convID string, startTime, endTime time.Time, limit int) (result []*model.Message, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageListByTimeRange,
		attribute.String(tracing.AttrConversationID, convID))
	defer func() { finish(err) }()

	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var msgs []*model.Message
	err = ms.db.WithContext(ctx).
		Where("conversation_id = ? AND created_at >= ? AND created_at <= ?", convID, startTime, endTime).
		Order("message_id ASC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list messages by time range: %w", err))
	}
	return msgs, nil
}

// GetLatestToolCallingMessage returns the most recent tool_calling message for a conversation
// that is in "executing" status. This is used to associate with RemoteCalling without
// relying on in-memory tracker (which would be lost on server restart).
// Returns nil, ErrNotFound if no executing tool_calling message exists.
func (ms *MessageStore) GetLatestToolCallingMessage(ctx context.Context, convID string) (result *model.Message, err error) {
	ctx, finish := startSpan(ctx, "store.MessageStore.GetLatestToolCallingMessage",
		attribute.String(tracing.AttrConversationID, convID))
	defer func() { finish(err) }()

	var msg model.Message
	err = ms.db.WithContext(ctx).
		Where("conversation_id = ? AND type = ? AND status = ?", convID, "tool_calling", "executing").
		Order("message_id DESC").
		First(&msg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get latest tool_calling message: %w", err))
	}
	return &msg, nil
}

// Restore undeletes a soft-deleted message identified by id.
func (ms *MessageStore) Restore(ctx context.Context, id string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageRestore)
	defer func() { finish(err) }()

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
func (ms *MessageStore) DeleteByConversation(ctx context.Context, convID string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageDeleteByConversation,
		attribute.String(tracing.AttrConversationID, convID))
	defer func() { finish(err) }()

	result := ms.db.WithContext(ctx).
		Where("conversation_id = ?", convID).
		Delete(&model.Message{})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete messages by conversation: %w", result.Error))
	}
	return nil
}

// MarkSummarized sets summarized=true for all messages in the conversation
// with MessageID <= maxMessageID. This is called after a summary is generated
// to mark the original messages as summarized.
func (ms *MessageStore) MarkSummarized(ctx context.Context, convID string, maxMessageID uint32) (err error) {
	ctx, finish := startSpan(ctx, "db.message.mark_summarized")
	defer func() { finish(err) }()

	result := ms.db.WithContext(ctx).
		Model(&model.Message{}).
		Where("conversation_id = ? AND message_id <= ? AND summarized = ?", convID, maxMessageID, false).
		Update("summarized", true)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: mark summarized: %w", result.Error))
	}
	return nil
}

// Begin starts a new database transaction and returns the underlying *gorm.DB
// scoped to that transaction. The caller is responsible for calling
// tx.Commit() or tx.Rollback().
func (ms *MessageStore) Begin() *gorm.DB {
	return ms.db.Begin()
}

// CountUnread returns the number of messages in the given conversation with
// MessageID greater than afterMessageID. Soft-deleted messages are excluded
// automatically by GORM's soft-delete plugin. This supports unread message
// counting.
func (ms *MessageStore) CountUnread(ctx context.Context, convID string, afterMessageID uint32) (count int64, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageCountUnread,
		attribute.String(tracing.AttrConversationID, convID))
	defer func() { finish(err) }()

	err = ms.db.WithContext(ctx).
		Model(&model.Message{}).
		Where("conversation_id = ? AND message_id > ?", convID, afterMessageID).
		Count(&count).Error
	if err != nil {
		return 0, classifyError(fmt.Errorf("store: count unread messages: %w", err))
	}
	// Defensive guard: the count should never be negative given the WHERE
	// clause above, but ensure a non-negative result regardless of database
	// quirks or edge cases (e.g. concurrent deletes during the query).
	if count < 0 {
		count = 0
	}
	return count, nil
}

// RestoreByConversation restores all soft-deleted messages belonging to the
// given conversation. Returns the number of restored rows.
func (ms *MessageStore) RestoreByConversation(ctx context.Context, convID string) (count int64, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageRestoreByConversation,
		attribute.String(tracing.AttrConversationID, convID))
	defer func() { finish(err) }()

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

// UpdateMessageContentTx updates the Content, Type, and Status fields of a
// message identified by (conversation_id, message_id) within the given
// transaction. Returns ErrNotFound if no matching record exists.
func (ms *MessageStore) UpdateMessageContentTx(ctx context.Context, tx *gorm.DB, conversationID string, messageID uint32, content, msgType, status string) (err error) {
	ctx, finish := startSpan(ctx, "db.message.update_content_tx")
	defer func() { finish(err) }()

	result := tx.WithContext(ctx).
		Model(&model.Message{}).
		Where("conversation_id = ? AND message_id = ?", conversationID, messageID).
		Updates(map[string]interface{}{
			"content": content,
			"type":    msgType,
			"status":  status,
		})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: update message content tx: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListRecentByConversation returns the most recent messages for a conversation,
// ordered by MessageID descending (newest first), limited to at most limit rows.
// Soft-deleted messages are excluded automatically by GORM's soft-delete plugin.
// This is used by the Agent context manager to load conversation history.
func (ms *MessageStore) ListRecentByConversation(ctx context.Context, convID string, limit int) (result []*model.Message, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBMessageListRecentByConversation,
		attribute.String(tracing.AttrConversationID, convID))
	defer func() { finish(err) }()

	if limit <= 0 || limit > 500 {
		limit = 50
	}

	var msgs []*model.Message
	err = ms.db.WithContext(ctx).
		Where("conversation_id = ? AND (summarized = ? OR type = ?)", convID, false, "summary").
		Order("message_id DESC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list recent messages by conversation: %w", err))
	}
	return msgs, nil
}
