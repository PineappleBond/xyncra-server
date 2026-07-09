package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// kill command structure tests
// ---------------------------------------------------------------------------

func TestNewKillCommand_HasFlags(t *testing.T) {
	cmd := newKillCommand()
	flags := []string{"force", "timeout"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestNewKillCommand_Use(t *testing.T) {
	cmd := newKillCommand()
	if cmd.Use != "kill" {
		t.Errorf("Use = %q, want %q", cmd.Use, "kill")
	}
}

// ---------------------------------------------------------------------------
// runKill scenario tests
// ---------------------------------------------------------------------------

// newTestKillCommand creates a cobra.Command with the persistent flags needed
// by NewCLIContext, plus the kill command's own flags merged in.
func newTestKillCommand() *cobra.Command {
	killCmd := newKillCommand()
	// Add persistent flags directly on killCmd so setFlag can merge them.
	killCmd.PersistentFlags().StringP("user-id", "u", "", "User ID")
	killCmd.PersistentFlags().String("device-id", "", "Device ID")
	killCmd.PersistentFlags().StringP("server", "s", "", "Server URL")
	killCmd.PersistentFlags().String("db-path", "", "Database path")
	killCmd.PersistentFlags().String("log-dir", "", "Log directory")
	return killCmd
}

func TestRunKill_NoLockFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XYNCRA_USER_ID", "")
	t.Setenv("XYNCRA_DEVICE_ID", "")
	t.Setenv("XYNCRA_SERVER", "")
	t.Setenv("XYNCRA_DB_PATH", "")
	t.Setenv("XYNCRA_LOG_DIR", "")

	killCmd := newTestKillCommand()
	setFlag(killCmd, "user-id", "testuser")

	output := captureStderr(func() {
		err := killCmd.RunE(killCmd, nil)
		if err == nil {
			t.Fatal("runKill should return error when no daemon is found")
		}
		if !strings.Contains(err.Error(), "no running daemon") {
			t.Errorf("error = %q, want it to contain 'no running daemon'", err.Error())
		}
	})

	if !strings.Contains(output, "No running daemon found.") {
		t.Errorf("output = %q, want it to contain 'No running daemon found.'", output)
	}
}

func TestRunKill_StaleLock(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XYNCRA_USER_ID", "")
	t.Setenv("XYNCRA_DEVICE_ID", "")
	t.Setenv("XYNCRA_SERVER", "")
	t.Setenv("XYNCRA_DB_PATH", "")
	t.Setenv("XYNCRA_LOG_DIR", "")

	killCmd := newTestKillCommand()
	setFlag(killCmd, "user-id", "testuser")

	// We need CLIContext to resolve paths. Build one manually to find the lock path.
	cliCtx, err := NewCLIContext(killCmd)
	if err != nil {
		t.Fatalf("NewCLIContext error: %v", err)
	}
	lockPath := cliCtx.LockPath()

	// Write a stale lock file with a PID that doesn't exist.
	staleInfo := LockInfo{
		PID:       99999999,
		StartedAt: time.Now().Add(-1 * time.Hour),
		DeviceID:  "testdevice",
	}
	data, _ := json.Marshal(staleInfo)
	if err := os.WriteFile(lockPath, data, 0600); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	output := captureStderr(func() {
		err := killCmd.RunE(killCmd, nil)
		if err != nil {
			t.Fatalf("runKill error: %v", err)
		}
	})

	if !strings.Contains(output, "not running") {
		t.Errorf("output = %q, want it to contain 'not running'", output)
	}

	// Lock file should be cleaned up.
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file %q still exists after stale lock cleanup", lockPath)
	}
}

