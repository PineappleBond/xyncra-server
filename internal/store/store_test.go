package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"gorm.io/gorm"
)

// now is a fixed time used across tests to avoid zero-value datetime issues with MySQL.
var testNow = time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)

// setupSQLite creates an in-memory SQLite database, runs AutoMigrate, and
// returns a Store for testing.
func setupSQLite(t *testing.T) *Store {
	t.Helper()
	db, err := NewDatabase(DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	s := New(db.DB())
	ctx := context.Background()
	if err := s.AutoMigrate(ctx); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return s
}

// setupPostgreSQL connects to the local Docker PostgreSQL instance.
func setupPostgreSQL(t *testing.T) *Store {
	t.Helper()
	db, err := NewDatabase(DatabaseConfig{
		Driver: "postgres",
		DSN:    "host=localhost port=5432 user=sequify password=sequify dbname=sequify sslmode=disable",
	})
	if err != nil {
		t.Skipf("postgresql not available: %v", err)
	}
	s := New(db.DB())
	ctx := context.Background()
	if err := s.AutoMigrate(ctx); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	cleanAll(t, s, ctx)
	return s
}

// setupMySQL connects to the local Docker MySQL instance.
func setupMySQL(t *testing.T) *Store {
	t.Helper()
	db, err := NewDatabase(DatabaseConfig{
		Driver: "mysql",
		DSN:    "sequify:sequify@tcp(localhost:3306)/sequify?charset=utf8mb4&parseTime=True",
	})
	if err != nil {
		t.Skipf("mysql not available: %v", err)
	}
	s := New(db.DB())
	ctx := context.Background()
	if err := s.AutoMigrate(ctx); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	cleanAll(t, s, ctx)
	return s
}

// cleanAll removes all test data from the store.
func cleanAll(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	s.db.WithContext(ctx).Where("1=1").Unscoped().Delete(&model.UserUpdate{})
	s.db.WithContext(ctx).Where("1=1").Unscoped().Delete(&model.Message{})
	s.db.WithContext(ctx).Where("1=1").Unscoped().Delete(&model.Conversation{})
}

// newTestConv creates a conversation with all required time fields set.
func newTestConv(id, uid1, uid2, typ, title string) *model.Conversation {
	return &model.Conversation{
		ID: id, UserID1: uid1, UserID2: uid2, Type: typ, Title: title,
		CreatedAt: testNow, UpdatedAt: testNow, LastMessageAt: testNow,
	}
}

// runOnAllDatabases runs the given test function against all three databases.
func runOnAllDatabases(t *testing.T, fn func(t *testing.T, s *Store)) {
	t.Helper()
	t.Run("SQLite", func(t *testing.T) {
		s := setupSQLite(t)
		fn(t, s)
	})
	t.Run("PostgreSQL", func(t *testing.T) {
		s := setupPostgreSQL(t)
		fn(t, s)
	})
	t.Run("MySQL", func(t *testing.T) {
		s := setupMySQL(t)
		fn(t, s)
	})
}

// --- NewDatabase tests ---

func TestNewDatabase(t *testing.T) {
	t.Run("invalid driver", func(t *testing.T) {
		_, err := NewDatabase(DatabaseConfig{Driver: "unknown", DSN: "foo"})
		if err == nil {
			t.Fatal("expected error for unknown driver")
		}
	})
	t.Run("sqlite opens successfully", func(t *testing.T) {
		db, err := NewDatabase(DatabaseConfig{Driver: "sqlite", DSN: "file::memory:?cache=shared"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if db == nil {
			t.Fatal("expected non-nil database")
		}
	})
	t.Run("default pool values applied", func(t *testing.T) {
		db, err := NewDatabase(DatabaseConfig{Driver: "sqlite", DSN: "file::memory:?cache=shared"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sqlDB, _ := db.DB().DB()
		if got := sqlDB.Stats().MaxOpenConnections; got != 25 {
			t.Fatalf("expected MaxOpenConns 25, got %d", got)
		}
	})
}

// --- Conversation tests ---

func TestConversationCRUD(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		conv := newTestConv("conv-test-001", "user-alice", "user-bob", "1-on-1", "Alice & Bob")

		// Create
		if err := s.Conversations.Create(ctx, conv); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		// Get
		got, err := s.Conversations.Get(ctx, "conv-test-001")
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if got.Title != "Alice & Bob" {
			t.Fatalf("expected title 'Alice & Bob', got %q", got.Title)
		}

		// Get not found
		_, err = s.Conversations.Get(ctx, "nonexistent")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}

		// Update
		got.Title = "Updated"
		if err := s.Conversations.Update(ctx, got); err != nil {
			t.Fatalf("update failed: %v", err)
		}
		got2, _ := s.Conversations.Get(ctx, "conv-test-001")
		if got2.Title != "Updated" {
			t.Fatalf("update not persisted, title = %q", got2.Title)
		}

		// UpdateLastMessage
		now := time.Now().Truncate(time.Second)
		if err := s.Conversations.UpdateLastMessage(ctx, "conv-test-001", now, 42); err != nil {
			t.Fatalf("update last message failed: %v", err)
		}
		got3, _ := s.Conversations.Get(ctx, "conv-test-001")
		if got3.LastProcessedMessageID != 42 {
			t.Fatalf("expected LastProcessedMessageID 42, got %d", got3.LastProcessedMessageID)
		}

		// Delete
		if err := s.Conversations.Delete(ctx, "conv-test-001"); err != nil {
			t.Fatalf("delete failed: %v", err)
		}
		_, err = s.Conversations.Get(ctx, "conv-test-001")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound after delete, got %v", err)
		}
	})
}

func TestConversationGetByUser(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		convs := []*model.Conversation{
			newTestConv("c1", "alice", "bob", "1-on-1", ""),
			newTestConv("c2", "alice", "charlie", "1-on-1", ""),
			newTestConv("c3", "bob", "dave", "1-on-1", ""),
		}
		convs[0].LastMessageAt = time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
		convs[1].LastMessageAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		convs[2].LastMessageAt = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

		for _, c := range convs {
			if err := s.Conversations.Create(ctx, c); err != nil {
				t.Fatalf("create failed: %v", err)
			}
		}

		got, err := s.Conversations.GetByUser(ctx, "alice", 0, 10)
		if err != nil {
			t.Fatalf("GetByUser failed: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 conversations for alice, got %d", len(got))
		}
		// Should be ordered by LastMessageAt DESC
		if got[0].ID != "c1" {
			t.Fatalf("expected first conv to be c1 (most recent), got %s", got[0].ID)
		}
	})
}

