package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// SyncStateStore provides key-value data access for client-side synchronization
// state tracking (e.g. local_max_seq, latest_seq).
type SyncStateStore struct {
	db *gorm.DB
}

const (
	// syncKeyLocalMaxSeq is the key for the local maximum processed seq.
	syncKeyLocalMaxSeq = "local_max_seq"
	// syncKeyLatestSeq is the key for the server-reported latest seq.
	syncKeyLatestSeq = "latest_seq"
)

// Get retrieves the value for the given key. Returns ErrNotFound if the key
// does not exist.
func (ss *SyncStateStore) Get(ctx context.Context, key string) (string, error) {
	var state model.SyncState
	err := ss.db.WithContext(ctx).
		Where("key = ?", key).
		First(&state).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", classifyError(fmt.Errorf("store: get sync state: %w", err))
	}
	return state.Value, nil
}

// Set performs an UPSERT for the given key-value pair. If the key already
// exists, the value is updated; otherwise a new record is inserted.
func (ss *SyncStateStore) Set(ctx context.Context, key, value string) error {
	state := model.SyncState{Key: key, Value: value}
	err := ss.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
		}).
		Create(&state).Error
	if err != nil {
		return classifyError(fmt.Errorf("store: set sync state: %w", err))
	}
	return nil
}

// GetLocalMaxSeq returns the local_max_seq value. Returns 0 if not set.
func (ss *SyncStateStore) GetLocalMaxSeq(ctx context.Context) (uint32, error) {
	val, err := ss.Get(ctx, syncKeyLocalMaxSeq)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	seq, err := strconv.ParseUint(val, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("store: parse local_max_seq: %w", err)
	}
	return uint32(seq), nil
}

// SetLocalMaxSeq sets the local_max_seq value.
func (ss *SyncStateStore) SetLocalMaxSeq(ctx context.Context, seq uint32) error {
	return ss.Set(ctx, syncKeyLocalMaxSeq, strconv.FormatUint(uint64(seq), 10))
}

// GetLatestSeq returns the latest_seq value. Returns 0 if not set.
func (ss *SyncStateStore) GetLatestSeq(ctx context.Context) (uint32, error) {
	val, err := ss.Get(ctx, syncKeyLatestSeq)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	seq, err := strconv.ParseUint(val, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("store: parse latest_seq: %w", err)
	}
	return uint32(seq), nil
}

// SetLatestSeq sets the latest_seq value.
func (ss *SyncStateStore) SetLatestSeq(ctx context.Context, seq uint32) error {
	return ss.Set(ctx, syncKeyLatestSeq, strconv.FormatUint(uint64(seq), 10))
}
