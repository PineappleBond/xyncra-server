package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Sentinel errors
// --------------------------------------------------------------------------

// Standard errors returned by the WebSocket server.
var (
	// ErrWebSocketServerClosed indicates the server has been shut down and
	// is no longer accepting new connections.
	ErrWebSocketServerClosed = errors.New("websocket: server closed")

	// ErrAuthenticationFailed indicates the client could not be
	// authenticated during the WebSocket upgrade handshake.
	ErrAuthenticationFailed = errors.New("websocket: authentication failed")
)

// --------------------------------------------------------------------------
// Logger
// --------------------------------------------------------------------------

// Logger defines a minimal structured logging interface used throughout the
// server package. Implementations must be safe for concurrent use.
type Logger interface {
	// Info logs a message at informational level.
	Info(msg string, args ...any)
	// Error logs a message at error level.
	Error(msg string, args ...any)
	// Debug logs a message at debug level.
	Debug(msg string, args ...any)
}

// slogDefaultLogger wraps slog.Default() to satisfy the Logger interface.
// It is used as the fallback when no Logger option is provided.
type slogDefaultLogger struct{}

func (slogDefaultLogger) Info(msg string, args ...any)  { slog.Default().Info(msg, args...) }
func (slogDefaultLogger) Error(msg string, args ...any) { slog.Default().Error(msg, args...) }
func (slogDefaultLogger) Debug(msg string, args ...any) { slog.Default().Debug(msg, args...) }

// --------------------------------------------------------------------------
// WebSocketServerConfig
// --------------------------------------------------------------------------

// WebSocketServerConfig holds the configuration for a WebSocketServer. The
// zero value is not usable; use NewWebSocketServer with functional options.
type WebSocketServerConfig struct {
	// Path is the URL path on which the server accepts WebSocket upgrades.
	// Defaults to "/ws".
	Path string

	// Authenticate is called during the upgrade handshake to extract the
	// authenticated user ID from the HTTP request (e.g. from a JWT query
	// parameter or a cookie). If nil, AuthenticateFunc from the options is
	// used; if neither is set, a default that extracts the "user_id" query
	// parameter is used.
	Authenticate func(r *http.Request) (userID string, err error)

	// ReadBufferSize is the I/O read buffer size for the WebSocket
	// connection. Zero uses the gorilla/websocket default.
	ReadBufferSize int

	// WriteBufferSize is the I/O write buffer size for the WebSocket
	// connection. Zero uses the gorilla/websocket default.
	WriteBufferSize int

	// EnableCompression enables per-message deflate compression.
	EnableCompression bool

	// WriteWait overrides the default write deadline for outgoing messages.
	WriteWait time.Duration

	// PongWait overrides the default deadline for receiving a pong reply.
	PongWait time.Duration

	// PingPeriod overrides the default interval between server pings.
	PingPeriod time.Duration

	// MaxMessageSize overrides the default maximum incoming message size.
	MaxMessageSize int64

	// MessageHandler overrides the DefaultMessageHandler.
	MessageHandler MessageHandler
}

// --------------------------------------------------------------------------
// WebSocketServer options
// --------------------------------------------------------------------------

// WebSocketServerOption configures a WebSocketServer during construction.
type WebSocketServerOption func(*webSocketServerOptions)

// Route represents an additional HTTP route to register on the server's mux.
type Route struct {
	Pattern string
	Handler http.Handler
}

type webSocketServerOptions struct {
	// Base server options (embedded).
	addr            string
	store           store.StoreAPI
	broker          mq.Broker
	connectionStore ConnectionStore

	// WebSocket-specific options.
	path                   string
	authenticate           func(r *http.Request) (string, error)
	readBufSize            int
	writeBufSize           int
	compression            bool
	writeWait              time.Duration
	pongWait               time.Duration
	pingPeriod             time.Duration
	maxMessageSize         int64
	messageHandler         MessageHandler
	logger                 Logger
	connectionInfoEnricher func(*ConnectionInfo, *http.Request)
	nodeBroadcaster        NodeBroadcaster
	functionRegistry       FunctionRegistry
	extraRoutes            []Route      // Additional HTTP routes to register on the mux
	funcCleanupGracePeriod time.Duration // Grace period for function cleanup after disconnect
}

