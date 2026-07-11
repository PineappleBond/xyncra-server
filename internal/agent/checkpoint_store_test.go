package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock Redis client for CheckPointStore
// ---------------------------------------------------------------------------

type mockCheckpointRedis struct {
	data    map[string]string
	ttls    map[string]time.Duration
	failGet bool
	failSet bool
}

func newMockCheckpointRedis() *mockCheckpointRedis {
	return &mockCheckpointRedis{
		data: make(map[string]string),
		ttls: make(map[string]time.Duration),
	}
}

func (m *mockCheckpointRedis) Get(_ context.Context, key string) *redis.StringCmd {
	if m.failGet {
		cmd := redis.NewStringCmd(context.Background(), "get", key)
		cmd.SetErr(redis.ErrClosed) // simulate Redis failure
		return cmd
	}
	val, ok := m.data[key]
	cmd := redis.NewStringCmd(context.Background(), "get", key)
	if !ok {
		cmd.SetErr(redis.Nil)
		return cmd
	}
	cmd.SetVal(val)
	return cmd
}

func (m *mockCheckpointRedis) Set(_ context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd {
	if m.failSet {
		cmd := redis.NewStatusCmd(context.Background(), "set", key, value)
		cmd.SetErr(redis.ErrClosed)
		return cmd
	}
	// The real Redis client passes the value as-is; we store it for retrieval.
	switch v := value.(type) {
	case string:
		m.data[key] = v
	case []byte:
		m.data[key] = string(v)
	default:
		m.data[key] = fmt.Sprintf("%v", v)
	}
	m.ttls[key] = expiration
	cmd := redis.NewStatusCmd(context.Background(), "set", key, value)
	cmd.SetVal("OK")
	return cmd
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRedisCheckPointStore_SetGet(t *testing.T) {
	mock := newMockCheckpointRedis()
	store := NewRedisCheckPointStore(mock, "", 0) // defaults

	ctx := context.Background()
	err := store.Set(ctx, "cp-1", []byte("checkpoint-data"))
	require.NoError(t, err)

	val, existed, err := store.Get(ctx, "cp-1")
	require.NoError(t, err)
	assert.True(t, existed)
	assert.Equal(t, "checkpoint-data", string(val))
}

func TestRedisCheckPointStore_GetNotFound(t *testing.T) {
	mock := newMockCheckpointRedis()
	store := NewRedisCheckPointStore(mock, "", 0)

	val, existed, err := store.Get(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.False(t, existed)
	assert.Nil(t, val)
}

func TestRedisCheckPointStore_DefaultPrefix(t *testing.T) {
	mock := newMockCheckpointRedis()
	store := NewRedisCheckPointStore(mock, "", 0)

	_ = store.Set(context.Background(), "key1", []byte("data"))

	// Verify the key has the default prefix.
	_, ok := mock.data["agent:checkpoint:key1"]
	assert.True(t, ok, "should use default prefix 'agent:checkpoint:'")
}

func TestRedisCheckPointStore_CustomPrefix(t *testing.T) {
	mock := newMockCheckpointRedis()
	store := NewRedisCheckPointStore(mock, "custom:", time.Hour)

	_ = store.Set(context.Background(), "key1", []byte("data"))

	_, ok := mock.data["custom:key1"]
	assert.True(t, ok, "should use custom prefix")
	assert.Equal(t, time.Hour, mock.ttls["custom:key1"])
}

func TestRedisCheckPointStore_DefaultTTL(t *testing.T) {
	mock := newMockCheckpointRedis()
	store := NewRedisCheckPointStore(mock, "", 0) // ttl=0 → default 24h

	_ = store.Set(context.Background(), "key1", []byte("data"))
	assert.Equal(t, 24*time.Hour, mock.ttls["agent:checkpoint:key1"])
}

func TestRedisCheckPointStore_GetRedisError(t *testing.T) {
	mock := newMockCheckpointRedis()
	mock.failGet = true
	store := NewRedisCheckPointStore(mock, "", 0)

	_, _, err := store.Get(context.Background(), "key1")
	assert.Error(t, err, "D-083: Redis errors must propagate (fail-closed)")
}

func TestRedisCheckPointStore_SetRedisError(t *testing.T) {
	mock := newMockCheckpointRedis()
	mock.failSet = true
	store := NewRedisCheckPointStore(mock, "", 0)

	err := store.Set(context.Background(), "key1", []byte("data"))
	assert.Error(t, err, "D-083: Redis errors must propagate (fail-closed)")
}

func TestRedisCheckPointStore_LargePayload(t *testing.T) {
	mock := newMockCheckpointRedis()
	store := NewRedisCheckPointStore(mock, "", 0)

	large := make([]byte, 1024*1024) // 1MB
	for i := range large {
		large[i] = byte(i % 256)
	}

	err := store.Set(context.Background(), "large", large)
	require.NoError(t, err)

	val, existed, err := store.Get(context.Background(), "large")
	require.NoError(t, err)
	assert.True(t, existed)
	assert.Equal(t, large, val)
}
