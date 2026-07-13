package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// --------------------------------------------------------------------------
// Redis client interface
// --------------------------------------------------------------------------

// redisPendingClient is the subset of redis.Client used by RedisPendingStore.
// Defining an interface allows unit tests to supply a fake implementation
// without spinning up a real Redis server.
type redisPendingClient interface {
	RPush(ctx context.Context, key string, values ...any) *redis.IntCmd
	LTrim(ctx context.Context, key string, start, stop int64) *redis.StatusCmd
	LRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
	Pipeline() redis.Pipeliner
}

// --------------------------------------------------------------------------
// RedisPendingStore
// --------------------------------------------------------------------------

// RedisPendingStore implements PendingStore backed by Redis Lists (D-103).
// Each device has a Redis list under the key "pending:{userID}\x00{deviceID}"
// containing JSON-encoded PendingRequest values. The list is bounded by
// MaxPendingPerDevice (oldest entries are trimmed) and has a configurable TTL.
type RedisPendingStore struct {
	client redisPendingClient
	cfg    PendingStoreConfig
}

// Ensure RedisPendingStore implements PendingStore at compile time.
var _ PendingStore = (*RedisPendingStore)(nil)

// NewRedisPendingStore creates a RedisPendingStore. Zero-valued cfg fields
// are filled with sensible defaults (D-001 zero-config).
func NewRedisPendingStore(client redisPendingClient, cfg PendingStoreConfig) *RedisPendingStore {
	cfg.resolveDefaults()
	return &RedisPendingStore{
		client: client,
		cfg:    cfg,
	}
}

// deviceKey returns the Redis key for a device's pending request list.
func (s *RedisPendingStore) deviceKey(userID, deviceID string) string {
	return "pending:" + userID + "\x00" + deviceID
}

// Save persists a pending request by appending it to the device's list.
// If the list exceeds MaxPendingPerDevice, the oldest entries are trimmed.
func (s *RedisPendingStore) Save(ctx context.Context, req *PendingRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("pending store: marshal request: %w", err)
	}

	key := s.deviceKey(req.UserID, req.DeviceID)

	pipe := s.client.Pipeline()
	pipe.RPush(ctx, key, data)
	pipe.LTrim(ctx, key, int64(-s.cfg.MaxPendingPerDevice), -1)
	pipe.Expire(ctx, key, s.cfg.RequestTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("pending store: save: %w", err)
	}
	return nil
}

// List returns all pending requests for a device, ordered by Seq ascending
// (insertion order). Returns an empty slice if there are no pending requests.
func (s *RedisPendingStore) List(ctx context.Context, userID, deviceID string) ([]*PendingRequest, error) {
	key := s.deviceKey(userID, deviceID)
	strs, err := s.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("pending store: list: %w", err)
	}

	result := make([]*PendingRequest, 0, len(strs))
	for _, raw := range strs {
		var req PendingRequest
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			// Skip corrupted entries rather than failing the entire list.
			continue
		}
		result = append(result, &req)
	}
	return result, nil
}

// Remove deletes a specific pending request by ID.
// It is a no-op if the request does not exist.
//
// NOTE: Del+RPush in a pipeline is not a true transaction. If the process
// crashes between the two commands, entries may be lost. This is acceptable
// for a pending store (fail-open semantics, D-103).
func (s *RedisPendingStore) Remove(ctx context.Context, userID, deviceID, requestID string) error {
	key := s.deviceKey(userID, deviceID)

	// Read all entries, filter out the target, and rewrite.
	strs, err := s.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("pending store: remove (read): %w", err)
	}

	var kept []string
	found := false
	for _, raw := range strs {
		var req PendingRequest
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			// Skip corrupted entries.
			continue
		}
		if req.ID == requestID {
			found = true
			continue
		}
		kept = append(kept, raw)
	}

	if !found {
		return nil // no-op
	}

	// Rewrite the list atomically using a pipeline.
	pipe := s.client.Pipeline()
	pipe.Del(ctx, key)
	if len(kept) > 0 {
		vals := make([]any, len(kept))
		for i, v := range kept {
			vals[i] = v
		}
		pipe.RPush(ctx, key, vals...)
		pipe.Expire(ctx, key, s.cfg.RequestTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("pending store: remove (write): %w", err)
	}
	return nil
}

// Update replaces an existing pending request (matched by ID) with the
// provided version. This is used to update mutable fields like RetryCount.
// No-op if the request does not exist.
//
// NOTE: Del+RPush in a pipeline is not a true transaction. If the process
// crashes between the two commands, entries may be lost. This is acceptable
// for a pending store (fail-open semantics, D-103).
func (s *RedisPendingStore) Update(ctx context.Context, req *PendingRequest) error {
	key := s.deviceKey(req.UserID, req.DeviceID)

	newData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("pending store: update: marshal request: %w", err)
	}

	// Read all entries, replace the target, and rewrite.
	strs, err := s.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("pending store: update (read): %w", err)
	}

	var kept []string
	found := false
	for _, raw := range strs {
		var existing PendingRequest
		if err := json.Unmarshal([]byte(raw), &existing); err != nil {
			// Skip corrupted entries.
			continue
		}
		if existing.ID == req.ID {
			found = true
			kept = append(kept, string(newData))
			continue
		}
		kept = append(kept, raw)
	}

	if !found {
		return nil // no-op
	}

	// Rewrite the list using a pipeline.
	pipe := s.client.Pipeline()
	pipe.Del(ctx, key)
	if len(kept) > 0 {
		vals := make([]any, len(kept))
		for i, v := range kept {
			vals[i] = v
		}
		pipe.RPush(ctx, key, vals...)
		pipe.Expire(ctx, key, s.cfg.RequestTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("pending store: update (write): %w", err)
	}
	return nil
}

// RemoveByDevice deletes all pending requests for a device.
func (s *RedisPendingStore) RemoveByDevice(ctx context.Context, userID, deviceID string) error {
	key := s.deviceKey(userID, deviceID)
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("pending store: remove by device: %w", err)
	}
	return nil
}