func TestConversationSearchByTitle(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("cs1", "alice", "bob", "group", "Project Alpha"))
		s.Conversations.Create(ctx, newTestConv("cs2", "alice", "charlie", "group", "Project Beta"))
		s.Conversations.Create(ctx, newTestConv("cs3", "alice", "dave", "group", "Random"))

		got, err := s.Conversations.SearchByTitle(ctx, "alice", "Project", 10)
		if err != nil {
			t.Fatalf("SearchByTitle failed: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 results, got %d", len(got))
		}

		// Empty title returns empty
		got2, err := s.Conversations.SearchByTitle(ctx, "alice", "", 10)
		if err != nil {
			t.Fatalf("SearchByTitle empty failed: %v", err)
		}
		if len(got2) != 0 {
			t.Fatalf("expected 0 results for empty title, got %d", len(got2))
		}
	})
}

func TestConversationRestore(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("cr1", "alice", "bob", "1-on-1", ""))

		// Soft delete
		if err := s.Conversations.Delete(ctx, "cr1"); err != nil {
			t.Fatalf("delete failed: %v", err)
		}

		// Should not be found normally
		_, err := s.Conversations.Get(ctx, "cr1")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound after delete, got %v", err)
		}

		// Restore
		if err := s.Conversations.Restore(ctx, "cr1"); err != nil {
			t.Fatalf("restore failed: %v", err)
		}

		// Should be found again
		got, err := s.Conversations.Get(ctx, "cr1")
		if err != nil {
			t.Fatalf("get after restore failed: %v", err)
		}
		if got.ID != "cr1" {
			t.Fatalf("expected cr1, got %s", got.ID)
		}

		// Restore non-existent
		err = s.Conversations.Restore(ctx, "nonexistent")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound for nonexistent restore, got %v", err)
		}
	})
}

