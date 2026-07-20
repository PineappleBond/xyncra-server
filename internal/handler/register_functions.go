package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// registerFunctionsHandler handles system.register_functions requests (D-098, D-099).
// It is stateless and safe for concurrent use.
type registerFunctionsHandler struct {
	registry server.FunctionRegistry
}

// NewRegisterFunctionsHandler creates a handler for the system.register_functions
// RPC method. The registry is required and must not be nil.
func NewRegisterFunctionsHandler(registry server.FunctionRegistry) *registerFunctionsHandler {
	return &registerFunctionsHandler{registry: registry}
}

// HandleRequest implements MethodHandler. It parses the request params, overrides
// the deviceID with the one from the authenticated client connection (D-093), and
// calls FunctionRegistry.RegisterFunctions to replace the device's function list.
// An empty functions list is valid and clears any previously registered functions.
func (h *registerFunctionsHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 0. Log incoming request.
	log.Printf("system.register_functions received: userID=%s deviceID=%s", client.UserID(), client.DeviceID())

	// 1. Parse params.
	var params server.RegisterFunctionsParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		log.Printf("system.register_functions: invalid params: %v", err)
		return nil, protocol.NewValidationError("invalid params")
	}
	log.Printf("system.register_functions: parsed %d functions", len(params.Functions))

	// 2. Override deviceID with the authenticated client's deviceID (D-093).
	// The client-supplied deviceID in params is ignored; the connection's
	// deviceID is authoritative.
	deviceID := client.DeviceID()

	// 3. Register (full replacement).
	if err := h.registry.RegisterFunctions(ctx, client.UserID(), deviceID, &params); err != nil {
		log.Printf("system.register_functions: registry error: %v", err)
		return nil, registryErrorToHandlerError(err)
	}

	log.Printf("system.register_functions: registered %d functions for userID=%s deviceID=%s", len(params.Functions), client.UserID(), deviceID)

	// 4. Return success.
	return marshalResponse(map[string]any{
		"status":    "ok",
		"count":     len(params.Functions),
		"device_id": deviceID,
	})
}

// registryErrorToHandlerError maps FunctionRegistry sentinel errors to
// protocol HandlerErrors. Unknown errors are wrapped as internal errors.
func registryErrorToHandlerError(err error) *protocol.HandlerError {
	switch {
	case errors.Is(err, server.ErrFunctionNameEmpty):
		return protocol.NewValidationError(err.Error())
	case errors.Is(err, server.ErrFunctionNameTooLong):
		return protocol.NewValidationError(err.Error())
	case errors.Is(err, server.ErrFunctionNameDuplicate):
		return protocol.NewValidationError(err.Error())
	case errors.Is(err, server.ErrMaxFunctionsPerDevice):
		return protocol.NewValidationError(err.Error())
	default:
		return protocol.NewInternalError(err)
	}
}
