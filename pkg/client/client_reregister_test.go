package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
)

// regClientSeq is a monotonic counter used to give each test client a unique
// in-memory database name, avoiding collisions when tests run in parallel.
var regClientSeq atomic.Int64

// ---------------------------------------------------------------------------
// Option tests (CL-REG-001 .. CL-REG-003)
// ---------------------------------------------------------------------------

// TestWithFunctions verifies that WithFunctions sets the function list on
// clientOptions.
func TestWithFunctions(t *testing.T) {
	t.Parallel()

	fns := []protocol.FunctionInfo{
		{Name: "fn_a", Description: "first"},
		{Name: "fn_b", Description: "second"},
	}
	var opts clientOptions
	WithFunctions(fns)(&opts)

	assert.Len(t, opts.functions, 2)
	assert.Equal(t, "fn_a", opts.functions[0].Name)
	assert.Equal(t, "fn_b", opts.functions[1].Name)
}

// TestWithFunctions_EmptySlice verifies that WithFunctions preserves a
// non-nil empty slice, distinguishing it from an unconfigured (nil) state.
func TestWithFunctions_EmptySlice(t *testing.T) {
	t.Parallel()

	var opts clientOptions
	WithFunctions([]protocol.FunctionInfo{})(&opts)
	assert.NotNil(t, opts.functions)
	assert.Empty(t, opts.functions)
}

// TestWithFunctions_NilSlice verifies that WithFunctions preserves a nil
// slice, leaving clientOptions.functions in its zero (unconfigured) state.
func TestWithFunctions_NilSlice(t *testing.T) {
	t.Parallel()

	var opts clientOptions
	WithFunctions(nil)(&opts)
	assert.Nil(t, opts.functions)
}

// TestWithDeviceName verifies that WithDeviceName sets the device name on
// clientOptions.
func TestWithDeviceName(t *testing.T) {
	t.Parallel()

	var opts clientOptions
	WithDeviceName("my-device")(&opts)

	assert.Equal(t, "my-device", opts.deviceName)
}

// TestWithDeviceType verifies that WithDeviceType sets the device type on
// clientOptions.
func TestWithDeviceType(t *testing.T) {
	t.Parallel()

	var opts clientOptions
	WithDeviceType("cli")(&opts)

	assert.Equal(t, "cli", opts.deviceType)
}

// ---------------------------------------------------------------------------
// reregisterFunctions tests (CL-REG-004 .. CL-REG-008)
// ---------------------------------------------------------------------------

// newRegTestClient creates a XyncraClient connected to the given mock server
// with a guaranteed-unique in-memory database name. This avoids SQLite name
// collisions when many tests run in parallel.
func newRegTestClient(t *testing.T, server *mockWSServer, opts ...ClientOption) *XyncraClient {
	t.Helper()
	seq := regClientSeq.Add(1)
	name := fmt.Sprintf("reg_test_%d_%d", time.Now().UnixNano(), seq)
	db, err := store.NewInMemory(name)
	if err != nil {
		t.Fatalf("newRegTestClient: create db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	allOpts := []ClientOption{
		WithServerURL(server.URL()),
		WithUserID("test-user"),
		WithDB(db),
		WithLogger(&testLogger{t: t}),
		WithReconnectMaxRetries(1),
		WithReconnectBaseDelay(10 * time.Millisecond),
		WithReconnectMaxDelay(50 * time.Millisecond),
		WithHeartbeatInterval(1 * time.Hour),
		WithPullDebounce(10 * time.Millisecond),
	}
	allOpts = append(allOpts, opts...)
	c, err := New(allOpts...)
	if err != nil {
		t.Fatalf("newRegTestClient: create client: %v", err)
	}
	t.Cleanup(func() { c.Stop() })
	return c
}

// startConnectedClient starts the client in a goroutine and waits for the
// connection to be established. The sync_updates handler must be registered
// on the server before calling this helper so the sync manager does not
// stall on startup.
func startConnectedClient(t *testing.T, c *XyncraClient, server *mockWSServer) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = c.Start(ctx) }()
	require.NoError(t, server.AcceptConnection(5*time.Second))
	// Give internal goroutines (sync manager, etc.) a moment to stabilise.
	time.Sleep(200 * time.Millisecond)
	return cancel
}

// registerSyncHandler sets up the mandatory sync_updates handler that the
// sync manager calls during Start().
func registerSyncHandler(server *mockWSServer) {
	server.SetRPCHandler("sync_updates", func(_ *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(SyncUpdatesResult{Updates: nil, HasMore: false, LatestSeq: 0})
	})
}

// TestReregisterFunctions_SendsRPC verifies that reregisterFunctions sends a
// system.register_functions RPC with the correct parameters and that the mock
// server receives them.
func TestReregisterFunctions_SendsRPC(t *testing.T) {
	t.Parallel()

	server := newMockWSServer(t)
	registerSyncHandler(server)

	var receivedMu sync.Mutex
	var receivedParams map[string]any

	server.SetRPCHandler("system.register_functions", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		var params map[string]any
		_ = json.Unmarshal(req.Params, &params)
		receivedMu.Lock()
		receivedParams = params
		receivedMu.Unlock()
		return json.RawMessage(`{}`), nil
	})

	fns := []protocol.FunctionInfo{
		{Name: "greet", Description: "say hello"},
	}

	c := newRegTestClient(t, server,
		WithDeviceName("test-dev"),
		WithDeviceType("cli"),
		WithFunctions(fns),
	)

	cancel := startConnectedClient(t, c, server)
	defer cancel()

	ctx := context.Background()
	c.reregisterFunctions(ctx)

	require.Eventually(t, func() bool {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		return receivedParams != nil
	}, 3*time.Second, 20*time.Millisecond)

	receivedMu.Lock()
	defer receivedMu.Unlock()

	assert.Equal(t, "test-dev", receivedParams["device_name"])
	assert.Equal(t, "cli", receivedParams["device_type"])

	fnList, ok := receivedParams["functions"].([]any)
	require.True(t, ok, "functions should be a slice")
	assert.Len(t, fnList, 1)
}

