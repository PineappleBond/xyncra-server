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

// deleteMessageParams is the JSON-decoded representation of the client-supplied
// parameters for the "delete_message" method.
type deleteMessageParams struct {
	MessageID string `json:"message_id"` // required, UUID primary key of the message
}

// deleteMessageResponse is the success response payload returned to the client.
type deleteMessageResponse struct {
	Status string `json:"status"` // "ok"
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// deleteMessageHandler implements MethodHandler for the "delete_message" method.
// It performs a soft delete on a single message, enforcing that only the original
// sender may delete it (D-014).
//
// The handler is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
type deleteMessageHandler struct {
	store store.StoreAPI
}

// NewDeleteMessageHandler creates a deleteMessageHandler backed by the given Store.
func NewDeleteMessageHandler(store store.StoreAPI) *deleteMessageHandler {
	return &deleteMessageHandler{store: store}
}

// HandleRequest implements MethodHandler. It processes a "delete_message" RPC
// call: parses parameters, validates the message_id, fetches the message and
// its conversation, verifies membership, checks the sender owns the message
// (D-014), then performs a soft delete.
func (h *deleteMessageHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params deleteMessageParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// 2. Validate required fields.
	if params.MessageID == "" {
		return nil, fmt.Errorf("missing required field: message_id")
	}

	msgID := params.MessageID

	// 3. Fetch message and verify existence.
	msg, err := h.store.MessageStore().Get(ctx, msgID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("message not found")
		}
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	// 4. Fetch conversation and verify existence.
	conv, err := h.store.ConversationStore().Get(ctx, msg.ConversationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("conversation not found")
		}
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}

	// 5. Verify membership (C-3).
	userID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, userID) {
		return nil, fmt.Errorf("user is not a member of the conversation")
	}

	// 6. Verify caller is the message sender (D-014).
	if msg.SenderID != userID {
		return nil, fmt.Errorf("only the sender can delete this message")
	}

	// 7. Soft-delete the message.
	if err := h.store.MessageStore().Delete(ctx, msgID); err != nil {
		return nil, fmt.Errorf("failed to delete message: %w", err)
	}

	// 8. Return response.
	return marshalResponse(deleteMessageResponse{Status: "ok"})
}
