package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/PineappleBond/xyncra-server/internal/mq"
)

// AgentProcessPayload is the MQ task payload for triggering agent processing.
// Fields must match the agentProcessPayload in internal/handler/send_message.go.
type AgentProcessPayload struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	AgentID        string `json:"agent_id"`
	SenderID       string `json:"sender_id"`
	DeviceID       string `json:"device_id"` // Phase 6 (D-102)
}

// IdempotencyStore provides atomic check-and-set for agent task deduplication.
type IdempotencyStore interface {
	// MarkProcessed atomically checks if key was already processed and marks it.
	// Returns true if the key was already processed (duplicate), false if this is the first time.
	// TTL controls how long the processed marker persists.
	MarkProcessed(ctx context.Context, key string, ttl time.Duration) (bool, error)
	// CheckProcessed checks if a key exists without setting it.
	// Returns (true, nil) if the key exists, (false, nil) otherwise.
	CheckProcessed(ctx context.Context, key string) (bool, error)
	// DeleteKey removes a key. Used to clean up processing markers.
	DeleteKey(ctx context.Context, key string) error
}

// redisClient is the subset of redis.Client used by RedisIdempotencyStore.
type redisClient interface {
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd
	Exists(ctx context.Context, keys ...string) *redis.IntCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// RedisIdempotencyStore implements IdempotencyStore using Redis SETNX.
type RedisIdempotencyStore struct {
	client redisClient
}

// NewRedisIdempotencyStore creates a new RedisIdempotencyStore.
func NewRedisIdempotencyStore(client redisClient) *RedisIdempotencyStore {
	return &RedisIdempotencyStore{client: client}
}

// MarkProcessed atomically checks if key was already processed and marks it.
// Returns (true, nil) if duplicate, (false, nil) if first time.
func (s *RedisIdempotencyStore) MarkProcessed(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	result, err := s.client.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, err
	}
	// SetNX returns true if the key was SET (first time), false if it already existed (duplicate).
	return !result, nil
}

