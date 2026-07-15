package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// builtinFunctionInfos returns metadata for the built-in diagnostic
// functions that every client device automatically exposes (D-098, D-099).
func builtinFunctionInfos() []protocol.FunctionInfo {
	return []protocol.FunctionInfo{
		{
			Name:        "ping",
			Description: "Echo back a message with timestamp",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{
						"type":        "string",
						"description": "Message to echo back",
					},
				},
			},
			Tags: []string{"diagnostic"},
			Returns: &protocol.ReturnInfo{
				Type:        "object",
				Description: "Echoed message and server timestamp",
			},
		},
		{
			Name:        "get_device_info",
			Description: "Get basic device information",
			Parameters:  map[string]any{"type": "object"},
			Tags:        []string{"diagnostic"},
			Returns: &protocol.ReturnInfo{
				Type:        "object",
				Description: "Device hostname, OS, architecture, and process ID",
			},
		},
		{
			Name:        "get_time",
			Description: "Get current device time",
			Parameters:  map[string]any{"type": "object"},
			Tags:        []string{"diagnostic"},
			Returns: &protocol.ReturnInfo{
				Type:        "object",
				Description: "Current UTC time, unix timestamp, and timezone",
			},
		},
	}
}

// registerBuiltinHandlers registers the built-in function handlers on the
// given XyncraClient so the server can invoke them via reverse RPC (D-092).
func registerBuiltinHandlers(xc *client.XyncraClient) {
	xc.RegisterRequestHandler("ping", pingHandler)
	xc.RegisterRequestHandler("get_device_info", getDeviceInfoHandler)
	xc.RegisterRequestHandler("get_time", getTimeHandler)
}

// pingHandler handles "ping" requests: echoes the input message together
// with an RFC 3339 nano timestamp.
func pingHandler(_ context.Context, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	var params struct {
		Message string `json:"message"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("ping: parse params: %w", err)
		}
	}
	resp := map[string]any{
		"echo":      params.Message,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("ping: marshal response: %w", err)
	}
	return data, nil
}

// getDeviceInfoHandler handles "get_device_info" requests: returns the
// host name, operating system, CPU architecture, and process ID.
func getDeviceInfoHandler(_ context.Context, _ *protocol.PackageDataRequest) (json.RawMessage, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	resp := map[string]any{
		"hostname": hostname,
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"pid":      os.Getpid(),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("get_device_info: marshal response: %w", err)
	}
	return data, nil
}

// getTimeHandler handles "get_time" requests: returns the current UTC time,
// Unix timestamp, and timezone string.
func getTimeHandler(_ context.Context, _ *protocol.PackageDataRequest) (json.RawMessage, error) {
	now := time.Now()
	resp := map[string]any{
		"utc":      now.UTC().Format(time.RFC3339Nano),
		"unix":     now.Unix(),
		"timezone": now.Location().String(),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("get_time: marshal response: %w", err)
	}
	return data, nil
}
