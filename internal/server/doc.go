// Package server provides the network server layer for the Xyncra messaging
// system. It defines a Server interface for lifecycle management, a
// ConnectionStore interface for tracking active client connections, and ships
// with a Redis-backed implementation of ConnectionStore.
//
// # Server Lifecycle
//
// A BaseServer manages the lifecycle of a network server. It is created via
// functional options:
//
//	srv, err := server.NewBaseServerFromOptions(
//	    server.WithAddr(":8080"),
//	    server.WithStore(myStore),
//	    server.WithBroker(myBroker),
//	    server.WithConnectionStore(myConnStore),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	// Start blocks until the context is cancelled.
//	go func() {
//	    if err := srv.Start(ctx); err != nil {
//	        log.Fatal(err)
//	    }
//	}()
//
//	// Graceful shutdown.
//	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer stopCancel()
//	if err := srv.GracefulStop(stopCtx); err != nil {
//	    slog.Error("graceful stop error", "error", err)
//	}
//
// # ConnectionStore
//
// The ConnectionStore interface manages active client connections. The
// Redis-backed implementation stores connections as JSON with per-key TTLs
// for automatic expiration.
//
//	cs, err := server.NewRedisConnectionStore(server.RedisConnectionStoreConfig{
//	    Addr:        "localhost:6379",
//	    DB:          0,
//	    KeyPrefix:   "xyncra:conn:",
//	    DefaultTTL:  30 * time.Minute,
//	    PoolSize:    20,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer cs.Close()
//
//	// Register a connection.
//	err = cs.Add(ctx, &server.ConnectionInfo{
//	    ID:         "conn-1",
//	    UserID:     "user-42",
//	    SessionID:  "sess-1",
//	    DeviceID:   "device-abc",
//	    DeviceType: "web",
//	    IPAddress:  "192.168.1.10",
//	    Protocol:   "websocket",
//	    Status:     "active",
//	    Metadata:   map[string]string{"version": "2.0"},
//	})
//
//	// List connections for a user (with limit).
//	conns, err := cs.ListByUser(ctx, "user-42", 100)
//
//	// Patch a connection atomically.
//	err = cs.Patch(ctx, "conn-1", func(ci *server.ConnectionInfo) {
//	    ci.Status = "idle"
//	    ci.LastHeartbeatAt = time.Now()
//	})
//
//	// Count all active connections.
//	total, err := cs.CountAll(ctx)
//
// # Per-user Connection Limits
//
// Set MaxConnectionsPerUser in the config to enforce a limit:
//
//	cs, err := server.NewRedisConnectionStore(server.RedisConnectionStoreConfig{
//	    Addr:                "localhost:6379",
//	    MaxConnectionsPerUser: 5,
//	})
//
// # Error Handling
//
// The package classifies Redis errors into sentinel errors for consistent
// error handling:
//
//	if err := cs.Add(ctx, info); err != nil {
//	    if errors.Is(err, server.ErrRedisConnectionFailed) {
//	        // Handle Redis connectivity failure.
//	    }
//	    if errors.Is(err, server.ErrRedisTimeout) {
//	        // Handle Redis timeout.
//	    }
//	}
//
// # Cross-Node Message Routing
//
// The NodeBroadcaster interface handles cross-node message routing using
// Redis Pub/Sub (D-018). In single-node deployments, the default
// NoopBroadcaster is used (no overhead). In multi-node deployments,
// RedisNodeBroadcaster fans out updates across all nodes.
//
//	srv, _ := server.NewWebSocketServer(
//	    server.WSWithAddr(":8080"),
//	    server.WSWithConnectionStore(connStore),
//	    server.WSWithNodeBroadcaster(nodeBroadcaster),
//	)
//
// # Reverse RPC & Error Propagation
//
// The WebSocketServer supports server-initiated RPC requests to connected
// clients via the ReverseRPC component (D-092). The component is always
// configured (never nil) and is automatically wired to the message handler.
//
// Client.Send() and Client.SendPackage() return one of two errors on failure:
//
//   - ErrClientClosed: the client connection has been closed.
//   - ErrSendBufferFull: the per-client send channel buffer is full.
//
// These errors propagate through the send path:
//
//   - broadcastLocal: logs and skips failed sends; does not return an error
//     (broadcast is best-effort, consistent with D-007 fire-and-forget).
//   - sendToUser: returns nil if at least one send succeeds; returns the last
//     error wrapped as "reverse_rpc: all sends to user %s failed: %w" only
//     when all sends fail. Returns "reverse_rpc: no connections for user %s"
//     when the user has no active connections.
//   - sendToDevice: returns ErrDeviceOffline when the device is not connected;
//     otherwise wraps the Send error as "reverse_rpc: send to device %s: %w".
//
// ReverseRPC.ServerRequest() blocks until a response arrives, the context
// expires, or the timeout elapses. Basic usage:
//
//	resp, err := srv.ServerRequest(ctx, userID, deviceID, "method", params, 30*time.Second)
//	// resp.Msg / resp.Code contain the client's response.
//	// err is non-nil on timeout, cancellation, or send failure.
//
// When deviceID is empty, the request is broadcast to all connections of the
// user (first response wins). When deviceID is non-empty, it is routed to
// that specific device via sendToDevice.
//
// Pending ServerRequest calls are cancelled in two scenarios:
//
//  1. Device replacement (D-095): when a new connection replaces an existing
//     one for the same (userID, deviceID), the old connection's pending
//     requests are failed with reason "device replaced" via CancelDevice.
//  2. Normal disconnect: when a client disconnects and no replacement
//     connection has registered (checked via the hasActiveConn guard on
//     clientsByDevice), all pending requests for that device are failed with
//     reason "device disconnected". If a replacement has already registered,
//     the guard prevents the old connection's cleanup from cancelling the
//     replacement's pending requests.
//
// Both paths use CancelDeviceWithReason, which writes a synthetic response
// (Code=-1, Msg=reason) into the pending respCh. The ServerRequest's select
// picks up the respCh response deterministically; context cancellation is
// handled by the deferred cancel() after ServerRequest returns.
//
// # Pending Store (Phase 4)
//
// The PendingStore interface (D-103) enables timed-out reverse-RPC requests to
// be persisted for later replay. The Redis-backed implementation,
// RedisPendingStore, stores requests as JSON in per-device Redis lists under
// the key "pending:{userID}\x00{deviceID}".
//
// When ServerRequest's context expires with DeadlineExceeded and a PendingStore
// is configured, the request is saved asynchronously via persistAsync. The
// goroutine uses a 5-second background context so that slow Redis does not
// block the caller. Errors during Save are logged but never propagated to the
// ServerRequest caller (fail-open semantics, D-103).
//
// The PackageDataRequest protocol message gained two new fields (D-104):
//
//   - IdempotencyKey: set to the request UUID (D-097) for exactly-once replay.
//   - Seq: per-device monotonically increasing sequence number (D-106).
//
// Both fields use `omitempty` so older clients ignore them without error.
//
// Per-device Seq counters are held in memory (ReverseRPC.deviceSeq). In Phase 5
// they may be upgraded to Redis INCR for cross-node durability (D-106).
//
// Pass a PendingStore via WSWithPendingStore to enable persistence:
//
//	ps, _ := server.NewRedisPendingStore(redisClient, server.PendingStoreConfig{})
//	srv, _ := server.NewWebSocketServer(
//	    server.WSWithPendingStore(ps),
//	    // ... other options
//	)
//
// The WebSocketServer automatically subscribes to Pub/Sub on Start and
// publishes updates via BroadcastUpdates.
package server
