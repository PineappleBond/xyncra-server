package client

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// TestIntegration_Reconnect_Handshake verifies that after connection, a
// system.reconnect request is sent to the server (async handshake).
func TestIntegration_Reconnect_Handshake(t *testing.T) {
	server := newMockWSServer(t)

	// Track if system.reconnect was called.
	var reconnectCalled atomic.Int32
	server.SetRPCHandler("system.reconnect", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		reconnectCalled.Add(1)
		return json.Marshal(map[string]any{"status": "ok", "replayed": 0, "total": 0})
	})

	// Handler for sync_updates (FullSync will be called after handshake).
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{
			"updates":    []any{},
			"has_more":   false,
			"latest_seq": 0,
		})
	})

	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = client.Start(ctx) }()

	// Wait for connection.
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("client did not connect: %v", err)
	}

	// Wait for async handshake to complete.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reconnectCalled.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if reconnectCalled.Load() == 0 {
		t.Error("system.reconnect was not called after connection")
	}
}

// TestIntegration_Reconnect_Handshake_Seq_Tracking verifies that the client
// tracks the highest Seq value from server-initiated requests and reports it
// in the system.reconnect handshake after reconnection.
func TestIntegration_Reconnect_Handshake_Seq_Tracking(t *testing.T) {
	server := newMockWSServer(t)

	// Track last_seen_seq values from reconnect calls.
	var lastSeenSeqs []uint64
	var lastSeenSeqMu atomic.Value
	lastSeenSeqMu.Store(&lastSeenSeqs)

	server.SetRPCHandler("system.reconnect", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		var params map[string]uint64
		if err := json.Unmarshal(req.Params, &params); err == nil {
			seq := params["last_seen_seq"]
			ptr := lastSeenSeqMu.Load().(*[]uint64)
			newSlice := append(*ptr, seq)
			lastSeenSeqMu.Store(&newSlice)
		}
		return json.Marshal(map[string]any{"status": "ok", "replayed": 0, "total": 0})
	})

	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{
			"updates":    []any{},
			"has_more":   false,
			"latest_seq": 0,
		})
	})

	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = client.Start(ctx) }()

	// Wait for initial connection.
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("client did not connect: %v", err)
	}

	// Wait for initial handshake.
	time.Sleep(200 * time.Millisecond)

	// Send server-initiated requests with Seq values 1, 2, 3.
	for i := uint64(1); i <= 3; i++ {
		req := &protocol.PackageDataRequest{
			ID:     "server-req-" + string(rune('0'+i)),
			Method: "test_method",
			Seq:    i,
		}
		reqData, _ := json.Marshal(req)
		pkg := &protocol.Package{
			Type: protocol.PackageTypeRequest,
			Data: reqData,
		}
		if err := server.SendPackage(pkg); err != nil {
			t.Fatalf("failed to send request with seq %d: %v", i, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Force disconnect by closing the connection.
	server.mu.Lock()
	if len(server.conns) > 0 {
		_ = server.conns[0].Close()
	}
	server.mu.Unlock()

	// Wait for reconnect and clean up dead connections.
	time.Sleep(500 * time.Millisecond)
	server.RemoveClosedConnections()

	// Verify system.reconnect was called with last_seen_seq = 3.
	ptr := lastSeenSeqMu.Load().(*[]uint64)
	seqs := *ptr
	if len(seqs) < 2 {
		t.Fatalf("expected at least 2 reconnect calls (initial + after reconnect), got %d", len(seqs))
	}

	// The last reconnect should have last_seen_seq = 3.
	lastSeq := seqs[len(seqs)-1]
	if lastSeq != 3 {
		t.Errorf("expected last_seen_seq = 3, got %d", lastSeq)
	}
}

// TestIntegration_Idempotency_Dedup_On_Replay verifies that when the server
// sends a request with the same IdempotencyKey but different ID, the handler
// is only invoked once.
func TestIntegration_Idempotency_Dedup_On_Replay(t *testing.T) {
	server := newMockWSServer(t)

	// Handler for system.reconnect (will be called during connection).
	server.SetRPCHandler("system.reconnect", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"status": "ok", "replayed": 0, "total": 0})
	})

	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{
			"updates":    []any{},
			"has_more":   false,
			"latest_seq": 0,
		})
	})

	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Track handler invocations.
	var handlerCallCount atomic.Int32
	client.RegisterRequestHandler("test_method", func(ctx context.Context, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		handlerCallCount.Add(1)
		return json.Marshal(map[string]string{"result": "ok"})
	})

	go func() { _ = client.Start(ctx) }()

	// Wait for connection.
	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("client did not connect: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Send first request with IdempotencyKey "key-1" and ID "req-1".
	req1 := &protocol.PackageDataRequest{
		ID:             "req-1",
		Method:         "test_method",
		IdempotencyKey: "key-1",
	}
	reqData1, _ := json.Marshal(req1)
	pkg1 := &protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: reqData1,
	}
	if err := server.SendPackage(pkg1); err != nil {
		t.Fatalf("failed to send first request: %v", err)
	}

	// Wait for handler to be called.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if handlerCallCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if handlerCallCount.Load() != 1 {
		t.Errorf("expected handler to be called once, got %d", handlerCallCount.Load())
	}

	// Send second request with same IdempotencyKey "key-1" but different ID "req-2".
	req2 := &protocol.PackageDataRequest{
		ID:             "req-2",
		Method:         "test_method",
		IdempotencyKey: "key-1",
	}
	reqData2, _ := json.Marshal(req2)
	pkg2 := &protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: reqData2,
	}
	if err := server.SendPackage(pkg2); err != nil {
		t.Fatalf("failed to send second request: %v", err)
	}

	// Wait a bit for potential second call.
	time.Sleep(300 * time.Millisecond)

	// Handler should NOT be called again.
	if handlerCallCount.Load() != 1 {
		t.Errorf("expected handler to still be called once (dedup), got %d", handlerCallCount.Load())
	}
}

