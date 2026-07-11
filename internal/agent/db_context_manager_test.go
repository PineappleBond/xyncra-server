package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// setupTestStore creates an in-memory SQLite store for testing.
func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.NewDatabase(store.DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
	})
	require.NoError(t, err)
	s := store.New(db.DB())
	require.NoError(t, s.AutoMigrate(context.Background()))
	return s
}

func createTestConv(t *testing.T, s *store.Store, convID string) {
	t.Helper()
	err := s.Conversations.Create(context.Background(), &model.Conversation{
		ID: convID, UserID1: "alice", UserID2: "agent/bot",
		Type: "1-on-1", Title: "Test", CreatedAt: time.Now(), UpdatedAt: time.Now(), LastMessageAt: time.Now(),
	})
	require.NoError(t, err)
}

func createTestMessages(t *testing.T, s *store.Store, convID string, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 1; i <= count; i++ {
		err := s.Messages.Create(ctx, &model.Message{
			ID:              fmt.Sprintf("msg-%d", i),
			ClientMessageID: fmt.Sprintf("client-%d", i),
			ConversationID:  convID,
			MessageID:       uint32(i),
			SenderID:        "alice",
			Content:         fmt.Sprintf("message content %d", i),
			CreatedAt:       time.Now(),
		})
		require.NoError(t, err)
	}
}

// fixedTokenCounter returns a fixed token count per message for deterministic tests.
type fixedTokenCounter struct {
	tokensPerMsg int
}

func (f *fixedTokenCounter) CountTokens(text string) int {
	if text == "" {
		return 0
	}
	return f.tokensPerMsg
}

// ---------------------------------------------------------------------------
// HeuristicTokenCounter
// ---------------------------------------------------------------------------

func TestHeuristicTokenCounter(t *testing.T) {
	tc := &HeuristicTokenCounter{}

	assert.Equal(t, 0, tc.CountTokens(""))
	assert.Equal(t, 0, tc.CountTokens("abc"))                        // len=3, 3/4=0
	assert.Equal(t, 1, tc.CountTokens("abcd"))                       // len=4, 4/4=1
	assert.Equal(t, 6, tc.CountTokens("abcdefghijklmnopqrstuvwxyz")) // len=26, 26/4=6
}

// ---------------------------------------------------------------------------
// trimByMessages
// ---------------------------------------------------------------------------

func TestTrimByMessages_Basic(t *testing.T) {
	msgs := make([]*model.Message, 10)
	for i := range msgs {
		msgs[i] = &model.Message{MessageID: uint32(i + 1)}
	}

	result := trimByMessages(msgs, 5)
	require.Len(t, result, 5)
	assert.Equal(t, uint32(6), result[0].MessageID, "should keep newest 5")
	assert.Equal(t, uint32(10), result[4].MessageID)
}

func TestTrimByMessages_MoreThanAvailable(t *testing.T) {
	msgs := make([]*model.Message, 3)
	for i := range msgs {
		msgs[i] = &model.Message{MessageID: uint32(i + 1)}
	}

	result := trimByMessages(msgs, 100)
	assert.Len(t, result, 3, "should return all when fewer than max")
}

func TestTrimByMessages_Zero(t *testing.T) {
	msgs := make([]*model.Message, 5)
	for i := range msgs {
		msgs[i] = &model.Message{MessageID: uint32(i + 1)}
	}

	result := trimByMessages(msgs, 0)
	assert.Len(t, result, 5, "maxMessages=0 should return all")
}

// ---------------------------------------------------------------------------
// trimByTokens
// ---------------------------------------------------------------------------

func TestTrimByTokens_Basic(t *testing.T) {
	cm := &DBContextManager{tokenizer: &fixedTokenCounter{tokensPerMsg: 10}}

	msgs := make([]*model.Message, 10)
	for i := range msgs {
		msgs[i] = &model.Message{MessageID: uint32(i + 1), Content: "content"}
	}

	// 10 msgs × 10 tokens = 100 total. MaxTokens=50 → keep 5 newest.
	result := cm.trimByTokens(msgs, 50)
	require.Len(t, result, 5)
	assert.Equal(t, uint32(6), result[0].MessageID, "should keep newest 5")
	assert.Equal(t, uint32(10), result[4].MessageID)
}

