package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Mock ContextManager
// ---------------------------------------------------------------------------

type mockContextManager struct {
	messages []*model.Message
	err      error
}

func (m *mockContextManager) GetContext(_ context.Context, _ string, _ *AgentConfig) ([]*model.Message, error) {
	return m.messages, m.err
}

func (m *mockContextManager) InvalidateCache(_ string) {}

// ---------------------------------------------------------------------------
// Mock StoreAPI
// ---------------------------------------------------------------------------

type mockStoreAPI struct {
	sendMessageResult *store.SendMessageResult
	sendMessageErr    error
	sendMessageCalls  []sendMessageCall
}

type sendMessageCall struct {
	msg       *model.Message
	memberIDs []string
}

// Only implement the method needed by the executor; all others panic.
func (m *mockStoreAPI) ConversationStore() *store.ConversationStore { panic("not implemented") }
func (m *mockStoreAPI) MessageStore() *store.MessageStore           { panic("not implemented") }
func (m *mockStoreAPI) UserUpdateStore() *store.UserUpdateStore     { panic("not implemented") }
func (m *mockStoreAPI) QuestionStore() *store.QuestionStore         { panic("not implemented") }
func (m *mockStoreAPI) SendMessage(ctx context.Context, msg *model.Message, memberIDs []string) (*store.SendMessageResult, error) {
	m.sendMessageCalls = append(m.sendMessageCalls, sendMessageCall{msg: msg, memberIDs: memberIDs})
	if m.sendMessageErr != nil {
		return nil, m.sendMessageErr
	}
	if m.sendMessageResult != nil {
		return m.sendMessageResult, nil
	}
	return &store.SendMessageResult{Message: msg}, nil
}
func (m *mockStoreAPI) Transaction(_ context.Context, _ func(tx *gorm.DB) error) error {
	panic("not implemented")
}
func (m *mockStoreAPI) BeginTx(_ context.Context) (*store.Tx, error) { panic("not implemented") }
func (m *mockStoreAPI) AutoMigrate(_ context.Context) error          { panic("not implemented") }
func (m *mockStoreAPI) Ping(_ context.Context) error                 { panic("not implemented") }
func (m *mockStoreAPI) HealthCheck(_ context.Context) error          { panic("not implemented") }

// Ensure mockStoreAPI satisfies StoreAPI at compile time.
var _ store.StoreAPI = (*mockStoreAPI)(nil)

// testLogger is a no-op Logger for tests.
type testLogger struct{}

func (testLogger) Info(string, ...any)  {}
func (testLogger) Error(string, ...any) {}
func (testLogger) Debug(string, ...any) {}

// ---------------------------------------------------------------------------
// classifyError tests (D-067)
// ---------------------------------------------------------------------------

func TestClassifyError(t *testing.T) {
	executor := &AgentExecutor{}

	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			"ErrAPIKeyMissing → config error",
			ErrAPIKeyMissing,
			"抱歉，我的配置有误，请联系管理员检查设置。",
		},
		{
			"ErrUnsupportedModel → config error",
			ErrUnsupportedModel,
			"抱歉，我的配置有误，请联系管理员检查设置。",
		},
		{
			"wrapped ErrAPIKeyMissing → config error",
			fmt.Errorf("build agent: %w", ErrAPIKeyMissing),
			"抱歉，我的配置有误，请联系管理员检查设置。",
		},
		{
			"ErrLLMTimeout → timeout message",
			ErrLLMTimeout,
			"抱歉，我暂时无法回复，请稍后重试。",
		},
		{
			"ErrLLMRateLimited → timeout message",
			ErrLLMRateLimited,
			"抱歉，我暂时无法回复，请稍后重试。",
		},
		{
			"ErrContextLoad → context error",
			ErrContextLoad,
			"抱歉，我无法读取对话历史，请重新发送消息。",
		},
		{
			"unknown error → generic message",
			errors.New("something went wrong"),
			"抱歉，处理遇到问题，请稍后重试。",
		},
		{
			"ErrAgentBuild → generic message",
			ErrAgentBuild,
			"抱歉，处理遇到问题，请稍后重试。",
		},
		{
			"ErrAgentNotFound → generic message",
			ErrAgentNotFound,
			"抱歉，处理遇到问题，请稍后重试。",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := executor.classifyError(tc.err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// Execute with unknown agent → ErrAgentNotFound
// ---------------------------------------------------------------------------

func TestExecute_UnknownAgent(t *testing.T) {
	registry := NewRegistry()
	// Do not register any agent — lookup will fail.

	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{})

	payload := ExecutePayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/nonexistent",
		SenderID:       "user/alice",
	}

	err := executor.Execute(context.Background(), payload)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAgentNotFound)
	assert.Contains(t, err.Error(), "nonexistent")
}

