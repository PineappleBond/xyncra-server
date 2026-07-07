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

// searchMessagesResult is the parsed response from the search_messages handler.
type searchMessagesResult struct {
	Messages []*model.Message `json:"messages"`
	HasMore  bool             `json:"has_more"`
}

// parseSearchMessagesResponse unmarshals the handler's response data.
func parseSearchMessagesResponse(t *testing.T, data json.RawMessage) searchMessagesResult {
	t.Helper()
	var result searchMessagesResult
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

// callSearchMessages is a convenience that builds a request, calls the handler,
// and parses the response. It fails the test on error.
func callSearchMessages(t *testing.T, h *searchMessagesHandler, userID string, params interface{}) searchMessagesResult {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient(userID)
	req := newTestRequest("req-search-msgs", "search_messages", params)
	data, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)
	return parseSearchMessagesResponse(t, data)
}

// callSearchMessagesExpectError is a convenience that builds a request, calls
// the handler, and asserts that an error is returned containing the given
// substring.
func callSearchMessagesExpectError(t *testing.T, h *searchMessagesHandler, userID string, params interface{}, errContains string) {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient(userID)
	req := newTestRequest("req-search-msgs", "search_messages", params)
	_, err := h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), errContains, "error should contain expected substring")
}

// seedTestMessagesWithContent inserts messages with specific content values
// into the given conversation. Each message has a unique ID, ClientMessageID,
// and sequential MessageIDs starting at startMessageID.
func seedTestMessagesWithContent(t *testing.T, s *testSQLiteStore, convID, senderID string, contents []string, startMessageID uint32) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	for i, content := range contents {
		msgID := startMessageID + uint32(i)
		msg := &model.Message{
			ID:              uuid.New().String(),
			ClientMessageID: uuid.New().String(),
			ConversationID:  convID,
			MessageID:       msgID,
			SenderID:        senderID,
			Content:         content,
			Type:            "text",
			Status:          "sent",
			CreatedAt:       now.Add(time.Duration(i) * time.Millisecond),
		}
		require.NoError(t, s.MessageStore().Create(ctx, msg))
	}
}

// ---------------------------------------------------------------------------
// SM-01: NormalSearch_MatchingMessages
// ---------------------------------------------------------------------------

func TestSearchMessages_NormalSearch(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessagesWithContent(t, s, convID, "alice", []string{
		"hello world",
		"goodbye world",
		"hello again",
		"nothing here",
	}, 1)

	result := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "hello",
	})

	require.Len(t, result.Messages, 2, "should return 2 messages matching 'hello'")
	assert.False(t, result.HasMore, "has_more should be false when all matches fit in limit")
}

// ---------------------------------------------------------------------------
// SM-02: NoMatches_EmptyResult
// ---------------------------------------------------------------------------

func TestSearchMessages_NoMatches(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessagesWithContent(t, s, convID, "alice", []string{
		"hello world",
		"goodbye world",
	}, 1)

	result := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "xyz",
	})

	assert.NotNil(t, result.Messages, "messages should not be null")
	assert.Empty(t, result.Messages, "messages should be empty when nothing matches")
	assert.False(t, result.HasMore, "has_more should be false for empty result")
}

// ---------------------------------------------------------------------------
// SM-03: CaseInsensitive_SEARCHMatchesHello
// SQLite LIKE is case-insensitive for ASCII by default.
// ---------------------------------------------------------------------------

func TestSearchMessages_CaseInsensitive(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessagesWithContent(t, s, convID, "alice", []string{
		"hello world",
	}, 1)

	result := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "HELLO",
	})

	require.Len(t, result.Messages, 1, "should match 'hello' with uppercase 'HELLO' (SQLite LIKE)")
	assert.Equal(t, "hello world", result.Messages[0].Content, "should return the original message content")
}

// ---------------------------------------------------------------------------
// SM-04: PartialMatch_elloMatchesHelloWorld
// ---------------------------------------------------------------------------

func TestSearchMessages_PartialMatch(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessagesWithContent(t, s, convID, "alice", []string{
		"hello world",
		"goodbye",
	}, 1)

	result := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "ello",
	})

	require.Len(t, result.Messages, 1, "partial match 'ello' should match 'hello world'")
	assert.Equal(t, "hello world", result.Messages[0].Content, "should return the matching message")
}

// ---------------------------------------------------------------------------
// SM-05: Sorting_MessageID_DESC
// ---------------------------------------------------------------------------

func TestSearchMessages_SortingMessageIDDESC(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessagesWithContent(t, s, convID, "alice", []string{
		"hello first",
		"hello second",
		"hello third",
	}, 1)

	result := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "hello",
	})

	require.Len(t, result.Messages, 3, "should return all 3 matching messages")
	for i := 1; i < len(result.Messages); i++ {
		assert.Greater(t, result.Messages[i-1].MessageID, result.Messages[i].MessageID,
			"messages should be ordered by MessageID DESC (newest first)")
	}
}

// ---------------------------------------------------------------------------
// SM-06: PaginationHasMore
// ---------------------------------------------------------------------------

