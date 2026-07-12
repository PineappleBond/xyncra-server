// Package e2e_test — concurrent E2E tests for the Xyncra WebSocket server.
//
// Unlike the serial tests in e2e_test.go, all subtests in
// TestConcurrentScenarios run in parallel against a single shared e2eEnv.
// The setup function (setupConcurrentE2E) calls FlushDB exactly once and
// uses a unique Redis key prefix to avoid colliding with serial tests.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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
// setupConcurrentE2E
// ---------------------------------------------------------------------------

// setupConcurrentE2E initialises a shared E2E environment for parallel
// subtests. It calls FlushDB exactly once during setup and uses a unique
// Redis key prefix derived from the test name to avoid collisions with
// serial E2E tests that share the same Redis instance.
//
// All parallel subtests receive the same *e2eEnv and must NOT call
// FlushDB themselves. Cleanup is registered via t.Cleanup.
func setupConcurrentE2E(t *testing.T) *e2eEnv {
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
		t.Skipf("Redis not available at %s (DB %d): %v — skipping concurrent E2E test", e2eRedisAddr, e2eRedisDB, err)
	}

	// 2. FlushDB once for the entire concurrent suite.
	//
	// NOTE: FlushDB clears the entire Redis DB (e2eRedisDB), not just the
	// keys owned by this test suite. This is acceptable because all e2e
	// tests share the same Redis instance and do not support -parallel > 1,
	// so no other test can be running concurrently. A more targeted cleanup
	// (SCAN + DEL by key prefix) would be safer but is not needed today.
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer flushCancel()
	require.NoError(t, redisClient.FlushDB(flushCtx).Err(), "FlushDB should succeed")
	_ = redisClient.Close()

	// 3. SQLite in-memory database (unique name for this suite).
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)", t.Name())
	db, err := store.NewDatabase(store.DatabaseConfig{
		Driver: "sqlite",
		DSN:    dsn,
	})
	require.NoError(t, err, "NewDatabase should succeed")

	// 4. Store + AutoMigrate.
	dataStore := store.NewFromDatabase(db)
	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer migrateCancel()
	require.NoError(t, dataStore.AutoMigrate(migrateCtx), "AutoMigrate should succeed")

	// 5. Redis connection store with a unique key prefix.
	keyPrefix := fmt.Sprintf("e2e:concurrent:%s:", t.Name())
	connStore, err := server.NewRedisConnectionStore(server.RedisConnectionStoreConfig{
		Addr:       e2eRedisAddr,
		DB:         e2eRedisDB,
		KeyPrefix:  keyPrefix,
		DefaultTTL: e2eDefaultTTL,
	})
	require.NoError(t, err, "NewRedisConnectionStore should succeed")

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

	// 10. Start broker and server.
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := broker.Start(ctx, taskHandler); err != nil {
			if ctx.Err() == nil {
				t.Logf("broker error: %v", err)
			}
		}
	}()

	go func() {
		if err := srv.Start(ctx); err != nil {
			if ctx.Err() == nil {
				t.Logf("server error: %v", err)
			}
		}
	}()

	// 11. Wait for the server to be ready.
	require.Eventually(t, func() bool {
		addr := srv.Addr()
		return addr != ":0" && addr != ""
	}, 3*time.Second, 20*time.Millisecond, "server should bind to a real address")

	addr := srv.Addr()

	// 12. Cleanup (reverse order).
	t.Cleanup(func() {
		cancel()

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = srv.GracefulStop(stopCtx)

		_ = broker.Close()
		_ = connStore.Close()
		_ = db.Close()

		// Final FlushDB to clean up. See note above: this clears the entire
		// Redis DB, which is safe because no other e2e tests run in parallel.
		cleanupClient := redis.NewClient(&redis.Options{
			Addr: e2eRedisAddr,
			DB:   e2eRedisDB,
		})
		defer func() { _ = cleanupClient.Close() }()
		fc, fcCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer fcCancel()
		_ = cleanupClient.FlushDB(fc).Err()
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
// TestConcurrentScenarios
// ---------------------------------------------------------------------------

// TestConcurrentScenarios runs multiple concurrency-focused E2E subtests in
// parallel against a shared environment. Each subtest uses unique user IDs
// to avoid data collisions.
func TestConcurrentScenarios(t *testing.T) {
	env := setupConcurrentE2E(t)

	t.Run("concurrent_send", func(t *testing.T) {
		t.Parallel()
		runConcurrentSend(t, env)
	})

	t.Run("concurrent_create_conversation", func(t *testing.T) {
		t.Parallel()
		runConcurrentCreateConversation(t, env)
	})

	t.Run("multi_device_broadcast", func(t *testing.T) {
		t.Parallel()
		runMultiDeviceBroadcast(t, env)
	})

	t.Run("offline_then_sync", func(t *testing.T) {
		t.Parallel()
		runOfflineThenSync(t, env)
	})
}

// ---------------------------------------------------------------------------
// concurrent_send
// ---------------------------------------------------------------------------

// runConcurrentSend verifies that 5 goroutines each sending 10 messages
// through independent WebSocket connections produce exactly 50 persisted
// messages with unique, non-duplicate MessageIDs.
//
// Design: each goroutine owns a dedicated alice WebSocket connection (all
// using the same aliceID, but each is a separate TCP+WS link). This avoids
// violating gorilla/websocket's "one concurrent writer" constraint, which
// would happen if they shared a single connection. bob receives push
// updates on a single connection.
//
// Verifies: D-006 (idempotency), D-008 (MessageID monotonic).
func runConcurrentSend(t *testing.T, env *e2eEnv) {
	t.Helper()

	const (
		numGoroutines    = 5
		msgsPerGoroutine = 10
	)

	// Use unique user IDs to avoid collision with other parallel subtests.
	aliceID := "cs-alice"
	bobID := "cs-bob"

	// Each goroutine gets its own alice WebSocket connection so there is
	// exactly one writer per connection at all times.
	aliceConns := make([]*wsConn, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		aliceConns[i] = connectClient(t, env.addr, aliceID, fmt.Sprintf("alice-dev-%d", i))
		defer aliceConns[i].Close()
	}
	bobConn := connectClient(t, env.addr, bobID, "bob-dev")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, aliceID, bobID)

	// Each goroutine sends msgsPerGoroutine messages and collects the
	// MessageIDs from responses.
	type sendResult struct {
		messageIDs []uint32
	}
	results := make([]sendResult, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(gIdx int) {
			defer wg.Done()
			conn := aliceConns[gIdx]
			var ids []uint32
			for m := 0; m < msgsPerGoroutine; m++ {
				reqID := fmt.Sprintf("cs-req-g%d-m%d", gIdx, m)
				clientMsgID := fmt.Sprintf("cs-cmid-g%d-m%d-%s", gIdx, m, uuid.New().String())
				sendRequest(t, conn, reqID, "send_message", map[string]interface{}{
					"conversation_id":   conv.ID,
					"client_message_id": clientMsgID,
					"content":           fmt.Sprintf("goroutine-%d-msg-%d", gIdx, m),
					"type":              "text",
				})
				resp := readResponse(t, conn, 10*time.Second)
				require.Equal(t, protocol.ResponseCodeOK, resp.Code,
					"send g%d-m%d should succeed", gIdx, m)

				var respData struct {
					Message model.Message `json:"message"`
				}
				require.NoError(t, json.Unmarshal(resp.Data, &respData))
				ids = append(ids, respData.Message.MessageID)
			}
			results[gIdx] = sendResult{messageIDs: ids}
		}(g)
	}

	wg.Wait()

	// Drain push updates so connections stay clean.
	for _, c := range aliceConns {
		drainPushUpdates(t, c)
	}
	drainPushUpdates(t, bobConn)

	// Collect all MessageIDs and verify uniqueness.
	allIDs := make(map[uint32]bool)
	for _, r := range results {
		for _, id := range r.messageIDs {
			assert.False(t, allIDs[id], "MessageID %d should be unique (D-008)", id)
			allIDs[id] = true
		}
	}
	assert.Len(t, allIDs, numGoroutines*msgsPerGoroutine,
		"should have %d unique MessageIDs (D-008)", numGoroutines*msgsPerGoroutine)

	// DB verification: exactly 50 messages in the conversation.
	ctx := context.Background()
	var msgCount int64
	env.db.DB().WithContext(ctx).Model(&model.Message{}).
		Where("conversation_id = ?", conv.ID).Count(&msgCount)
	assert.Equal(t, int64(numGoroutines*msgsPerGoroutine), msgCount,
		"should have exactly %d messages in DB", numGoroutines*msgsPerGoroutine)
}

// ---------------------------------------------------------------------------
// concurrent_create_conversation
// ---------------------------------------------------------------------------

// runConcurrentCreateConversation verifies that 10 pairs of users
// simultaneously calling create_conversation each produce exactly one
// conversation (D-011 find-or-create idempotency).
//
// Each pair uses unique user IDs (ccc-u0, ccc-u1, ...) to avoid collisions
// with other parallel subtests.
func runConcurrentCreateConversation(t *testing.T, env *e2eEnv) {
	t.Helper()

	const numPairs = 10

	type pairResult struct {
		convID    string
		duplicate bool
	}
	results := make([]pairResult, numPairs)

	var wg sync.WaitGroup
	wg.Add(numPairs * 2) // two users per pair

	conns := make([]*wsConn, numPairs*2)
	defer func() {
		for _, c := range conns {
			if c != nil {
				c.Close()
			}
		}
	}()

	for p := 0; p < numPairs; p++ {
		user1 := fmt.Sprintf("ccc-p%d-u1", p)
		user2 := fmt.Sprintf("ccc-p%d-u2", p)

		c1 := connectClient(t, env.addr, user1, "device-1")
		c2 := connectClient(t, env.addr, user2, "device-1")
		conns[p*2] = c1
		conns[p*2+1] = c2

		// Both users call create_conversation concurrently.
		go func(idx int, u1, u2 string, conn1, conn2 *wsConn) {
			defer wg.Done()
			sendRequest(t, conn1, fmt.Sprintf("ccc-create-%d-a", idx), "create_conversation", map[string]interface{}{
				"user_id": u2,
			})
			resp := readResponse(t, conn1, 10*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code,
				"create from user1 pair %d should succeed", idx)

			var data struct {
				Conversation struct {
					ID string `json:"ID"`
				} `json:"conversation"`
				Duplicate bool `json:"duplicate"`
			}
			require.NoError(t, json.Unmarshal(resp.Data, &data))
			results[idx] = pairResult{convID: data.Conversation.ID, duplicate: data.Duplicate}
		}(p, user1, user2, c1, c2)

		go func(idx int, u1 string, conn2 *wsConn) {
			defer wg.Done()
			sendRequest(t, conn2, fmt.Sprintf("ccc-create-%d-b", idx), "create_conversation", map[string]interface{}{
				"user_id": u1,
			})
			resp := readResponse(t, conn2, 10*time.Second)
			require.Equal(t, protocol.ResponseCodeOK, resp.Code,
				"create from user2 pair %d should succeed", idx)
		}(p, user1, c2)
	}

	wg.Wait()

	// Drain push updates from all connections.
	for _, c := range conns {
		if c != nil {
			drainPushUpdates(t, c)
		}
	}

	// Verify: each pair created exactly one conversation.
	for p := 0; p < numPairs; p++ {
		assert.NotEmpty(t, results[p].convID, "pair %d should have a conversation ID", p)
	}

	// DB verification: exactly numPairs conversations total.
	ctx := context.Background()
	var convCount int64
	env.db.DB().WithContext(ctx).Model(&model.Conversation{}).
		Where("user_id1 LIKE ?", "ccc-p%").Count(&convCount)
	assert.Equal(t, int64(numPairs), convCount,
		"should have exactly %d conversations (D-011)", numPairs)
}

// ---------------------------------------------------------------------------
// multi_device_broadcast
// ---------------------------------------------------------------------------

// runMultiDeviceBroadcast verifies that when a user has multiple WebSocket
// connections (simulating multiple devices), a message from the other party
// is delivered to ALL of them.
//
// alice connects 3 times (alice-dev1/2/3), bob connects once. bob sends a
// message. All 3 alice connections must receive the push update.
func runMultiDeviceBroadcast(t *testing.T, env *e2eEnv) {
	t.Helper()

	aliceUserID := "mdb-alice"
	bobUserID := "mdb-bob"

	// Alice connects 3 devices.
	aliceDevs := make([]*wsConn, 3)
	for i := 0; i < 3; i++ {
		aliceDevs[i] = connectClient(t, env.addr, aliceUserID, fmt.Sprintf("mdb-alice-dev-%d", i))
		defer aliceDevs[i].Close()
	}

	bobConn := connectClient(t, env.addr, bobUserID, "mdb-bob-dev")
	defer bobConn.Close()

	// Create conversation.
	conv := createTestConversation(t, env.store, aliceUserID, bobUserID)

	// Drain any initial messages.
	for _, d := range aliceDevs {
		drainPushUpdates(t, d)
	}
	drainPushUpdates(t, bobConn)

	// Bob sends a message.
	clientMsgID := uuid.New().String()
	sendRequest(t, bobConn, "mdb-send-1", "send_message", map[string]interface{}{
		"conversation_id":   conv.ID,
		"client_message_id": clientMsgID,
		"content":           "Hello from bob!",
		"type":              "text",
	})

	resp := readResponse(t, bobConn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp.Code, "bob send should succeed")

	// All 3 alice devices should receive the push update.
	var receivedWg sync.WaitGroup
	receivedWg.Add(3)
	for i := 0; i < 3; i++ {
		go func(devIdx int) {
			defer receivedWg.Done()
			updates := waitForUpdate(t, aliceDevs[devIdx], 15*time.Second)
			require.NotEmpty(t, updates.Updates,
				"alice device %d should receive at least 1 update", devIdx)

			var payload model.Message
			require.NoError(t, json.Unmarshal(updates.Updates[0].Payload, &payload))
			assert.Equal(t, "Hello from bob!", payload.Content,
				"alice device %d payload should match", devIdx)
		}(i)
	}

	receivedWg.Wait()

	// Bob also receives a push (C-10: fan-out includes sender).
	bobUpdates := waitForUpdate(t, bobConn, 10*time.Second)
	require.NotEmpty(t, bobUpdates.Updates, "bob should also receive push (C-10)")
}

// ---------------------------------------------------------------------------
// offline_then_sync
// ---------------------------------------------------------------------------

// runOfflineThenSync verifies that a user who disconnects, misses messages,
// then reconnects and calls sync_updates receives all missed updates.
//
// Steps:
//  1. alice and bob connect; create conversation.
//  2. alice disconnects.
//  3. bob sends 5 messages.
//  4. alice reconnects and calls sync_updates(after_seq=0).
//  5. alice receives all 5 updates.
func runOfflineThenSync(t *testing.T, env *e2eEnv) {
	t.Helper()

	aliceUserID := "ots-alice"
	bobUserID := "ots-bob"

	// 1. Both connect.
	aliceConn := connectClient(t, env.addr, aliceUserID, "device-1")
	bobConn := connectClient(t, env.addr, bobUserID, "device-1")
	defer bobConn.Close()

	conv := createTestConversation(t, env.store, aliceUserID, bobUserID)

	drainPushUpdates(t, aliceConn)
	drainPushUpdates(t, bobConn)

	// 2. Alice disconnects.
	aliceConn.Close()

	// Wait for connection store to clean up alice's stale connection.
	require.Eventually(t, func() bool {
		conns, err := env.connStore.ListByUser(context.Background(), aliceUserID, 10)
		return err == nil && len(conns) == 0
	}, 5*time.Second, 100*time.Millisecond, "alice should be disconnected")

	// 3. Bob sends 5 messages.
	for i := 0; i < 5; i++ {
		sendRequest(t, bobConn, fmt.Sprintf("ots-send-%d", i+1), "send_message", map[string]interface{}{
			"conversation_id":   conv.ID,
			"client_message_id": uuid.New().String(),
			"content":           fmt.Sprintf("missed message %d", i+1),
			"type":              "text",
		})
		resp := readResponse(t, bobConn, 5*time.Second)
		require.Equal(t, protocol.ResponseCodeOK, resp.Code,
			"bob send %d should succeed", i+1)
	}

	// Drain bob's push updates.
	drainPushUpdates(t, bobConn)

	// 4. Alice reconnects.
	aliceConn2 := connectClient(t, env.addr, aliceUserID, "device-1")
	defer aliceConn2.Close()

	// 5. Alice calls sync_updates.
	sendRequest(t, aliceConn2, "ots-sync-1", "sync_updates", map[string]interface{}{
		"after_seq": 0,
		"limit":     100,
	})

	syncResp := readResponse(t, aliceConn2, 10*time.Second)
	require.Equal(t, "ots-sync-1", syncResp.ID, "response ID should match")
	require.Equal(t, protocol.ResponseCodeOK, syncResp.Code, "sync should succeed")

	var syncData struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}
	require.NoError(t, json.Unmarshal(syncResp.Data, &syncData), "unmarshal sync data")

	// 6. Alice should have received all 5 updates.
	assert.Len(t, syncData.Updates, 5, "alice should receive all 5 missed updates")
	assert.False(t, syncData.HasMore, "has_more should be false")
	assert.Equal(t, uint32(5), syncData.LatestSeq, "latest_seq should be 5")

	// Verify content of each update.
	for i, u := range syncData.Updates {
		var payload model.Message
		require.NoError(t, json.Unmarshal(u.Payload, &payload),
			"unmarshal update %d payload", i)
		assert.Equal(t, fmt.Sprintf("missed message %d", i+1), payload.Content,
			"update %d content should match", i)
	}
}
