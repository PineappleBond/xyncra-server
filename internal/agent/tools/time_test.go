package tools

import (
	"context"
	"strings"
	"testing"
)

func TestTimeTool_Info(t *testing.T) {
	tl, err := NewTimeTool()
	if err != nil {
		t.Fatalf("NewTimeTool: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "get_current_time" {
		t.Errorf("Name = %q, want %q", info.Name, "get_current_time")
	}
}

func TestTimeTool_Invoke_ValidTimezone(t *testing.T) {
	tl, err := NewTimeTool()
	if err != nil {
		t.Fatalf("NewTimeTool: %v", err)
	}
	result, err := tl.InvokableRun(context.Background(), `{"timezone":"Asia/Shanghai"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(result, "Asia/Shanghai") {
		t.Errorf("expected result to contain timezone, got: %s", result)
	}
}

func TestTimeTool_Invoke_EmptyTimezone_DefaultsUTC(t *testing.T) {
	tl, err := NewTimeTool()
	if err != nil {
		t.Fatalf("NewTimeTool: %v", err)
	}
	result, err := tl.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(result, "UTC") {
		t.Errorf("expected result to contain UTC, got: %s", result)
	}
}

func TestTimeTool_Invoke_InvalidTimezone_Error(t *testing.T) {
	tl, err := NewTimeTool()
	if err != nil {
		t.Fatalf("NewTimeTool: %v", err)
	}
	result, err := tl.InvokableRun(context.Background(), `{"timezone":"Invalid/Zone"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	// Recoverable failure: returned as a ToolResult envelope (success=false),
	// not as a Go error (D-101).
	if !strings.Contains(result, `"success":false`) {
		t.Errorf("expected success:false envelope for invalid timezone, got: %s", result)
	}
}
