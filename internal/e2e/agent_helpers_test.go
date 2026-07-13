package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	agenttools "github.com/PineappleBond/xyncra-server/internal/agent/tools"
	"github.com/PineappleBond/xyncra-server/internal/handler"
	"github.com/PineappleBond/xyncra-server/internal/mq"
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

// testLogger adapts testing.T to the agent.Logger interface.
// Error-level messages are always logged; Info/Debug only in verbose mode.
type testLogger struct {
	t *testing.T
}

func (l testLogger) Info(msg string, args ...any) {
	l.t.Logf("[INFO] "+msg, args...)
}
func (l testLogger) Error(msg string, args ...any) {
	l.t.Logf("[ERROR] "+msg, args...)
}
func (l testLogger) Debug(msg string, args ...any) {
	l.t.Logf("[DEBUG] "+msg, args...)
}

// setupAgentE2E creates a complete agent E2E test environment. It supports
// two LLM modes:
//
//   - Mock mode (default): Creates a mock LLM server and registers two agents
//     ("test-bot" and "tool-bot") for fast, deterministic testing.
//   - Real LLM mode (gated by XYNCRA_TEST_REAL_LLM_ENABLED + XYNCRA_TEST_LLM_API_KEY):
//     Skips the mock server and registers a single "real-test-bot" that
//     connects to a real LLM provider.
//
// Steps:
//
//  1. Calls setupE2ETest to initialize the base infrastructure (DB, Redis,
//     broker, WebSocket server, message handlers).
//  2. Creates a temp agents directory.
//  3. Depending on realLLMMode(), either starts a mock LLM server with two
//     agents, or writes a real LLM agent config.
//  4. Loads agent configs from the temp directory into the registry.
//  5. Creates the full agent pipeline: LLMClientFactory → AgentBuilder →
//     AgentExecutor → BroadcastHelper → ContextManager.
//  6. Creates Redis-backed stores for idempotency, conversation locks, and
//     HITL checkpoints.
//  7. Registers agent task handlers (TypeAgentProcess, TypeAgentResume) on
//     the existing broker's task handler.
//  8. Registers agent RPC handlers (reload_agents, agent_resume) on the
//     existing message handler.
//  9. Registers t.Cleanup for all resources.
//
// The returned agentE2EEnv can be used directly in test functions to send
// messages to agents and wait for replies.
//
// Optional ExecutorOption values (e.g. agent.WithTotalTimeout) override the
// executor defaults. This is particularly useful for weak net tests that need
// shorter timeouts.
func setupAgentE2E(t *testing.T, opts ...agent.ExecutorOption) *agentE2EEnv {
	t.Helper()

	// 1. Base E2E environment (Redis, SQLite, broker, WS server).
	base := setupE2ETest(t)

	// 2. Create agents directory.
	agentsDir := t.TempDir()

	// 3. LLM mode: mock (default) or real LLM.
	var mockLLM *mockLLMServer
	if realLLMMode() {
		// Real LLM mode: skip mock server, write real LLM agent config.
		cfg := realLLMConfig()
		t.Setenv("XYNCRA_TEST_REAL_API_KEY", cfg.APIKey)
		realCfg := realLLMAgentConfig(cfg)
		writeAgentConfig(t, agentsDir, realCfg)
	} else {
		// Mock LLM mode (default): set mock API key and start mock server.
		t.Setenv("XYNCRA_TEST_MOCK_API_KEY", "mock-test-key-for-e2e")
		mockLLM = newMockLLMServer()
		t.Cleanup(func() { mockLLM.Close() })
		writeAgentConfig(t, agentsDir, basicAgentConfig(mockLLM.URL()))
		writeAgentConfig(t, agentsDir, toolAgentConfig(mockLLM.URL()))
	}

	// 4. Agent registry — load configs from the temp directory.
	agentRegistry := agent.NewRegistry()
	require.NoError(t, agentRegistry.Load(agentsDir))

	// 5. LLM client factory + agent builder.
	llmFactory := agent.NewLLMClientFactory()
	agentBuilder := agent.NewAgentBuilder(llmFactory)
	agentBuilder.SetToolRegistry(agenttools.DefaultRegistry)
	agentBuilder.SetRegistry(agentRegistry)
	agentBuilder.SetClientFunctionProvider(base.funcRegistry)
	agentBuilder.SetClientCaller(base.srv)

	// LLM call logger for E2E debugging — writes to a temp file.
	llmLogPath := filepath.Join(t.TempDir(), "llm-calls.log")
	llmLogFile, err := os.Create(llmLogPath)
	require.NoError(t, err, "create LLM log file")
	t.Cleanup(func() { _ = llmLogFile.Close() })
	llmLogger := agent.NewLLMLogger(llmLogFile, true) // indent=true for readability
	agentBuilder.SetLLMLogger(llmLogger)
	t.Logf("LLM call log: %s", llmLogPath)

	// 6. Redis client for idempotency, conversation lock, and checkpoints.
	//    Uses a dedicated client (same pattern as production main.go, D-074).
	redisAgentClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	t.Cleanup(func() { _ = redisAgentClient.Close() })

	// 7. Checkpoint store for HITL support (D-083).
	checkpointStore := agent.NewRedisCheckPointStore(redisAgentClient, "", 0)
	agentBuilder.SetCheckPointStore(checkpointStore)

	// 8. Stream bridge (50ms throttle, D-051).
	streamBridge := agent.NewStreamBridge()

	// 9. Broadcast helper — wires agent broadcasts to the WebSocket server.
	broadcastHelper := agent.NewBroadcastHelper(base.srv, base.srv.Logger())

	// 10. Context manager — loads conversation history from the DB.
	contextManager := agent.NewDBContextManager(base.store.MessageStore())

	// 11. Agent executor — orchestrates the full pipeline.
	agentExecutor := agent.NewAgentExecutor(
		agentRegistry,
		contextManager,
		agentBuilder,
		streamBridge,
		broadcastHelper,
		base.store,
		5, // maxConcurrent: lower for tests
		testLogger{t: t},
		opts...,
	)

	// 12. Idempotency store (Redis SETNX).
	idempotencyStore := agent.NewRedisIdempotencyStore(redisAgentClient)

	// 13. Conversation lock (Redis SETNX + Lua release).
	conversationLock := agent.NewRedisConversationLock(redisAgentClient)

	// 14. Register agent task handlers on the existing task handler.
	agentTaskHandler := agent.NewAgentTaskHandler(agentExecutor, idempotencyStore, conversationLock, testLogger{t: t})
	base.taskHandler.Register("mq:agent_process", agentTaskHandler)

	agentResumeHandler := agent.NewAgentResumeHandler(agentExecutor, agentRegistry, conversationLock, testLogger{t: t})
	base.taskHandler.Register("mq:agent_resume", agentResumeHandler)

	// 15. Register agent RPC handlers on the existing message handler.
	//     RegisterAll replaces previously-registered methods, which is fine
	//     because handler.RegisterAll re-registers ALL standard methods plus
	//     the agent-specific ones (reload_agents, agent_resume).
	handler.RegisterAll(base.msgHandler, handler.Dependencies{
		ConnStore:        base.connStore,
		Store:            base.store,
		Broker:           base.broker,
		BroadcastFn:      base.srv.BroadcastUpdates,
		AgentRegistry:    agentRegistry,
		FunctionRegistry: base.funcRegistry,
		ReverseRPC:       base.srv.ReverseRPC(), // Phase 5 (D-108)
		Logger:           base.srv.Logger(),     // Phase 5 (D-108)
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
// setupAgentE2EWeakNet — agent E2E with weak network simulation
// ---------------------------------------------------------------------------

// setupAgentE2EWeakNet creates an agent E2E environment with weak network
// fault injection enabled on the mock LLM server. It is a convenience wrapper
// around setupAgentE2E that applies the given llmWeakNetConfig to the mock.
//
// The cfg controls fault injection behavior (delays, rate limits, black holes,
// stream disconnects). Optional ExecutorOption values (e.g. agent.WithTotalTimeout)
// can override executor defaults — useful for weak net tests that need shorter
// timeouts to detect failures faster.
//
// Returns nil mockLLM in real LLM mode (weak net config is not applied).
func setupAgentE2EWeakNet(t *testing.T, cfg llmWeakNetConfig, opts ...agent.ExecutorOption) *agentE2EEnv {
	t.Helper()

	env := setupAgentE2E(t, opts...)

	// Apply weak net config to the mock LLM server (only in mock mode).
	if env.mockLLM != nil {
		env.mockLLM.SetWeakNetConfig(cfg)
		t.Cleanup(func() {
			env.mockLLM.ResetWeakNet()
		})
	}

	return env
}

// ---------------------------------------------------------------------------
// HITL resume helper (bypasses MQ)
// ---------------------------------------------------------------------------

// triggerAgentResume invokes the agent resume handler directly, bypassing MQ
// task delivery. This tests the HITL resume pipeline: lock acquire → agent
// build → ResumeWithParams → stream bridge → broadcast → persist.
// Production uses agent_resume RPC (D-085) which enqueues via MQ.
func triggerAgentResume(t *testing.T, env *agentE2EEnv, convID, checkpointID, interruptID, agentUserID, senderID, deviceID, answer string) error {
	t.Helper()

	payload := agent.AgentResumePayload{
		ConversationID: convID,
		CheckpointID:   checkpointID,
		InterruptID:    interruptID,
		Answer:         answer,
		SenderID:       senderID,
		AgentID:        agentUserID,
		DeviceID:       deviceID,
	}

	raw, err := json.Marshal(payload)
	require.NoError(t, err, "marshal resume payload should succeed")

	task := &mq.Task{
		Type:    mq.TypeAgentResume,
		Payload: raw,
	}

	return env.taskHandler.ProcessTask(context.Background(), task)
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

// clientToolsAgentConfig returns an AgentConfig with client tools middleware
// enabled. The caller can customise the full MiddlewareConfig (tags, excluded
// functions, call timeout). Intended for use with writeMiddlewareAgentConfig.
func clientToolsAgentConfig(mockURL string, mw agent.MiddlewareConfig) *agent.AgentConfig {
	cfg := middlewareAgentConfig(mockURL, "client-tools-bot")
	cfg.Middleware = mw
	return cfg
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
	conn := connectClient(t, env.addr, userID, "device-1")

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

// ---------------------------------------------------------------------------
// Real LLM mode detection and helpers
// ---------------------------------------------------------------------------

// realLLMMode returns true if real LLM tests should run.
// Gated by both XYNCRA_TEST_REAL_LLM_ENABLED and XYNCRA_TEST_LLM_API_KEY.
func realLLMMode() bool {
	return os.Getenv("XYNCRA_TEST_REAL_LLM_ENABLED") == "true" &&
		os.Getenv("XYNCRA_TEST_LLM_API_KEY") != "" &&
		os.Getenv("XYNCRA_TEST_LLM_API_KEY") != "your-api-key-here"
}

// realLLMConfig returns the real LLM configuration from env vars.
// BaseURL defaults to the Qwen/DashScope endpoint if not set (D-064, D-089).
func realLLMConfig() struct{ APIKey, BaseURL, Model, Provider string } {
	return struct{ APIKey, BaseURL, Model, Provider string }{
		APIKey:   os.Getenv("XYNCRA_TEST_LLM_API_KEY"),
		BaseURL:  envOrDefault("XYNCRA_TEST_LLM_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		Model:    envOrDefault("XYNCRA_TEST_LLM_MODEL", "qwen3.7-plus"),
		Provider: envOrDefault("XYNCRA_TEST_LLM_PROVIDER", "qwen"),
	}
}

// testTimeout scales a base timeout for real LLM mode.
// In real LLM mode, XYNCRA_TEST_REAL_LLM_TIMEOUT env var can override the
// scaled timeout (D-089).
func testTimeout(base time.Duration) time.Duration {
	if realLLMMode() {
		if timeoutStr := os.Getenv("XYNCRA_TEST_REAL_LLM_TIMEOUT"); timeoutStr != "" {
			if d, err := time.ParseDuration(timeoutStr); err == nil {
				return d
			}
		}
		return base * 6 // 10s → 60s, 5s → 30s
	}
	return base
}

// retryRealLLM retries a test function up to maxRetries times.
// Real LLM APIs may timeout or rate-limit; this handles transient failures.
// fn must return nil on success or an error on failure. Unlike t.Run-based
// approaches, this does not mark the parent test as failed on intermediate
// attempts — only a final failure after all retries are exhausted does.
func retryRealLLM(t *testing.T, maxRetries int, fn func(t *testing.T) error) {
	t.Helper()
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		lastErr = fn(t)
		if lastErr == nil {
			return // success
		}
		if attempt < maxRetries {
			t.Logf("attempt %d failed: %v, retrying...", attempt, lastErr)
			time.Sleep(2 * time.Second) // backoff
		}
	}
	t.Fatalf("all %d attempts failed, last error: %v", maxRetries, lastErr)
}

// envOrDefault returns the env var value or a default.
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// realLLMAgentConfig returns an AgentConfig for real LLM testing.
// Uses lower temperature (0.3) for more deterministic responses.
// MaxTokens can be overridden via XYNCRA_TEST_REAL_LLM_MAX_TOKENS env var (D-089).
func realLLMAgentConfig(cfg struct{ APIKey, BaseURL, Model, Provider string }) *agent.AgentConfig {
	maxTokens := 500
	if mtStr := os.Getenv("XYNCRA_TEST_REAL_LLM_MAX_TOKENS"); mtStr != "" {
		if mt, err := strconv.Atoi(mtStr); err == nil {
			maxTokens = mt
		}
	}
	return &agent.AgentConfig{
		ID:          "real-test-bot",
		Name:        "Real Test Bot",
		Description: "Real LLM e2e test agent",
		Model:       cfg.Model,
		APIKeyEnv:   "XYNCRA_TEST_REAL_API_KEY",
		BaseURL:     cfg.BaseURL,
		Parameters: agent.AgentParameters{
			Temperature: 0.3,       // lower = more deterministic
			MaxTokens:   maxTokens, // limit cost/latency
		},
		Context: agent.AgentContext{
			MaxTokens:   4000,
			MaxMessages: 5, // short conversations to limit cost
		},
		SystemPrompt: "You are a test assistant. Always respond in English. Keep replies under 2 sentences.",
	}
}
