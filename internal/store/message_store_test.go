package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers specific to message tests
// ---------------------------------------------------------------------------

// createTestMessages inserts count messages into the given conversation with
// sequential MessageIDs starting at 1. Each message has a unique ID and
// ClientMessageID derived from the prefix and index.
func createTestMessages(t *testing.T, s *Store, ctx context.Context, convID, prefix string, count int) {
	t.Helper()
	for i := 1; i <= count; i++ {
		require.NoError(t, s.Messages.Create(ctx, &model.Message{
			ID:              fmt.Sprintf("%s-%d", prefix, i),
			ClientMessageID: fmt.Sprintf("%s-client-%d", prefix, i),
			ConversationID:  convID,
			MessageID:       uint32(i),
			SenderID:        "alice",
			Content:         fmt.Sprintf("msg %d", i),
			CreatedAt:       testNow,
		}), "creating message %d should succeed", i)
	}
}

// ---------------------------------------------------------------------------
// ListByConversation
// ---------------------------------------------------------------------------

// TestMessageStore_ListByConversation_HappyPath verifies that
// ListByConversation returns all messages for a conversation ordered by
// MessageID ascending.
func TestMessageStore_ListByConversation_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		createTestMessages(t, s, ctx, "conv-1", "msg", 5)

		msgs, err := s.Messages.ListByConversation(ctx, "conv-1", 0, 10)
		require.NoError(t, err, "ListByConversation should succeed")
		require.Len(t, msgs, 5, "should return all 5 messages")

		// Verify MessageID ASC ordering.
		for i := 0; i < len(msgs); i++ {
			assert.Equal(t, uint32(i+1), msgs[i].MessageID, "messages should be in MessageID ASC order")
		}
	})
}

// TestMessageStore_ListByConversation_AfterMessageID verifies that
// ListByConversation returns only messages with MessageID greater than the
// supplied cursor.
func TestMessageStore_ListByConversation_AfterMessageID(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		createTestMessages(t, s, ctx, "conv-1", "msg", 5)

		msgs, err := s.Messages.ListByConversation(ctx, "conv-1", 3, 10)
		require.NoError(t, err, "ListByConversation should succeed")
		require.Len(t, msgs, 2, "should return messages with MessageID > 3")
		assert.Equal(t, uint32(4), msgs[0].MessageID, "first message should have MessageID 4")
		assert.Equal(t, uint32(5), msgs[1].MessageID, "second message should have MessageID 5")
	})
}

// TestMessageStore_ListByConversation_LimitZero verifies that limit=0 falls
// back to the default limit of 50.
func TestMessageStore_ListByConversation_LimitZero(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		// Create more than 50 messages to verify the default kicks in.
		for i := 1; i <= 55; i++ {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i), ClientMessageID: fmt.Sprintf("client-%d", i),
				ConversationID: "conv-1", MessageID: uint32(i), SenderID: "alice",
				Content: fmt.Sprintf("msg %d", i), CreatedAt: testNow,
			}), "creating message %d should succeed", i)
		}

		msgs, err := s.Messages.ListByConversation(ctx, "conv-1", 0, 0)
		require.NoError(t, err, "ListByConversation with limit=0 should succeed")
		assert.Len(t, msgs, 50, "expected default limit of 50 when limit=0")
	})
}

// TestMessageStore_ListByConversation_LimitNegative verifies that a negative
// limit falls back to the default limit of 50.
func TestMessageStore_ListByConversation_LimitNegative(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		for i := 1; i <= 55; i++ {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i), ClientMessageID: fmt.Sprintf("client-%d", i),
				ConversationID: "conv-1", MessageID: uint32(i), SenderID: "alice",
				Content: fmt.Sprintf("msg %d", i), CreatedAt: testNow,
			}), "creating message %d should succeed", i)
		}

		msgs, err := s.Messages.ListByConversation(ctx, "conv-1", 0, -5)
		require.NoError(t, err, "ListByConversation with negative limit should succeed")
		assert.Len(t, msgs, 50, "expected default limit of 50 when limit is negative")
	})
}

