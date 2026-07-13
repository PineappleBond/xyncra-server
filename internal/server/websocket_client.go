package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

var (
	// ErrClientClosed is returned by Send when the client connection has been closed.
	ErrClientClosed = errors.New("websocket: client closed")

	// ErrSendBufferFull is returned by Send when the send channel buffer is full.
	ErrSendBufferFull = errors.New("websocket: send buffer full")
)

// --------------------------------------------------------------------------
// Default constants
// --------------------------------------------------------------------------

const (
	// defaultWriteWait is the time allowed to write a message to the peer.
	defaultWriteWait = 10 * time.Second

	// defaultPongWait is the time allowed to read the next pong message from
	// the peer. The connection must receive a pong within this interval to
	// stay alive.
	defaultPongWait = 60 * time.Second

	// defaultPingPeriod is the interval at which the server sends pings to
	// the peer. Must be less than defaultPongWait.
	defaultPingPeriod = (defaultPongWait * 9) / 10

	// defaultSendBufSize is the buffer size of the per-client send channel.
	defaultSendBufSize = 256

	// defaultMaxMessageSize is the maximum size of an incoming message in
	// bytes.
	defaultMaxMessageSize = 64 * 1024 // 64 KiB
)

// --------------------------------------------------------------------------
// Client
// --------------------------------------------------------------------------

// Client represents a single WebSocket client connection. Each Client owns a
// read goroutine and a write goroutine that communicate with the peer through
// the underlying WebSocket connection. Messages received by the read goroutine
// are decoded into protocol.Package and dispatched via the MessageHandler.
//
// The zero value is not usable; use NewClient to create an instance.
type Client struct {
	// conn is the underlying WebSocket connection.
	conn *websocket.Conn

	// userID is the authenticated user that owns this connection.
	userID string

	// deviceID is the identifier of the device this connection belongs to.
	deviceID string

	// connID is the unique identifier for this connection, used for
	// registration in the ConnectionStore.
	connID string

	// send is a buffered channel of outbound messages encoded as JSON bytes.
	send chan []byte

	// handler is invoked for every incoming Package. It may be nil, in which
	// case incoming messages are silently discarded.
	handler MessageHandler

	// mu protects closed.
	mu sync.Mutex

	// closed indicates whether the client has been shut down.
	closed bool

	// writeWait overrides defaultWriteWait when non-zero.
	writeWait time.Duration

	// pongWait overrides defaultPongWait when non-zero.
	pongWait time.Duration

	// pingPeriod overrides defaultPingPeriod when non-zero.
	pingPeriod time.Duration

	// maxMessageSize overrides defaultMaxMessageSize when non-zero.
	maxMessageSize int64

	// ctx is the cancellation context for this client.
	ctx context.Context

	// cancel cancels the client context.
	cancel context.CancelFunc

	// done is closed when both read and write goroutines have exited.
	done chan struct{}
}

// ClientOption configures a Client during construction.
type ClientOption func(*Client)

// WithWriteWait sets the write deadline for outgoing messages.
func WithWriteWait(d time.Duration) ClientOption {
	return func(c *Client) { c.writeWait = d }
}

// WithPongWait sets the deadline for receiving a pong reply.
func WithPongWait(d time.Duration) ClientOption {
	return func(c *Client) { c.pongWait = d }
}

// WithPingPeriod sets the interval between server-initiated pings.
func WithPingPeriod(d time.Duration) ClientOption {
	return func(c *Client) { c.pingPeriod = d }
}

// WithMaxMessageSize sets the maximum allowed incoming message size in bytes.
func WithMaxMessageSize(n int64) ClientOption {
	return func(c *Client) { c.maxMessageSize = n }
}

// WithSendBufSize sets the buffer size of the send channel.
func WithSendBufSize(n int) ClientOption {
	return func(c *Client) {
		if n > 0 {
			c.send = make(chan []byte, n)
		}
	}
}

// WithMessageHandler sets the handler for incoming messages.
func WithMessageHandler(h MessageHandler) ClientOption {
	return func(c *Client) { c.handler = h }
}

