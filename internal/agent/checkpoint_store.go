// CheckPointStore implements Eino's compose.CheckpointStore using Redis (D-083).
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisCheckpointClient is the subset of redis.Client used by
// RedisCheckPointStore. Defining an interface allows unit tests to supply a
// fake implementation without spinning up a real Redis server.
type redisCheckpointClient interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
}

// RedisCheckPointStore implements compose.CheckPointStore (Eino) backed by
// Redis. Checkpoints are stored as raw bytes under a configurable key prefix
// with a TTL to bound storage growth.
//
// Design notes:
//   - D-083: HITL does NOT support fail-open. All Redis errors propagate to
//     the caller so the executor can abort the HITL flow.
//   - D-074: uses an independent redis.Client (same pattern as the Pub/Sub
//     client and idempotency client).
type RedisCheckPointStore struct {
	client    redisCheckpointClient
	keyPrefix string
	ttl       time.Duration
}

// NewRedisCheckPointStore creates a RedisCheckPointStore. If keyPrefix is
// empty it defaults to "agent:checkpoint:". If ttl <= 0 it defaults to 24 h.
func NewRedisCheckPointStore(client redisCheckpointClient, keyPrefix string, ttl time.Duration) *RedisCheckPointStore {
	if keyPrefix == "" {
		keyPrefix = "agent:checkpoint:"
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &RedisCheckPointStore{
		client:    client,
		keyPrefix: keyPrefix,
		ttl:       ttl,
	}
}

// Get retrieves a checkpoint by its ID.
//
// Returns (nil, false, nil) when the key does not exist (redis.Nil). Any other
// Redis error is returned to the caller (D-083: fail-closed).
func (s *RedisCheckPointStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	val, err := s.client.Get(ctx, s.keyPrefix+key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checkpoint store get: %w", err)
	}
	return []byte(val), true, nil
}

// Set persists a checkpoint value under the given key with the configured TTL.
func (s *RedisCheckPointStore) Set(ctx context.Context, key string, value []byte) error {
	if err := s.client.Set(ctx, s.keyPrefix+key, value, s.ttl).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrCheckpointStoreSet, err)
	}
	return nil
}
