package client

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// Mock handler that implements UpdateHandler + all 3 new agent optional interfaces
// ---------------------------------------------------------------------------

// statusRecord holds the arguments passed to OnAgentStatus.
type statusRecord struct {
	userID         string
	conversationID string
	status         string
}

// timeoutRecord holds the arguments passed to OnAgentTimeout.
type timeoutRecord struct {
	userID         string
	conversationID string
	reason         string
}

// agentMockHandler is a mock that implements UpdateHandler (via embedding
// mockUpdateHandler), AgentStatusHandler, and AgentTimeoutHandler for testing
// agent ephemeral events.
// D-125: removed AgentQuestionHandler and AgentCheckpointHandler since the
// corresponding ephemeral events were removed.
type agentMockHandler struct {
	mockUpdateHandler
	mu       sync.Mutex
	statuses []statusRecord
	timeouts []timeoutRecord
}

// OnAgentStatus records the status event (implements AgentStatusHandler).
func (h *agentMockHandler) OnAgentStatus(ctx context.Context, userID, conversationID, status string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.statuses = append(h.statuses, statusRecord{
		userID:         userID,
		conversationID: conversationID,
		status:         status,
	})
	return nil
}

// OnAgentTimeout records the timeout event (implements AgentTimeoutHandler).
func (h *agentMockHandler) OnAgentTimeout(ctx context.Context, userID, conversationID, reason string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timeouts = append(h.timeouts, timeoutRecord{
		userID:         userID,
		conversationID: conversationID,
		reason:         reason,
	})
	return nil
}

// ---------------------------------------------------------------------------
// JSON unmarshal tests
// ---------------------------------------------------------------------------

// D-125: TestAgentQuestionPayload_Unmarshal and
// TestAgentCheckpointCreatedPayload_Unmarshal were removed because the
// corresponding payload types (agentQuestionPayload, agentCheckpointCreatedPayload)
// were deleted as part of removing redundant HITL ephemeral events.

func TestAgentStatusPayload_Unmarshal(t *testing.T) {
	raw := `{
		"user_id": "agent/bot",
		"conversation_id": "conv-789",
		"status": "thinking",
		"timestamp": 1700000002
	}`

	var p agentStatusPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal agentStatusPayload: %v", err)
	}

	if p.UserID != "agent/bot" {
		t.Errorf("UserID: got %q, want %q", p.UserID, "agent/bot")
	}
	if p.ConversationID != "conv-789" {
		t.Errorf("ConversationID: got %q, want %q", p.ConversationID, "conv-789")
	}
	if p.Status != "thinking" {
		t.Errorf("Status: got %q, want %q", p.Status, "thinking")
	}
	if p.Timestamp != 1700000002 {
		t.Errorf("Timestamp: got %d, want %d", p.Timestamp, 1700000002)
	}
}

func TestAgentTimeoutPayload_Unmarshal(t *testing.T) {
	raw := `{
		"user_id": "agent/bot",
		"conversation_id": "conv-timeout",
		"reason": "llm_request_timeout",
		"timestamp": 1700000003
	}`

	var p agentTimeoutPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal agentTimeoutPayload: %v", err)
	}

	if p.UserID != "agent/bot" {
		t.Errorf("UserID: got %q, want %q", p.UserID, "agent/bot")
	}
	if p.ConversationID != "conv-timeout" {
		t.Errorf("ConversationID: got %q, want %q", p.ConversationID, "conv-timeout")
	}
	if p.Reason != "llm_request_timeout" {
		t.Errorf("Reason: got %q, want %q", p.Reason, "llm_request_timeout")
	}
	if p.Timestamp != 1700000003 {
		t.Errorf("Timestamp: got %d, want %d", p.Timestamp, 1700000003)
	}
}

// ---------------------------------------------------------------------------
// notifyHandler dispatch tests
// ---------------------------------------------------------------------------

// newAgentTestSyncManager creates a syncManager wired to the given UpdateHandler
// with an in-memory DB and a no-op rpcFn.
func newAgentTestSyncManager(t *testing.T, handler UpdateHandler) *syncManager {
	t.Helper()
	db := newTestStore(t)
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	t.Cleanup(func() { sm.Stop() })
	return sm
}

// mustMarshal marshals v to json.RawMessage, panicking on error.
func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// D-125: TestNotifyHandler_AgentQuestion and TestNotifyHandler_AgentCheckpointCreated
// were removed because the corresponding ephemeral events were removed.

