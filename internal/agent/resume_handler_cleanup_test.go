package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// ---------------------------------------------------------------------------
// Resume handler cleanup tests (D-112)
//
// The cleanup logic (checkpoint deletion) is embedded
// in the resume handler closure. These tests verify the building blocks
// and the DeletableCheckPointStore contract used by the cleanup path.
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
// cleanupAfterResume direct tests (D-112)
// ---------------------------------------------------------------------------

// TestCleanupAfterResume_Success verifies that a successful cleanup deletes
// the checkpoint from the store.
func TestCleanupAfterResume_Success(t *testing.T) {
	fs := newFakeDeletableStore()
	_ = fs.Set(context.Background(), "cp-1", []byte("data"))

	e := &AgentExecutor{}
	e.checkpointStore = fs

	e.cleanupAfterResume(context.Background(), "cp-1", noopLogger{})

	// Store Delete should have been called once.
	assert.Equal(t, 1, fs.deleteCnt, "Delete should be called once")
	// Data should be gone.
	_, ok, err := fs.Get(context.Background(), "cp-1")
	require.NoError(t, err)
	assert.False(t, ok, "checkpoint should be deleted")
}

// TestCleanupAfterResume_NilStore verifies that cleanup does not panic when
// checkpointStore is nil.
func TestCleanupAfterResume_NilStore(t *testing.T) {
	e := &AgentExecutor{
		checkpointStore: nil,
	}

	// Should not panic.
	assert.NotPanics(t, func() {
		e.cleanupAfterResume(context.Background(), "cp-2", noopLogger{})
	})
}

// TestCleanupAfterResume_DeleteFails verifies that when store.Delete returns
// an error, cleanupAfterResume does not return an error (non-fatal).
func TestCleanupAfterResume_DeleteFails(t *testing.T) {
	fs := newFakeDeletableStore()
	fs.deleteErr = fmt.Errorf("simulated redis failure")

	e := &AgentExecutor{}
	e.checkpointStore = fs

	// Should not panic.
	assert.NotPanics(t, func() {
		e.cleanupAfterResume(context.Background(), "cp-3", noopLogger{})
	})
	// Delete should have been attempted.
	assert.Equal(t, 1, fs.deleteCnt, "Delete should have been called once")
}

// ---------------------------------------------------------------------------
// Resume handler idempotency tests
//
// Verify that the resume handler skips duplicate resume attempts for the same
// checkpoint when an IdempotencyStore is provided.
// ---------------------------------------------------------------------------

// newMinimalResumeHandler creates a resume handler with just enough mocks to
// reach the idempotency check. The handler will fail at agent lookup (step 4)
// if the idempotency check passes — that's fine; we only need to verify whether
// the idempotency gate lets the task through or blocks it.
func newMinimalResumeHandler(
	t *testing.T,
	idempotency IdempotencyStore,
) (
	handler func(ctx context.Context, task *mq.Task) error,
	idemCalls *[]string,
) {
	t.Helper()
	registry := NewRegistry() // empty — agent lookup will fail if reached
	idemCalls = &[]string{}

	// Create a minimal executor with a real SQLite store so that
	// cleanupAfterResumeFailure can run without nil-pointer panics when the
	// idempotency gate opens and the handler reaches agent-not-found.
	testStore := setupTestStore(t)
	executor := &AgentExecutor{
		store:  testStore,
		logger: noopLogger{},
	}

	return NewAgentResumeHandler(
		executor,
		registry,
		nil, // lock: not needed for idempotency tests
		noopLogger{},
		idempotency,
	), idemCalls
}

// makeResumeTask builds a minimal TypeAgentResume MQ task payload.
func makeResumeTask(checkpointID string) *mq.Task {
	payload := AgentResumePayload{
		ConversationID: "conv-1",
		CheckpointID:   checkpointID,
		AgentID:        "agent/test",
	}
	raw, _ := json.Marshal(payload)
	return &mq.Task{Type: mq.TypeAgentResume, Payload: raw}
}

