package tools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/cloudwego/eino/components/tool"
)

// ToolFactory creates a tool instance on demand.
// The config map carries per-tool configuration from the agent's YAML
// tool_config block (D-078).
type ToolFactory func(ctx context.Context, config map[string]any) (tool.BaseTool, error)

// Registry manages tool creation and lookup.
// All methods are safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]ToolFactory
	Logger    *log.Logger
}

// NewRegistry creates an empty Registry.
// If Logger is nil when Create is called, log.Default() is used.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]ToolFactory),
	}
}

// Register adds or replaces a tool factory under the given name.
func (r *Registry) Register(name string, factory ToolFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// Create instantiates the named tools in order.
//
// Unregistered names are logged as warnings and skipped (fail-open, D-078).
// Factory errors are collected into a single joined error returned at the end
// so that one broken tool does not prevent the remaining tools from being
// created.
func (r *Registry) Create(ctx context.Context, names []string, config map[string]any) ([]tool.BaseTool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	logger := r.Logger
	if logger == nil {
		logger = log.Default()
	}

	// Per-tool config: if config["<toolName>"] is a map, use it; otherwise
	// pass the top-level config as-is so factories can read shared settings.
	var (
		tools []tool.BaseTool
		errs  []error
	)
	for _, name := range names {
		factory, ok := r.factories[name]
		if !ok {
			logger.Printf("[WARN] tools: unregistered tool %q skipped (fail-open, D-078)", name)
			continue
		}
		var toolCfg map[string]any
		if sub, ok := config[name]; ok {
			if m, ok := sub.(map[string]any); ok {
				toolCfg = m
			}
		}
		t, err := factory(ctx, toolCfg)
		if err != nil {
			logger.Printf("[ERROR] tools: create %q: %v", name, err)
			errs = append(errs, fmt.Errorf("tool %q: %w", name, err))
			continue
		}
		tools = append(tools, t)
	}

	if len(errs) > 0 {
		return tools, fmt.Errorf("tools: %d factory error(s): %w", len(errs), errors.Join(errs...))
	}
	return tools, nil
}

// ListNames returns the registered tool names in sorted order.
func (r *Registry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DefaultRegistry is the global registry pre-populated with built-in tools
// at init time. Custom tools can be registered into it from main.go.
var DefaultRegistry = NewRegistry()

func init() {
	// Register built-in tools. Each factory ignores the config map for now;
	// per-tool configuration can be wired in later steps.
	DefaultRegistry.Register("get_weather", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return NewWeatherTool()
	})
	DefaultRegistry.Register("get_current_time", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return NewTimeTool()
	})
	DefaultRegistry.Register("retrieve_tool_result", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return NewRetrieveTool(DefaultToolResultStore)
	})
}
