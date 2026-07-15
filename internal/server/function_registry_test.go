package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// newTestRegistry creates a MemoryFunctionRegistry with small limits suitable
// for unit tests. Callers can override via opts.
func newTestRegistry(t *testing.T, maxFuncs, maxNameLen int) *MemoryFunctionRegistry {
	t.Helper()
	return NewMemoryFunctionRegistry(FunctionRegistryConfig{
		MaxFunctionsPerDevice: maxFuncs,
		MaxFunctionNameLength: maxNameLen,
	})
}

// makeFunctions builds n FunctionInfo entries with deterministic names.
func makeFunctions(n int) []protocol.FunctionInfo {
	fns := make([]protocol.FunctionInfo, n)
	for i := range fns {
		fns[i] = protocol.FunctionInfo{
			Name:        fmt.Sprintf("func_%d", i),
			Description: fmt.Sprintf("description %d", i),
		}
	}
	return fns
}

// ---------------------------------------------------------------------------
// Normal path
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_Register_SingleDevice registers one function for
// a single device and verifies it can be retrieved.
func TestMemoryFunctionRegistry_Register_SingleDevice(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "my-cli", "type": "cli"},
		Functions: []protocol.FunctionInfo{
			{Name: "hello", Description: "say hello"},
		},
	}

	err := reg.RegisterFunctions(ctx, "user-1", "device-1", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	funcs, err := reg.GetFunctions(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("GetFunctions error: %v", err)
	}
	if len(funcs) != 1 {
		t.Fatalf("expected 1 function, got %d", len(funcs))
	}
	if funcs[0].Name != "hello" {
		t.Fatalf("expected function name 'hello', got %q", funcs[0].Name)
	}
	if funcs[0].Description != "say hello" {
		t.Fatalf("expected description 'say hello', got %q", funcs[0].Description)
	}
}

// TestMemoryFunctionRegistry_Register_MultipleFunctions registers several
// functions and verifies all are returned.
func TestMemoryFunctionRegistry_Register_MultipleFunctions(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	fns := makeFunctions(5)
	params := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "browser", "type": "browser"},
		Functions:  fns,
	}

	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	funcs, err := reg.GetFunctions(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("GetFunctions error: %v", err)
	}
	if len(funcs) != 5 {
		t.Fatalf("expected 5 functions, got %d", len(funcs))
	}
	for i, fn := range funcs {
		expected := fmt.Sprintf("func_%d", i)
		if fn.Name != expected {
			t.Errorf("function[%d]: expected name %q, got %q", i, expected, fn.Name)
		}
	}
}

// TestMemoryFunctionRegistry_Register_UpdateExisting verifies that calling
// RegisterFunctions again replaces the previous list (full replacement
// semantics).
func TestMemoryFunctionRegistry_Register_UpdateExisting(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	// First registration.
	params1 := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "cli", "type": "cli"},
		Functions: []protocol.FunctionInfo{
			{Name: "old_func_a"},
			{Name: "old_func_b"},
		},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params1); err != nil {
		t.Fatalf("first register error: %v", err)
	}

	// Second registration — full replacement.
	params2 := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "cli-updated", "type": "cli"},
		Functions: []protocol.FunctionInfo{
			{Name: "new_func"},
		},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params2); err != nil {
		t.Fatalf("second register error: %v", err)
	}

	funcs, err := reg.GetFunctions(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("GetFunctions error: %v", err)
	}
	if len(funcs) != 1 {
		t.Fatalf("expected 1 function after replacement, got %d", len(funcs))
	}
	if funcs[0].Name != "new_func" {
		t.Fatalf("expected 'new_func', got %q", funcs[0].Name)
	}

	// Verify metadata was also updated.
	df, err := reg.GetDeviceFunctions(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("GetDeviceFunctions error: %v", err)
	}
	if df.DeviceInfo["name"] != "cli-updated" {
		t.Fatalf("expected DeviceInfo name 'cli-updated', got %q", df.DeviceInfo["name"])
	}
}

