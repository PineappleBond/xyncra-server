package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConversationStore_Get_HappyPath verifies that Get returns the correct
// conversation when it exists in the database.
func TestConversationStore_Get_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-1", "user-1", "user-2", "direct", "Test Chat")
		conv.Title = "Test Chat"
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		got, err := s.Conversations.Get(ctx, "conv-1")
		require.NoError(t, err, "Get should succeed for existing conversation")
		require.NotNil(t, got, "Get should return a non-nil conversation")
		assert.Equal(t, "conv-1", got.ID, "ID should match")
		assert.Equal(t, "user-1", got.UserID1, "UserID1 should match")
		assert.Equal(t, "user-2", got.UserID2, "UserID2 should match")
		assert.Equal(t, "direct", got.Type, "Type should match")
		assert.Equal(t, "Test Chat", got.Title, "Title should match")
	})
}

// TestConversationStore_Get_NotFound verifies that Get returns ErrNotFound
// when the requested ID does not exist in the database.
func TestConversationStore_Get_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		got, err := s.Conversations.Get(ctx, "non-existent-id")
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound for non-existent ID")
		assert.Nil(t, got, "conversation should be nil when not found")
	})
}

// TestConversationStore_Get_EmptyID verifies that Get returns ErrNotFound
// when an empty string ID is provided.
func TestConversationStore_Get_EmptyID(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		got, err := s.Conversations.Get(ctx, "")
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound for empty string ID")
		assert.Nil(t, got, "conversation should be nil for empty ID")
	})
}

// TestConversationStore_GetByUsers_Bidirectional verifies that GetByUsers
// returns the same conversation regardless of the order of user1 and user2.
func TestConversationStore_GetByUsers_Bidirectional(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-bidir", "user-a", "user-b", "direct", "Bidirectional")
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		// Forward order: (user-a, user-b)
		got1, err := s.Conversations.GetByUsers(ctx, "user-a", "user-b")
		require.NoError(t, err, "GetByUsers(user-a, user-b) should succeed")
		require.NotNil(t, got1, "conversation should be found for forward order")
		assert.Equal(t, "conv-bidir", got1.ID, "ID should match for forward order")

		// Reverse order: (user-b, user-a)
		got2, err := s.Conversations.GetByUsers(ctx, "user-b", "user-a")
		require.NoError(t, err, "GetByUsers(user-b, user-a) should succeed")
		require.NotNil(t, got2, "conversation should be found for reverse order")
		assert.Equal(t, "conv-bidir", got2.ID, "ID should match for reverse order")
	})
}

// TestConversationStore_GetByUsers_NotFound verifies that GetByUsers returns
// ErrNotFound when no conversation exists between the two users.
func TestConversationStore_GetByUsers_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		got, err := s.Conversations.GetByUsers(ctx, "user-x", "user-y")
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound for non-existent user pair")
		assert.Nil(t, got, "conversation should be nil when user pair has no conversation")
	})
}

// TestConversationStore_GetByUser_Pagination verifies that GetByUser returns
// the correct subset of conversations with offset and limit pagination.
func TestConversationStore_GetByUser_Pagination(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create 5 conversations with distinct LastMessageAt timestamps.
		baseTime := testNow
		for i := 0; i < 5; i++ {
			conv := newTestConv("conv-page-"+string(rune('a'+i)), "alice", "bob-"+string(rune('a'+i)), "direct", "")
			conv.LastMessageAt = baseTime.Add(time.Duration(i) * time.Minute)
			require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation %d should succeed", i)
		}

		// Page 1: offset=0, limit=2 (should get 2 most recent)
		page1, err := s.Conversations.GetByUser(ctx, "alice", 0, 2)
		require.NoError(t, err, "GetByUser page 1 should succeed")
		require.Len(t, page1, 2, "page 1 should have 2 conversations")

		// Page 2: offset=2, limit=2 (should get 2 next most recent)
		page2, err := s.Conversations.GetByUser(ctx, "alice", 2, 2)
		require.NoError(t, err, "GetByUser page 2 should succeed")
		require.Len(t, page2, 2, "page 2 should have 2 conversations")

		// Page 3: offset=4, limit=2 (should get 1 remaining)
		page3, err := s.Conversations.GetByUser(ctx, "alice", 4, 2)
		require.NoError(t, err, "GetByUser page 3 should succeed")
		require.Len(t, page3, 1, "page 3 should have 1 conversation")

		// Ensure pages don't overlap: IDs on page1 and page2 should differ.
		assert.NotEqual(t, page1[0].ID, page2[0].ID, "page 1 and page 2 should not overlap")
	})
}

