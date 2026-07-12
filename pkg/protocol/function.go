package protocol

// FunctionInfo describes a single callable function that a client device
// exposes. It is the wire format used in system.register_functions requests.
type FunctionInfo struct {
	// Name is the unique function identifier within the device scope.
	// Must be non-empty and no longer than 255 characters.
	Name string `json:"name"`

	// Description is an optional human-readable summary of what the
	// function does. It is stored as-is and returned verbatim on queries.
	Description string `json:"description,omitempty"`

	// Parameters is an optional JSON Schema (draft 7) describing the
	// function's input parameters. The server does not validate against
	// this schema; it is stored for consumers (e.g. agents) to interpret.
	Parameters map[string]any `json:"parameters,omitempty"`

	// Returns optionally describes the function's return value.
	Returns *ReturnInfo `json:"returns,omitempty"`

	// Tags are optional labels for filtering functions.
	Tags []string `json:"tags,omitempty"`

	// TimeoutMs is the optional per-function timeout in milliseconds.
	// If zero, the Agent's default call_timeout applies.
	TimeoutMs int `json:"timeout_ms,omitempty"`
}

// ReturnInfo describes a function's return value.
type ReturnInfo struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}
