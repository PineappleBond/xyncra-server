package cli_e2e_test

// P1 functional completeness tests for the xyncra-client CLI (end-to-end).
//
// These tests cover daemon lifecycle edge cases, IPC operations and errors,
// standalone WebSocket fallback, write command error paths, and the
// delete/restore/mark-as-read lifecycle commands.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	clientstore "github.com/PineappleBond/xyncra-server/pkg/store"
	clientmodel "github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// TestListenSIGINT — CLI-E2E-005
// ---------------------------------------------------------------------------

// TestListenSIGINT verifies that sending SIGINT to the listen daemon results
// in a clean exit (code 0) and cleanup of the lock and socket files (D-031).
//
// Scenario: CLI-E2E-005 — listen SIGINT exit.
func TestListenSIGINT(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"

	dp := env.startDaemon(t, userID, deviceID)

	lockPath := env.lockPathFor(userID, deviceID)
	sockPath := dp.socketPath

	// Pre-condition: files exist.
	assertFileExists(t, lockPath)
	assertFileExists(t, sockPath)

	// Send SIGINT.
	err := dp.cmd.Process.Signal(syscall.SIGINT)
	require.NoError(t, err, "send SIGINT should succeed")

	// Wait for the daemon to exit.
	done := make(chan error, 1)
	go func() { done <- dp.cmd.Wait() }()
	select {
	case <-done:
		// Exited gracefully.
	case <-time.After(10 * time.Second):
		_ = dp.cmd.Process.Kill()
		<-done
		t.Fatal("daemon did not exit within 10s after SIGINT")
	}

	// Allow deferred cleanup to finish.
	time.Sleep(500 * time.Millisecond)

	// Socket and lock files should be cleaned up (D-031).
	assertFileNotExists(t, sockPath)
	assertFileNotExists(t, lockPath)
}

// ---------------------------------------------------------------------------
// TestListenNoUserID — CLI-E2E-006
// ---------------------------------------------------------------------------

// TestListenNoUserID verifies that running `listen` without --user-id and
// without XYNCRA_USER_ID fails with exit code 1 and an error message that
// mentions "user-id" (D-034, D-042).
//
// Scenario: CLI-E2E-006 — listen without user-id.
func TestListenNoUserID(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	result := env.runCLI(t, "listen")
	requireExitCode(t, result, 1)
	assert.Contains(t, result.Stderr, "user-id",
		"stderr should mention user-id when flag is missing")
}

// ---------------------------------------------------------------------------
// TestListenEnvVars — CLI-E2E-008
// ---------------------------------------------------------------------------

// TestListenEnvVars verifies that the XYNCRA_USER_ID environment variable is
// used as the user-id when no --user-id flag is provided (D-034). The daemon
// should start successfully and create its IPC socket.
//
// Scenario: CLI-E2E-008 — listen uses environment variables.
func TestListenEnvVars(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"

	// Ensure user directory exists.
	userDir := env.userDir(userID, deviceID)
	require.NoError(t, os.MkdirAll(userDir, 0700), "create user dir")

	// Start daemon manually with XYNCRA_USER_ID env var.
	cmd := env.buildCmd("listen", "--device-id", deviceID, "--server", e2eServerURL)
	cmd.Env = append(env.buildEnv(), "XYNCRA_USER_ID="+userID)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	cmd.Stdout = nil

	require.NoError(t, cmd.Start(), "start daemon with env var")

	sockPath := env.socketPathFor(userID, deviceID)

	// Wait for socket to appear (daemon started successfully).
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	if err := waitForSocket(waitCtx, sockPath); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("daemon socket did not appear at %s: %v\nstderr:\n%s",
			sockPath, err, stderrBuf.String())
	}

	assertFileExists(t, sockPath)

	// Cleanup.
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
}

// ---------------------------------------------------------------------------
// TestIPCOperations — CLI-E2E-022 ~ 026
// ---------------------------------------------------------------------------

// TestIPCOperations exercises the core IPC methods exposed by the listen
// daemon: create_conversation, delete_conversation, restore_conversation,
// delete_message, and mark_as_read. Each operation is tested as a sub-test
// in dependency order.
//
// Scenarios: CLI-E2E-022 through CLI-E2E-026.
func TestIPCOperations(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := uniqueUserID(fmt.Sprintf("alice-e2e-%s", t.Name()))
	peerID := uniqueUserID(fmt.Sprintf("bob-e2e-%s", t.Name()))
	deviceID := "dev1"

	dp := env.startDaemon(t, userID, deviceID)
	sockPath := dp.socketPath

	var convID string
	var msgUUID string

	// CLI-E2E-022: IPC create_conversation success.
	t.Run("create_conversation", func(t *testing.T) {
		resp := ipcCall(t, sockPath, "create_conversation", map[string]any{
			"user_id2": peerID,
		})
		require.Nil(t, resp.Error, "create_conversation IPC should succeed")

		var result client.CreateConversationResult
		require.NoError(t, json.Unmarshal(resp.Result, &result),
			"unmarshal create_conversation result")
		require.NotNil(t, result.Conversation, "conversation should not be nil")
		convID = result.Conversation.ID
		require.NotEmpty(t, convID, "conversation ID should not be empty")
	})

	// CLI-E2E-023: IPC delete_conversation success.
	t.Run("delete_conversation", func(t *testing.T) {
		require.NotEmpty(t, convID, "convID must be set from prior sub-test")
		resp := ipcCall(t, sockPath, "delete_conversation", map[string]any{
			"conversation_id": convID,
		})
		require.Nil(t, resp.Error, "delete_conversation IPC should succeed")
	})

	// CLI-E2E-024: IPC restore_conversation success.
	t.Run("restore_conversation", func(t *testing.T) {
		require.NotEmpty(t, convID, "convID must be set from prior sub-test")
		resp := ipcCall(t, sockPath, "restore_conversation", map[string]any{
			"conversation_id": convID,
		})
		require.Nil(t, resp.Error, "restore_conversation IPC should succeed")
	})

	// Prepare: send a message to obtain a message UUID for delete_message.
	t.Run("send_message_for_delete", func(t *testing.T) {
		require.NotEmpty(t, convID, "convID must be set from prior sub-test")
		resp := ipcCall(t, sockPath, "send_message", map[string]any{
			"conversation_id": convID,
			"content":         "msg to delete via IPC",
			"reply_to":        0,
		})
		require.Nil(t, resp.Error, "send_message IPC should succeed: %v", resp.Error)

		var result client.SendMessageResult
		require.NoError(t, json.Unmarshal(resp.Result, &result),
			"unmarshal send_message result")
		require.NotNil(t, result.Message, "message should not be nil")
		msgUUID = result.Message.ID
		require.NotEmpty(t, msgUUID, "message UUID should not be empty")
	})

	// CLI-E2E-025: IPC delete_message success.
	t.Run("delete_message", func(t *testing.T) {
		require.NotEmpty(t, msgUUID, "msgUUID must be set from prior sub-test")
		resp := ipcCall(t, sockPath, "delete_message", map[string]any{
			"message_id": msgUUID,
		})
		require.Nil(t, resp.Error, "delete_message IPC should succeed")
	})

	// CLI-E2E-026: IPC mark_as_read success.
	t.Run("mark_as_read", func(t *testing.T) {
		require.NotEmpty(t, convID, "convID must be set from prior sub-test")
		resp := ipcCall(t, sockPath, "mark_as_read", map[string]any{
			"conversation_id": convID,
			"message_id":      0, // mark all as read
		})
		require.Nil(t, resp.Error, "mark_as_read IPC should succeed")
	})

	requireStopDaemon(t, dp)
}

