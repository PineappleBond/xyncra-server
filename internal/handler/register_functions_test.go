package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// registerFunctionsResult is the parsed response from the register_functions handler.
type registerFunctionsResult struct {
	Status   string `json:"status"`
	Count    int    `json:"count"`
	DeviceID string `json:"device_id"`
}

// parseRegisterFunctionsResponse unmarshals the handler's response data.
func parseRegisterFunctionsResponse(t *testing.T, data json.RawMessage) registerFunctionsResult {
	t.Helper()
	var result registerFunctionsResult
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

// callRegisterFunctions is a convenience that builds a request, calls the
// handler, and parses the response. It fails the test on handler error.
func callRegisterFunctions(t *testing.T, h *registerFunctionsHandler, client *server.Client, params any) registerFunctionsResult {
	t.Helper()
	ctx := context.Background()
	req := newTestRequest("req-register-funcs", "system.register_functions", params)
	data, err := h.HandleRequest(ctx, client, req)
	require.NoError(t, err)
	return parseRegisterFunctionsResponse(t, data)
}

// requireHandlerErrorCode asserts that the error is a *protocol.HandlerError
// with the expected code. It fails the test if the assertion fails.
func requireHandlerErrorCode(t *testing.T, err error, expectedCode protocol.ResponseCode) {
	t.Helper()
	var handlerErr *protocol.HandlerError
	require.Error(t, err)
	require.True(t, errors.As(err, &handlerErr), "expected *protocol.HandlerError, got %T", err)
	assert.Equal(t, expectedCode, handlerErr.Code, "expected code %d, got %d", expectedCode, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// RF-01: ValidRegistration — happy path with 2 functions
// ---------------------------------------------------------------------------

func TestRegisterFunctions_HandleRequest_ValidRegistration(t *testing.T) {
	t.Parallel()

	registry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{})
	h := NewRegisterFunctionsHandler(registry)

	const (
		userID   = "alice"
		deviceID = "device-001"
		connID   = "conn-001"
	)

	client := server.NewTestClientWithDevice(userID, deviceID, connID)

	params := map[string]any{
		"device_id":   deviceID,
		"device_name": "Alice's CLI",
		"device_type": "cli",
		"functions": []map[string]any{
			{
				"name":        "read_file",
				"description": "Read a local file",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
					"required": []string{"path"},
				},
				"tags":       []string{"filesystem"},
				"timeout_ms": 5000,
			},
			{
				"name":        "list_dir",
				"description": "List directory contents",
			},
		},
	}

	result := callRegisterFunctions(t, h, client, params)

	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, 2, result.Count)
	assert.Equal(t, deviceID, result.DeviceID)

	// Verify registry actually stored the functions.
	ctx := context.Background()
	funcs, err := registry.GetFunctions(ctx, userID, deviceID)
	require.NoError(t, err)
	require.Len(t, funcs, 2)
	assert.Equal(t, "read_file", funcs[0].Name)
	assert.Equal(t, "list_dir", funcs[1].Name)

	// Verify metadata was stored.
	df, err := registry.GetDeviceFunctions(ctx, userID, deviceID)
	require.NoError(t, err)
	require.NotNil(t, df)
	assert.Equal(t, "Alice's CLI", df.DeviceName)
	assert.Equal(t, "cli", df.DeviceType)
}

// ---------------------------------------------------------------------------
// RF-02: DeviceIDFromClient — params device_id is ignored in favor of client's
// ---------------------------------------------------------------------------

func TestRegisterFunctions_HandleRequest_DeviceIDFromClient(t *testing.T) {
	t.Parallel()

	registry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{})
	h := NewRegisterFunctionsHandler(registry)

	const (
		userID       = "bob"
		correctDevID = "correct-device-id"
		wrongDevID   = "wrong-device-id"
		connID       = "conn-002"
	)

	// Client has the correct deviceID; params contain a different one.
	client := server.NewTestClientWithDevice(userID, correctDevID, connID)

	params := map[string]any{
		"device_id": wrongDevID, // should be overridden
		"functions": []map[string]any{
			{"name": "my_func"},
		},
	}

	result := callRegisterFunctions(t, h, client, params)

	// Response should use the client's deviceID, not the one from params.
	assert.Equal(t, correctDevID, result.DeviceID)

	ctx := context.Background()

	// Registry should have the function under the correct (client's) deviceID.
	funcs, err := registry.GetFunctions(ctx, userID, correctDevID)
	require.NoError(t, err)
	require.Len(t, funcs, 1)
	assert.Equal(t, "my_func", funcs[0].Name)

	// The wrong deviceID should have nothing registered.
	funcsWrong, err := registry.GetFunctions(ctx, userID, wrongDevID)
	require.NoError(t, err)
	assert.Nil(t, funcsWrong, "wrong deviceID should not have any functions registered")
}

