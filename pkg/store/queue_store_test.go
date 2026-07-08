package store

import (
	"context"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueueStore_Save_Success(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	task := &model.RetryTask{
		ID:          uid(),
		Method:      "SendMessage",
		Params:      []byte(`{"key":"value"}`),
		Attempt:     0,
		MaxAttempts: 5,
		NextRetry:   time.Now().Add(-1 * time.Second),
		Status:      "pending",
	}
	require.NoError(t, db.Queue.Save(ctx, task))

	tasks, err := db.Queue.ListPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, task.ID, tasks[0].ID)
	assert.Equal(t, "SendMessage", tasks[0].Method)
}

func TestQueueStore_ListPending_OrderAndFilter(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	// Create 3 pending tasks with different NextRetry times (all in the past or now).
	t1 := &model.RetryTask{ID: uid(), Method: "M1", NextRetry: now.Add(-3 * time.Second), Status: "pending"}
	t2 := &model.RetryTask{ID: uid(), Method: "M2", NextRetry: now.Add(-1 * time.Second), Status: "pending"}
	t3 := &model.RetryTask{ID: uid(), Method: "M3", NextRetry: now, Status: "pending"}

	require.NoError(t, db.Queue.Save(ctx, t1))
	require.NoError(t, db.Queue.Save(ctx, t2))
	require.NoError(t, db.Queue.Save(ctx, t3))

	tasks, err := db.Queue.ListPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 3)
	// Should be ordered by NextRetry ASC (soonest first).
	assert.Equal(t, t1.ID, tasks[0].ID)
	assert.Equal(t, t2.ID, tasks[1].ID)
	assert.Equal(t, t3.ID, tasks[2].ID)
}

func TestQueueStore_ListPending_ExcludesFuture(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	// One task in the past (should appear), one far in the future (should not).
	past := &model.RetryTask{ID: uid(), Method: "Past", NextRetry: now.Add(-1 * time.Minute), Status: "pending"}
	future := &model.RetryTask{ID: uid(), Method: "Future", NextRetry: now.Add(1 * time.Hour), Status: "pending"}

	require.NoError(t, db.Queue.Save(ctx, past))
	require.NoError(t, db.Queue.Save(ctx, future))

	tasks, err := db.Queue.ListPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, past.ID, tasks[0].ID)
}

func TestQueueStore_ListPending_ExcludesNonPending(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	pending := &model.RetryTask{ID: uid(), Method: "Pending", NextRetry: now.Add(-1 * time.Second), Status: "pending"}
	failed := &model.RetryTask{ID: uid(), Method: "Failed", NextRetry: now.Add(-1 * time.Second), Status: "failed"}

	require.NoError(t, db.Queue.Save(ctx, pending))
	require.NoError(t, db.Queue.Save(ctx, failed))

	tasks, err := db.Queue.ListPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, pending.ID, tasks[0].ID)
}

func TestQueueStore_Update_Success(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	task := &model.RetryTask{
		ID:          uid(),
		Method:      "TestMethod",
		Attempt:     0,
		MaxAttempts: 3,
		NextRetry:   time.Now().Add(-1 * time.Second),
		Status:      "pending",
	}
	require.NoError(t, db.Queue.Save(ctx, task))

	// Simulate a retry: increment attempt, update next retry and last error.
	task.Attempt = 1
	task.NextRetry = time.Now().Add(-1 * time.Second) // Keep it in the past so ListPending finds it.
	task.LastError = "connection timeout"
	require.NoError(t, db.Queue.Update(ctx, task))

	tasks, err := db.Queue.ListPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, 1, tasks[0].Attempt)
	assert.Equal(t, "connection timeout", tasks[0].LastError)
}

func TestQueueStore_MarkFailed_NotInPending(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	// MarkFailed on a non-existent task → ErrNotFound.
	err := db.Queue.MarkFailed(ctx, "nonexistent-id", "some error")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestQueueStore_MarkFailed_Success(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	task := &model.RetryTask{
		ID:        uid(),
		Method:    "TestMethod",
		NextRetry: time.Now().Add(-1 * time.Second),
		Status:    "pending",
	}
	require.NoError(t, db.Queue.Save(ctx, task))

	require.NoError(t, db.Queue.MarkFailed(ctx, task.ID, "permanent failure"))

	// Should no longer appear in pending list.
	tasks, err := db.Queue.ListPending(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, tasks)

	// Count should reflect the failed status.
	failedCount, err := db.Queue.Count(ctx, "failed")
	require.NoError(t, err)
	assert.Equal(t, int64(1), failedCount)
}

func TestQueueStore_Delete(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	task := &model.RetryTask{
		ID:        uid(),
		Method:    "TestMethod",
		NextRetry: time.Now(),
		Status:    "pending",
	}
	require.NoError(t, db.Queue.Save(ctx, task))

	require.NoError(t, db.Queue.Delete(ctx, task.ID))

	count, err := db.Queue.Count(ctx, "pending")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestQueueStore_Delete_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	err := db.Queue.Delete(ctx, "nonexistent-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestQueueStore_Count(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	// Empty → 0.
	count, err := db.Queue.Count(ctx, "pending")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	// Add some tasks.
	t1 := &model.RetryTask{ID: uid(), Method: "M1", NextRetry: time.Now(), Status: "pending"}
	t2 := &model.RetryTask{ID: uid(), Method: "M2", NextRetry: time.Now(), Status: "pending"}
	t3 := &model.RetryTask{ID: uid(), Method: "M3", NextRetry: time.Now(), Status: "failed"}
	require.NoError(t, db.Queue.Save(ctx, t1))
	require.NoError(t, db.Queue.Save(ctx, t2))
	require.NoError(t, db.Queue.Save(ctx, t3))

	pendingCount, err := db.Queue.Count(ctx, "pending")
	require.NoError(t, err)
	assert.Equal(t, int64(2), pendingCount)

	failedCount, err := db.Queue.Count(ctx, "failed")
	require.NoError(t, err)
	assert.Equal(t, int64(1), failedCount)

	unknownCount, err := db.Queue.Count(ctx, "unknown")
	require.NoError(t, err)
	assert.Equal(t, int64(0), unknownCount)
}

func TestQueueStore_ListPending_Limit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	now := time.Now()
	for i := 0; i < 5; i++ {
		task := &model.RetryTask{
			ID:        uid(),
			Method:    "Method",
			NextRetry: now.Add(-time.Duration(i) * time.Second),
			Status:    "pending",
		}
		require.NoError(t, db.Queue.Save(ctx, task))
	}

	tasks, err := db.Queue.ListPending(ctx, 3)
	require.NoError(t, err)
	require.Len(t, tasks, 3)
}

func TestQueueStore_Save_DuplicateKey(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cleanAll(t, db, ctx)

	taskID := uid()
	task1 := &model.RetryTask{ID: taskID, Method: "M1", Status: "pending"}
	require.NoError(t, db.Queue.Save(ctx, task1))

	task2 := &model.RetryTask{ID: taskID, Method: "M2", Status: "pending"}
	err := db.Queue.Save(ctx, task2)
	require.ErrorIs(t, err, ErrDuplicateKey)
}
