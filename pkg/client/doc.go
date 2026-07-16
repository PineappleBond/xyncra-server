// Package client implements the Xyncra client communication layer, providing
// a high-level API for connecting to a Xyncra server over WebSocket and
// synchronizing conversational data.
//
// # Overview
//
// The package is organized around [XyncraClient], which manages the full
// lifecycle of a client connection:
//
//   - WebSocket connection management with automatic reconnection and
//     exponential backoff (connectionManager)
//   - JSON-RPC request/response dispatch with correlation matching
//   - Incremental data synchronization via sync_updates with gap detection
//     and debounced pull (syncManager)
//   - Retry queue for failed RPC calls with configurable backoff (retryManager)
//
// # Usage
//
// Create a client with functional options and call Start:
//
//	db, _ := store.New("xyncra.db")
//	client, _ := client.New(
//	    client.WithServerURL("ws://localhost:8080/ws"),
//	    client.WithUserID("alice"),
//	    client.WithDB(db),
//	)
//	go client.Start(ctx)
//	defer client.Stop()
//
// # Error Codes
//
// Client-specific error codes extend the server's range (D-027):
//
//   - -400 ConnectionError: WebSocket connection failure
//   - -401 TimeoutError: RPC call timeout
//   - -402 SyncError: Data synchronization failure
//
// # Protocol
//
// All communication uses the WebSocket protocol defined in pkg/protocol.
// Messages are Package envelopes containing typed Data payloads:
//
//   - Request (Type=0): client-initiated RPC calls
//   - Response (Type=1): server replies correlated by request ID
//   - Updates (Type=2): push notifications with incremental data changes
//
// # Phase 8 Enhancements
//
// The following client-side enhancements provide resilience under weak or
// intermittent network conditions:
//
//   - IdempotencyCache: LRU dedup cache for replayed server-initiated requests,
//     preventing duplicate handler invocations after reconnect.
//   - RTTTracker: adaptive RPC timeout based on a sliding window of round-trip
//     time samples (trimmed-mean SRTT), automatically adjusting to network
//     conditions.
//   - ResponseRetryQueue: queues responses that failed to send (e.g. due to a
//     mid-flight disconnect) and retries them with exponential backoff.
//   - Reconnect handshake: on each (re)connect the client sends a
//     system.reconnect request carrying last_seen_seq and re-registers its
//     function handlers, enabling the server to replay missed requests.
//
// # Lifecycle (D-111)
//
//   - Start(ctx) blocks until the client stops (context cancellation, Stop()
//     call, or 4001 replacement).
//   - Stop() cancels the internal context and triggers async shutdown. Safe
//     to call multiple times.
//   - Done() returns a channel that is closed when the client has fully
//     stopped.
//   - On 4001 Close Frame (device replaced), the client cancels its context
//     and exits gracefully. The monitor goroutine returns, Start() unblocks,
//     shutdown() runs, and Done() closes.
//   - Shutdown is protected by sync.Once, ensuring cleanup runs exactly once
//     regardless of caller.
package client
