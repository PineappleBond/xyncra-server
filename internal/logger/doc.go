// Package logger provides structured logging for Xyncra Server, built on top of
// the standard library log/slog package.
//
// Architecture:
//
//	Init(cfg)       → install global slog.Default() logger with optional file rotation
//	config.go       → Config from XYNCRA_LOG_* environment variables
//	context.go      → store/retrieve *slog.Logger in context.Context
//	component.go    → per-component loggers (component=server/agent/mq/...)
//	fields.go       → typed slog.Attr helpers for common business fields
//	slog_adapter.go → SlogLogger adapter satisfying the legacy Logger interfaces
//
// Configuration is opt-in (D-063): when XYNCRA_LOG_DIR is empty, logs go only to
// stdout with zero file I/O overhead. Setting XYNCRA_LOG_DIR enables rolling file
// output via lumberjack alongside stdout.
//
// Default format is human-readable text (XYNCRA_LOG_FORMAT=text). Docker/production
// deployments may switch to JSON (XYNCRA_LOG_FORMAT=json) for structured log
// collection (e.g. Promtail → Loki).
//
// Concurrency: *slog.Logger is safe for concurrent use; the SlogLogger wrapper
// is therefore also safe for concurrent use by multiple goroutines.
//
// This package intentionally does NOT provide a global Logger facade. Callers are
// expected to use slog.Default() or a *slog.Logger obtained from context (via
// FromContext) or WithComponent. This keeps the API surface small and aligns with
// the standard library.
package logger
