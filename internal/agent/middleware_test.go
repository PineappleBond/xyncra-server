package agent

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// TestBuildMiddleware_NoMiddlewareEnabled
// ---------------------------------------------------------------------------

func TestBuildMiddleware_NoMiddlewareEnabled(t *testing.T) {
	builder := &AgentBuilder{}
	config := &AgentConfig{
		ID: "test-agent",
		Middleware: MiddlewareConfig{
			EnablePatchToolCalls: false,
			EnableSummarization:  false,
			EnableToolReduction:  false,
		},
	}

	mws := builder.buildMiddleware(context.Background(), config, &mockChatModel{})
	if len(mws) != 0 {
		t.Errorf("expected 0 middleware, got %d", len(mws))
	}
}

// ---------------------------------------------------------------------------
// TestBuildMiddleware_PatchToolCallsOnly
// ---------------------------------------------------------------------------

func TestBuildMiddleware_PatchToolCallsOnly(t *testing.T) {
	builder := &AgentBuilder{}
	config := &AgentConfig{
		ID: "test-agent",
		Middleware: MiddlewareConfig{
			EnablePatchToolCalls: true,
		},
	}

	mws := builder.buildMiddleware(context.Background(), config, &mockChatModel{})
	if len(mws) != 1 {
		t.Errorf("expected 1 middleware, got %d", len(mws))
	}
}

// ---------------------------------------------------------------------------
// TestBuildMiddleware_AllEnabled
// ---------------------------------------------------------------------------

func TestBuildMiddleware_AllEnabled(t *testing.T) {
	builder := &AgentBuilder{}
	config := &AgentConfig{
		ID: "test-agent",
		Middleware: MiddlewareConfig{
			EnablePatchToolCalls:  true,
			EnableSummarization:   true,
			SummarizationTokens:   100000,
			EnableToolReduction:   true,
			ToolReductionMaxChars: 10000,
		},
	}

	mws := builder.buildMiddleware(context.Background(), config, &mockChatModel{})
	if len(mws) != 3 {
		t.Errorf("expected 3 middleware, got %d", len(mws))
	}
}

// ---------------------------------------------------------------------------
// TestBuildMiddleware_DefaultTokens
// ---------------------------------------------------------------------------

func TestBuildMiddleware_DefaultTokens(t *testing.T) {
	builder := &AgentBuilder{}
	config := &AgentConfig{
		ID: "test-agent",
		Middleware: MiddlewareConfig{
			EnableSummarization: true,
			// SummarizationTokens = 0 → should default to 160000
		},
	}

	// Just verify it doesn't panic and returns 1 middleware.
	mws := builder.buildMiddleware(context.Background(), config, &mockChatModel{})
	if len(mws) != 1 {
		t.Errorf("expected 1 middleware (summarization), got %d", len(mws))
	}
}

// ---------------------------------------------------------------------------
// TestBuildMiddleware_DefaultMaxChars
// ---------------------------------------------------------------------------

func TestBuildMiddleware_DefaultMaxChars(t *testing.T) {
	builder := &AgentBuilder{}
	config := &AgentConfig{
		ID: "test-agent",
		Middleware: MiddlewareConfig{
			EnableToolReduction: true,
			// ToolReductionMaxChars = 0 → should default to 50000
		},
	}

	mws := builder.buildMiddleware(context.Background(), config, &mockChatModel{})
	if len(mws) != 1 {
		t.Errorf("expected 1 middleware (tool reduction), got %d", len(mws))
	}
}

// ---------------------------------------------------------------------------
// TestBuildMiddleware_DynamicToolProvider
// ---------------------------------------------------------------------------

func TestBuildMiddleware_DynamicToolProvider(t *testing.T) {
	// When EnableClientTools=true and provider is set → DynamicToolProvider registered.
	builder := &AgentBuilder{
		clientFunctionProvider: &mockFunctionProvider{},
	}
	config := &AgentConfig{
		ID: "test-agent",
		Middleware: MiddlewareConfig{
			EnableClientTools: true,
		},
	}

	mws := builder.buildMiddleware(context.Background(), config, &mockChatModel{})
	if len(mws) != 1 {
		t.Errorf("expected 1 middleware (DynamicToolProvider), got %d", len(mws))
	}
}

func TestBuildMiddleware_DynamicToolProvider_NilProviderSkipped(t *testing.T) {
	// When EnableClientTools=true but FunctionProvider not set → skipped.
	builder := &AgentBuilder{
		clientFunctionProvider: nil,
	}
	config := &AgentConfig{
		ID: "test-agent",
		Middleware: MiddlewareConfig{
			EnableClientTools: true,
		},
	}

	mws := builder.buildMiddleware(context.Background(), config, &mockChatModel{})
	if len(mws) != 0 {
		t.Errorf("expected 0 middleware (provider nil → skipped), got %d", len(mws))
	}
}
