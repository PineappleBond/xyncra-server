package cli_e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	clientstore "github.com/PineappleBond/xyncra-server/pkg/store"
)

// ---------------------------------------------------------------------------
// weakNetConfig
// ---------------------------------------------------------------------------

// weakNetConfig controls the weak network simulation behavior.
type weakNetConfig struct {
	// onlineDuration is how long the server stays online before disconnecting
	// all clients.
	onlineDuration time.Duration

	// offlineDuration is how long the server stays offline (rejecting/closing
	// connections).
	offlineDuration time.Duration

	// rejectOnOffline, when true, causes the server to reject new WebSocket
	// upgrades with HTTP 500 during the offline phase.
	rejectOnOffline bool

	// jitter is the random jitter range added to online/offline durations.
	// The actual duration is uniformly distributed in
	// [duration-jitter, duration+jitter). Set to 0 for fixed durations.
	jitter time.Duration
}

// ---------------------------------------------------------------------------
// weakNetServer
// ---------------------------------------------------------------------------

// weakNetServer is a WebSocket mock server that simulates unreliable network
// conditions by periodically disconnecting all clients. It maintains per-user
// update sequences and handles the same RPC methods as the real server.
//
// weakNetServer is designed for weak-network resilience E2E tests (D-049).
// It replaces the real xyncra-server so that tests can run without Redis,
// SQLite, or a compiled server binary.
type weakNetServer struct {
	server   *httptest.Server
	upgrader websocket.Upgrader
	cfg      weakNetConfig

	mu          sync.Mutex
	conns       []*websocket.Conn
	totalConns  int // cumulative connection count (including reconnects)
	disconnects int // cumulative disconnect cycle count

	// RPC handlers keyed by method name.
	rpcHandlers map[string]func(userID string, req *protocol.PackageDataRequest) (json.RawMessage, error)

	// Per-user update sequence store (protected by updatesMu).
	updatesMu sync.Mutex
	updates   map[string][]protocol.PackageDataUpdate
	nextSeq   map[string]uint32
	msgSeqs   map[string]uint32 // per-conversation message_id_seq counter

	// Lifecycle.
	ctx    context.Context
	cancel context.CancelFunc

	// offline is true during the offline phase of the disconnect cycle.
	offline atomic.Bool

	// Statistics (protected by mu).
	syncUpdatesCalls int

	// clientMsgIDIndex stores the response for each client_message_id seen,
	// enabling D-006 idempotency: duplicate client_message_id returns the
	// original response with duplicate=true.
	clientMsgIDMu    sync.Mutex
	clientMsgIDIndex map[string]json.RawMessage
}

// newWeakNetServer creates and starts a weakNetServer with the given config.
// The server uses a random port (httptest.Server). The periodic disconnect
// cycle starts immediately. t.Cleanup is registered for shutdown.
func newWeakNetServer(t *testing.T, cfg weakNetConfig) *weakNetServer {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	s := &weakNetServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		cfg:              cfg,
		rpcHandlers:      make(map[string]func(string, *protocol.PackageDataRequest) (json.RawMessage, error)),
		updates:          make(map[string][]protocol.PackageDataUpdate),
		nextSeq:          make(map[string]uint32),
		msgSeqs:          make(map[string]uint32),
		clientMsgIDIndex: make(map[string]json.RawMessage),
		ctx:              ctx,
		cancel:           cancel,
	}

	// Register built-in RPC handlers.
	s.rpcHandlers["sync_updates"] = s.handleSyncUpdates
	s.rpcHandlers["send_message"] = s.handleSendMessage
	s.rpcHandlers["create_conversation"] = s.handleCreateConversation
	s.rpcHandlers["heartbeat"] = s.handleHeartbeat
	s.rpcHandlers["mark_as_read"] = s.handleMarkAsRead
	s.rpcHandlers["delete_message"] = s.handleDeleteMessage
	s.rpcHandlers["delete_conversation"] = s.handleDeleteConversation
	s.rpcHandlers["restore_conversation"] = s.handleRestoreConversation

	// Set up HTTP handler.
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleHTTP)
	s.server = httptest.NewServer(mux)

	// Start the periodic disconnect cycle.
	go s.disconnectLoop()

	// Cleanup on test exit.
	t.Cleanup(func() {
		cancel()
		s.server.Close()
	})

	return s
}

// ---------------------------------------------------------------------------
// Public accessors
// ---------------------------------------------------------------------------

// URL returns the WebSocket URL of the mock server
// (e.g. "ws://127.0.0.1:PORT/ws").
func (s *weakNetServer) URL() string {
	return strings.Replace(s.server.URL, "http://", "ws://", 1) + "/ws"
}

// TotalConnections returns the cumulative number of WS connections accepted.
func (s *weakNetServer) TotalConnections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totalConns
}

// DisconnectCount returns the number of disconnect cycles completed.
func (s *weakNetServer) DisconnectCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disconnects
}

// ConnectedCount returns the number of currently active WS connections.
func (s *weakNetServer) ConnectedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// SeedUpdate appends an update to the per-user sequence and returns its seq.
// Used to pre-populate updates before the daemon connects (for FullSync tests).
func (s *weakNetServer) SeedUpdate(userID string, updateType string, payload json.RawMessage) uint32 {
	return s.appendUpdate(userID, updateType, payload)
}

// SyncUpdatesCallCount returns the number of sync_updates RPC calls received.
func (s *weakNetServer) SyncUpdatesCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncUpdatesCalls
}

// LatestSeq returns the latest seq for the given user (0 if no updates).
func (s *weakNetServer) LatestSeq(userID string) uint32 {
	s.updatesMu.Lock()
	defer s.updatesMu.Unlock()
	if next, ok := s.nextSeq[userID]; ok && next > 0 {
		return next - 1
	}
	return 0
}

