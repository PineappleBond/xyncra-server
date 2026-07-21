package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock caller
// ---------------------------------------------------------------------------

// mockClientCaller implements the caller interface for testing.
type mockClientCaller struct {
	resp        *protocol.PackageDataResponse
	err         error
	lastMethod  string
	lastParams  json.RawMessage
	lastTimeout time.Duration
	mu          sync.Mutex
}

func (m *mockClientCaller) ServerRequest(_ context.Context, _, _, method string, params json.RawMessage, timeout time.Duration) (*protocol.PackageDataResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMethod = method
	m.lastParams = params
	m.lastTimeout = timeout
	return m.resp, m.err
}

// ---------------------------------------------------------------------------
// CFT-01: Valid FunctionInfo creates tool with correct Name/Desc
// ---------------------------------------------------------------------------

func TestNewClientFunctionTool_Info(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path"},
			},
			"required": []any{"path"},
		},
	}
	caller := &mockClientCaller{resp: &protocol.PackageDataResponse{Data: json.RawMessage(`"content"`), Code: 0}}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "device-1", 30*time.Second)
	require.NoError(t, err)

	info, err := tool.Info(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "read_file", info.Name)
	assert.Equal(t, "Read a local file", info.Desc)
}

// ---------------------------------------------------------------------------
// CFT-02: Invoke happy path — caller returns success
// ---------------------------------------------------------------------------

func TestNewClientFunctionTool_InvokeSuccess(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	caller := &mockClientCaller{resp: &protocol.PackageDataResponse{Data: json.RawMessage(`"hello"`), Code: 0}}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	result, err := tool.InvokableRun(context.Background(), `{"path":"/tmp/test"}`)
	require.NoError(t, err)
	assert.Equal(t, `"hello"`, result)

	caller.mu.Lock()
	defer caller.mu.Unlock()
	assert.Equal(t, "read_file", caller.lastMethod)
	assert.Equal(t, json.RawMessage(`{"path":"/tmp/test"}`), caller.lastParams)
	assert.Equal(t, 30*time.Second, caller.lastTimeout)
}

// ---------------------------------------------------------------------------
// CFT-03: Invoke — device offline (caller returns "no connections" error)
// ---------------------------------------------------------------------------

func TestNewClientFunctionTool_InvokeDeviceOffline(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	caller := &errCaller{err: errWithMessage("no connections available for device")}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	// Recoverable failure is surfaced as a ToolResult envelope (success=false)
	// with a readable reason, NOT as a Go error, so the LLM can self-correct
	// (D-101).
	result, err := tool.InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)
	assert.Contains(t, result, `"success":false`)
	assert.Contains(t, result, "device is offline")
}

// errCaller is a minimal caller that always returns the given error.
type errCaller struct {
	err error
}

func (e *errCaller) ServerRequest(_ context.Context, _, _, _ string, _ json.RawMessage, _ time.Duration) (*protocol.PackageDataResponse, error) {
	return nil, e.err
}

// retryCaller returns an error on the first call, then returns a success
// response on subsequent calls. Used to test the offline retry logic.
type retryCaller struct {
	mu      sync.Mutex
	first   error
	resp    *protocol.PackageDataResponse
	callNum int
}

func (r *retryCaller) ServerRequest(_ context.Context, _, _, _ string, _ json.RawMessage, _ time.Duration) (*protocol.PackageDataResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callNum++
	if r.callNum == 1 {
		return nil, r.first
	}
	return r.resp, nil
}

func (r *retryCaller) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.callNum
}

// errWithMessage returns an error with the given message.
type errorWithMessage string

func (e errorWithMessage) Error() string { return string(e) }

func errWithMessage(msg string) error { return errorWithMessage(msg) }

// ---------------------------------------------------------------------------
// CFT-04: Invoke timeout — caller returns deadline exceeded
// ---------------------------------------------------------------------------

func TestNewClientFunctionTool_InvokeTimeout(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	caller := &errCaller{err: errWithMessage("context deadline exceeded")}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	// Recoverable failure → ToolResult envelope (success=false), not a Go error (D-101).
	result, err := tool.InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)
	assert.Contains(t, result, `"success":false`)
	assert.Contains(t, result, "timed out")
}

