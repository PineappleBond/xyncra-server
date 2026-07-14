package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Resume handler cleanup tests (D-112, D-113)
//
// The cleanup logic (checkpoint deletion + interruptID cleanup) is embedded
// in the resume handler closure. These tests verify the building blocks
// (registerInterruptIDs / getInterruptIDs) and the DeletableCheckPointStore
// contract used by the cleanup path.
// ---------------------------------------------------------------------------

// fakeDeletableStore is a minimal DeletableCheckPointStore for cleanup tests.
type fakeDeletableStore struct {
	mu        sync.Mutex
	data      map[string][]byte
	deleteErr error // if non-nil, Delete returns this error
	deleteCnt int   // number of times Delete was called
}

func newFakeDeletableStore() *fakeDeletableStore {
	return &fakeDeletableStore{data: make(map[string][]byte)}
}

func (s *fakeDeletableStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	return v, ok, nil
}

func (s *fakeDeletableStore) Set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func (s *fakeDeletableStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCnt++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.data, key)
	return nil
}

// ---------------------------------------------------------------------------
// interruptIDs sync.Map tests (D-113)
// ---------------------------------------------------------------------------

// TestRegisterInterruptIDs_And_Get verifies the store-then-load round trip.
func TestRegisterInterruptIDs_And_Get(t *testing.T) {
	e := &AgentExecutor{}

	// Register IDs for a checkpoint.
	e.registerInterruptIDs("cp-1", []string{"intr-a", "intr-b"})

	// Retrieve them.
	got := e.getInterruptIDs("cp-1")
	assert.Equal(t, []string{"intr-a", "intr-b"}, got)
}

// TestGetInterruptIDs_NotFound returns nil for unknown checkpoint.
func TestGetInterruptIDs_NotFound(t *testing.T) {
	e := &AgentExecutor{}

	got := e.getInterruptIDs("nonexistent")
	assert.Nil(t, got)
}

// TestRegisterInterruptIDs_EmptyCheckpointID is a no-op.
func TestRegisterInterruptIDs_EmptyCheckpointID(t *testing.T) {
	e := &AgentExecutor{}

	e.registerInterruptIDs("", []string{"intr-a"})
	got := e.getInterruptIDs("")
	assert.Nil(t, got, "empty checkpoint ID should not be stored")
}

// TestRegisterInterruptIDs_EmptyIDs is a no-op.
func TestRegisterInterruptIDs_EmptyIDs(t *testing.T) {
	e := &AgentExecutor{}

	e.registerInterruptIDs("cp-1", nil)
	got := e.getInterruptIDs("cp-1")
	assert.Nil(t, got, "nil IDs should not be stored")

	e.registerInterruptIDs("cp-2", []string{})
	got = e.getInterruptIDs("cp-2")
	assert.Nil(t, got, "empty IDs slice should not be stored")
}

// TestInterruptIDs_Delete verifies that interruptIDs.Delete removes the entry.
func TestInterruptIDs_Delete(t *testing.T) {
	e := &AgentExecutor{}

	// Register then delete.
	e.registerInterruptIDs("cp-1", []string{"intr-a"})
	e.interruptIDs.Delete("cp-1")

	got := e.getInterruptIDs("cp-1")
	assert.Nil(t, got, "interrupt IDs should be nil after Delete")
}

// TestInterruptIDs_Overwrite verifies that a second register overwrites the first.
func TestInterruptIDs_Overwrite(t *testing.T) {
	e := &AgentExecutor{}

	e.registerInterruptIDs("cp-1", []string{"intr-a"})
	e.registerInterruptIDs("cp-1", []string{"intr-b", "intr-c"})

	got := e.getInterruptIDs("cp-1")
	assert.Equal(t, []string{"intr-b", "intr-c"}, got)
}

// ---------------------------------------------------------------------------
// DeletableCheckPointStore contract tests (D-112)
// ---------------------------------------------------------------------------

// TestDeletableCheckPointStore_Interface verifies that RedisCheckPointStore
// satisfies the DeletableCheckPointStore interface.
func TestDeletableCheckPointStore_Interface(t *testing.T) {
	var _ DeletableCheckPointStore = (*RedisCheckPointStore)(nil)
}

