package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// installJSONTestLogger installs a JSON slog.Logger writing to buf and returns
// a function that decodes the nth record from the buffer.
func installJSONTestLogger(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
}

func TestSlogLogger_InfoLevel(t *testing.T) {
	var buf bytes.Buffer
	installJSONTestLogger(t, &buf)

	sl := NewSlogLogger(slog.Default())
	sl.Info("info message", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("JSON parse: %v\noutput: %q", err, buf.String())
	}
	if rec["msg"] != "info message" {
		t.Errorf("msg: got %v, want %q", rec["msg"], "info message")
	}
	if rec["level"] != "INFO" {
		t.Errorf("level: got %v, want %q", rec["level"], "INFO")
	}
	if rec["k"] != "v" {
		t.Errorf("extra field k: got %v, want %q", rec["k"], "v")
	}
}

func TestSlogLogger_ErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	installJSONTestLogger(t, &buf)

	sl := NewSlogLogger(slog.Default())
	sl.Error("boom", "code", 500)

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("JSON parse: %v", err)
	}
	if rec["msg"] != "boom" {
		t.Errorf("msg: got %v, want %q", rec["msg"], "boom")
	}
	if rec["level"] != "ERROR" {
		t.Errorf("level: got %v, want %q", rec["level"], "ERROR")
	}
}

func TestSlogLogger_DebugLevel(t *testing.T) {
	var buf bytes.Buffer
	installJSONTestLogger(t, &buf)

	sl := NewSlogLogger(slog.Default())
	sl.Debug("trace")

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("JSON parse: %v", err)
	}
	if rec["msg"] != "trace" {
		t.Errorf("msg: got %v, want %q", rec["msg"], "trace")
	}
	if rec["level"] != "DEBUG" {
		t.Errorf("level: got %v, want %q", rec["level"], "DEBUG")
	}
}

func TestSlogLogger_DebugFilteredAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(h))

	sl := NewSlogLogger(slog.Default())
	sl.Debug("should-not-appear")

	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("debug should be filtered at info level, got: %q", buf.String())
	}
}

func TestNewSlogLogger_NilFallsBackToDefault(t *testing.T) {
	sl := NewSlogLogger(nil)
	if sl == nil {
		t.Fatal("NewSlogLogger(nil) returned nil")
	}
	if sl.inner != slog.Default() {
		t.Errorf("inner = %p, want slog.Default() %p", sl.inner, slog.Default())
	}
}

func TestSlogLogger_InnerReturnsUnderlyingLogger(t *testing.T) {
	custom := slog.Default().With("x", "y")
	sl := NewSlogLogger(custom)
	if sl.Inner() != custom {
		t.Errorf("Inner() returned %p, want %p", sl.Inner(), custom)
	}
}

func TestSlogLogger_VariousArgs(t *testing.T) {
	var buf bytes.Buffer
	installJSONTestLogger(t, &buf)

	sl := NewSlogLogger(slog.Default())
	// Pass several mixed-type args; should all be serialized.
	sl.Info("multi", "s", "val", "n", 42, "b", true)

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("JSON parse: %v", err)
	}
	if rec["s"] != "val" {
		t.Errorf("field s: got %v, want %q", rec["s"], "val")
	}
	// JSON numbers decode as float64.
	if n, ok := rec["n"].(float64); !ok || n != 42 {
		t.Errorf("field n: got %v, want 42", rec["n"])
	}
	if rec["b"] != true {
		t.Errorf("field b: got %v, want true", rec["b"])
	}
}

// Compile-time interface conformance checks. These are commented out because
// the server/agent/client Logger interfaces are defined in packages that would
// create import cycles if imported here. They will be uncommented after the
// migration phase.
//
// var _ interface {
// 	Info(msg string, args ...any)
// 	Error(msg string, args ...any)
// 	Debug(msg string, args ...any)
// } = (*SlogLogger)(nil)

// serverLoggerShape is a structural clone of server.Logger used to verify that
// *SlogLogger satisfies the interface without importing the server package
// (which would create an import cycle).
type serverLoggerShape interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
	Debug(msg string, args ...any)
}

// TestSlogLogger_ImplementsServerLogger verifies at compile time that
// *SlogLogger satisfies the server.Logger interface shape. The actual
// cycle-free assertion will be added once the migration is complete;
// this test provides early structural validation.
func TestSlogLogger_ImplementsServerLogger(t *testing.T) {
	var _ serverLoggerShape = (*SlogLogger)(nil)

	// Also verify runtime behavior: the concrete value satisfies the interface.
	var got serverLoggerShape = NewSlogLogger(nil)
	if got == nil {
		t.Fatal("NewSlogLogger(nil) returned nil")
	}
	// Verify all three methods are callable without panic.
	got.Info("test-info", "k", "v")
	got.Error("test-error", "k", "v")
	got.Debug("test-debug", "k", "v")
}
