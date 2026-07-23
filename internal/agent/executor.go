package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/internal/tracing"
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
	AgentID        string // Full userID of the agent (exact match in registry, D-054 revised)
	SenderID       string // Human user who sent the message
	DeviceID       string // Device that initiated the conversation (D-102)
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
	debugErrors    bool          // when true, expose raw error messages to users

	// checkpointStore is used for HITL checkpoint cleanup after resume (D-112).
	// When non-nil and the underlying store supports Delete, the resume handler
	// removes the checkpoint from Redis after a successful resume to prevent
	// memory leaks.
	checkpointStore DeletableCheckPointStore

	// remoteCallingStore persists RemoteCallings to survive server restarts (D-137).
	// Optional: when nil, RemoteCalling persistence is skipped (D-063 nil-safe).
	remoteCallingStore *store.RemoteCallingStore

	// broadcastConversationUpdate creates persisted UserUpdate records and
	// enqueues MQ push notifications for conversation members. Injected via
	// WithBroadcastConversationUpdate to avoid circular dependency with handler.
	// Optional: when nil, persisted broadcast is skipped (D-063 nil-safe).
	broadcastConversationUpdate BroadcastConversationUpdateFunc
}

// BroadcastConversationUpdateFunc is the function signature for broadcasting
// persisted conversation updates. This mirrors handler.BroadcastConversationUpdateFunc
// to avoid importing the handler package (which would create a circular dependency).
type BroadcastConversationUpdateFunc func(
	ctx context.Context,
	conversationID string,
	memberIDs []string,
	action string,
) error

// DeletableCheckPointStore extends compose.CheckPointStore with a Delete
// capability (D-112). Any store that implements Get, Set, and Delete satisfies
// this interface.
type DeletableCheckPointStore interface {
	compose.CheckPointStore
	Delete(ctx context.Context, key string) error
}

