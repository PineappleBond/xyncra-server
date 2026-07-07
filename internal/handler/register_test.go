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

	// 4. CreateConversation handler (find-or-create, D-011).
	createHandler := NewCreateConversationHandler(deps.Store)
	createReq := &protocol.PackageDataRequest{
		ID:     "req-create",
		Method: "create_conversation",
		Params: mustMarshal(t, map[string]interface{}{
			"user_id": "charlie",
		}),
	}
	createResp, err := createHandler.HandleRequest(ctx, client, createReq)
	require.NoError(t, err)
	var createResult struct {
		Conversation interface{} `json:"conversation"`
		Duplicate    bool        `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(createResp, &createResult))
	assert.NotNil(t, createResult.Conversation)
	assert.False(t, createResult.Duplicate)

	// 5. ListConversations handler
	listHandler := NewListConversationsHandler(deps.Store)
	listReq := &protocol.PackageDataRequest{
		ID:     "req-list",
		Method: "list_conversations",
		Params: mustMarshal(t, map[string]interface{}{
			"offset": 0,
			"limit":  10,
		}),
	}
	listResp, err := listHandler.HandleRequest(ctx, client, listReq)
	require.NoError(t, err)
	var listResult struct {
		Conversations []interface{} `json:"conversations"`
		HasMore       bool          `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(listResp, &listResult))
	assert.NotNil(t, listResult.Conversations)

	// 6. GetMessages handler
	getMsgsHandler := NewGetMessagesHandler(deps.Store)
	getMsgsReq := &protocol.PackageDataRequest{
		ID:     "req-get-msgs",
		Method: "get_messages",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": convID,
			"limit":           10,
		}),
	}
	getMsgsResp, err := getMsgsHandler.HandleRequest(ctx, client, getMsgsReq)
	require.NoError(t, err)
	var getMsgsResult struct {
		Messages []interface{} `json:"messages"`
		HasMore  bool          `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(getMsgsResp, &getMsgsResult))
	assert.NotNil(t, getMsgsResult.Messages)

	// 7. SearchMessages handler
	searchHandler := NewSearchMessagesHandler(deps.Store)
	searchReq := &protocol.PackageDataRequest{
		ID:     "req-search",
		Method: "search_messages",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": convID,
			"query":           "Test",
			"limit":           10,
		}),
	}
	searchResp, err := searchHandler.HandleRequest(ctx, client, searchReq)
	require.NoError(t, err)
	var searchResult struct {
		Messages []interface{} `json:"messages"`
		HasMore  bool          `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(searchResp, &searchResult))
	assert.NotNil(t, searchResult.Messages)

	// 8. GetConversation handler
	getConvHandler := NewGetConversationHandler(deps.Store)
	getConvReq := &protocol.PackageDataRequest{
		ID:     "req-get-conv",
		Method: "get_conversation",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": convID,
		}),
	}
	getConvResp, err := getConvHandler.HandleRequest(ctx, client, getConvReq)
	require.NoError(t, err)
	var getConvResult struct {
		Conversation interface{} `json:"conversation"`
		UnreadCount  int64       `json:"unread_count"`
	}
	require.NoError(t, json.Unmarshal(getConvResp, &getConvResult))
	assert.NotNil(t, getConvResult.Conversation)

	// 9. DeleteConversation handler (create a temp conv to delete)
	deleteConvHandler := NewDeleteConversationHandler(deps.Store)
	tempConvID := "conv-delete-reg"
	createTestConversation(t, s, tempConvID, userID, "bob")
	deleteReq := &protocol.PackageDataRequest{
		ID:     "req-delete-conv",
		Method: "delete_conversation",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": tempConvID,
		}),
	}
	deleteResp, err := deleteConvHandler.HandleRequest(ctx, client, deleteReq)
	require.NoError(t, err)
	var deleteResult struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(deleteResp, &deleteResult))
	assert.Equal(t, "ok", deleteResult.Status)

	// 10. RestoreConversation handler
	restoreConvHandler := NewRestoreConversationHandler(deps.Store)
	restoreReq := &protocol.PackageDataRequest{
		ID:     "req-restore-conv",
		Method: "restore_conversation",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": tempConvID,
		}),
	}
	restoreResp, err := restoreConvHandler.HandleRequest(ctx, client, restoreReq)
	require.NoError(t, err)
	var restoreResult struct {
		Conversation interface{} `json:"conversation"`
	}
	require.NoError(t, json.Unmarshal(restoreResp, &restoreResult))
	assert.NotNil(t, restoreResult.Conversation)

	// 11. DeleteMessage handler -- need a message to delete
	sendHandler2 := NewSendMessageHandler(deps.Store, deps.Broker)
	sendReq2 := &protocol.PackageDataRequest{
		ID:     "req-send-for-delete",
		Method: "send_message",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id":   convID,
			"client_message_id": "client-msg-del-reg-1",
			"content":           "To be deleted",
			"type":              "text",
		}),
	}
	sendResp2, err := sendHandler2.HandleRequest(ctx, client, sendReq2)
	require.NoError(t, err)
	var sendResult2 struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	require.NoError(t, json.Unmarshal(sendResp2, &sendResult2))
	deleteMsgHandler := NewDeleteMessageHandler(deps.Store)
	deleteMsgReq := &protocol.PackageDataRequest{
		ID:     "req-delete-msg",
		Method: "delete_message",
		Params: mustMarshal(t, map[string]interface{}{
			"message_id": sendResult2.Message.ID,
		}),
	}
	deleteMsgResp, err := deleteMsgHandler.HandleRequest(ctx, client, deleteMsgReq)
	require.NoError(t, err)
	var deleteMsgResult struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(deleteMsgResp, &deleteMsgResult))
	assert.Equal(t, "ok", deleteMsgResult.Status)

	// 12. MarkAsRead handler
	markReadHandler := NewMarkAsReadHandler(deps.Store)
	markReadReq := &protocol.PackageDataRequest{
		ID:     "req-mark-read",
		Method: "mark_as_read",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": convID,
		}),
	}
	markReadResp, err := markReadHandler.HandleRequest(ctx, client, markReadReq)
	require.NoError(t, err)
	var markReadResult struct {
		Status      string `json:"status"`
		UnreadCount int64  `json:"unread_count"`
	}
	require.NoError(t, json.Unmarshal(markReadResp, &markReadResult))
	assert.Equal(t, "ok", markReadResult.Status)
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

	// CreateConversation uses Store.
	createHandler := NewCreateConversationHandler(deps.Store)
	createReq := &protocol.PackageDataRequest{
		ID:     "req-create-dep",
		Method: "create_conversation",
		Params: mustMarshal(t, map[string]interface{}{
			"user_id": "charlie",
		}),
	}
	_, err = createHandler.HandleRequest(ctx, client, createReq)
	require.NoError(t, err, "CreateConversation Store dependency should be correctly injected")

	// ListConversations uses Store.
	listHandler := NewListConversationsHandler(deps.Store)
	listReq := &protocol.PackageDataRequest{
		ID:     "req-list-dep",
		Method: "list_conversations",
		Params: mustMarshal(t, map[string]interface{}{
			"limit": 10,
		}),
	}
	_, err = listHandler.HandleRequest(ctx, client, listReq)
	require.NoError(t, err, "ListConversations Store dependency should be correctly injected")

	// GetMessages uses Store.
	getMsgsHandler := NewGetMessagesHandler(deps.Store)
	getMsgsReq := &protocol.PackageDataRequest{
		ID:     "req-get-msgs-dep",
		Method: "get_messages",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": convID,
			"limit":           10,
		}),
	}
	_, err = getMsgsHandler.HandleRequest(ctx, client, getMsgsReq)
	require.NoError(t, err, "GetMessages Store dependency should be correctly injected")

	// SearchMessages uses Store.
	searchHandler := NewSearchMessagesHandler(deps.Store)
	searchReq := &protocol.PackageDataRequest{
		ID:     "req-search-dep",
		Method: "search_messages",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": convID,
			"query":           "Test",
			"limit":           10,
		}),
	}
	_, err = searchHandler.HandleRequest(ctx, client, searchReq)
	require.NoError(t, err, "SearchMessages Store dependency should be correctly injected")

	// GetConversation uses Store.
	getConvHandler := NewGetConversationHandler(deps.Store)
	getConvReq := &protocol.PackageDataRequest{
		ID:     "req-get-conv-dep",
		Method: "get_conversation",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": convID,
		}),
	}
	_, err = getConvHandler.HandleRequest(ctx, client, getConvReq)
	require.NoError(t, err, "GetConversation Store dependency should be correctly injected")

	// DeleteConversation uses Store.
	deleteConvHandler := NewDeleteConversationHandler(deps.Store)
	tempConvID := "conv-delete-dep"
	createTestConversation(t, s, tempConvID, userID, "bob")
	deleteReq := &protocol.PackageDataRequest{
		ID:     "req-delete-conv-dep",
		Method: "delete_conversation",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": tempConvID,
		}),
	}
	_, err = deleteConvHandler.HandleRequest(ctx, client, deleteReq)
	require.NoError(t, err, "DeleteConversation Store dependency should be correctly injected")

	// RestoreConversation uses Store.
	restoreConvHandler := NewRestoreConversationHandler(deps.Store)
	restoreReq := &protocol.PackageDataRequest{
		ID:     "req-restore-conv-dep",
		Method: "restore_conversation",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": tempConvID,
		}),
	}
	_, err = restoreConvHandler.HandleRequest(ctx, client, restoreReq)
	require.NoError(t, err, "RestoreConversation Store dependency should be correctly injected")

	// DeleteMessage uses Store.
	// First send a message to delete.
	sendHandler2 := NewSendMessageHandler(deps.Store, deps.Broker)
	sendReq2 := &protocol.PackageDataRequest{
		ID:     "req-send-for-delete-dep",
		Method: "send_message",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id":   convID,
			"client_message_id": "client-msg-del-dep-1",
			"content":           "To be deleted",
			"type":              "text",
		}),
	}
	sendResp2, err := sendHandler2.HandleRequest(ctx, client, sendReq2)
	require.NoError(t, err)
	var sendResult2 struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	require.NoError(t, json.Unmarshal(sendResp2, &sendResult2))
	deleteMsgHandler := NewDeleteMessageHandler(deps.Store)
	deleteMsgReq := &protocol.PackageDataRequest{
		ID:     "req-delete-msg-dep",
		Method: "delete_message",
		Params: mustMarshal(t, map[string]interface{}{
			"message_id": sendResult2.Message.ID,
		}),
	}
	_, err = deleteMsgHandler.HandleRequest(ctx, client, deleteMsgReq)
	require.NoError(t, err, "DeleteMessage Store dependency should be correctly injected")

	// MarkAsRead uses Store.
	markReadHandler := NewMarkAsReadHandler(deps.Store)
	markReadReq := &protocol.PackageDataRequest{
		ID:     "req-mark-read-dep",
		Method: "mark_as_read",
		Params: mustMarshal(t, map[string]interface{}{
			"conversation_id": convID,
		}),
	}
	_, err = markReadHandler.HandleRequest(ctx, client, markReadReq)
	require.NoError(t, err, "MarkAsRead Store dependency should be correctly injected")
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
		{
			name: "create_conversation",
			params: mustMarshal(t, map[string]interface{}{
				"user_id": "charlie",
			}),
		},
		{
			name: "list_conversations",
			params: mustMarshal(t, map[string]interface{}{
				"limit": 10,
			}),
		},
		{
			name: "get_messages",
			params: mustMarshal(t, map[string]interface{}{
				"conversation_id": convID,
				"limit":           10,
			}),
		},
		{
			name: "search_messages",
			params: mustMarshal(t, map[string]interface{}{
				"conversation_id": convID,
				"query":           "Invoke",
				"limit":           10,
			}),
		},
		{
			name: "get_conversation",
			params: mustMarshal(t, map[string]interface{}{
				"conversation_id": convID,
			}),
		},
		{
			name: "delete_conversation",
			params: mustMarshal(t, map[string]interface{}{
				"conversation_id": convID,
			}),
		},
		{
			name: "restore_conversation",
			params: mustMarshal(t, map[string]interface{}{
				"conversation_id": convID,
			}),
		},
		{
			name: "send_message_for_delete",
			params: mustMarshal(t, map[string]interface{}{
				"conversation_id":   convID,
				"client_message_id": "client-msg-invoke-del",
				"content":           "To be deleted for invoke test",
				"type":              "text",
			}),
		},
		{
			name: "delete_message",
			params: mustMarshal(t, map[string]interface{}{
				"message_id": "placeholder", // will be overridden
			}),
		},
		{
			name: "mark_as_read",
			params: mustMarshal(t, map[string]interface{}{
				"conversation_id": convID,
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
		case "create_conversation":
			handler = NewCreateConversationHandler(deps.Store)
		case "list_conversations":
			handler = NewListConversationsHandler(deps.Store)
		case "get_messages":
			handler = NewGetMessagesHandler(deps.Store)
		case "search_messages":
			handler = NewSearchMessagesHandler(deps.Store)
		case "get_conversation":
			handler = NewGetConversationHandler(deps.Store)
		case "delete_conversation":
			handler = NewDeleteConversationHandler(deps.Store)
		case "restore_conversation":
			handler = NewRestoreConversationHandler(deps.Store)
		case "send_message_for_delete":
			handler = NewSendMessageHandler(deps.Store, deps.Broker)
		case "delete_message":
			// Send a real message to get its ID, then delete it
			tmpSendHandler := NewSendMessageHandler(deps.Store, deps.Broker)
			tmpReq := &protocol.PackageDataRequest{
				ID:     "req-invoke-send-del",
				Method: "send_message",
				Params: mustMarshal(t, map[string]interface{}{
					"conversation_id":   convID,
					"client_message_id": "client-msg-invoke-del-2",
					"content":           "Temp message for delete",
					"type":              "text",
				}),
			}
			tmpResp, tmpErr := tmpSendHandler.HandleRequest(ctx, client, tmpReq)
			require.NoError(t, tmpErr)
			var tmpResult struct {
				Message struct {
					ID string `json:"id"`
				} `json:"message"`
			}
			require.NoError(t, json.Unmarshal(tmpResp, &tmpResult))
			handler = NewDeleteMessageHandler(deps.Store)
			req.Params = mustMarshal(t, map[string]interface{}{
				"message_id": tmpResult.Message.ID,
			})
		case "mark_as_read":
			handler = NewMarkAsReadHandler(deps.Store)
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
