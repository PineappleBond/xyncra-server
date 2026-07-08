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

func TestNotificationLogStore_Save_Success(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	log := &model.NotificationLog{
		ID:        uid(),
		Seq:       1,
		Type:      "message",
		Payload:   []byte(`{"text":"hello"}`),
		CreatedAt: time.Now(),
	}
	require.NoError(t, db.NotificationLogs.Save(ctx, log))

	logs, err := db.NotificationLogs.List(ctx, NotificationLogFilter{})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, log.ID, logs[0].ID)
	assert.Equal(t, uint32(1), logs[0].Seq)
	assert.Equal(t, "message", logs[0].Type)
}

func TestNotificationLogStore_List_OrderDesc(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	l1 := &model.NotificationLog{ID: uid(), Seq: 1, Type: "message", CreatedAt: now.Add(-2 * time.Hour)}
	l2 := &model.NotificationLog{ID: uid(), Seq: 2, Type: "message", CreatedAt: now.Add(-1 * time.Hour)}
	l3 := &model.NotificationLog{ID: uid(), Seq: 3, Type: "message", CreatedAt: now}

	require.NoError(t, db.NotificationLogs.Save(ctx, l1))
	require.NoError(t, db.NotificationLogs.Save(ctx, l2))
	require.NoError(t, db.NotificationLogs.Save(ctx, l3))

	logs, err := db.NotificationLogs.List(ctx, NotificationLogFilter{})
	require.NoError(t, err)
	require.Len(t, logs, 3)
	// Ordered by CreatedAt DESC.
	assert.Equal(t, l3.ID, logs[0].ID)
	assert.Equal(t, l2.ID, logs[1].ID)
	assert.Equal(t, l1.ID, logs[2].ID)
}

func TestNotificationLogStore_List_FilterByType(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	l1 := &model.NotificationLog{ID: uid(), Seq: 1, Type: "message", CreatedAt: time.Now()}
	l2 := &model.NotificationLog{ID: uid(), Seq: 2, Type: "presence", CreatedAt: time.Now()}
	l3 := &model.NotificationLog{ID: uid(), Seq: 3, Type: "message", CreatedAt: time.Now()}

	require.NoError(t, db.NotificationLogs.Save(ctx, l1))
	require.NoError(t, db.NotificationLogs.Save(ctx, l2))
	require.NoError(t, db.NotificationLogs.Save(ctx, l3))

	logs, err := db.NotificationLogs.List(ctx, NotificationLogFilter{Type: "message"})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	for _, l := range logs {
		assert.Equal(t, "message", l.Type)
	}

	logs, err = db.NotificationLogs.List(ctx, NotificationLogFilter{Type: "presence"})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "presence", logs[0].Type)
}

func TestNotificationLogStore_List_FilterByTimeRange(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	l1 := &model.NotificationLog{ID: uid(), Seq: 1, Type: "message", CreatedAt: now.Add(-3 * time.Hour)}
	l2 := &model.NotificationLog{ID: uid(), Seq: 2, Type: "message", CreatedAt: now.Add(-1 * time.Hour)}
	l3 := &model.NotificationLog{ID: uid(), Seq: 3, Type: "message", CreatedAt: now}

	require.NoError(t, db.NotificationLogs.Save(ctx, l1))
	require.NoError(t, db.NotificationLogs.Save(ctx, l2))
	require.NoError(t, db.NotificationLogs.Save(ctx, l3))

	start := now.Add(-2 * time.Hour)
	logs, err := db.NotificationLogs.List(ctx, NotificationLogFilter{StartTime: &start})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	assert.Equal(t, l3.ID, logs[0].ID)
	assert.Equal(t, l2.ID, logs[1].ID)

	end := now.Add(-2 * time.Hour)
	logs, err = db.NotificationLogs.List(ctx, NotificationLogFilter{EndTime: &end})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, l1.ID, logs[0].ID)
}

func TestNotificationLogStore_ListBySeqRange(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	for i := uint32(1); i <= 10; i++ {
		l := &model.NotificationLog{ID: uid(), Seq: i, Type: "message", CreatedAt: time.Now()}
		require.NoError(t, db.NotificationLogs.Save(ctx, l))
	}

	// Range [3, 7] inclusive.
	logs, err := db.NotificationLogs.ListBySeqRange(ctx, 3, 7)
	require.NoError(t, err)
	require.Len(t, logs, 5)
	// Ordered by Seq ASC.
	assert.Equal(t, uint32(3), logs[0].Seq)
	assert.Equal(t, uint32(7), logs[4].Seq)

	// Range [1, 1].
	logs, err = db.NotificationLogs.ListBySeqRange(ctx, 1, 1)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, uint32(1), logs[0].Seq)

	// Range that includes everything.
	logs, err = db.NotificationLogs.ListBySeqRange(ctx, 0, 100)
	require.NoError(t, err)
	require.Len(t, logs, 10)

	// Empty range (start > end).
	logs, err = db.NotificationLogs.ListBySeqRange(ctx, 5, 3)
	require.NoError(t, err)
	assert.Empty(t, logs)
}

