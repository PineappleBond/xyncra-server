// Resume handler processes TypeAgentResume MQ tasks (Phase 8B / D-085).
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cloudwego/eino/adk"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// AgentResumePayload is the MQ task payload for TypeAgentResume.
type AgentResumePayload struct {
	ConversationID string `json:"conversation_id"`
	CheckpointID   string `json:"checkpoint_id"`
	InterruptID    string `json:"interrupt_id"`
	Answer         string `json:"answer"`
	SenderID       string `json:"sender_id"` // human user who sent the answer
	AgentID        string `json:"agent_id"`  // agent to resume (e.g. "agent/xxx")
	DeviceID       string `json:"device_id"` // Phase 6 (D-102)
}

// NewAgentResumeHandler returns an mq.TaskHandler-compatible function that
// processes TypeAgentResume tasks. It resumes a paused agent after HITL.
//
// The handler:
//  1. Unmarshals the task payload
//  2. Acquires a per-conversation lock (D-084)
//  3. Looks up agent config from registry
//  4. Builds the agent (with CheckPointStore)
//  5. Calls Runner.ResumeWithParams with the user's answer
//  6. Bridges the stream and broadcasts to the human user
//  7. Persists the final message
//  8. Always returns nil to MQ (D-073)
func NewAgentResumeHandler(
	executor *AgentExecutor,
	registry *AgentRegistry,
	lock ConversationLock,
	logger Logger,
) func(ctx context.Context, task *mq.Task) error {
	if logger == nil {
		logger = noopLogger{}
	}
	return func(ctx context.Context, task *mq.Task) error {
		// 1. Nil guard.
		if task == nil {
			return nil
		}

		// 2. Unmarshal payload.
		var payload AgentResumePayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			logger.Error("agent resume: unmarshal failed", "error", err)
			return nil
		}

		if payload.ConversationID == "" || payload.CheckpointID == "" || payload.AgentID == "" {
			logger.Error("agent resume: missing required fields",
				"conversation_id", payload.ConversationID,
				"checkpoint_id", payload.CheckpointID,
				"agent_id", payload.AgentID,
			)
			return nil
		}

		// 3. Acquire per-conversation lock (D-084).
		// For HITL, the initial execution's lock is NOT released. The resume
		// task reuses the same lock. If the lock is still held (expected for
		// HITL), we proceed without failing. If it's not held (e.g. TTL
		// expired), we acquire a new one.
		lockHeld := false
		if lock != nil {
			acquired, err := lock.Acquire(ctx, payload.ConversationID, 130*time.Second)
			if err != nil {
				logger.Error("agent resume: lock acquire failed",
					"conversation_id", payload.ConversationID, "error", err)
			} else if !acquired {
				// Lock already held by the initial HITL execution — this is
				// expected (D-084). Proceed without acquiring a new lock.
				logger.Debug("agent resume: lock already held (HITL in progress)",
					"conversation_id", payload.ConversationID)
			} else {
				lockHeld = true
			}
		}

		releaseLock := func() {
			if lockHeld && lock != nil {
				if err := lock.Release(ctx, payload.ConversationID); err != nil {
					logger.Error("agent resume: lock release failed",
						"conversation_id", payload.ConversationID, "error", err)
				}
			}
		}

		// 4. Look up agent config.
		agentID := strings.TrimPrefix(payload.AgentID, "agent/")
		config, ok := registry.Get(agentID)
		if !ok {
			logger.Error("agent resume: agent not found", "agent_id", agentID)
			releaseLock()
			return nil
		}

		// 5. Inject caller device into context for DynamicToolProvider (D-102).
		if payload.DeviceID != "" {
			ctx = ContextWithCallerDevice(ctx, CallerDevice{
				UserID:   payload.SenderID,
				DeviceID: payload.DeviceID,
			})
		}

		// 6. Build the agent.
		builtAgent, err := executor.agentBuilder.Build(ctx, config)
		if err != nil {
			logger.Error("agent resume: build failed", "agent_id", agentID, "error", err)
			execPayload := ExecutePayload{
				ConversationID: payload.ConversationID,
				AgentID:        payload.AgentID,
				SenderID:       payload.SenderID,
				DeviceID:       payload.DeviceID, // Phase 6 (D-102)
			}
			executor.sendErrorMessage(ctx, execPayload,
				"抱歉，恢复执行失败，请重新发送消息。")
			releaseLock()
			return nil
		}

		// 7. Resume the agent.
		ctx, cancel := context.WithTimeout(ctx, executor.totalTimeout)
		defer cancel()

		// Send typing indicator.
		executor.broadcaster.SendTyping(ctx, payload.AgentID, payload.SenderID, payload.ConversationID, true)
		var typingOnce sync.Once
		clearTyping := func() {
			typingOnce.Do(func() {
				executor.broadcaster.SendTyping(ctx, payload.AgentID, payload.SenderID, payload.ConversationID, false)
			})
		}
		defer clearTyping()

		// ResumeWithParams passes the user's answer back to the interrupted agent.
		params := &adk.ResumeParams{
			Targets: map[string]any{
				payload.InterruptID: payload.Answer,
			},
		}

		iter, err := builtAgent.Runner.ResumeWithParams(ctx, payload.CheckpointID, params)
		if err != nil {
			logger.Error("agent resume: resume failed",
				"checkpoint_id", payload.CheckpointID, "error", err)
			clearTyping()
			// Check if checkpoint expired.
			if errors.Is(err, ErrCheckpointNotFound) || strings.Contains(err.Error(), "not found") {
				execPayload := ExecutePayload{
					ConversationID: payload.ConversationID,
					AgentID:        payload.AgentID,
					SenderID:       payload.SenderID,
					DeviceID:       payload.DeviceID, // Phase 6 (D-102)
				}
				executor.sendErrorMessage(ctx, execPayload,
					"抱歉，等待时间过长，请重新发送消息。")
			}
			releaseLock()
			// Return transient errors to MQ for retry.
			if isTransientError(err) {
				return err
			}
			return nil
		}

		// 8. Bridge the stream.
		streamID := uuid.New().String()
		chunkCh := make(chan StreamChunk, 64)
		interruptCh := make(chan *InterruptInfo, 1)
		go executor.streamBridge.BridgeWithInterrupt(ctx, iter, chunkCh, interruptCh)

		// 9. Consume chunks and broadcast.
		var fullResponse strings.Builder
		firstToken := true

		for chunk := range chunkCh {
			if chunk.Err != nil {
				partialText := fullResponse.String()
				executor.broadcaster.SendStreamUpdate(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, streamID, partialText, true)
				clearTyping()
				releaseLock()
				// Return transient stream errors (LLM timeout, rate limit) for retry.
				if isTransientError(chunk.Err) {
					return chunk.Err
				}
				return nil
			}
			if chunk.Content != "" {
				if firstToken {
					clearTyping()
					firstToken = false
				}
				executor.broadcaster.SendStreamUpdate(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, streamID, chunk.Content, false)
				fullResponse.Reset()
				fullResponse.WriteString(chunk.Content)
			}
			if chunk.IsDone {
				break
			}
		}

		// 10. Check for another interrupt (multi-turn HITL).
		if info, ok := <-interruptCh; ok && info != nil {
			checkpointID := uuid.New().String()
			executor.broadcaster.SendAgentStatus(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, "asking_user")
			executor.broadcaster.SendAgentQuestion(ctx, payload.SenderID, payload.AgentID, payload.ConversationID,
				info.Question, checkpointID, "")
			executor.broadcaster.SendAgentCheckpointCreated(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, checkpointID)
			// D-084: Do NOT release lock on HITL re-interrupt.
			return nil
		}

		// 11. Send is_done and persist.
		finalText := fullResponse.String()
		executor.broadcaster.SendStreamUpdate(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, streamID, finalText, true)

		if finalText != "" {
			msg := &model.Message{
				ID:              uuid.New().String(),
				ClientMessageID: uuid.New().String(),
				ConversationID:  payload.ConversationID,
				SenderID:        payload.AgentID,
				Content:         finalText,
				Type:            "text",
				Status:          "sent",
				CreatedAt:       time.Now(),
			}
			if _, err := executor.store.SendMessage(ctx, msg, []string{payload.SenderID, payload.AgentID}); err != nil {
				logger.Error("agent resume: persist failed", "error", err)
			}
		}

		logger.Info("agent resume: completed",
			"agent_id", payload.AgentID,
			"conversation_id", payload.ConversationID,
			"checkpoint_id", payload.CheckpointID,
		)

		releaseLock()
		return nil
	}
}