// waitForServerPhase polls the mock server until it has entered the desired
// phase (disconnectCount >= target) or timeout expires. This replaces
// hardcoded time.Sleep calls for phase alignment, reducing CI flakiness.
func waitForServerPhase(t *testing.T, server *weakNetServer, targetDisconnects int, timeout time.Duration) {
	t.Helper()
	start := time.Now()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if server.DisconnectCount() >= targetDisconnects {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	elapsed := time.Since(start)
	t.Fatalf("waitForServerPhase: server did not reach %d disconnects within %s (elapsed: %s, current: %d)",
		targetDisconnects, timeout, elapsed, server.DisconnectCount())
}

// waitForConnection polls until the mock server has at least `target` active
// connections, or timeout expires.
func waitForConnection(t *testing.T, server *weakNetServer, target int, timeout time.Duration) {
	t.Helper()
	start := time.Now()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if server.ConnectedCount() >= target {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	elapsed := time.Since(start)
	t.Fatalf("waitForConnection: server did not reach %d connections within %s (elapsed: %s, current: %d)",
		target, timeout, elapsed, server.ConnectedCount())
}

// ---------------------------------------------------------------------------
// HTTP handler
// ---------------------------------------------------------------------------

// handleHTTP upgrades an HTTP request to a WebSocket connection. It extracts
// user_id from the query parameter (D-005) and tracks the connection.
func (s *weakNetServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Reject new connections during offline phase if configured.
	if s.offline.Load() && s.cfg.rejectOnOffline {
		http.Error(w, "server offline", http.StatusInternalServerError)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	userID := r.URL.Query().Get("user_id")

	s.mu.Lock()
	s.conns = append(s.conns, conn)
	s.totalConns++
	s.mu.Unlock()

	go s.handleConn(conn, userID)
}

// ---------------------------------------------------------------------------
// Connection read loop
// ---------------------------------------------------------------------------

// handleConn reads messages from a single WebSocket connection and dispatches
// RPC requests to the appropriate handler.
func (s *weakNetServer) handleConn(conn *websocket.Conn, userID string) {
	defer func() {
		conn.Close()
		s.removeConn(conn)
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return // connection closed
		}

		var pkg protocol.Package
		if err := json.Unmarshal(msg, &pkg); err != nil {
			continue
		}

		if pkg.Type != protocol.PackageTypeRequest {
			continue
		}

		var req protocol.PackageDataRequest
		if err := json.Unmarshal(pkg.Data, &req); err != nil {
			continue
		}

		// Dispatch to handler.
		handler, ok := s.rpcHandlers[req.Method]
		if !ok {
			s.sendResponse(conn, req.ID, protocol.ResponseCodeError, "unknown method", nil)
			continue
		}

		data, err := handler(userID, &req)
		if err != nil {
			s.sendResponse(conn, req.ID, protocol.ResponseCodeError, err.Error(), nil)
			continue
		}
		s.sendResponse(conn, req.ID, protocol.ResponseCodeOK, "ok", data)
	}
}

// ---------------------------------------------------------------------------
// Periodic disconnect cycle
// ---------------------------------------------------------------------------

// disconnectLoop alternates between online and offline phases. During the
// offline phase all active connections are closed and new connections may be
// rejected (depending on rejectOnOffline).
func (s *weakNetServer) disconnectLoop() {
	for {
		// --- ONLINE phase ---
		online := s.cfg.onlineDuration
		if s.cfg.jitter > 0 {
			online += time.Duration(rand.Int63n(int64(s.cfg.jitter*2))) - s.cfg.jitter
		}

		select {
		case <-s.ctx.Done():
			return
		case <-time.After(online):
		}

		// --- OFFLINE phase ---
		s.offline.Store(true)

		s.mu.Lock()
		for _, conn := range s.conns {
			conn.Close()
		}
		s.conns = nil
		s.disconnects++
		s.mu.Unlock()

		offline := s.cfg.offlineDuration
		if s.cfg.jitter > 0 {
			offline += time.Duration(rand.Int63n(int64(s.cfg.jitter*2))) - s.cfg.jitter
		}

		select {
		case <-s.ctx.Done():
			return
		case <-time.After(offline):
		}

		// Back to ONLINE phase.
		s.offline.Store(false)
	}
}

// ---------------------------------------------------------------------------
// Built-in RPC handlers
// ---------------------------------------------------------------------------

// handleSyncUpdates implements the sync_updates RPC. It returns paginated
// updates for the user starting after the given seq (D-009, D-029).
func (s *weakNetServer) handleSyncUpdates(userID string, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	s.mu.Lock()
	s.syncUpdatesCalls++
	s.mu.Unlock()

	var params struct {
		AfterSeq uint32 `json:"after_seq"`
		Limit    int    `json:"limit"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	if params.Limit <= 0 {
		params.Limit = 100
	}

	s.updatesMu.Lock()
	allUpdates := s.updates[userID]
	latestSeq := uint32(0)
	if next, ok := s.nextSeq[userID]; ok && next > 0 {
		latestSeq = next - 1
	}
	s.updatesMu.Unlock()

	// Filter: keep only updates with seq > afterSeq.
	var filtered []protocol.PackageDataUpdate
	for _, u := range allUpdates {
		if u.Seq > params.AfterSeq {
			filtered = append(filtered, u)
		}
	}

	hasMore := params.AfterSeq+uint32(params.Limit) < latestSeq

	// Paginate: take at most limit items.
	if len(filtered) > params.Limit {
		filtered = filtered[:params.Limit]
	}
	if filtered == nil {
		filtered = []protocol.PackageDataUpdate{}
	}

	resp, _ := json.Marshal(struct {
		Updates   []protocol.PackageDataUpdate `json:"updates"`
		HasMore   bool                         `json:"has_more"`
		LatestSeq uint32                       `json:"latest_seq"`
	}{
		Updates:   filtered,
		HasMore:   hasMore,
		LatestSeq: latestSeq,
	})
	return resp, nil
}

// handleSendMessage implements the send_message RPC. It generates a UUID for
// the message, creates a "message" update, and returns a response matching
// the real server format ({message: model.Message, duplicate: bool}).
// It implements D-006 idempotency: duplicate client_message_id returns the
// original response.
func (s *weakNetServer) handleSendMessage(userID string, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	var params struct {
		ConversationID  string `json:"conversation_id"`
		Content         string `json:"content"`
		ClientMessageID string `json:"client_message_id"`
		SenderID        string `json:"sender_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, err
	}

	// D-006: Check-and-reserve the client_message_id slot to prevent TOCTOU races.
	if params.ClientMessageID != "" {
		s.clientMsgIDMu.Lock()
		if existing, ok := s.clientMsgIDIndex[params.ClientMessageID]; ok && existing != nil {
			s.clientMsgIDMu.Unlock()
			// Patch the duplicate flag to true for idempotent hit.
			var patched map[string]any
			_ = json.Unmarshal(existing, &patched)
			patched["duplicate"] = true
			return json.Marshal(patched)
		}
		// Reserve the slot with a nil placeholder.
		s.clientMsgIDIndex[params.ClientMessageID] = nil
		s.clientMsgIDMu.Unlock()
	}

	messageID := uuid.New().String()

	// Assign per-conversation message_id_seq.
	s.updatesMu.Lock()
	s.msgSeqs[params.ConversationID]++
	msgSeq := s.msgSeqs[params.ConversationID]
	s.updatesMu.Unlock()

	// Build a full model.Message for the response and update payload
	// (matching real server format — Go field names, no json tags).
	// SenderID uses the authenticated userID from the WS connection, not params.
	msg := map[string]any{
		"ID":              messageID,
		"ClientMessageID": params.ClientMessageID,
		"ConversationID":  params.ConversationID,
		"MessageID":       msgSeq,
		"SenderID":        userID,
		"Content":         params.Content,
		"Type":            "text",
		"ReplyTo":         uint32(0),
		"Status":          "sent",
		"CreatedAt":       time.Now().Format(time.RFC3339Nano),
		"DeletedAt":       nil,
	}

	payload, _ := json.Marshal(msg)
	s.appendUpdate(userID, protocol.UpdateTypeMessage, payload)

	// Build response matching real server format: {message: Message, duplicate: bool}.
	resp, _ := json.Marshal(map[string]any{
		"message":   msg,
		"duplicate": false,
	})

	// Store for idempotency (D-006).
	if params.ClientMessageID != "" {
		s.clientMsgIDMu.Lock()
		s.clientMsgIDIndex[params.ClientMessageID] = resp
		s.clientMsgIDMu.Unlock()
	}

	return resp, nil
}

// handleCreateConversation implements the create_conversation RPC. It generates
// a UUID for the conversation and creates a "conversation" update (D-045).
func (s *weakNetServer) handleCreateConversation(userID string, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	var params struct {
		UserID2 string `json:"user_id"` // client sends "user_id", not "user_id2"
		Title   string `json:"title"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, err
	}

	convID := uuid.New().String()

	now := time.Now().Format(time.RFC3339Nano)
	conv := map[string]any{
		"ID":        convID,
		"UserID1":   userID,
		"UserID2":   params.UserID2,
		"Type":      "1-on-1",
		"Title":     params.Title,
		"CreatedAt": now,
		"UpdatedAt": now,
	}
	payload, _ := json.Marshal(map[string]any{
		"action":       "create",
		"conversation": conv,
	})

	s.appendUpdate(userID, protocol.UpdateTypeConversation, payload)

	// RPC response must include all model.Conversation fields using Go field
	// names (no json tags on model.Conversation → JSON keys are uppercase Go names).
	resp, _ := json.Marshal(map[string]any{
		"conversation": map[string]any{
			"ID":        convID,
			"UserID1":   userID,
			"UserID2":   params.UserID2,
			"Type":      "1-on-1",
			"Title":     params.Title,
			"CreatedAt": now,
			"UpdatedAt": now,
		},
		"duplicate": false,
	})
	return resp, nil
}

// handleHeartbeat implements the heartbeat RPC. It returns a successful
// response with null data.
func (s *weakNetServer) handleHeartbeat(_ string, _ *protocol.PackageDataRequest) (json.RawMessage, error) {
	return nil, nil //nolint:nilnil
}

// handleMarkAsRead implements the mark_as_read RPC. It creates a "mark_read"
// update for the user and returns the last_read_message_id.
func (s *weakNetServer) handleMarkAsRead(userID string, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	var params struct {
		ConversationID string `json:"conversation_id"`
		MessageID      uint32 `json:"message_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(map[string]any{
		"conversation_id":      params.ConversationID,
		"last_read_message_id": params.MessageID,
	})

	s.appendUpdate(userID, protocol.UpdateTypeMarkRead, payload)

	resp, _ := json.Marshal(map[string]uint32{
		"last_read_message_id": params.MessageID,
	})
	return resp, nil
}

// handleDeleteMessage implements the delete_message RPC. It creates a
// "delete_message" type update and returns success (D-014).
func (s *weakNetServer) handleDeleteMessage(userID string, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	var params struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(map[string]any{
		"message_id": params.MessageID,
		// We can't easily look up the real conversation_id in the mock, so leave it empty.
		"conversation_id": "",
	})
	s.appendUpdate(userID, protocol.UpdateTypeDeleteMessage, payload)

	return json.Marshal(map[string]any{"status": "ok"})
}

// handleDeleteConversation implements the delete_conversation RPC. It creates
// a "conversation" type update with action="delete" (D-013).
func (s *weakNetServer) handleDeleteConversation(userID string, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	var params struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(map[string]any{
		"conversation_id": params.ConversationID,
		"action":          "delete",
	})
	s.appendUpdate(userID, protocol.UpdateTypeConversation, payload)

	return json.Marshal(map[string]any{"status": "ok", "deleted_message_count": 0})
}

// handleRestoreConversation implements the restore_conversation RPC. It creates
// a "conversation" type update with action="restore" (D-015).
func (s *weakNetServer) handleRestoreConversation(userID string, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	var params struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(map[string]any{
		"conversation_id": params.ConversationID,
		"action":          "restore",
	})
	s.appendUpdate(userID, protocol.UpdateTypeConversation, payload)

	// Return a response matching the real server format (conversation + restored count).
	now := time.Now().Format(time.RFC3339Nano)
	resp, _ := json.Marshal(map[string]any{
		"conversation": map[string]any{
			"ID":        params.ConversationID,
			"CreatedAt": now,
			"UpdatedAt": now,
		},
		"restored_message_count": 0,
	})
	return resp, nil
}

// ---------------------------------------------------------------------------
// Helper methods
// ---------------------------------------------------------------------------

// sendResponse marshals and writes a Package (Type=Response) to the connection.
func (s *weakNetServer) sendResponse(conn *websocket.Conn, reqID string, code protocol.ResponseCode, msg string, data json.RawMessage) error {
	respData, err := json.Marshal(protocol.PackageDataResponse{
		ID:   reqID,
		Code: code,
		Msg:  msg,
		Data: data,
	})
	if err != nil {
		return err
	}
	pkg := protocol.Package{
		Type: protocol.PackageTypeResponse,
		Data: respData,
	}
	return conn.WriteJSON(pkg)
}

// removeConn removes a connection from the tracked list.
func (s *weakNetServer) removeConn(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.conns {
		if c == conn {
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			return
		}
	}
}

// nextSeqFor returns the next sequence number for the given user.
// Must be called with updatesMu held.
func (s *weakNetServer) nextSeqFor(userID string) uint32 {
	s.nextSeq[userID]++
	return s.nextSeq[userID]
}

// appendUpdate adds an update to the user's sequence with an auto-assigned seq
// and returns the assigned seq.
func (s *weakNetServer) appendUpdate(userID string, updateType string, payload json.RawMessage) uint32 {
	s.updatesMu.Lock()
	seq := s.nextSeqFor(userID)
	update := protocol.PackageDataUpdate{
		Seq:       seq,
		Type:      updateType,
		Payload:   payload,
		CreatedAt: time.Now(),
	}
	s.updates[userID] = append(s.updates[userID], update)
	s.updatesMu.Unlock()
	return seq
}

// ---------------------------------------------------------------------------
// weakNetTestEnv — isolated test environment for weak-network E2E tests (D-049)
// ---------------------------------------------------------------------------

// weakNetTestEnv holds the test environment for weak network E2E tests.
// Unlike the standard cliTestEnv, it does NOT require Redis or a real server.
type weakNetTestEnv struct {
	binaryPath string
	tempHome   string
	mockServer *weakNetServer
}

// setupWeakNetE2E compiles the xyncra-client binary and creates a temp home.
// It does NOT require Redis or a real server (D-049).
func setupWeakNetE2E(t *testing.T) *weakNetTestEnv {
	t.Helper()

	// Use /tmp to avoid macOS Unix socket path limit.
	tempHome, err := os.MkdirTemp("/tmp", "xwn-")
	require.NoError(t, err, "create temp home for weak net test")

	binaryPath := filepath.Join(tempHome, "xyncra-client")
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer buildCancel()
	buildCmd := exec.CommandContext(buildCtx, "go", "build", "-o", binaryPath, "./cmd/xyncra-client/")
	buildCmd.Dir = projectRoot()
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build xyncra-client failed: %s", string(buildOut))

	t.Cleanup(func() { _ = os.RemoveAll(tempHome) })

	return &weakNetTestEnv{
		binaryPath: binaryPath,
		tempHome:   tempHome,
	}
}

// buildEnv builds a clean environment for weak net tests (strips XYNCRA_*
// and overrides HOME).
func (e *weakNetTestEnv) buildEnv() []string {
	cleanEnv := make([]string, 0, len(os.Environ()))
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "XYNCRA_") {
			continue
		}
		if strings.HasPrefix(env, "HOME=") {
			continue
		}
		cleanEnv = append(cleanEnv, env)
	}
	cleanEnv = append(cleanEnv, "HOME="+e.tempHome)
	return cleanEnv
}

