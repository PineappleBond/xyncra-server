package store

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// storeTracer is the OpenTelemetry tracer for the store layer.
// When tracing is disabled, a no-op tracer is installed by tracing.InitTracer,
// resulting in zero allocation and zero overhead.
var storeTracer = otel.Tracer("xyncra-server")

// startSpan creates a named span with optional attributes.
// Returns (ctx, finish) where finish sets error status and ends the span.
func startSpan(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, func(error)) {
	ctx, span := storeTracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()
	}
}
