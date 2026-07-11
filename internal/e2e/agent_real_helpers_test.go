//go:build real_llm

// Package e2e_test contains assertion helpers for real LLM E2E tests.
// These helpers use fuzzy assertions because real LLM output is non-deterministic.
package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// waitForAgentMessageInDBErr is like waitForAgentMessageInDB but returns an
// error instead of calling t.Fatal. This allows retryRealLLM to catch failures
// and retry without marking the parent test as failed.
func waitForAgentMessageInDBErr(t *testing.T, env *agentE2EEnv, convID, agentUserID string, timeout time.Duration) (*model.Message, error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("waitForAgentMessageInDB: timed out after %v waiting for agent message from %s in conv %s",
				timeout, agentUserID, convID)
		}

		var messages []*model.Message
		env.db.DB().WithContext(context.Background()).
			Where("conversation_id = ? AND sender_id = ?", convID, agentUserID).
			Order("message_id DESC").
			Limit(1).
			Find(&messages)

		if len(messages) > 0 {
			return messages[0], nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// assertValidAgentReply verifies agent reply structure without checking exact content.
// Checks: non-nil, SenderID matches, ConversationID matches, ID is non-empty,
// MessageID > 0, Type is "text", Content is non-empty.
// Returns an error if any check fails (for use with retryRealLLM).
func assertValidAgentReply(t *testing.T, msg *model.Message, agentUserID, convID string) error {
	t.Helper()
	if msg == nil {
		return fmt.Errorf("agent reply should not be nil")
	}
	var errs []string
	if msg.SenderID != agentUserID {
		errs = append(errs, fmt.Sprintf("sender should be agent: got %q", msg.SenderID))
	}
	if msg.ConversationID != convID {
		errs = append(errs, fmt.Sprintf("conversation ID should match: got %q", msg.ConversationID))
	}
	if msg.ID == "" {
		errs = append(errs, "message ID (string UUID) should not be empty")
	}
	if msg.MessageID == 0 {
		errs = append(errs, "message_id (sequence) should be positive")
	}
	if msg.Type != "text" {
		errs = append(errs, fmt.Sprintf("message type should be text: got %q", msg.Type))
	}
	if msg.Content == "" {
		errs = append(errs, "message content should not be empty")
	}
	if len(errs) > 0 {
		return fmt.Errorf("assertValidAgentReply: %s", strings.Join(errs, "; "))
	}
	return nil
}

// assertContainsAny checks content contains at least one candidate substring (case-insensitive).
// Returns an error if no candidate is found (for use with retryRealLLM).
func assertContainsAny(t *testing.T, content string, candidates []string) error {
	t.Helper()
	lower := strings.ToLower(content)
	for _, c := range candidates {
		if strings.Contains(lower, strings.ToLower(c)) {
			return nil
		}
	}
	return fmt.Errorf("content %q should contain at least one of %v", content, candidates)
}

// assertReplyIsReasonable checks reply is non-empty, not too long, and is text type.
// Returns an error if any check fails (for use with retryRealLLM).
func assertReplyIsReasonable(t *testing.T, content string) error {
	t.Helper()
	if content == "" {
		return fmt.Errorf("reply should not be empty")
	}
	if len(content) > 2000 {
		return fmt.Errorf("reply should not be excessively long: got %d chars", len(content))
	}
	return nil
}

// waitForCondition polls a condition function until it returns true or the
// timeout expires. Returns an error if the condition is not met within the
// timeout. Unlike require.Eventually, this does not mark the test as failed,
// making it suitable for use inside retryRealLLM.
func waitForCondition(timeout, interval time.Duration, condition func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("condition not met within %v", timeout)
}