// ---------------------------------------------------------------------------
// ExecuteWithErrorMessage with unknown agent → error returned + message persisted
// ---------------------------------------------------------------------------

func TestExecuteWithErrorMessage_UnknownAgent(t *testing.T) {
	registry := NewRegistry()

	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{})

	payload := ExecutePayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/nonexistent",
		SenderID:       "user/alice",
	}

	err := executor.ExecuteWithErrorMessage(context.Background(), payload)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAgentNotFound)

	// An error message should have been persisted via SendMessage.
	require.Len(t, mockStore.sendMessageCalls, 1)
	call := mockStore.sendMessageCalls[0]
	assert.Equal(t, "conv-1", call.msg.ConversationID)
	assert.Equal(t, "agent/nonexistent", call.msg.SenderID)
	assert.Equal(t, "抱歉，处理遇到问题，请稍后重试。", call.msg.Content)
	assert.Equal(t, "text", call.msg.Type)
	assert.Equal(t, "sent", call.msg.Status)
}

// ---------------------------------------------------------------------------
// NewAgentExecutor with maxConcurrent=0 → sem is nil
// ---------------------------------------------------------------------------

func TestNewAgentExecutor_NoSemaphore(t *testing.T) {
	registry := NewRegistry()
	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{})

	// When maxConcurrent=0, sem may be nil or an unlimited semaphore (capacity=0).
	// Either way, Acquire should return nil immediately.
	if executor.sem != nil {
		assert.Equal(t, 0, executor.sem.Stats().Capacity, "sem capacity should be 0 when maxConcurrent=0")
	}
}

// ---------------------------------------------------------------------------
// NewAgentExecutor with maxConcurrent=5 → sem is non-nil
// ---------------------------------------------------------------------------

func TestNewAgentExecutor_WithSemaphore(t *testing.T) {
	registry := NewRegistry()
	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 5, testLogger{})

	require.NotNil(t, executor.sem, "sem should not be nil when maxConcurrent > 0")
	assert.Equal(t, 5, executor.sem.Stats().Capacity)
}

// ---------------------------------------------------------------------------
// Execute with context cancellation and semaphore
// ---------------------------------------------------------------------------

func TestExecute_ContextCancellationWithSemaphore(t *testing.T) {
	registry := NewRegistry()
	// Register an agent so the lookup succeeds.
	registry.Register(&AgentConfig{
		ID:        "agent/test-agent",
		Name:      "Test",
		Model:     "gpt-4",
		APIKeyEnv: "",
	})

	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	// Create executor with semaphore of size 1.
	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 1, testLogger{})

	// Fill the semaphore.
	require.NoError(t, executor.sem.Acquire(context.Background()))

	// Create a context that is already cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	payload := ExecutePayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test-agent",
		SenderID:       "user/alice",
	}

	err := executor.Execute(ctx, payload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")

	// Release the semaphore slot.
	executor.sem.Release()
}

// ---------------------------------------------------------------------------
// Execute sends typing indicator before context loading
// ---------------------------------------------------------------------------

func TestExecute_SendsTypingBeforeContextLoad(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&AgentConfig{
		ID:        "agent/test-agent",
		Name:      "Test",
		Model:     "gpt-4",
		APIKeyEnv: "",
	})

	// Make context loading fail so we can check typing was sent before the error.
	mockCtxMgr := &mockContextManager{err: ErrContextLoad}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{})

	payload := ExecutePayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test-agent",
		SenderID:       "user/alice",
	}

	err := executor.Execute(context.Background(), payload)
	require.Error(t, err)

	// typing=true should have been sent (at least one broadcast call).
	require.GreaterOrEqual(t, len(mockBS.calls), 1)

	// The first broadcast should be typing=true.
	firstPayload := decodeTypingPayload(t, mockBS.calls[0].updates.Updates[0].Payload)
	assert.Equal(t, "conv-1", firstPayload.ConversationID)
	assert.True(t, firstPayload.IsTyping, "first broadcast should be typing=true")
}

