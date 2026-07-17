package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

// recorder is a shared SpanRecorder used across all tests in this package.
// It is initialized once via TestMain to ensure the global tracer provider
// delegate is set exactly once (OTel uses sync.Once for delegation).
var recorder *tracetest.SpanRecorder

func initTestProvider() {
	recorder = tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
}

// spansSince returns all spans that ended after the given index.
func spansSince(startIdx int) []sdktrace.ReadOnlySpan {
	all := recorder.Ended()
	if startIdx >= len(all) {
		return nil
	}
	return all[startIdx:]
}

// roAttrMap extracts attributes from a ReadOnlySpan into a map[string]string.
func roAttrMap(s sdktrace.ReadOnlySpan) map[string]string {
	m := make(map[string]string)
	for _, kv := range s.Attributes() {
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

// attrMapSlice converts a slice of attribute.KeyValue into a map[string]string.
func attrMapSlice(attrs []attribute.KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

func TestMain(m *testing.M) {
	initTestProvider()
	m.Run()
}

func TestStartConnectionSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startConnectionSpan(context.Background(), "user-1", "device-1", "conn-1")
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanWSConnection, spans[0].Name())

	attrs := roAttrMap(spans[0])
	assert.Equal(t, "user-1", attrs[tracing.AttrUserID])
	assert.Equal(t, "device-1", attrs[tracing.AttrDeviceID])
	assert.Equal(t, "conn-1", attrs[tracing.AttrConnID])
}

func TestStartConnectionSpan_WithError(t *testing.T) {
	startIdx := len(recorder.Ended())

	_, finish := startConnectionSpan(context.Background(), "user-1", "device-1", "conn-1")
	finish(assert.AnError)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.NotEmpty(t, spans[0].Status().Description)
}

func TestStartMessageReceiveSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startMessageReceiveSpan(context.Background(), "send_message", 128)
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanWSMessageReceive, spans[0].Name())

	attrs := roAttrMap(spans[0])
	assert.Equal(t, "send_message", attrs[tracing.AttrMethod])
	assert.Equal(t, "128", attrs["xyncra.size_bytes"])
}

func TestStartHandlerInvokeSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startHandlerInvokeSpan(context.Background(), "get_conversation")
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanHandlerInvoke, spans[0].Name())

	attrs := roAttrMap(spans[0])
	assert.Equal(t, "get_conversation", attrs[tracing.AttrMethod])
}

func TestStartBroadcastSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startBroadcastSpan(context.Background(), "target-user-1")
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanHandlerBroadcast, spans[0].Name())

	attrs := roAttrMap(spans[0])
	assert.Equal(t, "target-user-1", attrs["xyncra.target_user_id"])
}

func TestStartMessageSendSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startMessageSendSpan(context.Background(), "user")
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanWSMessageSend, spans[0].Name())

	attrs := roAttrMap(spans[0])
	assert.Equal(t, "user", attrs["xyncra.target_type"])
}

func TestAllSpans_CreateAndEndCleanly(t *testing.T) {
	startIdx := len(recorder.Ended())
	ctx := context.Background()

	ctx1, f1 := startConnectionSpan(ctx, "u", "d", "c")
	f1(nil)
	ctx2, f2 := startMessageReceiveSpan(ctx1, "m", 10)
	f2(nil)
	ctx3, f3 := startHandlerInvokeSpan(ctx2, "h")
	f3(nil)
	ctx4, f4 := startBroadcastSpan(ctx3, "u")
	f4(nil)
	_, f5 := startMessageSendSpan(ctx4, "device")
	f5(nil)

	spans := spansSince(startIdx)
	assert.Len(t, spans, 5)
}
