package cli

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// sync-updates (IPC-only, D-036)
// ---------------------------------------------------------------------------

func TestSyncUpdatesViaIPC_Success(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"sync_updates": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			return NewIPCResponse(req.ID, map[string]string{"status": "ok"})
		},
	})

	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*1e9) // 5s
	resp, err := ipcClient.Call(context.Background(), "sync_updates", nil)
	if err != nil {
		t.Fatalf("IPC call error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("IPC response error: %v", resp.Error)
	}
}

func TestSyncUpdates_NoDaemon(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	// No IPC server running.
	output := captureStderr(func() {
		ipcClient := NewIPCClient(cliCtx.SocketPath(), 1*1e9) // 1s timeout
		_, err := ipcClient.Call(context.Background(), "sync_updates", nil)
		if err == nil {
			t.Error("expected error when daemon is not running")
		}
	})
	// The runSyncUpdates function would print "daemon not running" to stderr.
	// Since we're testing the IPC client path directly, we just verify the call fails.
	_ = output
}

func TestSyncUpdates_IPCError(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	startIPCServer(t, cliCtx.SocketPath(), map[string]func(ctx context.Context, req *IPCRequest) (*IPCResponse, error){
		"sync_updates": func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
			return NewIPCErrorResponse(req.ID, -300, "sync failed: connection lost"), nil
		},
	})

	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*1e9)
	resp, err := ipcClient.Call(context.Background(), "sync_updates", nil)
	if err != nil {
		t.Fatalf("IPC call error: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected IPC error response")
	}
	if !strings.Contains(resp.Error.Message, "sync failed") {
		t.Errorf("error message = %q, want it to contain 'sync failed'", resp.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// Command structure
// ---------------------------------------------------------------------------

func TestNewSyncUpdatesCommand(t *testing.T) {
	cmd := newSyncUpdatesCommand()
	if cmd.Use != "sync-updates" {
		t.Errorf("Use = %q, want %q", cmd.Use, "sync-updates")
	}
	if cmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestSyncUpdatesDaemonNotRunning_Message(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	// Verify the error message structure when daemon is not running.
	ipcClient := NewIPCClient(cliCtx.SocketPath(), 500*1e6) // 500ms
	_, err := ipcClient.Call(context.Background(), "sync_updates", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Errorf("error = %q, want it to mention connection failure", err.Error())
	}
}

// ---------------------------------------------------------------------------
// runSyncUpdates — end-to-end via cobra
// ---------------------------------------------------------------------------

// TestRunSyncUpdates_NoDaemon_StderrMessages executes the sync-updates command
// through the cobra root command (no IPC server running) and verifies that
// stderr contains both the "daemon not running" error and the "Hint:" message.
func TestRunSyncUpdates_NoDaemon_StderrMessages(t *testing.T) {
	// Redirect HOME to a temp dir so ensureUserDir does not pollute the real
	// home directory.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XYNCRA_USER_ID", "testuser")

	root := NewRootCommand()
	root.SetArgs([]string{"sync-updates"})

	output := captureStderr(func() {
		_ = root.Execute()
	})

	if !strings.Contains(output, "daemon not running") {
		t.Errorf("stderr = %q, want it to contain 'daemon not running'", output)
	}
	if !strings.Contains(output, "Hint:") {
		t.Errorf("stderr = %q, want it to contain 'Hint:'", output)
	}
}
