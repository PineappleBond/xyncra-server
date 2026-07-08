package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// WebSocketServer broadcast tests
// ---------------------------------------------------------------------------

// newTestClientWithBuffer creates a Client suitable for broadcast tests. It has
// a buffered send channel so that SendPackage succeeds without a real WebSocket
// connection, allowing us to inspect what was enqueued.
func newTestClientWithBuffer(userID, connID string) *Client {
	return &Client{
		userID: userID,
		connID: connID,
		send:   make(chan []byte, 10),
	}
}

// newTestWSServer creates a minimal WebSocketServer for broadcast tests. It
// does not start the HTTP listener or the Pub/Sub goroutine.
func newTestWSServer(t *testing.T) *WebSocketServer {
	t.Helper()
	srv, err := NewWebSocketServer(
		WSWithAddr(":0"),
		WSWithConnectionStore(&RedisConnectionStore{}),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
	)
	require.NoError(t, err)
	return srv
}

// newTestWSServerWithBroadcaster creates a minimal WebSocketServer with a
// custom NodeBroadcaster.
func newTestWSServerWithBroadcaster(t *testing.T, nb NodeBroadcaster) *WebSocketServer {
	t.Helper()
	srv, err := NewWebSocketServer(
		WSWithAddr(":0"),
		WSWithConnectionStore(&RedisConnectionStore{}),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
		WSWithNodeBroadcaster(nb),
	)
	require.NoError(t, err)
	return srv
}

// TestWebSocketServer_DefaultNodeBroadcaster verifies that when no
// NodeBroadcaster option is provided, the server uses a NoopBroadcaster.
func TestWebSocketServer_DefaultNodeBroadcaster(t *testing.T) {
	srv := newTestWSServer(t)

	// The nodeBroadcaster field should be a *NoopBroadcaster.
	_, ok := srv.nodeBroadcaster.(*NoopBroadcaster)
	assert.True(t, ok, "expected *NoopBroadcaster, got %T", srv.nodeBroadcaster)
}

// TestWebSocketServer_WithNodeBroadcaster verifies that WSWithNodeBroadcaster
// overrides the default NoopBroadcaster.
func TestWebSocketServer_WithNodeBroadcaster(t *testing.T) {
	nb := &NoopBroadcaster{} // Use NoopBroadcaster as a concrete example.
	srv := newTestWSServerWithBroadcaster(t, nb)

	assert.Same(t, nb, srv.nodeBroadcaster)
}

// TestHandleRemoteBroadcast_SkipSelf verifies that handleRemoteBroadcast does
// NOT call broadcastLocal when the sourceNodeID matches the server's own nodeID.
func TestHandleRemoteBroadcast_SkipSelf(t *testing.T) {
	srv := newTestWSServer(t)

	// Add a client for user-1 to the local index.
	client := newTestClientWithBuffer("user-1", "conn-1")
	srv.mu.Lock()
	srv.clientsByUser["user-1"] = map[string]*Client{
		"conn-1": client,
	}
	srv.mu.Unlock()

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{"self":"test"}`)},
		},
	}

	// Call handleRemoteBroadcast with the server's own nodeID.
	srv.handleRemoteBroadcast("user-1", updates, srv.nodeID)

	// The send channel should be empty because broadcastLocal was NOT called.
	select {
	case msg := <-client.send:
		t.Fatalf("expected no message (self message should be skipped), got %d bytes", len(msg))
	case <-time.After(50 * time.Millisecond):
		// Expected: no message delivered.
	}
}

// TestHandleRemoteBroadcast_RemoteNode verifies that handleRemoteBroadcast
// calls broadcastLocal when the sourceNodeID is from a different node.
func TestHandleRemoteBroadcast_RemoteNode(t *testing.T) {
	srv := newTestWSServer(t)

	// Add a client for user-1 to the local index.
	client := newTestClientWithBuffer("user-1", "conn-1")
	srv.mu.Lock()
	srv.clientsByUser["user-1"] = map[string]*Client{
		"conn-1": client,
	}
	srv.mu.Unlock()

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 7, Payload: json.RawMessage(`{"remote":"data"}`)},
		},
	}

	// Call handleRemoteBroadcast with a different sourceNodeID.
	srv.handleRemoteBroadcast("user-1", updates, "some-other-node-id")

	// The send channel should have a message because broadcastLocal was called.
	select {
	case msg := <-client.send:
		// Verify the message is a valid Package with type Updates.
		var pkg protocol.Package
		require.NoError(t, json.Unmarshal(msg, &pkg))
		assert.Equal(t, protocol.PackageTypeUpdates, pkg.Type)

		// Verify the payload contains the expected updates.
		var gotUpdates protocol.PackageDataUpdates
		require.NoError(t, json.Unmarshal(pkg.Data, &gotUpdates))
		require.Len(t, gotUpdates.Updates, 1)
		assert.Equal(t, uint32(7), gotUpdates.Updates[0].Seq)
		assert.JSONEq(t, `{"remote":"data"}`, string(gotUpdates.Updates[0].Payload))
	case <-time.After(2 * time.Second):
		t.Fatal("expected message from remote broadcast, got none")
	}
}

// TestHandleRemoteBroadcast_MultipleClients verifies that handleRemoteBroadcast
// delivers to all local connections of the target user.
func TestHandleRemoteBroadcast_MultipleClients(t *testing.T) {
	srv := newTestWSServer(t)

	// Add two clients for the same user.
	c1 := newTestClientWithBuffer("user-1", "conn-1")
	c2 := newTestClientWithBuffer("user-1", "conn-2")
	srv.mu.Lock()
	srv.clientsByUser["user-1"] = map[string]*Client{
		"conn-1": c1,
		"conn-2": c2,
	}
	srv.mu.Unlock()

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{}`)},
		},
	}

	srv.handleRemoteBroadcast("user-1", updates, "other-node")

	// Both clients should have received a message.
	for _, c := range []*Client{c1, c2} {
		select {
		case msg := <-c.send:
			var pkg protocol.Package
			require.NoError(t, json.Unmarshal(msg, &pkg))
			assert.Equal(t, protocol.PackageTypeUpdates, pkg.Type)
		case <-time.After(2 * time.Second):
			t.Fatalf("conn %s did not receive broadcast", c.ConnID())
		}
	}
}

// TestHandleRemoteBroadcast_NoLocalClients verifies that handleRemoteBroadcast
// does not panic when there are no local connections for the target user.
func TestHandleRemoteBroadcast_NoLocalClients(t *testing.T) {
	srv := newTestWSServer(t)

	// No clients registered for "user-ghost".
	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Payload: json.RawMessage(`{}`)},
		},
	}

	// Should not panic even with no local clients.
	assert.NotPanics(t, func() {
		srv.handleRemoteBroadcast("user-ghost", updates, "other-node")
	})
}
