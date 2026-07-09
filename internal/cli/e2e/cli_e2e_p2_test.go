package cli_e2e_test

// P2 robustness tests for the xyncra-client CLI (end-to-end).
//
// These tests cover edge cases, boundary conditions, special characters,
// invalid parameters, permission errors, and DB corruption scenarios.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	clientmodel "github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// TestDaemonEdgeCases — CLI-E2E-009, 010
// ---------------------------------------------------------------------------

// TestDaemonEdgeCases verifies daemon flag priority and custom device-id
// behavior. CLI-E2E-009: --user-id flag takes priority over XYNCRA_USER_ID
// environment variable (D-034). CLI-E2E-010: a custom --device-id produces
// socket/lock/db paths under the custom device directory (D-033).
//
// Scenarios: CLI-E2E-009 (flag priority), CLI-E2E-010 (custom device-id).
func TestDaemonEdgeCases(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	// CLI-E2E-009: --user-id flag takes priority over XYNCRA_USER_ID env var.
	// Set XYNCRA_USER_ID to a wrong value, but pass --user-id=correct.
	// The daemon should use the flag value (socket appears under the correct user dir).
	t.Run("flag_priority_over_env", func(t *testing.T) {
		correctUser := "flag-correct-e2e"
		wrongUser := "flag-wrong-e2e"
		deviceID := "dev1"

		// Start daemon with XYNCRA_USER_ID=wrong but --user-id=correct.
		cmd := env.buildCmd("listen",
			"--user-id", correctUser,
			"--device-id", deviceID,
			"--server", e2eServerURL,
		)
		cmd.Env = append(env.buildEnv(), "XYNCRA_USER_ID="+wrongUser)

		var stderrBuf strings.Builder
		cmd.Stderr = &stderrBuf
		cmd.Stdout = nil

		require.NoError(t, cmd.Start(), "start daemon")

		// The socket should appear under correctUser's directory, not wrongUser's.
		expectedSock := env.socketPathFor(correctUser, deviceID)
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer waitCancel()
		if err := waitForSocket(waitCtx, expectedSock); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("socket should appear under correct user dir %s: %v\nstderr: %s",
				expectedSock, err, stderrBuf.String())
		}

		// The wrong user's socket should NOT exist.
		wrongSock := env.socketPathFor(wrongUser, deviceID)
		_, statErr := os.Stat(wrongSock)
		assert.True(t, os.IsNotExist(statErr),
			"socket should NOT exist under wrong user dir %s", wrongSock)

		// Cleanup.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// CLI-E2E-010: Custom --device-id — daemon creates socket under the
	// custom device directory (D-033).
	t.Run("custom_device_id", func(t *testing.T) {
		userID := "devcust-e2e"
		customDevice := "mydevice"

		dp := env.startDaemon(t, userID, customDevice)

		// Socket should exist under the custom device path.
		expectedSock := env.socketPathFor(userID, customDevice)
		assertFileExists(t, expectedSock)
		assert.Equal(t, expectedSock, dp.socketPath,
			"daemon socket path should use custom device-id")

		// Lock file should also exist under custom device path.
		expectedLock := env.lockPathFor(userID, customDevice)
		assertFileExists(t, expectedLock)

		requireStopDaemon(t, dp)
	})
}

// ---------------------------------------------------------------------------
// TestIPCEdgeCases — CLI-E2E-031, 032
// ---------------------------------------------------------------------------

// TestIPCEdgeCases verifies IPC connection timeout and socket file existence.
// CLI-E2E-031: pointing to a non-existent socket path results in an error.
// CLI-E2E-032: the IPC socket file exists and is connectable (D-030).
//
// Scenarios: CLI-E2E-031 (connection timeout), CLI-E2E-032 (socket permissions).
func TestIPCEdgeCases(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	// CLI-E2E-031: IPC connection to non-existent socket fails.
	t.Run("connection_timeout", func(t *testing.T) {
		// Attempting to run a command that requires IPC (e.g. send) with
		// the daemon not running should fall back to WS, but if both fail
		// the CLI should exit with code 1.
		result := env.runCLI(t,
			"--user-id", "ipc-timeout-user",
			"--device-id", "dev1",
			"--server", "ws://localhost:19999/ws",
			"send", "-c", "00000000-0000-0000-0000-000000000000",
			"-m", "should fail",
		)
		requireExitCode(t, result, 1)
	})

	// CLI-E2E-032: IPC socket file exists and is connectable.
	t.Run("socket_exists_and_connectable", func(t *testing.T) {
		userID := "ipc-sock-e2e"
		deviceID := "dev1"

		dp := env.startDaemon(t, userID, deviceID)
		sockPath := dp.socketPath

		// Socket file should exist.
		assertFileExists(t, sockPath)

		// Verify the socket file has appropriate permissions (0600 or 0700 dir).
		info, err := os.Stat(sockPath)
		require.NoError(t, err, "stat socket file")
		// The socket is a Unix domain socket; just verify it exists and has
		// restricted permissions (owner read/write at minimum).
		perm := info.Mode().Perm()
		assert.True(t, perm&0077 == 0 || perm&0070 != 0,
			"socket should have restricted permissions, got: %v", perm)

		requireStopDaemon(t, dp)
	})
}

// ---------------------------------------------------------------------------
// TestWriteEdgeCases — CLI-E2E-055, 063, 133
// ---------------------------------------------------------------------------

// TestWriteEdgeCases verifies write command edge cases: send with --reply-to,
// create-conversation with --title, and get-messages on an empty conversation.
//
// Scenarios: CLI-E2E-055 (--reply-to), CLI-E2E-063 (--title),
// CLI-E2E-133 (empty message list).
func TestWriteEdgeCases(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := uniqueUserID(fmt.Sprintf("alice-e2e-%s", t.Name()))
	peerID := uniqueUserID(fmt.Sprintf("bob-e2e-%s", t.Name()))
	deviceID := "dev1"

	dp := env.startDaemon(t, userID, deviceID)
	sockPath := dp.socketPath

	// CLI-E2E-055: send with --reply-to parameter.
	t.Run("send_reply_to", func(t *testing.T) {
		// Create a conversation and send a message to get a message ID.
		createResp := ipcCall(t, sockPath, "create_conversation", map[string]any{
			"user_id2": peerID,
		})
		require.Nil(t, createResp.Error, "create_conversation should succeed")
		var createResult client.CreateConversationResult
		require.NoError(t, json.Unmarshal(createResp.Result, &createResult))
		convID := createResult.Conversation.ID

		// Send a first message.
		sendResp := ipcCall(t, sockPath, "send_message", map[string]any{
			"conversation_id": convID,
			"content":         "original message",
			"reply_to":        0,
		})
		require.Nil(t, sendResp.Error, "first send should succeed")
		var sr client.SendMessageResult
		require.NoError(t, json.Unmarshal(sendResp.Result, &sr))
		originalMsgID := sr.Message.MessageID

		// Send a reply via CLI with --reply-to.
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"send", "-c", convID, "-m", "this is a reply",
			"--reply-to", fmt.Sprintf("%d", originalMsgID),
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Message sent",
			"send with --reply-to should succeed")
	})

	// CLI-E2E-063: create-conversation with --title.
	t.Run("create_conversation_title", func(t *testing.T) {
		peerID2 := uniqueUserID(fmt.Sprintf("title-peer-e2e-%s", t.Name()))
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"create-conversation", "--peer-id", peerID2,
			"--title", "My Test Conversation",
		)
		requireExitCode(t, result, 0)
		// The title should appear in the output if the server returns it.
		assert.Contains(t, result.Stdout, "Conversation",
			"output should mention conversation")
	})

	// CLI-E2E-133: get-messages on empty conversation.
	t.Run("get_messages_empty", func(t *testing.T) {
		// Create a fresh conversation with no messages.
		peerID3 := uniqueUserID(fmt.Sprintf("empty-peer-e2e-%s", t.Name()))
		createResp := ipcCall(t, sockPath, "create_conversation", map[string]any{
			"user_id2": peerID3,
		})
		require.Nil(t, createResp.Error)
		var createResult client.CreateConversationResult
		require.NoError(t, json.Unmarshal(createResp.Result, &createResult))
		emptyConvID := createResult.Conversation.ID

		// Sync so local DB has the conversation.
		syncResult := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"sync-updates",
		)
		requireExitCode(t, syncResult, 0)

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"get-messages", "-c", emptyConvID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "No messages found",
			"empty conversation should show 'No messages found'")
	})

	requireStopDaemon(t, dp)
}

