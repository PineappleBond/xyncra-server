package cleanup

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupSQLite creates an in-memory SQLite database, runs AutoMigrate, and
// returns a Store for testing. Mirrors the pattern in the store package.
func setupSQLite(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.NewDatabase(store.DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
	})
	require.NoError(t, err, "failed to open sqlite")

	s := store.New(db.DB())
	ctx := context.Background()
	require.NoError(t, s.AutoMigrate(ctx), "auto migrate failed")
	return s
}

// insertExpiredUserUpdates inserts a batch of user updates with CreatedAt set
// relative to the current time. Records older than 30 days are considered
// expired per D-016.
func insertExpiredUserUpdates(t *testing.T, s *store.Store, ctx context.Context, count int, age time.Duration) {
	t.Helper()
	now := time.Now().UTC()
	updates := make([]model.UserUpdate, count)
	for i := 0; i < count; i++ {
		updates[i] = model.UserUpdate{
			ID:        fmt.Sprintf("uu-cleanup-%d-%d", now.UnixNano(), i),
			UserID:    fmt.Sprintf("user-%d", i),
			Seq:       uint32(i + 1),
			Payload:   []byte(`{"test":true}`),
			CreatedAt: now.Add(-age),
		}
	}
	require.NoError(t, s.UserUpdates.Create(ctx, updates),
		"inserting %d user updates should succeed", count)
}

// ---------------------------------------------------------------------------
// TestUserUpdateCleaner_NormalCleanup
//
// Scenario 1: Expired records (>30 days) are removed after one tick.
// ---------------------------------------------------------------------------

// TestUserUpdateCleaner_NormalCleanup verifies that expired user updates
// (older than 30 days per D-016) are deleted when the cleanup runs. The log
// output should record the number of deleted records.
func TestUserUpdateCleaner_NormalCleanup(t *testing.T) {
	s := setupSQLite(t)
	ctx := context.Background()

	// Insert 5 expired records (31 days old) and 3 fresh records (5 days old).
	insertExpiredUserUpdates(t, s, ctx, 5, 31*24*time.Hour)
	insertExpiredUserUpdates(t, s, ctx, 3, 5*24*time.Hour)

	var buf bytes.Buffer
	logger := log.New(&buf, "[cleanup] ", 0)

	cleaner := NewUserUpdateCleaner(Config{
		Interval: 10 * time.Millisecond,
		Store:    s.UserUpdates,
		Logger:   logger,
	})

	runCtx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cleaner.Run(runCtx)
		close(done)
	}()

	// Wait for at least one tick plus some margin.
	time.Sleep(100 * time.Millisecond)

	// Capture log state BEFORE cancelling.
	logOutput := buf.String()

	cancel()
	<-done // ensure goroutine exits cleanly

	// Verify expired records were deleted and only fresh records remain.
	// Use CleanupExpiredBefore with a future time to count remaining records.
	totalRemaining, err := s.UserUpdates.CleanupExpiredBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err, "CleanupExpiredBefore should succeed")

	// The 3 fresh records (5 days old) should still exist.
	assert.Equal(t, int64(3), totalRemaining,
		"only 3 fresh records should remain after cleanup")

	// Verify log output mentions the cleanup.
	assert.True(t, strings.Contains(logOutput, "cleaned up"),
		"log should contain cleanup message, got: %s", logOutput)
	assert.True(t, strings.Contains(logOutput, "5"),
		"log should mention 5 deleted records, got: %s", logOutput)
}

// ---------------------------------------------------------------------------
// TestUserUpdateCleaner_NoExpiredData
//
// Scenario 2: No expired records exist; nothing should be deleted and no
// cleanup log message should appear.
// ---------------------------------------------------------------------------