// TestReregisterFunctions_EmptyFunctions verifies that reregisterFunctions
// returns early (no RPC sent) when the function list is empty.
func TestReregisterFunctions_EmptyFunctions(t *testing.T) {
	t.Parallel()

	server := newMockWSServer(t)
	registerSyncHandler(server)

	rpcCalled := false
	server.SetRPCHandler("system.register_functions", func(_ *protocol.PackageDataRequest) (json.RawMessage, error) {
		rpcCalled = true
		return json.RawMessage(`{}`), nil
	})

	c := newRegTestClient(t, server,
		WithDeviceName("test-dev"),
		WithDeviceType("cli"),
		WithFunctions(nil),
	)

	cancel := startConnectedClient(t, c, server)
	defer cancel()

	ctx := context.Background()
	c.reregisterFunctions(ctx)

	// Allow a brief window for any stray RPC to arrive.
	time.Sleep(100 * time.Millisecond)

	assert.False(t, rpcCalled, "RPC should not be called when functions list is empty")
}

// TestReregisterFunctions_FailOpen verifies that reregisterFunctions does not
// panic or crash when the RPC fails. The error should be logged and the
// method should return gracefully (D-072 fail-open semantics).
func TestReregisterFunctions_FailOpen(t *testing.T) {
	t.Parallel()

	server := newMockWSServer(t)
	registerSyncHandler(server)

	// Register a handler that returns an error.
	server.SetRPCHandler("system.register_functions", func(_ *protocol.PackageDataRequest) (json.RawMessage, error) {
		return nil, assert.AnError
	})

	fns := []protocol.FunctionInfo{
		{Name: "risky_fn"},
	}

	c := newRegTestClient(t, server,
		WithDeviceName("test-dev"),
		WithDeviceType("cli"),
		WithFunctions(fns),
	)

	cancel := startConnectedClient(t, c, server)
	defer cancel()

	// Must not panic.
	assert.NotPanics(t, func() {
		ctx := context.Background()
		c.reregisterFunctions(ctx)
	})
}

// TestReregisterFunctions_Timeout verifies that reregisterFunctions respects
// its internal timeout and does not block indefinitely. We cancel the parent
// context quickly so the test does not wait the full 10 s internal timeout.
func TestReregisterFunctions_Timeout(t *testing.T) {
	t.Parallel()

	server := newMockWSServer(t)
	registerSyncHandler(server)

	// Handler that never responds — simulates a stalled server.
	server.SetRPCHandler("system.register_functions", func(_ *protocol.PackageDataRequest) (json.RawMessage, error) {
		// Block until the test finishes.
		<-t.Context().Done()
		return nil, t.Context().Err()
	})

	fns := []protocol.FunctionInfo{
		{Name: "slow_fn"},
	}

	c := newRegTestClient(t, server,
		WithDeviceName("test-dev"),
		WithDeviceType("cli"),
		WithFunctions(fns),
	)

	cancel := startConnectedClient(t, c, server)
	defer cancel()

	// Use a short-lived context to avoid waiting the full 10 s internal timeout.
	ctx, cancelCtx := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelCtx()

	done := make(chan struct{})
	go func() {
		assert.NotPanics(t, func() {
			c.reregisterFunctions(ctx)
		})
		close(done)
	}()

	select {
	case <-done:
		// returned within expected time
	case <-time.After(5 * time.Second):
		t.Fatal("reregisterFunctions did not return after context cancellation")
	}
}

// TestReregisterFunctions_ReconnectIntegration verifies that
// performReconnectHandshake calls reregisterFunctions after a reconnect. This
// is a lightweight integration test that confirms the handshake flow invokes
// both system.reconnect and system.register_functions without panicking.
func TestReregisterFunctions_ReconnectIntegration(t *testing.T) {
	t.Parallel()

	server := newMockWSServer(t)
	registerSyncHandler(server)

	var mu sync.Mutex
	reconnectCalled := false
	registerCalled := false

	server.SetRPCHandler("system.reconnect", func(_ *protocol.PackageDataRequest) (json.RawMessage, error) {
		mu.Lock()
		reconnectCalled = true
		mu.Unlock()
		return json.RawMessage(`{}`), nil
	})

	server.SetRPCHandler("system.register_functions", func(_ *protocol.PackageDataRequest) (json.RawMessage, error) {
		mu.Lock()
		registerCalled = true
		mu.Unlock()
		return json.RawMessage(`{}`), nil
	})

	fns := []protocol.FunctionInfo{
		{Name: "integration_fn"},
	}

	c := newRegTestClient(t, server,
		WithDeviceName("test-dev"),
		WithDeviceType("browser"),
		WithFunctions(fns),
	)

	cancel := startConnectedClient(t, c, server)
	defer cancel()

	// Call performReconnectHandshake directly — it launches a goroutine that
	// calls system.reconnect then reregisterFunctions.
	ctx := context.Background()
	c.performReconnectHandshake(ctx)

	// Wait for both RPCs to arrive.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return reconnectCalled && registerCalled
	}, 3*time.Second, 20*time.Millisecond,
		"expected both system.reconnect and system.register_functions to be called")
}
