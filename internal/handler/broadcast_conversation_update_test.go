package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// captureBroker records enqueued tasks for verification.
type captureBroker struct {
	mq.Broker
	mu    sync.Mutex
	tasks []*mq.Task
}

func (b *captureBroker) Enqueue(ctx context.Context, task *mq.Task, opts ...mq.EnqueueOption) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tasks = append(b.tasks, task)
	return "task-id", nil
}

func (b *captureBroker) capturedTasks() []*mq.Task {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]*mq.Task, len(b.tasks))
	copy(result, b.tasks)
	return result
}

// ---------------------------------------------------------------------------
// BC-01: Happy path — creates UserUpdates and enqueues MQ task
// ---------------------------------------------------------------------------

func TestBroadcastConversationUpdate_HappyPath(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &captureBroker{}
	ctx := context.Background()

	// Seed some existing updates to verify seq allocation.
	seedUserUpdates(t, s, "alice", 3, 1) // alice has seq 1,2,3
	seedUserUpdates(t, s, "bob", 2, 5)   // bob has seq 5,6

	err := broadcastConversationUpdateToMembers(
		ctx, s, broker, nil,
		"conv-1", []string{"alice", "bob"}, "update",
	)
	require.NoError(t, err)

	// Verify alice got seq 4 (latest was 3).
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	require.Len(t, aliceUpdates, 4) // 3 seeded + 1 new
	aliceNew := aliceUpdates[3]
	assert.Equal(t, uint32(4), aliceNew.Seq)
	assert.Equal(t, protocol.UpdateTypeConversation, aliceNew.Type)

	// Verify payload.
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(aliceNew.Payload, &payload))
	assert.Equal(t, "conv-1", payload["conversation_id"])
	assert.Equal(t, "update", payload["action"])

	// Verify bob got seq 7 (latest was 6).
	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 100)
	require.NoError(t, err)
	require.Len(t, bobUpdates, 3) // 2 seeded + 1 new
	bobNew := bobUpdates[2]
	assert.Equal(t, uint32(7), bobNew.Seq)
	assert.Equal(t, protocol.UpdateTypeConversation, bobNew.Type)

	// Verify MQ task was enqueued.
	tasks := broker.capturedTasks()
	require.Len(t, tasks, 1)
	assert.Equal(t, mq.TypeSendMessage, tasks[0].Type)

	var taskPayload sendMessageTaskPayload
	require.NoError(t, json.Unmarshal(tasks[0].Payload, &taskPayload))
	assert.Len(t, taskPayload.Recipients, 2)
	assert.Equal(t, "alice", taskPayload.Recipients[0].UserID)
	assert.Equal(t, uint32(4), taskPayload.Recipients[0].Updates[0].Seq)
	assert.Equal(t, "bob", taskPayload.Recipients[1].UserID)
	assert.Equal(t, uint32(7), taskPayload.Recipients[1].Updates[0].Seq)
}

// ---------------------------------------------------------------------------
// BC-02: Empty memberIDs — no-op
// ---------------------------------------------------------------------------

func TestBroadcastConversationUpdate_EmptyMembers(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &captureBroker{}
	ctx := context.Background()

	err := broadcastConversationUpdateToMembers(
		ctx, s, broker, nil,
		"conv-1", []string{}, "update",
	)
	require.NoError(t, err)

	// No MQ task should be enqueued.
	tasks := broker.capturedTasks()
	assert.Empty(t, tasks)
}

// ---------------------------------------------------------------------------
// BC-03: Nil broker — persistence only, no MQ
// ---------------------------------------------------------------------------

func TestBroadcastConversationUpdate_NilBroker(t *testing.T) {
	s := setupTestSQLite(t)
	ctx := context.Background()

	err := broadcastConversationUpdateToMembers(
		ctx, s, nil, nil,
		"conv-1", []string{"alice"}, "remote_calling_created",
	)
	require.NoError(t, err)

	// Verify UserUpdate was persisted.
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	require.Len(t, aliceUpdates, 1)
	assert.Equal(t, uint32(1), aliceUpdates[0].Seq)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(aliceUpdates[0].Payload, &payload))
	assert.Equal(t, "remote_calling_created", payload["action"])
}

// ---------------------------------------------------------------------------
// BC-04: Multiple members — each gets independent seq
// ---------------------------------------------------------------------------