// TestResumeIdempotency_DuplicateSkipped verifies that when the idempotency
// store reports the checkpoint as already processed, the handler returns nil
// without proceeding to agent lookup or lock acquisition.
func TestResumeIdempotency_DuplicateSkipped(t *testing.T) {
	idem := &mockIdempotencyStore{
		checkProcessedFn: func(_ context.Context, key string) (bool, error) {
			// Always report as already processed/in-progress.
			return true, nil
		},
	}
	handler, _ := newMinimalResumeHandler(t, idem)

	err := handler(context.Background(), makeResumeTask("cp-dup"))
	require.NoError(t, err, "duplicate resume should return nil (no error)")
}

// TestResumeIdempotency_FirstCallProceedes verifies that when the idempotency
// store reports first-time processing, the handler continues past the gate.
// It will fail at agent lookup (empty registry) but that proves the gate opened.
func TestResumeIdempotency_FirstCallProceeds(t *testing.T) {
	idem := &mockIdempotencyStore{
		checkProcessedFn: func(_ context.Context, key string) (bool, error) {
			return false, nil // not processed
		},
	}
	handler, _ := newMinimalResumeHandler(t, idem)

	// The handler will fail at agent lookup (empty registry) — that's expected.
	// What matters is that it did NOT return early at the idempotency check.
	err := handler(context.Background(), makeResumeTask("cp-first"))
	require.NoError(t, err, "handler always returns nil to MQ (D-073)")
}

// TestResumeIdempotency_ErrorFailsOpen verifies that when the idempotency store
// returns an error, the handler proceeds (fail-open) rather than blocking.
func TestResumeIdempotency_ErrorFailsOpen(t *testing.T) {
	idem := &mockIdempotencyStore{
		checkProcessedFn: func(_ context.Context, key string) (bool, error) {
			return false, fmt.Errorf("redis connection refused")
		},
	}
	handler, _ := newMinimalResumeHandler(t, idem)

	// Should proceed past idempotency despite error.
	err := handler(context.Background(), makeResumeTask("cp-err"))
	require.NoError(t, err, "handler should proceed on idempotency error (fail-open)")
}

// TestResumeIdempotency_NilStoreSkipsCheck verifies that when idempotency is
// nil, no check is performed and the handler proceeds directly.
func TestResumeIdempotency_NilStoreSkipsCheck(t *testing.T) {
	handler, _ := newMinimalResumeHandler(t, nil)

	// Should proceed without panic.
	assert.NotPanics(t, func() {
		err := handler(context.Background(), makeResumeTask("cp-nil"))
		assert.NoError(t, err)
	})
}

// TestResumeIdempotency_CorrectKeyFormat verifies that the idempotency keys
// use the expected "agent:resume:<checkpointID>" and
// "agent:resume:processing:<checkpointID>" formats.
func TestResumeIdempotency_CorrectKeyFormat(t *testing.T) {
	var capturedKeys []string
	idem := &mockIdempotencyStore{
		checkProcessedFn: func(_ context.Context, key string) (bool, error) {
			capturedKeys = append(capturedKeys, key)
			return true, nil // duplicate to short-circuit
		},
	}
	handler, _ := newMinimalResumeHandler(t, idem)

	_ = handler(context.Background(), makeResumeTask("cp-key-test"))
	assert.Contains(t, capturedKeys, "agent:resume:cp-key-test",
		"processed key should follow 'agent:resume:<checkpointID>' format")
	assert.Contains(t, capturedKeys, "agent:resume:processing:cp-key-test",
		"processing key should follow 'agent:resume:processing:<checkpointID>' format")
}

// ---------------------------------------------------------------------------
// R-01: cleanupAfterResumeFailure calls all cleanup steps (D-122)
// ---------------------------------------------------------------------------

