package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// connectionCallbacks
// ---------------------------------------------------------------------------

// connectionCallbacks holds the functions invoked by the connection manager at
// key lifecycle events. All callbacks are optional and may be nil.
type connectionCallbacks struct {
	// onResponse is called when a PackageTypeResponse is received from the server.
	onResponse func(*protocol.PackageDataResponse)
	// onUpdates is called when a PackageTypeUpdates batch is received from the server.
	onUpdates func(*protocol.PackageDataUpdates)
	// onRequest is called when a server-initiated PackageTypeRequest is received (D-092).
	onRequest func(*protocol.PackageDataRequest)
	// onConnect is called after a WebSocket connection has been successfully established.
	onConnect func()
	// onDisconnect is called when an active WebSocket connection is lost unexpectedly.
	onDisconnect func()
}

// ---------------------------------------------------------------------------
// connectionManager
// ---------------------------------------------------------------------------

// connectionManager manages the lifecycle of a single WebSocket connection to
// the Xyncra server, including reading, writing, heartbeating, and
// reconnection with exponential backoff. It mirrors the readPump/writePump
// dual-goroutine pattern used by the server's Client type.
//
// The zero value is not usable; use newConnectionManager to create an instance.
type connectionManager struct {
	// serverURL is the base WebSocket URL (without query parameters).
	serverURL string
	// userID is the authenticated user identifier appended as a query parameter
	// per D-005.
	userID string
	// deviceID identifies this device, appended as a query parameter (D-033).
	deviceID string

	// Connection tuning parameters.
	writeWait   time.Duration
	pongWait    time.Duration
	pingPeriod  time.Duration
	sendBufSize int
	maxMsgSize  int64

	// Reconnect parameters.
	baseDelay  time.Duration
	maxDelay   time.Duration
	maxRetries int

	// callbacks for lifecycle events.
	callbacks connectionCallbacks

	// mu protects conn, send, connected, closing, attempt, disconnectCh,
	// ctx, cancel, and pumpsDone.
	mu sync.Mutex
	// conn is the current WebSocket connection (nil when not connected).
	conn *websocket.Conn
	// send is the buffered channel of outbound JSON-encoded messages.
	send chan []byte
	// connected indicates whether a WebSocket connection is currently active.
	connected bool
	// closing indicates that Close has been called; no reconnect should occur.
	closing bool
	// attempt is the current reconnect attempt counter (reset to 0 on success).
	attempt int

	// disconnectCh is closed when an unexpected disconnection occurs, allowing
	// the connectionMonitor (in client.go) to detect and react to it.
	disconnectCh chan struct{}
	// ctx is the cancellation context for the current connection session.
	ctx context.Context
	// cancel cancels ctx.
	cancel context.CancelFunc
	// pumpsDone is closed when both readPump and writePump have exited for the
	// current connection session.
	pumpsDone chan struct{}

	// logger is used for diagnostic output.
	logger Logger
}

// newConnectionManager creates a connectionManager from the given clientOptions
// and callbacks. Connection-level parameters that are not part of clientOptions
// use the default constants defined in options.go.
func newConnectionManager(opts clientOptions, callbacks connectionCallbacks) *connectionManager {
	deviceID := opts.deviceID
	if deviceID == "" {
		deviceID = uuid.New().String()
	}
	return &connectionManager{
		serverURL:   opts.serverURL,
		userID:      opts.userID,
		deviceID:    deviceID,
		writeWait:   defaultWriteWait,
		pongWait:    defaultPongWait,
		pingPeriod:  defaultPingPeriod,
		sendBufSize: defaultSendBufSize,
		maxMsgSize:  defaultMaxMessageSize,
		baseDelay:   opts.reconnectBaseDelay,
		maxDelay:    opts.reconnectMaxDelay,
		maxRetries:  opts.reconnectMaxRetries,
		callbacks:   callbacks,
		logger:      opts.logger,
	}
}

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// Connect establishes a WebSocket connection to the server and starts the
// readPump and writePump goroutines. Per D-005 the user_id query parameter is
// appended to the server URL for authentication.
func (cm *connectionManager) Connect(ctx context.Context) error {
	u, err := url.Parse(cm.serverURL)
	if err != nil {
		return NewConnectionError(fmt.Errorf("parse server URL: %w", err))
	}
	q := u.Query()
	q.Set("user_id", cm.userID)
	q.Set("device_id", cm.deviceID)
	u.RawQuery = q.Encode()

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return NewConnectionError(fmt.Errorf("dial websocket: %w", err))
	}

	// Create session-scoped channels. We capture these in local variables so
	// that the goroutine closures reference the correct instance even if a
	// future Connect/Reconnect replaces the struct fields.
	send := make(chan []byte, cm.sendBufSize)
	disconnectCh := make(chan struct{})
	pumpsDone := make(chan struct{})
	sessionCtx, sessionCancel := context.WithCancel(ctx)

	cm.mu.Lock()
	cm.conn = conn
	cm.send = send
	cm.disconnectCh = disconnectCh
	cm.pumpsDone = pumpsDone
	cm.connected = true
	cm.closing = false
	cm.attempt = 0
	cm.ctx = sessionCtx
	cm.cancel = sessionCancel
	cm.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		cm.readPump(conn, disconnectCh)
	}()
	go func() {
		defer wg.Done()
		cm.writePump(conn, send, sessionCtx)
	}()
	go func() {
		wg.Wait()
		close(pumpsDone)
	}()

	if cm.callbacks.onConnect != nil {
		cm.callbacks.onConnect()
	}

	cm.logger.Info("connected", "url", cm.serverURL)
	return nil
}