// ---------------------------------------------------------------------------
// TestIPCErrors — CLI-E2E-028 ~ 030
// ---------------------------------------------------------------------------

// TestIPCErrors verifies that the IPC server returns the correct JSON-RPC 2.0
// error codes for various error conditions (D-030).
//
// Scenarios: CLI-E2E-028 (method not found), CLI-E2E-029 (invalid request),
// CLI-E2E-030 (invalid params).
func TestIPCErrors(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"

	dp := env.startDaemon(t, userID, deviceID)
	sockPath := dp.socketPath

	// CLI-E2E-028: Method not found → error code -32601.
	t.Run("method_not_found", func(t *testing.T) {
		resp := ipcCall(t, sockPath, "nonexistent_method", nil)
		require.NotNil(t, resp.Error, "should return an error for unknown method")
		assert.Equal(t, -32601, resp.Error.Code,
			"error code should be -32601 (method not found)")
	})

	// CLI-E2E-029: Invalid request (non-JSON-RPC data) → parse error -32700.
	t.Run("invalid_request", func(t *testing.T) {
		raw := []byte(`this is not valid JSON-RPC`)
		rawResp := ipcRawCall(t, sockPath, raw)

		var resp ipcResponse
		require.NoError(t, json.Unmarshal(rawResp, &resp),
			"should parse error response")
		require.NotNil(t, resp.Error, "should return an error for invalid request")
		assert.Equal(t, -32700, resp.Error.Code,
			"error code should be -32700 (parse error)")
	})

	// CLI-E2E-030: Invalid params → error code -32602.
	t.Run("invalid_params", func(t *testing.T) {
		// create_conversation expects user_id2; send an empty object so
		// the server cannot find required parameters.
		resp := ipcCall(t, sockPath, "create_conversation", map[string]any{})
		// The server's handler validates user_id and returns -100
		// (ResponseCodeValidationError) when user_id2 is missing.
		require.NotNil(t, resp.Error,
			"should return an error for invalid/missing params")
		assert.Equal(t, -100, resp.Error.Code,
			"error code should be -100 (validation error) for missing user_id2")
	})

	requireStopDaemon(t, dp)
}

// ---------------------------------------------------------------------------
// TestStandaloneFallback — CLI-E2E-041 ~ 045
// ---------------------------------------------------------------------------

// TestStandaloneFallback verifies that CLI write commands work through the
// standalone WebSocket fallback (D-032) when no daemon is running. Each
// command is tried after a daemon has been stopped: the IPC connection fails,
// and the CLI falls back to a direct WebSocket connection.
//
// Scenarios: CLI-E2E-041 (create-conversation), CLI-E2E-042 (delete-conversation),
// CLI-E2E-043 (restore-conversation), CLI-E2E-044 (delete-message),
// CLI-E2E-045 (mark-as-read).
func TestStandaloneFallback(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := uniqueUserID(fmt.Sprintf("alice-e2e-%s", t.Name()))
	peerID := uniqueUserID(fmt.Sprintf("bob-e2e-%s", t.Name()))
	deviceID := "dev1"

	// Phase 1: Use a daemon to create the initial conversation on the server.
	dp := env.startDaemon(t, userID, deviceID)

	createResult := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"create-conversation", "--peer-id", peerID,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")

	// Send a message to create a message on the server.
	sendResult := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"send", "--conversation-id", convID, "--content", "standalone test msg",
	)
	requireExitCode(t, sendResult, 0)

	// Get the message UUID from the local DB for delete-message test.
	dbPath := env.dbPathFor(userID, deviceID)

	// Seed local DB with the conversation so mark-as-read can resolve --message-id 0.
	// First sync to get messages into local DB.
	syncResult := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"sync-updates",
	)
	requireExitCode(t, syncResult, 0)

	requireStopDaemon(t, dp)

	// Phase 2: No daemon running. Test standalone fallback for each command.

	// CLI-E2E-041: create-conversation standalone.
	t.Run("create_conversation", func(t *testing.T) {
		peerID2 := uniqueUserID(fmt.Sprintf("charlie-e2e-%s", t.Name()))
		result := env.runCLI(t,
			"--user-id", userID,
			"--server", e2eServerURL,
			"create-conversation", "--peer-id", peerID2,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Conversation",
			"stdout should contain 'Conversation'")
	})

	// CLI-E2E-042: delete-conversation standalone.
	t.Run("delete_conversation", func(t *testing.T) {
		// Create a throwaway conv to delete.
		peerID3 := uniqueUserID(fmt.Sprintf("del-e2e-%s", t.Name()))
		createR := env.runCLI(t,
			"--user-id", userID, "--server", e2eServerURL,
			"create-conversation", "--peer-id", peerID3,
		)
		requireExitCode(t, createR, 0)
		throwawayID := extractConversationID(t, createR.Stdout)
		require.NotEmpty(t, throwawayID)

		result := env.runCLI(t,
			"--user-id", userID, "--server", e2eServerURL,
			"delete-conversation", "-c", throwawayID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Conversation deleted.")
	})

	// CLI-E2E-043: restore-conversation standalone.
	t.Run("restore_conversation", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--server", e2eServerURL,
			"restore-conversation", "-c", convID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Conversation restored.")
	})

	// CLI-E2E-044: delete-message standalone.
	t.Run("delete_message", func(t *testing.T) {
		// Get a message UUID from the local DB.
		var msgID string
		db, err := clientstore.New(dbPath)
		require.NoError(t, err, "open local DB")
		ctx := context.Background()
		msgs, err := db.Messages.ListByConversation(ctx, convID, 0, 10)
		_ = db.Close()
		if err == nil && len(msgs) > 0 {
			msgID = msgs[0].ID
		}
		if msgID == "" {
			t.Skip("no messages in local DB to delete")
		}

		result := env.runCLI(t,
			"--user-id", userID, "--server", e2eServerURL,
			"delete-message", "--message-id", msgID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Message deleted.")
	})

	// CLI-E2E-045: mark-as-read standalone.
	// mark-as-read needs the conversation in the local DB to resolve
	// the message sequence number (D-012). After daemon stop, the local
	// DB should contain the conversation from the earlier IPC create.
	t.Run("mark_as_read", func(t *testing.T) {
		// Ensure the DB file exists and the conversation is present.
		// If the daemon's sync didn't populate the DB, seed it manually.
		dbP := env.dbPathFor(userID, deviceID)
		func() {
			db, err := clientstore.New(dbP)
			if err != nil {
				return
			}
			defer db.Close()
			ctx := context.Background()
			convs, _ := db.Conversations.GetByUser(ctx, userID, 0, 100)
			for _, c := range convs {
				if c.ID == convID {
					return // already present
				}
			}
			// Not found — seed it.
			_ = db.Conversations.Create(ctx, &clientmodel.Conversation{
				ID:                     convID,
				UserID1:                userID,
				UserID2:                peerID,
				Type:                   "1-on-1",
				LastProcessedMessageID: 1,
				CreatedAt:              time.Now(),
				UpdatedAt:              time.Now(),
				LastMessageAt:          time.Now(),
			})
		}()

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID, "--server", e2eServerURL,
			"mark-as-read", "-c", convID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Marked as read")
	})
}

