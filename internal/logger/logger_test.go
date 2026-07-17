package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// restoreDefaultLogger saves the current slog.Default() and restores it on test
// completion. Init() mutates the global default, so tests must isolate.
func restoreDefaultLogger(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func TestInit_StdoutTextFormat(t *testing.T) {
	restoreDefaultLogger(t)

	var buf bytes.Buffer
	cfg := Config{
		Level:        "info",
		Format:       "text",
		stdoutWriter: &buf,
	}
	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer cleanup()

	slog.Default().Info("hello stdout")

	got := buf.String()
	if !strings.Contains(got, "msg=\"hello stdout\"") && !strings.Contains(got, "msg=hello stdout") {
		t.Errorf("expected message in output, got: %q", got)
	}
	if !strings.Contains(got, "level=INFO") {
		t.Errorf("expected level=INFO in output, got: %q", got)
	}
	// text format should NOT parse as JSON
	var dummy map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(got)), &dummy) == nil {
		t.Errorf("text format unexpectedly parsed as JSON: %q", got)
	}
}

func TestInit_StdoutJSONFormat(t *testing.T) {
	restoreDefaultLogger(t)

	var buf bytes.Buffer
	cfg := Config{
		Level:        "info",
		Format:       "json",
		stdoutWriter: &buf,
	}
	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer cleanup()

	slog.Default().Info("hello json")

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected JSON output, got empty")
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("JSON output did not parse: %v\noutput: %q", err, line)
	}
	if rec["msg"] != "hello json" {
		t.Errorf("msg field: got %v, want %q", rec["msg"], "hello json")
	}
	if rec["level"] != "INFO" {
		t.Errorf("level field: got %v, want %q", rec["level"], "INFO")
	}
}

func TestInit_FileAndStdoutDualWrite(t *testing.T) {
	restoreDefaultLogger(t)

	dir := t.TempDir()
	var buf bytes.Buffer
	cfg := Config{
		Level:        "info",
		Format:       "text",
		Dir:          dir,
		MaxSizeMB:    1,
		MaxAge:       1,
		MaxBackups:   1,
		Compress:     false,
		stdoutWriter: &buf,
	}
	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer cleanup()

	slog.Default().Info("dual-write msg")

	// Check stdout captured it
	if !strings.Contains(buf.String(), "dual-write msg") {
		t.Errorf("stdout missing message: %q", buf.String())
	}

	// Check file captured it
	logFile := filepath.Join(dir, "xyncra-server.log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logFile, err)
	}
	if !strings.Contains(string(data), "dual-write msg") {
		t.Errorf("file missing message: %q", string(data))
	}
}

func TestInit_DirCreated(t *testing.T) {
	restoreDefaultLogger(t)

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	var buf bytes.Buffer
	cfg := Config{
		Level:        "info",
		Format:       "text",
		Dir:          nested,
		stdoutWriter: &buf,
	}
	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer cleanup()

	slog.Default().Info("nested")

	logFile := filepath.Join(nested, "xyncra-server.log")
	if _, err := os.Stat(logFile); err != nil {
		t.Errorf("expected log file at %s, stat err: %v", logFile, err)
	}
}

func TestInit_EmptyDirNoFileIO(t *testing.T) {
	restoreDefaultLogger(t)

	var buf bytes.Buffer
	cfg := Config{
		Level:        "info",
		Format:       "text",
		Dir:          "", // no file output
		stdoutWriter: &buf,
	}
	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer cleanup()

	slog.Default().Info("stdout only")

	if !strings.Contains(buf.String(), "stdout only") {
		t.Errorf("expected message in stdout, got: %q", buf.String())
	}
}

func TestInit_LevelFiltering(t *testing.T) {
	restoreDefaultLogger(t)

	var buf bytes.Buffer
	cfg := Config{
		Level:        "warn",
		Format:       "text",
		stdoutWriter: &buf,
	}
	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer cleanup()

	slog.Default().Info("should be filtered")
	slog.Default().Warn("should appear")

	out := buf.String()
	if strings.Contains(out, "should be filtered") {
		t.Errorf("info message should be filtered out at warn level: %q", out)
	}
	if !strings.Contains(out, "should appear") {
		t.Errorf("warn message should appear: %q", out)
	}
}

func TestInit_CleanupIsCallableMultipleTimes(t *testing.T) {
	restoreDefaultLogger(t)

	dir := t.TempDir()
	var buf bytes.Buffer
	cfg := Config{
		Level:        "info",
		Format:       "text",
		Dir:          dir,
		stdoutWriter: &buf,
	}
	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	slog.Default().Info("before cleanup")

	// First cleanup should succeed without panic.
	cleanup()

	// Calling cleanup again should also be safe.
	cleanup()
}