// WSWithAddr sets the listen address.
func WSWithAddr(addr string) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		if addr != "" {
			o.addr = addr
		}
	}
}

// WSWithConnectionStore sets the ConnectionStore dependency.
func WSWithConnectionStore(cs ConnectionStore) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		o.connectionStore = cs
	}
}

// WSWithStore sets the Store dependency.
func WSWithStore(s store.StoreAPI) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		o.store = s
	}
}

// WSWithBroker sets the message queue Broker dependency.
func WSWithBroker(b mq.Broker) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		o.broker = b
	}
}

// WSWithPath sets the WebSocket URL path.
func WSWithPath(path string) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		if path != "" {
			o.path = path
		}
	}
}

// WSWithAuthenticate sets the authentication function for WebSocket upgrades.
func WSWithAuthenticate(fn func(r *http.Request) (string, error)) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		o.authenticate = fn
	}
}

// WSWithReadBufferSize sets the I/O read buffer size.
func WSWithReadBufferSize(n int) WebSocketServerOption {
	return func(o *webSocketServerOptions) { o.readBufSize = n }
}

// WSWithWriteBufferSize sets the I/O write buffer size.
func WSWithWriteBufferSize(n int) WebSocketServerOption {
	return func(o *webSocketServerOptions) { o.writeBufSize = n }
}

// WSWithCompression enables per-message deflate compression.
func WSWithCompression() WebSocketServerOption {
	return func(o *webSocketServerOptions) { o.compression = true }
}

// WSWithWriteWait sets the write deadline for outgoing messages.
func WSWithWriteWait(d time.Duration) WebSocketServerOption {
	return func(o *webSocketServerOptions) { o.writeWait = d }
}

// WSWithPongWait sets the deadline for receiving a pong reply.
func WSWithPongWait(d time.Duration) WebSocketServerOption {
	return func(o *webSocketServerOptions) { o.pongWait = d }
}

// WSWithPingPeriod sets the interval between server-initiated pings.
func WSWithPingPeriod(d time.Duration) WebSocketServerOption {
	return func(o *webSocketServerOptions) { o.pingPeriod = d }
}

// WSWithMaxMessageSize sets the maximum allowed incoming message size.
func WSWithMaxMessageSize(n int64) WebSocketServerOption {
	return func(o *webSocketServerOptions) { o.maxMessageSize = n }
}

// WSWithMessageHandler sets the message handler for incoming packages.
func WSWithMessageHandler(h MessageHandler) WebSocketServerOption {
	return func(o *webSocketServerOptions) { o.messageHandler = h }
}

// WSWithLogger sets a custom Logger for the server. If not set, a default
// logger backed by slog.Default() is used.
func WSWithLogger(l Logger) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		if l != nil {
			o.logger = l
		}
	}
}

// WSWithConnectionInfoEnricher sets a function that is called during the
// WebSocket upgrade to populate additional fields on ConnectionInfo from the
// HTTP request (e.g. user-agent, session headers). This allows developers to
// extract custom metadata from the request before the connection is registered.
func WSWithConnectionInfoEnricher(fn func(*ConnectionInfo, *http.Request)) WebSocketServerOption {
	return func(o *webSocketServerOptions) { o.connectionInfoEnricher = fn }
}

// WSWithNodeBroadcaster sets the NodeBroadcaster for cross-node message routing.
// Default: NoopBroadcaster (single-node deployment).
func WSWithNodeBroadcaster(nb NodeBroadcaster) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		if nb != nil {
			o.nodeBroadcaster = nb
		}
	}
}

// WSWithFunctionRegistry sets the function registry for managing client-declared
// function capabilities. When not set, function registry features are disabled.
func WSWithFunctionRegistry(fr FunctionRegistry) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		o.functionRegistry = fr
	}
}

// WSWithExtraRoutes registers additional HTTP routes on the server's HTTP mux.
// This allows exposing endpoints like /metrics without the server package
// knowing about Prometheus (D-003).
func WSWithExtraRoutes(routes ...Route) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		o.extraRoutes = append(o.extraRoutes, routes...)
	}
}

// WSWithFuncCleanupGracePeriod sets the grace period before cleaning up
// functions after a device disconnects. Defaults to 10 seconds.
// Use a shorter value in tests to speed up test execution.
func WSWithFuncCleanupGracePeriod(d time.Duration) WebSocketServerOption {
	return func(o *webSocketServerOptions) {
		o.funcCleanupGracePeriod = d
	}
}

