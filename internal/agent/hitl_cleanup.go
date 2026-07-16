// HITL timeout cleanup background task (D-123).
//
// Periodically scans conversations stuck in asking_user status and cleans up
// those that have exceeded the configured max age (default 24h). Cleanup steps
// mirror the D-122 cleanupAfterResumeFailure pattern: clear agent status,
// soft-delete questions, delete Redis checkpoint, send a user-friendly timeout
// message, and broadcast an agent_timeout ephemeral notification.
package agent

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// HITLCleanupConfig holds configuration for the HITL timeout cleanup task.
type HITLCleanupConfig struct {
	// Interval is the time between cleanup runs. Defaults to 5 minutes.
	Interval time.Duration

	// MaxAge is the maximum time a conversation can remain in asking_user
	// status before being cleaned up. Defaults to 24 hours.
	MaxAge time.Duration

	// BatchSize is the maximum number of conversations to process per cycle.
	// Defaults to 100.
	BatchSize int

	// LockTTL is the TTL for the per-conversation distributed lock.
	// Defaults to 30 seconds.
	LockTTL time.Duration
}

// HITLCleanupTask periodically cleans up conversations stuck in asking_user
// status. It implements D-123: background goroutine that scans for stale HITL
// conversations and releases their resources.
//
// All cleanup steps are non-fatal (D-007): errors are logged but do not
// interrupt processing of other conversations.
type HITLCleanupTask struct {
	config          HITLCleanupConfig
	convStore       *store.ConversationStore
	questionStore   *store.QuestionStore     // optional, nil-safe (D-063)
	checkpointStore DeletableCheckPointStore // optional
	broadcaster     *BroadcastHelper
	dataStore       store.StoreAPI // for SendMessage
	lockClient      redisClient    // reuses interface from task_handler.go (SetNX + Del)
	logger          Logger
}

// NewHITLCleanupTask creates a HITLCleanupTask with the given dependencies.
// Zero-value fields in cfg are filled with sensible defaults:
//
//   - Interval:  5 minutes
//   - MaxAge:    24 hours
//   - BatchSize: 100
//   - LockTTL:   30 seconds
//
// questionStore and checkpointStore may be nil (gracefully skipped).
func NewHITLCleanupTask(
	cfg HITLCleanupConfig,
	convStore *store.ConversationStore,
	questionStore *store.QuestionStore,
	checkpointStore DeletableCheckPointStore,
	broadcaster *BroadcastHelper,
	dataStore store.StoreAPI,
	lockClient redisClient,
	logger Logger,
) *HITLCleanupTask {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 24 * time.Hour
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.LockTTL <= 0 {
		cfg.LockTTL = 30 * time.Second
	}
	return &HITLCleanupTask{
		config:          cfg,
		convStore:       convStore,
		questionStore:   questionStore,
		checkpointStore: checkpointStore,
		broadcaster:     broadcaster,
		dataStore:       dataStore,
		lockClient:      lockClient,
		logger:          logger,
	}
}

// Run starts the cleanup loop. It blocks until ctx is cancelled.
//
// On each tick, Run calls cleanupOnce and logs the outcome. Panics are
// recovered to prevent the background goroutine from crashing the server.
// Cleanup failures are logged but do not interrupt the loop (D-007).
//
// The first cleanup does not run immediately; Run waits for the first tick.
func (t *HITLCleanupTask) Run(ctx context.Context) {
	ticker := time.NewTicker(t.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.logger.Error("panic recovered", "error", r)
					}
				}()
				t.cleanupOnce(ctx)
			}()
		}
	}
}

// cleanupOnce executes a single cleanup cycle: query stale conversations and
// attempt to clean each one up.
func (t *HITLCleanupTask) cleanupOnce(ctx context.Context) {
	conversations, err := t.convStore.ListStaleHITLConversations(ctx, t.config.MaxAge, t.config.BatchSize)
	if err != nil {
		t.logger.Error("failed to list stale conversations", "error", err)
		return
	}
	if len(conversations) == 0 {
		return
	}

	t.logger.Info("found stale HITL conversations", "count", len(conversations))

	for _, conv := range conversations {
		// Recover per-conversation so one panic does not abort the batch.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.logger.Error("panic recovered for conversation", "conversation_id", conv.ID, "error", r)
				}
			}()
			t.cleanupConversation(ctx, conv)
		}()
	}
}

