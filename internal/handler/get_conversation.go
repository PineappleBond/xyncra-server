package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// getConversationParams is the JSON-decoded representation of the client-supplied
// parameters for the "get_conversation" method.
type getConversationParams struct {
	ConversationID string `json:"conversation_id"` // required
}

// getConversationResponse is the success response payload returned to the client.
type getConversationResponse struct {
	Conversation *model.Conversation `json:"conversation"`
	UnreadCount  int64               `json:"unread_count"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// getConversationHandler implements MethodHandler for the "get_conversation" method.
// It fetches a single conversation by ID, verifies membership, and returns the
// conversation object along with the caller's unread message count.
//
// The handler is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
type getConversationHandler struct {
	store store.StoreAPI
}

// NewGetConversationHandler creates a getConversationHandler backed by the given Store.
func NewGetConversationHandler(store store.StoreAPI) *getConversationHandler {
	return &getConversationHandler{store: store}
}

// HandleRequest implements MethodHandler. It processes a "get_conversation" RPC
// call: parses parameters, validates the conversation_id, fetches the
// conversation, verifies membership, computes the caller's unread count, and
// returns the response.
func (h *getConversationHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params getConversationParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// 2. Validate required fields.
	if params.ConversationID == "" {
		return nil, fmt.Errorf("missing required field: conversation_id")
	}

	// 3. Fetch conversation and verify existence.
	conv, err := h.store.ConversationStore().Get(ctx, params.ConversationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("conversation not found")
		}
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}

	// 4. Verify membership (C-3).
	userID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, userID) {
		return nil, fmt.Errorf("user is not a member of the conversation")
	}

	// 5. Determine caller's last read message ID (D-012).
	var lastReadMessageID uint32
	if conv.UserID1 == userID {
		lastReadMessageID = conv.LastReadMessageID1
	} else if conv.UserID2 == userID {
		lastReadMessageID = conv.LastReadMessageID2
	}

	// 6. Calculate unread count.
	unreadCount, err := h.store.MessageStore().CountUnread(ctx, conv.ID, lastReadMessageID)
	if err != nil {
		// Handle error gracefully — default to 0 unread rather than failing.
		unreadCount = 0
	}

	// 7. Return response.
	return marshalResponse(getConversationResponse{
		Conversation: conv,
		UnreadCount:  unreadCount,
	})
}
