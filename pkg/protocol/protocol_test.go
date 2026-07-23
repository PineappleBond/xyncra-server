package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTypeAgentConstants(t *testing.T) {
	// Verify D-087 constants have the expected string values (D-125: removed
	// agent_question and agent_checkpoint_created; see PRODUCT_DECISIONS.md).
	assert.Equal(t, "agent_status", UpdateTypeAgentStatus)
	assert.Equal(t, "agent_timeout", UpdateTypeAgentTimeout)
}

func TestUpdateTypeAgentConstants_Distinct(t *testing.T) {
	// All agent ephemeral types must be distinct (D-125: removed
	// agent_question and agent_checkpoint_created).
	types := []string{
		UpdateTypeAgentStatus,
		UpdateTypeAgentTimeout,
	}
	seen := make(map[string]bool)
	for _, typ := range types {
		assert.False(t, seen[typ], "duplicate update type: %s", typ)
		seen[typ] = true
	}
}

// ---------------------------------------------------------------------------
// Phase 4: PackageDataRequest backward compatibility tests (D-104)
// ---------------------------------------------------------------------------

// TestPackageDataRequest_BackwardCompatible verifies that JSON produced by an
// old client (without idempotency_key and seq) can be unmarshalled without
// error. The new fields should default to their zero values.
func TestPackageDataRequest_BackwardCompatible(t *testing.T) {
	raw := `{"id":"1","method":"ping","params":null}`
	var req PackageDataRequest
	require.NoError(t, json.Unmarshal([]byte(raw), &req))
	assert.Equal(t, "1", req.ID)
	assert.Equal(t, "ping", req.Method)
	assert.Empty(t, req.IdempotencyKey, "old JSON should not set idempotency_key")
	assert.Equal(t, uint64(0), req.Seq, "old JSON should not set seq")
}

// TestPackageDataRequest_OmitEmpty verifies that when the new fields are not
// set, Marshal produces JSON without the idempotency_key and seq keys
// (omitempty semantics, D-104).
func TestPackageDataRequest_OmitEmpty(t *testing.T) {
	req := PackageDataRequest{
		ID:     "1",
		Method: "ping",
		Params: nil,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	// Unmarshal into a map to check which keys are present.
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Contains(t, m, "id")
	assert.Contains(t, m, "method")
	assert.NotContains(t, m, "idempotency_key", "omitempty should omit idempotency_key when empty")
	assert.NotContains(t, m, "seq", "omitempty should omit seq when zero")
}

// TestPackageDataRequest_WithNewFields verifies that setting the new fields
// and performing a Marshal + Unmarshal round-trip preserves their values.
func TestPackageDataRequest_WithNewFields(t *testing.T) {
	original := PackageDataRequest{
		ID:             "s-abc-123",
		Method:         "test.method",
		Params:         json.RawMessage(`{"key":"value"}`),
		IdempotencyKey: "s-abc-123",
		Seq:            42,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded PackageDataRequest
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Method, decoded.Method)
	assert.Equal(t, original.Params, decoded.Params)
	assert.Equal(t, original.IdempotencyKey, decoded.IdempotencyKey)
	assert.Equal(t, original.Seq, decoded.Seq)
}

// ---------------------------------------------------------------------------
// D-118: PackageDataResponse with piggyback Updates tests
// ---------------------------------------------------------------------------

// TestPackageDataResponse_WithUpdates verifies that JSON produced with Updates
// includes the "updates" field.
func TestPackageDataResponse_WithUpdates(t *testing.T) {
	resp := PackageDataResponse{
		ID:   "req-123",
		Code: ResponseCodeOK,
		Msg:  "ok",
		Data: json.RawMessage(`{"status":"queued"}`),
		Updates: []PackageDataUpdate{
			{
				Seq:     0,
				Type:    UpdateTypeConversation,
				Payload: json.RawMessage(`{"conversation_id":"conv-1","action":"update"}`),
			},
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	// Unmarshal into a map to check which keys are present.
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Contains(t, m, "updates", "JSON should contain 'updates' field when updates are present")
	updates := m["updates"].([]any)
	assert.Len(t, updates, 1)
}

// TestPackageDataResponse_OmitEmptyUpdates verifies that when Updates is nil,
// Marshal produces JSON without the "updates" key (omitempty semantics).
func TestPackageDataResponse_OmitEmptyUpdates(t *testing.T) {
	resp := PackageDataResponse{
		ID:   "req-456",
		Code: ResponseCodeOK,
		Msg:  "ok",
		Data: json.RawMessage(`{"status":"ok"}`),
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	// Unmarshal into a map to check which keys are present.
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.NotContains(t, m, "updates", "omitempty should omit 'updates' when nil")
}

// TestPackageDataResponse_EmptyUpdatesSlice verifies that when Updates is an
// empty (non-nil) slice, Marshal still omits the "updates" key.
func TestPackageDataResponse_EmptyUpdatesSlice(t *testing.T) {
	resp := PackageDataResponse{
		ID:      "req-789",
		Code:    ResponseCodeOK,
		Msg:     "ok",
		Data:    json.RawMessage(`{}`),
		Updates: []PackageDataUpdate{},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	// Unmarshal into a map to check which keys are present.
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	// Empty slice should still be omitted (Go json omitempty treats []T{} as empty).
	assert.NotContains(t, m, "updates", "omitempty should omit 'updates' when slice is empty")
}

// TestPackageDataResponse_BackwardCompatible verifies that JSON produced by an
// old server (without updates field) can be unmarshalled without error.
func TestPackageDataResponse_BackwardCompatible(t *testing.T) {
	raw := `{"id":"req-old","code":0,"msg":"ok","data":{"status":"queued"}}`
	var resp PackageDataResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))

	assert.Equal(t, "req-old", resp.ID)
	assert.Nil(t, resp.Updates, "old JSON should not set updates")
}
