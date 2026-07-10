package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// errSeqGap is returned by ApplyUpdate when a sequence gap is detected,
// signalling the caller to trigger a debounced pull from the server.
var errSeqGap = errors.New("client: sequence gap detected")

// ---------------------------------------------------------------------------
// Internal payload structs for update type dispatch
// ---------------------------------------------------------------------------

// deleteMessagePayload is the JSON structure of a "delete_message" update.
type deleteMessagePayload struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
}

// markReadPayload is the JSON structure of a "mark_read" update.
// The field name must match the server's markReadUpdatePayload
// (internal/handler/mark_as_read.go) which uses "last_read_message_id".
type markReadPayload struct {
	ConversationID    string `json:"conversation_id"`
	LastReadMessageID uint32 `json:"last_read_message_id"`
}

// conversationUpdatePayload is the JSON structure of a "conversation" update
// emitted by delete_conversation and restore_conversation (D-013, D-015). The
// server does not send a full model.Conversation; it only carries the
// conversation ID and the action performed.
type conversationUpdatePayload struct {
	ConversationID string `json:"conversation_id"`
	Action         string `json:"action"` // "delete" or "restore"
}

// createConversationUpdatePayload is the JSON structure of a "create" action
// conversation update emitted by create_conversation (D-045). The server wraps
// the full model.Conversation with an "action" field for consistency with
// delete and restore events.
type createConversationUpdatePayload struct {
	Action       string              `json:"action"` // "create"
	Conversation *model.Conversation `json:"conversation"`
}

// typingUpdatePayload is the JSON structure of a "typing" ephemeral update.
type typingUpdatePayload struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	IsTyping       bool   `json:"is_typing"`
	Timestamp      int64  `json:"timestamp"`
}

// streamingUpdatePayload is the JSON structure of a "streaming" ephemeral update.
type streamingUpdatePayload struct {
	StreamID       string `json:"stream_id"`
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Text           string `json:"text"`
	IsDone         bool   `json:"is_done"`
	Timestamp      int64  `json:"timestamp"`
}

// syncUpdatesResponse is the JSON structure returned by the sync_updates RPC.
type syncUpdatesResponse struct {
	Updates   []protocol.PackageDataUpdate `json:"updates"`
	HasMore   bool                         `json:"has_more"`
	LatestSeq uint32                       `json:"latest_seq"`
}

// ---------------------------------------------------------------------------
// syncManager
// ---------------------------------------------------------------------------

// syncManager processes incoming data updates, persists them to the local
// store, and notifies the UpdateHandler. It also manages debounced pull
// requests to fill sequence gaps and full synchronisation on startup.
type syncManager struct {
	// db is the local client database.
	db *store.ClientDB
	// handler receives processed updates for application-level side effects.
	handler UpdateHandler
	// userID is the authenticated user, used to determine which read cursor
	// column to update on mark_read events.
	userID string
	// rpcFn performs a JSON-RPC call to the server.
	rpcFn func(ctx context.Context, method string, params any) (json.RawMessage, error)
	// batchSize is the maximum number of updates fetched per sync_updates call.
	batchSize int
	// debounce is the coalescing window for pull requests triggered by gaps.
	debounce time.Duration
	// logger is used for diagnostic output.
	logger Logger

	// mu protects debounce timer state.
	mu sync.Mutex
	// applyMu serializes ApplyUpdates calls from different goroutines (H-3).
	applyMu     sync.Mutex
	pullTimer   *time.Timer
	pullPending bool

	// Lifecycle contexts.
	ctx    context.Context
	cancel context.CancelFunc
}

