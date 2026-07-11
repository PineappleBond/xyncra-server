package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	agenttools "github.com/PineappleBond/xyncra-server/internal/agent/tools"
	"github.com/PineappleBond/xyncra-server/internal/handler"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// agentE2EEnv — extends e2eEnv with agent-specific components
// ---------------------------------------------------------------------------

// agentE2EEnv embeds the base e2eEnv and adds all agent subsystem components
// needed for end-to-end agent testing. The base environment provides the
// WebSocket server, message broker, Redis, and SQLite database; the agent
// extensions add the mock LLM, agent registry, executor, and related stores.
type agentE2EEnv struct {
	*e2eEnv                             // embedded base environment
	mockLLM      *mockLLMServer         // OpenAI-compatible mock LLM server
	registry     *agent.AgentRegistry   // agent configuration registry
	executor     *agent.AgentExecutor   // agent execution orchestrator
	agentBuilder *agent.AgentBuilder    // agent builder (for assertions)
	agentsDir    string                 // temp directory containing agent .md configs
	lock         agent.ConversationLock // per-conversation lock (Redis-backed)
}

// ---------------------------------------------------------------------------
// setupAgentE2E — full agent test environment setup
// ---------------------------------------------------------------------------

// testLogger is a no-op logger for agent components in E2E tests.
// agent.Logger and server.Logger are structurally identical interfaces.
type testLogger struct{}

func (testLogger) Info(string, ...any)  {}
func (testLogger) Error(string, ...any) {}
func (testLogger) Debug(string, ...any) {}

// setupAgentE2E creates a complete agent E2E test environment. It:
//
//  1. Calls setupE2ETest to initialize the base infrastructure (DB, Redis,
//     broker, WebSocket server, message handlers).
//  2. Creates a mock LLM server and sets the mock API key env var.
//  3. Creates a temp agents directory with two pre-configured agents:
//     "test-bot" (no tools) and "tool-bot" (with weather + time tools).
//  4. Creates the full agent pipeline: LLMClientFactory → AgentBuilder →
//     AgentExecutor → BroadcastHelper → ContextManager.
//  5. Creates Redis-backed stores for idempotency, conversation locks, and
//     HITL checkpoints.
//  6. Registers agent task handlers (TypeAgentProcess, TypeAgentResume) on
//     the existing broker's task handler.
//  7. Registers agent RPC handlers (reload_agents, agent_resume) on the
//     existing message handler.
//  8. Registers t.Cleanup for all resources.
//
// The returned agentE2EEnv can be used directly in test functions to send
// messages to agents and wait for replies.
func setupAgentE2E(t *testing.T) *agentE2EEnv {
	t.Helper()

	// 1. Base E2E environment (Redis, SQLite, broker, WS server).
	base := setupE2ETest(t)

	// 2. Set mock API key env var. The agent config references this env var
	// in api_key_env; the LLMClientFactory reads it at Build time.
	t.Setenv("XYNCRA_TEST_MOCK_API_KEY", "mock-test-key-for-e2e")

	// 3. Mock LLM server.
	mockLLM := newMockLLMServer()
	t.Cleanup(func() { mockLLM.Close() })

	// 4. Create agents directory with default test agent configs.
	agentsDir := t.TempDir()
	writeAgentConfig(t, agentsDir, basicAgentConfig(mockLLM.URL()))
	writeAgentConfig(t, agentsDir, toolAgentConfig(mockLLM.URL()))

	// 5. Agent registry — load configs from the temp directory.
	agentRegistry := agent.NewRegistry()
	require.NoError(t, agentRegistry.Load(agentsDir))

	// 6. LLM client factory + agent builder.
	llmFactory := agent.NewLLMClientFactory()
	agentBuilder := agent.NewAgentBuilder(llmFactory)
	agentBuilder.SetToolRegistry(agenttools.DefaultRegistry)
	agentBuilder.SetRegistry(agentRegistry)

	// 7. Redis client for idempotency, conversation lock, and checkpoints.
	//    Uses a dedicated client (same pattern as production main.go, D-074).
	redisAgentClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	t.Cleanup(func() { _ = redisAgentClient.Close() })

	// 8. Checkpoint store for HITL support (D-083).
	checkpointStore := agent.NewRedisCheckPointStore(redisAgentClient, "", 0)
	agentBuilder.SetCheckPointStore(checkpointStore)

	// 9. Stream bridge (50ms throttle, D-051).
	streamBridge := agent.NewStreamBridge()

	// 10. Broadcast helper — wires agent broadcasts to the WebSocket server.
	broadcastHelper := agent.NewBroadcastHelper(base.srv, base.srv.Logger())

	// 11. Context manager — loads conversation history from the DB.
	contextManager := agent.NewDBContextManager(base.store.MessageStore())

	// 12. Agent executor — orchestrates the full pipeline.
	agentExecutor := agent.NewAgentExecutor(
		agentRegistry,
		contextManager,
		agentBuilder,
		streamBridge,
		broadcastHelper,
		base.store,
		5, // maxConcurrent: lower for tests
		testLogger{},
	)

	// 13. Idempotency store (Redis SETNX).
	idempotencyStore := agent.NewRedisIdempotencyStore(redisAgentClient)

	// 14. Conversation lock (Redis SETNX + Lua release).
	conversationLock := agent.NewRedisConversationLock(redisAgentClient)

	// 15. Register agent task handlers on the existing task handler.
	agentTaskHandler := agent.NewAgentTaskHandler(agentExecutor, idempotencyStore, conversationLock, testLogger{})
	base.taskHandler.Register("mq:agent_process", agentTaskHandler)

	agentResumeHandler := agent.NewAgentResumeHandler(agentExecutor, agentRegistry, conversationLock, testLogger{})
	base.taskHandler.Register("mq:agent_resume", agentResumeHandler)

	// 16. Register agent RPC handlers on the existing message handler.
	//     RegisterAll replaces previously-registered methods, which is fine
	//     because handler.RegisterAll re-registers ALL standard methods plus
	//     the agent-specific ones (reload_agents, agent_resume).
	handler.RegisterAll(base.msgHandler, handler.Dependencies{
		ConnStore:     base.connStore,
		Store:         base.store,
		Broker:        base.broker,
		BroadcastFn:   base.srv.BroadcastUpdates,
		AgentRegistry: agentRegistry,
	})

	return &agentE2EEnv{
		e2eEnv:       base,
		mockLLM:      mockLLM,
		registry:     agentRegistry,
		executor:     agentExecutor,
		agentBuilder: agentBuilder,
		agentsDir:    agentsDir,
		lock:         conversationLock,
	}
}

