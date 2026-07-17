package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// parseHeartbeatResponse unmarshals the handler's response data and returns
// the "status" field.
func parseHeartbeatResponse(t *testing.T, data json.RawMessage) string {
	t.Helper()
	var resp heartbeatResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	return resp.Status
}

// failingRefreshStore embeds MemoryConnectionStore but overrides Refresh to
// always return a configurable error. Used to simulate HB-03 (non-not-found
// Refresh failure).
type failingRefreshStore struct {
	*server.MemoryConnectionStore
	refreshErr error
}

func (s *failingRefreshStore) Refresh(_ context.Context, connID string) error {
	return s.refreshErr
}

// newHeartbeatRequest creates a PackageDataRequest for the "heartbeat" method
// with the given params marshaled as JSON. If params is nil, the Params field
// is left empty.
func newHeartbeatRequest(id string, params interface{}) *protocol.PackageDataRequest {
	if params == nil {
		return &protocol.PackageDataRequest{
			ID:     id,
			Method: "heartbeat",
		}
	}
	data, err := json.Marshal(params)
	if err != nil {
		panic(err)
	}
	return &protocol.PackageDataRequest{
		ID:     id,
		Method: "heartbeat",
		Params: data,
	}
}

// ---------------------------------------------------------------------------
// HB-01: HappyPath – Refresh succeeds, returns {status: "ok"}
// ---------------------------------------------------------------------------

func TestHeartbeat_HappyPath(t *testing.T) {
	store := server.NewMemoryConnectionStore(0)
	handler := NewHeartbeatHandler(store, nil)
	ctx := context.Background()

	connID := "conn-hb-01"
	userID := "alice"

	// Register a connection in the store.
	now := time.Now()
	err := store.Add(ctx, &server.ConnectionInfo{
		ID:        connID,
		UserID:    userID,
		SessionID: "sess-1",
		CreatedAt: now,
		UpdatedAt: now,
		TTL:       30 * time.Minute,
	})
	require.NoError(t, err)

	// Capture the UpdatedAt before the heartbeat.
	beforeRefresh, err := store.Get(ctx, connID)
	require.NoError(t, err)
	originalUpdatedAt := beforeRefresh.UpdatedAt

	// Small sleep to ensure time difference is measurable.
	time.Sleep(5 * time.Millisecond)

	// Invoke the handler.
	client := server.NewTestClientWithConnID(userID, connID)
	req := newHeartbeatRequest("req-1", nil)
	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err, "heartbeat should succeed")

	// Verify response status.
	status := parseHeartbeatResponse(t, data)
	assert.Equal(t, "ok", status)

	// Verify UpdatedAt was updated (TTL refreshed).
	afterRefresh, err := store.Get(ctx, connID)
	require.NoError(t, err)
	assert.True(t, afterRefresh.UpdatedAt.After(originalUpdatedAt),
		"UpdatedAt should be refreshed after heartbeat (before=%v, after=%v)",
		originalUpdatedAt, afterRefresh.UpdatedAt)
}

// ---------------------------------------------------------------------------
// HB-02: ConnectionExpired – Refresh returns ErrConnectionNotFound
// ---------------------------------------------------------------------------

func TestHeartbeat_ConnectionExpired(t *testing.T) {
	store := server.NewMemoryConnectionStore(0)
	handler := NewHeartbeatHandler(store, nil)
	ctx := context.Background()

	// Use a connID that does NOT exist in the store.
	client := server.NewTestClientWithConnID("alice", "nonexistent-conn")
	req := newHeartbeatRequest("req-2", nil)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err, "heartbeat should fail for expired/missing connection")
	assert.Contains(t, err.Error(), "connection expired")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeNotFound, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// HB-03: RefreshFailed – Refresh returns a non-ErrConnectionNotFound error
// ---------------------------------------------------------------------------

func TestHeartbeat_RefreshFailed(t *testing.T) {
	// Use a failing store that always returns a generic error from Refresh.
	memStore := server.NewMemoryConnectionStore(0)
	failingStore := &failingRefreshStore{
		MemoryConnectionStore: memStore,
		refreshErr:            fmt.Errorf("simulated redis timeout"),
	}
	handler := NewHeartbeatHandler(failingStore, nil)
	ctx := context.Background()

	client := server.NewTestClientWithConnID("alice", "conn-hb-03")
	req := newHeartbeatRequest("req-3", nil)

	_, err := handler.HandleRequest(ctx, client, req)
	require.Error(t, err, "heartbeat should fail when Refresh returns an error")
	assert.Contains(t, err.Error(), "refresh connection")
	assert.Contains(t, err.Error(), "simulated redis timeout")
	var handlerErr *protocol.HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeInternalError, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// HB-04: WithDeviceInfo – device_info is passed, handler still succeeds
// ---------------------------------------------------------------------------

func TestHeartbeat_WithDeviceInfo(t *testing.T) {
	store := server.NewMemoryConnectionStore(0)
	handler := NewHeartbeatHandler(store, nil)
	ctx := context.Background()

	connID := "conn-hb-04"
	userID := "alice"

	err := store.Add(ctx, &server.ConnectionInfo{
		ID:        connID,
		UserID:    userID,
		SessionID: "sess-1",
		TTL:       30 * time.Minute,
	})
	require.NoError(t, err)

	params := heartbeatParams{
		DeviceInfo: map[string]string{
			"os":          "iOS 17.0",
			"app_version": "1.2.3",
			"battery":     "85%",
		},
	}

	client := server.NewTestClientWithConnID(userID, connID)
	req := newHeartbeatRequest("req-4", params)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err, "heartbeat with device_info should succeed")

	status := parseHeartbeatResponse(t, data)
	assert.Equal(t, "ok", status)
}

// ---------------------------------------------------------------------------
// HB-05: EmptyParams – no params at all, handler still succeeds
// ---------------------------------------------------------------------------

func TestHeartbeat_EmptyParams(t *testing.T) {
	store := server.NewMemoryConnectionStore(0)
	handler := NewHeartbeatHandler(store, nil)
	ctx := context.Background()

	connID := "conn-hb-05"
	userID := "bob"

	err := store.Add(ctx, &server.ConnectionInfo{
		ID:        connID,
		UserID:    userID,
		SessionID: "sess-2",
		TTL:       30 * time.Minute,
	})
	require.NoError(t, err)

	// Request with no params (nil params).
	client := server.NewTestClientWithConnID(userID, connID)
	req := newHeartbeatRequest("req-5", nil)

	data, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err, "heartbeat with empty params should succeed")

	status := parseHeartbeatResponse(t, data)
	assert.Equal(t, "ok", status)
}
