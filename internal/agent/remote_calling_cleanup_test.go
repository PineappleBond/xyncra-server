package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Test helpers for HITL cleanup tests
// ---------------------------------------------------------------------------

// newTestRedisClient creates a miniredis-backed redis.Client and a cleanup
// function. The caller should defer cleanup().
func newTestRedisClient(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return client, func() {
		_ = client.Close()
		mr.Close()
	}
}

// newCleanupTaskWithDeps creates a RemoteCallingCleanupTask wired to real SQLite store,
// a miniredis-backed redis client, a mock broadcaster, and a mock StoreAPI.
// Returns the task and the mock objects for inspection.
func newCleanupTaskWithDeps(t *testing.T, cfg RemoteCallingCleanupConfig) (
	task *RemoteCallingCleanupTask,
	mockBS *mockBroadcastServer,
	mockStore *mockStoreAPI,
	redisClient *redis.Client,
	cleanup func(),
) {
	t.Helper()

	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	mockBS = &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore = &mockStoreAPI{}

	task = NewRemoteCallingCleanupTask(
		cfg,
		testStore.Conversations,
		testStore.RemoteCallings,
		newFakeDeletableStore(),
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	return task, mockBS, mockStore, client, redisCleanup
}

// createStaleAskingUserConv creates a conversation in asking_user status and
// sets its agent_last_activity and updated_at to the given age (a negative
// duration from now). Unique user IDs are derived from convID to avoid
// unique-index collisions.
func createStaleAskingUserConv(t *testing.T, s *store.Store, convID, agentID, checkpointID string, age time.Duration) {
	t.Helper()
	ctx := context.Background()
	uid1 := "user-" + convID
	uid2 := agentID
	conv := &model.Conversation{
		ID: convID, UserID1: uid1, UserID2: uid2,
		Type: "1-on-1", Title: "Test HITL",
		CreatedAt: time.Now(), UpdatedAt: time.Now().Add(age),
		LastMessageAt: time.Now(),
	}
	require.NoError(t, s.Conversations.Create(ctx, conv))
	_, err := s.Conversations.UpdateAgentStatus(ctx, convID, model.AgentStatusAskingUser, agentID, checkpointID)
	require.NoError(t, err)
	// Set both agent_last_activity and updated_at to the desired age via
	// Transaction + raw SQL, since GORM's UpdatedAt auto-management overrides
	// direct field assignment. The query uses agent_last_activity (D-123).
	require.NoError(t, s.Transaction(ctx, func(tx *gorm.DB) error {
		return tx.Model(&model.Conversation{}).
			Where("id = ?", convID).
			Updates(map[string]any{
				"updated_at":          time.Now().Add(age),
				"agent_last_activity": time.Now().Add(age),
			}).Error
	}))
}

// ---------------------------------------------------------------------------
// B1: HITL cleanup task start/stop via context cancellation
// ---------------------------------------------------------------------------

// TestHITLCleanup_RunStopsOnContextCancel verifies that the cleanup loop
// terminates when the context is cancelled.
func TestHITLCleanup_RunStopsOnContextCancel(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{
			Interval:  50 * time.Millisecond, // fast tick for testing
			MaxAge:    24 * time.Hour,
			BatchSize: 100,
			LockTTL:   30 * time.Second,
		},
		testStore.Conversations,
		nil, nil, nil, nil,
		nil,
		client,
		noopLogger{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		task.Run(ctx)
		close(done)
	}()

	// Cancel and wait for Run to exit.
	cancel()
	select {
	case <-done:
		// OK — Run exited.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2s after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// B2: Expired conversations are cleaned up (agent_status reset to idle)
// ---------------------------------------------------------------------------

// TestHITLCleanup_ResetsAgentStatusToIdle verifies that cleanupConversation
// resets agent_status from asking_user to idle via ClearAgentStatus.
func TestHITLCleanup_ResetsAgentStatusToIdle(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore := &mockStoreAPI{}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		newFakeDeletableStore(),
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	// Create a stale asking_user conversation (older than 1h MaxAge).
	createStaleAskingUserConv(t, testStore, "conv-b2", "agent/bot", "cp-b2", -2*time.Hour)

	ctx := context.Background()
	task.cleanupOnce(ctx)

	// Verify agent_status was reset to idle.
	conv, err := testStore.Conversations.Get(ctx, "conv-b2")
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusIdle, conv.AgentStatus,
		"agent_status should be reset to idle after cleanup")
}

// ---------------------------------------------------------------------------
// B3: RemoteCallings are soft-deleted after cleanup
// ---------------------------------------------------------------------------

// TestRemoteCallingCleanup_SoftDeletesRemoteCallings verifies that cleanupConversation
// soft-deletes questions associated with the checkpoint (D-123 step 4).
func TestRemoteCallingCleanup_SoftDeletesRemoteCallings(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore := &mockStoreAPI{}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		newFakeDeletableStore(),
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	ctx := context.Background()
	createStaleAskingUserConv(t, testStore, "conv-b3", "agent/bot", "cp-b3", -2*time.Hour)

	// Create a remote calling for the checkpoint.
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-b3-1", ConversationID: "conv-b3", CheckpointID: "cp-b3",
		AgentID: "agent/bot", Method: "ask_user", InterruptID: "int-1",
		Status: model.RemoteCallingStatusPending,
	}))

	// Verify question exists before cleanup.
	count, err := testStore.RemoteCallings.CountPendingByCheckpoint(ctx, "cp-b3")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	task.cleanupOnce(ctx)

	// Verify question was soft-deleted.
	countAfter, err := testStore.RemoteCallings.CountPendingByCheckpoint(ctx, "cp-b3")
	require.NoError(t, err)
	assert.Equal(t, int64(0), countAfter, "questions should be soft-deleted after cleanup")
}