// ---------------------------------------------------------------------------
// TestSendErrors — CLI-E2E-051, 053, 054
// ---------------------------------------------------------------------------

// TestSendErrors verifies the error paths for the send command: sending to a
// non-existent conversation, and missing required flags.
//
// Scenarios: CLI-E2E-051 (non-existent conv), CLI-E2E-053 (missing
// --conversation-id), CLI-E2E-054 (missing --content).
func TestSendErrors(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())

	// CLI-E2E-051: send to non-existent conversation.
	t.Run("nonexistent_conversation", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID,
			"--server", e2eServerURL,
			"send",
			"-c", "00000000-0000-0000-0000-000000000000",
			"-m", "should fail",
		)
		requireExitCode(t, result, 1)
		assert.True(t, containsAny(result.Stderr, "not found", "not_found", "conversation"),
			"stderr should indicate conversation not found, got: %s", result.Stderr)
	})

	// CLI-E2E-053: missing --conversation-id.
	t.Run("missing_conversation_id", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID,
			"send", "-m", "test",
		)
		requireExitCode(t, result, 1)
		assert.Contains(t, result.Stderr, "conversation-id",
			"stderr should mention conversation-id")
	})

	// CLI-E2E-054: missing --content.
	t.Run("missing_content", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID,
			"send", "-c", "00000000-0000-0000-0000-000000000000",
		)
		requireExitCode(t, result, 1)
		assert.Contains(t, result.Stderr, "content",
			"stderr should mention content")
	})
}

// ---------------------------------------------------------------------------
// TestDeleteConversation — CLI-E2E-070 ~ 072
// ---------------------------------------------------------------------------

// TestDeleteConversation verifies the delete-conversation command, including
// the cascade soft-delete behavior (D-013) and error handling for non-existent
// conversation IDs.
//
// Scenarios: CLI-E2E-070 (normal delete), CLI-E2E-071 (cascade delete),
// CLI-E2E-072 (non-existent ID).
func TestDeleteConversation(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := uniqueUserID(fmt.Sprintf("alice-e2e-%s", t.Name()))
	peerID := uniqueUserID(fmt.Sprintf("bob-e2e-%s", t.Name()))
	deviceID := "dev1"

	dp := env.startDaemon(t, userID, deviceID)
	sockPath := dp.socketPath

	// CLI-E2E-070: Normal delete — create a conversation and delete it.
	t.Run("normal_delete", func(t *testing.T) {
		createResp := ipcCall(t, sockPath, "create_conversation", map[string]any{
			"user_id2": peerID,
		})
		require.Nil(t, createResp.Error, "create_conversation should succeed")

		var createResult client.CreateConversationResult
		require.NoError(t, json.Unmarshal(createResp.Result, &createResult))
		convID := createResult.Conversation.ID
		require.NotEmpty(t, convID)

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"delete-conversation", "-c", convID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Conversation deleted.")
	})

	// CLI-E2E-071: Cascade delete — delete a conversation that has messages
	// and verify that the messages are also soft-deleted (D-013).
	t.Run("cascade_delete", func(t *testing.T) {
		cascadePeerID := uniqueUserID(fmt.Sprintf("cascade-e2e-%s", t.Name()))

		// Create conversation.
		createResp := ipcCall(t, sockPath, "create_conversation", map[string]any{
			"user_id2": cascadePeerID,
		})
		require.Nil(t, createResp.Error, "create should succeed")
		var createResult client.CreateConversationResult
		require.NoError(t, json.Unmarshal(createResp.Result, &createResult))
		convID := createResult.Conversation.ID

		// Send messages via IPC.
		for i := 0; i < 2; i++ {
			sendResp := ipcCall(t, sockPath, "send_message", map[string]any{
				"conversation_id": convID,
				"content":         fmt.Sprintf("cascade msg %d", i),
				"reply_to":        0,
			})
			require.Nil(t, sendResp.Error, "send should succeed")
		}

		// Sync to get messages into local DB.
		syncResult := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"sync-updates",
		)
		requireExitCode(t, syncResult, 0)

		// Verify messages exist in local DB before delete.
		dbPath := env.dbPathFor(userID, deviceID)
		db := openLocalDB(t, dbPath)
		ctx := context.Background()
		msgsBefore, err := db.Messages.ListByConversation(ctx, convID, 0, 100)
		require.NoError(t, err)
		require.NotEmpty(t, msgsBefore, "should have messages before delete")
		require.NoError(t, db.Close())

		// Delete the conversation.
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"delete-conversation", "-c", convID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Conversation deleted.")

		// Sync to apply the cascade delete to local DB.
		syncResult2 := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"sync-updates",
		)
		requireExitCode(t, syncResult2, 0)

		// Verify conversation is soft-deleted (not found via normal query).
		db2 := openLocalDB(t, dbPath)
		_, getErr := db2.Conversations.Get(ctx, convID)
		assert.Error(t, getErr, "conversation should be soft-deleted (not found)")

		// Verify messages are also soft-deleted (cascade, D-013).
		msgsAfter, err := db2.Messages.ListByConversation(ctx, convID, 0, 100)
		require.NoError(t, err)
		assert.Empty(t, msgsAfter,
			"messages should be cascade soft-deleted (D-013)")
		require.NoError(t, db2.Close())
	})

	// CLI-E2E-072: Non-existent conversation ID.
	t.Run("nonexistent_id", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"delete-conversation", "-c", "00000000-0000-0000-0000-000000000000",
		)
		requireExitCode(t, result, 1)
		assert.True(t, containsAny(result.Stderr, "not found", "not_found"),
			"stderr should indicate not found, got: %s", result.Stderr)
	})

	requireStopDaemon(t, dp)
}

// ---------------------------------------------------------------------------
// TestRestoreConversation — CLI-E2E-080 ~ 081
// ---------------------------------------------------------------------------

