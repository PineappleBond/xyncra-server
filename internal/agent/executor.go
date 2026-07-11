package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cloudwego/eino/adk"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// Logger is a structured logger interface compatible with server.Logger.
// Implementations should output key-value pairs from the args slice.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
	Debug(msg string, args ...any)
}

// noopLogger discards all log messages.
type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
func (noopLogger) Debug(string, ...any) {}

// ExecutePayload contains the parameters for a single agent execution.
type ExecutePayload struct {
	MessageID      string // ID of the triggering message
	ConversationID string // Conversation to operate in
	AgentID        string // Full "agent/xxx" userID
	SenderID       string // Human user who sent the message
}

// AgentExecutor orchestrates the full agent execution pipeline:
// context loading, agent building, LLM streaming, broadcast, and persistence.
type AgentExecutor struct {
	registry       *AgentRegistry
	contextManager ContextManager
	agentBuilder   *AgentBuilder
	streamBridge   *StreamBridge
	broadcaster    *BroadcastHelper
	store          store.StoreAPI
	sem            *Semaphore // concurrency semaphore (optional)
	logger         Logger
	totalTimeout   time.Duration // default 120s
	typingTimeout  time.Duration // default 60s
	metrics        LLMMetrics    // optional LLM call metrics recorder (nil = disabled)
}

// ExecutorOption configures an AgentExecutor.
type ExecutorOption func(*AgentExecutor)

// WithTotalTimeout sets the maximum total execution time for an agent task.
// Default is 120 seconds. Ignored if d <= 0.
func WithTotalTimeout(d time.Duration) ExecutorOption {
	return func(e *AgentExecutor) {
		if d > 0 {
			e.totalTimeout = d
		}
	}
}

// WithTypingTimeout sets the maximum time to wait for the first LLM token
// before clearing the typing indicator. Default is 60 seconds. Ignored if d <= 0.
func WithTypingTimeout(d time.Duration) ExecutorOption {
	return func(e *AgentExecutor) {
		if d > 0 {
			e.typingTimeout = d
		}
	}
}

// WithLLMMetrics sets the metrics recorder for LLM calls.
// When set, the executor records duration, model, and error information
// for each agent Build step. Pass nil (or omit) to disable metrics.
func WithLLMMetrics(m LLMMetrics) ExecutorOption {
	return func(e *AgentExecutor) {
		e.metrics = m
	}
}

// NewAgentExecutor creates an AgentExecutor with all dependencies.
// If maxConcurrent > 0, a semaphore channel is created to limit parallel executions.
// Optional ExecutorOption values override defaults (totalTimeout=120s, typingTimeout=60s).
func NewAgentExecutor(
	registry *AgentRegistry,
	contextManager ContextManager,
	agentBuilder *AgentBuilder,
	streamBridge *StreamBridge,
	broadcaster *BroadcastHelper,
	store store.StoreAPI,
	maxConcurrent int,
	logger Logger,
	opts ...ExecutorOption,
) *AgentExecutor {
	if logger == nil {
		logger = noopLogger{}
	}
	e := &AgentExecutor{
		registry:       registry,
		contextManager: contextManager,
		agentBuilder:   agentBuilder,
		streamBridge:   streamBridge,
		broadcaster:    broadcaster,
		store:          store,
		logger:         logger,
		totalTimeout:   120 * time.Second,
		typingTimeout:  60 * time.Second,
	}
	for _, opt := range opts {
		opt(e)
	}
	if maxConcurrent > 0 {
		e.sem = NewSemaphore(maxConcurrent)
	}
	return e
}

