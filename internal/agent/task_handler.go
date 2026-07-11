package agent

import (
	"context"
	"encoding/json"
	"log"
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
}

// IdempotencyStore provides atomic check-and-set for agent task deduplication.
type IdempotencyStore interface {
	// MarkProcessed atomically checks if key was already processed and marks it.
	// Returns true if the key was already processed (duplicate), false if this is the first time.
	// TTL controls how long the processed marker persists.
	MarkProcessed(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// redisClient is the subset of redis.Client used by RedisIdempotencyStore.
type redisClient interface {
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd
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

// NewAgentTaskHandler returns an mq.TaskHandler-compatible function that
// processes TypeAgentProcess tasks.
//
// The handler:
//  1. Unmarshals the task payload into AgentProcessPayload
//  2. Checks idempotency (skip if already processed)
//  3. Calls AgentExecutor.ExecuteWithErrorMessage
//  4. Always returns nil to MQ (errors are persisted as user-friendly messages, D-067)
func NewAgentTaskHandler(
	executor *AgentExecutor,
	idempotency IdempotencyStore,
	logger *log.Logger,
) func(ctx context.Context, task *mq.Task) error {
	return func(ctx context.Context, task *mq.Task) error {
		// 1. Nil guard.
		if task == nil {
			return nil
		}

		// 2. Unmarshal payload.
		var payload AgentProcessPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			logger.Printf("agent task: unmarshal: %v (payload: %.200s)", err, task.Payload)
			return nil // bad data, retry won't help
		}

		// 3. Validate required fields.
		// SenderID is intentionally not validated: the executor tolerates an empty
		// SenderID (broadcasts to an empty user are no-ops). The producer
		// (send_message.go) always populates it.
		if payload.MessageID == "" || payload.ConversationID == "" || payload.AgentID == "" {
			logger.Printf("agent task: missing required fields: %+v", payload)
			return nil
		}

		// 4. Idempotency check (Redis SETNX, 24h TTL).
		if idempotency != nil {
			dup, err := idempotency.MarkProcessed(ctx, "agent:processed:"+payload.MessageID, 24*time.Hour)
			if err != nil {
				logger.Printf("agent task: idempotency check: %v", err)
				// Continue processing — fail-open for idempotency.
			} else if dup {
				logger.Printf("agent task: skipping duplicate message_id=%s", payload.MessageID)
				return nil
			}
		}

		// 5. Execute.
		execPayload := ExecutePayload{
			MessageID:      payload.MessageID,
			ConversationID: payload.ConversationID,
			AgentID:        payload.AgentID,
			SenderID:       payload.SenderID,
		}
		if err := executor.ExecuteWithErrorMessage(ctx, execPayload); err != nil {
			// Error already persisted as user-friendly message (D-067).
			// Return nil to prevent MQ retry — the error is terminal.
			logger.Printf("agent task: execution failed: %v", err)
		}

		return nil
	}
}