// ---------------------------------------------------------------------------
// Helper: decode TypingPayload from raw JSON
// ---------------------------------------------------------------------------

func decodeTypingPayload(t *testing.T, raw []byte) TypingPayload {
	t.Helper()
	var payload TypingPayload
	err := json.Unmarshal(raw, &payload)
	require.NoError(t, err)
	return payload
}

// ---------------------------------------------------------------------------
// sendErrorMessage persists correctly
// ---------------------------------------------------------------------------

func TestSendErrorMessage_PersistsMessage(t *testing.T) {
	mockStore := &mockStoreAPI{}
	executor := &AgentExecutor{store: mockStore}

	payload := ExecutePayload{
		ConversationID: "conv-1",
		AgentID:        "agent/test-bot",
		SenderID:       "user/alice",
	}

	executor.sendErrorMessage(context.Background(), payload, "test error message")

	require.Len(t, mockStore.sendMessageCalls, 1)
	call := mockStore.sendMessageCalls[0]
	assert.Equal(t, "conv-1", call.msg.ConversationID)
	assert.Equal(t, "agent/test-bot", call.msg.SenderID)
	assert.Equal(t, "test error message", call.msg.Content)
	assert.Equal(t, "text", call.msg.Type)
	assert.Equal(t, "sent", call.msg.Status)
	assert.NotEmpty(t, call.msg.ID, "message ID should be generated")
	assert.Contains(t, call.memberIDs, "user/alice")
	assert.Contains(t, call.memberIDs, "agent/test-bot")
}

// ---------------------------------------------------------------------------
// Execute: context load failure clears typing indicator (defer cleanup)
// ---------------------------------------------------------------------------

func TestExecute_ContextLoadFails_TypingCleared(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&AgentConfig{
		ID:        "agent/test-agent",
		Name:      "Test",
		Model:     "gpt-4",
		APIKeyEnv: "",
	})

	mockCtxMgr := &mockContextManager{err: ErrContextLoad}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{})

	payload := ExecutePayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test-agent",
		SenderID:       "user/alice",
	}

	err := executor.Execute(context.Background(), payload)
	require.Error(t, err)

	// Should have at least two broadcast calls: typing=true then typing=false.
	require.GreaterOrEqual(t, len(mockBS.calls), 2,
		"should broadcast typing=true then typing=false on context load failure")

	// First call: typing=true.
	firstTyping := decodeTypingPayload(t, mockBS.calls[0].updates.Updates[0].Payload)
	assert.True(t, firstTyping.IsTyping, "first broadcast should be typing=true")

	// Last call: typing=false (from the defer cleanup).
	lastTyping := decodeTypingPayload(t, mockBS.calls[len(mockBS.calls)-1].updates.Updates[0].Payload)
	assert.False(t, lastTyping.IsTyping, "last broadcast should be typing=false (defer cleanup)")
}

// ---------------------------------------------------------------------------
// sendErrorMessage: store failure does not panic
// ---------------------------------------------------------------------------

func TestSendErrorMessage_StoreFails(t *testing.T) {
	mockStore := &mockStoreAPI{sendMessageErr: fmt.Errorf("db connection lost")}
	executor := &AgentExecutor{
		store:  mockStore,
		logger: testLogger{},
	}

	payload := ExecutePayload{
		ConversationID: "conv-1",
		AgentID:        "agent/test-bot",
		SenderID:       "user/alice",
	}

	// Should not panic even when store.SendMessage fails.
	assert.NotPanics(t, func() {
		executor.sendErrorMessage(context.Background(), payload, "error message")
	})

	// The call was still attempted.
	assert.Len(t, mockStore.sendMessageCalls, 1)
}

