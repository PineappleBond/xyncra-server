package e2e_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// mockClientDevice — simulates a client device for E2E testing
// ---------------------------------------------------------------------------
//
// mockClientDevice connects to the Xyncra server via WebSocket, registers
// functions via system.register_functions, and responds to reverse RPC calls
// from the server (simulating client-side function execution).
//
// Concurrency model:
//   - connect() wraps the raw *websocket.Conn with a wsConn (channel-backed
//     reader goroutine) and starts serve() in a background goroutine.
//   - serve() consumes messages from wsConn.msgCh: PackageTypeRequest (non-
//     system.*) is dispatched to registered handlers; PackageTypeResponse is
//     forwarded to respCh for registerFunctions() to consume; other types are
//     silently discarded.
//   - Handlers map is protected by mu; expectCall* methods are safe to call
//     concurrently with serve().

// mockHandler describes how to respond to a reverse RPC call.
type mockHandler struct {
	response *protocol.PackageDataResponse
	delay    time.Duration // simulated processing delay (for timeout tests)
	hang     bool          // if true, don't respond (simulate connection loss)
}

// mockClientDevice simulates a client device that registers functions and
// responds to reverse RPC calls.
type mockClientDevice struct {
	addr     string
	userID   string
	deviceID string

	conn   *wsConn                            // channel-backed WebSocket wrapper
	done   chan struct{}                      // closed to stop serve()
	respCh chan *protocol.PackageDataResponse // responses forwarded by serve()
	inCh   chan msgResult                     // internal channel for serve() to avoid data race on conn.msgCh

	handlers map[string]*mockHandler
	mu       sync.Mutex
	writeMu  sync.Mutex // protects concurrent conn.WriteMessage calls
	reqSeq   int
}

// newMockClientDevice creates a new mock client device. The returned device
// is not yet connected; call connect() to establish the WebSocket connection.
func newMockClientDevice(t *testing.T, addr, userID, deviceID string) *mockClientDevice {
	t.Helper()
	return &mockClientDevice{
		addr:     addr,
		userID:   userID,
		deviceID: deviceID,
		done:     make(chan struct{}),
		respCh:   make(chan *protocol.PackageDataResponse, 16),
		inCh:     make(chan msgResult, 16),
		handlers: make(map[string]*mockHandler),
	}
}

// connect establishes the WebSocket connection and starts the serve()
// goroutine. The connection URL is ws://{addr}/ws?user_id={userID}&device_id={deviceID}.
func (d *mockClientDevice) connect(t *testing.T) {
	t.Helper()

	u := buildWSURL(d.addr, d.userID, d.deviceID)
	rawConn, _, err := websocket.DefaultDialer.Dial(u, nil)
	require.NoError(t, err, "mock device WebSocket dial should succeed")

	d.conn = wrapConn(rawConn)

	// Bridge: forward messages from the wsConn reader goroutine into our
	// internal channel. This decouples the reader goroutine from serve(),
	// eliminating a data race when serve() re-queues batched JSON.
	go func() {
		for r := range d.conn.msgCh {
			d.inCh <- r
		}
		close(d.inCh)
	}()

	// Start the serve goroutine that handles reverse RPC calls and forwards
	// responses to respCh.
	go d.serve()
}

// registerFunctions sends a system.register_functions RPC and waits for a
// successful response. Must be called after connect().
func (d *mockClientDevice) registerFunctions(t *testing.T, funcs []protocol.FunctionInfo) {
	t.Helper()

	d.mu.Lock()
	d.reqSeq++
	reqID := fmt.Sprintf("reg-%d", d.reqSeq)
	d.mu.Unlock()

	params := map[string]interface{}{
		"device_id":   d.deviceID,
		"device_info": map[string]string{"name": "Mock Device " + d.deviceID, "type": "desktop"},
		"functions":   funcs,
	}

	// Hold writeMu around sendRequest because sendRequest calls
	// conn.WriteMessage, which must not run concurrently with
	// handleReverseRPC's WriteMessage from the serve() goroutine.
	d.writeMu.Lock()
	sendRequest(t, d.conn, reqID, "system.register_functions", params)
	d.writeMu.Unlock()

	// Wait for the response, forwarded by serve() via respCh.
	select {
	case resp := <-d.respCh:
		require.Equal(t, reqID, resp.ID, "register_functions response ID should match")
		require.Equal(t, protocol.ResponseCodeOK, resp.Code,
			"register_functions should succeed, got code %d: %s", resp.Code, resp.Msg)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for register_functions response")
	case <-d.done:
		t.Fatal("serve() stopped while waiting for register_functions response")
	}
}

