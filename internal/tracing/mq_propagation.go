package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
)

// MapCarrier implements propagation.TextMapCarrier using a plain map[string]string.
// It is used to inject and extract W3C Trace Context headers for message queue propagation.
type MapCarrier map[string]string

// Get returns the value associated with the passed key.
func (c MapCarrier) Get(key string) string {
	return c[key]
}

// Set stores the key-value pair in the carrier.
func (c MapCarrier) Set(key, value string) {
	c[key] = value
}

// Keys returns all keys held in the carrier.
func (c MapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// InjectTraceContext serializes the current trace context from ctx into a map of headers.
// The returned map can be attached to message queue payloads for cross-service propagation.
// Uses the global TextMapPropagator (W3C Trace Context by default).
func InjectTraceContext(ctx context.Context) map[string]string {
	carrier := make(MapCarrier)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return map[string]string(carrier)
}

// ExtractTraceContext restores trace context from a map of headers (e.g., from a message
// queue payload) into the returned context. The returned context carries the extracted
// span context as the remote parent.
func ExtractTraceContext(metadata map[string]string) context.Context {
	carrier := MapCarrier(metadata)
	return otel.GetTextMapPropagator().Extract(context.Background(), carrier)
}