func TestNotificationLogStore_ExportCSV(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	l1 := &model.NotificationLog{
		ID:        "notif-1",
		Seq:       1,
		Type:      "message",
		CreatedAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
	l2 := &model.NotificationLog{
		ID:        "notif-2",
		Seq:       2,
		Type:      "presence",
		CreatedAt: time.Date(2024, 1, 15, 10, 1, 0, 0, time.UTC),
	}
	require.NoError(t, db.NotificationLogs.Save(ctx, l1))
	require.NoError(t, db.NotificationLogs.Save(ctx, l2))

	var buf bytes.Buffer
	require.NoError(t, db.NotificationLogs.ExportCSV(ctx, &buf, NotificationLogFilter{}))

	reader := csv.NewReader(strings.NewReader(buf.String()))
	records, err := reader.ReadAll()
	require.NoError(t, err)

	// Header + 2 rows.
	require.Len(t, records, 3)

	// Header.
	assert.Equal(t, []string{"id", "seq", "type", "created_at"}, records[0])

	// Records are DESC by CreatedAt, so l2 is first.
	assert.Equal(t, "notif-2", records[1][0])
	assert.Equal(t, "2", records[1][1])
	assert.Equal(t, "presence", records[1][2])

	assert.Equal(t, "notif-1", records[2][0])
	assert.Equal(t, "1", records[2][1])
	assert.Equal(t, "message", records[2][2])
}

func TestNotificationLogStore_ExportJSON(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	l1 := &model.NotificationLog{
		ID:   "notif-1",
		Seq:  1,
		Type: "message",
	}
	require.NoError(t, db.NotificationLogs.Save(ctx, l1))

	var buf bytes.Buffer
	require.NoError(t, db.NotificationLogs.ExportJSON(ctx, &buf, NotificationLogFilter{}))

	var logs []*model.NotificationLog
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logs))
	require.Len(t, logs, 1)
	assert.Equal(t, "notif-1", logs[0].ID)
	assert.Equal(t, uint32(1), logs[0].Seq)
	assert.Equal(t, "message", logs[0].Type)
}

func TestNotificationLogStore_CleanupBefore(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	l1 := &model.NotificationLog{ID: uid(), Seq: 1, Type: "message", CreatedAt: now.Add(-48 * time.Hour)}
	l2 := &model.NotificationLog{ID: uid(), Seq: 2, Type: "message", CreatedAt: now.Add(-1 * time.Hour)}
	l3 := &model.NotificationLog{ID: uid(), Seq: 3, Type: "message", CreatedAt: now}

	require.NoError(t, db.NotificationLogs.Save(ctx, l1))
	require.NoError(t, db.NotificationLogs.Save(ctx, l2))
	require.NoError(t, db.NotificationLogs.Save(ctx, l3))

	before := now.Add(-24 * time.Hour)
	deleted, err := db.NotificationLogs.CleanupBefore(ctx, before)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	remaining, err := db.NotificationLogs.List(ctx, NotificationLogFilter{})
	require.NoError(t, err)
	require.Len(t, remaining, 2)
	// l1 should be gone.
	for _, l := range remaining {
		assert.NotEqual(t, l1.ID, l.ID)
	}
}

func TestNotificationLogStore_GetLatestSeq(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	l1 := &model.NotificationLog{ID: uid(), Seq: 5, Type: "message", CreatedAt: time.Now()}
	l2 := &model.NotificationLog{ID: uid(), Seq: 10, Type: "message", CreatedAt: time.Now()}
	l3 := &model.NotificationLog{ID: uid(), Seq: 3, Type: "message", CreatedAt: time.Now()}

	require.NoError(t, db.NotificationLogs.Save(ctx, l1))
	require.NoError(t, db.NotificationLogs.Save(ctx, l2))
	require.NoError(t, db.NotificationLogs.Save(ctx, l3))

	seq, err := db.NotificationLogs.GetLatestSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(10), seq)
}

func TestNotificationLogStore_GetLatestSeq_Empty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	seq, err := db.NotificationLogs.GetLatestSeq(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), seq)
}

func TestNotificationLogStore_List_Limit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	for i := uint32(1); i <= 5; i++ {
		l := &model.NotificationLog{ID: uid(), Seq: i, Type: "message", CreatedAt: time.Now()}
		require.NoError(t, db.NotificationLogs.Save(ctx, l))
	}

	logs, err := db.NotificationLogs.List(ctx, NotificationLogFilter{Limit: 3})
	require.NoError(t, err)
	require.Len(t, logs, 3)
}

func TestNotificationLogStore_Save_DuplicateKey(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	logID := uid()
	l1 := &model.NotificationLog{ID: logID, Seq: 1, Type: "message", CreatedAt: time.Now()}
	require.NoError(t, db.NotificationLogs.Save(ctx, l1))

	l2 := &model.NotificationLog{ID: logID, Seq: 2, Type: "presence", CreatedAt: time.Now()}
	err := db.NotificationLogs.Save(ctx, l2)
	require.ErrorIs(t, err, ErrDuplicateKey)
}
