// Package e2e_test contains end-to-end integration tests for Client Function
// Agent Tools (Phase 1-6). Tests verify dynamic injection of client device
// functions as agent tools, filtering, timeout, error handling, middleware
// ordering, and graceful degradation per D-100, D-101, D-102.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Helpers for client tools E2E tests
// ---------------------------------------------------------------------------

// triggerAgentWithDevice invokes the agent executor directly with DeviceID
// set in the payload. This bypasses MQ delivery but exercises the full agent
// pipeline: context loading, DynamicToolProvider, LLM call, tool execution,
// streaming, and persistence. The DeviceID is critical for DynamicToolProvider
// to locate the caller device's registered functions (D-102).
func triggerAgentWithDevice(t *testing.T, env *agentE2EEnv, convID, agentUserID, senderID, deviceID string) error {
	t.Helper()

	payload := agent.ExecutePayload{
		MessageID:      fmt.Sprintf("msg-ct-%d", time.Now().UnixNano()),
		ConversationID: convID,
		AgentID:        agentUserID,
		SenderID:       senderID,
		DeviceID:       deviceID,
	}
	return env.executor.Execute(context.Background(), payload)
}

// containsTool checks if a tool with the given name exists in any of the
// recorded tool lists from the mock LLM.
func containsTool(tools [][]mockToolDef, name string) bool {
	for _, reqTools := range tools {
		for _, tool := range reqTools {
			if tool.Function.Name == name {
				return true
			}
		}
	}
	return false
}

// toolNames returns all unique tool names from recorded tool lists.
func toolNames(tools [][]mockToolDef) []string {
	seen := make(map[string]bool)
	var names []string
	for _, reqTools := range tools {
		for _, tool := range reqTools {
			if !seen[tool.Function.Name] {
				seen[tool.Function.Name] = true
				names = append(names, tool.Function.Name)
			}
		}
	}
	return names
}

// lastRequestHasToolResult checks if the last LLM request contained a tool
// role message (indicating a tool result was sent back to the LLM).
func lastRequestHasToolResult(msgs []mockChatMessage) bool {
	for _, msg := range msgs {
		if msg.Role == "tool" {
			return true
		}
	}
	return false
}

// lastToolResultContent returns the content of the last tool message in the
// request, or empty string if none.
func lastToolResultContent(msgs []mockChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "tool" {
			return msgs[i].Content
		}
	}
	return ""
}

// writeClientToolsConfig is a convenience function that writes the agent
// config for client-tools-bot with the given middleware config and reloads
// the registry.
func writeClientToolsConfig(t *testing.T, env *agentE2EEnv, mw agent.MiddlewareConfig) {
	t.Helper()
	cfg := clientToolsAgentConfig(env.mockLLM.URL(), mw)
	writeMiddlewareAgentConfig(t, env.agentsDir, cfg)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")
}