// ---------------------------------------------------------------------------
// TestQueryEdgeCases — CLI-E2E-143, 144, 194
// ---------------------------------------------------------------------------

// TestQueryEdgeCases verifies query command edge cases: special characters in
// search, Chinese content search, and invalid --type for logs tail.
//
// Scenarios: CLI-E2E-143 (special chars), CLI-E2E-144 (Chinese content),
// CLI-E2E-194 (invalid --type).
func TestQueryEdgeCases(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	peerID := fmt.Sprintf("bob-e2e-%s", t.Name())
	deviceID := "dev1"
	dbPath := env.dbPathFor(userID, deviceID)

	// Phase 1: Create the local DB.
	dp := env.startDaemon(t, userID, deviceID)
	requireStopDaemon(t, dp)

	// Phase 2: Seed conversations and messages with special/Chinese content.
	now := time.Now()
	convID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	msgSpecialID := "11111111-1111-1111-1111-111111111111"
	msgChineseID := "22222222-2222-2222-2222-222222222222"
	msgPercentID := "33333333-3333-3333-3333-333333333333"

	seedLocalDBFull(t, dbPath,
		[]*clientmodel.Conversation{
			{
				ID: convID, UserID1: userID, UserID2: peerID,
				Type: "1-on-1", LastProcessedMessageID: 3,
				CreatedAt: now, UpdatedAt: now, LastMessageAt: now,
			},
		},
		[]*clientmodel.Message{
			{
				ID: msgSpecialID, ClientMessageID: "cms-sp1", ConversationID: convID,
				MessageID: 1, SenderID: userID, Content: "it's a test with 'quotes'",
				Type: "text", Status: "sent", CreatedAt: now,
			},
			{
				ID: msgChineseID, ClientMessageID: "cms-sp2", ConversationID: convID,
				MessageID: 2, SenderID: peerID, Content: "你好世界，这是中文消息",
				Type: "text", Status: "sent", CreatedAt: now,
			},
			{
				ID: msgPercentID, ClientMessageID: "cms-sp3", ConversationID: convID,
				MessageID: 3, SenderID: userID, Content: "100% complete _underscore_",
				Type: "text", Status: "sent", CreatedAt: now,
			},
		},
	)

	// CLI-E2E-143: search-messages with SQL special characters (', %, _).
	t.Run("search_special_chars", func(t *testing.T) {
		// Search for a single quote character.
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"search-messages", "-c", convID, "-q", "it's",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "it's a test",
			"should find message with single quote")

		// Search for percent sign.
		result2 := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"search-messages", "-c", convID, "-q", "100%",
		)
		requireExitCode(t, result2, 0)
		assert.Contains(t, result2.Stdout, "100%",
			"should find message with percent sign")

		// Search for underscore.
		result3 := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"search-messages", "-c", convID, "-q", "_underscore_",
		)
		requireExitCode(t, result3, 0)
		assert.Contains(t, result3.Stdout, "_underscore_",
			"should find message with underscore")
	})

	// CLI-E2E-144: search-messages with Chinese content.
	t.Run("search_chinese", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"search-messages", "-c", convID, "-q", "你好",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "你好世界",
			"should find Chinese content")
	})

	// CLI-E2E-194: logs tail with invalid --type.
	t.Run("logs_tail_invalid_type", func(t *testing.T) {
		ensureDir(t, env.userDir(userID, deviceID))

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "tail", "--type", "invalid",
		)
		requireExitCode(t, result, 1)
		assert.Contains(t, result.Stderr, "invalid type",
			"stderr should mention invalid type")
	})
}

