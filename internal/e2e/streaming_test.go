// Package e2e_test contains E2E tests for the ephemeral streaming text feature
// (D-051). Tests verify that streaming events are broadcast to online
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
// TestStreaming_OnlineRecipientReceivesEvent
// Verifies: D-050 (ephemeral push to all members), D-051 (Seq=0, Type=streaming)
// ---------------------------------------------------------------------------

// TestStreaming_OnlineRecipientReceivesEvent verifies that when Alice sends a
// stream_text event, Bob (online) receives a push update with Seq=0 and
// Type="streaming", and Alice also receives the push (D-050).
func TestStreaming_OnlineRecipientReceivesEvent(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Alice sends stream_text.
	sendRequest(t, aliceConn, "req-stream-1", "stream_text", map[string]interface{}{
		"conversation_id": conv.ID,
		"stream_id":       "stream-1",
		"text":            "hello",
		"is_done":         false,
	})

	// Alice receives streaming push first (broadcast arrives before response).
	aliceUpdates := waitForUpdate(t, aliceConn, 5*time.Second)
	require.Len(t, aliceUpdates.Updates, 1)
	assert.Equal(t, uint32(0), aliceUpdates.Updates[0].Seq, "streaming should be Seq=0")
	assert.Equal(t, protocol.UpdateTypeStreaming, aliceUpdates.Updates[0].Type)

	var alicePayload struct {
		StreamID       string `json:"stream_id"`
		UserID         string `json:"user_id"`
		ConversationID string `json:"conversation_id"`
		Text           string `json:"text"`
		IsDone         bool   `json:"is_done"`
	}
	require.NoError(t, json.Unmarshal(aliceUpdates.Updates[0].Payload, &alicePayload))
	assert.Equal(t, "stream-1", alicePayload.StreamID)
	assert.Equal(t, "alice", alicePayload.UserID)
	assert.Equal(t, conv.ID, alicePayload.ConversationID)
	assert.Equal(t, "hello", alicePayload.Text)
	assert.False(t, alicePayload.IsDone)

	// Alice also receives response(status=ok).
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-stream-1", resp.ID)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	var respData struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &respData))
	assert.Equal(t, "ok", respData.Status)

	// Bob also receives streaming push.
	bobUpdates := waitForUpdate(t, bobConn, 5*time.Second)
	require.Len(t, bobUpdates.Updates, 1)
	assert.Equal(t, uint32(0), bobUpdates.Updates[0].Seq, "streaming should be Seq=0")
	assert.Equal(t, protocol.UpdateTypeStreaming, bobUpdates.Updates[0].Type)

	var bobPayload struct {
		StreamID       string `json:"stream_id"`
		UserID         string `json:"user_id"`
		ConversationID string `json:"conversation_id"`
		Text           string `json:"text"`
		IsDone         bool   `json:"is_done"`
	}
	require.NoError(t, json.Unmarshal(bobUpdates.Updates[0].Payload, &bobPayload))
	assert.Equal(t, "stream-1", bobPayload.StreamID)
	assert.Equal(t, "alice", bobPayload.UserID)
	assert.Equal(t, conv.ID, bobPayload.ConversationID)
	assert.Equal(t, "hello", bobPayload.Text)
	assert.False(t, bobPayload.IsDone)
}

// ---------------------------------------------------------------------------
// TestStreaming_OfflineRecipientDoesNotReceive
// Verifies: D-050 (offline recipients silently skipped, no error)
// ---------------------------------------------------------------------------

// TestStreaming_OfflineRecipientDoesNotReceive verifies that when Alice sends a
// stream_text event and Bob is offline, Alice still gets the response but no
// exception occurs (fire-and-forget for offline users).
func TestStreaming_OfflineRecipientDoesNotReceive(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()
	// Bob does NOT connect.

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Alice sends stream_text.
	sendRequest(t, aliceConn, "req-stream-2", "stream_text", map[string]interface{}{
		"conversation_id": conv.ID,
		"stream_id":       "stream-2",
		"text":            "hello offline",
		"is_done":         false,
	})

	// Alice receives her own streaming push (D-050).
	_ = waitForUpdate(t, aliceConn, 5*time.Second)

	// Alice also receives response(status=ok).
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-stream-2", resp.ID)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)
}