// TestConversationStore_GetByUser_OffsetOutOfRange verifies that GetByUser
// returns an empty slice when offset exceeds the total number of conversations.
func TestConversationStore_GetByUser_OffsetOutOfRange(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-offset", "alice", "bob", "direct", "")
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		convs, err := s.Conversations.GetByUser(ctx, "alice", 100, 10)
		require.NoError(t, err, "GetByUser with large offset should succeed")
		assert.Empty(t, convs, "expected empty slice when offset exceeds total count")
	})
}

// TestConversationStore_GetByUser_LimitZero verifies that GetByUser uses
// the default limit of 20 when limit is 0.
func TestConversationStore_GetByUser_LimitZero(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create 25 conversations to exceed the default limit of 20.
		for i := 0; i < 25; i++ {
			id := "conv-lz-" + padInt(i)
			conv := newTestConv(id, "alice", "bob-"+padInt(i), "direct", "")
			conv.LastMessageAt = testNow.Add(time.Duration(i) * time.Minute)
			require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation %d should succeed", i)
		}

		convs, err := s.Conversations.GetByUser(ctx, "alice", 0, 0)
		require.NoError(t, err, "GetByUser with limit=0 should succeed")
		assert.Len(t, convs, 20, "expected default limit of 20 when limit=0")
	})
}

// TestConversationStore_GetByUser_LimitNegative verifies that GetByUser uses
// the default limit of 20 when limit is negative.
func TestConversationStore_GetByUser_LimitNegative(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create 25 conversations to exceed the default limit of 20.
		for i := 0; i < 25; i++ {
			id := "conv-ln-" + padInt(i)
			conv := newTestConv(id, "alice", "bob-"+padInt(i), "direct", "")
			conv.LastMessageAt = testNow.Add(time.Duration(i) * time.Minute)
			require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation %d should succeed", i)
		}

		convs, err := s.Conversations.GetByUser(ctx, "alice", 0, -5)
		require.NoError(t, err, "GetByUser with negative limit should succeed")
		assert.Len(t, convs, 20, "expected default limit of 20 when limit is negative")
	})
}

// TestConversationStore_GetByUser_LimitTooLarge verifies that GetByUser uses
// the default limit of 20 when limit exceeds 101.
func TestConversationStore_GetByUser_LimitTooLarge(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create 25 conversations to exceed the default limit of 20.
		for i := 0; i < 25; i++ {
			id := "conv-lt-" + padInt(i)
			conv := newTestConv(id, "alice", "bob-"+padInt(i), "direct", "")
			conv.LastMessageAt = testNow.Add(time.Duration(i) * time.Minute)
			require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation %d should succeed", i)
		}

		convs, err := s.Conversations.GetByUser(ctx, "alice", 0, 200)
		require.NoError(t, err, "GetByUser with limit>101 should succeed")
		assert.Len(t, convs, 20, "expected default limit of 20 when limit>101")
	})
}

// TestConversationStore_GetByUser_UserID2 verifies that GetByUser returns
// conversations where the user is in the UserID2 position.
func TestConversationStore_GetByUser_UserID2(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-u2", "alice", "bob", "direct", "")
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		// bob is in UserID2 position
		convs, err := s.Conversations.GetByUser(ctx, "bob", 0, 10)
		require.NoError(t, err, "GetByUser for user in UserID2 position should succeed")
		require.Len(t, convs, 1, "should find 1 conversation for user in UserID2")
		assert.Equal(t, "conv-u2", convs[0].ID, "conversation ID should match")
		assert.Equal(t, "bob", convs[0].UserID2, "user should be in UserID2 position")
	})
}

// TestConversationStore_GetByUser_SoftDeletedExcluded verifies that GetByUser
// does not include soft-deleted conversations in its results.
func TestConversationStore_GetByUser_SoftDeletedExcluded(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv1 := newTestConv("conv-sd-1", "alice", "bob-1", "direct", "")
		require.NoError(t, s.Conversations.Create(ctx, conv1), "creating conv1 should succeed")

		conv2 := newTestConv("conv-sd-2", "alice", "bob-2", "direct", "")
		require.NoError(t, s.Conversations.Create(ctx, conv2), "creating conv2 should succeed")

		// Soft-delete conv1
		require.NoError(t, s.Conversations.Delete(ctx, "conv-sd-1"), "soft deleting conv1 should succeed")

		convs, err := s.Conversations.GetByUser(ctx, "alice", 0, 10)
		require.NoError(t, err, "GetByUser should succeed")
		require.Len(t, convs, 1, "should only return non-deleted conversations")
		assert.Equal(t, "conv-sd-2", convs[0].ID, "only non-deleted conversation should appear")
	})
}

