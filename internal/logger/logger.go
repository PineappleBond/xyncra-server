package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Init initializes the global slog.Default() logger according to cfg.
//
// Behavior:
//   - Level is parsed from cfg.Level ("debug", "info", "warn", "error"). Invalid
//     values fall back to info.
//   - Format is "text" (slog.TextHandler) or "json" (slog.JSONHandler). Invalid
//     values fall back to text.
//   - When cfg.Dir is empty, output goes to stdout only. Zero file I/O is
//     performed (D-063 optional module pattern).
//   - When cfg.Dir is non-empty, output goes to both stdout and a rolling log
//     file at <cfg.Dir>/xyncra-server.log, rotated by lumberjack.
//
// Returns a cleanup function that MUST be called on program exit to release
// resources (currently closes the lumberjack writer). The cleanup function is
// always safe to call, even when no file writer was opened.
//
// Init is not safe for concurrent use; call it once at program startup.
func Init(cfg Config) (cleanup func(), err error) {
	level := parseLevel(cfg.Level)
	format := cfg.Format
	if format != "json" {
		format = "text"
	}

	var stdoutW io.Writer = os.Stdout
	if cfg.stdoutWriter != nil {
		stdoutW = cfg.stdoutWriter
	}

	w := stdoutW
	var lj *lumberjack.Logger

	if cfg.Dir != "" {
		// Ensure the directory exists. os.MkdirAll is a no-op if the directory
		// already exists.
		if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
			return func() {}, fmt.Errorf("logger: create log dir %q: %w", cfg.Dir, err)
		}
		filename := filepath.Join(cfg.Dir, "xyncra-server.log")
		lj = &lumberjack.Logger{
			Filename:   filename,
			MaxSize:    cfg.MaxSizeMB,
			MaxAge:     cfg.MaxAge,
			MaxBackups: cfg.MaxBackups,
			Compress:   cfg.Compress,
		}
		w = io.MultiWriter(stdoutW, lj)
	}

	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	default:
		handler = slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(handler))

	cleanup = func() {
		// lumberjack.Logger.Close is safe to call even after no writes.
		if lj != nil {
			_ = lj.Close()
		}
	}
	return cleanup, nil
}

// parseLevel converts a string level name to a slog.Level. Unrecognized values
// default to slog.LevelInfo.
func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
