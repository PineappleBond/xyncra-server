package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	agenttools "github.com/PineappleBond/xyncra-server/internal/agent/tools"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// newClientFunctionTool creates a tool.InvokableTool backed by a remote
// client function. The tool's Info() returns a schema built from the
// FunctionInfo's JSON Schema; InvokableRun() calls ServerRequest to
// invoke the function on the originating device (D-100).
func newClientFunctionTool(
	funcInfo protocol.FunctionInfo,
	c ClientCaller,
	userID, deviceID string,
	defaultTimeout time.Duration,
) (tool.InvokableTool, error) {
	toolInfo, err := buildToolInfo(funcInfo)
	if err != nil {
		return nil, fmt.Errorf("build tool info for %q: %w", funcInfo.Name, err)
	}

	return utils.NewTool[json.RawMessage, string](
		toolInfo,
		func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeClientFunction(ctx, c, userID, deviceID, funcInfo, input, defaultTimeout)
		},
	), nil
}

// buildToolInfo constructs a schema.ToolInfo from a protocol.FunctionInfo.
// The FunctionInfo's Parameters (map[string]any JSON Schema) is converted
// to *jsonschema.Schema via JSON roundtrip.
func buildToolInfo(funcInfo protocol.FunctionInfo) (*schema.ToolInfo, error) {
	params := funcInfo.Parameters
	if len(params) == 0 {
		// Normalize nil and empty {} to a valid object schema. An empty
		// schema (or missing parameters) is later converted by the LLM
		// tool-format layer into `parameters: true`, which the OpenAI-
		// compatible endpoint rejects with a 400 validation error.
		params = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	// JSON roundtrip: map[string]any -> bytes -> *jsonschema.Schema
	schemaBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal parameters: %w", err)
	}

	var js jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &js); err != nil {
		return nil, fmt.Errorf("unmarshal to jsonschema: %w", err)
	}

	return &schema.ToolInfo{
		Name:        funcInfo.Name,
		Desc:        funcInfo.Description,
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&js),
	}, nil
}

// executeClientFunction sends a ReverseRPC request to the client device
// and returns the response data as a string. Errors are mapped to
// LLM-friendly messages per D-100.
//
// When the device is detected as offline, the function waits briefly (3s)
// and retries once, since the device may be in the process of reconnecting.
func executeClientFunction(
	ctx context.Context,
	c ClientCaller,
	userID, deviceID string,
	funcInfo protocol.FunctionInfo,
	input json.RawMessage,
	defaultTimeout time.Duration,
) (string, error) {
	timeout := defaultTimeout
	if funcInfo.TimeoutMs > 0 {
		timeout = time.Duration(funcInfo.TimeoutMs) * time.Millisecond
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	resp, err := c.ServerRequest(ctx, userID, deviceID, funcInfo.Name, input, timeout)
	if err != nil && isDeviceOfflineError(err) {
		// Wait 3 seconds before retrying — the device may be reconnecting.
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return agenttools.SoftFailure("tool call cancelled"), nil
		}
		resp, err = c.ServerRequest(ctx, userID, deviceID, funcInfo.Name, input, timeout)
	}
	if err != nil {
		// Recoverable failure: surface the reason as normal tool content so the
		// LLM can self-correct or retry, instead of aborting the run (D-101).
		return agenttools.SoftFailure(formatClientToolError(err)), nil
	}

	if resp.Code < 0 {
		// Client returned a business error (e.g. unknown method, handler
		// failure). Return it as content, not a Go error, so the LLM sees the
		// reason and can adapt (D-101).
		return agenttools.SoftFailure(fmt.Sprintf(
			"client returned error (code %d): %s", resp.Code, resp.Msg,
		)), nil
	}

	return string(resp.Data), nil
}

// isDeviceOfflineError returns true if the error indicates the target device
// is not connected. It checks for known offline-related error strings that
// cross the agent/server package boundary via fmt.Errorf wrapping.
func isDeviceOfflineError(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "device offline") ||
		strings.Contains(errStr, "no connections") ||
		strings.Contains(errStr, "device is offline")
}

// formatClientToolError maps low-level errors to LLM-friendly messages
// per D-100. The returned string is used as the tool error shown to the
// LLM, allowing it to decide on retry or user notification.
func formatClientToolError(err error) string {
	errStr := err.Error()

	if strings.Contains(errStr, "deadline exceeded") || strings.Contains(errStr, "timeout") {
		return "tool call failed: request timed out. The client device may be slow or unresponsive."
	}

	if strings.Contains(errStr, "persisted for replay") {
		return "tool call failed: device is temporarily offline. The request has been queued and will be executed when the device reconnects. Please inform the user to wait a moment."
	}

	if strings.Contains(errStr, "no connections") || strings.Contains(errStr, "device") {
		return "tool call failed: device is offline. The client device is not currently connected."
	}

	return "tool call failed: unable to reach the device. Connection may have been lost."
}
