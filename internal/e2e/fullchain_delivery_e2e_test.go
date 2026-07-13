// Package e2e_test — full-chain delivery guarantee E2E tests.
//
// These tests verify message delivery reliability under various conditions:
// normal delivery, offline messages, ordering, idempotency, large payloads,
// and special character handling. Each test uses threeLayerCheck and
// testStepLogger for structured verification.
//
// No build tag — available to all tests.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Test 1: Normal Delivery — three-layer verification
// ---------------------------------------------------------------------------

// TestFullChainDelivery_NormalDelivery verifies the happy path: A sends a
// message to B, Server DB persists it, B receives the push update, and all
// three layers (Server DB, Redis connection store, WebSocket delivery) are
// consistent.
//
// Verifies: D-006 (idempotency), D-007 (fire-and-forget MQ), D-008 (MessageID).
func TestFullChainDelivery_NormalDelivery(t *testing.T) {
	env := setupE2ETest(t)
	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)

	// Step 1: Connect sender (alice) and receiver (bob).
	logger.Step("Connect alice and bob")
	aliceConn := connectClient(t, env.addr, "nd-alice", "nd-alice-dev")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "nd-bob", "nd-bob-dev")
	defer bobConn.Close()

	// Step 2: Create conversation.
	logger.Step("Create conversation")
	conv := createTestConversation(t, env.store, "nd-alice", "nd-bob")

	// BEFORE: 0 messages in conversation.
	check.VerifyServerDB("before-zero-msgs", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 100)
		if err != nil {
			return err
		}
		if len(msgs) != 0 {
			return fmt.Errorf("expected 0 messages before send, got %d", len(msgs))
		}
		return nil
	})

	// Step 3: Alice sends a message to Bob.
	logger.Step("Alice sends message")
	clientMsgID := uuid.New().String()
	content := "Hello from normal delivery test"
	sendRequest(t, aliceConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           content,
		"type":              "text",
	})

	// Step 4: Alice receives the response.
	logger.Step("Alice receives response")
	resp := readResponse(t, aliceConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send should succeed")

	var respData struct {
		Message   model.Message `json:"message"`
		Duplicate bool          `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &respData))
	assert.Equal(t, content, respData.Message.Content, "content should match")
	assert.Equal(t, "nd-alice", respData.Message.SenderID, "sender should be alice")
	assert.Equal(t, uint32(1), respData.Message.MessageID, "message_id should be 1 (D-008)")
	assert.False(t, respData.Duplicate, "first send should not be duplicate (D-006)")

	// Step 5: Bob receives the message via sync_updates (D-110: MQ broadcast
	// does not deliver in E2E test env, so we verify via sync_updates).
	logger.Step("Bob receives message via sync_updates")
	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, bobConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync should succeed")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))
	require.Len(t, syncData.Updates, 1, "bob should receive 1 update via sync")
	assert.Equal(t, protocol.UpdateTypeMessage, syncData.Updates[0].Type, "update type should be message")

	var bobPayload model.Message
	require.NoError(t, json.Unmarshal(syncData.Updates[0].Payload, &bobPayload))
	assert.Equal(t, content, bobPayload.Content, "bob payload should match")

	// AFTER: Three-layer verification.
	logger.Step("Three-layer verification")
	check.VerifyServerDB("after-msg-persisted", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 100)
		if err != nil {
			return err
		}
		if len(msgs) != 1 {
			return fmt.Errorf("expected 1 message after send, got %d", len(msgs))
		}
		if msgs[0].Content != content {
			return fmt.Errorf("expected content %q, got %q", content, msgs[0].Content)
		}
		return nil
	})

	check.VerifyServerDB("after-update-count", func() error {
		ctx := context.Background()
		var updateCount int64
		env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).
			Where("user_id IN ?", []string{"nd-alice", "nd-bob"}).Count(&updateCount)
		if updateCount != 2 {
			return fmt.Errorf("expected 2 user_updates (one per member), got %d", updateCount)
		}
		return nil
	})

	check.VerifyRedis("connections-exist", func() error {
		ctx := context.Background()
		aliceConns, err := env.connStore.ListByUser(ctx, "nd-alice", 10)
		if err != nil {
			return fmt.Errorf("list alice connections: %w", err)
		}
		if len(aliceConns) == 0 {
			return fmt.Errorf("alice should have at least 1 connection in Redis")
		}
		bobConns, err := env.connStore.ListByUser(ctx, "nd-bob", 10)
		if err != nil {
			return fmt.Errorf("list bob connections: %w", err)
		}
		if len(bobConns) == 0 {
			return fmt.Errorf("bob should have at least 1 connection in Redis")
		}
		return nil
	})

	// Note: Alice's push update (C-10) is not verified here because MQ broadcast
	// does not deliver in E2E test environment (D-110). Message persistence and
	// Bob's delivery via sync_updates are verified above.
}

// ---------------------------------------------------------------------------
// Test 2: Offline Message — B receives after connecting
// ---------------------------------------------------------------------------

// TestFullChainDelivery_OfflineMessage verifies that when B is offline and A
// sends a message, the message is persisted (D-007) and B receives it via
// sync_updates upon connecting.
//
// Verifies: D-007 (persistence-first), D-009 (sync_updates pagination).
func TestFullChainDelivery_OfflineMessage(t *testing.T) {
	env := setupE2ETest(t)
	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)

	// Step 1: Alice connects, Bob stays offline.
	logger.Step("Alice connects (Bob offline)")
	aliceConn := connectClient(t, env.addr, "om-alice", "om-alice-dev")
	defer aliceConn.Close()

	conv := createTestConversation(t, env.store, "om-alice", "om-bob")

	// Step 2: Alice sends message while Bob is offline.
	logger.Step("Alice sends message (Bob offline)")
	clientMsgID := uuid.New().String()
	content := "Are you there? Offline message test"
	sendRequest(t, aliceConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           content,
		"type":              "text",
	})

	resp := readResponse(t, aliceConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code,
		"send should succeed even when recipient is offline (D-007)")

	// Drain alice's push updates (MQ broadcast does not deliver in E2E env per D-110).
	drainPushUpdates(t, aliceConn)

	// Step 3: Verify message persisted in Server DB (D-007 persistence-first).
	logger.Step("Verify persistence (D-007)")
	check.VerifyServerDB("msg-persisted-offline", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 100)
		if err != nil {
			return err
		}
		if len(msgs) != 1 {
			return fmt.Errorf("expected 1 persisted message, got %d", len(msgs))
		}
		if msgs[0].Content != content {
			return fmt.Errorf("persisted content mismatch: expected %q, got %q", content, msgs[0].Content)
		}
		return nil
	})

	// Step 4: Alice disconnects to avoid interfering with Bob's sync.
	logger.Step("Alice disconnects")
	aliceConn.Close()

	// Wait for connection store cleanup.
	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(context.Background(), "om-alice", 10)
		return err == nil && len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond, "alice should be disconnected")

	// Step 5: Bob comes online and syncs.
	logger.Step("Bob connects and syncs")
	bobConn := connectClient(t, env.addr, "om-bob", "om-bob-dev")
	defer bobConn.Close()

	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})

	syncResp := readResponse(t, bobConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync should succeed (D-009)")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))

	assert.Len(t, syncData.Updates, 1, "bob should receive 1 offline update")
	assert.False(t, syncData.HasMore, "has_more should be false")
	assert.Equal(t, uint32(1), syncData.LatestSeq, "latest_seq should be 1")

	if len(syncData.Updates) > 0 {
		var payload model.Message
		require.NoError(t, json.Unmarshal(syncData.Updates[0].Payload, &payload))
		assert.Equal(t, content, payload.Content, "offline message content should match")
	}

	// Final three-layer check.
	logger.Step("Final verification")
	check.VerifyServerDB("offline-msg-count", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 100)
		if err != nil {
			return err
		}
		if len(msgs) != 1 {
			return fmt.Errorf("expected 1 message, got %d", len(msgs))
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Test 3: Message Ordering — 5 rapid messages ordered by MessageID
// ---------------------------------------------------------------------------

// TestFullChainDelivery_MessageOrdering verifies that 5 messages sent in rapid
// succession receive monotonically increasing MessageIDs (D-008) and are
// received in order by the recipient.
//
// Verifies: D-008 (MessageID monotonic increment).
func TestFullChainDelivery_MessageOrdering(t *testing.T) {
	env := setupE2ETest(t)
	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)

	// Step 1: Connect alice and bob.
	logger.Step("Connect alice and bob")
	aliceConn := connectClient(t, env.addr, "mo-alice", "mo-alice-dev")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "mo-bob", "mo-bob-dev")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "mo-alice", "mo-bob")

	// Step 2: Send 5 messages rapidly.
	logger.Step("Send 5 messages rapidly")
	const msgCount = 5
	var messageIDs []uint32
	var contents []string

	for i := 0; i < msgCount; i++ {
		clientMsgID := uuid.New().String()
		content := fmt.Sprintf("Ordered message %d", i+1)
		contents = append(contents, content)

		sendRequest(t, aliceConn, fmt.Sprintf("req-%d", i+1), "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": clientMsgID,
			"content":           content,
			"type":              "text",
		})

		resp := readResponse(t, aliceConn, normalTimeout)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "message %d should succeed", i+1)

		var respData struct {
			Message model.Message `json:"message"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &respData))
		messageIDs = append(messageIDs, respData.Message.MessageID)
	}

	// Step 3: Verify MessageIDs are monotonically increasing (D-008).
	logger.Step("Verify MessageID ordering (D-008)")
	for i := 0; i < len(messageIDs)-1; i++ {
		assert.Less(t, messageIDs[i], messageIDs[i+1],
			"MessageID %d should be < %d (D-008 monotonic)", messageIDs[i], messageIDs[i+1])
	}

	// Verify sequential: 1, 2, 3, 4, 5.
	for i, id := range messageIDs {
		assert.Equal(t, uint32(i+1), id, "message %d should have MessageID=%d (D-008)", i+1, i+1)
	}

	// Step 4: Bob receives all messages via sync_updates (D-110: MQ broadcast
	// does not deliver in E2E test env).
	logger.Step("Bob receives all 5 updates via sync_updates")
	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, bobConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync should succeed")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))
	require.Len(t, syncData.Updates, msgCount, "bob should receive all 5 updates via sync")

	var bobSeqs []uint32
	var bobContents []string
	for _, u := range syncData.Updates {
		bobSeqs = append(bobSeqs, u.Seq)
		var payload model.Message
		require.NoError(t, json.Unmarshal(u.Payload, &payload))
		bobContents = append(bobContents, payload.Content)
	}

	// Verify Bob received them in order.
	for i := 0; i < len(bobSeqs)-1; i++ {
		assert.Less(t, bobSeqs[i], bobSeqs[i+1],
			"Bob seq %d should be < %d (ordered delivery)", bobSeqs[i], bobSeqs[i+1])
	}
	for i, c := range bobContents {
		assert.Equal(t, contents[i], c, "Bob message %d content should match", i+1)
	}

	// Note: Alice's push updates (C-10) are not verified here because MQ broadcast
	// does not deliver in E2E test environment (D-110). Message ordering is verified
	// via Bob's sync_updates and Server DB.

	// Step 5: Three-layer verification.
	logger.Step("Three-layer verification")
	check.VerifyServerDB("ordering-msg-count", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 100)
		if err != nil {
			return err
		}
		if len(msgs) != msgCount {
			return fmt.Errorf("expected %d messages, got %d", msgCount, len(msgs))
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Test 4: Idempotency Enhanced — same client_message_id twice
// ---------------------------------------------------------------------------

// TestFullChainDelivery_Idempotency_Enhanced verifies that sending two messages
// with the same client_message_id results in only one persisted record and the
// second response includes duplicate=true (D-006).
//
// Verifies: D-006 (client_message_id idempotency).
func TestFullChainDelivery_Idempotency_Enhanced(t *testing.T) {
	env := setupE2ETest(t)
	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)

	// Step 1: Connect alice and bob.
	logger.Step("Connect alice and bob")
	aliceConn := connectClient(t, env.addr, "idp-alice", "idp-alice-dev")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "idp-bob", "idp-bob-dev")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "idp-alice", "idp-bob")

	// Step 2: First send.
	logger.Step("First send with unique client_message_id")
	clientMsgID := "idp-" + uuid.New().String()
	content := "Idempotency test message"

	sendRequest(t, aliceConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           content,
		"type":              "text",
	})

	resp1 := readResponse(t, aliceConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, resp1.Code, "first send should succeed")

	var respData1 struct {
		Message   model.Message `json:"message"`
		Duplicate bool          `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp1.Data, &respData1))
	assert.False(t, respData1.Duplicate, "first send should not be duplicate (D-006)")
	firstMsgID := respData1.Message.ID
	firstMessageID := respData1.Message.MessageID

	// Drain any push updates (MQ broadcast does not deliver in E2E env per D-110).
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// Step 3: Second send with same client_message_id.
	logger.Step("Second send with same client_message_id (duplicate)")
	sendRequest(t, aliceConn, "req-2", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           content,
		"type":              "text",
	})

	resp2 := readResponse(t, aliceConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, resp2.Code, "duplicate send should succeed (D-006)")

	var respData2 struct {
		Message   model.Message `json:"message"`
		Duplicate bool          `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp2.Data, &respData2))
	assert.True(t, respData2.Duplicate, "second send should be duplicate=true (D-006)")
	assert.Equal(t, firstMsgID, respData2.Message.ID,
		"duplicate should return same message ID (D-006)")
	assert.Equal(t, firstMessageID, respData2.Message.MessageID,
		"duplicate should return same MessageID (D-006)")
	assert.Equal(t, content, respData2.Message.Content,
		"duplicate should return same content (D-006)")

	// Step 4: Three-layer verification.
	logger.Step("Three-layer verification — only 1 message in DB")
	check.VerifyServerDB("idempotency-single-msg", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 100)
		if err != nil {
			return err
		}
		if len(msgs) != 1 {
			return fmt.Errorf("expected exactly 1 message after duplicate send, got %d", len(msgs))
		}
		if msgs[0].Content != content {
			return fmt.Errorf("message content mismatch: expected %q, got %q", content, msgs[0].Content)
		}
		return nil
	})

	check.VerifyServerDB("idempotency-single-update", func() error {
		ctx := context.Background()
		var updateCount int64
		env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).
			Where("user_id IN ?", []string{"idp-alice", "idp-bob"}).Count(&updateCount)
		if updateCount != 2 {
			return fmt.Errorf("expected 2 user_updates (no duplicates), got %d", updateCount)
		}
		return nil
	})

	check.VerifyServerDB("idempotency-client-msg-id", func() error {
		ctx := context.Background()
		var count int64
		env.db.DB().WithContext(ctx).Model(&model.Message{}).
			Where("client_message_id = ?", clientMsgID).Count(&count)
		if count != 1 {
			return fmt.Errorf("expected 1 message with client_message_id, got %d", count)
		}
		return nil
	})

	check.VerifyRedis("idempotency-connections-exist", func() error {
		ctx := context.Background()
		aliceConns, err := env.connStore.ListByUser(ctx, "idp-alice", 10)
		if err != nil {
			return fmt.Errorf("list alice connections: %w", err)
		}
		if len(aliceConns) == 0 {
			return fmt.Errorf("alice should have at least 1 connection in Redis")
		}
		bobConns, err := env.connStore.ListByUser(ctx, "idp-bob", 10)
		if err != nil {
			return fmt.Errorf("list bob connections: %w", err)
		}
		if len(bobConns) == 0 {
			return fmt.Errorf("bob should have at least 1 connection in Redis")
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Test 5: Large Message — 10KB content stored correctly
// ---------------------------------------------------------------------------

// TestFullChainDelivery_LargeMessage verifies that a 10KB message is correctly
// stored in the Server DB without truncation and delivered intact to the
// recipient.
//
// See also TestFullChainBoundary_LongInput_ThreeLayer for agent-side large
// input handling (D-091).
func TestFullChainDelivery_LargeMessage(t *testing.T) {
	env := setupE2ETest(t)
	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)

	// Step 1: Connect alice and bob.
	logger.Step("Connect alice and bob")
	aliceConn := connectClient(t, env.addr, "lm-alice", "lm-alice-dev")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "lm-bob", "lm-bob-dev")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "lm-alice", "lm-bob")

	// Step 2: Generate 10KB content.
	logger.Step("Generate 10KB content")
	// Build a 10KB string: 10240 bytes of repeating pattern.
	base := "Xyncra large message payload test. "
	var sb strings.Builder
	for sb.Len() < 10240 {
		sb.WriteString(base)
	}
	largeContent := sb.String()
	originalLen := len(largeContent)
	t.Logf("Large message size: %d bytes", originalLen)

	// Step 3: Send the large message.
	logger.Step("Send 10KB message")
	clientMsgID := uuid.New().String()
	sendRequest(t, aliceConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           largeContent,
		"type":              "text",
	})

	resp := readResponse(t, aliceConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code, "large message send should succeed")

	var respData struct {
		Message model.Message `json:"message"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &respData))
	assert.Equal(t, originalLen, len(respData.Message.Content),
		"response content length should match original")

	// Step 4: Bob receives the message via sync_updates with full content (D-110).
	logger.Step("Bob receives large message via sync_updates")
	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, bobConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync should succeed")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))
	require.Len(t, syncData.Updates, 1, "bob should receive 1 update via sync")

	var bobPayload model.Message
	require.NoError(t, json.Unmarshal(syncData.Updates[0].Payload, &bobPayload))
	assert.Equal(t, originalLen, len(bobPayload.Content),
		"bob received content length should match original (no truncation)")
	assert.Equal(t, largeContent, bobPayload.Content,
		"bob received content should match exactly")

	// Note: Alice's push update not verified (MQ broadcast does not deliver in E2E env per D-110).

	// Step 5: Three-layer verification.
	logger.Step("Three-layer verification — no truncation")
	check.VerifyServerDB("large-msg-length", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 10)
		if err != nil {
			return err
		}
		if len(msgs) != 1 {
			return fmt.Errorf("expected 1 message, got %d", len(msgs))
		}
		if len(msgs[0].Content) != originalLen {
			return fmt.Errorf("stored content length %d != original %d (truncation?)",
				len(msgs[0].Content), originalLen)
		}
		if msgs[0].Content != largeContent {
			return fmt.Errorf("stored content does not match original")
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Test 6: Special Characters — emoji + HTML + SQL injection
// ---------------------------------------------------------------------------

// TestFullChainDelivery_SpecialCharacters verifies that messages containing
// emoji, HTML tags, and SQL injection attempts are stored correctly without
// truncation, encoding corruption, or injection execution.
func TestFullChainDelivery_SpecialCharacters(t *testing.T) {
	env := setupE2ETest(t)
	logger := newTestStepLogger(t)
	check := newThreeLayerCheck(t, logger)

	// Step 1: Connect alice and bob.
	logger.Step("Connect alice and bob")
	aliceConn := connectClient(t, env.addr, "sc-alice", "sc-alice-dev")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "sc-bob", "sc-bob-dev")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "sc-alice", "sc-bob")

	// Step 2: Define special character test cases.
	testCases := []struct {
		name    string
		content string
	}{
		{
			name:    "emoji",
			content: "Hello 😀🚀❤️ World 🌟🎉",
		},
		{
			name:    "html_tags",
			content: `<script>alert("xss")</script><b>bold</b><a href="http://evil.com">click</a>`,
		},
		{
			name:    "sql_injection",
			content: `'; DROP TABLE messages; -- ' OR 1=1; /* comment */ UNION SELECT * FROM users`,
		},
		{
			name:    "unicode_mixed",
			content: "你好世界 こんにちは 😀 <tag> &amp; 'quote' \"double\"",
		},
	}

	// Step 3: Send each special character message.
	logger.Step("Send special character messages")
	for i, tc := range testCases {
		clientMsgID := uuid.New().String()
		sendRequest(t, aliceConn, fmt.Sprintf("req-%d", i+1), "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": clientMsgID,
			"content":           tc.content,
			"type":              "text",
		})

		resp := readResponse(t, aliceConn, normalTimeout)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code,
			"send %s should succeed", tc.name)

		var respData struct {
			Message model.Message `json:"message"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &respData))
		assert.Equal(t, tc.content, respData.Message.Content,
			"response content should match exactly for %s", tc.name)
		assert.Equal(t, len(tc.content), len(respData.Message.Content),
			"response content length should match for %s (no truncation)", tc.name)
	}

	// Step 4: Bob receives all messages via sync_updates (D-110).
	logger.Step("Bob receives all special character messages via sync_updates")
	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})
	syncResp := readResponse(t, bobConn, normalTimeout)
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync should succeed")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData))
	require.Len(t, syncData.Updates, len(testCases), "bob should receive all messages via sync")

	var bobContents []string
	for _, u := range syncData.Updates {
		var payload model.Message
		require.NoError(t, json.Unmarshal(u.Payload, &payload))
		bobContents = append(bobContents, payload.Content)
	}

	// Verify each message was delivered intact.
	for i, tc := range testCases {
		assert.Equal(t, tc.content, bobContents[i],
			"bob received content should match for %s", tc.name)
	}

	// Note: Alice's push updates not verified (MQ broadcast does not deliver in E2E env per D-110).

	// Step 5: Three-layer verification.
	logger.Step("Three-layer verification — special characters stored correctly")
	check.VerifyServerDB("special-chars-count", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 100)
		if err != nil {
			return err
		}
		if len(msgs) != len(testCases) {
			return fmt.Errorf("expected %d messages, got %d", len(testCases), len(msgs))
		}
		return nil
	})

	check.VerifyServerDB("special-chars-content-integrity", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 100)
		if err != nil {
			return err
		}
		// Verify each stored message content matches original exactly.
		storedContents := make(map[string]bool)
		for _, msg := range msgs {
			storedContents[msg.Content] = true
		}
		for _, tc := range testCases {
			if !storedContents[tc.content] {
				return fmt.Errorf("stored messages missing content for case %q (len=%d)",
					tc.name, len(tc.content))
			}
		}
		return nil
	})

	// Verify SQL injection did not execute (table still exists).
	check.VerifyServerDB("sql-injection-safe", func() error {
		ctx := context.Background()
		msgs, err := env.store.MessageStore().ListRecentByConversation(ctx, conv.ID, 100)
		if err != nil {
			return err
		}
		// If we can still read messages, the DROP TABLE did not execute.
		if len(msgs) == 0 {
			return fmt.Errorf("SQL injection may have executed: no messages found")
		}
		// Verify the SQL injection string is stored as-is (not executed).
		found := false
		for _, msg := range msgs {
			if strings.Contains(msg.Content, "DROP TABLE") {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("SQL injection string not found in stored messages")
		}
		return nil
	})
}