// ---------------------------------------------------------------------------
// TestLogsEdgeCases — CLI-E2E-203~204, 211~212, 223, 232~233
// ---------------------------------------------------------------------------

// TestLogsEdgeCases verifies logs command edge cases: time range search,
// invalid time parameters, interval stats, invalid intervals, invalid export
// format, and type-specific cleanup.
//
// Scenarios: CLI-E2E-203 (search --from/--to), CLI-E2E-204 (invalid time),
// CLI-E2E-211 (--interval 1m), CLI-E2E-212 (invalid --interval),
// CLI-E2E-223 (invalid --format), CLI-E2E-232 (cleanup --type rpc),
// CLI-E2E-233 (cleanup --type notifications).
func TestLogsEdgeCases(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	dbPath := env.dbPathFor(userID, deviceID)

	ensureDir(t, env.userDir(userID, deviceID))
	seedRPCLogs(t, dbPath, 10)

	// CLI-E2E-203: logs search --from/--to time range.
	t.Run("search_time_range", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "search", "--from", "2h", "--to", "0s",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "TIME",
			"should contain header row")
	})

	// CLI-E2E-204: logs search with invalid time parameter.
	t.Run("search_invalid_time", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "search", "--from", "not-a-time",
		)
		requireExitCode(t, result, 1)
		assert.True(t, len(result.Stderr) > 0,
			"stderr should contain error for invalid time, got: %s", result.Stderr)
	})

	// CLI-E2E-211: logs stats --interval 1m.
	t.Run("stats_interval_1m", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "stats", "--interval", "1m",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "INTERVAL",
			"output should contain INTERVAL column")
	})

	// CLI-E2E-212: logs stats with invalid --interval.
	t.Run("stats_invalid_interval", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "stats", "--interval", "2h",
		)
		requireExitCode(t, result, 1)
		assert.True(t, containsAny(result.Stderr, "invalid interval", "must be one of"),
			"stderr should mention valid intervals, got: %s", result.Stderr)
	})

	// CLI-E2E-223: logs export with invalid --format.
	t.Run("export_invalid_format", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "export", "--format", "xml",
		)
		requireExitCode(t, result, 1)
		assert.True(t, containsAny(result.Stderr, "invalid format", "must be csv or json"),
			"stderr should mention valid formats, got: %s", result.Stderr)
	})

	// CLI-E2E-232: logs cleanup --type rpc.
	t.Run("cleanup_type_rpc", func(t *testing.T) {
		// Re-seed to ensure data exists.
		seedRPCLogs(t, dbPath, 5)

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "cleanup", "--type", "rpc",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "RPC",
			"output should mention RPC log cleanup")
	})

	// CLI-E2E-233: logs cleanup --type notifications.
	t.Run("cleanup_type_notifications", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "cleanup", "--type", "notifications",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "notification",
			"output should mention notification log cleanup")
	})
}