// TestMemoryFunctionRegistry_Register_MultipleDevices verifies that different
// devices of the same user maintain independent function lists.
func TestMemoryFunctionRegistry_Register_MultipleDevices(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params1 := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "cli", "type": "cli"},
		Functions:  []protocol.FunctionInfo{{Name: "cli_func"}},
	}
	params2 := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "browser", "type": "browser"},
		Functions:  []protocol.FunctionInfo{{Name: "browser_func"}},
	}

	if err := reg.RegisterFunctions(ctx, "user-1", "device-cli", params1); err != nil {
		t.Fatalf("register device-cli error: %v", err)
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-browser", params2); err != nil {
		t.Fatalf("register device-browser error: %v", err)
	}

	funcs1, err := reg.GetFunctions(ctx, "user-1", "device-cli")
	if err != nil {
		t.Fatalf("GetFunctions device-cli error: %v", err)
	}
	if len(funcs1) != 1 || funcs1[0].Name != "cli_func" {
		t.Fatalf("unexpected functions for device-cli: %+v", funcs1)
	}

	funcs2, err := reg.GetFunctions(ctx, "user-1", "device-browser")
	if err != nil {
		t.Fatalf("GetFunctions device-browser error: %v", err)
	}
	if len(funcs2) != 1 || funcs2[0].Name != "browser_func" {
		t.Fatalf("unexpected functions for device-browser: %+v", funcs2)
	}
}

// TestMemoryFunctionRegistry_Register_MultipleUsers verifies isolation between
// different users.
func TestMemoryFunctionRegistry_Register_MultipleUsers(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "cli", "type": "cli"},
		Functions:  []protocol.FunctionInfo{{Name: "func_a"}},
	}

	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("register user-1 error: %v", err)
	}

	params2 := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "cli", "type": "cli"},
		Functions:  []protocol.FunctionInfo{{Name: "func_b"}},
	}
	if err := reg.RegisterFunctions(ctx, "user-2", "device-1", params2); err != nil {
		t.Fatalf("register user-2 error: %v", err)
	}

	// Each user should see only their own functions.
	funcs1, _ := reg.GetFunctions(ctx, "user-1", "device-1")
	funcs2, _ := reg.GetFunctions(ctx, "user-2", "device-1")

	if len(funcs1) != 1 || funcs1[0].Name != "func_a" {
		t.Fatalf("user-1 unexpected functions: %+v", funcs1)
	}
	if len(funcs2) != 1 || funcs2[0].Name != "func_b" {
		t.Fatalf("user-2 unexpected functions: %+v", funcs2)
	}
}

// ---------------------------------------------------------------------------
// Boundary conditions
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_Register_FunctionNameEmpty verifies that a
// function with an empty name returns ErrFunctionNameEmpty.
func TestMemoryFunctionRegistry_Register_FunctionNameEmpty(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		Functions: []protocol.FunctionInfo{
			{Name: ""},
		},
	}

	err := reg.RegisterFunctions(ctx, "user-1", "device-1", params)
	if !errors.Is(err, ErrFunctionNameEmpty) {
		t.Fatalf("expected ErrFunctionNameEmpty, got: %v", err)
	}
}

// TestMemoryFunctionRegistry_Register_ExactlyAtLimit verifies that registering
// exactly MaxFunctionsPerDevice functions succeeds.
func TestMemoryFunctionRegistry_Register_ExactlyAtLimit(t *testing.T) {
	t.Parallel()

	const limit = 5
	reg := newTestRegistry(t, limit, 255)
	ctx := context.Background()

	fns := makeFunctions(limit)
	params := &RegisterFunctionsParams{Functions: fns}

	err := reg.RegisterFunctions(ctx, "user-1", "device-1", params)
	if err != nil {
		t.Fatalf("expected success at limit, got error: %v", err)
	}

	funcs, _ := reg.GetFunctions(ctx, "user-1", "device-1")
	if len(funcs) != limit {
		t.Fatalf("expected %d functions, got %d", limit, len(funcs))
	}
}

// TestMemoryFunctionRegistry_Register_ExceedsLimit verifies that registering
// more than MaxFunctionsPerDevice functions returns ErrMaxFunctionsPerDevice.
func TestMemoryFunctionRegistry_Register_ExceedsLimit(t *testing.T) {
	t.Parallel()

	const limit = 5
	reg := newTestRegistry(t, limit, 255)
	ctx := context.Background()

	fns := makeFunctions(limit + 1)
	params := &RegisterFunctionsParams{Functions: fns}

	err := reg.RegisterFunctions(ctx, "user-1", "device-1", params)
	if !errors.Is(err, ErrMaxFunctionsPerDevice) {
		t.Fatalf("expected ErrMaxFunctionsPerDevice, got: %v", err)
	}
}