func TestNotifyHandler_AgentStatus(t *testing.T) {
	handler := &agentMockHandler{}
	sm := newAgentTestSyncManager(t, handler)
	ctx := context.Background()

	payload := mustMarshal(t, agentStatusPayload{
		UserID:         "agent/bot",
		ConversationID: "conv-st",
		Status:         "thinking",
		Timestamp:      1700000002,
	})

	update := &protocol.PackageDataUpdate{
		Seq:     0,
		Type:    protocol.UpdateTypeAgentStatus,
		Payload: payload,
	}

	sm.notifyHandler(ctx, update)

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if len(handler.statuses) != 1 {
		t.Fatalf("expected 1 status event, got %d", len(handler.statuses))
	}
	s := handler.statuses[0]
	if s.userID != "agent/bot" {
		t.Errorf("userID: got %q, want %q", s.userID, "agent/bot")
	}
	if s.conversationID != "conv-st" {
		t.Errorf("conversationID: got %q, want %q", s.conversationID, "conv-st")
	}
	if s.status != "thinking" {
		t.Errorf("status: got %q, want %q", s.status, "thinking")
	}
}

func TestNotifyHandler_AgentTimeout(t *testing.T) {
	handler := &agentMockHandler{}
	sm := newAgentTestSyncManager(t, handler)
	ctx := context.Background()

	payload := mustMarshal(t, agentTimeoutPayload{
		UserID:         "agent/bot",
		ConversationID: "conv-to",
		Reason:         "llm_request_timeout",
		Timestamp:      1700000003,
	})

	update := &protocol.PackageDataUpdate{
		Seq:     0,
		Type:    protocol.UpdateTypeAgentTimeout,
		Payload: payload,
	}

	sm.notifyHandler(ctx, update)

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if len(handler.timeouts) != 1 {
		t.Fatalf("expected 1 timeout event, got %d", len(handler.timeouts))
	}
	to := handler.timeouts[0]
	if to.userID != "agent/bot" {
		t.Errorf("userID: got %q, want %q", to.userID, "agent/bot")
	}
	if to.conversationID != "conv-to" {
		t.Errorf("conversationID: got %q, want %q", to.conversationID, "conv-to")
	}
	if to.reason != "llm_request_timeout" {
		t.Errorf("reason: got %q, want %q", to.reason, "llm_request_timeout")
	}
}

// ---------------------------------------------------------------------------
// Handler not implementing optional interface: events should be silently dropped
// ---------------------------------------------------------------------------

func TestNotifyHandler_AgentEvents_DroppedWhenHandlerNotImplemented(t *testing.T) {
	// mockUpdateHandler does not implement AgentStatusHandler etc.,
	// so agent events should be silently dropped (no panic, no error).
	handler := &mockUpdateHandler{}
	sm := newAgentTestSyncManager(t, handler)
	ctx := context.Background()

	// Send all agent event types (D-125: only agent_status and agent_timeout remain).
	for _, tc := range []struct {
		name    string
		typ     string
		payload any
	}{
		{
			name: "agent_status",
			typ:  protocol.UpdateTypeAgentStatus,
			payload: agentStatusPayload{
				UserID: "agent/bot", ConversationID: "conv-1", Status: "idle",
			},
		},
		{
			name: "agent_timeout",
			typ:  protocol.UpdateTypeAgentTimeout,
			payload: agentTimeoutPayload{
				UserID: "agent/bot", ConversationID: "conv-1", Reason: "llm_request_timeout",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := mustMarshal(t, tc.payload)
			update := &protocol.PackageDataUpdate{
				Seq:     0,
				Type:    tc.typ,
				Payload: payload,
			}
			// Should not panic.
			sm.notifyHandler(ctx, update)
		})
	}
}

// ---------------------------------------------------------------------------
// dispatchUpdateTx defensive cases: agent ephemeral types with Seq > 0 return nil
// ---------------------------------------------------------------------------

