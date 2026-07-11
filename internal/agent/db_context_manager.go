package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// cachedContext holds a cached context result with its fetch timestamp.
type cachedContext struct {
	messages  []*model.Message
	fetchedAt time.Time
}

// DBContextManager implements ContextManager using the database for storage
// and sync.Map for in-memory caching with TTL-based expiry.
type DBContextManager struct {
	messageStore *store.MessageStore
	cache        sync.Map // conversationID -> *cachedContext
	ttl          time.Duration
	tokenizer    TokenCounter
}

// DBContextOption configures a DBContextManager.
type DBContextOption func(*DBContextManager)

// WithCacheTTL sets the cache time-to-live duration.
func WithCacheTTL(d time.Duration) DBContextOption {
	return func(cm *DBContextManager) { cm.ttl = d }
}

// WithTokenCounter sets the token counting implementation.
func WithTokenCounter(tc TokenCounter) DBContextOption {
	return func(cm *DBContextManager) { cm.tokenizer = tc }
}

// NewDBContextManager creates a DBContextManager backed by the given MessageStore.
func NewDBContextManager(messageStore *store.MessageStore, opts ...DBContextOption) *DBContextManager {
	cm := &DBContextManager{
		messageStore: messageStore,
		ttl:          30 * time.Second,
		tokenizer:    &HeuristicTokenCounter{},
	}
	for _, opt := range opts {
		opt(cm)
	}
	return cm
}

// GetContext returns the trimmed message history for a conversation.
//
// The method checks an in-memory cache first (TTL-based expiry). On cache miss,
// it loads recent messages from the database, applies type filtering, and trims
// by token count (falling back to message count if MaxTokens is zero).
//
// Messages are returned in chronological order (oldest first).
func (cm *DBContextManager) GetContext(ctx context.Context, conversationID string, config *AgentConfig) ([]*model.Message, error) {
	// 1. Check cache.
	if cached, ok := cm.cache.Load(conversationID); ok {
		cc := cached.(*cachedContext)
		if time.Since(cc.fetchedAt) < cm.ttl {
			return cc.messages, nil
		}
	}

	// 2. Determine fetch limit: fetch extra for trimming, at least 100.
	fetchLimit := max(config.Context.MaxMessages*2, 100)

	// 3. Load from database.
	messages, err := cm.messageStore.ListRecentByConversation(ctx, conversationID, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrContextLoad, err)
	}

	// 4. Apply message type filter (passthrough in Phase 2).
	messages = defaultMessageFilter(messages)

	// 5. Reverse to chronological order (DB returns newest first).
	reverseMessages(messages)

	// 6. Trim by token count or message count.
	if config.Context.MaxTokens > 0 {
		messages = cm.trimByTokens(messages, config.Context.MaxTokens)
	} else if config.Context.MaxMessages > 0 {
		messages = trimByMessages(messages, config.Context.MaxMessages)
	}

	// 7. Update cache.
	cm.cache.Store(conversationID, &cachedContext{
		messages:  messages,
		fetchedAt: time.Now(),
	})

	return messages, nil
}

// InvalidateCache removes the cached context for a conversation.
func (cm *DBContextManager) InvalidateCache(conversationID string) {
	cm.cache.Delete(conversationID)
}

// trimByTokens removes the oldest messages until total token count is within
// maxTokens. Messages are processed from newest to oldest. The returned slice
// preserves chronological order (oldest first). At least one message is always
// returned, even if it alone exceeds the token limit.
func (cm *DBContextManager) trimByTokens(messages []*model.Message, maxTokens int) []*model.Message {
	if len(messages) == 0 {
		return messages
	}

	// Accumulate tokens from newest to oldest.
	totalTokens := 0
	cutoff := len(messages) // default: nothing fits yet

	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := cm.tokenizer.CountTokens(messages[i].Content)
		if totalTokens+msgTokens > maxTokens {
			break
		}
		totalTokens += msgTokens
		cutoff = i
	}

	// If nothing fit (cutoff still == len), keep at least the latest.
	if cutoff == len(messages) {
		return messages[len(messages)-1:]
	}

	return messages[cutoff:]
}

// trimByMessages returns at most maxMessages most recent messages,
// preserving chronological order (oldest first).
func trimByMessages(messages []*model.Message, maxMessages int) []*model.Message {
	if maxMessages <= 0 || len(messages) <= maxMessages {
		return messages
	}
	return messages[len(messages)-maxMessages:]
}

// defaultMessageFilter is a passthrough filter for Phase 2.
// Future phases will filter by message type (user/assistant/summary/tool_call/tool_result).
func defaultMessageFilter(msgs []*model.Message) []*model.Message {
	return msgs
}

// reverseMessages reverses a slice of messages in place.
func reverseMessages(msgs []*model.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}