// ---------------------------------------------------------------------------
// CFT-05: Invoke client error code — "client returned error (code -1)"
// ---------------------------------------------------------------------------

func TestNewClientFunctionTool_InvokeClientErrorCode(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	caller := &mockClientCaller{
		resp: &protocol.PackageDataResponse{Code: -1, Msg: "permission denied"},
	}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	// Client business error → ToolResult envelope (success=false), not a Go error (D-101).
	result, err := tool.InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)
	assert.Contains(t, result, `"success":false`)
	assert.Contains(t, result, "client returned error (code -1)")
	assert.Contains(t, result, "permission denied")
}

// ---------------------------------------------------------------------------
// CFT-06: Nil parameters → empty schema fallback (no error)
// ---------------------------------------------------------------------------

func TestNewClientFunctionTool_NilParameters(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "simple_fn",
		Description: "A function with no parameters",
		Parameters:  nil,
	}
	caller := &mockClientCaller{resp: &protocol.PackageDataResponse{Data: json.RawMessage(`"ok"`), Code: 0}}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	info, err := tool.Info(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "simple_fn", info.Name)
}

// ---------------------------------------------------------------------------
// CFT-07: Timeout — funcInfo.TimeoutMs overrides defaultTimeout
// ---------------------------------------------------------------------------

func TestNewClientFunctionTool_TimeoutOverride(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:      "slow_fn",
		TimeoutMs: 5000,
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
	caller := &mockClientCaller{resp: &protocol.PackageDataResponse{Data: json.RawMessage(`"ok"`), Code: 0}}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	_, err = tool.InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)

	caller.mu.Lock()
	assert.Equal(t, 5000*time.Millisecond, caller.lastTimeout)
	caller.mu.Unlock()
}

func TestNewClientFunctionTool_TimeoutDefault(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:      "fast_fn",
		TimeoutMs: 0, // no per-function override
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
	caller := &mockClientCaller{resp: &protocol.PackageDataResponse{Data: json.RawMessage(`"ok"`), Code: 0}}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	_, err = tool.InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)

	caller.mu.Lock()
	assert.Equal(t, 30*time.Second, caller.lastTimeout)
	caller.mu.Unlock()
}

// ---------------------------------------------------------------------------
// CFT-08: Concurrent InvokableRun calls — race detector
// ---------------------------------------------------------------------------

func TestNewClientFunctionTool_ConcurrentInvoke(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "concurrent_fn",
		Description: "A function called concurrently",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	caller := &mockClientCaller{resp: &protocol.PackageDataResponse{Data: json.RawMessage(`"ok"`), Code: 0}}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := tool.InvokableRun(context.Background(), `{}`)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// CFT-09: Connection lost error -> "unable to reach" fallback
// ---------------------------------------------------------------------------

// TestNewClientFunctionTool_InvokeConnectionLost verifies that when the
// caller returns a connection-level error (not timeout, not "no connections"),
// formatClientToolError maps it to the generic "unable to reach" message.
func TestNewClientFunctionTool_InvokeConnectionLost(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	caller := &errCaller{err: errWithMessage("connection reset by peer")}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	// Recoverable failure → ToolResult envelope (success=false), not a Go error (D-101).
	result, err := tool.InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)
	assert.Contains(t, result, `"success":false`)
	assert.Contains(t, result, "unable to reach")
}

// ---------------------------------------------------------------------------
// CFT-10: Unknown error -> generic fallback message
// ---------------------------------------------------------------------------

// TestNewClientFunctionTool_InvokeUnknownError verifies that an unrecognized
// error produces a generic "unable to reach" message (the fallback branch
// of formatClientToolError), not a panic or empty string.
func TestNewClientFunctionTool_InvokeUnknownError(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	caller := &errCaller{err: errWithMessage("something completely unexpected")}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	// Recoverable failure → ToolResult envelope (success=false), not a Go error (D-101).
	result, err := tool.InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)
	assert.Contains(t, result, `"success":false`)
	assert.Contains(t, result, "unable to reach")
}

// ---------------------------------------------------------------------------
// CFT-11: buildToolInfo with invalid / non-object schema does not panic
// ---------------------------------------------------------------------------

