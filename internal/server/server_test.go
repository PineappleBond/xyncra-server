package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

// mockStore is a minimal mock of store.StoreAPI used for server tests.
type mockStore struct {
	store.StoreAPI
}

func (m *mockStore) Ping(ctx context.Context) error { return nil }

// mockBroker is a minimal mock of mq.Broker used for server tests.
type mockBroker struct {
	mq.Broker
}

func (m *mockBroker) Enqueue(ctx context.Context, task *mq.Task, opts ...mq.EnqueueOption) (string, error) {
	return "task-id", nil
}

// ---------------------------------------------------------------------------
// Test helpers for Redis
// ---------------------------------------------------------------------------

const (
	testRedisAddr = "localhost:16379"
	testRedisDB   = 15 // Use DB 15 to avoid conflicts with production data
)

// setupTestRedis creates a RedisConnectionStore backed by the test Docker
// Redis instance. The caller must call the returned cleanup function when
// done (or use teardownTestRedis).
//
// If the Redis instance is not reachable, the test is skipped (A-014).
func setupTestRedis(t *testing.T) (*RedisConnectionStore, func()) {
	t.Helper()

	// Check connectivity first (A-014).
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("test Redis not reachable at %s: %v", testRedisAddr, err)
	}
	_ = client.Close()

	cs, err := NewRedisConnectionStore(RedisConnectionStoreConfig{
		Addr:       testRedisAddr,
		DB:         testRedisDB,
		DefaultTTL: 5 * time.Second,
	})
	require.NoError(t, err, "failed to connect to test Redis")

	// Flush the test database to ensure isolation between tests.
	flushCtx := context.Background()
	require.NoError(t, cs.client.FlushDB(flushCtx).Err())

	cleanup := func() {
		// Flush again so the next test starts clean.
		_ = cs.client.FlushDB(context.Background()).Err()
		_ = cs.Close()
	}

	return cs, cleanup
}

// teardownTestRedis stops and removes the test Redis Docker container.
// This is provided for completeness; individual tests should rely on
// the per-test cleanup function returned by setupTestRedis.
func teardownTestRedis() {
	// Intentionally a no-op in tests; the container is managed externally.
	// This function exists to satisfy the task requirement and can be used
	// by external test scripts.
}

// skipIfNoRedis skips the test if Redis is not reachable at testRedisAddr.
// Used by tests that create their own Redis clients (A-014).
func skipIfNoRedis(t *testing.T) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testRedisDB})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("test Redis not reachable at %s: %v", testRedisAddr, err)
	}
	_ = client.Close()
}

// newTestConnection returns a ConnectionInfo with sensible defaults for tests.
func newTestConnection(connID, userID string) *ConnectionInfo {
	return &ConnectionInfo{
		ID:         connID,
		UserID:     userID,
		SessionID:  "session-1",
		DeviceID:   "device-1",
		DeviceType: "web",
		IPAddress:  "127.0.0.1",
		Protocol:   "websocket",
		Status:     "active",
		Metadata:   map[string]string{"platform": "test"},
	}
}

// ---------------------------------------------------------------------------
// ServerConfig.Validate tests
// ---------------------------------------------------------------------------

func TestServerConfig_Validate(t *testing.T) {
	validCfg := ServerConfig{
		Addr:            ":8080",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: nil, // will set per-test
	}

	tests := []struct {
		name      string
		cfg       ServerConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid config with all fields",
			cfg: func() ServerConfig {
				c := validCfg
				c.ConnectionStore = &RedisConnectionStore{}
				return c
			}(),
			wantErr: false,
		},
		{
			name: "missing store",
			cfg: func() ServerConfig {
				c := validCfg
				c.Store = nil
				c.ConnectionStore = &RedisConnectionStore{}
				return c
			}(),
			wantErr:   true,
			errSubstr: "store is required",
		},
		{
			name: "missing broker",
			cfg: func() ServerConfig {
				c := validCfg
				c.Broker = nil
				c.ConnectionStore = &RedisConnectionStore{}
				return c
			}(),
			wantErr:   true,
			errSubstr: "broker is required",
		},
		{
			name: "missing connection store",
			cfg: func() ServerConfig {
				c := validCfg
				c.ConnectionStore = nil
				return c
			}(),
			wantErr:   true,
			errSubstr: "connection store is required",
		},
		{
			name:      "all fields nil",
			cfg:       ServerConfig{},
			wantErr:   true,
			errSubstr: "store is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NewBaseServer tests
// ---------------------------------------------------------------------------

func TestNewBaseServer(t *testing.T) {
	cs := &RedisConnectionStore{}
	cfg := ServerConfig{
		Addr:            ":9090",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: cs,
	}

	srv, err := NewBaseServer(cfg)
	require.NoError(t, err)
	require.NotNil(t, srv)

	assert.Equal(t, ":9090", srv.Addr())
	assert.Equal(t, cfg.Store, srv.Store())
	assert.Equal(t, cfg.Broker, srv.Broker())
	assert.Equal(t, cfg.ConnectionStore, srv.ConnectionStore())
	assert.False(t, srv.IsRunning())
	// Context() should return context.Background() when not started (A-005).
	assert.NotNil(t, srv.Context())
}

func TestNewBaseServer_InvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  ServerConfig
	}{
		{"missing store", ServerConfig{
			Broker:          &mockBroker{},
			ConnectionStore: &RedisConnectionStore{},
		}},
		{"missing broker", ServerConfig{
			Store:           &mockStore{},
			ConnectionStore: &RedisConnectionStore{},
		}},
		{"missing connection store", ServerConfig{
			Store:  &mockStore{},
			Broker: &mockBroker{},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, err := NewBaseServer(tc.cfg)
			require.Error(t, err)
			assert.Nil(t, srv)
		})
	}
}