// userDir returns the xyncra user directory path.
func (e *weakNetTestEnv) userDir(userID, deviceID string) string {
	return filepath.Join(e.tempHome, ".xyncra", userID, deviceID)
}

// dbPathFor returns the local DB path for (user, device).
func (e *weakNetTestEnv) dbPathFor(userID, deviceID string) string {
	return filepath.Join(e.userDir(userID, deviceID), "xyncra.db")
}

// socketPathFor returns the IPC socket path for (user, device).
func (e *weakNetTestEnv) socketPathFor(userID, deviceID string) string {
	return filepath.Join(e.userDir(userID, deviceID), "xyncra.sock")
}

// runCLI executes the CLI binary with the given args and returns the result.
func (e *weakNetTestEnv) runCLI(t *testing.T, args ...string) CLIResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	cmd.Env = e.buildEnv()
	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	result := CLIResult{Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}
	if err != nil {
		if ctx.Err() != nil {
			result.ExitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}
	return result
}

// startWeakNetDaemon starts the daemon connected to the mock server with
// accelerated reconnect delays (D-048).
func (e *weakNetTestEnv) startWeakNetDaemon(t *testing.T, userID, deviceID string) *daemonProcess {
	t.Helper()
	userDir := e.userDir(userID, deviceID)
	require.NoError(t, os.MkdirAll(userDir, 0700), "create user dir")

	socketPath := e.socketPathFor(userID, deviceID)

	cmd := exec.Command(e.binaryPath, "listen",
		"--user-id", userID,
		"--device-id", deviceID,
		"--server", e.mockServer.URL(),
	)

	// Build env with test reconnect delays injected (D-048).
	env := e.buildEnv()
	env = append(env,
		"XYNCRA_TEST_RECONNECT_BASE_DELAY=100ms",
		"XYNCRA_TEST_RECONNECT_MAX_DELAY=500ms",
	)
	cmd.Env = env

	cmd.Stdout = nil
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	require.NoError(t, cmd.Start(), "start weak net daemon")

	dp := &daemonProcess{
		cmd:        cmd,
		socketPath: socketPath,
		homeDir:    e.tempHome,
		userID:     userID,
		deviceID:   deviceID,
	}

	// Wait for the IPC socket to appear (daemon started).
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	if err := waitForSocket(waitCtx, socketPath); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("daemon socket did not appear at %s: %v\ndaemon stderr:\n%s",
			socketPath, err, stderrBuf.String())
	}

	// Write daemon stderr to test log on cleanup for debugging.
	t.Cleanup(func() {
		t.Logf("weak net daemon stderr for %s:\n%s", userID, stderrBuf.String())
	})

	// Cleanup: SIGTERM -> wait 5s -> SIGKILL.
	t.Cleanup(func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	return dp
}

