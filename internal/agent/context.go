package agent

import (
	"context"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// ContextManager provides conversation history for Agent processing.
// It loads messages from the database, caches them, and trims to fit
// the Agent's context window configuration.
type ContextManager interface {
	// GetContext returns the trimmed message history for a conversation,
	// suitable for passing to an LLM as context. Messages are returned
	// in chronological order (oldest first).
	GetContext(ctx context.Context, conversationID string, config *AgentConfig) ([]*model.Message, error)

	// InvalidateCache removes the cached context for a conversation.
	InvalidateCache(conversationID string)
}

// TokenCounter abstracts token counting for testability.
// Implementations range from simple heuristic estimators to precise
// tokenizers like tiktoken-go.
type TokenCounter interface {
	CountTokens(text string) int
}
