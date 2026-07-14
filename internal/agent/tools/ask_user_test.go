package tools

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// TestAskUserTool_Info
// ---------------------------------------------------------------------------

func TestAskUserTool_Info(t *testing.T) {
	tl, err := NewAskUserTool()
	if err != nil {
		t.Fatalf("NewAskUserTool: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "ask_user" {
		t.Errorf("Name = %q, want %q", info.Name, "ask_user")
	}
	if info.Desc == "" {
		t.Error("expected non-empty description")
	}
}

// ---------------------------------------------------------------------------
// TestAskUserTool_EmptyQuestion
// ---------------------------------------------------------------------------

func TestAskUserTool_EmptyQuestion(t *testing.T) {
	tl, err := NewAskUserTool()
	if err != nil {
		t.Fatalf("NewAskUserTool: %v", err)
	}

	_, err = tl.InvokableRun(context.Background(), `{"question":""}`)
	if err == nil {
		t.Fatal("expected error for empty question, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestAskUserTool_Interrupt
// ---------------------------------------------------------------------------
// A valid question should trigger an interrupt. Without a checkpoint backend
// the interrupt call still returns an error, which is what we assert here.

func TestAskUserTool_Interrupt(t *testing.T) {
	tl, err := NewAskUserTool()
	if err != nil {
		t.Fatalf("NewAskUserTool: %v", err)
	}

	_, err = tl.InvokableRun(context.Background(), `{"question":"Are you sure?"}`)
	if err == nil {
		t.Fatal("expected interrupt error, got nil")
	}
}