// newSyncManager creates a syncManager with the given dependencies.
func newSyncManager(
	db *store.ClientDB,
	handler UpdateHandler,
	userID string,
	rpcFn func(ctx context.Context, method string, params any) (json.RawMessage, error),
	batchSize int,
	debounce time.Duration,
	logger Logger,
) *syncManager {
	return &syncManager{
		db:        db,
		handler:   handler,
		userID:    userID,
		rpcFn:     rpcFn,
		batchSize: batchSize,
		debounce:  debounce,
		logger:    logger,
	}
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start initialises the syncManager's context, derived from the provided
// parent context. All background operations (debounced pulls) use this context.
func (sm *syncManager) Start(ctx context.Context) {
	sm.ctx, sm.cancel = context.WithCancel(ctx)
}

// Stop cancels the syncManager's context and stops any pending debounce timer.
func (sm *syncManager) Stop() {
	if sm.cancel != nil {
		sm.cancel()
	}
	sm.mu.Lock()
	if sm.pullTimer != nil {
		sm.pullTimer.Stop()
	}
	sm.mu.Unlock()
}

// ---------------------------------------------------------------------------
// ApplyUpdate — process a single update
// ---------------------------------------------------------------------------

// ApplyUpdate validates sequence continuity, deduplicates via the notification
// log, dispatches the update by type, and advances localMaxSeq on success.
// It returns errSeqGap when a gap is detected so the caller can schedule a
// debounced pull.
func (sm *syncManager) ApplyUpdate(ctx context.Context, update *protocol.PackageDataUpdate) error {
	// 0. Ephemeral updates (Seq == 0) bypass seq continuity, dedup, and persistence.
	if update.Seq == 0 {
		sm.notifyHandler(ctx, update)
		return nil
	}

	// 1. Read the current local maximum sequence number.
	localMaxSeq, err := sm.db.SyncStates.GetLocalMaxSeq(ctx)
	if err != nil {
		return NewSyncError(fmt.Errorf("get local max seq: %w", err))
	}

	// 2. Check sequence continuity.
	switch {
	case update.Seq <= localMaxSeq:
		// Already processed — skip silently.
		sm.logger.Debug("skipping already-processed update", "seq", update.Seq, "local_max_seq", localMaxSeq)
		return nil
	case update.Seq > localMaxSeq+1:
		// Gap detected — trigger debounced pull and return special error.
		sm.logger.Debug("sequence gap detected", "seq", update.Seq, "local_max_seq", localMaxSeq)
		return errSeqGap
	}
	// update.Seq == localMaxSeq + 1 → continue processing.

	// Steps 3-5 wrapped in an atomic transaction (H-3).
	var txErr error
	txErr = sm.db.Transaction(ctx, func(tx *gorm.DB) error {
		// 3. Deduplicate via NotificationLog (Seq uniqueIndex).
		nLog := &model.NotificationLog{
			ID:        uuid.New().String(),
			Seq:       update.Seq,
			Type:      update.Type,
			Payload:   []byte(update.Payload),
			CreatedAt: time.Now(),
		}
		if err := sm.db.NotificationLogs.SaveTx(ctx, tx, nLog); err != nil {
			if errors.Is(err, store.ErrDuplicateKey) {
				// Duplicate — advance seq and skip.
				sm.logger.Debug("duplicate update skipped", "seq", update.Seq)
				if err := sm.db.SyncStates.SetLocalMaxSeqTx(ctx, tx, update.Seq); err != nil {
					return fmt.Errorf("set local max seq after dedup: %w", err)
				}
				return nil
			}
			return fmt.Errorf("save notification log: %w", err)
		}

		// 4. Dispatch by type (DB writes only, no handler notifications).
		if err := sm.dispatchUpdateTx(ctx, tx, update); err != nil {
			return err
		}

		// 5. Advance localMaxSeq.
		if err := sm.db.SyncStates.SetLocalMaxSeqTx(ctx, tx, update.Seq); err != nil {
			return fmt.Errorf("set local max seq: %w", err)
		}
		return nil
	})
	if txErr != nil {
		return NewSyncError(txErr)
	}

	// Notify handler after successful transaction commit.
	sm.notifyHandler(ctx, update)
	return nil
}

// ---------------------------------------------------------------------------
// Transactional dispatch (DB writes only, no handler notifications)
// ---------------------------------------------------------------------------

// dispatchUpdateTx routes the update to the appropriate *Tx handler within the
// given transaction. Handler notifications are deferred until after commit.
func (sm *syncManager) dispatchUpdateTx(ctx context.Context, tx *gorm.DB, update *protocol.PackageDataUpdate) error {
	switch update.Type {
	case protocol.UpdateTypeMessage:
		return sm.handleMessageTx(ctx, tx, update.Payload)
	case protocol.UpdateTypeDeleteMessage:
		return sm.handleDeleteMessageTx(ctx, tx, update.Payload)
	case protocol.UpdateTypeMarkRead:
		return sm.handleMarkReadTx(ctx, tx, update.Payload)
	case protocol.UpdateTypeConversation:
		return sm.handleConversationTx(ctx, tx, update.Payload)
	case protocol.UpdateTypeGap:
		return nil
	case protocol.UpdateTypeTyping:
		// Defense-in-depth: reachable only if a typing update with Seq > 0 is
		// received (should never happen per D-050). Returns nil for graceful
		// degradation rather than erroring on an unknown type.
		return nil
	case protocol.UpdateTypeStreaming:
		// Defense-in-depth: reachable only if a streaming update with Seq > 0 is
		// received (should never happen per D-051). Returns nil for graceful
		// degradation rather than erroring on an unknown type.
		return nil
	default:
		return fmt.Errorf("unknown update type: %s", update.Type)
	}
}

// handleMessageTx persists the message and updates the conversation's
// last-message pointer within the given transaction.
func (sm *syncManager) handleMessageTx(ctx context.Context, tx *gorm.DB, payload json.RawMessage) error {
	var msg model.Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return fmt.Errorf("unmarshal message payload: %w", err)
	}

	// Persist the message. Ignore duplicate key errors (idempotent).
	if err := sm.db.Messages.CreateTx(ctx, tx, &msg); err != nil {
		if !errors.Is(err, store.ErrDuplicateKey) {
			return fmt.Errorf("create message: %w", err)
		}
	}

	// Update conversation last-message pointer.
	if err := sm.db.Conversations.UpdateLastMessageTx(ctx, tx, msg.ConversationID, msg.CreatedAt, msg.MessageID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("update conversation last message: %w", err)
		}
		// M-2: log when conversation not found instead of silently ignoring.
		sm.logger.Error("conversation not found for last message update", "conversation_id", msg.ConversationID, "message_id", msg.ID)
	}

	return nil
}

