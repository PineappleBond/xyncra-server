package tracing

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// InitTracer initializes the OpenTelemetry tracing subsystem.
// When cfg.Enabled is false, a no-op tracer provider is installed, ensuring zero overhead.
// The returned shutdown function must be called on application exit to flush pending spans.
func InitTracer(cfg TracingConfig) (shutdown func(context.Context) error, err error) {
	if !cfg.Enabled {
		otel.SetTracerProvider(noop.NewTracerProvider())
		otel.SetTextMapPropagator(propagation.TraceContext{})
		return func(context.Context) error { return nil }, nil
	}

	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(version()),
			semconv.HostName(hostname()),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: create resource: %w", err)
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("tracing: create otlp exporter: %w", err)
	}

	sampler := NewDebugSampler(cfg.SampleRate, cfg.DebugUsers, cfg.DebugDevices)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdownFn := func(ctx context.Context) error {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("tracing: shutdown tracer provider: %w", err)
		}
		return nil
	}

	return shutdownFn, nil
}

// Tracer returns a named tracer from the global tracer provider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// version returns the service version from the XYNCRA_VERSION environment variable,
// or "dev" if not set.
func version() string {
	if v := os.Getenv("XYNCRA_VERSION"); v != "" {
		return v
	}
	return "dev"
}

// hostname returns the system hostname or "unknown" on error.
func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		log.Printf("tracing: failed to get hostname: %v", err)
		return "unknown"
	}
	return h
}
