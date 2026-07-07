package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// syncUpdatesParams are the parameters for a "sync_updates" request.
// AfterSeq=0 means fetch from the beginning; Limit is clamped to [1,500]
// with a default of 100 when zero or negative.
type syncUpdatesParams struct {
	AfterSeq uint32 `json:"after_seq"`
	Limit    int    `json:"limit"`
}

// syncUpdatesResponse is the success response payload for "sync_updates".
type syncUpdatesResponse struct {
	Updates   []protocol.PackageDataUpdate `json:"updates"`
	HasMore   bool                         `json:"has_more"`
	LatestSeq uint32                       `json:"latest_seq"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// syncUpdatesHandler implements MethodHandler for the "sync_updates" method.
// It is stateless (only holds an immutable dependency reference) and therefore
// safe for concurrent use.
//
// The handler follows the D-009 pagination model: the client sends after_seq
// (the last seq it has seen) and a limit; the server returns up to limit
// updates with seq > after_seq, a has_more flag, and the latest_seq for the
// user so the client can detect gaps.
type syncUpdatesHandler struct {
	store store.StoreAPI
}

// NewSyncUpdatesHandler creates a syncUpdatesHandler backed by the given Store.
func NewSyncUpdatesHandler(store store.StoreAPI) *syncUpdatesHandler {
	return &syncUpdatesHandler{store: store}
}

// HandleRequest implements MethodHandler. It processes a "sync_updates" RPC
// call: parses parameters, normalises the limit, fetches incremental updates
// via the limit+1 probe technique, converts model records to protocol
// updates, and returns the paginated response.
func (h *syncUpdatesHandler) HandleRequest(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse params.
	var params syncUpdatesParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// 2. Normalise limit: default 100, cap 500.
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	userID := client.UserID()

	// 3. Fetch updates (limit+1 probe to detect has_more).
	updates, err := h.store.UserUpdateStore().ListByUser(ctx, userID, params.AfterSeq, limit+1)
	if err != nil {
		return nil, fmt.Errorf("failed to list updates: %w", err)
	}

	// 4. Determine has_more and truncate to the requested limit.
	hasMore := len(updates) > limit
	if hasMore {
		updates = updates[:limit]
	}

	// 5. Convert []*model.UserUpdate -> []protocol.PackageDataUpdate.
	pkgUpdates := make([]protocol.PackageDataUpdate, len(updates))
	for i, u := range updates {
		pkgUpdates[i] = protocol.PackageDataUpdate{
			Seq:       u.Seq,
			Payload:   json.RawMessage(u.Payload),
			CreatedAt: u.CreatedAt,
		}
	}

	// 6. Fetch the latest seq for the user.
	latestSeq, err := h.store.UserUpdateStore().GetLatestSeq(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest seq: %w", err)
	}

	// 7. Return response.
	return marshalResponse(syncUpdatesResponse{
		Updates:   pkgUpdates,
		HasMore:   hasMore,
		LatestSeq: latestSeq,
	})
}
