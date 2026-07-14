package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// newRegisterFunctionsCommand creates the "register-functions" subcommand.
func newRegisterFunctionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-functions",
		Short: "Register callable functions on the server and listen for ReverseRPC invocations",
		Long: `Register callable functions on the server for a device. The command
sends a system.register_functions RPC to register the device's function
capabilities, then keeps the WebSocket connection open to handle
incoming ReverseRPC requests from the server.

Functions can be provided as a JSON string via --functions or as a JSON
file path via --functions-file.

Example:
  xyncra-client register-functions --user-id alice \
    --server ws://localhost:18080/ws \
    --functions '[{"name":"get_device_status","description":"Get device status","parameters":{}}]'

  xyncra-client register-functions --user-id alice \
    --server ws://localhost:18080/ws \
    --functions-file functions.json`,
		RunE: runRegisterFunctions,
	}

	cmd.Flags().String("functions", "", "JSON array of function definitions (required unless --functions-file is set)")
	cmd.Flags().String("functions-file", "", "Path to a JSON file containing function definitions (required unless --functions is set)")
	cmd.Flags().String("device-name", "xyncra-cli", "Human-readable device name")
	cmd.Flags().String("device-type", "cli", "Device type (e.g. cli, browser)")

	return cmd
}

// functionDef is a convenience alias for the wire format used in the CLI flag.
type functionDef = protocol.FunctionInfo

// runRegisterFunctions is the entry point for the "register-functions" subcommand.
func runRegisterFunctions(cmd *cobra.Command, _ []string) error {
	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("register-functions: %w", err)
	}

	functionsFlag, _ := cmd.Flags().GetString("functions")
	functionsFile, _ := cmd.Flags().GetString("functions-file")
	deviceName, _ := cmd.Flags().GetString("device-name")
	deviceType, _ := cmd.Flags().GetString("device-type")

	if functionsFlag == "" && functionsFile == "" {
		return fmt.Errorf("register-functions: one of --functions or --functions-file is required")
	}
	if functionsFlag != "" && functionsFile != "" {
		return fmt.Errorf("register-functions: --functions and --functions-file are mutually exclusive")
	}

	// Parse function definitions.
	var functions []protocol.FunctionInfo
	if functionsFlag != "" {
		if err := json.Unmarshal([]byte(functionsFlag), &functions); err != nil {
			return fmt.Errorf("register-functions: parse --functions: %w", err)
		}
	} else {
		data, err := os.ReadFile(functionsFile)
		if err != nil {
			return fmt.Errorf("register-functions: read --functions-file: %w", err)
		}
		if err := json.Unmarshal(data, &functions); err != nil {
			return fmt.Errorf("register-functions: parse --functions-file: %w", err)
		}
	}

	if len(functions) == 0 {
		return fmt.Errorf("register-functions: at least one function must be defined")
	}

	// Build WebSocket URL with user_id and device_id query parameters.
	wsURL, err := buildRegisterURL(cliCtx)
	if err != nil {
		return fmt.Errorf("register-functions: %w", err)
	}

	// Set up context with signal handling.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Dial the WebSocket server.
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()

	fmt.Fprintf(os.Stderr, "[xyncra] Connecting to %s ...\n", cliCtx.ServerURL)
	ws, _, err := websocket.DefaultDialer.DialContext(dialCtx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("register-functions: dial: %w", err)
	}
	defer ws.Close()

	fmt.Fprintf(os.Stderr, "[xyncra] Connected. Registering %d function(s)...\n", len(functions))

	// Send system.register_functions RPC.
	if err := sendRegisterFunctions(ws, cliCtx.DeviceID, deviceName, deviceType, functions); err != nil {
		return fmt.Errorf("register-functions: %w", err)
	}

	// Read the response.
	resp, err := readResponse(ws)
	if err != nil {
		return fmt.Errorf("register-functions: read response: %w", err)
	}

	if resp.Code != protocol.ResponseCodeOK {
		return fmt.Errorf("register-functions: server error (code=%d): %s", resp.Code, resp.Msg)
	}

	// Print registration result.
	var result map[string]any
	if err := json.Unmarshal(resp.Data, &result); err == nil {
		fmt.Fprintf(os.Stdout, "Functions registered successfully:\n")
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	} else {
		fmt.Fprintf(os.Stdout, "Functions registered successfully: %s\n", string(resp.Data))
	}

	fmt.Fprintf(os.Stderr, "[xyncra] Listening for ReverseRPC calls... (Ctrl+C to stop)\n")

	// Enter the message loop, handling incoming ReverseRPC requests.
	return messageLoop(ctx, ws, functions)
}

