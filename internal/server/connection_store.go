package server

import (
	"context"
	"time"
)

// --------------------------------------------------------------------------
// ConnectionInfo model
// --------------------------------------------------------------------------

// ConnectionInfo represents the metadata for a single active client connection.
// It is stored in the ConnectionStore and used to track connected users,
// route messages, and manage session state.
type ConnectionInfo struct {
	// ID is the unique identifier for this connection. Typically set to a
	// UUID or a server-assigned session token.
	ID string `json:"id"`

	// UserID is the authenticated user that owns this connection.
	UserID string `json:"user_id"`

	// SessionID identifies the session this connection belongs to. A single
	// user may have multiple sessions (e.g. across devices).
	SessionID string `json:"session_id"`

	// DeviceID identifies the specific device for this connection. Used to
	// implement multi-device login or single-device login policies.
	DeviceID string `json:"device_id,omitempty"`

	// DeviceType describes the type of device (e.g. "ios", "android",
	// "web", "desktop").
	DeviceType string `json:"device_type,omitempty"`

	// IPAddress is the remote IP address of the connected client.
	IPAddress string `json:"ip_address,omitempty"`

	// Protocol is the transport protocol used by the connection (e.g.
	// "tcp", "websocket", "quic").
	Protocol string `json:"protocol,omitempty"`

	// LastHeartbeatAt is the time of the last heartbeat received from the
	// client. This is updated independently of UpdatedAt to allow fine-grained
	// liveness tracking.
	LastHeartbeatAt time.Time `json:"last_heartbeat_at,omitempty"`

	// Status indicates the connection state (e.g. "active", "idle",
	// "suspended").
	Status string `json:"status,omitempty"`

	// Metadata holds arbitrary key-value pairs associated with the
	// connection (e.g. client version, platform).
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt is the time the connection was first registered.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the time the connection metadata was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// TTL is the time-to-live for the connection. If zero, the
	// ConnectionStore's default TTL is used. The connection is
	// automatically evicted after TTL has elapsed since the last update.
	TTL time.Duration `json:"ttl,omitempty"`
}

// IsExpired reports whether the connection has exceeded its TTL relative to
// the given time.
func (ci *ConnectionInfo) IsExpired(now time.Time) bool {
	if ci.TTL <= 0 {
		return false
	}
	return now.After(ci.UpdatedAt.Add(ci.TTL))
}

// --------------------------------------------------------------------------
// ConnectionStore interface
// --------------------------------------------------------------------------

// ConnectionStore manages the lifecycle of active client connections. It
// provides CRUD operations, user-level indexing (list all connections for a
// user), and health checks. Implementations must be safe for concurrent use.
type ConnectionStore interface {
	// Add registers a new connection or updates an existing one. If a
	// connection with the same ID already exists, it is overwritten.
	// The connection's TTL determines how long it persists without
	// updates.
	//
	// If MaxConnectionsPerUser is configured and the user already has
	// that many active connections, Add returns
	// ErrMaxConnectionsExceeded (checked atomically inside the
	// implementation).
	//
	// The UserID field should be considered immutable after the initial
	// Add. Overwriting with a different UserID is supported and will
	// clean up the old user's index, but callers should prefer Add for
	// new user assignments.
	Add(ctx context.Context, info *ConnectionInfo) error

	// Get retrieves the connection info for the given connection ID.
	// Returns ErrConnectionNotFound if the connection does not exist or
	// has expired.
	Get(ctx context.Context, connID string) (*ConnectionInfo, error)

	// Remove deletes the connection identified by connID. It is a no-op
	// if the connection does not exist.
	//
	// Note: Remove first reads the connection info to determine the
	// owning user for set cleanup. If the info key expires between the
	// read and the deletion, the user-set entry is still removed
	// (cleaning up what would be an orphaned entry).
	Remove(ctx context.Context, connID string) error

	// Exists reports whether a connection with the given ID is currently
	// active.
	Exists(ctx context.Context, connID string) (bool, error)

	// Update modifies the metadata of an existing connection. Returns
	// ErrConnectionNotFound if the connection does not exist.
	//
	// Note: Update performs a non-atomic read-modify-write. Concurrent
	// Update calls may silently overwrite each other's metadata changes.
	// For atomic read-modify-write semantics, use Patch.
	Update(ctx context.Context, connID string, metadata map[string]string) error

	// Patch applies an updater function to an existing connection. The
	// updater receives a copy of the current ConnectionInfo and may
	// modify any field. The modified copy is written back, but the
	// read-modify-write cycle is NOT fully atomic: the updater runs in
	// the client process and concurrent Patch calls on the same
	// connection may silently overwrite each other.
	//
	// The UserID and ID fields should not be modified by the updater.
	// Changing UserID updates the info key but does NOT update the
	// per-user connection set index.
	//
	// Returns ErrConnectionNotFound if the connection does not exist.
	Patch(ctx context.Context, connID string, updater func(*ConnectionInfo)) error

	// Refresh resets the TTL of the connection, extending its lifetime.
	// Returns ErrConnectionNotFound if the connection does not exist.
	Refresh(ctx context.Context, connID string) error

	// ListByUser returns active connections for the given user, up to
	// limit results. If limit <= 0, all connections are returned.
	// Returns an empty slice (not an error) if the user has no active
	// connections.
	ListByUser(ctx context.Context, userID string, limit int) ([]*ConnectionInfo, error)

	// CountByUser returns the number of connections in the user's set.
	//
	// Note: this is an APPROXIMATE count. The underlying implementation
	// uses the cardinality of a Redis set (SCARD), which may include
	// entries whose info keys have expired but have not yet been cleaned
	// up. For an exact count of live connections, use ListByUser and
	// check the length of the returned slice.
	CountByUser(ctx context.Context, userID string) (int64, error)

	// CountAll returns the total number of active connections across all
	// users. This is an approximate count based on key scanning.
	CountAll(ctx context.Context) (int64, error)

	// RemoveByUser removes all connections for the given user.
	// Returns the number of user-set entries processed. Some info keys
	// may have already expired and been evicted by Redis before the deletion.
	RemoveByUser(ctx context.Context, userID string) (int64, error)

	// Ping verifies the underlying storage is reachable.
	Ping(ctx context.Context) error

	// Close releases any resources held by the ConnectionStore.
	Close() error
}
