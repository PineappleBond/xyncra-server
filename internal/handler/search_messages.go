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

// searchMessagesParams is the JSON-decoded representation of the client-supplied
// parameters for the "search_messages" method.
// ConversationID and Query are both required; AfterMessageID is an optional
// cursor for pagination (results are MessageID DESC, so after_message_id means
// "messages older than this ID"); Limit defaults to 50 and is capped at 200.
type searchMessagesParams struct {
	ConversationID string `json:"conversation_id"`
	Query          string `json:"query"`
	AfterMessageID uint32 `json:"after_message_id"`
	Limit          int    `json:"limit"`
}

// searchMessagesResponse is the success response payload returned to the client
// after a search_messages call.
type searchMessagesResponse struct {
	Messages []*model.Message `json:"messages"`
	HasMore  bool             `json:"has_more"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// searchMessagesHandler implements MethodHandler for the "search_messages"
// method. It performs a LIKE-based content search within a conversation,
// returning results ordered by MessageID descending (newest first). The
// handler is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
type searchMessagesHandler struct {
	store store.StoreAPI
}

// NewSearchMessagesHandler creates a searchMessagesHandler backed by the given
// Store.
func NewSearchMessagesHandler(store store.StoreAPI) *searchMessagesHandler {
	return &searchMessagesHandler{store: store}
}

// HandleRequest implements MethodHandler. It processes a "search_messages" RPC
// call: parses parameters, validates required fields, verifies conversation
// existence and membership, normalises the limit, fetches matching messages via
// MessageStore.SearchByConversation, determines whether more results remain
// using the limit+1 probe technique, and returns the paginated response.
func (h *searchMessagesHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params searchMessagesParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Validate required fields.
	if params.ConversationID == "" {
		return nil, protocol.NewValidationError("missing required field: conversation_id")
	}
	if params.Query == "" {
		return nil, protocol.NewValidationError("missing required field: query")
	}

	// 3. Fetch conversation and verify existence.
	conv, err := h.store.ConversationStore().Get(ctx, params.ConversationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("get conversation: %w", err))
	}

	// 4. Verify membership (C-3).
	userID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, userID) {
		return nil, protocol.NewPermissionDeniedError("user is not a member of the conversation")
	}

	// 5. Normalise limit: default 50, cap 200.
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// 6. Fetch messages using the limit+1 probe to detect has_more.
	msgs, err := h.store.MessageStore().SearchByConversation(ctx, conv.ID, params.Query, params.AfterMessageID, limit+1)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("search messages: %w", err))
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
	return marshalResponse(searchMessagesResponse{
		Messages: msgs,
		HasMore:  hasMore,
	})
}
