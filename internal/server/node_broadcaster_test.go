package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NoopBroadcaster tests
// ---------------------------------------------------------------------------

// TestNoopBroadcaster_Publish verifies that Publish is a no-op and returns nil.
func TestNoopBroadcaster_Publish(t *testing.T) {
	nb := &NoopBroadcaster{}
	err := nb.Publish(context.Background(), "user-1", &protocol.PackageDataUpdates{}, "node-1")
	assert.NoError(t, err)
}

// TestNoopBroadcaster_Subscribe verifies that Subscribe blocks until ctx is
// cancelled and then returns ctx.Err().
func TestNoopBroadcaster_Subscribe(t *testing.T) {
	nb := &NoopBroadcaster{}

	ctx, cancel := context.WithCancel(context.Background())

	var callbackInvoked bool
	done := make(chan error, 1)
	go func() {
		done <- nb.Subscribe(ctx, func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) {
			callbackInvoked = true
		})
	}()

	// Give Subscribe a moment to start blocking.
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancel")
	}

	assert.False(t, callbackInvoked, "callback should not be invoked on NoopBroadcaster")
}

// TestNoopBroadcaster_Close verifies that Close is a no-op and returns nil.
func TestNoopBroadcaster_Close(t *testing.T) {
	nb := &NoopBroadcaster{}
	err := nb.Close()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// RedisNodeBroadcaster tests (require Redis)
// ---------------------------------------------------------------------------

// newTestRedisBroadcaster creates a RedisNodeBroadcaster backed by a dedicated
// redis.Client connected to the test Redis instance. The caller must call the
// returned cleanup function when done.
func newTestRedisBroadcaster(t *testing.T, keyPrefix string) (*RedisNodeBroadcaster, *redis.Client, func()) {
	t.Helper()
	skipIfNoRedis(t)

	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})

	// Verify connectivity.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, client.Ping(ctx).Err(), "test Redis not reachable")

	b := NewRedisNodeBroadcaster(client, keyPrefix)

	cleanup := func() {
		_ = b.Close()
		_ = client.Close()
	}

	return b, client, cleanup
}

