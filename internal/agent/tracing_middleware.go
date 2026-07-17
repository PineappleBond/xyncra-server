package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

// TracingMiddleware records LLM call details as OpenTelemetry spans.
// It mirrors the structure of LoggingMiddleware but writes spans instead of
// JSONL logs. Each model invocation produces an "agent.llm.call" span with
// model name, iteration, token usage, and duration. Each tool call produces
// an "agent.tool.call" span with tool name, duration, and error status.
//
// Thread safety: Not safe for concurrent use. The Eino ADK serializes model
// calls per runner, so BeforeModelRewriteState/AfterModelRewriteState are
// always called in pairs without interleaving.
//
// TracingMiddleware is only appended to the middleware chain when tracing is
// enabled (see AgentBuilder.SetTracingEnabled). When the global tracer
// provider is a no-op, this middleware has zero overhead because it is not
// added to the chain at all.
type TracingMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	tracer    trace.Tracer
	agentID   string
	model     string
	iteration int32 // accessed atomically

	// modelCallStart records when the current model call began, used to
	// compute duration_ms in the span attributes.
	modelCallStart atomic.Value // stores time.Time

	// llmSpan holds the active agent.llm.call span for the current iteration.
	llmSpan trace.Span

	// debugUsers/debugDevices: when non-empty, full LLM content is recorded
	// as span events for matching callers (OR logic).
	debugUsers   map[string]bool
	debugDevices map[string]bool
	hasDebug     bool
	debugMatched bool // set per-iteration in BeforeModelRewriteState
}

// NewTracingMiddleware creates a TracingMiddleware for the given agent.
// It uses the global OpenTelemetry tracer provider (otel.Tracer).
func NewTracingMiddleware(agentID, model string) *TracingMiddleware {
	return &TracingMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		tracer:                       otel.Tracer("xyncra-server"),
		agentID:                      agentID,
		model:                        model,
	}
}

// SetDebugFilter configures per-user/device debug content capture.
// When the caller matches (OR logic), full request/response content is
// recorded as span events on the agent.llm.call span.
func (m *TracingMiddleware) SetDebugFilter(users, devices []string) {
	m.debugUsers = toSet(users)
	m.debugDevices = toSet(devices)
	m.hasDebug = len(m.debugUsers) > 0 || len(m.debugDevices) > 0
}

// BeforeModelRewriteState starts an "agent.llm.call" span before each model
// invocation. The span records the model name and iteration number.
func (m *TracingMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	iter := int(atomic.AddInt32(&m.iteration, 1))

	m.modelCallStart.Store(time.Now())

	ctx, span := m.tracer.Start(ctx, tracing.SpanAgentLLMCall,
		trace.WithAttributes(
			attribute.String(tracing.AttrAgentID, m.agentID),
			attribute.String(tracing.AttrModel, m.model),
			attribute.Int(tracing.AttrIteration, iter),
		),
	)
	m.llmSpan = span

	// Debug: record full request content as span event
	m.debugMatched = m.isDebugCaller(ctx)
	if m.debugMatched && len(state.Messages) > 0 {
		reqPayload := m.serializeMessages(state.Messages)
		span.AddEvent("llm.debug.request", trace.WithAttributes(
			attribute.String("llm.request.messages", reqPayload),
		))
	}

	return ctx, state, nil
}

// AfterModelRewriteState ends the active "agent.llm.call" span, recording
// token usage (if available) and duration in milliseconds.
func (m *TracingMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if m.llmSpan == nil {
		return ctx, state, nil
	}

	// Calculate duration.
	var durationMs int64
	if start, ok := m.modelCallStart.Load().(time.Time); ok {
		durationMs = time.Since(start).Milliseconds()
	}

	// Extract token usage from the last message's response metadata.
	var inputTokens, outputTokens, totalTokens int
	if n := len(state.Messages); n > 0 {
		last := state.Messages[n-1]
		if last.ResponseMeta != nil && last.ResponseMeta.Usage != nil {
			u := last.ResponseMeta.Usage
			inputTokens = u.PromptTokens
			outputTokens = u.CompletionTokens
			totalTokens = u.TotalTokens
		}
	}

	m.llmSpan.SetAttributes(
		attribute.Int(tracing.AttrInputTokens, inputTokens),
		attribute.Int(tracing.AttrOutputTokens, outputTokens),
		attribute.Int(tracing.AttrTotalTokens, totalTokens),
		attribute.Int64(tracing.AttrDurationMs, durationMs),
	)

	// Debug: record full response content as span event
	if m.debugMatched && len(state.Messages) > 0 {
		last := state.Messages[len(state.Messages)-1]
		respPayload := m.serializeMessage(last)
		m.llmSpan.AddEvent("llm.debug.response", trace.WithAttributes(
			attribute.String("llm.response.message", respPayload),
		))
	}

	m.llmSpan.End()
	m.llmSpan = nil

	return ctx, state, nil
}

