package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// MessageHandler
// --------------------------------------------------------------------------

// MessageHandler processes incoming protocol.Package messages received from a
// Client. Implementations must be safe for concurrent use.
type MessageHandler interface {
	// HandleMessage is invoked for every Package received by the client's
	// read goroutine. The context is cancelled when the client disconnects.
	HandleMessage(ctx context.Context, client *Client, pkg *protocol.Package)
}

// MessageHandlerFunc is an adapter that allows ordinary functions to be used
// as MessageHandler implementations.
type MessageHandlerFunc func(ctx context.Context, client *Client, pkg *protocol.Package)

// HandleMessage calls f(ctx, client, pkg).
func (f MessageHandlerFunc) HandleMessage(ctx context.Context, client *Client, pkg *protocol.Package) {
	f(ctx, client, pkg)
}

// --------------------------------------------------------------------------
// MethodHandler
// --------------------------------------------------------------------------

// MethodHandler processes a single Request method (e.g. "send_message",
// "sync_updates"). Implementations must be safe for concurrent use.
type MethodHandler interface {
	// HandleRequest processes a parsed PackageDataRequest and returns a
	// response payload. Returning a non-nil error signals the caller to
	// send an error response to the client.
	HandleRequest(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error)
}

// MethodHandlerFunc is an adapter that allows ordinary functions to be used
// as MethodHandler implementations.
type MethodHandlerFunc func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error)

// HandleRequest calls f(ctx, client, req).
func (f MethodHandlerFunc) HandleRequest(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	return f(ctx, client, req)
}

// --------------------------------------------------------------------------
// DefaultMessageHandler
// --------------------------------------------------------------------------

// DefaultMessageHandler is the default MessageHandler used by WebSocketServer.
// It dispatches incoming Packages by type:
//   - PackageTypeRequest: parsed into PackageDataRequest, routed to a
//     registered MethodHandler by method name.
//   - PackageTypeResponse: logged and ignored (RemoteCalling replaced ReverseRPC).
//   - PackageTypeUpdates: logged (reserved for future use).
type DefaultMessageHandler struct {
	mu       sync.RWMutex
	methods  map[string]MethodHandler
	fallback MethodHandler
}

// NewDefaultMessageHandler creates a DefaultMessageHandler with no registered
// methods.
func NewDefaultMessageHandler() *DefaultMessageHandler {
	return &DefaultMessageHandler{
		methods: make(map[string]MethodHandler),
	}
}

// RegisterMethod associates a method name with a MethodHandler. It overwrites
// any previously registered handler for the same method.
func (h *DefaultMessageHandler) RegisterMethod(method string, handler MethodHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.methods[method] = handler
}

// RegisterMethodFunc is a convenience wrapper around RegisterMethod that
// accepts a plain function.
func (h *DefaultMessageHandler) RegisterMethodFunc(method string, fn MethodHandlerFunc) {
	h.RegisterMethod(method, fn)
}

// SetFallback sets a fallback handler invoked when a request method is not
// registered. If no fallback is set, unknown methods return an error response
// to the client.
func (h *DefaultMessageHandler) SetFallback(handler MethodHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fallback = handler
}

// HandleMessage implements MessageHandler. It decodes the Package data
// according to its type and dispatches to the appropriate handler.
func (h *DefaultMessageHandler) HandleMessage(ctx context.Context, client *Client, pkg *protocol.Package) {
	switch pkg.Type {
	case protocol.PackageTypeRequest:
		h.handleRequest(ctx, client, pkg)
	case protocol.PackageTypeResponse:
		// ReverseRPC removed — Response packages are safely ignored.
		// Clients may still send Response packages during transition (D-140).
		slog.Debug("websocket: received response from client (ignored, ReverseRPC removed)", "connID", client.ConnID())
	case protocol.PackageTypeUpdates:
		slog.Debug("websocket: received updates package from client (ignored)", "connID", client.ConnID())
	default:
		slog.Warn("websocket: unknown package type", "type", pkg.Type, "connID", client.ConnID())
	}
}