// ---------------------------------------------------------------------------
// B4: Checkpoint is deleted from Redis
// ---------------------------------------------------------------------------

// TestHITLCleanup_DeletesCheckpointFromRedis verifies that cleanupConversation
// deletes the Eino checkpoint from Redis (D-123 step 5 / D-112).
func TestHITLCleanup_DeletesCheckpointFromRedis(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore := &mockStoreAPI{}
	cpStore := newFakeDeletableStore()

	// Pre-populate checkpoint in Redis.
	ctx := context.Background()
	require.NoError(t, cpStore.Set(ctx, "cp-b4", []byte("checkpoint-data")))

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		cpStore,
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	createStaleAskingUserConv(t, testStore, "conv-b4", "agent/bot", "cp-b4", -2*time.Hour)

	task.cleanupOnce(ctx)

	// Verify checkpoint was deleted from Redis.
	assert.Equal(t, 1, cpStore.deleteCnt, "checkpoint Delete should have been called once")
	_, ok, err := cpStore.Get(ctx, "cp-b4")
	require.NoError(t, err)
	assert.False(t, ok, "checkpoint should be deleted from Redis")
}

// ---------------------------------------------------------------------------
// B5: User-friendly timeout message is sent
// ---------------------------------------------------------------------------

// TestHITLCleanup_SendsTimeoutMessage verifies that cleanupConversation
// persists a user-friendly timeout error message (D-123 step 6 / D-067).
func TestHITLCleanup_SendsTimeoutMessage(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore := &mockStoreAPI{}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		newFakeDeletableStore(),
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	createStaleAskingUserConv(t, testStore, "conv-b5", "agent/bot", "cp-b5", -2*time.Hour)

	task.cleanupOnce(context.Background())

	// Verify SendMessage was called with the timeout message.
	require.Len(t, mockStore.sendMessageCalls, 1, "SendMessage should be called once")
	msg := mockStore.sendMessageCalls[0].msg
	assert.Equal(t, "conv-b5", msg.ConversationID)
	assert.Equal(t, "agent/bot", msg.SenderID)
	assert.Contains(t, msg.Content, "超时", "message should contain timeout indication (Chinese)")
	assert.Equal(t, "text", msg.Type)
	assert.Equal(t, "sent", msg.Status)
}

// ---------------------------------------------------------------------------
// B6: agent_timeout broadcast is sent
// ---------------------------------------------------------------------------

