package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConversationLock provides distributed per-conversation serialization.
type ConversationLock interface {
	// Acquire attempts to acquire the lock for the given conversation.
	// Returns (true, nil) if acquired, (false, nil) if already held,
	// (false, err) on Redis error.
	Acquire(ctx context.Context, conversationID string, ttl time.Duration) (bool, error)
	// Release releases the lock. Only the owner can release it.
	Release(ctx context.Context, conversationID string) error
}

// redisLockClient is the minimal Redis interface needed for conversation locks.
// *redis.Client satisfies this interface.
type redisLockClient interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd
}

// RedisConversationLock implements ConversationLock using Redis SETNX.
type RedisConversationLock struct {
	client redisLockClient
	token  string // unique token for this lock holder
}

// NewRedisConversationLock creates a lock backed by Redis.
// The client must implement both SetNX and Eval.
func NewRedisConversationLock(client redisLockClient) *RedisConversationLock {
	return &RedisConversationLock{
		client: client,
		token:  generateToken(),
	}
}

// lockKey returns the Redis key for a conversation lock.
func lockKey(conversationID string) string {
	return "agent:lock:" + conversationID
}

// Acquire uses SETNX with a unique token as value.
func (l *RedisConversationLock) Acquire(ctx context.Context, conversationID string, ttl time.Duration) (bool, error) {
	ok, err := l.client.SetNX(ctx, lockKey(conversationID), l.token, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("conversation lock acquire: %w", err)
	}
	return ok, nil
}

// releaseScript is a Lua script that atomically checks the token before deleting.
const releaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
`

// Release uses a Lua script to check token match before DEL.
func (l *RedisConversationLock) Release(ctx context.Context, conversationID string) error {
	_, err := l.client.Eval(ctx, releaseScript, []string{lockKey(conversationID)}, l.token).Result()
	if err != nil {
		return fmt.Errorf("conversation lock release: %w", err)
	}
	return nil
}

// generateToken creates a random hex token for lock ownership.
func generateToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