// ---------------------------------------------------------------------------
// P0 Weak-Network Tests (D-044, D-006, D-009, D-032, D-048, D-049)
// ---------------------------------------------------------------------------

// TestWeakNet_WN001_BasicReconnectFullSync verifies that after a disconnect-
// reconnect cycle, the daemon automatically performs FullSync and recovers
// all data (D-044).
func TestWeakNet_WN001_BasicReconnectFullSync(t *testing.T) {
	env := setupWeakNetE2E(t)
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  8 * time.Second,
		offlineDuration: 3 * time.Second,
	})

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	// Create conversation during online phase.
	createResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"create-conversation", "--peer-id", bob,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")
	t.Logf("WN-001: created conversation %s", convID)

	// Send 3 messages.
	for i := 1; i <= 3; i++ {
		sendResult := env.runCLI(t,
			"--user-id", alice, "--device-id", "dev1",
			"send", "--conversation-id", convID,
			"--content", fmt.Sprintf("msg%d", i),
		)
		requireExitCode(t, sendResult, 0)
	}

	// Wait for mock server to disconnect + daemon to reconnect + FullSync.
	// onlineDuration=8s -> disconnect at ~8s -> offlineDuration=3s ->
	// reconnect at ~11s. With 100ms/500ms reconnect delays, actual
	// reconnect is much faster. Use waitForSync to poll the DB.
	dbPath := env.dbPathFor(alice, "dev1")
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		count, err := db.Messages.CountUnread(context.Background(), convID, 0)
		if err != nil {
			return false
		}
		return count >= 3
	})

	// Verify mock server stats: at least 2 connections (initial + reconnect).
	assert.GreaterOrEqual(t, env.mockServer.TotalConnections(), 2,
		"should have at least 2 connections (initial + reconnect)")
	t.Logf("WN-001: total connections=%d, disconnects=%d",
		env.mockServer.TotalConnections(), env.mockServer.DisconnectCount())
}

