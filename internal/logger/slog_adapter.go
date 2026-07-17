package logger

import "log/slog"

// SlogLogger wraps *slog.Logger to satisfy the legacy Logger interfaces defined
// in server, agent, and pkg/client packages:
//
//	type Logger interface {
//	    Info(msg string, args ...any)
//	    Error(msg string, args ...any)
//	    Debug(msg string, args ...any)
//	}
//
// The wrapper is safe for concurrent use because *slog.Logger is itself safe for
// concurrent use.
//
// Compile-time interface assertions are intentionally commented out here; they
// will be uncommented once those packages have been migrated to depend on this
// logger package (currently they define their own Logger interfaces). Adding the
// assertions now would create an import cycle.
//
//	var _ server.Logger = (*SlogLogger)(nil)
//	var _ agent.Logger = (*SlogLogger)(nil)
//	var _ client.Logger = (*SlogLogger)(nil)
type SlogLogger struct {
	inner *slog.Logger
}

// NewSlogLogger creates a new SlogLogger wrapping the given *slog.Logger.
// If l is nil, slog.Default() is used.
func NewSlogLogger(l *slog.Logger) *SlogLogger {
	if l == nil {
		l = slog.Default()
	}
	return &SlogLogger{inner: l}
}

// Inner returns the underlying *slog.Logger. Useful for callers that need to
// pass the logger to functions that accept *slog.Logger directly.
func (s *SlogLogger) Inner() *slog.Logger { return s.inner }

// Info logs a message at info level.
func (s *SlogLogger) Info(msg string, args ...any) {
	s.inner.Info(msg, args...)
}

// Error logs a message at error level.
func (s *SlogLogger) Error(msg string, args ...any) {
	s.inner.Error(msg, args...)
}

// Debug logs a message at debug level.
func (s *SlogLogger) Debug(msg string, args ...any) {
	s.inner.Debug(msg, args...)
}
