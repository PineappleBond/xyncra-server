package logger

import (
	"io"
	"os"
	"strconv"
	"strings"
)

// Config holds logger configuration loaded from XYNCRA_LOG_* environment variables.
//
// Fields:
//   - Level: minimum log level, one of "debug", "info", "warn", "error". Default "info".
//   - Dir:   directory for rolling log files. Empty means stdout only (zero file I/O).
//   - Format: output format, "text" (default, human-readable) or "json" (structured).
//   - MaxSizeMB:  maximum size in megabytes of a log file before rotation (default 100).
//   - MaxAge:     maximum number of days to retain old log files (default 30).
//   - MaxBackups: maximum number of old log files to retain (default 10).
//   - Compress:   whether to compress rotated log files with gzip (default true).
type Config struct {
	Level      string
	Dir        string
	Format     string
	MaxSizeMB  int
	MaxAge     int
	MaxBackups int
	Compress   bool

	// stdoutWriter overrides the stdout destination for testing. When nil,
	// os.Stdout is used. Callers outside the package should not set this field.
	stdoutWriter io.Writer
}

// DefaultConfig loads configuration from XYNCRA_LOG_* environment variables.
// Returns a Config with sensible defaults when env vars are not set or invalid.
//
// Environment variables:
//
//	XYNCRA_LOG_LEVEL=info           # debug, info, warn, error
//	XYNCRA_LOG_DIR=                 # empty = stdout only; non-empty = stdout + rolling file
//	XYNCRA_LOG_FORMAT=text          # text (default) or json
//	XYNCRA_LOG_MAX_SIZE=100         # single file max MB
//	XYNCRA_LOG_MAX_AGE=30           # retention days
//	XYNCRA_LOG_MAX_BACKUPS=10       # max backup count
//	XYNCRA_LOG_COMPRESS=true        # gzip compress rotated files
func DefaultConfig() Config {
	return Config{
		Level:      envLevel("XYNCRA_LOG_LEVEL", "info"),
		Dir:        strings.TrimSpace(os.Getenv("XYNCRA_LOG_DIR")),
		Format:     envFormat("XYNCRA_LOG_FORMAT", "text"),
		MaxSizeMB:  envInt("XYNCRA_LOG_MAX_SIZE", 100),
		MaxAge:     envInt("XYNCRA_LOG_MAX_AGE", 30),
		MaxBackups: envInt("XYNCRA_LOG_MAX_BACKUPS", 10),
		Compress:   envBool("XYNCRA_LOG_COMPRESS", true),
	}
}

// envLevel reads a log level env var, falling back to defaultVal if the value is
// empty or not one of the recognized levels (debug/info/warn/error, case-insensitive).
// The returned level is always lowercased.
func envLevel(key, defaultVal string) string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "debug", "info", "warn", "error":
		return v
	case "":
		return defaultVal
	default:
		return defaultVal
	}
}

// envFormat reads a log format env var, falling back to defaultVal if the value is
// empty or not one of the recognized formats (text/json, case-insensitive).
// The returned format is always lowercased.
func envFormat(key, defaultVal string) string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "text", "json":
		return v
	case "":
		return defaultVal
	default:
		return defaultVal
	}
}

// envInt reads an integer env var, falling back to defaultVal if the value is
// empty or cannot be parsed. Negative values are clamped to 0.
func envInt(key string, defaultVal int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	if n < 0 {
		return 0
	}
	return n
}

// envBool reads a boolean env var, falling back to defaultVal if the value is
// empty or cannot be parsed.
func envBool(key string, defaultVal bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}