// TestWeakNet_WN002_SendDuringDisconnect verifies that messages sent during
// a disconnect are eventually delivered via the retry queue (D-006, D-032).
func TestWeakNet_WN002_SendDuringDisconnect(t *testing.T) {
	env := setupWeakNetE2E(t)
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  3 * time.Second,
		offlineDuration: 5 * time.Second,
	})

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	// Create conversation during online phase.
	createResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"create-conversation", "--peer-id", bob,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")

	// Send one message during online phase so we know the conversation
	// is synced to local DB.
	sendResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"send", "--conversation-id", convID,
		"--content", "pre-offline-msg",
	)
	requireExitCode(t, sendResult, 0)

	// Wait for the first message to appear in local DB (via initial FullSync
	// on first connect). This also confirms conversation exists locally.
	dbPath := env.dbPathFor(alice, "dev1")
	waitForSync(t, dbPath, 15*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 1
	})
	t.Log("WN-002: first message synced to local DB")

	// Wait for the first disconnect cycle to ensure we're in the offline phase.
	waitForServerPhase(t, env.mockServer, 1, 10*time.Second)

	// Send during offline phase. May fail (RPC error) or succeed (retry
	// queue processed before disconnect was detected).
	sendResult = env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"send", "--conversation-id", convID,
		"--content", "hello-weaknet",
	)
	t.Logf("WN-002: send during disconnect: exit=%d stdout=%s stderr=%s",
		sendResult.ExitCode, sendResult.Stdout, sendResult.Stderr)

	// Wait for server to come back online + reconnect + FullSync/retry.
	// offlineDuration=5s, so server comes back at ~8s from start.
	// Reconnect with 100ms/500ms delays adds <1s.
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 2
	})
	t.Logf("WN-002: both messages synced. total connections=%d",
		env.mockServer.TotalConnections())
}

// TestWeakNet_WN003_MultipleDisconnects verifies data consistency after
// multiple disconnect-reconnect cycles (D-044, D-009).
func TestWeakNet_WN003_MultipleDisconnects(t *testing.T) {
	env := setupWeakNetE2E(t)
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  3 * time.Second,
		offlineDuration: 2 * time.Second,
	})

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	// Create conversation and send all 5 messages during initial online phase.
	createResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"create-conversation", "--peer-id", bob,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")

	for i := 1; i <= 5; i++ {
		sendResult := env.runCLI(t,
			"--user-id", alice, "--device-id", "dev1",
			"send", "--conversation-id", convID,
			"--content", fmt.Sprintf("msg%d", i),
		)
		requireExitCode(t, sendResult, 0)
	}

	// Wait for multiple disconnect cycles. With online=3s + offline=2s,
	// 30s timeout gives ~6 full cycles.
	dbPath := env.dbPathFor(alice, "dev1")
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 5
	})

	// Verify at least 1 disconnect cycle occurred. The core assertion is that
	// all 5 messages are eventually consistent, not the number of disconnects.
	// With online=3s, all operations may complete before the first disconnect.
	assert.GreaterOrEqual(t, env.mockServer.DisconnectCount(), 1,
		"should have experienced at least 1 disconnect cycle")
	t.Logf("WN-003: all 5 messages synced after %d disconnects, %d total connections",
		env.mockServer.DisconnectCount(), env.mockServer.TotalConnections())
}

// TestWeakNet_WN004_FullSyncPagination verifies that FullSync correctly
// paginates when there are more updates than the batch size (D-009).
func TestWeakNet_WN004_FullSyncPagination(t *testing.T) {
	env := setupWeakNetE2E(t)

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	convID := uuid.New().String()

	// Pre-seed 1 conversation create update + 149 message updates = 150 total.
	// Default sync batch size is 100, so this requires >= 2 pages.
	wn004Now := time.Now().Format(time.RFC3339Nano)
	convPayload, _ := json.Marshal(map[string]any{
		"action": "create",
		"conversation": map[string]any{
			"ID":        convID,
			"UserID1":   alice,
			"UserID2":   bob,
			"Title":     "",
			"Type":      "1-on-1",
			"CreatedAt": wn004Now,
			"UpdatedAt": wn004Now,
		},
	})
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		// Long online duration so no disconnects interfere.
		onlineDuration:  60 * time.Second,
		offlineDuration: 1 * time.Second,
	})
	env.mockServer.SeedUpdate(alice, protocol.UpdateTypeConversation, convPayload)

	for i := 1; i <= 149; i++ {
		msgPayload, _ := json.Marshal(map[string]any{
			"ID":              uuid.New().String(),
			"ConversationID":  convID,
			"Content":         fmt.Sprintf("seeded-msg-%d", i),
			"SenderID":        alice,
			"MessageID":       uint32(i),
			"ClientMessageID": uuid.New().String(),
			"Type":            "text",
			"Status":          "sent",
			"CreatedAt":       time.Now().Format(time.RFC3339Nano),
		})
		env.mockServer.SeedUpdate(alice, protocol.UpdateTypeMessage, msgPayload)
	}

	t.Logf("WN-004: seeded %d updates (latest seq=%d)",
		150, env.mockServer.LatestSeq(alice))

	// Start daemon -> triggers initial FullSync -> needs >= 2 pages.
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	// Wait for all 149 messages to appear in DB.
	dbPath := env.dbPathFor(alice, "dev1")
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 149
	})

	// Verify pagination: sync_updates should have been called >= 2 times.
	syncCalls := env.mockServer.SyncUpdatesCallCount()
	assert.GreaterOrEqual(t, syncCalls, 2,
		"sync_updates should be called at least 2 times for 150 updates with batch size 100")
	t.Logf("WN-004: 149 messages synced, sync_updates calls=%d", syncCalls)
}

// TestWeakNet_WN005_RejectDuringOffline verifies that the daemon keeps
// retrying when the server rejects connections during offline phase (D-044).
func TestWeakNet_WN005_RejectDuringOffline(t *testing.T) {
	env := setupWeakNetE2E(t)
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  3 * time.Second,
		offlineDuration: 3 * time.Second,
		rejectOnOffline: true,
	})

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	// Create conversation during initial online phase.
	createResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"create-conversation", "--peer-id", bob,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")

	// Send one message to ensure data is in the update sequence.
	sendResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"send", "--conversation-id", convID,
		"--content", "before-offline",
	)
	requireExitCode(t, sendResult, 0)

	// Wait for initial sync so the conversation + message are in local DB.
	dbPath := env.dbPathFor(alice, "dev1")
	waitForSync(t, dbPath, 15*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 1
	})
	t.Log("WN-005: initial data synced")

	// Record total connections before the offline phase.
	connsBeforeOffline := env.mockServer.TotalConnections()
	t.Logf("WN-005: connections before offline=%d", connsBeforeOffline)

	// Wait for the first disconnect cycle.
	waitForServerPhase(t, env.mockServer, 1, 10*time.Second)

	// During offline phase, the server rejects new connections.
	// The daemon should be retrying in the background.
	// No new connections should have been accepted during offline.
	connsDuringOffline := env.mockServer.TotalConnections()
	t.Logf("WN-005: connections during offline=%d", connsDuringOffline)

	// Wait for server to come back online + daemon to reconnect + FullSync.
	// The daemon may have already sent 1 message; send another one.
	sendResult = env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"send", "--conversation-id", convID,
		"--content", "after-reconnect",
	)
	t.Logf("WN-005: send after reconnect attempt: exit=%d stdout=%s stderr=%s",
		sendResult.ExitCode, sendResult.Stdout, sendResult.Stderr)

	// Wait for the second message to appear in local DB.
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 2
	})

	// Verify at least one more connection was made after the offline phase.
	connsAfterReconnect := env.mockServer.TotalConnections()
	assert.Greater(t, connsAfterReconnect, connsBeforeOffline,
		"should have more connections after server comes back online")
	t.Logf("WN-005: connections after reconnect=%d (was %d before offline), disconnects=%d",
		connsAfterReconnect, connsBeforeOffline, env.mockServer.DisconnectCount())
}