// TestHITLCleanup_BroadcastsAgentTimeout verifies that cleanupConversation
// broadcasts an agent_timeout ephemeral notification (D-123 step 7 / D-087).
func TestHITLCleanup_BroadcastsAgentTimeout(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore := &mockStoreAPI{}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		newFakeDeletableStore(),
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	createStaleAskingUserConv(t, testStore, "conv-b6", "agent/bot", "cp-b6", -2*time.Hour)

	task.cleanupOnce(context.Background())

	// Find an agent_timeout broadcast in the calls.
	foundTimeout := false
	foundConvUpdate := false
	for _, call := range mockBS.calls {
		for _, u := range call.updates.Updates {
			if u.Type == protocol.UpdateTypeAgentTimeout {
				foundTimeout = true
				var payload AgentTimeoutPayload
				require.NoError(t, json.Unmarshal(u.Payload, &payload))
				assert.Equal(t, "conv-b6", payload.ConversationID)
				assert.Equal(t, "hitl_timeout", payload.Reason)
			}
			if u.Type == protocol.UpdateTypeConversation {
				foundConvUpdate = true
			}
		}
	}
	assert.True(t, foundTimeout, "should broadcast agent_timeout event")
	assert.True(t, foundConvUpdate, "should also broadcast conversation update (D-124)")
}

// ---------------------------------------------------------------------------
// B7: Distributed lock prevents duplicate cleanup
// ---------------------------------------------------------------------------

// TestHITLCleanup_SkipsWhenLockAlreadyHeld verifies that cleanupConversation
// does not process a conversation when another node already holds the lock
// (Redis SETNX returns false).
func TestHITLCleanup_SkipsWhenLockAlreadyHeld(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore := &mockStoreAPI{}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		newFakeDeletableStore(),
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	createStaleAskingUserConv(t, testStore, "conv-b7", "agent/bot", "cp-b7", -2*time.Hour)

	// Pre-acquire the lock using the same Redis key pattern.
	ctx := context.Background()
	lockKey := "hitl:cleanup:conv-b7"
	ok, err := client.SetNX(ctx, lockKey, "1", 30*time.Second).Result()
	require.NoError(t, err)
	require.True(t, ok, "initial lock acquisition should succeed")

	task.cleanupOnce(ctx)

	// Verify no cleanup was performed (conversation still in asking_user).
	conv, convErr := testStore.Conversations.Get(ctx, "conv-b7")
	require.NoError(t, convErr)
	assert.Equal(t, model.AgentStatusAskingUser, conv.AgentStatus,
		"conversation should NOT be cleaned up when lock is already held")
	assert.Empty(t, mockStore.sendMessageCalls,
		"SendMessage should NOT be called when lock is held")
}

// ---------------------------------------------------------------------------
// B8: Skip cleanup when conversation status has changed
// ---------------------------------------------------------------------------

// TestHITLCleanup_SkipsWhenStatusChanged verifies that cleanupConversation
// re-checks the conversation status after acquiring the lock and skips
// cleanup if the status is no longer asking_user (D-123 step 2).
func TestHITLCleanup_SkipsWhenStatusChanged(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore := &mockStoreAPI{}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		newFakeDeletableStore(),
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	createStaleAskingUserConv(t, testStore, "conv-b8", "agent/bot", "cp-b8", -2*time.Hour)

	// Change the conversation status to idle (simulating another node/user resolving it).
	ctx := context.Background()
	_, err := testStore.Conversations.ClearAgentStatus(ctx, "conv-b8")
	require.NoError(t, err)

	task.cleanupOnce(ctx)

	// Verify no timeout message was sent.
	assert.Empty(t, mockStore.sendMessageCalls,
		"SendMessage should NOT be called when conversation is no longer in asking_user")

	// Verify no broadcasts were made.
	assert.Empty(t, mockBS.calls,
		"no broadcasts should be sent when conversation status changed")
}

// ---------------------------------------------------------------------------
// B9: Configuration default values are filled correctly
// ---------------------------------------------------------------------------