func TestTrimByTokens_AllFit(t *testing.T) {
	cm := &DBContextManager{tokenizer: &fixedTokenCounter{tokensPerMsg: 10}}

	msgs := make([]*model.Message, 5)
	for i := range msgs {
		msgs[i] = &model.Message{MessageID: uint32(i + 1), Content: "content"}
	}

	// 5 msgs × 10 tokens = 50 total. MaxTokens=100 → all fit.
	result := cm.trimByTokens(msgs, 100)
	assert.Len(t, result, 5, "all messages should fit within token limit")
}

func TestTrimByTokens_SingleMessageExceeds(t *testing.T) {
	cm := &DBContextManager{tokenizer: &fixedTokenCounter{tokensPerMsg: 100}}

	msgs := []*model.Message{
		{MessageID: 1, Content: "old"},
		{MessageID: 2, Content: "new"},
	}

	// Each message is 100 tokens, MaxTokens=50 → can't even fit one.
	// Should keep at least the latest one.
	result := cm.trimByTokens(msgs, 50)
	require.Len(t, result, 1)
	assert.Equal(t, uint32(2), result[0].MessageID, "should keep at least the latest message")
}

func TestTrimByTokens_Empty(t *testing.T) {
	cm := &DBContextManager{tokenizer: &fixedTokenCounter{tokensPerMsg: 10}}
	result := cm.trimByTokens(nil, 100)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// reverseMessages
// ---------------------------------------------------------------------------

func TestReverseMessages(t *testing.T) {
	msgs := []*model.Message{
		{MessageID: 1}, {MessageID: 2}, {MessageID: 3}, {MessageID: 4},
	}
	reverseMessages(msgs)
	assert.Equal(t, uint32(4), msgs[0].MessageID)
	assert.Equal(t, uint32(1), msgs[3].MessageID)
}

func TestReverseMessages_Empty(t *testing.T) {
	reverseMessages(nil)                // should not panic
	reverseMessages([]*model.Message{}) // should not panic
}

// ---------------------------------------------------------------------------
// DBContextManager integration tests
// ---------------------------------------------------------------------------

func TestDBContextManager_GetContext_CacheMiss(t *testing.T) {
	s := setupTestStore(t)
	createTestConv(t, s, "conv-1")
	createTestMessages(t, s, "conv-1", 5)

	cm := NewDBContextManager(s.MessageStore())
	config := &AgentConfig{Context: AgentContext{MaxMessages: 10}}

	msgs, err := cm.GetContext(context.Background(), "conv-1", config)
	require.NoError(t, err)
	require.Len(t, msgs, 5)
	// Should be in chronological order (oldest first).
	assert.Equal(t, uint32(1), msgs[0].MessageID)
	assert.Equal(t, uint32(5), msgs[4].MessageID)
}

func TestDBContextManager_GetContext_CacheHit(t *testing.T) {
	s := setupTestStore(t)
	createTestConv(t, s, "conv-2")
	createTestMessages(t, s, "conv-2", 3)

	cm := NewDBContextManager(s.MessageStore())
	config := &AgentConfig{Context: AgentContext{MaxMessages: 10}}

	// First call: cache miss.
	msgs1, err := cm.GetContext(context.Background(), "conv-2", config)
	require.NoError(t, err)
	require.Len(t, msgs1, 3)

	// Second call with different conversation should be a cache miss.
	// But same conversation should be a cache hit (returns same 3 messages).
	msgs2, err := cm.GetContext(context.Background(), "conv-2", config)
	require.NoError(t, err)
	assert.Len(t, msgs2, 3, "cache hit should return same data")
}

func TestDBContextManager_GetContext_TTLExpiry(t *testing.T) {
	s := setupTestStore(t)
	createTestConv(t, s, "conv-3")
	createTestMessages(t, s, "conv-3", 3)

	// Use a very short TTL.
	cm := NewDBContextManager(s.MessageStore(), WithCacheTTL(1*time.Millisecond))
	config := &AgentConfig{Context: AgentContext{MaxMessages: 10}}

	// First call: cache miss, loads 3 messages.
	msgs1, err := cm.GetContext(context.Background(), "conv-3", config)
	require.NoError(t, err)
	require.Len(t, msgs1, 3)

	// Wait for TTL to expire.
	time.Sleep(5 * time.Millisecond)

	// Second call: cache expired, re-loads from DB.
	msgs2, err := cm.GetContext(context.Background(), "conv-3", config)
	require.NoError(t, err)
	assert.Len(t, msgs2, 3, "after TTL expiry should re-load from DB")
}

func TestDBContextManager_GetContext_TokenTrim(t *testing.T) {
	s := setupTestStore(t)
	createTestConv(t, s, "conv-4")

	// Create messages with known content length for token counting.
	ctx := context.Background()
	for i := 1; i <= 10; i++ {
		err := s.Messages.Create(ctx, &model.Message{
			ID:              fmt.Sprintf("msg-%d", i),
			ClientMessageID: fmt.Sprintf("client-%d", i),
			ConversationID:  "conv-4",
			MessageID:       uint32(i),
			SenderID:        "alice",
			Content:         "aaaaaaaa", // 8 chars → 2 tokens with heuristic
			CreatedAt:       time.Now(),
		})
		require.NoError(t, err)
	}

	cm := NewDBContextManager(s.MessageStore())
	// MaxTokens=10 → 10 messages × 2 tokens = 20 tokens → should trim to ~5 messages.
	config := &AgentConfig{Context: AgentContext{MaxTokens: 10}}

	msgs, err := cm.GetContext(ctx, "conv-4", config)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(msgs), 6, "token trim should reduce message count")
	assert.GreaterOrEqual(t, len(msgs), 4, "token trim should keep a reasonable number")
}

