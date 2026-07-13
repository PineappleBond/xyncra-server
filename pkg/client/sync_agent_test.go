package client

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Mock handler that implements UpdateHandler + all 3 new agent optional interfaces
// ---------------------------------------------------------------------------

// questionRecord holds the arguments passed to OnAgentQuestion.
type questionRecord struct {
	userID         string
	conversationID string
	question       string
	checkpointID   string
	interruptID    string
}

// checkpointRecord holds the arguments passed to OnAgentCheckpointCreated.
type checkpointRecord struct {
	userID         string
	conversationID string
	checkpointID   string
}

// statusRecord holds the arguments passed to OnAgentStatus.
type statusRecord struct {
	userID         string
	conversationID string
	status         string
}

// agentMockHandler is a mock that implements UpdateHandler (via embedding
// mockUpdateHandler), AgentQuestionHandler, AgentCheckpointHandler, and
// AgentStatusHandler for testing agent ephemeral events.
type agentMockHandler struct {
	mockUpdateHandler
	mu          sync.Mutex
	questions   []questionRecord
	checkpoints []checkpointRecord
	statuses    []statusRecord
}

// OnAgentQuestion records the HITL question event (implements AgentQuestionHandler).
func (h *agentMockHandler) OnAgentQuestion(ctx context.Context, userID, conversationID, question, checkpointID, interruptID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.questions = append(h.questions, questionRecord{
		userID:         userID,
		conversationID: conversationID,
		question:       question,
		checkpointID:   checkpointID,
		interruptID:    interruptID,
	})
	return nil
}

// OnAgentCheckpointCreated records the checkpoint event (implements AgentCheckpointHandler).
func (h *agentMockHandler) OnAgentCheckpointCreated(ctx context.Context, userID, conversationID, checkpointID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkpoints = append(h.checkpoints, checkpointRecord{
		userID:         userID,
		conversationID: conversationID,
		checkpointID:   checkpointID,
	})
	return nil
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

// ---------------------------------------------------------------------------
// JSON unmarshal tests
// ---------------------------------------------------------------------------

func TestAgentQuestionPayload_Unmarshal(t *testing.T) {
	raw := `{
		"user_id": "agent/bot",
		"conversation_id": "conv-123",
		"question": "Are you sure?",
		"checkpoint_id": "cp-abc",
		"interrupt_id": "int-xyz",
		"timestamp": 1700000000
	}`

	var p agentQuestionPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal agentQuestionPayload: %v", err)
	}

	if p.UserID != "agent/bot" {
		t.Errorf("UserID: got %q, want %q", p.UserID, "agent/bot")
	}
	if p.ConversationID != "conv-123" {
		t.Errorf("ConversationID: got %q, want %q", p.ConversationID, "conv-123")
	}
	if p.Question != "Are you sure?" {
		t.Errorf("Question: got %q, want %q", p.Question, "Are you sure?")
	}
	if p.CheckpointID != "cp-abc" {
		t.Errorf("CheckpointID: got %q, want %q", p.CheckpointID, "cp-abc")
	}
	if p.InterruptID != "int-xyz" {
		t.Errorf("InterruptID: got %q, want %q", p.InterruptID, "int-xyz")
	}
	if p.Timestamp != 1700000000 {
		t.Errorf("Timestamp: got %d, want %d", p.Timestamp, 1700000000)
	}
}

func TestAgentCheckpointCreatedPayload_Unmarshal(t *testing.T) {
	raw := `{
		"user_id": "agent/bot",
		"conversation_id": "conv-456",
		"checkpoint_id": "cp-def",
		"timestamp": 1700000001
	}`

	var p agentCheckpointCreatedPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal agentCheckpointCreatedPayload: %v", err)
	}

	if p.UserID != "agent/bot" {
		t.Errorf("UserID: got %q, want %q", p.UserID, "agent/bot")
	}
	if p.ConversationID != "conv-456" {
		t.Errorf("ConversationID: got %q, want %q", p.ConversationID, "conv-456")
	}
	if p.CheckpointID != "cp-def" {
		t.Errorf("CheckpointID: got %q, want %q", p.CheckpointID, "cp-def")
	}
	if p.Timestamp != 1700000001 {
		t.Errorf("Timestamp: got %d, want %d", p.Timestamp, 1700000001)
	}
}

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

