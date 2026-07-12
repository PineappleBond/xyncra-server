package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// startWSServerWithRegistry creates a WebSocketServer with an injected
// MemoryFunctionRegistry and registers a system.register_functions handler.
// It returns the server, the registry, the listen address, and a cleanup
// function. The caller should invoke cleanup via defer.
func startWSServerWithRegistry(t *testing.T) (*WebSocketServer, *MemoryFunctionRegistry, string, func()) {
	t.Helper()

	registry := NewMemoryFunctionRegistry(FunctionRegistryConfig{})
	handler := NewDefaultMessageHandler()

	// Register system.register_functions inline to avoid circular imports
	// with the handler package.
	handler.RegisterMethod("system.register_functions", MethodHandlerFunc(
		func(ctx context.Context, client *Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
			var params RegisterFunctionsParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			deviceID := client.DeviceID()
			if err := registry.RegisterFunctions(ctx, client.UserID(), deviceID, &params); err != nil {
				return nil, err
			}
			resp := map[string]any{
				"status":    "ok",
				"count":     len(params.Functions),
				"device_id": deviceID,
			}
			data, err := json.Marshal(resp)
			if err != nil {
				return nil, err
			}
			return data, nil
		},
	))

	cs := NewMemoryConnectionStore(0)
	srv, addr, cleanup := startWSServer(t, cs,
		WSWithMessageHandler(handler),
		WSWithFunctionRegistry(registry),
	)

	return srv, registry, addr, cleanup
}

// connectWSWithDevice connects a WebSocket client with both user_id and
// device_id query parameters.
func connectWSWithDevice(t *testing.T, addr, userID, deviceID string) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s/ws?user_id=%s&device_id=%s", addr, userID, deviceID)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	return conn
}

// sendRegisterFunctions sends a system.register_functions request with the
// given functions and reads the response.
func sendRegisterFunctions(t *testing.T, conn *websocket.Conn, reqID string, functions []protocol.FunctionInfo) *protocol.PackageDataResponse {
	t.Helper()

	params := RegisterFunctionsParams{
		Functions: functions,
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	sendRequestPackage(t, conn, reqID, "system.register_functions", paramsJSON)
	return readResponsePackage(t, conn, 3*time.Second)
}

// waitForCleanup polls until the given condition is true or timeout expires.
// Used to wait for async cleanup after disconnect.
func waitForCleanup(t *testing.T, timeout time.Duration, condition func() bool, msg string) {
	t.Helper()
	require.Eventually(t, condition, timeout, 50*time.Millisecond, msg)
}

// ---------------------------------------------------------------------------
// TestFunctionLifecycle_Disconnect_UnregistersFunctions
//
// Verifies that when a client disconnects, its registered functions are
// removed from the FunctionRegistry by the handleWebSocket cleanup logic.
// ---------------------------------------------------------------------------

func TestFunctionLifecycle_Disconnect_UnregistersFunctions(t *testing.T) {
	t.Parallel()

	_, registry, addr, cleanup := startWSServerWithRegistry(t)
	defer cleanup()

	const userID = "user-lifecycle-disconnect"
	const deviceID = "device-alpha"

	// Connect and register 2 functions.
	conn := connectWSWithDevice(t, addr, userID, deviceID)

	functions := []protocol.FunctionInfo{
		{Name: "read_file", Description: "Read a local file"},
		{Name: "write_file", Description: "Write to a local file"},
	}

	resp := sendRegisterFunctions(t, conn, "reg-1", functions)
	assert.Equal(t, "reg-1", resp.ID)
	assert.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Verify the registry contains the functions.
	ctx := context.Background()
	gotFuncs, err := registry.GetFunctions(ctx, userID, deviceID)
	require.NoError(t, err)
	require.Len(t, gotFuncs, 2)
	assert.Equal(t, "read_file", gotFuncs[0].Name)
	assert.Equal(t, "write_file", gotFuncs[1].Name)

	// Disconnect the client.
	conn.Close()

	// Wait for the server cleanup (handleWebSocket defer) to run.
	waitForCleanup(t, 3*time.Second, func() bool {
		fns, regErr := registry.GetFunctions(ctx, userID, deviceID)
		if regErr != nil {
			return false
		}
		return fns == nil
	}, "functions should be cleared after disconnect")
}

// ---------------------------------------------------------------------------
// TestFunctionLifecycle_DeviceReplacement_OldFunctionsCleared
//
// Verifies that device replacement preserves functions when the new device
// (same userID+deviceID) is still connected, and cleans them up only after
// the last connection for that device disconnects.
// ---------------------------------------------------------------------------

func TestFunctionLifecycle_DeviceReplacement_OldFunctionsCleared(t *testing.T) {
	t.Parallel()

	srv, registry, addr, cleanup := startWSServerWithRegistry(t)
	defer cleanup()

	const userID = "user-lifecycle-replace"
	const deviceID = "device-beta"
	ctx := context.Background()

	functions := []protocol.FunctionInfo{
		{Name: "search", Description: "Search documents"},
	}

	// Device A connects and registers functions.
	connA := connectWSWithDevice(t, addr, userID, deviceID)

	resp := sendRegisterFunctions(t, connA, "reg-a", functions)
	assert.Equal(t, "reg-a", resp.ID)
	assert.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Verify registry has the functions.
	gotFuncs, err := registry.GetFunctions(ctx, userID, deviceID)
	require.NoError(t, err)
	require.Len(t, gotFuncs, 1)
	assert.Equal(t, "search", gotFuncs[0].Name)

	// Device B connects with the same (userID, deviceID) -- triggers device
	// replacement (D-095). The old connection (A) receives a 4001 close frame.
	connB := connectWSWithDevice(t, addr, userID, deviceID)
	defer connB.Close()

	// Wait for device B to be registered and device A to be cleaned up.
	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 3*time.Second, 50*time.Millisecond, "device B should be registered")

	// With the race condition fix (Fix 1), device A's cleanup will check
	// clientsByDevice and skip OnDeviceDisconnect if device B is already
	// registered. So we can re-register on device B immediately without
	// waiting for A's cleanup to finish. Even if A's cleanup runs after
	// B registers, it will not wipe B's functions.
	//
	// Previously this used time.Sleep(300ms) which was flaky in CI.

	// Re-register functions on device B (since the device replacement cleared
	// the old connection, but the function registry is keyed by (userID,
	// deviceID) not by connection). The functions from device A are still in
	// the registry because OnDeviceDisconnect is called for the old connection
	// but device B has the same deviceID. However, the handleWebSocket cleanup
	// for device A will call OnDeviceDisconnect(userID, deviceID), which
	// removes the entry. Device B must re-register.
	resp = sendRegisterFunctions(t, connB, "reg-b", functions)
	assert.Equal(t, "reg-b", resp.ID)
	assert.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Verify the registry still has the functions (registered by device B).
	gotFuncs, err = registry.GetFunctions(ctx, userID, deviceID)
	require.NoError(t, err)
	require.Len(t, gotFuncs, 1, "functions should exist after device B re-registers")

	// Now close device B.
	connB.Close()

	// Wait for cleanup.
	waitForCleanup(t, 3*time.Second, func() bool {
		fns, regErr := registry.GetFunctions(ctx, userID, deviceID)
		if regErr != nil {
			return false
		}
		return fns == nil
	}, "functions should be cleared after device B disconnects")
}

