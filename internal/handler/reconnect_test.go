package handler

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers — local copies (these live in server package tests)
// ---------------------------------------------------------------------------

// reconnectNoopLogger satisfies server.Logger without producing output.
type reconnectNoopLogger struct{}

func (reconnectNoopLogger) Info(string, ...any)  {}
func (reconnectNoopLogger) Error(string, ...any) {}
func (reconnectNoopLogger) Debug(string, ...any) {}

// reconnectMockSendFunc is a thread-safe mock for the sendFunc callback.
type reconnectMockSendFunc struct {
	mu    sync.Mutex
	calls []reconnectSendCall
	err   error
}

type reconnectSendCall struct {
	userID   string
	deviceID string
	pkg      *protocol.Package
}

func (m *reconnectMockSendFunc) Send(userID, deviceID string, pkg *protocol.Package) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.calls = append(m.calls, reconnectSendCall{userID: userID, deviceID: deviceID, pkg: pkg})
	return nil
}

func (m *reconnectMockSendFunc) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// waitForCalls polls until the mock has received at least n calls or timeout.
func (m *reconnectMockSendFunc) waitForCalls(n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.callCount() >= n {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return m.callCount() >= n
}

// ---------------------------------------------------------------------------
// Mock PendingStore for reconnect tests
// ---------------------------------------------------------------------------

// mockReconnectPendingStore implements server.PendingStore with configurable
// List return values/errors and call tracking for Remove and Update.
type mockReconnectPendingStore struct {
	mu         sync.Mutex
	listResult []*server.PendingRequest
	listErr    error
	removed    []string // request IDs passed to Remove
	updated    []*server.PendingRequest
	saveErr    error
	removeErr  error
	updateErr  error
}

func (m *mockReconnectPendingStore) Save(_ context.Context, req *server.PendingRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveErr
}

func (m *mockReconnectPendingStore) List(_ context.Context, _, _ string) ([]*server.PendingRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	// Return a copy to avoid races.
	out := make([]*server.PendingRequest, len(m.listResult))
	copy(out, m.listResult)
	return out, nil
}

func (m *mockReconnectPendingStore) Remove(_ context.Context, _, _, requestID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed = append(m.removed, requestID)
	return m.removeErr
}

func (m *mockReconnectPendingStore) RemoveByDevice(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockReconnectPendingStore) Update(_ context.Context, req *server.PendingRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updated = append(m.updated, req)
	return m.updateErr
}

// removedIDs returns a snapshot of removed request IDs.
func (m *mockReconnectPendingStore) removedIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.removed))
	copy(out, m.removed)
	return out
}

// updatedReqs returns a snapshot of updated pending requests.
func (m *mockReconnectPendingStore) updatedReqs() []*server.PendingRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*server.PendingRequest, len(m.updated))
	copy(out, m.updated)
	return out
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// reconnectResponse is the parsed response from the reconnect handler.
type reconnectResponseParsed struct {
	Status   string `json:"status"`
	Replayed int    `json:"replayed"`
	Total    int    `json:"total"`
}

