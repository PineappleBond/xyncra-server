// Package cli_e2e_test contains end-to-end integration tests for the Xyncra
// CLI (xyncra-client). Tests exercise the full stack: compiled binary, Unix
// socket IPC, SQLite local database, and the running WebSocket server.
//
// e2e_test assumes:
//   - A Redis instance is available at localhost:16379
//   - Redis DB 15 is exclusively used for E2E tests (FlushDB is called before each test)
//   - A WebSocket server is running at ws://localhost:18080/ws
//   - Tests MUST NOT run in parallel (shared Redis and Server instances)
package cli_e2e_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// e2eRedisAddr is the Redis address used for all CLI E2E tests (D-043).
	e2eRedisAddr = "localhost:16379"

	// e2eRedisDB is the Redis database index used for E2E tests (D-043).
	e2eRedisDB = 15

	// e2eServerURL is the WebSocket server URL for CLI E2E tests (D-043).
	e2eServerURL = "ws://localhost:18080/ws"

	// e2eServerHTTP is the HTTP server URL for health checks (D-043).
	e2eServerHTTP = "http://localhost:18080"
)

// ---------------------------------------------------------------------------
// cliTestEnv
// ---------------------------------------------------------------------------

// cliTestEnv holds the shared environment for a CLI E2E test.
type cliTestEnv struct {
	serverURL   string
	binaryPath  string
	tempHome    string
	redisClient *redis.Client
}

// ---------------------------------------------------------------------------
// setupCliE2E
// ---------------------------------------------------------------------------

// setupCliE2E initialises the shared environment for a CLI E2E test:
// verifies Redis and Server availability, compiles xyncra-client, and creates
// a Redis client. It registers a t.Cleanup to flush Redis DB 15 and close
// the Redis client.
//
// If Redis or the Server is unreachable the test is skipped (not failed).
func setupCliE2E(t *testing.T) *cliTestEnv {
	t.Helper()

	// 1. Check Redis connectivity.
	redisClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer pingCancel()
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		_ = redisClient.Close()
		t.Skipf("Redis not available at %s (DB %d): %v — skipping CLI E2E test", e2eRedisAddr, e2eRedisDB, err)
	}

	// 2. FlushDB to ensure a clean slate.
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer flushCancel()
	require.NoError(t, redisClient.FlushDB(flushCtx).Err(), "FlushDB should succeed")

	// 3. Check Server health.
	healthURL := e2eServerHTTP + "/health"
	httpClient := &http.Client{Timeout: 2 * time.Second}
	resp, err := httpClient.Get(healthURL)
	if err != nil {
		_ = redisClient.Close()
		t.Skipf("Server not available at %s: %v — skipping CLI E2E test", healthURL, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = redisClient.Close()
		t.Skipf("Server health check returned status %d — skipping CLI E2E test", resp.StatusCode)
	}

	// 4. Compile xyncra-client binary.
	// Use a short temp dir to avoid macOS Unix socket path limit (104 chars).
	// t.TempDir() on macOS produces paths under /var/folders/... which are too long.
	tempHome, err := os.MkdirTemp("/tmp", "xe2e-")
	require.NoError(t, err, "create temp home dir")
	binaryPath := filepath.Join(tempHome, "xyncra-client")
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer buildCancel()
	buildCmd := exec.CommandContext(buildCtx, "go", "build", "-o", binaryPath, "./cmd/xyncra-client/")
	buildCmd.Dir = projectRoot()
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build xyncra-client failed: %s", string(buildOut))

	// 5. Cleanup.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = redisClient.FlushDB(cleanupCtx).Err()
		_ = redisClient.Close()
		_ = os.RemoveAll(tempHome)
	})

	return &cliTestEnv{
		serverURL:   e2eServerURL,
		binaryPath:  binaryPath,
		tempHome:    tempHome,
		redisClient: redisClient,
	}
}

// projectRoot returns the absolute path to the project root directory.
// It walks up from the working directory looking for go.mod.
func projectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("getwd: %v", err))
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("projectRoot: could not find go.mod")
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// CLIResult and runCLI
// ---------------------------------------------------------------------------