// SendPackage marshals a protocol.Package and enqueues it for asynchronous
// delivery. Package.Version is always set to 1 before marshalling. If the send
// channel is full the message is dropped with a warning log, mirroring the
// server's non-blocking Send pattern.
func (cm *connectionManager) SendPackage(pkg *protocol.Package) error {
	pkg.Version = 1

	data, err := marshalPackage(pkg)
	if err != nil {
		return fmt.Errorf("marshal package: %w", err)
	}

	cm.mu.Lock()
	if cm.closing || !cm.connected || cm.send == nil {
		cm.mu.Unlock()
		return NewConnectionError(fmt.Errorf("not connected"))
	}
	send := cm.send
	cm.mu.Unlock()

	select {
	case send <- data:
	default:
		return NewConnectionError(fmt.Errorf("send buffer full"))
	}
	return nil
}

// Reconnect closes the current connection and establishes a new one with
// exponential backoff. It respects maxRetries (0 means unlimited). On success
// the attempt counter is reset and the onConnect callback is invoked.
func (cm *connectionManager) Reconnect(ctx context.Context) error {
	cm.mu.Lock()
	if cm.maxRetries > 0 && cm.attempt >= cm.maxRetries {
		cm.mu.Unlock()
		return NewConnectionError(fmt.Errorf("max reconnect retries (%d) exceeded", cm.maxRetries))
	}
	cm.attempt++
	attempt := cm.attempt
	oldCancel := cm.cancel
	oldConn := cm.conn
	oldPumpsDone := cm.pumpsDone
	cm.mu.Unlock()

	// Shut down the previous connection session.
	if oldCancel != nil {
		oldCancel()
	}
	if oldConn != nil {
		_ = oldConn.Close()
	}
	// Wait for the old pumps to finish so we don't leak goroutines.
	if oldPumpsDone != nil {
		<-oldPumpsDone
	}

	delay := backoffDelay(attempt, cm.baseDelay, cm.maxDelay)
	cm.logger.Info("reconnecting", "attempt", attempt, "delay", delay)

	// Honour context cancellation during the backoff wait.
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}

	return cm.Connect(ctx)
}

// Close shuts down the connection manager. It is idempotent: calling Close
// more than once has no additional effect. It waits for the read/write pumps
// to exit before closing the underlying TCP connection, avoiding a concurrent
// write race between the close frame in writePump and Close itself.
func (cm *connectionManager) Close() {
	cm.mu.Lock()
	if cm.closing {
		cm.mu.Unlock()
		return
	}
	cm.closing = true
	cancel := cm.cancel
	pumpsDone := cm.pumpsDone
	conn := cm.conn
	cm.mu.Unlock()

	// 1. Cancel the session context so that writePump exits its select loop
	//    and sends the WebSocket close frame.
	if cancel != nil {
		cancel()
	}

	// 2. Wait for both pumps to finish writing before we touch the
	//    connection. This prevents a concurrent write between writePump's
	//    close-frame and our own.
	if pumpsDone != nil {
		select {
		case <-pumpsDone:
		case <-time.After(3 * time.Second):
		}
	}

	// 3. Now it is safe to close the underlying TCP connection.
	if conn != nil {
		_ = conn.Close()
	}

	cm.logger.Info("connection manager closed")
}

// IsConnected reports whether a WebSocket connection is currently active.
func (cm *connectionManager) IsConnected() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.connected
}

// Attempt returns the current reconnect attempt counter in a thread-safe manner.
func (cm *connectionManager) Attempt() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.attempt
}

// DeviceID returns the device identifier used by this connection manager.
func (cm *connectionManager) DeviceID() string {
	return cm.deviceID
}

// Disconnected returns a receive-only channel that is closed when an unexpected
// disconnection occurs. The connectionMonitor in client.go selects on this
// channel to trigger automatic reconnection. A new channel is created after
// each successful (re)connect.
func (cm *connectionManager) Disconnected() <-chan struct{} {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.disconnectCh
}

// ---------------------------------------------------------------------------
// Read pump
// ---------------------------------------------------------------------------

