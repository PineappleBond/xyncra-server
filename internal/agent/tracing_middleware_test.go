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

func TestTracingMiddleware_DebugFilter_UserMatch(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-debug-1", "gpt-4")
	m.SetDebugFilter([]string{"user-A"}, nil)

	state := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("hello"),
		},
	}

	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "user-A", DeviceID: "device-1"})
	outCtx, _, err := m.BeforeModelRewriteState(ctx, state, nil)
	require.NoError(t, err)

	// Simulate response
	respState := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("hello"),
			{
				Role:    schema.Assistant,
				Content: "hi there",
				ResponseMeta: &schema.ResponseMeta{
					Usage: &schema.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
				},
			},
		},
	}

	_, _, err = m.AfterModelRewriteState(outCtx, respState, nil)
	require.NoError(t, err)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)

	events := spans[0].Events()
	var hasRequest, hasResponse bool
	for _, e := range events {
		if e.Name == "llm.debug.request" {
			hasRequest = true
		}
		if e.Name == "llm.debug.response" {
			hasResponse = true
		}
	}
	assert.True(t, hasRequest, "expected llm.debug.request event for matching user")
	assert.True(t, hasResponse, "expected llm.debug.response event for matching user")
}

func TestTracingMiddleware_DebugFilter_DeviceMatch(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-debug-2", "gpt-4")
	m.SetDebugFilter(nil, []string{"device-X"})

	state := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("test"),
		},
	}

	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "user-B", DeviceID: "device-X"})
	outCtx, _, err := m.BeforeModelRewriteState(ctx, state, nil)
	require.NoError(t, err)

	respState := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("test"),
			{Role: schema.Assistant, Content: "reply"},
		},
	}

	_, _, err = m.AfterModelRewriteState(outCtx, respState, nil)
	require.NoError(t, err)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)

	events := spans[0].Events()
	var hasRequest, hasResponse bool
	for _, e := range events {
		if e.Name == "llm.debug.request" {
			hasRequest = true
		}
		if e.Name == "llm.debug.response" {
			hasResponse = true
		}
	}
	assert.True(t, hasRequest, "expected llm.debug.request event for matching device")
	assert.True(t, hasResponse, "expected llm.debug.response event for matching device")
}

func TestTracingMiddleware_DebugFilter_NoMatch(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-debug-3", "gpt-4")
	m.SetDebugFilter([]string{"user-A"}, []string{"device-A"})

	state := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("test"),
		},
	}

	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "user-B", DeviceID: "device-B"})
	outCtx, _, err := m.BeforeModelRewriteState(ctx, state, nil)
	require.NoError(t, err)

	respState := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("test"),
			{Role: schema.Assistant, Content: "reply"},
		},
	}

	_, _, err = m.AfterModelRewriteState(outCtx, respState, nil)
	require.NoError(t, err)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)

	for _, e := range spans[0].Events() {
		assert.NotEqual(t, "llm.debug.request", e.Name)
		assert.NotEqual(t, "llm.debug.response", e.Name)
	}
}

func TestTracingMiddleware_DebugFilter_NoFilter(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-debug-4", "gpt-4")
	// No SetDebugFilter called

	state := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("test"),
		},
	}

	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "user-A", DeviceID: "device-A"})
	outCtx, outState, err := m.BeforeModelRewriteState(ctx, state, nil)
	require.NoError(t, err)

	_, _, err = m.AfterModelRewriteState(outCtx, outState, nil)
	require.NoError(t, err)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)

	for _, e := range spans[0].Events() {
		assert.NotEqual(t, "llm.debug.request", e.Name)
		assert.NotEqual(t, "llm.debug.response", e.Name)
	}
}

func TestTracingMiddleware_DebugFilter_NoCallerDevice(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-debug-5", "gpt-4")
	m.SetDebugFilter([]string{"user-A"}, nil)

	state := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("test"),
		},
	}

	// No CallerDevice in context
	outCtx, outState, err := m.BeforeModelRewriteState(context.Background(), state, nil)
	require.NoError(t, err)

	_, _, err = m.AfterModelRewriteState(outCtx, outState, nil)
	require.NoError(t, err)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)

	for _, e := range spans[0].Events() {
		assert.NotEqual(t, "llm.debug.request", e.Name)
		assert.NotEqual(t, "llm.debug.response", e.Name)
	}
}