// CLIResult holds the outcome of a CLI command execution.
type CLIResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// runCLI executes the xyncra-client binary with the given arguments and
// returns the exit code, stdout, and stderr. It sets HOME to env.tempHome
// and clears any XYNCRA_ environment variables to ensure isolation.
//
// The command has a 30-second timeout. If the command is killed due to
// context deadline, ExitCode is set to -1.
func runCLI(t *testing.T, env *cliTestEnv, args ...string) CLIResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, env.binaryPath, args...)

	// Build a clean environment: keep PATH and HOME, set HOME to tempHome,
	// and strip all XYNCRA_ variables.
	cleanEnv := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "XYNCRA_") {
			continue
		}
		// Replace HOME.
		if strings.HasPrefix(e, "HOME=") {
			continue
		}
		cleanEnv = append(cleanEnv, e)
	}
	cleanEnv = append(cleanEnv, "HOME="+env.tempHome)
	cmd.Env = cleanEnv

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()

	result := CLIResult{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}

	if err != nil {
		if ctx.Err() != nil {
			// Context deadline exceeded.
			result.ExitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result
}

// ---------------------------------------------------------------------------
// daemonProcess and startDaemon
// ---------------------------------------------------------------------------

// daemonProcess represents a running xyncra-client daemon.
type daemonProcess struct {
	cmd        *exec.Cmd
	socketPath string
	homeDir    string
	userID     string
	deviceID   string
}

// startDaemon launches `xyncra-client listen` as a background process and
// waits for the Unix socket to become available. It registers a t.Cleanup
// that sends SIGTERM, waits up to 5 seconds, and falls back to SIGKILL.
func startDaemon(t *testing.T, env *cliTestEnv, userID, deviceID string) *daemonProcess {
	t.Helper()

	// Ensure the user directory exists.
	userDir := filepath.Join(env.tempHome, ".xyncra", userID, deviceID)
	require.NoError(t, os.MkdirAll(userDir, 0700), "create user dir")

	socketPath := filepath.Join(userDir, "xyncra.sock")

	cmd := exec.Command(env.binaryPath, "listen",
		"--user-id", userID,
		"--device-id", deviceID,
		"--server", e2eServerURL,
	)

	// Set environment.
	cleanEnv := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "XYNCRA_") {
			continue
		}
		if strings.HasPrefix(e, "HOME=") {
			continue
		}
		cleanEnv = append(cleanEnv, e)
	}
	cleanEnv = append(cleanEnv, "HOME="+env.tempHome)
	cmd.Env = cleanEnv

	// Discard stdout to avoid blocking on pipe buffers; capture stderr for debugging.
	cmd.Stdout = nil
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	require.NoError(t, cmd.Start(), "start daemon")

	dp := &daemonProcess{
		cmd:        cmd,
		socketPath: socketPath,
		homeDir:    env.tempHome,
		userID:     userID,
		deviceID:   deviceID,
	}

	// Wait for the socket to appear.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	if err := waitForSocket(waitCtx, socketPath); err != nil {
		// Kill the daemon if socket didn't appear.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("daemon socket did not appear at %s: %v\ndaemon stderr:\n%s", socketPath, err, stderrBuf.String())
	}

	// Cleanup: graceful shutdown with SIGTERM, then SIGKILL after 5s.
	t.Cleanup(func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)

		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Process exited.
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	return dp
}

// ---------------------------------------------------------------------------
// waitForSocket
// ---------------------------------------------------------------------------

// waitForSocket polls the Unix socket at the given path until it is
// connectable or the context is cancelled. It tries every 200ms.
func waitForSocket(ctx context.Context, path string) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waitForSocket: context cancelled before socket %s became available: %w", path, ctx.Err())
		default:
		}

		conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// IPC types and ipcCall
// ---------------------------------------------------------------------------

// ipcRequest is a JSON-RPC 2.0 request for IPC.
type ipcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// ipcResponse is a JSON-RPC 2.0 response from IPC.
type ipcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ipcError       `json:"error,omitempty"`
}

// ipcError represents a JSON-RPC 2.0 error object.
type ipcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ipcCall sends a JSON-RPC 2.0 request to the IPC server at the given socket
// path and returns the parsed response. The connection has a 5-second timeout.
// A newline delimiter is appended to the request as per D-030.
func ipcCall(t *testing.T, socketPath, method string, params interface{}) *ipcResponse {
	t.Helper()

	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	require.NoError(t, err, "ipc dial should succeed")
	defer conn.Close()

	// Set read deadline.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)),
		"set read deadline should succeed")

	reqID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	req := ipcRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	reqData, err := json.Marshal(req)
	require.NoError(t, err, "marshal ipc request should succeed")
	reqData = append(reqData, '\n')

	_, err = conn.Write(reqData)
	require.NoError(t, err, "write ipc request should succeed")

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	require.True(t, scanner.Scan(), "should read ipc response line")

	var resp ipcResponse
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &resp),
		"unmarshal ipc response should succeed")

	return &resp
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

// assertFileExists asserts that a file exists at the given path.
func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "file should exist: %s", path)
}

// assertFileNotExists asserts that no file exists at the given path.
func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "file should not exist: %s", path)
}

// requireExitCode asserts that the CLI result has the expected exit code.
// On failure it prints stdout and stderr for debugging.
func requireExitCode(t *testing.T, result CLIResult, expected int) {
	t.Helper()
	if result.ExitCode != expected {
		t.Fatalf("expected exit code %d, got %d\nstdout:\n%s\nstderr:\n%s",
			expected, result.ExitCode, result.Stdout, result.Stderr)
	}
}
