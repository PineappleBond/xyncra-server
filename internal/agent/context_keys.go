package agent

import "context"

// contextKey is an unexported type for context keys in this package.
type contextKey int

const (
	// ctxKeyCallerDevice is the context key for (userID, deviceID) of the
	// device that initiated the conversation.
	ctxKeyCallerDevice contextKey = iota

	// ctxKeyAgentID is the context key for the agent's userID. This is used
	// by DynamicToolProvider to look up functions registered by the agent's
	// device(s), which may differ from the human sender's identity stored in
	// CallerDevice.
	ctxKeyAgentID
)

// CallerDevice holds the (userID, deviceID) pair of the device that
// initiated the agent conversation.
type CallerDevice struct {
	UserID   string
	DeviceID string
}

// ContextWithCallerDevice returns a copy of ctx carrying the CallerDevice.
func ContextWithCallerDevice(ctx context.Context, d CallerDevice) context.Context {
	return context.WithValue(ctx, ctxKeyCallerDevice, d)
}

// CallerDeviceFromContext extracts the CallerDevice from ctx.
// Returns (zero, false) if not present.
func CallerDeviceFromContext(ctx context.Context) (CallerDevice, bool) {
	d, ok := ctx.Value(ctxKeyCallerDevice).(CallerDevice)
	return d, ok
}

// ContextWithAgentID returns a copy of ctx carrying the agent's userID.
func ContextWithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, ctxKeyAgentID, agentID)
}

// AgentIDFromContext extracts the agent userID from ctx.
// Returns ("", false) if not present.
func AgentIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKeyAgentID).(string)
	return id, ok
}
