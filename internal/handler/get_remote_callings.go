// get_remote_callings RPC handler — fetch pending RemoteCallings for a conversation (D-137).
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

// getRemoteCallingsParams is the JSON-decoded representation of the client-supplied
// parameters for the "get_remote_callings" RPC method.
type getRemoteCallingsParams struct {
	ConversationID string `json:"conversation_id"` // required
}

// getRemoteCallingsResponse is the success response payload returned to the client.
type getRemoteCallingsResponse struct {
	RemoteCallings []*model.RemoteCalling `json:"remote_callings"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// getRemoteCallingsHandler implements MethodHandler for the "get_remote_callings" method.
type getRemoteCallingsHandler struct {
	store store.StoreAPI
}

// NewGetRemoteCallingsHandler creates a getRemoteCallingsHandler backed by the given Store.
func NewGetRemoteCallingsHandler(store store.StoreAPI) *getRemoteCallingsHandler {
	return &getRemoteCallingsHandler{store: store}
}

// HandleRequest implements MethodHandler. It fetches all pending RemoteCallings
// for the given conversation.
func (h *getRemoteCallingsHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params getRemoteCallingsParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Validate required fields.
	if params.ConversationID == "" {
		return nil, protocol.NewValidationError("missing required field: conversation_id")
	}

	// 3. Query pending RemoteCallings.
	rcs := h.store.RemoteCallingStore()
	if rcs == nil {
		return nil, protocol.NewInternalError(fmt.Errorf("get_remote_callings: RemoteCallingStore not available"))
	}

	remoteCallings, err := rcs.GetPendingByConversation(ctx, params.ConversationID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("get_remote_callings: query: %w", err))
	}
	if remoteCallings == nil {
		remoteCallings = []*model.RemoteCalling{} // return empty array, not null
	}

	// 4. Return response.
	return marshalResponse(getRemoteCallingsResponse{
		RemoteCallings: remoteCallings,
	})
}
