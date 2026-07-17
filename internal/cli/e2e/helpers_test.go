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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientstore "github.com/PineappleBond/xyncra-server/pkg/store"
	clientmodel "github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// TestMain skips the entire E2E package when -short flag is set.
// These tests require a running Redis (port 16379) and xyncra-server (port 18080).
func TestMain(m *testing.M) {
	// Check for short mode. go test -short passes -test.short to the binary.
	short := false
	for _, arg := range os.Args {
		if arg == "-test.short" || arg == "--test.short" ||
			arg == "-test.short=true" || arg == "--test.short=true" ||
			arg == "-short" || arg == "--short" {
			short = true
			break
		}
	}
	if short {
		fmt.Println("SKIP: CLI E2E tests require running Redis + server (use make test-cli-e2e)")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

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

// ---------------------------------------------------------------------------
// Additional helpers for P0 tests
// ---------------------------------------------------------------------------

// perTestEnv holds an isolated per-test environment that uses a fresh HOME
// directory (important for tests that run multiple daemons or alter state
// files). It reuses the compiled binary and server URL from the shared
// cliTestEnv.
type perTestEnv struct {
	*cliTestEnv
	homeDir string // fresh temp HOME directory for this test
}

// newUserDir ensures the user directory exists and returns its path.
func (e perTestEnv) userDir(userID, deviceID string) string {
	return filepath.Join(e.homeDir, ".xyncra", userID, deviceID)
}

// socketPath returns the expected IPC socket path for (user, device).
func (e perTestEnv) socketPathFor(userID, deviceID string) string {
	return filepath.Join(e.userDir(userID, deviceID), "xyncra.sock")
}

// lockPathFor returns the expected lock file path for (user, device).
func (e perTestEnv) lockPathFor(userID, deviceID string) string {
	return filepath.Join(e.userDir(userID, deviceID), "xyncra.lock")
}

// dbPathFor returns the expected local DB path for (user, device).
func (e perTestEnv) dbPathFor(userID, deviceID string) string {
	return filepath.Join(e.userDir(userID, deviceID), "xyncra.db")
}

// newPerTestEnv creates a per-test environment with a fresh HOME directory.
// The fresh HOME prevents cross-test contamination for tests that manipulate
// state files (locks, sockets, DBs). It reuses the compiled binary from the
// shared cliTestEnv.
func newPerTestEnv(t *testing.T, shared *cliTestEnv) perTestEnv {
	t.Helper()
	homeDir, err := os.MkdirTemp("/tmp", "xe2e-pt-")
	require.NoError(t, err, "create per-test temp home")
	t.Cleanup(func() { _ = os.RemoveAll(homeDir) })
	return perTestEnv{cliTestEnv: shared, homeDir: homeDir}
}

// buildEnv builds a clean environment (stripped of XYNCRA_* and with HOME
// overridden) for a perTestEnv.
func (e perTestEnv) buildEnv() []string {
	cleanEnv := make([]string, 0, len(os.Environ()))
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "XYNCRA_") {
			continue
		}
		if strings.HasPrefix(env, "HOME=") {
			continue
		}
		cleanEnv = append(cleanEnv, env)
	}
	cleanEnv = append(cleanEnv, "HOME="+e.homeDir)
	return cleanEnv
}

// runCLIWithHome executes the CLI binary with the per-test HOME directory.
// It mirrors runCLI but uses the isolated home.
func (e perTestEnv) runCLI(t *testing.T, args ...string) CLIResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	cmd.Env = e.buildEnv()

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
			result.ExitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}
	return result
}

// startDaemonWithServer launches `xyncra-client listen` with a custom server
// URL. It is used by tests that want to simulate a broken/unreachable server
// (D-044: daemon keeps IPC available even when WS is unreachable).
func (e perTestEnv) startDaemonWithServer(t *testing.T, userID, deviceID, serverURL string) *daemonProcess {
	t.Helper()
	userDir := e.userDir(userID, deviceID)
	require.NoError(t, os.MkdirAll(userDir, 0700), "create user dir")

	socketPath := e.socketPathFor(userID, deviceID)

	cmd := exec.Command(e.binaryPath, "listen",
		"--user-id", userID,
		"--device-id", deviceID,
		"--server", serverURL,
	)
	cmd.Env = e.buildEnv()

	cmd.Stdout = nil
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	require.NoError(t, cmd.Start(), "start daemon with server %s", serverURL)

	dp := &daemonProcess{
		cmd:        cmd,
		socketPath: socketPath,
		homeDir:    e.homeDir,
		userID:     userID,
		deviceID:   deviceID,
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	if err := waitForSocket(waitCtx, socketPath); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("daemon socket did not appear at %s: %v\ndaemon stderr:\n%s",
			socketPath, err, stderrBuf.String())
	}

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
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	return dp
}

