package store

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// RPCLogStore provides data access operations for RPC call logging.
type RPCLogStore struct {
	db *gorm.DB
}

// Save inserts a new RPC log record.
func (rs *RPCLogStore) Save(ctx context.Context, log *model.RPCLog) error {
	if err := rs.db.WithContext(ctx).Create(log).Error; err != nil {
		return classifyError(fmt.Errorf("store: save rpc log: %w", err))
	}
	return nil
}

// Update updates an existing RPC log record (e.g. after receiving the response).
func (rs *RPCLogStore) Update(ctx context.Context, log *model.RPCLog) error {
	if err := rs.db.WithContext(ctx).Save(log).Error; err != nil {
		return classifyError(fmt.Errorf("store: update rpc log: %w", err))
	}
	return nil
}

// RPCLogFilter defines optional filters for listing RPC logs.
type RPCLogFilter struct {
	StartTime      *time.Time
	EndTime        *time.Time
	Method         string
	StatusCode     *int
	ConversationID string
	Limit          int
}

// List returns RPC logs matching the given filters, ordered by CreatedAt
// descending (newest first).
func (rs *RPCLogStore) List(ctx context.Context, filter RPCLogFilter) ([]*model.RPCLog, error) {
	query := rs.db.WithContext(ctx).Model(&model.RPCLog{})

	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("created_at <= ?", *filter.EndTime)
	}
	if filter.Method != "" {
		query = query.Where("method = ?", filter.Method)
	}
	if filter.StatusCode != nil {
		query = query.Where("status_code = ?", *filter.StatusCode)
	}
	if filter.ConversationID != "" {
		query = query.Where("conversation_id = ?", filter.ConversationID)
	}

	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var logs []*model.RPCLog
	err := query.
		Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list rpc logs: %w", err))
	}
	return logs, nil
}

// GetByRequestID retrieves an RPC log by its request ID.
// Returns ErrNotFound if no matching record exists.
func (rs *RPCLogStore) GetByRequestID(ctx context.Context, requestID string) (*model.RPCLog, error) {
	var log model.RPCLog
	err := rs.db.WithContext(ctx).
		Where("request_id = ?", requestID).
		First(&log).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get rpc log by request_id: %w", err))
	}
	return &log, nil
}

// RPCAggregateRow represents a single row in an aggregate report.
type RPCAggregateRow struct {
	Method     string  `json:"method"`
	Count      int64   `json:"count"`
	Success    int64   `json:"success"`
	ErrorCount int64   `json:"error_count"`
	AvgMs      float64 `json:"avg_ms"`
}

// Aggregate returns per-method RPC statistics for the given time range.
func (rs *RPCLogStore) Aggregate(ctx context.Context, startTime, endTime time.Time) ([]RPCAggregateRow, error) {
	var rows []RPCAggregateRow
	err := rs.db.WithContext(ctx).
		Model(&model.RPCLog{}).
		Select(`method,
			COUNT(*) as count,
			SUM(CASE WHEN status_code >= 0 THEN 1 ELSE 0 END) as success,
			SUM(CASE WHEN status_code < 0 THEN 1 ELSE 0 END) as error_count,
			AVG(CAST(duration AS FLOAT) / 1000000.0) as avg_ms`).
		Where("created_at >= ? AND created_at < ?", startTime, endTime).
		Group("method").
		Scan(&rows).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: aggregate rpc logs: %w", err))
	}
	return rows, nil
}

// RPCIntervalRow represents aggregated RPC log statistics for a time interval.
type RPCIntervalRow struct {
	Interval   string  `json:"interval"` // e.g. "2026-07-09 10:00"
	Method     string  `json:"method"`
	Count      int64   `json:"count"`
	Success    int64   `json:"success"`
	ErrorCount int64   `json:"error_count"`
	AvgMs      float64 `json:"avg_ms"`
}

