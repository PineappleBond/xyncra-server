package agent

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"

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
	m.llmSpan.End()
	m.llmSpan = nil

	return ctx, state, nil
}

// WrapInvokableToolCall wraps tool execution with an "agent.tool.call" span
// that records the tool name, duration, and any error.
func (m *TracingMiddleware) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		_, span := m.tracer.Start(ctx, tracing.SpanAgentToolCall,
			trace.WithAttributes(
				attribute.String(tracing.AttrAgentID, m.agentID),
				attribute.String(tracing.AttrToolName, tCtx.Name),
			),
		)

		start := time.Now()
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		durationMs := time.Since(start).Milliseconds()

		span.SetAttributes(
			attribute.Int64(tracing.AttrDurationMs, durationMs),
		)

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()

		return result, err
	}, nil
}
