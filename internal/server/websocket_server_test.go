package server

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Async device replacement tests (D-095, D-111)
//
// These tests verify that the device replacement logic in handleWebSocket
// upgrades the new connection first and performs old-connection cleanup in a
// background goroutine, so the HTTP handler is never blocked.
// ---------------------------------------------------------------------------

// readCloseCode reads from conn until a close error is returned, then extracts
// the WebSocket close code. Returns 0 if the error is not a CloseError.
func readCloseCode(conn *websocket.Conn, timeout time.Duration) int {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	_, _, err := conn.ReadMessage()
	if ce, ok := err.(*websocket.CloseError); ok {
		return ce.Code
	}
	return 0
}

// TestDeviceReplacement_Async verifies that device replacement does not block
// the HTTP handler: the second (replacement) connection's Upgrade completes
// quickly (< 50ms) while the old connection is cleaned up asynchronously.
func TestDeviceReplacement_Async(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
		WSWithPongWait(30*time.Second),
	)
	defer cleanup()

	const userID = "user-async-replace"
	const deviceID = "device-async"

	// Step 1: Connect the first client.
	connA := connectWSWithDevice(t, addr, userID, deviceID)
	defer connA.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	// Step 2: Dial the second connection and measure the Upgrade latency.
	// Because performDeviceReplacement runs asynchronously, the dial should
	// complete almost immediately — not wait for old-connection cleanup (which
	// includes a 10ms sleep + Close + 500ms Done wait).
	start := time.Now()
	connB := connectWSWithDevice(t, addr, userID, deviceID)
	upgradeDuration := time.Since(start)
	defer connB.Close()

	// The Upgrade should complete well under 50ms. The async cleanup path
	// would take > 500ms if it blocked.
	assert.Less(t, upgradeDuration, 50*time.Millisecond,
		"Upgrade should not block on device replacement cleanup (took %v)", upgradeDuration)

	// Step 3: Wait for async cleanup to finish. The old connection should
	// receive a 4001 close frame (D-095).
	closeCode := readCloseCode(connA, 5*time.Second)
	assert.Equal(t, 4001, closeCode,
		"old connection should receive close code 4001 (D-095)")

	// Step 4: Wait for the server to settle — only the new connection should
	// remain.
	require.Eventually(t, func() bool {
		return srv.ClientCount() == 1
	}, 5*time.Second, 50*time.Millisecond,
		"only the replacement connection should remain after async cleanup")
}

// TestDeviceReplacement_Concurrent verifies that concurrent connections for the
// same (userID, deviceID) do not cause panics or data races, and that the
// server eventually converges to a single surviving connection.
//
// When N goroutines dial truly concurrently, each captures a different snapshot
// of existing clients. Each goroutine's cleanup only closes the clients it
// captured, so some connections may survive the initial burst. A final serial
// replacement is used to trigger cleanup of all survivors, verifying that the
// system converges to exactly one connection (D-095).
func TestDeviceReplacement_Concurrent(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
		WSWithPongWait(30*time.Second),
	)
	defer cleanup()

	const userID = "user-concurrent-replace"
	const deviceID = "device-concurrent"

	// Connect the initial client.
	connInitial := connectWSWithDevice(t, addr, userID, deviceID)
	defer connInitial.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	// Phase 1: Launch 5 concurrent connections for the same device. The
	// primary assertion is that all dials succeed without panics or data
	// races. Under the race detector the async cleanup is slower, so some
	// connections may still be alive here.
	const numConcurrent = 5
	var wg sync.WaitGroup
	conns := make([]*websocket.Conn, numConcurrent)
	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := connectWSWithDevice(t, addr, userID, deviceID)
			conns[idx] = c
		}(i)
	}
	wg.Wait()

	// Defer close on all replacement connections.
	for _, c := range conns {
		if c != nil {
			defer c.Close()
		}
	}

	// Verify that at least some connections survived the concurrent burst.
	assert.GreaterOrEqual(t, srv.ClientCount(), 1,
		"at least one connection should survive the concurrent burst")

	// Phase 2: Serially connect one final replacement. This connection
	// captures all survivors and triggers their cleanup, guaranteeing
	// convergence to exactly one connection.
	connFinal := connectWSWithDevice(t, addr, userID, deviceID)
	defer connFinal.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() == 1
	}, 30*time.Second, 200*time.Millisecond,
		"exactly one connection should survive after final serial replacement")

	assert.Equal(t, 1, srv.ClientsByUser(userID),
		"only one connection should remain for the user")
}

// TestDeviceReplacement_RapidReconnect verifies that 10 rapid reconnections
// (100ms apart) for the same device do not form a reconnection loop and
// result in a single surviving connection.
func TestDeviceReplacement_RapidReconnect(t *testing.T) {
	srv, addr, cleanup := startWSServerMem(t,
		WSWithPingPeriod(1*time.Hour),
		WSWithPongWait(30*time.Second),
	)
	defer cleanup()

	const userID = "user-rapid-reconnect"
	const deviceID = "device-rapid"
	const numReconnects = 10

	// Connect the initial client.
	connInitial := connectWSWithDevice(t, addr, userID, deviceID)
	defer connInitial.Close()

	require.Eventually(t, func() bool {
		return srv.ClientCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	// Rapidly reconnect 10 times, 100ms apart.
	reconnectConns := make([]*websocket.Conn, numReconnects)
	for i := 0; i < numReconnects; i++ {
		c := connectWSWithDevice(t, addr, userID, deviceID)
		reconnectConns[i] = c
		time.Sleep(100 * time.Millisecond)
	}

	// Defer close on all reconnected connections.
	for _, c := range reconnectConns {
		defer c.Close()
	}

	// Wait for async cleanup to settle.
	require.Eventually(t, func() bool {
		return srv.ClientCount() == 1
	}, 10*time.Second, 100*time.Millisecond,
		"exactly one connection should survive rapid reconnects (D-095, D-111)")

	// Verify that old connections received 4001 close frames. At minimum, the
	// initial connection should have received a 4001.
	closeCode := readCloseCode(connInitial, 2*time.Second)
	assert.Equal(t, 4001, closeCode,
		fmt.Sprintf("initial connection should receive 4001 close frame, got %d", closeCode))

	// Final sanity: the server should report exactly 1 client.
	assert.Equal(t, 1, srv.ClientCount(), "server should have exactly 1 client")
	assert.Equal(t, 1, srv.ClientsByUser(userID), "user should have exactly 1 connection")
}