// TestConversationGetByUsers_HappyPath verifies that GetByUsers returns the
// correct 1-on-1 conversation between two users.
func TestConversationGetByUsers_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("gbu-happy-1", "alice", "bob", "1-on-1", "Alice & Bob")
		if err := s.Conversations.Create(ctx, conv); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		got, err := s.Conversations.GetByUsers(ctx, "alice", "bob")
		if err != nil {
			t.Fatalf("GetByUsers failed: %v", err)
		}
		if got.ID != "gbu-happy-1" {
			t.Fatalf("expected conv gbu-happy-1, got %s", got.ID)
		}
		if got.Title != "Alice & Bob" {
			t.Fatalf("expected title 'Alice & Bob', got %q", got.Title)
		}
	})
}

// TestConversationGetByUsers_UserOrderIndependent verifies that GetByUsers
// returns the same conversation regardless of the order of the user arguments.
func TestConversationGetByUsers_UserOrderIndependent(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("gbu-order-1", "alice", "bob", "1-on-1", "Chat")
		if err := s.Conversations.Create(ctx, conv); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		got1, err := s.Conversations.GetByUsers(ctx, "alice", "bob")
		if err != nil {
			t.Fatalf("GetByUsers(alice,bob) failed: %v", err)
		}
		got2, err := s.Conversations.GetByUsers(ctx, "bob", "alice")
		if err != nil {
			t.Fatalf("GetByUsers(bob,alice) failed: %v", err)
		}
		if got1.ID != got2.ID {
			t.Fatalf("expected same conv for both orderings, got %s and %s", got1.ID, got2.ID)
		}
	})
}

// TestConversationGetByUsers_NotFound verifies that GetByUsers returns
// ErrNotFound when no matching conversation exists.
func TestConversationGetByUsers_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		_, err := s.Conversations.GetByUsers(ctx, "alice", "bob")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

// TestConversationGetByUsers_SoftDeletedExcluded verifies that a soft-deleted
// conversation is not returned by GetByUsers.
func TestConversationGetByUsers_SoftDeletedExcluded(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("gbu-del-1", "alice", "bob", "1-on-1", "Chat")
		if err := s.Conversations.Create(ctx, conv); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		if err := s.Conversations.Delete(ctx, "gbu-del-1"); err != nil {
			t.Fatalf("delete failed: %v", err)
		}

		_, err := s.Conversations.GetByUsers(ctx, "alice", "bob")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound for soft-deleted conv, got %v", err)
		}
	})
}

// --- Message tests ---

func TestMessageCRUD(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		s.Conversations.Create(ctx, newTestConv("conv-msg-001", "alice", "", "group", "Test"))

		msg := &model.Message{
			ID:              "msg-001",
			ClientMessageID: "client-msg-001",
			ConversationID:  "conv-msg-001",
			MessageID:       1,
			SenderID:        "alice",
			Content:         "Hello",
			CreatedAt:       testNow,
		}

		// Create
		if err := s.Messages.Create(ctx, msg); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		// Get
		got, err := s.Messages.Get(ctx, "msg-001")
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if got.Content != "Hello" {
			t.Fatalf("expected content 'Hello', got %q", got.Content)
		}

		// GetByClientMessageID
		got2, err := s.Messages.GetByClientMessageID(ctx, "client-msg-001")
		if err != nil {
			t.Fatalf("get by client_message_id failed: %v", err)
		}
		if got2.ID != "msg-001" {
			t.Fatalf("expected msg-001, got %s", got2.ID)
		}

		// Duplicate client_message_id
		dup := &model.Message{
			ID: "msg-002", ClientMessageID: "client-msg-001",
			ConversationID: "conv-msg-001", MessageID: 2, SenderID: "alice", Content: "Dup",
			CreatedAt: testNow,
		}
		err = s.Messages.Create(ctx, dup)
		if err != ErrDuplicateKey {
			t.Fatalf("expected ErrDuplicateKey, got %v", err)
		}

		// Delete
		if err := s.Messages.Delete(ctx, "msg-001"); err != nil {
			t.Fatalf("delete failed: %v", err)
		}
		_, err = s.Messages.Get(ctx, "msg-001")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound after delete, got %v", err)
		}
	})
}

