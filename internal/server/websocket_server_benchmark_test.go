package server

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// BenchmarkBroadcastUpdates measures the throughput of BroadcastUpdates across
// varying numbers of concurrent client connections registered to the same user.
// It exercises the JSON marshal, per-user map lookup, and per-client SendPackage
// path without requiring a real WebSocket connection or a running server.
func BenchmarkBroadcastUpdates(b *testing.B) {
	for _, numConns := range []int{1, 10, 50, 100, 500} {
		b.Run(fmt.Sprintf("conns=%d", numConns), func(b *testing.B) {
			b.StopTimer()

			memStore := NewMemoryConnectionStore(0)
			srv, err := NewWebSocketServer(
				WSWithAddr(":0"),
				WSWithConnectionStore(memStore),
				WSWithStore(&mockStore{}),
				WSWithBroker(&mockBroker{}),
			)
			if err != nil {
				b.Fatalf("NewWebSocketServer: %v", err)
			}

			// Register fake clients directly into the server's internal maps.
			// Each client has a buffered send channel so SendPackage does not
			// block; once the buffer fills, messages are silently dropped.
			userID := "bench-user"
			for i := 0; i < numConns; i++ {
				connID := fmt.Sprintf("bench-conn-%d", i)
				client := &Client{
					userID: userID,
					connID: connID,
					send:   make(chan []byte, 256),
				}
				srv.mu.Lock()
				srv.clients[connID] = client
				if srv.clientsByUser[userID] == nil {
					srv.clientsByUser[userID] = make(map[string]*Client)
				}
				srv.clientsByUser[userID][connID] = client
				srv.mu.Unlock()
			}

			updates := &protocol.PackageDataUpdates{
				Updates: []protocol.PackageDataUpdate{
					{
						Seq:     1,
						Payload: json.RawMessage(`{"test":"benchmark-data"}`),
					},
				},
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = srv.BroadcastUpdates(userID, updates)
			}
		})
	}
}
