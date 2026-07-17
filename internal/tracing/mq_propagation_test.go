package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// setupTestTracer installs an InMemoryExporter-backed tracer provider
// and registers it as the global provider. The provider is shut down
// automatically when the test finishes.
func setupTestTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		tp.Shutdown(context.Background())
	})
	return exporter
}

func TestMapCarrier_GetSetKeys(t *testing.T) {
	c := MapCarrier{}
	c.Set("traceparent", "00-abc-def-01")
	assert.Equal(t, "00-abc-def-01", c.Get("traceparent"))
	assert.Equal(t, "", c.Get("nonexistent"))
	keys := c.Keys()
	assert.Contains(t, keys, "traceparent")
}

func TestMapCarrier_Empty(t *testing.T) {
	c := MapCarrier{}
	assert.Empty(t, c.Get("anything"))
	assert.Empty(t, c.Keys())
}

func TestInjectExtract_RoundTrip(t *testing.T) {
	_ = setupTestTracer(t)
	tracer := otel.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	// Inject.
	metadata := InjectTraceContext(ctx)
	assert.NotEmpty(t, metadata)
	assert.Contains(t, metadata, "traceparent")

	// Extract.
	extractedCtx := ExtractTraceContext(metadata)
	extractedSC := trace.SpanContextFromContext(extractedCtx)
	originalSC := trace.SpanContextFromContext(ctx)

	assert.Equal(t, originalSC.TraceID(), extractedSC.TraceID())
	assert.Equal(t, originalSC.SpanID(), extractedSC.SpanID())
	assert.True(t, extractedSC.IsRemote(), "extracted span context should be marked remote")
}

func TestInjectTraceContext_NoSpan(t *testing.T) {
	_ = setupTestTracer(t)

	// An empty context with no active span should produce empty metadata.
	metadata := InjectTraceContext(context.Background())
	assert.Empty(t, metadata)
}

func TestExtractTraceContext_NilMetadata(t *testing.T) {
	_ = setupTestTracer(t)

	ctx := ExtractTraceContext(nil)
	sc := trace.SpanContextFromContext(ctx)
	assert.False(t, sc.IsValid())
}

func TestExtractTraceContext_EmptyMetadata(t *testing.T) {
	_ = setupTestTracer(t)

	ctx := ExtractTraceContext(map[string]string{})
	sc := trace.SpanContextFromContext(ctx)
	assert.False(t, sc.IsValid())
}

func TestExtractTraceContext_InvalidTraceparent(t *testing.T) {
	_ = setupTestTracer(t)

	ctx := ExtractTraceContext(map[string]string{
		"traceparent": "garbage-value",
	})
	sc := trace.SpanContextFromContext(ctx)
	assert.False(t, sc.IsValid(), "malformed traceparent should not produce valid span context")
}

func TestInjectExtract_DifferentTraceIDs(t *testing.T) {
	_ = setupTestTracer(t)
	tracer := otel.Tracer("test")

	// Create two separate traces.
	ctx1, span1 := tracer.Start(context.Background(), "span-1")
	defer span1.End()
	ctx2, span2 := tracer.Start(context.Background(), "span-2")
	defer span2.End()

	md1 := InjectTraceContext(ctx1)
	md2 := InjectTraceContext(ctx2)

	sc1 := trace.SpanContextFromContext(ExtractTraceContext(md1))
	sc2 := trace.SpanContextFromContext(ExtractTraceContext(md2))

	assert.NotEqual(t, sc1.TraceID(), sc2.TraceID(), "different traces should have different trace IDs")
}

func TestExtractTraceContext_PreservesTraceFlags(t *testing.T) {
	_ = setupTestTracer(t)
	tracer := otel.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "flagged-span")
	defer span.End()

	metadata := InjectTraceContext(ctx)
	extractedCtx := ExtractTraceContext(metadata)
	extractedSC := trace.SpanContextFromContext(extractedCtx)

	// The sampled flag should survive round-trip.
	assert.True(t, extractedSC.TraceFlags().IsSampled())
}