// startDaemonPerTest launches a daemon using the per-test HOME. It uses the
// shared e2eServerURL from the cliTestEnv.
func (e perTestEnv) startDaemon(t *testing.T, userID, deviceID string) *daemonProcess {
	return e.startDaemonWithServer(t, userID, deviceID, e.serverURL)
}

// openLocalDB opens the local SQLite client database at dbPath for
// verification. The caller must call Close on the returned store.
func openLocalDB(t *testing.T, dbPath string) *clientstore.ClientDB {
	t.Helper()
	db, err := clientstore.New(dbPath)
	require.NoError(t, err, "open local client DB at %s", dbPath)
	return db
}

// waitForSync polls the local SQLite DB at dbPath until predicate returns
// true, or timeout expires. It opens and closes the DB on each iteration to
// avoid holding a long-lived connection (which could conflict with the
// running daemon's single-writer WAL mode).
func waitForSync(t testing.TB, dbPath string, timeout time.Duration, predicate func(db *clientstore.ClientDB) bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := clientstore.New(dbPath)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		ok := predicate(db)
		_ = db.Close()
		if ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("waitForSync: predicate not satisfied within %s (last err: %v)", timeout, lastErr)
}

// createStaleLock writes a lock file containing a fake (non-existent) PID,
// simulating a stale lock left behind by a previously killed daemon.
func createStaleLock(t *testing.T, userDir string, fakePID int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(userDir, 0700), "create user dir for stale lock")
	lockPath := filepath.Join(userDir, "xyncra.lock")
	content := fmt.Sprintf(`{"pid":%d,"started_at":"2020-01-01T00:00:00Z","device_id":"dev1"}`, fakePID)
	require.NoError(t, os.WriteFile(lockPath, []byte(content), 0600), "write stale lock file")
}

// ensureDir creates dir if it does not exist.
func ensureDir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0700), "ensure dir %s", dir)
}

// ---------------------------------------------------------------------------
// Additional helpers for P1 tests
// ---------------------------------------------------------------------------

// ipcRawCall sends raw bytes to the IPC socket at socketPath and returns
// the raw response bytes. It is used for testing invalid/non-JSON-RPC requests
// (CLI-E2E-029).
func ipcRawCall(t *testing.T, socketPath string, raw []byte) []byte {
	t.Helper()

	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	require.NoError(t, err, "ipc raw dial should succeed")
	defer conn.Close()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)),
		"set read deadline should succeed")

	// Ensure the raw data ends with a newline (IPC protocol delimiter).
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		raw = append(raw, '\n')
	}

	_, err = conn.Write(raw)
	require.NoError(t, err, "write raw ipc data should succeed")

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	require.True(t, scanner.Scan(), "should read ipc response line")

	return scanner.Bytes()
}

// runCLIWithEnv executes the CLI binary with additional environment variables
// appended on top of the perTestEnv's standard environment. This is used for
// testing XYNCRA_* environment variable support (CLI-E2E-008).
func (e perTestEnv) runCLIWithEnv(t *testing.T, extraEnv []string, args ...string) CLIResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	env := e.buildEnv()
	env = append(env, extraEnv...)
	cmd.Env = env

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
			result.ExitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}
	return result
}

// seedLocalDBFull opens the local SQLite database at dbPath, creates the
// given conversations and messages, then closes the DB. It is used to seed
// test data for query command tests (D-035).
func seedLocalDBFull(t *testing.T, dbPath string, convs []*clientmodel.Conversation, msgs []*clientmodel.Message) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0700), "ensure DB parent dir")
	db, err := clientstore.New(dbPath)
	require.NoError(t, err, "open local client DB for seeding")
	defer db.Close()

	ctx := context.Background()
	for _, c := range convs {
		err := db.Conversations.Create(ctx, c)
		require.NoError(t, err, "seed conversation")
	}
	for _, m := range msgs {
		err := db.Messages.Create(ctx, m)
		require.NoError(t, err, "seed message")
	}
}

// ---------------------------------------------------------------------------
// Shared helpers moved from individual test files
// ---------------------------------------------------------------------------