// TestMemoryFunctionRegistry_Register_EmptyFunctionsList verifies that an
// empty functions list is valid and results in zero registered functions.
func TestMemoryFunctionRegistry_Register_EmptyFunctionsList(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "cli", "type": "cli"},
		Functions:  []protocol.FunctionInfo{},
	}

	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("unexpected error with empty functions: %v", err)
	}

	funcs, err := reg.GetFunctions(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("GetFunctions error: %v", err)
	}
	if len(funcs) != 0 {
		t.Fatalf("expected 0 functions, got %d", len(funcs))
	}

	// DeviceFunctions should still exist.
	df, err := reg.GetDeviceFunctions(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("GetDeviceFunctions error: %v", err)
	}
	if df == nil {
		t.Fatal("expected non-nil DeviceFunctions")
	}
	if df.DeviceInfo["name"] != "cli" {
		t.Fatalf("expected DeviceInfo name 'cli', got %q", df.DeviceInfo["name"])
	}
}

// TestMemoryFunctionRegistry_Register_FunctionNameTooLong verifies that a
// function name exceeding MaxFunctionNameLength returns ErrFunctionNameTooLong.
func TestMemoryFunctionRegistry_Register_FunctionNameTooLong(t *testing.T) {
	t.Parallel()

	const maxLen = 10
	reg := newTestRegistry(t, 10, maxLen)
	ctx := context.Background()

	// Name exactly at limit should work.
	paramsOK := &RegisterFunctionsParams{
		Functions: []protocol.FunctionInfo{
			{Name: strings.Repeat("a", maxLen)},
		},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", paramsOK); err != nil {
		t.Fatalf("name at limit should succeed, got: %v", err)
	}

	// Name exceeding limit should fail.
	paramsBad := &RegisterFunctionsParams{
		Functions: []protocol.FunctionInfo{
			{Name: strings.Repeat("b", maxLen+1)},
		},
	}
	err := reg.RegisterFunctions(ctx, "user-1", "device-2", paramsBad)
	if !errors.Is(err, ErrFunctionNameTooLong) {
		t.Fatalf("expected ErrFunctionNameTooLong, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrent scenarios
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_ConcurrentRegister_DifferentDevices verifies
// that concurrent registration of different devices is race-free.
func TestMemoryFunctionRegistry_ConcurrentRegister_DifferentDevices(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func(idx int) {
			defer wg.Done()
			deviceID := fmt.Sprintf("device-%d", idx)
			params := &RegisterFunctionsParams{
				DeviceInfo: map[string]string{"name": deviceID, "type": "cli"},
				Functions:  []protocol.FunctionInfo{{Name: fmt.Sprintf("fn_%d", idx)}},
			}
			if err := reg.RegisterFunctions(ctx, "user-1", deviceID, params); err != nil {
				t.Errorf("concurrent register %s failed: %v", deviceID, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all devices are registered.
	for i := range n {
		deviceID := fmt.Sprintf("device-%d", i)
		funcs, err := reg.GetFunctions(ctx, "user-1", deviceID)
		if err != nil {
			t.Fatalf("GetFunctions(%s) error: %v", deviceID, err)
		}
		if len(funcs) != 1 {
			t.Errorf("device %s: expected 1 function, got %d", deviceID, len(funcs))
		}
	}
}

// TestMemoryFunctionRegistry_ConcurrentRegister_SameDevice verifies that
// concurrent registration of the same device does not corrupt state. The final
// state should contain exactly one registration (last writer wins).
func TestMemoryFunctionRegistry_ConcurrentRegister_SameDevice(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func(idx int) {
			defer wg.Done()
			params := &RegisterFunctionsParams{
				DeviceInfo: map[string]string{"name": fmt.Sprintf("iter-%d", idx), "type": "cli"},
				Functions:  []protocol.FunctionInfo{{Name: fmt.Sprintf("fn_%d", idx)}},
			}
			if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
				t.Errorf("concurrent register iter-%d failed: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify exactly one registration exists.
	funcs, err := reg.GetFunctions(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("GetFunctions error: %v", err)
	}
	if len(funcs) != 1 {
		t.Fatalf("expected 1 function after concurrent writes, got %d", len(funcs))
	}

	df, _ := reg.GetDeviceFunctions(ctx, "user-1", "device-1")
	if df == nil {
		t.Fatal("expected non-nil DeviceFunctions")
	}
}

// TestMemoryFunctionRegistry_ConcurrentReadWrite verifies that concurrent
// reads and writes do not cause races.
func TestMemoryFunctionRegistry_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 100, 255)
	ctx := context.Background()

	// Pre-populate.
	params := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "cli", "type": "cli"},
		Functions:  []protocol.FunctionInfo{{Name: "initial"}},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("pre-populate error: %v", err)
	}

	const writers = 10
	const readers = 20
	var wg sync.WaitGroup
	wg.Add(writers + readers)

	// Writers.
	for i := range writers {
		go func(idx int) {
			defer wg.Done()
			p := &RegisterFunctionsParams{
				DeviceInfo: map[string]string{"name": fmt.Sprintf("writer-%d", idx), "type": "cli"},
				Functions:  []protocol.FunctionInfo{{Name: fmt.Sprintf("fn_%d", idx)}},
			}
			_ = reg.RegisterFunctions(ctx, "user-1", "device-1", p)
		}(i)
	}

	// Readers.
	for range readers {
		go func() {
			defer wg.Done()
			_, _ = reg.GetFunctions(ctx, "user-1", "device-1")
			_, _ = reg.GetDeviceFunctions(ctx, "user-1", "device-1")
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_GetFunctions_NonExistentDevice verifies that
// querying a device that has never registered returns (nil, nil).
func TestMemoryFunctionRegistry_GetFunctions_NonExistentDevice(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	funcs, err := reg.GetFunctions(ctx, "user-unknown", "device-unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if funcs != nil {
		t.Fatalf("expected nil, got: %+v", funcs)
	}
}

// TestMemoryFunctionRegistry_GetDeviceFunctions_NonExistentUser verifies that
// GetDeviceFunctions returns (nil, nil) for a user that has never registered.
func TestMemoryFunctionRegistry_GetDeviceFunctions_NonExistentUser(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	df, err := reg.GetDeviceFunctions(ctx, "user-unknown", "device-unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if df != nil {
		t.Fatalf("expected nil, got: %+v", df)
	}
}

// TestMemoryFunctionRegistry_GetDeviceFunctions_NonExistentDevice verifies that
// GetDeviceFunctions returns (nil, nil) when the user exists but the device
// has not registered.
func TestMemoryFunctionRegistry_GetDeviceFunctions_NonExistentDevice(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	// Register a device for user-1.
	params := &RegisterFunctionsParams{
		Functions: []protocol.FunctionInfo{{Name: "fn_a"}},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("register error: %v", err)
	}

	// Query a different device that doesn't exist.
	df, err := reg.GetDeviceFunctions(ctx, "user-1", "device-999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if df != nil {
		t.Fatalf("expected nil for non-existent device, got: %+v", df)
	}
}

// TestMemoryFunctionRegistry_GetDeviceFunctions_AfterRegister verifies that
// GetDeviceFunctions returns the complete record after registration.
func TestMemoryFunctionRegistry_GetDeviceFunctions_AfterRegister(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "my-browser", "type": "browser"},
		Functions: []protocol.FunctionInfo{
			{Name: "fn_a", Description: "does A"},
			{Name: "fn_b", Description: "does B"},
		},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("register error: %v", err)
	}

	df, err := reg.GetDeviceFunctions(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("GetDeviceFunctions error: %v", err)
	}
	if df == nil {
		t.Fatal("expected non-nil DeviceFunctions")
	}
	if df.UserID != "user-1" {
		t.Errorf("expected UserID 'user-1', got %q", df.UserID)
	}
	if df.DeviceID != "device-1" {
		t.Errorf("expected DeviceID 'device-1', got %q", df.DeviceID)
	}
	if df.DeviceInfo["name"] != "my-browser" {
		t.Errorf("expected DeviceInfo name 'my-browser', got %q", df.DeviceInfo["name"])
	}
	if df.DeviceInfo["type"] != "browser" {
		t.Errorf("expected DeviceInfo type 'browser', got %q", df.DeviceInfo["type"])
	}
	if len(df.Functions) != 2 {
		t.Errorf("expected 2 functions, got %d", len(df.Functions))
	}
	if df.RegisteredAt.IsZero() {
		t.Error("expected non-zero RegisteredAt")
	}
}

// ---------------------------------------------------------------------------
// Disconnect
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_OnDeviceDisconnect_Existing verifies that
// disconnecting an existing device removes it and returns the removed data.
func TestMemoryFunctionRegistry_OnDeviceDisconnect_Existing(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "cli", "type": "cli"},
		Functions:  []protocol.FunctionInfo{{Name: "fn_a"}},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("register error: %v", err)
	}

	removed, err := reg.OnDeviceDisconnect(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("OnDeviceDisconnect error: %v", err)
	}
	if removed == nil {
		t.Fatal("expected non-nil removed DeviceFunctions")
	}
	if removed.DeviceID != "device-1" {
		t.Errorf("expected removed DeviceID 'device-1', got %q", removed.DeviceID)
	}
	if len(removed.Functions) != 1 || removed.Functions[0].Name != "fn_a" {
		t.Errorf("unexpected removed functions: %+v", removed.Functions)
	}

	// Verify the device is gone.
	funcs, _ := reg.GetFunctions(ctx, "user-1", "device-1")
	if funcs != nil {
		t.Fatalf("expected nil after disconnect, got: %+v", funcs)
	}
}

// TestMemoryFunctionRegistry_OnDeviceDisconnect_NonExistent verifies that
// disconnecting a non-existent device returns (nil, nil).
func TestMemoryFunctionRegistry_OnDeviceDisconnect_NonExistent(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	removed, err := reg.OnDeviceDisconnect(ctx, "user-unknown", "device-unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != nil {
		t.Fatalf("expected nil, got: %+v", removed)
	}
}

// TestMemoryFunctionRegistry_OnDeviceDisconnect_Idempotent verifies that
// calling OnDeviceDisconnect twice is idempotent.
func TestMemoryFunctionRegistry_OnDeviceDisconnect_Idempotent(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		Functions: []protocol.FunctionInfo{{Name: "fn_a"}},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("register error: %v", err)
	}

	// First disconnect.
	removed1, err := reg.OnDeviceDisconnect(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("first disconnect error: %v", err)
	}
	if removed1 == nil {
		t.Fatal("first disconnect should return non-nil")
	}

	// Second disconnect — idempotent.
	removed2, err := reg.OnDeviceDisconnect(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatalf("second disconnect error: %v", err)
	}
	if removed2 != nil {
		t.Fatalf("second disconnect should return nil, got: %+v", removed2)
	}
}

// ---------------------------------------------------------------------------
// Default config
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_DefaultConfig verifies that zero/negative config
// values are replaced with defaults.
func TestMemoryFunctionRegistry_DefaultConfig(t *testing.T) {
	t.Parallel()

	reg := NewMemoryFunctionRegistry(FunctionRegistryConfig{})
	if reg.config.MaxFunctionsPerDevice != DefaultMaxFunctionsPerDevice {
		t.Errorf("expected MaxFunctionsPerDevice=%d, got %d",
			DefaultMaxFunctionsPerDevice, reg.config.MaxFunctionsPerDevice)
	}
	if reg.config.MaxFunctionNameLength != DefaultMaxFunctionNameLength {
		t.Errorf("expected MaxFunctionNameLength=%d, got %d",
			DefaultMaxFunctionNameLength, reg.config.MaxFunctionNameLength)
	}

	regNeg := NewMemoryFunctionRegistry(FunctionRegistryConfig{
		MaxFunctionsPerDevice: -1,
		MaxFunctionNameLength: -1,
	})
	if regNeg.config.MaxFunctionsPerDevice != DefaultMaxFunctionsPerDevice {
		t.Errorf("negative config: expected MaxFunctionsPerDevice=%d, got %d",
			DefaultMaxFunctionsPerDevice, regNeg.config.MaxFunctionsPerDevice)
	}
}

// ---------------------------------------------------------------------------
// Slice isolation
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_SliceIsolation verifies that mutating the
// returned slice does not affect registry state.
func TestMemoryFunctionRegistry_SliceIsolation(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		Functions: []protocol.FunctionInfo{{Name: "original"}},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("register error: %v", err)
	}

	// Mutate the returned slice.
	funcs, _ := reg.GetFunctions(ctx, "user-1", "device-1")
	funcs[0].Name = "MUTATED"

	// Re-read — should still be "original".
	funcs2, _ := reg.GetFunctions(ctx, "user-1", "device-1")
	if funcs2[0].Name != "original" {
		t.Fatalf("registry state was mutated! got %q", funcs2[0].Name)
	}
}

// TestMemoryFunctionRegistry_GetDeviceFunctions_DeepCopy verifies that
// GetDeviceFunctions returns a deep copy so that callers cannot mutate
// internal registry state through the returned pointer.
func TestMemoryFunctionRegistry_GetDeviceFunctions_DeepCopy(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "original-device", "type": "cli"},
		Functions: []protocol.FunctionInfo{
			{Name: "fn_a", Description: "original desc"},
		},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("register error: %v", err)
	}

	// Get the DeviceFunctions and mutate it.
	df, err := reg.GetDeviceFunctions(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.NotNil(t, df)

	df.DeviceInfo["name"] = "MUTATED"
	df.Functions[0].Name = "MUTATED_FUNC"
	df.Functions[0].Description = "MUTATED_DESC"

	// Re-read — should still have original values.
	df2, err := reg.GetDeviceFunctions(ctx, "user-1", "device-1")
	require.NoError(t, err)
	require.NotNil(t, df2)
	assert.Equal(t, "original-device", df2.DeviceInfo["name"], "DeviceInfo name should not be mutated")
	assert.Equal(t, "fn_a", df2.Functions[0].Name, "function name should not be mutated")
	assert.Equal(t, "original desc", df2.Functions[0].Description, "function description should not be mutated")
}