func parseReconnectResponse(t *testing.T, data json.RawMessage) reconnectResponseParsed {
	t.Helper()
	var result reconnectResponseParsed
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

// makePendingRequest creates a PendingRequest with sensible defaults.
func makePendingRequest(id string, seq uint64, retryCount, maxRetries int) *server.PendingRequest {
	return &server.PendingRequest{
		ID:             id,
		UserID:         "test-user",
		DeviceID:       "test-device",
		Method:         "test.method",
		Params:         json.RawMessage(`{"key":"value"}`),
		IdempotencyKey: id,
		Seq:            seq,
		RetryCount:     retryCount,
		MaxRetries:     maxRetries,
		CreatedAt:      time.Now(),
	}
}

// newReconnectTestSetup creates a reconnectHandler wired with a mock
// PendingStore and mock send function. It returns the handler, mock store,
// and mock send function for assertions.
func newReconnectTestSetup(store *mockReconnectPendingStore) (*reconnectHandler, *reconnectMockSendFunc, *server.ReverseRPC) {
	ms := &reconnectMockSendFunc{}
	rpc := server.NewReverseRPC(server.ReverseRPCConfig{
		SendFunc:     ms.Send,
		Logger:       reconnectNoopLogger{},
		PendingStore: store,
	})
	h := NewReconnectHandler(rpc, reconnectNoopLogger{})
	return h, ms, rpc
}

// ---------------------------------------------------------------------------
// R-01: HappyPath_ReplaysFilteredRequests
// 3 pending with Seq 1,2,3. last_seen_seq=1 → only Seq 2,3 replayed.
// ---------------------------------------------------------------------------

func TestReconnect_HappyPath_ReplaysFilteredRequests(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{
		listResult: []*server.PendingRequest{
			makePendingRequest("req-1", 1, 0, 3),
			makePendingRequest("req-2", 2, 0, 3),
			makePendingRequest("req-3", 3, 0, 3),
		},
	}
	h, ms, rpc := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-1")

	params := map[string]any{"last_seen_seq": 1}
	req := newTestRequest("req-reconnect-1", "system.reconnect", params)

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	resp := parseReconnectResponse(t, data)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 2, resp.Replayed, "only Seq 2 and 3 should be replayed")
	assert.Equal(t, 3, resp.Total, "total should include all 3 pending")

	// The mock send function should receive replay requests for Seq 2 and 3.
	require.True(t, ms.waitForCalls(2, 2*time.Second),
		"expected 2 replay sends, got %d", ms.callCount())

	// Wait a bit for goroutines to finish and call Remove on success.
	// Since the mock send doesn't dispatch responses, replays will time out.
	// Instead, we verify the response counts directly.

	// Clean up: cancel pending ReverseRPC requests to avoid goroutine leaks.
	rpc.CancelAll()
}

// ---------------------------------------------------------------------------
// R-02: NoPendingRequests — empty list → replayed=0, total=0
// ---------------------------------------------------------------------------

func TestReconnect_NoPendingRequests(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{
		listResult: []*server.PendingRequest{},
	}
	h, _, _ := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-2")

	params := map[string]any{"last_seen_seq": 0}
	req := newTestRequest("req-reconnect-2", "system.reconnect", params)

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	resp := parseReconnectResponse(t, data)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 0, resp.Replayed)
	assert.Equal(t, 0, resp.Total)
}

// ---------------------------------------------------------------------------
// R-03: AllFilteredBySeq — all Seq ≤ last_seen_seq
// ---------------------------------------------------------------------------

func TestReconnect_AllFilteredBySeq(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{
		listResult: []*server.PendingRequest{
			makePendingRequest("req-1", 1, 0, 3),
			makePendingRequest("req-2", 2, 0, 3),
			makePendingRequest("req-3", 3, 0, 3),
		},
	}
	h, ms, rpc := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-3")

	// last_seen_seq=10 is greater than all seqs, so nothing should be replayed.
	params := map[string]any{"last_seen_seq": 10}
	req := newTestRequest("req-reconnect-3", "system.reconnect", params)

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	resp := parseReconnectResponse(t, data)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 0, resp.Replayed, "all should be filtered by seq")
	assert.Equal(t, 3, resp.Total, "total should still reflect all pending")

	// No sends should happen.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, ms.callCount(), "no replays should have been sent")

	rpc.CancelAll()
}

// ---------------------------------------------------------------------------
// R-04: AllFilteredByRetryCount — all RetryCount ≥ MaxRetries
// ---------------------------------------------------------------------------

func TestReconnect_AllFilteredByRetryCount(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{
		listResult: []*server.PendingRequest{
			makePendingRequest("req-1", 1, 3, 3), // RetryCount == MaxRetries
			makePendingRequest("req-2", 2, 4, 3), // RetryCount > MaxRetries
			makePendingRequest("req-3", 3, 5, 3), // RetryCount > MaxRetries
		},
	}
	h, ms, rpc := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-4")

	params := map[string]any{"last_seen_seq": 0}
	req := newTestRequest("req-reconnect-4", "system.reconnect", params)

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	resp := parseReconnectResponse(t, data)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 0, resp.Replayed, "all should be filtered by retry count")
	assert.Equal(t, 3, resp.Total)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, ms.callCount(), "no replays should have been sent")

	rpc.CancelAll()
}

// ---------------------------------------------------------------------------
// R-05: PendingStoreNil — nil store → {replayed:0,total:0} no panic
// ---------------------------------------------------------------------------

