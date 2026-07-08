package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/google/uuid"
)

// benchSQLite creates an in-memory SQLite database with AutoMigrate applied and
// returns a Store. Each call returns an isolated database so benchmarks do not
// interfere with one another.
func benchSQLite(b *testing.B) *Store {
	b.Helper()
	db, err := NewDatabase(DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:%s?mode=memory&cache=shared", b.Name()),
	})
	if err != nil {
		b.Fatalf("failed to open sqlite: %v", err)
	}
	s := New(db.DB())
	ctx := context.Background()
	if err := s.AutoMigrate(ctx); err != nil {
		b.Fatalf("auto migrate failed: %v", err)
	}
	return s
}

// BenchmarkMessageStore_Create measures single-message insert throughput.
func BenchmarkMessageStore_Create(b *testing.B) {
	s := benchSQLite(b)
	ctx := context.Background()

	// Create a conversation to satisfy the foreign-key relationship implied by
	// the conversation_id column.
	convID := "bench-conv"
	now := time.Now()
	err := s.ConversationStore().Create(ctx, &model.Conversation{
		ID:        convID,
		UserID1:   "user-a",
		UserID2:   "user-b",
		Type:      "1-on-1",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		b.Fatalf("create conversation: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := &model.Message{
			ID:              uuid.New().String(),
			ClientMessageID: uuid.New().String(),
			ConversationID:  convID,
			MessageID:       uint32(i + 1),
			SenderID:        "user-a",
			Content:         "benchmark message payload",
			Type:            "text",
			Status:          "sent",
			CreatedAt:       now,
		}
		if err := s.MessageStore().Create(ctx, msg); err != nil {
			b.Fatalf("create message: %v", err)
		}
	}
}

// BenchmarkMessageStore_ListByConversation measures list query throughput with
// pre-filled messages. The query fetches the first page of 50 messages starting
// after message ID 0.
func BenchmarkMessageStore_ListByConversation(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("msgs=%d", count), func(b *testing.B) {
			s := benchSQLite(b)
			ctx := context.Background()
			now := time.Now()

			convID := "bench-conv"
			err := s.ConversationStore().Create(ctx, &model.Conversation{
				ID:        convID,
				UserID1:   "user-a",
				UserID2:   "user-b",
				Type:      "1-on-1",
				CreatedAt: now,
				UpdatedAt: now,
			})
			if err != nil {
				b.Fatalf("create conversation: %v", err)
			}

			// Pre-fill messages.
			msgs := make([]*model.Message, count)
			for i := 0; i < count; i++ {
				msgs[i] = &model.Message{
					ID:              uuid.New().String(),
					ClientMessageID: uuid.New().String(),
					ConversationID:  convID,
					MessageID:       uint32(i + 1),
					SenderID:        "user-a",
					Content:         fmt.Sprintf("message-%d", i),
					Type:            "text",
					Status:          "sent",
					CreatedAt:       now,
				}
			}
			// Insert in batches to avoid hitting SQLite limits.
			for i := 0; i < count; i += 100 {
				end := i + 100
				if end > count {
					end = count
				}
				for j := i; j < end; j++ {
					if err := s.MessageStore().Create(ctx, msgs[j]); err != nil {
						b.Fatalf("seed message %d: %v", j, err)
					}
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := s.MessageStore().ListByConversation(ctx, convID, 0, 50)
				if err != nil {
					b.Fatalf("list messages: %v", err)
				}
			}
		})
	}
}

// BenchmarkUserUpdateStore_Create measures batch-insert throughput for user
// update records at different batch sizes.
func BenchmarkUserUpdateStore_Create(b *testing.B) {
	for _, batchSize := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			s := benchSQLite(b)
			ctx := context.Background()
			now := time.Now()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				updates := make([]model.UserUpdate, batchSize)
				for j := 0; j < batchSize; j++ {
					updates[j] = model.UserUpdate{
						ID:        uuid.New().String(),
						UserID:    fmt.Sprintf("user-%d", i*batchSize+j),
						Seq:       uint32(j + 1),
						Payload:   []byte(`{"bench":"data"}`),
						CreatedAt: now,
					}
				}
				if err := s.UserUpdateStore().Create(ctx, updates); err != nil {
					b.Fatalf("create user updates: %v", err)
				}
			}
		})
	}
}

// BenchmarkConversationStore_GetByUser measures the paginated conversation
// list query with pre-filled conversations.
func BenchmarkConversationStore_GetByUser(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("convs=%d", count), func(b *testing.B) {
			s := benchSQLite(b)
			ctx := context.Background()
			now := time.Now()
			userID := "bench-user"

			// Pre-fill conversations. Each conversation uses a unique peer user
			// so that the user_id1/user_id2 indexes are exercised.
			for i := 0; i < count; i++ {
				conv := &model.Conversation{
					ID:            uuid.New().String(),
					UserID1:       userID,
					UserID2:       fmt.Sprintf("peer-%d", i),
					Type:          "1-on-1",
					CreatedAt:     now,
					UpdatedAt:     now,
					LastMessageAt: now.Add(time.Duration(i) * time.Millisecond),
				}
				if err := s.ConversationStore().Create(ctx, conv); err != nil {
					b.Fatalf("seed conversation %d: %v", i, err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := s.ConversationStore().GetByUser(ctx, userID, 0, 20)
				if err != nil {
					b.Fatalf("get conversations by user: %v", err)
				}
			}
		})
	}
}