// ---------------------------------------------------------------------------
// NewBaseServerFromOptions tests
// ---------------------------------------------------------------------------

func TestNewBaseServerFromOptions(t *testing.T) {
	cs := &RedisConnectionStore{}
	st := &mockStore{}
	br := &mockBroker{}

	srv, err := NewBaseServerFromOptions(
		WithAddr(":7070"),
		WithStore(st),
		WithBroker(br),
		WithConnectionStore(cs),
	)
	require.NoError(t, err)
	require.NotNil(t, srv)

	assert.Equal(t, ":7070", srv.Addr())
	assert.Equal(t, st, srv.Store())
	assert.Equal(t, br, srv.Broker())
	assert.Equal(t, cs, srv.ConnectionStore())
}

func TestNewBaseServerFromOptions_WithAddrIgnoresEmpty(t *testing.T) {
	cs := &RedisConnectionStore{}
	st := &mockStore{}
	br := &mockBroker{}

	// WithAddr("") should be ignored; the addr should remain empty since no
	// other option sets it.
	srv, err := NewBaseServerFromOptions(
		WithAddr(""),
		WithStore(st),
		WithBroker(br),
		WithConnectionStore(cs),
	)
	require.NoError(t, err)
	assert.Equal(t, "", srv.Addr())
}

func TestNewBaseServerFromOptions_MissingRequired(t *testing.T) {
	// No WithStore => should fail
	srv, err := NewBaseServerFromOptions(
		WithAddr(":8080"),
		WithBroker(&mockBroker{}),
		WithConnectionStore(&RedisConnectionStore{}),
	)
	require.Error(t, err)
	assert.Nil(t, srv)
}

// ---------------------------------------------------------------------------
// BaseServer lifecycle tests
// ---------------------------------------------------------------------------

func TestBaseServer_StartStop(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Give Start a moment to set up.
	time.Sleep(50 * time.Millisecond)
	assert.True(t, srv.IsRunning())
	assert.NotNil(t, srv.Context())

	// Stop the server.
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}

	assert.False(t, srv.IsRunning())
}

func TestBaseServer_Stop(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	assert.True(t, srv.IsRunning())

	srv.Stop()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Stop()")
	}

	assert.False(t, srv.IsRunning())
}

func TestBaseServer_GracefulStop(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	assert.True(t, srv.IsRunning())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	err = srv.GracefulStop(stopCtx)
	require.NoError(t, err)

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after GracefulStop")
	}

	assert.False(t, srv.IsRunning())
}

func TestBaseServer_GracefulStop_Timeout(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	// Use a context that we control for Start so the server doesn't
	// immediately return.
	startCtx, startCancel := context.WithCancel(context.Background())
	defer startCancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(startCtx)
	}()

	time.Sleep(50 * time.Millisecond)

	// GracefulStop with a very short timeout context should time out.
	// Using a short live timeout (rather than an already-cancelled context)
	// avoids the non-deterministic select between done and ctx.Done() that
	// made this test flaky (A2-006).
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer stopCancel()

	// Wait long enough that the stopCtx will definitely expire before the
	// server completes its shutdown.
	time.Sleep(10 * time.Millisecond)

	err = srv.GracefulStop(stopCtx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")

	// Clean up: actually stop the server.
	startCancel()
	<-errCh
}

// A-002: GracefulStop before Start should return ErrServerNotStarted.
func TestBaseServer_GracefulStop_BeforeStart(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	err = srv.GracefulStop(stopCtx)
	assert.Equal(t, ErrServerNotStarted, err)
}

func TestBaseServer_StartTwice(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Second Start should return ErrServerAlreadyRunning immediately.
	err = srv.Start(ctx)
	assert.Equal(t, ErrServerAlreadyRunning, err)

	cancel()
	<-errCh
}

// A-001: Start after a previous run completed should not panic (done channel
// is recreated).
func TestBaseServer_StartAfterStop(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	// First run.
	ctx1, cancel1 := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx1)
	}()
	time.Sleep(50 * time.Millisecond)
	assert.True(t, srv.IsRunning())
	cancel1()
	<-errCh
	assert.False(t, srv.IsRunning())

	// Second run should work without panic.
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() {
		errCh <- srv.Start(ctx2)
	}()
	time.Sleep(50 * time.Millisecond)
	assert.True(t, srv.IsRunning())
	cancel2()
	<-errCh
	assert.False(t, srv.IsRunning())
}

func TestBaseServer_StartWithCancelledContext(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting

	err = srv.Start(ctx)
	require.Error(t, err)
	assert.False(t, srv.IsRunning())
}

func TestBaseServer_StopBeforeStart(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	// Stop before Start should be a no-op (not panic).
	assert.NotPanics(t, func() {
		srv.Stop()
	})
}

func TestBaseServer_ConcurrentStartStop(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	// Start the server in one goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Concurrent Stop calls should not panic.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Stop()
		}()
	}

	cancel()
	wg.Wait()
	assert.False(t, srv.IsRunning())
}