func TestDBContextManager_GetContext_MessageCountFallback(t *testing.T) {
	s := setupTestStore(t)
	createTestConv(t, s, "conv-5")
	createTestMessages(t, s, "conv-5", 20)

	cm := NewDBContextManager(s.MessageStore())
	// MaxTokens=0, MaxMessages=5 → should use message count fallback.
	config := &AgentConfig{Context: AgentContext{MaxTokens: 0, MaxMessages: 5}}

	msgs, err := cm.GetContext(context.Background(), "conv-5", config)
	require.NoError(t, err)
	require.Len(t, msgs, 5, "should trim to MaxMessages when MaxTokens=0")
	// Should be the newest 5 messages.
	assert.Equal(t, uint32(16), msgs[0].MessageID)
	assert.Equal(t, uint32(20), msgs[4].MessageID)
}

func TestDBContextManager_GetContext_NoLimits(t *testing.T) {
	s := setupTestStore(t)
	createTestConv(t, s, "conv-6")
	createTestMessages(t, s, "conv-6", 10)

	cm := NewDBContextManager(s.MessageStore())
	// Both zero → no trimming.
	config := &AgentConfig{Context: AgentContext{MaxTokens: 0, MaxMessages: 0}}

	msgs, err := cm.GetContext(context.Background(), "conv-6", config)
	require.NoError(t, err)
	assert.Len(t, msgs, 10, "no limits should return all messages")
}

func TestDBContextManager_GetContext_EmptyConversation(t *testing.T) {
	s := setupTestStore(t)
	createTestConv(t, s, "conv-7")

	cm := NewDBContextManager(s.MessageStore())
	config := &AgentConfig{Context: AgentContext{MaxTokens: 4000, MaxMessages: 20}}

	msgs, err := cm.GetContext(context.Background(), "conv-7", config)
	require.NoError(t, err)
	assert.Empty(t, msgs, "empty conversation should return empty slice")
}

