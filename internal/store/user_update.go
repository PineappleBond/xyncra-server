package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// UserUpdateStore provides data access operations for the UserUpdate model.
type UserUpdateStore struct {
	db *gorm.DB
}

// NewUserUpdateStore creates a UserUpdateStore backed by the given database.
func NewUserUpdateStore(db *gorm.DB) *UserUpdateStore {
	return &UserUpdateStore{db: db}
}

// Create inserts a batch of user update records. GORM's CreateInBatches is used
// to split large slices into manageable chunks (batch size 100).
func (us *UserUpdateStore) Create(ctx context.Context, updates []model.UserUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	if err := us.db.WithContext(ctx).CreateInBatches(updates, 100).Error; err != nil {
		return classifyError(err)
	}
	return nil
}

// ListByUser returns user updates for the given userID with Seq greater than
// afterSeq, ordered by Seq ascending, limited to at most limit rows. This
// supports incremental / long-polling sync of a user's event stream.
func (us *UserUpdateStore) ListByUser(ctx context.Context, userID string, afterSeq uint32, limit int) ([]*model.UserUpdate, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var updates []*model.UserUpdate
	err := us.db.WithContext(ctx).
		Where("user_id = ? AND seq > ?", userID, afterSeq).
		Order("seq ASC").
		Limit(limit).
		Find(&updates).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list user updates: %w", err))
	}
	return updates, nil
}

// GetLatestSeq returns the highest Seq value for the given user. Returns 0 and
// nil if the user has no update records.
func (us *UserUpdateStore) GetLatestSeq(ctx context.Context, userID string) (uint32, error) {
	var seq uint32
	err := us.db.WithContext(ctx).
		Model(&model.UserUpdate{}).
		Where("user_id = ?", userID).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&seq).Error
	if err != nil {
		return 0, classifyError(fmt.Errorf("store: get latest seq: %w", err))
	}
	return seq, nil
}

// DefaultCleanupRetention is the default retention period for user updates.
// Updates older than this are considered expired and eligible for cleanup.
const DefaultCleanupRetention = 30 * 24 * time.Hour // 30 days

// CleanupExpiredBefore deletes all user updates with CreatedAt strictly before
// the given time. This is a hard delete (Unscoped) since expired updates should
// be permanently removed. Returns the number of deleted rows.
func (us *UserUpdateStore) CleanupExpiredBefore(ctx context.Context, before time.Time) (int64, error) {
	result := us.db.WithContext(ctx).
		Unscoped().
		Where("created_at < ?", before).
		Delete(&model.UserUpdate{})
	if result.Error != nil {
		return 0, classifyError(fmt.Errorf("store: cleanup expired user updates: %w", result.Error))
	}
	return result.RowsAffected, nil
}

// CleanupExpired deletes all user updates older than DefaultCleanupRetention
// (30 days) from now. It is a convenience wrapper around CleanupExpiredBefore.
// For callers that need a custom retention period, use CleanupExpiredBefore
// directly with the desired cutoff time.
func (us *UserUpdateStore) CleanupExpired(ctx context.Context) (int64, error) {
	return us.CleanupExpiredBefore(ctx, time.Now().Add(-DefaultCleanupRetention))
}