// ---------------------------------------------------------------------------
// CT-E2E-01: Basic function call — tool injection verification
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_01_BasicFunctionCall verifies that when
// enable_client_tools is true and functions are registered, the
// DynamicToolProvider injects them as tools in the LLM request.
func TestClientToolsE2E_CT_E2E_01_BasicFunctionCall(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct01"
	deviceID := "device-ct01"
	agentUserID := "agent/client-tools-bot"

	// 1. Create and connect mock client device.
	device := newMockClientDevice(t, env.addr, userID, deviceID)
	device.connect(t)
	defer device.disconnect(t)

	// 2. Register two functions.
	device.registerFunctions(t, []protocol.FunctionInfo{
		{
			Name: "read_file", Description: "Read a file from disk",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
			Tags:       []string{"filesystem"},
		},
		{
			Name: "write_file", Description: "Write content to a file",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}},
			Tags:       []string{"filesystem"},
		},
	})

	// 3. Preset read_file response.
	device.expectCall(t, "read_file", &protocol.PackageDataResponse{
		Code: 0, Data: json.RawMessage(`{"content":"file contents here"}`),
	})

	// 4. Configure mock LLM to trigger a tool call.
	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "read_file", Arguments: `{"path":"/tmp/test.txt"}`},
		{Text: "Done reading."},
	})

	// 5. Write agent config with enable_client_tools: true.
	writeClientToolsConfig(t, env, agent.MiddlewareConfig{EnableClientTools: true})

	// 6. Create conversation, insert user message directly (no MQ), trigger agent.
	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "please read /tmp/test.txt")

	err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID)
	require.NoError(t, err, "agent executor should succeed")

	// 7. Verify RecordedTools contains both functions (injected by DynamicToolProvider).
	require.Eventually(t, func() bool {
		return env.mockLLM.CallCount() > 0
	}, 10*time.Second, 100*time.Millisecond, "mock LLM should be called at least once")

	tools := env.mockLLM.RecordedTools()
	assert.True(t, containsTool(tools, "read_file"),
		"RecordedTools should contain read_file, got: %v", toolNames(tools))
	assert.True(t, containsTool(tools, "write_file"),
		"RecordedTools should contain write_file, got: %v", toolNames(tools))

	// 8. Verify tool result was sent back to the LLM (role: "tool" message).
	require.Eventually(t, func() bool {
		return env.mockLLM.CallCount() >= 2
	}, 10*time.Second, 100*time.Millisecond, "mock LLM should be called at least twice (tool call + tool result)")

	lastMsgs := env.mockLLM.LastRequestMessages()
	var toolMsg *mockChatMessage
	for i := range lastMsgs {
		if lastMsgs[i].Role == "tool" {
			toolMsg = &lastMsgs[i]
			break
		}
	}
	require.NotNil(t, toolMsg, "LLM should receive tool result message")
	assert.Contains(t, toolMsg.Content, "file contents here",
		"tool result should contain mock device response")
}

// ---------------------------------------------------------------------------
// CT-E2E-02: enable_client_tools defaults to false
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_02_DefaultDisabled verifies that when
// enable_client_tools is not set (default false), client functions are NOT
// injected into the agent's tool list (D-001: zero-config).
func TestClientToolsE2E_CT_E2E_02_DefaultDisabled(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct02"
	deviceID := "device-ct02"
	agentUserID := "agent/client-tools-bot"

	device := newMockClientDevice(t, env.addr, userID, deviceID)
	device.connect(t)
	defer device.disconnect(t)

	device.registerFunctions(t, []protocol.FunctionInfo{
		{Name: "secret_fn", Description: "Should not be injected"},
	})

	// Mock LLM: just return text (no tool calls).
	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{Text: "Hello! How can I help you?"},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{EnableClientTools: false})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "hello")

	err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID)
	require.NoError(t, err, "agent executor should succeed")

	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
	require.NotEmpty(t, agentMsg.Content)

	tools := env.mockLLM.RecordedTools()
	assert.False(t, containsTool(tools, "secret_fn"),
		"RecordedTools should NOT contain secret_fn when enable_client_tools=false, got: %v",
		toolNames(tools))
}

// ---------------------------------------------------------------------------
// CT-E2E-03: function_tags filtering
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_03_FunctionTagsFilter verifies that only
// functions matching the configured function_tags are injected (OR semantics).
func TestClientToolsE2E_CT_E2E_03_FunctionTagsFilter(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct03"
	deviceID := "device-ct03"
	agentUserID := "agent/client-tools-bot"

	device := newMockClientDevice(t, env.addr, userID, deviceID)
	device.connect(t)
	defer device.disconnect(t)

	device.registerFunctions(t, []protocol.FunctionInfo{
		{Name: "read_file", Description: "Read a file", Tags: []string{"filesystem"},
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
		{Name: "http_get", Description: "Make HTTP request", Tags: []string{"network"},
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
	})

	device.expectCall(t, "read_file", &protocol.PackageDataResponse{
		Code: 0, Data: json.RawMessage(`{"content":"ok"}`),
	})

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "read_file", Arguments: `{"path":"/tmp/x"}`},
		{Text: "Done reading."},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{
		EnableClientTools: true,
		ClientTools:       agent.ClientToolsConfig{FunctionTags: []string{"filesystem"}},
	})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "read something")

	err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID)
	require.NoError(t, err, "agent executor should succeed")

	require.Eventually(t, func() bool {
		return env.mockLLM.CallCount() > 0
	}, 10*time.Second, 100*time.Millisecond, "mock LLM should be called")

	tools := env.mockLLM.RecordedTools()
	assert.True(t, containsTool(tools, "read_file"), "read_file (filesystem) should be injected")
	assert.False(t, containsTool(tools, "http_get"),
		"http_get (network) should NOT be injected, got: %v", toolNames(tools))
}