// ---------------------------------------------------------------------------
// Agent config helpers
// ---------------------------------------------------------------------------

// writeAgentConfig writes an agent configuration file (.md) to the specified
// directory. The config is serialized as YAML front matter followed by the
// system prompt body, matching the format expected by ParseFrontMatter.
func writeAgentConfig(t *testing.T, dir string, config *agent.AgentConfig) {
	t.Helper()

	content := fmt.Sprintf(`---
id: %s
name: %s
description: %s
model: %s
api_key_env: %s
base_url: %s
parameters:
  temperature: %.1f
  max_tokens: %d
context:
  max_tokens: %d
  max_messages: %d
%s---
%s
`,
		config.ID,
		config.Name,
		config.Description,
		config.Model,
		config.APIKeyEnv,
		config.BaseURL,
		config.Parameters.Temperature,
		config.Parameters.MaxTokens,
		config.Context.MaxTokens,
		config.Context.MaxMessages,
		formatToolsYAML(config.Tools),
		config.SystemPrompt,
	)

	err := os.WriteFile(filepath.Join(dir, config.ID+".md"), []byte(content), 0644)
	require.NoError(t, err, "write agent config for %s", config.ID)
}

// formatToolsYAML returns the YAML "tools:" block for the given tool names.
// Returns an empty string if tools is empty.
func formatToolsYAML(tools []string) string {
	if len(tools) == 0 {
		return ""
	}
	result := "tools:\n"
	for _, t := range tools {
		result += fmt.Sprintf("  - %s\n", t)
	}
	return result
}

// basicAgentConfig returns an AgentConfig for a simple test bot without tools.
// The bot uses the mock LLM server at mockURL for completions.
// The BaseURL includes "/v1" because the Eino OpenAI ChatModel appends
// "/chat/completions" to it (matching the OpenAI API URL structure).
func basicAgentConfig(mockURL string) *agent.AgentConfig {
	return &agent.AgentConfig{
		ID:          "test-bot",
		Name:        "Test Bot",
		Description: "Test agent for e2e tests",
		Model:       "gpt-4",
		APIKeyEnv:   "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:     mockURL + "/v1",
		Parameters: agent.AgentParameters{
			Temperature: 0.7,
			MaxTokens:   1000,
		},
		Context: agent.AgentContext{
			MaxTokens:   4000,
			MaxMessages: 10,
		},
		SystemPrompt: "You are a test assistant. Be concise.",
	}
}

