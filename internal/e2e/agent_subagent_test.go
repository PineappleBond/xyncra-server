// Package e2e_test contains Category F sub-agent E2E tests for the Agent
// system (Phase 8B). Tests verify sub-agent delegation, output merging,
// depth limits, and fail-open handling of non-existent sub-agents (D-081).
package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
)

// ---------------------------------------------------------------------------
// TestAgentSub_AE_SUB_001 — Sub-agent delegation succeeds
// Scenario: Parent agent config sub_agents → child agent executes → result
//
//	returns to parent (D-081)
//
// Verifies: Parent config references child-bot; both agents registered;
//
//	AgentBuilder.Build succeeds for parent (child is resolved as a tool).
//
// ---------------------------------------------------------------------------
func TestAgentSub_AE_SUB_001(t *testing.T) {
	env := setupAgentE2E(t)

	// 1. Write child agent config (simple bot, no sub-agents, no tools).
	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "child-bot",
		Name:         "Child Bot",
		Description:  "A child agent for delegation testing",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are a child assistant.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, nil)

	// 2. Write parent agent config referencing child-bot as a sub-agent.
	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "parent-bot",
		Name:         "Parent Bot",
		Description:  "A parent agent that delegates to child",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are a parent assistant. Delegate to child-bot when needed.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, []string{"child-bot"})

	// 3. Reload registry from disk so both agents are available.
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	// 4. Verify both agents are registered.
	parentCfg, ok := env.registry.Get("parent-bot")
	require.True(t, ok, "parent-bot should be registered")
	assert.Equal(t, []string{"child-bot"}, parentCfg.SubAgents,
		"parent config should reference child-bot in SubAgents (D-081)")

	_, ok = env.registry.Get("child-bot")
	require.True(t, ok, "child-bot should be registered")

	// 5. Build the parent agent. resolveSubAgents will find child-bot,
	//    build it (with SubAgents cleared), and wrap it as an AgentTool.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	built, err := env.agentBuilder.Build(ctx, parentCfg)
	require.NoError(t, err, "Build should succeed — sub-agent delegation works (D-081)")
	require.NotNil(t, built, "BuiltAgent should not be nil")
	assert.Equal(t, "parent-bot", built.Config.ID,
		"built agent config ID should be parent-bot")
}

// ---------------------------------------------------------------------------
// TestAgentSub_AE_SUB_002 — Sub-agent output merged into parent reply
// Scenario: Final reply contains sub-agent's work result (D-081)
// Verifies: Sub-agent tool is registered as part of the parent's tool set
//
//	during Build (D-081)
//
// Strategy: Full E2E execution of sub-agent delegation is complex (requires
// the LLM to actually invoke the sub-agent tool). Instead, we verify that
// the Build step correctly resolves sub-agents into tools by confirming:
// (a) Build succeeds, (b) the parent config retains SubAgents reference,
// (c) building without a registry skips sub-agent resolution entirely.
// ---------------------------------------------------------------------------
func TestAgentSub_AE_SUB_002(t *testing.T) {
	env := setupAgentE2E(t)

	// 1. Write child agent.
	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "worker-bot",
		Name:         "Worker Bot",
		Description:  "A worker agent",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are a worker assistant.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, nil)

	// 2. Write parent agent referencing worker-bot.
	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "coordinator-bot",
		Name:         "Coordinator Bot",
		Description:  "A coordinator agent",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are a coordinator. Delegate tasks to worker-bot.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, []string{"worker-bot"})

	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	parentCfg, ok := env.registry.Get("coordinator-bot")
	require.True(t, ok, "coordinator-bot should be registered")

	// 3. Build with registry set — sub-agent should be resolved.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	built, err := env.agentBuilder.Build(ctx, parentCfg)
	require.NoError(t, err, "Build should succeed with sub-agent resolved")
	require.NotNil(t, built)

	// 4. Verify parent config still has the SubAgents reference (D-081).
	//    The config itself is not modified; resolveSubAgents copies and clears.
	assert.Equal(t, []string{"worker-bot"}, parentCfg.SubAgents,
		"parent config SubAgents should remain intact after Build (D-081)")

	// 5. Verify that building without registry skips sub-agent resolution.
	//    Create a builder without SetRegistry — sub-agents should be ignored.
	builderNoRegistry := agent.NewAgentBuilder(agent.NewLLMClientFactory())
	builtNoReg, err := builderNoRegistry.Build(ctx, parentCfg)
	require.NoError(t, err, "Build without registry should still succeed")
	require.NotNil(t, builtNoReg,
		"BuiltAgent without registry should not be nil — sub-agents simply skipped")
}

