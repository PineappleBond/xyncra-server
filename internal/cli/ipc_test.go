package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestIPCTypeSerialization(t *testing.T) {
	t.Run("IPCRequest", func(t *testing.T) {
		req, err := NewIPCRequest("test_method", map[string]string{"key": "value"})
		if err != nil {
			t.Fatalf("NewIPCRequest() error: %v", err)
		}
		if req.JSONRPC != "2.0" {
			t.Errorf("JSONRPC = %q, want %q", req.JSONRPC, "2.0")
		}
		if req.Method != "test_method" {
			t.Errorf("Method = %q, want %q", req.Method, "test_method")
		}
		if req.ID == "" {
			t.Error("ID should not be empty")
		}

		b, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var decoded IPCRequest
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if decoded.Method != req.Method {
			t.Errorf("decoded Method = %q, want %q", decoded.Method, req.Method)
		}
		if decoded.ID != req.ID {
			t.Errorf("decoded ID = %q, want %q", decoded.ID, req.ID)
		}
	})

	t.Run("IPCResponse", func(t *testing.T) {
		resp, err := NewIPCResponse("req-1", map[string]int{"count": 42})
		if err != nil {
			t.Fatalf("NewIPCResponse() error: %v", err)
		}
		if resp.JSONRPC != "2.0" {
			t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, "2.0")
		}
		if resp.ID != "req-1" {
			t.Errorf("ID = %q, want %q", resp.ID, "req-1")
		}
		if resp.Error != nil {
			t.Error("Error should be nil for success response")
		}

		b, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var decoded IPCResponse
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if decoded.ID != "req-1" {
			t.Errorf("decoded ID = %q, want %q", decoded.ID, "req-1")
		}
	})

	t.Run("IPCError", func(t *testing.T) {
		resp := NewIPCErrorResponse("req-2", -32601, "Method not found")
		if resp.JSONRPC != "2.0" {
			t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, "2.0")
		}
		if resp.Error == nil {
			t.Fatal("Error should not be nil")
		}
		if resp.Error.Code != -32601 {
			t.Errorf("Error.Code = %d, want %d", resp.Error.Code, -32601)
		}
		if resp.Error.Message != "Method not found" {
			t.Errorf("Error.Message = %q, want %q", resp.Error.Message, "Method not found")
		}

		errStr := resp.Error.Error()
		if errStr != "ipc error -32601: Method not found" {
			t.Errorf("Error() = %q", errStr)
		}
	})

	t.Run("IPCRequest_NilParams", func(t *testing.T) {
		req, err := NewIPCRequest("no_params", nil)
		if err != nil {
			t.Fatalf("NewIPCRequest() error: %v", err)
		}
		if req.Params != nil {
			t.Errorf("Params should be nil, got %v", req.Params)
		}
	})
}

// startTestIPCServer creates an IPCServer with an echo handler registered.
func startTestIPCServer(t *testing.T) (*IPCServer, string) {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	srv := NewIPCServer(sockPath)

	srv.Register("echo", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		return NewIPCResponse(req.ID, json.RawMessage(req.Params))
	})

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("IPCServer.Start() error: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv, sockPath
}

func TestIPCServerClientRoundTrip(t *testing.T) {
	_, sockPath := startTestIPCServer(t)

	client := NewIPCClient(sockPath, 5*time.Second)
	resp, err := client.Call(context.Background(), "echo", map[string]string{"msg": "hello"})
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("response error: %v", resp.Error)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["msg"] != "hello" {
		t.Errorf("echo result = %v, want {msg: hello}", result)
	}
}

func TestIPCConcurrentClients(t *testing.T) {
	_, sockPath := startTestIPCServer(t)

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := NewIPCClient(sockPath, 5*time.Second)
			resp, err := c.Call(context.Background(), "echo", map[string]int{"i": idx})
			if err != nil {
				errs[idx] = fmt.Errorf("client %d: %w", idx, err)
				return
			}
			if resp.Error != nil {
				errs[idx] = fmt.Errorf("client %d: response error: %v", idx, resp.Error)
			}
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("client %d failed: %v", i, e)
		}
	}
}

func TestIPCSocketPermissions(t *testing.T) {
	_, sockPath := startTestIPCServer(t)

	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("Stat socket: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("socket permissions = %o, want 0600", perm)
	}
}

func TestIPCStaleSocketCleanup(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "stale.sock")

	// Create a stale socket file.
	if err := os.WriteFile(sockPath, []byte("stale"), 0600); err != nil {
		t.Fatalf("create stale socket: %v", err)
	}

	srv := NewIPCServer(sockPath)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("IPCServer.Start() should clean up stale socket, got error: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// Server should be running and functional.
	client := NewIPCClient(sockPath, 5*time.Second)
	srv.Register("ping", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		return NewIPCResponse(req.ID, "pong")
	})
	// Register must be called before Start, but we registered after Start
	// in this test. Let's use a fresh server for a proper test.
	_ = srv.Stop()

	// Proper test: register handler before Start.
	srv2 := NewIPCServer(sockPath)
	srv2.Register("ping", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		return NewIPCResponse(req.ID, "pong")
	})
	// Write stale socket again.
	if err := os.WriteFile(sockPath, []byte("stale"), 0600); err != nil {
		t.Fatalf("create stale socket: %v", err)
	}
	if err := srv2.Start(context.Background()); err != nil {
		t.Fatalf("IPCServer.Start() error: %v", err)
	}
	defer func() { _ = srv2.Stop() }()

	resp, err := client.Call(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("response error: %v", resp.Error)
	}
}

func TestIPCHandlerErrorPropagation(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	srv := NewIPCServer(sockPath)

	srv.Register("fail", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		return nil, fmt.Errorf("something went wrong")
	})

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("IPCServer.Start() error: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	client := NewIPCClient(sockPath, 5*time.Second)
	resp, err := client.Call(context.Background(), "fail", nil)
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, -32000)
	}
	if resp.Error.Message != "something went wrong" {
		t.Errorf("Error.Message = %q, want %q", resp.Error.Message, "something went wrong")
	}
}

func TestIPCInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	srv := NewIPCServer(sockPath)

	srv.Register("echo", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		return NewIPCResponse(req.ID, "ok")
	})

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("IPCServer.Start() error: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// Connect directly and send invalid JSON.
	conn, err := net.DialTimeout("unix", sockPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("this is not json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp IPCResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response for invalid JSON")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("Error.Code = %d, want %d (Parse error)", resp.Error.Code, -32700)
	}
}

func TestIPCUnknownMethod(t *testing.T) {
	_, sockPath := startTestIPCServer(t)

	client := NewIPCClient(sockPath, 5*time.Second)
	resp, err := client.Call(context.Background(), "nonexistent_method", nil)
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("Error.Code = %d, want %d (Method not found)", resp.Error.Code, -32601)
	}
	if resp.Error.Message != "Method not found" {
		t.Errorf("Error.Message = %q, want %q", resp.Error.Message, "Method not found")
	}
}

func TestIPCInvalidJSONRPCVersion(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	srv := NewIPCServer(sockPath)

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("IPCServer.Start() error: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// Connect directly and send a request with wrong jsonrpc version.
	conn, err := net.DialTimeout("unix", sockPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := `{"jsonrpc":"1.0","id":"1","method":"test"}`
	if _, err := conn.Write([]byte(req + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp IPCResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response for invalid JSONRPC version")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("Error.Code = %d, want %d (Invalid Request)", resp.Error.Code, -32600)
	}
}