// WrapInvokableToolCall wraps tool execution with an "agent.tool.call" span
// that records the tool name, duration, and any error. When debug content
// capture is active, it also records tool input (argumentsInJSON) and output
// (result) as span events for debugging purposes.
func (m *TracingMiddleware) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		_, span := m.tracer.Start(ctx, tracing.SpanAgentToolCall,
			trace.WithAttributes(
				attribute.String(tracing.AttrAgentID, m.agentID),
				attribute.String(tracing.AttrToolName, tCtx.Name),
			),
		)

		// Debug: record tool input as span event
		if m.debugMatched {
			span.AddEvent("tool.debug.input", trace.WithAttributes(
				attribute.String("tool.arguments", argumentsInJSON),
			))
		}

		start := time.Now()
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		durationMs := time.Since(start).Milliseconds()

		span.SetAttributes(
			attribute.Int64(tracing.AttrDurationMs, durationMs),
		)

		// Debug: record tool output as span event
		if m.debugMatched {
			span.AddEvent("tool.debug.output", trace.WithAttributes(
				attribute.String("tool.result", result),
			))
		}

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()

		return result, err
	}, nil
}

// isDebugCaller checks if the context's CallerDevice matches the debug filter.
func (m *TracingMiddleware) isDebugCaller(ctx context.Context) bool {
	if !m.hasDebug {
		return false
	}
	d, ok := CallerDeviceFromContext(ctx)
	if !ok {
		return false
	}
	return m.debugUsers[d.UserID] || m.debugDevices[d.DeviceID]
}

// serializeMessages converts messages to a JSON string for debug span events.
// It captures Role, Content, ToolCalls (for assistant messages), ToolCallID/ToolName
// (for tool messages), and ReasoningContent (thinking process).
func (m *TracingMiddleware) serializeMessages(msgs []*schema.Message) string {
	type toolCallJSON struct {
		Name string `json:"name"`
		Args string `json:"args"`
	}
	type msgJSON struct {
		Role             string         `json:"role"`
		Content          string         `json:"content"`
		ToolCalls        []toolCallJSON `json:"tool_calls,omitempty"`
		ToolCallID       string         `json:"tool_call_id,omitempty"`
		ToolName         string         `json:"tool_name,omitempty"`
		ReasoningContent string         `json:"reasoning_content,omitempty"`
	}
	out := make([]msgJSON, 0, len(msgs))
	for _, msg := range msgs {
		mj := msgJSON{
			Role:             string(msg.Role),
			Content:          msg.Content,
			ToolCallID:       msg.ToolCallID,
			ToolName:         msg.ToolName,
			ReasoningContent: msg.ReasoningContent,
		}
		for _, tc := range msg.ToolCalls {
			mj.ToolCalls = append(mj.ToolCalls, toolCallJSON{
				Name: tc.Function.Name,
				Args: tc.Function.Arguments,
			})
		}
		out = append(out, mj)
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// serializeMessage converts a single message to a JSON string.
// It captures Role, Content, ToolCalls (for assistant messages), ToolCallID/ToolName
// (for tool messages), and ReasoningContent (thinking process).
func (m *TracingMiddleware) serializeMessage(msg *schema.Message) string {
	type toolCallJSON struct {
		Name string `json:"name"`
		Args string `json:"args"`
	}
	type msgJSON struct {
		Role             string         `json:"role"`
		Content          string         `json:"content"`
		ToolCalls        []toolCallJSON `json:"tool_calls,omitempty"`
		ToolCallID       string         `json:"tool_call_id,omitempty"`
		ToolName         string         `json:"tool_name,omitempty"`
		ReasoningContent string         `json:"reasoning_content,omitempty"`
	}
	mj := msgJSON{
		Role:             string(msg.Role),
		Content:          msg.Content,
		ToolCallID:       msg.ToolCallID,
		ToolName:         msg.ToolName,
		ReasoningContent: msg.ReasoningContent,
	}
	for _, tc := range msg.ToolCalls {
		mj.ToolCalls = append(mj.ToolCalls, toolCallJSON{
			Name: tc.Function.Name,
			Args: tc.Function.Arguments,
		})
	}
	data, _ := json.Marshal(mj)
	return string(data)
}

// toSet converts a string slice to a lookup map.
func toSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}
