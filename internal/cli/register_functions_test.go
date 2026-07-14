package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

func TestBuildRegisterURL(t *testing.T) {
	cliCtx := &CLIContext{
		UserID:    "alice",
		DeviceID:  "dev123",
		ServerURL: "ws://localhost:18080/ws",
	}

	got, err := buildRegisterURL(cliCtx)
	require.NoError(t, err)
	assert.Contains(t, got, "user_id=alice")
	assert.Contains(t, got, "device_id=dev123")
	assert.Contains(t, got, "ws://localhost:18080/ws")
}

func TestBuildRegisterURL_SpecialChars(t *testing.T) {
	cliCtx := &CLIContext{
		UserID:    "alice bob",
		DeviceID:  "dev+1",
		ServerURL: "ws://example.com/ws",
	}

	got, err := buildRegisterURL(cliCtx)
	require.NoError(t, err)
	// URL-encoded values.
	assert.Contains(t, got, "user_id=alice+bob")
	assert.Contains(t, got, "device_id=dev%2B1")
}

func TestExecuteMockFunction_GetDeviceStatus(t *testing.T) {
	fn := protocol.FunctionInfo{Name: "get_device_status"}
	data, err := executeMockFunction(fn, json.RawMessage(`{}`))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, "online", result["status"])
	assert.Equal(t, float64(85), result["battery"])
	assert.Equal(t, 22.5, result["temperature"])
}

func TestExecuteMockFunction_GetTime(t *testing.T) {
	fn := protocol.FunctionInfo{Name: "get_time"}
	data, err := executeMockFunction(fn, json.RawMessage(`{}`))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, "UTC", result["timezone"])
	assert.NotEmpty(t, result["time"])
}

func TestExecuteMockFunction_Echo(t *testing.T) {
	fn := protocol.FunctionInfo{Name: "echo"}
	params := json.RawMessage(`{"key":"value","num":42}`)
	data, err := executeMockFunction(fn, params)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	echoMap, ok := result["echo"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", echoMap["key"])
	assert.Equal(t, float64(42), echoMap["num"])
}

func TestExecuteMockFunction_Generic(t *testing.T) {
	fn := protocol.FunctionInfo{Name: "custom_func"}
	params := json.RawMessage(`{"foo":"bar"}`)
	data, err := executeMockFunction(fn, params)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, "ok", result["result"])
	assert.Equal(t, "custom_func", result["function"])
}

func TestHandleReverseRPC_UnknownFunction(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	var respRecv protocol.PackageDataResponse
	var respRecvMu sync.Mutex
	done := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Read the response sent back by handleReverseRPC.
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var pkg protocol.Package
		if err := conn.ReadJSON(&pkg); err != nil {
			return
		}
		respRecvMu.Lock()
		_ = json.Unmarshal(pkg.Data, &respRecv)
		respRecvMu.Unlock()
		close(done)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	funcMap := map[string]protocol.FunctionInfo{
		"known_func": {Name: "known_func"},
	}

	// Build a request for an unknown function.
	reqData, _ := json.Marshal(protocol.PackageDataRequest{
		ID:     "req-unknown",
		Method: "unknown_func",
		Params: json.RawMessage(`{}`),
	})
	pkg := &protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: json.RawMessage(reqData),
	}

	handleReverseRPC(ws, pkg, funcMap)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive response")
	}

	respRecvMu.Lock()
	defer respRecvMu.Unlock()
	assert.Equal(t, "req-unknown", respRecv.ID)
	assert.Equal(t, protocol.ResponseCodeError, respRecv.Code)
	assert.Contains(t, respRecv.Msg, "unknown function")
}

func TestHandleReverseRPC_KnownFunction(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	var respRecv protocol.PackageDataResponse
	var respRecvMu sync.Mutex
	done := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var pkg protocol.Package
		if err := conn.ReadJSON(&pkg); err != nil {
			return
		}
		respRecvMu.Lock()
		_ = json.Unmarshal(pkg.Data, &respRecv)
		respRecvMu.Unlock()
		close(done)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	funcMap := map[string]protocol.FunctionInfo{
		"get_device_status": {Name: "get_device_status"},
	}

	reqData, _ := json.Marshal(protocol.PackageDataRequest{
		ID:     "req-known",
		Method: "get_device_status",
		Params: json.RawMessage(`{}`),
	})
	pkg := &protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: json.RawMessage(reqData),
	}

	handleReverseRPC(ws, pkg, funcMap)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive response")
	}

	respRecvMu.Lock()
	defer respRecvMu.Unlock()
	assert.Equal(t, "req-known", respRecv.ID)
	assert.Equal(t, protocol.ResponseCodeOK, respRecv.Code)
	assert.Equal(t, "ok", respRecv.Msg)

	var result map[string]any
	require.NoError(t, json.Unmarshal(respRecv.Data, &result))
	assert.Equal(t, "online", result["status"])
}