// NewClient creates a Client wrapping the given WebSocket connection. The
// caller must call Run to start the read/write goroutines and Close to clean
// up when the client is no longer needed.
func NewClient(conn *websocket.Conn, userID, deviceID, connID string, opts ...ClientOption) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		conn:           conn,
		userID:         userID,
		deviceID:       deviceID,
		connID:         connID,
		send:           make(chan []byte, defaultSendBufSize),
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
		writeWait:      defaultWriteWait,
		pongWait:       defaultPongWait,
		pingPeriod:     defaultPingPeriod,
		maxMessageSize: defaultMaxMessageSize,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// UserID returns the authenticated user ID of this client.
func (c *Client) UserID() string { return c.userID }

// DeviceID returns the device identifier of this client.
func (c *Client) DeviceID() string { return c.deviceID }

// ConnID returns the unique connection identifier.
func (c *Client) ConnID() string { return c.connID }

// Send enqueues a message for sending. It returns ErrClientClosed if the
// connection has been closed, or ErrSendBufferFull if the send buffer is full.
//
// Send may return nil even if Close is called between the closed check and
// the channel send; in that case the message is queued but discarded by
// writePump when it exits via context cancellation.
func (c *Client) Send(msg []byte) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClientClosed
	}
	c.mu.Unlock()

	select {
	case c.send <- msg:
		return nil
	default:
		return ErrSendBufferFull
	}
}

// SendPackage marshals a protocol.Package and enqueues it for delivery.
func (c *Client) SendPackage(pkg *protocol.Package) error {
	data, err := marshalPackage(pkg)
	if err != nil {
		return err
	}
	return c.Send(data)
}

// Close shuts down the client: it cancels the client context (which causes
// writePump to exit and send a close frame) and closes the underlying
// WebSocket connection. Close is idempotent.
//
// Design note: the send channel is intentionally NOT closed here. Closing it
// would race with concurrent Send calls and cause panics. Instead, writePump
// exits via ctx cancellation, which is cleaner and avoids the send-on-closed-
// channel hazard.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.cancel()
	c.mu.Unlock()

	_ = c.conn.Close()
}

// Done returns a channel that is closed when both the read and write
// goroutines have exited.
func (c *Client) Done() <-chan struct{} { return c.done }

// Run starts the read and write goroutines and blocks until the client is
// closed or the context is cancelled. Run must be called at most once.
func (c *Client) Run() {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		c.writePump()
	}()

	go func() {
		defer wg.Done()
		c.readPump()
	}()

	// Wait for both goroutines to exit, then close done.
	go func() {
		wg.Wait()
		close(c.done)
	}()

	// Block until context cancellation or done.
	select {
	case <-c.ctx.Done():
		c.Close()
	case <-c.done:
	}
}

// --------------------------------------------------------------------------
// Read pump
// --------------------------------------------------------------------------

// readPump reads messages from the WebSocket connection until the connection
// is closed or an error occurs. It runs in its own goroutine started by Run.
func (c *Client) readPump() {
	defer c.Close()

	c.conn.SetReadLimit(c.maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(c.pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
			) {
				log.Printf("websocket: read error [connID=%s]: %v", c.connID, err)
			} else {
				log.Printf("websocket: client disconnected [connID=%s]: %v", c.connID, err)
			}
			return
		}

		pkg, err := unmarshalPackage(message)
		if err != nil {
			log.Printf("websocket: decode message [connID=%s]: %v", c.connID, err)
			continue
		}

		if c.handler != nil {
			c.handler.HandleMessage(c.ctx, c, pkg)
		}
	}
}

// --------------------------------------------------------------------------
// Write pump
// --------------------------------------------------------------------------

// writePump pumps messages from the send channel to the WebSocket connection.
// It also sends periodic pings to keep the connection alive. It runs in its
// own goroutine started by Run.
func (c *Client) writePump() {
	ticker := time.NewTicker(c.pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
			if !ok {
				// Send channel was closed; the server shut down the connection.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.ctx.Done():
			// Send a close frame before exiting so the peer receives a proper
			// WebSocket close handshake (P1-01).
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
	}
}

// --------------------------------------------------------------------------
// Package encoding helpers
// --------------------------------------------------------------------------

// marshalPackage encodes a protocol.Package to JSON.
func marshalPackage(pkg *protocol.Package) ([]byte, error) {
	return jsonMarshal(pkg)
}

// unmarshalPackage decodes a protocol.Package from JSON.
func unmarshalPackage(data []byte) (*protocol.Package, error) {
	var pkg protocol.Package
	if err := jsonUnmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}
