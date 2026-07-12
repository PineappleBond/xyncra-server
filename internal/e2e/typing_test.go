// Package e2e_test contains E2E tests for the ephemeral typing indicator
// feature (D-050). Tests verify that typing events are broadcast to online
// recipients with Seq=0 (never persisted), never returned via sync_updates,
// and rejected for non-members.
package e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// TestTyping_OnlineRecipientReceivesEvent
// Verifies: D-050 (ephemeral push to online members), Seq=0, Type="typing"
// ---------------------------------------------------------------------------

// TestTyping_OnlineRecipientReceivesEvent verifies that when Alice sends a
// typing indicator, both Bob (online) and Alice (the sender) receive a push
// update with Seq=0 and Type="typing" (D-050: broadcast to ALL members).
func TestTyping_OnlineRecipientReceivesEvent(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Alice sends set_typing(is_typing=true).
	sendRequest(t, aliceConn, "req-typing-1", "set_typing", map[string]interface{}{
		"conversation_id": conv.ID,
		"is_typing":       true,
	})

	// Alice receives typing push first (broadcast arrives before response, D-050).
	aliceUpdates := waitForUpdate(t, aliceConn, 5*time.Second)
	require.Len(t, aliceUpdates.Updates, 1)
	assert.Equal(t, uint32(0), aliceUpdates.Updates[0].Seq, "typing should be Seq=0")
	assert.Equal(t, protocol.UpdateTypeTyping, aliceUpdates.Updates[0].Type)

	var alicePayload struct {
		UserID         string `json:"user_id"`
		ConversationID string `json:"conversation_id"`
		IsTyping       bool   `json:"is_typing"`
	}
	require.NoError(t, json.Unmarshal(aliceUpdates.Updates[0].Payload, &alicePayload))
	assert.Equal(t, "alice", alicePayload.UserID, "push should contain sender's user_id")
	assert.Equal(t, conv.ID, alicePayload.ConversationID)
	assert.True(t, alicePayload.IsTyping)

	// Alice also receives response(status=ok).
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-typing-1", resp.ID)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	var respData struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &respData))
	assert.Equal(t, "ok", respData.Status)

	// Bob also receives typing push (D-050: broadcast to ALL members).
	bobUpdates := waitForUpdate(t, bobConn, 5*time.Second)
	require.Len(t, bobUpdates.Updates, 1)
	assert.Equal(t, uint32(0), bobUpdates.Updates[0].Seq, "typing should be Seq=0")
	assert.Equal(t, protocol.UpdateTypeTyping, bobUpdates.Updates[0].Type, "update type should be 'typing'")

	var payload struct {
		UserID         string `json:"user_id"`
		ConversationID string `json:"conversation_id"`
		IsTyping       bool   `json:"is_typing"`
	}
	require.NoError(t, json.Unmarshal(bobUpdates.Updates[0].Payload, &payload))
	assert.Equal(t, "alice", payload.UserID)
	assert.Equal(t, conv.ID, payload.ConversationID)
	assert.True(t, payload.IsTyping)
}

// ---------------------------------------------------------------------------
// TestTyping_OfflineRecipientDoesNotReceive
// Verifies: D-050 (offline recipients silently skipped, no error)
// ---------------------------------------------------------------------------

// TestTyping_OfflineRecipientDoesNotReceive verifies that when Alice sends a
// typing indicator and Bob is offline, Alice still gets her own push (D-050)
// and the response, with no error (fire-and-forget for offline users).
func TestTyping_OfflineRecipientDoesNotReceive(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	// Bob does NOT connect.

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Alice sends set_typing.
	sendRequest(t, aliceConn, "req-typing-2", "set_typing", map[string]interface{}{
		"conversation_id": conv.ID,
		"is_typing":       true,
	})

	// Alice receives her own typing push (D-050: broadcast to ALL members).
	aliceUpdates := waitForUpdate(t, aliceConn, 5*time.Second)
	require.Len(t, aliceUpdates.Updates, 1)
	assert.Equal(t, uint32(0), aliceUpdates.Updates[0].Seq, "typing should be Seq=0")
	assert.Equal(t, protocol.UpdateTypeTyping, aliceUpdates.Updates[0].Type)

	// Alice also receives response(status=ok).
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-typing-2", resp.ID)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)
}

// ---------------------------------------------------------------------------
// TestTyping_SyncUpdatesDoesNotReturnTyping
// Verifies: D-050 (typing is Seq=0, never persisted, never returned by sync)
// ---------------------------------------------------------------------------

