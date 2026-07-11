// Package e2e_test contains Category I error handling E2E tests for the Agent
// system. Tests verify that agent execution failures are classified into
// user-friendly Chinese error messages and persisted to the database (D-067,
// D-073, D-082).
package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	agenttools "github.com/PineappleBond/xyncra-server/internal/agent/tools"
	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// ---------------------------------------------------------------------------
// Mock ContextManager for AE-ERR-003
// ---------------------------------------------------------------------------

// failingContextManager always returns ErrContextLoad from GetContext.
// Used to test the context-load failure error path (D-067).
type failingContextManager struct{}

func (failingContextManager) GetContext(ctx context.Context, conversationID string, config *agent.AgentConfig) ([]*model.Message, error) {
	return nil, fmt.Errorf("%w: simulated DB failure", agent.ErrContextLoad)
}

func (failingContextManager) InvalidateCache(conversationID string) {}

// ---------------------------------------------------------------------------
// TestAgentErr_AE_ERR_001 — LLM API error → error message (D-067)
// Verifies: When the LLM returns HTTP 500, a user-friendly error message
// containing "暂时无法回复" is persisted with sender_id = "agent/test-bot".
// ---------------------------------------------------------------------------
func TestAgentErr_AE_ERR_001(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-err-001"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "error_trigger")

	payload := agent.ExecutePayload{
		MessageID:      "msg-err-001",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)
	require.Error(t, err, "executor should fail when LLM returns HTTP 500")

	// Verify error message persisted in DB.
	var msgs []*model.Message
	require.Eventually(t, func() bool {
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
			Order("message_id DESC").Limit(1).Find(&msgs)
		return len(msgs) > 0
	}, 30*time.Second, 100*time.Millisecond, "error message should be persisted")

	assert.Contains(t, msgs[0].Content, "暂时无法回复",
		"HTTP 500 error should be classified as LLM transient error (D-067)")
	assert.Equal(t, agentUserID, msgs[0].SenderID, "sender_id should be agent")
}

// ---------------------------------------------------------------------------
// TestAgentErr_AE_ERR_002 — API Key missing → config error message (D-067)
// Verifies: When api_key_env points to a non-existent env var, a "配置有误"
// error message is persisted.
// ---------------------------------------------------------------------------
func TestAgentErr_AE_ERR_002(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-err-002"
	agentID := "no-key-bot"
	agentUser := "agent/" + agentID

	// Create agent config with a non-existent env var for api_key_env.
	badConfig := &agent.AgentConfig{
		ID:           agentID,
		Name:         "No Key Bot",
		Description:  "Agent with missing API key",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_NONEXISTENT_KEY_12345",
		BaseURL:      env.mockLLM.URL() + "/v1",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 1000},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
		SystemPrompt: "You are a test assistant.",
	}
	writeAgentConfig(t, env.agentsDir, badConfig)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	conv := createAgentConversation(t, env, userID, agentUser)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	payload := agent.ExecutePayload{
		MessageID:      "msg-err-002",
		ConversationID: conv.ID,
		AgentID:        agentUser,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)
	require.Error(t, err, "executor should fail when API key is missing")
	require.True(t, errors.Is(err, agent.ErrAPIKeyMissing),
		"error should wrap ErrAPIKeyMissing, got: %v", err)

	// Verify error message persisted in DB.
	var msgs []*model.Message
	require.Eventually(t, func() bool {
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUser).
			Order("message_id DESC").Limit(1).Find(&msgs)
		return len(msgs) > 0
	}, 30*time.Second, 100*time.Millisecond, "error message should be persisted")

	assert.Contains(t, msgs[0].Content, "配置有误",
		"error should be classified as configuration error (D-067)")
	assert.Equal(t, agentUser, msgs[0].SenderID, "sender_id should be agent")
}