func TestMessageListByConversation(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("conv-list", "alice", "", "group", ""))

		for i := uint32(1); i <= 5; i++ {
			s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-list-%d", i), ClientMessageID: fmt.Sprintf("client-%d", i),
				ConversationID: "conv-list", MessageID: i, SenderID: "alice",
				Content: fmt.Sprintf("msg %d", i), CreatedAt: testNow,
			})
		}

		// List from MessageID > 2
		msgs, err := s.Messages.ListByConversation(ctx, "conv-list", 2, 10)
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages (id > 2), got %d", len(msgs))
		}
		if msgs[0].MessageID != 3 {
			t.Fatalf("expected first message ID 3, got %d", msgs[0].MessageID)
		}
	})
}

func TestMessageSearchByConversation(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("conv-search", "alice", "", "group", ""))

		for i, content := range []string{"hello world", "goodbye world", "hello again"} {
			s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("ms-%d", i+1), ClientMessageID: fmt.Sprintf("ms-client-%d", i+1),
				ConversationID: "conv-search", MessageID: uint32(i + 1), SenderID: "alice",
				Content: content, CreatedAt: testNow,
			})
		}

		msgs, err := s.Messages.SearchByConversation(ctx, "conv-search", "hello", 0, 10)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("expected 2 results, got %d", len(msgs))
		}
	})
}

func TestMessageListByTimeRange(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("conv-tr", "alice", "", "group", ""))

		times := []time.Time{
			time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		}
		for i, tm := range times {
			s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("tr-%d", i+1), ClientMessageID: fmt.Sprintf("tr-client-%d", i+1),
				ConversationID: "conv-tr", MessageID: uint32(i + 1), SenderID: "alice",
				Content: fmt.Sprintf("msg %d", i+1), CreatedAt: tm,
			})
		}

		start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
		msgs, err := s.Messages.ListByTimeRange(ctx, "conv-tr", start, end, 10)
		if err != nil {
			t.Fatalf("list by time range failed: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages in range, got %d", len(msgs))
		}
	})
}

func TestMessageRestore(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("conv-mr", "alice", "", "group", ""))
		s.Messages.Create(ctx, &model.Message{
			ID: "mr-1", ClientMessageID: "mr-client-1",
			ConversationID: "conv-mr", MessageID: 1, SenderID: "alice",
			Content: "test", CreatedAt: testNow,
		})

		// Soft delete
		if err := s.Messages.Delete(ctx, "mr-1"); err != nil {
			t.Fatalf("delete failed: %v", err)
		}

		// Restore
		if err := s.Messages.Restore(ctx, "mr-1"); err != nil {
			t.Fatalf("restore failed: %v", err)
		}

		got, err := s.Messages.Get(ctx, "mr-1")
		if err != nil {
			t.Fatalf("get after restore failed: %v", err)
		}
		if got.ID != "mr-1" {
			t.Fatalf("expected mr-1, got %s", got.ID)
		}
	})
}

func TestMessageDeleteByConversation(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("conv-dc", "alice", "", "group", ""))

		for i := uint32(1); i <= 3; i++ {
			s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("dc-%d", i), ClientMessageID: fmt.Sprintf("dc-client-%d", i),
				ConversationID: "conv-dc", MessageID: i, SenderID: "alice",
				Content: fmt.Sprintf("msg %d", i), CreatedAt: testNow,
			})
		}

		if err := s.Messages.DeleteByConversation(ctx, "conv-dc"); err != nil {
			t.Fatalf("delete by conversation failed: %v", err)
		}

		msgs, err := s.Messages.ListByConversation(ctx, "conv-dc", 0, 100)
		if err != nil {
			t.Fatalf("list after delete failed: %v", err)
		}
		if len(msgs) != 0 {
			t.Fatalf("expected 0 messages, got %d", len(msgs))
		}
	})
}

func TestMessageCountUnread(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("conv-cu", "alice", "", "group", ""))

		for i := uint32(1); i <= 5; i++ {
			s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("cu-%d", i), ClientMessageID: fmt.Sprintf("cu-client-%d", i),
				ConversationID: "conv-cu", MessageID: i, SenderID: "alice",
				Content: fmt.Sprintf("msg %d", i), CreatedAt: testNow,
			})
		}

		count, err := s.Messages.CountUnread(ctx, "conv-cu", 3)
		if err != nil {
			t.Fatalf("count unread failed: %v", err)
		}
		if count != 2 {
			t.Fatalf("expected 2 unread, got %d", count)
		}
	})
}

// --- UserUpdate tests ---

