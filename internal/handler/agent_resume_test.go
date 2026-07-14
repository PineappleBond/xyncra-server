package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
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

// parseAgentResumeResponse unmarshals the agent_resume success response.
func parseAgentResumeResponse(t *testing.T, data json.RawMessage) map[string]string {
	t.Helper()
	var resp map[string]string
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
				CheckpointID: "cp-1",
				AgentID:      "agent/bot1",
			},
		},
		{
			name: "missing checkpoint_id",
			params: agentResumeParams{
				ConversationID: "conv-1",
				AgentID:        "agent/bot1",
			},
		},
		{
			name: "missing agent_id",
			params: agentResumeParams{
				ConversationID: "conv-1",
				CheckpointID:   "cp-1",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			broker := &agentResumeBroker{}
			h := NewAgentResumeHandler(broker)

			_, err := callAgentResume(t, h, server.NewTestClient("alice"), tc.params)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "required")
			assert.Empty(t, broker.enqueued, "no task should be enqueued on validation failure")
		})
	}
}

// TestAgentResumeHandler_InvalidJSON verifies that invalid JSON params
// return a descriptive error.
func TestAgentResumeHandler_InvalidJSON(t *testing.T) {
	broker := &agentResumeBroker{}
	h := NewAgentResumeHandler(broker)

	ctx := context.Background()
	client := server.NewTestClient("alice")
	req := &protocol.PackageDataRequest{
		ID:     "req-invalid",
		Method: "agent_resume",
		Params: json.RawMessage(`not valid json`),
	}

	_, err := h.HandleRequest(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")
}

// TestAgentResumeHandler_EnqueueMQ verifies that valid params result in
// a TypeAgentResume task being enqueued to the broker.
func TestAgentResumeHandler_EnqueueMQ(t *testing.T) {
	broker := &agentResumeBroker{}
	h := NewAgentResumeHandler(broker)

	params := agentResumeParams{
		ConversationID: "conv-1",
		CheckpointID:   "cp-1",
		InterruptID:    "intr-1",
		Answer:         "yes please",
		AgentID:        "agent/weather-bot",
	}

	data, err := callAgentResume(t, h, server.NewTestClient("alice"), params)
	require.NoError(t, err)

	// Response should be {"status": "queued"}.
	resp := parseAgentResumeResponse(t, data)
	assert.Equal(t, "queued", resp["status"])

	// Verify the enqueued task.
	require.Len(t, broker.enqueued, 1)
	payload := decodeAgentResumePayload(t, broker)
	assert.Equal(t, "conv-1", payload.ConversationID)
	assert.Equal(t, "cp-1", payload.CheckpointID)
	assert.Equal(t, "intr-1", payload.InterruptID)
	assert.Equal(t, "yes please", payload.Answer)
	assert.Equal(t, "agent/weather-bot", payload.AgentID)
	assert.Equal(t, "alice", payload.SenderID)
}

// TestAgentResumeHandler_NilClient verifies that a nil client does not panic
// and still enqueues the task (sender_id will be empty).
func TestAgentResumeHandler_NilClient(t *testing.T) {
	broker := &agentResumeBroker{}
	h := NewAgentResumeHandler(broker)

	params := agentResumeParams{
		ConversationID: "conv-1",
		CheckpointID:   "cp-1",
		Answer:         "yes",
		AgentID:        "agent/bot1",
	}

	data, err := callAgentResume(t, h, nil, params)
	require.NoError(t, err)

	resp := parseAgentResumeResponse(t, data)
	assert.Equal(t, "queued", resp["status"])

	payload := decodeAgentResumePayload(t, broker)
	assert.Empty(t, payload.SenderID, "sender_id should be empty when client is nil")
	assert.Empty(t, payload.DeviceID, "device_id should be empty when client is nil")
}

// TestAgentResumeHandler_WithDeviceID verifies that the device ID from the
// client is forwarded into the task payload (D-102).
func TestAgentResumeHandler_WithDeviceID(t *testing.T) {
	broker := &agentResumeBroker{}
	h := NewAgentResumeHandler(broker)

	params := agentResumeParams{
		ConversationID: "conv-1",
		CheckpointID:   "cp-1",
		Answer:         "42",
		AgentID:        "agent/bot1",
	}

	client := server.NewTestClientWithDevice("alice", "device-xyz", "conn-1")
	data, err := callAgentResume(t, h, client, params)
	require.NoError(t, err)

	resp := parseAgentResumeResponse(t, data)
	assert.Equal(t, "queued", resp["status"])

	payload := decodeAgentResumePayload(t, broker)
	assert.Equal(t, "alice", payload.SenderID)
	assert.Equal(t, "device-xyz", payload.DeviceID)
}

// TestAgentResumeHandler_OptionalInterruptID verifies that the interrupt_id
// field is optional — when empty, the task is still enqueued.
func TestAgentResumeHandler_OptionalInterruptID(t *testing.T) {
	broker := &agentResumeBroker{}
	h := NewAgentResumeHandler(broker)

	params := agentResumeParams{
		ConversationID: "conv-1",
		CheckpointID:   "cp-1",
		InterruptID:    "", // empty — should still succeed
		Answer:         "sure",
		AgentID:        "agent/bot1",
	}

	_, err := callAgentResume(t, h, server.NewTestClient("alice"), params)
	require.NoError(t, err)

	payload := decodeAgentResumePayload(t, broker)
	assert.Empty(t, payload.InterruptID, "interrupt_id should be empty in payload")
}

// Compile-time interface check.
var _ interface {
	HandleRequest(context.Context, *server.Client, *protocol.PackageDataRequest) (json.RawMessage, error)
} = (*agentResumeHandler)(nil)
