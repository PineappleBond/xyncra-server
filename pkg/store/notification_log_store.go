package store

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// NotificationLogStore provides data access operations for push notification
// logging and deduplication.
type NotificationLogStore struct {
	db *gorm.DB
}

// Save inserts a new notification log record.
func (ns *NotificationLogStore) Save(ctx context.Context, log *model.NotificationLog) error {
	if err := ns.db.WithContext(ctx).Create(log).Error; err != nil {
		return classifyError(fmt.Errorf("store: save notification log: %w", err))
	}
	return nil
}

// NotificationLogFilter defines optional filters for listing notification logs.
type NotificationLogFilter struct {
	StartTime *time.Time
	EndTime   *time.Time
	Type      string
	Limit     int
}

// List returns notification logs matching the given filters, ordered by
// CreatedAt descending (newest first).
func (ns *NotificationLogStore) List(ctx context.Context, filter NotificationLogFilter) ([]*model.NotificationLog, error) {
	query := ns.db.WithContext(ctx).Model(&model.NotificationLog{})

	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("created_at <= ?", *filter.EndTime)
	}
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}

	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var logs []*model.NotificationLog
	err := query.
		Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list notification logs: %w", err))
	}
	return logs, nil
}

// ListBySeqRange returns notification logs with Seq in the range
// [startSeq, endSeq] (inclusive), ordered by Seq ascending.
func (ns *NotificationLogStore) ListBySeqRange(ctx context.Context, startSeq, endSeq uint32) ([]*model.NotificationLog, error) {
	var logs []*model.NotificationLog
	err := ns.db.WithContext(ctx).
		Where("seq >= ? AND seq <= ?", startSeq, endSeq).
		Order("seq ASC").
		Find(&logs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list notification logs by seq range: %w", err))
	}
	return logs, nil
}

// ExportCSV writes notification logs matching the filter as CSV to the given writer.
func (ns *NotificationLogStore) ExportCSV(ctx context.Context, w io.Writer, filter NotificationLogFilter) error {
	logs, err := ns.List(ctx, filter)
	if err != nil {
		return err
	}

	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write([]string{"id", "seq", "type", "created_at"}); err != nil {
		return fmt.Errorf("store: write csv header: %w", err)
	}

	for _, l := range logs {
		record := []string{
			l.ID,
			fmt.Sprintf("%d", l.Seq),
			l.Type,
			l.CreatedAt.Format(time.RFC3339),
		}
		if err := cw.Write(record); err != nil {
			return fmt.Errorf("store: write csv record: %w", err)
		}
	}
	return nil
}

// ExportJSON writes notification logs matching the filter as JSON to the given writer.
func (ns *NotificationLogStore) ExportJSON(ctx context.Context, w io.Writer, filter NotificationLogFilter) error {
	logs, err := ns.List(ctx, filter)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(logs); err != nil {
		return fmt.Errorf("store: encode notification logs json: %w", err)
	}
	return nil
}

// CleanupBefore hard-deletes notification logs with CreatedAt strictly before
// the given time. Returns the number of deleted rows.
func (ns *NotificationLogStore) CleanupBefore(ctx context.Context, before time.Time) (int64, error) {
	result := ns.db.WithContext(ctx).
		Unscoped().
		Where("created_at < ?", before).
		Delete(&model.NotificationLog{})
	if result.Error != nil {
		return 0, classifyError(fmt.Errorf("store: cleanup notification logs: %w", result.Error))
	}
	return result.RowsAffected, nil
}

// GetLatestSeq returns the highest Seq value in the notification log.
// Returns 0 if the log is empty.
func (ns *NotificationLogStore) GetLatestSeq(ctx context.Context) (uint32, error) {
	var seq uint32
	err := ns.db.WithContext(ctx).
		Model(&model.NotificationLog{}).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&seq).Error
	if err != nil {
		return 0, classifyError(fmt.Errorf("store: get latest notification seq: %w", err))
	}
	return seq, nil
}