// ---------------------------------------------------------------------------
// TestStreaming_SyncUpdatesDoesNotReturnStreaming
// Verifies: D-051 (streaming is Seq=0, never persisted, never returned by sync)
// ---------------------------------------------------------------------------

// TestStreaming_SyncUpdatesDoesNotReturnStreaming verifies that streaming
// updates do not appear in sync_updates responses (they are ephemeral).
func TestStreaming_SyncUpdatesDoesNotReturnStreaming(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Alice sends stream_text.
	sendRequest(t, aliceConn, "req-stream-3", "stream_text", map[string]interface{}{
		"conversation_id": conv.ID,
		"stream_id":       "stream-3",
		"text":            "ephemeral",
		"is_done":         false,
	})

	// Consume alice's push (broadcast includes caller per D-050).
	_ = waitForUpdate(t, aliceConn, 5*time.Second)

	// Consume alice's response.
	_ = readResponse(t, aliceConn, 5*time.Second)

	// Consume bob's streaming push.
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

	// Verify no streaming updates in sync response.
	for _, u := range syncData.Updates {
		assert.NotEqual(t, protocol.UpdateTypeStreaming, u.Type,
			"sync_updates should not contain streaming updates (D-051)")
	}
}

// ---------------------------------------------------------------------------
// TestStreaming_NonMemberRejected
// Verifies: C-3 (permission check), non-member cannot send streaming
// ---------------------------------------------------------------------------

// TestStreaming_NonMemberRejected verifies that a user who is not a member of
// a conversation cannot send stream_text events to it.
func TestStreaming_NonMemberRejected(t *testing.T) {
	env := setupE2ETest(t)

	eveConn := connectClient(t, env.addr, "eve")
	defer eveConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Eve sends stream_text to alice+bob conversation.
	sendRequest(t, eveConn, "req-stream-4", "stream_text", map[string]interface{}{
		"conversation_id": conv.ID,
		"stream_id":       "stream-4",
		"text":            "intruder",
		"is_done":         false,
	})

	resp := readResponse(t, eveConn, 5*time.Second)
	require.Equal(t, "req-stream-4", resp.ID)
	assert.Equal(t, protocol.ResponseCodePermissionDenied, resp.Code,
		"non-member stream_text should be rejected")
}

// ---------------------------------------------------------------------------
// TestStreaming_NoDBSideEffects
// Verifies: D-051 (streaming never persists to DB: no UserUpdate, no Message)
// ---------------------------------------------------------------------------

// TestStreaming_NoDBSideEffects verifies that sending a stream_text event does
// not create any records in the UserUpdate or Message tables.
func TestStreaming_NoDBSideEffects(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Record baseline counts before streaming.
	ctx := context.Background()
	var updateCountBefore int64
	env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).
		Where("user_id IN ?", []string{"alice", "bob"}).
		Count(&updateCountBefore)

	var msgCountBefore int64
	env.db.DB().WithContext(ctx).Model(&model.Message{}).
		Where("conversation_id = ?", conv.ID).
		Count(&msgCountBefore)

	// Alice sends stream_text.
	sendRequest(t, aliceConn, "req-stream-5", "stream_text", map[string]interface{}{
		"conversation_id": conv.ID,
		"stream_id":       "stream-5",
		"text":            "no side effects",
		"is_done":         false,
	})

	// Consume alice's push (broadcast includes caller per D-050).
	_ = waitForUpdate(t, aliceConn, 5*time.Second)

	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-stream-5", resp.ID)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Verify no new UserUpdate records.
	var updateCountAfter int64
	env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).
		Where("user_id IN ?", []string{"alice", "bob"}).
		Count(&updateCountAfter)
	assert.Equal(t, updateCountBefore, updateCountAfter,
		"stream_text should not create UserUpdate records (D-051)")

	// Verify no new Message records.
	var msgCountAfter int64
	env.db.DB().WithContext(ctx).Model(&model.Message{}).
		Where("conversation_id = ?", conv.ID).
		Count(&msgCountAfter)
	assert.Equal(t, msgCountBefore, msgCountAfter,
		"stream_text should not create Message records (D-051)")
}

