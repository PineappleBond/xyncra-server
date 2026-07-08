package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// listConversationsParams is the JSON-decoded representation of the
// client-supplied parameters for the "list_conversations" method.
// Offset is the pagination start index (>= 0, default 0).
// Limit is the page size (default 20, capped at 100).
type listConversationsParams struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// listConversationsResponse is the success response payload returned to the
// client after a list_conversations call.
type listConversationsResponse struct {
	Conversations []*model.Conversation `json:"conversations"`
	HasMore       bool                  `json:"has_more"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// listConversationsHandler implements MethodHandler for the
// "list_conversations" method. It returns the caller's conversations ordered
// by LastMessageAt descending, with offset/limit pagination and a has_more
// flag.
//
// The handler is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
type listConversationsHandler struct {
	store store.StoreAPI
}

// NewListConversationsHandler creates a listConversationsHandler backed by
// the given Store.
func NewListConversationsHandler(store store.StoreAPI) *listConversationsHandler {
	return &listConversationsHandler{store: store}
}

// HandleRequest implements MethodHandler. It processes a "list_conversations"
// RPC call: parses parameters, normalises offset and limit, fetches the
// caller's conversations via ConversationStore.GetByUser, determines whether
// more results remain using the limit+1 probe technique, and returns the
// paginated response.
func (h *listConversationsHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params listConversationsParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Normalise limit: default 20, cap 100.
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	// 3. Normalise offset: must be >= 0.
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	userID := client.UserID()

	// 4. Fetch conversations using the limit+1 probe to detect has_more.
	// The store allows up to 101 (one above the user-facing cap of 100) to
	// accommodate the probe without triggering its internal default fallback.
	storeLimit := limit + 1
	convs, err := h.store.ConversationStore().GetByUser(ctx, userID, offset, storeLimit)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("list conversations: %w", err))
	}

	// 5. Determine has_more and truncate to the requested limit.
	hasMore := len(convs) > limit
	if hasMore {
		convs = convs[:limit]
	}

	// 6. Return response (never null for conversations).
	if convs == nil {
		convs = []*model.Conversation{}
	}
	return marshalResponse(listConversationsResponse{
		Conversations: convs,
		HasMore:       hasMore,
	})
}