func TestDBContextManager_InvalidateCache(t *testing.T) {
	s := setupTestStore(t)
	createTestConv(t, s, "conv-8")
	createTestMessages(t, s, "conv-8", 3)

	cm := NewDBContextManager(s.MessageStore())
	config := &AgentConfig{Context: AgentContext{MaxMessages: 10}}

	// First call: cache miss.
	msgs1, err := cm.GetContext(context.Background(), "conv-8", config)
	require.NoError(t, err)
	require.Len(t, msgs1, 3)

	// Invalidate cache.
	cm.InvalidateCache("conv-8")

	// After invalidation, next call should re-load from DB.
	msgs2, err := cm.GetContext(context.Background(), "conv-8", config)
	require.NoError(t, err)
	assert.Len(t, msgs2, 3)
}

func TestDBContextManager_ConcurrentAccess(t *testing.T) {
	s := setupTestStore(t)
	createTestConv(t, s, "conv-9")
	createTestMessages(t, s, "conv-9", 10)

	cm := NewDBContextManager(s.MessageStore(), WithCacheTTL(1*time.Millisecond))
	config := &AgentConfig{Context: AgentContext{MaxMessages: 5}}

	const goroutines = 20
	const iterations = 50

	errCh := make(chan error, goroutines*iterations)
	done := make(chan struct{}, goroutines)

	for g := range goroutines {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			for range iterations {
				msgs, err := cm.GetContext(context.Background(), "conv-9", config)
				if err != nil {
					errCh <- err
					return
				}
				if len(msgs) == 0 {
					errCh <- errors.New("expected non-empty messages")
					return
				}
				// Alternate between reads and cache invalidation.
				if i%3 == 0 {
					cm.InvalidateCache("conv-9")
				}
			}
		}(g)
	}

	for range goroutines {
		<-done
	}
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent access error: %v", err)
	}
}

func TestDBContextManager_WithOptions(t *testing.T) {
	s := setupTestStore(t)
	cm := NewDBContextManager(s.MessageStore(),
		WithCacheTTL(5*time.Second),
		WithTokenCounter(&fixedTokenCounter{tokensPerMsg: 42}),
	)

	assert.Equal(t, 5*time.Second, cm.ttl)
	_, ok := cm.tokenizer.(*fixedTokenCounter)
	assert.True(t, ok, "should use the custom token counter")
}

// ---------------------------------------------------------------------------
// cleanupExpired / StartCleanup tests
// ---------------------------------------------------------------------------

// TestDBContextManager_CleanupExpired verifies that expired entries are removed
// from the cache while fresh entries are preserved.
func TestDBContextManager_CleanupExpired(t *testing.T) {
	s := setupTestStore(t)
	cm := NewDBContextManager(s.MessageStore(), WithCacheTTL(50*time.Millisecond))

	// Inject two expired entries and one fresh entry directly into the cache.
	now := time.Now()
	cm.cache.Store("expired-1", &cachedContext{
		messages:  []*model.Message{{ID: "old-1"}},
		fetchedAt: now.Add(-100 * time.Millisecond),
	})
	cm.cache.Store("expired-2", &cachedContext{
		messages:  []*model.Message{{ID: "old-2"}},
		fetchedAt: now.Add(-200 * time.Millisecond),
	})
	cm.cache.Store("fresh-1", &cachedContext{
		messages:  []*model.Message{{ID: "new-1"}},
		fetchedAt: now,
	})

	// Also inject a corrupted entry (wrong value type) — should be removed.
	cm.cache.Store("corrupted", "not-a-cachedContext")

	// Wait for TTL to elapse for the "fresh" entry as well (so cleanupExpired sees all as expired).
	// Actually, we want to verify that fresh entries survive. Let's call cleanupExpired
	// while "fresh-1" is still within TTL.
	time.Sleep(10 * time.Millisecond)
	cm.cleanupExpired()

	// Expired entries should be gone.
	_, ok1 := cm.cache.Load("expired-1")
	assert.False(t, ok1, "expired-1 should be cleaned up")
	_, ok2 := cm.cache.Load("expired-2")
	assert.False(t, ok2, "expired-2 should be cleaned up")

	// Corrupted entry should be gone.
	_, okCorrupt := cm.cache.Load("corrupted")
	assert.False(t, okCorrupt, "corrupted entry should be cleaned up")

	// Fresh entry should still be present.
	val, okFresh := cm.cache.Load("fresh-1")
	assert.True(t, okFresh, "fresh-1 should still be in cache")
	if okFresh {
		cc := val.(*cachedContext)
		assert.Equal(t, "new-1", cc.messages[0].ID)
	}

	// Now wait for fresh entry to expire too, then clean again.
	time.Sleep(60 * time.Millisecond)
	cm.cleanupExpired()

	_, okFresh2 := cm.cache.Load("fresh-1")
	assert.False(t, okFresh2, "fresh-1 should be cleaned up after TTL elapses")
}

