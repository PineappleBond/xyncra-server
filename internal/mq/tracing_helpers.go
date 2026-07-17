package mq

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

var mqTracer = otel.Tracer("xyncra-server")

// startMQProcessSpan creates a new span for MQ task processing.
// Returns the updated context and a finish function that must be called
// when processing completes (pass nil for success, or the error on failure).
func startMQProcessSpan(ctx context.Context, taskType string) (context.Context, func(error)) {
	ctx, span := mqTracer.Start(ctx, tracing.SpanBrokerProcess,
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
