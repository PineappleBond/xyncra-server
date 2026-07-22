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

// RemoteCallingStore handles RemoteCalling persistence operations.
type RemoteCallingStore struct {
	db *gorm.DB
}

// NewRemoteCallingStore creates a new RemoteCallingStore.
func NewRemoteCallingStore(db *gorm.DB) *RemoteCallingStore {
	return &RemoteCallingStore{db: db}
}

// Create persists a new RemoteCalling.
func (rs *RemoteCallingStore) Create(ctx context.Context, rc *model.RemoteCalling) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingCreate)
	defer func() { finish(err) }()

	if err = rs.db.WithContext(ctx).Create(rc).Error; err != nil {
		return classifyError(fmt.Errorf("store: create remote calling: %w", err))
	}
	return nil
}

// GetByID returns a RemoteCalling by its ID.
// Returns ErrNotFound if the record does not exist.
func (rs *RemoteCallingStore) GetByID(ctx context.Context, id string) (result *model.RemoteCalling, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingGetByID,
		attribute.String("xyncra.remote_calling.id", id))
	defer func() { finish(err) }()

	var rc model.RemoteCalling
	if err = rs.db.WithContext(ctx).Where("id = ?", id).First(&rc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get remote calling by id: %w", err))
	}
	return &rc, nil
}

// GetPendingByConversation returns all pending RemoteCallings for a conversation,
// ordered by creation time ascending.
func (rs *RemoteCallingStore) GetPendingByConversation(ctx context.Context, conversationID string) (result []*model.RemoteCalling, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingGetPendingByConversation,
		attribute.String(tracing.AttrConversationID, conversationID))
	defer func() { finish(err) }()

	var rcs []*model.RemoteCalling
	if err = rs.db.WithContext(ctx).
		Where("conversation_id = ? AND status = ?", conversationID, model.RemoteCallingStatusPending).
		Order("created_at ASC").
		Find(&rcs).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get pending remote callings by conversation: %w", err))
	}
	return rcs, nil
}

// GetPendingByCheckpoint returns all pending RemoteCallings for a checkpoint.
func (rs *RemoteCallingStore) GetPendingByCheckpoint(ctx context.Context, checkpointID string) (result []*model.RemoteCalling, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingGetPendingByCheckpoint)
	defer func() { finish(err) }()

	var rcs []*model.RemoteCalling
	if err = rs.db.WithContext(ctx).
		Where("checkpoint_id = ? AND status = ?", checkpointID, model.RemoteCallingStatusPending).
		Order("created_at ASC").
		Find(&rcs).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get pending remote callings by checkpoint: %w", err))
	}
	return rcs, nil
}

// GetByCheckpoint returns all RemoteCallings for a checkpoint (all statuses).
func (rs *RemoteCallingStore) GetByCheckpoint(ctx context.Context, checkpointID string) (result []*model.RemoteCalling, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingGetByCheckpoint)
	defer func() { finish(err) }()

	var rcs []*model.RemoteCalling
	if err = rs.db.WithContext(ctx).
		Where("checkpoint_id = ?", checkpointID).
		Order("created_at ASC").
		Find(&rcs).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get remote callings by checkpoint: %w", err))
	}
	return rcs, nil
}

// ResolveResult marks a pending RemoteCalling as resolved with a successful result.
// Returns ErrConflict if the RemoteCalling is already resolved.
func (rs *RemoteCallingStore) ResolveResult(ctx context.Context, id, result string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingResolveResult)
	defer func() { finish(err) }()

	now := time.Now()
	dbResult := rs.db.WithContext(ctx).
		Model(&model.RemoteCalling{}).
		Where("id = ? AND status = ?", id, model.RemoteCallingStatusPending).
		Updates(map[string]interface{}{
			"status":      model.RemoteCallingStatusResolved,
			"result":      result,
			"success":     true,
			"resolved_at": now,
		})
	if dbResult.Error != nil {
		return classifyError(fmt.Errorf("store: resolve remote calling result: %w", dbResult.Error))
	}
	if dbResult.RowsAffected == 0 {
		// Either the record doesn't exist, or it's already resolved.
		var existing model.RemoteCalling
		if err = rs.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return classifyError(fmt.Errorf("store: get remote calling for resolve check: %w", err))
		}
		// Record exists but is already resolved — conflict (idempotency).
		return ErrConflict
	}
	return nil
}

// ResolveError marks a pending RemoteCalling as resolved with an error.
// Returns ErrConflict if the RemoteCalling is already resolved.
func (rs *RemoteCallingStore) ResolveError(ctx context.Context, id, errorMessage string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingResolveError)
	defer func() { finish(err) }()

	now := time.Now()
	dbResult := rs.db.WithContext(ctx).
		Model(&model.RemoteCalling{}).
		Where("id = ? AND status = ?", id, model.RemoteCallingStatusPending).
		Updates(map[string]interface{}{
			"status":        model.RemoteCallingStatusResolved,
			"error_message": errorMessage,
			"success":       false,
			"resolved_at":   now,
		})
	if dbResult.Error != nil {
		return classifyError(fmt.Errorf("store: resolve remote calling error: %w", dbResult.Error))
	}
	if dbResult.RowsAffected == 0 {
		var existing model.RemoteCalling
		if err = rs.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return classifyError(fmt.Errorf("store: get remote calling for resolve check: %w", err))
		}
		return ErrConflict
	}
	return nil
}

