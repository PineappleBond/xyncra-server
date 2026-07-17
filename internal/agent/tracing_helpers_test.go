package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
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
// Used to isolate spans created by individual tests.
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

func TestMain(m *testing.M) {
	initTestProvider()
	m.Run()
}

func TestStartAgentExecuteSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startAgentExecuteSpan(context.Background(), "agent-1", "conv-1", "user-1")
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanAgentExecute, spans[0].Name())

	attrs := roAttrMap(spans[0])
	assert.Equal(t, "agent-1", attrs[tracing.AttrAgentID])
	assert.Equal(t, "conv-1", attrs[tracing.AttrConversationID])
	assert.Equal(t, "user-1", attrs[tracing.AttrUserID])
}

func TestStartAgentExecuteSpan_WithError(t *testing.T) {
	startIdx := len(recorder.Ended())

	_, finish := startAgentExecuteSpan(context.Background(), "agent-1", "conv-1", "user-1")
	finish(assert.AnError)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, assert.AnError.Error(), spans[0].Status().Description)
}

func TestStartAgentBuildSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startAgentBuildSpan(context.Background(), "agent-2")
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanAgentBuild, spans[0].Name())

	attrs := roAttrMap(spans[0])
	assert.Equal(t, "agent-2", attrs[tracing.AttrAgentID])
}

func TestStartAgentRunSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startAgentRunSpan(context.Background(), "agent-3")
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanAgentRun, spans[0].Name())
}

func TestStartAgentStreamSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startAgentStreamSpan(context.Background())
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanAgentStream, spans[0].Name())
}

func TestStartAgentCheckpointSaveSpan(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startAgentCheckpointSaveSpan(context.Background(), "cp-123")
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanAgentCheckpointSave, spans[0].Name())

	attrs := roAttrMap(spans[0])
	assert.Equal(t, "cp-123", attrs[tracing.AttrCheckpointID])
}
