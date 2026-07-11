// Sub-agent resolution and delegation for AgentBuilder (D-081).
package agent

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

// resolveSubAgents looks up each sub-agent ID referenced by config.SubAgents,
// builds the child agent, and wraps it as a tool via adk.NewAgentTool.
//
// Recursion guard (D-081): the child config's SubAgents field is cleared before
// Build so that sub-agents cannot themselves declare sub-agents (depth limit 1).
// Unknown sub-agent IDs are logged and skipped (fail-open).
//
// The returned slice is appended to the parent's tool list by AgentBuilder.Build.
func (b *AgentBuilder) resolveSubAgents(ctx context.Context, parentConfig *AgentConfig) ([]tool.BaseTool, error) {
	if len(parentConfig.SubAgents) == 0 {
		return nil, nil
	}

	var (
		subTools []tool.BaseTool
		errs     []error
	)

	for _, subID := range parentConfig.SubAgents {
		subConfig, ok := b.registry.Get(subID)
		if !ok {
			log.Default().Printf("[WARN] agent %s: sub-agent %q not found in registry, skipping (D-081)", parentConfig.ID, subID)
			continue
		}

		// Clear sub-agents to enforce depth limit 1 (D-081).
		childConfig := *subConfig
		childConfig.SubAgents = nil

		child, err := b.Build(ctx, &childConfig)
		if err != nil {
			log.Default().Printf("[ERROR] agent %s: failed to build sub-agent %q: %v", parentConfig.ID, subID, err)
			errs = append(errs, fmt.Errorf("sub-agent %q: %w", subID, err))
			continue
		}

		// Wrap the child Agent as a tool. NewAgentTool uses the agent's Name
		// and Description as the tool name and description, so they must be
		// non-empty (already validated by AgentConfig.Validate).
		subTool := adk.NewAgentTool(ctx, child.Agent)
		subTools = append(subTools, subTool)
	}

	if len(errs) > 0 {
		return subTools, fmt.Errorf("agent %s: %d sub-agent(s) failed to build: %w",
			parentConfig.ID, len(errs), errors.Join(errs...))
	}
	return subTools, nil
}
