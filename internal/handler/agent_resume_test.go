package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
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
	h := NewAgentResumeHandler(s, broker)
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

// seedQuestion inserts a pending Question record.
func seedQuestion(t *testing.T, env *agentResumeTestEnv, id, convID, cpID, interruptID, text string) {
	t.Helper()
	q := &model.Question{
		ID:             id,
		ConversationID: convID,
		CheckpointID:   cpID,
		InterruptID:    interruptID,
		QuestionText:   text,
		Status:         model.QuestionStatusPending,
	}
	require.NoError(t, env.store.QuestionStore().Create(context.Background(), q))
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
// task in the broker.
func decodeAgentResumePayload(t *testing.T, broker *agentResumeBroker) agentResumeTaskPayload {
	t.Helper()
	require.GreaterOrEqual(t, len(broker.enqueued), 1, "expected at least one enqueued task")
	task := broker.enqueued[0]
	assert.Equal(t, mq.TypeAgentResume, task.Type)

	var payload agentResumeTaskPayload
	require.NoError(t, json.Unmarshal(task.Payload, &payload))
	return payload
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestAgentResumeHandler_ParameterValidation verifies that missing required
// params return an error.
func TestAgentResumeHandler_ParameterValidation(t *testing.T) {
	tests := []struct {
		name   string
		params agentResumeParams
	}{
		{
			name:   "missing all",
			params: agentResumeParams{},
		},
		{
			name: "missing conversation_id",
			params: agentResumeParams{
				Answer:  "yes",
				AgentID: "agent/bot1",
			},
		},
		{
			name: "missing answer",
			params: agentResumeParams{
				ConversationID: "conv-1",
				AgentID:        "agent/bot1",
			},
		},
		{
			name: "missing agent_id",
			params: agentResumeParams{
				ConversationID: "conv-1",
				Answer:         "yes",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newAgentResumeTestEnv(t)

			_, err := callAgentResume(t, env.h, server.NewTestClient("alice"), tc.params)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "required")
			assert.Empty(t, env.broker.enqueued, "no task should be enqueued on validation failure")
		})
	}
}

// TestAgentResumeHandler_InvalidJSON verifies that invalid JSON params
// return a descriptive error.
func TestAgentResumeHandler_InvalidJSON(t *testing.T) {
	env := newAgentResumeTestEnv(t)

	ctx := context.Background()
	client := server.NewTestClient("alice")
	req := &protocol.PackageDataRequest{
		ID:     "req-invalid",
		Method: "agent_resume",
		Params: json.RawMessage(`not valid json`),
	}

	_, err := env.h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")
}

// TestAgentResumeHandler_AllAnswered_EnqueuesMQ verifies that when all
// questions for a checkpoint are answered, a TypeAgentResume task is enqueued
// and the response contains status "queued".
func TestAgentResumeHandler_AllAnswered_EnqueuesMQ(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	seedConversation(t, env, "conv-1", "cp-1")
	seedQuestion(t, env, "q-1", "conv-1", "cp-1", "intr-1", "What color?")

	params := agentResumeParams{
		ConversationID: "conv-1",
		CheckpointID:   "cp-1",
		InterruptID:    "intr-1",
		Answer:         "blue",
		AgentID:        "agent/weather-bot",
	}

	data, err := callAgentResume(t, env.h, server.NewTestClient("alice"), params)
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "queued", resp["status"])
	assert.Equal(t, float64(1), resp["answered"])
	assert.Equal(t, float64(1), resp["total"])

	// Verify the enqueued task payload does NOT contain the answer (D-116).
	require.Len(t, env.broker.enqueued, 1)
	payload := decodeAgentResumePayload(t, env.broker)
	assert.Equal(t, "conv-1", payload.ConversationID)
	assert.Equal(t, "cp-1", payload.CheckpointID)
	assert.Equal(t, "agent/weather-bot", payload.AgentID)
	assert.Equal(t, "alice", payload.SenderID)
	assert.Empty(t, payload.DeviceID)
}

