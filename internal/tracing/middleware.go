package tracing

import (
	"context"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// debugContextKey is an unexported type used as a context key so that
// downstream code cannot forge or collide with it.
type debugContextKey struct{}

// debugInfo carries the user/device identifiers used by DebugSampler to
// decide whether to force-sample a trace.
type debugInfo struct {
	userID   string
	deviceID string
}

// WithDebug marks the context as debug-sampled for the given user/device.
// The DebugSampler checks for this marker and forces RecordAndSample when
// the user or device appears in the configured debug lists.
func WithDebug(ctx context.Context, userID, deviceID string) context.Context {
	return context.WithValue(ctx, debugContextKey{}, debugInfo{userID: userID, deviceID: deviceID})
}

// IsDebugContext returns true if the context is marked for debug sampling.
func IsDebugContext(ctx context.Context) bool {
	return ctx.Value(debugContextKey{}) != nil
}

// DebugSampler wraps a fallback sampler and forces sampling for traces
// associated with configured debug users or devices. It identifies debug
// traces via a context value set by WithDebug at the handler layer.
type DebugSampler struct {
	fallback     sdktrace.Sampler
	debugUsers   map[string]bool
	debugDevices map[string]bool
}

// NewDebugSampler creates a DebugSampler that falls back to TraceIDRatioBased
// sampling for non-debug traces. Debug traces (identified via a context value
// set by WithDebug) are always sampled when the user or device is in the
// configured debug list.
func NewDebugSampler(fallbackRatio float64, debugUsers, debugDevices []string) *DebugSampler {
	du := make(map[string]bool, len(debugUsers))
	for _, u := range debugUsers {
		du[u] = true
	}
	dd := make(map[string]bool, len(debugDevices))
	for _, d := range debugDevices {
		dd[d] = true
	}
	return &DebugSampler{
		fallback:     sdktrace.TraceIDRatioBased(fallbackRatio),
		debugUsers:   du,
		debugDevices: dd,
	}
}

// ShouldSample implements sdktrace.Sampler.
// It checks whether the parent context carries a debug marker matching a
// configured debug user/device. If so, it forces RecordAndSample.
// Otherwise, it delegates to the fallback sampler.
func (s *DebugSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	parentSC := trace.SpanContextFromContext(p.ParentContext)

	// Check for debug marker in parent context value.
	if info, ok := p.ParentContext.Value(debugContextKey{}).(debugInfo); ok {
		if s.debugUsers[info.userID] || s.debugDevices[info.deviceID] {
			return sdktrace.SamplingResult{
				Decision:   sdktrace.RecordAndSample,
				Tracestate: parentSC.TraceState(),
			}
		}
	}

	// If the parent span context is valid and already sampled, respect that decision.
	if parentSC.IsValid() && parentSC.TraceFlags().IsSampled() {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Tracestate: parentSC.TraceState(),
		}
	}

	return s.fallback.ShouldSample(p)
}

// Description returns a human-readable description of the sampler.
func (s *DebugSampler) Description() string {
	return "DebugSampler{fallback=" + s.fallback.Description() + "}"
}
