package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

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

// heartbeatHandler implements MethodHandler for the "heartbeat" method.
// It is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
//
// The heartbeat follows the passive renewal strategy (D-010): each
// heartbeat call invokes ConnectionStore.Refresh to reset the connection
// TTL, keeping the connection alive without requiring a write to the
// store's metadata fields.
type heartbeatHandler struct {
	connStore server.ConnectionStore
}

// NewHeartbeatHandler creates a heartbeatHandler backed by the given
// ConnectionStore.
func NewHeartbeatHandler(connStore server.ConnectionStore) *heartbeatHandler {
	return &heartbeatHandler{connStore: connStore}
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
	// 1. Parse params (best-effort; heartbeat must not fail on bad params).
	var params heartbeatParams
	_ = json.Unmarshal(req.Params, &params)

	// 2. Log device info if provided (observability only, not persisted).
	if len(params.DeviceInfo) > 0 {
		log.Printf("heartbeat: device_info from [connID=%s, userID=%s]: %v",
			client.ConnID(), client.UserID(), params.DeviceInfo)
	}

	// 3. Refresh the connection TTL (D-010 passive renewal).
	if err := h.connStore.Refresh(ctx, client.ConnID()); err != nil {
		if errors.Is(err, server.ErrConnectionNotFound) {
			return nil, protocol.NewNotFoundError("connection expired")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("refresh connection: %w", err))
	}

	// 4. Return success.
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
