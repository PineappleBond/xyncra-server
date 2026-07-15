package cli

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPingHandler_Echo verifies that the ping handler echoes the input
// message and includes a timestamp field (CLI-BF-001).
func TestPingHandler_Echo(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{"message":"hello world"}`)
	req := &protocol.PackageDataRequest{Params: params}

	data, err := pingHandler(context.Background(), req)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(data, &resp))

	assert.Equal(t, "hello world", resp["echo"])
	assert.NotEmpty(t, resp["timestamp"])
}

// TestPingHandler_EmptyMessage verifies that the ping handler returns an
// empty echo when no message is provided (CLI-BF-002).
func TestPingHandler_EmptyMessage(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{"message":""}`)
	req := &protocol.PackageDataRequest{Params: params}

	data, err := pingHandler(context.Background(), req)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(data, &resp))

	assert.Equal(t, "", resp["echo"])
	assert.NotEmpty(t, resp["timestamp"])
}

// TestGetDeviceInfoHandler verifies that get_device_info returns hostname,
// os, arch, and pid fields with correct types (CLI-BF-003).
func TestGetDeviceInfoHandler(t *testing.T) {
	t.Parallel()

	req := &protocol.PackageDataRequest{}

	data, err := getDeviceInfoHandler(context.Background(), req)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(data, &resp))

	assert.Contains(t, resp, "hostname")
	assert.Contains(t, resp, "os")
	assert.Contains(t, resp, "arch")
	assert.Contains(t, resp, "pid")

	assert.IsType(t, "", resp["hostname"])
	assert.IsType(t, "", resp["os"])
	assert.IsType(t, "", resp["arch"])
	// JSON numbers unmarshal as float64 by default.
	assert.IsType(t, float64(0), resp["pid"])
}

// TestGetTimeHandler verifies that get_time returns utc, unix, and timezone
// fields with correct types (CLI-BF-004).
func TestGetTimeHandler(t *testing.T) {
	t.Parallel()

	req := &protocol.PackageDataRequest{}

	data, err := getTimeHandler(context.Background(), req)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(data, &resp))

	assert.Contains(t, resp, "utc")
	assert.Contains(t, resp, "unix")
	assert.Contains(t, resp, "timezone")

	assert.IsType(t, "", resp["utc"])
	assert.IsType(t, float64(0), resp["unix"])
	assert.IsType(t, "", resp["timezone"])
}

// TestRegisterBuiltinHandlers_Count verifies that registerBuiltinHandlers
// does not panic and that the expected three handler names match the
// function infos (CLI-BF-005).
func TestRegisterBuiltinHandlers_Count(t *testing.T) {
	t.Parallel()

	// We cannot easily construct a real XyncraClient without a database,
	// so we verify indirectly: builtinFunctionInfos returns exactly 3
	// entries, whose names match the handlers registered by
	// registerBuiltinHandlers.
	infos := builtinFunctionInfos()
	require.Len(t, infos, 3)

	expectedNames := map[string]bool{
		"ping":            true,
		"get_device_info": true,
		"get_time":        true,
	}
	for _, info := range infos {
		assert.True(t, expectedNames[info.Name], "unexpected function name: %s", info.Name)
	}
}

// TestBuiltinFunctionInfos_Count verifies that builtinFunctionInfos
// returns exactly 3 function descriptors, each with a non-empty name
// and description (CLI-BF-006).
func TestBuiltinFunctionInfos_Count(t *testing.T) {
	t.Parallel()

	infos := builtinFunctionInfos()
	require.Len(t, infos, 3)

	for _, info := range infos {
		assert.NotEmpty(t, info.Name)
		assert.NotEmpty(t, info.Description)
	}
}

// TestBuiltinFunctionInfos_NoDuplicates verifies that all function names
// returned by builtinFunctionInfos are unique (CLI-BF-007).
func TestBuiltinFunctionInfos_NoDuplicates(t *testing.T) {
	t.Parallel()

	infos := builtinFunctionInfos()
	require.Len(t, infos, 3)

	seen := make(map[string]bool, len(infos))
	for _, info := range infos {
		assert.False(t, seen[info.Name], "duplicate function name: %s", info.Name)
		seen[info.Name] = true
	}
}

// TestPingHandler_NilParams verifies that the ping handler does not panic or
// return an error when req.Params is nil — it should return an empty echo
// together with a timestamp (CLI-BF-009).
func TestPingHandler_NilParams(t *testing.T) {
	t.Parallel()

	req := &protocol.PackageDataRequest{Params: nil}
	data, err := pingHandler(context.Background(), req)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(data, &resp))

	assert.Equal(t, "", resp["echo"])
	assert.NotEmpty(t, resp["timestamp"])
}

// TestPingHandler_InvalidParams verifies that the ping handler returns an
// error when given malformed JSON params (CLI-BF-008).
func TestPingHandler_InvalidParams(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{not valid json}`)
	req := &protocol.PackageDataRequest{Params: params}

	_, err := pingHandler(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping: parse params")
}

// TestParseDeviceInfo verifies that parseDeviceInfo handles empty strings,
// valid JSON, invalid JSON, JSON arrays, and JSON strings correctly.
func TestParseDeviceInfo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{"empty string", "", nil},
		{"valid JSON", `{"name":"test","os":"linux"}`, map[string]string{"name": "test", "os": "linux"}},
		{"invalid JSON", "not-json", map[string]string{}},
		{"JSON array", `[1,2,3]`, map[string]string{}},
		{"JSON string", `"hello"`, map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseDeviceInfo(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