func TestTracingMiddleware_SerializeMessages_IncludesToolCalls(t *testing.T) {
	m := NewTracingMiddleware("agent-ser-1", "gpt-4")

	msgs := []*schema.Message{
		schema.UserMessage("What's the weather?"),
		{
			Role:    schema.Assistant,
			Content: "",
			ToolCalls: []schema.ToolCall{
				{
					ID: "call-1",
					Function: schema.FunctionCall{
						Name:      "get_weather",
						Arguments: `{"city":"Beijing"}`,
					},
				},
			},
		},
		{
			Role:       schema.Tool,
			Content:    `{"temp":"27°C"}`,
			ToolCallID: "call-1",
			ToolName:   "get_weather",
		},
	}

	result := m.serializeMessages(msgs)

	// Verify tool_calls are included for assistant message
	assert.Contains(t, result, `"tool_calls"`)
	assert.Contains(t, result, `"get_weather"`)
	assert.Contains(t, result, "Beijing")

	// Verify tool message fields are included
	assert.Contains(t, result, `"tool_call_id"`)
	assert.Contains(t, result, `"call-1"`)
	assert.Contains(t, result, `"tool_name"`)

	// Verify the assistant message content is empty but tool_calls present
	assert.Contains(t, result, `"content":""`)
}

func TestTracingMiddleware_SerializeMessages_IncludesReasoningContent(t *testing.T) {
	m := NewTracingMiddleware("agent-ser-2", "gpt-4")

	msgs := []*schema.Message{
		schema.UserMessage("Solve this math problem"),
		{
			Role:             schema.Assistant,
			Content:          "The answer is 42.",
			ReasoningContent: "Let me think step by step. First, I need to...",
		},
	}

	result := m.serializeMessages(msgs)

	// Verify reasoning_content is included
	assert.Contains(t, result, `"reasoning_content"`)
	assert.Contains(t, result, "Let me think step by step")
}

func TestTracingMiddleware_SerializeMessage_IncludesToolCalls(t *testing.T) {
	m := NewTracingMiddleware("agent-ser-3", "gpt-4")

	msg := &schema.Message{
		Role:    schema.Assistant,
		Content: "",
		ToolCalls: []schema.ToolCall{
			{
				ID: "call-abc",
				Function: schema.FunctionCall{
					Name:      "search",
					Arguments: `{"query":"test"}`,
				},
			},
		},
		ReasoningContent: "I should search for this",
	}

	result := m.serializeMessage(msg)

	assert.Contains(t, result, `"tool_calls"`)
	assert.Contains(t, result, `"search"`)
	assert.Contains(t, result, `"reasoning_content"`)
	assert.Contains(t, result, "I should search for this")
}

func TestTracingMiddleware_WrapInvokableToolCall_DebugEvents(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-tool-debug", "gpt-4")
	m.SetDebugFilter([]string{"user-debug"}, nil)

	mockEndpoint := func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		return `{"result":"sunny"}`, nil
	}

	tCtx := &adk.ToolContext{Name: "get_weather", CallID: "call-xyz"}

	wrapped, err := m.WrapInvokableToolCall(
		ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "user-debug", DeviceID: "dev-1"}),
		mockEndpoint, tCtx,
	)
	require.NoError(t, err)

	// Set debugMatched (normally set in BeforeModelRewriteState)
	m.debugMatched = true

	result, err := wrapped(context.Background(), `{"city":"Shanghai"}`)
	require.NoError(t, err)
	assert.Equal(t, `{"result":"sunny"}`, result)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)

	// Verify tool.debug.input and tool.debug.output events exist
	events := spans[0].Events()
	var hasInput, hasOutput bool
	for _, e := range events {
		if e.Name == "tool.debug.input" {
			hasInput = true
			// Check the input contains the arguments
			for _, attr := range e.Attributes {
				if attr.Key == "tool.arguments" {
					assert.Contains(t, attr.Value.AsString(), "Shanghai")
				}
			}
		}
		if e.Name == "tool.debug.output" {
			hasOutput = true
			for _, attr := range e.Attributes {
				if attr.Key == "tool.result" {
					assert.Contains(t, attr.Value.AsString(), "sunny")
				}
			}
		}
	}
	assert.True(t, hasInput, "expected tool.debug.input event for debug-matched user")
	assert.True(t, hasOutput, "expected tool.debug.output event for debug-matched user")
}

func TestTracingMiddleware_WrapInvokableToolCall_NoDebugEvents_WhenNotMatched(t *testing.T) {
	startIdx := len(recorder.Ended())
	m := NewTracingMiddleware("agent-tool-no-debug", "gpt-4")
	m.SetDebugFilter([]string{"user-other"}, nil)

	mockEndpoint := func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		return "ok", nil
	}

	tCtx := &adk.ToolContext{Name: "test-tool", CallID: "call-999"}

	wrapped, err := m.WrapInvokableToolCall(
		ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "user-debug", DeviceID: "dev-1"}),
		mockEndpoint, tCtx,
	)
	require.NoError(t, err)

	// debugMatched is false (user doesn't match filter)
	m.debugMatched = false

	_, err = wrapped(context.Background(), `{"key":"val"}`)
	require.NoError(t, err)

	spans := spansSince(startIdx)
	require.Len(t, spans, 1)

	// Verify NO tool.debug.input/output events
	for _, e := range spans[0].Events() {
		assert.NotEqual(t, "tool.debug.input", e.Name)
		assert.NotEqual(t, "tool.debug.output", e.Name)
	}
}