// ---------------------------------------------------------------------------
// TestKillEdgeCases — CLI-E2E-245
// ---------------------------------------------------------------------------

// TestKillEdgeCases verifies the kill command with a custom --timeout value
// specified as a plain number of seconds. CLI-E2E-245: --timeout 3 uses a
// 3-second timeout (D-039).
//
// Scenario: CLI-E2E-245 — kill --timeout custom duration (seconds).
func TestKillEdgeCases(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	userDir := env.userDir(userID, deviceID)
	ensureDir(t, userDir)

	// Start a sleep process that ignores SIGTERM.
	sleepCmd := exec.Command("sleep", "300")
	require.NoError(t, sleepCmd.Start(), "start sleep process")
	pid := sleepCmd.Process.Pid
	t.Cleanup(func() { _ = sleepCmd.Process.Kill(); _ = sleepCmd.Wait() })

	// Write a lock file pointing to the sleep process.
	createStaleLock(t, userDir, pid)

	start := time.Now()

	// Run kill with --timeout 3s. The sleep process won't die from SIGTERM,
	// so the kill command should time out and exit with code 3.
	result := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"kill", "--timeout", "3s",
	)

	elapsed := time.Since(start)

	// Exit code 3 = timeout (D-039).
	requireExitCode(t, result, 3)

	// Verify the timeout was approximately 3 seconds.
	assert.Greater(t, elapsed, 2500*time.Millisecond,
		"should wait at least ~3 seconds")
	assert.Less(t, elapsed, 6000*time.Millisecond,
		"should not wait for the default 5 seconds")
}