// TestMessageStore_ListByConversation_LimitTooLarge verifies that a limit
// exceeding 200 falls back to the default of 50.
func TestMessageStore_ListByConversation_LimitTooLarge(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		for i := 1; i <= 55; i++ {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i), ClientMessageID: fmt.Sprintf("client-%d", i),
				ConversationID: "conv-1", MessageID: uint32(i), SenderID: "alice",
				Content: fmt.Sprintf("msg %d", i), CreatedAt: testNow,
			}), "creating message %d should succeed", i)
		}

		msgs, err := s.Messages.ListByConversation(ctx, "conv-1", 0, 999)
		require.NoError(t, err, "ListByConversation with limit>200 should succeed")
		assert.Len(t, msgs, 50, "expected default limit of 50 when limit>200")
	})
}

// TestMessageStore_ListByConversation_AfterMessageIDTooLarge verifies that
// when afterMessageID exceeds the last MessageID, no messages are returned.
func TestMessageStore_ListByConversation_AfterMessageIDTooLarge(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		createTestMessages(t, s, ctx, "conv-1", "msg", 5)

		msgs, err := s.Messages.ListByConversation(ctx, "conv-1", 9999, 10)
		require.NoError(t, err, "ListByConversation with huge afterMessageID should succeed")
		assert.Empty(t, msgs, "expected empty result when afterMessageID exceeds last MessageID")
	})
}

// ---------------------------------------------------------------------------
// SearchByConversation
// ---------------------------------------------------------------------------

// TestMessageStore_SearchByConversation_HappyPath verifies that
// SearchByConversation finds messages whose content contains the search term.
func TestMessageStore_SearchByConversation_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		contents := []string{"hello world", "goodbye world", "hello again", "random text"}
		for i, c := range contents {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i+1), ClientMessageID: fmt.Sprintf("client-%d", i+1),
				ConversationID: "conv-1", MessageID: uint32(i + 1), SenderID: "alice",
				Content: c, CreatedAt: testNow,
			}), "creating message %d should succeed", i+1)
		}

		results, err := s.Messages.SearchByConversation(ctx, "conv-1", "hello", 0, 10)
		require.NoError(t, err, "SearchByConversation should succeed")
		require.Len(t, results, 2, "should find 2 messages containing 'hello'")
	})
}

// TestMessageStore_SearchByConversation_EmptyQuery verifies that an empty
// content query returns an empty slice without hitting the database.
func TestMessageStore_SearchByConversation_EmptyQuery(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		createTestMessages(t, s, ctx, "conv-1", "msg", 3)

		results, err := s.Messages.SearchByConversation(ctx, "conv-1", "", 0, 10)
		require.NoError(t, err, "SearchByConversation with empty query should succeed")
		assert.Empty(t, results, "expected empty slice for empty query")
	})
}

// TestMessageStore_SearchByConversation_Ordering verifies that
// SearchByConversation returns results in MessageID DESC order (newest first).
func TestMessageStore_SearchByConversation_Ordering(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		contents := []string{"hello first", "hello second", "hello third"}
		for i, c := range contents {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i+1), ClientMessageID: fmt.Sprintf("client-%d", i+1),
				ConversationID: "conv-1", MessageID: uint32(i + 1), SenderID: "alice",
				Content: c, CreatedAt: testNow,
			}), "creating message %d should succeed", i+1)
		}

		results, err := s.Messages.SearchByConversation(ctx, "conv-1", "hello", 0, 10)
		require.NoError(t, err, "SearchByConversation should succeed")
		require.Len(t, results, 3, "should find all 3 messages")

		// Newest first: msg-3 (MessageID=3), msg-2 (MessageID=2), msg-1 (MessageID=1)
		assert.Equal(t, uint32(3), results[0].MessageID, "first result should be newest (MessageID=3)")
		assert.Equal(t, uint32(2), results[1].MessageID, "second result should be middle (MessageID=2)")
		assert.Equal(t, uint32(1), results[2].MessageID, "third result should be oldest (MessageID=1)")
	})
}

