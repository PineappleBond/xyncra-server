package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// agentResumeBroker is a mock mq.Broker that records enqueued tasks for
// inspection in agent_resume handler tests.
type agentResumeBroker struct {
	mq.Broker
	enqueued []*mq.Task
}

func (b *agentResumeBroker) Enqueue(_ context.Context, task *mq.Task, _ ...mq.EnqueueOption) (string, error) {
	b.enqueued = append(b.enqueued, task)
	return "task-resume-1", nil
}

// agentResumeTestEnv holds the dependencies needed by agent_resume tests.
type agentResumeTestEnv struct {
	store  *testSQLiteStore
	broker *agentResumeBroker
	h      *agentResumeHandler
}

// newAgentResumeTestEnv creates a fresh test environment with an in-memory
// SQLite store and a mock broker.
func newAgentResumeTestEnv(t *testing.T) *agentResumeTestEnv {
	t.Helper()
	s := setupTestSQLite(t)
	broker := &agentResumeBroker{}
	h := NewAgentResumeHandler(s, broker, nil)
	return &agentResumeTestEnv{store: s, broker: broker, h: h}
}

// seedConversation inserts a Conversation record with the given checkpoint_id.
func seedConversation(t *testing.T, env *agentResumeTestEnv, convID, checkpointID string) {
	t.Helper()
	conv := &model.Conversation{
		ID:           convID,
		UserID1:      "alice",
		UserID2:      "agent/bot1",
		Type:         "1-on-1",
		CheckpointID: checkpointID,
		AgentStatus:  model.AgentStatusAskingUser,
	}
	require.NoError(t, env.store.ConversationStore().Create(context.Background(), conv))
}

// seedRemoteCalling inserts a pending RemoteCalling record.
func seedRemoteCalling(t *testing.T, env *agentResumeTestEnv, id, convID, cpID, agentID, method string) {
	t.Helper()
	rc := &model.RemoteCalling{
		ID:             id,
		ConversationID: convID,
		CheckpointID:   cpID,
		AgentID:        agentID,
		Method:         method,
		Status:         model.RemoteCallingStatusPending,
	}
	require.NoError(t, env.store.RemoteCallingStore().Create(context.Background(), rc))
}

// callAgentResume invokes the agent_resume handler and returns the raw
// response data and error.
func callAgentResume(
	t *testing.T,
	h *agentResumeHandler,
	client *server.Client,
	params interface{},
) (json.RawMessage, error) {
	t.Helper()
	ctx := context.Background()
	req := newTestRequest("1", "agent_resume", params)
	return h.HandleRequest(ctx, client, req)
}

// parseAgentResumeResponseMap unmarshals the agent_resume success response
// into a generic map (for responses with mixed types).
func parseAgentResumeResponseMap(t *testing.T, data json.RawMessage) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &resp))
	return resp
}