// --------------------------------------------------------------------------
// WebSocketServer
// --------------------------------------------------------------------------

// WebSocketServer is a WebSocket server that manages client connections,
// dispatches incoming messages through a configurable handler, and provides
// broadcast capabilities for pushing updates to user connections.
//
// It embeds BaseServer for lifecycle management (Start, Stop, GracefulStop)
// and dependency injection (Store, Broker, ConnectionStore).
//
// The zero value is not usable; use NewWebSocketServer to create an instance.
type WebSocketServer struct {
	*BaseServer

	// upgrader is the gorilla/websocket HTTP upgrader.
	upgrader websocket.Upgrader

	// path is the URL path for the WebSocket endpoint.
	path string

	// authenticate extracts the user ID from the HTTP request.
	authenticate func(r *http.Request) (string, error)

	// handler is the MessageHandler for incoming packages.
	handler MessageHandler

	// mu protects clients, clientsByUser, and clientsByDevice.
	mu sync.RWMutex

	// clients is the set of active client connections, keyed by connID.
	clients map[string]*Client

	// clientsByUser is a per-user index of active connections, keyed by
	// userID then connID. It enables O(1) lookup for BroadcastUpdates
	// instead of scanning all clients (P1-03).
	clientsByUser map[string]map[string]*Client

	// clientsByDevice is a per-device index of active connections, keyed by
	// "userID\x00deviceID" then connID. It enables O(1) lookup for device
	// replacement (D-093, D-095).
	clientsByDevice map[string]map[string]*Client

	// httpServer is the underlying HTTP server.
	httpServer *http.Server

	// listener is the active network listener.
	listener net.Listener

	// clientOptions are applied to every new Client.
	clientOptions []ClientOption

	// wsConfig holds the original configuration.
	wsConfig WebSocketServerConfig

	// logger is used for all server-side logging.
	logger Logger

	// connectionInfoEnricher is called during upgrade to populate extra
	// ConnectionInfo fields from the HTTP request.
	connectionInfoEnricher func(*ConnectionInfo, *http.Request)

	// nodeBroadcaster handles cross-node message routing via Pub/Sub.
	nodeBroadcaster NodeBroadcaster

	// nodeID is a unique identifier for this node, used to skip
	// self-originated messages in Pub/Sub (D-018).
	nodeID string

	// functionRegistry manages client-declared function capabilities.
	// When nil, function registry features are disabled (nil-safe per D-063).
	functionRegistry FunctionRegistry

	// extraRoutes are additional HTTP routes registered on the server's mux.
	extraRoutes []Route

	// pendingFuncCleanup tracks deferred function cleanup timers per device.
	// When a device disconnects, cleanup is deferred to allow reconnection.
	// Key: "userID\x00deviceID", Value: cancel function for the timer.
	pendingFuncCleanup map[string]func()

	// funcCleanupGracePeriod is the grace period before cleaning up functions
	// after a device disconnects. If the device reconnects within this window,
	// the cleanup is cancelled. Defaults to 10 seconds.
	funcCleanupGracePeriod time.Duration
}

// Ensure WebSocketServer implements Server at compile time.
var _ Server = (*WebSocketServer)(nil)