// TestMessageStore_SearchByConversation_AfterMessageID verifies cursor-based
// pagination: only messages with MessageID < afterMessageID are returned.
func TestMessageStore_SearchByConversation_AfterMessageID(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		contents := []string{"hello first", "hello second", "hello third", "hello fourth"}
		for i, c := range contents {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i+1), ClientMessageID: fmt.Sprintf("client-%d", i+1),
				ConversationID: "conv-1", MessageID: uint32(i + 1), SenderID: "alice",
				Content: c, CreatedAt: testNow,
			}), "creating message %d should succeed", i+1)
		}

		// Cursor at MessageID=3 => should return messages with MessageID < 3,
		// i.e. msg-2 and msg-1 (in DESC order).
		results, err := s.Messages.SearchByConversation(ctx, "conv-1", "hello", 3, 10)
		require.NoError(t, err, "SearchByConversation with afterMessageID=3 should succeed")
		require.Len(t, results, 2, "should return 2 messages with MessageID < 3")
		assert.Equal(t, uint32(2), results[0].MessageID, "first result should have MessageID 2")
		assert.Equal(t, uint32(1), results[1].MessageID, "second result should have MessageID 1")
	})
}

// TestMessageStore_SearchByConversation_LikeSpecialChars verifies that LIKE
// special characters (%, _, \) in the search term do not cause SQL errors and
// never result in over-matching via wildcard expansion.
//
// The escapeLikePattern function escapes special chars with '\', but the SQL
// LIKE clause does not include an explicit ESCAPE '\' clause. This means
// exact-match behaviour varies by database:
//   - MySQL: '\' is the default LIKE escape, so "100%" matches "100% complete".
//   - SQLite/PostgreSQL: '\' is not an escape, so the escaped pattern matches
//     a literal backslash, yielding 0 results for "100%".
//
// This test verifies the cross-database invariant: special characters must
// never cause over-matching (wildcard expansion).
func TestMessageStore_SearchByConversation_LikeSpecialChars(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		// Messages with special chars in content.
		specialContents := []struct {
			id      string
			content string
		}{
			{"msg-pct", "100% complete"},
			{"msg-under", "file_name.txt"},
			{"msg-bs", `path\to\file`},
			{"msg-normal", "Normal message"},
		}
		for i, sc := range specialContents {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: sc.id, ClientMessageID: fmt.Sprintf("client-%s", sc.id),
				ConversationID: "conv-1", MessageID: uint32(i + 1), SenderID: "alice",
				Content: sc.content, CreatedAt: testNow,
			}), "creating message %s should succeed", sc.id)
		}

		// Baseline: normal search works correctly.
		resultsNormal, err := s.Messages.SearchByConversation(ctx, "conv-1", "Normal", 0, 10)
		require.NoError(t, err, "SearchByConversation for 'Normal' should succeed")
		require.Len(t, resultsNormal, 1, "should find exactly 1 Normal message")
		assert.Equal(t, "msg-normal", resultsNormal[0].ID, "should match the Normal message")

		// Key invariant: searching for "100%" must never match records that
		// do not contain the literal "100%" substring.
		resultsPct, err := s.Messages.SearchByConversation(ctx, "conv-1", "100%", 0, 10)
		require.NoError(t, err, "SearchByConversation with '%%' should not cause SQL error")
		assert.LessOrEqual(t, len(resultsPct), 1,
			"searching for '100%%' must not over-match via wildcard expansion")

		// Underscore must not act as a single-char wildcard.
		resultsUnder, err := s.Messages.SearchByConversation(ctx, "conv-1", "file_name", 0, 10)
		require.NoError(t, err, "SearchByConversation with underscore should not cause SQL error")
		assert.LessOrEqual(t, len(resultsUnder), 1,
			"searching for 'file_name' must not over-match via wildcard expansion")

		// Backslash must not cause errors or over-matching.
		resultsBs, err := s.Messages.SearchByConversation(ctx, "conv-1", `path\to`, 0, 10)
		require.NoError(t, err, "SearchByConversation with backslash should not cause SQL error")
		assert.LessOrEqual(t, len(resultsBs), 1,
			"searching for 'path\\to' must not over-match via wildcard expansion")
	})
}

