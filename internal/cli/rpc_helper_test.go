package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// startMockWSServer starts a mock WebSocket server that invokes handler for
// each incoming request package.
func startMockWSServer(t *testing.T, handler func(t *testing.T, pkg protocol.Package) (protocol.Package, bool)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		var pkg protocol.Package
		if err := conn.ReadJSON(&pkg); err != nil {
			return
		}

		resp, ok := handler(t, pkg)
		if !ok {
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = conn.WriteJSON(resp)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// wsURL converts an httptest.Server URL to a ws:// URL.
func wsURL(ts *httptest.Server) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http")
}

func TestStandaloneRPC_Success(t *testing.T) {
	ts := startMockWSServer(t, func(t *testing.T, pkg protocol.Package) (protocol.Package, bool) {
		var req protocol.PackageDataRequest
		if err := json.Unmarshal(pkg.Data, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return protocol.Package{}, false
		}
		if req.Method != "test_method" {
			t.Errorf("method = %q, want %q", req.Method, "test_method")
		}

		respData, _ := json.Marshal(protocol.PackageDataResponse{
			ID:   req.ID,
			Code: protocol.ResponseCodeOK,
			Msg:  "ok",
			Data: json.RawMessage(`{"result":"hello"}`),
		})
		return protocol.Package{
			Version: 1,
			Type:    protocol.PackageTypeResponse,
			Data:    json.RawMessage(respData),
		}, true
	})

	cliCtx := &CLIContext{
		UserID:    "testuser",
		DeviceID:  "testdevice",
		ServerURL: wsURL(ts),
		UserDir:   t.TempDir(),
	}

	data, err := standaloneRPC(context.Background(), cliCtx, "test_method", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("standaloneRPC() error: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if result["result"] != "hello" {
		t.Errorf("result = %q, want %q", result["result"], "hello")
	}
}

func TestStandaloneRPC_ServerError(t *testing.T) {
	ts := startMockWSServer(t, func(t *testing.T, pkg protocol.Package) (protocol.Package, bool) {
		respData, _ := json.Marshal(protocol.PackageDataResponse{
			ID:   "1",
			Code: protocol.ResponseCodeValidationError,
			Msg:  "invalid param",
		})
		return protocol.Package{
			Version: 1,
			Type:    protocol.PackageTypeResponse,
			Data:    json.RawMessage(respData),
		}, true
	})

	cliCtx := &CLIContext{
		UserID:    "testuser",
		DeviceID:  "testdevice",
		ServerURL: wsURL(ts),
		UserDir:   t.TempDir(),
	}

	_, err := standaloneRPC(context.Background(), cliCtx, "test_method", nil)
	if err == nil {
		t.Fatal("standaloneRPC() should fail on server error")
	}
	var ce *client.ClientError
	if !errorsAs(err, &ce) {
		t.Fatalf("expected *client.ClientError, got %T: %v", err, err)
	}
	if ce.Code != protocol.ResponseCodeValidationError {
		t.Errorf("code = %d, want %d", ce.Code, protocol.ResponseCodeValidationError)
	}
}

func TestStandaloneRPC_ConnectionFailed(t *testing.T) {
	cliCtx := &CLIContext{
		UserID:    "testuser",
		DeviceID:  "testdevice",
		ServerURL: "ws://127.0.0.1:1", // nothing listening
		UserDir:   t.TempDir(),
	}

	_, err := standaloneRPC(context.Background(), cliCtx, "test_method", nil)
	if err == nil {
		t.Fatal("standaloneRPC() should fail when server is unreachable")
	}
}

func TestStandaloneRPC_Timeout(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Read the request but never respond — let the deadline expire.
		var pkg protocol.Package
		_ = conn.ReadJSON(&pkg)
		// Sleep longer than the read deadline (5s).
		time.Sleep(10 * time.Second)
	}))
	defer ts.Close()

	cliCtx := &CLIContext{
		UserID:    "testuser",
		DeviceID:  "testdevice",
		ServerURL: wsURL(ts),
		UserDir:   t.TempDir(),
	}

	_, err := standaloneRPC(context.Background(), cliCtx, "test_method", nil)
	if err == nil {
		t.Fatal("standaloneRPC() should fail on timeout")
	}
	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error = %q, want it to contain timeout", err.Error())
	}
}

func TestStandaloneRPC_RequestFormat(t *testing.T) {
	var capturedReq protocol.PackageDataRequest
	ts := startMockWSServer(t, func(t *testing.T, pkg protocol.Package) (protocol.Package, bool) {
		if pkg.Version != 1 {
			t.Errorf("version = %d, want 1", pkg.Version)
		}
		if pkg.Type != protocol.PackageTypeRequest {
			t.Errorf("type = %d, want %d", pkg.Type, protocol.PackageTypeRequest)
		}
		if err := json.Unmarshal(pkg.Data, &capturedReq); err != nil {
			t.Errorf("unmarshal: %v", err)
			return protocol.Package{}, false
		}

		respData, _ := json.Marshal(protocol.PackageDataResponse{
			ID:   capturedReq.ID,
			Code: protocol.ResponseCodeOK,
			Msg:  "ok",
			Data: json.RawMessage(`{}`),
		})
		return protocol.Package{
			Version: 1,
			Type:    protocol.PackageTypeResponse,
			Data:    json.RawMessage(respData),
		}, true
	})

	cliCtx := &CLIContext{
		UserID:    "testuser",
		DeviceID:  "testdevice",
		ServerURL: wsURL(ts),
		UserDir:   t.TempDir(),
	}

	_, err := standaloneRPC(context.Background(), cliCtx, "my_method", map[string]any{"foo": "bar"})
	if err != nil {
		t.Fatalf("standaloneRPC() error: %v", err)
	}

	if capturedReq.Method != "my_method" {
		t.Errorf("method = %q, want %q", capturedReq.Method, "my_method")
	}
	var params map[string]any
	if err := json.Unmarshal(capturedReq.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["foo"] != "bar" {
		t.Errorf("params[foo] = %v, want bar", params["foo"])
	}
}

func TestStandaloneRPC_NilParams(t *testing.T) {
	var capturedReq protocol.PackageDataRequest
	ts := startMockWSServer(t, func(t *testing.T, pkg protocol.Package) (protocol.Package, bool) {
		_ = json.Unmarshal(pkg.Data, &capturedReq)
		respData, _ := json.Marshal(protocol.PackageDataResponse{
			ID:   "1",
			Code: protocol.ResponseCodeOK,
			Data: json.RawMessage(`{}`),
		})
		return protocol.Package{Version: 1, Type: protocol.PackageTypeResponse, Data: json.RawMessage(respData)}, true
	})

	cliCtx := &CLIContext{
		UserID:    "testuser",
		DeviceID:  "testdevice",
		ServerURL: wsURL(ts),
		UserDir:   t.TempDir(),
	}

	_, err := standaloneRPC(context.Background(), cliCtx, "method", nil)
	if err != nil {
		t.Fatalf("standaloneRPC() error: %v", err)
	}
	// Params should be "null" JSON.
	if string(capturedReq.Params) != "null" {
		t.Errorf("params = %s, want null", string(capturedReq.Params))
	}
}

// errorsAs is a helper for errors.As that avoids the Go 1.25 errors.AsType dependency.
func errorsAs(err error, target any) bool {
	switch t := target.(type) {
	case **client.ClientError:
		return errorAsClientError(err, t)
	default:
		return false
	}
}

func errorAsClientError(err error, target **client.ClientError) bool {
	for err != nil {
		if ce, ok := err.(*client.ClientError); ok {
			*target = ce
			return true
		}
		// Try unwrapping.
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