// buildRegisterURL constructs the WebSocket URL with user_id and device_id
// query parameters appended.
func buildRegisterURL(cliCtx *CLIContext) (string, error) {
	u, err := url.Parse(cliCtx.ServerURL)
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}
	q := u.Query()
	q.Set("user_id", cliCtx.UserID)
	q.Set("device_id", cliCtx.DeviceID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// sendRegisterFunctions sends a system.register_functions RPC request.
func sendRegisterFunctions(ws *websocket.Conn, deviceID, deviceName, deviceType string, functions []protocol.FunctionInfo) error {
	params := map[string]any{
		"device_id":   deviceID,
		"device_name": deviceName,
		"device_type": deviceType,
		"functions":   functions,
	}
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}

	reqData, err := json.Marshal(protocol.PackageDataRequest{
		ID:     "register-1",
		Method: "system.register_functions",
		Params: json.RawMessage(paramsBytes),
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	pkg := protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeRequest,
		Data:    json.RawMessage(reqData),
	}

	if err := ws.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	return ws.WriteJSON(pkg)
}

// readResponse reads a single Package from the WebSocket and parses it as a
// PackageDataResponse.
func readResponse(ws *websocket.Conn) (*protocol.PackageDataResponse, error) {
	if err := ws.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}
	var pkg protocol.Package
	if err := ws.ReadJSON(&pkg); err != nil {
		return nil, fmt.Errorf("read package: %w", err)
	}
	var resp protocol.PackageDataResponse
	if err := json.Unmarshal(pkg.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}

// messageLoop reads packages from the WebSocket and handles incoming
// ReverseRPC requests (PackageTypeRequest from the server). For each
// request, it dispatches to a mock handler and sends back a response.
// The loop exits when ctx is cancelled or the connection is closed.
func messageLoop(ctx context.Context, ws *websocket.Conn, functions []protocol.FunctionInfo) error {
	// Build a lookup of registered function names for mock dispatch.
	funcMap := make(map[string]protocol.FunctionInfo, len(functions))
	for _, f := range functions {
		funcMap[f.Name] = f
	}

	// Clear read deadline so ReadJSON blocks indefinitely.
	_ = ws.SetReadDeadline(time.Time{})

	// readDone is closed when the read goroutine exits.
	readDone := make(chan struct{})

	go func() {
		defer close(readDone)
		for {
			var pkg protocol.Package
			if err := ws.ReadJSON(&pkg); err != nil {
				// Check if context was cancelled (normal shutdown).
				if ctx.Err() != nil {
					return
				}
				// Check for websocket close error (normal shutdown).
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return
				}
				fmt.Fprintf(os.Stderr, "[xyncra] read error: %v\n", err)
				return
			}

			switch pkg.Type {
			case protocol.PackageTypeRequest:
				handleReverseRPC(ws, &pkg, funcMap)
			case protocol.PackageTypeUpdates:
				// Server-pushed updates; log but ignore.
				fmt.Fprintf(os.Stderr, "[xyncra] received update push\n")
			case protocol.PackageTypeResponse:
				// Unsolicited responses (e.g. heartbeat replies); log.
				fmt.Fprintf(os.Stderr, "[xyncra] received unsolicited response\n")
			default:
				fmt.Fprintf(os.Stderr, "[xyncra] received unknown package type: %d\n", pkg.Type)
			}
		}
	}()

	// Wait for context cancellation or read goroutine exit.
	select {
	case <-ctx.Done():
		fmt.Fprintf(os.Stderr, "\n[xyncra] Shutting down...\n")
		// Send a close frame to the server.
		_ = ws.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "client shutting down"),
			time.Now().Add(2*time.Second),
		)
	case <-readDone:
		fmt.Fprintf(os.Stderr, "[xyncra] Connection closed by server\n")
	}

	return nil
}