// TestRedisNodeBroadcaster_Publish verifies that Publish sends a message to
// the expected Redis channel.
func TestRedisNodeBroadcaster_Publish(t *testing.T) {
	skipIfNoRedis(t)

	// Use a dedicated client for subscribing (Pub/Sub requires its own conn).
	subClient := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testRedisDB})
	defer subClient.Close()

	b, _, cleanup := newTestRedisBroadcaster(t, "test-pub")
	defer cleanup()

	// Subscribe on the channel to capture the published message.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ps := subClient.PSubscribe(ctx, "test-pub:broadcast:*")
	defer ps.Close()

	ch := ps.Channel()

	// Publish a message.
	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{"key":"value"}`)},
		},
	}
	err := b.Publish(ctx, "user-1", updates, "node-A")
	require.NoError(t, err)

	// Read the message from the subscription.
	select {
	case msg := <-ch:
		require.NotNil(t, msg)
		assert.Equal(t, "test-pub:broadcast:user-1", msg.Channel)

		var bm broadcastMessage
		require.NoError(t, json.Unmarshal([]byte(msg.Payload), &bm))
		assert.Equal(t, "node-A", bm.SourceNodeID)
		require.NotNil(t, bm.Updates)
		require.Len(t, bm.Updates.Updates, 1)
		assert.Equal(t, uint32(1), bm.Updates.Updates[0].Seq)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

// TestRedisNodeBroadcaster_Subscribe verifies that Subscribe receives messages
// published to the broadcast channel.
func TestRedisNodeBroadcaster_Subscribe(t *testing.T) {
	b, _, cleanup := newTestRedisBroadcaster(t, "test-sub")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type receivedMsg struct {
		userID       string
		updates      *protocol.PackageDataUpdates
		sourceNodeID string
	}

	var (
		mu       sync.Mutex
		received []receivedMsg
	)

	// Start Subscribe in a goroutine.
	subDone := make(chan error, 1)
	go func() {
		subDone <- b.Subscribe(ctx, func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) {
			mu.Lock()
			received = append(received, receivedMsg{userID, updates, sourceNodeID})
			mu.Unlock()
		})
	}()

	// Give Subscribe a moment to set up PSubscribe.
	time.Sleep(100 * time.Millisecond)

	// Publish a message using a separate publisher broadcaster.
	publisher, _, pubCleanup := newTestRedisBroadcaster(t, "test-sub")
	defer pubCleanup()

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 42, Payload: json.RawMessage(`{"data":"test"}`)},
		},
	}
	err := publisher.Publish(ctx, "user-42", updates, "node-B")
	require.NoError(t, err)

	// Wait for the callback to be invoked.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) > 0
	}, 3*time.Second, 50*time.Millisecond, "callback was not invoked")

	mu.Lock()
	r := received[0]
	mu.Unlock()

	assert.Equal(t, "user-42", r.userID)
	assert.Equal(t, "node-B", r.sourceNodeID)
	require.NotNil(t, r.updates)
	require.Len(t, r.updates.Updates, 1)
	assert.Equal(t, uint32(42), r.updates.Updates[0].Seq)

	// Cancel context to stop Subscribe.
	cancel()
	select {
	case err := <-subDone:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancel")
	}
}

// TestRedisNodeBroadcaster_PayloadRoundTrip verifies that the payload
// survives JSON serialization/deserialization intact.
func TestRedisNodeBroadcaster_PayloadRoundTrip(t *testing.T) {
	b, _, cleanup := newTestRedisBroadcaster(t, "test-payload")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	original := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{"a":1}`), CreatedAt: time.Now().Truncate(time.Millisecond)},
			{Seq: 2, Payload: json.RawMessage(`{"b":"two"}`), CreatedAt: time.Now().Truncate(time.Millisecond)},
		},
	}

	// Verify broadcastMessage round-trips through JSON.
	bm := broadcastMessage{SourceNodeID: "node-X", Updates: original}
	data, err := json.Marshal(bm)
	require.NoError(t, err)

	var decoded broadcastMessage
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, "node-X", decoded.SourceNodeID)
	require.NotNil(t, decoded.Updates)
	require.Len(t, decoded.Updates.Updates, 2)
	assert.Equal(t, uint32(1), decoded.Updates.Updates[0].Seq)
	assert.Equal(t, uint32(2), decoded.Updates.Updates[1].Seq)
	assert.JSONEq(t, `{"a":1}`, string(decoded.Updates.Updates[0].Payload))
	assert.JSONEq(t, `{"b":"two"}`, string(decoded.Updates.Updates[1].Payload))

	// Also verify end-to-end via Publish/Subscribe.
	var (
		gotUpdates *protocol.PackageDataUpdates
		done       = make(chan struct{})
	)

	subDone := make(chan error, 1)
	go func() {
		subDone <- b.Subscribe(ctx, func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) {
			gotUpdates = updates
			close(done)
		})
	}()
	time.Sleep(100 * time.Millisecond)

	err = b.Publish(ctx, "user-rt", original, "node-RT")
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for round-trip message")
	}

	require.NotNil(t, gotUpdates)
	require.Len(t, gotUpdates.Updates, 2)
	assert.Equal(t, uint32(1), gotUpdates.Updates[0].Seq)
	assert.JSONEq(t, `{"a":1}`, string(gotUpdates.Updates[0].Payload))

	cancel()
	<-subDone
}

// TestRedisNodeBroadcaster_CloseReturns verifies that Close causes Subscribe
// to return.
func TestRedisNodeBroadcaster_CloseReturns(t *testing.T) {
	b, _, cleanup := newTestRedisBroadcaster(t, "test-close")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	subDone := make(chan error, 1)
	go func() {
		subDone <- b.Subscribe(ctx, func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) {})
	}()

	// Give Subscribe time to set up PSubscribe.
	time.Sleep(100 * time.Millisecond)

	// Close should cause Subscribe to return.
	require.NoError(t, b.Close())

	select {
	case <-subDone:
		// Subscribe returned as expected.
	case <-time.After(3 * time.Second):
		t.Fatal("Subscribe did not return after Close")
	}
}

// TestRedisNodeBroadcaster_CrossNode verifies that two independent broadcaster
// instances (simulating two nodes) can exchange messages via Redis Pub/Sub.
func TestRedisNodeBroadcaster_CrossNode(t *testing.T) {
	// Node A.
	bA, _, cleanupA := newTestRedisBroadcaster(t, "test-cross")
	defer cleanupA()

	// Node B.
	bB, _, cleanupB := newTestRedisBroadcaster(t, "test-cross")
	defer cleanupB()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Node B subscribes.
	var (
		gotUserID  string
		gotNodeID  string
		gotUpdates *protocol.PackageDataUpdates
		done       = make(chan struct{})
	)

	subDone := make(chan error, 1)
	go func() {
		subDone <- bB.Subscribe(ctx, func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) {
			gotUserID = userID
			gotNodeID = sourceNodeID
			gotUpdates = updates
			close(done)
		})
	}()
	time.Sleep(100 * time.Millisecond)

	// Node A publishes.
	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 99, Payload: json.RawMessage(`{"cross":"node"}`)},
		},
	}
	err := bA.Publish(ctx, "user-cross", updates, "node-A")
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cross-node message")
	}

	assert.Equal(t, "user-cross", gotUserID)
	assert.Equal(t, "node-A", gotNodeID)
	require.NotNil(t, gotUpdates)
	require.Len(t, gotUpdates.Updates, 1)
	assert.Equal(t, uint32(99), gotUpdates.Updates[0].Seq)

	cancel()
	<-subDone
}

