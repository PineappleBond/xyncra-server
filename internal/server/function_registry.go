package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Configuration
// --------------------------------------------------------------------------

// FunctionRegistryConfig holds tunable limits for the FunctionRegistry.
type FunctionRegistryConfig struct {
	// MaxFunctionsPerDevice is the maximum number of functions a single
	// device may register. Zero or negative values fall back to
	// DefaultMaxFunctionsPerDevice.
	MaxFunctionsPerDevice int

	// MaxFunctionNameLength is the maximum allowed length of a function
	// name. Zero or negative values fall back to DefaultMaxFunctionNameLength.
	MaxFunctionNameLength int
}

const (
	// DefaultMaxFunctionsPerDevice is the default upper bound on the number
	// of functions a single device may register (D-099).
	DefaultMaxFunctionsPerDevice = 500

	// DefaultMaxFunctionNameLength is the default maximum length for a
	// function name (D-099).
	DefaultMaxFunctionNameLength = 255
)

// --------------------------------------------------------------------------
// DeviceFunctions
// --------------------------------------------------------------------------

// DeviceFunctions holds the registered functions for a single device.
type DeviceFunctions struct {
	// UserID is the authenticated user that owns the device.
	UserID string

	// DeviceID is the device identifier (D-093).
	DeviceID string

	// DeviceInfo holds arbitrary device metadata (e.g. "name", "type").
	DeviceInfo map[string]string

	// Functions is the list of callable functions declared by the device.
	Functions []protocol.FunctionInfo

	// RegisteredAt records when the functions were last registered.
	RegisteredAt time.Time
}

// --------------------------------------------------------------------------
// FunctionRegistry interface
// --------------------------------------------------------------------------

// FunctionRegistry manages client-declared function capabilities keyed by
// (userID, deviceID). Implementations must be safe for concurrent use.
type FunctionRegistry interface {
	// RegisterFunctions replaces the function list for a device. An empty
	// functions slice is valid and clears any previously registered
	// functions for the device.
	RegisterFunctions(ctx context.Context, userID, deviceID string, params *RegisterFunctionsParams) error

	// GetFunctions returns the registered functions for a device. If the
	// device has not registered any functions, it returns (nil, nil).
	GetFunctions(ctx context.Context, userID, deviceID string) ([]protocol.FunctionInfo, error)

	// GetDeviceFunctions returns the full device record including metadata.
	// If the device has not registered, it returns (nil, nil).
	GetDeviceFunctions(ctx context.Context, userID, deviceID string) (*DeviceFunctions, error)

	// GetFunctionsByUser returns all registered functions for the given userID,
	// keyed by deviceID. If no devices have registered, it returns (nil, nil).
	// This is used by DynamicToolProvider when the agent's deviceID is unknown
	// but its userID is known. The map keys enable callers to route tool
	// invocations to the correct device.
	GetFunctionsByUser(ctx context.Context, userID string) (map[string][]protocol.FunctionInfo, error)

	// OnDeviceDisconnect removes the function registration for a device.
	// It is idempotent: calling it for an unknown device returns (nil, nil).
	// The returned *DeviceFunctions (if non-nil) contains the data that was
	// removed, suitable for logging.
	OnDeviceDisconnect(ctx context.Context, userID, deviceID string) (*DeviceFunctions, error)
}

// --------------------------------------------------------------------------
// RegisterFunctionsParams
// --------------------------------------------------------------------------

// RegisterFunctionsParams is the RPC params sent by the client in a
// system.register_functions request (D-098, D-099).
type RegisterFunctionsParams struct {
	// DeviceID identifies the device registering functions.
	DeviceID string `json:"device_id"`

	// DeviceInfo holds arbitrary device metadata (e.g. "name", "type").
	DeviceInfo map[string]string `json:"device_info,omitempty"`

	// Functions is the list of callable functions the device exposes.
	Functions []protocol.FunctionInfo `json:"functions"`
}

// --------------------------------------------------------------------------
// Sentinel errors
// --------------------------------------------------------------------------

// Sentinel errors returned by FunctionRegistry implementations.
var (
	// ErrFunctionNameEmpty is returned when a function has an empty name.
	ErrFunctionNameEmpty = errors.New("function name must not be empty")

	// ErrFunctionNameTooLong is returned when a function name exceeds the
	// configured maximum length.
	ErrFunctionNameTooLong = errors.New("function name exceeds maximum length")

	// ErrFunctionNameDuplicate is returned when a registration contains
	// duplicate function names.
	ErrFunctionNameDuplicate = errors.New("duplicate function name in registration")

	// ErrMaxFunctionsPerDevice is returned when the number of functions
	// exceeds the per-device limit.
	ErrMaxFunctionsPerDevice = errors.New("max functions per device exceeded")
)

// --------------------------------------------------------------------------
// MemoryFunctionRegistry
// --------------------------------------------------------------------------

// MemoryFunctionRegistry is an in-memory FunctionRegistry backed by a
// two-level map (userID -> deviceID -> DeviceFunctions). It is safe for
// concurrent use.
type MemoryFunctionRegistry struct {
	mu      sync.RWMutex
	devices map[string]map[string]*DeviceFunctions // userID -> deviceID -> DeviceFunctions
	config  FunctionRegistryConfig
}

// NewMemoryFunctionRegistry creates a MemoryFunctionRegistry with the given
// configuration. Zero or negative config values are replaced with defaults.
func NewMemoryFunctionRegistry(cfg FunctionRegistryConfig) *MemoryFunctionRegistry {
	if cfg.MaxFunctionsPerDevice <= 0 {
		cfg.MaxFunctionsPerDevice = DefaultMaxFunctionsPerDevice
	}
	if cfg.MaxFunctionNameLength <= 0 {
		cfg.MaxFunctionNameLength = DefaultMaxFunctionNameLength
	}
	return &MemoryFunctionRegistry{
		devices: make(map[string]map[string]*DeviceFunctions),
		config:  cfg,
	}
}

