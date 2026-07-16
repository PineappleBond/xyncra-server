package client

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// D1-D4: Ephemeral conversation update — updated_at comparison (D-124)
//
// These tests verify the D-124 optimisation: when an ephemeral (Seq=0)
// conversation "update" notification arrives with an updated_at timestamp,
// the client compares it to the local cache and skips the get_conversation
// RPC when the local data is already current.
// ---------------------------------------------------------------------------

// newConvUpdateSyncManager creates a syncManager with a counting rpcFn that
// returns a fixed conversation for get_conversation calls.
func newConvUpdateSyncManager(t *testing.T, conv *model.Conversation) (
	sm *syncManager,
	rpcCalls *int32,
	handler *mockUpdateHandler,
) {
	t.Helper()
	db := newTestStore(t)
	handler = &mockUpdateHandler{}
	logger := &testLogger{t: t}

	var rpcCallsCount int32
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		atomic.AddInt32(&rpcCallsCount, 1)
		if method == "get_conversation" && conv != nil {
			resp := map[string]any{"conversation": conv}
			data, _ := json.Marshal(resp)
			return data, nil
		}
		return json.RawMessage(`{}`), nil
	}
	sm = newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	t.Cleanup(func() { sm.Stop() })
	return sm, &rpcCallsCount, handler
}

// seedLocalConversation creates a conversation in the local DB with a specific
// UpdatedAt timestamp for D-124 comparison tests.
func seedLocalConversation(t *testing.T, sm *syncManager, convID string, updatedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	conv := &model.Conversation{
		ID:        convID,
		UserID1:   "test-user",
		UserID2:   "other-user",
		Type:      "1-on-1",
		Title:     "Test",
		CreatedAt: updatedAt,
		UpdatedAt: updatedAt,
	}
	require.NoError(t, sm.db.Conversations.Create(ctx, conv))
}

// makeEphemeralConvUpdate constructs a Seq=0 conversation "update" payload
// with the given updated_at value.
func makeEphemeralConvUpdate(convID string, updatedAt int64) *protocol.PackageDataUpdate {
	payload := conversationUpdatePayload{
		ConversationID: convID,
		Action:         "update",
		UpdatedAt:      updatedAt,
	}
	data, _ := json.Marshal(payload)
	return &protocol.PackageDataUpdate{
		Seq:     0, // ephemeral
		Type:    protocol.UpdateTypeConversation,
		Payload: data,
	}
}

// ---------------------------------------------------------------------------
// D1: updated_at equals local → skip RPC
// ---------------------------------------------------------------------------

// TestEphemeralConvUpdate_SkipsRPCWhenLocalIsCurrent verifies that when the
// payload's updated_at matches the local conversation's UpdatedAt (Unix
// seconds), the get_conversation RPC is NOT called (D-124).
func TestEphemeralConvUpdate_SkipsRPCWhenLocalIsCurrent(t *testing.T) {
	localTime := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	serverConv := &model.Conversation{
		ID: "conv-d1", UserID1: "test-user", UserID2: "other-user",
		Type: "1-on-1", UpdatedAt: localTime,
	}
	sm, rpcCalls, handler := newConvUpdateSyncManager(t, serverConv)

	// Seed local conversation with the same timestamp.
	seedLocalConversation(t, sm, "conv-d1", localTime)

	// Send an ephemeral update with updated_at matching local.
	update := makeEphemeralConvUpdate("conv-d1", localTime.Unix())
	require.NoError(t, sm.ApplyUpdate(context.Background(), update))

	// Verify RPC was NOT called.
	assert.Equal(t, int32(0), atomic.LoadInt32(rpcCalls),
		"RPC should NOT be called when local cache is current")

	// Verify handler was notified with local conversation data.
	handler.mu.Lock()
	require.Len(t, handler.conversations, 1, "handler should be notified with local data")
	assert.Equal(t, "conv-d1", handler.conversations[0].ID)
	handler.mu.Unlock()
}

// ---------------------------------------------------------------------------
// D2: updated_at differs from local → execute RPC
// ---------------------------------------------------------------------------

// TestEphemeralConvUpdate_PerformsRPCWhenStale verifies that when the
// payload's updated_at is newer than the local conversation's UpdatedAt,
// the get_conversation RPC IS called to fetch fresh data (D-124).
func TestEphemeralConvUpdate_PerformsRPCWhenStale(t *testing.T) {
	localTime := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	newerTime := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	serverConv := &model.Conversation{
		ID: "conv-d2", UserID1: "test-user", UserID2: "other-user",
		Type: "1-on-1", Title: "Updated Title", UpdatedAt: newerTime,
	}
	sm, rpcCalls, handler := newConvUpdateSyncManager(t, serverConv)

	// Seed local conversation with an older timestamp.
	seedLocalConversation(t, sm, "conv-d2", localTime)

	// Send an ephemeral update with a newer updated_at.
	update := makeEphemeralConvUpdate("conv-d2", newerTime.Unix())
	require.NoError(t, sm.ApplyUpdate(context.Background(), update))

	// Verify RPC WAS called.
	assert.Equal(t, int32(1), atomic.LoadInt32(rpcCalls),
		"RPC should be called when local cache is stale")

	// Verify handler was notified with the fresh server data.
	handler.mu.Lock()
	require.Len(t, handler.conversations, 1, "handler should be notified with fresh data")
	assert.Equal(t, "conv-d2", handler.conversations[0].ID)
	assert.Equal(t, "Updated Title", handler.conversations[0].Title)
	handler.mu.Unlock()
}

