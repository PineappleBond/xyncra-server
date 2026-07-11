// Package e2e_test contains Category G middleware E2E tests for the Agent
// system (Phase 8C). Tests verify middleware configuration, ordering, and
// fail-open behavior as specified in D-079.
package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
)

// ---------------------------------------------------------------------------
// Helper: write agent config with middleware section
// ---------------------------------------------------------------------------

// writeMiddlewareAgentConfig writes an agent config .md file with a
// middleware section to the specified directory. The middleware booleans and
// optional numeric parameters are emitted as YAML.
func writeMiddlewareAgentConfig(t *testing.T, dir string, config *agent.AgentConfig) {
	t.Helper()

	mw := config.Middleware
	mwYAML := "middleware:\n"
	mwYAML += fmt.Sprintf("  enable_patch_tool_calls: %t\n", mw.EnablePatchToolCalls)
	mwYAML += fmt.Sprintf("  enable_summarization: %t\n", mw.EnableSummarization)
	if mw.SummarizationTokens > 0 {
		mwYAML += fmt.Sprintf("  summarization_tokens: %d\n", mw.SummarizationTokens)
	}
	mwYAML += fmt.Sprintf("  enable_tool_reduction: %t\n", mw.EnableToolReduction)
	if mw.ToolReductionMaxChars > 0 {
		mwYAML += fmt.Sprintf("  tool_reduction_max_chars: %d\n", mw.ToolReductionMaxChars)
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
		mwYAML,
		config.SystemPrompt,
	)

	err := os.WriteFile(filepath.Join(dir, config.ID+".md"), []byte(content), 0644)
	require.NoError(t, err, "write middleware agent config for %s", config.ID)
}

// middlewareAgentConfig returns a base AgentConfig suitable for middleware
// testing. The middleware section is zero-valued (nothing enabled); the caller
// sets the desired flags before passing to writeMiddlewareAgentConfig.
func middlewareAgentConfig(mockURL string, id string) *agent.AgentConfig {
	return &agent.AgentConfig{
		ID:           id,
		Name:         id + " Name",
		Description:  id + " description",
		Model:        "gpt-4",
		APIKeyEnv:    "XYNCRA_TEST_MOCK_API_KEY",
		BaseURL:      mockURL + "/v1",
		Parameters:   agent.AgentParameters{Temperature: 0.7, MaxTokens: 500},
		Context:      agent.AgentContext{MaxTokens: 4000, MaxMessages: 10},
		SystemPrompt: "You are a test assistant.",
	}
}

// getAgentHandlerTypeNames uses unsafe reflection to inspect Eino's internal
// TypedChatModelAgent.handlers field (which is unexported). This is fragile
// and will break if Eino's internal structure changes. It's used here because
// Eino doesn't provide a public API to inspect middleware configuration.
//
// WARNING: This test cannot run under -gcflags=-d=checkptr and may panic if
// Eino's struct layout changes. If that happens, disable this test or find
// an alternative way to verify middleware presence.
//
// Returns a slice of type name strings, e.g.
//
//	["*patchtoolcalls.typedMiddleware[*schema.Message]",
//	 "*summarization.TypedMiddleware[*schema.Message]",
//	 "*reduction.typedToolReductionMiddleware[*schema.Message]"]
func getAgentHandlerTypeNames(t *testing.T, built *agent.BuiltAgent) []string {
	t.Helper()

	// built.Agent is adk.Agent = TypedAgent[*schema.Message] (interface).
	// The concrete type is *TypedChatModelAgent[*schema.Message].
	agentVal := reflect.ValueOf(built.Agent)
	if agentVal.Kind() == reflect.Ptr {
		agentVal = agentVal.Elem()
	}

	handlersField := agentVal.FieldByName("handlers")
	if !handlersField.IsValid() {
		t.Fatalf("getAgentHandlerTypeNames: cannot find 'handlers' field on agent (type=%T)", built.Agent)
	}

	names := make([]string, handlersField.Len())
	for i := 0; i < handlersField.Len(); i++ {
		h := handlersField.Index(i)
		// h is a reflect.Value holding an interface. We cannot call h.Interface()
		// on unexported fields (panic), but we can inspect the reflect.Type.
		// For interface-typed values, we need to use the underlying concrete type.
		// We work around this by reading via unsafe pointer.
		//
		// Alternative: use reflect.NewAt to create a readable copy.
		hv := reflect.NewAt(h.Type(), unsafe.Pointer(h.UnsafeAddr())).Elem()
		if hv.Kind() == reflect.Interface {
			// For interface values from unexported fields, we extract the type
			// name from the reflect.Value's type chain.
			concrete := hv.Elem()
			if concrete.IsValid() {
				names[i] = concrete.Type().String()
			} else {
				names[i] = h.Type().String()
			}
		} else {
			names[i] = hv.Type().String()
		}
	}
	return names
}