// TestRestoreConversation verifies the restore-conversation command, including
// the cascade restore semantics (D-015) and error handling for non-existent
// conversation IDs.
//
// Scenarios: CLI-E2E-080 (restore deleted conversation), CLI-E2E-081
// (non-existent ID).
func TestRestoreConversation(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := uniqueUserID(fmt.Sprintf("alice-e2e-%s", t.Name()))
	peerID := uniqueUserID(fmt.Sprintf("bob-e2e-%s", t.Name()))
	deviceID := "dev1"

	dp := env.startDaemon(t, userID, deviceID)
	sockPath := dp.socketPath

	// CLI-E2E-080: Create → delete → restore → verify.
	t.Run("restore_deleted", func(t *testing.T) {
		// Create.
		createResp := ipcCall(t, sockPath, "create_conversation", map[string]any{
			"user_id2": peerID,
		})
		require.Nil(t, createResp.Error)
		var createResult client.CreateConversationResult
		require.NoError(t, json.Unmarshal(createResp.Result, &createResult))
		convID := createResult.Conversation.ID

		// Send a message for cascade verification.
		sendResp := ipcCall(t, sockPath, "send_message", map[string]any{
			"conversation_id": convID,
			"content":         "restore test msg",
			"reply_to":        0,
		})
		require.Nil(t, sendResp.Error)

		// Sync to get data into local DB.
		syncResult := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"sync-updates",
		)
		requireExitCode(t, syncResult, 0)

		// Delete.
		delResult := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"delete-conversation", "-c", convID,
		)
		requireExitCode(t, delResult, 0)

		// Sync to apply delete locally.
		syncResult2 := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"sync-updates",
		)
		requireExitCode(t, syncResult2, 0)

		// Restore (D-015: cascade restore).
		restoreResult := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"restore-conversation", "-c", convID,
		)
		requireExitCode(t, restoreResult, 0)
		assert.Contains(t, restoreResult.Stdout, "Conversation restored.")

		// Sync to apply restore locally.
		syncResult3 := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"sync-updates",
		)
		requireExitCode(t, syncResult3, 0)

		// Verify conversation is accessible again.
		dbPath := env.dbPathFor(userID, deviceID)
		db := openLocalDB(t, dbPath)
		ctx := context.Background()
		conv, err := db.Conversations.Get(ctx, convID)
		require.NoError(t, err, "conversation should be found after restore")
		assert.NotNil(t, conv, "conversation should not be nil after restore")
		require.NoError(t, db.Close())
	})

	// CLI-E2E-081: Non-existent conversation ID.
	t.Run("nonexistent_id", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"restore-conversation", "-c", "00000000-0000-0000-0000-000000000000",
		)
		requireExitCode(t, result, 1)
		assert.True(t, containsAny(result.Stderr, "not found", "not_found"),
			"stderr should indicate not found, got: %s", result.Stderr)
	})

	requireStopDaemon(t, dp)
}

// ---------------------------------------------------------------------------
// TestDeleteMessage — CLI-E2E-090 ~ 092
// ---------------------------------------------------------------------------

// TestDeleteMessage verifies the delete-message command, including sender-only
// permission (D-014) and error handling for non-existent message IDs.
//
// Scenarios: CLI-E2E-090 (own message), CLI-E2E-091 (permission denied),
// CLI-E2E-092 (non-existent message).
func TestDeleteMessage(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := uniqueUserID(fmt.Sprintf("alice-e2e-%s", t.Name()))
	peerID := uniqueUserID(fmt.Sprintf("bob-e2e-%s", t.Name()))
	deviceID := "dev1"

	dp := env.startDaemon(t, userID, deviceID)
	sockPath := dp.socketPath

	// Create a conversation and send a message.
	createResp := ipcCall(t, sockPath, "create_conversation", map[string]any{
		"user_id2": peerID,
	})
	require.Nil(t, createResp.Error)
	var createResult client.CreateConversationResult
	require.NoError(t, json.Unmarshal(createResp.Result, &createResult))
	convID := createResult.Conversation.ID

	sendResp := ipcCall(t, sockPath, "send_message", map[string]any{
		"conversation_id": convID,
		"content":         "msg to delete",
		"reply_to":        0,
	})
	require.Nil(t, sendResp.Error)
	var sendResult client.SendMessageResult
	require.NoError(t, json.Unmarshal(sendResp.Result, &sendResult))
	msgUUID := sendResult.Message.ID

	// CLI-E2E-090: Delete own message (D-014).
	t.Run("own_message", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"delete-message", "--message-id", msgUUID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Message deleted.")
	})

	// CLI-E2E-091: Delete another user's message (permission denied, D-014).
	t.Run("permission_denied", func(t *testing.T) {
		// Send another message as alice.
		sendResp2 := ipcCall(t, sockPath, "send_message", map[string]any{
			"conversation_id": convID,
			"content":         "msg for perm test",
			"reply_to":        0,
		})
		require.Nil(t, sendResp2.Error)
		var sendResult2 client.SendMessageResult
		require.NoError(t, json.Unmarshal(sendResp2.Result, &sendResult2))
		aliceMsgUUID := sendResult2.Message.ID

		// Start bob's daemon. Bob tries to delete alice's message.
		dpBob := env.startDaemon(t, peerID, deviceID)
		defer requireStopDaemon(t, dpBob)

		result := env.runCLI(t,
			"--user-id", peerID, "--device-id", deviceID,
			"delete-message", "--message-id", aliceMsgUUID,
		)
		requireExitCode(t, result, 1)
		assert.True(t, containsAny(result.Stderr, "permission", "denied", "not allowed", "not the sender", "cannot delete", "all delivery methods failed"),
			"stderr should indicate error, got: %s", result.Stderr)
	})

	// CLI-E2E-092: Delete non-existent message.
	t.Run("nonexistent_message", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"delete-message", "--message-id", "00000000-0000-0000-0000-000000000000",
		)
		requireExitCode(t, result, 1)
		assert.True(t, containsAny(result.Stderr, "not found", "not_found"),
			"stderr should indicate not found, got: %s", result.Stderr)
	})

	requireStopDaemon(t, dp)
}

// ---------------------------------------------------------------------------
// TestMarkAsRead — CLI-E2E-100 ~ 103
// ---------------------------------------------------------------------------

