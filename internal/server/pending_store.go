package server

import (
	"context"
	"encoding/json"
	"time"
)

// --------------------------------------------------------------------------
// PendingRequest model
// --------------------------------------------------------------------------

// PendingRequest represents a timed-out reverse-RPC request persisted to Redis
// for later replay when the device reconnects (Phase 4, D-103).
type PendingRequest struct {
	// ID is the unique request identifier (same as the original reqID).
	ID string `json:"id"`
	// UserID is the target user for this request.
	UserID string `json:"user_id"`
	// DeviceID is the target device for this request.
	DeviceID string `json:"device_id"`
	// Method is the RPC method name.
	Method string `json:"method"`
	// Params contains the method parameters as JSON.
	Params json.RawMessage `json:"params"`
	// IdempotencyKey is a server-generated deduplication key (equal to reqID).
	IdempotencyKey string `json:"idempotency_key"`
	// Seq is the per-device sequence number assigned at save time.
	Seq uint64 `json:"seq"`
	// RetryCount tracks how many times this request has been replayed.
	RetryCount int `json:"retry_count"`
	// MaxRetries is the maximum number of replay attempts allowed.
	MaxRetries int `json:"max_retries"`
	// CreatedAt is the time the pending request was first persisted.
	CreatedAt time.Time `json:"created_at"`
}

// Default configuration values for PendingStore (D-001 zero-config).
const (
	// defaultMaxPendingPerDevice is the default maximum number of pending
	// requests stored per device.
	defaultMaxPendingPerDevice = 50

	// defaultPendingRequestTTL is the default time-to-live for pending
	// requests in Redis.
	defaultPendingRequestTTL = 24 * time.Hour

	// defaultMaxReplayRetries is the default maximum number of replay
	// attempts for a pending request.
	defaultMaxReplayRetries = 3
)

// --------------------------------------------------------------------------
// PendingStoreConfig
// --------------------------------------------------------------------------

// PendingStoreConfig holds configuration for a PendingStore.
// Zero values use sensible defaults (D-001 zero-config).
type PendingStoreConfig struct {
	// MaxPendingPerDevice is the maximum number of pending requests stored
	// per device. Older entries are trimmed when this limit is exceeded.
	// Default: 50.
	MaxPendingPerDevice int

	// RequestTTL is the time-to-live for pending requests in Redis.
	// Default: 24h.
	RequestTTL time.Duration

	// MaxReplayRetries is the maximum number of replay attempts for a
	// pending request before it is discarded.
	// Default: 3.
	MaxReplayRetries int
}

// resolveDefaults fills zero-valued fields with sensible defaults.
func (c *PendingStoreConfig) resolveDefaults() {
	if c.MaxPendingPerDevice <= 0 {
		c.MaxPendingPerDevice = defaultMaxPendingPerDevice
	}
	if c.RequestTTL <= 0 {
		c.RequestTTL = defaultPendingRequestTTL
	}
	if c.MaxReplayRetries <= 0 {
		c.MaxReplayRetries = defaultMaxReplayRetries
	}
}

// --------------------------------------------------------------------------
// PendingStore interface
// --------------------------------------------------------------------------

// PendingStore persists timed-out reverse-RPC requests for later replay.
// All methods are safe for concurrent use.
// Fail-open: errors are returned but callers should log-and-continue (D-103).
type PendingStore interface {
	// Save persists a pending request. It is appended to the device's list.
	// If the list exceeds MaxPendingPerDevice, the oldest entries are trimmed.
	Save(ctx context.Context, req *PendingRequest) error

	// List returns all pending requests for a device, ordered by Seq ascending.
	// Returns an empty slice (not an error) if there are no pending requests.
	List(ctx context.Context, userID, deviceID string) ([]*PendingRequest, error)

	// Remove deletes a specific pending request by ID.
	// It is a no-op if the request does not exist.
	Remove(ctx context.Context, userID, deviceID, requestID string) error

	// RemoveByDevice deletes all pending requests for a device.
	RemoveByDevice(ctx context.Context, userID, deviceID string) error
}
