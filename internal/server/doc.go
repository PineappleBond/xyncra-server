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
//	    log.Printf("graceful stop error: %v", err)
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
// The WebSocketServer automatically subscribes to Pub/Sub on Start and
// publishes updates via BroadcastUpdates.
package server
