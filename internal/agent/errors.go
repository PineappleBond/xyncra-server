package agent

import "errors"

// Sentinel errors for agent operations.
var (
	ErrMissingID        = errors.New("agent: missing required field: id")
	ErrMissingName      = errors.New("agent: missing required field: name")
	ErrMissingModel     = errors.New("agent: missing required field: model")
	ErrMissingAPIKeyEnv = errors.New("agent: missing required field: api_key_env")
	// ErrAgentNotFound indicates the agent ID was not found in the registry.
	ErrAgentNotFound      = errors.New("agent: not found in registry")
	ErrInvalidFrontMatter = errors.New("agent: invalid front matter")
	ErrNoFrontMatter      = errors.New("agent: no front matter found")
	ErrContextLoad        = errors.New("agent: failed to load context")

	// LLM provider errors
	ErrAPIKeyMissing    = errors.New("agent: API key environment variable not set")
	ErrUnsupportedModel = errors.New("agent: unsupported LLM provider for model")
	ErrLLMTimeout       = errors.New("agent: LLM request timed out")
	ErrLLMRateLimited   = errors.New("agent: LLM rate limited")
	ErrStreamClosed     = errors.New("agent: stream closed unexpectedly")

	// Agent execution errors
	ErrAgentBuild = errors.New("agent: failed to build agent")
	ErrAgentRun   = errors.New("agent: agent execution failed")

	// HITL errors (Phase 8B)
	ErrHITLInterrupted    = errors.New("agent: HITL interrupted, awaiting user input")
	ErrCheckpointStoreSet = errors.New("agent: checkpoint store failed to persist")
	ErrCheckpointNotFound = errors.New("agent: checkpoint not found or expired")

	// MCP errors (Phase 8C, D-086)
	ErrInvalidMCPTransport = errors.New("mcp: transport must be 'sse' or 'stdio'")
	ErrMCPMissingURL       = errors.New("mcp: SSE transport requires url")
	ErrMCPMissingCommand   = errors.New("mcp: stdio transport requires command")
	ErrMCPMissingName      = errors.New("mcp: server name is required")
	ErrMCPDuplicateName    = errors.New("mcp: duplicate server name")
	ErrMCPUnreachable      = errors.New("agent: MCP service unreachable")
)