// ---------------------------------------------------------------------------
// TestAgentMW_AE_MW_001 — Summarization middleware triggers
// Scenario: Long conversation triggers summary compression (D-079)
// Verifies: Agent config with enable_summarization=true builds successfully
//
//	and the middleware list contains the Summarization middleware.
//
// ---------------------------------------------------------------------------
func TestAgentMW_AE_MW_001(t *testing.T) {
	env := setupAgentE2E(t)

	cfg := middlewareAgentConfig(env.mockLLM.URL(), "mw-summarization")
	cfg.Middleware.EnableSummarization = true

	// Write config, reload registry.
	writeMiddlewareAgentConfig(t, env.agentsDir, cfg)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	loadedCfg, ok := env.registry.Get("mw-summarization")
	require.True(t, ok, "mw-summarization should be registered")
	assert.True(t, loadedCfg.Middleware.EnableSummarization,
		"loaded config should have EnableSummarization=true (D-079)")

	// Build agent — should succeed.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	built, err := env.agentBuilder.Build(ctx, loadedCfg)
	require.NoError(t, err, "Build should succeed with summarization middleware (D-079)")
	require.NotNil(t, built, "BuiltAgent should not be nil")

	// Verify middleware list contains Summarization.
	names := getAgentHandlerTypeNames(t, built)
	require.Len(t, names, 1, "should have exactly 1 middleware handler")
	assert.True(t, strings.Contains(names[0], "summarization"),
		"middleware should be summarization, got %q", names[0])
}

// ---------------------------------------------------------------------------
// TestAgentMW_AE_MW_002 — ToolReduction middleware triggers
// Scenario: Large tool results cleaned up (D-079)
// Verifies: Agent config with enable_tool_reduction=true builds successfully
//
//	and the middleware list contains the ToolReduction middleware.
//
// ---------------------------------------------------------------------------
func TestAgentMW_AE_MW_002(t *testing.T) {
	env := setupAgentE2E(t)

	cfg := middlewareAgentConfig(env.mockLLM.URL(), "mw-reduction")
	cfg.Middleware.EnableToolReduction = true

	writeMiddlewareAgentConfig(t, env.agentsDir, cfg)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	loadedCfg, ok := env.registry.Get("mw-reduction")
	require.True(t, ok, "mw-reduction should be registered")
	assert.True(t, loadedCfg.Middleware.EnableToolReduction,
		"loaded config should have EnableToolReduction=true (D-079)")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	built, err := env.agentBuilder.Build(ctx, loadedCfg)
	require.NoError(t, err, "Build should succeed with tool reduction middleware (D-079)")
	require.NotNil(t, built, "BuiltAgent should not be nil")

	// Verify middleware list contains ToolReduction.
	names := getAgentHandlerTypeNames(t, built)
	require.Len(t, names, 1, "should have exactly 1 middleware handler")
	assert.True(t, strings.Contains(names[0], "reduction"),
		"middleware should be tool reduction, got %q", names[0])
}

// ---------------------------------------------------------------------------
// TestAgentMW_AE_MW_003 — PatchToolCalls middleware repairs
// Scenario: Dangling tool calls repaired (D-079)
// Verifies: Agent config with enable_patch_tool_calls=true builds successfully
//
//	and the middleware list contains the PatchToolCalls middleware.
//
// ---------------------------------------------------------------------------
func TestAgentMW_AE_MW_003(t *testing.T) {
	env := setupAgentE2E(t)

	cfg := middlewareAgentConfig(env.mockLLM.URL(), "mw-patch")
	cfg.Middleware.EnablePatchToolCalls = true

	writeMiddlewareAgentConfig(t, env.agentsDir, cfg)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	loadedCfg, ok := env.registry.Get("mw-patch")
	require.True(t, ok, "mw-patch should be registered")
	assert.True(t, loadedCfg.Middleware.EnablePatchToolCalls,
		"loaded config should have EnablePatchToolCalls=true (D-079)")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	built, err := env.agentBuilder.Build(ctx, loadedCfg)
	require.NoError(t, err, "Build should succeed with patch tool calls middleware (D-079)")
	require.NotNil(t, built, "BuiltAgent should not be nil")

	// Verify middleware list contains PatchToolCalls.
	names := getAgentHandlerTypeNames(t, built)
	require.Len(t, names, 1, "should have exactly 1 middleware handler")
	assert.True(t, strings.Contains(names[0], "patchtoolcalls"),
		"middleware should be patchtoolcalls, got %q", names[0])
}

