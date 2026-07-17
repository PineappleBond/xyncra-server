package server

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

// serverTracer is the OpenTelemetry tracer for the xyncra-server service.
// When tracing is disabled, a no-op tracer is installed by tracing.InitTracer,
// resulting in zero allocation and zero overhead.
var serverTracer = otel.Tracer("xyncra-server")

// startConnectionSpan creates a ws.connection span that covers the entire
// WebSocket connection lifecycle. It records user, device, and connection
// identifiers as span attributes.
func startConnectionSpan(ctx context.Context, userID, deviceID, connID string) (context.Context, func(error)) {
	// Mark context for debug sampling if user/device matches debug list.
	ctx = tracing.WithDebug(ctx, userID, deviceID)

	ctx, span := serverTracer.Start(ctx, tracing.SpanWSConnection,
		trace.WithAttributes(
			attribute.String(tracing.AttrUserID, userID),
			attribute.String(tracing.AttrDeviceID, deviceID),
			attribute.String(tracing.AttrConnID, connID),
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

// startMessageReceiveSpan creates a ws.message.receive span for a single
// incoming WebSocket message. It records the method name (when available)
// and the raw message size.
func startMessageReceiveSpan(ctx context.Context, method string, sizeBytes int) (context.Context, func(error)) {
	ctx, span := serverTracer.Start(ctx, tracing.SpanWSMessageReceive,
		trace.WithAttributes(
			attribute.String(tracing.AttrMethod, method),
			attribute.Int(tracing.AttrSizeBytes, sizeBytes),
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

// startHandlerInvokeSpan creates a handler.invoke span for the duration of a
// method handler invocation. It records the method name being handled.
func startHandlerInvokeSpan(ctx context.Context, method string) (context.Context, func(error)) {
	ctx, span := serverTracer.Start(ctx, tracing.SpanHandlerInvoke,
		trace.WithAttributes(
			attribute.String(tracing.AttrMethod, method),
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

// startBroadcastSpan creates a handler.broadcast span for a BroadcastUpdates
// call. It records the target user ID.
func startBroadcastSpan(ctx context.Context, targetUserID string) (context.Context, func(error)) {
	ctx, span := serverTracer.Start(ctx, tracing.SpanHandlerBroadcast,
		trace.WithAttributes(
			attribute.String(tracing.AttrTargetUserID, targetUserID),
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

// startMessageSendSpan creates a ws.message.send span for an outbound message.
// It records the target type (user/device).
func startMessageSendSpan(ctx context.Context, targetType string) (context.Context, func(error)) {
	ctx, span := serverTracer.Start(ctx, tracing.SpanWSMessageSend,
		trace.WithAttributes(
			attribute.String(tracing.AttrTargetType, targetType),
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
