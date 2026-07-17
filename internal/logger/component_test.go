package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// installTestLogger installs a slog.Logger writing to buf as the default
// logger, and restores the previous default on test completion.
func installTestLogger(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
}

func TestWithComponent_AddsComponentAttribute(t *testing.T) {
	var buf bytes.Buffer
	installTestLogger(t, &buf)

	log := WithComponent("server")
	log.Info("starting")

	got := buf.String()
	if !strings.Contains(got, "component=server") {
		t.Errorf("expected component=server in output, got: %q", got)
	}
	if !strings.Contains(got, "msg=starting") && !strings.Contains(got, "msg=\"starting\"") {
		t.Errorf("expected msg=starting in output, got: %q", got)
	}
}

func TestWithComponent_DifferentNames(t *testing.T) {
	names := []string{"server", "agent", "mq", "store"}
	for _, name := range names {
		var buf bytes.Buffer
		installTestLogger(t, &buf)

		log := WithComponent(name)
		log.Info("event")

		got := buf.String()
		want := "component=" + name
		if !strings.Contains(got, want) {
			t.Errorf("WithComponent(%q): expected %q in output, got: %q", name, want, got)
		}
	}
}

func TestWithComponent_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))

	log := WithComponent("agent")
	log.Info("json-test")

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("JSON parse failed: %v\noutput: %q", err, buf.String())
	}
	if rec["component"] != "agent" {
		t.Errorf("component field: got %v, want %q", rec["component"], "agent")
	}
}
