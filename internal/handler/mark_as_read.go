package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// markAsReadParams is the JSON-decoded representation of the client-supplied
// parameters for the "mark_as_read" method.
type markAsReadParams struct {
	ConversationID string `json:"conversation_id"` // required
	MessageID      uint32 `json:"message_id"`      // optional, defaults to LastProcessedMessageID (mark all as read)
}

// markAsReadResponse is the success response payload returned to the client
// after a mark_as_read call.
type markAsReadResponse struct {
	Status            string `json:"status"`
	UnreadCount       int64  `json:"unread_count"`
	LastReadMessageID uint32 `json:"last_read_message_id"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// markAsReadHandler implements MethodHandler for the "mark_as_read" method.
// It updates the caller's read cursor position in a conversation using MAX
// semantics (D-012): the read cursor can only advance forward, never go back.
//
// The handler is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
type markAsReadHandler struct {
	store store.StoreAPI
}

// NewMarkAsReadHandler creates a markAsReadHandler backed by the given Store.
func NewMarkAsReadHandler(store store.StoreAPI) *markAsReadHandler {
	return &markAsReadHandler{store: store}
}

// HandleRequest implements MethodHandler. It processes a "mark_as_read" RPC
// call: validates parameters, verifies the caller is a member of the
// conversation, updates the read cursor with MAX semantics (D-012), and
// returns the updated unread count.
//
// Errors:
//   - Missing or empty conversation_id: "missing required field: conversation_id"
//   - Conversation not found: "conversation not found"
//   - Caller is not a member: "user is not a member of the conversation"
//   - Store errors are wrapped and returned to the caller.
func (h *markAsReadHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params markAsReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// Validate required fields.
	if params.ConversationID == "" {
		return nil, protocol.NewValidationError("missing required field: conversation_id")
	}

	// 2. Fetch conversation and verify it exists.
	conv, err := h.store.ConversationStore().Get(ctx, params.ConversationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("get conversation: %w", err))
	}

	// 3. Verify membership.
	userID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, userID) {
		return nil, protocol.NewPermissionDeniedError("user is not a member of the conversation")
	}

	// 4. Determine target messageID.
	// If params.MessageID > 0, use it. Otherwise use LastProcessedMessageID
	// (mark ALL as read).
	messageID := params.MessageID
	if messageID == 0 {
		messageID = conv.LastProcessedMessageID
	}

	// 5. Clamp: if messageID > LastProcessedMessageID, set to LastProcessedMessageID.
	if messageID > conv.LastProcessedMessageID {
		messageID = conv.LastProcessedMessageID
	}

	// 6. Update read cursor (MAX semantics enforced by store, D-012).
	if err := h.store.ConversationStore().UpdateLastRead(ctx, params.ConversationID, userID, messageID); err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("update last read: %w", err))
	}

	// 7. Calculate unread count.
	unreadCount, err := h.store.MessageStore().CountUnread(ctx, params.ConversationID, messageID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("count unread: %w", err))
	}

	// 8. Return success.
	resp := markAsReadResponse{
		Status:            "ok",
		UnreadCount:       unreadCount,
		LastReadMessageID: messageID,
	}
	return marshalResponse(resp)
}
