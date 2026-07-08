package store

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRPCLogStore_Save_Success(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	log := &model.RPCLog{
		ID:         uid(),
		RequestID:  uid(),
		Method:     "SendMessage",
		StatusCode: 0,
		Duration:   15 * time.Millisecond,
		CreatedAt:  time.Now(),
	}
	require.NoError(t, db.RPCLogs.Save(ctx, log))

	got, err := db.RPCLogs.GetByRequestID(ctx, log.RequestID)
	require.NoError(t, err)
	assert.Equal(t, log.ID, got.ID)
	assert.Equal(t, "SendMessage", got.Method)
}

func TestRPCLogStore_List_OrderDesc(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	l1 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M1", CreatedAt: now.Add(-2 * time.Hour)}
	l2 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M2", CreatedAt: now.Add(-1 * time.Hour)}
	l3 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M3", CreatedAt: now}

	require.NoError(t, db.RPCLogs.Save(ctx, l1))
	require.NoError(t, db.RPCLogs.Save(ctx, l2))
	require.NoError(t, db.RPCLogs.Save(ctx, l3))

	logs, err := db.RPCLogs.List(ctx, RPCLogFilter{})
	require.NoError(t, err)
	require.Len(t, logs, 3)
	// Ordered by CreatedAt DESC (newest first).
	assert.Equal(t, l3.ID, logs[0].ID)
	assert.Equal(t, l2.ID, logs[1].ID)
	assert.Equal(t, l1.ID, logs[2].ID)
}

func TestRPCLogStore_List_FilterByMethod(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	l1 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "SendMessage", CreatedAt: time.Now()}
	l2 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "GetMessages", CreatedAt: time.Now()}
	l3 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "SendMessage", CreatedAt: time.Now()}

	require.NoError(t, db.RPCLogs.Save(ctx, l1))
	require.NoError(t, db.RPCLogs.Save(ctx, l2))
	require.NoError(t, db.RPCLogs.Save(ctx, l3))

	logs, err := db.RPCLogs.List(ctx, RPCLogFilter{Method: "SendMessage"})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	for _, l := range logs {
		assert.Equal(t, "SendMessage", l.Method)
	}
}

func TestRPCLogStore_List_FilterByTimeRange(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	l1 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M1", CreatedAt: now.Add(-3 * time.Hour)}
	l2 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M2", CreatedAt: now.Add(-1 * time.Hour)}
	l3 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M3", CreatedAt: now}

	require.NoError(t, db.RPCLogs.Save(ctx, l1))
	require.NoError(t, db.RPCLogs.Save(ctx, l2))
	require.NoError(t, db.RPCLogs.Save(ctx, l3))

	start := now.Add(-2 * time.Hour)
	logs, err := db.RPCLogs.List(ctx, RPCLogFilter{StartTime: &start})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	assert.Equal(t, l3.ID, logs[0].ID)
	assert.Equal(t, l2.ID, logs[1].ID)

	end := now.Add(-2 * time.Hour)
	logs, err = db.RPCLogs.List(ctx, RPCLogFilter{EndTime: &end})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, l1.ID, logs[0].ID)
}

func TestRPCLogStore_List_FilterByStatusCode(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	l1 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M1", StatusCode: 0, CreatedAt: time.Now()}
	l2 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M2", StatusCode: -1, CreatedAt: time.Now()}
	l3 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M3", StatusCode: 0, CreatedAt: time.Now()}

	require.NoError(t, db.RPCLogs.Save(ctx, l1))
	require.NoError(t, db.RPCLogs.Save(ctx, l2))
	require.NoError(t, db.RPCLogs.Save(ctx, l3))

	code := 0
	logs, err := db.RPCLogs.List(ctx, RPCLogFilter{StatusCode: &code})
	require.NoError(t, err)
	require.Len(t, logs, 2)

	errCode := -1
	logs, err = db.RPCLogs.List(ctx, RPCLogFilter{StatusCode: &errCode})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, l2.ID, logs[0].ID)
}