func TestNotifyHandler_AgentQuestion(t *testing.T) {
	handler := &agentMockHandler{}
	sm := newAgentTestSyncManager(t, handler)
	ctx := context.Background()

	payload := mustMarshal(t, agentQuestionPayload{
		UserID:         "agent/bot",
		ConversationID: "conv-q",
		Question:       "Proceed?",
		CheckpointID:   "cp-1",
		InterruptID:    "int-1",
		Timestamp:      1700000000,
	})

	update := &protocol.PackageDataUpdate{
		Seq:     0,
		Type:    protocol.UpdateTypeAgentQuestion,
		Payload: payload,
	}

	sm.notifyHandler(ctx, update)

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if len(handler.questions) != 1 {
		t.Fatalf("expected 1 question event, got %d", len(handler.questions))
	}
	q := handler.questions[0]
	if q.userID != "agent/bot" {
		t.Errorf("userID: got %q, want %q", q.userID, "agent/bot")
	}
	if q.conversationID != "conv-q" {
		t.Errorf("conversationID: got %q, want %q", q.conversationID, "conv-q")
	}
	if q.question != "Proceed?" {
		t.Errorf("question: got %q, want %q", q.question, "Proceed?")
	}
	if q.checkpointID != "cp-1" {
		t.Errorf("checkpointID: got %q, want %q", q.checkpointID, "cp-1")
	}
	if q.interruptID != "int-1" {
		t.Errorf("interruptID: got %q, want %q", q.interruptID, "int-1")
	}
}

func TestNotifyHandler_AgentCheckpointCreated(t *testing.T) {
	handler := &agentMockHandler{}
	sm := newAgentTestSyncManager(t, handler)
	ctx := context.Background()

	payload := mustMarshal(t, agentCheckpointCreatedPayload{
		UserID:         "agent/bot",
		ConversationID: "conv-cp",
		CheckpointID:   "cp-42",
		Timestamp:      1700000001,
	})

	update := &protocol.PackageDataUpdate{
		Seq:     0,
		Type:    protocol.UpdateTypeAgentCheckpointCreated,
		Payload: payload,
	}

	sm.notifyHandler(ctx, update)

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if len(handler.checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint event, got %d", len(handler.checkpoints))
	}
	cp := handler.checkpoints[0]
	if cp.userID != "agent/bot" {
		t.Errorf("userID: got %q, want %q", cp.userID, "agent/bot")
	}
	if cp.conversationID != "conv-cp" {
		t.Errorf("conversationID: got %q, want %q", cp.conversationID, "conv-cp")
	}
	if cp.checkpointID != "cp-42" {
		t.Errorf("checkpointID: got %q, want %q", cp.checkpointID, "cp-42")
	}
}

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

// ---------------------------------------------------------------------------
// Handler not implementing optional interface: events should be silently dropped
// ---------------------------------------------------------------------------

func TestNotifyHandler_AgentEvents_DroppedWhenHandlerNotImplemented(t *testing.T) {
	// mockUpdateHandler does not implement AgentQuestionHandler etc.,
	// so agent events should be silently dropped (no panic, no error).
	handler := &mockUpdateHandler{}
	sm := newAgentTestSyncManager(t, handler)
	ctx := context.Background()

	// Send all three agent event types.
	for _, tc := range []struct {
		name    string
		typ     string
		payload any
	}{
		{
			name: "agent_question",
			typ:  protocol.UpdateTypeAgentQuestion,
			payload: agentQuestionPayload{
				UserID: "agent/bot", ConversationID: "conv-1",
				Question: "ok?", CheckpointID: "cp-1", InterruptID: "int-1",
			},
		},
		{
			name: "agent_checkpoint_created",
			typ:  protocol.UpdateTypeAgentCheckpointCreated,
			payload: agentCheckpointCreatedPayload{
				UserID: "agent/bot", ConversationID: "conv-1", CheckpointID: "cp-2",
			},
		},
		{
			name: "agent_status",
			typ:  protocol.UpdateTypeAgentStatus,
			payload: agentStatusPayload{
				UserID: "agent/bot", ConversationID: "conv-1", Status: "idle",
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
	for _, typ := range []string{
		protocol.UpdateTypeAgentQuestion,
		protocol.UpdateTypeAgentCheckpointCreated,
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

	// Send 2 status events, 1 question, 1 checkpoint.
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
			Seq:  0,
			Type: protocol.UpdateTypeAgentQuestion,
			Payload: mustMarshal(t, agentQuestionPayload{
				UserID: "agent/bot", ConversationID: "conv-m",
				Question: "Confirm?", CheckpointID: "cp-99", InterruptID: "int-99",
			}),
		},
		{
			Seq:  0,
			Type: protocol.UpdateTypeAgentCheckpointCreated,
			Payload: mustMarshal(t, agentCheckpointCreatedPayload{
				UserID: "agent/bot", ConversationID: "conv-m", CheckpointID: "cp-99",
			}),
		},
	}

	for _, update := range events {
		sm.notifyHandler(ctx, update)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if len(handler.statuses) != 2 {
		t.Errorf("expected 2 status events, got %d", len(handler.statuses))
	}
	if len(handler.questions) != 1 {
		t.Errorf("expected 1 question event, got %d", len(handler.questions))
	}
	if len(handler.checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint event, got %d", len(handler.checkpoints))
	}
}
