package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
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
// call: parses parameters, normalises the limit, fetches the latest seq,
// computes the expected seq range, queries actual updates within that range,
// and fills any missing seq positions with synthetic "gap" updates so the
// client receives a contiguous sequence (see D-029).
func (h *syncUpdatesHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse params.
	var params syncUpdatesParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
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

	// 3. Fetch the latest seq for the user first (moved earlier for gap-filling).
	latestSeq, err := h.store.UserUpdateStore().GetLatestSeq(ctx, userID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("get latest seq: %w", err))
	}

	// 4. Early exit: no new updates.
	if latestSeq <= params.AfterSeq {
		return marshalResponse(syncUpdatesResponse{
			Updates:   []protocol.PackageDataUpdate{},
			HasMore:   false,
			LatestSeq: latestSeq,
		})
	}

	// 5. Compute expected end of the range.
	expectedEnd := params.AfterSeq + uint32(limit)
	if expectedEnd > latestSeq {
		expectedEnd = latestSeq
	}

	// 6. Query actual updates within the expected range (afterSeq, expectedEnd].
	actualUpdates, err := h.store.UserUpdateStore().ListByUserRange(ctx, userID, params.AfterSeq, expectedEnd)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("list updates: %w", err))
	}

	// 7. Build seq -> update lookup map.
	actualMap := make(map[uint32]*model.UserUpdate, len(actualUpdates))
	for _, u := range actualUpdates {
		actualMap[u.Seq] = u
	}

	// 8. Build result, filling gaps for any missing seq positions.
	result := make([]protocol.PackageDataUpdate, 0, int(expectedEnd-params.AfterSeq))
	for seq := params.AfterSeq + 1; seq <= expectedEnd; seq++ {
		if u, ok := actualMap[seq]; ok {
			result = append(result, protocol.PackageDataUpdate{
				Seq:       u.Seq,
				Type:      u.Type,
				Payload:   json.RawMessage(u.Payload),
				CreatedAt: u.CreatedAt,
			})
		} else {
			result = append(result, protocol.PackageDataUpdate{
				Seq:       seq,
				Type:      protocol.UpdateTypeGap,
				Payload:   nil,
				CreatedAt: time.Now(),
			})
		}
	}

	// 9. has_more: true when the requested window extends beyond what we returned.
	hasMore := params.AfterSeq+uint32(limit) < latestSeq

	// 10. Return response.
	return marshalResponse(syncUpdatesResponse{
		Updates:   result,
		HasMore:   hasMore,
		LatestSeq: latestSeq,
	})
}
