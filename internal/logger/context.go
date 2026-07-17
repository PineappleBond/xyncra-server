package logger

import (
	"context"
	"log/slog"
)

// contextKey is an unexported type used as the key for storing a *slog.Logger in
// context.Context. Using a private type prevents collisions with keys defined in
// other packages.
type contextKey struct{}

// FromContext returns the *slog.Logger stored in ctx, or slog.Default() if no
// logger is present. The returned *slog.Logger is always non-nil.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return slog.Default()
	}
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithContext returns a copy of ctx with the given *slog.Logger stored under a
// private key. The logger can later be retrieved with FromContext. If logger is
// nil, slog.Default() is stored instead, so FromContext never returns nil.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		logger = slog.Default()
	}
	return context.WithValue(ctx, contextKey{}, logger)
}