// cleanupAfterResume removes the checkpoint after a successful resume.
// Non-fatal: errors are logged but do not affect the resume result (D-112).
func (e *AgentExecutor) cleanupAfterResume(ctx context.Context, checkpointID string, logger Logger) {
	if e.checkpointStore != nil {
		logger.Info("agent resume: attempting checkpoint cleanup",
			"checkpoint_id", checkpointID)
		if delErr := e.checkpointStore.Delete(ctx, checkpointID); delErr != nil {
			// Non-fatal: TTL 24h safety net will cleanup (D-112).
			logger.Info("agent resume: checkpoint cleanup failed (non-fatal, TTL will cleanup)",
				"checkpoint_id", checkpointID, "error", delErr)
		} else {
			logger.Info("agent resume: checkpoint cleaned up successfully",
				"checkpoint_id", checkpointID)
		}
	} else {
		logger.Info("agent resume: checkpointStore is nil, skipping cleanup",
			"checkpoint_id", checkpointID)
	}
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

// WithCheckPointStore sets the checkpoint store for HITL checkpoint cleanup
// after resume (D-112). The store must implement Delete; when set, the resume
// handler removes the checkpoint from Redis after a successful resume.
func WithCheckPointStore(store DeletableCheckPointStore) ExecutorOption {
	return func(e *AgentExecutor) {
		e.checkpointStore = store
	}
}

// WithRemoteCallingStore sets the RemoteCallingStore for RemoteCalling persistence (D-137).
// When not set, RemoteCalling creation is skipped (D-063 nil-safe).
func WithRemoteCallingStore(rs *store.RemoteCallingStore) ExecutorOption {
	return func(e *AgentExecutor) {
		e.remoteCallingStore = rs
	}
}

// WithBroadcastConversationUpdate sets the function for broadcasting persisted
// conversation updates to conversation members. When not set, persisted broadcast
// is skipped (D-063 nil-safe).
// This is injected from the handler package to avoid circular dependency.
func WithBroadcastConversationUpdate(fn BroadcastConversationUpdateFunc) ExecutorOption {
	return func(e *AgentExecutor) {
		e.broadcastConversationUpdate = fn
	}
}

// WithDebugErrors enables exposing raw error messages to users instead of
// generic friendly messages. Useful for development and debugging.
// WARNING: Do NOT enable in production — raw errors may leak sensitive info.
func WithDebugErrors(enabled bool) ExecutorOption {
	return func(e *AgentExecutor) {
		e.debugErrors = enabled
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
func (e *AgentExecutor) Execute(ctx context.Context, payload ExecutePayload) (err error) {
	startTime := time.Now()

	// Create agent.execute span for distributed tracing.
	ctx, executeFinish := startAgentExecuteSpan(ctx, payload.AgentID, payload.ConversationID, payload.SenderID)
	defer func() { executeFinish(err) }()

	// Inject caller device into context for tracing/debug (D-102).
	if payload.DeviceID != "" {
		ctx = ContextWithCallerDevice(ctx, CallerDevice{
			UserID:   payload.SenderID,
			DeviceID: payload.DeviceID,
		})
	}

	// Inject agent userID for DynamicToolProvider function lookup.
	// The agent's registered functions are keyed by the agent's userID,
	// not the human sender's identity (which is in CallerDevice).
	ctx = ContextWithAgentID(ctx, payload.AgentID)

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

	// 2b. Enrich context with broadcast metadata so that the
	// LoggingMiddleware can emit function_call updates to clients.
	ctx = WithBroadcastInfo(ctx, e.broadcaster, payload.SenderID, payload.AgentID, payload.ConversationID)

	// 2c. Enrich context with StoreAPI so that the LoggingMiddleware can
	// persist tool_calling messages (D-141). When the store is nil, the
	// middleware falls back to ephemeral-only broadcasting (D-063 nil-safe).
	ctx = WithStore(ctx, e.store)

	// 3. Look up agent config by exact match in the registry (D-054 revised).
	config, ok := e.registry.Get(payload.AgentID)
	if !ok {
		return fmt.Errorf("execute agent: %w: %s", ErrAgentNotFound, payload.AgentID)
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
	schemaMessages := convertMessages(messages, e.registry)

	// 9. Generate stream_id and checkpoint_id for this execution.
	streamID := uuid.New().String()
	checkpointID := uuid.New().String()

	// 10. Run agent with checkpoint ID for HITL support (D-083/D-084).
	ctx, runFinish := startAgentRunSpan(ctx, payload.AgentID)
	defer runFinish(nil)

	// Inject ConversationID into context for tools to use (e.g. client function tools).
	ctx = ContextWithConversationID(ctx, payload.ConversationID)

	iter := builtAgent.Runner.Run(ctx, schemaMessages, adk.WithCheckPointID(checkpointID))

	// 11. Bridge stream with interrupt detection (Phase 8B).
	chunkCh := make(chan StreamChunk, 64)
	interruptCh := make(chan *InterruptInfo, 1)
	go e.streamBridge.BridgeWithInterrupt(ctx, iter, chunkCh, interruptCh)

	// 12. Consume chunks and broadcast to the human user.
	// Start agent.stream span for distributed tracing.
	streamCtx, streamFinish := startAgentStreamSpan(ctx)
	var chunkCount int
	var totalChars int

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
				if result, persistErr := e.store.SendMessage(ctx, msg, []string{payload.SenderID, payload.AgentID}); persistErr != nil {
					e.logger.Error("agent executor: failed to persist partial response", "error", persistErr)
				} else if result != nil {
					// Broadcast the persisted partial message to both users in real-time.
					e.broadcaster.BroadcastMessageUpdate(ctx, result.Updates)
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
			// Finalize stream span with error before returning.
			if span := trace.SpanFromContext(streamCtx); span != nil {
				span.SetAttributes(
					attribute.Int(tracing.AttrChunkCount, chunkCount),
					attribute.Int(tracing.AttrTotalChars, totalChars),
				)
			}
			streamFinish(streamErr)
			return fmt.Errorf("execute agent: stream: %w", streamErr)
		}

		if chunk.Content != "" {
			chunkCount++
			totalChars += len(chunk.Content)

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

	// Finalize agent.stream span with final counts.
	if span := trace.SpanFromContext(streamCtx); span != nil {
		span.SetAttributes(
			attribute.Int(tracing.AttrChunkCount, chunkCount),
			attribute.Int(tracing.AttrTotalChars, totalChars),
		)
	}
	streamFinish(nil)

	// 12b. Check for HITL or client function interrupt (Phase 8B / D-137).
	// BridgeWithInterrupt closes both channels when done. The interruptCh
	// receives at most one value. A non-blocking select detects whether the
	// agent paused for user input or client function call.
	if info, ok := <-interruptCh; ok && info != nil {
		// Create agent.checkpoint.save span for checkpoint persistence.
		_, checkpointFinish := startAgentCheckpointSaveSpan(ctx, checkpointID)
		defer checkpointFinish(nil)

		// Try to parse interrupt data as JSON to distinguish HITL vs client function.
		interruptInfo := parseInterruptData(info.Question)

		if interruptInfo != nil && interruptInfo.IsClientFunc {
			// --- Client function interrupt path ---
			e.logger.Info("agent executor: client function interrupt",
				"agent_id", payload.AgentID,
				"conversation_id", payload.ConversationID,
				"checkpoint_id", checkpointID,
				"method", interruptInfo.Method,
			)

			// 1. Update conversation state to tool_calling.
			hitlUpdatedAt, err := e.store.ConversationStore().UpdateAgentStatus(ctx, payload.ConversationID,
				model.AgentStatusToolCalling, payload.AgentID, checkpointID)
			if err != nil {
				return fmt.Errorf("execute agent: update agent status: %w", err)
			}

			// 2. Persist RemoteCalling to DB (D-063: nil-safe).
			if e.remoteCallingStore != nil {
				// Query the latest executing tool_calling message from DB instead of using in-memory tracker.
				// This is more reliable and survives server restarts.
				var toolCallingMsgID uint32
				if msgStore := e.store.MessageStore(); msgStore != nil {
					if latestMsg, err := msgStore.GetLatestToolCallingMessage(ctx, payload.ConversationID); err == nil && latestMsg != nil {
						toolCallingMsgID = latestMsg.MessageID
					}
				}

				timeoutMs := NormalizeClientFunctionTimeout(int(interruptInfo.TimeoutMs), 0)
				expiresAt := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
				rc := &model.RemoteCalling{
					ID:             uuid.New().String(),
					ConversationID: payload.ConversationID,
					CheckpointID:   checkpointID,
					AgentID:        payload.AgentID,
					Method:         interruptInfo.Method,
					Params:         interruptInfo.Params,
					InterruptID:    info.InterruptID,
					DeviceID:       interruptInfo.DeviceID,
					MessageID:      toolCallingMsgID,
					Status:         model.RemoteCallingStatusPending,
					CreatedAt:      time.Now(),
					ExpiresAt:      &expiresAt,
				}
				if err := e.remoteCallingStore.Create(ctx, rc); err != nil {
					return fmt.Errorf("execute agent: create remote calling: %w", err)
				}
			}

			// 2b. Persisted broadcast to conversation members (D-137).
			if e.broadcastConversationUpdate != nil {
				memberIDs := e.getConversationMemberIDs(ctx, payload.ConversationID)
				if err := e.broadcastConversationUpdate(ctx, payload.ConversationID, memberIDs, "update"); err != nil {
					e.logger.Info("executor: broadcast conversation update failed (non-fatal)",
						"conversation_id", payload.ConversationID, "error", err)
				}
			}

			// 3. Close the stream (D-052) so clients exit the streaming state.
			partialText := fullResponse.String()
			e.broadcaster.SendStreamUpdate(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, streamID, partialText, true)

			// 4. Broadcast lightweight conversation update (pull notification pattern, D-124).
			// Broadcast to both the human user and the agent's daemon so that
			// the daemon receives the notification and can pull RemoteCallings.
			// Use base userID (e.g. "agent" from "agent/weather-bot") because
			// the daemon connects with the base userID, not the full agentID.
			e.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, hitlUpdatedAt)
			e.broadcaster.SendConversationUpdate(ctx, extractBaseUserID(payload.AgentID), payload.ConversationID, hitlUpdatedAt)

			// 5. Broadcast agent status.
			e.broadcaster.SendAgentStatus(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, "tool_calling")

			// 6. Return ErrHITLInterrupted — conversation lock is held (D-084).
			return fmt.Errorf("execute agent: %w", ErrHITLInterrupted)
		}

		// --- HITL (ask_user) interrupt path ---
		e.logger.Info("agent executor: HITL interrupt",
			"agent_id", payload.AgentID,
			"conversation_id", payload.ConversationID,
			"checkpoint_id", checkpointID,
		)

		// 1. Update conversation state to asking_user (D-083: failure aborts HITL).
		hitlUpdatedAt, err := e.store.ConversationStore().UpdateAgentStatus(ctx, payload.ConversationID,
			model.AgentStatusAskingUser, payload.AgentID, checkpointID)
		if err != nil {
			return fmt.Errorf("execute agent: update agent status: %w", err)
		}

		// 2. Persist RemoteCalling to DB (D-063: nil-safe, skip when remoteCallingStore is nil).
		if e.remoteCallingStore != nil {
			expiresAt := time.Now().Add(DefaultHITLTimeout) // D-137: 24h default timeout
			rc := &model.RemoteCalling{
				ID:             uuid.New().String(),
				ConversationID: payload.ConversationID,
				CheckpointID:   checkpointID,
				AgentID:        payload.AgentID,
				Method:         "ask_user",
				Params:         info.Question, // question text directly, no nested JSON
				InterruptID:    info.InterruptID,
				DeviceID:       "", // any device can respond
				Status:         model.RemoteCallingStatusPending,
				CreatedAt:      time.Now(),
				ExpiresAt:      &expiresAt,
			}
			if err := e.remoteCallingStore.Create(ctx, rc); err != nil {
				return fmt.Errorf("execute agent: create remote calling: %w", err)
			}
		}

		// 2b. Persisted broadcast to conversation members (D-137).
		if e.broadcastConversationUpdate != nil {
			memberIDs := e.getConversationMemberIDs(ctx, payload.ConversationID)
			if err := e.broadcastConversationUpdate(ctx, payload.ConversationID, memberIDs, "update"); err != nil {
				e.logger.Info("executor: broadcast conversation update failed (non-fatal)",
					"conversation_id", payload.ConversationID, "error", err)
			}
		}

		// 3. Close the stream (D-052) so clients exit the streaming state.
		partialText := fullResponse.String()
		e.broadcaster.SendStreamUpdate(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, streamID, partialText, true)

		// 4. Broadcast lightweight conversation update (pull notification pattern, D-124).
		// Use base userID for the agent's daemon (see comment in client function path above).
		e.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, hitlUpdatedAt)
		e.broadcaster.SendConversationUpdate(ctx, extractBaseUserID(payload.AgentID), payload.ConversationID, hitlUpdatedAt)

		// 5. Broadcast agent status (D-125: redundant agent_question and
		// agent_checkpoint_created removed; conversation update carries the data).
		e.broadcaster.SendAgentStatus(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, "asking_user")

		// 6. Do NOT register interruptIDs — they are now persisted in the Question table.
		// 7. Return ErrHITLInterrupted — conversation lock is held (D-084).
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
		if result, err := e.store.SendMessage(ctx, msg, []string{payload.SenderID, payload.AgentID}); err != nil {
			e.logger.Error("agent executor: failed to persist final message",
				"agent_id", payload.AgentID,
				"conversation_id", payload.ConversationID,
				"error", err,
			)
			return fmt.Errorf("execute agent: persist message: %w", err)
		} else if result != nil {
			// 14b. Broadcast the persisted message to both users in real-time
			// so they receive the seq-ed update without needing sync_updates.
			e.broadcaster.BroadcastMessageUpdate(ctx, result.Updates)
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

// InvalidateContextCache removes the cached context for a conversation so that
// the next task (e.g. a retried one) loads fresh messages from the database.
// Safe to call even if contextManager is nil.
func (e *AgentExecutor) InvalidateContextCache(conversationID string) {
	if e.contextManager != nil {
		e.contextManager.InvalidateCache(conversationID)
	}
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
// When debugErrors is enabled, raw error messages are exposed for debugging.
func (e *AgentExecutor) classifyError(err error) string {
	if e.debugErrors {
		return fmt.Sprintf("错误详情（调试模式）: %v", err)
	}

	switch {
	case errors.Is(err, ErrAPIKeyMissing), errors.Is(err, ErrUnsupportedModel):
		return "抱歉，我的配置有误，请联系管理员检查设置。"
	case errors.Is(err, ErrAgentNotFound):
		return "抱歉，该助手尚未注册，请联系管理员。"
	case errors.Is(err, ErrAgentBuild):
		return "抱歉，助手初始化失败，请稍后重试。"
	case errors.Is(err, ErrLLMTimeout), errors.Is(err, ErrLLMRateLimited):
		return "抱歉，我暂时无法回复，请稍后重试。"
	case errors.Is(err, ErrContextLoad):
		return "抱歉，我无法读取对话历史，请重新发送消息。"
	case errors.Is(err, ErrCheckpointStoreSet):
		return "抱歉，等待时间过长，请重新发送消息。"
	case errors.Is(err, ErrStreamClosed):
		return "抱歉，连接中断，请重新发送消息。"
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
	if result, err := e.store.SendMessage(ctx, msg, []string{payload.SenderID, payload.AgentID}); err != nil {
		e.logger.Error("agent executor: failed to persist error message", "error", err)
		return
	} else if result != nil {
		// Broadcast the persisted error message to both users in real-time
		// so they receive the seq-ed update without needing sync_updates.
		e.broadcaster.BroadcastMessageUpdate(ctx, result.Updates)
	}
	// Broadcast conversation update so client pulls the error message.
	e.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, time.Now())
	// Also broadcast to agent's daemon using base userID.
	e.broadcaster.SendConversationUpdate(ctx, extractBaseUserID(payload.AgentID), payload.ConversationID, time.Now())
}

// extractBaseUserID extracts the base userID from an agentID.
// For example: "agent/weather-bot" -> "agent", "human-user" -> "human-user".
// This is needed because daemon clients connect using the base userID,
// while agents are registered with full agentIDs (e.g., "agent/weather-bot).
func extractBaseUserID(agentID string) string {
	if idx := strings.Index(agentID, "/"); idx > 0 {
		return agentID[:idx]
	}
	return agentID
}

// getConversationMemberIDs fetches the conversation and returns member user IDs.
// Returns [senderID, agentID] on failure to fetch (best-effort, non-fatal).
func (e *AgentExecutor) getConversationMemberIDs(ctx context.Context, conversationID string) []string {
	conv, err := e.store.ConversationStore().Get(ctx, conversationID)
	if err != nil {
		e.logger.Info("executor: get conversation for member IDs failed (non-fatal)",
			"conversation_id", conversationID, "error", err)
		return nil
	}
	members := []string{conv.UserID1}
	if conv.UserID2 != "" {
		members = append(members, conv.UserID2)
	}
	return members
}

// interruptData holds parsed interrupt data distinguishing HITL vs client function.
type interruptData struct {
	IsClientFunc bool
	Method       string
	Params       string
	DeviceID     string
	TimeoutMs    int64
}

// parseInterruptData parses the interrupt question JSON to determine if it's a
// client function call or a HITL (ask_user) interrupt. Returns nil if the data
// is not a valid client function interrupt.
func parseInterruptData(question string) *interruptData {
	var payload struct {
		Method    string `json:"method"`
		Params    string `json:"params"`
		DeviceID  string `json:"device_id"`
		TimeoutMs int64  `json:"timeout_ms"`
	}
	if err := json.Unmarshal([]byte(question), &payload); err != nil || payload.Method == "" || payload.Method == "ask_user" {
		return nil
	}
	return &interruptData{
		IsClientFunc: true,
		Method:       payload.Method,
		Params:       payload.Params,
		DeviceID:     payload.DeviceID,
		TimeoutMs:    payload.TimeoutMs,
	}
}
