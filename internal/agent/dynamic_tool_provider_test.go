package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

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

// mockCallerForDTP implements ClientCaller for testing.
type mockCallerForDTP struct {
	resp *protocol.PackageDataResponse
	err  error
}

func (m *mockCallerForDTP) ServerRequest(_ context.Context, _, _, _ string, _ json.RawMessage, _ time.Duration) (*protocol.PackageDataResponse, error) {
	return m.resp, m.err
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
		&mockCallerForDTP{resp: &protocol.PackageDataResponse{Code: 0}},
		ClientToolsConfig{},
		discardLogger(),
	)
	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "alice", DeviceID: "dev1"})
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
		&mockCallerForDTP{resp: &protocol.PackageDataResponse{Code: 0}},
		ClientToolsConfig{},
		discardLogger(),
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
		&mockCallerForDTP{resp: &protocol.PackageDataResponse{Code: 0}},
		ClientToolsConfig{},
		discardLogger(),
	)
	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "alice", DeviceID: "dev1"})
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
		&mockCallerForDTP{resp: &protocol.PackageDataResponse{Code: 0}},
		ClientToolsConfig{},
		discardLogger(),
	)
	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "alice", DeviceID: "dev1"})
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
		&mockCallerForDTP{resp: &protocol.PackageDataResponse{Code: 0}},
		ClientToolsConfig{FunctionTags: []string{"filesystem"}},
		discardLogger(),
	)
	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "alice", DeviceID: "dev1"})
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
		&mockCallerForDTP{resp: &protocol.PackageDataResponse{Code: 0}},
		ClientToolsConfig{ExcludedFunctions: []string{"fn2"}},
		discardLogger(),
	)
	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "alice", DeviceID: "dev1"})
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
		&mockCallerForDTP{resp: &protocol.PackageDataResponse{Code: 0}},
		ClientToolsConfig{
			FunctionTags:      []string{"fs"},
			ExcludedFunctions: []string{"fn1"},
		},
		discardLogger(),
	)
	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "alice", DeviceID: "dev1"})
	runCtx := &adk.ChatModelAgentContext{Tools: nil}

	_, runCtx, err := dtp.BeforeAgent(ctx, runCtx)
	require.NoError(t, err)
	require.Len(t, runCtx.Tools, 1)

	names := toolNames(t, runCtx.Tools)
	assert.Contains(t, names, "fn2")
	assert.NotContains(t, names, "fn1")
}

// ---------------------------------------------------------------------------
// DTP-08: (skipped — hard to construct an invalid FunctionInfo with map[string]any)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// DTP-09: Concurrent BeforeAgent calls — race detector
// ---------------------------------------------------------------------------

func TestDynamicToolProvider_ConcurrentBeforeAgent(t *testing.T) {
	funcs := []protocol.FunctionInfo{makeTestFuncInfo("fn1"), makeTestFuncInfo("fn2")}
	dtp := NewDynamicToolProvider(
		&mockFunctionProvider{funcs: funcs},
		&mockCallerForDTP{resp: &protocol.PackageDataResponse{Code: 0}},
		ClientToolsConfig{},
		discardLogger(),
	)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ctx := ContextWithCallerDevice(context.Background(), CallerDevice{UserID: "alice", DeviceID: "dev1"})
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
