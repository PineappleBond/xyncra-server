package server

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store"
)

// --------------------------------------------------------------------------
// Sentinel errors
// --------------------------------------------------------------------------

// Standard errors returned by the server package.
var (
	// ErrServerNotStarted indicates an operation was attempted on a server
	// that has not been started.
	ErrServerNotStarted = errors.New("server: not started")

	// ErrServerAlreadyRunning indicates Start was called on a server that
	// is already running.
	ErrServerAlreadyRunning = errors.New("server: already running")

	// ErrServerStopped indicates the server has been stopped and cannot be
	// reused.
	ErrServerStopped = errors.New("server: stopped")

	// ErrConnectionNotFound indicates the requested connection does not
	// exist in the ConnectionStore.
	ErrConnectionNotFound = errors.New("server: connection not found")

	// ErrMaxConnectionsExceeded indicates that the per-user connection
	// limit (MaxConnectionsPerUser) has been reached and no further
	// connections can be added for that user.
	ErrMaxConnectionsExceeded = errors.New("server: max connections per user exceeded")
)

// --------------------------------------------------------------------------
// Server interfaces
// --------------------------------------------------------------------------

// ServerDeps provides access to the server's dependencies. It is separated
// from Server (lifecycle) following the Interface Segregation Principle so
// that consumers that only need dependency access do not depend on lifecycle
// methods.
type ServerDeps interface {
	// Store returns the Store dependency.
	Store() store.StoreAPI

	// Broker returns the message queue Broker dependency.
	Broker() mq.Broker

	// ConnectionStore returns the ConnectionStore dependency used to
	// manage client connection metadata.
	ConnectionStore() ConnectionStore
}

// Server defines the lifecycle interface for a network server that accepts
// client connections and routes messages through the store and message queue.
// It embeds ServerDeps for dependency access.
type Server interface {
	ServerDeps

	// Start launches the server and blocks until the context is cancelled
	// or an unrecoverable error occurs. Start must not be called more than
	// once on the same Server instance.
	Start(ctx context.Context) error

	// Stop initiates an immediate shutdown of the server. In-flight
	// operations may be interrupted.
	Stop()

	// GracefulStop initiates a graceful shutdown: the server stops
	// accepting new connections, waits for in-flight operations to
	// complete (subject to the provided context's deadline), and then
	// returns.
	GracefulStop(ctx context.Context) error

	// Addr returns the address the server is listening on. Returns an
	// empty string if the server has not been started.
	Addr() string
}

// --------------------------------------------------------------------------
// ServerConfig
// --------------------------------------------------------------------------

// ServerConfig holds the dependencies and configuration for creating a Server.
// It is used internally by NewBaseServerFromOptions and is also available for
// direct use when constructing a BaseServer via NewBaseServer.
type ServerConfig struct {
	// Addr is the network address to listen on (e.g. ":8080").
	Addr string

	// Store provides access to the persistent data layer.
	Store store.StoreAPI

	// Broker provides access to the asynchronous message queue.
	Broker mq.Broker

	// ConnectionStore manages active client connection metadata.
	ConnectionStore ConnectionStore
}

// Validate checks that all required fields are set.
func (c ServerConfig) Validate() error {
	if c.Store == nil {
		return fmt.Errorf("server: store is required")
	}
	if c.Broker == nil {
		return fmt.Errorf("server: broker is required")
	}
	if c.ConnectionStore == nil {
		return fmt.Errorf("server: connection store is required")
	}
	return nil
}

// --------------------------------------------------------------------------
// Functional options for Server construction
// --------------------------------------------------------------------------

// ServerOption configures a Server during construction.
type ServerOption func(*serverOptions)

type serverOptions struct {
	addr            string
	store           store.StoreAPI
	broker          mq.Broker
	connectionStore ConnectionStore
}

// WithAddr sets the listen address.
func WithAddr(addr string) ServerOption {
	return func(o *serverOptions) {
		if addr != "" {
			o.addr = addr
		}
	}
}

