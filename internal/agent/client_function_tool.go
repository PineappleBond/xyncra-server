package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	agenttools "github.com/PineappleBond/xyncra-server/internal/agent/tools"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// newClientFunctionTool creates a tool.InvokableTool backed by a remote
// client function. The tool's Info() returns a schema built from the
// FunctionInfo's JSON Schema; InvokableRun() triggers a tool.Interrupt
// to pause execution and create a RemoteCalling for async client invocation.
//
// On resume, the tool detects it via tool.GetResumeContext and returns the
// client's response directly without re-interrupting.
func newClientFunctionTool(
	funcInfo protocol.FunctionInfo,
	userID, deviceID string,
	defaultCallTimeoutMs int,
) (tool.InvokableTool, error) {
	toolInfo, err := buildToolInfo(funcInfo)
	if err != nil {
		return nil, fmt.Errorf("build tool info for %q: %w", funcInfo.Name, err)
	}

	return utils.NewTool[json.RawMessage, string](
		toolInfo,
		func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeClientFunction(ctx, funcInfo, deviceID, input, defaultCallTimeoutMs)
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

// executeClientFunction invokes a client function via the RemoteCalling
// interrupt-resume pattern. On first call, it triggers tool.Interrupt to
// pause execution. On resume, it returns the client's response data.
//
// The interrupt data JSON contains method, params, device_id, and timeout_ms.
// The executor's interrupt handler parses this to create a RemoteCalling record.
func executeClientFunction(
	ctx context.Context,
	funcInfo protocol.FunctionInfo,
	deviceID string,
	input json.RawMessage,
	defaultCallTimeoutMs int,
) (string, error) {
	// Check if we are being resumed after an interrupt.
	isResumeTarget, hasData, data := tool.GetResumeContext[string](ctx)
	if isResumeTarget && hasData {
		return data, nil
	}
	if isResumeTarget && !hasData {
		return agenttools.SoftFailure("客户端函数调用超时或已被取消"), nil
	}

	// First call: build interrupt data and trigger interrupt.
	timeoutMs := NormalizeClientFunctionTimeout(funcInfo.TimeoutMs, defaultCallTimeoutMs)

	interruptData, err := json.Marshal(map[string]any{
		"method":     funcInfo.Name,
		"params":     string(input),
		"device_id":  deviceID,
		"timeout_ms": timeoutMs,
	})
	if err != nil {
		return agenttools.SoftFailure("构建中断数据失败"), nil
	}

	return "", tool.Interrupt(ctx, string(interruptData))
}