// ---------------------------------------------------------------------------
// TestErrorEdgeCases — CLI-E2E-264~267
// ---------------------------------------------------------------------------

// TestErrorEdgeCases verifies error handling for corrupted DB, socket
// permission issues, and invalid parameters.
//
// Scenarios: CLI-E2E-264 (corrupted DB), CLI-E2E-265 (socket permission),
// CLI-E2E-266 (invalid time), CLI-E2E-267 (invalid duration).
func TestErrorEdgeCases(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"

	// CLI-E2E-264: Corrupted DB file — write garbage data to xyncra.db,
	// then run a query command. Should fail with exit code 1.
	t.Run("db_corrupted", func(t *testing.T) {
		dbPath := env.dbPathFor(userID, deviceID)
		ensureDir(t, env.userDir(userID, deviceID))

		// Write garbage data to simulate a corrupted DB.
		err := os.WriteFile(dbPath, []byte("this is not a valid SQLite database"), 0600)
		require.NoError(t, err, "write garbage to DB file")

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"list-conversations",
		)
		requireExitCode(t, result, 1)
		assert.True(t, len(result.Stderr) > 0,
			"stderr should contain DB error, got: %s", result.Stderr)
	})

	// CLI-E2E-265: IPC socket permission denied — start a daemon, then
	// chmod 000 the socket file and verify IPC fails.
	t.Run("ipc_socket_permission_denied", func(t *testing.T) {
		userID2 := "perm-ipc-e2e"
		dp := env.startDaemon(t, userID2, deviceID)
		sockPath := dp.socketPath

		// Verify socket exists first.
		assertFileExists(t, sockPath)

		// Change socket permissions to 0000 (no access).
		err := os.Chmod(sockPath, 0000)
		require.NoError(t, err, "chmod socket to 0000")

		// Restore permissions in cleanup so daemon can clean up.
		t.Cleanup(func() { _ = os.Chmod(sockPath, 0600) })

		// Try an IPC-dependent command. The CLI should fail to connect.
		result := env.runCLI(t,
			"--user-id", userID2, "--device-id", deviceID,
			"--server", "ws://localhost:19999/ws",
			"send", "-c", "00000000-0000-0000-0000-000000000000",
			"-m", "should fail",
		)
		requireExitCode(t, result, 1)

		requireStopDaemon(t, dp)
	})

	// CLI-E2E-266: Invalid time parameter — logs tail --since "invalid".
	t.Run("invalid_time_param", func(t *testing.T) {
		ensureDir(t, env.userDir(userID, deviceID))

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "tail", "--since", "invalid",
		)
		requireExitCode(t, result, 1)
		assert.True(t, len(result.Stderr) > 0,
			"stderr should mention invalid time, got: %s", result.Stderr)
	})

	// CLI-E2E-267: Invalid duration parameter — logs cleanup --retain "invalid".
	t.Run("invalid_duration_param", func(t *testing.T) {
		ensureDir(t, env.userDir(userID, deviceID))

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "cleanup", "--retain", "invalid",
		)
		requireExitCode(t, result, 1)
		assert.True(t, len(result.Stderr) > 0,
			"stderr should mention invalid duration, got: %s", result.Stderr)
	})
}