// expectCall pre-configures a response for a function call (reverse RPC).
// When the server sends a request with the given method name, the mock device
// will respond with the given response (ID is overwritten to match the request).
func (d *mockClientDevice) expectCall(t *testing.T, methodName string, response *protocol.PackageDataResponse) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[methodName] = &mockHandler{response: response}
}

// expectCallDelayed pre-configures a delayed response for timeout testing.
// The mock device will sleep for the given duration before responding.
func (d *mockClientDevice) expectCallDelayed(t *testing.T, methodName string, delay time.Duration, response *protocol.PackageDataResponse) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[methodName] = &mockHandler{response: response, delay: delay}
}

// expectCallHang pre-configures no response for a function call, simulating
// a hung or unresponsive device. The mock device will not send any reply.
func (d *mockClientDevice) expectCallHang(t *testing.T, methodName string) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[methodName] = &mockHandler{hang: true}
}

// disconnect closes the done channel (stopping serve()) and closes the
// underlying WebSocket connection.
func (d *mockClientDevice) disconnect(t *testing.T) {
	t.Helper()
	select {
	case <-d.done:
		// Already disconnected.
		return
	default:
		close(d.done)
	}
	if d.conn != nil {
		_ = d.conn.Close()
	}
}

// serve runs in a background goroutine, reading messages from the internal
// channel (inCh, bridged from wsConn) and dispatching them:
//   - PackageTypeRequest (non-system.*): looks up handler and sends response
//   - PackageTypeResponse: forwards to respCh for registerFunctions()
//   - PackageTypeUpdates: silently discarded
func (d *mockClientDevice) serve() {
	for {
		select {
		case r, ok := <-d.inCh:
			if !ok {
				return // reader goroutine exited
			}
			if r.err != nil {
				return // read error
			}

			// Handle batched JSON (same logic as readPackage).
			first, rest := firstJSON(r.data)
			if len(rest) > 0 {
				d.inCh <- msgResult{messageType: r.messageType, data: rest}
			}

			var pkg protocol.Package
			if err := json.Unmarshal(first, &pkg); err != nil {
				continue
			}

			switch pkg.Type {
			case protocol.PackageTypeRequest:
				d.handleReverseRPC(pkg.Data)

			case protocol.PackageTypeResponse:
				var resp protocol.PackageDataResponse
				if err := json.Unmarshal(pkg.Data, &resp); err != nil {
					continue
				}
				// Forward to respCh (non-blocking drop if full).
				select {
				case d.respCh <- &resp:
				default:
				}

			case protocol.PackageTypeUpdates:
				// Silently discard push updates.
			}

		case <-d.done:
			return
		}
	}
}

// handleReverseRPC processes a reverse RPC request from the server.
// Response sending (including any configured delay) runs in a separate
// goroutine so that the serve() loop is never blocked.
func (d *mockClientDevice) handleReverseRPC(data json.RawMessage) {
	var req protocol.PackageDataRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}

	// Skip system.* methods (these are server responses to our requests,
	// not reverse RPC calls).
	if strings.HasPrefix(req.Method, "system.") {
		return
	}

	d.mu.Lock()
	h, found := d.handlers[req.Method]
	d.mu.Unlock()

	if !found {
		return
	}

	// Simulate hang (no response).
	if h.hang {
		return
	}

	// Spawn a goroutine for the delay + response so that serve() is not
	// blocked while the handler sleeps (timeout tests may sleep seconds).
	go func() {
		// Simulate processing delay (for timeout tests).
		if h.delay > 0 {
			time.Sleep(h.delay)
		}

		// Build response with the same ID as the request.
		resp := *h.response
		resp.ID = req.ID
		respData, err := json.Marshal(resp)
		if err != nil {
			return
		}

		respPkg := protocol.Package{
			Type: protocol.PackageTypeResponse,
			Data: respData,
		}
		pkgData, err := json.Marshal(respPkg)
		if err != nil {
			return
		}

		// Write response under writeMu to prevent concurrent writes
		// with registerFunctions (which also writes via sendRequest).
		d.writeMu.Lock()
		_ = d.conn.WriteMessage(websocket.TextMessage, pkgData)
		d.writeMu.Unlock()
	}()
}

// buildWSURL constructs the WebSocket connection URL.
func buildWSURL(addr, userID, deviceID string) string {
	return fmt.Sprintf("ws://%s/ws?user_id=%s&device_id=%s", addr, userID, deviceID)
}