func TestDispatchUpdateTx_AgentEphemeralTypes_ReturnNil(t *testing.T) {
	ctx := context.Background()

	// Test each agent ephemeral type via ApplyUpdate with Seq > 0.
	// The defensive case in dispatchUpdateTx should return nil (not "unknown
	// update type" error). ApplyUpdate with Seq=1 will go through dedup,
	// dispatchUpdateTx, and advance localMaxSeq.
	// D-125: removed UpdateTypeAgentQuestion and UpdateTypeAgentCheckpointCreated.
	for _, typ := range []string{
		protocol.UpdateTypeAgentStatus,
		protocol.UpdateTypeAgentTimeout,
	} {
		t.Run(typ, func(t *testing.T) {
			// Use a fresh handler per subtest so seq tracking is clean.
			freshHandler := &mockUpdateHandler{}
			freshSM := newAgentTestSyncManager(t, freshHandler)

			update := &protocol.PackageDataUpdate{
				Seq:     1, // deliberately non-zero to exercise the defensive path
				Type:    typ,
				Payload: json.RawMessage(`{}`),
			}
			err := freshSM.ApplyUpdate(ctx, update)
			if err != nil {
				t.Errorf("ApplyUpdate with agent type %q and Seq=1: got error %v, want nil", typ, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Multiple events delivered in sequence
// ---------------------------------------------------------------------------

func TestNotifyHandler_MultipleAgentEvents(t *testing.T) {
	handler := &agentMockHandler{}
	sm := newAgentTestSyncManager(t, handler)
	ctx := context.Background()

	// Send 3 status events and 1 timeout event (D-125: removed question and checkpoint).
	events := []*protocol.PackageDataUpdate{
		{
			Seq:     0,
			Type:    protocol.UpdateTypeAgentStatus,
			Payload: mustMarshal(t, agentStatusPayload{UserID: "agent/bot", ConversationID: "conv-m", Status: "thinking"}),
		},
		{
			Seq:     0,
			Type:    protocol.UpdateTypeAgentStatus,
			Payload: mustMarshal(t, agentStatusPayload{UserID: "agent/bot", ConversationID: "conv-m", Status: "asking_user"}),
		},
		{
			Seq:     0,
			Type:    protocol.UpdateTypeAgentStatus,
			Payload: mustMarshal(t, agentStatusPayload{UserID: "agent/bot", ConversationID: "conv-m", Status: "running"}),
		},
		{
			Seq:     0,
			Type:    protocol.UpdateTypeAgentTimeout,
			Payload: mustMarshal(t, agentTimeoutPayload{UserID: "agent/bot", ConversationID: "conv-m", Reason: "timeout"}),
		},
	}

	for _, update := range events {
		sm.notifyHandler(ctx, update)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if len(handler.statuses) != 3 {
		t.Errorf("expected 3 status events, got %d", len(handler.statuses))
	}
	if len(handler.timeouts) != 1 {
		t.Errorf("expected 1 timeout event, got %d", len(handler.timeouts))
	}
}

// ---------------------------------------------------------------------------
// Conversation update → questions parse flow (D-125)
// ---------------------------------------------------------------------------

// TestConversationUpdate_ParsesAndStoresQuestions verifies that when an
// ephemeral conversation update triggers a get_conversation RPC and the
// response includes HITL questions, those questions are parsed and stored
// in the local QuestionStore (D-125).
func TestConversationUpdate_ParsesAndStoresQuestions(t *testing.T) {
	convID := "conv-q-parse"
	serverConv := &model.Conversation{
		ID:           convID,
		UserID1:      "test-user",
		UserID2:      "agent/bot",
		Type:         "1-on-1",
		AgentStatus:  "asking_user",
		CheckpointID: "cp-test",
	}

	// Questions that the get_conversation RPC should return.
	questions := []*model.Question{
		{ID: "q-1", ConversationID: convID, CheckpointID: "cp-test", InterruptID: "int-1", QuestionText: "Proceed?", Status: "pending"},
		{ID: "q-2", ConversationID: convID, CheckpointID: "cp-test", InterruptID: "int-2", QuestionText: "Are you sure?", Status: "pending"},
	}

	// Build a response that includes both conversation and questions.
	resp := map[string]any{
		"conversation": serverConv,
		"questions":    questions,
	}
	respData, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}

	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method == "get_conversation" {
			return respData, nil
		}
		return json.RawMessage(`{}`), nil
	}

	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	t.Cleanup(func() { sm.Stop() })

	// Send an ephemeral conversation update with updated_at > 0 (triggers RPC).
	payload := conversationUpdatePayload{
		ConversationID: convID,
		Action:         "update",
		UpdatedAt:      time.Now().Unix(),
	}
	payloadData, _ := json.Marshal(payload)
	update := &protocol.PackageDataUpdate{
		Seq:     0, // ephemeral
		Type:    protocol.UpdateTypeConversation,
		Payload: payloadData,
	}

	require.NoError(t, sm.ApplyUpdate(context.Background(), update))

	// Verify questions are stored in the local QuestionStore.
	got, err := db.Questions.GetByConversation(context.Background(), convID)
	require.NoError(t, err)
	require.Len(t, got, 2, "should have stored 2 questions from get_conversation response")

	// Verify question content.
	qMap := make(map[string]*model.Question)
	for _, q := range got {
		qMap[q.ID] = q
	}
	require.Contains(t, qMap, "q-1")
	assert.Equal(t, "Proceed?", qMap["q-1"].QuestionText)
	assert.Equal(t, "pending", qMap["q-1"].Status)
	assert.Equal(t, "int-1", qMap["q-1"].InterruptID)

	require.Contains(t, qMap, "q-2")
	assert.Equal(t, "Are you sure?", qMap["q-2"].QuestionText)

	// Verify conversation was also upserted.
	localConv, err := db.Conversations.Get(context.Background(), convID)
	require.NoError(t, err)
	require.NotNil(t, localConv)
	assert.Equal(t, "asking_user", localConv.AgentStatus)
	assert.Equal(t, "cp-test", localConv.CheckpointID)
}
