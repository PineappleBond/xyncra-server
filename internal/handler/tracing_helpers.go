package handler

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

var handlerTracer = otel.Tracer("xyncra-server")

// startBrokerEnqueueSpan creates a new span for MQ enqueue operations.
// Returns the updated context and a finish function that must be called
// when the operation completes (pass nil for success, or the error on failure).
func startBrokerEnqueueSpan(ctx context.Context, taskType string) (context.Context, func(error)) {
	ctx, span := handlerTracer.Start(ctx, tracing.SpanBrokerEnqueue,
		trace.WithAttributes(
			attribute.String(tracing.AttrTaskType, taskType),
		),
	)
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()
	}
}