// ---------------------------------------------------------------------------
// HeuristicTokenCounter: empty string
// ---------------------------------------------------------------------------

func TestHeuristicTokenCounter_EmptyString(t *testing.T) {
	counter := &HeuristicTokenCounter{}
	assert.Equal(t, 0, counter.CountTokens(""), "empty string should have 0 tokens")
}

// ---------------------------------------------------------------------------
// classifyError: wrapped errors (errors.Is compatibility)
// ---------------------------------------------------------------------------

func TestClassifyError_WrappedErrors(t *testing.T) {
	executor := &AgentExecutor{}

	t.Run("single-wrapped ErrAPIKeyMissing", func(t *testing.T) {
		err := fmt.Errorf("execute agent: %w", ErrAPIKeyMissing)
		result := executor.classifyError(err)
		assert.Equal(t, "抱歉，我的配置有误，请联系管理员检查设置。", result)
	})

	t.Run("double-wrapped ErrAgentBuild and ErrAPIKeyMissing", func(t *testing.T) {
		// Go 1.20+ supports multiple %w verbs; errors.Is matches any of them.
		err := fmt.Errorf("execute agent: %w: %w", ErrAgentBuild, ErrAPIKeyMissing)
		result := executor.classifyError(err)
		// errors.Is(err, ErrAPIKeyMissing) should be true → config error message.
		assert.Equal(t, "抱歉，我的配置有误，请联系管理员检查设置。", result)
	})
}

// ---------------------------------------------------------------------------
// ExecutorOption tests
// ---------------------------------------------------------------------------

func TestWithTotalTimeout_PositiveValue(t *testing.T) {
	registry := NewRegistry()
	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{},
		WithTotalTimeout(30*time.Second))

	assert.Equal(t, 30*time.Second, executor.totalTimeout)
}

func TestWithTotalTimeout_ZeroIgnored(t *testing.T) {
	registry := NewRegistry()
	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{},
		WithTotalTimeout(0))

	// Zero should be ignored; default 120s should remain.
	assert.Equal(t, 120*time.Second, executor.totalTimeout)
}

func TestWithTotalTimeout_NegativeIgnored(t *testing.T) {
	registry := NewRegistry()
	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{},
		WithTotalTimeout(-1*time.Second))

	// Negative should be ignored; default 120s should remain.
	assert.Equal(t, 120*time.Second, executor.totalTimeout)
}

func TestWithTypingTimeout_PositiveValue(t *testing.T) {
	registry := NewRegistry()
	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{},
		WithTypingTimeout(30*time.Second))

	assert.Equal(t, 30*time.Second, executor.typingTimeout)
}

func TestWithTypingTimeout_ZeroIgnored(t *testing.T) {
	registry := NewRegistry()
	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{},
		WithTypingTimeout(0))

	// Zero should be ignored; default 60s should remain.
	assert.Equal(t, 60*time.Second, executor.typingTimeout)
}

func TestWithLLMMetrics_SetsMetrics(t *testing.T) {
	registry := NewRegistry()
	mockCtxMgr := &mockContextManager{}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	metrics := &LogMetrics{logger: testLogger{}}
	executor := NewAgentExecutor(registry, mockCtxMgr, nil, sb, bh, mockStore, 0, testLogger{},
		WithLLMMetrics(metrics))

	assert.NotNil(t, executor.metrics, "WithLLMMetrics should set the metrics field")
}

// ---------------------------------------------------------------------------
// Phase 6: ExecutePayload DeviceID field (DEV-01, DEV-02)
// ---------------------------------------------------------------------------

func TestExecutePayload_DeviceID(t *testing.T) {
	payload := ExecutePayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test",
		SenderID:       "alice",
		DeviceID:       "device-1",
	}
	assert.Equal(t, "device-1", payload.DeviceID)
}

func TestExecutePayload_EmptyDeviceID(t *testing.T) {
	payload := ExecutePayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test",
		SenderID:       "alice",
	}
	assert.Empty(t, payload.DeviceID) // backward compatible: empty = no device info
}

// ---------------------------------------------------------------------------
// Phase 6: Execute injects CallerDevice into context when DeviceID is set
// ---------------------------------------------------------------------------

