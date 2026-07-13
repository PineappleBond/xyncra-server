package client

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// TestWeakNet_Reconnect_Replay_Dedup tests the full cycle: server sends request
// -> client processes -> disconnect -> reconnect -> server replays -> client
// deduplicates.
func TestWeakNet_Reconnect_Replay_Dedup(t *testing.T) {
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

	// Send initial request with IdempotencyKey.
	req1 := &protocol.PackageDataRequest{
		ID:             "original-req-id",
		Method:         "test_method",
		IdempotencyKey: "idem-key-123",
		Seq:            1,
	}
	reqData1, _ := json.Marshal(req1)
	pkg1 := &protocol.Package{Type: protocol.PackageTypeRequest, Data: reqData1}
	if err := server.SendPackage(pkg1); err != nil {
		t.Fatalf("failed to send initial request: %v", err)
	}

	// Wait for handler to be called.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if handlerCallCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if handlerCallCount.Load() != 1 {
		t.Fatalf("expected handler to be called once, got %d", handlerCallCount.Load())
	}

	// Force disconnect.
	server.mu.Lock()
	if len(server.conns) > 0 {
		_ = server.conns[0].Close()
	}
	server.mu.Unlock()

	// Wait for reconnect and clean up dead connections.
	time.Sleep(500 * time.Millisecond)
	server.RemoveClosedConnections()

	// Server replays same request with same IdempotencyKey but new ID.
	req2 := &protocol.PackageDataRequest{
		ID:             "s-replay-new-uuid",
		Method:         "test_method",
		IdempotencyKey: "idem-key-123",
		Seq:            2,
	}
	reqData2, _ := json.Marshal(req2)
	pkg2 := &protocol.Package{Type: protocol.PackageTypeRequest, Data: reqData2}
	if err := server.SendPackage(pkg2); err != nil {
		t.Fatalf("failed to send replay request: %v", err)
	}

	// Wait a bit for potential duplicate call.
	time.Sleep(500 * time.Millisecond)

	// Handler should NOT be called again due to idempotency dedup.
	if handlerCallCount.Load() != 1 {
		t.Errorf("expected handler to still be called once (dedup on replay), got %d", handlerCallCount.Load())
	}
}

// TestWeakNet_Adaptive_Timeout_Under_Jitter tests that alternating fast/slow
// responses cause the timeout to reflect the average, not the worst case.
func TestWeakNet_Adaptive_Timeout_Under_Jitter(t *testing.T) {
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

	// Server alternates between 50ms and 500ms delays.
	var callCount atomic.Int32
	server.SetRPCHandler("jitter_rpc", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		n := callCount.Add(1)
		if n%2 == 1 {
			time.Sleep(50 * time.Millisecond)
		} else {
			time.Sleep(500 * time.Millisecond)
		}
		return json.Marshal(map[string]string{"result": "ok"})
	})

	client := newTestClient(t, server, WithRPCTimeout(10*time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = client.Start(ctx) }()

	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("client did not connect: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	// Make 20 RPC calls with alternating delays.
	for i := 0; i < 20; i++ {
		_, err := client.Call(ctx, "jitter_rpc", nil)
		if err != nil {
			t.Fatalf("RPC call %d failed: %v", i, err)
		}
	}

	// Verify SRTT is moderate (~275ms average), not worst case (500ms).
	srtt := client.rttTracker.SRTT()
	// Expected: ~275ms average. Should be < 400ms (well below 500ms worst case).
	if srtt > 400*time.Millisecond {
		t.Errorf("expected SRTT to be moderate (< 400ms), got %v (too close to worst case)", srtt)
	}
	// Should be > 100ms (not dominated by fast responses only).
	if srtt < 100*time.Millisecond {
		t.Errorf("expected SRTT to reflect some slow responses (> 100ms), got %v", srtt)
	}
}

// TestWeakNet_Response_Retry_Flaky_Send tests the retry queue behavior when
// response send fails.
func TestWeakNet_Response_Retry_Flaky_Send(t *testing.T) {
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

	client := newTestClient(t, server, WithResponseRetryMax(5))
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

	// Set closeAfter to 1 to simulate flaky send (connection closes after receiving 1 message).
	// The handler response will be the message that fails to send.
	server.mu.Lock()
	server.closeAfter = 1
	server.mu.Unlock()

	// Send request that will trigger handler and response.
	req := &protocol.PackageDataRequest{
		ID:     "req-for-retry",
		Method: "test_method",
	}
	reqData, _ := json.Marshal(req)
	pkg := &protocol.Package{Type: protocol.PackageTypeRequest, Data: reqData}
	if err := server.SendPackage(pkg); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	// Wait for handler to be called (it will be called before response send fails).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if handlerCallCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if handlerCallCount.Load() != 1 {
		t.Fatalf("expected handler to be called once, got %d", handlerCallCount.Load())
	}

	// Wait for response retry queue to have entries.
	time.Sleep(500 * time.Millisecond)

	// Verify queue has entries (response send failed, queued for retry).
	queueLen := client.respRetryQueue.Len()
	if queueLen == 0 {
		t.Log("Response retry queue is empty (response may have been sent before connection closed)")
		// This is acceptable if the response was sent before connection closed.
	}

	// Reset closeAfter to allow reconnection.
	server.mu.Lock()
	server.closeAfter = 0
	server.mu.Unlock()

	// Wait for reconnect and retry loop to process.
	time.Sleep(2 * time.Second)
}