// CheckProcessed checks if a key exists without setting it.
// Returns (true, nil) if the key exists, (false, nil) otherwise.
func (s *RedisIdempotencyStore) CheckProcessed(ctx context.Context, key string) (bool, error) {
	n, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteKey removes a key. Used to clean up processing markers.
func (s *RedisIdempotencyStore) DeleteKey(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

// NewAgentTaskHandler returns an mq.TaskHandler-compatible function that
// processes TypeAgentProcess tasks.
//
// The handler:
//  1. Unmarshals the task payload into AgentProcessPayload
//  2. Acquires a per-conversation lock (if lock is non-nil); returns error if held so Asynq retries
//  3. Checks idempotency (skip if already processed)
//  4. Calls AgentExecutor.ExecuteWithErrorMessage
//  5. Returns nil to MQ for permanent errors (D-067); returns transient errors for retry
//
// On normal completion (after releasing the lock) the handler invalidates the
// conversation context cache so subsequent retries load fresh DB state.
//
// HITL interrupts (D-084): when the executor returns ErrHITLInterrupted the
// conversation lock is intentionally NOT released so that no new task can
// conflict while the agent is paused. The lock expires naturally.
//
// lock may be nil to disable conversation-level serialization (backward compatible).
func NewAgentTaskHandler(
	executor *AgentExecutor,
	idempotency IdempotencyStore,
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
		var payload AgentProcessPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			logger.Error("agent task: unmarshal failed", "error", err, "payload", string(task.Payload))
			return nil // bad data, retry won't help
		}

		// 3. Validate required fields.
		// SenderID is intentionally not validated: the executor tolerates an empty
		// SenderID (broadcasts to an empty user are no-ops). The producer
		// (send_message.go) always populates it.
		if payload.MessageID == "" || payload.ConversationID == "" || payload.AgentID == "" {
			logger.Error("agent task: missing required fields",
				"message_id", payload.MessageID,
				"conversation_id", payload.ConversationID,
				"agent_id", payload.AgentID,
			)
			return nil
		}

		// 4. Acquire per-conversation lock (D-075).
		// For HITL interrupts (D-084) the lock is NOT released on pause.
		lockHeld := false
		if lock != nil {
			acquired, err := lock.Acquire(ctx, payload.ConversationID, 130*time.Second)
			if err != nil {
				logger.Error("conversation lock: acquire failed, proceeding without lock",
					"conversation_id", payload.ConversationID, "error", err)
				// fail-open (D-072 pattern)
			} else if !acquired {
				logger.Info("conversation lock: already held, requeueing",
					"conversation_id", payload.ConversationID)
				return fmt.Errorf("conversation lock held by another task, retrying later") // D-073: Asynq will retry with exponential backoff
			} else {
				lockHeld = true
			}
		}

		// Helper to release the lock when appropriate.
		releaseLock := func() {
			if lockHeld && lock != nil {
				if err := lock.Release(ctx, payload.ConversationID); err != nil {
					logger.Error("conversation lock: release failed",
						"conversation_id", payload.ConversationID, "error", err)
				}
			}
		}

		// 5. Two-phase idempotency check (D-121).
		// Phase 1: Check if already fully processed (replay protection).
		// Phase 2: Check if currently processing (concurrency protection).
		if idempotency != nil {
			processedKey := "agent:processed:" + payload.MessageID
			processingKey := "agent:processing:" + payload.MessageID

			processed, err1 := idempotency.CheckProcessed(ctx, processedKey)
			processing, err2 := idempotency.CheckProcessed(ctx, processingKey)

			// Fail-open: only skip if check succeeded and key exists (D-072).
			if (err1 == nil && processed) || (err2 == nil && processing) {
				logger.Debug("agent task: skipping duplicate/in-progress",
					"message_id", payload.MessageID)
				releaseLock()
				return nil
			}

			// Mark as in-progress with lock-matching TTL (130s, D-075).
			if _, err := idempotency.MarkProcessed(ctx, processingKey, 130*time.Second); err != nil {
				logger.Error("agent task: processing mark failed (non-fatal)", "error", err)
			}
		}

		// 6. Execute.
		execPayload := ExecutePayload{ //nolint:staticcheck // all fields used by executor
			MessageID:      payload.MessageID,
			ConversationID: payload.ConversationID,
			AgentID:        payload.AgentID,
			SenderID:       payload.SenderID,
			DeviceID:       payload.DeviceID, // Phase 6 (D-102)
		}
		execErr := executor.ExecuteWithErrorMessage(ctx, execPayload)

		// 7. HITL interrupt: hold lock, let processing key expire naturally (D-084).
		if execErr != nil && isHITLInterrupt(execErr) {
			logger.Info("agent task: HITL interrupted, holding conversation lock",
				"conversation_id", payload.ConversationID)
			return nil // D-073: always return nil to MQ
		}

		// Mark as processed (24h) for replay protection. Clean up processing key.
		// Only for non-transient errors — transient errors should allow retry.
		if idempotency != nil && (execErr == nil || !isTransientError(execErr)) {
			processedKey := "agent:processed:" + payload.MessageID
			processingKey := "agent:processing:" + payload.MessageID
			if _, err := idempotency.MarkProcessed(ctx, processedKey, 24*time.Hour); err != nil {
				logger.Error("agent task: processed mark failed (non-fatal)", "error", err)
			}
			_ = idempotency.DeleteKey(ctx, processingKey)
		}

		// Normal path: release the lock.
		releaseLock()

		// Invalidate context cache so retried tasks load fresh messages from DB.
		executor.InvalidateContextCache(payload.ConversationID)

		if execErr != nil {
			// Error already persisted as user-friendly message (D-067).
			logger.Error("agent task: execution failed", "error", execErr)
			// Return transient errors to MQ for retry (D-073 refinement).
			if isTransientError(execErr) {
				return execErr // Asynq retry; processing key expires at 130s
			}
		}

		return nil
	}
}

// isHITLInterrupt reports whether err wraps ErrHITLInterrupted.
func isHITLInterrupt(err error) bool {
	return errors.Is(err, ErrHITLInterrupted)
}

// isTransientError reports whether err is a transient failure that may
// succeed on retry (e.g. LLM timeout, rate limit). Permanent failures
// (unmarshal, agent not found, etc.) return false.
func isTransientError(err error) bool {
	return errors.Is(err, ErrLLMTimeout) || errors.Is(err, ErrLLMRateLimited)
}
