package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// heartbeatParams are the optional parameters for a "heartbeat" request.
// All fields are optional; a heartbeat with no params is valid.
type heartbeatParams struct {
	// DeviceInfo holds optional device metadata (e.g. OS version, app
	// version, battery level) sent by the client alongside the heartbeat.
	// It is logged for observability but not persisted.
	DeviceInfo map[string]string `json:"device_info,omitempty"`
}

// heartbeatResponse is returned on a successful heartbeat.
type heartbeatResponse struct {
	// Status is always "ok" when the heartbeat succeeds.
	Status string `json:"status"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// minHeartbeatInterval is the minimum allowed interval between heartbeat
// requests from the same connection. Requests arriving faster than this
// are silently dropped to prevent heartbeat flooding (BUG-001).
const minHeartbeatInterval = 5 * time.Second

// heartbeatHandler implements MethodHandler for the "heartbeat" method.
// It follows the passive renewal strategy (D-010): each heartbeat call
// invokes ConnectionStore.Refresh to reset the connection TTL.
//
// Rate limiting (BUG-001): the handler tracks the last heartbeat time
// per connection and silently drops requests that arrive faster than
// minHeartbeatInterval. This prevents a misconfigured or buggy client
// from flooding the server with heartbeat requests, which would block
// processing of other RPCs (e.g. get_conversation for RemoteCalling).
type heartbeatHandler struct {
	connStore server.ConnectionStore
	logger    server.Logger

	// mu protects lastHeartbeat map.
	mu sync.Mutex
	// lastHeartbeat tracks the last heartbeat time per connection ID.
	// Entries are cleaned up when the connection is closed (TTL-based
	// eviction is not needed because the map is bounded by active connections).
	lastHeartbeat map[string]time.Time
}

// NewHeartbeatHandler creates a heartbeatHandler backed by the given
// ConnectionStore.
func NewHeartbeatHandler(connStore server.ConnectionStore, logger server.Logger) *heartbeatHandler {
	if logger == nil {
		logger = defaultLogger{}
	}
	return &heartbeatHandler{
		connStore:     connStore,
		logger:        logger,
		lastHeartbeat: make(map[string]time.Time),
	}
}

// HandleRequest implements MethodHandler. It processes a "heartbeat" RPC
// call: optionally parses device info (logged only), refreshes the
// connection TTL via ConnectionStore.Refresh, and returns a success
// response.
//
// Errors:
//   - If the connection has expired or been evicted, a "connection
//     expired" error is returned so the caller can signal the client to
//     reconnect.
//   - Other Refresh errors are wrapped and returned to the caller.
//
// Params parsing is intentionally lenient: a malformed params payload
// does not break the heartbeat, because the only purpose of params is
// to carry optional device info for logging.
func (h *heartbeatHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	connID := client.ConnID()

	// 1. Rate limiting (BUG-001): drop heartbeat requests that arrive faster
	//    than minHeartbeatInterval. This prevents a misconfigured client from
	//    flooding the server and blocking other RPCs.
	now := time.Now()
	h.mu.Lock()
	last, exists := h.lastHeartbeat[connID]
	if exists && now.Sub(last) < minHeartbeatInterval {
		h.mu.Unlock()
		h.logger.Debug("heartbeat: rate-limited (too fast)",
			"connID", connID, "interval", now.Sub(last))
		// Return success silently — the connection is still alive, no need
		// to penalise the client with an error.
		return marshalResponse(heartbeatResponse{Status: "ok"})
	}
	h.lastHeartbeat[connID] = now
	h.mu.Unlock()

	// 2. Parse params (best-effort; heartbeat must not fail on bad params).
	var params heartbeatParams
	_ = json.Unmarshal(req.Params, &params)

	// 3. Log device info if provided (observability only, not persisted).
	if len(params.DeviceInfo) > 0 {
		h.logger.Debug("heartbeat: device_info received",
			"connID", connID, "userID", client.UserID(), "device_info", params.DeviceInfo)
	}

	// 4. Refresh the connection TTL (D-010 passive renewal).
	if err := h.connStore.Refresh(ctx, connID); err != nil {
		if errors.Is(err, server.ErrConnectionNotFound) {
			// Clean up the lastHeartbeat entry for expired connections.
			h.mu.Lock()
			delete(h.lastHeartbeat, connID)
			h.mu.Unlock()
			return nil, protocol.NewNotFoundError("connection expired")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("refresh connection: %w", err))
	}

	// 5. Return success.
	return marshalResponse(heartbeatResponse{Status: "ok"})
}

// marshalResponse marshals v into a json.RawMessage, returning an error
// suitable for the MethodHandler contract on failure.
func marshalResponse(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return data, nil
}
