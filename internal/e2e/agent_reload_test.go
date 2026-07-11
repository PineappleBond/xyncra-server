// Package e2e_test contains Category J config reload E2E tests for the Agent
// system (Phase 1-8). Tests verify hot-reload behavior of AgentRegistry:
// adding new agents, removing deleted agents, skipping invalid files, and
// ensuring reload does not affect in-progress tasks (D-076, D-077).
package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
)

// ---------------------------------------------------------------------------
// Category J: Config Reload (AE-RELOAD)
// ---------------------------------------------------------------------------

// TestAgentReload_AE_RELOAD_001 verifies that reload_agents loads a newly
// added agent configuration from disk. After writing a new .md file to the
// agents directory and calling Reload(), the new agent must be retrievable
// via registry.Get (D-076, D-077).
func TestAgentReload_AE_RELOAD_001(t *testing.T) {
	env := setupAgentE2E(t)

	// Write a brand-new agent config to the agents directory.
	newAgent := &agent.AgentConfig{
		ID:          "reload-bot",
		Name:        "Reload Bot",
		Description: "Agent added after initial load",
		Model:       "gpt-4",
		APIKeyEnv:   "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:     env.mockLLM.URL() + "/v1",
		Parameters: agent.AgentParameters{
			Temperature: 0.5,
			MaxTokens:   500,
		},
		Context: agent.AgentContext{
			MaxTokens:   4000,
			MaxMessages: 10,
		},
		SystemPrompt: "You are a reload test bot.",
	}
	writeAgentConfig(t, env.agentsDir, newAgent)

	// Before reload, the new agent must not be present.
	_, found := env.registry.Get("reload-bot")
	assert.False(t, found, "new agent should not exist before reload")

	// Reload from disk.
	err := env.registry.Reload()
	require.NoError(t, err, "Reload() should succeed")

	// After reload, the new agent must be available.
	cfg, found := env.registry.Get("reload-bot")
	require.True(t, found, "new agent should exist after reload")
	assert.Equal(t, "Reload Bot", cfg.Name)
	assert.Equal(t, "You are a reload test bot.", cfg.SystemPrompt)

	// Pre-existing agents must still be present.
	_, foundTestBot := env.registry.Get("test-bot")
	assert.True(t, foundTestBot, "test-bot should still exist after reload")
	_, foundToolBot := env.registry.Get("tool-bot")
	assert.True(t, foundToolBot, "tool-bot should still exist after reload")
}

// TestAgentReload_AE_RELOAD_002 verifies that reload_agents removes an agent
// whose configuration file has been deleted from disk. After removing the
// .md file and calling Reload(), the old agent must no longer be retrievable
// (D-076).
func TestAgentReload_AE_RELOAD_002(t *testing.T) {
	env := setupAgentE2E(t)

	// Verify test-bot exists before deletion.
	_, found := env.registry.Get("test-bot")
	require.True(t, found, "test-bot should exist before file deletion")

	// Delete the test-bot.md file from disk.
	err := os.Remove(filepath.Join(env.agentsDir, "test-bot.md"))
	require.NoError(t, err, "removing test-bot.md should succeed")

	// Reload from disk.
	err = env.registry.Reload()
	require.NoError(t, err, "Reload() should succeed")

	// test-bot must no longer be available.
	_, found = env.registry.Get("test-bot")
	assert.False(t, found, "test-bot should not exist after file deletion and reload")

	// tool-bot (still on disk) must remain available.
	_, found = env.registry.Get("tool-bot")
	assert.True(t, found, "tool-bot should still exist after reload")
}

// TestAgentReload_AE_RELOAD_003 verifies that reload_agents skips files with
// invalid YAML front matter without returning an error, and that other valid
// agents continue to load correctly (D-076).
func TestAgentReload_AE_RELOAD_003(t *testing.T) {
	env := setupAgentE2E(t)

	// Write an invalid YAML agent file to the agents directory.
	invalidContent := []byte("---\nthis is not: [valid: yaml: {{{\n---\ninvalid body\n")
	err := os.WriteFile(filepath.Join(env.agentsDir, "broken-bot.md"), invalidContent, 0644)
	require.NoError(t, err, "writing broken config should succeed")

	// Reload must not return an error — invalid files are skipped and logged.
	err = env.registry.Reload()
	require.NoError(t, err, "Reload() should succeed even with invalid files")

	// The broken agent must not be registered.
	_, found := env.registry.Get("broken-bot")
	assert.False(t, found, "broken-bot should not be registered")

	// Valid agents must still be available.
	_, foundTestBot := env.registry.Get("test-bot")
	assert.True(t, foundTestBot, "test-bot should still exist after reload with invalid file")
	_, foundToolBot := env.registry.Get("tool-bot")
	assert.True(t, foundToolBot, "tool-bot should still exist after reload with invalid file")
}

// TestAgentReload_AE_RELOAD_004 verifies that a reload does not invalidate
// AgentConfig pointers already held by in-progress tasks. After Reload(), the
// old pointer must still point to a valid, unmodified config (D-076).
//
// This tests the contract stated in D-076: "reload 期间，正在执行的 Agent
// 任务不受影响（它们持有旧 AgentConfig 指针的引用）".
func TestAgentReload_AE_RELOAD_004(t *testing.T) {
	env := setupAgentE2E(t)

	// Grab a reference to the current test-bot config.
	oldCfg, found := env.registry.Get("test-bot")
	require.True(t, found, "test-bot should exist before reload")
	oldName := oldCfg.Name
	oldModel := oldCfg.Model
	oldSystemPrompt := oldCfg.SystemPrompt

	// Now modify the test-bot config on disk (change name and model).
	modifiedCfg := &agent.AgentConfig{
		ID:          "test-bot",
		Name:        "Modified Test Bot",
		Description: "Modified description",
		Model:       "gpt-4-turbo",
		APIKeyEnv:   "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:     env.mockLLM.URL() + "/v1",
		Parameters: agent.AgentParameters{
			Temperature: 0.9,
			MaxTokens:   2000,
		},
		Context: agent.AgentContext{
			MaxTokens:   8000,
			MaxMessages: 20,
		},
		SystemPrompt: "You are a modified test assistant.",
	}
	writeAgentConfig(t, env.agentsDir, modifiedCfg)

	// Reload from disk.
	err := env.registry.Reload()
	require.NoError(t, err, "Reload() should succeed")

	// The registry now returns the new config.
	newCfg, found := env.registry.Get("test-bot")
	require.True(t, found, "test-bot should exist after reload")
	assert.Equal(t, "Modified Test Bot", newCfg.Name, "registry should have new config")
	assert.Equal(t, "gpt-4-turbo", newCfg.Model)

	// The old pointer must still reference the original, unmodified config.
	assert.Equal(t, oldName, oldCfg.Name, "old pointer name must be unchanged")
	assert.Equal(t, oldModel, oldCfg.Model, "old pointer model must be unchanged")
	assert.Equal(t, oldSystemPrompt, oldCfg.SystemPrompt, "old pointer system prompt must be unchanged")
}