// cleanupConversation performs all cleanup steps for a single stale HITL
// conversation. All steps are non-fatal — errors are logged but do not affect
// subsequent steps (D-007 / D-122).
func (t *HITLCleanupTask) cleanupConversation(ctx context.Context, conv *model.Conversation) {
	// 1. Acquire distributed lock (Redis SETNX).
	// Key format: hitl:cleanup:{conversationID}
	// TTL ensures the lock is released even if this node crashes.
	lockKey := "hitl:cleanup:" + conv.ID
	ok, err := t.lockClient.SetNX(ctx, lockKey, "1", t.config.LockTTL).Result()
	if err != nil {
		t.logger.Error("lock acquire failed (non-fatal)", "conversation_id", conv.ID, "error", err)
		return
	}
	if !ok {
		// Another node is handling this conversation.
		t.logger.Debug("lock already held, skipping", "conversation_id", conv.ID)
		return
	}
	// Lock TTL ensures automatic release; no explicit DEL needed.

	// 2. Re-check conversation status (another node or user may have resolved it).
	fresh, err := t.convStore.Get(ctx, conv.ID)
	if err != nil {
		t.logger.Error("re-check failed (non-fatal)", "conversation_id", conv.ID, "error", err)
		return
	}
	if fresh.AgentStatus != model.AgentStatusAskingUser {
		t.logger.Info("conversation no longer in asking_user, skipping",
			"conversation_id", conv.ID, "agent_status", fresh.AgentStatus)
		return
	}

	// Determine the human user (the non-agent participant).
	humanUserID := fresh.UserID1
	if humanUserID == fresh.AgentID {
		humanUserID = fresh.UserID2
	}

	// 3. ClearAgentStatus (reset conversation to idle).
	cleanupUpdatedAt, clearErr := t.convStore.ClearAgentStatus(ctx, conv.ID)
	if clearErr != nil {
		t.logger.Error("clear agent status failed (non-fatal)", "conversation_id", conv.ID, "error", clearErr)
	}

	// 4. DeleteByCheckpoint (soft-delete Questions via GORM).
	if fresh.CheckpointID != "" && t.questionStore != nil {
		if err := t.questionStore.DeleteByCheckpoint(ctx, fresh.CheckpointID); err != nil {
			t.logger.Error("delete questions failed (non-fatal)",
				"conversation_id", conv.ID, "checkpoint_id", fresh.CheckpointID, "error", err)
		}
	}

	// 5. Delete checkpoint from Redis (D-112).
	if fresh.CheckpointID != "" && t.checkpointStore != nil {
		if err := t.checkpointStore.Delete(ctx, fresh.CheckpointID); err != nil {
			t.logger.Error("delete checkpoint failed (non-fatal)",
				"conversation_id", conv.ID, "checkpoint_id", fresh.CheckpointID, "error", err)
		}
	}

	// 6. Send user-friendly timeout error message (D-067 / D-082).
	msg := &model.Message{
		ID:              uuid.New().String(),
		ClientMessageID: uuid.New().String(),
		ConversationID:  conv.ID,
		SenderID:        fresh.AgentID,
		Content:         "抱歉，等待时间过长，会话已超时。请重新发送消息。",
		Type:            "text",
		Status:          "sent",
		CreatedAt:       time.Now(),
	}
	if _, err := t.dataStore.SendMessage(ctx, msg, []string{humanUserID, fresh.AgentID}); err != nil {
		t.logger.Error("send timeout message failed (non-fatal)", "conversation_id", conv.ID, "error", err)
	}

	// 7. Broadcast agent_timeout ephemeral notification (D-087).
	if t.broadcaster != nil {
		t.broadcaster.SendAgentTimeout(ctx, humanUserID, fresh.AgentID, conv.ID, "hitl_timeout")
		// Also send conversation update so clients refresh state (D-120 / D-124).
		t.broadcaster.SendConversationUpdate(ctx, humanUserID, conv.ID, cleanupUpdatedAt)
	}

	t.logger.Info("cleaned up stale HITL conversation",
		"conversation_id", conv.ID, "agent_id", fresh.AgentID, "checkpoint_id", fresh.CheckpointID)
}
