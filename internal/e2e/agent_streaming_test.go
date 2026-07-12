// Package e2e_test contains Category B streaming output E2E tests for the
// Agent system. Tests verify that typing indicators, streaming text updates,
// is_done signals, and message persistence follow the correct protocol
// (D-050, D-051, D-052, D-065).
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// directMsgCounter is an atomic counter for generating unique message IDs
// in insertUserMessageDirect, replacing UnixNano to prevent ID collisions
// under rapid goroutine scheduling (AE-EDGE-008, AE-EDGE-010).
var directMsgCounter int64

// insertUserMessageDirect inserts a user message into the database WITHOUT
// going through the send_message RPC handler, avoiding the automatic MQ
// agent_process task (D-063).
func insertUserMessageDirect(t *testing.T, env *agentE2EEnv, userID, convID, content string) *model.Message {
	t.Helper()
	msg := &model.Message{
		ID:              fmt.Sprintf("msg-direct-%d", atomic.AddInt64(&directMsgCounter, 1)),
		ClientMessageID: fmt.Sprintf("cmid-direct-%d", atomic.AddInt64(&directMsgCounter, 1)),
		ConversationID:  convID,
		SenderID:        userID,
		Content:         content,
		Type:            "text",
		Status:          "sent",
		CreatedAt:       time.Now(),
	}
	_, err := env.store.SendMessage(context.Background(), msg, []string{userID, "agent/test-bot"})
	require.NoError(t, err, "insert user message should succeed")
	return msg
}