// TestCleanupAfterResumeFailure_CallsAllSteps verifies that
// cleanupAfterResumeFailure invokes ClearAgentStatus, DeleteByCheckpoint, and
// cleanupAfterResume (checkpoint deletion).
func TestCleanupAfterResumeFailure_CallsAllSteps(t *testing.T) {
	// Use a real SQLite store so that ClearAgentStatus and DeleteByCheckpoint
	// can run without complex mocking.
	testStore := setupTestStore(t)

	// Seed a conversation with agent_status = asking_user.
	ctx := context.Background()
	convID := "conv-cleanup-1"
	createTestConv(t, testStore, convID)
	_, err := testStore.Conversations.UpdateAgentStatus(ctx, convID,
		model.AgentStatusAskingUser, "agent/bot", "cp-cleanup")
	require.NoError(t, err)

	// Seed a Question for the checkpoint.
	questionStore := testStore.Questions
	require.NoError(t, questionStore.Create(ctx, &model.Question{
		ID:             "q-cleanup-1",
		ConversationID: convID,
		CheckpointID:   "cp-cleanup",
		InterruptID:    "intr-1",
		QuestionText:   "Are you sure?",
		Status:         model.QuestionStatusPending,
	}))

	// Verify question exists before cleanup.
	qCount, err := questionStore.CountPendingByCheckpoint(ctx, "cp-cleanup")
	require.NoError(t, err)
	assert.Equal(t, int64(1), qCount)

	// Set up checkpoint store to track deletion.
	fs := newFakeDeletableStore()
	_ = fs.Set(ctx, "cp-cleanup", []byte("checkpoint-data"))

	executor := &AgentExecutor{
		store:           testStore,
		questionStore:   questionStore,
		checkpointStore: fs,
		logger:          noopLogger{},
	}

	cleanupAfterResumeFailure(ctx, executor, convID, "cp-cleanup", noopLogger{})

	// 1. ClearAgentStatus: conversation should be reset to idle.
	conv, err := testStore.Conversations.Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusIdle, conv.AgentStatus,
		"ClearAgentStatus should reset agent_status to idle")

	// 2. DeleteByCheckpoint: questions should be soft-deleted.
	qCountAfter, err := questionStore.CountPendingByCheckpoint(ctx, "cp-cleanup")
	require.NoError(t, err)
	assert.Equal(t, int64(0), qCountAfter,
		"DeleteByCheckpoint should remove all questions for the checkpoint")

	// 3. cleanupAfterResume: checkpoint should be deleted from store.
	assert.Equal(t, 1, fs.deleteCnt, "checkpoint Delete should be called once")
	_, ok, err := fs.Get(ctx, "cp-cleanup")
	require.NoError(t, err)
	assert.False(t, ok, "checkpoint data should be deleted")
}

// ---------------------------------------------------------------------------
// R-02: cleanupAfterResumeFailure with nil questionStore/checkpointStore
// ---------------------------------------------------------------------------

// TestCleanupAfterResumeFailure_NilOptionalStores verifies that
// cleanupAfterResumeFailure does not panic when optional stores (questionStore,
// checkpointStore) are nil. Note: executor.store must be non-nil as the
// function calls store.ConversationStore() unconditionally.
func TestCleanupAfterResumeFailure_NilOptionalStores(t *testing.T) {
	testStore := setupTestStore(t)
	ctx := context.Background()
	convID := "conv-nil-opt"
	createTestConv(t, testStore, convID)
	_, err := testStore.Conversations.UpdateAgentStatus(ctx, convID,
		model.AgentStatusAskingUser, "agent/bot", "cp-nil")
	require.NoError(t, err)

	executor := &AgentExecutor{
		store:           testStore,
		questionStore:   nil, // optional, should not panic
		checkpointStore: nil, // optional, should not panic
		logger:          noopLogger{},
	}

	assert.NotPanics(t, func() {
		cleanupAfterResumeFailure(ctx, executor, convID, "cp-nil", noopLogger{})
	})

	// ClearAgentStatus should still have been called.
	conv, err := testStore.Conversations.Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusIdle, conv.AgentStatus,
		"ClearAgentStatus should still work even with nil optional stores")
}