// handleReverseRPC processes an incoming server-initiated request package,
// dispatches to the appropriate mock handler, and sends back a response.
func handleReverseRPC(ws *websocket.Conn, pkg *protocol.Package, funcMap map[string]protocol.FunctionInfo) {
	var req protocol.PackageDataRequest
	if err := json.Unmarshal(pkg.Data, &req); err != nil {
		fmt.Fprintf(os.Stderr, "[xyncra] failed to parse reverse RPC request: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "[xyncra] ReverseRPC request: id=%s method=%s\n", req.ID, req.Method)

	var resp protocol.PackageDataResponse
	resp.ID = req.ID

	// Dispatch to mock handler.
	handler, ok := funcMap[req.Method]
	if !ok {
		resp.Code = protocol.ResponseCodeError
		resp.Msg = fmt.Sprintf("unknown function: %s", req.Method)
		fmt.Fprintf(os.Stderr, "[xyncra] no handler for function %q\n", req.Method)
	} else {
		data, err := executeMockFunction(handler, req.Params)
		if err != nil {
			resp.Code = protocol.ResponseCodeError
			resp.Msg = err.Error()
			fmt.Fprintf(os.Stderr, "[xyncra] function %q failed: %v\n", req.Method, err)
		} else {
			resp.Code = protocol.ResponseCodeOK
			resp.Msg = "ok"
			resp.Data = data
			fmt.Fprintf(os.Stderr, "[xyncra] function %q executed successfully\n", req.Method)
		}
	}

	// Record idempotency: if the request has an idempotency key, include it in
	// the response for dedup (matching XyncraClient.handleIncomingRequest).

	// Send the response back.
	respData, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[xyncra] marshal response error: %v\n", err)
		return
	}
	respPkg := protocol.Package{
		Type: protocol.PackageTypeResponse,
		Data: json.RawMessage(respData),
	}

	if err := ws.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		fmt.Fprintf(os.Stderr, "[xyncra] set write deadline error: %v\n", err)
		return
	}
	if err := ws.WriteJSON(respPkg); err != nil {
		fmt.Fprintf(os.Stderr, "[xyncra] send response error: %v\n", err)
	}
}

// executeMockFunction executes a mock implementation of a registered function.
// It returns mock data based on the function name, or a generic success
// response for unknown functions.
func executeMockFunction(fn protocol.FunctionInfo, params json.RawMessage) (json.RawMessage, error) {
	// Parse the incoming params for context.
	var paramsMap map[string]any
	_ = json.Unmarshal(params, &paramsMap)

	// Mock implementations based on function name.
	var result any
	switch fn.Name {
	case "get_device_status":
		result = map[string]any{
			"status":      "online",
			"battery":     85,
			"temperature": 22.5,
			"uptime_secs": 3600,
		}
	case "get_time":
		result = map[string]any{
			"time":     time.Now().UTC().Format(time.RFC3339),
			"timezone": "UTC",
		}
	case "echo":
		// Echo back the params as the result.
		result = map[string]any{
			"echo": paramsMap,
		}
	default:
		// Generic mock: return function name and received params.
		result = map[string]any{
			"result":   "ok",
			"function": fn.Name,
			"params":   paramsMap,
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal mock result: %w", err)
	}
	return data, nil
}