// TestAgentResumeHandler_PartialAnswer verifies that when only some questions
// are answered, no MQ task is enqueued and the response is "partial".
func TestAgentResumeHandler_PartialAnswer(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	seedConversation(t, env, "conv-1", "cp-1")
	seedQuestion(t, env, "q-1", "conv-1", "cp-1", "intr-1", "Question 1?")
	seedQuestion(t, env, "q-2", "conv-1", "cp-1", "intr-2", "Question 2?")

	params := agentResumeParams{
		ConversationID: "conv-1",
		Answer:         "answer-1",
		AgentID:        "agent/bot1",
	}

	data, err := callAgentResume(t, env.h, server.NewTestClient("alice"), params)
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "partial", resp["status"])
	assert.Equal(t, float64(1), resp["answered"])
	assert.Equal(t, float64(2), resp["total"])
	assert.Equal(t, float64(1), resp["pending"])
	assert.Empty(t, env.broker.enqueued, "no MQ task should be enqueued on partial answer")
}

// TestAgentResumeHandler_Conflict verifies that answering an already-answered
// question returns a 409 error (multi-device race protection).
func TestAgentResumeHandler_Conflict(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	seedConversation(t, env, "conv-1", "cp-1")
	seedQuestion(t, env, "q-1", "conv-1", "cp-1", "intr-1", "What?")

	params := agentResumeParams{
		ConversationID: "conv-1",
		InterruptID:    "intr-1",
		Answer:         "first",
		AgentID:        "agent/bot1",
	}

	// First call succeeds.
	_, err := callAgentResume(t, env.h, server.NewTestClient("alice"), params)
	require.NoError(t, err)

	// Second call with same interrupt_id should return conflict (question already answered).
	_, err = callAgentResume(t, env.h, server.NewTestClient("bob"), params)
	require.Error(t, err)
	var herr *protocol.HandlerError
	require.ErrorAs(t, err, &herr)
	assert.Equal(t, protocol.ResponseCode(-409), herr.Code)
	assert.Equal(t, "question_already_answered", herr.Message)
}

// TestAgentResumeHandler_CheckpointIDInferred verifies that checkpoint_id
// can be inferred from the Conversation when not supplied.
func TestAgentResumeHandler_CheckpointIDInferred(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	seedConversation(t, env, "conv-1", "cp-auto")
	seedQuestion(t, env, "q-1", "conv-1", "cp-auto", "intr-1", "What?")

	params := agentResumeParams{
		ConversationID: "conv-1",
		// CheckpointID intentionally omitted
		Answer:  "yes",
		AgentID: "agent/bot1",
	}

	data, err := callAgentResume(t, env.h, server.NewTestClient("alice"), params)
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "queued", resp["status"])

	payload := decodeAgentResumePayload(t, env.broker)
	assert.Equal(t, "cp-auto", payload.CheckpointID)
}

// TestAgentResumeHandler_InterruptIDFilter verifies that interrupt_id filters
// which question is answered.
func TestAgentResumeHandler_InterruptIDFilter(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	seedConversation(t, env, "conv-1", "cp-1")
	seedQuestion(t, env, "q-1", "conv-1", "cp-1", "intr-1", "Q1?")
	seedQuestion(t, env, "q-2", "conv-1", "cp-1", "intr-2", "Q2?")

	// Answer only intr-2.
	params := agentResumeParams{
		ConversationID: "conv-1",
		InterruptID:    "intr-2",
		Answer:         "answer-for-2",
		AgentID:        "agent/bot1",
	}

	data, err := callAgentResume(t, env.h, server.NewTestClient("alice"), params)
	require.NoError(t, err)

	// q-1 is still pending → partial.
	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "partial", resp["status"])

	// Now answer intr-1 → all answered → queued.
	params2 := agentResumeParams{
		ConversationID: "conv-1",
		InterruptID:    "intr-1",
		Answer:         "answer-for-1",
		AgentID:        "agent/bot1",
	}
	data2, err := callAgentResume(t, env.h, server.NewTestClient("alice"), params2)
	require.NoError(t, err)

	resp2 := parseAgentResumeResponseMap(t, data2)
	assert.Equal(t, "queued", resp2["status"])
	require.Len(t, env.broker.enqueued, 1)
}