func TestRunKill_MissingUserID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XYNCRA_USER_ID", "")
	t.Setenv("XYNCRA_DEVICE_ID", "")
	t.Setenv("XYNCRA_SERVER", "")

	killCmd := newTestKillCommand()
	// Don't set user-id flag or env var.

	err := killCmd.RunE(killCmd, nil)
	if err == nil {
		t.Fatal("runKill should fail when user-id is missing")
	}
	if !strings.Contains(err.Error(), "user-id is required") {
		t.Errorf("error = %q, want it to contain 'user-id is required'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// terminateProcess tests
// ---------------------------------------------------------------------------

func TestTerminateProcess_Success_DeadPID(t *testing.T) {
	// Use a PID that doesn't exist. isProcessAlive(deadPID) returns false on
	// first poll iteration, so terminateProcess returns nil immediately.
	deadPID := 99999999

	// Mock osFindProcess to return a valid process (Unix always succeeds).
	origFindProcess := osFindProcess
	defer func() { osFindProcess = origFindProcess }()

	osFindProcess = func(pid int) (*os.Process, error) {
		return os.FindProcess(os.Getpid()) // return current process as stand-in
	}

	// Mock osSignalProcess to succeed (no-op).
	origSignalProcess := osSignalProcess
	defer func() { osSignalProcess = origSignalProcess }()
	osSignalProcess = func(p *os.Process, sig os.Signal) error {
		return nil
	}

	err := terminateProcess(deadPID, syscall.SIGTERM, 1*time.Second)
	if err != nil {
		t.Errorf("terminateProcess(deadPID) error: %v", err)
	}
}

func TestTerminateProcess_Timeout_SIGTERM(t *testing.T) {
	// Use current PID (alive). Mock signal to do nothing.
	// isProcessAlive(currentPID) returns true → loop until timeout.
	alivePID := os.Getpid()

	origFindProcess := osFindProcess
	defer func() { osFindProcess = origFindProcess }()
	osFindProcess = func(pid int) (*os.Process, error) {
		return os.FindProcess(pid)
	}

	origSignalProcess := osSignalProcess
	defer func() { osSignalProcess = origSignalProcess }()
	osSignalProcess = func(p *os.Process, sig os.Signal) error {
		return nil // do nothing — process stays alive
	}

	err := terminateProcess(alivePID, syscall.SIGTERM, 500*time.Millisecond)
	if err == nil {
		t.Fatal("terminateProcess should return timeout error when process doesn't exit")
	}
	if !strings.Contains(err.Error(), "did not respond to SIGTERM") {
		t.Errorf("error = %q, want it to contain 'did not respond to SIGTERM'", err.Error())
	}
}

func TestTerminateProcess_SignalError(t *testing.T) {
	origFindProcess := osFindProcess
	defer func() { osFindProcess = origFindProcess }()
	osFindProcess = func(pid int) (*os.Process, error) {
		return os.FindProcess(os.Getpid())
	}

	origSignalProcess := osSignalProcess
	defer func() { osSignalProcess = origSignalProcess }()
	osSignalProcess = func(p *os.Process, sig os.Signal) error {
		return os.ErrPermission
	}

	err := terminateProcess(os.Getpid(), syscall.SIGTERM, 1*time.Second)
	if err == nil {
		t.Fatal("terminateProcess should return error when signal fails")
	}
	if !strings.Contains(err.Error(), "signal process") {
		t.Errorf("error = %q, want it to contain 'signal process'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// cleanupDaemonFiles tests
// ---------------------------------------------------------------------------

func TestCleanupDaemonFiles_RemovesBothFiles(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XYNCRA_USER_ID", "")
	t.Setenv("XYNCRA_DEVICE_ID", "")
	t.Setenv("XYNCRA_SERVER", "")
	t.Setenv("XYNCRA_DB_PATH", "")
	t.Setenv("XYNCRA_LOG_DIR", "")

	killCmd := newTestKillCommand()
	setFlag(killCmd, "user-id", "testuser")

	cliCtx, err := NewCLIContext(killCmd)
	if err != nil {
		t.Fatalf("NewCLIContext error: %v", err)
	}

	// Create the lock and socket files.
	lockPath := cliCtx.LockPath()
	socketPath := cliCtx.SocketPath()

	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("{}"), 0600); err != nil {
		t.Fatalf("write lock file: %v", err)
	}
	if err := os.WriteFile(socketPath, []byte(""), 0600); err != nil {
		t.Fatalf("write socket file: %v", err)
	}

	cleanupDaemonFiles(cliCtx)

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file %q still exists after cleanup", lockPath)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Errorf("socket file %q still exists after cleanup", socketPath)
	}
}

func TestCleanupDaemonFiles_NoFilesNoError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XYNCRA_USER_ID", "")
	t.Setenv("XYNCRA_DEVICE_ID", "")
	t.Setenv("XYNCRA_SERVER", "")
	t.Setenv("XYNCRA_DB_PATH", "")
	t.Setenv("XYNCRA_LOG_DIR", "")

	killCmd := newTestKillCommand()
	setFlag(killCmd, "user-id", "testuser")

	cliCtx, err := NewCLIContext(killCmd)
	if err != nil {
		t.Fatalf("NewCLIContext error: %v", err)
	}

	// Should not panic or return error when files don't exist.
	cleanupDaemonFiles(cliCtx)
}

// ---------------------------------------------------------------------------
// kill command --force flag test
// ---------------------------------------------------------------------------

func TestNewKillCommand_ForceFlag(t *testing.T) {
	cmd := newKillCommand()
	flag := cmd.Flags().Lookup("force")
	if flag == nil {
		t.Fatal("missing --force flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--force default = %q, want %q", flag.DefValue, "false")
	}
}
