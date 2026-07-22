package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// mockFunctionProvider implements ClientFunctionProvider for testing.
type mockFunctionProvider struct {
	funcs []protocol.FunctionInfo
	err   error
}

func (m *mockFunctionProvider) GetFunctions(_ context.Context, _, _ string) ([]protocol.FunctionInfo, error) {
	return m.funcs, m.err
}

func (m *mockFunctionProvider) GetFunctionsByUser(_ context.Context, _ string) (map[string][]protocol.FunctionInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if len(m.funcs) == 0 {
		return nil, nil
	}
	return map[string][]protocol.FunctionInfo{"dev1": m.funcs}, nil
}

// makeTestFuncInfo creates a minimal valid FunctionInfo for testing.
func makeTestFuncInfo(name string) protocol.FunctionInfo {
	return protocol.FunctionInfo{
		Name:        name,
		Description: "Test function " + name,
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// discardLogger returns a Logger that discards all output.
func discardLogger() Logger {
	return noopLogger{}
}

// ---------------------------------------------------------------------------
// DTP-01: context with CallerDevice + 2 functions → 2 tools injected
// ---------------------------------------------------------------------------

func TestDynamicToolProvider_InjectsTools(t *testing.T) {
	funcs := []protocol.FunctionInfo{makeTestFuncInfo("fn1"), makeTestFuncInfo("fn2")}
	dtp := NewDynamicToolProvider(
		&mockFunctionProvider{funcs: funcs},
		ClientToolsConfig{},
		discardLogger(),
		nil,
		nil,
	)
	ctx := ContextWithAgentID(context.Background(), "alice")
	runCtx := &adk.ChatModelAgentContext{Tools: nil}

	_, runCtx, err := dtp.BeforeAgent(ctx, runCtx)
	require.NoError(t, err)
	assert.Len(t, runCtx.Tools, 2)
}

// ---------------------------------------------------------------------------
// DTP-02: context without CallerDevice → no-op
// ---------------------------------------------------------------------------

func TestDynamicToolProvider_NoCallerDevice_NoOp(t *testing.T) {
	funcs := []protocol.FunctionInfo{makeTestFuncInfo("fn1")}
	dtp := NewDynamicToolProvider(
		&mockFunctionProvider{funcs: funcs},
		ClientToolsConfig{},
		discardLogger(),
		nil,
		nil,
	)
	ctx := context.Background() // no CallerDevice
	runCtx := &adk.ChatModelAgentContext{Tools: nil}

	_, runCtx, err := dtp.BeforeAgent(ctx, runCtx)
	require.NoError(t, err)
	assert.Nil(t, runCtx.Tools)
}

// ---------------------------------------------------------------------------
// DTP-03: GetFunctions returns empty list → no injection
// ---------------------------------------------------------------------------

func TestDynamicToolProvider_EmptyFunctions_NoInjection(t *testing.T) {
	dtp := NewDynamicToolProvider(
		&mockFunctionProvider{funcs: []protocol.FunctionInfo{}},
		ClientToolsConfig{},
		discardLogger(),
		nil,
		nil,
	)
	ctx := ContextWithAgentID(context.Background(), "alice")
	runCtx := &adk.ChatModelAgentContext{Tools: nil}

	_, runCtx, err := dtp.BeforeAgent(ctx, runCtx)
	require.NoError(t, err)
	assert.Nil(t, runCtx.Tools)
}

// ---------------------------------------------------------------------------
// DTP-04: GetFunctions returns error → fail-open, no panic
// ---------------------------------------------------------------------------

func TestDynamicToolProvider_GetFunctionsError_FailOpen(t *testing.T) {
	dtp := NewDynamicToolProvider(
		&mockFunctionProvider{err: assert.AnError},
		ClientToolsConfig{},
		discardLogger(),
		nil,
		nil,
	)
	ctx := ContextWithAgentID(context.Background(), "alice")
	runCtx := &adk.ChatModelAgentContext{Tools: nil}

	assert.NotPanics(t, func() {
		_, runCtx, _ = dtp.BeforeAgent(ctx, runCtx)
	})
	assert.Nil(t, runCtx.Tools)
}

// ---------------------------------------------------------------------------
// DTP-05: function_tags filter (OR semantics)
// ---------------------------------------------------------------------------

func TestDynamicToolProvider_FunctionTagsFilter(t *testing.T) {
	fn1 := makeTestFuncInfo("fn1")
	fn1.Tags = []string{"filesystem"}
	fn2 := makeTestFuncInfo("fn2")
	fn2.Tags = []string{"network"}
	fn3 := makeTestFuncInfo("fn3")
	fn3.Tags = []string{"filesystem", "network"}

	dtp := NewDynamicToolProvider(
		&mockFunctionProvider{funcs: []protocol.FunctionInfo{fn1, fn2, fn3}},
		ClientToolsConfig{FunctionTags: []string{"filesystem"}},
		discardLogger(),
		nil,
		nil,
	)
	ctx := ContextWithAgentID(context.Background(), "alice")
	runCtx := &adk.ChatModelAgentContext{Tools: nil}

	_, runCtx, err := dtp.BeforeAgent(ctx, runCtx)
	require.NoError(t, err)
	require.Len(t, runCtx.Tools, 2)

	// Verify the injected tools are fn1 and fn3.
	names := toolNames(t, runCtx.Tools)
	assert.Contains(t, names, "fn1")
	assert.Contains(t, names, "fn3")
	assert.NotContains(t, names, "fn2")
}

// ---------------------------------------------------------------------------
// DTP-06: excluded_functions filter
// ---------------------------------------------------------------------------

func TestDynamicToolProvider_ExcludedFunctions(t *testing.T) {
	funcs := []protocol.FunctionInfo{makeTestFuncInfo("fn1"), makeTestFuncInfo("fn2")}
	dtp := NewDynamicToolProvider(
		&mockFunctionProvider{funcs: funcs},
		ClientToolsConfig{ExcludedFunctions: []string{"fn2"}},
		discardLogger(),
		nil,
		nil,
	)
	ctx := ContextWithAgentID(context.Background(), "alice")
	runCtx := &adk.ChatModelAgentContext{Tools: nil}

	_, runCtx, err := dtp.BeforeAgent(ctx, runCtx)
	require.NoError(t, err)
	require.Len(t, runCtx.Tools, 1)

	names := toolNames(t, runCtx.Tools)
	assert.Contains(t, names, "fn1")
	assert.NotContains(t, names, "fn2")
}

// ---------------------------------------------------------------------------
// DTP-07: Combined tags + excluded
// ---------------------------------------------------------------------------

func TestDynamicToolProvider_TagsAndExcluded(t *testing.T) {
	fn1 := makeTestFuncInfo("fn1")
	fn1.Tags = []string{"fs"}
	fn2 := makeTestFuncInfo("fn2")
	fn2.Tags = []string{"fs"}

	dtp := NewDynamicToolProvider(
		&mockFunctionProvider{funcs: []protocol.FunctionInfo{fn1, fn2}},
		ClientToolsConfig{
			FunctionTags:      []string{"fs"},
			ExcludedFunctions: []string{"fn1"},
		},
		discardLogger(),
		nil,
		nil,
	)
	ctx := ContextWithAgentID(context.Background(), "alice")
	runCtx := &adk.ChatModelAgentContext{Tools: nil}

	_, runCtx, err := dtp.BeforeAgent(ctx, runCtx)
	require.NoError(t, err)
	require.Len(t, runCtx.Tools, 1)

	names := toolNames(t, runCtx.Tools)
	assert.Contains(t, names, "fn2")
	assert.NotContains(t, names, "fn1")
}

// ---------------------------------------------------------------------------
// DTP-09: Concurrent BeforeAgent calls — race detector
// ---------------------------------------------------------------------------

func TestDynamicToolProvider_ConcurrentBeforeAgent(t *testing.T) {
	funcs := []protocol.FunctionInfo{makeTestFuncInfo("fn1"), makeTestFuncInfo("fn2")}
	dtp := NewDynamicToolProvider(
		&mockFunctionProvider{funcs: funcs},
		ClientToolsConfig{},
		discardLogger(),
		nil,
		nil,
	)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ctx := ContextWithAgentID(context.Background(), "alice")
			runCtx := &adk.ChatModelAgentContext{Tools: nil}
			_, runCtx, err := dtp.BeforeAgent(ctx, runCtx)
			assert.NoError(t, err)
			assert.Len(t, runCtx.Tools, 2)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Helper: extract tool names from a slice of tool.BaseTool
// ---------------------------------------------------------------------------

func toolNames(t *testing.T, tools []tool.BaseTool) []string {
	t.Helper()
	var names []string
	for _, tl := range tools {
		info, err := tl.Info(context.Background())
		require.NoError(t, err)
		names = append(names, info.Name)
	}
	return names
}
