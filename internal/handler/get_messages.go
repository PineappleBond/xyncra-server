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

// getMessagesParams is the JSON-decoded representation of the client-supplied
// parameters for the "get_messages" method.
// ConversationID is required; AfterMessageID defaults to 0 (fetch from the
// beginning); Limit defaults to 50 and is capped at 200.
type getMessagesParams struct {
	ConversationID string `json:"conversation_id"`
	AfterMessageID uint32 `json:"after_message_id"`
	Limit          int    `json:"limit"`
}

// getMessagesResponse is the success response payload returned to the client
// after a get_messages call.
type getMessagesResponse struct {
	Messages []*model.Message `json:"messages"`
	HasMore  bool             `json:"has_more"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// getMessagesHandler implements MethodHandler for the "get_messages" method.
// It returns messages for a conversation ordered by MessageID ascending, with
// cursor-based pagination using after_message_id and a has_more flag.
//
// The handler is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
type getMessagesHandler struct {
	store store.StoreAPI
}

// NewGetMessagesHandler creates a getMessagesHandler backed by the given Store.
func NewGetMessagesHandler(store store.StoreAPI) *getMessagesHandler {
	return &getMessagesHandler{store: store}
}

// HandleRequest implements MethodHandler. It processes a "get_messages" RPC
// call: parses parameters, validates the conversation_id, verifies membership,
// normalises the limit, fetches messages via MessageStore.ListByConversation,
// determines whether more results remain using the limit+1 probe technique,
// and returns the paginated response.
func (h *getMessagesHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params getMessagesParams
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

	// 5. Normalise limit: default 50, cap 200.
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// 6. Fetch messages using the limit+1 probe to detect has_more.
	msgs, err := h.store.MessageStore().ListByConversation(ctx, conv.ID, params.AfterMessageID, limit+1)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	// 7. Determine has_more and truncate to the requested limit.
	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}

	// 8. Return response (never null for messages).
	if msgs == nil {
		msgs = []*model.Message{}
	}
	return marshalResponse(getMessagesResponse{
		Messages: msgs,
		HasMore:  hasMore,
	})
}