// handleDeleteMessageTx soft-deletes the local message within the given
// transaction.
func (sm *syncManager) handleDeleteMessageTx(ctx context.Context, tx *gorm.DB, payload json.RawMessage) error {
	var dp deleteMessagePayload
	if err := json.Unmarshal(payload, &dp); err != nil {
		return fmt.Errorf("unmarshal delete_message payload: %w", err)
	}

	// Soft-delete the message. ErrNotFound is acceptable — the message may
	// not have been synced locally yet.
	if err := sm.db.Messages.SoftDeleteTx(ctx, tx, dp.MessageID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("delete message: %w", err)
		}
	}

	return nil
}

// handleMarkReadTx updates the conversation read cursor for the current user
// within the given transaction.
func (sm *syncManager) handleMarkReadTx(ctx context.Context, tx *gorm.DB, payload json.RawMessage) error {
	var mp markReadPayload
	if err := json.Unmarshal(payload, &mp); err != nil {
		return fmt.Errorf("unmarshal mark_read payload: %w", err)
	}

	// Update conversation read cursor. ErrNotFound is acceptable.
	if err := sm.db.Conversations.UpdateLastReadTx(ctx, tx, mp.ConversationID, sm.userID, mp.LastReadMessageID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("update last read: %w", err)
		}
	}

	return nil
}

// handleConversationTx processes a "conversation" type update within the given
// transaction. Unknown actions return an error (M-6).
func (sm *syncManager) handleConversationTx(ctx context.Context, tx *gorm.DB, payload json.RawMessage) error {
	var peek conversationUpdatePayload
	if err := json.Unmarshal(payload, &peek); err != nil {
		return fmt.Errorf("unmarshal conversation update peek: %w", err)
	}

	switch peek.Action {
	case "delete":
		return sm.handleConversationDeleteTx(ctx, tx, peek.ConversationID)
	case "restore":
		return sm.handleConversationRestoreTx(ctx, tx, peek.ConversationID)
	case "create":
		return sm.handleConversationCreateTx(ctx, tx, payload)
	case "":
		// No action field — treat as a full conversation record (backward
		// compatibility with legacy create events that omit the action).
		return sm.handleConversationUpsertTx(ctx, tx, payload)
	default:
		// M-6: log and return error for unrecognised actions.
		sm.logger.Error("unknown conversation action", "action", peek.Action, "conversation_id", peek.ConversationID)
		return fmt.Errorf("unknown conversation action: %s", peek.Action)
	}
}

// handleConversationDeleteTx cascade soft-deletes the conversation and its
// messages within the given transaction (D-013).
func (sm *syncManager) handleConversationDeleteTx(ctx context.Context, tx *gorm.DB, convID string) error {
	if err := sm.db.Conversations.SoftDeleteTx(ctx, tx, convID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("delete conversation locally: %w", err)
		}
		sm.logger.Debug("conversation delete: not found locally, skipping", "conversation_id", convID)
	}
	return nil
}

