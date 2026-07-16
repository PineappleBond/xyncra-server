package cli

import (
	"context"
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// reload-agents (IPC-only, D-036/D-076)
// ---------------------------------------------------------------------------

// TestReloadAgents_Success verifies that reload-agents succeeds when the IPC
// server is running and returns the agent count.
func TestReloadAgents_Success(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"reload_agents": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			result := map[string]int{"count": 3}
			return NewIPCResponse(req.ID, result)
		},
	})

	// Use the IPC client directly to test the round-trip.
	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*1e9) // 5s
	resp, err := ipcClient.Call(context.Background(), "reload_agents", nil)
	if err != nil {
		t.Fatalf("reload_agents IPC call failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("reload_agents returned error: %s", resp.Error.Message)
	}

	var result map[string]int
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["count"] != 3 {
		t.Errorf("expected count=3, got %d", result["count"])
	}
}

// TestReloadAgents_DaemonNotRunning verifies that reload-agents fails when
// no daemon is running (IPC connection refused).
func TestReloadAgents_DaemonNotRunning(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	// Don't start an IPC server — simulates daemon not running.

	ipcClient := NewIPCClient(cliCtx.SocketPath(), 1*1e9) // 1s timeout
	_, err := ipcClient.Call(context.Background(), "reload_agents", nil)
	if err == nil {
		t.Fatal("expected error when daemon is not running, got nil")
	}
}