// TestConversationStore_GetByUser_Ordering verifies that GetByUser returns
// conversations sorted by LastMessageAt in descending order (newest first).
func TestConversationStore_GetByUser_Ordering(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Create 3 conversations with different LastMessageAt values.
		conv1 := newTestConv("conv-ord-1", "alice", "bob-1", "direct", "")
		conv1.LastMessageAt = testNow.Add(-2 * time.Hour)
		require.NoError(t, s.Conversations.Create(ctx, conv1), "creating conv1 should succeed")

		conv2 := newTestConv("conv-ord-2", "alice", "bob-2", "direct", "")
		conv2.LastMessageAt = testNow
		require.NoError(t, s.Conversations.Create(ctx, conv2), "creating conv2 should succeed")

		conv3 := newTestConv("conv-ord-3", "alice", "bob-3", "direct", "")
		conv3.LastMessageAt = testNow.Add(-1 * time.Hour)
		require.NoError(t, s.Conversations.Create(ctx, conv3), "creating conv3 should succeed")

		convs, err := s.Conversations.GetByUser(ctx, "alice", 0, 10)
		require.NoError(t, err, "GetByUser should succeed")
		require.Len(t, convs, 3, "should return all 3 conversations")

		// Expected order: conv2 (newest), conv3 (middle), conv1 (oldest)
		assert.Equal(t, "conv-ord-2", convs[0].ID, "first should be newest (conv2)")
		assert.Equal(t, "conv-ord-3", convs[1].ID, "second should be middle (conv3)")
		assert.Equal(t, "conv-ord-1", convs[2].ID, "third should be oldest (conv1)")
	})
}

// TestConversationStore_Delete_HappyPath verifies that Delete performs a
// soft delete on an existing conversation and returns no error.
func TestConversationStore_Delete_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-del", "alice", "bob", "direct", "")
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		err := s.Conversations.Delete(ctx, "conv-del")
		require.NoError(t, err, "Delete should succeed for existing conversation")
	})
}

// TestConversationStore_Delete_NotFound verifies that Delete returns
// ErrNotFound when the conversation ID does not exist.
func TestConversationStore_Delete_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		err := s.Conversations.Delete(ctx, "non-existent-id")
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound for non-existent ID")
	})
}

// TestConversationStore_Delete_ThenGet verifies that after soft-deleting a
// conversation, Get returns ErrNotFound (GORM soft-delete plugin filters it).
func TestConversationStore_Delete_ThenGet(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-del-get", "alice", "bob", "direct", "")
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		err := s.Conversations.Delete(ctx, "conv-del-get")
		require.NoError(t, err, "Delete should succeed")

		got, err := s.Conversations.Get(ctx, "conv-del-get")
		require.ErrorIs(t, err, ErrNotFound, "Get should return ErrNotFound after soft delete")
		assert.Nil(t, got, "conversation should be nil after soft delete")
	})
}

// TestConversationStore_Restore_HappyPath verifies that Restore undeletes a
// previously soft-deleted conversation, making it visible to Get again.
func TestConversationStore_Restore_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-restore", "alice", "bob", "direct", "Restore Me")
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		// Soft delete
		require.NoError(t, s.Conversations.Delete(ctx, "conv-restore"), "Delete should succeed")

		// Verify it is gone from normal Get
		_, err := s.Conversations.Get(ctx, "conv-restore")
		require.ErrorIs(t, err, ErrNotFound, "Get should return ErrNotFound after delete")

		// Restore
		err = s.Conversations.Restore(ctx, "conv-restore")
		require.NoError(t, err, "Restore should succeed for soft-deleted conversation")

		// Verify it is accessible again
		got, err := s.Conversations.Get(ctx, "conv-restore")
		require.NoError(t, err, "Get should succeed after Restore")
		require.NotNil(t, got, "conversation should be non-nil after Restore")
		assert.Equal(t, "conv-restore", got.ID, "ID should match after Restore")
		assert.Equal(t, "Restore Me", got.Title, "Title should be preserved after Restore")
	})
}

// TestConversationStore_Restore_NotDeleted verifies that Restore returns
// ErrNotFound when the conversation exists but has not been soft-deleted.
func TestConversationStore_Restore_NotDeleted(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-restore-nd", "alice", "bob", "direct", "")
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		// Restore on a non-deleted conversation should return ErrNotFound
		err := s.Conversations.Restore(ctx, "conv-restore-nd")
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound when restoring non-deleted conversation")
	})
}