func TestRPCLogStore_GetByRequestID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	log := &model.RPCLog{
		ID:         uid(),
		RequestID:  "req-123",
		Method:     "TestMethod",
		StatusCode: 0,
		CreatedAt:  time.Now(),
	}
	require.NoError(t, db.RPCLogs.Save(ctx, log))

	got, err := db.RPCLogs.GetByRequestID(ctx, "req-123")
	require.NoError(t, err)
	assert.Equal(t, log.ID, got.ID)
	assert.Equal(t, "req-123", got.RequestID)
}

func TestRPCLogStore_GetByRequestID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	_, err := db.RPCLogs.GetByRequestID(ctx, "nonexistent-request")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRPCLogStore_Aggregate_Stats(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	start := now.Add(-1 * time.Hour)
	end := now.Add(1 * time.Hour)

	// Create RPC logs for two methods.
	logs := []*model.RPCLog{
		{ID: uid(), RequestID: uid(), Method: "SendMessage", StatusCode: 0, Duration: 10 * time.Millisecond, CreatedAt: now},
		{ID: uid(), RequestID: uid(), Method: "SendMessage", StatusCode: 0, Duration: 20 * time.Millisecond, CreatedAt: now},
		{ID: uid(), RequestID: uid(), Method: "SendMessage", StatusCode: -1, Duration: 5 * time.Millisecond, CreatedAt: now},
		{ID: uid(), RequestID: uid(), Method: "GetMessages", StatusCode: 0, Duration: 50 * time.Millisecond, CreatedAt: now},
	}
	for _, l := range logs {
		require.NoError(t, db.RPCLogs.Save(ctx, l))
	}

	rows, err := db.RPCLogs.Aggregate(ctx, start, end)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// Find SendMessage row.
	var sendRow, getRow RPCAggregateRow
	for _, r := range rows {
		if r.Method == "SendMessage" {
			sendRow = r
		} else if r.Method == "GetMessages" {
			getRow = r
		}
	}

	assert.Equal(t, int64(3), sendRow.Count)
	assert.Equal(t, int64(2), sendRow.Success)
	assert.Equal(t, int64(1), sendRow.ErrorCount)
	// Avg duration: (10+20+5)/3 = 11.67ms (approx).
	assert.InDelta(t, 11.67, sendRow.AvgMs, 1.0)

	assert.Equal(t, int64(1), getRow.Count)
	assert.Equal(t, int64(1), getRow.Success)
	assert.Equal(t, int64(0), getRow.ErrorCount)
	assert.InDelta(t, 50.0, getRow.AvgMs, 1.0)
}

func TestRPCLogStore_ExportCSV(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	l1 := &model.RPCLog{
		ID:             "id-1",
		RequestID:      "req-1",
		Method:         "SendMessage",
		StatusCode:     0,
		ConversationID: "conv-1",
		Duration:       15 * time.Millisecond,
		ErrorMsg:       "",
		CreatedAt:      time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}
	l2 := &model.RPCLog{
		ID:             "id-2",
		RequestID:      "req-2",
		Method:         "GetMessages",
		StatusCode:     -1,
		ConversationID: "conv-2",
		Duration:       5 * time.Millisecond,
		ErrorMsg:       "timeout",
		CreatedAt:      time.Date(2024, 1, 15, 10, 31, 0, 0, time.UTC),
	}
	require.NoError(t, db.RPCLogs.Save(ctx, l1))
	require.NoError(t, db.RPCLogs.Save(ctx, l2))

	var buf bytes.Buffer
	require.NoError(t, db.RPCLogs.ExportCSV(ctx, &buf, RPCLogFilter{}))

	reader := csv.NewReader(strings.NewReader(buf.String()))
	records, err := reader.ReadAll()
	require.NoError(t, err)

	// Header + 2 data rows.
	require.Len(t, records, 3)

	// Verify header.
	assert.Equal(t, []string{"id", "request_id", "method", "status_code", "conversation_id", "duration_ms", "error", "created_at"}, records[0])

	// Records are ordered by CreatedAt DESC, so l2 (10:31) is first.
	assert.Equal(t, "id-2", records[1][0])
	assert.Equal(t, "req-2", records[1][1])
	assert.Equal(t, "GetMessages", records[1][2])
	assert.Equal(t, "-1", records[1][3])
	assert.Equal(t, "conv-2", records[1][4])
	assert.Equal(t, "5.000", records[1][5])
	assert.Equal(t, "timeout", records[1][6])

	assert.Equal(t, "id-1", records[2][0])
	assert.Equal(t, "SendMessage", records[2][2])
	assert.Equal(t, "0", records[2][3])
	assert.Equal(t, "15.000", records[2][5])
	assert.Equal(t, "", records[2][6])
}