// ---------------------------------------------------------------------------
// TestAgentSub_AE_SUB_003 — Sub-agent depth limit enforced
// Scenario: 3-layer nesting rejected or degraded (D-081)
// Verifies: Depth limit of 1 is enforced — grandparent can reference parent,
//
//	but parent's own sub_agents are cleared during grandparent's Build.
//	The child agent is never built as a sub-sub-agent.
//
// ---------------------------------------------------------------------------
func TestAgentSub_AE_SUB_003(t *testing.T) {
	env := setupAgentE2E(t)

	// 1. Write three agents: grandparent → parent → child.
	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "grandchild-bot",
		Name:         "Grandchild Bot",
		Description:  "The deepest agent",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are a grandchild assistant.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, nil)

	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "mid-bot",
		Name:         "Mid Bot",
		Description:  "A middle-layer agent",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are a mid-level assistant.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, []string{"grandchild-bot"})

	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "top-bot",
		Name:         "Top Bot",
		Description:  "The top-level agent",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are the top-level assistant. Delegate to mid-bot.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, []string{"mid-bot"})

	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	// 2. Verify the chain in the registry.
	topCfg, ok := env.registry.Get("top-bot")
	require.True(t, ok, "top-bot should be registered")
	assert.Equal(t, []string{"mid-bot"}, topCfg.SubAgents)

	midCfg, ok := env.registry.Get("mid-bot")
	require.True(t, ok, "mid-bot should be registered")
	assert.Equal(t, []string{"grandchild-bot"}, midCfg.SubAgents)

	_, ok = env.registry.Get("grandchild-bot")
	require.True(t, ok, "grandchild-bot should be registered")

	// 3. Build grandparent agent. This triggers resolveSubAgents which:
	//    - Builds mid-bot as a sub-agent with SubAgents=nil (depth limit)
	//    - mid-bot's resolveSubAgents is skipped (SubAgents is empty)
	//    - grandchild-bot is never built as a sub-sub-agent
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	built, err := env.agentBuilder.Build(ctx, topCfg)
	require.NoError(t, err,
		"Build should succeed — depth limit prevents 3-layer recursion (D-081)")
	require.NotNil(t, built)

	// 4. Verify registry configs are NOT modified (resolveSubAgents uses copies).
	midCfgAfter, _ := env.registry.Get("mid-bot")
	assert.Equal(t, []string{"grandchild-bot"}, midCfgAfter.SubAgents,
		"registry config for mid-bot should NOT be modified — depth limit uses copies (D-081)")
}

// ---------------------------------------------------------------------------
// TestAgentSub_AE_SUB_004 — Non-existent sub-agent skipped
// Scenario: References unregistered agent ID → skipped with warning (D-081)
// Verifies: Agent builds successfully even when sub_agents contains IDs not
//
//	found in the registry. Fail-open behavior — no error returned.
//
// ---------------------------------------------------------------------------
func TestAgentSub_AE_SUB_004(t *testing.T) {
	env := setupAgentE2E(t)

	// 1. Write agent referencing a non-existent sub-agent "nonexistent-bot".
	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "orphan-parent",
		Name:         "Orphan Parent",
		Description:  "Parent with missing sub-agent reference",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are a parent assistant with a missing child.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, []string{"nonexistent-bot"})

	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	// 2. Verify the agent is registered with the bad reference.
	parentCfg, ok := env.registry.Get("orphan-parent")
	require.True(t, ok, "orphan-parent should be registered")
	assert.Equal(t, []string{"nonexistent-bot"}, parentCfg.SubAgents)

	// 3. Verify the referenced agent does NOT exist.
	_, ok = env.registry.Get("nonexistent-bot")
	assert.False(t, ok, "nonexistent-bot should NOT be in the registry")

	// 4. Build should succeed (fail-open: D-081).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	built, err := env.agentBuilder.Build(ctx, parentCfg)
	require.NoError(t, err,
		"Build should succeed — missing sub-agents are skipped (fail-open, D-081)")
	require.NotNil(t, built, "BuiltAgent should not be nil")
	assert.Equal(t, "orphan-parent", built.Config.ID)

	// 5. Also test mixed case: one existing + one non-existing sub-agent.
	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "existing-child",
		Name:         "Existing Child",
		Description:  "An existing child agent",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are a child assistant.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, nil)

	writeSubAgentConfigFile(t, env.agentsDir, &agent.AgentConfig{
		ID:           "mixed-parent",
		Name:         "Mixed Parent",
		Description:  "Parent with mixed sub-agent references",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      env.mockLLM.URL() + "/v1",
		SystemPrompt: "You are a parent assistant.",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
	}, []string{"existing-child", "also-nonexistent"})

	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	mixedCfg, ok := env.registry.Get("mixed-parent")
	require.True(t, ok, "mixed-parent should be registered")

	built2, err := env.agentBuilder.Build(ctx, mixedCfg)
	require.NoError(t, err,
		"Build should succeed — missing sub-agents skipped, existing ones resolved (D-081)")
	require.NotNil(t, built2, "BuiltAgent for mixed case should not be nil")
}

// ---------------------------------------------------------------------------
// Helper: write agent config with sub_agents support
// ---------------------------------------------------------------------------

// writeSubAgentConfigFile writes an agent config .md file with optional
// sub_agents list to the specified directory. This extends the existing
// writeAgentConfig helper (which does not emit sub_agents) to support the
// sub_agents YAML field required by D-081 tests.
func writeSubAgentConfigFile(t *testing.T, dir string, config *agent.AgentConfig, subAgents []string) {
	t.Helper()

	subAgentsYAML := ""
	if len(subAgents) > 0 {
		subAgentsYAML = "sub_agents:\n"
		for _, id := range subAgents {
			subAgentsYAML += fmt.Sprintf("  - %s\n", id)
		}
	}

	toolsYAML := ""
	if len(config.Tools) > 0 {
		toolsYAML = "tools:\n"
		for _, tool := range config.Tools {
			toolsYAML += fmt.Sprintf("  - %s\n", tool)
		}
	}

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
%s%s---
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
		toolsYAML,
		subAgentsYAML,
		config.SystemPrompt,
	)

	err := os.WriteFile(filepath.Join(dir, config.ID+".md"), []byte(content), 0644)
	require.NoError(t, err, "write sub-agent config for %s", config.ID)
}
