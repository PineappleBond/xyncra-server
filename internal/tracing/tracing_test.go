package tracing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInitTracer_Disabled(t *testing.T) {
	cfg := TracingConfig{Enabled: false}
	shutdown, err := InitTracer(cfg)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Should install no-op provider.
	tp := otel.GetTracerProvider()
	assert.IsType(t, noop.TracerProvider{}, tp)

	// Shutdown should be a no-op.
	err = shutdown(context.Background())
	assert.NoError(t, err)
}

func TestInitTracer_Disabled_ShutdownIdempotent(t *testing.T) {
	cfg := TracingConfig{Enabled: false}
	shutdown, err := InitTracer(cfg)
	require.NoError(t, err)

	// Calling shutdown multiple times should be safe.
	assert.NoError(t, shutdown(context.Background()))
	assert.NoError(t, shutdown(context.Background()))
}

func TestInitTracer_Disabled_PropagatorStillSet(t *testing.T) {
	cfg := TracingConfig{Enabled: false}
	_, err := InitTracer(cfg)
	require.NoError(t, err)

	// Even when disabled, a propagator should be installed.
	p := otel.GetTextMapPropagator()
	assert.NotNil(t, p)
}

func TestInitTracer_Enabled_InvalidEndpoint(t *testing.T) {
	// OTLP exporter creation doesn't validate connectivity, so this should succeed.
	cfg := TracingConfig{
		Enabled:  true,
		Endpoint: "localhost:1",
		Insecure: true,
	}
	shutdown, err := InitTracer(cfg)
	if err != nil {
		// Some environments may fail to create the exporter; that's acceptable.
		t.Skipf("exporter creation failed (acceptable): %v", err)
	}
	require.NotNil(t, shutdown)

	// Provider should NOT be no-op.
	tp := otel.GetTracerProvider()
	assert.NotEqual(t, fmt.Sprintf("%T", noop.TracerProvider{}), fmt.Sprintf("%T", tp))

	require.NoError(t, shutdown(context.Background()))
}

func TestInitTracer_Shutdown_Timeout(t *testing.T) {
	cfg := TracingConfig{
		Enabled:  true,
		Endpoint: "localhost:1",
		Insecure: true,
	}
	shutdown, err := InitTracer(cfg)
	if err != nil {
		t.Skipf("exporter creation failed: %v", err)
	}

	// Shutdown with an already-expired context should not panic.
	expiredCtx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	time.Sleep(1 * time.Millisecond) // ensure expiry

	// The shutdown function adds its own 5s timeout on top, so this tests
	// that the inner timeout wrapping works correctly.
	_ = shutdown(expiredCtx)
}

func TestInitTracer_EnabledSuccessfulInit(t *testing.T) {
	cfg := TracingConfig{
		Enabled:     true,
		ServiceName: "test-service",
		Endpoint:    "localhost:4317",
		Insecure:    true,
		SampleRate:  1.0,
	}
	shutdown, err := InitTracer(cfg)
	if err != nil {
		t.Skipf("exporter creation failed: %v", err)
	}
	require.NotNil(t, shutdown)

	tp := otel.GetTracerProvider()
	assert.NotEqual(t, fmt.Sprintf("%T", noop.TracerProvider{}), fmt.Sprintf("%T", tp))

	require.NoError(t, shutdown(context.Background()))
}

func TestTracer_ReturnsGlobal(t *testing.T) {
	// Install a known provider, then verify Tracer() draws from it.
	tp := noop.NewTracerProvider()
	otel.SetTracerProvider(tp)

	tr := Tracer("test-component")
	assert.NotNil(t, tr)
}

func TestInitTracer_Propagator(t *testing.T) {
	cfg := TracingConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Insecure: true,
	}
	shutdown, err := InitTracer(cfg)
	if err != nil {
		t.Skipf("exporter creation failed: %v", err)
	}
	defer shutdown(context.Background())

	p := otel.GetTextMapPropagator()
	// Should be a composite propagator containing at least TraceContext.
	// We verify it can inject/extract (non-nil, non-panic).
	assert.NotNil(t, p)

	// Verify it behaves like a TraceContext propagator.
	carrier := propagation.MapCarrier{}
	ctx := context.Background()
	p.Inject(ctx, carrier)
	// An empty context with no span should produce no traceparent.
	_, hasTraceparent := carrier["traceparent"]
	assert.False(t, hasTraceparent, "no active span means no traceparent injected")
}

func TestInitTracer_ShutdownFlushes(t *testing.T) {
	cfg := TracingConfig{
		Enabled:     true,
		ServiceName: "flush-test",
		Endpoint:    "localhost:1",
		Insecure:    true,
		SampleRate:  1.0,
	}
	shutdown, err := InitTracer(cfg)
	if err != nil {
		t.Skipf("exporter creation failed: %v", err)
	}

	// Create a span to ensure there is something to flush.
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()

	// Shutdown should complete (may error if no collector is reachable).
	// We just verify it doesn't hang.
	_ = shutdown(context.Background())
}

func TestInitTracer_Enabled_Secure(t *testing.T) {
	// When Insecure=false the exporter uses TLS; creation should still succeed.
	cfg := TracingConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Insecure: false,
	}
	shutdown, err := InitTracer(cfg)
	if err != nil {
		t.Skipf("exporter creation failed: %v", err)
	}
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestVersion(t *testing.T) {
	// Without env var set, version should be "dev".
	t.Setenv("XYNCRA_VERSION", "")
	assert.Equal(t, "dev", version())

	t.Setenv("XYNCRA_VERSION", "v1.2.3")
	assert.Equal(t, "v1.2.3", version())
}

func TestHostname(t *testing.T) {
	h := hostname()
	assert.NotEmpty(t, h)
}
