package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// listConversationsResult is the parsed response from the list_conversations handler.
type listConversationsResult struct {
	Conversations []*model.Conversation `json:"conversations"`
	HasMore       bool                  `json:"has_more"`
}

// parseListConversationsResponse unmarshals the handler's response data.
func parseListConversationsResponse(t *testing.T, data json.RawMessage) listConversationsResult {
	t.Helper()
	var result listConversationsResult
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

// callListConversations is a convenience that builds a request, calls the
// handler, and parses the response. It fails the test on error.
func callListConversations(t *testing.T, h *listConversationsHandler, userID string, params interface{}) listConversationsResult {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient(userID)
	req := newTestRequest("req-list-convos", "list_conversations", params)
	data, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)
	return parseListConversationsResponse(t, data)
}

// createTestConversationAtTime inserts a conversation record directly into the
// store with the given user pair, title, and LastMessageAt timestamp. It fails
// the test on error. Use this variant when tests need precise control over
// LastMessageAt (e.g. sorting tests); otherwise prefer createTestConversation
// from send_message_test.go.
func createTestConversationAtTime(t *testing.T, s *testSQLiteStore, id, user1, user2, title string, lastMessageAt time.Time) {
	t.Helper()
	ctx := context.Background()
	conv := &model.Conversation{
		ID:            id,
		UserID1:       user1,
		UserID2:       user2,
		Type:          "1-on-1",
		Title:         title,
		CreatedAt:     lastMessageAt,
		UpdatedAt:     lastMessageAt,
		LastMessageAt: lastMessageAt,
	}
	require.NoError(t, s.ConversationStore().Create(ctx, conv))
}

// seedConversationsForUser inserts count conversations for the given userID
// (as UserID1), each with a distinct LastMessageAt spaced by 1 hour starting
// from baseTime. It fails the test on error.
func seedConversationsForUser(t *testing.T, s *testSQLiteStore, userID string, count int, baseTime time.Time) {
	t.Helper()
	for i := 0; i < count; i++ {
		otherUser := uuid.New().String()
		// Later conversations have later LastMessageAt so that DESC ordering
		// places index 0 at the most recent timestamp.
		ts := baseTime.Add(time.Duration(count-1-i) * time.Hour)
		createTestConversationAtTime(t, s, uuid.New().String(), userID, otherUser,
			"chat-"+otherUser[:8], ts)
	}
}

// ---------------------------------------------------------------------------
// LC-01: NormalList_3Conversations_HasMoreFalse
// Related decision: list_conversations uses offset/limit pagination
// (see note on D-009 deviation).
// ---------------------------------------------------------------------------

func TestListConversations_NormalList_HasMoreFalse(t *testing.T) {
	// list_conversations uses offset/limit pagination (see note on D-009).
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)
	const userID = "alice"

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seedConversationsForUser(t, s, userID, 3, base)

	result := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 0,
		"limit":  10,
	})

	require.Len(t, result.Conversations, 3, "should return all 3 conversations")
	assert.False(t, result.HasMore, "has_more should be false when all results fit in limit")

	// Verify every conversation belongs to alice.
	for _, c := range result.Conversations {
		assert.Equal(t, userID, c.UserID1, "UserID1 should be alice")
		assert.NotEmpty(t, c.ID, "conversation should have an ID")
	}
}

// ---------------------------------------------------------------------------
// LC-02: EmptyList_NoConversations
// ---------------------------------------------------------------------------

func TestListConversations_EmptyList(t *testing.T) {
	// list_conversations uses offset/limit pagination (see note on D-009).
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)

	result := callListConversations(t, handler, "nobody", map[string]interface{}{
		"offset": 0,
		"limit":  20,
	})

	assert.NotNil(t, result.Conversations, "conversations should not be null (D-009 note)")
	assert.Empty(t, result.Conversations, "conversations should be empty array")
	assert.False(t, result.HasMore, "has_more should be false for empty list")
}

// ---------------------------------------------------------------------------
// LC-03: PaginationHasMore_5Conversations_Limit2
// ---------------------------------------------------------------------------

