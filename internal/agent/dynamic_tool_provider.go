package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ClientFunctionProvider retrieves function declarations registered by a
// client device. Defined here to avoid circular dependency on server
// package (D-101).
type ClientFunctionProvider interface {
	GetFunctions(ctx context.Context, userID, deviceID string) ([]protocol.FunctionInfo, error)
}

// ClientCaller sends a request to a specific client device and waits for
// a response. Defined here to avoid circular dependency on server
// package (D-101).
type ClientCaller interface {
	ServerRequest(ctx context.Context, userID, deviceID, method string, params json.RawMessage, timeout time.Duration) (*protocol.PackageDataResponse, error)
}

// DynamicToolProvider is an Eino ChatModelAgentMiddleware that dynamically
// injects client-device functions as InvokableTool instances before each
// agent run (Phase 6 / D-100, D-101, D-102).
type DynamicToolProvider struct {
	*adk.BaseChatModelAgentMiddleware

	funcRegistry ClientFunctionProvider
	caller       ClientCaller
	config       ClientToolsConfig
	logger       Logger
}

// NewDynamicToolProvider creates a DynamicToolProvider.
func NewDynamicToolProvider(
	registry ClientFunctionProvider,
	caller ClientCaller,
	cfg ClientToolsConfig,
	logger Logger,
) *DynamicToolProvider {
	if logger == nil {
		logger = noopLogger{}
	}
	return &DynamicToolProvider{
		funcRegistry: registry,
		caller:       caller,
		config:       cfg,
		logger:       logger,
	}
}

// BeforeAgent implements the Eino middleware hook. It queries the calling
// device's registered functions and appends them as tools to runCtx.Tools.
// Fail-open (D-072 spirit): errors in GetFunctions or tool creation are
// logged and skipped, never blocking agent execution.
func (d *DynamicToolProvider) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	// 1. Extract caller device from context.
	device, ok := CallerDeviceFromContext(ctx)
	if !ok {
		// No device info (e.g. MQ task without DeviceID) -- skip injection.
		return ctx, runCtx, nil
	}

	// 2. Get registered functions for this device (fail-open).
	funcs, err := d.funcRegistry.GetFunctions(ctx, device.UserID, device.DeviceID)
	if err != nil {
		d.logger.Error("DynamicToolProvider: GetFunctions failed", "user", device.UserID, "device", device.DeviceID, "error", err)
		return ctx, runCtx, nil // fail-open
	}
	if len(funcs) == 0 {
		return ctx, runCtx, nil
	}

	// 3. Apply filters.
	funcs = d.applyFilters(funcs)
	if len(funcs) == 0 {
		return ctx, runCtx, nil
	}

	// 4. Create tools for each function.
	defaultTimeout := d.config.CallTimeout
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}

	var tools []tool.BaseTool
	for _, fn := range funcs {
		t, err := newClientFunctionTool(fn, d.caller, device.UserID, device.DeviceID, defaultTimeout)
		if err != nil {
			d.logger.Error("DynamicToolProvider: failed to create tool", "function", fn.Name, "error", err)
			continue // fail-open per function
		}
		tools = append(tools, t)
	}

	if len(tools) == 0 {
		return ctx, runCtx, nil
	}

	// 5. Append tools to runCtx.Tools (allocate new slice to avoid aliasing).
	merged := make([]tool.BaseTool, 0, len(runCtx.Tools)+len(tools))
	merged = append(merged, runCtx.Tools...)
	merged = append(merged, tools...)
	runCtx.Tools = merged

	d.logger.Info("DynamicToolProvider: injected client tools", "count", len(tools), "device", device.DeviceID)

	return ctx, runCtx, nil
}

// applyFilters returns the subset of funcs matching the configured tags and
// not in the excluded set.
//   - Excluded functions are checked first (exact match).
//   - Empty FunctionTags = accept all (no tag filtering).
//   - Non-empty FunctionTags = OR semantics: function matches if it has at
//     least one tag in the list.
func (d *DynamicToolProvider) applyFilters(funcs []protocol.FunctionInfo) []protocol.FunctionInfo {
	// Build excluded set for O(1) lookup.
	excluded := make(map[string]bool, len(d.config.ExcludedFunctions))
	for _, name := range d.config.ExcludedFunctions {
		excluded[name] = true
	}

	// Build tag set for O(1) lookup.
	tagSet := make(map[string]bool, len(d.config.FunctionTags))
	for _, tag := range d.config.FunctionTags {
		tagSet[tag] = true
	}

	var result []protocol.FunctionInfo
	for _, fn := range funcs {
		// Check excluded first.
		if excluded[fn.Name] {
			continue
		}

		// Check tags (empty = accept all).
		if len(tagSet) > 0 {
			matched := false
			for _, tag := range fn.Tags {
				if tagSet[tag] {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		result = append(result, fn)
	}

	return result
}