// handleRequest dispatches a PackageTypeRequest. It parses the
// PackageDataRequest, looks up the method handler, invokes it, and sends back
// a PackageTypeResponse with the result (or an error).
//
// A ResponseSidecar is injected into the context before calling the handler.
// Handlers can optionally attach piggyback updates via GetSidecar(ctx).Append().
// After the handler returns, any accumulated updates are attached to the response.
//
// Design note: When the handler returns an error, any accumulated sidecar
// updates are discarded. This is intentional — the only sidecar consumer
// (agent_resume) appends updates on the success path only. If future handlers
// need to attach updates on error paths, this behavior should be revisited.
func (h *DefaultMessageHandler) handleRequest(ctx context.Context, client *Client, pkg *protocol.Package) {
	var req protocol.PackageDataRequest
	if err := jsonUnmarshal(pkg.Data, &req); err != nil {
		slog.Error("websocket: decode request", "connID", client.ConnID(), "error", err)
		_ = sendErrorResponse(client, "", protocol.ResponseCodeError, "invalid request data")
		return
	}

	slog.Info("websocket: received request", "method", req.Method, "connID", client.ConnID(), "userID", client.UserID())

	// Start handler.invoke span after we know the method name.
	invokeCtx, invokeFinish := startHandlerInvokeSpan(ctx, req.Method)
	defer invokeFinish(nil)

	// Inject ResponseSidecar for piggyback updates (D-118).
	invokeCtx = WithSidecar(invokeCtx)

	h.mu.RLock()
	methodHandler, ok := h.methods[req.Method]
	if !ok {
		methodHandler = h.fallback
	}
	h.mu.RUnlock()

	if methodHandler == nil {
		slog.Warn("websocket: unknown method", "method", req.Method, "connID", client.ConnID())
		_ = sendErrorResponse(client, req.ID, protocol.ResponseCodeError,
			fmt.Sprintf("unknown method: %s", req.Method))
		return
	}

	result, err := methodHandler.HandleRequest(invokeCtx, client, &req)
	if err != nil {
		slog.Error("websocket: handler error", "connID", client.ConnID(), "method", req.Method, "error", err)
		var handlerErr *protocol.HandlerError
		if errors.As(err, &handlerErr) {
			_ = sendErrorResponse(client, req.ID, handlerErr.Code, handlerErr.Message)
		} else {
			// Unmigrated handler or unexpected error: use generic error code.
			_ = sendErrorResponse(client, req.ID, protocol.ResponseCodeError, err.Error())
		}
		return
	}

	// Read piggyback updates from sidecar (D-118).
	var updates []protocol.PackageDataUpdate
	if sc := GetSidecar(invokeCtx); sc != nil {
		updates = sc.Updates()
	}

	_ = sendSuccessResponseWithUpdates(client, req.ID, result, updates)
}

// --------------------------------------------------------------------------
// Response helpers
// --------------------------------------------------------------------------

// sendSuccessResponse sends a PackageTypeResponse with a success code to the
// given client.
func sendSuccessResponse(client *Client, id string, data json.RawMessage) error {
	return sendSuccessResponseWithUpdates(client, id, data, nil)
}

// sendSuccessResponseWithUpdates sends a PackageTypeResponse with a success
// code and optional piggyback updates to the given client (D-118).
func sendSuccessResponseWithUpdates(client *Client, id string, data json.RawMessage, updates []protocol.PackageDataUpdate) error {
	resp := &protocol.PackageDataResponse{
		ID:      id,
		Code:    protocol.ResponseCodeOK,
		Msg:     "ok",
		Data:    data,
		Updates: updates,
	}
	return sendResponse(client, resp)
}

// sendErrorResponse sends a PackageTypeResponse with an error code to the
// given client.
func sendErrorResponse(client *Client, id string, code protocol.ResponseCode, msg string) error {
	resp := &protocol.PackageDataResponse{
		ID:   id,
		Code: code,
		Msg:  msg,
	}
	return sendResponse(client, resp)
}

// sendResponse marshals a PackageDataResponse into a Package and sends it to
// the client.
func sendResponse(client *Client, resp *protocol.PackageDataResponse) error {
	data, err := jsonMarshal(resp)
	if err != nil {
		return fmt.Errorf("websocket: marshal response: %w", err)
	}
	pkg := &protocol.Package{
		Type: protocol.PackageTypeResponse,
		Data: data,
	}
	return client.SendPackage(pkg)
}

// --------------------------------------------------------------------------
// JSON helpers
// --------------------------------------------------------------------------

// jsonMarshal is a thin wrapper around json.Marshal used throughout the server
// package. It exists as a package-level function so that callers can swap in a
// different encoder (e.g. one that uses a buffer pool) in the future.
//
// Future optimization: for high-throughput scenarios, consider using a
// sync.Pool of bytes.Buffers or a faster JSON encoder (e.g. sonic, jsoniter)
// to reduce allocations. The current stdlib encoding/json implementation is
// sufficient for moderate workloads (P2-05).
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// jsonUnmarshal is a thin wrapper around json.Unmarshal used throughout the
// server package. See jsonMarshal for future optimization directions.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