// TestBuildToolInfo_InvalidSchema verifies that buildToolInfo handles
// degenerate parameter schemas (non-object types) without panicking.
// The function uses JSON roundtrip so most map[string]any inputs produce
// valid jsonschema.Schema values, but degenerate inputs should still
// be tolerated gracefully.
func TestBuildToolInfo_InvalidSchema(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
	}{
		{"empty map", map[string]any{}},
		{"non-object type field", map[string]any{"type": "string"}},
		{"malformed nested structure", map[string]any{
			"type":       "object",
			"properties": map[string]any{"field": "not-a-schema"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			funcInfo := protocol.FunctionInfo{
				Name:        "degenerate_fn",
				Description: "A function with degenerate schema",
				Parameters:  tc.params,
			}
			require.NotPanics(t, func() {
				info, err := buildToolInfo(funcInfo)
				// buildToolInfo may succeed or return an error,
				// but must not panic.
				if err == nil {
					assert.Equal(t, "degenerate_fn", info.Name)
				}
			})
		})
	}
}

// ---------------------------------------------------------------------------
// CFT-12: Offline retry — first call fails, retry succeeds
// ---------------------------------------------------------------------------

// TestNewClientFunctionTool_OfflineRetry_Success verifies that when the first
// ServerRequest returns a device-offline error, executeClientFunction waits
// and retries once, succeeding on the second attempt.
func TestNewClientFunctionTool_OfflineRetry_Success(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	caller := &retryCaller{
		first: errWithMessage("send request: device offline, request persisted for replay: reverse_rpc: device is offline"),
		resp:  &protocol.PackageDataResponse{Data: json.RawMessage(`"recovered"`), Code: 0},
	}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	result, err := tool.InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)
	assert.Equal(t, `"recovered"`, result)
	assert.Equal(t, 2, caller.callCount(), "should have called ServerRequest twice")
}

// ---------------------------------------------------------------------------
// CFT-13: Offline retry — both attempts fail (persisted for replay message)
// ---------------------------------------------------------------------------

// TestNewClientFunctionTool_OfflineRetry_PersistedMessage verifies that when
// both the initial call and retry return a device-offline error with
// "persisted for replay", the LLM-friendly message reflects that the request
// was queued.
func TestNewClientFunctionTool_OfflineRetry_PersistedMessage(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	// errCaller always returns the same error — both attempts fail.
	caller := &errCaller{err: errWithMessage("send request: device offline, request persisted for replay: reverse_rpc: device is offline")}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	result, err := tool.InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)
	assert.Contains(t, result, `"success":false`)
	assert.Contains(t, result, "queued")
	assert.Contains(t, result, "reconnects")
}

// ---------------------------------------------------------------------------
// CFT-14: isDeviceOfflineError helper
// ---------------------------------------------------------------------------

func TestIsDeviceOfflineError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{"device offline", "send request: device offline, request persisted for replay: reverse_rpc: device is offline", true},
		{"no connections", "no connections available for device", true},
		{"device is offline", "device is offline", true},
		{"timeout", "context deadline exceeded", false},
		{"connection reset", "connection reset by peer", false},
		{"generic error", "something else", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := errWithMessage(tc.errMsg)
			assert.Equal(t, tc.expected, isDeviceOfflineError(err))
		})
	}
}

// ---------------------------------------------------------------------------
// CFT-15: Offline retry — context cancelled during wait
// ---------------------------------------------------------------------------

// TestNewClientFunctionTool_OfflineRetry_ContextCancelled verifies that if the
// context is cancelled during the 3-second retry wait, the function returns
// a soft failure "tool call cancelled" without attempting the retry.
func TestNewClientFunctionTool_OfflineRetry_ContextCancelled(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "read_file",
		Description: "Read a local file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
	caller := &retryCaller{
		first: errWithMessage("send request: device offline, request persisted for replay: reverse_rpc: device is offline"),
		resp:  &protocol.PackageDataResponse{Data: json.RawMessage(`"ok"`), Code: 0},
	}
	tool, err := newClientFunctionTool(funcInfo, caller, "alice", "dev-1", 30*time.Second)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after 500ms — well within the 3-second retry wait.
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	result, err := tool.InvokableRun(ctx, `{}`)
	require.NoError(t, err)
	assert.Contains(t, result, `"success":false`)
	assert.Contains(t, result, "cancelled")
	assert.Equal(t, 1, caller.callCount(), "should NOT have retried after context cancel")
}