func TestUserUpdateCRUD(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		updates := []model.UserUpdate{
			{ID: "uu-1", UserID: "alice", Seq: 1, Payload: []byte(`{"type":"msg"}`), CreatedAt: testNow},
			{ID: "uu-2", UserID: "alice", Seq: 2, Payload: []byte(`{"type":"read"}`), CreatedAt: testNow},
			{ID: "uu-3", UserID: "alice", Seq: 3, Payload: []byte(`{"type":"msg"}`), CreatedAt: testNow},
			{ID: "uu-4", UserID: "bob", Seq: 1, Payload: []byte(`{"type":"msg"}`), CreatedAt: testNow},
		}

		// Create batch
		if err := s.UserUpdates.Create(ctx, updates); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		// ListByUser for alice, after seq 1
		got, err := s.UserUpdates.ListByUser(ctx, "alice", 1, 10)
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 updates for alice (seq > 1), got %d", len(got))
		}

		// GetLatestSeq
		seq, err := s.UserUpdates.GetLatestSeq(ctx, "alice")
		if err != nil {
			t.Fatalf("get latest seq failed: %v", err)
		}
		if seq != 3 {
			t.Fatalf("expected latest seq 3, got %d", seq)
		}

		// GetLatestSeq for non-existent user
		seq2, err := s.UserUpdates.GetLatestSeq(ctx, "nobody")
		if err != nil {
			t.Fatalf("get latest seq failed: %v", err)
		}
		if seq2 != 0 {
			t.Fatalf("expected 0 for non-existent user, got %d", seq2)
		}
	})
}

func TestUserUpdateCleanupExpired(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		oldTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		newTime := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

		updates := []model.UserUpdate{
			{ID: "clean-1", UserID: "alice", Seq: 1, Payload: []byte(`{}`), CreatedAt: oldTime},
			{ID: "clean-2", UserID: "alice", Seq: 2, Payload: []byte(`{}`), CreatedAt: oldTime},
			{ID: "clean-3", UserID: "alice", Seq: 3, Payload: []byte(`{}`), CreatedAt: newTime},
		}
		if err := s.UserUpdates.Create(ctx, updates); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		deleted, err := s.UserUpdates.CleanupExpiredBefore(ctx, cutoff)
		if err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
		if deleted != 2 {
			t.Fatalf("expected 2 deleted, got %d", deleted)
		}

		// Verify remaining
		got, err := s.UserUpdates.ListByUser(ctx, "alice", 0, 10)
		if err != nil {
			t.Fatalf("list after cleanup failed: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 remaining, got %d", len(got))
		}
	})
}

// --- SendMessage (transaction) tests ---

func TestSendMessage(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		// Create conversation first
		conv := newTestConv("conv-send", "alice", "bob", "1-on-1", "Chat")
		s.Conversations.Create(ctx, conv)

		msg := &model.Message{
			ID: "msg-send-1", ClientMessageID: "client-send-1",
			ConversationID: "conv-send",
			SenderID:       "alice", Content: "Hello Bob!",
			CreatedAt: testNow,
		}

		result, err := s.SendMessage(ctx, msg, []string{"alice", "bob"})
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		// Verify MessageID was allocated
		if result.Message.MessageID != 1 {
			t.Fatalf("expected MessageID 1, got %d", result.Message.MessageID)
		}

		// Verify message exists
		gotMsg, err := s.Messages.Get(ctx, "msg-send-1")
		if err != nil {
			t.Fatalf("message not found after send: %v", err)
		}
		if gotMsg.Content != "Hello Bob!" {
			t.Fatalf("wrong content: %q", gotMsg.Content)
		}

		// Verify user updates exist
		aliceUpdates, _ := s.UserUpdates.ListByUser(ctx, "alice", 0, 10)
		if len(aliceUpdates) != 1 {
			t.Fatalf("expected 1 update for alice, got %d", len(aliceUpdates))
		}

		// Verify conversation updated
		gotConv, _ := s.Conversations.Get(ctx, "conv-send")
		if gotConv.LastProcessedMessageID != 1 {
			t.Fatalf("expected LastProcessedMessageID 1, got %d", gotConv.LastProcessedMessageID)
		}
	})
}