func TestRPCLogStore_ExportJSON(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	l1 := &model.RPCLog{
		ID:         "id-1",
		RequestID:  "req-1",
		Method:     "SendMessage",
		StatusCode: 0,
		CreatedAt:  time.Now(),
	}
	require.NoError(t, db.RPCLogs.Save(ctx, l1))

	var buf bytes.Buffer
	require.NoError(t, db.RPCLogs.ExportJSON(ctx, &buf, RPCLogFilter{}))

	var logs []*model.RPCLog
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logs))
	require.Len(t, logs, 1)
	assert.Equal(t, "id-1", logs[0].ID)
	assert.Equal(t, "SendMessage", logs[0].Method)
}

func TestRPCLogStore_CleanupBefore(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	l1 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M1", CreatedAt: now.Add(-48 * time.Hour)}
	l2 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M2", CreatedAt: now.Add(-1 * time.Hour)}
	l3 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M3", CreatedAt: now}

	require.NoError(t, db.RPCLogs.Save(ctx, l1))
	require.NoError(t, db.RPCLogs.Save(ctx, l2))
	require.NoError(t, db.RPCLogs.Save(ctx, l3))

	// Delete everything before 24h ago → only l1 should be deleted.
	before := now.Add(-24 * time.Hour)
	deleted, err := db.RPCLogs.CleanupBefore(ctx, before)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	remaining, err := db.RPCLogs.List(ctx, RPCLogFilter{})
	require.NoError(t, err)
	require.Len(t, remaining, 2)
}

func TestRPCLogStore_CleanupOlderThan(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	old := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "Old", CreatedAt: now.Add(-72 * time.Hour)}
	recent := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "Recent", CreatedAt: now}

	require.NoError(t, db.RPCLogs.Save(ctx, old))
	require.NoError(t, db.RPCLogs.Save(ctx, recent))

	deleted, err := db.RPCLogs.CleanupOlderThan(ctx, 48*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	remaining, err := db.RPCLogs.List(ctx, RPCLogFilter{})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "Recent", remaining[0].Method)
}

func TestRPCLogStore_List_Limit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	for i := 0; i < 5; i++ {
		l := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M", CreatedAt: time.Now()}
		require.NoError(t, db.RPCLogs.Save(ctx, l))
	}

	logs, err := db.RPCLogs.List(ctx, RPCLogFilter{Limit: 3})
	require.NoError(t, err)
	require.Len(t, logs, 3)
}

func TestRPCLogStore_List_FilterByConversationID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	l1 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M1", ConversationID: "conv-A", CreatedAt: time.Now()}
	l2 := &model.RPCLog{ID: uid(), RequestID: uid(), Method: "M2", ConversationID: "conv-B", CreatedAt: time.Now()}

	require.NoError(t, db.RPCLogs.Save(ctx, l1))
	require.NoError(t, db.RPCLogs.Save(ctx, l2))

	logs, err := db.RPCLogs.List(ctx, RPCLogFilter{ConversationID: "conv-A"})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, l1.ID, logs[0].ID)
}

func TestRPCLogStore_Save_DuplicateKey(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	logID := uid()
	l1 := &model.RPCLog{ID: logID, RequestID: uid(), Method: "M1", CreatedAt: time.Now()}
	require.NoError(t, db.RPCLogs.Save(ctx, l1))

	l2 := &model.RPCLog{ID: logID, RequestID: uid(), Method: "M2", CreatedAt: time.Now()}
	err := db.RPCLogs.Save(ctx, l2)
	require.ErrorIs(t, err, ErrDuplicateKey)
}