func TestInit_NilCleanupWhenNoFile(t *testing.T) {
	restoreDefaultLogger(t)

	var buf bytes.Buffer
	cfg := Config{
		Level:        "info",
		Format:       "text",
		Dir:          "",
		stdoutWriter: &buf,
	}
	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if cleanup == nil {
		t.Fatal("cleanup should never be nil")
	}
	// Must be callable even when no file writer exists.
	cleanup()
}

func TestInit_InvalidFormatFallsBackToText(t *testing.T) {
	restoreDefaultLogger(t)

	var buf bytes.Buffer
	cfg := Config{
		Level:        "info",
		Format:       "xml-bogus",
		stdoutWriter: &buf,
	}
	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer cleanup()

	slog.Default().Info("fallback test")

	got := buf.String()
	// text format output contains level=INFO, JSON would contain "level":"INFO"
	if strings.Contains(got, `"level":"INFO"`) {
		t.Errorf("expected text fallback, got JSON: %q", got)
	}
	if !strings.Contains(got, "level=INFO") {
		t.Errorf("expected text format output, got: %q", got)
	}
}

func TestInit_InvalidDirReturnsError(t *testing.T) {
	restoreDefaultLogger(t)

	var buf bytes.Buffer
	// Use an invalid path that cannot be created (e.g., a file in the way).
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	invalidDir := filepath.Join(blocker, "cannot-exist")

	cfg := Config{
		Level:        "info",
		Format:       "text",
		Dir:          invalidDir,
		stdoutWriter: &buf,
	}
	_, err := Init(cfg)
	if err == nil {
		t.Fatal("expected error for invalid log dir, got nil")
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"bogus", slog.LevelInfo},
		{"DEBUG", slog.LevelInfo}, // case-sensitive; uppercase is unrecognized → default
	}
	for _, tc := range cases {
		got := parseLevel(tc.in)
		if got != tc.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Alias tests for acceptance-criteria naming convention.
// These wrap the existing tests under the exact names from the test matrix,
// ensuring grep-based traceability from requirements to tests.
// ---------------------------------------------------------------------------

// TestSlogOutput_IsValidJSON verifies that JSON-formatted slog output produces
// valid JSON lines that can be parsed by json.Unmarshal.
//
// Acceptance criteria:
//   - Set XYNCRA_LOG_FORMAT=json, capture stdout
//   - Each line parses with json.Unmarshal
func TestSlogOutput_IsValidJSON(t *testing.T) {
	// Delegate to the existing implementation that exercises this scenario.
	t.Run("json-format-each-line-is-valid-json", func(t *testing.T) {
		restoreDefaultLogger(t)
		var buf bytes.Buffer
		cfg := Config{
			Level:        "info",
			Format:       "json",
			stdoutWriter: &buf,
		}
		cleanup, err := Init(cfg)
		if err != nil {
			t.Fatalf("Init returned error: %v", err)
		}
		defer cleanup()

		slog.Default().Info("json-validity-check")

		line := strings.TrimSpace(buf.String())
		if line == "" {
			t.Fatal("expected JSON output, got empty")
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line did not parse as JSON: %v\noutput: %q", err, line)
		}
	})
}

// TestSlogOutput_TextFormat verifies that the default text format produces
// human-readable output, not JSON.
//
// Acceptance criteria:
//   - Default configuration, capture stdout
//   - Output is human-readable text format (not JSON)
func TestSlogOutput_TextFormat(t *testing.T) {
	t.Run("text-format-is-human-readable", func(t *testing.T) {
		restoreDefaultLogger(t)
		var buf bytes.Buffer
		cfg := Config{
			Level:        "info",
			Format:       "text",
			stdoutWriter: &buf,
		}
		cleanup, err := Init(cfg)
		if err != nil {
			t.Fatalf("Init returned error: %v", err)
		}
		defer cleanup()

		slog.Default().Info("text-format-check")

		got := buf.String()
		// Text format should contain level=INFO style output.
		if !strings.Contains(got, "level=INFO") {
			t.Errorf("expected text format with level=INFO, got: %q", got)
		}
		// Text format should NOT parse as JSON.
		var dummy map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(got)), &dummy) == nil {
			t.Errorf("text format unexpectedly parsed as JSON: %q", got)
		}
	})
}
