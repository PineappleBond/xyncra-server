// Package e2e_test contains end-to-end integration tests for the Xyncra
// WebSocket server. Tests exercise the full stack: WebSocket protocol,
// message handlers, MQ broker, Redis connection store, and SQLite database.
//
// e2e_test assumes:
//   - A Redis instance is available at localhost:16379
//   - Redis DB 15 is exclusively used for E2E tests (FlushDB is called before each test)
//   - Tests MUST NOT run in parallel (shared Redis instance)
//   - SQLite in-memory database is used for data isolation
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/handler"
	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// e2eRedisAddr is the Redis address used for all E2E tests.
	e2eRedisAddr = "localhost:16379"

	// e2eRedisDB is the Redis database index used for E2E tests.
	// DB 15 is reserved to avoid interfering with development data.
	e2eRedisDB = 15

	// e2eDefaultTTL is the default connection TTL used in tests.
	// Kept short so that TTL-related assertions can run quickly.
	e2eDefaultTTL = 5 * time.Second
)

// ---------------------------------------------------------------------------
// e2eEnv
// ---------------------------------------------------------------------------

// e2eEnv holds all the components needed by a single E2E test. Each test
// receives its own isolated environment (independent SQLite DB, independent
// Redis key namespace).
type e2eEnv struct {
	db          *store.Database
	store       *store.Store
	connStore   *server.RedisConnectionStore
	broker      *mq.AsynqBroker
	srv         *server.WebSocketServer
	addr        string
	cancel      context.CancelFunc
	redisKey    string                        // key prefix for TTL verification
	taskHandler *mq.TaskHandler               // exposed for agent E2E handler registration
	msgHandler  *server.DefaultMessageHandler // exposed for agent E2E RPC registration
}

// ---------------------------------------------------------------------------
// setupE2ETest
// ---------------------------------------------------------------------------

// setupE2ETest initialises a complete E2E environment: SQLite in-memory DB,
// Redis connection store, AsynqBroker, message handlers, and WebSocket server.
// It registers a t.Cleanup that tears everything down in reverse order.
//
// If Redis is unreachable the test is skipped (not failed), because Redis may
// not be available in all CI environments.
func setupE2ETest(t *testing.T) *e2eEnv {
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
		t.Skipf("Redis not available at %s (DB %d): %v — skipping E2E test", e2eRedisAddr, e2eRedisDB, err)
	}

	// 2. FlushDB to ensure a clean slate.
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer flushCancel()
	require.NoError(t, redisClient.FlushDB(flushCtx).Err(), "FlushDB should succeed")
	_ = redisClient.Close()

	// 3. SQLite in-memory database (each test gets its own named DB).
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)", t.Name())
	db, err := store.NewDatabase(store.DatabaseConfig{
		Driver: "sqlite",
		DSN:    dsn,
	})
	require.NoError(t, err, "NewDatabase should succeed (C-9)")

	// 4. Store + AutoMigrate.
	dataStore := store.NewFromDatabase(db)
	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer migrateCancel()
	require.NoError(t, dataStore.AutoMigrate(migrateCtx), "AutoMigrate should succeed")

	// 5. Redis connection store.
	keyPrefix := fmt.Sprintf("e2e:%s:", t.Name())
	connStore, err := server.NewRedisConnectionStore(server.RedisConnectionStoreConfig{
		Addr:       e2eRedisAddr,
		DB:         e2eRedisDB,
		KeyPrefix:  keyPrefix,
		DefaultTTL: e2eDefaultTTL,
	})
	require.NoError(t, err, "NewRedisConnectionStore should succeed (C-12)")

	// 6. AsynqBroker.
	broker, err := mq.NewAsynqBroker(mq.AsynqConfig{
		RedisAddr:     e2eRedisAddr,
		RedisPassword: "",
		RedisDB:       e2eRedisDB,
	})
	require.NoError(t, err, "NewAsynqBroker should succeed")

	// 7. Message handler.
	msgHandler := server.NewDefaultMessageHandler()

	// 8. WebSocket server (created before RegisterAll so BroadcastFn is available).
	srv, err := server.NewWebSocketServer(
		server.WSWithAddr(":0"),
		server.WSWithConnectionStore(connStore),
		server.WSWithStore(dataStore),
		server.WSWithBroker(broker),
		server.WSWithMessageHandler(msgHandler),
		server.WSWithPingPeriod(500*time.Millisecond),
		server.WSWithPongWait(3*time.Second),
		server.WSWithWriteWait(3*time.Second),
	)
	require.NoError(t, err, "NewWebSocketServer should succeed")

	// 9. RegisterAll with BroadcastFn (requires srv to exist first).
	handler.RegisterAll(msgHandler, handler.Dependencies{
		ConnStore:   connStore,
		Store:       dataStore,
		Broker:      broker,
		BroadcastFn: srv.BroadcastUpdates,
	})

	// 10. Task handler + Register(TypeSendMessage).
	taskHandler := mq.NewTaskHandler()
	taskHandler.Register(mq.TypeSendMessage,
		handler.NewSendMessageTaskHandler(srv.BroadcastUpdates, srv.Logger()))

	// 11. Start broker and server in goroutines (C-2).
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := broker.Start(ctx, taskHandler); err != nil {
			// Context cancellation is expected during cleanup.
			if ctx.Err() == nil {
				t.Logf("broker error: %v", err)
			}
		}
	}()

	go func() {
		if err := srv.Start(ctx); err != nil {
			// Context cancellation is expected during cleanup.
			if ctx.Err() == nil {
				t.Logf("server error: %v", err)
			}
		}
	}()

	// 12. Wait for the server to be ready (C-3).
	require.Eventually(t, func() bool {
		addr := srv.Addr()
		return addr != ":0" && addr != ""
	}, 3*time.Second, 20*time.Millisecond, "server should bind to a real address (C-3)")

	addr := srv.Addr()

	// 13. Cleanup (C-1 reverse order, using t.Cleanup not defer).
	t.Cleanup(func() {
		// Cancel context first to signal goroutines to stop.
		cancel()

		// GracefulStop with a 5s timeout.
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = srv.GracefulStop(stopCtx)

		_ = broker.Close()
		_ = connStore.Close()
		_ = db.Close()

		// Final FlushDB to clean up.
		cleanupClient := redis.NewClient(&redis.Options{
			Addr: e2eRedisAddr,
			DB:   e2eRedisDB,
		})
		defer func() { _ = cleanupClient.Close() }()
		flushCtx2, flushCancel2 := context.WithTimeout(context.Background(), 2*time.Second)
		defer flushCancel2()
		_ = cleanupClient.FlushDB(flushCtx2).Err()
	})

	return &e2eEnv{
		db:          db,
		store:       dataStore,
		connStore:   connStore,
		broker:      broker,
		srv:         srv,
		addr:        addr,
		cancel:      cancel,
		redisKey:    keyPrefix,
		taskHandler: taskHandler,
		msgHandler:  msgHandler,
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// connectClient opens a WebSocket connection for the given userID and deviceID,
// returning a *wsConn (channel-backed wrapper). The connection URL is constructed
// as ws://{addr}/ws?user_id={userID}&device_id={deviceID} (C-4, D-033).
func connectClient(t *testing.T, addr, userID, deviceID string) *wsConn {
	t.Helper()

	q := url.Values{}
	q.Set("user_id", userID)
	q.Set("device_id", deviceID)
	u := url.URL{
		Scheme:   "ws",
		Host:     addr,
		Path:     "/ws",
		RawQuery: q.Encode(),
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	require.NoError(t, err, "WebSocket dial should succeed for user %s", userID)
	return wrapConn(conn)
}

// sendRequest marshals the given params into a PackageDataRequest, wraps it
// in a Package{Type:Request}, and writes it to the connection.
func sendRequest(t *testing.T, conn *wsConn, id, method string, params map[string]interface{}) {
	t.Helper()

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err, "marshal params should succeed")

	req := protocol.PackageDataRequest{
		ID:     id,
		Method: method,
		Params: paramsJSON,
	}
	reqData, err := json.Marshal(req)
	require.NoError(t, err, "marshal request should succeed")

	pkg := protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: reqData,
	}
	pkgData, err := json.Marshal(pkg)
	require.NoError(t, err, "marshal package should succeed")

	err = conn.WriteMessage(websocket.TextMessage, pkgData)
	require.NoError(t, err, "write message should succeed")
}

// readResponse reads messages from the connection until a PackageTypeResponse
// is received or the timeout expires. Non-Response packages (e.g. push
// updates that arrive after drainPushUpdates returned) are silently skipped.
//
// This is necessary because MQ delivery is asynchronous: a push update from a
// previous send may arrive between the current request and its response.
func readResponse(t *testing.T, conn *wsConn, timeout time.Duration) *protocol.PackageDataResponse {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("readResponse: timed out after %v waiting for PackageTypeResponse", timeout)
		}

		_, data, err := conn.recv(remaining)
		require.NoError(t, err, "read message should succeed within timeout")

		first, rest := firstJSON(data)

		// Push remaining JSON objects back so they are not lost.
		if len(rest) > 0 {
			conn.msgCh <- msgResult{messageType: 1, data: rest}
		}

		var pkg protocol.Package
		require.NoError(t, json.Unmarshal(first, &pkg), "unmarshal package should succeed")
		if pkg.Type == protocol.PackageTypeResponse {
			var resp protocol.PackageDataResponse
			require.NoError(t, json.Unmarshal(pkg.Data, &resp), "unmarshal response should succeed")
			return &resp
		}
		// Skip non-Response packages (e.g. push updates).
	}
}

// firstJSON extracts the first complete JSON object from data.
// If data contains multiple concatenated JSON objects (as can happen when the
// server's writePump batches messages into a single WebSocket frame), only the
// first object is returned, along with the remaining bytes.
//
// This is needed because gorilla/websocket's ReadMessage may return multiple
// WriteMessage calls coalesced into a single frame when they are written in
// rapid succession.
func firstJSON(data []byte) (first, rest []byte) {
	data = bytes.TrimLeft(data, " \t\r\n")
	if len(data) == 0 || data[0] != '{' {
		return data, nil
	}
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		b := data[i]
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' && inString {
			escaped = true
			continue
		}
		if b == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch b {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return data[:i+1], bytes.TrimLeft(data[i+1:], " \t\r\n")
			}
		}
	}
	// Incomplete JSON — return everything as first.
	return data, nil
}

// readPackage reads a single message from the connection and returns the
// decoded Package without checking its type. If the server batched multiple
// JSON packages into a single WebSocket frame (writePump drains the send
// channel for efficiency), only the first JSON object is consumed and the
// remainder is pushed back to msgCh for subsequent reads.
func readPackage(t *testing.T, conn *wsConn, timeout time.Duration) *protocol.Package {
	t.Helper()

	_, data, err := conn.recv(timeout)
	require.NoError(t, err, "read message should succeed within timeout")

	first, rest := firstJSON(data)

	// Push remaining JSON objects back so they are not lost.
	if len(rest) > 0 {
		conn.msgCh <- msgResult{messageType: 1, data: rest}
	}

	var pkg protocol.Package
	require.NoError(t, json.Unmarshal(first, &pkg), "unmarshal package should succeed")
	return &pkg
}

// waitForUpdate loops reading packages from the connection until a
// PackageTypeUpdates is received or the timeout expires. Non-Updates packages
// (e.g. responses that arrive before the push) are silently skipped.
//
// This is necessary because MQ delivery is asynchronous: the send_message
// response may arrive before the push notification.
func waitForUpdate(t *testing.T, conn *wsConn, timeout time.Duration) *protocol.PackageDataUpdates {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("waitForUpdate: timed out after %v waiting for PackageTypeUpdates", timeout)
		}

		pkg := readPackage(t, conn, remaining)
		if pkg.Type == protocol.PackageTypeUpdates {
			var updates protocol.PackageDataUpdates
			require.NoError(t, json.Unmarshal(pkg.Data, &updates),
				"unmarshal updates should succeed")
			return &updates
		}
		// Skip non-Updates packages (e.g. responses).
	}
}

