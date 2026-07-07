package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// seedUserUpdates inserts count UserUpdate records for the given userID,
// starting at startSeq. Payloads are simple JSON objects for verification.
func seedUserUpdates(t *testing.T, s *testSQLiteStore, userID string, count int, startSeq uint32) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()

	updates := make([]model.UserUpdate, count)
	for i := 0; i < count; i++ {
		seq := startSeq + uint32(i)
		updates[i] = model.UserUpdate{
			ID:        uuid.New().String(),
			UserID:    userID,
			Seq:       seq,
			Payload:   []byte(fmt.Sprintf(`{"msg":"update-%d"}`, seq)),
			CreatedAt: now.Add(time.Duration(i) * time.Millisecond),
		}
	}

	require.NoError(t, s.UserUpdateStore().Create(ctx, updates))
}

// syncUpdatesResult is the parsed response from the sync_updates handler.
type syncUpdatesResult struct {
	Updates   []protocol.PackageDataUpdate `json:"updates"`
	HasMore   bool                         `json:"has_more"`
	LatestSeq uint32                       `json:"latest_seq"`
}

// parseSyncUpdatesResponse unmarshals the handler's response data.
func parseSyncUpdatesResponse(t *testing.T, data json.RawMessage) syncUpdatesResult {
	t.Helper()
	var result syncUpdatesResult
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

// callSyncUpdates is a convenience that builds a request, calls the handler,
// and parses the response. It fails the test on error.
func callSyncUpdates(t *testing.T, h *syncUpdatesHandler, userID string, params interface{}) syncUpdatesResult {
	t.Helper()
	ctx := context.Background()
	client := newTestClient(userID)
	req := newTestRequest("req-sync", "sync_updates", params)
	data, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)
	return parseSyncUpdatesResponse(t, data)
}

// ---------------------------------------------------------------------------
// SU-01: HappyPath_WithUpdates
// ---------------------------------------------------------------------------

func TestSyncUpdates_HappyPath_WithUpdates(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "alice"

	// Seed 5 updates with seq 1..5
	seedUserUpdates(t, s, userID, 5, 1)

	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
		"limit":     10,
	})

	assert.Len(t, result.Updates, 5, "should return all 5 updates")
	assert.False(t, result.HasMore, "has_more should be false when all updates fit")
	assert.Equal(t, uint32(5), result.LatestSeq, "latest_seq should be 5")

	// Verify each update has correct seq ordering and payload
	for i, u := range result.Updates {
		expectedSeq := uint32(i + 1)
		assert.Equal(t, expectedSeq, u.Seq, "update %d should have seq %d", i, expectedSeq)
		assert.NotEmpty(t, u.Payload, "payload should not be empty")
	}
}

// ---------------------------------------------------------------------------
// SU-02: HappyPath_NoUpdates
// ---------------------------------------------------------------------------

func TestSyncUpdates_HappyPath_NoUpdates(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)

	// No updates seeded for this user
	result := callSyncUpdates(t, handler, "nobody", map[string]interface{}{
		"after_seq": 0,
		"limit":     10,
	})

	assert.Empty(t, result.Updates, "updates should be empty")
	assert.False(t, result.HasMore, "has_more should be false")
	assert.Equal(t, uint32(0), result.LatestSeq, "latest_seq should be 0 for user with no updates")
}

// ---------------------------------------------------------------------------
// SU-03: PartialUpdates - has_more=true
// ---------------------------------------------------------------------------

func TestSyncUpdates_PartialUpdates(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "bob"

	// Seed 10 updates, request limit=3
	seedUserUpdates(t, s, userID, 10, 1)

	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
		"limit":     3,
	})

	assert.Len(t, result.Updates, 3, "should return exactly 3 updates (limit)")
	assert.True(t, result.HasMore, "has_more should be true when more updates exist beyond limit")
	assert.Equal(t, uint32(10), result.LatestSeq, "latest_seq should reflect the global latest")

	// Verify we got the first 3 (seq 1,2,3)
	assert.Equal(t, uint32(1), result.Updates[0].Seq)
	assert.Equal(t, uint32(2), result.Updates[1].Seq)
	assert.Equal(t, uint32(3), result.Updates[2].Seq)
}