// TestMarkAsRead verifies the mark-as-read command, including specific message
// targeting, mark-all-as-read, MAX semantics (D-012), and error handling for
// non-existent conversation IDs.
//
// Scenarios: CLI-E2E-100 (specific message), CLI-E2E-101 (mark all),
// CLI-E2E-102 (MAX semantics), CLI-E2E-103 (non-existent conv).
func TestMarkAsRead(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := uniqueUserID(fmt.Sprintf("alice-e2e-%s", t.Name()))
	peerID := uniqueUserID(fmt.Sprintf("bob-e2e-%s", t.Name()))
	deviceID := "dev1"

	dp := env.startDaemon(t, userID, deviceID)
	sockPath := dp.socketPath

	// Create a conversation and send 3 messages.
	createResp := ipcCall(t, sockPath, "create_conversation", map[string]any{
		"user_id2": peerID,
	})
	require.Nil(t, createResp.Error)
	var createResult client.CreateConversationResult
	require.NoError(t, json.Unmarshal(createResp.Result, &createResult))
	convID := createResult.Conversation.ID

	var lastMsgSeq uint32
	for i := 0; i < 3; i++ {
		sendResp := ipcCall(t, sockPath, "send_message", map[string]any{
			"conversation_id": convID,
			"content":         fmt.Sprintf("mark-as-read test msg %d", i),
			"reply_to":        0,
		})
		require.Nil(t, sendResp.Error)
		var sr client.SendMessageResult
		require.NoError(t, json.Unmarshal(sendResp.Result, &sr))
		lastMsgSeq = sr.Message.MessageID
	}
	require.True(t, lastMsgSeq >= 3, "should have at least 3 messages")

	// CLI-E2E-100: Mark as read up to a specific message.
	t.Run("specific_message", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"mark-as-read", "-c", convID, "--message-id", "2",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Marked as read up to message #2.")
	})

	// CLI-E2E-101: Mark all as read (no --message-id, resolves from local DB).
	// First sync to populate local DB with the conversation's LastProcessedMessageID.
	t.Run("mark_all_read", func(t *testing.T) {
		// Sync so local DB has the conversation.
		syncResult := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"sync-updates",
		)
		requireExitCode(t, syncResult, 0)

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"mark-as-read", "-c", convID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Marked as read")
	})

	// CLI-E2E-102: MAX semantics — marking as read at a lower seq should not
	// move the cursor backward (D-012). After the previous mark_all_read set
	// the cursor to lastMsgSeq, marking at seq 1 should be a no-op.
	t.Run("max_semantics", func(t *testing.T) {
		// First sync to populate the local DB so we can verify cursor state.
		syncResult := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"sync-updates",
		)
		requireExitCode(t, syncResult, 0)

		// Verify the cursor is at lastMsgSeq after mark_all_read.
		dbPath := env.dbPathFor(userID, deviceID)
		var cursorAfterMarkAll uint32
		func() {
			db, err := clientstore.New(dbPath)
			require.NoError(t, err, "open local DB")
			defer db.Close()
			ctx := context.Background()
			conv, err := db.Conversations.Get(ctx, convID)
			require.NoError(t, err, "get conversation for cursor check")
			require.NotNil(t, conv)
			// The read cursor is stored as LastReadMessageID1 or
			// LastReadMessageID2 depending on which side the user is.
			cursorAfterMarkAll = conv.LastReadMessageID1
			if conv.UserID2 == userID {
				cursorAfterMarkAll = conv.LastReadMessageID2
			}
		}()

		// Now mark-as-read at seq 1 — should be a no-op due to MAX semantics.
		// D-047: CLI displays the server-confirmed cursor value (MAX result),
		// not the requested value. Since cursor is already at lastMsgSeq (>=3),
		// the server returns lastMsgSeq as the confirmed cursor.
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"mark-as-read", "-c", convID, "--message-id", "1",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout,
			fmt.Sprintf("Marked as read up to message #%d.", cursorAfterMarkAll),
		)

		// Sync to apply server-side cursor state to local DB.
		syncResult2 := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"sync-updates",
		)
		requireExitCode(t, syncResult2, 0)

		// Verify via DB that the cursor has NOT moved backward (D-012).
		func() {
			db, err := clientstore.New(dbPath)
			require.NoError(t, err, "open local DB for cursor check")
			defer db.Close()
			ctx := context.Background()
			conv, err := db.Conversations.Get(ctx, convID)
			require.NoError(t, err, "get conversation for cursor check")
			require.NotNil(t, conv)
			cursorAfterLowMark := conv.LastReadMessageID1
			if conv.UserID2 == userID {
				cursorAfterLowMark = conv.LastReadMessageID2
			}
			assert.Equal(t, cursorAfterMarkAll, cursorAfterLowMark,
				"MAX semantics (D-012): cursor should not move backward from %d after mark-as-read at seq 1",
				cursorAfterMarkAll)
		}()

		// A subsequent mark-as-read at a higher seq should still work.
		result2 := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"mark-as-read", "-c", convID, "--message-id", fmt.Sprintf("%d", lastMsgSeq),
		)
		requireExitCode(t, result2, 0)
		assert.Contains(t, result2.Stdout,
			fmt.Sprintf("Marked as read up to message #%d.", lastMsgSeq))
	})

	// CLI-E2E-103: Non-existent conversation.
	t.Run("nonexistent_conversation", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"mark-as-read", "-c", "00000000-0000-0000-0000-000000000000",
			"--message-id", "1",
		)
		requireExitCode(t, result, 1)
		assert.True(t, containsAny(result.Stderr, "not found", "not_found"),
			"stderr should indicate not found, got: %s", result.Stderr)
	})

	requireStopDaemon(t, dp)
}

// ---------------------------------------------------------------------------
// Helpers local to P1 tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// TestQueryCommands — CLI-E2E-111~113, 120~122, 131~132, 140~142
// ---------------------------------------------------------------------------