// createTestConversation creates a 1-on-1 conversation between user1 and user2
// directly in the database (bypassing the WebSocket layer).
func createTestConversation(t *testing.T, s *store.Store, user1, user2 string) *model.Conversation {
	t.Helper()

	conv := &model.Conversation{
		ID:      uuid.New().String(),
		UserID1: user1,
		UserID2: user2,
		Type:    "1-on-1",
	}
	err := s.ConversationStore().Create(context.Background(), conv)
	require.NoError(t, err, "create conversation should succeed")
	return conv
}

// ---------------------------------------------------------------------------
// TC-1: TestBasicMessageDelivery
// Verifies: D-006 (idempotency), D-007 (fire-and-forget), D-008 (MessageID),
//           D-010 (passive renewal)
// ---------------------------------------------------------------------------

// TestBasicMessageDelivery verifies the happy path: Alice sends a message to
// Bob, both receive the push, and the database is consistent.
func TestBasicMessageDelivery(t *testing.T) {
	env := setupE2ETest(t)

	// 1. Connect Alice and Bob.
	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	// 2. Create conversation.
	conv := createTestConversation(t, env.store, "alice", "bob")

	// 3. Alice sends a message.
	clientMsgID := uuid.New().String()
	sendRequest(t, aliceConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Hello Bob!",
		"type":              "text",
	})

	// 4. Alice receives the response.
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "req-1", resp.ID, "response ID should match (D-006)")
	require.Equal(t, protocol.ResponseCodeOK, resp.Code, "response code should be OK")

	// Parse response data.
	var respData struct {
		Message   model.Message `json:"message"`
		Duplicate bool          `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &respData), "unmarshal response data")
	assert.Equal(t, "Hello Bob!", respData.Message.Content, "content should match")
	assert.Equal(t, "alice", respData.Message.SenderID, "sender_id should be alice")
	assert.Equal(t, uint32(1), respData.Message.MessageID, "message_id should be 1 (D-008)")
	assert.False(t, respData.Duplicate, "duplicate should be false on first send (D-006)")

	// 5. Bob receives the push update.
	bobUpdates := waitForUpdate(t, bobConn, 10*time.Second)
	require.Len(t, bobUpdates.Updates, 1, "bob should receive exactly 1 update")
	assert.Equal(t, uint32(1), bobUpdates.Updates[0].Seq, "bob's seq should be 1 (D-008)")
	assert.Equal(t, protocol.UpdateTypeMessage, bobUpdates.Updates[0].Type, "update Type should be 'message' (D-028)")

	var bobPayload model.Message
	require.NoError(t, json.Unmarshal(bobUpdates.Updates[0].Payload, &bobPayload),
		"unmarshal bob payload")
	assert.Equal(t, "Hello Bob!", bobPayload.Content, "bob payload content should match")

	// 6. Alice also receives the push (C-10: fan-out includes sender).
	aliceUpdates := waitForUpdate(t, aliceConn, 10*time.Second)
	require.Len(t, aliceUpdates.Updates, 1, "alice should also receive 1 update (C-10)")
	assert.Equal(t, protocol.UpdateTypeMessage, aliceUpdates.Updates[0].Type, "alice update Type should be 'message' (D-028)")

	// 7. DB verification.
	ctx := context.Background()
	var msgCount int64
	env.db.DB().WithContext(ctx).Model(&model.Message{}).Where("conversation_id = ?", conv.ID).Count(&msgCount)
	assert.Equal(t, int64(1), msgCount, "should have exactly 1 message in DB")

	var updateCount int64
	env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).Where("user_id IN ?", []string{"alice", "bob"}).Count(&updateCount)
	assert.Equal(t, int64(2), updateCount, "should have 2 user_updates (one per member) (D-007)")

	// Verify UserUpdate Type field.
	var typedUpdateCount int64
	env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).Where("user_id IN ? AND type = ?", []string{"alice", "bob"}, protocol.UpdateTypeMessage).Count(&typedUpdateCount)
	assert.Equal(t, int64(2), typedUpdateCount, "both user_updates should have Type='message' (D-028)")

	var updatedConv model.Conversation
	require.NoError(t, env.db.DB().WithContext(ctx).Where("id = ?", conv.ID).First(&updatedConv).Error)
	assert.Equal(t, uint32(1), updatedConv.LastProcessedMessageID,
		"conversation last_processed_message_id should be 1 (D-008)")
}

// ---------------------------------------------------------------------------
// TC-2: TestOfflineMessageSync
// Verifies: D-009 (sync_updates pagination)
// ---------------------------------------------------------------------------

// TestOfflineMessageSync verifies that an offline user can fetch messages
// they missed via sync_updates when they come back online.
func TestOfflineMessageSync(t *testing.T) {
	env := setupE2ETest(t)

	// 1. Alice connects and sends a message (Bob is offline).
	aliceConn := connectClient(t, env.addr, "alice", "alice")

	conv := createTestConversation(t, env.store, "alice", "bob")

	clientMsgID := uuid.New().String()
	sendRequest(t, aliceConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Are you there?",
		"type":              "text",
	})

	// 2. Alice gets the response.
	resp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send should succeed")

	// Consume Alice's push (C-10).
	_ = waitForUpdate(t, aliceConn, 15*time.Second)

	// 3. Alice disconnects.
	aliceConn.Close()

	// Wait for connection store to clean up Alice's stale connection before
	// Bob comes online; otherwise sync timing becomes flaky under CI load.
	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(context.Background(), "alice", 10)
		return err == nil && len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond, "alice should be disconnected")

	// 4. Bob comes online.
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	// 5. Bob requests sync_updates from the beginning.
	sendRequest(t, bobConn, "sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})

	syncResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, "sync-1", syncResp.ID, "response ID should match")
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync should succeed (D-009)")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData), "unmarshal sync data")

	assert.Len(t, syncData.Updates, 1, "should have 1 update for bob (D-009)")
	assert.False(t, syncData.HasMore, "has_more should be false")
	assert.Equal(t, uint32(1), syncData.LatestSeq, "latest_seq should be 1 (D-009)")

	if len(syncData.Updates) > 0 {
		assert.Equal(t, uint32(1), syncData.Updates[0].Seq, "update seq should be 1")
		assert.Equal(t, protocol.UpdateTypeMessage, syncData.Updates[0].Type, "update Type should be 'message' (D-028)")
		var payload model.Message
		require.NoError(t, json.Unmarshal(syncData.Updates[0].Payload, &payload))
		assert.Equal(t, "Are you there?", payload.Content, "payload content should match")
	}

	// 6. Bob requests again with after_seq=1 — should get empty.
	sendRequest(t, bobConn, "sync-2", "sync_updates", map[string]interface{}{
		"after_seq": 1,
		"limit":     100,
	})

	syncResp2 := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, syncResp2.Code, "second sync should succeed")

	var syncData2 struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp2.Data, &syncData2), "unmarshal sync data 2")

	assert.Empty(t, syncData2.Updates, "no updates after seq 1 (D-009)")
	assert.False(t, syncData2.HasMore, "has_more should be false")
}

// ---------------------------------------------------------------------------
// TC-3: TestMultipleMessageOrdering
// Verifies: D-008 (MessageID monotonic increment)
// ---------------------------------------------------------------------------

// TestMultipleMessageOrdering verifies that sequential messages in the same
// conversation receive strictly increasing MessageIDs and Seq values.
func TestMultipleMessageOrdering(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// 3. Send 3 messages sequentially and record message_ids.
	var messageIDs []uint32
	for i := 0; i < 3; i++ {
		reqID := fmt.Sprintf("req-%d", i+1)
		clientMsgID := uuid.New().String()
		sendRequest(t, aliceConn, reqID, "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": clientMsgID,
			"content":           fmt.Sprintf("Message %d", i+1),
			"type":              "text",
		})

		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "message %d should succeed", i+1)

		var respData struct {
			Message model.Message `json:"message"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &respData))
		messageIDs = append(messageIDs, respData.Message.MessageID)
	}

	// 4. Verify message_ids are 1, 2, 3 (D-008).
	assert.Equal(t, uint32(1), messageIDs[0], "first message_id should be 1 (D-008)")
	assert.Equal(t, uint32(2), messageIDs[1], "second message_id should be 2 (D-008)")
	assert.Equal(t, uint32(3), messageIDs[2], "third message_id should be 3 (D-008)")

	// 5. Collect Bob's push updates (3 updates, one per message).
	// Each message produces 2 pushes (bob + alice per C-10). We only care about
	// bob's updates here. The writePump may batch multiple broadcasts into a
	// single WebSocket frame, so we accumulate seqs rather than asserting
	// exactly 1 update per frame.
	var bobSeqs []uint32
	for len(bobSeqs) < 3 {
		updates := waitForUpdate(t, bobConn, 10*time.Second)
		for _, u := range updates.Updates {
			bobSeqs = append(bobSeqs, u.Seq)
		}
	}

	// 6. Verify bob's seqs are 1, 2, 3.
	require.Len(t, bobSeqs, 3, "bob should receive exactly 3 updates (D-008)")
	assert.Equal(t, uint32(1), bobSeqs[0], "bob first seq should be 1 (D-008)")
	assert.Equal(t, uint32(2), bobSeqs[1], "bob second seq should be 2 (D-008)")
	assert.Equal(t, uint32(3), bobSeqs[2], "bob third seq should be 3 (D-008)")

	// Consume Alice's push updates to keep the connection clean.
	var aliceSeqCount int
	for aliceSeqCount < 3 {
		updates := waitForUpdate(t, aliceConn, 10*time.Second)
		aliceSeqCount += len(updates.Updates)
	}
}

// ---------------------------------------------------------------------------
// TC-4: TestMessageIdempotency
// Verifies: D-006 (client_message_id idempotency)
// ---------------------------------------------------------------------------

// TestMessageIdempotency verifies that sending the same client_message_id
// twice returns the same message with duplicate=true, and does not create
// additional database records.
func TestMessageIdempotency(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	clientMsgID := "msg-dup-" + uuid.New().String()

	// 3. First send.
	sendRequest(t, aliceConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Test",
		"type":              "text",
	})

	resp1 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp1.Code, "first send should succeed")

	var respData1 struct {
		Message   model.Message `json:"message"`
		Duplicate bool          `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp1.Data, &respData1))
	assert.False(t, respData1.Duplicate, "first send should not be duplicate (D-006)")
	firstMsgID := respData1.Message.ID
	firstContent := respData1.Message.Content

	// 5. Consume first push to Bob and Alice.
	_ = waitForUpdate(t, bobConn, 10*time.Second)
	_ = waitForUpdate(t, aliceConn, 10*time.Second)

	// 7. Second send with same client_message_id.
	sendRequest(t, aliceConn, "req-2", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Test",
		"type":              "text",
	})

	resp2 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp2.Code, "duplicate send should succeed (D-006)")

	var respData2 struct {
		Message   model.Message `json:"message"`
		Duplicate bool          `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp2.Data, &respData2))
	assert.True(t, respData2.Duplicate, "second send should be duplicate (D-006)")
	assert.Equal(t, firstMsgID, respData2.Message.ID, "duplicate should return same message ID (D-006)")
	assert.Equal(t, firstContent, respData2.Message.Content, "duplicate should return same content (D-006)")

	// 9. DB verification: no new records created.
	ctx := context.Background()
	var msgCount int64
	env.db.DB().WithContext(ctx).Model(&model.Message{}).
		Where("client_message_id = ?", clientMsgID).Count(&msgCount)
	assert.Equal(t, int64(1), msgCount, "should have exactly 1 message (D-006)")

	var updateCount int64
	env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).
		Where("user_id IN ?", []string{"alice", "bob"}).Count(&updateCount)
	assert.Equal(t, int64(2), updateCount, "should have exactly 2 user_updates (D-006)")

	// Verify UserUpdate Type field.
	var typedUpdateCount int64
	env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).
		Where("user_id IN ? AND type = ?", []string{"alice", "bob"}, protocol.UpdateTypeMessage).Count(&typedUpdateCount)
	assert.Equal(t, int64(2), typedUpdateCount, "both user_updates should have Type='message' (D-028)")
}