// ---------------------------------------------------------------------------
// RF-03: EmptyFunctionsList — valid, clears functions and returns count=0
// ---------------------------------------------------------------------------

func TestRegisterFunctions_HandleRequest_EmptyFunctionsList(t *testing.T) {
	t.Parallel()

	registry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{})
	h := NewRegisterFunctionsHandler(registry)

	const (
		userID   = "carol"
		deviceID = "device-003"
		connID   = "conn-003"
	)

	client := server.NewTestClientWithDevice(userID, deviceID, connID)
	ctx := context.Background()

	// First, register some functions.
	paramsFirst := map[string]any{
		"functions": []map[string]any{
			{"name": "func_a"},
			{"name": "func_b"},
		},
	}
	result1 := callRegisterFunctions(t, h, client, paramsFirst)
	assert.Equal(t, 2, result1.Count)

	// Now send an empty list — this should clear the previous registrations.
	paramsEmpty := map[string]any{
		"functions": []map[string]any{},
	}
	result2 := callRegisterFunctions(t, h, client, paramsEmpty)
	assert.Equal(t, "ok", result2.Status)
	assert.Equal(t, 0, result2.Count)
	assert.Equal(t, deviceID, result2.DeviceID)

	// Verify registry is empty for this device.
	funcs, err := registry.GetFunctions(ctx, userID, deviceID)
	require.NoError(t, err)
	assert.Empty(t, funcs, "functions should be cleared after empty registration")
}

// ---------------------------------------------------------------------------
// RF-04: InvalidJSON — malformed params should return ValidationError (-100)
// ---------------------------------------------------------------------------

func TestRegisterFunctions_HandleRequest_InvalidJSON(t *testing.T) {
	t.Parallel()

	registry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{})
	h := NewRegisterFunctionsHandler(registry)

	client := server.NewTestClientWithDevice("dave", "device-004", "conn-004")

	// Construct a request with invalid JSON params directly (bypass newTestRequest).
	req := &protocol.PackageDataRequest{
		ID:     "req-invalid",
		Method: "system.register_functions",
		Params: json.RawMessage(`{invalid json!!!`),
	}

	_, err := h.HandleRequest(context.Background(), client, req)
	requireHandlerErrorCode(t, err, protocol.ResponseCodeValidationError)
}

// ---------------------------------------------------------------------------
// RF-05: EmptyFunctionName — function with empty name returns ValidationError
// ---------------------------------------------------------------------------

func TestRegisterFunctions_HandleRequest_EmptyFunctionName(t *testing.T) {
	t.Parallel()

	registry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{})
	h := NewRegisterFunctionsHandler(registry)

	client := server.NewTestClientWithDevice("eve", "device-005", "conn-005")

	params := map[string]any{
		"functions": []map[string]any{
			{"name": ""}, // empty name — invalid
		},
	}

	req := newTestRequest("req-empty-name", "system.register_functions", params)
	_, err := h.HandleRequest(context.Background(), client, req)
	requireHandlerErrorCode(t, err, protocol.ResponseCodeValidationError)
}

// ---------------------------------------------------------------------------
// RF-06: TooManyFunctions — exceeding per-device limit returns ValidationError
// ---------------------------------------------------------------------------