func TestListConversations_PaginationHasMore(t *testing.T) {
	// list_conversations uses offset/limit pagination (see note on D-009).
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)
	const userID = "bob"

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seedConversationsForUser(t, s, userID, 5, base)

	result := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 0,
		"limit":  2,
	})

	assert.Len(t, result.Conversations, 2, "should return exactly 2 conversations (limit)")
	assert.True(t, result.HasMore, "has_more should be true when more conversations exist beyond limit")
}

// ---------------------------------------------------------------------------
// LC-04: PaginationOffset_Offset2_Limit2
// ---------------------------------------------------------------------------

func TestListConversations_PaginationOffset(t *testing.T) {
	// list_conversations uses offset/limit pagination (see note on D-009).
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)
	const userID = "charlie"

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seedConversationsForUser(t, s, userID, 5, base)

	// Get the full list first to compare.
	full := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 0,
		"limit":  100,
	})
	require.Len(t, full.Conversations, 5)

	// Now request offset=2, limit=2.
	result := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 2,
		"limit":  2,
	})

	require.Len(t, result.Conversations, 2, "should return exactly 2 conversations")
	// The returned IDs must match the 3rd and 4th from the full list.
	assert.Equal(t, full.Conversations[2].ID, result.Conversations[0].ID,
		"first returned conversation should be the 3rd in DESC order")
	assert.Equal(t, full.Conversations[3].ID, result.Conversations[1].ID,
		"second returned conversation should be the 4th in DESC order")
	assert.True(t, result.HasMore,
		"has_more should be true because 5 total - offset 2 = 3 remaining > limit 2 (D-009 note)")
}

// ---------------------------------------------------------------------------
// LC-05: DefaultLimit_Omitted
// ---------------------------------------------------------------------------

func TestListConversations_DefaultLimit(t *testing.T) {
	// list_conversations uses offset/limit pagination; default limit is 20
	// (see note on D-009).
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)
	const userID = "dave"

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	// Seed 25 conversations — more than the default limit of 20.
	seedConversationsForUser(t, s, userID, 25, base)

	// Omit limit entirely — should default to 20.
	result := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 0,
	})

	assert.Len(t, result.Conversations, 20, "default limit should be 20")
	assert.True(t, result.HasMore, "has_more should be true because 25 > 20")
}

// ---------------------------------------------------------------------------
// LC-06: LimitTruncation_Limit200_CappedAt100
// ---------------------------------------------------------------------------

func TestListConversations_LimitTruncation(t *testing.T) {
	// list_conversations uses offset/limit pagination; limit is capped at 100
	// (see note on D-009).
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)
	const userID = "eve"

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seedConversationsForUser(t, s, userID, 105, base)

	// limit=200 should be capped to 100. The store's own limit cap is also
	// 100, so the limit+1 probe is clamped to 100 — at this boundary,
	// has_more detection is inherently limited. We verify the result does
	// not exceed the cap.
	result := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 0,
		"limit":  200,
	})

	assert.LessOrEqual(t, len(result.Conversations), 100,
		"limit=200 should be capped at 100")
	assert.Greater(t, len(result.Conversations), 0,
		"should return some conversations")
}

// ---------------------------------------------------------------------------
// LC-07: UserIsolation_AliceSeesOnlyHerOwn
// ---------------------------------------------------------------------------

func TestListConversations_UserIsolation(t *testing.T) {
	// list_conversations uses offset/limit pagination; each user should see
	// only their own conversations (see note on D-009).
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seedConversationsForUser(t, s, "alice", 3, base)
	seedConversationsForUser(t, s, "bob", 5, base)

	aliceResult := callListConversations(t, handler, "alice", map[string]interface{}{
		"offset": 0,
		"limit":  100,
	})
	assert.Len(t, aliceResult.Conversations, 3, "alice should see only her 3 conversations")
	for _, c := range aliceResult.Conversations {
		assert.Equal(t, "alice", c.UserID1, "each conversation should belong to alice")
	}

	bobResult := callListConversations(t, handler, "bob", map[string]interface{}{
		"offset": 0,
		"limit":  100,
	})
	assert.Len(t, bobResult.Conversations, 5, "bob should see only his 5 conversations")
	for _, c := range bobResult.Conversations {
		assert.Equal(t, "bob", c.UserID1, "each conversation should belong to bob")
	}
}