// ---------------------------------------------------------------------------
// TC-5: TestHeartbeatKeepAlive
// Verifies: D-010 (passive TTL renewal via heartbeat)
// ---------------------------------------------------------------------------

// TestHeartbeatKeepAlive verifies that sending a heartbeat refreshes the
// connection TTL in Redis (D-010).
func TestHeartbeatKeepAlive(t *testing.T) {
	env := setupE2ETest(t)

	// 1. Alice connects.
	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()

	// 2. Wait for connection registration.
	ctx, cancelCtx := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCtx()
	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(ctx, "alice", 10)
		return err == nil && len(conns) > 0
	}, 3*time.Second, 50*time.Millisecond, "alice should be registered in connStore")

	// 3. First heartbeat.
	sendRequest(t, aliceConn, "hb-1", "heartbeat", map[string]interface{}{})
	resp1 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp1.Code, "first heartbeat should succeed (D-010)")

	var hbStatus1 struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(resp1.Data, &hbStatus1), "unmarshal first heartbeat data")
	assert.Equal(t, "ok", hbStatus1.Status, "first heartbeat status should be 'ok'")

	// 5. Get alice's connID.
	conns, err := env.connStore.ListByUser(ctx, "alice", 10)
	require.NoError(t, err, "ListByUser should succeed")
	require.NotEmpty(t, conns, "alice should have at least 1 connection")
	connID := conns[0].ID

	// 6. Read PTTL from Redis.
	redisClient := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	defer func() { _ = redisClient.Close() }()

	infoKey := env.redisKey + "xyncra:conn:info:" + connID
	ttl1, err := redisClient.PTTL(ctx, infoKey).Result()
	require.NoError(t, err, "PTTL should succeed")
	t.Logf("PTTL after first heartbeat: %v", ttl1)

	// 7. Wait some time for TTL to decrease.
	time.Sleep(500 * time.Millisecond)

	// 8. Second heartbeat.
	sendRequest(t, aliceConn, "hb-2", "heartbeat", map[string]interface{}{})
	resp2 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp2.Code, "second heartbeat should succeed (D-010)")

	var hbStatus2 struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(resp2.Data, &hbStatus2), "unmarshal second heartbeat data")
	assert.Equal(t, "ok", hbStatus2.Status, "second heartbeat status should be 'ok'")

	// 10. Read PTTL again.
	ttl2, err := redisClient.PTTL(ctx, infoKey).Result()
	require.NoError(t, err, "PTTL should succeed after second heartbeat")
	t.Logf("PTTL after second heartbeat: %v", ttl2)

	// 11. Verify TTL was refreshed.
	assert.Greater(t, ttl2, 4*time.Second,
		"TTL should be refreshed to ~5s after heartbeat (D-010)")
	assert.Greater(t, ttl2, ttl1, "TTL should be refreshed after second heartbeat (D-010)")
}

// ---------------------------------------------------------------------------
// TC-6: TestNonMemberSendRejected
// ---------------------------------------------------------------------------

// TestNonMemberSendRejected verifies that a user who is not a member of a
// conversation cannot send messages to it.
func TestNonMemberSendRejected(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Eve is not a member.
	eveConn := connectClient(t, env.addr, "eve", "eve")
	defer eveConn.Close()

	clientMsgID := uuid.New().String()
	sendRequest(t, eveConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Hack!",
		"type":              "text",
	})

	resp := readResponse(t, eveConn, 5*time.Second)
	assert.Equal(t, protocol.ResponseCodePermissionDenied, resp.Code,
		"non-member send should be rejected")
	assert.True(t, strings.Contains(strings.ToLower(resp.Msg), "not a member"),
		"error message should mention 'not a member', got: %s", resp.Msg)
}

// ---------------------------------------------------------------------------
// TC-7: TestSendToNonexistentConversation
// ---------------------------------------------------------------------------

// TestSendToNonexistentConversation verifies that sending a message to a
// conversation that does not exist returns an error.
func TestSendToNonexistentConversation(t *testing.T) {
	env := setupE2ETest(t)

	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()

	clientMsgID := uuid.New().String()
	sendRequest(t, aliceConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   uuid.New().String(),
		"client_message_id": clientMsgID,
		"content":           "Hello",
		"type":              "text",
	})

	resp := readResponse(t, aliceConn, 5*time.Second)
	assert.Equal(t, protocol.ResponseCodeNotFound, resp.Code,
		"send to nonexistent conversation should fail")
	assert.True(t, strings.Contains(strings.ToLower(resp.Msg), "not found"),
		"error message should mention 'not found', got: %s", resp.Msg)
}

// ---------------------------------------------------------------------------
// TC-8: TestSendMessageValidation
// ---------------------------------------------------------------------------

// TestSendMessageValidation is a table-driven test that verifies required
// field validation for the send_message method.
func TestSendMessageValidation(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]interface{}
		expect string
	}{
		{
			name: "missing conversation_id",
			params: map[string]interface{}{
				"client_message_id": "x-" + uuid.New().String(),
				"content":           "y",
			},
			expect: "conversation_id",
		},
		{
			name: "missing client_message_id",
			params: map[string]interface{}{
				"conversation_id": "c",
				"content":         "y",
			},
			expect: "client_message_id",
		},
		{
			name: "missing content",
			params: map[string]interface{}{
				"conversation_id":   "c",
				"client_message_id": "x-" + uuid.New().String(),
			},
			expect: "content",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := setupE2ETest(t)

			aliceConn := connectClient(t, env.addr, "alice", "alice")
			defer aliceConn.Close()

			sendRequest(t, aliceConn, "req-1", "send_message", tc.params)

			resp := readResponse(t, aliceConn, 5*time.Second)
			assert.Equal(t, protocol.ResponseCodeValidationError, resp.Code,
				"validation should fail for %s", tc.name)
			assert.True(t, strings.Contains(strings.ToLower(resp.Msg), tc.expect),
				"error message should mention %q, got: %s", tc.expect, resp.Msg)
		})
	}
}

// ---------------------------------------------------------------------------
// wsConn — channel-backed WebSocket reader
// ---------------------------------------------------------------------------
//
// gorilla/websocket v1.5.3 permanently poisons a connection after any
// ReadMessage error (including timeouts): NextReader stores the error in
// c.readErr, and every subsequent read returns the same error.  The old
// drainPushUpdates implementation called ReadMessage in a loop until it
// timed out, which killed the connection for the rest of the test.
//
// wsConn solves this by running a dedicated reader goroutine that feeds
// messages into a buffered channel.  Drain / recv timeouts operate on the
// channel (via select + time.After) and never touch the underlying
// connection's read path, so the WebSocket stays healthy.
//
// Concurrency: gorilla/websocket allows one concurrent reader and one
// concurrent writer.  The reader goroutine owns reads; the test goroutine
// owns writes via the embedded *websocket.Conn — exactly one of each.

// msgResult is a single message (or error) delivered by the reader goroutine.
type msgResult struct {
	messageType int
	data        []byte
	err         error
}

// wsConn wraps *websocket.Conn with a background reader goroutine.
type wsConn struct {
	*websocket.Conn                // promoted for WriteMessage / Close
	msgCh           chan msgResult // buffered channel of incoming messages
	done            chan struct{}  // closed to stop the reader goroutine
	stopOnce        sync.Once
}

// wrapConn starts the reader goroutine and returns a *wsConn.
func wrapConn(conn *websocket.Conn) *wsConn {
	wc := &wsConn{
		Conn:  conn,
		msgCh: make(chan msgResult, 256),
		done:  make(chan struct{}),
	}
	go wc.readPump()
	return wc
}

// readPump continuously reads from the WebSocket and delivers results to
// msgCh.  It exits when the connection is closed (ReadMessage returns an
// error) or when done is closed.
func (wc *wsConn) readPump() {
	defer close(wc.msgCh)
	for {
		mt, data, err := wc.Conn.ReadMessage()
		select {
		case wc.msgCh <- msgResult{mt, data, err}:
		case <-wc.done:
			return
		}
		if err != nil {
			return
		}
	}
}

// recv reads one message from the channel, blocking up to timeout.
func (wc *wsConn) recv(timeout time.Duration) (int, []byte, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r, ok := <-wc.msgCh:
		if !ok {
			return 0, nil, fmt.Errorf("wsConn: reader goroutine exited")
		}
		return r.messageType, r.data, r.err
	case <-timer.C:
		return 0, nil, fmt.Errorf("wsConn: recv timeout after %v", timeout)
	}
}

// stop terminates the reader goroutine and closes the underlying connection.
// Safe to call multiple times.
func (wc *wsConn) stop() {
	wc.stopOnce.Do(func() {
		close(wc.done)
		_ = wc.Conn.Close()
	})
}

// Close shadows the embedded *websocket.Conn.Close to also stop the reader
// goroutine.  This ensures that `defer conn.Close()` in tests cleans up both
// the WebSocket connection and the background reader goroutine.
func (wc *wsConn) Close() error {
	wc.stopOnce.Do(func() {
		close(wc.done)
	})
	return wc.Conn.Close()
}

// ---------------------------------------------------------------------------
// drainPushUpdates reads and discards push update packages from a connection
// until no more are available within a short timeout. This prevents stale
// updates from interfering with subsequent assertions.
//
// It operates on the wsConn channel — never on the underlying WebSocket —
// so the connection remains healthy for subsequent reads.
// ---------------------------------------------------------------------------
func drainPushUpdates(t *testing.T, conn *wsConn) {
	t.Helper()
	for {
		select {
		case r, ok := <-conn.msgCh:
			if !ok {
				return // reader exited
			}
			if r.err != nil {
				return // read error — stop draining
			}
			// discard message, keep draining
		case <-time.After(500 * time.Millisecond):
			return // no more messages within the window
		}
	}
}

// ---------------------------------------------------------------------------
// TC-9: TestGetConversationE2E
// Verifies: get_conversation returns conversation with unread_count (D-012)
// ---------------------------------------------------------------------------