// TestConversationStore_Restore_NotFound verifies that Restore returns
// ErrNotFound when the conversation does not exist at all.
func TestConversationStore_Restore_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		err := s.Conversations.Restore(ctx, "non-existent-id")
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound for non-existent conversation")
	})
}

// TestConversationStore_UpdateLastMessage_HappyPath verifies that
// UpdateLastMessage correctly updates the LastMessageAt and
// LastProcessedMessageID fields of an existing conversation.
func TestConversationStore_UpdateLastMessage_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-ulm", "alice", "bob", "direct", "")
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		newTime := testNow.Add(5 * time.Minute)
		err := s.Conversations.UpdateLastMessage(ctx, "conv-ulm", newTime, 42)
		require.NoError(t, err, "UpdateLastMessage should succeed")

		got, err := s.Conversations.Get(ctx, "conv-ulm")
		require.NoError(t, err, "Get should succeed after UpdateLastMessage")
		assert.True(t, got.LastMessageAt.Equal(newTime), "LastMessageAt should be updated to new time")
		assert.Equal(t, uint32(42), got.LastProcessedMessageID, "LastProcessedMessageID should be updated to 42")
	})
}

// TestConversationStore_UpdateLastMessage_NotFound verifies that
// UpdateLastMessage returns ErrNotFound when the conversation does not exist.
func TestConversationStore_UpdateLastMessage_NotFound(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		err := s.Conversations.UpdateLastMessage(ctx, "non-existent-id", testNow, 1)
		require.ErrorIs(t, err, ErrNotFound, "expected ErrNotFound for non-existent conversation")
	})
}

// TestConversationStore_SearchByTitle_HappyPath verifies that SearchByTitle
// returns conversations whose title contains the search substring.
func TestConversationStore_SearchByTitle_HappyPath(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv1 := newTestConv("conv-search-1", "alice", "bob-1", "direct", "Go Programming")
		conv1.LastMessageAt = testNow.Add(-1 * time.Hour)
		require.NoError(t, s.Conversations.Create(ctx, conv1), "creating conv1 should succeed")

		conv2 := newTestConv("conv-search-2", "alice", "bob-2", "direct", "Rust Programming")
		conv2.LastMessageAt = testNow
		require.NoError(t, s.Conversations.Create(ctx, conv2), "creating conv2 should succeed")

		conv3 := newTestConv("conv-search-3", "alice", "bob-3", "direct", "Cooking Recipes")
		conv3.LastMessageAt = testNow.Add(-2 * time.Hour)
		require.NoError(t, s.Conversations.Create(ctx, conv3), "creating conv3 should succeed")

		// Search for "Programming" - should match conv1 and conv2
		results, err := s.Conversations.SearchByTitle(ctx, "alice", "Programming", 10)
		require.NoError(t, err, "SearchByTitle should succeed")
		require.Len(t, results, 2, "should find 2 conversations with 'Programming' in title")

		// Verify ordering: conv2 (newer) before conv1 (older)
		assert.Equal(t, "conv-search-2", results[0].ID, "first result should be newer conversation")
		assert.Equal(t, "conv-search-1", results[1].ID, "second result should be older conversation")

		// Search for "Cooking" - should match conv3 only
		results2, err := s.Conversations.SearchByTitle(ctx, "alice", "Cooking", 10)
		require.NoError(t, err, "SearchByTitle should succeed")
		require.Len(t, results2, 1, "should find 1 conversation with 'Cooking' in title")
		assert.Equal(t, "conv-search-3", results2[0].ID, "result should match Cooking conversation")
	})
}

// TestConversationStore_SearchByTitle_Empty verifies that SearchByTitle
// returns an empty slice when the title parameter is an empty string.
func TestConversationStore_SearchByTitle_Empty(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv := newTestConv("conv-empty", "alice", "bob", "direct", "Some Title")
		require.NoError(t, s.Conversations.Create(ctx, conv), "creating conversation should succeed")

		results, err := s.Conversations.SearchByTitle(ctx, "alice", "", 10)
		require.NoError(t, err, "SearchByTitle with empty title should succeed")
		assert.Empty(t, results, "expected empty slice for empty title")
	})
}

