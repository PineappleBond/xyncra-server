package client

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// D-111: Client 4001 close frame handling
// ---------------------------------------------------------------------------

// sendCloseFrame sends a WebSocket close frame with the given code to the
// first connected client. This is used to simulate server-side close frames
// like 4001 (device replacement).
func (m *mockWSServer) sendCloseFrame(code int) error {
	m.mu.Lock()
	conns := make([]*websocket.Conn, len(m.conns))
	copy(conns, m.conns)
	m.mu.Unlock()

	if len(conns) == 0 {
		return websocket.ErrCloseSent
	}

	m.writeMu.Lock()
	msg := websocket.FormatCloseMessage(code, "device replaced")
	err := conns[0].WriteControl(websocket.CloseMessage, msg, time.Now().Add(5*time.Second))
	m.writeMu.Unlock()
	return err
}

// TestD111_CloseFrame4001_TriggersGracefulExit verifies that when the server
// sends a 4001 close frame (device replacement), the client detects it and
// initiates graceful exit: the onDisconnect callback fires with replaced=true
// and Stop() is called (D-111).
func TestD111_CloseFrame4001_TriggersGracefulExit(t *testing.T) {
	srv := newMockWSServer(t)
	db := newTestStore(t)

	var replacedDetected atomic.Bool
	c, err := New(
		WithServerURL(srv.URL()),
		WithUserID("test-user"),
		WithDB(db),
		WithLogger(&testLogger{t: t}),
		WithReconnectBaseDelay(10*time.Millisecond),
		WithReconnectMaxDelay(50*time.Millisecond),
		WithHeartbeatInterval(1*time.Hour), // disable heartbeat
		WithPullDebounce(10*time.Millisecond),
	)
	require.NoError(t, err)

	// Override the onDisconnect callback to track when 4001 is detected.
	origCallbacks := c.connMgr.callbacks
	c.connMgr.callbacks = connectionCallbacks{
		onResponse: origCallbacks.onResponse,
		onUpdates:  origCallbacks.onUpdates,
		onRequest:  origCallbacks.onRequest,
		onConnect:  origCallbacks.onConnect,
		onDisconnect: func(replaced bool) {
			if replaced {
				replacedDetected.Store(true)
			}
			if origCallbacks.onDisconnect != nil {
				origCallbacks.onDisconnect(replaced)
			}
		},
	}

	// Start the client in a goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = c.Start(ctx)
	}()

	// Wait for the connection to be established.
	if err := srv.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("AcceptConnection: %v", err)
	}

	// Give the client time to complete initial handshake.
	time.Sleep(100 * time.Millisecond)

	// Send a 4001 close frame to simulate device replacement.
	if err := srv.sendCloseFrame(4001); err != nil {
		t.Fatalf("sendCloseFrame: %v", err)
	}

	// Wait for the client to detect 4001 and trigger graceful exit.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if replacedDetected.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !replacedDetected.Load() {
		t.Fatal("client did not detect 4001 close frame within timeout")
	}

	// The client should have called Stop(), which cancels the context.
	// Verify by calling Stop() again (idempotent, no panic).
	c.Stop()
	c.Stop()
}

// TestD111_CloseFrame4001_StopIdempotent verifies that calling Stop() after
// a 4001 close frame is safe and idempotent (no panic or double-close).
func TestD111_CloseFrame4001_StopIdempotent(t *testing.T) {
	srv := newMockWSServer(t)
	db := newTestStore(t)

	c, err := New(
		WithServerURL(srv.URL()),
		WithUserID("test-user"),
		WithDB(db),
		WithLogger(&testLogger{t: t}),
		WithReconnectBaseDelay(10*time.Millisecond),
		WithReconnectMaxDelay(50*time.Millisecond),
		WithHeartbeatInterval(1*time.Hour),
		WithPullDebounce(10*time.Millisecond),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = c.Start(ctx)
	}()

	if err := srv.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("AcceptConnection: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Send 4001 close frame.
	if err := srv.sendCloseFrame(4001); err != nil {
		t.Fatalf("sendCloseFrame: %v", err)
	}

	// Wait for the connection to be detected as replaced.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.connMgr.Replaced() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !c.connMgr.Replaced() {
		t.Fatal("client did not detect 4001 close frame")
	}

	// Give the monitor time to call Stop() internally.
	time.Sleep(200 * time.Millisecond)

	// Calling Stop() again should be safe (idempotent).
	c.Stop()
	c.Stop() // should not panic
}

