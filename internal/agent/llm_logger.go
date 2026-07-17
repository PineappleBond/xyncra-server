package agent

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ---------------------------------------------------------------------------
// LLM Logger — structured JSONL logger for LLM observability
// ---------------------------------------------------------------------------

// LLMLogger writes structured JSON log records to an io.Writer in JSONL
// format (one JSON object per line). It is safe for concurrent use; all
// writes are serialized through an internal mutex.
type LLMLogger struct {
	mu     sync.Mutex
	w      io.Writer
	enc    *json.Encoder
	indent bool
}

// NewLLMLogger creates an LLMLogger that writes to w. When indent is true
// each record is pretty-printed; production deployments should pass false
// to keep one record per line.
func NewLLMLogger(w io.Writer, indent bool) *LLMLogger {
	enc := json.NewEncoder(w)
	if indent {
		enc.SetIndent("", "  ")
	}
	return &LLMLogger{w: w, enc: enc, indent: indent}
}

// write serializes a LogRecord as a single JSON line.
func (l *LLMLogger) write(rec LogRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(rec)
}

// ---------------------------------------------------------------------------
// Snapshot types (JSON-serializable representations of Eino types)
// ---------------------------------------------------------------------------

// LogRecord is a single JSONL record describing one phase of LLM interaction.
type LogRecord struct {
	Timestamp  time.Time         `json:"timestamp"`
	AgentID    string            `json:"agent_id"`
	Model      string            `json:"model"`
	Iteration  int               `json:"iteration"`
	Phase      string            `json:"phase"` // "request" | "response" | "tool_call" | "tool_result" | "agent_start" | "agent_end" | "error"
	Messages   []MessageSnapshot `json:"messages,omitempty"`
	Tools      []ToolSnapshot    `json:"tools,omitempty"`
	Output     *MessageSnapshot  `json:"output,omitempty"`
	TokenUsage *TokenSnapshot    `json:"token_usage,omitempty"`
	DurationMs int64             `json:"duration_ms,omitempty"`
	ToolName   string            `json:"tool_name,omitempty"`
	ToolArgs   string            `json:"tool_args,omitempty"`
	ToolResult string            `json:"tool_result,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// MessageSnapshot is a trimmed representation of a schema.Message.
type MessageSnapshot struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	ToolCalls []ToolCallSnapshot `json:"tool_calls,omitempty"`
}

// ToolCallSnapshot captures one tool call from an assistant message.
type ToolCallSnapshot struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

// ToolSnapshot is a trimmed representation of a schema.ToolInfo.
type ToolSnapshot struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// TokenSnapshot captures token usage from a model response.
type TokenSnapshot struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// convertMessage converts a schema.Message to a MessageSnapshot, truncating
// long content to keep log records manageable.
func convertMessage(msg *schema.Message) MessageSnapshot {
	if msg == nil {
		return MessageSnapshot{}
	}
	snap := MessageSnapshot{
		Role:    string(msg.Role),
		Content: truncate(msg.Content, 4096),
	}
	for _, tc := range msg.ToolCalls {
		snap.ToolCalls = append(snap.ToolCalls, ToolCallSnapshot{
			Name: tc.Function.Name,
			Args: truncate(tc.Function.Arguments, 2048),
		})
	}
	return snap
}

// convertToolInfo converts a schema.ToolInfo to a ToolSnapshot.
func convertToolInfo(ti *schema.ToolInfo) ToolSnapshot {
	if ti == nil {
		return ToolSnapshot{}
	}
	return ToolSnapshot{
		Name: ti.Name,
		Desc: ti.Desc,
	}
}

// truncate shortens s to at most maxLen characters. When truncation occurs
// the suffix "…[truncated]" is appended so the reader knows the value was
// cut.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 16 {
		return s[:maxLen]
	}
	return s[:maxLen-14] + "...[truncated]"
}

// ---------------------------------------------------------------------------
// LoggingMiddleware — Eino middleware that records LLM interactions
// ---------------------------------------------------------------------------

// LoggingMiddleware is an Eino ChatModelAgentMiddleware that logs every
// model request/response, tool call, and agent lifecycle event to an
// LLMLogger. It implements adk.TypedChatModelAgentMiddleware[*schema.Message]
// by embedding *adk.BaseChatModelAgentMiddleware and overriding only the
// hooks it needs.
type LoggingMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	logger    *LLMLogger
	agentID   string
	model     string
	iteration int32 // accessed atomically

	// modelCallStart records when the current model call began, used to
	// compute DurationMs in the "response" record.
	modelCallStart atomic.Value // stores time.Time
}

// NewLoggingMiddleware creates a LoggingMiddleware that writes records to
// logger for the given agentID and model name.
func NewLoggingMiddleware(logger *LLMLogger, agentID, model string) *LoggingMiddleware {
	return &LoggingMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		logger:                       logger,
		agentID:                      agentID,
		model:                        model,
	}
}

// BeforeAgent logs the "agent_start" phase.
func (m *LoggingMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Model:     m.model,
		Iteration: int(atomic.LoadInt32(&m.iteration)),
		Phase:     "agent_start",
	})
	return ctx, runCtx, nil
}

// AfterAgent logs the "agent_end" phase with a summary of the final state.
func (m *LoggingMiddleware) AfterAgent(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, error) {
	var lastMsg *MessageSnapshot
	if n := len(state.Messages); n > 0 {
		snap := convertMessage(state.Messages[n-1])
		lastMsg = &snap
	}
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Model:     m.model,
		Iteration: int(atomic.LoadInt32(&m.iteration)),
		Phase:     "agent_end",
		Output:    lastMsg,
	})
	return ctx, nil
}

// BeforeModelRewriteState logs the "request" phase before each model
// invocation. It captures the full message list and tool definitions that
// will be sent to the model.
func (m *LoggingMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	iter := int(atomic.AddInt32(&m.iteration, 1))

	msgs := make([]MessageSnapshot, 0, len(state.Messages))
	for _, msg := range state.Messages {
		msgs = append(msgs, convertMessage(msg))
	}
	tools := make([]ToolSnapshot, 0, len(state.ToolInfos))
	for _, ti := range state.ToolInfos {
		tools = append(tools, convertToolInfo(ti))
	}

	m.modelCallStart.Store(time.Now())

	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Model:     m.model,
		Iteration: iter,
		Phase:     "request",
		Messages:  msgs,
		Tools:     tools,
	})
	return ctx, state, nil
}

// AfterModelRewriteState logs the "response" phase after each model
// invocation. It captures the model's output (last message) and any token
// usage information.
func (m *LoggingMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	rec := LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Model:     m.model,
		Iteration: int(atomic.LoadInt32(&m.iteration)),
		Phase:     "response",
	}

	if n := len(state.Messages); n > 0 {
		last := state.Messages[n-1]
		snap := convertMessage(last)
		rec.Output = &snap

		// Extract token usage from the model response metadata.
		if last.ResponseMeta != nil && last.ResponseMeta.Usage != nil {
			u := last.ResponseMeta.Usage
			rec.TokenUsage = &TokenSnapshot{
				InputTokens:  u.PromptTokens,
				OutputTokens: u.CompletionTokens,
				TotalTokens:  u.TotalTokens,
			}
		}
	}

	// Compute duration from the stored start time.
	if startVal := m.modelCallStart.Load(); startVal != nil {
		if start, ok := startVal.(time.Time); ok {
			rec.DurationMs = time.Since(start).Milliseconds()
		}
	}

	m.logger.write(rec)
	return ctx, state, nil
}

// WrapInvokableToolCall wraps tool execution to log "tool_call" before
// invocation and "tool_result" after completion.
func (m *LoggingMiddleware) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		iter := int(atomic.LoadInt32(&m.iteration))

		m.logger.write(LogRecord{
			Timestamp: time.Now(),
			AgentID:   m.agentID,
			Model:     m.model,
			Iteration: iter,
			Phase:     "tool_call",
			ToolName:  tCtx.Name,
			ToolArgs:  truncate(argumentsInJSON, 2048),
		})

		start := time.Now()
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		dur := time.Since(start).Milliseconds()

		resultRec := LogRecord{
			Timestamp:  time.Now(),
			AgentID:    m.agentID,
			Model:      m.model,
			Iteration:  iter,
			Phase:      "tool_result",
			ToolName:   tCtx.Name,
			ToolResult: truncate(result, 4096),
			DurationMs: dur,
		}
		if err != nil {
			resultRec.Error = err.Error()
		}
		m.logger.write(resultRec)

		return result, err
	}, nil
}
