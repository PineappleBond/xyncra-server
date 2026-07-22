// RemoteCalling timeout cleanup background task (D-123 / D-137).
//
// Two-layer cleanup:
//  1. Conversation-level: scans conversations stuck in asking_user status and cleans up
//     those that have exceeded the configured max age (default 24h).
//  2. RemoteCalling-level: scans individual RemoteCallings with expires_at < NOW().
//
// Cleanup steps mirror the D-122 cleanupAfterResumeFailure pattern: clear agent status,
// soft-delete RemoteCallings, delete Redis checkpoint, send a user-friendly timeout
// message, and broadcast an agent_timeout ephemeral notification.
package agent

import (
	"context"
	"encoding/json"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// RemoteCallingCleanupConfig holds configuration for the RemoteCalling timeout cleanup task.
type RemoteCallingCleanupConfig struct {
	// Interval is the time between cleanup runs. Defaults to 5 minutes.
	Interval time.Duration

	// MaxAge is the maximum time a conversation can remain in asking_user
	// status before being cleaned up. Defaults to 24 hours.
	MaxAge time.Duration

	// BatchSize is the maximum number of conversations/remote callings to process per cycle.
	// Defaults to 100.
	BatchSize int

	// LockTTL is the TTL for the per-conversation distributed lock.
	// Defaults to 30 seconds.
	LockTTL time.Duration
}

// RemoteCallingCleanupTask periodically cleans up conversations stuck in asking_user
// status and individual expired RemoteCallings. It implements D-123 / D-137.
//
// All cleanup steps are non-fatal (D-007): errors are logged but do not
// interrupt processing of other conversations.
type RemoteCallingCleanupTask struct {
	config            RemoteCallingCleanupConfig
	convStore         *store.ConversationStore
	remoteCallingStore *store.RemoteCallingStore  // optional, nil-safe (D-063)
	checkpointStore   DeletableCheckPointStore   // optional
	broadcaster       *BroadcastHelper
	dataStore         store.StoreAPI // for SendMessage
	broker            mq.Broker     // for enqueuing agent resume tasks
	lockClient        redisClient    // reuses interface from task_handler.go (SetNX + Del)
	logger            Logger
}

// NewRemoteCallingCleanupTask creates a RemoteCallingCleanupTask with the given dependencies.
// Zero-value fields in cfg are filled with sensible defaults:
//
//   - Interval:  5 minutes
//   - MaxAge:    24 hours
//   - BatchSize: 100
//   - LockTTL:   30 seconds
//
// remoteCallingStore and checkpointStore may be nil (gracefully skipped).
// broker may be nil (agent resume enqueue skipped).
func NewRemoteCallingCleanupTask(
	cfg RemoteCallingCleanupConfig,
	convStore *store.ConversationStore,
	remoteCallingStore *store.RemoteCallingStore,
	checkpointStore DeletableCheckPointStore,
	broadcaster *BroadcastHelper,
	dataStore store.StoreAPI,
	broker mq.Broker,
	lockClient redisClient,
	logger Logger,
) *RemoteCallingCleanupTask {
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
	return &RemoteCallingCleanupTask{
		config:             cfg,
		convStore:          convStore,
		remoteCallingStore: remoteCallingStore,
		checkpointStore:    checkpointStore,
		broadcaster:        broadcaster,
		dataStore:          dataStore,
		broker:             broker,
		lockClient:         lockClient,
		logger:             logger,
	}
}

// Run starts the cleanup loop. It blocks until ctx is cancelled.
//
// On each tick, Run calls cleanupOnce and logs the outcome. Panics are
// recovered to prevent the background goroutine from crashing the server.
// Cleanup failures are logged but do not interrupt the loop (D-007).
//
// The first cleanup does not run immediately; Run waits for the first tick.
func (t *RemoteCallingCleanupTask) Run(ctx context.Context) {
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
						t.logger.Error("panic recovered", "error", r, "stack", string(debug.Stack()))
					}
				}()
				t.cleanupOnce(ctx)
			}()
		}
	}
}

// cleanupOnce executes a single cleanup cycle: query stale conversations,
// clean expired RemoteCallings, and attempt to clean each one up.
func (t *RemoteCallingCleanupTask) cleanupOnce(ctx context.Context) {
	// Layer 1: Conversation-level cleanup (existing logic).
	t.cleanupStaleConversations(ctx)

	// Layer 2: RemoteCalling-level cleanup (D-137).
	t.cleanupExpiredRemoteCallings(ctx)
}