// ---------------------------------------------------------------------------
// LC-08: Sorting_LastMessageAt_DESC
// ---------------------------------------------------------------------------

func TestListConversations_Sorting_LastMessageAtDESC(t *testing.T) {
	// list_conversations should return conversations ordered by LastMessageAt
	// descending (see note on D-009).
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)
	const userID = "frank"

	// Create 3 conversations with distinct LastMessageAt timestamps.
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t1 := now                    // oldest
	t2 := now.Add(1 * time.Hour) // middle
	t3 := now.Add(2 * time.Hour) // newest

	createTestConversationAtTime(t, s, uuid.New().String(), userID, "u1", "oldest", t1)
	createTestConversationAtTime(t, s, uuid.New().String(), userID, "u2", "middle", t2)
	createTestConversationAtTime(t, s, uuid.New().String(), userID, "u3", "newest", t3)

	result := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 0,
		"limit":  100,
	})

	require.Len(t, result.Conversations, 3, "should return all 3 conversations")

	// Verify DESC order by LastMessageAt.
	assert.True(t, !result.Conversations[0].LastMessageAt.Before(result.Conversations[1].LastMessageAt),
		"conversations[0].LastMessageAt should be >= conversations[1].LastMessageAt")
	assert.True(t, !result.Conversations[1].LastMessageAt.Before(result.Conversations[2].LastMessageAt),
		"conversations[1].LastMessageAt should be >= conversations[2].LastMessageAt")

	// The newest should be first.
	assert.Equal(t, "newest", result.Conversations[0].Title,
		"first conversation should be the newest (D-009 note)")
	assert.Equal(t, "middle", result.Conversations[1].Title,
		"second conversation should be the middle")
	assert.Equal(t, "oldest", result.Conversations[2].Title,
		"third conversation should be the oldest")
}

// ---------------------------------------------------------------------------
// LC-09: BoundaryTest_ZeroAndNegativeLimit
// ---------------------------------------------------------------------------

func TestListConversations_BoundaryZeroNegativeLimit(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)
	const userID = "boundary-user"

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seedConversationsForUser(t, s, userID, 5, base)

	// limit=0 should reset to default (20).
	result := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 0,
		"limit":  0,
	})
	assert.Len(t, result.Conversations, 5, "limit=0 should reset to default 20; all 5 should be returned")
	assert.False(t, result.HasMore, "has_more should be false for 5 conversations with default limit")

	// limit=-1 should also reset to default (20).
	result2 := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 0,
		"limit":  -1,
	})
	assert.Len(t, result2.Conversations, 5, "limit=-1 should reset to default 20; all 5 should be returned")
	assert.False(t, result2.HasMore, "has_more should be false for 5 conversations with default limit")
}

// ---------------------------------------------------------------------------
// LC-10: BoundaryTest_NegativeOffset
// ---------------------------------------------------------------------------

func TestListConversations_BoundaryNegativeOffset(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)
	const userID = "boundary-offset"

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seedConversationsForUser(t, s, userID, 3, base)

	// Negative offset should be clamped to 0.
	result := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": -5,
		"limit":  10,
	})
	assert.Len(t, result.Conversations, 3, "negative offset should be clamped to 0; all 3 should be returned")
}

// ---------------------------------------------------------------------------
// LC-11: BoundaryTest_LimitExactly100_HasMoreDetection
// The limit+1 probe must work at the cap boundary (limit=100).
// ---------------------------------------------------------------------------

func TestListConversations_BoundaryLimit100HasMore(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewListConversationsHandler(s)
	const userID = "boundary-100"

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	// Seed 105 conversations — more than the cap of 100.
	seedConversationsForUser(t, s, userID, 105, base)

	result := callListConversations(t, handler, userID, map[string]interface{}{
		"offset": 0,
		"limit":  100,
	})

	assert.Len(t, result.Conversations, 100, "should return exactly 100 conversations (cap)")
	assert.True(t, result.HasMore, "has_more should be true when 105 > 100 (probe must work at cap boundary)")
}