// ---------------------------------------------------------------------------
// D3: updated_at == 0 → execute RPC (backward compatibility)
// ---------------------------------------------------------------------------

// TestEphemeralConvUpdate_PerformsRPCWhenUpdatedAtIsZero verifies that when
// the payload's updated_at is 0 (absent, old server), the get_conversation
// RPC is always called for backward compatibility (D-124).
func TestEphemeralConvUpdate_PerformsRPCWhenUpdatedAtIsZero(t *testing.T) {
	localTime := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	serverConv := &model.Conversation{
		ID: "conv-d3", UserID1: "test-user", UserID2: "other-user",
		Type: "1-on-1", UpdatedAt: localTime,
	}
	sm, rpcCalls, _ := newConvUpdateSyncManager(t, serverConv)

	// Seed local conversation with a valid timestamp.
	seedLocalConversation(t, sm, "conv-d3", localTime)

	// Send an ephemeral update with updated_at = 0 (old server behavior).
	update := makeEphemeralConvUpdate("conv-d3", 0)
	require.NoError(t, sm.ApplyUpdate(context.Background(), update))

	// Verify RPC WAS called (backward compat: no timestamp means always fetch).
	assert.Equal(t, int32(1), atomic.LoadInt32(rpcCalls),
		"RPC should be called when updated_at is 0 (backward compat)")
}

// ---------------------------------------------------------------------------
// D4: no local cache → execute RPC (first fetch)
// ---------------------------------------------------------------------------

// TestEphemeralConvUpdate_PerformsRPCWhenNoLocalCache verifies that when
// the conversation is not in the local DB (first fetch), the RPC is always
// called regardless of the updated_at value (D-124).
func TestEphemeralConvUpdate_PerformsRPCWhenNoLocalCache(t *testing.T) {
	serverTime := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	serverConv := &model.Conversation{
		ID: "conv-d4", UserID1: "test-user", UserID2: "other-user",
		Type: "1-on-1", Title: "From Server", UpdatedAt: serverTime,
	}
	sm, rpcCalls, handler := newConvUpdateSyncManager(t, serverConv)

	// Do NOT seed any local conversation — simulating first fetch.

	// Send an ephemeral update with a valid updated_at.
	update := makeEphemeralConvUpdate("conv-d4", serverTime.Unix())
	require.NoError(t, sm.ApplyUpdate(context.Background(), update))

	// Verify RPC WAS called (no local cache → must fetch).
	assert.Equal(t, int32(1), atomic.LoadInt32(rpcCalls),
		"RPC should be called when local cache is missing")

	// Verify handler was notified with the fetched data.
	handler.mu.Lock()
	require.Len(t, handler.conversations, 1, "handler should be notified with fetched data")
	assert.Equal(t, "conv-d4", handler.conversations[0].ID)
	assert.Equal(t, "From Server", handler.conversations[0].Title)
	handler.mu.Unlock()
}

// ---------------------------------------------------------------------------
// D5: RPC failure → fall back to handler with minimal data (D-118 degraded)
// ---------------------------------------------------------------------------

// TestEphemeralConvUpdate_FallsBackToHandlerOnRPCFailure verifies that when
// the get_conversation RPC fails, the handler is still called with minimal
// data (just the conversation ID) and no panic occurs (D-118 degraded mode).
func TestEphemeralConvUpdate_FallsBackToHandlerOnRPCFailure(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}

	var rpcCallsCount int32
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		atomic.AddInt32(&rpcCallsCount, 1)
		if method == "get_conversation" {
			return json.RawMessage(`{}`), errors.New("simulated RPC failure")
		}
		return json.RawMessage(`{}`), nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	t.Cleanup(func() { sm.Stop() })

	// Seed local conversation with an older timestamp so the RPC is attempted.
	localTime := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	seedLocalConversation(t, sm, "conv-d5", localTime)

	// Send an ephemeral update with a newer updated_at → RPC will be called and fail.
	newerTime := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	update := makeEphemeralConvUpdate("conv-d5", newerTime.Unix())
	require.NoError(t, sm.ApplyUpdate(context.Background(), update))

	// Verify RPC was called.
	assert.Equal(t, int32(1), atomic.LoadInt32(&rpcCallsCount),
		"RPC should be called when local cache is stale")

	// Verify handler was still notified with minimal data (just the ID).
	handler.mu.Lock()
	require.Len(t, handler.conversations, 1,
		"handler should be notified even when RPC fails (D-118 degraded)")
	assert.Equal(t, "conv-d5", handler.conversations[0].ID,
		"handler should receive conversation ID in degraded mode")
	handler.mu.Unlock()
}