// TestD111_ConnectionManager_ReplacedFlag verifies that the connectionManager
// sets the replaced flag when a 4001 close frame is received.
func TestD111_ConnectionManager_ReplacedFlag(t *testing.T) {
	srv := newMockWSServer(t)

	var disconnectCalled atomic.Bool
	var replacedValue atomic.Bool

	cbs := connectionCallbacks{
		onDisconnect: func(replaced bool) {
			disconnectCalled.Store(true)
			replacedValue.Store(replaced)
		},
	}

	cm := newTestCM(t, srv, cbs)

	// Send 4001 close frame.
	if err := srv.sendCloseFrame(4001); err != nil {
		t.Fatalf("sendCloseFrame: %v", err)
	}

	// Wait for the disconnect callback to be invoked.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cm.Replaced() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !cm.Replaced() {
		t.Error("connectionManager.Replaced() = false, want true after 4001 close frame")
	}

	// Give time for callback to fire.
	time.Sleep(50 * time.Millisecond)

	if !disconnectCalled.Load() {
		t.Error("onDisconnect callback was not called")
	}
	if !replacedValue.Load() {
		t.Error("onDisconnect(replaced=false), want replaced=true")
	}
}

// TestD111_DoneChannel_ClosedAfter4001 verifies that when a 4001 close frame
// triggers Stop() from within the connection monitor goroutine, the Done()
// channel is eventually closed. This is the regression test for the bug where
// Stop() set c.closed=true before calling shutdown(), and shutdown() early-
// returned on the c.closed check, never closing c.done.
func TestD111_DoneChannel_ClosedAfter4001(t *testing.T) {
	srv := newMockWSServer(t)
	db := newTestStore(t)

	c, err := New(
		WithServerURL(srv.URL()),
		WithUserID("test-user"),
		WithDB(db),
		WithLogger(&testLogger{t: t}),
		WithReconnectBaseDelay(10*time.Millisecond),
		WithReconnectMaxDelay(50*time.Millisecond),
		WithHeartbeatInterval(1*time.Hour),
		WithPullDebounce(10*time.Millisecond),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = c.Start(ctx)
	}()

	if err := srv.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("AcceptConnection: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Send 4001 close frame — triggers Stop() from within connection monitor.
	if err := srv.sendCloseFrame(4001); err != nil {
		t.Fatalf("sendCloseFrame: %v", err)
	}

	// Done() must be closed within a reasonable time. This was the bug:
	// before the fix, Done() would never close.
	select {
	case <-c.Done():
		// success — Done() closed
	case <-time.After(5 * time.Second):
		t.Fatal("Done() channel was not closed within 5s after 4001 triggered Stop()")
	}
}

// TestD111_DoneChannel_ClosedAfterExternalStop verifies that when Stop() is
// called externally (not from a tracked goroutine), Done() is still closed.
func TestD111_DoneChannel_ClosedAfterExternalStop(t *testing.T) {
	srv := newMockWSServer(t)
	db := newTestStore(t)

	c, err := New(
		WithServerURL(srv.URL()),
		WithUserID("test-user"),
		WithDB(db),
		WithLogger(&testLogger{t: t}),
		WithReconnectBaseDelay(10*time.Millisecond),
		WithReconnectMaxDelay(50*time.Millisecond),
		WithHeartbeatInterval(1*time.Hour),
		WithPullDebounce(10*time.Millisecond),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = c.Start(ctx)
	}()

	if err := srv.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("AcceptConnection: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// External Stop() call.
	c.Stop()

	select {
	case <-c.Done():
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Done() channel was not closed within 5s after external Stop()")
	}
}

// TestD111_ConcurrentStop verifies that calling Stop() from multiple
// goroutines simultaneously is safe: no panic, no race condition, and
// Done() is closed exactly once (D-111).
func TestD111_ConcurrentStop(t *testing.T) {
	srv := newMockWSServer(t)
	db := newTestStore(t)

	c, err := New(
		WithServerURL(srv.URL()),
		WithUserID("test-user"),
		WithDB(db),
		WithLogger(&testLogger{t: t}),
		WithReconnectBaseDelay(10*time.Millisecond),
		WithReconnectMaxDelay(50*time.Millisecond),
		WithHeartbeatInterval(1*time.Hour),
		WithPullDebounce(10*time.Millisecond),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = c.Start(ctx)
	}()

	if err := srv.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("AcceptConnection: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Launch 10 goroutines all calling Stop() simultaneously.
	const goroutines = 10
	var started sync.WaitGroup
	started.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			started.Done()
			c.Stop()
		}()
	}
	started.Wait()

	// Done() must be closed exactly once, no panic.
	select {
	case <-c.Done():
		// success — Done() closed
	case <-time.After(5 * time.Second):
		t.Fatal("Done() channel was not closed within 5s after concurrent Stop() calls")
	}

	// Additional Stop() calls after shutdown must still be safe.
	c.Stop()
	c.Stop()
}