// NewWebSocketServer creates a WebSocketServer with the given functional
// options. The server embeds a BaseServer for lifecycle management. Call
// Start to begin accepting connections.
func NewWebSocketServer(opts ...WebSocketServerOption) (*WebSocketServer, error) {
	o := &webSocketServerOptions{
		path: "/ws",
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.connectionStore == nil {
		return nil, fmt.Errorf("websocket: connection store is required")
	}

	cfg := ServerConfig{
		Addr:            o.addr,
		Store:           o.store,
		Broker:          o.broker,
		ConnectionStore: o.connectionStore,
	}

	base, err := NewBaseServerFromOptions(
		WithAddr(cfg.Addr),
		WithStore(o.store),
		WithBroker(o.broker),
		WithConnectionStore(cfg.ConnectionStore),
	)
	if err != nil {
		return nil, fmt.Errorf("websocket: %w", err)
	}

	authenticate := o.authenticate
	if authenticate == nil {
		authenticate = defaultAuthenticate
	}

	handler := o.messageHandler
	if handler == nil {
		handler = NewDefaultMessageHandler()
	}

	var clientOpts []ClientOption
	if o.writeWait > 0 {
		clientOpts = append(clientOpts, WithWriteWait(o.writeWait))
	}
	if o.pongWait > 0 {
		clientOpts = append(clientOpts, WithPongWait(o.pongWait))
	}
	if o.pingPeriod > 0 {
		clientOpts = append(clientOpts, WithPingPeriod(o.pingPeriod))
	}
	if o.maxMessageSize > 0 {
		clientOpts = append(clientOpts, WithMaxMessageSize(o.maxMessageSize))
	}
	clientOpts = append(clientOpts, WithMessageHandler(handler))

	upgrader := websocket.Upgrader{
		ReadBufferSize:    o.readBufSize,
		WriteBufferSize:   o.writeBufSize,
		EnableCompression: o.compression,
		CheckOrigin: func(r *http.Request) bool {
			// Intentional: CORS handled by reverse proxy per PRODUCT_DECISIONS.md D-004
			return true
		},
	}

	logger := o.logger
	if logger == nil {
		logger = slogDefaultLogger{}
	}

	nodeBroadcaster := o.nodeBroadcaster
	if nodeBroadcaster == nil {
		nodeBroadcaster = &NoopBroadcaster{}
	}

	gracePeriod := o.funcCleanupGracePeriod
	if gracePeriod <= 0 {
		gracePeriod = 10 * time.Second
	}

	s := &WebSocketServer{
		BaseServer:             base,
		upgrader:               upgrader,
		path:                   o.path,
		authenticate:           authenticate,
		handler:                handler,
		clients:                make(map[string]*Client),
		clientsByUser:          make(map[string]map[string]*Client),
		clientsByDevice:        make(map[string]map[string]*Client),
		clientOptions:          clientOpts,
		logger:                 logger,
		connectionInfoEnricher: o.connectionInfoEnricher,
		nodeBroadcaster:        nodeBroadcaster,
		functionRegistry:       o.functionRegistry,
		extraRoutes:            o.extraRoutes,
		nodeID:                 uuid.New().String(),
		pendingFuncCleanup:     make(map[string]func()),
		funcCleanupGracePeriod: gracePeriod,
		wsConfig: WebSocketServerConfig{
			Path:              o.path,
			Authenticate:      authenticate,
			ReadBufferSize:    o.readBufSize,
			WriteBufferSize:   o.writeBufSize,
			EnableCompression: o.compression,
			WriteWait:         o.writeWait,
			PongWait:          o.pongWait,
			PingPeriod:        o.pingPeriod,
			MaxMessageSize:    o.maxMessageSize,
			MessageHandler:    handler,
		},
	}

	return s, nil
}

// Start launches the WebSocket server and blocks until the context is
// cancelled or an unrecoverable error occurs. It begins by binding the HTTP
// listener and then delegates to the embedded BaseServer for lifecycle
// management.
func (s *WebSocketServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.path, s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)
	for _, r := range s.extraRoutes {
		mux.Handle(r.Pattern, r.Handler)
	}

	ln, err := net.Listen("tcp", s.Addr())
	if err != nil {
		return fmt.Errorf("websocket: listen on %s: %w", s.Addr(), err)
	}
	s.mu.Lock()
	s.listener = ln
	s.httpServer = &http.Server{Handler: mux}
	s.mu.Unlock()

	// Start the HTTP server in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Start the Pub/Sub subscription for cross-node message routing (D-018).
	go func() {
		if err := s.nodeBroadcaster.Subscribe(s.Context(), s.handleRemoteBroadcast); err != nil && s.Context().Err() == nil {
			s.logger.Error("node broadcaster subscribe error", "error", err)
		}
	}()

	// Run the BaseServer lifecycle (blocks until context is cancelled).
	err = s.BaseServer.Start(ctx)

	// Shutdown the HTTP server.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if shutdownErr := s.httpServer.Shutdown(shutdownCtx); shutdownErr != nil {
		s.logger.Error("websocket: http server shutdown", "error", shutdownErr)
	}

	// Close all active clients.
	s.closeAllClients()

	// Check if the HTTP server returned an unexpected error.
	if httpErr := <-errCh; httpErr != nil && err == nil {
		err = httpErr
	}

	return err
}