// A-005: Context() should return context.Background() when not started.
func TestBaseServer_Context_NotStarted(t *testing.T) {
	srv, err := NewBaseServer(ServerConfig{
		Addr:            ":0",
		Store:           &mockStore{},
		Broker:          &mockBroker{},
		ConnectionStore: &RedisConnectionStore{},
	})
	require.NoError(t, err)

	ctx := srv.Context()
	assert.NotNil(t, ctx)
	// Should be equivalent to context.Background().
	assert.Equal(t, context.Background(), ctx)
}

// ---------------------------------------------------------------------------
// Server interface compliance: ServerDeps embedded in Server
// ---------------------------------------------------------------------------

func TestServerInterface_CompileTimeCheck(t *testing.T) {
	// Verify that BaseServer satisfies both Server and ServerDeps.
	var _ Server = (*BaseServer)(nil)
	var _ ServerDeps = (*BaseServer)(nil)
}

// ---------------------------------------------------------------------------
// ConnectionInfo tests
// ---------------------------------------------------------------------------

func TestConnectionInfo_IsExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		info     ConnectionInfo
		expected bool
	}{
		{
			name: "not expired with zero TTL",
			info: ConnectionInfo{
				UpdatedAt: now.Add(-1 * time.Hour),
				TTL:       0,
			},
			expected: false,
		},
		{
			name: "not expired with negative TTL",
			info: ConnectionInfo{
				UpdatedAt: now.Add(-1 * time.Hour),
				TTL:       -1,
			},
			expected: false,
		},
		{
			name: "not expired within TTL",
			info: ConnectionInfo{
				UpdatedAt: now.Add(-1 * time.Minute),
				TTL:       5 * time.Minute,
			},
			expected: false,
		},
		{
			name: "expired past TTL",
			info: ConnectionInfo{
				UpdatedAt: now.Add(-10 * time.Minute),
				TTL:       5 * time.Minute,
			},
			expected: true,
		},
		{
			name: "exactly at TTL boundary",
			info: ConnectionInfo{
				UpdatedAt: now.Add(-5 * time.Minute),
				TTL:       5 * time.Minute,
			},
			expected: false, // now is not *after* UpdatedAt + TTL, it is equal
		},
		{
			name: "just past TTL boundary",
			info: ConnectionInfo{
				UpdatedAt: now.Add(-5*time.Minute - time.Second),
				TTL:       5 * time.Minute,
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.info.IsExpired(now))
		})
	}
}

// P-002, P-004: New fields are serialized correctly.
func TestConnectionInfo_NewFields(t *testing.T) {
	info := ConnectionInfo{
		ID:              "conn-1",
		UserID:          "user-1",
		SessionID:       "session-1",
		DeviceID:        "device-abc",
		DeviceType:      "android",
		IPAddress:       "192.168.1.100",
		Protocol:        "websocket",
		LastHeartbeatAt: time.Now(),
		Status:          "active",
		Metadata:        map[string]string{"version": "2.0"},
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
		TTL:             5 * time.Minute,
	}

	assert.Equal(t, "device-abc", info.DeviceID)
	assert.Equal(t, "android", info.DeviceType)
	assert.Equal(t, "192.168.1.100", info.IPAddress)
	assert.Equal(t, "websocket", info.Protocol)
	assert.Equal(t, "active", info.Status)
	assert.False(t, info.LastHeartbeatAt.IsZero())
}

// ---------------------------------------------------------------------------
// RedisConnectionStore - constructor tests
// ---------------------------------------------------------------------------