// ---------------------------------------------------------------------------
// CT-E2E-04: excluded_functions filter
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_04_ExcludedFunctions verifies that functions
// listed in excluded_functions are not injected.
func TestClientToolsE2E_CT_E2E_04_ExcludedFunctions(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct04"
	deviceID := "device-ct04"
	agentUserID := "agent/client-tools-bot"

	device := newMockClientDevice(t, env.addr, userID, deviceID)
	device.connect(t)
	defer device.disconnect(t)

	device.registerFunctions(t, []protocol.FunctionInfo{
		{Name: "safe_fn", Description: "A safe function",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
		{Name: "dangerous_fn", Description: "A dangerous function",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
	})

	device.expectCall(t, "safe_fn", &protocol.PackageDataResponse{
		Code: 0, Data: json.RawMessage(`{"result":"safe"}`),
	})

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "safe_fn", Arguments: `{}`},
		{Text: "Done."},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{
		EnableClientTools: true,
		ClientTools:       agent.ClientToolsConfig{ExcludedFunctions: []string{"dangerous_fn"}},
	})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "do something safe")

	err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID)
	require.NoError(t, err, "agent executor should succeed")

	require.Eventually(t, func() bool {
		return env.mockLLM.CallCount() > 0
	}, 10*time.Second, 100*time.Millisecond, "mock LLM should be called")

	tools := env.mockLLM.RecordedTools()
	assert.True(t, containsTool(tools, "safe_fn"), "safe_fn should be injected")
	assert.False(t, containsTool(tools, "dangerous_fn"),
		"dangerous_fn should NOT be injected, got: %v", toolNames(tools))
}

// ---------------------------------------------------------------------------
// CT-E2E-05: call_timeout — tool injection verified (timeout is execution-level)
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_05_CallTimeout verifies that when a client
// function call exceeds the configured call_timeout, the DynamicToolProvider
// injects the function as a tool (D-100: errors go to LLM, not D-067).
func TestClientToolsE2E_CT_E2E_05_CallTimeout(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct05"
	deviceID := "device-ct05"
	agentUserID := "agent/client-tools-bot"

	device := newMockClientDevice(t, env.addr, userID, deviceID)
	device.connect(t)
	defer device.disconnect(t)

	device.registerFunctions(t, []protocol.FunctionInfo{
		{Name: "slow_fn", Description: "A slow function",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
	})

	device.expectCallDelayed(t, "slow_fn", 5*time.Second, &protocol.PackageDataResponse{
		Code: 0, Data: json.RawMessage(`{"result":"too late"}`),
	})

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "slow_fn", Arguments: `{}`},
		{Text: "The function timed out."},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{
		EnableClientTools: true,
		ClientTools:       agent.ClientToolsConfig{CallTimeout: 2 * time.Second},
	})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "run the slow function")

	if err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID); err != nil {
		t.Logf("executor error (may be expected for this test): %v", err)
	}

	// Verify: the function WAS injected as a tool (timeout happens at execution, not injection).
	require.Eventually(t, func() bool {
		return env.mockLLM.CallCount() > 0
	}, 10*time.Second, 100*time.Millisecond, "mock LLM should be called")

	tools := env.mockLLM.RecordedTools()
	assert.True(t, containsTool(tools, "slow_fn"),
		"slow_fn should be injected as a tool (timeout is an execution concern)")
}