func TestBroadcastConversationUpdate_MultipleMembers(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &captureBroker{}
	ctx := context.Background()

	// Seed different seq counts for each user.
	seedUserUpdates(t, s, "alice", 10, 1) // alice has seq 1..10
	seedUserUpdates(t, s, "bob", 0, 0)    // bob has no updates
	seedUserUpdates(t, s, "charlie", 5, 3) // charlie has seq 3..7

	err := broadcastConversationUpdateToMembers(
		ctx, s, broker, nil,
		"conv-1", []string{"alice", "bob", "charlie"}, "cancel_remote_calls",
	)
	require.NoError(t, err)

	// alice: seq 11
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 10, 100)
	require.NoError(t, err)
	require.Len(t, aliceUpdates, 1)
	assert.Equal(t, uint32(11), aliceUpdates[0].Seq)

	// bob: seq 1 (first update ever)
	bobUpdates, err := s.UserUpdateStore().ListByUser(ctx, "bob", 0, 100)
	require.NoError(t, err)
	require.Len(t, bobUpdates, 1)
	assert.Equal(t, uint32(1), bobUpdates[0].Seq)

	// charlie: seq 8
	charlieUpdates, err := s.UserUpdateStore().ListByUser(ctx, "charlie", 7, 100)
	require.NoError(t, err)
	require.Len(t, charlieUpdates, 1)
	assert.Equal(t, uint32(8), charlieUpdates[0].Seq)

	// Verify MQ task has all 3 recipients.
	tasks := broker.capturedTasks()
	require.Len(t, tasks, 1)
	var taskPayload sendMessageTaskPayload
	require.NoError(t, json.Unmarshal(tasks[0].Payload, &taskPayload))
	assert.Len(t, taskPayload.Recipients, 3)
}

// ---------------------------------------------------------------------------
// BC-05: Nil logger — defaults to slog
// ---------------------------------------------------------------------------

func TestBroadcastConversationUpdate_NilLogger(t *testing.T) {
	s := setupTestSQLite(t)
	ctx := context.Background()

	// Should not panic with nil logger.
	err := broadcastConversationUpdateToMembers(
		ctx, s, nil, nil,
		"conv-1", []string{"alice"}, "update",
	)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// BC-06: NewBroadcastConversationUpdateFunc — closure works correctly
// ---------------------------------------------------------------------------

func TestNewBroadcastConversationUpdateFunc(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &captureBroker{}
	ctx := context.Background()

	fn := NewBroadcastConversationUpdateFunc(s, broker, nil)
	require.NotNil(t, fn)

	err := fn(ctx, "conv-1", []string{"alice", "bob"}, "remote_calling_resolved")
	require.NoError(t, err)

	// Verify persistence.
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	require.Len(t, aliceUpdates, 1)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(aliceUpdates[0].Payload, &payload))
	assert.Equal(t, "remote_calling_resolved", payload["action"])
	assert.Equal(t, "conv-1", payload["conversation_id"])

	// Verify MQ.
	tasks := broker.capturedTasks()
	require.Len(t, tasks, 1)
}

// ---------------------------------------------------------------------------
// BC-07: Various action strings
// ---------------------------------------------------------------------------

func TestBroadcastConversationUpdate_VariousActions(t *testing.T) {
	s := setupTestSQLite(t)
	ctx := context.Background()

	actions := []string{"update", "remote_calling_created", "remote_calling_resolved", "cancel_remote_calls"}

	for i, action := range actions {
		userID := fmt.Sprintf("user-%d", i)
		err := broadcastConversationUpdateToMembers(
			ctx, s, nil, nil,
			"conv-1", []string{userID}, action,
		)
		require.NoError(t, err)

		updates, err := s.UserUpdateStore().ListByUser(ctx, userID, 0, 100)
		require.NoError(t, err)
		require.Len(t, updates, 1)

		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(updates[0].Payload, &payload))
		assert.Equal(t, action, payload["action"], "action should match for %q", action)
	}
}

// ---------------------------------------------------------------------------
// BC-08: Failing broker — persistence succeeds, MQ error is non-fatal
// ---------------------------------------------------------------------------

func TestBroadcastConversationUpdate_FailingBroker(t *testing.T) {
	s := setupTestSQLite(t)
	broker := &failingBroker{}
	ctx := context.Background()

	err := broadcastConversationUpdateToMembers(
		ctx, s, broker, nil,
		"conv-1", []string{"alice"}, "update",
	)
	// Should NOT return error — MQ failure is fire-and-forget (D-007).
	require.NoError(t, err)

	// Verify UserUpdate was still persisted.
	aliceUpdates, err := s.UserUpdateStore().ListByUser(ctx, "alice", 0, 100)
	require.NoError(t, err)
	require.Len(t, aliceUpdates, 1)
	assert.Equal(t, uint32(1), aliceUpdates[0].Seq)
}