// cleanupStaleConversations scans conversations stuck in asking_user status
// and cleans up those that have exceeded the configured max age.
func (t *RemoteCallingCleanupTask) cleanupStaleConversations(ctx context.Context) {
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
					t.logger.Error("panic recovered for conversation", "conversation_id", conv.ID, "error", r, "stack", string(debug.Stack()))
				}
			}()
			t.cleanupConversation(ctx, conv)
		}()
	}
}

// cleanupExpiredRemoteCallings scans individual RemoteCallings with expires_at < NOW()
// and marks them as expired. If no pending remain for a checkpoint, triggers conversation cleanup.
//
// Optimization: expired RCs are grouped by checkpointID so that the pending count
// query (CountPendingByCheckpoint) is executed once per checkpoint instead of once
// per expired RC, reducing redundant database queries.
func (t *RemoteCallingCleanupTask) cleanupExpiredRemoteCallings(ctx context.Context) {
	if t.remoteCallingStore == nil {
		return
	}

	expired, err := t.remoteCallingStore.ListExpired(ctx, t.config.BatchSize, time.Now())
	if err != nil {
		t.logger.Error("failed to list expired remote callings", "error", err)
		return
	}
	if len(expired) == 0 {
		return
	}

	t.logger.Info("found expired remote callings", "count", len(expired))

	// Mark all as expired first (batch operation).
	for _, rc := range expired {
		if err := t.remoteCallingStore.MarkExpired(ctx, rc.ID); err != nil {
			t.logger.Error("mark remote calling expired failed (non-fatal)",
				"id", rc.ID, "error", err)
		}
	}

	// Group by checkpointID to avoid redundant CountPendingByCheckpoint queries.
	type checkpointGroup struct {
		RCs []*model.RemoteCalling
	}
	groups := make(map[string]*checkpointGroup)
	for _, rc := range expired {
		g, ok := groups[rc.CheckpointID]
		if !ok {
			g = &checkpointGroup{}
			groups[rc.CheckpointID] = g
		}
		g.RCs = append(g.RCs, rc)
	}

	t.logger.Info("grouped expired remote callings by checkpoint",
		"checkpoints", len(groups))

	// Process each checkpoint group once.
	for checkpointID, g := range groups {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.logger.Error("panic recovered for checkpoint group",
						"checkpoint_id", checkpointID, "error", r, "stack", string(debug.Stack()))
				}
			}()
			// Use the first RC as representative for the group.
			t.cleanupExpiredCheckpoint(ctx, checkpointID, g.RCs[0])
		}()
	}
}

