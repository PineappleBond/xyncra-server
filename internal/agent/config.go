package agent

import (
	"fmt"
	"time"
)

// MiddlewareConfig controls which Eino middleware to enable per agent.
// All fields are optional; when the middleware block is absent from YAML,
// no middleware is applied (backward compatible with Phase 1-7).
type MiddlewareConfig struct {
	EnableSummarization   bool `yaml:"enable_summarization" json:"enable_summarization"`
	SummarizationTokens   int  `yaml:"summarization_tokens" json:"summarization_tokens"` // default: 160000
	EnableToolReduction   bool `yaml:"enable_tool_reduction" json:"enable_tool_reduction"`
	ToolReductionMaxChars int  `yaml:"tool_reduction_max_chars" json:"tool_reduction_max_chars"` // default: 50000
	EnablePatchToolCalls  bool `yaml:"enable_patch_tool_calls" json:"enable_patch_tool_calls"`
	// EnableClientTools enables dynamic injection of client device functions
	// as agent tools. Requires ClientFunctionProvider to be set on AgentBuilder.
	// Client functions use the RemoteCalling interrupt-resume pattern (D-137).
	EnableClientTools bool `yaml:"enable_client_tools" json:"enable_client_tools"`
	// ClientTools controls how client device functions are surfaced as tools.
	ClientTools ClientToolsConfig `yaml:"client_tools" json:"client_tools"`
}

// ClientToolsConfig controls how client device functions are surfaced as
// agent tools (Phase 6 / D-100, D-101).
type ClientToolsConfig struct {
	// FunctionTags filters functions by tag. Empty = accept all functions.
	// A function matches if it has at least one tag in this list (OR semantics).
	FunctionTags []string `yaml:"function_tags" json:"function_tags"`
	// ExcludedFunctions excludes specific function names. Exact match only.
	ExcludedFunctions []string `yaml:"excluded_functions" json:"excluded_functions"`
	// CallTimeout is the default timeout for client function calls.
	// Individual functions may override via timeout_ms. Default: 120s.
	// A zero value means "use default (120s)".
	CallTimeout time.Duration `yaml:"call_timeout" json:"call_timeout"`
}

// MCPServerConfig defines an MCP server connection (D-086).
// Transport selects the connection method: "sse" for remote servers via
// Server-Sent Events, "stdio" for local processes communicating over
// standard input/output.
type MCPServerConfig struct {
	Name      string   `yaml:"name" json:"name"`
	Transport string   `yaml:"transport" json:"transport"`                 // "sse" or "stdio"
	URL       string   `yaml:"url,omitempty" json:"url,omitempty"`         // SSE endpoint
	Command   string   `yaml:"command,omitempty" json:"command,omitempty"` // stdio command
	Args      []string `yaml:"args,omitempty" json:"args,omitempty"`       // stdio arguments
	Env       []string `yaml:"env,omitempty" json:"env,omitempty"`         // stdio environment variables
	Tools     []string `yaml:"tools,omitempty" json:"tools,omitempty"`     // filter specific tools (empty = all)
}

// AgentConfig represents the configuration for an AI agent.
// Parsed from YAML front matter in agent definition files.
type AgentConfig struct {
	ID          string          `yaml:"id" json:"id"`
	Name        string          `yaml:"name" json:"name"`
	Description string          `yaml:"description" json:"description"`
	Model       string          `yaml:"model" json:"model"`
	APIKeyEnv   string          `yaml:"api_key_env" json:"api_key_env"`
	BaseURL     string          `yaml:"base_url" json:"base_url"`
	Parameters  AgentParameters `yaml:"parameters" json:"parameters"`
	Context     AgentContext    `yaml:"context" json:"context"`
	Tools       []string        `yaml:"tools" json:"tools"`
	// DynamicTools lists tool names that should be resolved from the tool
	// registry at runtime (per-execution) instead of at build time. This
	// enables the Eino framework's 0->nonzero tool transition, which triggers
	// graph rebuild and ensures tools are indexed in the ToolNode.
	DynamicTools []string          `yaml:"dynamic_tools" json:"dynamic_tools"`
	ToolConfig   map[string]any    `yaml:"tool_config" json:"tool_config"` // per-tool config passed to tool factories
	Middleware   MiddlewareConfig  `yaml:"middleware" json:"middleware"`   // middleware toggles (D-079)
	SubAgents    []string          `yaml:"sub_agents" json:"sub_agents"`   // Phase 8B placeholder (D-081)
	MCPServers   []MCPServerConfig `yaml:"mcp_servers" json:"mcp_servers"` // MCP server connections (D-086)
	SystemPrompt string            `yaml:"-" json:"system_prompt"`         // Markdown body
}

// AgentParameters holds model generation parameters.
type AgentParameters struct {
	Temperature float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	MaxTokens   int     `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	TopP        float64 `yaml:"top_p,omitempty" json:"top_p,omitempty"`
}

// AgentContext holds context window configuration.
type AgentContext struct {
	MaxTokens   int `yaml:"max_tokens" json:"max_tokens"`
	MaxMessages int `yaml:"max_messages" json:"max_messages"`
}

// Validate checks that the AgentConfig has all required fields.
// It also validates nested MCP server configurations (D-086).
func (c *AgentConfig) Validate() error {
	if c.ID == "" {
		return ErrMissingID
	}
	if c.Name == "" {
		return ErrMissingName
	}
	if c.Model == "" {
		return ErrMissingModel
	}
	if c.APIKeyEnv == "" {
		return ErrMissingAPIKeyEnv
	}
	seen := make(map[string]bool)
	for _, mcp := range c.MCPServers {
		if mcp.Name == "" {
			return ErrMCPMissingName
		}
		if seen[mcp.Name] {
			return fmt.Errorf("%w: %q", ErrMCPDuplicateName, mcp.Name)
		}
		seen[mcp.Name] = true
		switch mcp.Transport {
		case "sse":
			if mcp.URL == "" {
				return ErrMCPMissingURL
			}
		case "stdio":
			if mcp.Command == "" {
				return ErrMCPMissingCommand
			}
		default:
			return ErrInvalidMCPTransport
		}
	}
	return nil
}
