package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

func TestNewTracingMiddleware(t *testing.T) {
	m := NewTracingMiddleware("agent-1", "gpt-4")
	require.NotNil(t, m)
	assert.Equal(t, "agent-1", m.agentID)
	assert.Equal(t, "gpt-4", m.model)
	assert.NotNil(t, m.tracer)
}

func TestTracingMiddleware_BeforeModelRewriteState(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-1", "claude-3")

	ctx := context.Background()
	state := &adk.ChatModelAgentState{}

	outCtx, outState, err := m.BeforeModelRewriteState(ctx, state, nil)
	require.NoError(t, err)
	assert.NotNil(t, outCtx)
	assert.NotNil(t, outState)
	assert.NotNil(t, m.llmSpan, "llmSpan should be set after BeforeModel")

	// Span is still active, not yet exported.
	spans := spansSince(startIdx)
	assert.Len(t, spans, 0, "span is still active, not yet exported")

	// End the span via AfterModel.
	_, _, err = m.AfterModelRewriteState(outCtx, outState, nil)
	require.NoError(t, err)
	assert.Nil(t, m.llmSpan, "llmSpan should be nil after AfterModel")

	spans = spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanAgentLLMCall, spans[0].Name())
}

func TestTracingMiddleware_AfterModelRewriteState_WithTokens(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-2", "gpt-4")

	ctx := context.Background()
	state := &adk.ChatModelAgentState{}

	outCtx, _, err := m.BeforeModelRewriteState(ctx, state, nil)
	require.NoError(t, err)

	// Simulate a model response with token usage.
	stateWithMsg := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			{
				Role: schema.Assistant,
				ResponseMeta: &schema.ResponseMeta{
					Usage: &schema.TokenUsage{
						PromptTokens:     100,
						CompletionTokens: 50,
						TotalTokens:      150,
					},
				},
			},
		},
	}

	_, _, err = m.AfterModelRewriteState(outCtx, stateWithMsg, nil)
	require.NoError(t, err)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanAgentLLMCall, spans[0].Name())

	// Check token attributes exist.
	attrs := roAttrMap(spans[0])
	assert.Contains(t, attrs, tracing.AttrInputTokens)
	assert.Contains(t, attrs, tracing.AttrOutputTokens)
	assert.Contains(t, attrs, tracing.AttrTotalTokens)
	assert.Contains(t, attrs, tracing.AttrDurationMs)
	assert.Equal(t, "100", attrs[tracing.AttrInputTokens])
	assert.Equal(t, "50", attrs[tracing.AttrOutputTokens])
	assert.Equal(t, "150", attrs[tracing.AttrTotalTokens])
}

func TestTracingMiddleware_AfterModelRewriteState_NoSpan(t *testing.T) {
	m := NewTracingMiddleware("agent-3", "gpt-4")

	// Call AfterModel without BeforeModel -- llmSpan is nil.
	ctx := context.Background()
	state := &adk.ChatModelAgentState{}

	// Should not panic and return nil error.
	_, _, err := m.AfterModelRewriteState(ctx, state, nil)
	assert.NoError(t, err)
}

func TestTracingMiddleware_WrapInvokableToolCall(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-4", "gpt-4")

	// Create a mock endpoint.
	mockEndpoint := func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		return "result", nil
	}

	tCtx := &adk.ToolContext{
		Name:   "test-tool",
		CallID: "call-123",
	}

	wrapped, err := m.WrapInvokableToolCall(context.Background(), mockEndpoint, tCtx)
	require.NoError(t, err)
	require.NotNil(t, wrapped)

	// Call the wrapped endpoint.
	result, err := wrapped(context.Background(), `{"key":"val"}`)
	assert.NoError(t, err)
	assert.Equal(t, "result", result)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, tracing.SpanAgentToolCall, spans[0].Name())

	// Verify tool name attribute.
	attrs := roAttrMap(spans[0])
	assert.Equal(t, "test-tool", attrs[tracing.AttrToolName])
}

func TestTracingMiddleware_WrapInvokableToolCall_WithError(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-5", "gpt-4")

	mockEndpoint := func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		return "", fmt.Errorf("tool failed")
	}

	tCtx := &adk.ToolContext{Name: "failing-tool", CallID: "call-456"}

	wrapped, err := m.WrapInvokableToolCall(context.Background(), mockEndpoint, tCtx)
	require.NoError(t, err)

	_, callErr := wrapped(context.Background(), `{}`)
	assert.Error(t, callErr)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)
	// Verify the span has error status.
	assert.Equal(t, "tool failed", spans[0].Status().Description)
}

func TestTracingMiddleware_IterationIncrement(t *testing.T) {
	m := NewTracingMiddleware("agent-6", "gpt-4")

	ctx := context.Background()
	state := &adk.ChatModelAgentState{}

	for i := 0; i < 3; i++ {
		outCtx, outState, err := m.BeforeModelRewriteState(ctx, state, nil)
		require.NoError(t, err)
		_, _, err = m.AfterModelRewriteState(outCtx, outState, nil)
		require.NoError(t, err)
	}

	// iteration should be 3 after three model calls.
	assert.Equal(t, int32(3), m.iteration)
}