// TestHITLCleanup_DefaultConfigValues verifies that NewRemoteCallingCleanupTask fills
// zero-value config fields with the documented defaults (D-123).
func TestHITLCleanup_DefaultConfigValues(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	// Pass an entirely zero-value config.
	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{},
		testStore.Conversations,
		nil, nil, nil, nil,
		nil,
		client,
		noopLogger{},
	)

	assert.Equal(t, 5*time.Minute, task.config.Interval, "default interval should be 5 minutes")
	assert.Equal(t, 24*time.Hour, task.config.MaxAge, "default max age should be 24 hours")
	assert.Equal(t, 100, task.config.BatchSize, "default batch size should be 100")
	assert.Equal(t, 30*time.Second, task.config.LockTTL, "default lock TTL should be 30 seconds")
}

// TestHITLCleanup_CustomConfigValues verifies that non-zero config values
// are preserved and not overwritten by defaults.
func TestHITLCleanup_CustomConfigValues(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{
			Interval:  10 * time.Minute,
			MaxAge:    48 * time.Hour,
			BatchSize: 50,
			LockTTL:   1 * time.Minute,
		},
		testStore.Conversations,
		nil, nil, nil, nil,
		nil,
		client,
		noopLogger{},
	)

	assert.Equal(t, 10*time.Minute, task.config.Interval)
	assert.Equal(t, 48*time.Hour, task.config.MaxAge)
	assert.Equal(t, 50, task.config.BatchSize)
	assert.Equal(t, 1*time.Minute, task.config.LockTTL)
}

// ---------------------------------------------------------------------------
// Integration: full cleanup cycle
// ---------------------------------------------------------------------------

// TestHITLCleanup_FullCycle verifies that cleanupOnce processes stale
// conversations through all cleanup steps in a single pass.
func TestHITLCleanup_FullCycle(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockStoreAPI := &mockStoreAPI{}

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	cpStore := newFakeDeletableStore()
	_ = cpStore.Set(context.Background(), "cp-full", []byte("data"))

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		cpStore,
		broadcaster,
		mockStoreAPI,
		nil,
		client,
		noopLogger{},
	)

	ctx := context.Background()

	// Create 2 stale conversations and 1 fresh one.
	createStaleAskingUserConv(t, testStore, "conv-full-1", "agent/bot", "cp-full-1", -2*time.Hour)
	createStaleAskingUserConv(t, testStore, "conv-full-2", "agent/bot", "cp-full-2", -3*time.Hour)
	createStaleAskingUserConv(t, testStore, "conv-fresh", "agent/bot", "cp-fresh", -30*time.Minute)

	// Create remote callings for the stale ones.
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-full-1", ConversationID: "conv-full-1", CheckpointID: "cp-full-1",
		AgentID: "agent/bot", Method: "ask_user", InterruptID: "int-1",
		Status: model.RemoteCallingStatusPending,
	}))
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-full-2", ConversationID: "conv-full-2", CheckpointID: "cp-full-2",
		AgentID: "agent/bot", Method: "ask_user", InterruptID: "int-2",
		Status: model.RemoteCallingStatusPending,
	}))

	task.cleanupOnce(ctx)

	// Verify both stale conversations were cleaned up.
	for _, id := range []string{"conv-full-1", "conv-full-2"} {
		conv, err := testStore.Conversations.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, model.AgentStatusIdle, conv.AgentStatus, "conv %s should be idle", id)
	}

	// Verify fresh conversation was NOT cleaned up.
	fresh, err := testStore.Conversations.Get(ctx, "conv-fresh")
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusAskingUser, fresh.AgentStatus,
		"fresh conversation should remain in asking_user")

	// Verify questions were soft-deleted.
	for _, cpID := range []string{"cp-full-1", "cp-full-2"} {
		count, countErr := testStore.RemoteCallings.CountPendingByCheckpoint(ctx, cpID)
		require.NoError(t, countErr)
		assert.Equal(t, int64(0), count, "questions for %s should be deleted", cpID)
	}

	// Verify SendMessage was called twice (once per stale conversation).
	assert.Len(t, mockStoreAPI.sendMessageCalls, 2, "SendMessage should be called for each stale conversation")
}

// ---------------------------------------------------------------------------
// Edge case: nil optional stores
// ---------------------------------------------------------------------------