// TestRegisterFunctionsCommand_Integration tests the full flow:
// register-functions connects to a mock server, sends system.register_functions,
// receives a response, then handles a ReverseRPC request from the server.
func TestRegisterFunctionsCommand_Integration(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	var mu sync.Mutex
	var receivedMethod string
	var receivedParams map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read the register_functions request.
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var pkg protocol.Package
		if err := conn.ReadJSON(&pkg); err != nil {
			return
		}

		var req protocol.PackageDataRequest
		_ = json.Unmarshal(pkg.Data, &req)

		mu.Lock()
		receivedMethod = req.Method
		_ = json.Unmarshal(req.Params, &receivedParams)
		mu.Unlock()

		// Send success response.
		respData, _ := json.Marshal(map[string]any{
			"status":    "ok",
			"count":     1,
			"device_id": r.URL.Query().Get("device_id"),
		})
		resp := protocol.PackageDataResponse{
			ID:   req.ID,
			Code: protocol.ResponseCodeOK,
			Msg:  "ok",
			Data: respData,
		}
		respJSON, _ := json.Marshal(resp)
		respPkg := protocol.Package{Type: protocol.PackageTypeResponse, Data: respJSON}
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = conn.WriteJSON(respPkg)

		// Now send a ReverseRPC request to the client.
		time.Sleep(100 * time.Millisecond)
		reverseReqData, _ := json.Marshal(protocol.PackageDataRequest{
			ID:     "s-reverse-1",
			Method: "get_device_status",
			Params: json.RawMessage(`{}`),
			Seq:    1,
		})
		reversePkg := protocol.Package{
			Type: protocol.PackageTypeRequest,
			Data: reverseReqData,
		}
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = conn.WriteJSON(reversePkg)

		// Read the client's response.
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var clientRespPkg protocol.Package
		if err := conn.ReadJSON(&clientRespPkg); err != nil {
			return
		}
		var clientResp protocol.PackageDataResponse
		_ = json.Unmarshal(clientRespPkg.Data, &clientResp)

		// Verify client responded correctly.
		assert.Equal(t, "s-reverse-1", clientResp.ID)
		assert.Equal(t, protocol.ResponseCodeOK, clientResp.Code)

		// Close connection.
		conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
			time.Now().Add(2*time.Second))
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	functions := []protocol.FunctionInfo{
		{Name: "get_device_status", Description: "Get device status"},
	}

	// Run the register functions flow (extracted for testability).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()

	cliCtx := &CLIContext{
		UserID:    "alice",
		DeviceID:  "test-dev",
		ServerURL: wsURL,
	}
	fullURL, err := buildRegisterURL(cliCtx)
	require.NoError(t, err)

	ws, _, err := websocket.DefaultDialer.DialContext(dialCtx, fullURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Send register_functions.
	err = sendRegisterFunctions(ws, cliCtx.DeviceID, "xyncra-cli", "cli", functions)
	require.NoError(t, err)

	// Read response.
	resp, err := readResponse(ws)
	require.NoError(t, err)
	assert.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Verify server received correct params.
	mu.Lock()
	assert.Equal(t, "system.register_functions", receivedMethod)
	assert.Equal(t, "test-dev", receivedParams["device_id"])
	assert.Equal(t, "xyncra-cli", receivedParams["device_name"])
	assert.Equal(t, "cli", receivedParams["device_type"])
	mu.Unlock()

	// Enter message loop to handle ReverseRPC.
	funcMap := make(map[string]protocol.FunctionInfo, len(functions))
	for _, f := range functions {
		funcMap[f.Name] = f
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			var pkg protocol.Package
			if err := ws.ReadJSON(&pkg); err != nil {
				return
			}
			if pkg.Type == protocol.PackageTypeRequest {
				handleReverseRPC(ws, &pkg, funcMap)
			}
		}
	}()

	select {
	case <-readDone:
		// Connection closed by server (expected).
	case <-ctx.Done():
		t.Fatal("test timed out waiting for ReverseRPC flow to complete")
	}
}

func TestRegisterFunctionsCommand_FunctionsFile(t *testing.T) {
	// Create a temp JSON file.
	tmpFile, err := os.CreateTemp("", "functions-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	functions := []protocol.FunctionInfo{
		{Name: "test_func", Description: "A test function"},
	}
	data, _ := json.Marshal(functions)
	_, err = tmpFile.Write(data)
	require.NoError(t, err)
	tmpFile.Close()

	// Read it back using the same logic as the command.
	readData, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)

	var parsed []protocol.FunctionInfo
	require.NoError(t, json.Unmarshal(readData, &parsed))
	assert.Len(t, parsed, 1)
	assert.Equal(t, "test_func", parsed[0].Name)
	assert.Equal(t, "A test function", parsed[0].Description)
}