func TestSendMessageBatchLimit(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("conv-limit", "alice", "bob", "1-on-1", ""))

		msg := &model.Message{
			ID: "msg-limit", ClientMessageID: "client-limit",
			ConversationID: "conv-limit",
			SenderID:       "alice", Content: "test", CreatedAt: testNow,
		}

		// Create 501 member IDs (exceeds limit)
		memberIDs := make([]string, 501)
		for i := range memberIDs {
			memberIDs[i] = fmt.Sprintf("user-%d", i)
		}

		_, err := s.SendMessage(ctx, msg, memberIDs)
		if err == nil {
			t.Fatal("expected error for exceeding batch limit")
		}
	})
}

// --- Transaction tests ---

func TestTransactionCommit(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		err := s.Transaction(ctx, func(tx *gorm.DB) error {
			return tx.Create(newTestConv("conv-tx-1", "alice", "", "1-on-1", "")).Error
		})
		if err != nil {
			t.Fatalf("transaction failed: %v", err)
		}

		_, err = s.Conversations.Get(ctx, "conv-tx-1")
		if err != nil {
			t.Fatalf("record should exist after commit: %v", err)
		}
	})
}

func TestTransactionRollback(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		err := s.Transaction(ctx, func(tx *gorm.DB) error {
			tx.Create(newTestConv("conv-tx-rollback", "alice", "", "1-on-1", ""))
			return fmt.Errorf("intentional error")
		})
		if err == nil {
			t.Fatal("expected error from transaction")
		}

		_, err = s.Conversations.Get(ctx, "conv-tx-rollback")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound after rollback, got %v", err)
		}
	})
}

func TestTransactionContextExpired(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		err := s.Transaction(ctx, func(tx *gorm.DB) error {
			return tx.Create(newTestConv("conv-ctx", "alice", "", "1-on-1", "")).Error
		})
		if err == nil {
			t.Fatal("expected error for expired context")
		}
	})
}

func TestBeginTx(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		tx, err := s.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx failed: %v", err)
		}

		tx.DB().Create(newTestConv("conv-begintx", "alice", "", "1-on-1", ""))
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit failed: %v", err)
		}

		_, err = s.Conversations.Get(ctx, "conv-begintx")
		if err != nil {
			t.Fatalf("record should exist after commit: %v", err)
		}
	})
}

// --- Ping and HealthCheck tests ---

func TestPing(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		if err := s.Ping(ctx); err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
	})
}

func TestHealthCheck(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		if err := s.HealthCheck(ctx); err != nil {
			t.Fatalf("HealthCheck failed: %v", err)
		}
	})
}

// --- StoreAPI interface compliance test ---

func TestStoreAPICompliance(t *testing.T) {
	var _ StoreAPI = (*Store)(nil)
}

// --- AutoMigrate test ---

func TestAutoMigrate(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		// Running AutoMigrate again should be idempotent
		if err := s.AutoMigrate(ctx); err != nil {
			t.Fatalf("second auto migrate failed: %v", err)
		}
	})
}

// --- Model field tests ---

func TestConversationNewFields(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		conv := newTestConv("conv-fields", "alice", "bob", "1-on-1", "Test")
		conv.Pinned = true
		conv.Muted = true
		conv.AvatarURL = "https://example.com/avatar.png"
		conv.Description = "A test conversation"

		if err := s.Conversations.Create(ctx, conv); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		got, err := s.Conversations.Get(ctx, "conv-fields")
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if !got.Pinned {
			t.Fatal("expected Pinned true")
		}
		if !got.Muted {
			t.Fatal("expected Muted true")
		}
		if got.AvatarURL != "https://example.com/avatar.png" {
			t.Fatalf("expected AvatarURL set, got %q", got.AvatarURL)
		}
		if got.Description != "A test conversation" {
			t.Fatalf("expected Description set, got %q", got.Description)
		}
	})
}

func TestMessageNewFields(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		s.Conversations.Create(ctx, newTestConv("conv-mf", "alice", "", "group", ""))

		msg := &model.Message{
			ID: "msg-fields", ClientMessageID: "client-fields",
			ConversationID: "conv-mf", MessageID: 1, SenderID: "alice",
			Content: "test", Type: "image", ReplyTo: 0, Status: "delivered",
			CreatedAt: testNow,
		}

		if err := s.Messages.Create(ctx, msg); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		got, err := s.Messages.Get(ctx, "msg-fields")
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if got.Type != "image" {
			t.Fatalf("expected Type 'image', got %q", got.Type)
		}
		if got.Status != "delivered" {
			t.Fatalf("expected Status 'delivered', got %q", got.Status)
		}
	})
}