// TestMessageStore_SearchByConversation_LimitTooLarge verifies that a limit
// exceeding 201 falls back to the default of 50.
func TestMessageStore_SearchByConversation_LimitTooLarge(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		// Create 55 messages all containing "hello".
		for i := 1; i <= 55; i++ {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i), ClientMessageID: fmt.Sprintf("client-%d", i),
				ConversationID: "conv-1", MessageID: uint32(i), SenderID: "alice",
				Content: "hello world", CreatedAt: testNow,
			}), "creating message %d should succeed", i)
		}

		results, err := s.Messages.SearchByConversation(ctx, "conv-1", "hello", 0, 999)
		require.NoError(t, err, "SearchByConversation with limit>201 should succeed")
		assert.Len(t, results, 50, "expected default limit of 50 when limit>201")
	})
}

// ---------------------------------------------------------------------------
// ListByTimeRange
// ---------------------------------------------------------------------------

// TestMessageStore_ListByTimeRange_HappyPath verifies that ListByTimeRange
// returns messages within the inclusive [startTime, endTime] window.
func TestMessageStore_ListByTimeRange_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		times := []time.Time{
			time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		}
		for i, tm := range times {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i+1), ClientMessageID: fmt.Sprintf("client-%d", i+1),
				ConversationID: "conv-1", MessageID: uint32(i + 1), SenderID: "alice",
				Content: fmt.Sprintf("msg %d", i+1), CreatedAt: tm,
			}), "creating message %d should succeed", i+1)
		}

		start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
		msgs, err := s.Messages.ListByTimeRange(ctx, "conv-1", start, end, 10)
		require.NoError(t, err, "ListByTimeRange should succeed")
		require.Len(t, msgs, 2, "should return 2 messages within the time range (Feb and Mar)")
		assert.Equal(t, uint32(2), msgs[0].MessageID, "first should be msg-2 (Feb)")
		assert.Equal(t, uint32(3), msgs[1].MessageID, "second should be msg-3 (Mar)")
	})
}

// TestMessageStore_ListByTimeRange_EmptyRange verifies that when no messages
// fall within the specified time range, an empty slice is returned.
func TestMessageStore_ListByTimeRange_EmptyRange(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		createTestMessages(t, s, ctx, "conv-1", "msg", 3)

		// Use a range far in the future where no messages exist.
		start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2030, 12, 31, 0, 0, 0, 0, time.UTC)
		msgs, err := s.Messages.ListByTimeRange(ctx, "conv-1", start, end, 10)
		require.NoError(t, err, "ListByTimeRange with future range should succeed")
		assert.Empty(t, msgs, "expected empty result when no messages are in range")
	})
}

// TestMessageStore_ListByTimeRange_LimitZero verifies that limit=0 falls back
// to the default of 50.
func TestMessageStore_ListByTimeRange_LimitZero(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		// Create 55 messages within the time range.
		baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
		for i := 1; i <= 55; i++ {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i), ClientMessageID: fmt.Sprintf("client-%d", i),
				ConversationID: "conv-1", MessageID: uint32(i), SenderID: "alice",
				Content:   fmt.Sprintf("msg %d", i),
				CreatedAt: baseTime.Add(time.Duration(i) * time.Minute),
			}), "creating message %d should succeed", i)
		}

		start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 6, 1, 23, 59, 59, 0, time.UTC)
		msgs, err := s.Messages.ListByTimeRange(ctx, "conv-1", start, end, 0)
		require.NoError(t, err, "ListByTimeRange with limit=0 should succeed")
		assert.Len(t, msgs, 50, "expected default limit of 50 when limit=0")
	})
}