// ---------------------------------------------------------------------------
// TestFunctionLifecycle_Disconnect_OtherDevicesUnaffected
//
// Verifies that when device A disconnects and its functions are cleared,
// device B's functions remain unaffected.
// ---------------------------------------------------------------------------

func TestFunctionLifecycle_Disconnect_OtherDevicesUnaffected(t *testing.T) {
	t.Parallel()

	_, registry, addr, cleanup := startWSServerWithRegistry(t)
	defer cleanup()

	const userID = "user-lifecycle-multi"
	const deviceA = "device-gamma"
	const deviceB = "device-delta"
	ctx := context.Background()

	functionsA := []protocol.FunctionInfo{
		{Name: "func_a", Description: "Function from device A"},
	}
	functionsB := []protocol.FunctionInfo{
		{Name: "func_b", Description: "Function from device B"},
	}

	// Device A connects and registers.
	connA := connectWSWithDevice(t, addr, userID, deviceA)

	respA := sendRegisterFunctions(t, connA, "reg-a", functionsA)
	assert.Equal(t, "reg-a", respA.ID)
	assert.Equal(t, protocol.ResponseCodeOK, respA.Code)

	// Device B connects and registers.
	connB := connectWSWithDevice(t, addr, userID, deviceB)
	defer connB.Close()

	respB := sendRegisterFunctions(t, connB, "reg-b", functionsB)
	assert.Equal(t, "reg-b", respB.ID)
	assert.Equal(t, protocol.ResponseCodeOK, respB.Code)

	// Verify both devices have their functions.
	gotA, err := registry.GetFunctions(ctx, userID, deviceA)
	require.NoError(t, err)
	require.Len(t, gotA, 1)
	assert.Equal(t, "func_a", gotA[0].Name)

	gotB, err := registry.GetFunctions(ctx, userID, deviceB)
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	assert.Equal(t, "func_b", gotB[0].Name)

	// Disconnect device A.
	connA.Close()

	// Wait for device A's functions to be cleared.
	waitForCleanup(t, 3*time.Second, func() bool {
		fns, regErr := registry.GetFunctions(ctx, userID, deviceA)
		if regErr != nil {
			return false
		}
		return fns == nil
	}, "device A functions should be cleared after disconnect")

	// Device B's functions should still be present.
	gotB, err = registry.GetFunctions(ctx, userID, deviceB)
	require.NoError(t, err)
	require.Len(t, gotB, 1, "device B functions should not be affected by device A disconnect")
	assert.Equal(t, "func_b", gotB[0].Name)
}
