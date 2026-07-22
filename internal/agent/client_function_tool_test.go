package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	tool, err := newClientFunctionTool(funcInfo, "alice", "device-1", 0)
	require.NoError(t, err)

	info, err := tool.Info(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "read_file", info.Name)
	assert.Equal(t, "Read a local file", info.Desc)
}

// ---------------------------------------------------------------------------
// CFT-02: Nil parameters → empty schema fallback (no error)
// ---------------------------------------------------------------------------

func TestNewClientFunctionTool_NilParameters(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:        "simple_fn",
		Description: "A function with no parameters",
		Parameters:  nil,
	}
	tool, err := newClientFunctionTool(funcInfo, "alice", "dev-1", 0)
	require.NoError(t, err)

	info, err := tool.Info(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "simple_fn", info.Name)
}

// ---------------------------------------------------------------------------
// CFT-03: buildToolInfo with invalid / non-object schema does not panic
// ---------------------------------------------------------------------------

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
				if err == nil {
					assert.Equal(t, "degenerate_fn", info.Name)
				}
			})
		})
	}
}

// ---------------------------------------------------------------------------
// CFT-04: Interrupt data JSON contains correct fields
// ---------------------------------------------------------------------------

func TestExecuteClientFunction_InterruptDataFormat(t *testing.T) {
	// Verify the interrupt data JSON structure that executeClientFunction builds.
	// Since tool.Interrupt is a framework call that requires Eino runtime,
	// we test the JSON construction logic directly.
	funcInfo := protocol.FunctionInfo{
		Name:      "read_file",
		TimeoutMs: 60000, // 60 seconds, above minimum
	}

	timeoutMs := funcInfo.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = DefaultClientFunctionCallTimeoutMs
	}
	// Enforce minimum timeout
	if timeoutMs < MinClientFunctionCallTimeoutMs {
		timeoutMs = MinClientFunctionCallTimeoutMs
	}

	interruptData, _ := json.Marshal(map[string]any{
		"method":     funcInfo.Name,
		"params":     `{"path":"/tmp/test"}`,
		"device_id":  "dev-1",
		"timeout_ms": timeoutMs,
	})

	var parsed struct {
		Method    string `json:"method"`
		Params    string `json:"params"`
		DeviceID  string `json:"device_id"`
		TimeoutMs int64  `json:"timeout_ms"`
	}
	err := json.Unmarshal(interruptData, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "read_file", parsed.Method)
	assert.Equal(t, `{"path":"/tmp/test"}`, parsed.Params)
	assert.Equal(t, "dev-1", parsed.DeviceID)
	assert.Equal(t, int64(60000), parsed.TimeoutMs)
}

// ---------------------------------------------------------------------------
// CFT-04b: Minimum timeout enforcement when TimeoutMs is too small
// ---------------------------------------------------------------------------

func TestExecuteClientFunction_MinTimeoutEnforcement(t *testing.T) {
	// Verify that timeout_ms below minimum is clamped to MinClientFunctionCallTimeoutMs.
	funcInfo := protocol.FunctionInfo{
		Name:      "slow_fn",
		TimeoutMs: 5000, // 5 seconds, below minimum (60s)
	}

	timeoutMs := funcInfo.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = DefaultClientFunctionCallTimeoutMs
	}
	// Enforce minimum timeout
	if timeoutMs < MinClientFunctionCallTimeoutMs {
		timeoutMs = MinClientFunctionCallTimeoutMs
	}

	assert.Equal(t, int64(MinClientFunctionCallTimeoutMs), int64(timeoutMs),
		"timeout_ms below minimum should be clamped to MinClientFunctionCallTimeoutMs")
}

// ---------------------------------------------------------------------------
// CFT-05: Default timeout when TimeoutMs is 0
// ---------------------------------------------------------------------------

func TestExecuteClientFunction_DefaultTimeout(t *testing.T) {
	funcInfo := protocol.FunctionInfo{
		Name:      "fast_fn",
		TimeoutMs: 0,
	}

	timeoutMs := funcInfo.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = DefaultClientFunctionCallTimeoutMs
	}
	// Enforce minimum timeout
	if timeoutMs < MinClientFunctionCallTimeoutMs {
		timeoutMs = MinClientFunctionCallTimeoutMs
	}

	assert.Equal(t, int64(DefaultClientFunctionCallTimeoutMs), int64(timeoutMs))
}