// TestGetConversationE2E verifies the full get_conversation flow: create
// conversation, send messages, and verify the response includes unread_count.
func TestGetConversationE2E(t *testing.T) {
	env := setupE2ETest(t)

	// 1. Connect alice and bob.
	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	// 2. Create conversation in DB.
	conv := createTestConversation(t, env.store, "alice", "bob")

	// 3. Alice sends 2 messages.
	var messageIDs []string
	for i := 0; i < 2; i++ {
		clientMsgID := uuid.New().String()
		sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": clientMsgID,
			"content":           fmt.Sprintf("Message %d", i+1),
			"type":              "text",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)

		var respData struct {
			Message model.Message `json:"message"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &respData))
		messageIDs = append(messageIDs, respData.Message.ID)
	}

	// 4. Consume push updates for both connections.
	// Each send_message generates a push to bob and a push to alice (C-10).
	// We need to drain these so they don't interfere with later reads.
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// 5. Alice calls get_conversation — verify conversation returned with unread_count.
	sendRequest(t, aliceConn, "get-conv-1", "get_conversation", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	aliceResp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, aliceResp.Code, "alice get_conversation should succeed")

	var aliceData struct {
		Conversation model.Conversation `json:"conversation"`
		UnreadCount  int64              `json:"unread_count"`
	}
	require.NoError(t, json.Unmarshal(aliceResp.Data, &aliceData))
	assert.Equal(t, conv.ID, aliceData.Conversation.ID, "conversation ID should match")
	// Alice sent the messages, so her read cursor is at 0 (no mark_as_read called).
	// She has 2 unread messages (she hasn't marked them as read).
	assert.Equal(t, int64(2), aliceData.UnreadCount, "alice should have 2 unread messages")

	// 6. Bob calls get_conversation — verify conversation returned with unread_count.
	sendRequest(t, bobConn, "get-conv-2", "get_conversation", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	bobResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, bobResp.Code, "bob get_conversation should succeed")

	var bobData struct {
		Conversation model.Conversation `json:"conversation"`
		UnreadCount  int64              `json:"unread_count"`
	}
	require.NoError(t, json.Unmarshal(bobResp.Data, &bobData))
	assert.Equal(t, conv.ID, bobData.Conversation.ID, "conversation ID should match")
	assert.Equal(t, int64(2), bobData.UnreadCount, "bob should have 2 unread messages")
}

// ---------------------------------------------------------------------------
// TC-10: TestMarkAsReadAndGetConversationE2E
// Verifies: mark_as_read decreases unread_count, get_conversation reflects it (D-012)
// ---------------------------------------------------------------------------

// TestMarkAsReadAndGetConversationE2E verifies the full mark_as_read +
// get_conversation flow: unread_count decreases after mark_as_read, and
// subsequent messages increase it again.
func TestMarkAsReadAndGetConversationE2E(t *testing.T) {
	env := setupE2ETest(t)

	// 1. Connect alice and bob.
	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	// 2. Create conversation, alice sends 3 messages.
	conv := createTestConversation(t, env.store, "alice", "bob")
	var lastMessageID uint32
	for i := 0; i < 3; i++ {
		clientMsgID := uuid.New().String()
		sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": clientMsgID,
			"content":           fmt.Sprintf("Message %d", i+1),
			"type":              "text",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)

		var respData struct {
			Message model.Message `json:"message"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &respData))
		lastMessageID = respData.Message.MessageID
	}

	// Consume push updates.
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// 3. Bob calls mark_as_read — verify status "ok", unread_count == 0.
	sendRequest(t, bobConn, "mark-1", "mark_as_read", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	markResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, markResp.Code, "mark_as_read should succeed")

	var markData struct {
		Status            string `json:"status"`
		UnreadCount       int64  `json:"unread_count"`
		LastReadMessageID uint32 `json:"last_read_message_id"`
	}
	require.NoError(t, json.Unmarshal(markResp.Data, &markData))
	assert.Equal(t, "ok", markData.Status, "mark_as_read status should be 'ok'")
	assert.Equal(t, int64(0), markData.UnreadCount, "bob unread_count should be 0 after mark_as_read")
	assert.Equal(t, lastMessageID, markData.LastReadMessageID, "last_read_message_id should be 3")

	// 4. Bob calls get_conversation — verify unread_count == 0.
	sendRequest(t, bobConn, "get-conv-1", "get_conversation", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	bobResp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, bobResp.Code, "bob get_conversation should succeed")

	var bobData struct {
		Conversation model.Conversation `json:"conversation"`
		UnreadCount  int64              `json:"unread_count"`
	}
	require.NoError(t, json.Unmarshal(bobResp.Data, &bobData))
	assert.Equal(t, int64(0), bobData.UnreadCount, "bob unread_count should be 0 in get_conversation")

	// 5. Alice sends 2 more messages.
	for i := 0; i < 2; i++ {
		clientMsgID := uuid.New().String()
		sendRequest(t, aliceConn, fmt.Sprintf("send-more-%d", i+1), "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": clientMsgID,
			"content":           fmt.Sprintf("Message %d", i+4),
			"type":              "text",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+4)
	}

	// Consume push updates.
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// 6. Bob calls get_conversation — verify unread_count == 2.
	sendRequest(t, bobConn, "get-conv-2", "get_conversation", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	bobResp2 := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, bobResp2.Code, "bob get_conversation should succeed")

	var bobData2 struct {
		Conversation model.Conversation `json:"conversation"`
		UnreadCount  int64              `json:"unread_count"`
	}
	require.NoError(t, json.Unmarshal(bobResp2.Data, &bobData2))
	assert.Equal(t, int64(2), bobData2.UnreadCount, "bob unread_count should be 2 after 2 new messages")
}

// ---------------------------------------------------------------------------
// TC-11: TestDeleteAndRestoreConversationE2E
// Verifies: delete_conversation cascade soft delete (D-013),
//           restore_conversation cascade restore (D-015)
// ---------------------------------------------------------------------------

