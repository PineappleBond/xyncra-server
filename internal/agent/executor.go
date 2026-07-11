package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

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
	sem            chan struct{} // concurrency semaphore (optional)
	logger         *log.Logger
}

// NewAgentExecutor creates an AgentExecutor with all dependencies.
// If maxConcurrent > 0, a semaphore channel is created to limit parallel executions.
func NewAgentExecutor(
	registry *AgentRegistry,
	contextManager ContextManager,
	agentBuilder *AgentBuilder,
	streamBridge *StreamBridge,
	broadcaster *BroadcastHelper,
	store store.StoreAPI,
	maxConcurrent int,
) *AgentExecutor {
	e := &AgentExecutor{
		registry:       registry,
		contextManager: contextManager,
		agentBuilder:   agentBuilder,
		streamBridge:   streamBridge,
		broadcaster:    broadcaster,
		store:          store,
		logger:         log.New(os.Stderr, "[agent-executor] ", log.LstdFlags),
	}
	if maxConcurrent > 0 {
		e.sem = make(chan struct{}, maxConcurrent)
	}
	return e
}

// Execute runs the full agent execution pipeline for a single user message.
//
// Pipeline steps:
//  1. Acquire semaphore (if configured).
//  2. Apply total timeout (120s).
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
	// 1. Semaphore: acquire with context cancellation check.
	if e.sem != nil {
		select {
		case e.sem <- struct{}{}:
			defer func() { <-e.sem }()
		case <-ctx.Done():
			return fmt.Errorf("execute agent: %w", ctx.Err())
		}
	}

	// 2. Total timeout to bound execution time.
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
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
	//    60-second typing timeout (D-065) and the first-token path can
	//    safely clear the indicator without racing or double-broadcasting.
	var typingOnce sync.Once
	clearTyping := func() {
		typingOnce.Do(func() {
			e.broadcaster.SendTyping(ctx, payload.AgentID, payload.SenderID, payload.ConversationID, false)
		})
	}
	defer clearTyping()

	// 60-second typing timeout (D-065): if no token arrives within 60s,
	// clear the typing indicator. The 120s total timeout will eventually
	// kill the execution; this just improves UX by stopping the spinner.
	go func() {
		timer := time.NewTimer(60 * time.Second)
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
	builtAgent, err := e.agentBuilder.Build(ctx, config)
	if err != nil {
		return fmt.Errorf("execute agent: build agent: %w", err)
	}

	// 8. Convert messages to Eino schema.
	schemaMessages := convertMessages(messages)

	// 9. Generate stream_id for this execution.
	streamID := uuid.New().String()

	// 10. Run agent: returns an AsyncIterator over AgentEvents.
	iter := builtAgent.Runner.Run(ctx, schemaMessages)

	// 11. Bridge stream: convert Eino events into StreamChunks.
	chunkCh := make(chan StreamChunk, 64)
	go e.streamBridge.Bridge(ctx, iter, chunkCh)

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
					e.logger.Printf("Execute: failed to persist partial response: %v", persistErr)
				}
			}
			// Map to sentinel errors for classifyError (D-067).
			streamErr := chunk.Err
			if errors.Is(streamErr, context.DeadlineExceeded) {
				streamErr = fmt.Errorf("%w: %v", ErrLLMTimeout, streamErr)
			} else if strings.Contains(streamErr.Error(), "rate") || strings.Contains(streamErr.Error(), "429") {
				streamErr = fmt.Errorf("%w: %v", ErrLLMRateLimited, streamErr)
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
			return fmt.Errorf("execute agent: persist message: %w", err)
		}
	}

	return nil
}

// ExecuteWithErrorMessage wraps Execute and sends a user-friendly error message
// on failure (D-067). The original error is always returned to the caller.
func (e *AgentExecutor) ExecuteWithErrorMessage(ctx context.Context, payload ExecutePayload) error {
	err := e.Execute(ctx, payload)
	if err != nil {
		e.logger.Printf("ExecuteWithErrorMessage: agent=%s conversation=%s error=%v", payload.AgentID, payload.ConversationID, err)
		classified := e.classifyError(err)
		e.sendErrorMessage(ctx, payload, classified)
		return err
	}
	return nil
}

// classifyError maps sentinel errors to user-friendly Chinese error messages (D-067).
func (e *AgentExecutor) classifyError(err error) string {
	switch {
	case errors.Is(err, ErrAPIKeyMissing), errors.Is(err, ErrUnsupportedModel):
		return "抱歉，我的配置有误，请联系管理员检查设置。"
	case errors.Is(err, ErrLLMTimeout), errors.Is(err, ErrLLMRateLimited):
		return "抱歉，我暂时无法回复，请稍后重试。"
	case errors.Is(err, ErrContextLoad):
		return "抱歉，我无法读取对话历史，请重新发送消息。"
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
		e.logger.Printf("sendErrorMessage: persist error message failed: %v", err)
	}
}
