// Package e2e_test contains Category C tool system E2E tests for the Agent
// system (Phase 1-8). Tests verify tool invocation via registered tools,
// tool result reflection in replies, fail-open handling of unregistered
// tools (D-078), tool result truncation, retrieval, and TTL expiry (D-080).
package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agenttools "github.com/PineappleBond/xyncra-server/internal/agent/tools"
)

// ---------------------------------------------------------------------------
// TestAgentTools_AE_TOOL_001 — Agent calls a registered tool
// Scenario: LLM returns tool_calls → tool executes → result returned to LLM
// Verifies: Mock LLM called at least twice (tool_call turn + final text turn);
//
//	ToolCallCount > 0 (D-078)
//
// ---------------------------------------------------------------------------
func TestAgentTools_AE_TOOL_001(t *testing.T) {
	env := setupAgentE2E(t)

	// Override the default tool call arguments: get_weather expects "city",
	// not "location". Using the wrong key would cause the tool to fail.
	env.mockLLM.SetToolCallResponse(
		"get_weather",
		`{"city":"Beijing"}`,
		`{"temperature":"22°C","condition":"sunny","humidity":"45%"}`,
	)

	userID := "user-tool-001"
	agentUserID := "agent/tool-bot"

	// 1. Create conversation and send triggering message.
	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := sendUserMessage(t, env, userID, conv.ID, "tool_weather")
	defer conn.Close()

	// 2. Trigger agent processing directly.
	err := triggerAgentProcessing(t, env, "msg-tool-001", conv.ID, agentUserID, userID)
	require.NoError(t, err, "agent executor should succeed")

	// 3. Wait for the agent's reply to be persisted.
	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 30*time.Second)
	assert.NotEmpty(t, agentMsg.Content, "agent reply should not be empty")

	// 4. Verify mock LLM was called at least twice (tool_call turn + final text turn).
	assert.GreaterOrEqual(t, env.mockLLM.CallCount(), 2,
		"mock LLM should be called at least twice: once returning tool_calls, once with final text")

	// 5. Verify at least one tool call was made.
	assert.Greater(t, env.mockLLM.ToolCallCount(), 0,
		"at least one tool call should have been issued (D-078)")
}

// ---------------------------------------------------------------------------
// TestAgentTools_AE_TOOL_002 — Tool result reflected in reply
// Scenario: Agent reply is derived from tool execution output
// Verifies: Agent reply content is non-empty after tool result is processed (D-078)
// ---------------------------------------------------------------------------
func TestAgentTools_AE_TOOL_002(t *testing.T) {
	env := setupAgentE2E(t)

	// Configure the tool to return identifiable weather data.
	env.mockLLM.SetToolCallResponse(
		"get_weather",
		`{"city":"Beijing"}`,
		`{"temperature":"22°C","condition":"sunny","humidity":"45%"}`,
	)

	userID := "user-tool-002"
	agentUserID := "agent/tool-bot"

	// 1. Create conversation and send message that triggers tool call.
	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := sendUserMessage(t, env, userID, conv.ID, "tool_weather")
	defer conn.Close()

	// 2. Trigger agent processing.
	err := triggerAgentProcessing(t, env, "msg-tool-002", conv.ID, agentUserID, userID)
	require.NoError(t, err, "agent executor should succeed")

	// 3. Wait for the agent's reply to be persisted.
	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 30*time.Second)

	// 4. The agent reply should be non-empty (tool result was processed by LLM).
	assert.NotEmpty(t, agentMsg.Content,
		"agent reply should be non-empty — tool result should be reflected in reply (D-078)")

	// 5. Also verify that a tool call happened.
	assert.Greater(t, env.mockLLM.ToolCallCount(), 0,
		"a tool call should have been issued before the final reply")
}

// ---------------------------------------------------------------------------
// TestAgentTools_AE_TOOL_003 — Unregistered tool name skipped
// Scenario: Agent config references a non-existent tool → agent still starts
// Verifies: Agent starts and replies normally; unknown tool is skipped (D-078)
// ---------------------------------------------------------------------------
func TestAgentTools_AE_TOOL_003(t *testing.T) {
	env := setupAgentE2E(t)

	// 1. Write a new agent config that references a non-existent tool
	//    "nonexistent_tool" alongside a valid one.
	brokenCfg := toolAgentConfig(env.mockLLM.URL())
	brokenCfg.ID = "broken-tool"
	brokenCfg.Name = "Broken Tool"
	brokenCfg.Tools = []string{"nonexistent_tool", "get_weather"}
	writeAgentConfig(t, env.agentsDir, brokenCfg)

	// 2. Reload the agent registry from disk. Per D-078 the unknown tool
	//    name is logged as a warning and skipped (fail-open).
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	// 3. Verify the new agent is registered.
	_, ok := env.registry.Get("broken-tool")
	require.True(t, ok, "broken-tool agent should be registered after reload")

	// 4. Create a conversation with the broken-tool agent and send a
	//    simple greeting (no tool trigger).
	userID := "user-tool-003"
	agentUserID := "agent/broken-tool"
	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := sendUserMessage(t, env, userID, conv.ID, "hello")
	defer conn.Close()

	// 5. Trigger agent processing. Even though the config referenced an
	//    unknown tool, the agent should not crash.
	err := triggerAgentProcessing(t, env, "msg-tool-003", conv.ID, agentUserID, userID)
	require.NoError(t, err, "agent executor should succeed despite unknown tool (D-078)")

	// 6. Wait for the agent's reply.
	agentMsg := waitForAgentMessageInDB(t, env, conv.ID, agentUserID, 30*time.Second)
	assert.NotEmpty(t, agentMsg.Content, "agent should still produce a reply")

	// 7. Verify mock LLM was called (agent did invoke the model).
	assert.Greater(t, env.mockLLM.CallCount(), 0,
		"mock LLM should have been called")
}