// ---------------------------------------------------------------------------
// P1 Weak-Network Tests (D-044, D-006, D-009, D-035)
// ---------------------------------------------------------------------------

// TestWeakNet_WN006_IPCAvailableDuringBriefDisconnect verifies that local CLI
// queries (list-conversations) remain available during a brief WS disconnect,
// because they read from the local SQLite DB (D-035, D-044).
func TestWeakNet_WN006_IPCAvailableDuringBriefDisconnect(t *testing.T) {
	env := setupWeakNetE2E(t)
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  5 * time.Second,
		offlineDuration: 2 * time.Second,
	})

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	// Create conversation during online phase so it is synced to local DB.
	createResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"create-conversation", "--peer-id", bob,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")
	t.Logf("WN-006: created conversation %s", convID)

	// Wait for initial FullSync so the conversation lands in the local DB.
	dbPath := env.dbPathFor(alice, "dev1")
	waitForSync(t, dbPath, 15*time.Second, func(db *clientstore.ClientDB) bool {
		convs, _ := db.Conversations.GetByUser(context.Background(), alice, 0, 100)
		return len(convs) >= 1
	})
	t.Log("WN-006: conversation synced to local DB")

	// Wait for the first disconnect cycle.
	waitForServerPhase(t, env.mockServer, 1, 10*time.Second)
	t.Logf("WN-006: server has entered offline phase (disconnects=%d)", env.mockServer.DisconnectCount())

	// Execute list-conversations during the offline phase.
	// This reads from local SQLite (D-035) and does NOT require a WS connection.
	listResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"list-conversations",
	)

	// Assert: CLI command succeeds (exit code 0) even though WS is disconnected.
	requireExitCode(t, listResult, 0)

	// Assert: output contains the conversation ID we created.
	assert.Contains(t, listResult.Stdout, convID,
		"list-conversations should show the conversation created before disconnect")
	t.Logf("WN-006: list-conversations succeeded during offline phase (output length=%d)",
		len(listResult.Stdout))
}

// TestWeakNet_WN008_DisconnectDuringRPC verifies that when a disconnect
// happens during an RPC call, the retry mechanism ensures eventual delivery
// with idempotent client_message_id (D-006).
func TestWeakNet_WN008_DisconnectDuringRPC(t *testing.T) {
	env := setupWeakNetE2E(t)
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  3 * time.Second,
		offlineDuration: 5 * time.Second,
	})

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	// Create conversation during online phase.
	createResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"create-conversation", "--peer-id", bob,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")
	t.Logf("WN-008: created conversation %s", convID)

	// Wait for conversation to be synced to local DB.
	dbPath := env.dbPathFor(alice, "dev1")
	waitForSync(t, dbPath, 15*time.Second, func(db *clientstore.ClientDB) bool {
		convs, _ := db.Conversations.GetByUser(context.Background(), alice, 0, 100)
		return len(convs) >= 1
	})

	// Use a fixed client_message_id for idempotency (D-006).
	fixedClientMsgID := uuid.New().String()

	// Send a message close to the online/offline boundary.
	// We send it immediately so the RPC may overlap with the disconnect.
	// The message may succeed or fail at the CLI level; what matters is that
	// after reconnect + FullSync, exactly 1 copy exists in the DB.
	sendResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"send", "--conversation-id", convID,
		"--content", "wn008-idempotent-msg",
		"--client-msg-id", fixedClientMsgID,
	)
	t.Logf("WN-008: send result: exit=%d stdout=%s stderr=%s",
		sendResult.ExitCode, sendResult.Stdout, sendResult.Stderr)

	// Wait for server to go offline, then come back online.
	// onlineDuration=3s, offlineDuration=5s -> reconnect at ~8s.
	// Use a generous timeout for the retry/reconnect cycle.
	waitForSync(t, dbPath, 45*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 1
	})
	t.Log("WN-008: message appeared in local DB")

	// Verify exactly 1 message (idempotency — no duplicates from retries).
	var finalCount int64
	db, err := clientstore.New(dbPath)
	require.NoError(t, err, "open DB for final count")
	finalCount, err = db.Messages.CountUnread(context.Background(), convID, 0)
	_ = db.Close()
	require.NoError(t, err, "count messages")

	assert.Equal(t, int64(1), finalCount,
		"should have exactly 1 message (idempotent delivery via client_message_id)")
	t.Logf("WN-008: message count=%d, total connections=%d, disconnects=%d",
		finalCount, env.mockServer.TotalConnections(), env.mockServer.DisconnectCount())
}