// ---------------------------------------------------------------------------
// TestAgentErr_AE_ERR_003 — Context loading failure → error message (D-067)
// Note: This is a component-level test because simulating a real DB failure in
// E2E would require replacing the SQLite driver. Instead, we inject a failing
// ContextManager into a new executor via SetContextManager (testing seam).
// Verifies: "无法读取对话历史" error message is persisted.
// ---------------------------------------------------------------------------
func TestAgentErr_AE_ERR_003(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-err-003"
	agentUserID := "agent/test-bot"

	// Create a custom executor with a failing context manager.
	failingExecutor := agent.NewAgentExecutor(
		env.registry,
		failingContextManager{},
		env.agentBuilder,
		agent.NewStreamBridge(),
		agent.NewBroadcastHelper(env.srv, env.srv.Logger()),
		env.store,
		1,
		testLogger{},
	)

	conv := createAgentConversation(t, env, userID, agentUserID)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	payload := agent.ExecutePayload{
		MessageID:      "msg-err-003",
		ConversationID: conv.ID,
		AgentID:        agentUserID,
		SenderID:       userID,
	}
	err := failingExecutor.ExecuteWithErrorMessage(context.Background(), payload)
	require.Error(t, err, "executor should fail when context loading fails")
	require.True(t, errors.Is(err, agent.ErrContextLoad),
		"error should wrap ErrContextLoad, got: %v", err)

	// Verify error message persisted in DB.
	var msgs []*model.Message
	require.Eventually(t, func() bool {
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
			Order("message_id DESC").Limit(1).Find(&msgs)
		return len(msgs) > 0
	}, 30*time.Second, 100*time.Millisecond, "error message should be persisted")

	assert.Contains(t, msgs[0].Content, "无法读取对话历史",
		"error should be classified as context load failure (D-067)")
	assert.Equal(t, agentUserID, msgs[0].SenderID, "sender_id should be agent")
}

// ---------------------------------------------------------------------------
// TestAgentErr_AE_ERR_004 — Unknown error → generic error message (D-067)
// Verifies: When the agent ID is not found in the registry (an unclassified
// error), the default "处理遇到问题" message is persisted.
// ---------------------------------------------------------------------------
func TestAgentErr_AE_ERR_004(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-err-004"
	agentUser := "agent/nonexistent-bot"

	conv := createAgentConversation(t, env, userID, agentUser)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	payload := agent.ExecutePayload{
		MessageID:      "msg-err-004",
		ConversationID: conv.ID,
		AgentID:        agentUser,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)
	require.Error(t, err, "executor should fail for non-existent agent")
	require.True(t, errors.Is(err, agent.ErrAgentNotFound),
		"error should wrap ErrAgentNotFound, got: %v", err)

	// Verify error message persisted in DB.
	var msgs []*model.Message
	require.Eventually(t, func() bool {
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUser).
			Order("message_id DESC").Limit(1).Find(&msgs)
		return len(msgs) > 0
	}, 30*time.Second, 100*time.Millisecond, "error message should be persisted")

	assert.Contains(t, msgs[0].Content, "处理遇到问题",
		"unknown error should get generic message (D-067)")
	assert.Equal(t, agentUser, msgs[0].SenderID, "sender_id should be agent")
}