// readPump reads messages from the WebSocket connection, decodes them into
// protocol.Package values, and dispatches them via the registered callbacks.
// It runs in its own goroutine and exits when the connection is closed or an
// error occurs. The conn and disconnectCh parameters are the session-specific
// instances captured at Connect time to avoid races with Reconnect.
func (cm *connectionManager) readPump(conn *websocket.Conn, disconnectCh chan struct{}) {
	defer cm.handleDisconnect(disconnectCh)

	conn.SetReadLimit(cm.maxMsgSize)
	_ = conn.SetReadDeadline(time.Now().Add(cm.pongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(cm.pongWait))
		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				4001, // device replaced (D-095)
			) {
				cm.logger.Error("read error", "error", err)
			}
			return
		}

		pkg, err := unmarshalPackage(message)
		if err != nil {
			cm.logger.Error("decode package", "error", err)
			continue
		}

		switch pkg.Type {
		case protocol.PackageTypeResponse:
			var resp protocol.PackageDataResponse
			if err := json.Unmarshal(pkg.Data, &resp); err != nil {
				cm.logger.Error("decode response data", "error", err)
				continue
			}
			if cm.callbacks.onResponse != nil {
				cm.callbacks.onResponse(&resp)
			}

		case protocol.PackageTypeUpdates:
			var updates protocol.PackageDataUpdates
			if err := json.Unmarshal(pkg.Data, &updates); err != nil {
				cm.logger.Error("decode updates data", "error", err)
				continue
			}
			if cm.callbacks.onUpdates != nil {
				cm.callbacks.onUpdates(&updates)
			}

		case protocol.PackageTypeRequest:
			var req protocol.PackageDataRequest
			if err := json.Unmarshal(pkg.Data, &req); err != nil {
				cm.logger.Error("decode server request", "error", err)
				continue
			}
			if cm.callbacks.onRequest != nil {
				cm.callbacks.onRequest(&req)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Write pump
// ---------------------------------------------------------------------------

// writePump pumps messages from the send channel to the WebSocket connection
// and sends periodic pings to keep the connection alive. It mirrors the
// server's writePump pattern with three select cases: outbound message,
// ping ticker, and context cancellation. The conn, send, and ctx parameters
// are the session-specific instances captured at Connect time.
// Each message from the send channel is written as a separate WebSocket frame
// to preserve protocol message boundaries.
func (cm *connectionManager) writePump(conn *websocket.Conn, send chan []byte, ctx context.Context) {
	ticker := time.NewTicker(cm.pingPeriod)
	defer func() {
		ticker.Stop()
		_ = conn.Close()
	}()

	for {
		select {
		case msg, ok := <-send:
			_ = conn.SetWriteDeadline(time.Now().Add(cm.writeWait))
			if !ok {
				// Send channel was closed; the client is shutting down.
				_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Write each message as its own WebSocket frame to preserve
			// protocol message boundaries.
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(cm.writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-ctx.Done():
			// Send a close frame so the peer receives a proper WebSocket close
			// handshake before we exit.
			_ = conn.SetWriteDeadline(time.Now().Add(cm.writeWait))
			_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Disconnect handling
// ---------------------------------------------------------------------------

// handleDisconnect is called (via defer) when readPump exits. It updates the
// connection state and, if the disconnection was unexpected, signals the
// connectionMonitor via the provided disconnectCh and invokes the onDisconnect
// callback. The disconnectCh parameter is the session-specific instance to
// avoid closing a channel that belongs to a newer session.
func (cm *connectionManager) handleDisconnect(disconnectCh chan struct{}) {
	cm.mu.Lock()
	wasConnected := cm.connected
	cm.connected = false
	isClosing := cm.closing
	cm.mu.Unlock()

	if wasConnected && !isClosing {
		if disconnectCh != nil {
			close(disconnectCh)
		}
		if cm.callbacks.onDisconnect != nil {
			cm.callbacks.onDisconnect()
		}
	}
}

// ---------------------------------------------------------------------------
// Package encoding helpers
// ---------------------------------------------------------------------------

// marshalPackage encodes a protocol.Package to JSON bytes.
func marshalPackage(pkg *protocol.Package) ([]byte, error) {
	return json.Marshal(pkg)
}

// unmarshalPackage decodes a protocol.Package from JSON bytes.
func unmarshalPackage(data []byte) (*protocol.Package, error) {
	var pkg protocol.Package
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

// ---------------------------------------------------------------------------
// Exponential backoff
// ---------------------------------------------------------------------------

// backoffDelay computes the delay for a given reconnect attempt using
// exponential backoff (base * 2^(attempt-1)), capped at max, with +/-25%
// random jitter. Attempt should be >= 1.
func backoffDelay(attempt int, base, max time.Duration) time.Duration {
	exp := attempt - 1
	if exp < 0 {
		exp = 0
	}
	// Guard against overflow for very large attempt numbers.
	if exp > 30 {
		exp = 30
	}
	delay := base * time.Duration(1<<uint(exp))
	if delay > max || delay <= 0 {
		delay = max
	}
	// Jitter: +/-25% of delay.
	jitterRange := delay / 2
	if jitterRange > 0 {
		jitter := time.Duration(rand.Int63n(int64(jitterRange))) - delay/4
		delay += jitter
	}
	return delay
}