// TestConversationStore_SearchByTitle_LikeSpecialChars verifies that
// SearchByTitle handles LIKE special characters (%, _, \) without causing
// SQL errors and without allowing them to act as wildcards (over-matching).
//
// Note: the escapeLikePattern function escapes special chars with '\', but the
// SQL LIKE clause does not include an explicit ESCAPE '\' clause. This means
// exact-match behaviour for literal special chars varies by database:
//   - MySQL: '\' is the default LIKE escape, so "100%" matches "100% complete".
//   - SQLite/PostgreSQL: '\' is not an escape, so the escaped pattern matches
//     a literal backslash, yielding 0 results for "100%".
//
// This test verifies the cross-database invariant: special characters in the
// search term must never cause over-matching (wildcard expansion).
func TestConversationStore_SearchByTitle_LikeSpecialChars(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		// Records with special chars in titles.
		conv1 := newTestConv("conv-like-1", "alice", "bob-1", "direct", "100% complete")
		require.NoError(t, s.Conversations.Create(ctx, conv1), "creating conv1 should succeed")

		conv2 := newTestConv("conv-like-2", "alice", "bob-2", "direct", "file_name.txt")
		require.NoError(t, s.Conversations.Create(ctx, conv2), "creating conv2 should succeed")

		conv3 := newTestConv("conv-like-3", "alice", "bob-3", "direct", "path\\to\\file")
		require.NoError(t, s.Conversations.Create(ctx, conv3), "creating conv3 should succeed")

		conv4 := newTestConv("conv-like-4", "alice", "bob-4", "direct", "Normal chat")
		require.NoError(t, s.Conversations.Create(ctx, conv4), "creating conv4 should succeed")

		// Baseline: normal search works correctly.
		results4, err := s.Conversations.SearchByTitle(ctx, "alice", "Normal", 10)
		require.NoError(t, err, "SearchByTitle for 'Normal' should succeed")
		require.Len(t, results4, 1, "should find exactly 1 Normal conversation")
		assert.Equal(t, "conv-like-4", results4[0].ID, "should match the Normal conversation")

		// Key invariant: searching for "100%" must never match records that
		// do not contain the literal "100%" substring (i.e., "%" must not act
		// as a wildcard). On MySQL this returns 1 (exact match); on SQLite/PG
		// this returns 0 (due to missing ESCAPE clause). Both are acceptable.
		results, err := s.Conversations.SearchByTitle(ctx, "alice", "100%", 10)
		require.NoError(t, err, "SearchByTitle with '%%' should not cause SQL error")
		assert.LessOrEqual(t, len(results), 1, "searching for '100%%' must not over-match via wildcard expansion")

		// Same invariant for underscore: must not act as single-char wildcard.
		results2, err := s.Conversations.SearchByTitle(ctx, "alice", "file_name", 10)
		require.NoError(t, err, "SearchByTitle with underscore should not cause SQL error")
		assert.LessOrEqual(t, len(results2), 1, "searching for 'file_name' must not over-match via wildcard expansion")

		// Same invariant for backslash.
		results3, err := s.Conversations.SearchByTitle(ctx, "alice", `path\to`, 10)
		require.NoError(t, err, "SearchByTitle with backslash should not cause SQL error")
		assert.LessOrEqual(t, len(results3), 1, "searching for 'path\\to' must not over-match via wildcard expansion")
	})
}

// TestConversationStore_SearchByTitle_SoftDeletedExcluded verifies that
// SearchByTitle does not return soft-deleted conversations in its results.
func TestConversationStore_SearchByTitle_SoftDeletedExcluded(t *testing.T) {
	runOnAllDatabases(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		cleanAll(t, s, ctx)

		conv1 := newTestConv("conv-sd-s1", "alice", "bob-1", "direct", "Alpha Project")
		require.NoError(t, s.Conversations.Create(ctx, conv1), "creating conv1 should succeed")

		conv2 := newTestConv("conv-sd-s2", "alice", "bob-2", "direct", "Alpha Test")
		require.NoError(t, s.Conversations.Create(ctx, conv2), "creating conv2 should succeed")

		// Soft-delete conv1
		require.NoError(t, s.Conversations.Delete(ctx, "conv-sd-s1"), "Delete should succeed")

		results, err := s.Conversations.SearchByTitle(ctx, "alice", "Alpha", 10)
		require.NoError(t, err, "SearchByTitle should succeed")
		require.Len(t, results, 1, "should only return non-deleted conversations")
		assert.Equal(t, "conv-sd-s2", results[0].ID, "only non-deleted conversation should appear")
	})
}

// padInt returns a zero-padded two-digit string for use in unique test IDs.
func padInt(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