func TestReconnect_PendingStoreNil(t *testing.T) {
	t.Parallel()

	ms := &reconnectMockSendFunc{}
	rpc := server.NewReverseRPC(server.ReverseRPCConfig{
		SendFunc:     ms.Send,
		Logger:       reconnectNoopLogger{},
		PendingStore: nil, // explicitly nil
	})
	h := NewReconnectHandler(rpc, reconnectNoopLogger{})
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-5")

	params := map[string]any{"last_seen_seq": 0}
	req := newTestRequest("req-reconnect-5", "system.reconnect", params)

	assert.NotPanics(t, func() {
		data, err := h.HandleRequest(context.Background(), client, req)
		require.NoError(t, err)

		resp := parseReconnectResponse(t, data)
		assert.Equal(t, "ok", resp.Status)
		assert.Equal(t, 0, resp.Replayed)
		assert.Equal(t, 0, resp.Total)
	})
}

// ---------------------------------------------------------------------------
// R-06: ListError_FailOpen — List returns error → fail-open returns 0
// ---------------------------------------------------------------------------

func TestReconnect_ListError_FailOpen(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{
		listErr: errors.New("redis connection lost"),
	}
	h, _, _ := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-6")

	params := map[string]any{"last_seen_seq": 0}
	req := newTestRequest("req-reconnect-6", "system.reconnect", params)

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err, "handler should not return error on List failure (fail-open)")

	resp := parseReconnectResponse(t, data)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 0, resp.Replayed, "fail-open should return 0 replayed")
	assert.Equal(t, 0, resp.Total, "fail-open should return 0 total")
}

// ---------------------------------------------------------------------------
// R-07: EmptyParams_DefaultsToZero — nil/empty params → last_seen_seq=0
// ---------------------------------------------------------------------------

func TestReconnect_EmptyParams_DefaultsToZero(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{
		listResult: []*server.PendingRequest{
			makePendingRequest("req-1", 1, 0, 3),
			makePendingRequest("req-2", 2, 0, 3),
		},
	}
	h, ms, rpc := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-7")

	// Empty params — last_seen_seq should default to 0, so all are replayed.
	req := &protocol.PackageDataRequest{
		ID:     "req-reconnect-7",
		Method: "system.reconnect",
		Params: nil, // no params at all
	}

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	resp := parseReconnectResponse(t, data)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 2, resp.Replayed, "with default last_seen_seq=0, all should replay")
	assert.Equal(t, 2, resp.Total)

	// Verify replay sends happened.
	require.True(t, ms.waitForCalls(2, 2*time.Second),
		"expected 2 replay sends with default last_seen_seq=0")

	rpc.CancelAll()
}

// ---------------------------------------------------------------------------
// R-07b: EmptyJSONParams — empty JSON object {} → last_seen_seq=0
// ---------------------------------------------------------------------------

func TestReconnect_EmptyJSONParams_DefaultsToZero(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{
		listResult: []*server.PendingRequest{
			makePendingRequest("req-1", 1, 0, 3),
		},
	}
	h, ms, rpc := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-7b")

	// Empty JSON object — last_seen_seq should default to 0.
	req := &protocol.PackageDataRequest{
		ID:     "req-reconnect-7b",
		Method: "system.reconnect",
		Params: json.RawMessage(`{}`),
	}

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	resp := parseReconnectResponse(t, data)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 1, resp.Replayed)
	assert.Equal(t, 1, resp.Total)

	require.True(t, ms.waitForCalls(1, 2*time.Second))

	rpc.CancelAll()
}

// ---------------------------------------------------------------------------
// R-08: ResponseFormat — verify JSON response structure
// ---------------------------------------------------------------------------

func TestReconnect_ResponseFormat(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{
		listResult: []*server.PendingRequest{
			makePendingRequest("req-1", 1, 0, 3),
		},
	}
	h, _, rpc := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-8")

	params := map[string]any{"last_seen_seq": 0}
	req := newTestRequest("req-reconnect-8", "system.reconnect", params)

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	// Verify the raw JSON has exactly the expected keys.
	var rawMap map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &rawMap))

	// Must contain "status", "replayed", "total".
	assert.Contains(t, rawMap, "status")
	assert.Contains(t, rawMap, "replayed")
	assert.Contains(t, rawMap, "total")

	// Verify types by unmarshaling individually.
	var status string
	require.NoError(t, json.Unmarshal(rawMap["status"], &status))
	assert.Equal(t, "ok", status)

	var replayed int
	require.NoError(t, json.Unmarshal(rawMap["replayed"], &replayed))
	assert.Equal(t, 1, replayed)

	var total int
	require.NoError(t, json.Unmarshal(rawMap["total"], &total))
	assert.Equal(t, 1, total)

	rpc.CancelAll()
}