// ---------------------------------------------------------------------------
// TestAgentErr_AE_ERR_005 — Tool execution failure → error message (D-082)
// Note: The current implementation uses a fail-open pattern: when a tool
// factory or invocation fails, the error is logged but the agent continues
// without the tool (D-078 spirit). The Eino framework also handles tool
// invocation errors gracefully by returning them as tool results to the LLM.
// This test verifies the system does not crash when tools fail, and checks
// whether any error path produces a persisted message.
// TODO: When tool-specific error classification is added to classifyError
// (D-082), update this test to assert the "工具调用失败" error message.
// ---------------------------------------------------------------------------
func TestAgentErr_AE_ERR_005(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-err-005"
	agentID := "failing-tool-bot"
	agentUser := "agent/" + agentID

	// Register a tool with a factory that always fails.
	agenttools.DefaultRegistry.Register("failing_tool", func(ctx context.Context, config map[string]any) (tool.BaseTool, error) {
		return utils.InferTool(
			"failing_tool",
			"A tool that always fails for testing",
			func(ctx context.Context, input string) (*string, error) {
				return nil, fmt.Errorf("simulated tool execution failure")
			},
		)
	})
	// NOTE: DefaultRegistry does not expose a Deregister method, so "failing_tool"
	// remains registered for the duration of the process. This is a known test
	// isolation limitation — the tool name is unique enough to avoid collisions
	// with other tests.
	t.Cleanup(func() {
		// No-op: DefaultRegistry lacks Deregister. Documented limitation.
	})

	// Create agent config referencing the failing tool.
	failingToolConfig := &agent.AgentConfig{
		ID:           agentID,
		Name:         "Failing Tool Bot",
		Description:  "Agent with a failing tool",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 1000},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
		Tools:        []string{"failing_tool"},
		SystemPrompt: "You are a test assistant with a broken tool.",
	}
	writeAgentConfig(t, env.agentsDir, failingToolConfig)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	conv := createAgentConversation(t, env, userID, agentUser)
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	payload := agent.ExecutePayload{
		MessageID:      "msg-err-005",
		ConversationID: conv.ID,
		AgentID:        agentUser,
		SenderID:       userID,
	}
	err := env.executor.ExecuteWithErrorMessage(context.Background(), payload)

	// Check both paths:
	if err != nil {
		// Path A: Tool error propagated → verify error message was persisted.
		var msgs []*model.Message
		require.Eventually(t, func() bool {
			env.db.DB().WithContext(context.Background()).
				Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUser).
				Order("message_id DESC").Limit(1).Find(&msgs)
			return len(msgs) > 0
		}, 30*time.Second, 100*time.Millisecond, "error message should be persisted")
		assert.NotEmpty(t, msgs[0].Content, "error message content should not be empty")
		assert.Equal(t, agentUser, msgs[0].SenderID, "sender_id should be agent")
		t.Logf("Tool error propagated; error message: %s", msgs[0].Content)
	} else {
		// Path B: Fail-open — agent continued without the failing tool.
		// Verify a message was persisted (either a normal reply or error).
		var msgs []*model.Message
		require.Eventually(t, func() bool {
			env.db.DB().WithContext(context.Background()).
				Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUser).
				Order("message_id DESC").Limit(1).Find(&msgs)
			return len(msgs) > 0
		}, 30*time.Second, 100*time.Millisecond,
			"agent should persist a message (fail-open) or error message")
		assert.NotEmpty(t, msgs[0].Content, "message content should not be empty")
		assert.Equal(t, agentUser, msgs[0].SenderID, "sender_id should be agent")
		t.Logf("Fail-open: agent continued without failing tool; message: %s", msgs[0].Content)
	}
}

// ---------------------------------------------------------------------------
// TestAgentErr_AE_ERR_006 — TaskHandler always returns nil (D-073)
// Verifies: Even when execution fails, the task handler returns nil to MQ
// (preventing retries), and the error message is persisted to the DB.
// ---------------------------------------------------------------------------
func TestAgentErr_AE_ERR_006(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-err-006"
	agentUser := "agent/nonexistent-handler-bot"

	// Create conversation so SendMessage FK succeeds during error persistence.
	conv := createAgentConversation(t, env, userID, agentUser)

	// Create the task handler (no idempotency, no lock — simplified for test).
	handler := agent.NewAgentTaskHandler(env.executor, nil, nil, testLogger{})

	// Build a task payload referencing a non-existent agent.
	taskPayload, err := json.Marshal(agent.AgentProcessPayload{
		MessageID:      "msg-err-006",
		ConversationID: conv.ID,
		AgentID:        agentUser,
		SenderID:       userID,
	})
	require.NoError(t, err)

	task := &mq.Task{
		Type:    "mq:agent_process",
		Payload: taskPayload,
	}

	// Handler should always return nil (D-073).
	result := handler(context.Background(), task)
	assert.Nil(t, result, "handler should always return nil to MQ (D-073)")

	// Verify error message was persisted despite the failure.
	var msgs []*model.Message
	require.Eventually(t, func() bool {
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUser).
			Order("message_id DESC").Limit(1).Find(&msgs)
		return len(msgs) > 0
	}, 30*time.Second, 100*time.Millisecond, "error message should be persisted")

	assert.NotEmpty(t, msgs[0].Content, "error message should have content")
	assert.Equal(t, agentUser, msgs[0].SenderID, "sender_id should be agent")
}
