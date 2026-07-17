package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/google/uuid"
)

// benchStore creates an isolated in-memory SQLite store for benchmark use.
func benchStore(b *testing.B) *store.Store {
	b.Helper()
	db, err := store.NewDatabase(store.DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:bench_%s?mode=memory&cache=shared", b.Name()),
	})
	if err != nil {
		b.Fatalf("failed to open sqlite: %v", err)
	}
	s := store.New(db.DB())
	ctx := context.Background()
	if err := s.AutoMigrate(ctx); err != nil {
		b.Fatalf("auto migrate failed: %v", err)
	}
	return s
}

// BenchmarkSendMessage measures the end-to-end throughput of the send_message
// handler: JSON parse, idempotency check, conversation lookup, message persist,
// user update fan-out, and MQ enqueue.
func BenchmarkSendMessage(b *testing.B) {
	b.StopTimer()

	s := benchStore(b)
	broker := &mockBroker{}
	handler := NewSendMessageHandler(s, broker, nil, nil)
	ctx := context.Background()

	// Pre-create a conversation between two users.
	convID := "bench-conv"
	now := time.Now()
	err := s.ConversationStore().Create(ctx, &model.Conversation{
		ID:        convID,
		UserID1:   "alice",
		UserID2:   "bob",
		Type:      "1-on-1",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		b.Fatalf("create conversation: %v", err)
	}

	client := server.NewTestClient("alice")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		params := map[string]interface{}{
			"conversation_id":   convID,
			"client_message_id": uuid.New().String(),
			"content":           fmt.Sprintf("benchmark message %d", i),
			"type":              "text",
		}
		req := newTestRequest(fmt.Sprintf("bench-req-%d", i), "send_message", params)
		if _, err := handler.HandleRequest(ctx, client, req); err != nil {
			b.Fatalf("send_message failed: %v", err)
		}
	}
}

// BenchmarkSyncUpdates measures the throughput of the sync_updates handler
// across varying numbers of pre-filled user update records.
func BenchmarkSyncUpdates(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("updates=%d", count), func(b *testing.B) {
			b.StopTimer()

			s := benchStore(b)
			handler := NewSyncUpdatesHandler(s)
			ctx := context.Background()
			userID := "bench-user"
			now := time.Now()

			// Pre-fill user update records.
			updates := make([]model.UserUpdate, count)
			for i := 0; i < count; i++ {
				updates[i] = model.UserUpdate{
					ID:        uuid.New().String(),
					UserID:    userID,
					Seq:       uint32(i + 1),
					Payload:   []byte(fmt.Sprintf(`{"msg":"update-%d"}`, i)),
					CreatedAt: now.Add(time.Duration(i) * time.Millisecond),
				}
			}
			if err := s.UserUpdateStore().Create(ctx, updates); err != nil {
				b.Fatalf("seed user updates: %v", err)
			}

			client := server.NewTestClient(userID)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				params := map[string]interface{}{
					"after_seq": 0,
					"limit":     100,
				}
				req := newTestRequest(fmt.Sprintf("bench-sync-%d", i), "sync_updates", params)
				if _, err := handler.HandleRequest(ctx, client, req); err != nil {
					b.Fatalf("sync_updates failed: %v", err)
				}
			}
		})
	}
}