// TestUserUpdateCleaner_NoExpiredData verifies that when all records are within
// the 30-day retention period, no data is deleted and no cleanup log message
// is emitted (per the Run implementation: deleted==0 produces no log).
func TestUserUpdateCleaner_NoExpiredData(t *testing.T) {
	s := setupSQLite(t)
	ctx := context.Background()

	// Insert only fresh records (5 days old, well within 30-day retention).
	insertExpiredUserUpdates(t, s, ctx, 4, 5*24*time.Hour)

	var buf bytes.Buffer
	logger := log.New(&buf, "[cleanup] ", 0)

	cleaner := NewUserUpdateCleaner(Config{
		Interval: 10 * time.Millisecond,
		Store:    s.UserUpdates,
		Logger:   logger,
	})

	runCtx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cleaner.Run(runCtx)
		close(done)
	}()

	// Wait for at least one tick.
	time.Sleep(100 * time.Millisecond)

	// Capture log state BEFORE cancelling to avoid race with context-canceled
	// errors being logged during shutdown.
	logOutput := buf.String()

	cancel()
	<-done

	// All 4 fresh records should still exist.
	// We verify by trying to clean up with a future cutoff.
	remaining, err := s.UserUpdates.CleanupExpiredBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err, "CleanupExpiredBefore should succeed")
	assert.Equal(t, int64(4), remaining, "all 4 fresh records should still exist")

	// No cleanup log message should have been emitted (deleted==0 => no log).
	assert.Empty(t, logOutput,
		"no log output expected when nothing is cleaned, got: %s", logOutput)
}

// ---------------------------------------------------------------------------
// TestUserUpdateCleaner_ContextCancellation
//
// Scenario 3: Cancelling the context causes Run to return without panicking or
// leaking goroutines.
// ---------------------------------------------------------------------------

// TestUserUpdateCleaner_ContextCancellation verifies that Run exits promptly
// when the context is cancelled, without panicking.
func TestUserUpdateCleaner_ContextCancellation(t *testing.T) {
	s := setupSQLite(t)

	var buf bytes.Buffer
	logger := log.New(&buf, "[cleanup] ", 0)

	cleaner := NewUserUpdateCleaner(Config{
		Interval: 50 * time.Millisecond,
		Store:    s.UserUpdates,
		Logger:   logger,
	})

	runCtx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cleaner.Run(runCtx)
		close(done)
	}()

	// Cancel quickly, before any tick fires.
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Run should return within a reasonable time.
	select {
	case <-done:
		// Success: goroutine exited.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// TestUserUpdateCleaner_CleanupFailure
//
// Scenario 4: When Store.CleanupExpired returns an error, the error is logged
// and the goroutine continues running (fire-and-forget per D-007 philosophy).
// ---------------------------------------------------------------------------

// TestUserUpdateCleaner_CleanupFailure verifies that a cleanup failure is
// logged and does not terminate the cleanup loop. We simulate failure by using
// a store backed by a closed database connection.
func TestUserUpdateCleaner_CleanupFailure(t *testing.T) {
	// Create a database and then close its underlying connection so that
	// CleanupExpired will fail.
	db, err := store.NewDatabase(store.DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:%s_closed?mode=memory&cache=shared", t.Name()),
	})
	require.NoError(t, err, "failed to open sqlite for failure test")

	s := store.New(db.DB())
	ctx := context.Background()
	require.NoError(t, s.AutoMigrate(ctx), "auto migrate failed")

	// Close the underlying database to force cleanup errors.
	sqlDB, err := db.DB().DB()
	require.NoError(t, err, "failed to get underlying db")
	require.NoError(t, sqlDB.Close(), "failed to close db")

	var buf bytes.Buffer
	logger := log.New(&buf, "[cleanup] ", 0)

	cleaner := NewUserUpdateCleaner(Config{
		Interval: 10 * time.Millisecond,
		Store:    s.UserUpdates,
		Logger:   logger,
	})

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		cleaner.Run(runCtx)
		close(done)
	}()

	// Wait for multiple ticks to exercise that the loop keeps running.
	time.Sleep(150 * time.Millisecond)

	// The loop should still be running (not crashed). Verify by checking that
	// the goroutine hasn't exited.
	select {
	case <-done:
		t.Fatal("Run exited prematurely after cleanup failure; should keep running")
	default:
		// Good: goroutine still alive.
	}

	cancel()
	<-done

	// Verify the error was logged (read buffer after goroutine exits to avoid
	// data race on bytes.Buffer).
	logOutput := buf.String()
	assert.True(t, strings.Contains(logOutput, "cleanup failed"),
		"log should contain 'cleanup failed', got: %s", logOutput)
}