// WithStore sets the Store dependency.
func WithStore(s store.StoreAPI) ServerOption {
	return func(o *serverOptions) {
		o.store = s
	}
}

// WithBroker sets the message queue Broker dependency.
func WithBroker(b mq.Broker) ServerOption {
	return func(o *serverOptions) {
		o.broker = b
	}
}

// WithConnectionStore sets the ConnectionStore dependency.
func WithConnectionStore(cs ConnectionStore) ServerOption {
	return func(o *serverOptions) {
		o.connectionStore = cs
	}
}

// --------------------------------------------------------------------------
// BaseServer
// --------------------------------------------------------------------------

// BaseServer provides a concrete implementation of the Server interface with
// lifecycle management and dependency injection. Embed it in more specific
// server types (e.g. HTTP, WebSocket) to inherit the common infrastructure.
//
// The zero value is not usable; use NewBaseServerFromOptions to create an
// instance.
type BaseServer struct {
	config ServerConfig

	mu      sync.Mutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{} // closed when Start returns
}

// Ensure BaseServer implements Server at compile time.
var _ Server = (*BaseServer)(nil)

// NewBaseServer creates a BaseServer from the provided config. Returns an
// error if the config is invalid.
func NewBaseServer(cfg ServerConfig) (*BaseServer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &BaseServer{
		config: cfg,
	}, nil
}

// NewBaseServerFromOptions creates a BaseServer from functional options.
func NewBaseServerFromOptions(opts ...ServerOption) (*BaseServer, error) {
	o := &serverOptions{}
	for _, opt := range opts {
		opt(o)
	}
	cfg := ServerConfig{
		Addr:            o.addr,
		Store:           o.store,
		Broker:          o.broker,
		ConnectionStore: o.connectionStore,
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &BaseServer{
		config: cfg,
	}, nil
}

// Start launches the server and blocks until the context is cancelled or an
// unrecoverable error occurs. It returns ErrServerAlreadyRunning if called
// more than once.
func (s *BaseServer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrServerAlreadyRunning
	}

	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("server: %w", err)
	}

	// Recreate the done channel so that Start can be safely called again
	// after a previous run completed. The previous channel was closed when
	// the last Start returned; without recreating it, a subsequent Start
	// would panic on a double-close (P2-09).
	s.done = make(chan struct{})
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.running = true
	s.mu.Unlock()

	// Block until the context is cancelled.
	<-s.ctx.Done()

	s.mu.Lock()
	s.running = false
	close(s.done)
	s.mu.Unlock()

	return nil
}

// Stop initiates an immediate shutdown of the server.
func (s *BaseServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// GracefulStop initiates a graceful shutdown. It waits for the server to
// stop or the context to expire, whichever comes first.
//
// If the server has not been started, GracefulStop returns
// ErrServerNotStarted immediately instead of blocking.
func (s *BaseServer) GracefulStop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running && s.done == nil {
		// Server was never started.
		s.mu.Unlock()
		return ErrServerNotStarted
	}
	done := s.done
	s.mu.Unlock()

	s.Stop()

	// done is guaranteed non-nil here: if s.running was true then Start
	// had already set s.done before setting s.running (under the same
	// lock), so the captured done cannot be nil (A2-007).

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("server: graceful stop timed out: %w", ctx.Err())
	}
}

// Addr returns the address the server is configured to listen on.
func (s *BaseServer) Addr() string {
	return s.config.Addr
}

// Store returns the Store dependency.
func (s *BaseServer) Store() store.StoreAPI {
	return s.config.Store
}

// Broker returns the message queue Broker dependency.
func (s *BaseServer) Broker() mq.Broker {
	return s.config.Broker
}

// ConnectionStore returns the ConnectionStore dependency.
func (s *BaseServer) ConnectionStore() ConnectionStore {
	return s.config.ConnectionStore
}

// Context returns the server's running context. If the server has not been
// started, it returns context.Background() (never nil).
func (s *BaseServer) Context() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// IsRunning reports whether the server is currently running.
func (s *BaseServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