// contextCapturingContextManager captures the context passed to GetContext
// so tests can verify that CallerDevice was injected.
type contextCapturingContextManager struct {
	capturedCtx context.Context
	err         error
}

func (m *contextCapturingContextManager) GetContext(ctx context.Context, _ string, _ *AgentConfig) ([]*model.Message, error) {
	m.capturedCtx = ctx
	return nil, m.err
}

func (m *contextCapturingContextManager) InvalidateCache(_ string) {}

func TestExecute_DeviceID_InjectedIntoContext(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&AgentConfig{
		ID:    "agent/test-agent",
		Name:  "Test",
		Model: "gpt-4",
	})

	ccMgr := &contextCapturingContextManager{err: ErrContextLoad}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, ccMgr, nil, sb, bh, mockStore, 0, testLogger{})

	payload := ExecutePayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test-agent",
		SenderID:       "alice",
		DeviceID:       "device-42",
	}

	// Execute will fail at context loading, but the context injection happens
	// before that, so we can still verify the CallerDevice was set.
	_ = executor.Execute(context.Background(), payload)

	require.NotNil(t, ccMgr.capturedCtx, "GetContext should have been called")
	device, ok := CallerDeviceFromContext(ccMgr.capturedCtx)
	require.True(t, ok, "CallerDevice should be in context when DeviceID is set")
	assert.Equal(t, "alice", device.UserID)
	assert.Equal(t, "device-42", device.DeviceID)
}

func TestExecute_EmptyDeviceID_NoCallerDeviceInContext(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&AgentConfig{
		ID:    "agent/test-agent",
		Name:  "Test",
		Model: "gpt-4",
	})

	ccMgr := &contextCapturingContextManager{err: ErrContextLoad}
	mockStore := &mockStoreAPI{}
	mockBS := &mockBroadcastServer{}
	bh := NewBroadcastHelper(mockBS, testLogger{})
	sb := NewStreamBridge()

	executor := NewAgentExecutor(registry, ccMgr, nil, sb, bh, mockStore, 0, testLogger{})

	payload := ExecutePayload{
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		AgentID:        "agent/test-agent",
		SenderID:       "alice",
		// DeviceID intentionally empty
	}

	_ = executor.Execute(context.Background(), payload)

	require.NotNil(t, ccMgr.capturedCtx, "GetContext should have been called")
	_, ok := CallerDeviceFromContext(ccMgr.capturedCtx)
	assert.False(t, ok, "CallerDevice should NOT be in context when DeviceID is empty")
}

// ---------------------------------------------------------------------------
// cleanupAfterResume: spy logger verifies error is logged when Delete fails
// (D-112: Delete failure must not block resume, but should be logged)
// ---------------------------------------------------------------------------

// TestCleanupAfterResume_DeleteFails_LogsError verifies that when
// checkpointStore.Delete returns an error, cleanupAfterResume logs the event
// via the logger. The log is at Info level because the failure is non-fatal
// (D-112: TTL 24h safety net will eventually clean up the checkpoint).
func TestCleanupAfterResume_DeleteFails_LogsError(t *testing.T) {
	fs := newFakeDeletableStore()
	fs.deleteErr = fmt.Errorf("simulated redis connection lost")

	e := &AgentExecutor{}
	e.checkpointStore = fs

	// Use a captureLogger to verify the log output.
	logger := &captureLogger{}
	e.cleanupAfterResume(context.Background(), "cp-log", logger)

	// Two info-level logs should have been recorded: one for attempt, one for failure.
	require.Equal(t, 2, logger.infoCount(), "exactly two Info() calls expected (attempt + failure)")

	// Verify the last log message mentions checkpoint cleanup failure.
	lastInfo := logger.lastInfo()
	assert.Contains(t, lastInfo.msg, "checkpoint cleanup failed",
		"log should mention checkpoint cleanup failure")

	// Verify structured fields: checkpoint_id and error.
	assert.True(t, argsContains(lastInfo.args, "checkpoint_id", "cp-log"),
		"log should contain checkpoint_id field")
	assert.True(t, argsContains(lastInfo.args, "error", fs.deleteErr),
		"log should contain the original error")
}
