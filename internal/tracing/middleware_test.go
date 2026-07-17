package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestNewDebugSampler_Creates(t *testing.T) {
	s := NewDebugSampler(0.5, nil, nil)
	assert.NotNil(t, s)
}

func TestDebugSampler_Description(t *testing.T) {
	s := NewDebugSampler(0.5, nil, nil)
	desc := s.Description()
	assert.Contains(t, desc, "DebugSampler")
	assert.Contains(t, desc, "fallback")
}

func TestDebugSampler_Fallback_AlwaysSample(t *testing.T) {
	s := NewDebugSampler(1.0, nil, nil) // 100% fallback sampling
	p := sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
	}
	result := s.ShouldSample(p)
	assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
}

func TestDebugSampler_Fallback_NeverSample(t *testing.T) {
	s := NewDebugSampler(0.0, nil, nil) // 0% fallback sampling
	p := sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
	}
	result := s.ShouldSample(p)
	assert.Equal(t, sdktrace.Drop, result.Decision)
}

func TestDebugSampler_ParentSampled(t *testing.T) {
	s := NewDebugSampler(0.0, nil, nil) // fallback is 0%, but parent is sampled
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
		SpanID:     trace.SpanID{0x01},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	p := sdktrace.SamplingParameters{
		ParentContext: trace.ContextWithSpanContext(t.Context(), sc),
		TraceID:       sc.TraceID(),
	}
	result := s.ShouldSample(p)
	assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
}

func TestDebugSampler_ParentNotSampled(t *testing.T) {
	// Parent valid but not sampled, fallback is 100% — should sample via fallback.
	s := NewDebugSampler(1.0, nil, nil)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
		SpanID:     trace.SpanID{0x01},
		TraceFlags: 0, // not sampled
		Remote:     true,
	})
	p := sdktrace.SamplingParameters{
		ParentContext: trace.ContextWithSpanContext(t.Context(), sc),
		TraceID:       sc.TraceID(),
	}
	result := s.ShouldSample(p)
	// With ratio 1.0 the fallback should sample.
	assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
}

func TestDebugSampler_DebugUser(t *testing.T) {
	// Debug user in list should be force-sampled even with 0% fallback.
	s := NewDebugSampler(0.0, []string{"user-1", "user-2"}, nil)

	ctx := WithDebug(context.Background(), "user-1", "device-x")
	p := sdktrace.SamplingParameters{
		ParentContext: ctx,
		TraceID:       trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
	}
	result := s.ShouldSample(p)
	assert.Equal(t, sdktrace.RecordAndSample, result.Decision, "debug user should be force-sampled")
}

func TestDebugSampler_DebugDevice(t *testing.T) {
	// Debug device in list should be force-sampled even with 0% fallback.
	s := NewDebugSampler(0.0, nil, []string{"device-abc"})

	ctx := WithDebug(context.Background(), "unknown-user", "device-abc")
	p := sdktrace.SamplingParameters{
		ParentContext: ctx,
		TraceID:       trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
	}
	result := s.ShouldSample(p)
	assert.Equal(t, sdktrace.RecordAndSample, result.Decision, "debug device should be force-sampled")
}

func TestDebugSampler_NonDebugUserFallsThrough(t *testing.T) {
	// User not in debug list should fall through to fallback (0% → drop).
	s := NewDebugSampler(0.0, []string{"user-1"}, nil)

	ctx := WithDebug(context.Background(), "user-other", "device-x")
	p := sdktrace.SamplingParameters{
		ParentContext: ctx,
		TraceID:       trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
	}
	result := s.ShouldSample(p)
	assert.Equal(t, sdktrace.Drop, result.Decision, "non-debug user should fall through to 0% fallback")
}

func TestDebugSampler_NoDebugContext(t *testing.T) {
	// No debug context marker → falls through to fallback (100% → sample).
	s := NewDebugSampler(1.0, []string{"user-1"}, nil)

	p := sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
	}
	result := s.ShouldSample(p)
	assert.Equal(t, sdktrace.RecordAndSample, result.Decision, "no debug context → fallback 100% should sample")
}

func TestIsDebugContext(t *testing.T) {
	assert.False(t, IsDebugContext(context.Background()))

	ctx := WithDebug(context.Background(), "u", "d")
	assert.True(t, IsDebugContext(ctx))
}