// Execute runs the full agent execution pipeline for a single user message.
//
// Pipeline steps:
//  1. Acquire semaphore (if configured).
//  2. Apply total timeout (default 120s, configurable via WithTotalTimeout).
//  3. Look up agent config from registry.
//  4. Send typing=true to the human user (D-065).
//  5. Load conversation context.
//  6. Build the agent (LLM client + Eino runner).
//  7. Convert messages to Eino schema.
//  8. Run the agent and bridge the stream.
//  9. Broadcast streaming chunks to the human user.
//  10. Send is_done=true broadcast (D-052 step 1).
//  11. Persist the final message (D-052 step 2).
func (e *AgentExecutor) Execute(ctx context.Context, payload ExecutePayload) error {
	startTime := time.Now()
	e.logger.Info("agent executor: starting",
		"message_id", payload.MessageID,
		"conversation_id", payload.ConversationID,
		"agent_id", payload.AgentID,
	)

	// 1. Semaphore: acquire with context cancellation check.
	if e.sem != nil {
		if err := e.sem.Acquire(ctx); err != nil {
			return fmt.Errorf("execute agent: %w", err)
		}
		e.logger.Debug("agent executor: semaphore acquired")
		defer func() {
			e.sem.Release()
			e.logger.Debug("agent executor: semaphore released")
		}()
	}

	// 2. Total timeout to bound execution time.
	ctx, cancel := context.WithTimeout(ctx, e.totalTimeout)
	defer cancel()

	// 3. Look up agent config by trimming "agent/" prefix.
	agentID := strings.TrimPrefix(payload.AgentID, "agent/")
	config, ok := e.registry.Get(agentID)
	if !ok {
		return fmt.Errorf("execute agent: %w: %s", ErrAgentNotFound, agentID)
	}

	// 4. Send typing=true to the human user (D-065).
	e.broadcaster.SendTyping(ctx, payload.AgentID, payload.SenderID, payload.ConversationID, true)

	// 5. Ensure typing is cleared on exit. Use sync.Once so both the
	//    typing timeout (D-065) and the first-token path can
	//    safely clear the indicator without racing or double-broadcasting.
	var typingOnce sync.Once
	clearTyping := func() {
		typingOnce.Do(func() {
			e.broadcaster.SendTyping(ctx, payload.AgentID, payload.SenderID, payload.ConversationID, false)
		})
	}
	defer clearTyping()

	// Typing timeout (D-065): if no token arrives within the configured
	// typing timeout, clear the typing indicator. The total timeout will
	// eventually kill the execution; this just improves UX by stopping
	// the spinner.
	go func() {
		timer := time.NewTimer(e.typingTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			clearTyping()
		case <-ctx.Done():
		}
	}()

	// 6. Load conversation context.
	messages, err := e.contextManager.GetContext(ctx, payload.ConversationID, config)
	if err != nil {
		return fmt.Errorf("execute agent: load context: %w", err)
	}

	// 7. Build the agent (LLM client + Eino runner).
	buildStart := time.Now()
	builtAgent, err := e.agentBuilder.Build(ctx, config)
	buildDuration := time.Since(buildStart)
	if e.metrics != nil {
		e.metrics.Record(ctx, LLMCallEvent{
			AgentID:  payload.AgentID,
			Model:    config.Model,
			Duration: buildDuration,
			Error:    err,
		})
	}
	if err != nil {
		return fmt.Errorf("execute agent: build agent: %w", err)
	}

	// 8. Convert messages to Eino schema.
	schemaMessages := convertMessages(messages)

	// 9. Generate stream_id and checkpoint_id for this execution.
	streamID := uuid.New().String()
	checkpointID := uuid.New().String()

	// 10. Run agent with checkpoint ID for HITL support (D-083/D-084).
	iter := builtAgent.Runner.Run(ctx, schemaMessages, adk.WithCheckPointID(checkpointID))

	// 11. Bridge stream with interrupt detection (Phase 8B).
	chunkCh := make(chan StreamChunk, 64)
	interruptCh := make(chan *InterruptInfo, 1)
	go e.streamBridge.BridgeWithInterrupt(ctx, iter, chunkCh, interruptCh)

	// 12. Consume chunks and broadcast to the human user.
	var fullResponse strings.Builder
	firstToken := true

	for chunk := range chunkCh {
		if chunk.Err != nil {
			// Broadcast is_done with partial text so clients clean up (D-052).
			partialText := fullResponse.String()
			e.broadcaster.SendStreamUpdate(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, streamID, partialText, true)
			// Persist partial text if any.
			if partialText != "" {
				msg := &model.Message{
					ID:              uuid.New().String(),
					ClientMessageID: uuid.New().String(),
					ConversationID:  payload.ConversationID,
					SenderID:        payload.AgentID,
					Content:         partialText,
					Type:            "text",
					Status:          "sent",
					CreatedAt:       time.Now(),
				}
				if _, persistErr := e.store.SendMessage(ctx, msg, []string{payload.SenderID, payload.AgentID}); persistErr != nil {
					e.logger.Error("agent executor: failed to persist partial response", "error", persistErr)
				}
			}
			// Map to sentinel errors for classifyError (D-067).
			streamErr := chunk.Err
			if errors.Is(streamErr, context.DeadlineExceeded) {
				streamErr = fmt.Errorf("%w: %v", ErrLLMTimeout, streamErr)
			} else if strings.Contains(streamErr.Error(), "rate") || strings.Contains(streamErr.Error(), "429") {
				streamErr = fmt.Errorf("%w: %v", ErrLLMRateLimited, streamErr)
			} else if strings.Contains(streamErr.Error(), "500") || strings.Contains(streamErr.Error(), "502") || strings.Contains(streamErr.Error(), "503") {
				// HTTP 5xx errors from the LLM provider are transient.
				streamErr = fmt.Errorf("%w: %v", ErrLLMTimeout, streamErr)
			}
			return fmt.Errorf("execute agent: stream: %w", streamErr)
		}

		if chunk.Content != "" {
			// On first token, clear typing indicator (D-065).
			if firstToken {
				clearTyping()
				firstToken = false
			}

			// Broadcast cumulative text snapshot (D-051).
			e.broadcaster.SendStreamUpdate(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, streamID, chunk.Content, false)

			// Track the full response for persistence.
			fullResponse.Reset()
			fullResponse.WriteString(chunk.Content)
		}

		if chunk.IsDone {
			break
		}
	}

	// 12b. Check for HITL interrupt (Phase 8B).
	// BridgeWithInterrupt closes both channels when done. The interruptCh
	// receives at most one value. A non-blocking select detects whether the
	// agent paused for user input.
	if info, ok := <-interruptCh; ok && info != nil {
		e.logger.Info("agent executor: HITL interrupt",
			"agent_id", payload.AgentID,
			"conversation_id", payload.ConversationID,
			"checkpoint_id", checkpointID,
		)
		// Close the stream (D-052) so clients exit the streaming state.
		partialText := fullResponse.String()
		e.broadcaster.SendStreamUpdate(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, streamID, partialText, true)
		// Broadcast agent status → asking_user.
		e.broadcaster.SendAgentStatus(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, "asking_user")
		// Broadcast the question to the human user.
		e.broadcaster.SendAgentQuestion(ctx, payload.SenderID, payload.AgentID, payload.ConversationID,
			info.Question, checkpointID, "")
		// Broadcast checkpoint created.
		e.broadcaster.SendAgentCheckpointCreated(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, checkpointID)
		// Do NOT persist a message — the agent is paused, not done.
		// Do NOT return an error — this is a controlled pause.
		// The conversation lock is held by the task handler; for HITL it
		// will NOT be released (D-084). We signal this via ErrHITLInterrupted.
		return fmt.Errorf("execute agent: %w", ErrHITLInterrupted)
	}

	// 13. Send is_done=true broadcast (D-052 step 1).
	finalText := fullResponse.String()
	e.broadcaster.SendStreamUpdate(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, streamID, finalText, true)

	// 14. Persist the final message (D-052 step 2).
	if finalText != "" {
		msg := &model.Message{
			ID:              uuid.New().String(),
			ClientMessageID: uuid.New().String(),
			ConversationID:  payload.ConversationID,
			SenderID:        payload.AgentID, // the agent, not the human
			Content:         finalText,
			Type:            "text",
			Status:          "sent",
			CreatedAt:       time.Now(),
		}
		if _, err := e.store.SendMessage(ctx, msg, []string{payload.SenderID, payload.AgentID}); err != nil {
			e.logger.Error("agent executor: failed to persist final message",
				"agent_id", payload.AgentID,
				"conversation_id", payload.ConversationID,
				"error", err,
			)
			return fmt.Errorf("execute agent: persist message: %w", err)
		}
	}

	e.logger.Info("agent executor: completed",
		"agent_id", payload.AgentID,
		"conversation_id", payload.ConversationID,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// SetContextManager replaces the context manager. Exported for testing only.
func (e *AgentExecutor) SetContextManager(cm ContextManager) {
	e.contextManager = cm
}

// ExecuteWithErrorMessage wraps Execute and sends a user-friendly error message
// on failure (D-067). The original error is always returned to the caller.
// HITL interrupts (ErrHITLInterrupted) are NOT treated as errors — no error
// message is persisted.
func (e *AgentExecutor) ExecuteWithErrorMessage(ctx context.Context, payload ExecutePayload) error {
	err := e.Execute(ctx, payload)
	if err != nil {
		// HITL interrupt is not an error — skip error message.
		if errors.Is(err, ErrHITLInterrupted) {
			return err
		}
		e.logger.Error("agent executor: execution failed",
			"agent_id", payload.AgentID,
			"conversation_id", payload.ConversationID,
			"error", err,
		)
		classified := e.classifyError(err)
		e.sendErrorMessage(ctx, payload, classified)
		return err
	}
	return nil
}

// classifyError maps sentinel errors to user-friendly Chinese error messages (D-067/D-082).
func (e *AgentExecutor) classifyError(err error) string {
	switch {
	case errors.Is(err, ErrAPIKeyMissing), errors.Is(err, ErrUnsupportedModel):
		return "抱歉，我的配置有误，请联系管理员检查设置。"
	case errors.Is(err, ErrLLMTimeout), errors.Is(err, ErrLLMRateLimited):
		return "抱歉，我暂时无法回复，请稍后重试。"
	case errors.Is(err, ErrContextLoad):
		return "抱歉，我无法读取对话历史，请重新发送消息。"
	case errors.Is(err, ErrCheckpointStoreSet):
		return "抱歉，等待时间过长，请重新发送消息。"
	case errors.Is(err, ErrMCPUnreachable):
		return "抱歉，外部工具服务不可用，请稍后重试。"
	default:
		return "抱歉，处理遇到问题，请稍后重试。"
	}
}

// sendErrorMessage persists an error message from the agent to the conversation
// so the user sees a friendly failure notice via sync_updates (D-067).
// If persistence itself fails, the error is logged but not returned.
func (e *AgentExecutor) sendErrorMessage(ctx context.Context, payload ExecutePayload, content string) {
	msg := &model.Message{
		ID:              uuid.New().String(),
		ClientMessageID: uuid.New().String(),
		ConversationID:  payload.ConversationID,
		SenderID:        payload.AgentID,
		Content:         content,
		Type:            "text",
		Status:          "sent",
		CreatedAt:       time.Now(),
	}
	if _, err := e.store.SendMessage(ctx, msg, []string{payload.SenderID, payload.AgentID}); err != nil {
		e.logger.Error("agent executor: failed to persist error message", "error", err)
	}
}
