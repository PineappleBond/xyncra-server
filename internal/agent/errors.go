package agent

import "errors"

// Sentinel errors for agent operations.
var (
	ErrMissingID        = errors.New("agent: missing required field: id")
	ErrMissingName      = errors.New("agent: missing required field: name")
	ErrMissingModel     = errors.New("agent: missing required field: model")
	ErrMissingAPIKeyEnv = errors.New("agent: missing required field: api_key_env")
	// ErrAgentNotFound is reserved for future use when agent lookup
	// needs to return an error instead of a boolean.
	ErrAgentNotFound      = errors.New("agent: not found")
	ErrInvalidFrontMatter = errors.New("agent: invalid front matter")
	ErrNoFrontMatter      = errors.New("agent: no front matter found")
)
