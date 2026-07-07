package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MemoryConnectionStore implements ConnectionStore using an in-memory map. It
// is designed for unit tests that do not require Redis. It is safe for
// concurrent use.
//
// MemoryConnectionStore is NOT suitable for production: data is lost when the
// process exits, and there is no cross-instance synchronization.
type MemoryConnectionStore struct {
	mu              sync.RWMutex
	infoByKey       map[string]*ConnectionInfo // connID -> info
	userSet         map[string]map[string]bool // userID -> set of connIDs
	maxConnsPerUser int
}

// Ensure MemoryConnectionStore implements ConnectionStore at compile time.
var _ ConnectionStore = (*MemoryConnectionStore)(nil)

// NewMemoryConnectionStore creates an in-memory ConnectionStore for testing.
// Set maxConnsPerUser to 0 for unlimited connections.
func NewMemoryConnectionStore(maxConnsPerUser int) *MemoryConnectionStore {
	return &MemoryConnectionStore{
		infoByKey:       make(map[string]*ConnectionInfo),
		userSet:         make(map[string]map[string]bool),
		maxConnsPerUser: maxConnsPerUser,
	}
}

// Add registers a new connection or updates an existing one.
func (s *MemoryConnectionStore) Add(_ context.Context, info *ConnectionInfo) error {
	if info == nil {
		return fmt.Errorf("server: connection info is nil")
	}
	if info.ID == "" {
		return fmt.Errorf("server: connection ID is required")
	}
	if info.UserID == "" {
		return fmt.Errorf("server: user ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check per-user limit for new connections.
	if _, exists := s.infoByKey[info.ID]; !exists {
		if s.maxConnsPerUser > 0 {
			userConns := s.userSet[info.UserID]
			if len(userConns) >= s.maxConnsPerUser {
				return fmt.Errorf("server: add connection [connID=%s]: %w",
					info.ID, ErrMaxConnectionsExceeded)
			}
		}
	}

	// Clean up old user's set if UserID changed.
	if old, exists := s.infoByKey[info.ID]; exists && old.UserID != info.UserID {
		if userConns, ok := s.userSet[old.UserID]; ok {
			delete(userConns, info.ID)
			if len(userConns) == 0 {
				delete(s.userSet, old.UserID)
			}
		}
	}

	now := time.Now()
	if info.CreatedAt.IsZero() {
		info.CreatedAt = now
	}
	info.UpdatedAt = now

	// Deep copy via JSON round-trip to prevent callers from mutating our state.
	stored := deepCopyConnectionInfo(info)
	s.infoByKey[info.ID] = stored

	if s.userSet[info.UserID] == nil {
		s.userSet[info.UserID] = make(map[string]bool)
	}
	s.userSet[info.UserID][info.ID] = true

	// Reflect CreatedAt back to caller (matches Redis behavior).
	info.CreatedAt = stored.CreatedAt
	info.UpdatedAt = stored.UpdatedAt

	return nil
}

// Get retrieves the connection info for the given connection ID.
func (s *MemoryConnectionStore) Get(_ context.Context, connID string) (*ConnectionInfo, error) {
	if connID == "" {
		return nil, fmt.Errorf("server: connection ID is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	info, ok := s.infoByKey[connID]
	if !ok {
		return nil, ErrConnectionNotFound
	}
	return deepCopyConnectionInfo(info), nil
}

// Remove deletes the connection identified by connID.
func (s *MemoryConnectionStore) Remove(_ context.Context, connID string) error {
	if connID == "" {
		return fmt.Errorf("server: connection ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.infoByKey[connID]
	if !ok {
		return nil // no-op
	}

	userID := info.UserID
	delete(s.infoByKey, connID)
	if userConns, ok := s.userSet[userID]; ok {
		delete(userConns, connID)
		if len(userConns) == 0 {
			delete(s.userSet, userID)
		}
	}
	return nil
}

// Exists reports whether a connection with the given ID is currently active.
func (s *MemoryConnectionStore) Exists(_ context.Context, connID string) (bool, error) {
	if connID == "" {
		return false, fmt.Errorf("server: connection ID is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.infoByKey[connID]
	return ok, nil
}

// Update modifies the metadata of an existing connection.
func (s *MemoryConnectionStore) Update(_ context.Context, connID string, metadata map[string]string) error {
	if connID == "" {
		return fmt.Errorf("server: connection ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.infoByKey[connID]
	if !ok {
		return ErrConnectionNotFound
	}
	info.Metadata = metadata
	info.UpdatedAt = time.Now()
	return nil
}

// Patch applies an updater function to an existing connection.
func (s *MemoryConnectionStore) Patch(_ context.Context, connID string, updater func(*ConnectionInfo)) error {
	if connID == "" {
		return fmt.Errorf("server: connection ID is required")
	}
	if updater == nil {
		return fmt.Errorf("server: updater function is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.infoByKey[connID]
	if !ok {
		return ErrConnectionNotFound
	}
	updater(info)
	info.UpdatedAt = time.Now()
	return nil
}

// Refresh resets the UpdatedAt of the connection, extending its logical lifetime.
func (s *MemoryConnectionStore) Refresh(_ context.Context, connID string) error {
	if connID == "" {
		return fmt.Errorf("server: connection ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.infoByKey[connID]
	if !ok {
		return ErrConnectionNotFound
	}
	info.UpdatedAt = time.Now()
	return nil
}

// ListByUser returns active connections for the given user, up to limit.
func (s *MemoryConnectionStore) ListByUser(_ context.Context, userID string, limit int) ([]*ConnectionInfo, error) {
	if userID == "" {
		return nil, fmt.Errorf("server: user ID is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	connIDs := s.userSet[userID]
	if len(connIDs) == 0 {
		return []*ConnectionInfo{}, nil
	}

	var result []*ConnectionInfo
	for id := range connIDs {
		if info, ok := s.infoByKey[id]; ok {
			result = append(result, deepCopyConnectionInfo(info))
		}
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	if result == nil {
		return []*ConnectionInfo{}, nil
	}
	return result, nil
}

// CountByUser returns the number of connections for the given user.
func (s *MemoryConnectionStore) CountByUser(_ context.Context, userID string) (int64, error) {
	if userID == "" {
		return 0, fmt.Errorf("server: user ID is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return int64(len(s.userSet[userID])), nil
}

// CountAll returns the total number of active connections.
func (s *MemoryConnectionStore) CountAll(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return int64(len(s.infoByKey)), nil
}

// RemoveByUser removes all connections for the given user.
func (s *MemoryConnectionStore) RemoveByUser(_ context.Context, userID string) (int64, error) {
	if userID == "" {
		return 0, fmt.Errorf("server: user ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connIDs := s.userSet[userID]
	count := int64(len(connIDs))
	for id := range connIDs {
		delete(s.infoByKey, id)
	}
	delete(s.userSet, userID)
	return count, nil
}

// Ping is a no-op for the in-memory store; it always returns nil.
func (s *MemoryConnectionStore) Ping(_ context.Context) error {
	return nil
}

// Close is a no-op for the in-memory store.
func (s *MemoryConnectionStore) Close() error {
	return nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// deepCopyConnectionInfo makes a deep copy of a ConnectionInfo via JSON
// round-trip to prevent callers from mutating store state.
func deepCopyConnectionInfo(info *ConnectionInfo) *ConnectionInfo {
	data, _ := json.Marshal(info)
	var copy ConnectionInfo
	_ = json.Unmarshal(data, &copy)
	return &copy
}