// CancelByCheckpoint cancels all pending RemoteCallings for a checkpoint.
// Returns the cancelled count, conversation ID, and agent ID from the first cancelled record.
// The conversation ID and agent ID are needed to enqueue the agent resume task.
//
// BUG-FIX: Wrap the two-step operation (fetch + update) in a transaction to
// prevent a race window where another goroutine could modify the records
// between the First and Updates calls.
func (rs *RemoteCallingStore) CancelByCheckpoint(ctx context.Context, checkpointID, reason, cancelledBy string) (count int64, conversationID string, agentID string, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingCancelByCheckpoint)
	defer func() { finish(err) }()

	err = rs.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// First, fetch one pending record to get conversationID and agentID before cancelling.
		var first model.RemoteCalling
		if err := tx.
			Where("checkpoint_id = ? AND status = ?", checkpointID, model.RemoteCallingStatusPending).
			First(&first).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // nothing to cancel, count=0
			}
			return classifyError(fmt.Errorf("store: get first pending for cancel: %w", err))
		}

		now := time.Now()
		result := tx.
			Model(&model.RemoteCalling{}).
			Where("checkpoint_id = ? AND status = ?", checkpointID, model.RemoteCallingStatusPending).
			Updates(map[string]interface{}{
				"status":        model.RemoteCallingStatusCancelled,
				"cancelled_at":  now,
				"cancelled_by":  cancelledBy,
				"cancel_reason": reason,
			})
		if result.Error != nil {
			return classifyError(fmt.Errorf("store: cancel remote callings by checkpoint: %w", result.Error))
		}
		count = result.RowsAffected
		conversationID = first.ConversationID
		agentID = first.AgentID
		return nil
	})
	if err != nil {
		return 0, "", "", err
	}
	return count, conversationID, agentID, nil
}

// DeleteByConversation soft-deletes all RemoteCallings for a conversation.
func (rs *RemoteCallingStore) DeleteByConversation(ctx context.Context, conversationID string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingDeleteByConversation,
		attribute.String(tracing.AttrConversationID, conversationID))
	defer func() { finish(err) }()

	if err = rs.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Delete(&model.RemoteCalling{}).Error; err != nil {
		return classifyError(fmt.Errorf("store: delete remote callings by conversation: %w", err))
	}
	return nil
}

// DeleteByCheckpoint soft-deletes all RemoteCallings for a checkpoint.
func (rs *RemoteCallingStore) DeleteByCheckpoint(ctx context.Context, checkpointID string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingDeleteByCheckpoint)
	defer func() { finish(err) }()

	result := rs.db.WithContext(ctx).
		Where("checkpoint_id = ?", checkpointID).
		Delete(&model.RemoteCalling{})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete remote callings by checkpoint: %w", result.Error))
	}
	return nil
}

// CountPendingByCheckpoint returns the count of pending RemoteCallings for a checkpoint.
func (rs *RemoteCallingStore) CountPendingByCheckpoint(ctx context.Context, checkpointID string) (count int64, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingCountPendingByCheckpoint)
	defer func() { finish(err) }()

	err = rs.db.WithContext(ctx).
		Model(&model.RemoteCalling{}).
		Where("checkpoint_id = ? AND status = ?", checkpointID, model.RemoteCallingStatusPending).
		Count(&count).Error
	if err != nil {
		return 0, classifyError(fmt.Errorf("store: count pending remote callings: %w", err))
	}
	return count, nil
}

// ListExpired returns pending RemoteCallings that have passed their expiration time.
func (rs *RemoteCallingStore) ListExpired(ctx context.Context, limit int) (result []*model.RemoteCalling, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingListExpired)
	defer func() { finish(err) }()

	var rcs []*model.RemoteCalling
	if err = rs.db.WithContext(ctx).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at < ?", model.RemoteCallingStatusPending, time.Now()).
		Limit(limit).
		Order("created_at ASC").
		Find(&rcs).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: list expired remote callings: %w", err))
	}
	return rcs, nil
}

// MarkExpired marks a pending RemoteCalling as expired.
// Returns ErrNotFound if the record does not exist.
// Returns ErrConflict if the record is already resolved/cancelled/expired.
func (rs *RemoteCallingStore) MarkExpired(ctx context.Context, id string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingMarkExpired)
	defer func() { finish(err) }()

	result := rs.db.WithContext(ctx).
		Model(&model.RemoteCalling{}).
		Where("id = ? AND status = ?", id, model.RemoteCallingStatusPending).
		Update("status", model.RemoteCallingStatusExpired)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: mark remote calling expired: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		// Either the record doesn't exist, or it's already in a non-pending status.
		var existing model.RemoteCalling
		if err = rs.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return classifyError(fmt.Errorf("store: get remote calling for expired check: %w", err))
		}
		return ErrConflict
	}
	return nil
}