// TestIntegration_Idempotency_Different_Keys verifies that requests with
// different IdempotencyKeys both trigger the handler.
func TestIntegration_Idempotency_Different_Keys(t *testing.T) {
	server := newMockWSServer(t)

	server.SetRPCHandler("system.reconnect", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"status": "ok", "replayed": 0, "total": 0})
	})

	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{
			"updates":    []any{},
			"has_more":   false,
			"latest_seq": 0,
		})
	})

	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var handlerCallCount atomic.Int32
	client.RegisterRequestHandler("test_method", func(ctx context.Context, req *protocol.PackageDataRequest) (json.RawMessage, error) {
		handlerCallCount.Add(1)
		return json.Marshal(map[string]string{"result": "ok"})
	})

	go func() { _ = client.Start(ctx) }()

	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("client did not connect: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Send first request with IdempotencyKey "key-1".
	req1 := &protocol.PackageDataRequest{
		ID:             "req-1",
		Method:         "test_method",
		IdempotencyKey: "key-1",
	}
	reqData1, _ := json.Marshal(req1)
	pkg1 := &protocol.Package{Type: protocol.PackageTypeRequest, Data: reqData1}
	if err := server.SendPackage(pkg1); err != nil {
		t.Fatalf("failed to send first request: %v", err)
	}

	// Wait for first call.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if handlerCallCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Send second request with different IdempotencyKey "key-2".
	req2 := &protocol.PackageDataRequest{
		ID:             "req-2",
		Method:         "test_method",
		IdempotencyKey: "key-2",
	}
	reqData2, _ := json.Marshal(req2)
	pkg2 := &protocol.Package{Type: protocol.PackageTypeRequest, Data: reqData2}
	if err := server.SendPackage(pkg2); err != nil {
		t.Fatalf("failed to send second request: %v", err)
	}

	// Wait for second call.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if handlerCallCount.Load() >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if handlerCallCount.Load() != 2 {
		t.Errorf("expected handler to be called twice (different keys), got %d", handlerCallCount.Load())
	}
}

// TestIntegration_Backward_Compat_Unknown_Reconnect verifies that when the
// server returns an error for system.reconnect, the client continues with
// FullSync anyway (graceful degradation, D-072).
func TestIntegration_Backward_Compat_Unknown_Reconnect(t *testing.T) {
	server := newMockWSServer(t)

	// Server returns error for system.reconnect.
	server.SetRPCHandler("system.reconnect", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return nil, context.DeadlineExceeded
	})

	// Server returns valid data for sync_updates.
	var syncCalled atomic.Int32
	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		syncCalled.Add(1)
		return json.Marshal(map[string]any{
			"updates":    []any{},
			"has_more":   false,
			"latest_seq": 0,
		})
	})

	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = client.Start(ctx) }()

	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("client did not connect: %v", err)
	}

	// Wait for FullSync to be called (system.reconnect error should not block it).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if syncCalled.Load() > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if syncCalled.Load() == 0 {
		t.Error("sync_updates was not called after system.reconnect error (FullSync should still proceed)")
	}
}

// TestIntegration_DeviceID_Persistence verifies that WithDeviceID("device-abc")
// causes client.DeviceID() to return "device-abc".
func TestIntegration_DeviceID_Persistence(t *testing.T) {
	server := newMockWSServer(t)

	server.SetRPCHandler("system.reconnect", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"status": "ok", "replayed": 0, "total": 0})
	})

	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{
			"updates":    []any{},
			"has_more":   false,
			"latest_seq": 0,
		})
	})

	client := newTestClient(t, server, WithDeviceID("device-abc"))

	if got := client.DeviceID(); got != "device-abc" {
		t.Errorf("expected DeviceID() = %q, got %q", "device-abc", got)
	}
}

// TestIntegration_Adaptive_Timeout_Convergence verifies that after 10 RPCs
// with 200ms delay, the SRTT rises above 100ms (should be ~200ms).
func TestIntegration_Adaptive_Timeout_Convergence(t *testing.T) {
	server := newMockWSServer(t)

	server.SetRPCHandler("system.reconnect", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"status": "ok", "replayed": 0, "total": 0})
	})

	server.SetRPCHandler("sync_updates", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]any{
			"updates":    []any{},
			"has_more":   false,
			"latest_seq": 0,
		})
	})

	// Server delays responses by 200ms.
	server.SetRPCHandler("slow_rpc", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		time.Sleep(200 * time.Millisecond)
		return json.Marshal(map[string]string{"result": "ok"})
	})

	client := newTestClient(t, server, WithRPCTimeout(10*time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = client.Start(ctx) }()

	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("client did not connect: %v", err)
	}

	// Wait for connection to stabilize.
	time.Sleep(300 * time.Millisecond)

	// Make 10 RPC calls with delay.
	for i := 0; i < 10; i++ {
		_, err := client.Call(ctx, "slow_rpc", nil)
		if err != nil {
			t.Fatalf("RPC call %d failed: %v", i, err)
		}
	}

	// Verify SRTT is > 100ms (should be ~200ms).
	srtt := client.rttTracker.SRTT()
	if srtt < 100*time.Millisecond {
		t.Errorf("expected SRTT > 100ms, got %v", srtt)
	}
}
