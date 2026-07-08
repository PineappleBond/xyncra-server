package cli

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

func TestCLIUpdateHandler_OnMessage(t *testing.T) {
	h := newCLIUpdateHandler()
	msg := &model.Message{
		MessageID:      42,
		SenderID:       "alice",
		ConversationID: "conv-1",
		Content:        "hello world",
	}

	output := captureStdout(func() {
		if err := h.OnMessage(context.Background(), msg); err != nil {
			t.Fatalf("OnMessage() error: %v", err)
		}
	})

	if !strings.Contains(output, "[new message]") {
		t.Errorf("output should contain [new message], got %q", output)
	}
	if !strings.Contains(output, "seq=42") {
		t.Errorf("output should contain seq=42, got %q", output)
	}
	if !strings.Contains(output, "from=alice") {
		t.Errorf("output should contain from=alice, got %q", output)
	}
	if !strings.Contains(output, "conv=conv-1") {
		t.Errorf("output should contain conv=conv-1, got %q", output)
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("output should contain message content, got %q", output)
	}
}

func TestCLIUpdateHandler_OnMessage_Nil(t *testing.T) {
	h := newCLIUpdateHandler()
	// Should not panic.
	if err := h.OnMessage(context.Background(), nil); err != nil {
		t.Fatalf("OnMessage(nil) error: %v", err)
	}
}

func TestCLIUpdateHandler_OnDeleteMessage(t *testing.T) {
	h := newCLIUpdateHandler()

	output := captureStdout(func() {
		if err := h.OnDeleteMessage(context.Background(), "msg-123", "conv-456"); err != nil {
			t.Fatalf("OnDeleteMessage() error: %v", err)
		}
	})

	if !strings.Contains(output, "[delete message]") {
		t.Errorf("output should contain [delete message], got %q", output)
	}
	if !strings.Contains(output, "conv=conv-456") {
		t.Errorf("output should contain conv=conv-456, got %q", output)
	}
	if !strings.Contains(output, "msg=msg-123") {
		t.Errorf("output should contain msg=msg-123, got %q", output)
	}
}

func TestCLIUpdateHandler_OnMarkRead(t *testing.T) {
	h := newCLIUpdateHandler()

	output := captureStdout(func() {
		if err := h.OnMarkRead(context.Background(), "conv-789", 100); err != nil {
			t.Fatalf("OnMarkRead() error: %v", err)
		}
	})

	if !strings.Contains(output, "[mark read]") {
		t.Errorf("output should contain [mark read], got %q", output)
	}
	if !strings.Contains(output, "conv=conv-789") {
		t.Errorf("output should contain conv=conv-789, got %q", output)
	}
	if !strings.Contains(output, "msg_id=100") {
		t.Errorf("output should contain msg_id=100, got %q", output)
	}
}

func TestCLIUpdateHandler_OnConversation(t *testing.T) {
	h := newCLIUpdateHandler()
	conv := &model.Conversation{
		ID:    "conv-abc",
		Title: "My Chat",
	}

	output := captureStdout(func() {
		if err := h.OnConversation(context.Background(), conv); err != nil {
			t.Fatalf("OnConversation() error: %v", err)
		}
	})

	if !strings.Contains(output, "[conversation]") {
		t.Errorf("output should contain [conversation], got %q", output)
	}
	if !strings.Contains(output, "id=conv-abc") {
		t.Errorf("output should contain id=conv-abc, got %q", output)
	}
	if !strings.Contains(output, "My Chat") {
		t.Errorf("output should contain title, got %q", output)
	}
}

func TestCLIUpdateHandler_OnConversation_Nil(t *testing.T) {
	h := newCLIUpdateHandler()
	// Should not panic.
	if err := h.OnConversation(context.Background(), nil); err != nil {
		t.Fatalf("OnConversation(nil) error: %v", err)
	}
}

func TestCLIUpdateHandler_OnGap(t *testing.T) {
	h := newCLIUpdateHandler()

	output := captureStdout(func() {
		if err := h.OnGap(context.Background(), 999); err != nil {
			t.Fatalf("OnGap() error: %v", err)
		}
	})

	if !strings.Contains(output, "[gap]") {
		t.Errorf("output should contain [gap], got %q", output)
	}
	if !strings.Contains(output, "seq=999") {
		t.Errorf("output should contain seq=999, got %q", output)
	}
}