// TestHITLCleanup_NilOptionalStores verifies that cleanup does not panic
// when remoteCallingStore and checkpointStore are nil (D-063 nil-safe design).
func TestHITLCleanup_NilOptionalStores(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore := &mockStoreAPI{}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		nil, // remoteCallingStore is nil
		nil, // checkpointStore is nil
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	createStaleAskingUserConv(t, testStore, "conv-nil", "agent/bot", "cp-nil", -2*time.Hour)

	assert.NotPanics(t, func() {
		task.cleanupOnce(context.Background())
	})

	// Agent status should still be reset (ClearAgentStatus doesn't need optional stores).
	conv, err := testStore.Conversations.Get(context.Background(), "conv-nil")
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusIdle, conv.AgentStatus)
}

// ---------------------------------------------------------------------------
// Edge case: Redis lock failure is non-fatal
// ---------------------------------------------------------------------------

// TestHITLCleanup_LockFailureNonFatal verifies that when Redis SETNX fails,
// cleanupConversation logs the error and returns without panicking (D-007).
func TestHITLCleanup_LockFailureNonFatal(t *testing.T) {
	testStore := setupTestStore(t)
	// Use a client connected to a closed miniredis to simulate Redis failure.
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mr.Close() // close immediately to force errors
	defer func() { _ = client.Close() }()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore := &mockStoreAPI{}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		newFakeDeletableStore(),
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	createStaleAskingUserConv(t, testStore, "conv-redis-fail", "agent/bot", "cp-fail", -2*time.Hour)

	// Should not panic even when Redis is unavailable.
	assert.NotPanics(t, func() {
		task.cleanupOnce(context.Background())
	})

	// Conversation should still be in asking_user (cleanup was skipped).
	conv, convErr := testStore.Conversations.Get(context.Background(), "conv-redis-fail")
	require.NoError(t, convErr)
	assert.Equal(t, model.AgentStatusAskingUser, conv.AgentStatus,
		"conversation should remain unchanged when lock acquisition fails")
}

// Silence unused import warnings.
var _ = fmt.Sprintf

// ---------------------------------------------------------------------------
// D-007 non-fatal continuation tests
// ---------------------------------------------------------------------------

// TestHITLCleanup_ContinuesAfterSendMessageFails verifies that when
// SendMessage fails, the agent_timeout broadcast still fires (D-007).
func TestHITLCleanup_ContinuesAfterSendMessageFails(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	cpStore := newFakeDeletableStore()
	_ = cpStore.Set(context.Background(), "cp-send-fail", []byte("data"))

	mockStore := &mockStoreAPI{
		sendMessageErr: errors.New("simulated SendMessage failure"),
	}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		cpStore,
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	ctx := context.Background()
	createStaleAskingUserConv(t, testStore, "conv-send-fail", "agent/bot", "cp-send-fail", -2*time.Hour)

	// Should not panic even when SendMessage fails.
	assert.NotPanics(t, func() {
		task.cleanupOnce(ctx)
	})

	// Verify conversation was still cleaned up (ClearAgentStatus succeeded).
	conv, err := testStore.Conversations.Get(ctx, "conv-send-fail")
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusIdle, conv.AgentStatus,
		"conversation should be idle even when SendMessage fails")

	// Verify broadcast was still sent (agent_timeout + conversation_update).
	assert.GreaterOrEqual(t, len(mockBS.calls), 1,
		"broadcast should still fire when SendMessage fails")

	// Verify checkpoint was deleted.
	_, exists, _ := cpStore.Get(ctx, "cp-send-fail")
	assert.False(t, exists, "checkpoint should be deleted even when SendMessage fails")
}