// toolAgentConfig returns an AgentConfig for a test bot with tools enabled.
// The bot has access to get_weather and get_current_time tools.
func toolAgentConfig(mockURL string) *agent.AgentConfig {
	return &agent.AgentConfig{
		ID:          "tool-bot",
		Name:        "Tool Bot",
		Description: "Agent with tools for testing",
		Model:       "gpt-4",
		APIKeyEnv:   "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:     mockURL + "/v1",
		Parameters: agent.AgentParameters{
			Temperature: 0.7,
			MaxTokens:   2000,
		},
		Context: agent.AgentContext{
			MaxTokens:   4000,
			MaxMessages: 10,
		},
		Tools:        []string{"get_weather", "get_current_time"},
		SystemPrompt: "You are a helpful assistant with access to weather and time tools.",
	}
}

// ---------------------------------------------------------------------------
// Agent interaction helpers
// ---------------------------------------------------------------------------

// sendToAgent sends a message to an agent through the full WebSocket→handler→
// MQ→executor pipeline. It:
//  1. Connects the user via WebSocket.
//  2. Sends a send_message RPC.
//  3. Reads the RPC response (asserts success).
//  4. Drains ephemeral updates (typing, streaming) and consumes the sender's
//     own message push update.
//
// The agent starts processing immediately after the message is persisted, so
// ephemeral typing/streaming updates may arrive before the sender's message
// echo. This helper skips those ephemeral updates (Seq=0) to find the
// persisted message push (Seq>0).
//
// The returned *wsConn can be passed to waitForAgentReply to wait for the
// agent's response. The caller is responsible for closing the connection.
func sendToAgent(t *testing.T, env *agentE2EEnv, userID, agentUserID, convID, message string) *wsConn {
	t.Helper()

	// Connect user.
	conn := connectClient(t, env.addr, userID)

	// Send message via send_message RPC.
	clientMsgID := fmt.Sprintf("agent-msg-%s-%d", userID, time.Now().UnixNano())
	sendRequest(t, conn, "req-agent-1", "send_message", map[string]interface{}{
		"conversation_id":   convID,
		"client_message_id": clientMsgID,
		"content":           message,
		"type":              "text",
	})

	// Read the RPC response — should succeed.
	resp := readResponse(t, conn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code,
		"send_message to agent should succeed, got code=%d msg=%s", resp.Code, resp.Msg)

	// Drain ephemeral updates (typing, streaming with Seq=0) and consume the
	// sender's own message push update (Seq>0, type="message").
	// The agent starts processing immediately via MQ, so typing/streaming
	// updates may arrive before the sender's message echo (C-10).
	deadline := time.Now().Add(10 * time.Second)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("sendToAgent: timed out waiting for sender's message push")
		}
		updates := waitForUpdate(t, conn, remaining)
		for _, u := range updates.Updates {
			// Ephemeral updates have Seq=0 — skip them.
			if u.Seq == 0 {
				continue
			}
			// Found a persisted update — this should be the sender's message push.
			if u.Type == protocol.UpdateTypeMessage {
				return conn
			}
		}
	}
}

// createAgentConversation creates a 1-on-1 conversation between a human user
// and an agent user in the database. The agentUserID must have the format
// "agent/{id}" (D-054).
func createAgentConversation(t *testing.T, env *agentE2EEnv, userID, agentUserID string) *model.Conversation {
	t.Helper()

	conv := &model.Conversation{
		ID:      fmt.Sprintf("conv-agent-%s-%s", userID, agentUserID),
		UserID1: userID,
		UserID2: agentUserID,
		Type:    "1-on-1",
	}
	err := env.store.ConversationStore().Create(context.Background(), conv)
	require.NoError(t, err, "create agent conversation should succeed")
	return conv
}

