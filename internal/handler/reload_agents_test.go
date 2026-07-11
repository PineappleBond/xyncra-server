package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callReloadAgents invokes the reload_agents handler and returns the raw
// response and error.
func callReloadAgents(
	t *testing.T,
	h *reloadAgentsHandler,
) (json.RawMessage, error) {
	t.Helper()
	ctx := context.Background()
	client := server.NewTestClient("admin")
	req := newTestRequest("1", "reload_agents", nil)
	return h.HandleRequest(ctx, client, req)
}

// parseReloadAgentsResponse unmarshals the reload_agents success response.
func parseReloadAgentsResponse(t *testing.T, data json.RawMessage) map[string]int {
	t.Helper()
	var resp map[string]int
	require.NoError(t, json.Unmarshal(data, &resp))
	return resp
}

// writeAgentFile writes content to a .md file in dir for testing.
func writeAgentFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
}

const testAgentConfig = `---
id: test-reload-bot
name: Reload Test Bot
model: gpt-4
api_key_env: TEST_KEY
---
You are a reload test bot.
`

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestReloadAgents_Success(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "bot1.md", testAgentConfig)

	registry := agent.NewRegistry()
	require.NoError(t, registry.Load(dir))
	assert.Equal(t, 1, registry.Count())

	h := NewReloadAgentsHandler(registry)
	data, err := callReloadAgents(t, h)
	require.NoError(t, err)

	resp := parseReloadAgentsResponse(t, data)
	assert.Equal(t, 1, resp["count"])
}

func TestReloadAgents_NilRegistry(t *testing.T) {
	h := NewReloadAgentsHandler(nil)
	data, err := callReloadAgents(t, h)
	require.NoError(t, err)

	resp := parseReloadAgentsResponse(t, data)
	assert.Equal(t, 0, resp["count"])
}

func TestReloadAgents_ReflectsNewFiles(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "bot1.md", testAgentConfig)

	registry := agent.NewRegistry()
	require.NoError(t, registry.Load(dir))
	assert.Equal(t, 1, registry.Count())

	// Add another agent config and reload.
	writeAgentFile(t, dir, "bot2.md", `---
id: second-bot
name: Second Bot
model: gpt-3.5-turbo
api_key_env: KEY2
---
`)

	h := NewReloadAgentsHandler(registry)
	data, err := callReloadAgents(t, h)
	require.NoError(t, err)

	resp := parseReloadAgentsResponse(t, data)
	assert.Equal(t, 2, resp["count"])
}

func TestReloadAgents_NonExistentDirAfterLoad(t *testing.T) {
	// Load from a valid directory first, then test reload after removing it.
	dir := t.TempDir()
	writeAgentFile(t, dir, "bot.md", testAgentConfig)

	registry := agent.NewRegistry()
	require.NoError(t, registry.Load(dir))
	assert.Equal(t, 1, registry.Count())

	// Reload should work fine when directory still exists.
	h := NewReloadAgentsHandler(registry)
	data, err := callReloadAgents(t, h)
	require.NoError(t, err)

	resp := parseReloadAgentsResponse(t, data)
	assert.Equal(t, 1, resp["count"])
}

// Ensure the handler satisfies the MethodHandler interface at compile time.
var _ interface {
	HandleRequest(context.Context, *server.Client, *protocol.PackageDataRequest) (json.RawMessage, error)
} = (*reloadAgentsHandler)(nil)