// parseEventsFromRaw parses raw WebSocket data and appends event labels.
func parseEventsFromRaw(t *testing.T, data []byte, agentUserID string, events *[]string) {
	t.Helper()
	var pkg struct {
		Type int             `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &pkg) != nil || pkg.Type != int(protocol.PackageTypeUpdates) {
		return
	}
	var updates struct {
		Updates []struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		} `json:"updates"`
	}
	if json.Unmarshal(pkg.Data, &updates) != nil {
		return
	}
	for _, u := range updates.Updates {
		switch u.Type {
		case protocol.UpdateTypeTyping:
			var p struct {
				IsTyping bool `json:"is_typing"`
			}
			if json.Unmarshal(u.Payload, &p) == nil {
				if p.IsTyping {
					*events = append(*events, "typing_start")
				} else {
					*events = append(*events, "typing_stop")
				}
			}
		case protocol.UpdateTypeStreaming:
			var p struct {
				UserID string `json:"user_id"`
				Text   string `json:"text"`
			}
			if json.Unmarshal(u.Payload, &p) == nil && p.UserID == agentUserID && p.Text != "" {
				*events = append(*events, "streaming")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TestAgentStream_AE_STREAM_001 — Typing indicator before first token (D-050, D-065)
// ---------------------------------------------------------------------------
func TestAgentStream_AE_STREAM_001(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-stream-001"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	execDone := make(chan struct{})
	go func() {
		defer close(execDone)
		_ = triggerAgentProcessing(t, env, "msg-001", conv.ID, agentUserID, userID)
	}()

	updates := waitForEphemeral(t, conn, protocol.UpdateTypeTyping, 30*time.Second)
	var found bool
	for _, u := range updates.Updates {
		if u.Type != protocol.UpdateTypeTyping {
			continue
		}
		found = true
		assert.Equal(t, uint32(0), u.Seq, "typing should be Seq=0")
		var p struct {
			UserID   string `json:"user_id"`
			IsTyping bool   `json:"is_typing"`
		}
		require.NoError(t, json.Unmarshal(u.Payload, &p))
		assert.Equal(t, agentUserID, p.UserID)
		assert.True(t, p.IsTyping, "is_typing should be true (D-065)")
	}
	assert.True(t, found, "should find a typing update")
	<-execDone // wait for executor to finish before cleanup
}

// ---------------------------------------------------------------------------
// TestAgentStream_AE_STREAM_002 — Streaming tokens cumulative (D-050, D-051)
// ---------------------------------------------------------------------------
func TestAgentStream_AE_STREAM_002(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-stream-002"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	execDone := make(chan struct{})
	go func() {
		defer close(execDone)
		_ = triggerAgentProcessing(t, env, "msg-002", conv.ID, agentUserID, userID)
	}()

	var texts []string
	deadline := time.Now().Add(30 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out, got %d texts", len(texts))
		}
		updates := waitForUpdate(t, conn, time.Until(deadline))
		for _, u := range updates.Updates {
			if u.Type != protocol.UpdateTypeStreaming {
				continue
			}
			var p struct {
				UserID string `json:"user_id"`
				Text   string `json:"text"`
				IsDone bool   `json:"is_done"`
			}
			require.NoError(t, json.Unmarshal(u.Payload, &p))
			if p.UserID != agentUserID || p.IsDone {
				if p.IsDone && p.UserID == agentUserID {
					goto collected
				}
				continue
			}
			assert.Equal(t, uint32(0), u.Seq)
			texts = append(texts, p.Text)
		}
	}
collected:
	require.GreaterOrEqual(t, len(texts), 2, "should receive multiple streaming updates (D-051)")
	for i := 1; i < len(texts); i++ {
		assert.GreaterOrEqual(t, len(texts[i]), len(texts[i-1]), "text should be cumulative (D-051)")
	}
	assert.NotEmpty(t, texts[0])
	<-execDone // wait for executor to finish before cleanup
}

// ---------------------------------------------------------------------------
// TestAgentStream_AE_STREAM_003 — is_done flag (D-052)
// ---------------------------------------------------------------------------
func TestAgentStream_AE_STREAM_003(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-stream-003"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	execDone := make(chan struct{})
	go func() {
		defer close(execDone)
		_ = triggerAgentProcessing(t, env, "msg-003", conv.ID, agentUserID, userID)
	}()

	finalText := waitForAgentStreamDone(t, conn, "test-bot", 30*time.Second)
	assert.NotEmpty(t, finalText, "final text should not be empty (D-052)")
	<-execDone // wait for executor to finish before cleanup
}

// ---------------------------------------------------------------------------
// TestAgentStream_AE_STREAM_004 — Typing stops after first token (D-065)
// ---------------------------------------------------------------------------
func TestAgentStream_AE_STREAM_004(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-stream-004"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	execDone := make(chan struct{})
	go func() {
		defer close(execDone)
		_ = triggerAgentProcessing(t, env, "msg-004", conv.ID, agentUserID, userID)
	}()

	// Step 1: Wait for typing=true.
	typingStart := waitForEphemeral(t, conn, protocol.UpdateTypeTyping, 30*time.Second)
	var foundStart bool
	for _, u := range typingStart.Updates {
		if u.Type == protocol.UpdateTypeTyping {
			var p struct {
				IsTyping bool `json:"is_typing"`
			}
			require.NoError(t, json.Unmarshal(u.Payload, &p))
			if p.IsTyping {
				foundStart = true
			}
		}
	}
	require.True(t, foundStart, "should receive typing=true first (D-065)")

	// Steps 2+3: Wait for streaming AND typing=false. These may arrive in the
	// same WebSocket batch, so we check for both in each batch we read.
	var foundStreaming, foundStop bool
	deadline := time.Now().Add(30 * time.Second)
	for !foundStreaming || !foundStop {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: streaming=%v typing_stop=%v", foundStreaming, foundStop)
		}
		updates := waitForUpdate(t, conn, time.Until(deadline))
		for _, u := range updates.Updates {
			switch u.Type {
			case protocol.UpdateTypeStreaming:
				var p struct {
					UserID string `json:"user_id"`
					Text   string `json:"text"`
				}
				if json.Unmarshal(u.Payload, &p) == nil && p.UserID == agentUserID && p.Text != "" {
					foundStreaming = true
				}
			case protocol.UpdateTypeTyping:
				var p struct {
					IsTyping bool `json:"is_typing"`
				}
				if json.Unmarshal(u.Payload, &p) == nil && !p.IsTyping {
					foundStop = true
				}
			}
		}
	}

	assert.True(t, foundStreaming, "should receive streaming updates")
	assert.True(t, foundStop, "typing should stop after first token (D-065)")
	<-execDone
}

// ---------------------------------------------------------------------------
// TestAgentStream_AE_STREAM_005 — Streaming not in sync_updates (D-050, D-051)
// ---------------------------------------------------------------------------
func TestAgentStream_AE_STREAM_005(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-stream-005"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, "device-1")
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	execDone := make(chan struct{})
	go func() {
		defer close(execDone)
		_ = triggerAgentProcessing(t, env, "msg-005", conv.ID, agentUserID, userID)
	}()

	// Wait for streaming to complete (is_done=true).
	streamText := waitForAgentStreamDone(t, conn, "test-bot", 30*time.Second)
	require.NotEmpty(t, streamText)
	<-execDone // ensure executor completes before DB check

	// Wait for executor to persist the message to DB.
	require.Eventually(t, func() bool {
		var msgs []*model.Message
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
			Order("message_id DESC").Limit(1).Find(&msgs)
		return len(msgs) > 0
	}, 30*time.Second, 100*time.Millisecond, "agent message should be persisted")

	// Drain and close connection.
	drainPushUpdates(t, conn)
	conn.Close()

	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(context.Background(), userID, 10)
		return err == nil && len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond)

	// Reconnect and verify sync_updates has no streaming/typing.
	newConn := connectClient(t, env.addr, userID, "device-1")
	defer newConn.Close()
	drainPushUpdates(t, newConn)

	sendRequest(t, newConn, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0, "limit": 100,
	})
	syncResp := readResponse(t, newConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code)

	var syncData struct {
		Updates []protocol.PackageDataUpdate `json:"updates"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))

	for _, u := range syncData.Updates {
		assert.NotEqual(t, protocol.UpdateTypeStreaming, u.Type, "no streaming in sync (D-051)")
		assert.NotEqual(t, protocol.UpdateTypeTyping, u.Type, "no typing in sync (D-050)")
	}

	// Verify the agent's persisted message IS in sync.
	var foundMsg bool
	for _, u := range syncData.Updates {
		if u.Type == protocol.UpdateTypeMessage {
			var msg model.Message
			require.NoError(t, json.Unmarshal(u.Payload, &msg))
			if msg.SenderID == agentUserID {
				foundMsg = true
				assert.Equal(t, streamText, msg.Content)
			}
		}
	}
	assert.True(t, foundMsg, "sync should contain agent's persisted message")
}

