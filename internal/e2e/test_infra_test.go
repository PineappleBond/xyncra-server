// Package e2e_test contains test infrastructure shared across all E2E tests.
// This file provides channel-based waiting, structured logging, timeout
// constants, and assertion helpers for Server DB and Redis verification.
//
// No build tag — available to all tests.
package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/store"
)

// ---------------------------------------------------------------------------
// Timeout constants
// ---------------------------------------------------------------------------

const (
	// fastTimeout is used for Redis operations and DB queries.
	fastTimeout = 5 * time.Second

	// normalTimeout is used for WebSocket message waiting.
	normalTimeout = 15 * time.Second

	// agentTimeout is used for agent processing with mock LLM.
	agentTimeout = 30 * time.Second

	// realLLMTimeout is used for agent processing with a real LLM provider.
	realLLMTimeout = 60 * time.Second

	// mqTimeout is used for MQ task completion waiting.
	mqTimeout = 20 * time.Second
)

// ---------------------------------------------------------------------------
// channelWaiter — generic channel-based waiting for async events
// ---------------------------------------------------------------------------

// channelWaiter provides generic channel-based waiting for async events.
// Use this instead of time.Sleep or require.Eventually for MQ-driven updates.
type channelWaiter struct {
	ch   chan struct{}
	name string
}

// newChannelWaiter creates a new channelWaiter with the given name and buffer
// size. The buffer size determines how many signals can be queued without
// blocking the sender.
func newChannelWaiter(name string, bufSize int) *channelWaiter {
	return &channelWaiter{
		ch:   make(chan struct{}, bufSize),
		name: name,
	}
}

// wait blocks until n signals have been received on the channel or the timeout
// expires. Returns an error wrapping the waiter name on timeout.
func (w *channelWaiter) wait(n int, timeout time.Duration) error {
	deadline := time.After(timeout)
	for i := 0; i < n; i++ {
		select {
		case <-w.ch:
			// Signal received, continue waiting for the next one.
		case <-deadline:
			return fmt.Errorf("channelWaiter(%s): timed out after %v waiting for %d signal(s), got %d",
				w.name, timeout, n, i)
		}
	}
	return nil
}

// signal performs a non-blocking send on the channel. If the channel buffer is
// full the signal is silently dropped, preventing the caller from blocking.
func (w *channelWaiter) signal() {
	select {
	case w.ch <- struct{}{}:
	default:
	}
}

// ---------------------------------------------------------------------------
// testStepLogger — step-based test logging with checkpoint support
// ---------------------------------------------------------------------------

// testStepLogger provides step-based test logging with checkpoint support.
// Each call to Step increments the step counter and logs a section header.
// Checkpoint and FailCheckpoint log structured verification lines.
type testStepLogger struct {
	t    *testing.T
	step int
}

// newTestStepLogger creates a testStepLogger bound to the given testing.T.
func newTestStepLogger(t *testing.T) *testStepLogger {
	t.Helper()
	return &testStepLogger{t: t}
}

// Step logs a new test step header and increments the internal step counter.
func (l *testStepLogger) Step(name string) {
	l.t.Helper()
	l.step++
	l.t.Logf("=== STEP %d: %s ===", l.step, name)
}

// Checkpoint logs a successful verification checkpoint with the given name,
// layer description, and details.
func (l *testStepLogger) Checkpoint(name string, layer string, details string) {
	l.t.Helper()
	l.t.Logf("[CHECKPOINT] %s | %s | %s", name, layer, details)
}

// FailCheckpoint logs a failed verification checkpoint with the given name,
// layer description, and error.
func (l *testStepLogger) FailCheckpoint(name string, layer string, err error) {
	l.t.Helper()
	l.t.Logf("[FAIL-CHECKPOINT] %s | %s | %v", name, layer, err)
}

// ---------------------------------------------------------------------------
// Server DB assertion helpers
// ---------------------------------------------------------------------------

// requireServerDBConversationExists verifies a conversation exists in the
// Server DB. It fails the test immediately if the conversation is not found or
// if a database error occurs.
func requireServerDBConversationExists(t *testing.T, s *store.Store, convID string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
	defer cancel()

	conv, err := s.ConversationStore().Get(ctx, convID)
	require.NoError(t, err, "server DB: query conversation %s", convID)
	require.NotNil(t, conv, "server DB: conversation %s should exist", convID)
	require.Equal(t, convID, conv.ID, "server DB: conversation ID should match")
}

// requireServerDBMessageCount verifies the message count in a conversation
// matches expected. It uses ListRecentByConversation with a high limit and
// checks the returned slice length.
func requireServerDBMessageCount(t *testing.T, s *store.Store, convID string, expected int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
	defer cancel()

	msgs, err := s.MessageStore().ListRecentByConversation(ctx, convID, 500)
	require.NoError(t, err, "server DB: list messages for conversation %s", convID)
	require.Equal(t, expected, len(msgs),
		"server DB: conversation %s should have %d message(s), got %d", convID, expected, len(msgs))
}