// ---------------------------------------------------------------------------
// TestAgentTools_AE_TOOL_004 — Tool result truncation
// Scenario: Oversized tool output is truncated, retrieval ID preserved (D-080)
// Verifies: ToolResultStore.Store returns truncated output plus a retrieval ID
// ---------------------------------------------------------------------------
func TestAgentTools_AE_TOOL_004(t *testing.T) {
	// Directly test ToolResultStore truncation. Create a store with a small
	// threshold so we don't need to generate huge content.
	store := agenttools.NewToolResultStore(100, 1*time.Hour)
	store.SetThreshold(100)

	// Build content larger than the threshold (100 runes).
	bigContent := strings.Repeat("x", 200)

	truncated, rid := store.Store("test-id-004", bigContent)

	// 1. Truncated output must be non-empty.
	assert.NotEmpty(t, truncated, "truncated output should not be empty")

	// 2. A retrieval ID must be returned for oversized content.
	assert.NotEmpty(t, rid, "retrieval ID should be assigned for oversized content (D-080)")

	// 3. The returned string includes a truncation marker appended to the
	//    first-threshold runes of the original. Verify the marker is present
	//    and that the leading portion is the truncated prefix (not the full
	//    200-rune input).
	assert.Contains(t, truncated, "result truncated",
		"truncated message should contain truncation marker")
	assert.Contains(t, truncated, rid,
		"truncated message should reference the retrieval ID")

	// The prefix of the returned string must equal the first `threshold`
	// runes of the original content (i.e. not the full 200 runes).
	const threshold = 100
	prefix := truncated[:threshold]
	assert.Equal(t, strings.Repeat("x", threshold), prefix,
		"prefix of truncated output should equal the first threshold runes")

	// 4. The stored full content is retrievable via the returned ID.
	full, ok := store.Retrieve(rid)
	assert.True(t, ok, "stored content should be retrievable")
	assert.Equal(t, bigContent, full, "stored content should match the original")
}

// ---------------------------------------------------------------------------
// TestAgentTools_AE_TOOL_005 — Truncated result retrieval
// Scenario: retrieve_tool_result returns the full content via retrieval ID
// Verifies: ToolResultStore.Retrieve returns the original untruncated content
// ---------------------------------------------------------------------------
func TestAgentTools_AE_TOOL_005(t *testing.T) {
	// Use a fresh store with a long TTL and a small threshold.
	store := agenttools.NewToolResultStore(100, 1*time.Hour)
	store.SetThreshold(50)

	// Build content larger than the threshold.
	bigContent := strings.Repeat("y", 200)

	// Store returns truncated output + retrieval ID.
	_, rid := store.Store("test-id-005", bigContent)
	require.NotEmpty(t, rid, "retrieval ID should be assigned")

	// Retrieve the full content by ID.
	retrieved, ok := store.Retrieve(rid)

	// 1. Retrieval should succeed.
	assert.True(t, ok, "retrieval should succeed for a valid, non-expired ID (D-080)")

	// 2. Retrieved content should match the original (full) content.
	assert.Equal(t, bigContent, retrieved,
		"retrieve_tool_result should return the full untruncated content")
}

// ---------------------------------------------------------------------------
// TestAgentTools_AE_TOOL_006 — Truncated result TTL expiry
// Scenario: After TTL elapses, retrieve returns expired / not-found
// Verifies: ToolResultStore.Retrieve returns false once TTL has passed (D-080)
// ---------------------------------------------------------------------------
func TestAgentTools_AE_TOOL_006(t *testing.T) {
	// Use a very short TTL (50ms) so the test runs quickly.
	store := agenttools.NewToolResultStore(100, 50*time.Millisecond)
	store.SetThreshold(50)

	// Build content larger than the threshold so a retrieval ID is assigned.
	bigContent := strings.Repeat("z", 200)
	_, rid := store.Store("test-id-006", bigContent)
	require.NotEmpty(t, rid, "retrieval ID should be assigned")

	// Immediately: retrieval should succeed.
	_, ok := store.Retrieve(rid)
	require.True(t, ok, "retrieval should succeed before TTL expires")

	// Wait for TTL to elapse.
	time.Sleep(100 * time.Millisecond)

	// 1. After TTL: retrieval should fail.
	content, ok := store.Retrieve(rid)
	assert.False(t, ok,
		"retrieval should fail after TTL expires (D-080)")

	// 2. Returned content should be empty on expired lookup.
	assert.Empty(t, content,
		"content should be empty when retrieval ID has expired")
}
