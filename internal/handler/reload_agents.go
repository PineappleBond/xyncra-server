package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// reloadAgentsHandler handles the "reload_agents" RPC method (D-076).
// It re-scans the agents directory on disk and reloads all configurations.
type reloadAgentsHandler struct {
	registry *agent.AgentRegistry
}

// NewReloadAgentsHandler creates a handler for the "reload_agents" RPC method.
// If registry is nil, the handler returns {"count": 0} without error (D-063).
func NewReloadAgentsHandler(registry *agent.AgentRegistry) *reloadAgentsHandler {
	return &reloadAgentsHandler{registry: registry}
}

// HandleRequest implements MethodHandler. It reloads agent configurations
// from disk and returns the number of loaded agents.
func (h *reloadAgentsHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	if h.registry == nil {
		return json.Marshal(map[string]int{"count": 0})
	}
	if err := h.registry.Reload(); err != nil {
		return nil, fmt.Errorf("reload agents from %q: %w", h.registry.Dir(), err)
	}
	count := len(h.registry.ListAll())
	return json.Marshal(map[string]int{"count": count})
}
