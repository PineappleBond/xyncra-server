package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// mockWSServer wraps httptest.Server with WebSocket helpers for testing.
type mockWSServer struct {
	server   *httptest.Server
	upgrader websocket.Upgrader

	mu          sync.Mutex
	conns       []*websocket.Conn
	rpcHandlers map[string]func(*protocol.PackageDataRequest) (json.RawMessage, error)

	// writeMu serializes all writes to WebSocket connections to prevent
	// concurrent writes from the readLoop (RPC responses) and test goroutines
	// (SendPackage/SendUpdates).
	writeMu sync.Mutex

	// For controlling behavior in tests.
	rejectConn bool        // when true, HTTP handler returns 500
	closeAfter int         // close connection after N received messages (0 = never)
	msgCount   int         // running count of received messages
	readCh     chan []byte // channel for routing read messages to waiters
}

// newMockWSServer creates a new mock WebSocket server. The server accepts
// WebSocket connections and dispatches incoming Request packages to registered
// RPC handlers, sending back Response packages automatically.
func newMockWSServer(t *testing.T) *mockWSServer {
	t.Helper()
	m := &mockWSServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		rpcHandlers: make(map[string]func(*protocol.PackageDataRequest) (json.RawMessage, error)),
		readCh:      make(chan []byte, 64),
	}

	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		reject := m.rejectConn
		m.mu.Unlock()
		if reject {
			http.Error(w, "rejected", http.StatusInternalServerError)
			return
		}

		conn, err := m.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		m.mu.Lock()
		m.conns = append(m.conns, conn)
		m.mu.Unlock()

		// readLoop: read packages and dispatch.
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			m.mu.Lock()
			m.msgCount++
			count := m.msgCount
			closeAfter := m.closeAfter
			m.mu.Unlock()

			// Route raw message to any ReadPackage waiter.
			select {
			case m.readCh <- message:
			default:
			}

			// Try to decode as a Package for automatic RPC dispatch.
			var pkg protocol.Package
			if err := json.Unmarshal(message, &pkg); err != nil {
				continue
			}

			if pkg.Type == protocol.PackageTypeRequest {
				var req protocol.PackageDataRequest
				if err := json.Unmarshal(pkg.Data, &req); err != nil {
					continue
				}

				m.mu.Lock()
				handler, ok := m.rpcHandlers[req.Method]
				m.mu.Unlock()

				if ok {
					data, err := handler(&req)
					code := protocol.ResponseCodeOK
					msg := "ok"
					if err != nil {
						code = protocol.ResponseCodeError
						msg = err.Error()
					}
					resp := protocol.PackageDataResponse{
						ID:   req.ID,
						Code: code,
						Msg:  msg,
						Data: data,
					}
					respData, _ := json.Marshal(resp)
					respPkg := protocol.Package{
						Version: 1,
						Type:    protocol.PackageTypeResponse,
						Data:    respData,
					}
					respBytes, _ := json.Marshal(respPkg)
					m.writeMu.Lock()
					_ = conn.WriteMessage(websocket.TextMessage, respBytes)
					m.writeMu.Unlock()
				}
			}

			if closeAfter > 0 && count >= closeAfter {
				_ = conn.Close()
				return
			}
		}
	}))

	t.Cleanup(func() { m.Close() })
	return m
}

// AcceptConnection waits for a client to connect within the given timeout.
func (m *mockWSServer) AcceptConnection(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		n := len(m.conns)
		m.mu.Unlock()
		if n > 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return websocket.ErrCloseSent // use a sentinel error for timeout
}

// ReadPackage reads one raw package message from the first connected client.
// It waits up to timeout for a message to arrive.
func (m *mockWSServer) ReadPackage(timeout time.Duration) (*protocol.Package, error) {
	select {
	case data := <-m.readCh:
		var pkg protocol.Package
		if err := json.Unmarshal(data, &pkg); err != nil {
			return nil, err
		}
		return &pkg, nil
	case <-time.After(timeout):
		return nil, websocket.ErrCloseSent
	}
}

// SendPackage sends a package to the first connected client.
func (m *mockWSServer) SendPackage(pkg *protocol.Package) error {
	data, err := json.Marshal(pkg)
	if err != nil {
		return err
	}
	m.mu.Lock()
	conns := make([]*websocket.Conn, len(m.conns))
	copy(conns, m.conns)
	m.mu.Unlock()

	if len(conns) == 0 {
		return websocket.ErrCloseSent
	}
	m.writeMu.Lock()
	err = conns[0].WriteMessage(websocket.TextMessage, data)
	m.writeMu.Unlock()
	return err
}

// SetRPCHandler registers a handler for a specific RPC method. When a Request
// package with the matching method is received, the handler is invoked and the
// result is sent back as a Response.
func (m *mockWSServer) SetRPCHandler(method string, fn func(*protocol.PackageDataRequest) (json.RawMessage, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rpcHandlers[method] = fn
}

// SendResponse sends a response package to the first connected client for the
// given request ID.
func (m *mockWSServer) SendResponse(id string, code protocol.ResponseCode, data json.RawMessage) error {
	resp := protocol.PackageDataResponse{
		ID:   id,
		Code: code,
		Msg:  "ok",
		Data: data,
	}
	respData, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	pkg := protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeResponse,
		Data:    respData,
	}
	return m.SendPackage(&pkg)
}

// SendUpdates sends a batch of updates to the first connected client.
func (m *mockWSServer) SendUpdates(updates []protocol.PackageDataUpdate) error {
	updatesData, err := json.Marshal(protocol.PackageDataUpdates{Updates: updates})
	if err != nil {
		return err
	}
	pkg := protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeUpdates,
		Data:    updatesData,
	}
	return m.SendPackage(&pkg)
}

// Close shuts down the mock server and closes all active connections.
func (m *mockWSServer) Close() {
	m.mu.Lock()
	conns := make([]*websocket.Conn, len(m.conns))
	copy(conns, m.conns)
	m.conns = nil
	m.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}
	m.server.Close()
}

// URL returns the WebSocket URL of the mock server. The http:// scheme is
// converted to ws:// for WebSocket dialing.
func (m *mockWSServer) URL() string {
	url := m.server.URL
	// httptest.Server uses http://, but we need ws://
	if len(url) > 7 && url[:7] == "http://" {
		return "ws://" + url[7:]
	}
	return url
}

// ConnectionCount returns the number of active connections.
func (m *mockWSServer) ConnectionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.conns)
}

// MessageCount returns the number of messages received by the server.
func (m *mockWSServer) MessageCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.msgCount
}

// RemoveClosedConnections removes closed connections from the conns array.
// This is useful after a disconnect/reconnect cycle to ensure SendPackage
// sends to the active connection rather than a stale closed one.
func (m *mockWSServer) RemoveClosedConnections() {
	m.mu.Lock()
	defer m.mu.Unlock()

	active := make([]*websocket.Conn, 0, len(m.conns))
	for _, conn := range m.conns {
		// Try to detect if the connection is closed by checking if we can
		// set a read deadline. Closed connections will return an error.
		if conn != nil {
			// Check if connection is still alive by attempting to set a deadline.
			err := conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
			if err == nil {
				active = append(active, conn)
			}
		}
	}
	m.conns = active
}

// WaitForConnectionCount waits until the number of connections equals the
// expected count or the timeout expires. Useful for waiting for reconnections.
func (m *mockWSServer) WaitForConnectionCount(expected int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		n := len(m.conns)
		m.mu.Unlock()
		if n >= expected {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %d connections, got %d", expected, m.ConnectionCount())
}