// TestMessageStore_ListByTimeRange_LimitNegative verifies that a negative
// limit falls back to the default of 50.
func TestMessageStore_ListByTimeRange_LimitNegative(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		// Create 55 messages within the time range.
		baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
		for i := 1; i <= 55; i++ {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i), ClientMessageID: fmt.Sprintf("client-%d", i),
				ConversationID: "conv-1", MessageID: uint32(i), SenderID: "alice",
				Content:   fmt.Sprintf("msg %d", i),
				CreatedAt: baseTime.Add(time.Duration(i) * time.Minute),
			}), "creating message %d should succeed", i)
		}

		start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 6, 1, 23, 59, 59, 0, time.UTC)
		msgs, err := s.Messages.ListByTimeRange(ctx, "conv-1", start, end, -5)
		require.NoError(t, err, "ListByTimeRange with negative limit should succeed")
		assert.Len(t, msgs, 50, "expected default limit of 50 when limit<0")
	})
}

// TestMessageStore_ListByTimeRange_LimitTooLarge verifies that limit>200 falls
// back to the default of 50.
func TestMessageStore_ListByTimeRange_LimitTooLarge(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		// Create 55 messages within the time range.
		baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
		for i := 1; i <= 55; i++ {
			require.NoError(t, s.Messages.Create(ctx, &model.Message{
				ID: fmt.Sprintf("msg-%d", i), ClientMessageID: fmt.Sprintf("client-%d", i),
				ConversationID: "conv-1", MessageID: uint32(i), SenderID: "alice",
				Content:   fmt.Sprintf("msg %d", i),
				CreatedAt: baseTime.Add(time.Duration(i) * time.Minute),
			}), "creating message %d should succeed", i)
		}

		start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 6, 1, 23, 59, 59, 0, time.UTC)
		msgs, err := s.Messages.ListByTimeRange(ctx, "conv-1", start, end, 999)
		require.NoError(t, err, "ListByTimeRange with limit>200 should succeed")
		assert.Len(t, msgs, 50, "expected default limit of 50 when limit>200")
	})
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

// TestMessageStore_Delete_HappyPath verifies that Delete soft-deletes an
// existing message so it is no longer visible via Get.
func TestMessageStore_Delete_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		require.NoError(t, s.Messages.Create(ctx, &model.Message{
			ID: "msg-del-1", ClientMessageID: "client-del-1",
			ConversationID: "conv-1", MessageID: 1, SenderID: "alice",
			Content: "to be deleted", CreatedAt: testNow,
		}), "creating message should succeed")

		err := s.Messages.Delete(ctx, "msg-del-1")
		require.NoError(t, err, "Delete should succeed for existing message")

		// Verify message is no longer accessible via Get.
		got, err := s.Messages.Get(ctx, "msg-del-1")
		require.ErrorIs(t, err, ErrNotFound, "Get should return ErrNotFound after soft delete")
		assert.Nil(t, got, "message should be nil after soft delete")
	})
}

// TestMessageStore_Delete_NotFound verifies that Delete returns ErrNotFound
// when the message ID does not exist.
func TestMessageStore_Delete_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		err := s.Messages.Delete(ctx, "non-existent-msg-id")
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound for non-existent message ID")
	})
}

// ---------------------------------------------------------------------------
// Restore
// ---------------------------------------------------------------------------