// TestQueryCommands verifies the query commands (list-conversations,
// get-conversation, get-messages, search-messages) that read from the local
// SQLite database (D-035). Test data is seeded directly into the local DB for
// deterministic, fast execution without depending on daemon sync.
//
// Scenarios: CLI-E2E-111 (pagination), CLI-E2E-112 (empty list),
// CLI-E2E-113 (exclude soft-deleted), CLI-E2E-120 (detail),
// CLI-E2E-121 (non-existent ID), CLI-E2E-122 (missing --conversation-id),
// CLI-E2E-131 (get-messages pagination), CLI-E2E-132 (get-messages --limit),
// CLI-E2E-140 (search-messages), CLI-E2E-141 (no match),
// CLI-E2E-142 (missing --query).
func TestQueryCommands(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	peerID := fmt.Sprintf("bob-e2e-%s", t.Name())
	deviceID := "dev1"

	// Phase 1: Create the local DB by starting a daemon briefly.
	dp := env.startDaemon(t, userID, deviceID)
	dbPath := env.dbPathFor(userID, deviceID)
	requireStopDaemon(t, dp)

	// Phase 2: Seed conversations and messages directly into the local DB.
	now := time.Now()
	conv1ID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	conv2ID := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
	conv3ID := "cccccccc-dddd-eeee-ffff-aaaaaaaaaaaa"
	msg1ID := "11111111-1111-1111-1111-111111111111"
	msg2ID := "22222222-2222-2222-2222-222222222222"
	msg3ID := "33333333-3333-3333-3333-333333333333"

	seedLocalDBFull(t, dbPath,
		[]*clientmodel.Conversation{
			{
				ID: conv1ID, UserID1: userID, UserID2: peerID,
				Type: "1-on-1", LastProcessedMessageID: 3,
				CreatedAt: now, UpdatedAt: now, LastMessageAt: now,
			},
			{
				ID: conv2ID, UserID1: userID, UserID2: "charlie-e2e",
				Type: "1-on-1", LastProcessedMessageID: 0,
				CreatedAt: now, UpdatedAt: now, LastMessageAt: now,
			},
			{
				// This conversation will be soft-deleted for CLI-E2E-113.
				ID: conv3ID, UserID1: userID, UserID2: "deleted-peer",
				Type: "1-on-1", LastProcessedMessageID: 0,
				CreatedAt: now, UpdatedAt: now, LastMessageAt: now,
				DeletedAt: gorm.DeletedAt{Time: now, Valid: true},
			},
		},
		[]*clientmodel.Message{
			{
				ID: msg1ID, ClientMessageID: "cms-001", ConversationID: conv1ID,
				MessageID: 1, SenderID: userID, Content: "hello world",
				Type: "text", Status: "sent", CreatedAt: now,
			},
			{
				ID: msg2ID, ClientMessageID: "cms-002", ConversationID: conv1ID,
				MessageID: 2, SenderID: peerID, Content: "hello back",
				Type: "text", Status: "sent", CreatedAt: now,
			},
			{
				ID: msg3ID, ClientMessageID: "cms-003", ConversationID: conv1ID,
				MessageID: 3, SenderID: userID, Content: "searchable needle",
				Type: "text", Status: "sent", CreatedAt: now,
			},
		},
	)

	// CLI-E2E-111: list-conversations pagination (--limit 1 should show "...more").
	t.Run("list_pagination", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"list-conversations", "--limit", "1",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "more",
			"should show pagination hint when more conversations exist")
	})

	// CLI-E2E-112: list-conversations empty list.
	t.Run("list_empty", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", "nonexistent-user-query", "--device-id", deviceID,
			"list-conversations",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "No conversations found",
			"should indicate no conversations for unknown user")
	})

	// CLI-E2E-113: list-conversations excludes soft-deleted (D-035).
	t.Run("list_excludes_deleted", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"list-conversations",
		)
		requireExitCode(t, result, 0)
		assert.NotContains(t, result.Stdout, "deleted-peer",
			"soft-deleted conversation should not appear in listing")
	})

	// CLI-E2E-120: get-conversation details.
	t.Run("get_detail", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"get-conversation", "-c", conv1ID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Conversation Details")
		assert.Contains(t, result.Stdout, conv1ID)
		assert.Contains(t, result.Stdout, peerID)
		assert.Contains(t, result.Stdout, "Unread:")
	})

	// CLI-E2E-121: get-conversation non-existent ID.
	t.Run("get_nonexistent", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"get-conversation", "-c", "00000000-0000-0000-0000-000000000000",
		)
		requireExitCode(t, result, 1)
		assert.Contains(t, result.Stderr, "not found")
	})

	// CLI-E2E-122: get-conversation missing --conversation-id.
	t.Run("get_missing_id", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"get-conversation",
		)
		requireExitCode(t, result, 1)
	})

	// CLI-E2E-131: get-messages --after-message-id pagination.
	t.Run("get_messages_pagination", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"get-messages", "-c", conv1ID, "--after-message-id", "1",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "hello back")
		assert.Contains(t, result.Stdout, "searchable needle")
		assert.NotContains(t, result.Stdout, "[#1]",
			"message #1 should not appear after cursor")
	})

	// CLI-E2E-132: get-messages --limit.
	t.Run("get_messages_limit", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"get-messages", "-c", conv1ID, "--limit", "2",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "more",
			"should indicate more messages available beyond limit")
	})

	// CLI-E2E-140: search-messages content search.
	t.Run("search_found", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"search-messages", "-c", conv1ID, "-q", "needle",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "searchable needle")
	})

	// CLI-E2E-141: search-messages no match.
	t.Run("search_no_match", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"search-messages", "-c", conv1ID, "-q", "zzzznotexist",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "No messages found")
	})

	// CLI-E2E-142: search-messages missing --query.
	t.Run("search_missing_query", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"search-messages", "-c", conv1ID,
		)
		requireExitCode(t, result, 1)
	})
}

// ---------------------------------------------------------------------------
// TestSyncUpdatesNoNewData — CLI-E2E-152
// ---------------------------------------------------------------------------

// TestSyncUpdatesNoNewData verifies that running sync-updates twice in a row
// succeeds both times, with the second invocation reporting "Sync complete."
// and no new data (D-036).
//
// Scenario: CLI-E2E-152 — sync-updates no new data.
func TestSyncUpdatesNoNewData(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := uniqueUserID(fmt.Sprintf("alice-e2e-%s", t.Name()))
	peerID := uniqueUserID(fmt.Sprintf("bob-e2e-%s", t.Name()))
	deviceID := "dev1"

	dp := env.startDaemon(t, userID, deviceID)

	// Create some data so the first sync has something.
	createConversationAndSend(t, env, userID, deviceID, peerID)

	// First sync-updates: should succeed.
	sync1 := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"sync-updates",
	)
	requireExitCode(t, sync1, 0)
	assert.Contains(t, sync1.Stdout, "Sync complete.")

	// Second sync-updates: no new data, should still succeed (D-036).
	sync2 := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"sync-updates",
	)
	requireExitCode(t, sync2, 0)
	assert.Contains(t, sync2.Stdout, "Sync complete.")

	requireStopDaemon(t, dp)
}

// ---------------------------------------------------------------------------
// TestDraftSave — CLI-E2E-160~163
// ---------------------------------------------------------------------------

// TestDraftSave verifies the "draft save" command: creating a new draft,
// upserting an existing draft, and missing required flags. Drafts are
// local-only operations that do not require a running daemon.
//
// Scenarios: CLI-E2E-160 (new draft), CLI-E2E-161 (upsert),
// CLI-E2E-162 (missing --conversation-id), CLI-E2E-163 (missing --content).
func TestDraftSave(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	convID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	// Ensure the user directory exists so the DB can be created.
	ensureDir(t, env.userDir(userID, deviceID))

	// CLI-E2E-160: Save a new draft.
	t.Run("new_draft", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "save", "-c", convID, "-m", "draft text v1",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Draft saved.")
	})

	// CLI-E2E-161: Save again with different content (UPSERT).
	t.Run("upsert", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "save", "-c", convID, "-m", "draft text v2",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Draft saved.")

		// Verify the upserted content is returned by "draft get".
		getResult := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "get", "-c", convID,
		)
		requireExitCode(t, getResult, 0)
		assert.Contains(t, getResult.Stdout, "draft text v2",
			"upserted draft should have the new content")
	})

	// CLI-E2E-162: Missing --conversation-id.
	t.Run("missing_conv_id", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "save", "-m", "test",
		)
		requireExitCode(t, result, 1)
	})

	// CLI-E2E-163: Missing --content.
	t.Run("missing_content", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "save", "-c", convID,
		)
		requireExitCode(t, result, 1)
	})
}