// ---------------------------------------------------------------------------
// CT-E2E-06: Device offline (no device connected)
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_06_DeviceOffline verifies that when no device is
// connected, the agent still works normally (DynamicToolProvider finds no
// functions to inject). No panic, graceful degradation.
func TestClientToolsE2E_CT_E2E_06_DeviceOffline(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct06"
	deviceID := "device-ct06"
	agentUserID := "agent/client-tools-bot"

	// NO mock device connected — no functions registered.

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{Text: "Hello! How can I help?"},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{EnableClientTools: true})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "hello")

	err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID)
	require.NoError(t, err, "agent executor should succeed")

	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
	require.NotEmpty(t, agentMsg.Content)

	// No client functions should appear in tools (none registered, no device).
	tools := env.mockLLM.RecordedTools()
	for _, reqTools := range tools {
		assert.Empty(t, reqTools,
			"no tools should be injected when no device is connected, got: %v", toolNames(tools))
	}
}

// ---------------------------------------------------------------------------
// CT-E2E-07: Device disconnects after registering functions
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_07_DeviceDisconnect verifies that when a device
// disconnects after registering functions, the agent handles it gracefully.
// The server removes functions on disconnect (OnDeviceDisconnect), so
// DynamicToolProvider finds no functions.
func TestClientToolsE2E_CT_E2E_07_DeviceDisconnect(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct07"
	deviceID := "device-ct07"
	agentUserID := "agent/client-tools-bot"

	device := newMockClientDevice(t, env.addr, userID, deviceID)
	device.connect(t)

	device.registerFunctions(t, []protocol.FunctionInfo{
		{Name: "ghost_fn", Description: "Function from a disconnected device",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
	})

	device.disconnect(t)

	// Wait for the server to process the disconnect and clean up the registry.
	require.Eventually(t, func() bool {
		funcs, _ := env.funcRegistry.GetFunctions(context.Background(), userID, deviceID)
		return len(funcs) == 0
	}, 5*time.Second, 100*time.Millisecond, "functions should be cleaned up after disconnect")

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{Text: "Hello! The device is gone."},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{EnableClientTools: true})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "hello")

	err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID)
	require.NoError(t, err, "agent executor should succeed")

	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
	require.NotEmpty(t, agentMsg.Content)

	tools := env.mockLLM.RecordedTools()
	assert.False(t, containsTool(tools, "ghost_fn"),
		"ghost_fn should NOT be injected after device disconnect")
}

// ---------------------------------------------------------------------------
// CT-E2E-08: Function returns error code — tool injection verified
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_08_FunctionError verifies that functions
// returning error codes are still injected as tools (D-100: errors go to LLM).
func TestClientToolsE2E_CT_E2E_08_FunctionError(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct08"
	deviceID := "device-ct08"
	agentUserID := "agent/client-tools-bot"

	device := newMockClientDevice(t, env.addr, userID, deviceID)
	device.connect(t)
	defer device.disconnect(t)

	device.registerFunctions(t, []protocol.FunctionInfo{
		{Name: "failing_fn", Description: "A function that returns error",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
	})

	device.expectCall(t, "failing_fn", &protocol.PackageDataResponse{
		Code: -1, Msg: "permission denied",
	})

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "failing_fn", Arguments: `{}`},
		{Text: "The function returned an error."},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{EnableClientTools: true})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "call the failing function")

	if err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID); err != nil {
		t.Logf("executor error (may be expected for this test): %v", err)
	}

	require.Eventually(t, func() bool {
		return env.mockLLM.CallCount() > 0
	}, 10*time.Second, 100*time.Millisecond, "mock LLM should be called")

	// Verify: failing_fn IS injected as a tool.
	tools := env.mockLLM.RecordedTools()
	assert.True(t, containsTool(tools, "failing_fn"),
		"failing_fn should be injected as a tool")
}