// GracefulStop initiates a graceful shutdown: the server stops accepting new
// connections, waits for all active clients to disconnect (or the context to
// expire), and then returns.
func (s *WebSocketServer) GracefulStop(ctx context.Context) error {
	// Close the node broadcaster to release Pub/Sub resources.
	if s.nodeBroadcaster != nil {
		if err := s.nodeBroadcaster.Close(); err != nil {
			s.logger.Error("node broadcaster close error", "error", err)
		}
	}
	return s.BaseServer.GracefulStop(ctx)
}

// Addr returns the address the server is listening on.
func (s *WebSocketServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.BaseServer.Addr()
}

// --------------------------------------------------------------------------
// WebSocket upgrade handler
// --------------------------------------------------------------------------

// handleWebSocket handles the HTTP upgrade request and registers the resulting
// WebSocket connection.
func (s *WebSocketServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	userID, err := s.authenticate(r)
	if err != nil {
		s.logger.Error("websocket: authenticate", "error", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	if userID == "" {
		http.Error(w, "missing user id", http.StatusUnauthorized)
		return
	}

	// Extract device_id from query param (D-002/D-005).
	// If missing, auto-generate a UUID v4 (D-094).
	q := r.URL.Query()
	deviceID := q.Get("device_id")
	if deviceID == "" {
		deviceID = uuid.New().String()
		s.logger.Info("client connected without device_id, assigned UUID", "user_id", userID, "device_id", deviceID)
	}
	if len(deviceID) > 255 {
		http.Error(w, "device_id too long", http.StatusBadRequest)
		return
	}

	// Capture existing connections for the same (userID, deviceID). All
	// entries in clientsByDevice[deviceKey] are old connections that must be
	// replaced by the new one (D-095).
	deviceKey := userID + "\x00" + deviceID
	s.mu.Lock()
	oldClients := make(map[string]*Client)
	for id, c := range s.clientsByDevice[deviceKey] {
		oldClients[id] = c
	}
	s.mu.Unlock()

	// Upgrade first — zero blocking from device replacement.
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket: upgrade", "error", err)
		return
	}

	connID := uuid.New().String()

	// Create ws.connection span covering the entire connection lifecycle.
	// The span uses r.Context() as parent so it inherits HTTP transport context
	// (trace IDs propagated from reverse proxy).
	connCtx, connFinish := startConnectionSpan(r.Context(), userID, deviceID, connID)

	client := NewClient(conn, userID, deviceID, connID, append(s.clientOptions, WithContext(connCtx))...)

	// End the connection span when handleWebSocket returns. Placed immediately
	// after NewClient so that ALL exit paths (including ConnectionStore.Add
	// failure) properly close the span.
	defer connFinish(nil)

	// Atomically register the new connection and remove old connections from
	// the device index. The new connID is different from all old connIDs, so
	// subsequent removeClient calls on old connIDs cannot affect the new
	// connection (D-095).
	s.mu.Lock()
	for id := range oldClients {
		delete(s.clientsByDevice[deviceKey], id)
	}
	s.clients[connID] = client
	userClients, ok := s.clientsByUser[userID]
	if !ok {
		userClients = make(map[string]*Client)
		s.clientsByUser[userID] = userClients
	}
	userClients[connID] = client

	// Cancel any pending function cleanup for this device. This prevents
	// the race condition where functions are cleaned up during page
	// navigation while the client is reconnecting.
	s.cancelPendingFuncCleanup(userID, deviceID)
	if s.clientsByDevice[deviceKey] == nil {
		s.clientsByDevice[deviceKey] = make(map[string]*Client)
	}
	s.clientsByDevice[deviceKey][connID] = client
	s.mu.Unlock()

	// Asynchronously clean up old connections (D-095).
	// Note: this goroutine is not tracked by a WaitGroup — async cleanup is
	// best-effort and may not complete during graceful shutdown. This is
	// acceptable because the local map is the routing source of truth and
	// ConnectionStore inconsistency is transient (eventual consistency).
	if len(oldClients) > 0 {
		go s.performDeviceReplacement(oldClients, userID, deviceID)
	}

	// Register in the ConnectionStore.
	ip := extractIP(r)
	connInfo := &ConnectionInfo{
		ID:        connID,
		UserID:    userID,
		DeviceID:  deviceID,
		Protocol:  "websocket",
		IPAddress: ip,
		Status:    "active",
	}
	// Call the enricher to populate additional fields from the HTTP request.
	if s.connectionInfoEnricher != nil {
		s.connectionInfoEnricher(connInfo, r)
	}
	if addErr := s.ConnectionStore().Add(s.Context(), connInfo); addErr != nil {
		s.logger.Error("websocket: register connection", "connID", connID, "error", addErr)
		client.Close()
		s.removeClient(connID, userID, deviceID)
		return
	}

	// Run the client (blocks until the client disconnects).
	client.Run()

	// Cleanup: remove from ConnectionStore and local map. Use a bounded
	// context to avoid blocking indefinitely when Redis is unreachable
	// (P1-02).
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	if removeErr := s.ConnectionStore().Remove(cleanupCtx, connID); removeErr != nil {
		s.logger.Error("websocket: remove connection", "connID", connID, "error", removeErr)
	}
	s.removeClient(connID, userID, deviceID)

	// Clean up function registry for this device (Phase 2).
	// Use deferred cleanup with a grace period to handle reconnection
	// during page navigation. The cleanup is scheduled but can be cancelled
	// if the device reconnects within the grace period.
	if s.functionRegistry != nil {
		deviceKey := userID + "\x00" + deviceID
		s.mu.RLock()
		_, hasActiveConn := s.clientsByDevice[deviceKey]
		s.mu.RUnlock()

		if !hasActiveConn {
			s.scheduleFuncCleanup(userID, deviceID)
		}
	}

	s.logger.Info("websocket: client disconnected", "connID", connID, "userID", userID, "deviceID", deviceID)
}

// --------------------------------------------------------------------------
// Client management
// --------------------------------------------------------------------------

// removeClient removes a client from the local map, the per-user index, and
// the per-device index by connID, userID, and deviceID.
func (s *WebSocketServer) removeClient(connID, userID, deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, connID)
	if userClients, ok := s.clientsByUser[userID]; ok {
		delete(userClients, connID)
		if len(userClients) == 0 {
			delete(s.clientsByUser, userID)
		}
	}
	deviceKey := userID + "\x00" + deviceID
	if deviceClients, ok := s.clientsByDevice[deviceKey]; ok {
		delete(deviceClients, connID)
		if len(deviceClients) == 0 {
			delete(s.clientsByDevice, deviceKey)
		}
	}
}