func TestSearchMessages_PaginationHasMore(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessagesWithContent(t, s, convID, "alice", []string{
		"hello one",
		"hello two",
		"hello three",
		"hello four",
		"hello five",
		"nomatch",
	}, 1)

	result := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "hello",
		"limit":           2,
	})

	assert.Len(t, result.Messages, 2, "should return exactly 2 messages (limit)")
	assert.True(t, result.HasMore, "has_more should be true when more matches exist beyond limit")
}

// ---------------------------------------------------------------------------
// SM-07: MissingConversationID_Error
// ---------------------------------------------------------------------------

func TestSearchMessages_MissingConversationID(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)

	callSearchMessagesExpectError(t, handler, "alice", map[string]interface{}{
		"query": "hello",
	}, "conversation_id")
}

// ---------------------------------------------------------------------------
// SM-08: MissingQuery_Error
// ---------------------------------------------------------------------------

func TestSearchMessages_MissingQuery(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "query field completely missing",
			params: map[string]interface{}{"conversation_id": convID},
		},
		{
			name:   "query is empty string",
			params: map[string]interface{}{"conversation_id": convID, "query": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callSearchMessagesExpectError(t, handler, "alice", tt.params, "query")
		})
	}
}

// ---------------------------------------------------------------------------
// SM-09: ConversationNotFound_Error
// ---------------------------------------------------------------------------

func TestSearchMessages_ConversationNotFound(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)

	callSearchMessagesExpectError(t, handler, "alice", map[string]interface{}{
		"conversation_id": "nonexistent-conv-id",
		"query":           "hello",
	}, "not found")
}

// ---------------------------------------------------------------------------
// SM-10: NotAMember_Error
// C-3: member verification required
// ---------------------------------------------------------------------------

func TestSearchMessages_NotAMember(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")

	callSearchMessagesExpectError(t, handler, "charlie", map[string]interface{}{
		"conversation_id": convID,
		"query":           "hello",
	}, "not a member of the conversation")
}

// ---------------------------------------------------------------------------
// SM-11: BoundaryTest_ZeroAndNegativeLimit
// ---------------------------------------------------------------------------

func TestSearchMessages_BoundaryZeroNegativeLimit(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessagesWithContent(t, s, convID, "alice", []string{
		"hello one",
		"hello two",
	}, 1)

	// limit=0 should reset to default (50).
	result := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "hello",
		"limit":           0,
	})
	assert.Len(t, result.Messages, 2, "limit=0 should reset to default 50; all 2 matches returned")

	// limit=-1 should also reset to default (50).
	result2 := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "hello",
		"limit":           -1,
	})
	assert.Len(t, result2.Messages, 2, "limit=-1 should reset to default 50; all 2 matches returned")
}

// ---------------------------------------------------------------------------
// SM-12: BoundaryTest_LimitAboveCap
// ---------------------------------------------------------------------------

func TestSearchMessages_BoundaryLimitAboveCap(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessagesWithContent(t, s, convID, "alice", []string{
		"hello one",
	}, 1)

	// limit=999 should reset to default 50 (since > 200 resets to default).
	result := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "hello",
		"limit":           999,
	})
	assert.Len(t, result.Messages, 1, "limit=999 should reset to default 50; 1 match returned")
}

// ---------------------------------------------------------------------------
// SM-13: PaginationCursor_AfterMessageID
// Verify that after_message_id enables cursor-based pagination.
// ---------------------------------------------------------------------------

func TestSearchMessages_PaginationCursor(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSearchMessagesHandler(s)
	convID := uuid.New().String()
	createTestConversation(t, s, convID, "alice", "bob")
	seedTestMessagesWithContent(t, s, convID, "alice", []string{
		"hello first",   // MessageID=1
		"hello second",  // MessageID=2
		"hello third",   // MessageID=3
		"nomatch",       // MessageID=4
		"hello fourth",  // MessageID=5
	}, 1)

	// First page: get the 2 newest matches (results are DESC).
	page1 := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id": convID,
		"query":           "hello",
		"limit":           2,
	})
	require.Len(t, page1.Messages, 2, "page 1 should return 2 messages")
	assert.True(t, page1.HasMore, "page 1 should indicate has_more")
	// Results are DESC, so first message should be the newest.
	assert.Equal(t, uint32(5), page1.Messages[0].MessageID, "first result should be MessageID=5 (newest match)")
	assert.Equal(t, uint32(3), page1.Messages[1].MessageID, "second result should be MessageID=3")

	// Second page: use after_message_id to get older matches.
	page2 := callSearchMessages(t, handler, "alice", map[string]interface{}{
		"conversation_id":  convID,
		"query":            "hello",
		"after_message_id": page1.Messages[len(page1.Messages)-1].MessageID,
		"limit":            2,
	})
	require.Len(t, page2.Messages, 2, "page 2 should return 2 messages")
	assert.False(t, page2.HasMore, "page 2 should have has_more=false (all matches exhausted)")
	assert.Equal(t, uint32(2), page2.Messages[0].MessageID, "first result on page 2 should be MessageID=2")
	assert.Equal(t, uint32(1), page2.Messages[1].MessageID, "second result on page 2 should be MessageID=1")
}