// decodeAgentResumePayload extracts the task payload from the first enqueued
func decodeAgentResumePayload(t *testing.T, env *agentResumeTestEnv) agentResumeTaskPayload {
	t.Helper()
	require.Len(t, env.broker.enqueued, 1, "expected 1 enqueued task")
	var payload agentResumeTaskPayload
	require.NoError(t, json.Unmarshal(env.broker.enqueued[0].Payload, &payload))
	return payload
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestAgentResume_InvalidParams verifies that invalid params return a validation error.
func TestAgentResume_InvalidParams(t *testing.T) {
	env := newAgentResumeTestEnv(t)

	_, err := callAgentResume(t, env.h, nil, "invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")
}

// TestAgentResume_MissingID verifies that missing id returns a validation error.
func TestAgentResume_MissingID(t *testing.T) {
	env := newAgentResumeTestEnv(t)

	_, err := callAgentResume(t, env.h, nil, agentResumeParams{
		AgentID: "agent/bot1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id and agent_id are required")
}

// TestAgentResume_MissingAgentID verifies that missing agent_id returns a validation error.
func TestAgentResume_MissingAgentID(t *testing.T) {
	env := newAgentResumeTestEnv(t)

	_, err := callAgentResume(t, env.h, nil, agentResumeParams{
		ID: "rc-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id and agent_id are required")
}

// TestAgentResume_NotFound verifies that a non-existent remote calling returns 404.
func TestAgentResume_NotFound(t *testing.T) {
	env := newAgentResumeTestEnv(t)

	_, err := callAgentResume(t, env.h, nil, agentResumeParams{
		ID:      "non-existent-id",
		AgentID: "agent/bot1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote calling not found")
}

// TestAgentResume_AlreadyProcessed verifies idempotency for already resolved calls.
func TestAgentResume_AlreadyProcessed(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	ctx := context.Background()

	seedConversation(t, env, "conv-1", "cp-1")
	seedRemoteCalling(t, env, "rc-1", "conv-1", "cp-1", "agent/bot1", "ask_user")

	// Resolve it first
	require.NoError(t, env.store.RemoteCallingStore().ResolveResult(ctx, "rc-1", "done"))

	// Call again - should be idempotent
	data, err := callAgentResume(t, env.h, nil, agentResumeParams{
		ID:      "rc-1",
		AgentID: "agent/bot1",
		Success: true,
		Result:  "another result",
	})
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "resolved", resp["status"])
}

// TestAgentResume_SuccessResult verifies successful resolution.
func TestAgentResume_SuccessResult(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	ctx := context.Background()

	seedConversation(t, env, "conv-1", "cp-1")
	seedRemoteCalling(t, env, "rc-1", "conv-1", "cp-1", "agent/bot1", "ask_user")

	data, err := callAgentResume(t, env.h, nil, agentResumeParams{
		ID:      "rc-1",
		AgentID: "agent/bot1",
		Success: true,
		Result:  "Alice",
	})
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "queued", resp["status"])

	// Verify the record was resolved
	rc, err := env.store.RemoteCallingStore().GetByID(ctx, "rc-1")
	require.NoError(t, err)
	assert.Equal(t, model.RemoteCallingStatusResolved, rc.Status)
	assert.Equal(t, "Alice", rc.Result)
	assert.True(t, rc.Success)

	// Verify task was enqueued
	payload := decodeAgentResumePayload(t, env)
	assert.Equal(t, "conv-1", payload.ConversationID)
	assert.Equal(t, "cp-1", payload.CheckpointID)
	assert.Equal(t, "agent/bot1", payload.AgentID)
}

// TestAgentResume_ErrorResult verifies error resolution.
func TestAgentResume_ErrorResult(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	ctx := context.Background()

	seedConversation(t, env, "conv-1", "cp-1")
	seedRemoteCalling(t, env, "rc-1", "conv-1", "cp-1", "agent/bot1", "ask_user")

	data, err := callAgentResume(t, env.h, nil, agentResumeParams{
		ID:           "rc-1",
		AgentID:      "agent/bot1",
		Success:      false,
		ErrorMessage: "timeout",
	})
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "queued", resp["status"])

	// Verify the record was resolved with error
	rc, err := env.store.RemoteCallingStore().GetByID(ctx, "rc-1")
	require.NoError(t, err)
	assert.Equal(t, model.RemoteCallingStatusResolved, rc.Status)
	assert.Equal(t, "timeout", rc.ErrorMessage)
	assert.False(t, rc.Success)
}

// TestAgentResume_PartialResolution verifies partial response when more callings are pending.
func TestAgentResume_PartialResolution(t *testing.T) {
	env := newAgentResumeTestEnv(t)

	seedConversation(t, env, "conv-1", "cp-1")
	seedRemoteCalling(t, env, "rc-1", "conv-1", "cp-1", "agent/bot1", "ask_user")
	seedRemoteCalling(t, env, "rc-2", "conv-1", "cp-1", "agent/bot1", "ask_user")

	data, err := callAgentResume(t, env.h, nil, agentResumeParams{
		ID:      "rc-1",
		AgentID: "agent/bot1",
		Success: true,
		Result:  "Alice",
	})
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "partial", resp["status"])
	assert.Equal(t, float64(1), resp["pending_count"])

	// No task should be enqueued yet
	assert.Len(t, env.broker.enqueued, 0)
}

// TestAgentResume_AllResolvedEnqueue verifies that task is enqueued when all callings are resolved.
func TestAgentResume_AllResolvedEnqueue(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	ctx := context.Background()

	seedConversation(t, env, "conv-1", "cp-1")
	seedRemoteCalling(t, env, "rc-1", "conv-1", "cp-1", "agent/bot1", "ask_user")
	seedRemoteCalling(t, env, "rc-2", "conv-1", "cp-1", "agent/bot1", "ask_user")

	// Resolve first one
	require.NoError(t, env.store.RemoteCallingStore().ResolveResult(ctx, "rc-1", "Alice"))

	// Resolve second one - should trigger enqueue
	data, err := callAgentResume(t, env.h, nil, agentResumeParams{
		ID:      "rc-2",
		AgentID: "agent/bot1",
		Success: true,
		Result:  "Bob",
	})
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "queued", resp["status"])

	// Verify task was enqueued
	payload := decodeAgentResumePayload(t, env)
	assert.Equal(t, "conv-1", payload.ConversationID)
	assert.Equal(t, "cp-1", payload.CheckpointID)
	assert.Equal(t, "agent/bot1", payload.AgentID)
}