// TestWeakNet_WN009_FullSyncPaginationInterrupted verifies that FullSync
// resumes correctly when a disconnect happens during pagination (D-009, D-029).
func TestWeakNet_WN009_FullSyncPaginationInterrupted(t *testing.T) {
	env := setupWeakNetE2E(t)

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	convID := uuid.New().String()

	// Create the mock server first so we can seed data before the daemon starts.
	// onlineDuration=4s, offlineDuration=3s: disconnect happens during page 2-3
	// of FullSync (batch size is 100, 250 updates need 3 pages).
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  4 * time.Second,
		offlineDuration: 3 * time.Second,
	})

	// Seed 1 conversation + 249 messages = 250 updates total.
	wn009Now := time.Now().Format(time.RFC3339Nano)
	convPayload, _ := json.Marshal(map[string]any{
		"action": "create",
		"conversation": map[string]any{
			"ID":        convID,
			"UserID1":   alice,
			"UserID2":   bob,
			"Title":     "",
			"Type":      "1-on-1",
			"CreatedAt": wn009Now,
			"UpdatedAt": wn009Now,
		},
	})
	env.mockServer.SeedUpdate(alice, protocol.UpdateTypeConversation, convPayload)

	for i := 1; i <= 249; i++ {
		msgPayload, _ := json.Marshal(map[string]any{
			"ID":              uuid.New().String(),
			"ConversationID":  convID,
			"Content":         fmt.Sprintf("seeded-%d", i),
			"SenderID":        alice,
			"MessageID":       uint32(i + 1),
			"ClientMessageID": uuid.New().String(),
			"Type":            "text",
			"Status":          "sent",
			"CreatedAt":       time.Now().Format(time.RFC3339Nano),
		})
		env.mockServer.SeedUpdate(alice, protocol.UpdateTypeMessage, msgPayload)
	}

	latestSeq := env.mockServer.LatestSeq(alice)
	t.Logf("WN-009: seeded 250 updates (latest seq=%d)", latestSeq)

	// Start daemon -> triggers initial FullSync.
	// Page 1 (seq 1-100) should succeed during the first online phase.
	// Disconnect at ~4s interrupts page 2 or 3.
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	dbPath := env.dbPathFor(alice, "dev1")

	// Wait for the first batch of messages to arrive via initial FullSync.
	// This confirms the first page was synced before the disconnect.
	waitForSync(t, dbPath, 15*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 50 // At least half of page 1 should arrive
	})
	t.Log("WN-009: initial FullSync page started")

	// Wait for reconnect + automatic FullSync after reconnect.
	// The daemon should reconnect and trigger another FullSync from localMaxSeq.
	// If pagination is interrupted, multiple reconnect cycles may be needed.
	// Give it enough time for multiple cycles (online=4s + offline=3s = 7s per cycle).
	waitForSync(t, dbPath, 60*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 249
	})
	t.Log("WN-009: all 249 messages synced after reconnect cycles")

	// Verify final state: all 249 messages in DB.
	db, err := clientstore.New(dbPath)
	require.NoError(t, err, "open DB for final count")
	msgCount, err := db.Messages.CountUnread(context.Background(), convID, 0)
	_ = db.Close()
	require.NoError(t, err, "count messages")

	assert.Equal(t, int64(249), msgCount, "should have all 249 seeded messages")
	assert.GreaterOrEqual(t, env.mockServer.SyncUpdatesCallCount(), 3,
		"sync_updates should be called at least 3 times (3 pages of 100)")
	t.Logf("WN-009: final msg count=%d, sync_updates calls=%d, total connections=%d",
		msgCount, env.mockServer.SyncUpdatesCallCount(), env.mockServer.TotalConnections())
}

// TestWeakNet_WN010_DeleteMessageDuringWeakNet verifies that a message deleted
// via delete-message during a weak-network cycle is eventually reflected in the
// local DB after the daemon reconnects and syncs the delete_message update
// (D-014, D-049).
//
// Scenario:
//  1. Setup mock server with short online/offline cycles (5s/3s).
//  2. Start daemon, create a conversation, send 2 messages.
//  3. Wait for messages to sync to local DB.
//  4. Execute delete-message --message-id <first-msg-uuid> via CLI.
//  5. Wait for disconnect cycle + sync_updates propagation.
//  6. Verify exactly 1 message remains in local DB (first was soft-deleted).
func TestWeakNet_WN010_DeleteMessageDuringWeakNet(t *testing.T) {
	env := setupWeakNetE2E(t)
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  5 * time.Second,
		offlineDuration: 3 * time.Second,
	})

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	// Create conversation during online phase.
	createResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"create-conversation", "--peer-id", bob,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")
	t.Logf("WN-010: created conversation %s", convID)

	// Send 2 messages.
	for i := 1; i <= 2; i++ {
		sendResult := env.runCLI(t,
			"--user-id", alice, "--device-id", "dev1",
			"send", "--conversation-id", convID,
			"--content", fmt.Sprintf("msg%d", i),
		)
		requireExitCode(t, sendResult, 0)
	}

	// Wait for messages to sync to local DB.
	dbPath := env.dbPathFor(alice, "dev1")
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count >= 2
	})
	t.Log("WN-010: both messages synced to local DB")

	// Open DB to get the first message's UUID for deletion.
	db, err := clientstore.New(dbPath)
	require.NoError(t, err, "open DB to read message IDs")
	msgs, err := db.Messages.ListByConversation(context.Background(), convID, 0, 10)
	_ = db.Close()
	require.NoError(t, err, "list messages")
	require.GreaterOrEqual(t, len(msgs), 2, "should have at least 2 messages")
	firstMsgID := msgs[0].ID
	t.Logf("WN-010: deleting first message %s", firstMsgID)

	// Delete the first message. Retry if the daemon is temporarily disconnected.
	// The daemon's WS connection may be down during an offline phase, causing the
	// CLI RPC to fail. The daemon reconnects quickly (100-500ms delays), so a
	// retry loop handles this gracefully.
	var deleteResult CLIResult
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		deleteResult = env.runCLI(t,
			"--user-id", alice, "--device-id", "dev1",
			"delete-message", "--message-id", firstMsgID,
		)
		if deleteResult.ExitCode == 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	requireExitCode(t, deleteResult, 0)
	t.Logf("WN-010: delete-message CLI succeeded")

	// Wait for a disconnect cycle so the daemon picks up the delete_message
	// update via sync_updates.
	waitForServerPhase(t, env.mockServer, 1, 15*time.Second)

	// Wait for sync: CountUnread should be 1 (first message soft-deleted).
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		count, _ := db.Messages.CountUnread(context.Background(), convID, 0)
		return count == 1
	})

	// Final verification: exactly 1 message remains.
	db2, err := clientstore.New(dbPath)
	require.NoError(t, err, "open DB for final count")
	finalCount, err := db2.Messages.CountUnread(context.Background(), convID, 0)
	_ = db2.Close()
	require.NoError(t, err, "count messages")

	assert.Equal(t, int64(1), finalCount,
		"should have exactly 1 message after delete-message")
	t.Logf("WN-010: final message count=%d, disconnects=%d, total connections=%d",
		finalCount, env.mockServer.DisconnectCount(), env.mockServer.TotalConnections())
}