// cancelPendingFuncCleanup cancels any pending deferred function cleanup for
// the given device. This is called when a new connection arrives for the same
// device, preventing the cleanup from removing functions that the new
// connection will re-register.
func (s *WebSocketServer) cancelPendingFuncCleanup(userID, deviceID string) {
	deviceKey := userID + "\x00" + deviceID
	if cancel, ok := s.pendingFuncCleanup[deviceKey]; ok {
		cancel()
		delete(s.pendingFuncCleanup, deviceKey)
		s.logger.Info("websocket: cancelled pending function cleanup", "userID", userID, "deviceID", deviceID)
	}
}

// scheduleFuncCleanup schedules a deferred function cleanup for the given
// device. If the device reconnects within the grace period, the cleanup is
// cancelled by cancelPendingFuncCleanup. This prevents the race condition
// where functions are removed during page navigation while the client is
// reconnecting.
func (s *WebSocketServer) scheduleFuncCleanup(userID, deviceID string) {
	deviceKey := userID + "\x00" + deviceID

	// Cancel any existing pending cleanup for this device (safety measure).
	if cancel, ok := s.pendingFuncCleanup[deviceKey]; ok {
		cancel()
	}

	// Create a cancellable context for this cleanup.
	// Use context.Background() instead of the caller's context, because
	// the caller's context (cleanupCtx from handleWebSocket) is cancelled
	// when handleWebSocket returns, which would immediately cancel the
	// deferred cleanup before the grace period expires.
	cleanupCtx, cancel := context.WithCancel(context.Background())
	s.pendingFuncCleanup[deviceKey] = cancel

	go func() {
		timer := time.NewTimer(s.funcCleanupGracePeriod)
		defer timer.Stop()

		select {
		case <-cleanupCtx.Done():
			// Cleanup was cancelled (device reconnected).
			return
		case <-timer.C:
			// Grace period expired, proceed with cleanup.
		}

		// Check if a new connection has arrived for this device.
		s.mu.RLock()
		_, hasActiveConn := s.clientsByDevice[deviceKey]
		s.mu.RUnlock()

		if hasActiveConn {
			s.logger.Info("websocket: skipping function cleanup, device reconnected", "userID", userID, "deviceID", deviceID)
			return
		}

		// Perform the cleanup.
		if s.functionRegistry != nil {
			if removed, regErr := s.functionRegistry.OnDeviceDisconnect(cleanupCtx, userID, deviceID); regErr != nil {
				s.logger.Error("websocket: clean function registry", "userID", userID, "deviceID", deviceID, "error", regErr)
			} else if removed != nil {
				s.logger.Info("websocket: cleaned functions from registry after grace period", "count", len(removed.Functions), "userID", userID, "deviceID", deviceID)
			}
		}

		// Clean up the pending entry.
		s.mu.Lock()
		delete(s.pendingFuncCleanup, deviceKey)
		s.mu.Unlock()
	}()
}