// ---------------------------------------------------------------------------
// SU-04: AfterSeqZero - fetch from beginning
// ---------------------------------------------------------------------------

func TestSyncUpdates_AfterSeqZero(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "charlie"

	seedUserUpdates(t, s, userID, 7, 1)

	// after_seq=0 should fetch from the beginning
	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})

	assert.Len(t, result.Updates, 7, "after_seq=0 should return all updates from the start")
	assert.False(t, result.HasMore)
	assert.Equal(t, uint32(1), result.Updates[0].Seq, "first update should have seq=1")
	assert.Equal(t, uint32(7), result.Updates[6].Seq, "last update should have seq=7")
}

// ---------------------------------------------------------------------------
// SU-05: DefaultLimit - limit omitted, defaults to 100
// ---------------------------------------------------------------------------

func TestSyncUpdates_DefaultLimit(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "dave"

	// Seed 150 updates (more than default limit of 100)
	seedUserUpdates(t, s, userID, 150, 1)

	// Omit limit entirely - should default to 100
	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
	})

	assert.Len(t, result.Updates, 100, "default limit should be 100")
	assert.True(t, result.HasMore, "has_more should be true since 150 > 100")
	assert.Equal(t, uint32(150), result.LatestSeq)
}

// ---------------------------------------------------------------------------
// SU-06: LimitCapped - limit=1000, capped to 500
// ---------------------------------------------------------------------------

func TestSyncUpdates_LimitCapped(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "eve"

	// Seed 600 updates (more than the cap of 500)
	seedUserUpdates(t, s, userID, 600, 1)

	// limit=1000 should be capped to 500
	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
		"limit":     1000,
	})

	// The handler caps at 500; the actual number returned depends on the
	// store's own limit handling. We verify it does not exceed 500.
	assert.LessOrEqual(t, len(result.Updates), 500,
		"updates should not exceed the handler cap of 500")
	assert.Equal(t, uint32(600), result.LatestSeq,
		"latest_seq should reflect the global latest regardless of limit")
}

// ---------------------------------------------------------------------------
// SU-07: HasMoreBoundary_ExactLimit - exactly limit records, has_more=false
// ---------------------------------------------------------------------------

func TestSyncUpdates_HasMoreBoundary_ExactLimit(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "frank"

	const limit = 5
	// Seed exactly 5 updates (equal to limit)
	seedUserUpdates(t, s, userID, limit, 1)

	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
		"limit":     limit,
	})

	assert.Len(t, result.Updates, limit, "should return exactly %d updates", limit)
	assert.False(t, result.HasMore, "has_more should be false when exactly limit records exist")
	assert.Equal(t, uint32(limit), result.LatestSeq)
}

// ---------------------------------------------------------------------------
// SU-08: HasMoreBoundary_LimitPlusOne - limit+1 records, has_more=true
// ---------------------------------------------------------------------------

func TestSyncUpdates_HasMoreBoundary_LimitPlusOne(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "grace"

	const limit = 5
	// Seed limit+1 = 6 updates
	seedUserUpdates(t, s, userID, limit+1, 1)

	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
		"limit":     limit,
	})

	assert.Len(t, result.Updates, limit, "should return exactly %d updates (truncated)", limit)
	assert.True(t, result.HasMore, "has_more should be true when limit+1 records exist")
	assert.Equal(t, uint32(limit+1), result.LatestSeq)
}

// ---------------------------------------------------------------------------
// SU-09: SeqOrdering - results ordered by seq ascending
// ---------------------------------------------------------------------------