// handleConversationRestoreTx cascade restores a previously soft-deleted
// conversation and its messages within the given transaction (D-015).
func (sm *syncManager) handleConversationRestoreTx(ctx context.Context, tx *gorm.DB, convID string) error {
	if err := sm.db.Conversations.RestoreTx(ctx, tx, convID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("restore conversation locally: %w", err)
		}
		sm.logger.Debug("conversation restore: not found locally, skipping", "conversation_id", convID)
	}
	return nil
}

// handleConversationCreateTx processes a "create" action for a conversation
// update within the given transaction (D-045).
func (sm *syncManager) handleConversationCreateTx(ctx context.Context, tx *gorm.DB, payload json.RawMessage) error {
	var wrapped createConversationUpdatePayload
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return fmt.Errorf("unmarshal create conversation payload: %w", err)
	}
	if wrapped.Conversation == nil {
		return fmt.Errorf("create conversation payload has nil conversation")
	}

	if err := sm.db.Conversations.UpsertTx(ctx, tx, wrapped.Conversation); err != nil {
		return fmt.Errorf("upsert conversation: %w", err)
	}
	return nil
}

// handleConversationUpsertTx creates or updates a conversation from a full
// model.Conversation payload within the given transaction.
func (sm *syncManager) handleConversationUpsertTx(ctx context.Context, tx *gorm.DB, payload json.RawMessage) error {
	var conv model.Conversation
	if err := json.Unmarshal(payload, &conv); err != nil {
		return fmt.Errorf("unmarshal conversation payload: %w", err)
	}

	if err := sm.db.Conversations.UpsertTx(ctx, tx, &conv); err != nil {
		return fmt.Errorf("upsert conversation: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Post-commit handler notifications
// ---------------------------------------------------------------------------

// notifyHandler calls handler methods after the transaction commits. Errors are
// logged but do not fail the sync pipeline.
func (sm *syncManager) notifyHandler(ctx context.Context, update *protocol.PackageDataUpdate) {
	if sm.handler == nil {
		return
	}
	switch update.Type {
	case protocol.UpdateTypeMessage:
		var msg model.Message
		if err := json.Unmarshal(update.Payload, &msg); err == nil {
			_ = sm.handler.OnMessage(ctx, &msg)
		}
	case protocol.UpdateTypeDeleteMessage:
		var dp deleteMessagePayload
		if err := json.Unmarshal(update.Payload, &dp); err == nil {
			_ = sm.handler.OnDeleteMessage(ctx, dp.MessageID, dp.ConversationID)
		}
	case protocol.UpdateTypeMarkRead:
		var mp markReadPayload
		if err := json.Unmarshal(update.Payload, &mp); err == nil {
			_ = sm.handler.OnMarkRead(ctx, mp.ConversationID, mp.LastReadMessageID)
		}
	case protocol.UpdateTypeConversation:
		var peek conversationUpdatePayload
		if err := json.Unmarshal(update.Payload, &peek); err == nil {
			conv := &model.Conversation{ID: peek.ConversationID}
			_ = sm.handler.OnConversation(ctx, conv)
		}
	case protocol.UpdateTypeTyping:
		var tp typingUpdatePayload
		if err := json.Unmarshal(update.Payload, &tp); err == nil {
			if th, ok := sm.handler.(TypingHandler); ok {
				_ = th.OnTyping(ctx, tp.UserID, tp.ConversationID, tp.IsTyping)
			}
		}
	case protocol.UpdateTypeStreaming:
		var sp streamingUpdatePayload
		if err := json.Unmarshal(update.Payload, &sp); err == nil {
			if sh, ok := sm.handler.(StreamingHandler); ok {
				_ = sh.OnStreaming(ctx, sp.UserID, sp.ConversationID, sp.StreamID, sp.Text, sp.IsDone)
			}
		}
	case protocol.UpdateTypeGap:
		_ = sm.handler.OnGap(ctx, update.Seq)
	}
}

// ---------------------------------------------------------------------------
// ApplyUpdates — process a batch of updates
// ---------------------------------------------------------------------------

// ApplyUpdates applies a slice of updates in order. If a gap is detected the
// remaining updates are not processed and errSeqGap is returned so the caller
// can schedule a debounced pull.
func (sm *syncManager) ApplyUpdates(ctx context.Context, updates []protocol.PackageDataUpdate) error {
	sm.applyMu.Lock()
	defer sm.applyMu.Unlock()
	for i := range updates {
		if err := sm.ApplyUpdate(ctx, &updates[i]); err != nil {
			if errors.Is(err, errSeqGap) {
				sm.scheduleDebouncedPull()
			}
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Debounced pull
// ---------------------------------------------------------------------------

// scheduleDebouncedPull starts (or coalesces) a debounce timer that will
// issue a sync_updates RPC to fill the sequence gap.
func (sm *syncManager) scheduleDebouncedPull() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.pullPending {
		return // Timer already armed — coalesce.
	}
	sm.pullPending = true
	sm.pullTimer = time.AfterFunc(sm.debounce, func() {
		sm.debouncedPull()
	})
}

// debouncedPull executes the sync_updates RPC, applies the returned updates,
// and reschedules itself if has_more indicates additional data is available.
func (sm *syncManager) debouncedPull() {
	sm.mu.Lock()
	sm.pullPending = false
	sm.mu.Unlock()

	ctx := sm.ctx
	if ctx == nil {
		return
	}

	// Read the current local max seq.
	localMaxSeq, err := sm.db.SyncStates.GetLocalMaxSeq(ctx)
	if err != nil {
		sm.logger.Error("debounced pull: get local max seq", "error", err)
		return
	}

	params := map[string]any{
		"after_seq": localMaxSeq,
		"limit":     sm.batchSize,
	}

	data, err := sm.rpcFn(ctx, "sync_updates", params)
	if err != nil {
		sm.logger.Error("debounced pull: rpc failed", "error", err)
		return
	}

	var resp syncUpdatesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		sm.logger.Error("debounced pull: parse response", "error", err)
		return
	}

	if err := sm.ApplyUpdates(ctx, resp.Updates); err != nil {
		sm.logger.Error("debounced pull: apply updates", "error", err, "retry", true)
		// Schedule a single retry after a short delay (L-6).
		time.AfterFunc(5*time.Second, func() {
			sm.mu.Lock()
			if !sm.pullPending {
				sm.pullPending = true
				sm.pullTimer = time.AfterFunc(sm.debounce, func() {
					sm.mu.Lock()
					sm.pullPending = false
					sm.mu.Unlock()
					sm.debouncedPull()
				})
			}
			sm.mu.Unlock()
		})
		return
	}

	if err := sm.db.SyncStates.SetLatestSeq(ctx, resp.LatestSeq); err != nil {
		sm.logger.Error("debounced pull: set latest seq", "error", err)
	}

	// If the server indicates more data, schedule another pull.
	if resp.HasMore {
		sm.scheduleDebouncedPull()
	}
}

// ---------------------------------------------------------------------------
// FullSync
// ---------------------------------------------------------------------------

// FullSync performs a blocking, paginated synchronisation with the server,
// fetching all updates after the current localMaxSeq until has_more is false.
// It is intended for use on initial connection or reconnection.
func (sm *syncManager) FullSync(ctx context.Context) error {
	localMaxSeq, err := sm.db.SyncStates.GetLocalMaxSeq(ctx)
	if err != nil {
		return NewSyncError(fmt.Errorf("full sync: get local max seq: %w", err))
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		params := map[string]any{
			"after_seq": localMaxSeq,
			"limit":     sm.batchSize,
		}

		data, err := sm.rpcFn(ctx, "sync_updates", params)
		if err != nil {
			return NewSyncError(fmt.Errorf("full sync rpc: %w", err))
		}

		var resp syncUpdatesResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return NewSyncError(fmt.Errorf("parse sync response: %w", err))
		}

		if err := sm.ApplyUpdates(ctx, resp.Updates); err != nil {
			// Gaps are unlikely during FullSync (server fills them), but
			// handle gracefully by logging and continuing.
			if !errors.Is(err, errSeqGap) {
				return err
			}
			sm.logger.Error("full sync: gap in updates", "error", err)
		}

		if err := sm.db.SyncStates.SetLatestSeq(ctx, resp.LatestSeq); err != nil {
			return NewSyncError(fmt.Errorf("full sync: set latest seq: %w", err))
		}

		if !resp.HasMore {
			break
		}

		// Re-read localMaxSeq for the next page.
		localMaxSeq, err = sm.db.SyncStates.GetLocalMaxSeq(ctx)
		if err != nil {
			return NewSyncError(fmt.Errorf("full sync: get local max seq: %w", err))
		}
	}

	return nil
}
