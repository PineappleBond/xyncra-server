package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/redis/go-redis/v9"
)

// --------------------------------------------------------------------------
// Redis Pub/Sub broadcaster for cross-node message routing (D-018)
// --------------------------------------------------------------------------
//
// Channel layout:
//
//	xyncra:broadcast:{userID}   -- per-user broadcast channel
//
// Subscribe uses PSubscribe on "xyncra:broadcast:*" to receive all user
// channels on this node. The payload is a JSON-encoded broadcastMessage.

const (
	// redisBroadcastPrefix is the channel prefix for cross-node broadcasts.
	redisBroadcastPrefix = ":broadcast:"
)

// broadcastMessage is the envelope published on each broadcast channel.
type broadcastMessage struct {
	SourceNodeID string                       `json:"source_node_id"`
	Updates      *protocol.PackageDataUpdates `json:"updates"`
}

// RedisNodeBroadcaster implements NodeBroadcaster using Redis Pub/Sub for
// cross-node message delivery.
//
// The zero value is not usable; use NewRedisNodeBroadcaster to create an
// instance.
type RedisNodeBroadcaster struct {
	client    *redis.Client
	keyPrefix string
	ps        *redis.PubSub
	mu        sync.Mutex
}

// Ensure RedisNodeBroadcaster implements NodeBroadcaster at compile time.
var _ NodeBroadcaster = (*RedisNodeBroadcaster)(nil)

// NewRedisNodeBroadcaster creates a RedisNodeBroadcaster that uses the given
// Redis client for Pub/Sub operations. The client must be a dedicated
// connection owned by the caller (Pub/Sub requires an exclusive connection).
//
// If keyPrefix is empty, "xyncra" is used.
func NewRedisNodeBroadcaster(client *redis.Client, keyPrefix string) *RedisNodeBroadcaster {
	if keyPrefix == "" {
		keyPrefix = "xyncra"
	}
	return &RedisNodeBroadcaster{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// Publish sends a broadcast message for the given userID on the Redis
// channel "{keyPrefix}:broadcast:{userID}". The payload includes the
// sourceNodeID so that receiving nodes can skip messages they originated.
func (b *RedisNodeBroadcaster) Publish(ctx context.Context, userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) error {
	if userID == "" {
		return fmt.Errorf("server: broadcast publish: user ID is required")
	}

	msg := broadcastMessage{
		SourceNodeID: sourceNodeID,
		Updates:      updates,
	}

	payload, err := jsonMarshal(msg)
	if err != nil {
		return fmt.Errorf("server: broadcast publish: marshal payload: %w", err)
	}

	channel := b.channelForUser(userID)

	if err := b.client.Publish(ctx, channel, payload).Err(); err != nil {
		return fmt.Errorf("server: broadcast publish [userID=%s]: %w", userID, err)
	}

	return nil
}

// Subscribe starts listening for broadcast messages from other nodes using
// Redis PSubscribe on the pattern "{keyPrefix}:broadcast:*". For each
// received message, the callback is invoked with the extracted userID, the
// deserialised updates, and the originating sourceNodeID.
//
// Subscribe blocks until ctx is cancelled, then closes the Pub/Sub
// subscription and returns ctx.Err().
func (b *RedisNodeBroadcaster) Subscribe(ctx context.Context, callback func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string)) error {
	pattern := b.keyPrefix + redisBroadcastPrefix + "*"

	ps := b.client.PSubscribe(ctx, pattern)
	defer ps.Close()

	b.mu.Lock()
	b.ps = ps
	b.mu.Unlock()

	ch := ps.Channel()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}

			userID := b.extractUserID(msg.Channel)

			var bm broadcastMessage
			if err := jsonUnmarshal([]byte(msg.Payload), &bm); err != nil {
				// Skip malformed messages rather than aborting the loop.
				continue
			}

			callback(userID, bm.Updates, bm.SourceNodeID)
		}
	}
}

// Close releases the Pub/Sub subscription. It is safe to call multiple times.
func (b *RedisNodeBroadcaster) Close() error {
	b.mu.Lock()
	ps := b.ps
	b.ps = nil // Prevent duplicate close attempts.
	b.mu.Unlock()

	if ps != nil {
		if err := ps.Close(); err != nil {
			return fmt.Errorf("server: close broadcast pubsub: %w", err)
		}
	}
	return nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// channelForUser returns the Redis channel name for the given userID.
func (b *RedisNodeBroadcaster) channelForUser(userID string) string {
	return b.keyPrefix + redisBroadcastPrefix + userID
}

// extractUserID extracts the userID from a channel name of the form
// "{keyPrefix}:broadcast:{userID}".
func (b *RedisNodeBroadcaster) extractUserID(channel string) string {
	prefix := b.keyPrefix + redisBroadcastPrefix
	return strings.TrimPrefix(channel, prefix)
}