// performDeviceReplacement asynchronously handles the cleanup of old
// connections when a new connection replaces them (D-095). This runs in a
// separate goroutine so that the HTTP handler is not blocked by TCP flush
// waits, goroutine shutdown, or Redis calls.
//
// Safety: removeClient deletes by connID, and the new connection has a
// different connID, so there is no risk of removing the replacement.
func (s *WebSocketServer) performDeviceReplacement(
	oldClients map[string]*Client,
	userID, deviceID string,
) {
	for connID, oldClient := range oldClients {
		s.logger.Info("websocket: device replacement detected", "userID", userID, "deviceID", deviceID, "oldConnID", connID)

		// 1. Send 4001 close frame to the old connection.
		closeMsg := websocket.FormatCloseMessage(4001, "replaced by new connection from same device")
		if writeErr := oldClient.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(5*time.Second)); writeErr != nil {
			s.logger.Error("websocket: failed to send 4001 close frame", "oldConnID", connID, "error", writeErr)
		}

		// 2. Brief pause for TCP send buffer flush. WriteControl writes the
		//    close frame to the kernel TCP send buffer, but conn.Close() sends
		//    a TCP FIN immediately. Without this pause the FIN can race ahead
		//    of the close-frame data, causing the client to see a TCP reset
		//    instead of the 4001 close frame (D-095).
		time.Sleep(10 * time.Millisecond)

		// 3. Close the old client.
		oldClient.Close()

		// 4. Wait for old goroutines to exit (bounded).
		select {
		case <-oldClient.Done():
		case <-time.After(500 * time.Millisecond):
		}

		// 5. Clean up local indexes. removeClient is safe: it deletes by
		//    connID, and the new connection has a different connID.
		s.removeClient(connID, userID, deviceID)

		// 6. ConnectionStore.Remove is intentionally skipped here — the old
		//    connection's own handleWebSocket handler (after client.Run()
		//    returns) will remove it from ConnectionStore. This avoids a
		//    redundant Redis call.
	}

	// Note: CancelDevice is called in handleWebSocket before Upgrade (not here)
	// to avoid canceling pending requests that arrive on the new connection
	// during the async cleanup window (D-095).
}

// closeAllClients sends a close frame to every active client and waits up to
// 5 seconds for their write pumps to drain before forcefully closing
// remaining connections (P2-5).
func (s *WebSocketServer) closeAllClients() {
	// Collect client references, then reset all client indexes in a single
	// lock acquisition to avoid unnecessary lock separation.
	s.mu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.clientsByDevice = make(map[string]map[string]*Client)
	s.clientsByUser = make(map[string]map[string]*Client)
	s.clients = make(map[string]*Client)
	s.mu.Unlock()

	// First, cancel all client contexts so writePumps send close frames and
	// stop accepting new messages.
	for _, c := range clients {
		c.Close()
	}

	// Wait up to 5 seconds for all write pumps to drain.
	done := make(chan struct{})
	go func() {
		for _, c := range clients {
			<-c.Done()
		}
		close(done)
	}()
	select {
	case <-done:
		// All write pumps exited gracefully.
	case <-time.After(5 * time.Second):
		s.logger.Error("websocket: timed out waiting for clients to drain during shutdown")
	}
}