// uniqueUserID returns a unique user ID by combining the given prefix with a
// random 4-byte hex suffix. This is necessary because the server's SQLite
// database persists across test runs; using only t.Name() could result in
// create_conversation returning Duplicate=true (idempotent hit) which does
// NOT create UserUpdates, breaking sync-based tests.
func uniqueUserID(prefix string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b[:]))
}

// createConversationAndSend creates a 1-on-1 conversation (caller <-> peerID)
// via a temporary daemon and sends a single message. It is used to seed
// server-side data (UserUpdates) for subsequent sync tests.
//
// The peerID is automatically suffixed with a random component to guarantee
// that the server does not already hold a conversation between callerID and
// the peer — see D-011 (find-or-create idempotency).
func createConversationAndSend(t *testing.T, env perTestEnv, callerID, deviceID, peerID string) {
	t.Helper()

	peerID = uniqueUserID(peerID)

	dp := env.startDaemon(t, callerID, deviceID)

	createResult := env.runCLI(t,
		"--user-id", callerID, "--device-id", deviceID,
		"create-conversation", "--peer-id", peerID,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")

	sendResult := env.runCLI(t,
		"--user-id", callerID, "--device-id", deviceID,
		"send", "--conversation-id", convID, "--content", "seed message",
	)
	requireExitCode(t, sendResult, 0)

	requireStopDaemon(t, dp)
}

// containsAny reports whether s contains at least one of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && searchString(s, sub) {
			return true
		}
	}
	return false
}

// searchString performs a simple substring search.
func searchString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// buildCmd creates an exec.Cmd for the xyncra-client binary with the given
// arguments, using the perTestEnv's standard environment.
func (e perTestEnv) buildCmd(args ...string) *exec.Cmd {
	cmd := exec.Command(e.binaryPath, args...)
	cmd.Env = e.buildEnv()
	return cmd
}

// seedRPCLogs opens the local SQLite database at dbPath and inserts n RPC log
// entries with alternating methods. The entries have recent timestamps so they
// fall within the default query windows.
func seedRPCLogs(t *testing.T, dbPath string, n int) {
	t.Helper()
	db, err := clientstore.New(dbPath)
	require.NoError(t, err, "open local client DB for RPC log seeding")
	defer db.Close()

	ctx := context.Background()
	methods := []string{"send_message", "create_conversation"}
	for i := 0; i < n; i++ {
		log := &clientmodel.RPCLog{
			ID:         uuid.New().String(),
			Type:       "response",
			RequestID:  fmt.Sprintf("req-%d", i),
			Method:     methods[i%len(methods)],
			StatusCode: 0,
			Duration:   time.Duration(i+1) * time.Millisecond,
			CreatedAt:  time.Now().Add(-time.Duration(i) * time.Second),
		}
		err := db.RPCLogs.Save(ctx, log)
		require.NoError(t, err, "seed RPC log %d", i)
	}
}

// requireStopDaemon sends SIGTERM to the daemon process, waits for it to
// exit, and allows a brief grace period for file cleanup. It does NOT use
// SIGKILL fallback — if the daemon does not exit gracefully, the test fails.
func requireStopDaemon(t *testing.T, dp *daemonProcess) {
	t.Helper()
	if dp.cmd.Process == nil {
		return
	}
	_ = dp.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_ = dp.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		// Process exited gracefully.
	case <-time.After(5 * time.Second):
		_ = dp.cmd.Process.Kill()
		<-done
		t.Fatal("daemon did not exit within timeout after SIGTERM")
	}
	// Allow a moment for deferred file cleanup in the daemon.
	time.Sleep(500 * time.Millisecond)
}

// convIDPattern matches UUIDs in the standard 8-4-4-4-12 hex format.
var convIDPattern = regexp.MustCompile(`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

// extractConversationID parses the conversation ID from create-conversation
// output. The expected format is:
//
//	Conversation created.
//	  Conversation ID: <uuid>
//	  Peer: <peer-id>
//
// or:
//
//	Conversation already exists (find-or-create).
//	  Conversation ID: <uuid>
//	  Peer: <peer-id>
func extractConversationID(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Conversation ID:") {
			id := strings.TrimSpace(strings.TrimPrefix(line, "Conversation ID:"))
			if id != "" {
				return id
			}
		}
	}
	// Fallback: search for any UUID pattern.
	if match := convIDPattern.FindString(output); match != "" {
		return match
	}
	return ""
}