func TestCLILogger_Info(t *testing.T) {
	l := &cliLogger{debug: false}

	output := captureStderr(func() {
		l.Info("server started", "port", 8080)
	})

	if !strings.Contains(output, "[INFO]") {
		t.Errorf("output should contain [INFO], got %q", output)
	}
	if !strings.Contains(output, "server started") {
		t.Errorf("output should contain message, got %q", output)
	}
	if !strings.Contains(output, "port=8080") {
		t.Errorf("output should contain port=8080, got %q", output)
	}

	// Timestamp format check: should start with [YYYY-MM-DD HH:MM:SS]
	tsPattern := `^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\]`
	matched, _ := regexp.MatchString(tsPattern, output)
	if !matched {
		t.Errorf("output should start with timestamp, got %q", output)
	}
}

func TestCLILogger_Error(t *testing.T) {
	l := &cliLogger{debug: false}

	output := captureStderr(func() {
		l.Error("connection failed", "error", "timeout")
	})

	if !strings.Contains(output, "[ERROR]") {
		t.Errorf("output should contain [ERROR], got %q", output)
	}
	if !strings.Contains(output, "connection failed") {
		t.Errorf("output should contain message, got %q", output)
	}
	if !strings.Contains(output, "error=timeout") {
		t.Errorf("output should contain error=timeout, got %q", output)
	}
}

func TestCLILogger_DebugSuppressed(t *testing.T) {
	l := &cliLogger{debug: false}

	output := captureStderr(func() {
		l.Debug("should not appear")
	})

	if output != "" {
		t.Errorf("Debug should be suppressed when debug=false, got %q", output)
	}
}

func TestCLILogger_DebugEnabled(t *testing.T) {
	l := &cliLogger{debug: true}

	output := captureStderr(func() {
		l.Debug("debug info", "key", "value")
	})

	if !strings.Contains(output, "[DEBUG]") {
		t.Errorf("output should contain [DEBUG], got %q", output)
	}
	if !strings.Contains(output, "debug info") {
		t.Errorf("output should contain message, got %q", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("output should contain key=value, got %q", output)
	}
}

func TestNewCLILogger_DebugEnv(t *testing.T) {
	t.Run("XYNCRA_DEBUG=1", func(t *testing.T) {
		t.Setenv("XYNCRA_DEBUG", "1")
		l := newCLILogger()
		if !l.debug {
			t.Error("debug should be true when XYNCRA_DEBUG=1")
		}
	})

	t.Run("XYNCRA_DEBUG=true", func(t *testing.T) {
		t.Setenv("XYNCRA_DEBUG", "true")
		l := newCLILogger()
		if !l.debug {
			t.Error("debug should be true when XYNCRA_DEBUG=true")
		}
	})

	t.Run("XYNCRA_DEBUG=0", func(t *testing.T) {
		t.Setenv("XYNCRA_DEBUG", "0")
		l := newCLILogger()
		if l.debug {
			t.Error("debug should be false when XYNCRA_DEBUG=0")
		}
	})

	t.Run("XYNCRA_DEBUG not set", func(t *testing.T) {
		t.Setenv("XYNCRA_DEBUG", "")
		l := newCLILogger()
		if l.debug {
			t.Error("debug should be false when XYNCRA_DEBUG is empty")
		}
	})
}

func TestFormatLogArgs(t *testing.T) {
	tests := []struct {
		name string
		args []any
		want string
	}{
		{"empty", nil, ""},
		{"single key", []any{"key"}, " key=MISSING"},
		{"key-value", []any{"key", "value"}, " key=value"},
		{"multiple pairs", []any{"a", 1, "b", 2}, " a=1 b=2"},
		{"odd number", []any{"a", 1, "b"}, " a=1 b=MISSING"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLogArgs(tt.args)
			if got != tt.want {
				t.Errorf("formatLogArgs(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestLogTimestamp(t *testing.T) {
	ts := logTimestamp()
	// Should match YYYY-MM-DD HH:MM:SS format.
	matched, err := regexp.MatchString(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`, ts)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Errorf("logTimestamp() = %q, want format YYYY-MM-DD HH:MM:SS", ts)
	}
}