// ---------------------------------------------------------------------------
// R-09: ReturnsImmediately — handler returns before goroutines finish
// ---------------------------------------------------------------------------

func TestReconnect_ReturnsImmediately(t *testing.T) {
	t.Parallel()

	// Create many pending requests to amplify any blocking behavior.
	const numPending = 20
	pending := make([]*server.PendingRequest, numPending)
	for i := range numPending {
		pending[i] = makePendingRequest(
			"req-"+string(rune('A'+i)),
			uint64(i+1),
			0, 3,
		)
	}

	store := &mockReconnectPendingStore{
		listResult: pending,
	}
	h, ms, rpc := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-9")

	params := map[string]any{"last_seen_seq": 0}
	req := newTestRequest("req-reconnect-9", "system.reconnect", params)

	// Measure handler execution time.
	start := time.Now()
	data, err := h.HandleRequest(context.Background(), client, req)
	elapsed := time.Since(start)

	require.NoError(t, err)

	resp := parseReconnectResponse(t, data)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, numPending, resp.Replayed)
	assert.Equal(t, numPending, resp.Total)

	// Handler should return nearly instantly (< 100ms), not waiting for
	// all replay goroutines to complete.
	assert.Less(t, elapsed, 500*time.Millisecond,
		"handler should return immediately, not wait for replays")

	// Replays should still be happening in background (goroutines launched).
	// The mock send will accumulate calls over time.
	assert.True(t, ms.waitForCalls(numPending, 5*time.Second),
		"all %d replays should eventually be sent", numPending)

	rpc.CancelAll()
}

// ---------------------------------------------------------------------------
// R-10: InvalidParams — malformed JSON returns ValidationError
// ---------------------------------------------------------------------------

func TestReconnect_InvalidParams(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{}
	h, _, _ := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-10")

	req := &protocol.PackageDataRequest{
		ID:     "req-reconnect-10",
		Method: "system.reconnect",
		Params: json.RawMessage(`{invalid json!!!`),
	}

	_, err := h.HandleRequest(context.Background(), client, req)
	requireHandlerErrorCode(t, err, protocol.ResponseCodeValidationError)
}

// ---------------------------------------------------------------------------
// R-11: MixedFiltering — some pass seq filter, some pass retry filter, some pass both
// ---------------------------------------------------------------------------

func TestReconnect_MixedFiltering(t *testing.T) {
	t.Parallel()

	store := &mockReconnectPendingStore{
		listResult: []*server.PendingRequest{
			makePendingRequest("req-1", 1, 0, 3), // seq=1 ≤ last_seen=2 → filtered by seq
			makePendingRequest("req-2", 2, 0, 3), // seq=2 ≤ last_seen=2 → filtered by seq
			makePendingRequest("req-3", 3, 0, 3), // seq=3 > 2, retry=0 < 3 → PASS
			makePendingRequest("req-4", 4, 3, 3), // seq=4 > 2, retry=3 >= 3 → filtered by retry
			makePendingRequest("req-5", 5, 0, 3), // seq=5 > 2, retry=0 < 3 → PASS
		},
	}
	h, ms, rpc := newReconnectTestSetup(store)
	client := server.NewTestClientWithDevice("test-user", "test-device", "conn-11")

	params := map[string]any{"last_seen_seq": 2}
	req := newTestRequest("req-reconnect-11", "system.reconnect", params)

	data, err := h.HandleRequest(context.Background(), client, req)
	require.NoError(t, err)

	resp := parseReconnectResponse(t, data)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 2, resp.Replayed, "only req-3 and req-5 should be replayed")
	assert.Equal(t, 5, resp.Total)

	// Verify exactly 2 sends happen.
	require.True(t, ms.waitForCalls(2, 2*time.Second))

	rpc.CancelAll()
}