// AggregateByInterval returns per-interval, per-method RPC statistics for the
// given time range [startTime, endTime). Supported intervals: "1m", "5m",
// "15m", "1h", "1d". Results are ordered by interval ASC, method ASC.
func (rs *RPCLogStore) AggregateByInterval(ctx context.Context, startTime, endTime time.Time, interval string) ([]RPCIntervalRow, error) {
	var bucketExpr string
	switch interval {
	case "1m":
		bucketExpr = "strftime('%Y-%m-%d %H:%M:00', created_at)"
	case "5m":
		bucketExpr = "datetime((strftime('%s', created_at) / 300) * 300, 'unixepoch')"
	case "15m":
		bucketExpr = "datetime((strftime('%s', created_at) / 900) * 900, 'unixepoch')"
	case "1h":
		bucketExpr = "strftime('%Y-%m-%d %H:00:00', created_at)"
	case "1d":
		bucketExpr = "date(created_at)"
	default:
		return nil, fmt.Errorf("store: invalid interval %q: must be one of 1m, 5m, 15m, 1h, 1d", interval)
	}

	var results []RPCIntervalRow
	err := rs.db.WithContext(ctx).
		Table("rpc_logs").
		Select(bucketExpr+` as interval,
			method,
			COUNT(*) as count,
			SUM(CASE WHEN status_code >= 0 THEN 1 ELSE 0 END) as success,
			SUM(CASE WHEN status_code < 0 THEN 1 ELSE 0 END) as error_count,
			AVG(CAST(duration AS FLOAT) / 1000000.0) as avg_ms`).
		Where("created_at >= ? AND created_at < ?", startTime, endTime).
		Group("interval").
		Group("method").
		Order("interval ASC").
		Order("method ASC").
		Find(&results).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: aggregate rpc logs by interval: %w", err))
	}
	return results, nil
}

// ExportCSV writes RPC logs matching the filter as CSV to the given writer.
func (rs *RPCLogStore) ExportCSV(ctx context.Context, w io.Writer, filter RPCLogFilter) error {
	logs, err := rs.List(ctx, filter)
	if err != nil {
		return err
	}

	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header row.
	if err := cw.Write([]string{"id", "request_id", "method", "status_code", "conversation_id", "duration_ms", "error", "created_at"}); err != nil {
		return fmt.Errorf("store: write csv header: %w", err)
	}

	for _, l := range logs {
		record := []string{
			l.ID,
			l.RequestID,
			l.Method,
			fmt.Sprintf("%d", l.StatusCode),
			l.ConversationID,
			fmt.Sprintf("%.3f", float64(l.Duration.Microseconds())/1000.0),
			l.ErrorMsg,
			l.CreatedAt.Format(time.RFC3339),
		}
		if err := cw.Write(record); err != nil {
			return fmt.Errorf("store: write csv record: %w", err)
		}
	}
	return nil
}

// ExportJSON writes RPC logs matching the filter as JSON to the given writer.
func (rs *RPCLogStore) ExportJSON(ctx context.Context, w io.Writer, filter RPCLogFilter) error {
	logs, err := rs.List(ctx, filter)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(logs); err != nil {
		return fmt.Errorf("store: encode rpc logs json: %w", err)
	}
	return nil
}

// CleanupBefore hard-deletes RPC logs with CreatedAt strictly before the given
// time. Returns the number of deleted rows.
func (rs *RPCLogStore) CleanupBefore(ctx context.Context, before time.Time) (int64, error) {
	result := rs.db.WithContext(ctx).
		Unscoped().
		Where("created_at < ?", before).
		Delete(&model.RPCLog{})
	if result.Error != nil {
		return 0, classifyError(fmt.Errorf("store: cleanup rpc logs: %w", result.Error))
	}
	return result.RowsAffected, nil
}

// CleanupOlderThan hard-deletes RPC logs older than the given duration.
func (rs *RPCLogStore) CleanupOlderThan(ctx context.Context, retention time.Duration) (int64, error) {
	return rs.CleanupBefore(ctx, time.Now().Add(-retention))
}

// CountBefore returns the number of RPC logs with CreatedAt strictly before the
// given time without deleting them.
func (rs *RPCLogStore) CountBefore(ctx context.Context, before time.Time) (int64, error) {
	var count int64
	err := rs.db.WithContext(ctx).
		Model(&model.RPCLog{}).
		Where("created_at < ?", before).
		Count(&count).Error
	if err != nil {
		return 0, classifyError(fmt.Errorf("store: count rpc logs before: %w", err))
	}
	return count, nil
}
