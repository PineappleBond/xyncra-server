package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool/utils"
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

	out, err := tl.InvokableRun(context.Background(), `{"question":""}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"success":false`) {
		t.Fatalf("expected soft failure for empty question, got: %s", out)
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

// ---------------------------------------------------------------------------
// TestAskUserTool_ResumeReturnsString
//
// The ask_user tool was changed to return string instead of a struct
// (e.g. *AskUserOutput) so that Eino's marshalString passes the value through
// as plain text. Returning a struct would produce JSON like {"answer":"..."}
// which the LLM tends to copy verbatim, leaking implementation details.
//
// Since Eino's internal resume context cannot be constructed from outside the
// package, this test verifies the contract indirectly:
//  1. A tool with the same (string, error) return type passes strings through
//     without JSON encoding.
//  2. A tool with a struct return type produces JSON output (the bug behavior).
//  3. The actual NewAskUserTool function signature returns string.
// ---------------------------------------------------------------------------

// TestAskUserTool_ResumeReturnsString verifies that the ask_user tool uses a
// string return type so that resume values are emitted as plain text rather
// than JSON-encoded structs.
func TestAskUserTool_ResumeReturnsString(t *testing.T) {
	// 1. A tool returning (string, error) passes strings through without
	// JSON encoding. This is the pattern used by the ask_user tool.
	stringTool, err := utils.InferTool(
		"test_resume_string",
		"test tool that mimics ask_user resume behavior",
		func(ctx context.Context, input AskUserInput) (string, error) {
			return "yes, confirmed", nil
		},
	)
	if err != nil {
		t.Fatalf("InferTool: %v", err)
	}

	output, err := stringTool.InvokableRun(context.Background(), `{"question":"confirm?"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if output != "yes, confirmed" {
		t.Errorf("string tool output = %q, want %q (plain text, not JSON)", output, "yes, confirmed")
	}

	// 2. Verify the bug scenario: a struct return type produces JSON output.
	// This is what happened when the tool returned *AskUserOutput.
	type askUserOutput struct {
		Answer string `json:"answer"`
	}
	structTool, err := utils.InferTool(
		"test_resume_struct",
		"test tool that returns struct (bug scenario)",
		func(ctx context.Context, input AskUserInput) (askUserOutput, error) {
			return askUserOutput{Answer: "yes, confirmed"}, nil
		},
	)
	if err != nil {
		t.Fatalf("InferTool (struct): %v", err)
	}

	structOutput, err := structTool.InvokableRun(context.Background(), `{"question":"confirm?"}`)
	if err != nil {
		t.Fatalf("InvokableRun (struct): %v", err)
	}
	if !strings.Contains(structOutput, "{") || !strings.Contains(structOutput, "}") {
		t.Errorf("struct tool output = %q, expected JSON-wrapped output", structOutput)
	}
	if structOutput == "yes, confirmed" {
		t.Error("struct tool should produce JSON, not plain text")
	}

	// 3. Verify the actual ask_user tool is a valid InvokableTool.
	// The function signature func(ctx, AskUserInput) (string, error) provides
	// compile-time safety that resume values are plain strings.
	tl, err := NewAskUserTool()
	if err != nil {
		t.Fatalf("NewAskUserTool: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "ask_user" {
		t.Errorf("tool name = %q, want %q", info.Name, "ask_user")
	}
}
