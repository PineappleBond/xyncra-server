package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// systemReconnectParams holds the parameters for a "system.reconnect" request.
type systemReconnectParams struct {
	// LastSeenSeq is the highest PackageDataRequest.Seq the client has
	// received from the server. The server MAY use this to replay missed
	// requests from its PendingStore (D-072).
	LastSeenSeq uint64 `json:"last_seen_seq"`
}

// systemReconnectResponse is returned on a successful reconnect handshake.
type systemReconnectResponse struct {
	// Status is always "ok" when the reconnect handshake succeeds.
	Status string `json:"status"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// systemReconnectHandler implements MethodHandler for the "system.reconnect"
// method. It is stateless and safe for concurrent use.
//
// The reconnect handshake is called by the client after establishing (or
// re-establishing) a WebSocket connection. It serves two purposes:
//  1. Notifies the server that the client has reconnected, allowing the
//     server to replay any pending requests that were queued while the
//     client was offline.
//  2. Provides the client's last seen sequence number so the server can
//     determine which requests to replay.
//
// Currently this handler logs the reconnect event and returns success.
// Future enhancements may include PendingStore replay logic (D-072).
type systemReconnectHandler struct {
	logger server.Logger
}

// NewSystemReconnectHandler creates a systemReconnectHandler.
func NewSystemReconnectHandler(logger server.Logger) *systemReconnectHandler {
	if logger == nil {
		logger = defaultLogger{}
	}
	return &systemReconnectHandler{logger: logger}
}

// HandleRequest implements MethodHandler. It processes a "system.reconnect"
// RPC call: parses the last_seen_seq parameter, logs the reconnect event,
// and returns a success response.
//
// Errors:
//   - Malformed params are logged but do not fail the handshake (the
//     reconnect must succeed to allow the client to proceed).
func (h *systemReconnectHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse params (best-effort; reconnect must not fail on bad params).
	var params systemReconnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		h.logger.Debug("system.reconnect: malformed params (non-fatal)",
			"connID", client.ConnID(), "userID", client.UserID(), "error", err)
	}

	// 2. Log the reconnect event for observability.
	h.logger.Info("system.reconnect: client reconnected",
		"connID", client.ConnID(),
		"userID", client.UserID(),
		"deviceID", client.DeviceID(),
		"last_seen_seq", params.LastSeenSeq)

	// 3. Return success.
	data, err := json.Marshal(systemReconnectResponse{Status: "ok"})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return data, nil
}
