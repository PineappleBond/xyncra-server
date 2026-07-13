package handler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// replayTimeout is the timeout for each individual replay request (D-109).
const replayTimeout = 10 * time.Second

// reconnectHandler handles "system.reconnect" requests (Phase 5, D-108).
// It queries the PendingStore for pending requests, filters by last_seen_seq,
// and asynchronously replays them to the client.
type reconnectHandler struct {
	reverseRPC *server.ReverseRPC
	logger     server.Logger
}

// NewReconnectHandler creates a handler for system.reconnect.
func NewReconnectHandler(reverseRPC *server.ReverseRPC, logger server.Logger) *reconnectHandler {
	return &reconnectHandler{
		reverseRPC: reverseRPC,
		logger:     logger,
	}
}

// reconnectParams is the expected params structure for system.reconnect.
type reconnectParams struct {
	LastSeenSeq uint64 `json:"last_seen_seq"`
}

// reconnectResponse is the response structure for system.reconnect.
type reconnectResponse struct {
	Status   string `json:"status"`
	Replayed int    `json:"replayed"`
	Total    int    `json:"total"`
}

// HandleRequest implements MethodHandler for system.reconnect (D-108).
//
// It parses last_seen_seq from params (default 0), queries PendingStore for
// pending requests, filters by Seq > last_seen_seq AND RetryCount < MaxRetries,
// and asynchronously replays each matching request. Returns immediately with
// the count of requests being replayed.
func (h *reconnectHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	userID := client.UserID()
	deviceID := client.DeviceID()

	// Parse params. last_seen_seq defaults to 0 if missing.
	var params reconnectParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, protocol.NewValidationError("invalid params: " + err.Error())
		}
	}

	h.logger.Info("system.reconnect received",
		"userID", userID, "deviceID", deviceID, "last_seen_seq", params.LastSeenSeq)

	// Check if PendingStore is available (nil-safe, D-063).
	ps := h.reverseRPC.PendingStore()
	if ps == nil {
		return marshalResponse(reconnectResponse{
			Status:   "ok",
			Replayed: 0,
			Total:    0,
		})
	}

	// List pending requests from the store. Fail-open on error (D-072).
	pending, err := ps.List(ctx, userID, deviceID)
	if err != nil {
		h.logger.Error("system.reconnect: failed to list pending requests",
			"userID", userID, "deviceID", deviceID, "error", err)
		return marshalResponse(reconnectResponse{
			Status:   "ok",
			Replayed: 0,
			Total:    0,
		})
	}

	// Filter: Seq > last_seen_seq AND RetryCount < MaxRetries.
	var toReplay []*server.PendingRequest
	for _, preq := range pending {
		if preq.Seq > params.LastSeenSeq && preq.RetryCount < preq.MaxRetries {
			toReplay = append(toReplay, preq)
		}
	}

	h.logger.Info("system.reconnect summary",
		"total", len(pending), "replayed", len(toReplay),
		"filtered_by_seq", len(pending)-len(toReplay))

	// Asynchronously replay each request in a separate goroutine (D-109).
	for _, preq := range toReplay {
		go h.replayOne(preq)
	}

	return marshalResponse(reconnectResponse{
		Status:   "ok",
		Replayed: len(toReplay),
		Total:    len(pending),
	})
}

// replayOne handles the lifecycle of a single replay request.
func (h *reconnectHandler) replayOne(preq *server.PendingRequest) {
	ctx := context.Background()

	h.logger.Info("replay request sent",
		"reqID", preq.ID, "method", preq.Method, "seq", preq.Seq)

	resp, err := h.reverseRPC.ReplayRequest(ctx, preq, replayTimeout)
	ps := h.reverseRPC.PendingStore()

	if err != nil || resp == nil || resp.Code != 0 {
		// Replay failed (timeout, send error, or non-success response).
		preq.RetryCount++

		if preq.RetryCount >= preq.MaxRetries {
			// Exceeded max retries — discard.
			h.logger.Info("replay discarded",
				"reqID", preq.ID, "retryCount", preq.RetryCount, "maxRetries", preq.MaxRetries)
			if ps != nil {
				if rmErr := ps.Remove(ctx, preq.UserID, preq.DeviceID, preq.ID); rmErr != nil {
					h.logger.Error("replay: failed to remove discarded request",
						"reqID", preq.ID, "error", rmErr)
				}
			}
			return
		}

		h.logger.Info("replay timeout",
			"reqID", preq.ID, "retryCount", preq.RetryCount)
		if ps != nil {
			if upErr := ps.Update(ctx, preq); upErr != nil {
				h.logger.Error("replay: failed to update retry count",
					"reqID", preq.ID, "error", upErr)
			}
		}
		return
	}

	// Replay succeeded — remove from pending store.
	h.logger.Info("replay success", "reqID", preq.ID)
	if ps != nil {
		if rmErr := ps.Remove(ctx, preq.UserID, preq.DeviceID, preq.ID); rmErr != nil {
			h.logger.Error("replay: failed to remove successful request",
				"reqID", preq.ID, "error", rmErr)
		}
	}
}
