//go:build real_llm

// Package e2e_test provides Client DB assertion helpers that depend on
// fullChainUpdateHandler (defined in fullchain_e2e_test.go under the real_llm
// build tag).
package e2e_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// requireClientDBHasConversation verifies a conversation exists in the Client
// DB handler. It checks the conversations slice recorded by the
// fullChainUpdateHandler.
func requireClientDBHasConversation(t *testing.T, handler *fullChainUpdateHandler, convID string) {
	t.Helper()

	handler.mu.Lock()
	defer handler.mu.Unlock()

	for _, conv := range handler.conversations {
		if conv.ID == convID {
			return
		}
	}

	require.Fail(t, "client DB: conversation %s not found in handler (recorded %d conversations)",
		convID, len(handler.conversations))
}

// requireClientDBHasMessage verifies that a message containing
// contentSubstring exists in the Client DB handler. It searches the messages
// slice recorded by the fullChainUpdateHandler.
func requireClientDBHasMessage(t *testing.T, handler *fullChainUpdateHandler, convID string, contentSubstring string) {
	t.Helper()

	handler.mu.Lock()
	defer handler.mu.Unlock()

	for _, msg := range handler.messages {
		if msg.ConversationID == convID && strings.Contains(msg.Content, contentSubstring) {
			return
		}
	}

	require.Fail(t, "client DB: no message containing %q found in conversation %s (searched %d messages)",
		contentSubstring, convID, len(handler.messages))
}

// requireClientDBMessageCount verifies the message count in a conversation
// meets or exceeds minExpected. It checks the messages slice recorded by the
// fullChainUpdateHandler.
func requireClientDBMessageCount(t *testing.T, handler *fullChainUpdateHandler, convID string, minExpected int) {
	t.Helper()

	handler.mu.Lock()
	defer handler.mu.Unlock()

	count := 0
	for _, msg := range handler.messages {
		if msg.ConversationID == convID {
			count++
		}
	}

	require.GreaterOrEqual(t, count, minExpected,
		"client DB: conversation %s should have at least %d message(s), got %d",
		convID, minExpected, count)
}

// waitForClientDBMessage waits until at least one message appears in the Client
// DB handler for the given conversation, or the timeout expires. Uses the
// handler's messageCh for efficient waiting instead of polling.
func waitForClientDBMessage(t *testing.T, handler *fullChainUpdateHandler, convID string, timeout time.Duration) {
	t.Helper()

	// Fast path: message already present.
	handler.mu.Lock()
	for _, msg := range handler.messages {
		if msg.ConversationID == convID {
			handler.mu.Unlock()
			return
		}
	}
	handler.mu.Unlock()

	// Slow path: wait for a signal on messageCh.
	deadline := time.After(timeout)
	for {
		select {
		case <-handler.messageCh:
			handler.mu.Lock()
			for _, msg := range handler.messages {
				if msg.ConversationID == convID {
					handler.mu.Unlock()
					return
				}
			}
			handler.mu.Unlock()
		case <-deadline:
			require.Fail(t, fmt.Sprintf(
				"client DB: timed out after %v waiting for message in conversation %s", timeout, convID))
			return
		}
	}
}

// waitForClientDBConversation waits until a conversation appears in the Client
// DB handler, or the timeout expires. Uses the handler's conversationCh for
// efficient waiting instead of polling.
func waitForClientDBConversation(t *testing.T, handler *fullChainUpdateHandler, convID string, timeout time.Duration) {
	t.Helper()

	// Fast path: conversation already present.
	handler.mu.Lock()
	for _, conv := range handler.conversations {
		if conv.ID == convID {
			handler.mu.Unlock()
			return
		}
	}
	handler.mu.Unlock()

	// Slow path: wait for a signal on conversationCh.
	deadline := time.After(timeout)
	for {
		select {
		case <-handler.conversationCh:
			handler.mu.Lock()
			for _, conv := range handler.conversations {
				if conv.ID == convID {
					handler.mu.Unlock()
					return
				}
			}
			handler.mu.Unlock()
		case <-deadline:
			require.Fail(t, fmt.Sprintf(
				"client DB: timed out after %v waiting for conversation %s", timeout, convID))
			return
		}
	}
}