// TestFakeDeletableStore_SetGetDelete exercises the fake store used in
// resume handler integration tests.
func TestFakeDeletableStore_SetGetDelete(t *testing.T) {
	s := newFakeDeletableStore()
	ctx := context.Background()

	// Set then Get.
	require.NoError(t, s.Set(ctx, "key1", []byte("val1")))
	got, ok, err := s.Get(ctx, "key1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "val1", string(got))

	// Delete.
	require.NoError(t, s.Delete(ctx, "key1"))
	_, ok, err = s.Get(ctx, "key1")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestFakeDeletableStore_DeleteError verifies that the fake store can
// simulate Delete failures.
func TestFakeDeletableStore_DeleteError(t *testing.T) {
	s := newFakeDeletableStore()
	s.deleteErr = fmt.Errorf("simulated redis failure")

	err := s.Delete(context.Background(), "key1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated redis failure")
	assert.Equal(t, 1, s.deleteCnt, "Delete should have been called once")
}

// TestWithCheckPointStore_Option verifies that the WithCheckPointStore option
// correctly sets the checkpointStore field on AgentExecutor.
func TestWithCheckPointStore_Option(t *testing.T) {
	store := newFakeDeletableStore()

	// Create an executor with the option.
	e := NewAgentExecutor(
		nil, // registry
		nil, // contextManager
		nil, // agentBuilder
		nil, // streamBridge
		nil, // broadcaster
		nil, // store
		0,   // maxConcurrent
		nil, // logger
		WithCheckPointStore(store),
	)

	require.NotNil(t, e.checkpointStore, "checkpointStore should be set by WithCheckPointStore")
	assert.Equal(t, store, e.checkpointStore)
}

// TestWithCheckPointStore_NilOption verifies that not using WithCheckPointStore
// leaves checkpointStore as nil.
func TestWithCheckPointStore_NilOption(t *testing.T) {
	e := NewAgentExecutor(nil, nil, nil, nil, nil, nil, 0, nil)
	assert.Nil(t, e.checkpointStore, "checkpointStore should be nil by default")
}

// ---------------------------------------------------------------------------
// cleanupAfterResume direct tests (D-112, D-113)
// ---------------------------------------------------------------------------

// TestCleanupAfterResume_Success verifies that a successful cleanup deletes
// the checkpoint from the store and removes the interruptID mapping.
func TestCleanupAfterResume_Success(t *testing.T) {
	fs := newFakeDeletableStore()
	_ = fs.Set(context.Background(), "cp-1", []byte("data"))

	e := &AgentExecutor{}
	e.checkpointStore = fs
	e.registerInterruptIDs("cp-1", []string{"intr-a"})

	e.cleanupAfterResume(context.Background(), "cp-1", noopLogger{})

	// Store Delete should have been called once.
	assert.Equal(t, 1, fs.deleteCnt, "Delete should be called once")
	// Data should be gone.
	_, ok, err := fs.Get(context.Background(), "cp-1")
	require.NoError(t, err)
	assert.False(t, ok, "checkpoint should be deleted")
	// interruptIDs should be cleaned.
	assert.Nil(t, e.getInterruptIDs("cp-1"), "interruptIDs should be deleted")
}

// TestCleanupAfterResume_NilStore verifies that cleanup does not panic when
// checkpointStore is nil, and still cleans up interruptIDs.
func TestCleanupAfterResume_NilStore(t *testing.T) {
	e := &AgentExecutor{
		checkpointStore: nil,
	}
	e.registerInterruptIDs("cp-2", []string{"intr-b"})

	// Should not panic.
	assert.NotPanics(t, func() {
		e.cleanupAfterResume(context.Background(), "cp-2", noopLogger{})
	})
	// interruptIDs should still be cleaned.
	assert.Nil(t, e.getInterruptIDs("cp-2"), "interruptIDs should be deleted even with nil store")
}

// TestCleanupAfterResume_DeleteFails verifies that when store.Delete returns
// an error, cleanupAfterResume does not return an error (non-fatal) and still
// cleans up interruptIDs.
func TestCleanupAfterResume_DeleteFails(t *testing.T) {
	fs := newFakeDeletableStore()
	fs.deleteErr = fmt.Errorf("simulated redis failure")

	e := &AgentExecutor{}
	e.checkpointStore = fs
	e.registerInterruptIDs("cp-3", []string{"intr-c"})

	// Should not panic.
	assert.NotPanics(t, func() {
		e.cleanupAfterResume(context.Background(), "cp-3", noopLogger{})
	})
	// Delete should have been attempted.
	assert.Equal(t, 1, fs.deleteCnt, "Delete should have been called once")
	// interruptIDs should still be cleaned despite store failure.
	assert.Nil(t, e.getInterruptIDs("cp-3"), "interruptIDs should be deleted even on store error")
}