// ---------------------------------------------------------------------------
// Memory cleanup
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_OnDeviceDisconnect_CleansUpUserMap verifies that
// when the last device for a user is disconnected, the user-level map entry is
// removed (no memory leak).
func TestMemoryFunctionRegistry_OnDeviceDisconnect_CleansUpUserMap(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		Functions: []protocol.FunctionInfo{{Name: "fn"}},
	}
	if err := reg.RegisterFunctions(ctx, "user-1", "device-1", params); err != nil {
		t.Fatalf("register error: %v", err)
	}

	if _, err := reg.OnDeviceDisconnect(ctx, "user-1", "device-1"); err != nil {
		t.Fatalf("disconnect error: %v", err)
	}

	// User-level map should be cleaned up.
	reg.mu.RLock()
	_, exists := reg.devices["user-1"]
	reg.mu.RUnlock()

	if exists {
		t.Fatal("user-level map entry should be cleaned up after last device disconnects")
	}
}

// ---------------------------------------------------------------------------
// Duplicate function name validation
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_Register_DuplicateFunctionNames verifies that
// registering functions with duplicate names within a single request returns
// ErrFunctionNameDuplicate.
func TestMemoryFunctionRegistry_Register_DuplicateFunctionNames(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t, 10, 255)
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		Functions: []protocol.FunctionInfo{
			{Name: "foo"},
			{Name: "bar"},
			{Name: "foo"}, // duplicate
		},
	}

	err := reg.RegisterFunctions(ctx, "user-1", "device-1", params)
	if !errors.Is(err, ErrFunctionNameDuplicate) {
		t.Fatalf("expected ErrFunctionNameDuplicate, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FunctionInfo full round-trip
// ---------------------------------------------------------------------------

// TestMemoryFunctionRegistry_Register_FunctionInfoFullRoundTrip verifies that
// all optional fields of FunctionInfo (Parameters, Returns, Tags, TimeoutMs)
// survive a register-then-read round trip.
func TestMemoryFunctionRegistry_Register_FunctionInfoFullRoundTrip(t *testing.T) {
	t.Parallel()

	registry := NewMemoryFunctionRegistry(FunctionRegistryConfig{})
	ctx := context.Background()

	params := &RegisterFunctionsParams{
		DeviceInfo: map[string]string{"name": "test-device", "type": "cli"},
		Functions: []protocol.FunctionInfo{
			{
				Name:        "read_file",
				Description: "Read a local file",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
					"required": []string{"path"},
				},
				Returns: &protocol.ReturnInfo{
					Type:        "string",
					Description: "File contents",
				},
				Tags:      []string{"filesystem", "io"},
				TimeoutMs: 5000,
			},
		},
	}

	require.NoError(t, registry.RegisterFunctions(ctx, "user1", "dev1", params))

	funcs, err := registry.GetFunctions(ctx, "user1", "dev1")
	require.NoError(t, err)
	require.Len(t, funcs, 1)

	fn := funcs[0]
	assert.Equal(t, "read_file", fn.Name)
	assert.Equal(t, "Read a local file", fn.Description)
	assert.Equal(t, "object", fn.Parameters["type"])
	assert.NotNil(t, fn.Returns)
	assert.Equal(t, "string", fn.Returns.Type)
	assert.Equal(t, "File contents", fn.Returns.Description)
	assert.Equal(t, []string{"filesystem", "io"}, fn.Tags)
	assert.Equal(t, 5000, fn.TimeoutMs)
}
