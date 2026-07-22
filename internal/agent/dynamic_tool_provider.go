package agent

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"

	agenttools "github.com/PineappleBond/xyncra-server/internal/agent/tools"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ClientFunctionProvider retrieves function declarations registered by a
// client device. Defined here to avoid circular dependency on server
// package (D-101).
type ClientFunctionProvider interface {
	GetFunctions(ctx context.Context, userID, deviceID string) ([]protocol.FunctionInfo, error)
	// GetFunctionsByUser returns all registered functions for a userID,
	// keyed by deviceID. Used when the agent's deviceID is unknown.
	GetFunctionsByUser(ctx context.Context, userID string) (map[string][]protocol.FunctionInfo, error)
}

// DynamicToolProvider is an Eino ChatModelAgentMiddleware that dynamically
// injects client-device functions as InvokableTool instances before each
// agent run (Phase 6 / D-100, D-101, D-102).
type DynamicToolProvider struct {
	*adk.BaseChatModelAgentMiddleware

	funcRegistry ClientFunctionProvider
	config       ClientToolsConfig
	logger       Logger
	toolRegistry *agenttools.Registry // for resolving dynamic_tools from agent config
	dynamicTools []string             // tool names to resolve from registry at runtime
}

// NewDynamicToolProvider creates a DynamicToolProvider.
// toolRegistry and dynamicTools enable resolution of static tools from the
// registry at runtime (per-execution), which is required for the Eino
// framework's 0->nonzero tool transition to trigger a graph rebuild.
func NewDynamicToolProvider(
	registry ClientFunctionProvider,
	cfg ClientToolsConfig,
	logger Logger,
	toolRegistry *agenttools.Registry,
	dynamicTools []string,
) *DynamicToolProvider {
	if logger == nil {
		logger = noopLogger{}
	}
	return &DynamicToolProvider{
		funcRegistry: registry,
		config:       cfg,
		logger:       logger,
		toolRegistry: toolRegistry,
		dynamicTools: dynamicTools,
	}
}

// BeforeAgent implements the Eino middleware hook. It queries the calling
// device's registered functions and appends them as tools to runCtx.Tools.
// It also resolves dynamic_tools from the tool registry. Both injection
// paths are independent: dynamic_tools are injected even when no device
// context or client functions are available.
// Fail-open (D-072 spirit): errors in GetFunctions or tool creation are
// logged and skipped, never blocking agent execution.
func (d *DynamicToolProvider) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	var merged []tool.BaseTool

	// --- Client function tools (require agent identity in context) ---
	// Use the agent's userID to look up functions registered by the agent's
	// device(s). This is distinct from CallerDevice which carries the human
	// sender's identity (used for tracing/debug).
	agentID, hasAgent := AgentIDFromContext(ctx)
	if hasAgent && agentID != "" {
		// Extract base userID from agentID (e.g. "agent/ui-assistant" -> "agent").
		// Client devices register functions under the base userID, not the full agentID.
		baseUserID := agentID
		if idx := strings.Index(agentID, "/"); idx > 0 {
			baseUserID = agentID[:idx]
		}
		// 2. Get registered functions for this agent user, keyed by deviceID (fail-open).
		deviceFuncs, err := d.funcRegistry.GetFunctionsByUser(ctx, baseUserID)
		if err != nil {
			d.logger.Error("DynamicToolProvider: GetFunctionsByUser failed", "agent", agentID, "error", err)
		} else if len(deviceFuncs) > 0 {
			// Deduplicate across devices: when multiple devices register the same
			// function name, prefer the lexicographically smallest deviceID for
			// deterministic behavior (map iteration order is non-deterministic).
			type funcEntry struct {
				deviceID string
				info     protocol.FunctionInfo
			}
			seenFuncs := make(map[string]funcEntry)
			// Sort deviceIDs for deterministic iteration.
			deviceIDs := make([]string, 0, len(deviceFuncs))
			for deviceID := range deviceFuncs {
				deviceIDs = append(deviceIDs, deviceID)
			}
			sort.Strings(deviceIDs)
			for _, deviceID := range deviceIDs {
				for _, fn := range deviceFuncs[deviceID] {
					if existing, exists := seenFuncs[fn.Name]; !exists || deviceID < existing.deviceID {
						seenFuncs[fn.Name] = funcEntry{deviceID: deviceID, info: fn}
					}
				}
			}

			// Group by deviceID.
			deviceFuncsDeduped := make(map[string][]protocol.FunctionInfo)
			for _, entry := range seenFuncs {
				deviceFuncsDeduped[entry.deviceID] = append(deviceFuncsDeduped[entry.deviceID], entry.info)
			}

			for deviceID, funcs := range deviceFuncsDeduped {
				funcs = d.applyFilters(funcs)
				if len(funcs) == 0 {
					continue
				}

				var tools []tool.BaseTool
				// Convert CallTimeout (time.Duration) to milliseconds for the tool.
				defaultTimeoutMs := int(d.config.CallTimeout / time.Millisecond)
				for _, fn := range funcs {
					t, err := newClientFunctionTool(fn, agentID, deviceID, defaultTimeoutMs)
					if err != nil {
						d.logger.Error("DynamicToolProvider: failed to create tool", "function", fn.Name, "error", err)
						continue // fail-open per function
					}
					tools = append(tools, t)
				}

				if len(tools) > 0 {
					merged = append(merged, tools...)
					d.logger.Debug("DynamicToolProvider: injected client tools", "count", len(tools), "device", deviceID)
				}
			}
		}
	}

	// --- Dynamic tools from registry (device-independent) ---
	if d.toolRegistry != nil && len(d.dynamicTools) > 0 {
		staticTools, err := d.toolRegistry.Create(ctx, d.dynamicTools, nil)
		if err != nil {
			d.logger.Error("DynamicToolProvider: failed to create dynamic tools", "error", err)
		}
		if len(staticTools) > 0 {
			merged = append(merged, staticTools...)
			d.logger.Debug("DynamicToolProvider: injected registry tools", "count", len(staticTools), "tools", d.dynamicTools)
		}
	}

	// Deduplicate: merged tools override existing tools with the same name.
	if len(merged) > 0 {
		// Build a set of names from merged (injected) tools.
		mergedNames := make(map[string]bool, len(merged))
		for _, t := range merged {
			if ti, err := t.Info(ctx); err == nil && ti != nil {
				mergedNames[ti.Name] = true
			}
		}

		// Keep existing tools whose names are NOT overridden by merged tools.
		newTools := make([]tool.BaseTool, 0, len(runCtx.Tools)+len(merged))
		for _, t := range runCtx.Tools {
			skip := false
			if ti, err := t.Info(ctx); err == nil && ti != nil {
				if mergedNames[ti.Name] {
					skip = true
				}
			}
			if !skip {
				newTools = append(newTools, t)
			}
		}

		// Deduplicate within merged itself (client functions may overlap with registry tools).
		seen := make(map[string]bool, len(merged))
		deduped := make([]tool.BaseTool, 0, len(merged))
		for _, t := range merged {
			if ti, err := t.Info(ctx); err == nil && ti != nil {
				if !seen[ti.Name] {
					seen[ti.Name] = true
					deduped = append(deduped, t)
				}
			} else {
				deduped = append(deduped, t) // keep if we can't get info
			}
		}

		newTools = append(newTools, deduped...)
		runCtx.Tools = newTools
		d.logger.Info("DynamicToolProvider: final tools count", "existing", len(runCtx.Tools)-len(deduped), "injected", len(deduped), "total", len(runCtx.Tools))
	}

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