// TestTyping_SyncUpdatesDoesNotReturnTyping verifies that typing updates do
// not appear in sync_updates responses (they are ephemeral, never persisted).
func TestTyping_SyncUpdatesDoesNotReturnTyping(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Alice sends set_typing.
	sendRequest(t, aliceConn, "req-typing-3", "set_typing", map[string]interface{}{
		"conversation_id": conv.ID,
		"is_typing":       true,
	})

	// Alice gets response.
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Consume bob's typing push.
	_ = waitForUpdate(t, bobConn, 5*time.Second)

	// Bob calls sync_updates(after_seq=0).
	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})

	syncResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code)

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))

	// Verify no typing updates in sync response.
	for _, u := range syncData.Updates {
		assert.NotEqual(t, protocol.UpdateTypeTyping, u.Type,
			"sync_updates should not contain typing updates (D-050)")
	}
}

// ---------------------------------------------------------------------------
// TestTyping_NonMemberRejected
// Verifies: C-3 (permission check), non-member cannot send typing
// ---------------------------------------------------------------------------

// TestTyping_NonMemberRejected verifies that a user who is not a member of a
// conversation cannot send typing indicators to it.
func TestTyping_NonMemberRejected(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	eveConn := connectClient(t, env.addr, "eve", "eve")
	defer eveConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Eve sends set_typing to alice+bob conversation.
	sendRequest(t, eveConn, "req-typing-4", "set_typing", map[string]interface{}{
		"conversation_id": conv.ID,
		"is_typing":       true,
	})

	resp := readResponse(t, eveConn, 5*time.Second)
	require.Equal(t, "req-typing-4", resp.ID)
	assert.Equal(t, protocol.ResponseCodePermissionDenied, resp.Code,
		"non-member set_typing should be rejected")
}

// ---------------------------------------------------------------------------
// TestTyping_StopTypingEvent
// Verifies: D-050 (is_typing=false propagated correctly)
// ---------------------------------------------------------------------------

// TestTyping_StopTypingEvent verifies that sending set_typing(is_typing=false)
// results in a push with is_typing=false in the payload.
func TestTyping_StopTypingEvent(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Alice sends set_typing(is_typing=false).
	sendRequest(t, aliceConn, "req-typing-5", "set_typing", map[string]interface{}{
		"conversation_id": conv.ID,
		"is_typing":       false,
	})

	// Alice gets response.
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-typing-5", resp.ID)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Bob receives push.
	bobUpdates := waitForUpdate(t, bobConn, 5*time.Second)
	require.Len(t, bobUpdates.Updates, 1)
	assert.Equal(t, uint32(0), bobUpdates.Updates[0].Seq, "typing should be Seq=0")
	assert.Equal(t, protocol.UpdateTypeTyping, bobUpdates.Updates[0].Type)

	var payload struct {
		UserID         string `json:"user_id"`
		ConversationID string `json:"conversation_id"`
		IsTyping       bool   `json:"is_typing"`
	}
	require.NoError(t, json.Unmarshal(bobUpdates.Updates[0].Payload, &payload))
	assert.Equal(t, "alice", payload.UserID)
	assert.Equal(t, conv.ID, payload.ConversationID)
	assert.False(t, payload.IsTyping, "is_typing should be false")
}

// ---------------------------------------------------------------------------
// TestTyping_NoDBSideEffects
// Verifies: D-050 (typing never persists to DB: no UserUpdate, no Message)
// ---------------------------------------------------------------------------

// TestTyping_NoDBSideEffects verifies that sending a typing indicator does not
// create any records in the UserUpdate or Message tables.
func TestTyping_NoDBSideEffects(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Record baseline counts before typing.
	ctx := context.Background()
	var updateCountBefore int64
	env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).
		Where("user_id IN ?", []string{"alice", "bob"}).
		Count(&updateCountBefore)

	var msgCountBefore int64
	env.db.DB().WithContext(ctx).Model(&model.Message{}).
		Where("conversation_id = ?", conv.ID).
		Count(&msgCountBefore)

	// Alice sends set_typing.
	sendRequest(t, aliceConn, "req-typing-6", "set_typing", map[string]interface{}{
		"conversation_id": conv.ID,
		"is_typing":       true,
	})

	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-typing-6", resp.ID)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Verify no new UserUpdate records.
	var updateCountAfter int64
	env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).
		Where("user_id IN ?", []string{"alice", "bob"}).
		Count(&updateCountAfter)
	assert.Equal(t, updateCountBefore, updateCountAfter,
		"typing should not create UserUpdate records (D-050)")

	// Verify no new Message records.
	var msgCountAfter int64
	env.db.DB().WithContext(ctx).Model(&model.Message{}).
		Where("conversation_id = ?", conv.ID).
		Count(&msgCountAfter)
	assert.Equal(t, msgCountBefore, msgCountAfter,
		"typing should not create Message records (D-050)")
}