// TestHITLCleanup_ContinuesAfterCheckpointDeleteFails verifies that when
// checkpoint delete fails, subsequent steps (SendMessage, broadcast) still
// execute (D-007).
func TestHITLCleanup_ContinuesAfterCheckpointDeleteFails(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	mockBS := &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	cpStore := newFakeDeletableStore()
	cpStore.deleteErr = errors.New("simulated checkpoint delete failure")
	_ = cpStore.Set(context.Background(), "cp-del-fail", []byte("data"))

	mockStore := &mockStoreAPI{}

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 1 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		testStore.RemoteCallings,
		cpStore,
		broadcaster,
		mockStore,
		nil,
		client,
		noopLogger{},
	)

	ctx := context.Background()
	createStaleAskingUserConv(t, testStore, "conv-del-fail", "agent/bot", "cp-del-fail", -2*time.Hour)

	// Should not panic even when checkpoint delete fails.
	assert.NotPanics(t, func() {
		task.cleanupOnce(ctx)
	})

	// Verify conversation was cleaned up.
	conv, err := testStore.Conversations.Get(ctx, "conv-del-fail")
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusIdle, conv.AgentStatus,
		"conversation should be idle even when checkpoint delete fails")

	// Verify SendMessage was still called.
	assert.Len(t, mockStore.sendMessageCalls, 1,
		"SendMessage should still be called when checkpoint delete fails")

	// Verify broadcast was still sent.
	assert.GreaterOrEqual(t, len(mockBS.calls), 1,
		"broadcast should still fire when checkpoint delete fails")
}

// ---------------------------------------------------------------------------
// Layer 2: cleanupExpiredRemoteCallings tests (D-137)
// ---------------------------------------------------------------------------

// mockCleanupBroker is a mock mq.Broker that records enqueued tasks for
// inspection in cleanup tests.
type mockCleanupBroker struct {
	enqueued []*mq.Task
}

func (b *mockCleanupBroker) Enqueue(_ context.Context, task *mq.Task, _ ...mq.EnqueueOption) (string, error) {
	b.enqueued = append(b.enqueued, task)
	return "cleanup-task-1", nil
}

func (b *mockCleanupBroker) Start(_ context.Context, _ mq.Handler) error { return nil }
func (b *mockCleanupBroker) Stop()                                       {}
func (b *mockCleanupBroker) GetTaskState(_ context.Context, _ string) (mq.TaskState, error) {
	return mq.TaskStateUnknown, nil
}

// newCleanupTaskWithBroker creates a RemoteCallingCleanupTask wired to a mock broker.
func newCleanupTaskWithBroker(t *testing.T, cfg RemoteCallingCleanupConfig, broker mq.Broker) (
	task *RemoteCallingCleanupTask,
	testStore *store.Store,
	mockBS *mockBroadcastServer,
	mockStore *mockStoreAPI,
	redisClient *redis.Client,
	cleanup func(),
) {
	t.Helper()

	testStore = setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	mockBS = &mockBroadcastServer{}
	broadcaster := NewBroadcastHelper(mockBS, noopLogger{})
	mockStore = &mockStoreAPI{}
	cpStore := newFakeDeletableStore()

	task = NewRemoteCallingCleanupTask(
		cfg,
		testStore.Conversations,
		testStore.RemoteCallings,
		cpStore,
		broadcaster,
		mockStore,
		broker,
		client,
		noopLogger{},
	)

	return task, testStore, mockBS, mockStore, client, func() { redisCleanup() }
}

// TestCleanupExpiredRemoteCallings_AllExpired_CleansUpConversation verifies that
// when all RemoteCallings for a checkpoint have expired without any being resolved,
// the conversation is cleaned up (agent status reset, timeout message sent).
func TestCleanupExpiredRemoteCallings_AllExpired_CleansUpConversation(t *testing.T) {
	task, testStore, mockBS, mockStore, _, cleanupFn := newCleanupTaskWithBroker(
		t,
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 24 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		nil, // broker nil — no agent resume expected
	)
	defer cleanupFn()

	ctx := context.Background()

	// Create a conversation in tool_calling status.
	conv := &model.Conversation{
		ID: "conv-exp-1", UserID1: "alice", UserID2: "agent/bot",
		Type: "1-on-1", Title: "Test", CreatedAt: time.Now(), UpdatedAt: time.Now(), LastMessageAt: time.Now(),
	}
	require.NoError(t, testStore.Conversations.Create(ctx, conv))
	_, err := testStore.Conversations.UpdateAgentStatus(ctx, "conv-exp-1", model.AgentStatusToolCalling, "agent/bot", "cp-exp-1")
	require.NoError(t, err)

	// Create two expired RemoteCallings for the same checkpoint.
	pastTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-exp-1", ConversationID: "conv-exp-1", CheckpointID: "cp-exp-1",
		AgentID: "agent/bot", Method: "test_func", InterruptID: "int-1",
		Status: model.RemoteCallingStatusPending, ExpiresAt: &pastTime,
	}))
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-exp-2", ConversationID: "conv-exp-1", CheckpointID: "cp-exp-1",
		AgentID: "agent/bot", Method: "test_func", InterruptID: "int-2",
		Status: model.RemoteCallingStatusPending, ExpiresAt: &pastTime,
	}))

	task.cleanupExpiredRemoteCallings(ctx)

	// Verify both RCs were marked as expired.
	for _, id := range []string{"rc-exp-1", "rc-exp-2"} {
		rc, getErr := testStore.RemoteCallings.GetByID(ctx, id)
		require.NoError(t, getErr)
		assert.Equal(t, model.RemoteCallingStatusExpired, rc.Status,
			"RC %s should be marked as expired", id)
	}

	// Verify conversation was cleaned up (agent status reset to idle).
	conv, err = testStore.Conversations.Get(ctx, "conv-exp-1")
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusIdle, conv.AgentStatus,
		"conversation should be reset to idle when all RCs expired")

	// Verify timeout message was sent.
	require.Len(t, mockStore.sendMessageCalls, 1, "SendMessage should be called once")
	assert.Contains(t, mockStore.sendMessageCalls[0].msg.Content, "超时",
		"timeout message should contain Chinese timeout indication")

	// Verify broadcast was sent.
	assert.GreaterOrEqual(t, len(mockBS.calls), 1, "broadcast should be sent")
}