// --- D-012 to D-015 Store method tests ---

// TestConversationGetUnscoped verifies that GetUnscoped finds conversations
// including soft-deleted ones, and returns ErrNotFound for non-existent IDs.
func TestConversationGetUnscoped(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create a conversation, verify GetUnscoped finds it
		conv := newTestConv("conv-unscoped-1", "alice", "bob", "1-on-1", "Test")
		if err := s.Conversations.Create(ctx, conv); err != nil {
			t.Fatalf("create failed: %v", err)
		}
		got, err := s.Conversations.GetUnscoped(ctx, "conv-unscoped-1")
		if err != nil {
			t.Fatalf("GetUnscoped failed: %v", err)
		}
		if got.ID != "conv-unscoped-1" {
			t.Fatalf("expected conv-unscoped-1, got %s", got.ID)
		}

		// Soft-delete it
		if err := s.Conversations.Delete(ctx, "conv-unscoped-1"); err != nil {
			t.Fatalf("delete failed: %v", err)
		}

		// Get returns ErrNotFound but GetUnscoped still finds it
		_, err = s.Conversations.Get(ctx, "conv-unscoped-1")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound from Get after delete, got %v", err)
		}
		got2, err := s.Conversations.GetUnscoped(ctx, "conv-unscoped-1")
		if err != nil {
			t.Fatalf("GetUnscoped after delete failed: %v", err)
		}
		if got2.ID != "conv-unscoped-1" {
			t.Fatalf("expected conv-unscoped-1 from GetUnscoped, got %s", got2.ID)
		}

		// Non-existent ID returns ErrNotFound from GetUnscoped
		_, err = s.Conversations.GetUnscoped(ctx, "nonexistent-id")
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound for non-existent ID, got %v", err)
		}
	})
}

// TestConversationUpdateLastRead verifies the MAX-semantics update of read
// cursors (D-012): only advances forward, never backward.
func TestConversationUpdateLastRead(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-lr-1", "alice", "bob", "1-on-1", "Read Test")
		if err := s.Conversations.Create(ctx, conv); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		// UpdateLastRead for alice with messageID=5
		if err := s.Conversations.UpdateLastRead(ctx, "conv-lr-1", "alice", 5); err != nil {
			t.Fatalf("UpdateLastRead alice=5 failed: %v", err)
		}
		got, _ := s.Conversations.Get(ctx, "conv-lr-1")
		if got.LastReadMessageID1 != 5 {
			t.Fatalf("expected LastReadMessageID1=5, got %d", got.LastReadMessageID1)
		}

		// UpdateLastRead for bob with messageID=3
		if err := s.Conversations.UpdateLastRead(ctx, "conv-lr-1", "bob", 3); err != nil {
			t.Fatalf("UpdateLastRead bob=3 failed: %v", err)
		}
		got, _ = s.Conversations.Get(ctx, "conv-lr-1")
		if got.LastReadMessageID2 != 3 {
			t.Fatalf("expected LastReadMessageID2=3, got %d", got.LastReadMessageID2)
		}

		// UpdateLastRead for alice with messageID=2 (smaller) — should stay at 5 (MAX semantics, D-012)
		if err := s.Conversations.UpdateLastRead(ctx, "conv-lr-1", "alice", 2); err != nil {
			t.Fatalf("UpdateLastRead alice=2 failed: %v", err)
		}
		got, _ = s.Conversations.Get(ctx, "conv-lr-1")
		if got.LastReadMessageID1 != 5 {
			t.Fatalf("expected LastReadMessageID1 still 5 (MAX semantics), got %d", got.LastReadMessageID1)
		}

		// UpdateLastRead for alice with messageID=10 (larger) — should advance to 10
		if err := s.Conversations.UpdateLastRead(ctx, "conv-lr-1", "alice", 10); err != nil {
			t.Fatalf("UpdateLastRead alice=10 failed: %v", err)
		}
		got, _ = s.Conversations.Get(ctx, "conv-lr-1")
		if got.LastReadMessageID1 != 10 {
			t.Fatalf("expected LastReadMessageID1=10, got %d", got.LastReadMessageID1)
		}

		// UpdateLastRead for non-member should return ErrNotFound
		err := s.Conversations.UpdateLastRead(ctx, "conv-lr-1", "charlie", 1)
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound for non-member, got %v", err)
		}
	})
}

