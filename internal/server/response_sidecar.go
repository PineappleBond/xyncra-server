// ResponseSidecar allows handlers to attach piggyback updates to a response
// without modifying the MethodHandler interface (D-118).
//
// Design intent: The sidecar is created per-request and injected into the
// context. Handlers can optionally append updates during processing. After
// the handler returns, the WebSocket handler reads the accumulated updates
// and attaches them to the response package.
//
// Lifecycle: Each request gets a fresh sidecar instance. No mutex is needed
// because a single request is processed by a single goroutine.
package server

import (
	"context"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// contextKey is an unexported type for context keys in this package.
type contextKey struct{}

// sidecarKey is the context key for ResponseSidecar.
var sidecarKey = contextKey{}

// ResponseSidecar accumulates piggyback updates during request handling.
// It is safe for single-goroutine use (one request = one goroutine).
type ResponseSidecar struct {
	updates []protocol.PackageDataUpdate
}

// Append adds one or more updates to the sidecar.
func (s *ResponseSidecar) Append(updates ...protocol.PackageDataUpdate) {
	s.updates = append(s.updates, updates...)
}

// Updates returns the accumulated updates. Returns nil if no updates were added.
func (s *ResponseSidecar) Updates() []protocol.PackageDataUpdate {
	if len(s.updates) == 0 {
		return nil
	}
	return s.updates
}

// WithSidecar creates a new ResponseSidecar and injects it into the context.
// Called by the WebSocket handler before dispatching to a MethodHandler.
func WithSidecar(ctx context.Context) context.Context {
	return context.WithValue(ctx, sidecarKey, &ResponseSidecar{})
}

// GetSidecar retrieves the ResponseSidecar from the context.
// Returns nil if no sidecar was injected (e.g., in tests or non-WebSocket contexts).
func GetSidecar(ctx context.Context) *ResponseSidecar {
	if sc, ok := ctx.Value(sidecarKey).(*ResponseSidecar); ok {
		return sc
	}
	return nil
}