// ---------------------------------------------------------------------------
// TestAgentStream_AE_STREAM_006 — Persisted content matches stream (D-052)
// ---------------------------------------------------------------------------
func TestAgentStream_AE_STREAM_006(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-stream-006"
	agentUserID := "agent/test-bot"

	conv := createAgentConversation(t, env, userID, agentUserID)
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()
	_ = insertUserMessageDirect(t, env, userID, conv.ID, "hello")

	execDone := make(chan struct{})
	go func() {
		defer close(execDone)
		_ = triggerAgentProcessing(t, env, "msg-006", conv.ID, agentUserID, userID)
	}()

	// Wait for is_done streaming update.
	streamFinalText := waitForAgentStreamDone(t, conn, "test-bot", 30*time.Second)
	require.NotEmpty(t, streamFinalText, "streamed final text should not be empty")
	<-execDone // ensure executor completes before DB check

	// Wait for the message to be persisted in DB (the executor persists after
	// sending is_done, but the WS push may not arrive since we bypass MQ).
	var agentMsg *model.Message
	require.Eventually(t, func() bool {
		var msgs []*model.Message
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", conv.ID, agentUserID).
			Order("message_id DESC").Limit(1).Find(&msgs)
		if len(msgs) > 0 {
			agentMsg = msgs[0]
			return true
		}
		return false
	}, 30*time.Second, 100*time.Millisecond, "agent message should be persisted in DB")

	// Verify persisted content equals streamed text (D-052).
	assert.Equal(t, streamFinalText, agentMsg.Content,
		"persisted content should equal streamed text (D-052)")
	assert.Equal(t, agentUserID, agentMsg.SenderID,
		"sender_id should be the agent (D-054)")
	assertAgentMessagePersisted(t, env, conv.ID, agentUserID, streamFinalText)
}