// ---------------------------------------------------------------------------
// TestUserUpdateCleaner_DefaultConfig
//
// Scenario 5: When Interval is not specified (zero value), the cleaner uses
// DefaultInterval (1 hour).
// ---------------------------------------------------------------------------

// TestUserUpdateCleaner_DefaultConfig verifies that NewUserUpdateCleaner
// applies DefaultInterval (1 hour) when Config.Interval is zero.
func TestUserUpdateCleaner_DefaultConfig(t *testing.T) {
	s := setupSQLite(t)

	cleaner := NewUserUpdateCleaner(Config{
		Store: s.UserUpdates,
		// Interval intentionally omitted (zero value).
	})

	// We cannot directly inspect the unexported config field, but we can
	// verify the cleaner was created successfully and uses DefaultInterval by
	// observing that Run blocks (1-hour interval means no immediate tick).
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		cleaner.Run(runCtx)
		close(done)
	}()

	// Wait a short time; Run should NOT have produced any cleanup activity
	// because the first tick is 1 hour away.
	time.Sleep(50 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("Run should not have exited")
	default:
		// Good: still running.
	}

	cancel()

	select {
	case <-done:
		// Good: exited cleanly after cancel.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2 seconds after cancellation")
	}

	// Verify the DefaultInterval constant is 1 hour.
	assert.Equal(t, 1*time.Hour, DefaultInterval,
		"DefaultInterval should be 1 hour")

	t.Run("NegativeInterval", func(t *testing.T) {
		cleaner := NewUserUpdateCleaner(Config{
			Interval: -5 * time.Second,
			Store:    s.UserUpdates,
		})

		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan struct{})
		go func() {
			cleaner.Run(runCtx)
			close(done)
		}()

		// Negative interval should be replaced with DefaultInterval (1 hour),
		// so no tick should fire within 50ms.
		time.Sleep(50 * time.Millisecond)

		select {
		case <-done:
			t.Fatal("Run should not have exited")
		default:
			// Good: still running, meaning DefaultInterval was applied.
		}

		cancel()

		select {
		case <-done:
			// Good: exited cleanly.
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not exit within 2 seconds after cancellation")
		}
	})
}

// ---------------------------------------------------------------------------
// TestUserUpdateCleaner_NilLogger
//
// Scenario 6: When Logger is nil, NewUserUpdateCleaner creates a default
// stderr logger and the cleaner runs without panicking.
// ---------------------------------------------------------------------------

// TestUserUpdateCleaner_NilLogger verifies that passing Logger: nil does not
// cause a panic and that the cleaner uses a default stderr logger.
func TestUserUpdateCleaner_NilLogger(t *testing.T) {
	s := setupSQLite(t)
	ctx := context.Background()

	// Insert an expired record so runOnce produces log output, exercising
	// the default logger path.
	insertExpiredUserUpdates(t, s, ctx, 1, 31*24*time.Hour)

	cleaner := NewUserUpdateCleaner(Config{
		Interval: 10 * time.Millisecond,
		Store:    s.UserUpdates,
		Logger:   nil, // should use default stderr logger
	})

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		cleaner.Run(runCtx)
		close(done)
	}()

	// Wait for at least one tick.
	time.Sleep(100 * time.Millisecond)

	// The goroutine should still be running (no panic from nil logger).
	select {
	case <-done:
		t.Fatal("Run exited prematurely; nil Logger should not cause panic")
	default:
		// Good: still running.
	}

	cancel()

	select {
	case <-done:
		// Good: exited cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2 seconds after cancellation")
	}
}