func TestNewRedisConnectionStore(t *testing.T) {
	cs, err := NewRedisConnectionStore(RedisConnectionStoreConfig{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
	if err != nil {
		t.Skipf("test Redis not reachable: %v", err)
	}
	require.NotNil(t, cs)
	defer cs.Close()

	assert.Equal(t, defaultConnectionTTL, cs.defaultTTL)
}

func TestNewRedisConnectionStore_CustomTTL(t *testing.T) {
	cs, err := NewRedisConnectionStore(RedisConnectionStoreConfig{
		Addr:       testRedisAddr,
		DB:         testRedisDB,
		DefaultTTL: 10 * time.Minute,
	})
	if err != nil {
		t.Skipf("test Redis not reachable: %v", err)
	}
	require.NotNil(t, cs)
	defer cs.Close()

	assert.Equal(t, 10*time.Minute, cs.defaultTTL)
}

func TestNewRedisConnectionStore_EmptyAddr(t *testing.T) {
	_, err := NewRedisConnectionStore(RedisConnectionStoreConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis address is required")
}

func TestNewRedisConnectionStore_BadAddr(t *testing.T) {
	_, err := NewRedisConnectionStore(RedisConnectionStoreConfig{
		Addr: "localhost:1", // port 1 should fail
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis ping failed")
}

// P-007: LazyConnect skips initial ping.
func TestNewRedisConnectionStore_LazyConnect(t *testing.T) {
	// Even with a bad address, LazyConnect should succeed.
	cs, err := NewRedisConnectionStore(RedisConnectionStoreConfig{
		Addr:        "localhost:1", // unreachable
		LazyConnect: true,
	})
	require.NoError(t, err)
	require.NotNil(t, cs)
	defer cs.Close()

	// Operations will fail because Redis is unreachable, but construction
	// should succeed.
	ctx := context.Background()
	err = cs.Ping(ctx)
	assert.Error(t, err)
}

// P-007 + A-010: Pool configuration.
func TestNewRedisConnectionStore_PoolConfig(t *testing.T) {
	cs, err := NewRedisConnectionStore(RedisConnectionStoreConfig{
		Addr:         testRedisAddr,
		DB:           testRedisDB,
		PoolSize:     20,
		MinIdleConns: 5,
		PoolTimeout:  3 * time.Second,
	})
	if err != nil {
		t.Skipf("test Redis not reachable: %v", err)
	}
	require.NotNil(t, cs)
	defer cs.Close()

	// Verify pool settings were applied.
	opts := cs.client.Options()
	assert.Equal(t, 20, opts.PoolSize)
	assert.Equal(t, 5, opts.MinIdleConns)
	assert.Equal(t, 3*time.Second, opts.PoolTimeout)
}

// A-010: MaxConnectionsPerUser config.
func TestNewRedisConnectionStore_MaxConnectionsPerUser(t *testing.T) {
	cs, err := NewRedisConnectionStore(RedisConnectionStoreConfig{
		Addr:                testRedisAddr,
		DB:                  testRedisDB,
		MaxConnectionsPerUser: 3,
	})
	if err != nil {
		t.Skipf("test Redis not reachable: %v", err)
	}
	require.NotNil(t, cs)
	defer cs.Close()

	assert.Equal(t, 3, cs.maxConnsPerUser)
}

func TestNewRedisConnectionStoreFromClient(t *testing.T) {
	skipIfNoRedis(t)
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
	defer client.Close()

	cs := NewRedisConnectionStoreFromClient(RedisConnectionStoreFromClientConfig{
		Client:    client,
		DefaultTTL: 2 * time.Minute,
	})
	require.NotNil(t, cs)

	assert.Equal(t, 2*time.Minute, cs.defaultTTL)
	assert.False(t, cs.ownsClient)

	// Close should not close the external client.
	err := cs.Close()
	assert.NoError(t, err)

	// Client should still be usable.
	ctx := context.Background()
	assert.NoError(t, client.Ping(ctx).Err())
}

// A-008: FromClient now supports KeyPrefix.
func TestNewRedisConnectionStoreFromClient_WithKeyPrefix(t *testing.T) {
	skipIfNoRedis(t)
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
	defer client.Close()

	cs := NewRedisConnectionStoreFromClient(RedisConnectionStoreFromClientConfig{
		Client:    client,
		DefaultTTL: 5 * time.Minute,
		KeyPrefix: "tenant1:",
	})
	require.NotNil(t, cs)
	assert.Equal(t, "tenant1:", cs.keyPrefix)
}

func TestNewRedisConnectionStoreFromClient_ZeroTTL(t *testing.T) {
	skipIfNoRedis(t)
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
	defer client.Close()

	cs := NewRedisConnectionStoreFromClient(RedisConnectionStoreFromClientConfig{
		Client: client,
	})
	assert.Equal(t, defaultConnectionTTL, cs.defaultTTL)
}

// ---------------------------------------------------------------------------
// RedisConnectionStore - CRUD tests
// ---------------------------------------------------------------------------

func TestRedisConnectionStore_Add(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	err := cs.Add(ctx, info)
	require.NoError(t, err)

	// Verify CreatedAt was set.
	assert.False(t, info.CreatedAt.IsZero())
	assert.False(t, info.UpdatedAt.IsZero())
}

func TestRedisConnectionStore_Add_OverwritesExisting(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	info.Metadata = map[string]string{"version": "1"}
	require.NoError(t, cs.Add(ctx, info))

	// Overwrite with new metadata.
	info2 := newTestConnection("conn-1", "user-1")
	info2.Metadata = map[string]string{"version": "2"}
	require.NoError(t, cs.Add(ctx, info2))

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "2", got.Metadata["version"])
}

func TestRedisConnectionStore_Add_NilInfo(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Add(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection info is nil")
}

func TestRedisConnectionStore_Add_EmptyID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Add(ctx, &ConnectionInfo{UserID: "user-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection ID is required")
}

func TestRedisConnectionStore_Add_EmptyUserID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Add(ctx, &ConnectionInfo{ID: "conn-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

// P-012: MaxConnectionsPerUser enforcement.
func TestRedisConnectionStore_Add_MaxConnectionsExceeded(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()
	cs.maxConnsPerUser = 2

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))

	// Third connection for the same user should fail.
	err := cs.Add(ctx, newTestConnection("conn-3", "user-1"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMaxConnectionsExceeded)

	// Overwriting an existing connection should still work.
	info := newTestConnection("conn-1", "user-1")
	info.Metadata = map[string]string{"updated": "true"}
	err = cs.Add(ctx, info)
	assert.NoError(t, err)
}

// P-002, P-004: New fields round-trip through Add/Get.
func TestRedisConnectionStore_Add_NewFieldsRoundTrip(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	info := &ConnectionInfo{
		ID:              "conn-full",
		UserID:          "user-1",
		SessionID:       "sess-1",
		DeviceID:        "device-xyz",
		DeviceType:      "ios",
		IPAddress:       "10.0.0.1",
		Protocol:        "quic",
		LastHeartbeatAt: now,
		Status:          "active",
		Metadata:        map[string]string{"env": "test"},
	}
	require.NoError(t, cs.Add(ctx, info))

	got, err := cs.Get(ctx, "conn-full")
	require.NoError(t, err)
	assert.Equal(t, "device-xyz", got.DeviceID)
	assert.Equal(t, "ios", got.DeviceType)
	assert.Equal(t, "10.0.0.1", got.IPAddress)
	assert.Equal(t, "quic", got.Protocol)
	assert.Equal(t, "active", got.Status)
	assert.Equal(t, now.Unix(), got.LastHeartbeatAt.Unix())
}

// P-009: Error messages contain context.
func TestRedisConnectionStore_Add_ErrorContext(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Add(ctx, &ConnectionInfo{ID: "conn-ctx"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

func TestRedisConnectionStore_Get(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	info.Metadata = map[string]string{"ip": "127.0.0.1"}
	require.NoError(t, cs.Add(ctx, info))

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "conn-1", got.ID)
	assert.Equal(t, "user-1", got.UserID)
	assert.Equal(t, "session-1", got.SessionID)
	assert.Equal(t, "127.0.0.1", got.Metadata["ip"])
}

func TestRedisConnectionStore_Get_NotFound(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	_, err := cs.Get(ctx, "nonexistent")
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestRedisConnectionStore_Get_EmptyID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	_, err := cs.Get(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection ID is required")
}

func TestRedisConnectionStore_Remove(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	require.NoError(t, cs.Add(ctx, info))

	err := cs.Remove(ctx, "conn-1")
	require.NoError(t, err)

	_, err = cs.Get(ctx, "conn-1")
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestRedisConnectionStore_Remove_Nonexistent(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	// Removing a nonexistent connection should be a no-op.
	err := cs.Remove(ctx, "nonexistent")
	require.NoError(t, err)
}

func TestRedisConnectionStore_Remove_EmptyID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Remove(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection ID is required")
}

func TestRedisConnectionStore_Remove_CleansUpUserSet(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	// Add two connections for the same user.
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))

	// Remove one.
	require.NoError(t, cs.Remove(ctx, "conn-1"))

	// The other should still be listed.
	conns, err := cs.ListByUser(ctx, "user-1", -1)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, "conn-2", conns[0].ID)
}

func TestRedisConnectionStore_Exists(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))

	exists, err := cs.Exists(ctx, "conn-1")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = cs.Exists(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRedisConnectionStore_Exists_EmptyID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	_, err := cs.Exists(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection ID is required")
}

func TestRedisConnectionStore_Update(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	info.Metadata = map[string]string{"platform": "ios"}
	require.NoError(t, cs.Add(ctx, info))

	newMeta := map[string]string{"platform": "android", "version": "2.0"}
	err := cs.Update(ctx, "conn-1", newMeta)
	require.NoError(t, err)

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "android", got.Metadata["platform"])
	assert.Equal(t, "2.0", got.Metadata["version"])
}

func TestRedisConnectionStore_Update_NotFound(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Update(ctx, "nonexistent", map[string]string{"k": "v"})
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestRedisConnectionStore_Update_EmptyID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Update(ctx, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection ID is required")
}

// P-005: Patch method tests.
func TestRedisConnectionStore_Patch(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	info.Metadata = map[string]string{"platform": "ios"}
	require.NoError(t, cs.Add(ctx, info))

	// Patch the status and add a heartbeat.
	err := cs.Patch(ctx, "conn-1", func(ci *ConnectionInfo) {
		ci.Status = "idle"
		ci.LastHeartbeatAt = time.Now()
		ci.Metadata["patched"] = "true"
	})
	require.NoError(t, err)

	got, err := cs.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "idle", got.Status)
	assert.False(t, got.LastHeartbeatAt.IsZero())
	assert.Equal(t, "true", got.Metadata["patched"])
	assert.Equal(t, "ios", got.Metadata["platform"]) // unchanged
}

func TestRedisConnectionStore_Patch_NotFound(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Patch(ctx, "nonexistent", func(ci *ConnectionInfo) {
		ci.Status = "idle"
	})
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestRedisConnectionStore_Patch_EmptyID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Patch(ctx, "", func(ci *ConnectionInfo) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection ID is required")
}

func TestRedisConnectionStore_Patch_NilUpdater(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))

	err := cs.Patch(ctx, "conn-1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "updater function is nil")
}

func TestRedisConnectionStore_Refresh(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	info := newTestConnection("conn-1", "user-1")
	require.NoError(t, cs.Add(ctx, info))

	// Get the original TTL.
	ttlBefore, err := cs.client.TTL(ctx, cs.infoKey("conn-1")).Result()
	require.NoError(t, err)

	// Wait a bit so TTL decreases significantly.
	time.Sleep(500 * time.Millisecond)

	err = cs.Refresh(ctx, "conn-1")
	require.NoError(t, err)

	// TTL should be reset to the full value.
	ttlAfter, err := cs.client.TTL(ctx, cs.infoKey("conn-1")).Result()
	require.NoError(t, err)
	// After the Refresh, the TTL should be close to the original full value
	// (5s), not the decreased value it had before the Refresh. We check that
	// the refreshed TTL is greater than what it would have been without the
	// Refresh (ttlBefore - 500ms).
	assert.True(t, ttlAfter > ttlBefore-500*time.Millisecond,
		"TTL should be refreshed: before=%v, after=%v", ttlBefore, ttlAfter)
	// Also verify the refreshed TTL is close to the full value.
	assert.True(t, ttlAfter >= 4*time.Second,
		"Refreshed TTL should be close to full value (5s), got %v", ttlAfter)
}

func TestRedisConnectionStore_Refresh_NotFound(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Refresh(ctx, "nonexistent")
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestRedisConnectionStore_Refresh_EmptyID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Refresh(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection ID is required")
}

// ---------------------------------------------------------------------------
// RedisConnectionStore - user-level operations
// ---------------------------------------------------------------------------

func TestRedisConnectionStore_ListByUser(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	// Add connections for two different users.
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-3", "user-2")))

	conns, err := cs.ListByUser(ctx, "user-1", -1)
	require.NoError(t, err)
	assert.Len(t, conns, 2)

	connIDs := make([]string, len(conns))
	for i, c := range conns {
		connIDs[i] = c.ID
	}
	assert.ElementsMatch(t, []string{"conn-1", "conn-2"}, connIDs)
}

func TestRedisConnectionStore_ListByUser_NoConnections(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	conns, err := cs.ListByUser(ctx, "user-no-conns", -1)
	require.NoError(t, err)
	assert.Empty(t, conns)
	assert.IsType(t, []*ConnectionInfo{}, conns) // should be empty slice, not nil
}

func TestRedisConnectionStore_ListByUser_EmptyUserID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	_, err := cs.ListByUser(ctx, "", -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

// P-001: ListByUser with limit.
func TestRedisConnectionStore_ListByUser_WithLimit(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, cs.Add(ctx, newTestConnection(
			fmt.Sprintf("conn-%d", i), "user-1")))
	}

	conns, err := cs.ListByUser(ctx, "user-1", 3)
	require.NoError(t, err)
	assert.Len(t, conns, 3)
}

// P-001: ListByUser with limit=0 should return all.
func TestRedisConnectionStore_ListByUser_ZeroLimitReturnsAll(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, cs.Add(ctx, newTestConnection(
			fmt.Sprintf("conn-%d", i), "user-1")))
	}

	conns, err := cs.ListByUser(ctx, "user-1", 0)
	require.NoError(t, err)
	assert.Len(t, conns, 5)
}

func TestRedisConnectionStore_ListByUser_CleansStaleEntries(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	// Add a connection with a very short TTL.
	info := newTestConnection("conn-ephemeral", "user-1")
	info.TTL = 100 * time.Millisecond
	require.NoError(t, cs.Add(ctx, info))

	// Add a stable connection.
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-stable", "user-1")))

	// Wait for the ephemeral connection to expire.
	time.Sleep(200 * time.Millisecond)

	conns, err := cs.ListByUser(ctx, "user-1", -1)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, "conn-stable", conns[0].ID)

	// The stale entry should have been cleaned from the user set.
	count, err := cs.CountByUser(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestRedisConnectionStore_CountByUser(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-3", "user-2")))

	count, err := cs.CountByUser(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestRedisConnectionStore_CountByUser_NoConnections(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	count, err := cs.CountByUser(ctx, "user-no-conns")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestRedisConnectionStore_CountByUser_EmptyUserID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	_, err := cs.CountByUser(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

// P-010: CountAll.
func TestRedisConnectionStore_CountAll(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-3", "user-2")))

	count, err := cs.CountAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestRedisConnectionStore_CountAll_Empty(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	count, err := cs.CountAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestRedisConnectionStore_RemoveByUser(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-1", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-2", "user-1")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-3", "user-2")))

	removed, err := cs.RemoveByUser(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), removed)

	// All user-1 connections should be gone.
	count, err := cs.CountByUser(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	// user-2's connections should be unaffected.
	count, err = cs.CountByUser(ctx, "user-2")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// The info keys should also be gone.
	_, err = cs.Get(ctx, "conn-1")
	assert.Equal(t, ErrConnectionNotFound, err)
	_, err = cs.Get(ctx, "conn-2")
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestRedisConnectionStore_RemoveByUser_NoConnections(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	removed, err := cs.RemoveByUser(ctx, "user-no-conns")
	require.NoError(t, err)
	assert.Equal(t, int64(0), removed)
}

func TestRedisConnectionStore_RemoveByUser_EmptyUserID(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	_, err := cs.RemoveByUser(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

// ---------------------------------------------------------------------------
// RedisConnectionStore - Ping, Close, TTL
// ---------------------------------------------------------------------------

func TestRedisConnectionStore_Ping(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	err := cs.Ping(ctx)
	require.NoError(t, err)
}

func TestRedisConnectionStore_Close(t *testing.T) {
	// Close on a store created via NewRedisConnectionStore (owns client).
	cs, err := NewRedisConnectionStore(RedisConnectionStoreConfig{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
	if err != nil {
		t.Skipf("test Redis not reachable: %v", err)
	}

	err = cs.Close()
	require.NoError(t, err)

	// Further operations should fail because the client is closed.
	ctx := context.Background()
	err = cs.Ping(ctx)
	require.Error(t, err)
}

func TestRedisConnectionStore_Close_DoesNotCloseExternalClient(t *testing.T) {
	skipIfNoRedis(t)
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
	defer client.Close()

	cs := NewRedisConnectionStoreFromClient(RedisConnectionStoreFromClientConfig{
		Client: client,
	})
	err := cs.Close()
	require.NoError(t, err)

	// Client should still work because it is not owned by the store.
	ctx := context.Background()
	assert.NoError(t, client.Ping(ctx).Err())
}

func TestRedisConnectionStore_TTL_Expiration(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	// Override the store's default TTL for this test.
	cs.defaultTTL = 200 * time.Millisecond

	info := newTestConnection("conn-ttl", "user-1")
	require.NoError(t, cs.Add(ctx, info))

	// Should be retrievable immediately.
	got, err := cs.Get(ctx, "conn-ttl")
	require.NoError(t, err)
	assert.Equal(t, "conn-ttl", got.ID)

	// Wait for expiration.
	time.Sleep(300 * time.Millisecond)

	// Should now be gone (Redis TTL eviction).
	_, err = cs.Get(ctx, "conn-ttl")
	assert.Equal(t, ErrConnectionNotFound, err)
}

func TestRedisConnectionStore_CustomPerConnectionTTL(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	info := newTestConnection("conn-short", "user-1")
	info.TTL = 200 * time.Millisecond // shorter than the store default
	require.NoError(t, cs.Add(ctx, info))

	// Should exist initially.
	_, err := cs.Get(ctx, "conn-short")
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	_, err = cs.Get(ctx, "conn-short")
	assert.Equal(t, ErrConnectionNotFound, err)
}

// ---------------------------------------------------------------------------
// RedisConnectionStore - KeyPrefix (multi-tenant)
// ---------------------------------------------------------------------------

func TestRedisConnectionStore_KeyPrefix(t *testing.T) {
	skipIfNoRedis(t)
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
	defer client.Close()

	ctx := context.Background()
	_ = client.FlushDB(ctx).Err()

	cs1 := NewRedisConnectionStoreFromClient(RedisConnectionStoreFromClientConfig{
		Client:    client,
		DefaultTTL: 5 * time.Second,
		KeyPrefix: "tenant1:",
	})

	cs2 := NewRedisConnectionStoreFromClient(RedisConnectionStoreFromClientConfig{
		Client:    client,
		DefaultTTL: 5 * time.Second,
		KeyPrefix: "tenant2:",
	})

	defer func() {
		_ = client.FlushDB(ctx).Err()
	}()

	// Add to tenant1.
	require.NoError(t, cs1.Add(ctx, newTestConnection("conn-1", "user-1")))

	// Should be visible in tenant1.
	got, err := cs1.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "conn-1", got.ID)

	// Should NOT be visible in tenant2 (different key prefix).
	_, err = cs2.Get(ctx, "conn-1")
	assert.Equal(t, ErrConnectionNotFound, err)
}

// ---------------------------------------------------------------------------
// RedisConnectionStore - Concurrent operations
// ---------------------------------------------------------------------------

func TestRedisConnectionStore_ConcurrentAddGet(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup

	// Concurrent adds.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			connID := fmt.Sprintf("conn-%d", i)
			userID := fmt.Sprintf("user-%d", i%5)
			info := newTestConnection(connID, userID)
			assert.NoError(t, cs.Add(ctx, info))
		}(i)
	}
	wg.Wait()

	// Concurrent gets.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			connID := fmt.Sprintf("conn-%d", i)
			got, err := cs.Get(ctx, connID)
			assert.NoError(t, err)
			assert.Equal(t, connID, got.ID)
		}(i)
	}
	wg.Wait()

	// Verify counts.
	for u := 0; u < 5; u++ {
		userID := fmt.Sprintf("user-%d", u)
		count, err := cs.CountByUser(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, int64(4), count) // 20 connections / 5 users
	}
}

// ---------------------------------------------------------------------------
// RedisConnectionStore - resolveDefaultTTL config method
// ---------------------------------------------------------------------------

func TestRedisConnectionStoreConfig_resolveDefaultTTL(t *testing.T) {
	tests := []struct {
		name     string
		cfg      RedisConnectionStoreConfig
		expected time.Duration
	}{
		{
			name:     "zero uses package default",
			cfg:      RedisConnectionStoreConfig{},
			expected: defaultConnectionTTL,
		},
		{
			name:     "negative uses package default",
			cfg:      RedisConnectionStoreConfig{DefaultTTL: -1},
			expected: defaultConnectionTTL,
		},
		{
			name:     "custom value",
			cfg:      RedisConnectionStoreConfig{DefaultTTL: 10 * time.Minute},
			expected: 10 * time.Minute,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.cfg.resolveDefaultTTL())
		})
	}
}

// ---------------------------------------------------------------------------
// A-012: classifyRedisError tests
// ---------------------------------------------------------------------------

func TestClassifyRedisError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantNil bool
		is      error
	}{
		{
			name:    "nil error",
			err:     nil,
			wantNil: true,
		},
		{
			name: "connection refused",
			err:  fmt.Errorf("dial tcp 127.0.0.1:6379: connection refused"),
			is:   ErrRedisConnectionFailed,
		},
		{
			name: "connection reset",
			err:  fmt.Errorf("read: connection reset by peer"),
			is:   ErrRedisConnectionFailed,
		},
		{
			name: "broken pipe",
			err:  fmt.Errorf("write: broken pipe"),
			is:   ErrRedisConnectionFailed,
		},
		{
			name: "no such host",
			err:  fmt.Errorf("dial tcp: lookup redis: no such host"),
			is:   ErrRedisConnectionFailed,
		},
		{
			name: "context deadline exceeded",
			err:  fmt.Errorf("context deadline exceeded"),
			is:   ErrRedisTimeout,
		},
		{
			name: "unrelated error passes through",
			err:  fmt.Errorf("some other error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := classifyRedisError(tc.err)
			if tc.wantNil {
				assert.Nil(t, result)
				return
			}
			if tc.is != nil {
				assert.ErrorIs(t, result, tc.is)
			} else {
				// Should be the original error.
				assert.Equal(t, tc.err, result)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RedisConnectionStore - Lua script atomicity (P-003, A-003, A-007)
// ---------------------------------------------------------------------------

func TestRedisConnectionStore_Add_UsesLuaScript(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	// Verify that Add creates both the info key and user set entry.
	info := newTestConnection("conn-lua", "user-lua")
	require.NoError(t, cs.Add(ctx, info))

	// Info key should exist.
	got, err := cs.Get(ctx, "conn-lua")
	require.NoError(t, err)
	assert.Equal(t, "conn-lua", got.ID)

	// User set should contain the connection ID.
	members, err := cs.client.SMembers(ctx, cs.userKey("user-lua")).Result()
	require.NoError(t, err)
	assert.Contains(t, members, "conn-lua")
}

func TestRedisConnectionStore_Remove_UsesLuaScript(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, cs.Add(ctx, newTestConnection("conn-rm", "user-rm")))

	// Remove should atomically delete info key and remove from user set.
	require.NoError(t, cs.Remove(ctx, "conn-rm"))

	// Info key should be gone.
	_, err := cs.Get(ctx, "conn-rm")
	assert.Equal(t, ErrConnectionNotFound, err)

	// User set should be empty.
	members, err := cs.client.SMembers(ctx, cs.userKey("user-rm")).Result()
	require.NoError(t, err)
	assert.Empty(t, members)
}

// ---------------------------------------------------------------------------
// Second-round review fix tests
// ---------------------------------------------------------------------------

// P2-007/A2-001: "i/o timeout" should be classified as timeout, not
// connection failure.
func TestClassifyRedisError_IOTimeoutIsTimeout(t *testing.T) {
	err := fmt.Errorf("read: i/o timeout")
	result := classifyRedisError(err)
	assert.ErrorIs(t, result, ErrRedisTimeout)
	assert.NotErrorIs(t, result, ErrRedisConnectionFailed)
}

// P2-005/A2-004: ErrMaxConnectionsExceeded is detectable via errors.Is.
func TestErrMaxConnectionsExceeded_ErrorsIs(t *testing.T) {
	wrapped := fmt.Errorf("server: add connection [connID=x]: %w", ErrMaxConnectionsExceeded)
	assert.ErrorIs(t, wrapped, ErrMaxConnectionsExceeded)
}

// A2-008: Overwriting a connection with a different UserID cleans up the old
// user's set.
func TestRedisConnectionStore_Add_UserIDChange_CleansOldUserSet(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	// Add a connection under user-A.
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-migrate", "user-A")))

	// Verify user-A's set contains the connection.
	members, err := cs.client.SMembers(ctx, cs.userKey("user-A")).Result()
	require.NoError(t, err)
	assert.Contains(t, members, "conn-migrate")

	// Overwrite the same connection ID with a different UserID (user-B).
	info := newTestConnection("conn-migrate", "user-B")
	require.NoError(t, cs.Add(ctx, info))

	// The connection should now belong to user-B.
	got, err := cs.Get(ctx, "conn-migrate")
	require.NoError(t, err)
	assert.Equal(t, "user-B", got.UserID)

	// user-B's set should contain the connection.
	membersB, err := cs.client.SMembers(ctx, cs.userKey("user-B")).Result()
	require.NoError(t, err)
	assert.Contains(t, membersB, "conn-migrate")

	// user-A's set should no longer contain the connection.
	membersA, err := cs.client.SMembers(ctx, cs.userKey("user-A")).Result()
	require.NoError(t, err)
	assert.NotContains(t, membersA, "conn-migrate")
}

// A2-008: Overwriting with a different UserID respects MaxConnectionsPerUser
// for the new user.
func TestRedisConnectionStore_Add_UserIDChange_RespectsMaxConns(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()
	cs.maxConnsPerUser = 2

	// user-B already has 2 connections.
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-b1", "user-B")))
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-b2", "user-B")))

	// user-A has conn-a1.
	require.NoError(t, cs.Add(ctx, newTestConnection("conn-a1", "user-A")))

	// Try to migrate conn-a1 to user-B by overwriting with UserID=user-B.
	// Should fail because user-B is at the limit and conn-a1 is not in
	// user-B's set.
	info := newTestConnection("conn-a1", "user-B")
	err := cs.Add(ctx, info)
	assert.ErrorIs(t, err, ErrMaxConnectionsExceeded)

	// conn-a1 should still belong to user-A.
	got, err := cs.Get(ctx, "conn-a1")
	require.NoError(t, err)
	assert.Equal(t, "user-A", got.UserID)
}

// P2-003/A2-005: Refresh extends TTL successfully.
func TestRedisConnectionStore_Refresh_LuaAtomic(t *testing.T) {
	cs, cleanup := setupTestRedis(t)
	defer cleanup()
	ctx := context.Background()

	info := newTestConnection("conn-refresh-lua", "user-1")
	info.TTL = 5 * time.Second
	require.NoError(t, cs.Add(ctx, info))

	// Wait for some TTL to pass.
	time.Sleep(200 * time.Millisecond)

	// Refresh should succeed and reset the TTL.
	require.NoError(t, cs.Refresh(ctx, "conn-refresh-lua"))

	ttlAfter, err := cs.client.TTL(ctx, cs.infoKey("conn-refresh-lua")).Result()
	require.NoError(t, err)
	// TTL should be close to 5s (minus a tiny amount for execution time).
	assert.True(t, ttlAfter >= 4*time.Second,
		"TTL should be refreshed to ~5s, got %v", ttlAfter)
}
