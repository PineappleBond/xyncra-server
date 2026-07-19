package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AgentRegistry manages loaded agent configurations.
// It is safe for concurrent use.
type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*AgentConfig
	dir    string // directory path from which configs were loaded (D-077)
	logger Logger // structured logger; defaults to noopLogger{}
}

// NewRegistry creates an empty AgentRegistry.
func NewRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*AgentConfig),
		logger: noopLogger{},
	}
}

// SetLogger sets the structured logger for registry operations.
// If logger is nil, the call is ignored (the existing logger is kept).
func (r *AgentRegistry) SetLogger(logger Logger) {
	if logger == nil {
		return
	}
	r.mu.Lock()
	r.logger = logger
	r.mu.Unlock()
}

// Dir returns the directory path from which agents were last loaded.
func (r *AgentRegistry) Dir() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dir
}

// Load scans the given directory for .md agent config files and loads them.
// Existing agents are cleared before loading.
// If the directory does not exist, Load returns nil (optional module, D-063).
func (r *AgentRegistry) Load(dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents = make(map[string]*AgentConfig)
	r.dir = dir

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read agents dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			r.logger.Info("agent: failed to read config file, skipping",
				"file", entry.Name(), "error", err)
			continue
		}
		config, err := ParseFrontMatter(data)
		if err != nil {
			r.logger.Info("agent: skipping invalid config",
				"file", entry.Name(), "error", err)
			continue
		}
		if _, exists := r.agents[config.ID]; exists {
			r.logger.Info("agent: duplicate ID, skipping",
				"id", config.ID, "file", entry.Name())
			continue
		}
		r.agents[config.ID] = config
	}
	return nil
}

// Reload re-scans the agents directory and reloads all configurations.
func (r *AgentRegistry) Reload() error {
	r.mu.RLock()
	dir := r.dir
	r.mu.RUnlock()
	return r.Load(dir)
}

// Register adds an agent config to the registry.
// This is primarily intended for testing; production code should use Load.
func (r *AgentRegistry) Register(config *AgentConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[config.ID] = config
}

// Get returns the AgentConfig for the given agent ID.
func (r *AgentRegistry) Get(id string) (*AgentConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	config, ok := r.agents[id]
	return config, ok
}

// IsAgent reports whether the given userID corresponds to a registered agent.
// It performs an exact-match lookup in the registry (D-054 revised).
// Returns the AgentConfig and true if found, nil and false otherwise.
func (r *AgentRegistry) IsAgent(userID string) (*AgentConfig, bool) {
	return r.Get(userID)
}

// ListAll returns a copy of all registered agent configurations.
func (r *AgentRegistry) ListAll() []*AgentConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*AgentConfig, 0, len(r.agents))
	for _, config := range r.agents {
		result = append(result, config)
	}
	return result
}

// Count returns the number of registered agents.
func (r *AgentRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}