// TestMessageRestoreByConversation verifies cascade restore of messages (D-015):
// deleting and restoring messages by conversation.
func TestMessageRestoreByConversation(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create a conversation with 3 messages
		s.Conversations.Create(ctx, newTestConv("conv-restore-1", "alice", "bob", "1-on-1", "Restore Test"))
		for i := uint32(1); i <= 3; i++ {
			s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("restore-msg-%d", i), ClientMessageID: fmt.Sprintf("restore-client-%d", i),
				ConversationID: "conv-restore-1", MessageID: i, SenderID: "alice",
				Content: fmt.Sprintf("msg %d", i), CreatedAt: testNow,
			})
		}

		// Delete all messages via DeleteByConversation
		if err := s.Messages.DeleteByConversation(ctx, "conv-restore-1"); err != nil {
			t.Fatalf("DeleteByConversation failed: %v", err)
		}

		// Verify messages are gone
		msgs, _ := s.Messages.ListByConversation(ctx, "conv-restore-1", 0, 100)
		if len(msgs) != 0 {
			t.Fatalf("expected 0 messages after delete, got %d", len(msgs))
		}

		// RestoreByConversation should return 3 rows affected
		restored, err := s.Messages.RestoreByConversation(ctx, "conv-restore-1")
		if err != nil {
			t.Fatalf("RestoreByConversation failed: %v", err)
		}
		if restored != 3 {
			t.Fatalf("expected 3 restored rows, got %d", restored)
		}

		// Verify all 3 messages are visible again
		msgs, err = s.Messages.ListByConversation(ctx, "conv-restore-1", 0, 100)
		if err != nil {
			t.Fatalf("ListByConversation after restore failed: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages after restore, got %d", len(msgs))
		}

		// Test with empty conversation: RestoreByConversation returns 0
		s.Conversations.Create(ctx, newTestConv("conv-restore-empty", "alice", "bob", "1-on-1", "Empty"))
		restored2, err := s.Messages.RestoreByConversation(ctx, "conv-restore-empty")
		if err != nil {
			t.Fatalf("RestoreByConversation on empty conv failed: %v", err)
		}
		if restored2 != 0 {
			t.Fatalf("expected 0 restored rows for empty conv, got %d", restored2)
		}

		// Test isolation: only affects target conversation
		s.Conversations.Create(ctx, newTestConv("conv-restore-2", "charlie", "dave", "1-on-1", "Isolation"))
		for i := uint32(1); i <= 2; i++ {
			s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("iso-msg-%d", i), ClientMessageID: fmt.Sprintf("iso-client-%d", i),
				ConversationID: "conv-restore-2", MessageID: i, SenderID: "charlie",
				Content: fmt.Sprintf("iso msg %d", i), CreatedAt: testNow,
			})
		}
		// Delete conv2 messages
		if err := s.Messages.DeleteByConversation(ctx, "conv-restore-2"); err != nil {
			t.Fatalf("DeleteByConversation conv2 failed: %v", err)
		}
		// Restore conv1 messages (already restored above, but messages are not deleted now)
		// Actually conv1 messages are already restored. Let's delete conv1 again and restore only conv1.
		if err := s.Messages.DeleteByConversation(ctx, "conv-restore-1"); err != nil {
			t.Fatalf("DeleteByConversation conv1 failed: %v", err)
		}
		// Now restore only conv1
		restored3, err := s.Messages.RestoreByConversation(ctx, "conv-restore-1")
		if err != nil {
			t.Fatalf("RestoreByConversation conv1 isolation failed: %v", err)
		}
		if restored3 != 3 {
			t.Fatalf("expected 3 restored rows for conv1, got %d", restored3)
		}
		// conv2 messages should still be deleted
		msgs2, _ := s.Messages.ListByConversation(ctx, "conv-restore-2", 0, 100)
		if len(msgs2) != 0 {
			t.Fatalf("expected 0 messages in conv2 (should stay deleted), got %d", len(msgs2))
		}
		// conv1 messages should be restored
		msgs1, _ := s.Messages.ListByConversation(ctx, "conv-restore-1", 0, 100)
		if len(msgs1) != 3 {
			t.Fatalf("expected 3 messages in conv1 after restore, got %d", len(msgs1))
		}
	})
}