// TestAgentResumeHandler_NilClient verifies that a nil client does not panic
// and still succeeds (sender_id will be empty).
func TestAgentResumeHandler_NilClient(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	seedConversation(t, env, "conv-1", "cp-1")
	seedQuestion(t, env, "q-1", "conv-1", "cp-1", "intr-1", "What?")

	params := agentResumeParams{
		ConversationID: "conv-1",
		Answer:         "yes",
		AgentID:        "agent/bot1",
	}

	data, err := callAgentResume(t, env.h, nil, params)
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "queued", resp["status"])

	payload := decodeAgentResumePayload(t, env.broker)
	assert.Empty(t, payload.SenderID, "sender_id should be empty when client is nil")
	assert.Empty(t, payload.DeviceID, "device_id should be empty when client is nil")
}

// TestAgentResumeHandler_WithDeviceID verifies that the device ID from the
// client is forwarded into the task payload (D-102).
func TestAgentResumeHandler_WithDeviceID(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	seedConversation(t, env, "conv-1", "cp-1")
	seedQuestion(t, env, "q-1", "conv-1", "cp-1", "intr-1", "What?")

	params := agentResumeParams{
		ConversationID: "conv-1",
		Answer:         "42",
		AgentID:        "agent/bot1",
	}

	client := server.NewTestClientWithDevice("alice", "device-xyz", "conn-1")
	data, err := callAgentResume(t, env.h, client, params)
	require.NoError(t, err)

	resp := parseAgentResumeResponseMap(t, data)
	assert.Equal(t, "queued", resp["status"])

	payload := decodeAgentResumePayload(t, env.broker)
	assert.Equal(t, "alice", payload.SenderID)
	assert.Equal(t, "device-xyz", payload.DeviceID)
}

// TestAgentResumeHandler_NoPendingQuestion verifies that when there is no
// pending question matching the filter, a not-found error is returned.
func TestAgentResumeHandler_NoPendingQuestion(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	seedConversation(t, env, "conv-1", "cp-1")
	// No questions seeded.

	params := agentResumeParams{
		ConversationID: "conv-1",
		Answer:         "yes",
		AgentID:        "agent/bot1",
	}

	_, err := callAgentResume(t, env.h, server.NewTestClient("alice"), params)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no pending question")
}

// TestAgentResumeHandler_ConversationNotFound verifies that a non-existent
// conversation returns a not-found error.
func TestAgentResumeHandler_ConversationNotFound(t *testing.T) {
	env := newAgentResumeTestEnv(t)

	params := agentResumeParams{
		ConversationID: "conv-nonexistent",
		Answer:         "yes",
		AgentID:        "agent/bot1",
	}

	_, err := callAgentResume(t, env.h, server.NewTestClient("alice"), params)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conversation not found")
}

// TestAgentResumeHandler_NoCheckpointID verifies that when checkpoint_id
// cannot be inferred from the conversation, a validation error is returned.
func TestAgentResumeHandler_NoCheckpointID(t *testing.T) {
	env := newAgentResumeTestEnv(t)
	// Conversation with empty checkpoint_id.
	seedConversation(t, env, "conv-1", "")

	params := agentResumeParams{
		ConversationID: "conv-1",
		Answer:         "yes",
		AgentID:        "agent/bot1",
	}

	_, err := callAgentResume(t, env.h, server.NewTestClient("alice"), params)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint_id")
}

// Compile-time interface check.
var _ interface {
	HandleRequest(context.Context, *server.Client, *protocol.PackageDataRequest) (json.RawMessage, error)
} = (*agentResumeHandler)(nil)
