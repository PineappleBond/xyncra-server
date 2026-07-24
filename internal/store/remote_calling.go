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
	// Use Unscoped() to also find soft-deleted records, consistent with
	// ResolveResult/ResolveError which use Unscoped() for idempotency checks.
	// After cleanup (DeleteByCheckpoint), resolved RemoteCallings are soft-deleted
	// but GetByID should still find them so callers get a consistent view.
	if err = rs.db.WithContext(ctx).Unscoped().Where("id = ?", id).First(&rc).Error; err != nil {
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

// GetResolvedByCheckpoint returns only resolved RemoteCallings for a checkpoint.
// This is more efficient than GetByCheckpoint when only resolved results are needed
// (e.g., in the cleanup task to check if any were resolved).
func (rs *RemoteCallingStore) GetResolvedByCheckpoint(ctx context.Context, checkpointID string) (result []*model.RemoteCalling, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingGetByCheckpoint)
	defer func() { finish(err) }()

	var rcs []*model.RemoteCalling
	if err = rs.db.WithContext(ctx).
		Where("checkpoint_id = ? AND status = ?", checkpointID, model.RemoteCallingStatusResolved).
		Order("created_at ASC").
		Find(&rcs).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get resolved remote callings by checkpoint: %w", err))
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
		// Use Unscoped() to also find soft-deleted records (D-137): after
		// cleanup (DeleteByCheckpoint), resolved RemoteCallings are soft-deleted
		// but we still need to return ErrConflict (not ErrNotFound) so the
		// client gets an "already processed" response instead of "not found".
		var existing model.RemoteCalling
		if err = rs.db.WithContext(ctx).Unscoped().Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return classifyError(fmt.Errorf("store: get remote calling for resolve check: %w", err))
		}
		// Record exists (possibly soft-deleted) — conflict (idempotency).
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
		// Use Unscoped() to also find soft-deleted records (D-137), matching
		// the ResolveResult behavior above.
		var existing model.RemoteCalling
		if err = rs.db.WithContext(ctx).Unscoped().Where("id = ?", id).First(&existing).Error; err != nil {
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

// GetResolvedByMethodAndConversation returns the most recent resolved RemoteCalling
// for a given method and conversation that has a valid message_id (message_id > 0).
// This is used by WrapInvokableToolCall during Resume to find the existing
// tool_calling message instead of creating a duplicate.
// Returns nil, ErrNotFound if no matching record exists.
func (rs *RemoteCallingStore) GetResolvedByMethodAndConversation(ctx context.Context, method, conversationID string) (result *model.RemoteCalling, err error) {
	ctx, finish := startSpan(ctx, "store.RemoteCallingStore.GetResolvedByMethodAndConversation")
	defer func() { finish(err) }()

	var rc model.RemoteCalling
	err = rs.db.WithContext(ctx).
		Where("method = ? AND conversation_id = ? AND status = ? AND message_id > 0",
			method, conversationID, model.RemoteCallingStatusResolved).
		Order("created_at DESC").
		First(&rc).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get resolved remote calling by method and conversation: %w", err))
	}
	return &rc, nil
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
// The now parameter allows callers to inject the current time for testability.
func (rs *RemoteCallingStore) ListExpired(ctx context.Context, limit int, now time.Time) (result []*model.RemoteCalling, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingListExpired)
	defer func() { finish(err) }()

	var rcs []*model.RemoteCalling
	if err = rs.db.WithContext(ctx).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at < ?", model.RemoteCallingStatusPending, now).
		Limit(limit).
		Order("created_at ASC").
		Find(&rcs).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: list expired remote callings: %w", err))
	}
	return rcs, nil
}

// MarkExpiredByCheckpoint batch-marks all pending RemoteCallings for a checkpoint
// that have passed their expires_at as expired. Returns the count of newly expired RCs.
// This is called by the agent_resume handler to immediately expire overdue siblings
// before checking pending count, avoiding the need to wait for the periodic cleanup task.
//
// The now parameter allows callers to inject the current time for testability,
// consistent with ListExpired which also accepts a now parameter.
func (rs *RemoteCallingStore) MarkExpiredByCheckpoint(ctx context.Context, checkpointID string, now time.Time) (count int64, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBRemoteCallingMarkExpiredByCheckpoint)
	defer func() { finish(err) }()

	result := rs.db.WithContext(ctx).
		Model(&model.RemoteCalling{}).
		Where("checkpoint_id = ? AND status = ? AND expires_at IS NOT NULL AND expires_at < ?",
			checkpointID, model.RemoteCallingStatusPending, now).
		Update("status", model.RemoteCallingStatusExpired)
	if result.Error != nil {
		return 0, classifyError(fmt.Errorf("store: mark expired by checkpoint: %w", result.Error))
	}
	return result.RowsAffected, nil
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
