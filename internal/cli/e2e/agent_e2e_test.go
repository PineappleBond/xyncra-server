package cli_e2e_test

// Agent E2E tests for the xyncra-client CLI (end-to-end).
//
// These tests exercise the agent interaction path against a real Redis,
// WebSocket server, and an agent-aware server deployment. Because agent
// support requires a configured LLM API key (DASHSCOPE_API_KEY), most
// agent tests skip when the key is unavailable.
//
// Tests MUST NOT run in parallel (shared Redis and Server instances).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientstore "github.com/PineappleBond/xyncra-server/pkg/store"
)

// ---------------------------------------------------------------------------
// TestAgentE2E_FullFlow — AE-001
// ---------------------------------------------------------------------------

// TestAgentE2E_FullFlow tests the complete agent interaction flow:
//
//	human sends message → agent typing → agent streaming → agent reply persisted.
//
// Prerequisites:
//   - Redis at localhost:16379
//   - Server at ws://localhost:18080/ws with agent support enabled
//   - DASHSCOPE_API_KEY (or compatible LLM) configured for the weather-bot agent
//
// If DASHSCOPE_API_KEY is not set, the test is skipped.
func TestAgentE2E_FullFlow(t *testing.T) {
	if os.Getenv("DASHSCOPE_API_KEY") == "" {
		t.Skip("DASHSCOPE_API_KEY not set, skipping agent E2E test")
	}

	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	humanID := uniqueUserID("human")
	deviceID := "dev1"

	// Start daemon for the human user.
	dp := env.startDaemon(t, humanID, deviceID)
	defer requireStopDaemon(t, dp)

	agentID := "agent/weather-bot"

	// Create conversation with the agent.
	createResult := env.runCLI(t,
		"--user-id", humanID, "--device-id", deviceID,
		"create-conversation", "--peer-id", agentID,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")

	// Send a message to the agent.
	sendResult := env.runCLI(t,
		"--user-id", humanID, "--device-id", deviceID,
		"send", "--conversation-id", convID, "--content", "Hello, what can you do?",
	)
	requireExitCode(t, sendResult, 0)

	// Wait for agent reply to appear in local DB via sync_updates.
	dbPath := env.dbPathFor(humanID, deviceID)
	waitForSync(t, dbPath, 120*time.Second, func(db *clientstore.ClientDB) bool {
		ctx := context.Background()
		msgs, err := db.Messages.ListByConversation(ctx, convID, 0, 100)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.SenderID == agentID {
				return true
			}
		}
		return false
	})

	t.Log("Agent E2E: received reply from agent")
}

// ---------------------------------------------------------------------------
// TestAgentE2E_NonAgentUnaffected — regression
// ---------------------------------------------------------------------------

