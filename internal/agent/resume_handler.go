// Resume handler processes TypeAgentResume MQ tasks (Phase 8B / D-085).
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

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// AgentResumePayload is the MQ task payload for TypeAgentResume.
// Answers are NOT included — they are persisted in the Question table (D-116).
// The resume handler reads answered Questions from DB to build the targets map.
type AgentResumePayload struct {
	ConversationID string `json:"conversation_id"`
	CheckpointID   string `json:"checkpoint_id"`
	AgentID        string `json:"agent_id"`
	SenderID       string `json:"sender_id"` // human user who sent the answer
	DeviceID       string `json:"device_id"` // Phase 6 (D-102)
}

// NewAgentResumeHandler returns an mq.TaskHandler-compatible function that
// processes TypeAgentResume tasks. It resumes a paused agent after HITL.
//
// The handler:
//  1. Unmarshals the task payload
//  2. Checks idempotency (skip if checkpoint already resumed)
//  3. Acquires a per-conversation lock (D-084)
//  4. Looks up agent config from registry
//  5. Builds the agent (with CheckPointStore)
//  6. Calls Runner.ResumeWithParams with the user's answer
//  7. Bridges the stream and broadcasts to the human user
//  8. Persists the final message
//  9. Always returns nil to MQ (D-073)
//
// idempotency may be nil to disable the duplicate-resume guard (backward
// compatible). When non-nil, a Redis SETNX on "agent:resume:<checkpointID>"
// ensures that the same checkpoint is resumed at most once, even if multiple
// TypeAgentResume tasks are enqueued (e.g. client calls both send_message
// and agent_resume, or retries agent_resume).
func NewAgentResumeHandler(
	executor *AgentExecutor,
	registry *AgentRegistry,
	lock ConversationLock,
	logger Logger,
	idempotency IdempotencyStore,
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

		// 2b. Two-phase idempotency check (D-121).
		if idempotency != nil {
			processedKey := "agent:resume:" + payload.CheckpointID
			processingKey := "agent:resume:processing:" + payload.CheckpointID

			processed, err1 := idempotency.CheckProcessed(ctx, processedKey)
			processing, err2 := idempotency.CheckProcessed(ctx, processingKey)

			// Fail-open: only skip if check succeeded and key exists (D-072).
			if (err1 == nil && processed) || (err2 == nil && processing) {
				logger.Info("agent resume: skipping duplicate/in-progress resume",
					"checkpoint_id", payload.CheckpointID,
					"conversation_id", payload.ConversationID,
				)
				return nil
			}

			// Mark as in-progress with lock-matching TTL (130s, D-075).
			if _, err := idempotency.MarkProcessed(ctx, processingKey, 130*time.Second); err != nil {
				logger.Error("agent resume: processing mark failed (non-fatal)", "error", err)
			}
		}

		// Create agent.execute span for the resume path.
		// HITL resume creates an independent trace (no link to the original
		// process trace). Cross-trace linking may be added later.
		ctx, executeFinish := startAgentExecuteSpan(ctx, payload.AgentID, payload.ConversationID, payload.SenderID)
		defer executeFinish(nil)

		// 3. Acquire per-conversation lock (D-084).
		// For HITL, the initial execution's lock is NOT released. The resume
		// task reuses the same lock. If the lock is still held (expected for
		// HITL), we proceed without failing. If it's not held (e.g. TTL
		// expired), we acquire a new one.
		weOwnLock := false
		if lock != nil {
			acquired, err := lock.Acquire(ctx, payload.ConversationID, 130*time.Second)
			if err != nil {
				logger.Error("agent resume: lock acquire failed",
					"conversation_id", payload.ConversationID, "error", err)
				// fail-open: proceed without lock
			} else if !acquired {
				// Lock already held by the initial HITL execution — this is
				// expected (D-084). We do NOT own it.
				logger.Debug("agent resume: lock already held (HITL in progress)",
					"conversation_id", payload.ConversationID)
			} else {
				weOwnLock = true
			}
		}

		releaseLock := func() {
			if weOwnLock && lock != nil {
				if err := lock.Release(ctx, payload.ConversationID); err != nil {
					logger.Error("agent resume: lock release failed",
						"conversation_id", payload.ConversationID, "error", err)
				} else {
					logger.Debug("agent resume: conversation lock released",
						"conversation_id", payload.ConversationID)
				}
			}
		}

		// 4. Look up agent config by exact match in the registry (D-054 revised).
		agentID := payload.AgentID
		config, ok := registry.Get(agentID)
		if !ok {
			logger.Error("agent resume: agent not found", "agent_id", agentID)
			cleanupAfterResumeFailure(ctx, executor, payload.ConversationID, payload.CheckpointID, logger)
			execPayload := ExecutePayload{
				ConversationID: payload.ConversationID,
				AgentID:        payload.AgentID,
				SenderID:       payload.SenderID,
				DeviceID:       payload.DeviceID,
			}
			executor.sendErrorMessage(ctx, execPayload,
				"抱歉，Agent 配置不存在，请重新发送消息。")
			// Clean up idempotency keys (D-121).
			if idempotency != nil {
				processingKey := "agent:resume:processing:" + payload.CheckpointID
				processedKey := "agent:resume:" + payload.CheckpointID
				_ = idempotency.DeleteKey(ctx, processingKey)
				if _, err := idempotency.MarkProcessed(ctx, processedKey, 24*time.Hour); err != nil {
					logger.Error("agent resume: processed mark after failure (non-fatal)", "error", err)
				}
			}
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
			cleanupAfterResumeFailure(ctx, executor, payload.ConversationID, payload.CheckpointID, logger)
			execPayload := ExecutePayload{
				ConversationID: payload.ConversationID,
				AgentID:        payload.AgentID,
				SenderID:       payload.SenderID,
				DeviceID:       payload.DeviceID, // Phase 6 (D-102)
			}
			executor.sendErrorMessage(ctx, execPayload,
				"抱歉，恢复执行失败，请重新发送消息。")
			// Clean up idempotency keys (D-121).
			if idempotency != nil {
				processingKey := "agent:resume:processing:" + payload.CheckpointID
				processedKey := "agent:resume:" + payload.CheckpointID
				_ = idempotency.DeleteKey(ctx, processingKey)
				if _, err := idempotency.MarkProcessed(ctx, processedKey, 24*time.Hour); err != nil {
					logger.Error("agent resume: processed mark after failure (non-fatal)", "error", err)
				}
			}
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

		// Read answered Questions from DB to build the Targets map (D-116).
		// Answers are persisted in the Question table, not in the MQ payload.
		if executor.questionStore == nil {
			logger.Error("agent resume: questionStore is nil, cannot read answers",
				"checkpoint_id", payload.CheckpointID)
			releaseLock()
			return nil
		}
		questions, err := executor.questionStore.GetByCheckpoint(ctx, payload.CheckpointID)
		if err != nil {
			logger.Error("agent resume: get questions failed",
				"checkpoint_id", payload.CheckpointID, "error", err)
			cleanupAfterResumeFailure(ctx, executor, payload.ConversationID, payload.CheckpointID, logger)
			execPayload := ExecutePayload{
				ConversationID: payload.ConversationID,
				AgentID:        payload.AgentID,
				SenderID:       payload.SenderID,
				DeviceID:       payload.DeviceID,
			}
			executor.sendErrorMessage(ctx, execPayload,
				"抱歉，恢复执行失败，请重新发送消息。")
			// Clean up idempotency keys (D-121).
			if idempotency != nil {
				processingKey := "agent:resume:processing:" + payload.CheckpointID
				processedKey := "agent:resume:" + payload.CheckpointID
				_ = idempotency.DeleteKey(ctx, processingKey)
				if _, err := idempotency.MarkProcessed(ctx, processedKey, 24*time.Hour); err != nil {
					logger.Error("agent resume: processed mark after failure (non-fatal)", "error", err)
				}
			}
			releaseLock()
			return nil // D-073
		}

		targets := make(map[string]any)
		for _, q := range questions {
			if q.Status == model.QuestionStatusAnswered && q.InterruptID != "" {
				targets[q.InterruptID] = q.Answer
			}
		}
		if len(targets) == 0 {
			logger.Info("agent resume: no answered questions found for checkpoint",
				"checkpoint_id", payload.CheckpointID)
		} else {
			logger.Debug("agent resume: built targets from DB questions",
				"targets_count", len(targets),
				"checkpoint_id", payload.CheckpointID)
		}
		params := &adk.ResumeParams{
			Targets: targets,
		}

		// Note: Eino's resume path saves re-interrupt checkpoints under the
		// original checkPointID (the function parameter), ignoring the
		// WithCheckPointID option. We therefore broadcast payload.CheckpointID
		// to clients for subsequent resumes.
		iter, err := builtAgent.Runner.ResumeWithParams(ctx, payload.CheckpointID, params)
		if err != nil {
			logger.Error("agent resume: resume failed",
				"checkpoint_id", payload.CheckpointID, "error", err)
			clearTyping()
			// Check if checkpoint expired.
			if errors.Is(err, ErrCheckpointNotFound) || strings.Contains(err.Error(), "not found") {
				cleanupAfterResumeFailure(ctx, executor, payload.ConversationID, payload.CheckpointID, logger)
				execPayload := ExecutePayload{
					ConversationID: payload.ConversationID,
					AgentID:        payload.AgentID,
					SenderID:       payload.SenderID,
					DeviceID:       payload.DeviceID, // Phase 6 (D-102)
				}
				executor.sendErrorMessage(ctx, execPayload,
					"抱歉，等待时间过长，请重新发送消息。")
				// Clean up idempotency keys (D-121).
				if idempotency != nil {
					processingKey := "agent:resume:processing:" + payload.CheckpointID
					processedKey := "agent:resume:" + payload.CheckpointID
					_ = idempotency.DeleteKey(ctx, processingKey)
					if _, err := idempotency.MarkProcessed(ctx, processedKey, 24*time.Hour); err != nil {
						logger.Error("agent resume: processed mark after failure (non-fatal)", "error", err)
					}
				}
			}
			// HITL resume: transient error notifies user rather than auto-retrying via MQ,
			// because the user has invested interaction cost and should decide whether to retry.
			// This differs from task_handler which returns error for Asynq auto-retry.
			if isTransientError(err) {
				execPayload := ExecutePayload{
					ConversationID: payload.ConversationID,
					AgentID:        payload.AgentID,
					SenderID:       payload.SenderID,
					DeviceID:       payload.DeviceID,
				}
				executor.sendErrorMessage(ctx, execPayload,
					"抱歉，服务暂时不可用，请稍后重试。")
				// Delete processing key to allow immediate retry (D-121).
				if idempotency != nil {
					processingKey := "agent:resume:processing:" + payload.CheckpointID
					_ = idempotency.DeleteKey(ctx, processingKey)
				}
			}
			releaseLock()
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
				// HITL resume: transient error notifies user rather than auto-retrying via MQ,
				// because the user has invested interaction cost and should decide whether to retry.
				// This differs from task_handler which returns error for Asynq auto-retry.
				if isTransientError(chunk.Err) {
					execPayload := ExecutePayload{
						ConversationID: payload.ConversationID,
						AgentID:        payload.AgentID,
						SenderID:       payload.SenderID,
						DeviceID:       payload.DeviceID,
					}
					executor.sendErrorMessage(ctx, execPayload,
						"抱歉，服务暂时不可用，请稍后重试。")
					// Delete processing key to allow immediate retry (D-121).
					if idempotency != nil {
						processingKey := "agent:resume:processing:" + payload.CheckpointID
						_ = idempotency.DeleteKey(ctx, processingKey)
					}
				}
				releaseLock()
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
			// Update conversation state to asking_user (D-083).
			resumeHitlUpdatedAt, err := executor.store.ConversationStore().UpdateAgentStatus(ctx, payload.ConversationID,
				model.AgentStatusAskingUser, payload.AgentID, payload.CheckpointID)
			if err != nil {
				releaseLock()
				return fmt.Errorf("agent resume: update agent status: %w", err)
			}

			// Persist Question to DB (D-063: nil-safe).
			if executor.questionStore != nil {
				question := &model.Question{
					ID:             uuid.New().String(),
					ConversationID: payload.ConversationID,
					CheckpointID:   payload.CheckpointID,
					InterruptID:    info.InterruptID,
					QuestionText:   info.Question,
					Status:         model.QuestionStatusPending,
					CreatedAt:      time.Now(),
				}
				if err := executor.questionStore.Create(ctx, question); err != nil {
					releaseLock()
					return fmt.Errorf("agent resume: create question: %w", err)
				}
			}

			// Broadcast conversation update (pull notification pattern, D-124).
			executor.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, resumeHitlUpdatedAt)

			executor.broadcaster.SendAgentStatus(ctx, payload.SenderID, payload.AgentID, payload.ConversationID, "asking_user")
			// D-084: Do NOT release lock on HITL re-interrupt.
			// Delete processing key to allow subsequent resume (D-121).
			if idempotency != nil {
				processingKey := "agent:resume:processing:" + payload.CheckpointID
				_ = idempotency.DeleteKey(ctx, processingKey)
			}
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
			result, err := executor.store.SendMessage(ctx, msg, []string{payload.SenderID, payload.AgentID})
			if err != nil {
				logger.Error("agent resume: persist failed", "error", err)
			} else if result != nil {
				// Broadcast the message update to each recipient so the
				// WebSocket client receives the new message in real-time.
				for _, u := range result.Updates {
					updates := &protocol.PackageDataUpdates{
						Updates: []protocol.PackageDataUpdate{
							{
								Seq:       u.Seq,
								Type:      protocol.UpdateTypeMessage,
								Payload:   u.Payload,
								CreatedAt: u.CreatedAt,
							},
						},
					}
					if broadcastErr := executor.broadcaster.BroadcastRaw(u.UserID, updates); broadcastErr != nil {
						logger.Error("agent resume: broadcast message update failed",
							"user_id", u.UserID, "error", broadcastErr)
					}
				}
			}
		}

		logger.Info("agent resume: completed",
			"agent_id", payload.AgentID,
			"conversation_id", payload.ConversationID,
			"checkpoint_id", payload.CheckpointID,
		)

		// Mark as processed (24h) for replay protection. Clean up processing key (D-121).
		if idempotency != nil {
			processedKey := "agent:resume:" + payload.CheckpointID
			processingKey := "agent:resume:processing:" + payload.CheckpointID
			if _, err := idempotency.MarkProcessed(ctx, processedKey, 24*time.Hour); err != nil {
				logger.Error("agent resume: processed mark failed (non-fatal)", "error", err)
			}
			_ = idempotency.DeleteKey(ctx, processingKey)
		}

		// Phase 2 cleanup: clear conversation status, delete questions, delete checkpoint.
		// All cleanup operations are non-fatal — errors are logged but do not affect
		// the resume result.

		// 1. Clear Conversation agent status (reset to idle).
		if _, err := executor.store.ConversationStore().ClearAgentStatus(ctx, payload.ConversationID); err != nil {
			logger.Error("agent resume: clear conversation status failed (non-fatal)",
				"conversation_id", payload.ConversationID, "error", err)
		}

		// 2. Delete Questions for this checkpoint (D-116).
		if executor.questionStore != nil {
			if err := executor.questionStore.DeleteByCheckpoint(ctx, payload.CheckpointID); err != nil {
				logger.Error("agent resume: delete questions failed (non-fatal)",
					"checkpoint_id", payload.CheckpointID, "error", err)
			}
		}

		// 3. Delete checkpoint from Redis (D-112).
		executor.cleanupAfterResume(ctx, payload.CheckpointID, logger)

		// 4. Broadcast conversation update to notify clients that questions have been cleared.
		// This triggers the pull-on-notification pattern: clients will fetch the latest
		// conversation state via get_conversation RPC and see that questions are empty.
		executor.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, time.Now())

		releaseLock()
		return nil
	}
}

// cleanupAfterResumeFailure resets conversation state after a permanent resume failure.
// All operations are non-fatal — errors are logged but do not propagate (D-122).
func cleanupAfterResumeFailure(ctx context.Context, executor *AgentExecutor,
	conversationID, checkpointID string, logger Logger) {
	// 1. Clear agent status (reset to idle).
	if _, err := executor.store.ConversationStore().ClearAgentStatus(ctx, conversationID); err != nil {
		logger.Error("agent resume: clear status after failure (non-fatal)",
			"conversation_id", conversationID, "error", err)
	}
	// 2. Delete questions for this checkpoint (soft-delete via GORM).
	if executor.questionStore != nil {
		if err := executor.questionStore.DeleteByCheckpoint(ctx, checkpointID); err != nil {
			logger.Error("agent resume: delete questions after failure (non-fatal)",
				"checkpoint_id", checkpointID, "error", err)
		}
	}
	// 3. Delete checkpoint from Redis (D-112).
	executor.cleanupAfterResume(ctx, checkpointID, logger)
}
