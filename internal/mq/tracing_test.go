package mq

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
var recorder *tracetest.SpanRecorder

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

func TestMain(m *testing.M) {
	recorder = tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	m.Run()
}

func TestStartMQProcessSpan_Success(t *testing.T) {
	startIdx := len(recorder.Ended())

	ctx, finish := startMQProcessSpan(context.Background(), "agent_task")
	require.NotNil(t, ctx)
	finish(nil)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanBrokerProcess, spans[0].Name())

	attrs := roAttrMap(spans[0])
	assert.Equal(t, "agent_task", attrs[tracing.AttrTaskType])
}

func TestStartMQProcessSpan_WithError(t *testing.T) {
	startIdx := len(recorder.Ended())

	_, finish := startMQProcessSpan(context.Background(), "agent_task")
	finish(assert.AnError)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.NotEmpty(t, spans[0].Status().Description)
}

func TestMQTraceContextPropagation_InjectExtract(t *testing.T) {
	startIdx := len(recorder.Ended())
	tracer := otel.Tracer("mq-test")

	// Producer side: create a span and inject context.
	ctx, span := tracer.Start(context.Background(), "produce")
	metadata := tracing.InjectTraceContext(ctx)
	span.End()

	assert.Contains(t, metadata, "traceparent", "injected metadata should contain traceparent")

	// Consumer side: extract context from metadata and create a child span.
	extractedCtx := tracing.ExtractTraceContext(metadata)
	_, childSpan := tracer.Start(extractedCtx, "consume")
	childSpan.End()

	spans := spansSince(startIdx)
	require.Len(t, spans, 2)

	produceSpan := spans[0]
	consumeSpan := spans[1]
	assert.Equal(t, "produce", produceSpan.Name())
	assert.Equal(t, "consume", consumeSpan.Name())
	assert.Equal(t, produceSpan.SpanContext().TraceID(), consumeSpan.Parent().TraceID(),
		"consumer span should be a child of the producer span")
}

func TestStartMQProcessSpan_ContextCarriesParent(t *testing.T) {
	startIdx := len(recorder.Ended())
	tracer := otel.Tracer("mq-test")

	// Create a parent span.
	ctx, parentSpan := tracer.Start(context.Background(), "parent")

	// Create MQ process span as a child.
	_, finish := startMQProcessSpan(ctx, "child_task")
	finish(nil)
	parentSpan.End()

	spans := spansSince(startIdx)
	require.Len(t, spans, 2)

	// Find the MQ process span.
	var mqSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == tracing.SpanBrokerProcess {
			mqSpan = s
			break
		}
	}
	require.NotNil(t, mqSpan, "MQ process span should exist")
	assert.True(t, mqSpan.Parent().IsValid(), "MQ process span should have a valid parent")
}
