package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// QueueStore provides data access operations for the retry task queue.
type QueueStore struct {
	db *gorm.DB
}

// Save inserts a new retry task into the queue.
func (qs *QueueStore) Save(ctx context.Context, task *model.RetryTask) error {
	if err := qs.db.WithContext(ctx).Create(task).Error; err != nil {
		return classifyError(fmt.Errorf("store: save retry task: %w", err))
	}
	return nil
}

// ListPending returns retry tasks with status "pending" and NextRetry <= now,
// ordered by NextRetry ascending (soonest first).
func (qs *QueueStore) ListPending(ctx context.Context, limit int) ([]*model.RetryTask, error) {
	if limit <= 0 {
		limit = 50
	}

	var tasks []*model.RetryTask
	err := qs.db.WithContext(ctx).
		Where("status = ? AND next_retry <= ?", "pending", time.Now()).
		Order("next_retry ASC").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list pending retry tasks: %w", err))
	}
	return tasks, nil
}

// Update saves changes to a retry task (attempt count, next retry time,
// last error, etc.).
func (qs *QueueStore) Update(ctx context.Context, task *model.RetryTask) error {
	if err := qs.db.WithContext(ctx).Save(task).Error; err != nil {
		return classifyError(fmt.Errorf("store: update retry task: %w", err))
	}
	return nil
}

// MarkFailed sets the task's status to "failed" so it no longer appears in
// ListPending results.
func (qs *QueueStore) MarkFailed(ctx context.Context, id string, lastError string) error {
	result := qs.db.WithContext(ctx).
		Model(&model.RetryTask{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     "failed",
			"last_error": lastError,
		})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: mark retry task failed: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a retry task by its primary key.
func (qs *QueueStore) Delete(ctx context.Context, id string) error {
	result := qs.db.WithContext(ctx).Delete(&model.RetryTask{}, "id = ?", id)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete retry task: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Count returns the total number of retry tasks with the given status.
func (qs *QueueStore) Count(ctx context.Context, status string) (int64, error) {
	var count int64
	err := qs.db.WithContext(ctx).
		Model(&model.RetryTask{}).
		Where("status = ?", status).
		Count(&count).Error
	if err != nil {
		return 0, classifyError(fmt.Errorf("store: count retry tasks: %w", err))
	}
	return count, nil
}
