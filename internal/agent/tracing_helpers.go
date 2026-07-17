package agent

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

// agentTracer is the package-level tracer for all agent-layer spans.
var agentTracer = otel.Tracer("xyncra-server")

// startAgentExecuteSpan creates an "agent.execute" span covering the full
// AgentExecutor.Execute pipeline. The returned finish function must be called
// (typically via defer) to end the span and record any error.
func startAgentExecuteSpan(ctx context.Context, agentID, conversationID, userID string) (context.Context, func(error)) {
	ctx, span := agentTracer.Start(ctx, tracing.SpanAgentExecute,
		trace.WithAttributes(
			attribute.String(tracing.AttrAgentID, agentID),
			attribute.String(tracing.AttrConversationID, conversationID),
			attribute.String(tracing.AttrUserID, userID),
		),
	)
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()
	}
}

// startAgentBuildSpan creates an "agent.build" span covering LLM client
// construction, tool creation, and runner assembly.
func startAgentBuildSpan(ctx context.Context, agentID string) (context.Context, func(error)) {
	ctx, span := agentTracer.Start(ctx, tracing.SpanAgentBuild,
		trace.WithAttributes(
			attribute.String(tracing.AttrAgentID, agentID),
		),
	)
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()
	}
}

// startAgentRunSpan creates an "agent.run" span covering the ADK Runner.Run
// invocation including LLM inference and tool execution.
func startAgentRunSpan(ctx context.Context, agentID string) (context.Context, func(error)) {
	ctx, span := agentTracer.Start(ctx, tracing.SpanAgentRun,
		trace.WithAttributes(
			attribute.String(tracing.AttrAgentID, agentID),
		),
	)
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()
	}
}

// startAgentStreamSpan creates an "agent.stream" span covering the chunk
// consumption loop. The caller should update chunk_count and total_chars
// attributes after the loop completes using the span from the returned context.
func startAgentStreamSpan(ctx context.Context) (context.Context, func(error)) {
	ctx, span := agentTracer.Start(ctx, tracing.SpanAgentStream)
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()
	}
}

// startAgentCheckpointSaveSpan creates an "agent.checkpoint.save" span covering
// the HITL checkpoint persistence path.
func startAgentCheckpointSaveSpan(ctx context.Context, checkpointID string) (context.Context, func(error)) {
	ctx, span := agentTracer.Start(ctx, tracing.SpanAgentCheckpointSave,
		trace.WithAttributes(
			attribute.String(tracing.AttrCheckpointID, checkpointID),
		),
	)
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()
	}
}