// TestCleanupExpiredRemoteCallings_SomeResolved_EnqueuesAgentResume verifies that
// when some RemoteCallings are resolved and the rest expire, an agent resume task
// is enqueued so the agent can process the resolved results.
func TestCleanupExpiredRemoteCallings_SomeResolved_EnqueuesAgentResume(t *testing.T) {
	broker := &mockCleanupBroker{}
	task, testStore, _, _, _, cleanupFn := newCleanupTaskWithBroker(
		t,
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 24 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		broker,
	)
	defer cleanupFn()

	ctx := context.Background()

	// Create a conversation in tool_calling status.
	conv := &model.Conversation{
		ID: "conv-exp-2", UserID1: "alice", UserID2: "agent/bot",
		Type: "1-on-1", Title: "Test", CreatedAt: time.Now(), UpdatedAt: time.Now(), LastMessageAt: time.Now(),
	}
	require.NoError(t, testStore.Conversations.Create(ctx, conv))
	_, err := testStore.Conversations.UpdateAgentStatus(ctx, "conv-exp-2", model.AgentStatusToolCalling, "agent/bot", "cp-exp-2")
	require.NoError(t, err)

	// Create one expired and one resolved RemoteCalling for the same checkpoint.
	pastTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-exp-3", ConversationID: "conv-exp-2", CheckpointID: "cp-exp-2",
		AgentID: "agent/bot", Method: "test_func", InterruptID: "int-1",
		Status: model.RemoteCallingStatusPending, ExpiresAt: &pastTime,
	}))
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-resolved-1", ConversationID: "conv-exp-2", CheckpointID: "cp-exp-2",
		AgentID: "agent/bot", Method: "test_func", InterruptID: "int-2",
		Status: model.RemoteCallingStatusResolved, Result: "ok", Success: true,
	}))

	task.cleanupExpiredRemoteCallings(ctx)

	// Verify the expired RC was marked as expired.
	rc, getErr := testStore.RemoteCallings.GetByID(ctx, "rc-exp-3")
	require.NoError(t, getErr)
	assert.Equal(t, model.RemoteCallingStatusExpired, rc.Status)

	// Verify agent resume task was enqueued.
	require.Len(t, broker.enqueued, 1, "should enqueue one agent resume task")
	var payload AgentResumePayload
	require.NoError(t, json.Unmarshal(broker.enqueued[0].Payload, &payload))
	assert.Equal(t, "conv-exp-2", payload.ConversationID)
	assert.Equal(t, "cp-exp-2", payload.CheckpointID)
	assert.Equal(t, "agent/bot", payload.AgentID)
}