// ---------------------------------------------------------------------------
// Edge case tests
// ---------------------------------------------------------------------------

// TestRedisNodeBroadcaster_PublishEmptyUserID verifies that Publish returns an
// error when userID is empty.
func TestRedisNodeBroadcaster_PublishEmptyUserID(t *testing.T) {
	skipIfNoRedis(t)

	client := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testRedisDB})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, client.Ping(ctx).Err(), "test Redis not reachable")

	b := NewRedisNodeBroadcaster(client, "test-empty")
	defer b.Close()

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{"test":"data"}`)},
		},
	}

	err := b.Publish(ctx, "", updates, "node-1")
	assert.Error(t, err, "Publish should return error when userID is empty")
	assert.Contains(t, err.Error(), "user ID is required")
}

// TestRedisNodeBroadcaster_SubscribeMalformedJSON verifies that Subscribe
// continues processing messages after receiving malformed JSON.
func TestRedisNodeBroadcaster_SubscribeMalformedJSON(t *testing.T) {
	skipIfNoRedis(t)

	b, _, cleanup := newTestRedisBroadcaster(t, "test-malformed")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		mu            sync.Mutex
		validMsgCount int
	)

	// Start Subscribe in a goroutine.
	subDone := make(chan error, 1)
	go func() {
		subDone <- b.Subscribe(ctx, func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) {
			mu.Lock()
			validMsgCount++
			mu.Unlock()
		})
	}()

	// Give Subscribe time to set up PSubscribe.
	time.Sleep(100 * time.Millisecond)

	// Use a separate client to publish malformed JSON directly to the channel.
	publisherClient := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testRedisDB})
	defer publisherClient.Close()

	channel := "test-malformed:broadcast:user-test"

	// Publish malformed JSON (should be skipped).
	err := publisherClient.Publish(ctx, channel, "not valid json").Err()
	require.NoError(t, err)

	// Give time for the malformed message to be processed.
	time.Sleep(100 * time.Millisecond)

	// Now publish a valid message to verify Subscribe is still running.
	validMsg := broadcastMessage{
		SourceNodeID: "node-valid",
		Updates: &protocol.PackageDataUpdates{
			Updates: []protocol.PackageDataUpdate{
				{Seq: 1, Payload: json.RawMessage(`{"valid":"message"}`)},
			},
		},
	}
	validPayload, err := json.Marshal(validMsg)
	require.NoError(t, err)

	err = publisherClient.Publish(ctx, channel, string(validPayload)).Err()
	require.NoError(t, err)

	// Wait for the valid message callback.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return validMsgCount > 0
	}, 3*time.Second, 50*time.Millisecond, "Subscribe should still process valid messages after malformed JSON")

	mu.Lock()
	count := validMsgCount
	mu.Unlock()
	assert.Equal(t, 1, count, "should have received exactly 1 valid message")

	cancel()
	select {
	case <-subDone:
		// Subscribe returned as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancel")
	}
}

// TestRedisNodeBroadcaster_CloseIdempotent verifies that Close can be called
// multiple times safely without panic.
func TestRedisNodeBroadcaster_CloseIdempotent(t *testing.T) {
	skipIfNoRedis(t)

	b, _, cleanup := newTestRedisBroadcaster(t, "test-idempotent")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start Subscribe to initialize the ps field.
	subDone := make(chan error, 1)
	go func() {
		subDone <- b.Subscribe(ctx, func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) {})
	}()

	// Give Subscribe time to set up PSubscribe.
	time.Sleep(100 * time.Millisecond)

	// First Close should succeed.
	err1 := b.Close()
	assert.NoError(t, err1, "first Close should succeed")

	// Wait for Subscribe to return.
	select {
	case <-subDone:
		// Subscribe returned as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after first Close")
	}

	// Second Close should also succeed (idempotent).
	err2 := b.Close()
	assert.NoError(t, err2, "second Close should succeed (idempotent)")

	// Third Close should also succeed.
	err3 := b.Close()
	assert.NoError(t, err3, "third Close should succeed (idempotent)")
}