func TestSyncUpdates_SeqOrdering(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "henry"

	// Seed 20 updates with seq 1..20
	seedUserUpdates(t, s, userID, 20, 1)

	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})

	assert.Len(t, result.Updates, 20)

	// Verify ascending seq order
	for i := 1; i < len(result.Updates); i++ {
		assert.Greater(t, result.Updates[i].Seq, result.Updates[i-1].Seq,
			"updates should be in ascending seq order: update[%d].Seq=%d should be > update[%d].Seq=%d",
			i, result.Updates[i].Seq, i-1, result.Updates[i-1].Seq)
	}

	// Verify first and last
	assert.Equal(t, uint32(1), result.Updates[0].Seq)
	assert.Equal(t, uint32(20), result.Updates[19].Seq)
}

// ---------------------------------------------------------------------------
// SU-10: InvalidParams - malformed JSON
// ---------------------------------------------------------------------------

func TestSyncUpdates_InvalidParams(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	ctx := context.Background()

	client := newTestClient("alice")

	// Create a request with invalid JSON params
	req := &protocol.PackageDataRequest{
		ID:     "req-bad",
		Method: "sync_updates",
		Params: json.RawMessage(`{invalid json!!!`),
	}

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err, "should return error for invalid JSON")
	assert.Contains(t, err.Error(), "invalid params",
		"error should contain 'invalid params'")
}

// ---------------------------------------------------------------------------
// SU-11: PayloadTypeConversion - Payload is valid JSON
// ---------------------------------------------------------------------------

func TestSyncUpdates_PayloadTypeConversion(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "iris"

	// Seed updates with JSON object payloads
	seedUserUpdates(t, s, userID, 3, 1)

	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
		"limit":     10,
	})

	require.Len(t, result.Updates, 3)

	for i, u := range result.Updates {
		// Each Payload should be parseable as a JSON object
		var payload map[string]interface{}
		err := json.Unmarshal(u.Payload, &payload)
		require.NoError(t, err, "update[%d] payload should be valid JSON", i)
		assert.Contains(t, payload, "msg",
			"payload should contain the 'msg' key")
	}
}

// ---------------------------------------------------------------------------
// Additional edge case: after_seq skips earlier updates
// ---------------------------------------------------------------------------

func TestSyncUpdates_AfterSeqSkips(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "jane"

	seedUserUpdates(t, s, userID, 10, 1)

	// Request updates after seq=5 - should get seq 6..10
	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 5,
		"limit":     100,
	})

	assert.Len(t, result.Updates, 5, "should return 5 updates (seq 6-10)")
	assert.False(t, result.HasMore)
	assert.Equal(t, uint32(6), result.Updates[0].Seq, "first returned update should have seq=6")
	assert.Equal(t, uint32(10), result.Updates[4].Seq, "last returned update should have seq=10")
	assert.Equal(t, uint32(10), result.LatestSeq)
}

// ---------------------------------------------------------------------------
// Additional edge case: user isolation - updates for other users not returned
// ---------------------------------------------------------------------------

func TestSyncUpdates_UserIsolation(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)

	// Seed updates for two different users
	seedUserUpdates(t, s, "alice", 5, 1)
	seedUserUpdates(t, s, "bob", 3, 1)

	// Alice should only see her own updates
	aliceResult := callSyncUpdates(t, handler, "alice", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	assert.Len(t, aliceResult.Updates, 5)
	assert.Equal(t, uint32(5), aliceResult.LatestSeq)

	// Bob should only see his own updates
	bobResult := callSyncUpdates(t, handler, "bob", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	assert.Len(t, bobResult.Updates, 3)
	assert.Equal(t, uint32(3), bobResult.LatestSeq)
}

// ---------------------------------------------------------------------------
// Additional edge case: negative limit uses default 100
// ---------------------------------------------------------------------------

func TestSyncUpdates_NegativeLimitUsesDefault(t *testing.T) {
	s := setupTestSQLite(t)
	handler := NewSyncUpdatesHandler(s)
	const userID = "kate"

	// Seed 150 updates
	seedUserUpdates(t, s, userID, 150, 1)

	// Negative limit should default to 100
	result := callSyncUpdates(t, handler, userID, map[string]interface{}{
		"after_seq": 0,
		"limit":     -5,
	})

	assert.Len(t, result.Updates, 100, "negative limit should default to 100")
	assert.True(t, result.HasMore, "has_more should be true since 150 > 100")
}