// ---------------------------------------------------------------------------
// TestDraftGet — CLI-E2E-170~171
// ---------------------------------------------------------------------------

// TestDraftGet verifies the "draft get" command: retrieving an existing draft
// and handling the case where no draft exists for the conversation.
//
// Scenarios: CLI-E2E-170 (existing draft), CLI-E2E-171 (no draft).
func TestDraftGet(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	convID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	convID2 := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"

	ensureDir(t, env.userDir(userID, deviceID))

	// Save a draft first.
	saveResult := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"draft", "save", "-c", convID, "-m", "get test draft",
	)
	requireExitCode(t, saveResult, 0)

	// CLI-E2E-170: Get existing draft.
	t.Run("existing", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "get", "-c", convID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "get test draft")
	})

	// CLI-E2E-171: Get non-existent draft.
	t.Run("not_found", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "get", "-c", convID2,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "No draft found")
	})
}

// ---------------------------------------------------------------------------
// TestDraftDelete — CLI-E2E-180~181
// ---------------------------------------------------------------------------

// TestDraftDelete verifies the "draft delete" command: deleting an existing
// draft and handling the case where no draft exists.
//
// Scenarios: CLI-E2E-180 (existing draft), CLI-E2E-181 (no draft).
func TestDraftDelete(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	convID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	convID2 := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"

	ensureDir(t, env.userDir(userID, deviceID))

	// Save a draft first.
	saveResult := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"draft", "save", "-c", convID, "-m", "to be deleted",
	)
	requireExitCode(t, saveResult, 0)

	// CLI-E2E-180: Delete existing draft.
	t.Run("existing", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "delete", "-c", convID,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Draft deleted.")

		// Verify the draft is gone.
		getResult := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "get", "-c", convID,
		)
		requireExitCode(t, getResult, 0)
		assert.Contains(t, getResult.Stdout, "No draft found")
	})

	// CLI-E2E-181: Delete non-existent draft.
	t.Run("not_found", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"draft", "delete", "-c", convID2,
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "No draft found")
	})
}

// ---------------------------------------------------------------------------
// TestLogsTail — CLI-E2E-190~192
// ---------------------------------------------------------------------------

// TestLogsTail verifies the "logs tail" command: basic output, filtering by
// --type, and --limit. RPC log entries are seeded directly into the local DB.
//
// Scenarios: CLI-E2E-190 (basic), CLI-E2E-191 (--type rpc), CLI-E2E-192 (--limit).
func TestLogsTail(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	dbPath := env.dbPathFor(userID, deviceID)

	ensureDir(t, env.userDir(userID, deviceID))
	seedRPCLogs(t, dbPath, 10)

	// CLI-E2E-190: Basic logs tail (default --type rpc).
	t.Run("basic", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "tail",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "TIME")
		assert.Contains(t, result.Stdout, "METHOD")
		assert.Contains(t, result.Stdout, "STATUS")
	})

	// CLI-E2E-191: --type rpc (explicit).
	t.Run("type_rpc", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "tail", "--type", "rpc",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "send_message")
	})

	// CLI-E2E-192: --limit.
	t.Run("limit", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "tail", "--limit", "3",
		)
		requireExitCode(t, result, 0)
		// Count data lines (those containing a seeded method name).
		lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
		dataLines := 0
		for _, line := range lines {
			if strings.Contains(line, "send_message") || strings.Contains(line, "create_conversation") {
				dataLines++
			}
		}
		assert.LessOrEqual(t, dataLines, 3, "should have at most 3 data entries")
	})
}

// ---------------------------------------------------------------------------
// TestLogsSearch — CLI-E2E-200~201
// ---------------------------------------------------------------------------

// TestLogsSearch verifies the "logs search" command: basic search and
// filtering by --method.
//
// Scenarios: CLI-E2E-200 (basic), CLI-E2E-201 (--method filter).
func TestLogsSearch(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	dbPath := env.dbPathFor(userID, deviceID)

	ensureDir(t, env.userDir(userID, deviceID))
	seedRPCLogs(t, dbPath, 5)

	// CLI-E2E-200: Basic logs search.
	t.Run("basic", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "search",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "TIME")
		assert.Contains(t, result.Stdout, "METHOD")
	})

	// CLI-E2E-201: --method filter.
	t.Run("method_filter", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "search", "--method", "send_message",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "send_message")
		assert.NotContains(t, result.Stdout, "create_conversation",
			"should only contain send_message entries")
	})
}

// ---------------------------------------------------------------------------
// TestLogsStats — CLI-E2E-210
// ---------------------------------------------------------------------------

// TestLogsStats verifies the "logs stats" command produces per-method
// aggregate statistics.
//
// Scenario: CLI-E2E-210 — basic stats.
func TestLogsStats(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	dbPath := env.dbPathFor(userID, deviceID)

	ensureDir(t, env.userDir(userID, deviceID))
	seedRPCLogs(t, dbPath, 5)

	result := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"logs", "stats",
	)
	requireExitCode(t, result, 0)
	assert.Contains(t, result.Stdout, "METHOD")
	assert.Contains(t, result.Stdout, "COUNT")
	assert.Contains(t, result.Stdout, "SUCCESS")
	assert.Contains(t, result.Stdout, "ERRORS")
}

// ---------------------------------------------------------------------------
// TestLogsExport — CLI-E2E-220~222
// ---------------------------------------------------------------------------

// TestLogsExport verifies the "logs export" command: CSV format, JSON format,
// and exporting to a file.
//
// Scenarios: CLI-E2E-220 (CSV), CLI-E2E-221 (JSON), CLI-E2E-222 (file output).
func TestLogsExport(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	dbPath := env.dbPathFor(userID, deviceID)

	ensureDir(t, env.userDir(userID, deviceID))
	seedRPCLogs(t, dbPath, 5)

	// CLI-E2E-220: Export as CSV.
	t.Run("csv", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "export", "--format", "csv",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "id,request_id,method",
			"CSV output should contain header row")
	})

	// CLI-E2E-221: Export as JSON.
	t.Run("json", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "export", "--format", "json",
		)
		requireExitCode(t, result, 0)
		// JSON output should start with [ (array).
		trimmed := strings.TrimSpace(result.Stdout)
		assert.True(t, len(trimmed) > 0 && trimmed[0] == '[',
			"JSON output should be a JSON array, got: %s",
			trimmed[:min(50, len(trimmed))])
	})

	// CLI-E2E-222: Export to file.
	t.Run("to_file", func(t *testing.T) {
		outPath := filepath.Join(t.TempDir(), "export.csv")
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "export", "--format", "csv", "-o", outPath,
		)
		requireExitCode(t, result, 0)
		assertFileExists(t, outPath)
		assert.Contains(t, result.Stderr, "Exported to")
	})
}