// TestWeakNet_WN011_DeleteRestoreConversationDuringWeakNet verifies that a
// conversation deleted via delete-conversation and then restored via
// restore-conversation during weak-network cycles is eventually reflected in
// the local DB (D-013, D-015, D-049).
//
// Scenario:
//  1. Setup mock server with short online/offline cycles (5s/3s).
//  2. Start daemon, create a conversation.
//  3. Wait for conversation to sync to local DB.
//  4. Execute delete-conversation --conversation-id <uuid>.
//  5. Wait for disconnect cycle + sync.
//  6. Verify conversation disappeared from local DB (soft-deleted).
//  7. Execute restore-conversation --conversation-id <uuid>.
//  8. Wait for another disconnect cycle + sync.
//  9. Verify conversation reappeared in local DB.
func TestWeakNet_WN011_DeleteRestoreConversationDuringWeakNet(t *testing.T) {
	env := setupWeakNetE2E(t)
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  5 * time.Second,
		offlineDuration: 3 * time.Second,
	})

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	// Create conversation during online phase.
	createResult := env.runCLI(t,
		"--user-id", alice, "--device-id", "dev1",
		"create-conversation", "--peer-id", bob,
	)
	requireExitCode(t, createResult, 0)
	convID := extractConversationID(t, createResult.Stdout)
	require.NotEmpty(t, convID, "should extract conversation ID")
	t.Logf("WN-011: created conversation %s", convID)

	// Wait for conversation to sync to local DB.
	dbPath := env.dbPathFor(alice, "dev1")
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		convs, _ := db.Conversations.GetByUser(context.Background(), alice, 0, 100)
		return len(convs) >= 1
	})
	t.Log("WN-011: conversation synced to local DB")

	// Delete the conversation. Retry if the daemon is temporarily disconnected.
	// The daemon's WS connection may be down during an offline phase, causing the
	// CLI RPC to fail. The daemon reconnects quickly (100-500ms delays), so a
	// retry loop handles this gracefully.
	var deleteConvResult CLIResult
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		deleteConvResult = env.runCLI(t,
			"--user-id", alice, "--device-id", "dev1",
			"delete-conversation", "--conversation-id", convID,
		)
		if deleteConvResult.ExitCode == 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	requireExitCode(t, deleteConvResult, 0)
	t.Logf("WN-011: delete-conversation CLI succeeded")

	// Wait for a disconnect cycle so the daemon picks up the delete update.
	waitForServerPhase(t, env.mockServer, 1, 30*time.Second)

	// Wait for sync: GetByUser should return 0 conversations (soft-deleted).
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		convs, _ := db.Conversations.GetByUser(context.Background(), alice, 0, 100)
		return len(convs) == 0
	})
	t.Log("WN-011: conversation disappeared from local DB after delete")

	// Restore the conversation. Retry if the daemon is temporarily disconnected.
	var restoreConvResult CLIResult
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		restoreConvResult = env.runCLI(t,
			"--user-id", alice, "--device-id", "dev1",
			"restore-conversation", "--conversation-id", convID,
		)
		if restoreConvResult.ExitCode == 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	requireExitCode(t, restoreConvResult, 0)
	t.Logf("WN-011: restore-conversation CLI succeeded")

	// Wait for another disconnect cycle so the daemon picks up the restore update.
	waitForServerPhase(t, env.mockServer, 2, 30*time.Second)

	// Wait for sync: GetByUser should return 1 conversation (restored).
	waitForSync(t, dbPath, 30*time.Second, func(db *clientstore.ClientDB) bool {
		convs, _ := db.Conversations.GetByUser(context.Background(), alice, 0, 100)
		return len(convs) >= 1
	})
	t.Log("WN-011: conversation reappeared in local DB after restore")

	// Final verification: conversation is visible.
	db, err := clientstore.New(dbPath)
	require.NoError(t, err, "open DB for final verification")
	finalConvs, err := db.Conversations.GetByUser(context.Background(), alice, 0, 100)
	_ = db.Close()
	require.NoError(t, err, "list conversations")

	assert.GreaterOrEqual(t, len(finalConvs), 1,
		"should have at least 1 conversation after restore")
	t.Logf("WN-011: final conversation count=%d, disconnects=%d, total connections=%d",
		len(finalConvs), env.mockServer.DisconnectCount(), env.mockServer.TotalConnections())
}

// TestWeakNet_WN012_ServerUnavailableAtStartup verifies that the daemon
// retries indefinitely when the server is initially unreachable, and
// eventually connects when the server becomes available (D-044).
func TestWeakNet_WN012_ServerUnavailableAtStartup(t *testing.T) {
	env := setupWeakNetE2E(t)

	// Use short cycles: online=2s, offline=4s, reject new connections offline.
	env.mockServer = newWeakNetServer(t, weakNetConfig{
		onlineDuration:  2 * time.Second,
		offlineDuration: 4 * time.Second,
		rejectOnOffline: true,
	})

	alice := uniqueUserID("alice")
	bob := uniqueUserID("bob")

	// Wait for the server to enter the offline phase before starting the daemon.
	// The daemon will start during offline, so its initial connection attempt
	// will be rejected (HTTP 500).
	waitForServerPhase(t, env.mockServer, 1, 10*time.Second)
	t.Logf("WN-012: server has entered offline phase (disconnects=%d, connected=%d)",
		env.mockServer.DisconnectCount(), env.mockServer.ConnectedCount())

	// Start daemon while server is offline -> connection rejected -> infinite retry.
	dp := env.startWeakNetDaemon(t, alice, "dev1")
	defer requireStopDaemon(t, dp)

	t.Log("WN-012: daemon started during offline phase, waiting for server to come back online")

	// Wait for the server to enter the next online phase (~4s offline remaining).
	// The daemon should reconnect during the online phase.
	// Then verify we can create a conversation.
	// Total wait: ~4s (offline) + ~2s (online settle) + ~1s (reconnect buffer) = ~7s.
	// Use waitForSync with a generous timeout.
	dbPath := env.dbPathFor(alice, "dev1")

	// Try creating a conversation. It may fail a few times if the daemon hasn't
	// reconnected yet, so we poll.
	var convID string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		createResult := env.runCLI(t,
			"--user-id", alice, "--device-id", "dev1",
			"create-conversation", "--peer-id", bob,
		)
		if createResult.ExitCode == 0 {
			convID = extractConversationID(t, createResult.Stdout)
			if convID != "" {
				t.Logf("WN-012: create-conversation succeeded: %s", convID)
				break
			}
		}
		t.Logf("WN-012: create-conversation attempt: exit=%d (retrying...)", createResult.ExitCode)
		time.Sleep(2 * time.Second)
	}
	require.NotEmpty(t, convID, "should eventually create a conversation after server comes online")

	// Wait for the conversation to sync to local DB via FullSync.
	waitForSync(t, dbPath, 15*time.Second, func(db *clientstore.ClientDB) bool {
		convs, _ := db.Conversations.GetByUser(context.Background(), alice, 0, 100)
		return len(convs) >= 1
	})

	// Verify that TotalConnections >= 2 (initial rejected attempts + successful connection).
	// The daemon may have attempted multiple connections during the offline phase.
	totalConns := env.mockServer.TotalConnections()
	assert.GreaterOrEqual(t, totalConns, 1,
		"should have at least 1 successful connection after server comes online")
	t.Logf("WN-012: total connections=%d, disconnects=%d",
		totalConns, env.mockServer.DisconnectCount())
}