// TestDeleteAndRestoreConversationE2E verifies the full delete/restore flow:
// delete cascade soft-deletes conversation + messages, restore cascade restores
// them, and get_messages works after restore.
func TestDeleteAndRestoreConversationE2E(t *testing.T) {
	env := setupE2ETest(t)

	// 1. Connect alice and bob.
	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	// 2. Create conversation, alice sends 2 messages.
	conv := createTestConversation(t, env.store, "alice", "bob")
	for i := 0; i < 2; i++ {
		clientMsgID := uuid.New().String()
		sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": clientMsgID,
			"content":           fmt.Sprintf("Message %d", i+1),
			"type":              "text",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)
	}

	// 3. Consume responses and push updates.
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// 4. Alice calls delete_conversation — verify status "ok", deleted_message_count == 2.
	sendRequest(t, aliceConn, "del-conv-1", "delete_conversation", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	delResp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, delResp.Code, "delete_conversation should succeed")

	var delData struct {
		Status              string `json:"status"`
		DeletedMessageCount int64  `json:"deleted_message_count"`
	}
	require.NoError(t, json.Unmarshal(delResp.Data, &delData))
	assert.Equal(t, "ok", delData.Status, "delete_conversation status should be 'ok'")
	assert.Equal(t, int64(2), delData.DeletedMessageCount, "should have deleted 2 messages (D-013)")

	// 5. Alice calls get_messages — error "conversation not found" (deleted).
	sendRequest(t, aliceConn, "get-msgs-1", "get_messages", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	errResp := readResponse(t, aliceConn, 5*time.Second)
	assert.Equal(t, protocol.ResponseCodeNotFound, errResp.Code, "get_messages should fail after delete")
	assert.True(t, strings.Contains(strings.ToLower(errResp.Msg), "not found"),
		"error should mention 'not found', got: %s", errResp.Msg)

	// 6. Alice calls restore_conversation — verify conversation returned,
	//    restored_message_count == 2.
	sendRequest(t, aliceConn, "restore-1", "restore_conversation", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	restoreResp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, restoreResp.Code, "restore_conversation should succeed")

	var restoreData struct {
		Conversation         model.Conversation `json:"conversation"`
		RestoredMessageCount int64              `json:"restored_message_count"`
	}
	require.NoError(t, json.Unmarshal(restoreResp.Data, &restoreData))
	assert.Equal(t, conv.ID, restoreData.Conversation.ID, "restored conversation ID should match")
	assert.Equal(t, int64(2), restoreData.RestoredMessageCount, "should have restored 2 messages (D-015)")

	// 7. Alice calls get_messages — verify 2 messages returned.
	sendRequest(t, aliceConn, "get-msgs-2", "get_messages", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	msgsResp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, msgsResp.Code, "get_messages after restore should succeed")

	var msgsData struct {
		Messages []*model.Message `json:"messages"`
		HasMore  bool             `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(msgsResp.Data, &msgsData))
	assert.Len(t, msgsData.Messages, 2, "should have 2 messages after restore")
	assert.False(t, msgsData.HasMore, "has_more should be false")
}

// ---------------------------------------------------------------------------
// TC-12: TestDeleteMessageE2E
// Verifies: delete_message soft deletes a single message (D-014)
// ---------------------------------------------------------------------------

// TestDeleteMessageE2E verifies the full delete_message flow: send messages,
// delete one, and verify only the remaining message is returned by get_messages.
func TestDeleteMessageE2E(t *testing.T) {
	env := setupE2ETest(t)

	// 1. Connect alice and bob.
	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	// 2. Create conversation, alice sends 2 messages.
	conv := createTestConversation(t, env.store, "alice", "bob")
	var firstMsgID string
	for i := 0; i < 2; i++ {
		clientMsgID := uuid.New().String()
		sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": clientMsgID,
			"content":           fmt.Sprintf("Message %d", i+1),
			"type":              "text",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)

		var respData struct {
			Message model.Message `json:"message"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &respData))
		if i == 0 {
			firstMsgID = respData.Message.ID
		}
	}

	// 3. Consume responses and push updates.
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// 4. Alice calls delete_message on first message — verify status "ok".
	sendRequest(t, aliceConn, "del-msg-1", "delete_message", map[string]interface{}{
		"message_id": firstMsgID,
	})
	delResp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, delResp.Code, "delete_message should succeed")

	var delData struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(delResp.Data, &delData))
	assert.Equal(t, "ok", delData.Status, "delete_message status should be 'ok'")

	// 5. Alice calls get_messages — verify only 1 message returned (second one).
	sendRequest(t, aliceConn, "get-msgs-1", "get_messages", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	msgsResp := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, msgsResp.Code, "get_messages should succeed")

	var msgsData struct {
		Messages []*model.Message `json:"messages"`
		HasMore  bool             `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(msgsResp.Data, &msgsData))
	assert.Len(t, msgsData.Messages, 1, "should have only 1 message after deleting first (D-014)")
	assert.Equal(t, "Message 2", msgsData.Messages[0].Content, "remaining message should be the second one")
}

// ---------------------------------------------------------------------------
// TC-13: TestNonMemberOperationsRejectedE2E
// Verifies: non-member operations are rejected for get_conversation,
//           delete_conversation, and mark_as_read (C-3)
// ---------------------------------------------------------------------------

// TestNonMemberOperationsRejectedE2E verifies that a user who is not a member
// of a conversation cannot call get_conversation, delete_conversation, or
// mark_as_read on it.
func TestNonMemberOperationsRejectedE2E(t *testing.T) {
	env := setupE2ETest(t)

	// 1. Connect alice, bob, and eve (non-member).
	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()
	eveConn := connectClient(t, env.addr, "eve", "eve")
	defer eveConn.Close()

	// 2. Create conversation between alice and bob.
	conv := createTestConversation(t, env.store, "alice", "bob")

	// 3. Eve calls get_conversation — error "not a member".
	sendRequest(t, eveConn, "eve-get-1", "get_conversation", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	getResp := readResponse(t, eveConn, 5*time.Second)
	assert.Equal(t, protocol.ResponseCodePermissionDenied, getResp.Code,
		"eve get_conversation should be rejected")
	assert.True(t, strings.Contains(strings.ToLower(getResp.Msg), "not a member"),
		"error should mention 'not a member', got: %s", getResp.Msg)

	// 4. Eve calls delete_conversation — error "not a member".
	sendRequest(t, eveConn, "eve-del-1", "delete_conversation", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	delResp := readResponse(t, eveConn, 5*time.Second)
	assert.Equal(t, protocol.ResponseCodePermissionDenied, delResp.Code,
		"eve delete_conversation should be rejected")
	assert.True(t, strings.Contains(strings.ToLower(delResp.Msg), "not a member"),
		"error should mention 'not a member', got: %s", delResp.Msg)

	// 5. Eve calls mark_as_read — error "not a member".
	sendRequest(t, eveConn, "eve-mark-1", "mark_as_read", map[string]interface{}{
		"conversation_id": conv.ID,
	})
	markResp := readResponse(t, eveConn, 5*time.Second)
	assert.Equal(t, protocol.ResponseCodePermissionDenied, markResp.Code,
		"eve mark_as_read should be rejected")
	assert.True(t, strings.Contains(strings.ToLower(markResp.Msg), "not a member"),
		"error should mention 'not a member', got: %s", markResp.Msg)
}

// ---------------------------------------------------------------------------
// TC-14: TestCreateConversationE2E
// Verifies: D-011 (find-or-create idempotency)
// Scenarios:
//   CC-1  Happy path: Alice creates conversation with Bob (duplicate=false)
//   CC-2  Idempotent: Alice creates again, same ID, duplicate=true
//   CC-3  Reverse: Bob creates with Alice, same conversation (D-011 bidirectional)
//   CC-4  Missing user_id returns error containing "user_id"
//   CC-5  Self-conversation returns error containing "yourself"
//   CC-6  With title: title field present in response
//   CC-7  Concurrent creates: exactly one conversation in DB
// ---------------------------------------------------------------------------

// TestCreateConversationE2E verifies the full create_conversation flow:
// first call creates, second call returns existing with duplicate=true.
func TestCreateConversationE2E(t *testing.T) {
	env := setupE2ETest(t)

	// Setup: connect alice and bob.
	aliceConn := connectClient(t, env.addr, "alice", "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob", "bob")
	defer bobConn.Close()

	// Drain any startup messages.
	drainPushUpdates(t, aliceConn)
	drainPushUpdates(t, bobConn)

	// -----------------------------------------------------------------------
	// CC-1: Happy path — Alice creates conversation with Bob.
	// -----------------------------------------------------------------------
	sendRequest(t, aliceConn, "create-1", "create_conversation", map[string]interface{}{
		"user_id": "bob",
	})

	resp1 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "create-1", resp1.ID, "response ID should match (CC-1)")
	require.Equal(t, protocol.ResponseCodeOK, resp1.Code, "first create should succeed (D-011)")

	var data1 struct {
		Conversation struct {
			ID      string `json:"ID"`
			UserID1 string `json:"UserID1"`
			UserID2 string `json:"UserID2"`
			Title   string `json:"Title"`
			Type    string `json:"Type"`
		} `json:"conversation"`
		Duplicate bool `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp1.Data, &data1), "unmarshal create response (CC-1)")

	assert.NotEmpty(t, data1.Conversation.ID, "conversation ID should be non-empty (D-011)")
	assert.Equal(t, "alice", data1.Conversation.UserID1, "user_id_1 should be alice (D-011)")
	assert.Equal(t, "bob", data1.Conversation.UserID2, "user_id_2 should be bob (D-011)")
	assert.Equal(t, "1-on-1", data1.Conversation.Type, "type should be 1-on-1")
	assert.False(t, data1.Duplicate, "first create should have duplicate=false (D-011)")

	firstConvID := data1.Conversation.ID

	// Consume push updates from CC-1.
	drainPushUpdates(t, aliceConn)
	drainPushUpdates(t, bobConn)

	// -----------------------------------------------------------------------
	// CC-2: Idempotent — Alice creates again with Bob, duplicate=true, same ID.
	// -----------------------------------------------------------------------
	sendRequest(t, aliceConn, "create-2", "create_conversation", map[string]interface{}{
		"user_id": "bob",
	})

	resp2 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "create-2", resp2.ID, "response ID should match (CC-2)")
	require.Equal(t, protocol.ResponseCodeOK, resp2.Code, "duplicate create should succeed (D-011)")

	var data2 struct {
		Conversation struct {
			ID      string `json:"ID"`
			UserID1 string `json:"UserID1"`
			UserID2 string `json:"UserID2"`
		} `json:"conversation"`
		Duplicate bool `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp2.Data, &data2), "unmarshal duplicate response (CC-2)")

	assert.Equal(t, firstConvID, data2.Conversation.ID, "same conversation ID on duplicate (D-011)")
	assert.True(t, data2.Duplicate, "second create should have duplicate=true (D-011)")

	// Consume push updates from CC-2 (if any).
	drainPushUpdates(t, aliceConn)
	drainPushUpdates(t, bobConn)

	// -----------------------------------------------------------------------
	// CC-3: Reverse — Bob creates with Alice, same conversation (D-011
	// bidirectional).
	// -----------------------------------------------------------------------
	sendRequest(t, bobConn, "create-3", "create_conversation", map[string]interface{}{
		"user_id": "alice",
	})

	resp3 := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, "create-3", resp3.ID, "response ID should match (CC-3)")
	require.Equal(t, protocol.ResponseCodeOK, resp3.Code, "reverse create should succeed (D-011)")

	var data3 struct {
		Conversation struct {
			ID      string `json:"ID"`
			UserID1 string `json:"UserID1"`
			UserID2 string `json:"UserID2"`
		} `json:"conversation"`
		Duplicate bool `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp3.Data, &data3), "unmarshal reverse response (CC-3)")

	assert.Equal(t, firstConvID, data3.Conversation.ID,
		"reverse create should return same conversation ID (D-011 bidirectional)")
	assert.True(t, data3.Duplicate,
		"reverse create should have duplicate=true (D-011 bidirectional)")

	// Consume push updates from CC-3.
	drainPushUpdates(t, bobConn)
	drainPushUpdates(t, aliceConn)

	// -----------------------------------------------------------------------
	// CC-4: Missing user_id — error contains "user_id".
	// -----------------------------------------------------------------------
	sendRequest(t, aliceConn, "create-4", "create_conversation", map[string]interface{}{})

	resp4 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "create-4", resp4.ID, "response ID should match (CC-4)")
	assert.Equal(t, protocol.ResponseCodeValidationError, resp4.Code,
		"missing user_id should fail (CC-4)")
	assert.True(t, strings.Contains(strings.ToLower(resp4.Msg), "user_id"),
		"error should mention 'user_id', got: %s (CC-4)", resp4.Msg)

	// -----------------------------------------------------------------------
	// CC-5: Self-conversation — error contains "yourself".
	// -----------------------------------------------------------------------
	sendRequest(t, aliceConn, "create-5", "create_conversation", map[string]interface{}{
		"user_id": "alice",
	})

	resp5 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "create-5", resp5.ID, "response ID should match (CC-5)")
	assert.Equal(t, protocol.ResponseCodeValidationError, resp5.Code,
		"self-conversation should fail (CC-5)")
	assert.True(t, strings.Contains(strings.ToLower(resp5.Msg), "yourself"),
		"error should mention 'yourself', got: %s (CC-5)", resp5.Msg)

	// -----------------------------------------------------------------------
	// CC-6: With title — title field present in response.
	// -----------------------------------------------------------------------
	sendRequest(t, aliceConn, "create-6", "create_conversation", map[string]interface{}{
		"user_id": "eve",
		"title":   "Chat with Eve",
	})

	resp6 := readResponse(t, aliceConn, 5*time.Second)
	require.Equal(t, "create-6", resp6.ID, "response ID should match (CC-6)")
	require.Equal(t, protocol.ResponseCodeOK, resp6.Code,
		"create with title should succeed (CC-6)")

	var data6 struct {
		Conversation struct {
			ID    string `json:"ID"`
			Title string `json:"Title"`
		} `json:"conversation"`
		Duplicate bool `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(resp6.Data, &data6), "unmarshal title response (CC-6)")

	assert.Equal(t, "Chat with Eve", data6.Conversation.Title,
		"title should be preserved in response (CC-6)")
	assert.False(t, data6.Duplicate, "first create with title should have duplicate=false (CC-6)")
	assert.NotEmpty(t, data6.Conversation.ID, "conversation ID should be non-empty (CC-6)")

	// Consume push updates from CC-6.
	drainPushUpdates(t, aliceConn)

	// -----------------------------------------------------------------------
	// CC-7: Concurrent creates — only one conversation should exist in DB.
	// -----------------------------------------------------------------------
	// Use a fresh pair to avoid interaction with prior scenarios.
	charlieConn := connectClient(t, env.addr, "charlie", "charlie")
	defer charlieConn.Close()
	daveConn := connectClient(t, env.addr, "dave", "dave")
	defer daveConn.Close()

	drainPushUpdates(t, charlieConn)
	drainPushUpdates(t, daveConn)

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: charlie creates with dave.
	go func() {
		defer wg.Done()
		sendRequest(t, charlieConn, "create-7a", "create_conversation", map[string]interface{}{
			"user_id": "dave",
		})
		resp := readResponse(t, charlieConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code,
			"concurrent create from charlie should succeed (CC-7)")
	}()

	// Goroutine 2: dave creates with charlie (reverse direction).
	go func() {
		defer wg.Done()
		sendRequest(t, daveConn, "create-7b", "create_conversation", map[string]interface{}{
			"user_id": "charlie",
		})
		resp := readResponse(t, daveConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code,
			"concurrent create from dave should succeed (CC-7)")
	}()

	wg.Wait()

	// Drain any push updates.
	drainPushUpdates(t, charlieConn)
	drainPushUpdates(t, daveConn)

	// Verify: exactly 1 conversation between charlie and dave in DB.
	ctx := context.Background()
	var convCount int64
	env.db.DB().WithContext(ctx).Model(&model.Conversation{}).
		Where("(user_id1 = ? AND user_id2 = ?) OR (user_id1 = ? AND user_id2 = ?)",
			"charlie", "dave", "dave", "charlie").
		Count(&convCount)
	assert.Equal(t, int64(1), convCount,
		"exactly 1 conversation should exist between charlie and dave after concurrent creates (D-011)")
}

// ---------------------------------------------------------------------------
// TC-15: TestListConversationsE2E
// Verifies: list_conversations pagination, ordering, isolation
// ---------------------------------------------------------------------------

// TestListConversationsE2E verifies the list_conversations flow:
// pagination with offset/limit, ordering by LastMessageAt DESC, user isolation.
func TestListConversationsE2E(t *testing.T) {
	// -----------------------------------------------------------------------
	// LC-1: Happy path — user with 3 conversations gets all 3 back.
	// -----------------------------------------------------------------------
	t.Run("LC-1_HappyPath", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		// Pre-create 3 conversations for alice with different users.
		conv1 := createTestConversation(t, env.store, "alice", "bob")
		conv2 := createTestConversation(t, env.store, "alice", "charlie")
		conv3 := createTestConversation(t, env.store, "alice", "eve")

		// Drain any push updates.
		drainPushUpdates(t, aliceConn)

		// Call list_conversations with default params.
		sendRequest(t, aliceConn, "list-1", "list_conversations", map[string]interface{}{})
		resp := readResponse(t, aliceConn, 5*time.Second)

		require.Equal(t, "list-1", resp.ID, "response ID should match (LC-1)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "list_conversations should succeed (LC-1)")

		var data struct {
			Conversations []struct {
				ID      string `json:"ID"`
				UserID1 string `json:"UserID1"`
				UserID2 string `json:"UserID2"`
			} `json:"conversations"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (LC-1)")

		assert.Len(t, data.Conversations, 3, "should return 3 conversations (LC-1)")
		assert.False(t, data.HasMore, "has_more should be false when all fit (LC-1)")

		// Verify all conversation IDs are present.
		convIDs := make(map[string]bool)
		for _, c := range data.Conversations {
			convIDs[c.ID] = true
		}
		assert.True(t, convIDs[conv1.ID], "should contain conv1 (LC-1)")
		assert.True(t, convIDs[conv2.ID], "should contain conv2 (LC-1)")
		assert.True(t, convIDs[conv3.ID], "should contain conv3 (LC-1)")
	})

	// -----------------------------------------------------------------------
	// LC-2: Empty list — new user with no conversations.
	// -----------------------------------------------------------------------
	t.Run("LC-2_EmptyList", func(t *testing.T) {
		env := setupE2ETest(t)

		// "newuser" has never been part of any conversation.
		newConn := connectClient(t, env.addr, "newuser", "newuser")
		defer newConn.Close()

		drainPushUpdates(t, newConn)

		sendRequest(t, newConn, "list-2", "list_conversations", map[string]interface{}{})
		resp := readResponse(t, newConn, 5*time.Second)

		require.Equal(t, "list-2", resp.ID, "response ID should match (LC-2)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "list_conversations should succeed (LC-2)")

		var data struct {
			Conversations []struct {
				ID string `json:"ID"`
			} `json:"conversations"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (LC-2)")

		assert.Empty(t, data.Conversations, "new user should have 0 conversations (LC-2)")
		assert.False(t, data.HasMore, "has_more should be false for empty list (LC-2)")
	})

	// -----------------------------------------------------------------------
	// LC-3: Pagination — limit=2 with 5 conversations yields has_more=true.
	// -----------------------------------------------------------------------
	t.Run("LC-3_Pagination", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		// Pre-create 5 conversations for alice.
		for i := 0; i < 5; i++ {
			createTestConversation(t, env.store, "alice", fmt.Sprintf("user%d", i))
		}

		drainPushUpdates(t, aliceConn)

		// Request page 1: limit=2, offset=0.
		sendRequest(t, aliceConn, "list-3a", "list_conversations", map[string]interface{}{
			"limit": 2,
		})
		resp1 := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp1.Code, "first page should succeed (LC-3)")

		var page1 struct {
			Conversations []struct {
				ID string `json:"ID"`
			} `json:"conversations"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp1.Data, &page1), "unmarshal page 1 (LC-3)")

		assert.Len(t, page1.Conversations, 2, "page 1 should have 2 conversations (LC-3)")
		assert.True(t, page1.HasMore, "has_more should be true when more exist (LC-3)")

		// Request page 2: limit=2, offset=2.
		sendRequest(t, aliceConn, "list-3b", "list_conversations", map[string]interface{}{
			"offset": 2,
			"limit":  2,
		})
		resp2 := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp2.Code, "second page should succeed (LC-3)")

		var page2 struct {
			Conversations []struct {
				ID string `json:"ID"`
			} `json:"conversations"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp2.Data, &page2), "unmarshal page 2 (LC-3)")

		assert.Len(t, page2.Conversations, 2, "page 2 should have 2 conversations (LC-3)")
		assert.True(t, page2.HasMore, "has_more should be true for page 2 of 3 (LC-3)")

		// Request page 3: limit=2, offset=4.
		sendRequest(t, aliceConn, "list-3c", "list_conversations", map[string]interface{}{
			"offset": 4,
			"limit":  2,
		})
		resp3 := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp3.Code, "third page should succeed (LC-3)")

		var page3 struct {
			Conversations []struct {
				ID string `json:"ID"`
			} `json:"conversations"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp3.Data, &page3), "unmarshal page 3 (LC-3)")

		assert.Len(t, page3.Conversations, 1, "page 3 should have 1 conversation (LC-3)")
		assert.False(t, page3.HasMore, "has_more should be false on last page (LC-3)")

		// Verify no overlap between pages.
		page1IDs := make(map[string]bool)
		for _, c := range page1.Conversations {
			page1IDs[c.ID] = true
		}
		for _, c := range page2.Conversations {
			assert.False(t, page1IDs[c.ID], "page 2 should not overlap with page 1 (LC-3)")
		}
	})

	// -----------------------------------------------------------------------
	// LC-4: Ordering — conversations sorted by LastMessageAt DESC.
	// -----------------------------------------------------------------------
	t.Run("LC-4_Ordering", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		// Create 3 conversations with distinct LastMessageAt values.
		// conv1: oldest, conv2: middle, conv3: newest.
		now := time.Now()
		conv1 := createTestConversation(t, env.store, "alice", "bob")
		conv2 := createTestConversation(t, env.store, "alice", "charlie")
		conv3 := createTestConversation(t, env.store, "alice", "eve")

		// Update LastMessageAt directly via GORM to control ordering.
		ctx := context.Background()
		conv1.LastMessageAt = now.Add(-3 * time.Hour)
		require.NoError(t, env.store.ConversationStore().Update(ctx, conv1),
			"update conv1 LastMessageAt should succeed (LC-4)")

		conv2.LastMessageAt = now.Add(-1 * time.Hour)
		require.NoError(t, env.store.ConversationStore().Update(ctx, conv2),
			"update conv2 LastMessageAt should succeed (LC-4)")

		conv3.LastMessageAt = now
		require.NoError(t, env.store.ConversationStore().Update(ctx, conv3),
			"update conv3 LastMessageAt should succeed (LC-4)")

		drainPushUpdates(t, aliceConn)

		// Call list_conversations — expect ordering: conv3, conv2, conv1.
		sendRequest(t, aliceConn, "list-4", "list_conversations", map[string]interface{}{})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "list_conversations should succeed (LC-4)")

		var data struct {
			Conversations []struct {
				ID            string    `json:"ID"`
				LastMessageAt time.Time `json:"LastMessageAt"`
			} `json:"conversations"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (LC-4)")

		require.Len(t, data.Conversations, 3, "should return 3 conversations (LC-4)")
		assert.Equal(t, conv3.ID, data.Conversations[0].ID,
			"newest conversation (conv3) should be first (LC-4)")
		assert.Equal(t, conv2.ID, data.Conversations[1].ID,
			"middle conversation (conv2) should be second (LC-4)")
		assert.Equal(t, conv1.ID, data.Conversations[2].ID,
			"oldest conversation (conv1) should be last (LC-4)")

		// Verify LastMessageAt values are in descending order.
		assert.True(t, data.Conversations[0].LastMessageAt.After(data.Conversations[1].LastMessageAt) ||
			data.Conversations[0].LastMessageAt.Equal(data.Conversations[1].LastMessageAt),
			"first LastMessageAt >= second (LC-4)")
		assert.True(t, data.Conversations[1].LastMessageAt.After(data.Conversations[2].LastMessageAt) ||
			data.Conversations[1].LastMessageAt.Equal(data.Conversations[2].LastMessageAt),
			"second LastMessageAt >= third (LC-4)")
	})

	// -----------------------------------------------------------------------
	// LC-5: User isolation — Alice cannot see Bob-Charlie conversation.
	// -----------------------------------------------------------------------
	t.Run("LC-5_UserIsolation", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		// Alice has a conversation with Bob.
		aliceBob := createTestConversation(t, env.store, "alice", "bob")

		// Bob has a separate conversation with Charlie (Alice is NOT a member).
		bobCharlie := createTestConversation(t, env.store, "bob", "charlie")

		drainPushUpdates(t, aliceConn)

		// Alice calls list_conversations.
		sendRequest(t, aliceConn, "list-5", "list_conversations", map[string]interface{}{})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code,
			"list_conversations should succeed (LC-5)")

		var data struct {
			Conversations []struct {
				ID string `json:"ID"`
			} `json:"conversations"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data),
			"unmarshal response data (LC-5)")

		// Alice should only see her own conversation.
		assert.Len(t, data.Conversations, 1,
			"alice should see only her own conversations (LC-5)")
		assert.Equal(t, aliceBob.ID, data.Conversations[0].ID,
			"alice should see alice-bob conversation (LC-5)")

		// Verify Bob-Charlie conversation is NOT visible to Alice.
		for _, c := range data.Conversations {
			assert.NotEqual(t, bobCharlie.ID, c.ID,
				"alice should NOT see bob-charlie conversation (LC-5)")
		}
	})

	// -----------------------------------------------------------------------
	// LC-6: Soft-deleted excluded — deleted conversations not in list.
	// -----------------------------------------------------------------------
	t.Run("LC-6_SoftDeletedExcluded", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		// Create 3 conversations for alice.
		conv1 := createTestConversation(t, env.store, "alice", "bob")
		conv2 := createTestConversation(t, env.store, "alice", "charlie")
		conv3 := createTestConversation(t, env.store, "alice", "eve")

		// Soft-delete the second conversation.
		ctx := context.Background()
		err := env.store.ConversationStore().Delete(ctx, conv2.ID)
		require.NoError(t, err, "soft delete should succeed (LC-6)")

		drainPushUpdates(t, aliceConn)

		// Call list_conversations — expect only 2 (conv1 and conv3).
		sendRequest(t, aliceConn, "list-6", "list_conversations", map[string]interface{}{})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code,
			"list_conversations should succeed (LC-6)")

		var data struct {
			Conversations []struct {
				ID        string      `json:"ID"`
				DeletedAt interface{} `json:"DeletedAt"`
			} `json:"conversations"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data),
			"unmarshal response data (LC-6)")

		assert.Len(t, data.Conversations, 2,
			"should return 2 conversations after soft delete (LC-6)")
		assert.False(t, data.HasMore,
			"has_more should be false (LC-6)")

		// Verify the deleted conversation is not in the list.
		for _, c := range data.Conversations {
			assert.NotEqual(t, conv2.ID, c.ID,
				"soft-deleted conversation should not appear (LC-6)")
			assert.Nil(t, c.DeletedAt,
				"returned conversations should not be soft-deleted (LC-6)")
		}

		// Verify conv1 and conv3 are present.
		convIDs := make(map[string]bool)
		for _, c := range data.Conversations {
			convIDs[c.ID] = true
		}
		assert.True(t, convIDs[conv1.ID], "conv1 should be present (LC-6)")
		assert.True(t, convIDs[conv3.ID], "conv3 should be present (LC-6)")
	})
}

// ---------------------------------------------------------------------------
// TC-16: TestGetMessagesE2E
// Verifies: D-008 (MessageID ordering), C-3 (member check)
// ---------------------------------------------------------------------------

// TestGetMessagesE2E verifies the get_messages flow:
// pagination with after_message_id cursor, ordering by MessageID ASC, member check.
func TestGetMessagesE2E(t *testing.T) {
	// -----------------------------------------------------------------------
	// GM-E1: Happy path — 5 messages, verify ASC ordering (D-008).
	// -----------------------------------------------------------------------
	t.Run("GM-E1_HappyPath", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()
		bobConn := connectClient(t, env.addr, "bob", "bob")
		defer bobConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send 5 messages from alice.
		var messageIDs []uint32
		for i := 0; i < 5; i++ {
			sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
				"conversation_id":   conv.ID,
				"client_message_id": uuid.New().String(),
				"content":           fmt.Sprintf("Message %d", i+1),
				"type":              "text",
			})
			resp := readResponse(t, aliceConn, 5*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)

			var respData struct {
				Message model.Message `json:"message"`
			}
			require.NoError(t, json.Unmarshal(resp.Data, &respData))
			messageIDs = append(messageIDs, respData.Message.MessageID)
		}

		// Consume push updates.
		drainPushUpdates(t, bobConn)
		drainPushUpdates(t, aliceConn)

		// Call get_messages.
		sendRequest(t, aliceConn, "get-msgs-1", "get_messages", map[string]interface{}{
			"conversation_id": conv.ID,
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "get-msgs-1", resp.ID, "response ID should match (GM-E1)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "get_messages should succeed (GM-E1)")

		var data struct {
			Messages []struct {
				ID        uint32      `json:"MessageID"`
				Content   string      `json:"Content"`
				SenderID  string      `json:"SenderID"`
				DeletedAt interface{} `json:"DeletedAt"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (GM-E1)")

		assert.Len(t, data.Messages, 5, "should return 5 messages (GM-E1)")
		assert.False(t, data.HasMore, "has_more should be false when all fit (GM-E1)")

		// Verify MessageID ascending order (D-008).
		for i := 0; i < len(data.Messages)-1; i++ {
			assert.Less(t, data.Messages[i].ID, data.Messages[i+1].ID,
				"messages should be in ascending order (D-008)")
		}

		// Verify content matches.
		for i, msg := range data.Messages {
			assert.Equal(t, fmt.Sprintf("Message %d", i+1), msg.Content,
				"message %d content should match (GM-E1)", i+1)
			assert.Equal(t, "alice", msg.SenderID, "sender should be alice (GM-E1)")
		}
	})

	// -----------------------------------------------------------------------
	// GM-E2: Empty conversation — messages=[], has_more=false.
	// -----------------------------------------------------------------------
	t.Run("GM-E2_EmptyConversation", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		drainPushUpdates(t, aliceConn)

		// Call get_messages on empty conversation.
		sendRequest(t, aliceConn, "get-msgs-2", "get_messages", map[string]interface{}{
			"conversation_id": conv.ID,
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "get-msgs-2", resp.ID, "response ID should match (GM-E2)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "get_messages should succeed (GM-E2)")

		var data struct {
			Messages []struct {
				ID uint32 `json:"MessageID"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (GM-E2)")

		assert.Empty(t, data.Messages, "messages should be empty for empty conversation (GM-E2)")
		assert.False(t, data.HasMore, "has_more should be false for empty conversation (GM-E2)")
	})

	// -----------------------------------------------------------------------
	// GM-E3: Pagination with after_message_id cursor (D-009).
	// -----------------------------------------------------------------------
	t.Run("GM-E3_Pagination", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()
		bobConn := connectClient(t, env.addr, "bob", "bob")
		defer bobConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send 5 messages.
		var messageIDs []uint32
		for i := 0; i < 5; i++ {
			sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
				"conversation_id":   conv.ID,
				"client_message_id": uuid.New().String(),
				"content":           fmt.Sprintf("Message %d", i+1),
				"type":              "text",
			})
			resp := readResponse(t, aliceConn, 5*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)

			var respData struct {
				Message model.Message `json:"message"`
			}
			require.NoError(t, json.Unmarshal(resp.Data, &respData))
			messageIDs = append(messageIDs, respData.Message.MessageID)
		}

		drainPushUpdates(t, bobConn)
		drainPushUpdates(t, aliceConn)

		// Request messages after message 2.
		sendRequest(t, aliceConn, "get-msgs-3", "get_messages", map[string]interface{}{
			"conversation_id":  conv.ID,
			"after_message_id": messageIDs[1], // after 2nd message
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "get-msgs-3", resp.ID, "response ID should match (GM-E3)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "get_messages should succeed (GM-E3)")

		var data struct {
			Messages []struct {
				ID      uint32 `json:"MessageID"`
				Content string `json:"Content"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (GM-E3)")

		assert.Len(t, data.Messages, 3, "should return 3 messages after cursor (GM-E3)")
		assert.False(t, data.HasMore, "has_more should be false (GM-E3)")

		// Verify only messages 3, 4, 5 are returned.
		for i, msg := range data.Messages {
			assert.Equal(t, messageIDs[i+2], msg.ID,
				"message ID should match (GM-E3)")
			assert.Equal(t, fmt.Sprintf("Message %d", i+3), msg.Content,
				"message content should match (GM-E3)")
		}
	})

	// -----------------------------------------------------------------------
	// GM-E4: Has more detection — limit=3, 10 messages => has_more=true.
	// -----------------------------------------------------------------------
	t.Run("GM-E4_HasMore", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()
		bobConn := connectClient(t, env.addr, "bob", "bob")
		defer bobConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send 10 messages.
		for i := 0; i < 10; i++ {
			sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
				"conversation_id":   conv.ID,
				"client_message_id": uuid.New().String(),
				"content":           fmt.Sprintf("Message %d", i+1),
				"type":              "text",
			})
			resp := readResponse(t, aliceConn, 5*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)
		}

		drainPushUpdates(t, bobConn)
		drainPushUpdates(t, aliceConn)

		// Request with limit=3.
		sendRequest(t, aliceConn, "get-msgs-4", "get_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"limit":           3,
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "get-msgs-4", resp.ID, "response ID should match (GM-E4)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "get_messages should succeed (GM-E4)")

		var data struct {
			Messages []struct {
				ID uint32 `json:"MessageID"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (GM-E4)")

		assert.Len(t, data.Messages, 3, "should return exactly 3 messages (GM-E4)")
		assert.True(t, data.HasMore, "has_more should be true when more exist (GM-E4)")

		// Verify IDs are 1, 2, 3.
		for i := 0; i < 3; i++ {
			assert.Equal(t, uint32(i+1), data.Messages[i].ID,
				"message ID should be %d (GM-E4)", i+1)
		}
	})

	// -----------------------------------------------------------------------
	// GM-E5: Non-member rejected — error "not a member" (C-3).
	// -----------------------------------------------------------------------
	t.Run("GM-E5_NonMemberRejected", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()
		eveConn := connectClient(t, env.addr, "eve", "eve")
		defer eveConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		drainPushUpdates(t, aliceConn)
		drainPushUpdates(t, eveConn)

		// Eve (non-member) calls get_messages.
		sendRequest(t, eveConn, "get-msgs-5", "get_messages", map[string]interface{}{
			"conversation_id": conv.ID,
		})
		resp := readResponse(t, eveConn, 5*time.Second)
		require.Equal(t, "get-msgs-5", resp.ID, "response ID should match (GM-E5)")
		assert.Equal(t, protocol.ResponseCodePermissionDenied, resp.Code,
			"non-member get_messages should be rejected (C-3)")
		assert.True(t, strings.Contains(strings.ToLower(resp.Msg), "not a member"),
			"error should mention 'not a member', got: %s (C-3)", resp.Msg)
	})

	// -----------------------------------------------------------------------
	// GM-E6: Non-existent conversation — error "not found".
	// -----------------------------------------------------------------------
	t.Run("GM-E6_NonExistentConversation", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		drainPushUpdates(t, aliceConn)

		// Call get_messages with non-existent conversation.
		sendRequest(t, aliceConn, "get-msgs-6", "get_messages", map[string]interface{}{
			"conversation_id": uuid.New().String(),
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "get-msgs-6", resp.ID, "response ID should match (GM-E6)")
		assert.Equal(t, protocol.ResponseCodeNotFound, resp.Code,
			"get_messages for non-existent conversation should fail")
		assert.True(t, strings.Contains(strings.ToLower(resp.Msg), "not found"),
			"error should mention 'not found', got: %s", resp.Msg)
	})

	// -----------------------------------------------------------------------
	// GM-E7: Soft-deleted messages excluded.
	// -----------------------------------------------------------------------
	t.Run("GM-E7_SoftDeletedExcluded", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()
		bobConn := connectClient(t, env.addr, "bob", "bob")
		defer bobConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send 3 messages.
		var messageIDs []string
		for i := 0; i < 3; i++ {
			sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
				"conversation_id":   conv.ID,
				"client_message_id": uuid.New().String(),
				"content":           fmt.Sprintf("Message %d", i+1),
				"type":              "text",
			})
			resp := readResponse(t, aliceConn, 5*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)

			var respData struct {
				Message model.Message `json:"message"`
			}
			require.NoError(t, json.Unmarshal(resp.Data, &respData))
			messageIDs = append(messageIDs, respData.Message.ID)
		}

		drainPushUpdates(t, bobConn)
		drainPushUpdates(t, aliceConn)

		// Soft-delete the middle message.
		ctx := context.Background()
		err := env.store.MessageStore().Delete(ctx, messageIDs[1])
		require.NoError(t, err, "soft delete should succeed (GM-E7)")

		// Call get_messages.
		sendRequest(t, aliceConn, "get-msgs-7", "get_messages", map[string]interface{}{
			"conversation_id": conv.ID,
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "get-msgs-7", resp.ID, "response ID should match (GM-E7)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "get_messages should succeed (GM-E7)")

		var data struct {
			Messages []struct {
				ID        string      `json:"ID"`
				Content   string      `json:"Content"`
				DeletedAt interface{} `json:"DeletedAt"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (GM-E7)")

		assert.Len(t, data.Messages, 2, "should return 2 messages after soft delete (GM-E7)")
		assert.False(t, data.HasMore, "has_more should be false (GM-E7)")

		// Verify the deleted message is not in the results.
		for _, msg := range data.Messages {
			assert.NotEqual(t, messageIDs[1], msg.ID,
				"soft-deleted message should not appear (GM-E7)")
			assert.Nil(t, msg.DeletedAt,
				"returned messages should not be soft-deleted (GM-E7)")
		}

		// Verify messages 1 and 3 are present.
		msgIDs := make(map[string]bool)
		for _, msg := range data.Messages {
			msgIDs[msg.ID] = true
		}
		assert.True(t, msgIDs[messageIDs[0]], "message 1 should be present (GM-E7)")
		assert.True(t, msgIDs[messageIDs[2]], "message 3 should be present (GM-E7)")
	})
}

// ---------------------------------------------------------------------------
// TC-17: TestSearchMessagesE2E
// Verifies: search_messages with case-insensitive LIKE, DESC ordering, C-3 member check
// ---------------------------------------------------------------------------

// TestSearchMessagesE2E verifies the search_messages flow:
// case-insensitive search, pagination with after_message_id, DESC ordering.
func TestSearchMessagesE2E(t *testing.T) {
	// -----------------------------------------------------------------------
	// SM-E1: Happy path — search "hello" returns matching messages.
	// -----------------------------------------------------------------------
	t.Run("SM-E1_HappyPath", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()
		bobConn := connectClient(t, env.addr, "bob", "bob")
		defer bobConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send 3 messages with "hello" in content.
		for i := 0; i < 3; i++ {
			sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
				"conversation_id":   conv.ID,
				"client_message_id": uuid.New().String(),
				"content":           fmt.Sprintf("hello world %d", i+1),
				"type":              "text",
			})
			resp := readResponse(t, aliceConn, 5*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)
		}

		// Send 2 messages without "hello".
		for i := 0; i < 2; i++ {
			sendRequest(t, aliceConn, fmt.Sprintf("send-other-%d", i+1), "send_message", map[string]interface{}{
				"conversation_id":   conv.ID,
				"client_message_id": uuid.New().String(),
				"content":           fmt.Sprintf("goodbye %d", i+1),
				"type":              "text",
			})
			resp := readResponse(t, aliceConn, 5*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send other message %d should succeed", i+1)
		}

		drainPushUpdates(t, bobConn)
		drainPushUpdates(t, aliceConn)

		// Search for "hello".
		sendRequest(t, aliceConn, "search-1", "search_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"query":           "hello",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-1", resp.ID, "response ID should match (SM-E1)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "search_messages should succeed (SM-E1)")

		var data struct {
			Messages []struct {
				ID              uint32 `json:"MessageID"`
				ConversationID  string `json:"ConversationID"`
				SenderID        string `json:"SenderID"`
				Content         string `json:"Content"`
				ClientMessageID string `json:"ClientMessageID"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (SM-E1)")

		assert.Len(t, data.Messages, 3, "should return 3 messages containing 'hello' (SM-E1)")
		assert.False(t, data.HasMore, "has_more should be false (SM-E1)")

		// Verify all messages contain "hello".
		for _, msg := range data.Messages {
			assert.Contains(t, strings.ToLower(msg.Content), "hello",
				"message should contain 'hello' (SM-E1)")
			assert.Equal(t, "alice", msg.SenderID, "sender should be alice (SM-E1)")
		}
	})

	// -----------------------------------------------------------------------
	// SM-E2: No results — empty query returns messages=[], has_more=false.
	// -----------------------------------------------------------------------
	t.Run("SM-E2_NoResults", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send 2 messages.
		for i := 0; i < 2; i++ {
			sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
				"conversation_id":   conv.ID,
				"client_message_id": uuid.New().String(),
				"content":           fmt.Sprintf("message %d", i+1),
				"type":              "text",
			})
			resp := readResponse(t, aliceConn, 5*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)
		}

		drainPushUpdates(t, aliceConn)

		// Search for non-existent term.
		sendRequest(t, aliceConn, "search-2", "search_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"query":           "nonexistent",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-2", resp.ID, "response ID should match (SM-E2)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "search_messages should succeed (SM-E2)")

		var data struct {
			Messages []struct {
				ID uint32 `json:"MessageID"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (SM-E2)")

		assert.Empty(t, data.Messages, "should return empty messages array (SM-E2)")
		assert.False(t, data.HasMore, "has_more should be false (SM-E2)")
	})

	// -----------------------------------------------------------------------
	// SM-E3: Case insensitive — search "HELLO" matches "hello".
	// -----------------------------------------------------------------------
	t.Run("SM-E3_CaseInsensitive", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send messages with mixed case.
		sendRequest(t, aliceConn, "send-1", "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": uuid.New().String(),
			"content":           "Hello World",
			"type":              "text",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code)

		sendRequest(t, aliceConn, "send-2", "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": uuid.New().String(),
			"content":           "hello again",
			"type":              "text",
		})
		resp = readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code)

		drainPushUpdates(t, aliceConn)

		// Search for "HELLO" (uppercase).
		sendRequest(t, aliceConn, "search-3", "search_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"query":           "HELLO",
		})
		resp = readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-3", resp.ID, "response ID should match (SM-E3)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "search_messages should succeed (SM-E3)")

		var data struct {
			Messages []struct {
				Content string `json:"Content"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (SM-E3)")

		assert.Len(t, data.Messages, 2, "should match both 'Hello' and 'hello' (SM-E3)")
	})

	// -----------------------------------------------------------------------
	// SM-E4: Pagination with after_message_id — cursor-based pagination.
	// -----------------------------------------------------------------------
	t.Run("SM-E4_Pagination", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send 5 messages containing "test".
		var messageIDs []uint32
		for i := 0; i < 5; i++ {
			sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
				"conversation_id":   conv.ID,
				"client_message_id": uuid.New().String(),
				"content":           fmt.Sprintf("test message %d", i+1),
				"type":              "text",
			})
			resp := readResponse(t, aliceConn, 5*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)

			var respData struct {
				Message model.Message `json:"message"`
			}
			require.NoError(t, json.Unmarshal(resp.Data, &respData))
			messageIDs = append(messageIDs, respData.Message.MessageID)
		}

		drainPushUpdates(t, aliceConn)

		// First page: limit=2.
		sendRequest(t, aliceConn, "search-4a", "search_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"query":           "test",
			"limit":           2,
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-4a", resp.ID, "response ID should match (SM-E4)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "search_messages should succeed (SM-E4)")

		var data1 struct {
			Messages []struct {
				ID uint32 `json:"MessageID"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data1), "unmarshal response data (SM-E4 page 1)")

		assert.Len(t, data1.Messages, 2, "first page should have 2 messages (SM-E4)")
		assert.True(t, data1.HasMore, "has_more should be true (SM-E4)")
		// DESC order: messageIDs[4], messageIDs[3]
		assert.Equal(t, messageIDs[4], data1.Messages[0].ID, "first message should be newest (SM-E4)")
		assert.Equal(t, messageIDs[3], data1.Messages[1].ID, "second message should be next (SM-E4)")

		// Second page: after_message_id = messageIDs[3].
		sendRequest(t, aliceConn, "search-4b", "search_messages", map[string]interface{}{
			"conversation_id":  conv.ID,
			"query":            "test",
			"limit":            2,
			"after_message_id": messageIDs[3],
		})
		resp = readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-4b", resp.ID, "response ID should match (SM-E4)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "search_messages should succeed (SM-E4)")

		var data2 struct {
			Messages []struct {
				ID uint32 `json:"MessageID"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data2), "unmarshal response data (SM-E4 page 2)")

		assert.Len(t, data2.Messages, 2, "second page should have 2 messages (SM-E4)")
		assert.True(t, data2.HasMore, "has_more should be true (SM-E4)")
		// DESC order: messageIDs[2], messageIDs[1]
		assert.Equal(t, messageIDs[2], data2.Messages[0].ID, "first message on page 2 (SM-E4)")
		assert.Equal(t, messageIDs[1], data2.Messages[1].ID, "second message on page 2 (SM-E4)")

		// Third page: after_message_id = messageIDs[1].
		sendRequest(t, aliceConn, "search-4c", "search_messages", map[string]interface{}{
			"conversation_id":  conv.ID,
			"query":            "test",
			"limit":            2,
			"after_message_id": messageIDs[1],
		})
		resp = readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-4c", resp.ID, "response ID should match (SM-E4)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "search_messages should succeed (SM-E4)")

		var data3 struct {
			Messages []struct {
				ID uint32 `json:"MessageID"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data3), "unmarshal response data (SM-E4 page 3)")

		assert.Len(t, data3.Messages, 1, "third page should have 1 message (SM-E4)")
		assert.False(t, data3.HasMore, "has_more should be false (SM-E4)")
		assert.Equal(t, messageIDs[0], data3.Messages[0].ID, "last message on page 3 (SM-E4)")
	})

	// -----------------------------------------------------------------------
	// SM-E5: Ordering DESC — newest matching message first.
	// -----------------------------------------------------------------------
	t.Run("SM-E5_OrderingDESC", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send 5 messages with "apple".
		var messageIDs []uint32
		for i := 0; i < 5; i++ {
			sendRequest(t, aliceConn, fmt.Sprintf("send-%d", i+1), "send_message", map[string]interface{}{
				"conversation_id":   conv.ID,
				"client_message_id": uuid.New().String(),
				"content":           fmt.Sprintf("apple %d", i+1),
				"type":              "text",
			})
			resp := readResponse(t, aliceConn, 5*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code, "send message %d should succeed", i+1)

			var respData struct {
				Message model.Message `json:"message"`
			}
			require.NoError(t, json.Unmarshal(resp.Data, &respData))
			messageIDs = append(messageIDs, respData.Message.MessageID)
		}

		drainPushUpdates(t, aliceConn)

		// Search for "apple".
		sendRequest(t, aliceConn, "search-5", "search_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"query":           "apple",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-5", resp.ID, "response ID should match (SM-E5)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "search_messages should succeed (SM-E5)")

		var data struct {
			Messages []struct {
				ID uint32 `json:"MessageID"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data), "unmarshal response data (SM-E5)")

		assert.Len(t, data.Messages, 5, "should return 5 messages (SM-E5)")

		// Verify DESC ordering: IDs should decrease.
		for i := 0; i < len(data.Messages)-1; i++ {
			assert.Greater(t, data.Messages[i].ID, data.Messages[i+1].ID,
				"messages should be in descending order (SM-E5)")
		}

		// First message should be the newest (messageIDs[4]).
		assert.Equal(t, messageIDs[4], data.Messages[0].ID,
			"first message should be newest (SM-E5)")
		// Last message should be the oldest (messageIDs[0]).
		assert.Equal(t, messageIDs[0], data.Messages[4].ID,
			"last message should be oldest (SM-E5)")
	})

	// -----------------------------------------------------------------------
	// SM-E6: Non-member rejected — error "not a member" (C-3).
	// -----------------------------------------------------------------------
	t.Run("SM-E6_NonMemberRejected", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()
		eveConn := connectClient(t, env.addr, "eve", "eve")
		defer eveConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		drainPushUpdates(t, aliceConn)
		drainPushUpdates(t, eveConn)

		// Eve (non-member) calls search_messages.
		sendRequest(t, eveConn, "search-6", "search_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"query":           "test",
		})
		resp := readResponse(t, eveConn, 5*time.Second)
		require.Equal(t, "search-6", resp.ID, "response ID should match (SM-E6)")
		assert.Equal(t, protocol.ResponseCodePermissionDenied, resp.Code,
			"non-member search_messages should be rejected (C-3)")
		assert.True(t, strings.Contains(strings.ToLower(resp.Msg), "not a member"),
			"error should mention 'not a member', got: %s (C-3)", resp.Msg)
	})

	// -----------------------------------------------------------------------
	// SM-E7: Empty query — error "query".
	// -----------------------------------------------------------------------
	t.Run("SM-E7_EmptyQuery", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		drainPushUpdates(t, aliceConn)

		// Call search_messages with empty query.
		sendRequest(t, aliceConn, "search-7", "search_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"query":           "",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-7", resp.ID, "response ID should match (SM-E7)")
		assert.Equal(t, protocol.ResponseCodeValidationError, resp.Code,
			"empty query should fail")
		assert.True(t, strings.Contains(strings.ToLower(resp.Msg), "query"),
			"error should mention 'query', got: %s", resp.Msg)
	})

	// -----------------------------------------------------------------------
	// SM-E8: LIKE special chars — search with % and _ doesn't break.
	// -----------------------------------------------------------------------
	t.Run("SM-E8_LIKESpecialChars", func(t *testing.T) {
		env := setupE2ETest(t)

		aliceConn := connectClient(t, env.addr, "alice", "alice")
		defer aliceConn.Close()

		conv := createTestConversation(t, env.store, "alice", "bob")

		// Send messages with LIKE special characters.
		sendRequest(t, aliceConn, "send-1", "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": uuid.New().String(),
			"content":           "100% complete",
			"type":              "text",
		})
		resp := readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code)

		sendRequest(t, aliceConn, "send-2", "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": uuid.New().String(),
			"content":           "user_name",
			"type":              "text",
		})
		resp = readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code)

		sendRequest(t, aliceConn, "send-3", "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": uuid.New().String(),
			"content":           "normal message",
			"type":              "text",
		})
		resp = readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code)

		drainPushUpdates(t, aliceConn)

		// Search for "%".
		sendRequest(t, aliceConn, "search-8a", "search_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"query":           "%",
		})
		resp = readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-8a", resp.ID, "response ID should match (SM-E8)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "search_messages with '%' should succeed (SM-E8)")

		var data1 struct {
			Messages []struct {
				Content string `json:"Content"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data1), "unmarshal response data (SM-E8)")

		assert.Len(t, data1.Messages, 1, "should match exactly 1 message with '%' (SM-E8)")
		assert.Contains(t, data1.Messages[0].Content, "%", "content should contain '%' (SM-E8)")

		// Search for "_".
		sendRequest(t, aliceConn, "search-8b", "search_messages", map[string]interface{}{
			"conversation_id": conv.ID,
			"query":           "_",
		})
		resp = readResponse(t, aliceConn, 5*time.Second)
		require.Equal(t, "search-8b", resp.ID, "response ID should match (SM-E8)")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code, "search_messages with '_' should succeed (SM-E8)")

		var data2 struct {
			Messages []struct {
				Content string `json:"Content"`
			} `json:"messages"`
			HasMore bool `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &data2), "unmarshal response data (SM-E8)")

		assert.Len(t, data2.Messages, 1, "should match exactly 1 message with '_' (SM-E8)")
		assert.Contains(t, data2.Messages[0].Content, "_", "content should contain '_' (SM-E8)")
	})
}