// BroadcastUpdates sends a PackageDataUpdates package to all connections
// of the given user, both local and remote (across nodes). It performs
// a local broadcast first, then publishes to Redis Pub/Sub for other
// nodes to pick up (D-018).
//
// Pub/Sub failures are logged but do not cause BroadcastUpdates to return
// an error, consistent with the fire-and-forget strategy (D-007).
func (s *WebSocketServer) BroadcastUpdates(userID string, updates *protocol.PackageDataUpdates) error {
	if updates == nil {
		return fmt.Errorf("websocket: updates is nil")
	}

	// Create handler.broadcast span (fire-and-forget: tracing errors must not
	// block business logic). Uses context.Background() as parent because
	// BroadcastUpdates has no caller context.
	bcastCtx, bcastFinish := startBroadcastSpan(context.Background(), userID)
	defer bcastFinish(nil)

	// 1. Local broadcast (existing logic).
	s.broadcastLocal(bcastCtx, userID, updates)

	// 2. Cross-node publish via Pub/Sub.
	if err := s.nodeBroadcaster.Publish(bcastCtx, userID, updates, s.nodeID); err != nil {
		s.logger.Error("cross-node broadcast failed", "userID", userID, "error", err)
		// Fire-and-forget: data is persisted, delivery via sync_updates (D-007).
	}

	return nil
}

// broadcastLocal sends a PackageDataUpdates package to all local connections
// of the given user. It uses the per-user index for O(k) lookup where k is
// the number of connections for that user.
func (s *WebSocketServer) broadcastLocal(ctx context.Context, userID string, updates *protocol.PackageDataUpdates) {
	data, err := jsonMarshal(updates)
	if err != nil {
		s.logger.Error("websocket: marshal updates for local broadcast", "error", err)
		return
	}
	pkg := &protocol.Package{
		Type: protocol.PackageTypeUpdates,
		Data: data,
	}

	s.mu.RLock()
	userClients := s.clientsByUser[userID]
	clients := make([]*Client, 0, len(userClients))
	for _, c := range userClients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()

	for _, client := range clients {
		if sendErr := client.SendPackage(pkg); sendErr != nil {
			s.logger.Error("websocket: broadcast failed", "connID", client.ConnID(), "error", sendErr)
		}
	}
}

// handleRemoteBroadcast is called when a Pub/Sub message is received from
// another node. It skips messages originated by this node (avoiding
// duplicate delivery) and performs a local broadcast.
func (s *WebSocketServer) handleRemoteBroadcast(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) {
	// Skip messages from this node (already delivered locally).
	if sourceNodeID == s.nodeID {
		return
	}
	s.broadcastLocal(context.Background(), userID, updates)
}

// ClientCount returns the number of active client connections.
func (s *WebSocketServer) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// ClientsByUser returns the number of active connections for the given user.
func (s *WebSocketServer) ClientsByUser(userID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clientsByUser[userID])
}

// MessageHandlerInstance returns the active MessageHandler.
func (s *WebSocketServer) MessageHandlerInstance() MessageHandler {
	return s.handler
}

// Logger returns the Logger instance used by the server.
func (s *WebSocketServer) Logger() Logger {
	return s.logger
}

// handleHealth responds to health check requests with the status of the
// server's dependencies. It returns HTTP 200 with a JSON payload describing
// whether the ConnectionStore is reachable (P2-8).
func (s *WebSocketServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	httpStatus := http.StatusOK

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.ConnectionStore().Ping(ctx); err != nil {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
		s.logger.Error("websocket: health check: connection store ping failed", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_, _ = fmt.Fprintf(w, `{"status":%q,"connections":%d}`, status, s.ClientCount())
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// defaultAuthenticate extracts the user ID from the "user_id" query parameter.
// This is a simple default suitable for development; production deployments
// should use JWT or cookie-based authentication.
func defaultAuthenticate(r *http.Request) (string, error) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		return "", ErrAuthenticationFailed
	}
	return userID, nil
}

// extractIP extracts the client IP address from the request, preferring
// X-Forwarded-For over RemoteAddr. When XFF contains multiple IPs
// (comma-separated), only the first (leftmost) IP is returned, as it
// represents the original client.
func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// XFF may contain a comma-separated list: "client, proxy1, proxy2".
		// The first entry is the original client IP.
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