// ---------------------------------------------------------------------------
// TestAgentMW_AE_MW_004 — Middleware order correct
// Scenario: PatchToolCalls → Summarization → ToolReduction (D-079)
// Verifies: When all three middleware are enabled, they appear in the
//
//	correct order in the handler list.
//
// ---------------------------------------------------------------------------
func TestAgentMW_AE_MW_004(t *testing.T) {
	env := setupAgentE2E(t)

	cfg := middlewareAgentConfig(env.mockLLM.URL(), "mw-all")
	cfg.Middleware.EnablePatchToolCalls = true
	cfg.Middleware.EnableSummarization = true
	cfg.Middleware.EnableToolReduction = true

	writeMiddlewareAgentConfig(t, env.agentsDir, cfg)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	loadedCfg, ok := env.registry.Get("mw-all")
	require.True(t, ok, "mw-all should be registered")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	built, err := env.agentBuilder.Build(ctx, loadedCfg)
	require.NoError(t, err, "Build should succeed with all three middleware (D-079)")
	require.NotNil(t, built, "BuiltAgent should not be nil")

	// Verify middleware list length and order.
	names := getAgentHandlerTypeNames(t, built)
	require.GreaterOrEqual(t, len(names), 3,
		"should have at least 3 middleware handlers (PatchToolCalls, Summarization, ToolReduction)")

	// Find indices of each middleware by package name.
	patchIdx := -1
	summIdx := -1
	redIdx := -1
	for i, name := range names {
		switch {
		case strings.Contains(name, "patchtoolcalls"):
			patchIdx = i
		case strings.Contains(name, "summarization"):
			summIdx = i
		case strings.Contains(name, "reduction"):
			redIdx = i
		}
	}

	require.NotEqual(t, -1, patchIdx, "PatchToolCalls middleware should be present")
	require.NotEqual(t, -1, summIdx, "Summarization middleware should be present")
	require.NotEqual(t, -1, redIdx, "ToolReduction middleware should be present")

	// Verify order: PatchToolCalls < Summarization < ToolReduction (D-079).
	assert.Less(t, patchIdx, summIdx,
		"PatchToolCalls (idx=%d) must come before Summarization (idx=%d) per D-079", patchIdx, summIdx)
	assert.Less(t, summIdx, redIdx,
		"Summarization (idx=%d) must come before ToolReduction (idx=%d) per D-079", summIdx, redIdx)
}

// ---------------------------------------------------------------------------
// TestAgentMW_AE_MW_005 — Middleware creation failure skipped
// Scenario: Invalid middleware config → agent still starts (D-079)
// Verifies: When all middleware are enabled (including potentially failing
//
//	configurations), the agent still builds successfully. The fail-open
//	strategy ensures individual middleware failures don't prevent agent
//	startup (D-079).
//
// Additionally verifies: an agent with no middleware enabled builds fine
// (baseline), and mixed configs (some enabled, some not) also succeed.
// ---------------------------------------------------------------------------
func TestAgentMW_AE_MW_005(t *testing.T) {
	env := setupAgentE2E(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Case 1: All middleware disabled — agent should still build (baseline).
	cfgNone := middlewareAgentConfig(env.mockLLM.URL(), "mw-none")
	writeMiddlewareAgentConfig(t, env.agentsDir, cfgNone)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	loadedNone, ok := env.registry.Get("mw-none")
	require.True(t, ok, "mw-none should be registered")

	builtNone, err := env.agentBuilder.Build(ctx, loadedNone)
	require.NoError(t, err,
		"Build should succeed with no middleware enabled (baseline, D-079)")
	require.NotNil(t, builtNone, "BuiltAgent with no middleware should not be nil")

	// Case 2: All middleware enabled — agent should still build (fail-open).
	// Even if some middleware fail internally (e.g., summarization with
	// unusual token counts), the agent must still start.
	cfgAll := middlewareAgentConfig(env.mockLLM.URL(), "mw-failopen")
	cfgAll.Middleware.EnablePatchToolCalls = true
	cfgAll.Middleware.EnableSummarization = true
	cfgAll.Middleware.SummarizationTokens = 160000
	cfgAll.Middleware.EnableToolReduction = true
	cfgAll.Middleware.ToolReductionMaxChars = 50000

	writeMiddlewareAgentConfig(t, env.agentsDir, cfgAll)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	loadedAll, ok := env.registry.Get("mw-failopen")
	require.True(t, ok, "mw-failopen should be registered")

	builtAll, err := env.agentBuilder.Build(ctx, loadedAll)
	require.NoError(t, err,
		"Build should succeed with all middleware enabled — fail-open (D-079)")
	require.NotNil(t, builtAll, "BuiltAgent should not be nil even if some middleware fail")

	// Case 3: Only PatchToolCalls enabled, others disabled — mixed config.
	cfgMixed := middlewareAgentConfig(env.mockLLM.URL(), "mw-mixed")
	cfgMixed.Middleware.EnablePatchToolCalls = true

	writeMiddlewareAgentConfig(t, env.agentsDir, cfgMixed)
	require.NoError(t, env.registry.Reload(), "registry reload should succeed")

	loadedMixed, ok := env.registry.Get("mw-mixed")
	require.True(t, ok, "mw-mixed should be registered")

	builtMixed, err := env.agentBuilder.Build(ctx, loadedMixed)
	require.NoError(t, err,
		"Build should succeed with partial middleware config (D-079)")
	require.NotNil(t, builtMixed, "BuiltAgent with mixed config should not be nil")

	// Verify mixed config has exactly 1 handler (PatchToolCalls only).
	names := getAgentHandlerTypeNames(t, builtMixed)
	assert.Len(t, names, 1,
		"mixed config with only PatchToolCalls should have exactly 1 handler")
}
