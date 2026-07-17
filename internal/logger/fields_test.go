package logger

import (
	"log/slog"
	"testing"
)

func TestFieldHelpers_AttributeKeys(t *testing.T) {
	tests := []struct {
		name string
		attr slog.Attr
		key  string
	}{
		{"AgentID", AgentID("a1"), "agent_id"},
		{"UserID", UserID("u1"), "user_id"},
		{"DeviceID", DeviceID("d1"), "device_id"},
		{"ConversationID", ConversationID("c1"), "conversation_id"},
		{"Model", Model("claude-3"), "model"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.attr.Key != tc.key {
				t.Errorf("%s: key = %q, want %q", tc.name, tc.attr.Key, tc.key)
			}
		})
	}
}

func TestFieldHelpers_StringValues(t *testing.T) {
	if v := AgentID("a1").Value.String(); v != "a1" {
		t.Errorf("AgentID value: got %q, want %q", v, "a1")
	}
	if v := UserID("u1").Value.String(); v != "u1" {
		t.Errorf("UserID value: got %q, want %q", v, "u1")
	}
	if v := DeviceID("d1").Value.String(); v != "d1" {
		t.Errorf("DeviceID value: got %q, want %q", v, "d1")
	}
	if v := ConversationID("c1").Value.String(); v != "c1" {
		t.Errorf("ConversationID value: got %q, want %q", v, "c1")
	}
	if v := Model("claude-3").Value.String(); v != "claude-3" {
		t.Errorf("Model value: got %q, want %q", v, "claude-3")
	}
}

func TestDurationMs_KeyAndValue(t *testing.T) {
	attr := DurationMs(1234)
	if attr.Key != "duration_ms" {
		t.Errorf("key: got %q, want %q", attr.Key, "duration_ms")
	}
	if attr.Value.Int64() != 1234 {
		t.Errorf("value: got %d, want 1234", attr.Value.Int64())
	}
}

func TestDurationMs_ZeroAndNegative(t *testing.T) {
	if got := DurationMs(0).Value.Int64(); got != 0 {
		t.Errorf("zero duration: got %d, want 0", got)
	}
	if got := DurationMs(-1).Value.Int64(); got != -1 {
		t.Errorf("negative duration: got %d, want -1", got)
	}
}

func TestFieldHelpers_EmptyStrings(t *testing.T) {
	// Empty strings should still produce attributes with the correct keys.
	cases := []struct {
		attr slog.Attr
		key  string
	}{
		{AgentID(""), "agent_id"},
		{UserID(""), "user_id"},
		{DeviceID(""), "device_id"},
		{ConversationID(""), "conversation_id"},
		{Model(""), "model"},
	}
	for _, tc := range cases {
		if tc.attr.Key != tc.key {
			t.Errorf("empty string: key = %q, want %q", tc.attr.Key, tc.key)
		}
		if tc.attr.Value.String() != "" {
			t.Errorf("empty string: value = %q, want empty", tc.attr.Value.String())
		}
	}
}