// waitForAgentReply waits for a persisted message update from the agent on
// the given WebSocket connection. It filters out ephemeral updates (streaming,
// typing, agent_status) and returns only when a "message" type update with
// sender_id matching "agent/{agentID}" is received.
//
// Returns the message content and the full update. Times out after the
// specified duration.
func waitForAgentReply(t *testing.T, conn *wsConn, agentID string, timeout time.Duration) string {
	t.Helper()

	agentUserID := fmt.Sprintf("agent/%s", agentID)
	deadline := time.Now().Add(timeout)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("waitForAgentReply: timed out after %v waiting for message from %s", timeout, agentUserID)
		}

		updates := waitForUpdate(t, conn, remaining)

		for _, u := range updates.Updates {
			// Skip ephemeral updates: streaming, typing, agent_status, agent_question.
			if u.Type == protocol.UpdateTypeStreaming ||
				u.Type == protocol.UpdateTypeTyping ||
				u.Type == protocol.UpdateTypeAgentStatus ||
				u.Type == protocol.UpdateTypeAgentQuestion ||
				u.Type == protocol.UpdateTypeAgentCheckpointCreated ||
				u.Type == protocol.UpdateTypeAgentTimeout {
				continue
			}

			if u.Type == protocol.UpdateTypeMessage {
				var msg model.Message
				require.NoError(t, json.Unmarshal(u.Payload, &msg))
				if msg.SenderID == agentUserID {
					return msg.Content
				}
			}
		}
	}
}

// waitForEphemeral waits for an ephemeral update of the specified type on the
// given WebSocket connection. It skips all other update types. Common types:
//   - protocol.UpdateTypeStreaming (D-050, D-051)
//   - protocol.UpdateTypeTyping (D-050, D-065)
//   - protocol.UpdateTypeAgentStatus (D-087)
//   - protocol.UpdateTypeAgentQuestion (D-087)
//
// Returns the matching PackageDataUpdates or fails the test on timeout.
func waitForEphemeral(t *testing.T, conn *wsConn, updateType string, timeout time.Duration) *protocol.PackageDataUpdates {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("waitForEphemeral: timed out after %v waiting for update type %q", timeout, updateType)
		}

		updates := waitForUpdate(t, conn, remaining)

		for _, u := range updates.Updates {
			if u.Type == updateType {
				return updates
			}
		}
	}
}

// waitForAgentStreamDone waits for the streaming "is_done" signal from the
// agent. This is the final streaming update before the agent persists its
// message. Returns the final streamed text.
func waitForAgentStreamDone(t *testing.T, conn *wsConn, agentID string, timeout time.Duration) string {
	t.Helper()

	agentUserID := fmt.Sprintf("agent/%s", agentID)
	deadline := time.Now().Add(timeout)
	var lastText string

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("waitForAgentStreamDone: timed out after %v waiting for stream done from %s", timeout, agentUserID)
		}

		updates := waitForUpdate(t, conn, remaining)

		for _, u := range updates.Updates {
			if u.Type != protocol.UpdateTypeStreaming {
				continue
			}

			var payload struct {
				UserID string `json:"user_id"`
				Text   string `json:"text"`
				IsDone bool   `json:"is_done"`
			}
			require.NoError(t, json.Unmarshal(u.Payload, &payload))

			if payload.UserID == agentUserID {
				lastText = payload.Text
				if payload.IsDone {
					return lastText
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

// assertAgentMessagePersisted verifies that the agent's response message was
// persisted to the database with the correct sender_id and conversation_id.
func assertAgentMessagePersisted(t *testing.T, env *agentE2EEnv, convID, agentUserID, expectedContent string) {
	t.Helper()

	ctx := context.Background()
	var messages []*model.Message
	env.db.DB().WithContext(ctx).
		Where("conversation_id = ? AND sender_id = ?", convID, agentUserID).
		Order("message_id DESC").
		Limit(1).
		Find(&messages)

	require.Len(t, messages, 1, "agent message should be persisted")
	assert.Equal(t, expectedContent, messages[0].Content, "persisted content should match")
	assert.Equal(t, agentUserID, messages[0].SenderID, "sender_id should be the agent")
}

// ---------------------------------------------------------------------------
// Mock LLM configuration helpers
// ---------------------------------------------------------------------------

// configureMockForGreeting sets up the mock LLM to respond to simple greetings.
// This is the default behavior; call this after ResetCounters if needed.
func configureMockForGreeting(m *mockLLMServer) {
	m.SetResponse("hello", "Hello! I'm the test bot. How can I help you?")
}

// configureMockForError sets up the mock LLM to return HTTP 500 when the
// message contains "error_trigger".
func configureMockForError(m *mockLLMServer) {
	// Default behavior already handles "error_trigger" → HTTP 500.
	// This function exists for explicit test setup clarity.
}