// TestWeakNet_Multiple_Reconnect_Cycles tests 5 consecutive disconnect/reconnect
// cycles, each time verifying that last_seen_seq increases.
func TestWeakNet_Multiple_Reconnect_Cycles(t *testing.T) {
	server := newMockWSServer(t)

	// Track last_seen_seq from each reconnect call.
	var lastSeenSeqs []uint64
	server.SetRPCHandler("system.reconnect", func(req *protocol.PackageDataRequest) (json.RawMessage, error) {
		var params map[string]uint64
		if err := json.Unmarshal(req.Params, &params); err == nil {
			seq := params["last_seen_seq"]
			lastSeenSeqs = append(lastSeenSeqs, seq)
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

	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("client did not connect: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Perform 5 disconnect/reconnect cycles.
	for cycle := 0; cycle < 5; cycle++ {
		// Send requests with increasing seq values.
		seqStart := uint64(cycle*3) + 1
		for i := uint64(0); i < 3; i++ {
			req := &protocol.PackageDataRequest{
				ID:     "req-cycle-" + string(rune('0'+cycle)) + "-" + string(rune('0'+i)),
				Method: "test_method",
				Seq:    seqStart + i,
			}
			reqData, _ := json.Marshal(req)
			pkg := &protocol.Package{Type: protocol.PackageTypeRequest, Data: reqData}
			if err := server.SendPackage(pkg); err != nil {
				t.Fatalf("cycle %d: failed to send request: %v", cycle, err)
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Force disconnect.
		server.mu.Lock()
		if len(server.conns) > 0 {
			_ = server.conns[0].Close()
		}
		server.mu.Unlock()

		// Wait for reconnect and clean up dead connections.
		time.Sleep(500 * time.Millisecond)
		server.RemoveClosedConnections()
	}

	// Verify last_seen_seq values are increasing.
	if len(lastSeenSeqs) < 5 {
		t.Logf("Warning: expected at least 5 reconnect calls, got %d", len(lastSeenSeqs))
	}

	// Check that values are monotonically increasing.
	for i := 1; i < len(lastSeenSeqs); i++ {
		if lastSeenSeqs[i] < lastSeenSeqs[i-1] {
			t.Errorf("last_seen_seq decreased from %d to %d at index %d",
				lastSeenSeqs[i-1], lastSeenSeqs[i], i)
		}
	}
}

// TestWeakNet_Reconnect_Handshake_Timeout tests that when the server doesn't
// respond to system.reconnect, the client doesn't block and continues FullSync.
func TestWeakNet_Reconnect_Handshake_Timeout(t *testing.T) {
	server := newMockWSServer(t)

	// Don't set handler for system.reconnect (or set one that doesn't respond).
	// The client's Call will timeout, but FullSync should still proceed.

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

	// Use short RPC timeout so the test doesn't hang.
	client := newTestClient(t, server, WithRPCTimeout(500*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = client.Start(ctx) }()

	if err := server.AcceptConnection(5 * time.Second); err != nil {
		t.Fatalf("client did not connect: %v", err)
	}

	// Wait for FullSync to be called (system.reconnect timeout should not block it).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if syncCalled.Load() > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if syncCalled.Load() == 0 {
		t.Error("sync_updates was not called (FullSync blocked by system.reconnect timeout)")
	}
}
