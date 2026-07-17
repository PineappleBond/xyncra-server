package logger

import "log/slog"

// WithComponent returns a logger with a "component" attribute attached, derived
// from slog.Default(). The component attribute is useful for identifying the
// subsystem (e.g. "server", "agent", "mq", "store") in structured log output.
//
// Example:
//
//	log := logger.WithComponent("server")
//	log.Info("listening", "addr", addr)
//	// Output: time=... level=INFO source=... component=server msg="listening" addr=...
func WithComponent(name string) *slog.Logger {
	return slog.Default().With("component", name)
}