// TestAgentE2E_NonAgentUnaffected verifies that regular human-to-human
// messaging continues to work correctly and is not affected by the agent
// code path. This test does NOT require DASHSCOPE_API_KEY.
func TestAgentE2E_NonAgentUnaffected(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	deviceID := "dev1"

	dpAlice := env.startDaemon(t, alice, deviceID)
	defer requireStopDaemon(t, dpAlice)

	dpBob := env.startDaemon(t, bob, deviceID)
	defer requireStopDaemon(t, dpBob)

	// Create conversation between two humans.
	createResult := env.runCLI(t,
		"--user-id", alice, "--device-id", deviceID,
		"create-conversation", "--peer-id", bob,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")

	// Send message from alice to bob.
	sendResult := env.runCLI(t,
		"--user-id", alice, "--device-id", deviceID,
		"send", "--conversation-id", convID, "--content", "Hi Bob!",
	)
	requireExitCode(t, sendResult, 0)

	// Wait for bob to receive the message in his local DB.
	dbPathBob := env.dbPathFor(bob, deviceID)
	waitForSync(t, dbPathBob, 15*time.Second, func(db *clientstore.ClientDB) bool {
		ctx := context.Background()
		msgs, err := db.Messages.ListByConversation(ctx, convID, 0, 100)
		return err == nil && len(msgs) > 0
	})

	// Verify alice's own message also synced to her local DB.
	dbPathAlice := env.dbPathFor(alice, deviceID)
	waitForSync(t, dbPathAlice, 15*time.Second, func(db *clientstore.ClientDB) bool {
		ctx := context.Background()
		msgs, err := db.Messages.ListByConversation(ctx, convID, 0, 100)
		return err == nil && len(msgs) > 0
	})
}

// ---------------------------------------------------------------------------
// TestAgentE2E_ConversationWithAgentSynced — AE-003
// ---------------------------------------------------------------------------

// TestAgentE2E_ConversationWithAgentSynced verifies that creating a
// conversation with an agent user and sending a message results in the
// conversation being synced to the local DB, even if the agent reply
// never arrives (i.e. the server is running but has no LLM API key).
//
// This test does NOT require DASHSCOPE_API_KEY.
func TestAgentE2E_ConversationWithAgentSynced(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	humanID := uniqueUserID("human")
	deviceID := "dev1"

	dp := env.startDaemon(t, humanID, deviceID)
	defer requireStopDaemon(t, dp)

	agentID := "agent/weather-bot"

	// Create conversation with the agent.
	createResult := env.runCLI(t,
		"--user-id", humanID, "--device-id", deviceID,
		"create-conversation", "--peer-id", agentID,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")

	// Wait for the conversation to sync to the local DB.
	dbPath := env.dbPathFor(humanID, deviceID)
	waitForSync(t, dbPath, 15*time.Second, func(db *clientstore.ClientDB) bool {
		ctx := context.Background()
		convs, err := db.Conversations.GetByUser(ctx, humanID, 0, 10)
		if err != nil || len(convs) == 0 {
			return false
		}
		// Verify the conversation with the agent exists.
		for _, c := range convs {
			if c.ID == convID {
				return true
			}
		}
		return false
	})

	// Send a message to the agent (may not be processed by an actual LLM
	// but it should at least be accepted by the server).
	sendResult := env.runCLI(t,
		"--user-id", humanID, "--device-id", deviceID,
		"send", "--conversation-id", convID, "--content", "Hello?",
	)
	requireExitCode(t, sendResult, 0)

	// Verify the user's own message is synced to the local DB.
	waitForSync(t, dbPath, 15*time.Second, func(db *clientstore.ClientDB) bool {
		ctx := context.Background()
		msgs, err := db.Messages.ListByConversation(ctx, convID, 0, 100)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.SenderID == humanID {
				return true
			}
		}
		return false
	})
}

// ---------------------------------------------------------------------------
// TestAgentE2E_DaemonDBPath — auxiliary
// ---------------------------------------------------------------------------

// TestAgentE2E_DaemonDBPath verifies that the daemon's local DB path is
// constructed correctly for a human user when interacting with an agent.
// This is a sanity check on the environment setup, not on agent behaviour.
func TestAgentE2E_DaemonDBPath(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	humanID := uniqueUserID("human")
	deviceID := "dev1"

	dp := env.startDaemon(t, humanID, deviceID)
	defer requireStopDaemon(t, dp)

	// Verify that the DB path helper returns the expected location.
	expectedDBPath := filepath.Join(env.homeDir, ".xyncra", humanID, deviceID, "xyncra.db")
	actualDBPath := env.dbPathFor(humanID, deviceID)
	assert.Equal(t, expectedDBPath, actualDBPath,
		"dbPathFor should return the expected path")

	// The DB file should exist after the daemon starts.
	assertFileExists(t, actualDBPath)
}

// ---------------------------------------------------------------------------
// TestAgentE2E_AgentPrefixInConversation — AE-004
// ---------------------------------------------------------------------------

// TestAgentE2E_AgentPrefixInConversation verifies that a conversation with
// an agent peer is created successfully and that the agent prefix is
// preserved in the conversation state.
func TestAgentE2E_AgentPrefixInConversation(t *testing.T) {
	shared := setupCliE2E(t)
	env := newPerTestEnv(t, shared)

	humanID := uniqueUserID("human")
	deviceID := "dev1"

	dp := env.startDaemon(t, humanID, deviceID)
	defer requireStopDaemon(t, dp)

	agentID := "agent/weather-bot"

	// Create conversation with the agent.
	createResult := env.runCLI(t,
		"--user-id", humanID, "--device-id", deviceID,
		"create-conversation", "--peer-id", agentID,
	)
	requireExitCode(t, createResult, 0)

	// The stdout should contain the agent peer ID.
	assert.Contains(t, createResult.Stdout, agentID,
		"create-conversation output should mention the agent peer ID")

	// List conversations via IPC and verify the agent conversation is present.
	resp := ipcCall(t, dp.socketPath, "list_conversations", map[string]any{
		"offset": 0,
		"limit":  10,
	})
	require.Nil(t, resp.Error, "list_conversations IPC should succeed")

	// Parse the result to verify the agent conversation exists.
	var result struct {
		Conversations []struct {
			ID   string `json:"id"`
			Peer string `json:"peer_id"`
		} `json:"conversations"`
	}
	require.NoError(t, decodeJSONRaw(resp.Result, &result),
		"should decode list_conversations result")

	found := false
	for _, c := range result.Conversations {
		if c.Peer == agentID {
			found = true
			break
		}
	}
	assert.True(t, found, "list_conversations should include a conversation with agent peer %s", agentID)
}

// ---------------------------------------------------------------------------
// decodeJSONRaw — JSON helper
// ---------------------------------------------------------------------------

// decodeJSONRaw unmarshals a json.RawMessage into the given target.
// It is a thin wrapper used to keep test assertions readable.
func decodeJSONRaw(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty JSON payload")
	}
	return json.Unmarshal(raw, target)
}