// TestCleanupExpiredRemoteCallings_NilRemoteCallingStore_NoOp verifies that
// cleanupExpiredRemoteCallings is a no-op when remoteCallingStore is nil (D-063).
func TestCleanupExpiredRemoteCallings_NilRemoteCallingStore_NoOp(t *testing.T) {
	testStore := setupTestStore(t)
	client, redisCleanup := newTestRedisClient(t)
	defer redisCleanup()

	task := NewRemoteCallingCleanupTask(
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 24 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		testStore.Conversations,
		nil, // remoteCallingStore is nil
		nil, nil, nil, nil,
		client,
		noopLogger{},
	)

	assert.NotPanics(t, func() {
		task.cleanupExpiredRemoteCallings(context.Background())
	})
}

// TestCleanupExpiredRemoteCallings_NoExpired_NoOp verifies that
// cleanupExpiredRemoteCallings is a no-op when there are no expired RCs.
func TestCleanupExpiredRemoteCallings_NoExpired_NoOp(t *testing.T) {
	task, testStore, mockBS, mockStore, _, cleanupFn := newCleanupTaskWithBroker(
		t,
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 24 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		nil,
	)
	defer cleanupFn()

	ctx := context.Background()

	// Create a pending RC that has NOT expired yet.
	futureTime := time.Now().Add(1 * time.Hour)
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-not-expired", ConversationID: "conv-noop", CheckpointID: "cp-noop",
		AgentID: "agent/bot", Method: "test_func", InterruptID: "int-1",
		Status: model.RemoteCallingStatusPending, ExpiresAt: &futureTime,
	}))

	task.cleanupExpiredRemoteCallings(ctx)

	// Verify no cleanup was performed.
	assert.Empty(t, mockStore.sendMessageCalls, "SendMessage should not be called")
	assert.Empty(t, mockBS.calls, "no broadcasts should be sent")
}

// TestCleanupExpiredRemoteCallings_AgentResumeGuardPreventsDuplicate verifies that
// the cleanup resume guard (Redis SETNX) prevents duplicate agent resume enqueues
// within the same cleanup window.
func TestCleanupExpiredRemoteCallings_AgentResumeGuardPreventsDuplicate(t *testing.T) {
	broker := &mockCleanupBroker{}
	task, testStore, _, _, redisClient, cleanupFn := newCleanupTaskWithBroker(
		t,
		RemoteCallingCleanupConfig{Interval: time.Minute, MaxAge: 24 * time.Hour, BatchSize: 100, LockTTL: 30 * time.Second},
		broker,
	)
	defer cleanupFn()

	ctx := context.Background()

	// Create conversation and resolved+expired RCs.
	conv := &model.Conversation{
		ID: "conv-guard", UserID1: "alice", UserID2: "agent/bot",
		Type: "1-on-1", Title: "Test", CreatedAt: time.Now(), UpdatedAt: time.Now(), LastMessageAt: time.Now(),
	}
	require.NoError(t, testStore.Conversations.Create(ctx, conv))
	_, err := testStore.Conversations.UpdateAgentStatus(ctx, "conv-guard", model.AgentStatusToolCalling, "agent/bot", "cp-guard")
	require.NoError(t, err)

	pastTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-guard-exp", ConversationID: "conv-guard", CheckpointID: "cp-guard",
		AgentID: "agent/bot", Method: "test_func", InterruptID: "int-1",
		Status: model.RemoteCallingStatusPending, ExpiresAt: &pastTime,
	}))
	require.NoError(t, testStore.RemoteCallings.Create(ctx, &model.RemoteCalling{
		ID: "rc-guard-resolved", ConversationID: "conv-guard", CheckpointID: "cp-guard",
		AgentID: "agent/bot", Method: "test_func", InterruptID: "int-2",
		Status: model.RemoteCallingStatusResolved, Result: "ok", Success: true,
	}))

	// Pre-set the cleanup resume guard key.
	guardKey := "cleanup:resume:cp-guard"
	ok, setErr := redisClient.SetNX(ctx, guardKey, "1", 5*time.Minute).Result()
	require.NoError(t, setErr)
	require.True(t, ok)

	task.cleanupExpiredRemoteCallings(ctx)

	// Verify no agent resume was enqueued (guard prevented it).
	assert.Empty(t, broker.enqueued, "should NOT enqueue agent resume when guard key exists")
}