func TestRegisterFunctions_HandleRequest_TooManyFunctions(t *testing.T) {
	t.Parallel()

	// Set a low limit so we can exceed it easily.
	registry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{
		MaxFunctionsPerDevice: 2,
	})
	h := NewRegisterFunctionsHandler(registry)

	client := server.NewTestClientWithDevice("frank", "device-006", "conn-006")

	// Try to register 3 functions when the limit is 2.
	params := map[string]any{
		"functions": []map[string]any{
			{"name": "func_1"},
			{"name": "func_2"},
			{"name": "func_3"},
		},
	}

	req := newTestRequest("req-too-many", "system.register_functions", params)
	_, err := h.HandleRequest(context.Background(), client, req)
	requireHandlerErrorCode(t, err, protocol.ResponseCodeValidationError)
}

// ---------------------------------------------------------------------------
// RF-07: FunctionNameTooLong — name exceeding max length returns ValidationError
// ---------------------------------------------------------------------------

func TestRegisterFunctions_HandleRequest_FunctionNameTooLong(t *testing.T) {
	t.Parallel()

	// Use a small max name length for easy testing.
	registry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{
		MaxFunctionNameLength: 5,
	})
	h := NewRegisterFunctionsHandler(registry)

	client := server.NewTestClientWithDevice("grace", "device-007", "conn-007")

	// "abcdefgh" is 8 chars, exceeding the limit of 5.
	params := map[string]any{
		"functions": []map[string]any{
			{"name": "abcdefgh"},
		},
	}

	req := newTestRequest("req-name-too-long", "system.register_functions", params)
	_, err := h.HandleRequest(context.Background(), client, req)
	requireHandlerErrorCode(t, err, protocol.ResponseCodeValidationError)
}

// ---------------------------------------------------------------------------
// RF-08: InternalError — unknown registry error returns InternalError (-300)
// ---------------------------------------------------------------------------

// errTestRegistry is a mock FunctionRegistry that always returns a
// configurable error from RegisterFunctions. It is used to exercise the
// default branch of registryErrorToHandlerError.
type errTestRegistry struct {
	err error
}

func (r *errTestRegistry) RegisterFunctions(_ context.Context, _, _ string, _ *server.RegisterFunctionsParams) error {
	return r.err
}
func (r *errTestRegistry) GetFunctions(_ context.Context, _, _ string) ([]protocol.FunctionInfo, error) {
	return nil, nil
}
func (r *errTestRegistry) GetDeviceFunctions(_ context.Context, _, _ string) (*server.DeviceFunctions, error) {
	return nil, nil
}
func (r *errTestRegistry) OnDeviceDisconnect(_ context.Context, _, _ string) (*server.DeviceFunctions, error) {
	return nil, nil
}

func TestRegisterFunctions_HandleRequest_InternalError(t *testing.T) {
	t.Parallel()

	// Use a mock registry that returns an unknown error.
	unknownErr := errors.New("unexpected registry failure")
	mockReg := &errTestRegistry{err: unknownErr}
	h := NewRegisterFunctionsHandler(mockReg)

	client := server.NewTestClientWithDevice("heidi", "device-008", "conn-008")

	params := map[string]any{
		"functions": []map[string]any{
			{"name": "valid_func"},
		},
	}

	req := newTestRequest("req-internal-err", "system.register_functions", params)
	_, err := h.HandleRequest(context.Background(), client, req)

	var handlerErr *protocol.HandlerError
	require.Error(t, err)
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, protocol.ResponseCodeInternalError, handlerErr.Code,
		"unknown registry error should map to InternalError (-300)")
}

// ---------------------------------------------------------------------------
// RF-09: DuplicateFunctionNames — duplicate names in single request returns
// ValidationError
// ---------------------------------------------------------------------------

func TestRegisterFunctions_HandleRequest_DuplicateFunctionNames(t *testing.T) {
	t.Parallel()

	registry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{})
	h := NewRegisterFunctionsHandler(registry)

	client := server.NewTestClientWithDevice("ivan", "device-009", "conn-009")

	params := map[string]any{
		"functions": []map[string]any{
			{"name": "foo"},
			{"name": "bar"},
			{"name": "foo"}, // duplicate
		},
	}

	req := newTestRequest("req-dup-names", "system.register_functions", params)
	_, err := h.HandleRequest(context.Background(), client, req)
	requireHandlerErrorCode(t, err, protocol.ResponseCodeValidationError)
}
