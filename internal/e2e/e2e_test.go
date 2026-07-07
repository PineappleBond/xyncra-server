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
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
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
	db        *store.Database
	store     *store.Store
	connStore *server.RedisConnectionStore
	broker    *mq.AsynqBroker
	srv       *server.WebSocketServer
	addr      string
	cancel    context.CancelFunc
	redisKey  string // key prefix for TTL verification
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
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
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

	// 7. Message handler + RegisterAll.
	msgHandler := server.NewDefaultMessageHandler()
	handler.RegisterAll(msgHandler, handler.Dependencies{
		ConnStore: connStore,
		Store:     dataStore,
		Broker:    broker,
	})

	// 8. WebSocket server.
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

	// 9. Task handler + Register(TypeSendMessage).
	taskHandler := mq.NewTaskHandler()
	taskHandler.Register(mq.TypeSendMessage,
		handler.NewSendMessageTaskHandler(srv.BroadcastUpdates, srv.Logger()))

	// 10. Start broker and server in goroutines (C-2).
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

	// 11. Wait for the server to be ready (C-3).
	require.Eventually(t, func() bool {
		addr := srv.Addr()
		return addr != ":0" && addr != ""
	}, 3*time.Second, 20*time.Millisecond, "server should bind to a real address (C-3)")

	addr := srv.Addr()

	// 12. Cleanup (C-1 reverse order, using t.Cleanup not defer).
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
		db:        db,
		store:     dataStore,
		connStore: connStore,
		broker:    broker,
		srv:       srv,
		addr:      addr,
		cancel:    cancel,
		redisKey:  keyPrefix,
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// connectClient opens a WebSocket connection for the given userID and returns
// the underlying *websocket.Conn. The connection URL is constructed as
// ws://{addr}/ws?user_id={userID} (C-4).
func connectClient(t *testing.T, addr, userID string) *websocket.Conn {
	t.Helper()

	u := url.URL{
		Scheme:   "ws",
		Host:     addr,
		Path:     "/ws",
		RawQuery: url.Values{"user_id": {userID}}.Encode(),
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	require.NoError(t, err, "WebSocket dial should succeed for user %s", userID)
	return conn
}

// sendRequest marshals the given params into a PackageDataRequest, wraps it
// in a Package{Type:Request}, and writes it to the connection.
func sendRequest(t *testing.T, conn *websocket.Conn, id, method string, params map[string]interface{}) {
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

// readResponse reads a single message from the connection, expects it to be a
// PackageTypeResponse, and returns the decoded PackageDataResponse.
func readResponse(t *testing.T, conn *websocket.Conn, timeout time.Duration) *protocol.PackageDataResponse {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err, "read message should succeed within timeout")

	var pkg protocol.Package
	require.NoError(t, json.Unmarshal(data, &pkg), "unmarshal package should succeed")
	require.Equal(t, protocol.PackageTypeResponse, pkg.Type,
		"expected PackageTypeResponse, got %d", pkg.Type)

	var resp protocol.PackageDataResponse
	require.NoError(t, json.Unmarshal(pkg.Data, &resp), "unmarshal response should succeed")
	return &resp
}

// readPackage reads a single message from the connection and returns the
// decoded Package without checking its type.
func readPackage(t *testing.T, conn *websocket.Conn, timeout time.Duration) *protocol.Package {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err, "read message should succeed within timeout")

	var pkg protocol.Package
	require.NoError(t, json.Unmarshal(data, &pkg), "unmarshal package should succeed")
	return &pkg
}

// waitForUpdate loops reading packages from the connection until a
// PackageTypeUpdates is received or the timeout expires. Non-Updates packages
// (e.g. responses that arrive before the push) are silently skipped.
//
// This is necessary because MQ delivery is asynchronous: the send_message
// response may arrive before the push notification.
func waitForUpdate(t *testing.T, conn *websocket.Conn, timeout time.Duration) *protocol.PackageDataUpdates {
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
	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob")
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

	var bobPayload model.Message
	require.NoError(t, json.Unmarshal(bobUpdates.Updates[0].Payload, &bobPayload),
		"unmarshal bob payload")
	assert.Equal(t, "Hello Bob!", bobPayload.Content, "bob payload content should match")

	// 6. Alice also receives the push (C-10: fan-out includes sender).
	aliceUpdates := waitForUpdate(t, aliceConn, 10*time.Second)
	require.Len(t, aliceUpdates.Updates, 1, "alice should also receive 1 update (C-10)")

	// 7. DB verification.
	ctx := context.Background()
	var msgCount int64
	env.db.DB().WithContext(ctx).Model(&model.Message{}).Where("conversation_id = ?", conv.ID).Count(&msgCount)
	assert.Equal(t, int64(1), msgCount, "should have exactly 1 message in DB")

	var updateCount int64
	env.db.DB().WithContext(ctx).Model(&model.UserUpdate{}).Where("user_id IN ?", []string{"alice", "bob"}).Count(&updateCount)
	assert.Equal(t, int64(2), updateCount, "should have 2 user_updates (one per member) (D-007)")

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
	aliceConn := connectClient(t, env.addr, "alice")

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
	bobConn := connectClient(t, env.addr, "bob")
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

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob")
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

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob")
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
	aliceConn := connectClient(t, env.addr, "alice")
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

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()
	bobConn := connectClient(t, env.addr, "bob")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, "alice", "bob")

	// Eve is not a member.
	eveConn := connectClient(t, env.addr, "eve")
	defer eveConn.Close()

	clientMsgID := uuid.New().String()
	sendRequest(t, eveConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Hack!",
		"type":              "text",
	})

	resp := readResponse(t, eveConn, 5*time.Second)
	assert.Equal(t, protocol.ResponseCodeError, resp.Code,
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

	aliceConn := connectClient(t, env.addr, "alice")
	defer aliceConn.Close()

	clientMsgID := uuid.New().String()
	sendRequest(t, aliceConn, "req-1", "send_message", map[string]interface{}{
		"conversation_id":   uuid.New().String(),
		"client_message_id": clientMsgID,
		"content":           "Hello",
		"type":              "text",
	})

	resp := readResponse(t, aliceConn, 5*time.Second)
	assert.Equal(t, protocol.ResponseCodeError, resp.Code,
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

			aliceConn := connectClient(t, env.addr, "alice")
			defer aliceConn.Close()

			sendRequest(t, aliceConn, "req-1", "send_message", tc.params)

			resp := readResponse(t, aliceConn, 5*time.Second)
			assert.Equal(t, protocol.ResponseCodeError, resp.Code,
				"validation should fail for %s", tc.name)
			assert.True(t, strings.Contains(strings.ToLower(resp.Msg), tc.expect),
				"error message should mention %q, got: %s", tc.expect, resp.Msg)
		})
	}
}