// cleanupExpiredExpiredCheckpoint checks if all RemoteCallings for a checkpoint
// are resolved/expired and handles cleanup or agent resume accordingly.
// This consolidates the per-checkpoint logic to avoid redundant pending count queries.
func (t *RemoteCallingCleanupTask) cleanupExpiredCheckpoint(ctx context.Context, checkpointID string, representativeRC *model.RemoteCalling) {
	// Check if there are still pending RemoteCallings for this checkpoint.
	pending, err := t.remoteCallingStore.CountPendingByCheckpoint(ctx, checkpointID)
	if err != nil {
		t.logger.Error("count pending remote callings failed (non-fatal)",
			"checkpoint_id", checkpointID, "error", err)
		return
	}

	// If no more pending, determine whether to resume the agent or clean up.
	if pending == 0 {
		t.logger.Info("all remote callings resolved/expired for checkpoint",
			"checkpoint_id", checkpointID, "conversation_id", representativeRC.ConversationID)

		// Check if any RemoteCallings were resolved (client reported results).
		// If some were resolved, enqueue agent resume so it can process the results.
		// If ALL are expired (none resolved), clean up the conversation to break
		// the infinite loop where: expire → resume → agent re-calls function →
		// new RemoteCalling → expire again (BUG-002).
		resolvedRCs, listErr := t.remoteCallingStore.GetResolvedByCheckpoint(ctx, checkpointID)
		if listErr != nil {
			t.logger.Error("get resolved remote callings by checkpoint failed (non-fatal)",
				"checkpoint_id", checkpointID, "error", listErr)
			return
		}

		if len(resolvedRCs) > 0 {
			// Some callings were resolved — enqueue agent resume to process results.
			//
			// Anti-loop guard (BUG-002): use a Redis SETNX key to prevent the
			// cleanup task from repeatedly triggering resume for the same checkpoint.
			// Without this, the cycle would be: expire → cleanup enqueues resume →
			// agent re-calls function → new RCs → expire → cleanup enqueues resume again.
			// The TTL (5 minutes) is intentionally short — it only needs to outlast
			// the cleanup interval (5 minutes) to prevent duplicate enqueues within
			// a single cleanup window. The idempotency guard in the resume handler
			// (D-121) provides longer-term deduplication.
			if t.broker != nil && t.lockClient != nil {
				resumeGuardKey := "cleanup:resume:" + checkpointID
				acquired, lockErr := t.lockClient.SetNX(ctx, resumeGuardKey, "1", 5*time.Minute).Result()
				if lockErr != nil {
					t.logger.Error("cleanup resume guard check failed (non-fatal)",
						"checkpoint_id", checkpointID, "error", lockErr)
					// Fail-open: proceed with enqueue even if guard check fails.
				} else if !acquired {
					t.logger.Info("cleanup resume already triggered for checkpoint, skipping",
						"checkpoint_id", checkpointID)
					return
				}
			}

			if t.broker != nil {
				payload := AgentResumePayload{
					ConversationID: representativeRC.ConversationID,
					CheckpointID:   checkpointID,
					AgentID:        representativeRC.AgentID,
					SenderID:       "", // cleanup-triggered; no specific sender
					DeviceID:       "", // cleanup-triggered; no specific device
				}
				raw, marshalErr := json.Marshal(payload)
				if marshalErr != nil {
					t.logger.Error("marshal agent resume payload failed (non-fatal)",
						"checkpoint_id", checkpointID, "error", marshalErr)
				} else {
					task := &mq.Task{
						Type:    mq.TypeAgentResume,
						Payload: raw,
						Queue:   mq.QueueDefault,
					}
					if _, enqueueErr := t.broker.Enqueue(ctx, task); enqueueErr != nil {
						t.logger.Error("enqueue agent resume after all expired failed (non-fatal)",
							"checkpoint_id", checkpointID, "error", enqueueErr)
					}
				}
			}
		} else {
			// All callings expired with none resolved — clean up conversation
			// instead of re-triggering agent execution.
			t.cleanupExpiredConversation(ctx, representativeRC)
		}
	}
}

// NOTE: cleanupExpiredRemoteCalling was removed in favor of the grouped
// approach in cleanupExpiredRemoteCallings + cleanupExpiredCheckpoint,
// which reduces redundant CountPendingByCheckpoint queries.

// cleanupExpiredConversation performs full conversation cleanup when all
// RemoteCallings for a checkpoint have expired without any being resolved.
// This breaks the infinite loop: expire → agent resume → re-call function →
// new RemoteCalling → expire (BUG-002).
//
// Steps: clear agent status, delete RemoteCallings, delete Redis checkpoint,
// send a user-friendly timeout message, and broadcast notifications.
// All steps are non-fatal — errors are logged but do not abort the cleanup.
func (t *RemoteCallingCleanupTask) cleanupExpiredConversation(ctx context.Context, rc *model.RemoteCalling) {
	// Re-check conversation status to avoid duplicate cleanup.
	// NOTE: Only checking for tool_calling here (not asking_user) because:
	// - cleanupStaleConversations (Layer 1) handles conversations stuck in asking_user.
	// - cleanupExpiredRemoteCallings (Layer 2) handles individual expired RemoteCallings,
	//   which trigger this method only when ALL are expired (none resolved).
	// This division ensures each cleanup path has clear responsibility.
	conv, err := t.convStore.Get(ctx, rc.ConversationID)
	if err != nil {
		t.logger.Error("get conversation for expired cleanup failed (non-fatal)",
			"conversation_id", rc.ConversationID, "error", err)
		return
	}
	if conv.AgentStatus != model.AgentStatusToolCalling {
		t.logger.Info("conversation no longer in tool_calling, skipping expired cleanup",
			"conversation_id", rc.ConversationID, "agent_status", conv.AgentStatus)
		return
	}

	// Determine the human user (the non-agent participant).
	humanUserID := conv.UserID1
	if humanUserID == conv.AgentID {
		humanUserID = conv.UserID2
	}

	// 1. Clear agent status (reset conversation to idle).
	cleanupUpdatedAt, clearErr := t.convStore.ClearAgentStatus(ctx, rc.ConversationID)
	if clearErr != nil {
		t.logger.Error("clear agent status failed (non-fatal)",
			"conversation_id", rc.ConversationID, "error", clearErr)
	}

	// 2. Delete RemoteCallings for this checkpoint.
	if err := t.remoteCallingStore.DeleteByCheckpoint(ctx, rc.CheckpointID); err != nil {
		t.logger.Error("delete remote callings failed (non-fatal)",
			"checkpoint_id", rc.CheckpointID, "error", err)
	}

	// 3. Delete checkpoint from Redis.
	if t.checkpointStore != nil {
		if err := t.checkpointStore.Delete(ctx, rc.CheckpointID); err != nil {
			t.logger.Error("delete checkpoint failed (non-fatal)",
				"checkpoint_id", rc.CheckpointID, "error", err)
		}
	}

	// 4. Send user-friendly timeout error message.
	msg := &model.Message{
		ID:              uuid.New().String(),
		ClientMessageID: uuid.New().String(),
		ConversationID:  rc.ConversationID,
		SenderID:        rc.AgentID,
		Content:         "抱歉，远程函数调用超时，请重新发送消息。",
		Type:            "text",
		Status:          "sent",
		CreatedAt:       time.Now(),
	}
	if _, err := t.dataStore.SendMessage(ctx, msg, []string{humanUserID, rc.AgentID}); err != nil {
		t.logger.Error("send timeout message failed (non-fatal)",
			"conversation_id", rc.ConversationID, "error", err)
	}

	// 5. Broadcast notifications.
	if t.broadcaster != nil {
		t.broadcaster.SendAgentTimeout(ctx, humanUserID, rc.AgentID, rc.ConversationID, "remote_calling_timeout")
		// Broadcast conversation update to both participants (BUG-001).
		// Use base userID for the agent's daemon (e.g. "agent" from "agent/weather-bot").
		t.broadcaster.SendConversationUpdate(ctx, humanUserID, rc.ConversationID, cleanupUpdatedAt)
		t.broadcaster.SendConversationUpdate(ctx, extractBaseUserID(rc.AgentID), rc.ConversationID, cleanupUpdatedAt)
	}

	t.logger.Info("cleaned up expired remote callings conversation",
		"conversation_id", rc.ConversationID, "agent_id", rc.AgentID, "checkpoint_id", rc.CheckpointID)
}