// ---------------------------------------------------------------------------
// TestStreaming_IsDoneFollowedBySendMessage
// Verifies: D-052 (is_done=true followed by send_message)
// ---------------------------------------------------------------------------

// TestStreaming_IsDoneFollowedBySendMessage verifies the two-step protocol:
// first broadcast is_done=true via stream_text, then the caller sends
// send_message to persist the final text. This test verifies that:
// 1. The is_done streaming frame is broadcast to all members.
// 2. The send_message call succeeds after streaming completes.
func TestStreaming_IsDoneFollowedBySendMessage(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Step 1: Alice sends stream_text with is_done=true.
	sendRequest(t, aliceConn, "req-stream-6a", "stream_text", map[string]interface{}{
		"conversation_id": conv.ID,
		"stream_id":       "stream-6",
		"text":            "final text",
		"is_done":         true,
	})

	// Consume alice's streaming push.
	aliceStreamUpdates := waitForUpdate(t, aliceConn, 5*time.Second)
	require.Len(t, aliceStreamUpdates.Updates, 1)
	assert.Equal(t, protocol.UpdateTypeStreaming, aliceStreamUpdates.Updates[0].Type)

	// Consume alice's response.
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-stream-6a", resp.ID)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Consume bob's streaming push.
	bobStreamUpdates := waitForUpdate(t, bobConn, 5*time.Second)
	require.Len(t, bobStreamUpdates.Updates, 1)
	assert.Equal(t, protocol.UpdateTypeStreaming, bobStreamUpdates.Updates[0].Type)

	var isDonePayload struct {
		IsDone bool   `json:"is_done"`
		Text   string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(bobStreamUpdates.Updates[0].Payload, &isDonePayload))
	assert.True(t, isDonePayload.IsDone, "is_done should be true")
	assert.Equal(t, "final text", isDonePayload.Text)

	// Step 2: Alice sends send_message to persist the final text.
	// Verify the response is successful (the message push delivery is
	// already tested by TestBasicMessageDelivery).
	clientMsgID := "cmid-stream-done"
	sendRequest(t, aliceConn, "req-send-6b", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "final text",
		"type":              "text",
	})

	sendResp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-send-6b", sendResp.ID)
	require.Equal(t, protocol.ResponseCodeOK, sendResp.Code)

	// Verify the response contains the message.
	var sendData struct {
		Message *struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"message"`
	}
	require.NoError(t, json.Unmarshal(sendResp.Data, &sendData))
	require.NotNil(t, sendData.Message)
	assert.Equal(t, "final text", sendData.Message.Content)
}

// ---------------------------------------------------------------------------
// TestStreaming_SenderAlsoReceivesPush
// Verifies: D-050 (sender also receives ephemeral push)
// ---------------------------------------------------------------------------

// TestStreaming_SenderAlsoReceivesPush verifies that Alice (the sender) also
// receives a streaming push update (D-050: broadcast to ALL members).
func TestStreaming_SenderAlsoReceivesPush(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Alice sends stream_text.
	sendRequest(t, aliceConn, "req-stream-7", "stream_text", map[string]interface{}{
		"conversation_id": conv.ID,
		"stream_id":       "stream-7",
		"text":            "echo",
		"is_done":         false,
	})

	// Alice receives streaming push first (D-050).
	aliceUpdates := waitForUpdate(t, aliceConn, 5*time.Second)
	require.Len(t, aliceUpdates.Updates, 1)
	assert.Equal(t, uint32(0), aliceUpdates.Updates[0].Seq, "streaming should be Seq=0")
	assert.Equal(t, protocol.UpdateTypeStreaming, aliceUpdates.Updates[0].Type)

	var alicePayload struct {
		UserID string `json:"user_id"`
		Text   string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(aliceUpdates.Updates[0].Payload, &alicePayload))
	assert.Equal(t, "alice", alicePayload.UserID, "push should contain sender's user_id")
	assert.Equal(t, "echo", alicePayload.Text)

	// Alice also receives the response.
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-stream-7", resp.ID)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code)

	// Bob also receives the push.
	bobUpdates := waitForUpdate(t, bobConn, 5*time.Second)
	require.Len(t, bobUpdates.Updates, 1)
	assert.Equal(t, protocol.UpdateTypeStreaming, bobUpdates.Updates[0].Type)
}