// Compile-time check that MemoryFunctionRegistry satisfies FunctionRegistry.
var _ FunctionRegistry = (*MemoryFunctionRegistry)(nil)

// RegisterFunctions performs a full replacement of the function list for the
// given (userID, deviceID) pair. It validates each function name (non-empty,
// within length limit) and the total count against the per-device maximum
// before committing. An empty functions slice is valid and clears any
// previously registered functions.
func (r *MemoryFunctionRegistry) RegisterFunctions(_ context.Context, userID, deviceID string, params *RegisterFunctionsParams) error {
	// Validate function count.
	if len(params.Functions) > r.config.MaxFunctionsPerDevice {
		return ErrMaxFunctionsPerDevice
	}

	// Validate each function name.
	seen := make(map[string]struct{}, len(params.Functions))
	for i, fn := range params.Functions {
		if fn.Name == "" {
			return ErrFunctionNameEmpty
		}
		if len(fn.Name) > r.config.MaxFunctionNameLength {
			return ErrFunctionNameTooLong
		}
		if _, exists := seen[fn.Name]; exists {
			return ErrFunctionNameDuplicate
		}
		seen[fn.Name] = struct{}{}
		_ = i // index available for future per-function error context
	}

	// Build the device record. Make a copy of the functions slice so the
	// caller cannot mutate registry state after the call returns.
	funcs := make([]protocol.FunctionInfo, len(params.Functions))
	copy(funcs, params.Functions)

	// Deep copy DeviceInfo to prevent caller mutation.
	var deviceInfoCopy map[string]string
	if params.DeviceInfo != nil {
		deviceInfoCopy = make(map[string]string, len(params.DeviceInfo))
		for k, v := range params.DeviceInfo {
			deviceInfoCopy[k] = v
		}
	}

	record := &DeviceFunctions{
		UserID:       userID,
		DeviceID:     deviceID,
		DeviceInfo:   deviceInfoCopy,
		Functions:    funcs,
		RegisteredAt: time.Now(),
	}

	r.mu.Lock()
	userDevices, ok := r.devices[userID]
	if !ok {
		userDevices = make(map[string]*DeviceFunctions)
		r.devices[userID] = userDevices
	}
	userDevices[deviceID] = record
	r.mu.Unlock()

	return nil
}

// GetFunctions returns the registered functions for a device. If the device
// has not registered any functions, it returns (nil, nil).
func (r *MemoryFunctionRegistry) GetFunctions(_ context.Context, userID, deviceID string) ([]protocol.FunctionInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	userDevices, ok := r.devices[userID]
	if !ok {
		return nil, nil
	}
	df, ok := userDevices[deviceID]
	if !ok {
		return nil, nil
	}

	// Return a copy to prevent callers from mutating registry state.
	out := make([]protocol.FunctionInfo, len(df.Functions))
	copy(out, df.Functions)
	return out, nil
}

// GetFunctionsByUser returns all registered functions for the given userID,
// keyed by deviceID. If no devices have registered, it returns (nil, nil).
// Returns a copy to prevent callers from mutating registry state.
func (r *MemoryFunctionRegistry) GetFunctionsByUser(_ context.Context, userID string) (map[string][]protocol.FunctionInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	userDevices, ok := r.devices[userID]
	if !ok {
		return nil, nil
	}

	result := make(map[string][]protocol.FunctionInfo, len(userDevices))
	for deviceID, df := range userDevices {
		funcs := make([]protocol.FunctionInfo, len(df.Functions))
		copy(funcs, df.Functions)
		result[deviceID] = funcs
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// GetDeviceFunctions returns the full device record including metadata. If
// the device has not registered, it returns (nil, nil). The returned
// DeviceFunctions is a deep copy to prevent caller mutation of internal state.
func (r *MemoryFunctionRegistry) GetDeviceFunctions(_ context.Context, userID, deviceID string) (*DeviceFunctions, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	userDevices, ok := r.devices[userID]
	if !ok {
		return nil, nil
	}
	df, ok := userDevices[deviceID]
	if !ok {
		return nil, nil
	}

	// Return a deep copy to prevent caller mutation of internal state.
	cp := *df
	cp.Functions = make([]protocol.FunctionInfo, len(df.Functions))
	copy(cp.Functions, df.Functions)
	if df.DeviceInfo != nil {
		cp.DeviceInfo = make(map[string]string, len(df.DeviceInfo))
		for k, v := range df.DeviceInfo {
			cp.DeviceInfo[k] = v
		}
	}
	return &cp, nil
}

// OnDeviceDisconnect removes the function registration for a device. It is
// idempotent: if the device is not found, it returns (nil, nil). When the
// last device for a user is removed, the user-level map entry is cleaned up
// to avoid memory leaks.
func (r *MemoryFunctionRegistry) OnDeviceDisconnect(_ context.Context, userID, deviceID string) (*DeviceFunctions, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	userDevices, ok := r.devices[userID]
	if !ok {
		return nil, nil
	}
	df, ok := userDevices[deviceID]
	if !ok {
		return nil, nil
	}

	// Remove the device entry.
	delete(userDevices, deviceID)

	// Clean up the user-level map if no devices remain.
	if len(userDevices) == 0 {
		delete(r.devices, userID)
	}

	return df, nil
}