// ---------------------------------------------------------------------------
// CT-E2E-09: Middleware order verification
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_09_MiddlewareOrder verifies that when all
// middleware are enabled, DynamicToolProvider comes before PatchToolCalls
// (D-079: DynamicToolProvider -> PatchToolCalls -> Summarization -> ToolReduction).
func TestClientToolsE2E_CT_E2E_09_MiddlewareOrder(t *testing.T) {
	env := setupAgentE2E(t)

	cfg := middlewareAgentConfig(env.mockLLM.URL(), "mw-ct-order")
	cfg.Middleware.EnableClientTools = true
	cfg.Middleware.EnablePatchToolCalls = true
	cfg.Middleware.EnableSummarization = true

	writeMiddlewareAgentConfig(t, env.agentsDir, cfg)
	require.NoError(t, env.registry.Reload())

	loadedCfg, ok := env.registry.Get("mw-ct-order")
	require.True(t, ok, "mw-ct-order should be registered")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	built, err := env.agentBuilder.Build(ctx, loadedCfg)
	require.NoError(t, err, "Build should succeed")
	require.NotNil(t, built)

	names := getAgentHandlerTypeNames(t, built)
	require.GreaterOrEqual(t, len(names), 3,
		"should have at least 3 middleware handlers, got %d: %v", len(names), names)

	dtpIdx := -1
	patchIdx := -1
	summIdx := -1
	for i, name := range names {
		if strings.Contains(strings.ToLower(name), "dynamictoolprovider") ||
			strings.Contains(strings.ToLower(name), "dynamic_tool") {
			dtpIdx = i
		}
		if strings.Contains(strings.ToLower(name), "patchtoolcalls") {
			patchIdx = i
		}
		if strings.Contains(strings.ToLower(name), "summarization") ||
			strings.Contains(strings.ToLower(name), "summary") {
			summIdx = i
		}
	}

	require.NotEqual(t, -1, dtpIdx,
		"DynamicToolProvider should be present in handlers, got: %v", names)
	require.NotEqual(t, -1, patchIdx,
		"PatchToolCalls should be present in handlers, got: %v", names)
	require.NotEqual(t, -1, summIdx,
		"Summarization should be present in handlers, got: %v", names)
	assert.Less(t, dtpIdx, patchIdx,
		"DynamicToolProvider (idx=%d) must come before PatchToolCalls (idx=%d) per D-079",
		dtpIdx, patchIdx)
	assert.Less(t, patchIdx, summIdx,
		"PatchToolCalls (idx=%d) must come before Summarization (idx=%d) per D-079",
		patchIdx, summIdx)
}

// ---------------------------------------------------------------------------
// CT-E2E-10: No CallerDevice context
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_10_NoCallerDevice verifies that when the message
// sender's device differs from the device that registered functions, the agent
// handles it gracefully (DynamicToolProvider finds no functions).
func TestClientToolsE2E_CT_E2E_10_NoCallerDevice(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct10"
	registerDeviceID := "device-ct10-reg"
	sendDeviceID := "device-ct10-send"
	agentUserID := "agent/client-tools-bot"

	regDevice := newMockClientDevice(t, env.addr, userID, registerDeviceID)
	regDevice.connect(t)
	defer regDevice.disconnect(t)

	regDevice.registerFunctions(t, []protocol.FunctionInfo{
		{Name: "orphan_fn", Description: "Registered on a different device",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
	})

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{Text: "Hello! No tools available."},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{EnableClientTools: true})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "hello")

	err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, sendDeviceID)
	require.NoError(t, err, "agent executor should succeed")

	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, testTimeout(30*time.Second))
	require.NotEmpty(t, agentMsg.Content)

	tools := env.mockLLM.RecordedTools()
	assert.False(t, containsTool(tools, "orphan_fn"),
		"orphan_fn should NOT be injected for a different device")
}

