package logger

import (
	"context"
	"log/slog"
	"testing"
)

func TestFromContext_NoLoggerReturnsDefault(t *testing.T) {
	got := FromContext(context.Background())
	if got == nil {
		t.Fatal("FromContext returned nil, want non-nil")
	}
	// Should equal slog.Default()
	want := slog.Default()
	if got != want {
		t.Errorf("FromContext on empty ctx returned %p, want slog.Default() %p", got, want)
	}
}

func TestFromContext_NilContextReturnsDefault(t *testing.T) {
	//nolint:staticcheck // intentionally passing nil to verify safety
	got := FromContext(nil)
	if got == nil {
		t.Fatal("FromContext(nil) returned nil, want non-nil")
	}
}

func TestWithContext_ThenFromContext_RoundTrip(t *testing.T) {
	custom := slog.Default().With("marker", "round-trip")
	ctx := WithContext(context.Background(), custom)

	got := FromContext(ctx)
	if got != custom {
		t.Errorf("FromContext returned %p, want %p", got, custom)
	}
}

func TestWithContext_NilLoggerStoresDefault(t *testing.T) {
	ctx := WithContext(context.Background(), nil)

	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext returned nil after WithContext(ctx, nil)")
	}
	// Should be slog.Default() since we passed nil
	want := slog.Default()
	if got != want {
		t.Errorf("FromContext returned %p, want slog.Default() %p", got, want)
	}
}

func TestWithContext_OverrideInNestedContext(t *testing.T) {
	outer := slog.Default().With("scope", "outer")
	inner := slog.Default().With("scope", "inner")

	ctxOuter := WithContext(context.Background(), outer)
	ctxInner := WithContext(ctxOuter, inner)

	// Outer context is unchanged.
	gotOuter := FromContext(ctxOuter)
	if gotOuter != outer {
		t.Errorf("outer ctx: got %p, want %p", gotOuter, outer)
	}
	// Inner context returns inner logger.
	gotInner := FromContext(ctxInner)
	if gotInner != inner {
		t.Errorf("inner ctx: got %p, want %p", gotInner, inner)
	}
}

func TestFromContext_DifferentKeyTypeNotConfused(t *testing.T) {
	// Use a different context key type to ensure we don't accidentally pick up
	// unrelated context values.
	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, slog.Default())

	got := FromContext(ctx)
	want := slog.Default()
	if got != want {
		t.Errorf("FromContext picked up unrelated value: got %p, want slog.Default() %p", got, want)
	}
}