// ---------------------------------------------------------------------------
// TestLogsCleanup — CLI-E2E-230~231
// ---------------------------------------------------------------------------

// TestLogsCleanup verifies the "logs cleanup" command: actual cleanup and
// dry-run mode (D-040).
//
// Scenarios: CLI-E2E-231 (--dry-run), CLI-E2E-230 (basic cleanup).
func TestLogsCleanup(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"
	dbPath := env.dbPathFor(userID, deviceID)

	ensureDir(t, env.userDir(userID, deviceID))
	seedRPCLogs(t, dbPath, 5)

	// CLI-E2E-231: --dry-run (test first to avoid destroying data).
	t.Run("dry_run", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "cleanup", "--dry-run",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Would delete",
			"dry-run should report what would be deleted")
	})

	// CLI-E2E-230: Basic cleanup.
	t.Run("basic", func(t *testing.T) {
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"logs", "cleanup",
		)
		requireExitCode(t, result, 0)
		assert.Contains(t, result.Stdout, "Deleted",
			"cleanup should report deleted count")
	})
}

// ---------------------------------------------------------------------------
// TestKillNoDaemon — CLI-E2E-242
// ---------------------------------------------------------------------------

// TestKillNoDaemon verifies that the kill command gracefully handles the case
// where no daemon is running (D-039).
//
// Scenario: CLI-E2E-242 — kill daemon not running.
func TestKillNoDaemon(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"

	// Ensure user directory exists but no daemon is running.
	ensureDir(t, env.userDir(userID, deviceID))

	result := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"kill",
	)
	// D-039: "kill" with no daemon running should exit 0 (success — nothing to do).
	requireExitCode(t, result, 0)
	assert.Contains(t, result.Stderr, "No running daemon")
}

// ---------------------------------------------------------------------------
// TestKillCustomTimeout — CLI-E2E-244
// ---------------------------------------------------------------------------

// TestKillCustomTimeout verifies that the --timeout flag is respected when the
// daemon process does not respond to SIGTERM (D-039). A sleep process (which
// ignores SIGTERM) is used; the kill command should exit with code 3 after the
// specified timeout.
//
// Scenario: CLI-E2E-244 — kill --timeout custom duration.
func TestKillCustomTimeout(t *testing.T) {
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

	// Run kill with --timeout 2s. The sleep process won't die from SIGTERM,
	// so the kill command should time out and exit with code 3.
	result := env.runCLI(t,
		"--user-id", userID, "--device-id", deviceID,
		"kill", "--timeout", "2s",
	)

	elapsed := time.Since(start)

	// Exit code 3 = timeout (D-039).
	requireExitCode(t, result, 3)

	// Verify the timeout was approximately 2 seconds (not the default 5).
	assert.Greater(t, elapsed, 1500*time.Millisecond,
		"should wait at least ~2 seconds")
	assert.Less(t, elapsed, 4500*time.Millisecond,
		"should not wait for the default 5 seconds")
}

// ---------------------------------------------------------------------------
// TestMultiDBIsolation — CLI-E2E-254
// ---------------------------------------------------------------------------

// TestMultiDBIsolation verifies that different user_id daemons use separate
// local database files that do not interfere with each other.
//
// Scenario: CLI-E2E-254 — multi-daemon local DB isolation.
func TestMultiDBIsolation(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	user1 := fmt.Sprintf("alice-e2e-%s", t.Name())
	user2 := fmt.Sprintf("bob-e2e-%s", t.Name())
	peer := fmt.Sprintf("peer-e2e-%s", t.Name())
	deviceID := "dev1"

	convID1 := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	now := time.Now()

	// Start both daemons briefly to create their DBs.
	dp1 := env.startDaemon(t, user1, deviceID)
	dp2 := env.startDaemon(t, user2, deviceID)
	dbPath1 := env.dbPathFor(user1, deviceID)
	dbPath2 := env.dbPathFor(user2, deviceID)

	requireStopDaemon(t, dp1)
	requireStopDaemon(t, dp2)

	// Seed data into user1's DB only.
	seedLocalDBFull(t, dbPath1,
		[]*clientmodel.Conversation{
			{
				ID: convID1, UserID1: user1, UserID2: peer,
				Type: "1-on-1", LastProcessedMessageID: 0,
				CreatedAt: now, UpdatedAt: now, LastMessageAt: now,
			},
		},
		nil,
	)

	// User1's list should show the conversation.
	result1 := env.runCLI(t,
		"--user-id", user1, "--device-id", deviceID,
		"list-conversations",
	)
	requireExitCode(t, result1, 0)
	assert.Contains(t, result1.Stdout, convID1,
		"user1 should see their conversation")

	// User2's list should be empty (data isolated).
	result2 := env.runCLI(t,
		"--user-id", user2, "--device-id", deviceID,
		"list-conversations",
	)
	requireExitCode(t, result2, 0)
	assert.Contains(t, result2.Stdout, "No conversations found",
		"user2 should not see user1's data")

	// Verify both DB files exist but are independent.
	assertFileExists(t, dbPath1)
	assertFileExists(t, dbPath2)
}

// ---------------------------------------------------------------------------
// TestErrorHandling — CLI-E2E-262~263
// ---------------------------------------------------------------------------

// TestErrorHandling verifies error paths for the CLI: both IPC and WS failing
// simultaneously, and an invalid DB path.
//
// Scenarios: CLI-E2E-262 (IPC+WS both fail), CLI-E2E-263 (DB path error).
func TestErrorHandling(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	userID := fmt.Sprintf("alice-e2e-%s", t.Name())
	deviceID := "dev1"

	// CLI-E2E-262: IPC and WS both fail. No daemon running (IPC fails) and
	// an unreachable server URL (WS fallback fails).
	t.Run("ipc_and_ws_both_fail", func(t *testing.T) {
		ensureDir(t, env.userDir(userID, deviceID))

		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"--server", "ws://localhost:19999/ws",
			"send", "-c", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			"-m", "should fail",
		)
		requireExitCode(t, result, 1)
		// Stderr should contain error information about both failures.
		assert.True(t, len(result.Stderr) > 0,
			"stderr should contain error details, got: %s", result.Stderr)
	})

	// CLI-E2E-263: DB path does not exist. Use a path where the parent
	// directory does not exist, forcing store.New() to fail.
	t.Run("db_path_not_exist", func(t *testing.T) {
		badDBPath := "/nonexistent_dir_e2e_test_12345/subdir/xyncra.db"
		result := env.runCLI(t,
			"--user-id", userID, "--device-id", deviceID,
			"--db-path", badDBPath,
			"list-conversations",
		)
		requireExitCode(t, result, 1)
		assert.True(t, len(result.Stderr) > 0,
			"stderr should contain DB error, got: %s", result.Stderr)
	})
}

// ---------------------------------------------------------------------------
// Helpers for P1 second-half tests
// ---------------------------------------------------------------------------
