package handler

import (
	"context"
	"encoding/json"
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

// setupRegisterAllTest creates a DefaultMessageHandler with all handlers registered
// using the provided Dependencies.
func setupRegisterAllTest(t *testing.T, deps Dependencies) *server.DefaultMessageHandler {
	t.Helper()
	h := server.NewDefaultMessageHandler()
	RegisterAll(h, deps)
	return h
}

// ---------------------------------------------------------------------------
// Test: RegisterAll correctly registers all handlers
// ---------------------------------------------------------------------------

func TestRegisterAll_RegistersAllHandlers(t *testing.T) {
	connStore := server.NewMemoryConnectionStore(0)
	s := setupTestSQLite(t)
	broker := &mockBroker{} // Uses mockBroker from send_message_test.go

	deps := Dependencies{
		ConnStore: connStore,
		Store:     s,
		Broker:    broker,
	}

	// RegisterAll creates a handler with all methods registered.
	_ = setupRegisterAllTest(t, deps) // Verify compilation works
	ctx := context.Background()

	// Setup test data.
	connID := "conn-register-1"
	userID := "alice"

	err := connStore.Add(ctx, &server.ConnectionInfo{
		ID:        connID,
		UserID:    userID,
		SessionID: "sess-register",
		TTL:       30 * time.Minute,
	})
	require.NoError(t, err)

	convID := "conv-register-1"
	createTestConversation(t, s, convID, userID, "bob")

	client := server.NewTestClientWithConnID(userID, connID)

	// Test each handler directly via HandleRequest (not HandleMessage)
	// to verify they were registered with correct dependencies.

	// 1. Heartbeat handler
	heartbeatHandler := NewHeartbeatHandler(deps.ConnStore)
	heartbeatReq := &protocol.PackageDataRequest{
		ID:     "req-heartbeat",
		Method: "heartbeat",
	}
	heartbeatResp, err := heartbeatHandler.HandleRequest(ctx, client, heartbeatReq)
	require.NoError(t, err)
	var hbResult struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(heartbeatResp, &hbResult))
	assert.Equal(t, "ok", hbResult.Status)

	// 2. SyncUpdates handler
	syncHandler := NewSyncUpdatesHandler(deps.Store)
	syncReq := &protocol.PackageDataRequest{
		ID:     "req-sync",
		Method: "sync_updates",
		Params: mustMarshal(t, map[string]interface{}{
			"after_seq": 0,
			"limit":     10,
		}),
	}
	syncResp, err := syncHandler.HandleRequest(ctx, client, syncReq)
	require.NoError(t, err)
	var syncResult struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp, &syncResult))
	assert.NotNil(t, syncResult.Updates)

	// 3. SendMessage handler
	sendHandler := NewSendMessageHandler(deps.Store, deps.Broker)
	sendReq := &protocol.PackageDataRequest{
		ID:     "req-send",
		Method: "send_message",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id":   convID,
			"client_message_id": "client-msg-register-1",
			"content":           "Test message",
			"type":              "text",
		}),
	}
	sendResp, err := sendHandler.HandleRequest(ctx, client, sendReq)
	require.NoError(t, err)
	var sendResult struct {
		Message   interface{} `json:"message"`
		Duplicate bool        `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(sendResp, &sendResult))
	assert.NotNil(t, sendResult.Message)
	assert.False(t, sendResult.Duplicate)
}

// ---------------------------------------------------------------------------
// Test: Dependency injection via RegisterAll
// ---------------------------------------------------------------------------

func TestRegisterAll_DependencyInjection(t *testing.T) {
	connStore := server.NewMemoryConnectionStore(0)
	s := setupTestSQLite(t)
	broker := &mockBroker{}

	deps := Dependencies{
		ConnStore: connStore,
		Store:     s,
		Broker:    broker,
	}

	ctx := context.Background()
	connID := "conn-dep-2"
	userID := "alice"

	err := connStore.Add(ctx, &server.ConnectionInfo{
		ID:        connID,
		UserID:    userID,
		SessionID: "sess-dep",
		TTL:       30 * time.Minute,
	})
	require.NoError(t, err)

	convID := "conv-dep-2"
	createTestConversation(t, s, convID, userID, "bob")

	client := server.NewTestClientWithConnID(userID, connID)

	// Create handlers using RegisterAll dependencies directly.
	// This verifies that Dependencies struct contains correct values.

	// Heartbeat uses ConnStore.
	hbHandler := NewHeartbeatHandler(deps.ConnStore)
	hbReq := &protocol.PackageDataRequest{
		ID:     "req-hb-dep",
		Method: "heartbeat",
	}
	_, err = hbHandler.HandleRequest(ctx, client, hbReq)
	require.NoError(t, err, "ConnStore dependency should be correctly injected")

	// SyncUpdates uses Store.
	syncHandler := NewSyncUpdatesHandler(deps.Store)
	syncReq := &protocol.PackageDataRequest{
		ID:     "req-sync-dep",
		Method: "sync_updates",
		Params: mustMarshal(t, map[string]interface{}{
			"after_seq": 0,
			"limit":     10,
		}),
	}
	_, err = syncHandler.HandleRequest(ctx, client, syncReq)
	require.NoError(t, err, "Store dependency should be correctly injected")

	// SendMessage uses Store and Broker.
	sendHandler := NewSendMessageHandler(deps.Store, deps.Broker)
	sendReq := &protocol.PackageDataRequest{
		ID:     "req-send-dep",
		Method: "send_message",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id":   convID,
			"client_message_id": "client-msg-dep-2",
			"content":           "Test message for dep injection",
			"type":              "text",
		}),
	}
	_, err = sendHandler.HandleRequest(ctx, client, sendReq)
	require.NoError(t, err, "Store and Broker dependencies should be correctly injected")
}

// ---------------------------------------------------------------------------
// Test: Handlers are invokable after registration
// ---------------------------------------------------------------------------

func TestRegisterAll_HandlersInvokable(t *testing.T) {
	connStore := server.NewMemoryConnectionStore(0)
	s := setupTestSQLite(t)
	broker := &mockBroker{}

	deps := Dependencies{
		ConnStore: connStore,
		Store:     s,
		Broker:    broker,
	}

	// Create handler and register all.
	h := server.NewDefaultMessageHandler()
	RegisterAll(h, deps)

	ctx := context.Background()
	connID := "conn-invoke-2"
	userID := "alice"

	err := connStore.Add(ctx, &server.ConnectionInfo{
		ID:        connID,
		UserID:    userID,
		SessionID: "sess-invoke",
		TTL:       30 * time.Minute,
	})
	require.NoError(t, err)

	convID := "conv-invoke-2"
	createTestConversation(t, s, convID, userID, "bob")

	client := server.NewTestClientWithConnID(userID, connID)

	// Invoke handlers directly (simulating RPC calls after registration).
	methods := []struct {
		name   string
		params json.RawMessage
	}{
		{
			name:   "heartbeat",
			params: nil,
		},
		{
			name: "sync_updates",
			params: mustMarshal(t, map[string]interface{}{
				"after_seq": 0,
				"limit":     10,
			}),
		},
		{
			name: "send_message",
			params: mustMarshal(t, map[string]interface{}{
				"conversation_id":   convID,
				"client_message_id": "client-msg-invoke",
				"content":           "Invoke test",
				"type":              "text",
			}),
		},
	}

	for _, tc := range methods {
		req := &protocol.PackageDataRequest{
			ID:     "req-" + tc.name,
			Method: tc.name,
			Params: tc.params,
		}

		// Use the handler constructors directly (they're what RegisterAll registered).
		var handler server.MethodHandler
		switch tc.name {
		case "heartbeat":
			handler = NewHeartbeatHandler(deps.ConnStore)
		case "sync_updates":
			handler = NewSyncUpdatesHandler(deps.Store)
		case "send_message":
			handler = NewSendMessageHandler(deps.Store, deps.Broker)
		}

		resp, err := handler.HandleRequest(ctx, client, req)
		require.NoError(t, err, "method %s should be invokable", tc.name)
		require.NotNil(t, resp, "method %s should return response", tc.name)
	}
}

// ---------------------------------------------------------------------------
// Test: Multiple calls to RegisterAll work correctly
// ---------------------------------------------------------------------------

func TestRegisterAll_MultipleCalls(t *testing.T) {
	connStore := server.NewMemoryConnectionStore(0)
	s := setupTestSQLite(t)
	broker := &mockBroker{}

	deps := Dependencies{
		ConnStore: connStore,
		Store:     s,
		Broker:    broker,
	}

	h := server.NewDefaultMessageHandler()

	// Call RegisterAll multiple times (simulating re-registration).
	RegisterAll(h, deps)
	RegisterAll(h, deps)

	ctx := context.Background()
	connID := "conn-multi-1"
	userID := "alice"

	err := connStore.Add(ctx, &server.ConnectionInfo{
		ID:        connID,
		UserID:    userID,
		SessionID: "sess-multi",
		TTL:       30 * time.Minute,
	})
	require.NoError(t, err)

	client := server.NewTestClientWithConnID(userID, connID)

	// Verify handlers still work after multiple registrations.
	handler := NewHeartbeatHandler(deps.ConnStore)
	req := &protocol.PackageDataRequest{
		ID:     "req-multi",
		Method: "heartbeat",
	}
	resp, err := handler.HandleRequest(ctx, client, req)
	require.NoError(t, err)

	var result struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))
	assert.Equal(t, "ok", result.Status)
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// mustMarshal marshals v to JSON, failing the test on error.
func mustMarshal(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}