// TestMessageStore_Restore_HappyPath verifies that Restore undeletes a
// previously soft-deleted message, making it visible again via Get and
// ListByConversation.
func TestMessageStore_Restore_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		require.NoError(t, s.Messages.Create(ctx, &model.Message{
			ID: "msg-restore-1", ClientMessageID: "client-restore-1",
			ConversationID: "conv-1", MessageID: 1, SenderID: "alice",
			Content: "restore me", CreatedAt: testNow,
		}), "creating message should succeed")

		// Soft delete
		require.NoError(t, s.Messages.Delete(ctx, "msg-restore-1"), "Delete should succeed")

		// Verify it is gone
		_, err := s.Messages.Get(ctx, "msg-restore-1")
		require.ErrorIs(t, err, ErrNotFound, "Get should return ErrNotFound after delete")

		// Restore
		err = s.Messages.Restore(ctx, "msg-restore-1")
		require.NoError(t, err, "Restore should succeed for soft-deleted message")

		// Verify it is accessible again
		got, err := s.Messages.Get(ctx, "msg-restore-1")
		require.NoError(t, err, "Get should succeed after Restore")
		require.NotNil(t, got, "message should be non-nil after Restore")
		assert.Equal(t, "msg-restore-1", got.ID, "ID should match after Restore")
		assert.Equal(t, "restore me", got.Content, "Content should be preserved after Restore")

		// Verify it appears in ListByConversation
		msgs, listErr := s.Messages.ListByConversation(ctx, "conv-1", 0, 10)
		require.NoError(t, listErr, "ListByConversation should succeed after Restore")
		require.Len(t, msgs, 1, "restored message should appear in list")
	})
}

// TestMessageStore_Restore_NotDeleted verifies that Restore returns
// ErrNotFound when the message exists but has not been soft-deleted.
func TestMessageStore_Restore_NotDeleted(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		require.NoError(t, s.Messages.Create(ctx, &model.Message{
			ID: "msg-nd-1", ClientMessageID: "client-nd-1",
			ConversationID: "conv-1", MessageID: 1, SenderID: "alice",
			Content: "not deleted", CreatedAt: testNow,
		}), "creating message should succeed")

		// Restore on a non-deleted message should return ErrNotFound.
		err := s.Messages.Restore(ctx, "msg-nd-1")
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound when restoring non-deleted message")
	})
}

// ---------------------------------------------------------------------------
// DeleteByConversation
// ---------------------------------------------------------------------------

// TestMessageStore_DeleteByConversation_HappyPath verifies that all messages
// belonging to a conversation are soft-deleted.
func TestMessageStore_DeleteByConversation_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		createTestMessages(t, s, ctx, "conv-1", "msg", 5)

		err := s.Messages.DeleteByConversation(ctx, "conv-1")
		require.NoError(t, err, "DeleteByConversation should succeed")

		// All messages should be gone from normal queries.
		msgs, err := s.Messages.ListByConversation(ctx, "conv-1", 0, 100)
		require.NoError(t, err, "ListByConversation after DeleteByConversation should succeed")
		assert.Empty(t, msgs, "all messages should be soft-deleted")
	})
}

// TestMessageStore_DeleteByConversation_EmptyConversation verifies that
// calling DeleteByConversation on a conversation with no messages does not
// return an error.
func TestMessageStore_DeleteByConversation_EmptyConversation(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-empty", "alice", "bob", "1-on-1", "Empty")),
			"creating conversation should succeed")

		err := s.Messages.DeleteByConversation(ctx, "conv-empty")
		require.NoError(t, err, "DeleteByConversation on empty conversation should not error")
	})
}

// TestMessageStore_DeleteByConversation_Isolation verifies that
// DeleteByConversation only affects messages in the target conversation and
// does not touch messages in other conversations.
func TestMessageStore_DeleteByConversation_Isolation(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-a", "alice", "bob", "1-on-1", "A")),
			"creating conv-a should succeed")
		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-b", "charlie", "dave", "1-on-1", "B")),
			"creating conv-b should succeed")

		createTestMessages(t, s, ctx, "conv-a", "msga", 3)
		createTestMessages(t, s, ctx, "conv-b", "msgb", 3)

		// Delete only conv-a messages.
		require.NoError(t, s.Messages.DeleteByConversation(ctx, "conv-a"),
			"DeleteByConversation for conv-a should succeed")

		// conv-a should be empty.
		msgsA, err := s.Messages.ListByConversation(ctx, "conv-a", 0, 100)
		require.NoError(t, err, "ListByConversation for conv-a should succeed")
		assert.Empty(t, msgsA, "all conv-a messages should be deleted")

		// conv-b should still have all 3 messages.
		msgsB, err := s.Messages.ListByConversation(ctx, "conv-b", 0, 100)
		require.NoError(t, err, "ListByConversation for conv-b should succeed")
		require.Len(t, msgsB, 3, "conv-b messages should be unaffected")
	})
}