// requireServerDBHasMessage verifies that a message containing
// contentSubstring exists in the given conversation. It searches the most
// recent 500 messages.
func requireServerDBHasMessage(t *testing.T, s *store.Store, convID string, contentSubstring string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
	defer cancel()

	msgs, err := s.MessageStore().ListRecentByConversation(ctx, convID, 500)
	require.NoError(t, err, "server DB: list messages for conversation %s", convID)

	for _, msg := range msgs {
		if strings.Contains(msg.Content, contentSubstring) {
			return
		}
	}

	require.Fail(t, "server DB: no message containing %q found in conversation %s (searched %d messages)",
		contentSubstring, convID, len(msgs))
}

// ---------------------------------------------------------------------------
// Redis assertion helpers
// ---------------------------------------------------------------------------

// requireRedisSessionLockReleased verifies the per-conversation agent lock is
// released (i.e. the key does not exist in Redis). The key format is
// agent:lock:{conversationID} (D-075).
func requireRedisSessionLockReleased(t *testing.T, redisClient *redis.Client, convID string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
	defer cancel()

	key := fmt.Sprintf("agent:lock:%s", convID)
	exists, err := redisClient.Exists(ctx, key).Result()
	require.NoError(t, err, "redis: check lock key %s", key)
	require.Equal(t, int64(0), exists,
		"redis: agent lock for conversation %s should be released (key %s should not exist)", convID, key)
}

// requireRedisCheckpointExists verifies a HITL checkpoint exists in Redis.
// The key format is agent:checkpoint:{checkpointID} (D-083).
func requireRedisCheckpointExists(t *testing.T, redisClient *redis.Client, checkpointID string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
	defer cancel()

	key := fmt.Sprintf("agent:checkpoint:%s", checkpointID)
	exists, err := redisClient.Exists(ctx, key).Result()
	require.NoError(t, err, "redis: check checkpoint key %s", key)
	require.Equal(t, int64(1), exists,
		"redis: checkpoint %s should exist (key %s)", checkpointID, key)
}

// requireRedisCheckpointNotExists verifies a HITL checkpoint does not exist in
// Redis. The key format is agent:checkpoint:{checkpointID} (D-083).
func requireRedisCheckpointNotExists(t *testing.T, redisClient *redis.Client, checkpointID string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), fastTimeout)
	defer cancel()

	key := fmt.Sprintf("agent:checkpoint:%s", checkpointID)
	exists, err := redisClient.Exists(ctx, key).Result()
	require.NoError(t, err, "redis: check checkpoint key %s", key)
	require.Equal(t, int64(0), exists,
		"redis: checkpoint %s should NOT exist (key %s)", checkpointID, key)
}

// ---------------------------------------------------------------------------
// threeLayerCheck — synchronized verification of Server DB, Redis, Client DB
// ---------------------------------------------------------------------------

// threeLayerCheck provides synchronized verification of Server DB, Redis, and
// Client DB. Each Verify method logs the result through the embedded
// testStepLogger and calls either require.NoError or t.Log depending on
// whether the check is soft or hard.
type threeLayerCheck struct {
	t      *testing.T
	logger *testStepLogger
}

// newThreeLayerCheck creates a threeLayerCheck bound to the given testing.T
// and testStepLogger.
func newThreeLayerCheck(t *testing.T, logger *testStepLogger) *threeLayerCheck {
	t.Helper()
	return &threeLayerCheck{t: t, logger: logger}
}

// VerifyServerDB runs fn and logs the result as a Server DB checkpoint. A
// non-nil error from fn fails the test immediately via require.NoError.
func (c *threeLayerCheck) VerifyServerDB(name string, fn func() error) {
	c.t.Helper()

	err := fn()
	if err != nil {
		c.logger.FailCheckpoint(name, "ServerDB", err)
		require.NoError(c.t, err, "three-layer check: ServerDB %s", name)
		return
	}
	c.logger.Checkpoint(name, "ServerDB", "verified")
}

// VerifyRedis runs fn and logs the result as a Redis checkpoint. A non-nil
// error from fn fails the test immediately via require.NoError.
func (c *threeLayerCheck) VerifyRedis(name string, fn func() error) {
	c.t.Helper()

	err := fn()
	if err != nil {
		c.logger.FailCheckpoint(name, "Redis", err)
		require.NoError(c.t, err, "three-layer check: Redis %s", name)
		return
	}
	c.logger.Checkpoint(name, "Redis", "verified")
}

// VerifyClientDB runs fn and logs the result as a Client DB checkpoint. When
// soft is true, a non-nil error from fn is logged but does not fail the test
// (because MQ push in E2E may not deliver updates to the client DB). When soft
// is false, a non-nil error fails the test via require.NoError.
func (c *threeLayerCheck) VerifyClientDB(name string, fn func() error, soft bool) {
	c.t.Helper()

	err := fn()
	if err != nil {
		c.logger.FailCheckpoint(name, "ClientDB", err)
		if soft {
			c.t.Logf("three-layer check: ClientDB %s: soft failure: %v", name, err)
			return
		}
		require.NoError(c.t, err, "three-layer check: ClientDB %s", name)
		return
	}
	c.logger.Checkpoint(name, "ClientDB", "verified")
}