// cleanupConversation performs all cleanup steps for a single stale HITL
// conversation. All steps are non-fatal — errors are logged but do not affect
// subsequent steps (D-007 / D-122).
func (t *RemoteCallingCleanupTask) cleanupConversation(ctx context.Context, conv *model.Conversation) {
	// 1. Check if the agent lock is held (agent:lock:{conversationID}).
	// If an agent is actively processing, skip cleanup to avoid interference.
	agentLockKey := "agent:lock:" + conv.ID
	agentLockHeld, err := t.lockClient.Exists(ctx, agentLockKey).Result()
	if err != nil {
		t.logger.Error("agent lock check failed (non-fatal)", "conversation_id", conv.ID, "error", err)
		return
	}
	if agentLockHeld > 0 {
		t.logger.Debug("agent lock held, skipping cleanup", "conversation_id", conv.ID)
		return
	}

	// 2. Acquire cleanup lock (Redis SETNX).
	// Key format: hitl:cleanup:{conversationID}
	// NOTE: LockKey prefix differs from agent:lock: intentionally — agent:lock is
	// held by the agent task handler during active processing, while hitl:cleanup
	// is held by this cleanup task to prevent duplicate cleanup across nodes.
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
	if fresh.AgentStatus != model.AgentStatusAskingUser && fresh.AgentStatus != model.AgentStatusToolCalling {
		t.logger.Info("conversation no longer in asking_user/tool_calling, skipping",
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

	// 4. DeleteByCheckpoint (soft-delete RemoteCallings via GORM, D-137).
	if fresh.CheckpointID != "" && t.remoteCallingStore != nil {
		if err := t.remoteCallingStore.DeleteByCheckpoint(ctx, fresh.CheckpointID); err != nil {
			t.logger.Error("delete remote callings failed (non-fatal)",
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
		// Broadcast conversation update to both participants so clients refresh state (D-120 / D-124).
		// Use base userID for the agent's daemon (e.g. "agent" from "agent/weather-bot").
		t.broadcaster.SendConversationUpdate(ctx, humanUserID, conv.ID, cleanupUpdatedAt)
		t.broadcaster.SendConversationUpdate(ctx, extractBaseUserID(fresh.AgentID), conv.ID, cleanupUpdatedAt)
	}

	t.logger.Info("cleaned up stale HITL conversation",
		"conversation_id", conv.ID, "agent_id", fresh.AgentID, "checkpoint_id", fresh.CheckpointID)
}