// ---------------------------------------------------------------------------
// CountUnread
// ---------------------------------------------------------------------------

// TestMessageStore_CountUnread_AllMessages verifies that afterMessageID=0
// counts all non-deleted messages in the conversation.
func TestMessageStore_CountUnread_AllMessages(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		createTestMessages(t, s, ctx, "conv-1", "msg", 5)

		count, err := s.Messages.CountUnread(ctx, "conv-1", 0)
		require.NoError(t, err, "CountUnread with afterMessageID=0 should succeed")
		assert.Equal(t, int64(5), count, "expected count of 5 when afterMessageID=0")
	})
}

// TestMessageStore_CountUnread_AfterLatest verifies that when afterMessageID
// equals the latest MessageID, the unread count is 0.
func TestMessageStore_CountUnread_AfterLatest(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		createTestMessages(t, s, ctx, "conv-1", "msg", 5)

		count, err := s.Messages.CountUnread(ctx, "conv-1", 5)
		require.NoError(t, err, "CountUnread with afterMessageID=latest should succeed")
		assert.Equal(t, int64(0), count, "expected 0 unread when afterMessageID equals latest MessageID")
	})
}

// TestMessageStore_CountUnread_ExcludesSoftDeleted verifies that soft-deleted
// messages are excluded from the unread count.
func TestMessageStore_CountUnread_ExcludesSoftDeleted(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		createTestMessages(t, s, ctx, "conv-1", "msg", 5)

		// Soft-delete msg-3 and msg-4.
		require.NoError(t, s.Messages.Delete(ctx, "msg-3"), "Delete msg-3 should succeed")
		require.NoError(t, s.Messages.Delete(ctx, "msg-4"), "Delete msg-4 should succeed")

		count, err := s.Messages.CountUnread(ctx, "conv-1", 0)
		require.NoError(t, err, "CountUnread should succeed")
		assert.Equal(t, int64(3), count, "expected count of 3 after soft-deleting 2 of 5 messages")
	})
}

// ---------------------------------------------------------------------------
// GetByClientMessageID
// ---------------------------------------------------------------------------

// TestMessageStore_GetByClientMessageID_HappyPath verifies that a message can
// be retrieved by its client-generated unique ID.
func TestMessageStore_GetByClientMessageID_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		require.NoError(t, s.Conversations.Create(ctx, newTestConv("conv-1", "alice", "bob", "1-on-1", "Test")),
			"creating conversation should succeed")

		require.NoError(t, s.Messages.Create(ctx, &model.Message{
			ID: "msg-cm-1", ClientMessageID: "unique-client-id-42",
			ConversationID: "conv-1", MessageID: 1, SenderID: "alice",
			Content: "hello", CreatedAt: testNow,
		}), "creating message should succeed")

		got, err := s.Messages.GetByClientMessageID(ctx, "unique-client-id-42")
		require.NoError(t, err, "GetByClientMessageID should succeed")
		require.NotNil(t, got, "message should be non-nil")
		assert.Equal(t, "msg-cm-1", got.ID, "ID should match")
		assert.Equal(t, "unique-client-id-42", got.ClientMessageID, "ClientMessageID should match")
		assert.Equal(t, "hello", got.Content, "Content should match")
	})
}

// TestMessageStore_GetByClientMessageID_NotFound verifies that
// GetByClientMessageID returns ErrNotFound when the client_message_id does not
// exist.
func TestMessageStore_GetByClientMessageID_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		got, err := s.Messages.GetByClientMessageID(ctx, "non-existent-client-id")
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound for non-existent client_message_id")
		assert.Nil(t, got, "message should be nil when not found")
	})
}