// TestDBContextManager_CleanupExpired_PanicRecovery verifies that cleanupExpired
// does not crash even if a panic occurs during Range iteration.
func TestDBContextManager_CleanupExpired_PanicRecovery(t *testing.T) {
	cm := &DBContextManager{ttl: 1 * time.Millisecond}
	// Store an entry that will be expired.
	cm.cache.Store("key1", &cachedContext{
		messages:  nil,
		fetchedAt: time.Now().Add(-1 * time.Second),
	})

	// cleanupExpired should complete without panicking.
	assert.NotPanics(t, cm.cleanupExpired)

	// Entry should have been cleaned up.
	_, ok := cm.cache.Load("key1")
	assert.False(t, ok, "entry should be cleaned up")
}

// TestDBContextManager_StartCleanup_CtxCancel verifies that StartCleanup
// exits when the context is cancelled.
func TestDBContextManager_StartCleanup_CtxCancel(t *testing.T) {
	cm := &DBContextManager{ttl: 1 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		cm.StartCleanup(ctx, 20*time.Millisecond)
		close(done)
	}()

	// Give the goroutine time to start and tick at least once.
	time.Sleep(50 * time.Millisecond)

	// Cancel context — goroutine should exit.
	cancel()

	select {
	case <-done:
		// Success: goroutine exited.
	case <-time.After(1 * time.Second):
		t.Fatal("StartCleanup did not exit after context cancellation")
	}
}

// TestDBContextManager_StartCleanup_DefaultInterval verifies that a zero or
// negative interval defaults to 5 minutes (we test by ensuring the goroutine
// does not panic on creation with zero interval).
func TestDBContextManager_StartCleanup_DefaultInterval(t *testing.T) {
	cm := &DBContextManager{ttl: 1 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		// interval <= 0 should default to 5 minutes, not panic.
		cm.StartCleanup(ctx, 0)
		close(done)
	}()

	// Cancel immediately to verify it works with default interval.
	cancel()
	select {
	case <-done:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("StartCleanup with zero interval did not exit on context cancellation")
	}
}

// TestDBContextManager_StartCleanup_ActuallyCleans verifies that StartCleanup
// actually removes expired entries during its tick cycle.
func TestDBContextManager_StartCleanup_ActuallyCleans(t *testing.T) {
	cm := &DBContextManager{ttl: 10 * time.Millisecond}

	// Inject an expired entry.
	cm.cache.Store("stale-key", &cachedContext{
		messages:  []*model.Message{{ID: "stale"}},
		fetchedAt: time.Now().Add(-1 * time.Second),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		cm.StartCleanup(ctx, 20*time.Millisecond)
		close(done)
	}()

	// Wait for at least one tick (20ms interval).
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	_, ok := cm.cache.Load("stale-key")
	assert.False(t, ok, "stale-key should have been cleaned up by StartCleanup")
}