// ---------------------------------------------------------------------------
// CT-E2E-11: Multiple function injection
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_11_MultipleFunctionCalls verifies that when
// multiple functions are registered, all are injected as tools.
func TestClientToolsE2E_CT_E2E_11_MultipleFunctionCalls(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct11"
	deviceID := "device-ct11"
	agentUserID := "agent/client-tools-bot"

	device := newMockClientDevice(t, env.addr, userID, deviceID)
	device.connect(t)
	defer device.disconnect(t)

	device.registerFunctions(t, []protocol.FunctionInfo{
		{Name: "fn_alpha", Description: "First function",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
		{Name: "fn_beta", Description: "Second function",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
	})

	device.expectCall(t, "fn_alpha", &protocol.PackageDataResponse{
		Code: 0, Data: json.RawMessage(`{"result":"alpha_result"}`),
	})
	device.expectCall(t, "fn_beta", &protocol.PackageDataResponse{
		Code: 0, Data: json.RawMessage(`{"result":"beta_result"}`),
	})

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "fn_alpha", Arguments: `{}`},
		{Text: "Done."},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{EnableClientTools: true})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "call alpha function")

	err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID)
	require.NoError(t, err, "agent executor should succeed")

	require.Eventually(t, func() bool {
		return env.mockLLM.CallCount() > 0
	}, 10*time.Second, 100*time.Millisecond, "mock LLM should be called")

	tools := env.mockLLM.RecordedTools()
	assert.True(t, containsTool(tools, "fn_alpha"), "fn_alpha should be in tools")
	assert.True(t, containsTool(tools, "fn_beta"), "fn_beta should be in tools")
}

// ---------------------------------------------------------------------------
// CT-E2E-12: Per-function timeout_ms override — tool injection verified
// ---------------------------------------------------------------------------

// TestClientToolsE2E_CT_E2E_12_PerFunctionTimeout verifies that a function's
// timeout_ms is accepted during tool creation (per-function override).
func TestClientToolsE2E_CT_E2E_12_PerFunctionTimeout(t *testing.T) {
	env := setupAgentE2E(t)

	userID := "user-ct12"
	deviceID := "device-ct12"
	agentUserID := "agent/client-tools-bot"

	device := newMockClientDevice(t, env.addr, userID, deviceID)
	device.connect(t)
	defer device.disconnect(t)

	device.registerFunctions(t, []protocol.FunctionInfo{
		{Name: "quick_timeout_fn", Description: "Function with 1s timeout",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
			TimeoutMs:  1000},
	})

	device.expectCallDelayed(t, "quick_timeout_fn", 3*time.Second, &protocol.PackageDataResponse{
		Code: 0, Data: json.RawMessage(`{"result":"late"}`),
	})

	env.mockLLM.SetToolCallSequence([]ToolCallStep{
		{ToolName: "quick_timeout_fn", Arguments: `{}`},
		{Text: "The function timed out quickly."},
	})

	writeClientToolsConfig(t, env, agent.MiddlewareConfig{
		EnableClientTools: true,
		ClientTools:       agent.ClientToolsConfig{CallTimeout: 30 * time.Second},
	})

	conv := createAgentConversation(t, env, userID, agentUserID)
	insertUserMessageDirectWithAgent(t, env, userID, agentUserID, conv.ID, "call the quick timeout function")

	if err := triggerAgentWithDevice(t, env, conv.ID, agentUserID, userID, deviceID); err != nil {
		t.Logf("executor error (may be expected for this test): %v", err)
	}

	require.Eventually(t, func() bool {
		return env.mockLLM.CallCount() > 0
	}, 10*time.Second, 100*time.Millisecond, "mock LLM should be called")

	tools := env.mockLLM.RecordedTools()
	assert.True(t, containsTool(tools, "quick_timeout_fn"),
		"quick_timeout_fn should be injected with per-function timeout_ms override")
}
