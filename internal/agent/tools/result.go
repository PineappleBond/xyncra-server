package tools

import (
	"encoding/json"
)

// ToolResult is the unified envelope returned by tools for BOTH success and
// recoverable failure. A "recoverable failure" is any situation where the
// calling LLM could plausibly correct course if it knew the reason — e.g. an
// invalid argument, a client device that is offline, or a tool that returned
// a business error. By returning the failure as normal tool content (not a Go
// error), the LLM sees the reason and can self-correct or retry, instead of
// the Eino framework aborting the whole run with a NodeRunError (D-101).
//
// Genuine programming/infrastructure errors (unparseable input, panics,
// unavailable backing services) should still be returned as Go errors so they
// fail fast.
type ToolResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// SoftFailure returns a ToolResult JSON string describing a recoverable
// failure. It is intended to be returned as normal tool content with a nil
// Go error, so the LLM can read the reason and recover.
func SoftFailure(msg string) string {
	b, _ := json.Marshal(ToolResult{Success: false, Error: msg})
	return string(b)
}

// SuccessResult marshals a successful payload into a ToolResult JSON string.
// Tools that adopt the envelope may use this for consistency; tools that
// return domain-specific structs unchanged may continue to do so.
func SuccessResult(data any) (string, error) {
	b, err := json.Marshal(ToolResult{Success: true, Data: data})
	if err != nil {
		return "", err
	}
	return string(b), nil
}
