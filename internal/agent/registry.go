package agent

import (
	"io/fs"
	"log"
	"strings"
	"sync"
)

// AgentRegistry manages loaded agent configurations.
// It is safe for concurrent use.
type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*AgentConfig
}

// NewRegistry creates an empty AgentRegistry.
func NewRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*AgentConfig),
	}
}

// Load reads agent configuration files from the embedded filesystem.
// Invalid configs are logged and skipped (graceful degradation per D-001).
func (r *AgentRegistry) Load(fsys fs.FS) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			log.Printf("[WARN] agent: failed to read %s: %v", entry.Name(), err)
			continue
		}
		config, err := ParseFrontMatter(data)
		if err != nil {
			log.Printf("[WARN] agent: skipping %s: %v", entry.Name(), err)
			continue
		}
		r.mu.Lock()
		if _, exists := r.agents[config.ID]; exists {
			log.Printf("[WARN] agent: duplicate ID %q in %s, skipping", config.ID, entry.Name())
			r.mu.Unlock()
			continue
		}
		r.agents[config.ID] = config
		r.mu.Unlock()
	}
	return nil
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
// Agent userIDs have the format "agent/{id}" (D-054).
// Returns the AgentConfig and true if found, nil and false otherwise.
func (r *AgentRegistry) IsAgent(userID string) (*AgentConfig, bool) {
	const prefix = "agent/"
	if !strings.HasPrefix(userID, prefix) {
		return nil, false
	}
	id := strings.TrimPrefix(userID, prefix)
	if id == "" {
		return nil, false
	}
	return r.Get(id)
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
