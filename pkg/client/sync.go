package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

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
type markReadPayload struct {
	ConversationID string `json:"conversation_id"`
	MessageID      uint32 `json:"message_id"`
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

	// Debounce timer state, protected by mu.
	mu          sync.Mutex
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

	// 3. Deduplicate via NotificationLog (Seq uniqueIndex).
	nLog := &model.NotificationLog{
		ID:        uuid.New().String(),
		Seq:       update.Seq,
		Type:      update.Type,
		Payload:   []byte(update.Payload),
		CreatedAt: time.Now(),
	}
	if err := sm.db.NotificationLogs.Save(ctx, nLog); err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			// Duplicate — skip but still advance seq below.
			sm.logger.Debug("duplicate update skipped", "seq", update.Seq)
			if err := sm.db.SyncStates.SetLocalMaxSeq(ctx, update.Seq); err != nil {
				return NewSyncError(fmt.Errorf("set local max seq after dedup: %w", err))
			}
			return nil
		}
		return NewSyncError(fmt.Errorf("save notification log: %w", err))
	}

	// 4. Dispatch by type.
	if err := sm.dispatchUpdate(ctx, update); err != nil {
		return err
	}

	// 5. Advance localMaxSeq.
	if err := sm.db.SyncStates.SetLocalMaxSeq(ctx, update.Seq); err != nil {
		return NewSyncError(fmt.Errorf("set local max seq: %w", err))
	}

	return nil
}

// dispatchUpdate routes the update to the appropriate handler based on its Type.
func (sm *syncManager) dispatchUpdate(ctx context.Context, update *protocol.PackageDataUpdate) error {
	switch update.Type {
	case protocol.UpdateTypeMessage:
		return sm.handleMessage(ctx, update.Payload)
	case protocol.UpdateTypeDeleteMessage:
		return sm.handleDeleteMessage(ctx, update.Payload)
	case protocol.UpdateTypeMarkRead:
		return sm.handleMarkRead(ctx, update.Payload)
	case protocol.UpdateTypeConversation:
		return sm.handleConversation(ctx, update.Payload)
	case protocol.UpdateTypeGap:
		// D-029: gap type only advances seq, no data written.
		if sm.handler != nil {
			if err := sm.handler.OnGap(ctx, update.Seq); err != nil {
				sm.logger.Error("handler OnGap failed", "seq", update.Seq, "error", err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown update type: %s", update.Type)
	}
}

// ---------------------------------------------------------------------------
// Type handlers
// ---------------------------------------------------------------------------

// handleMessage parses the payload as a model.Message, persists it, updates
// the conversation's last-message pointer, and notifies the handler.
func (sm *syncManager) handleMessage(ctx context.Context, payload json.RawMessage) error {
	var msg model.Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return NewSyncError(fmt.Errorf("unmarshal message payload: %w", err))
	}

	// Persist the message. Ignore duplicate key errors (idempotent).
	if err := sm.db.Messages.Create(ctx, &msg); err != nil {
		if !errors.Is(err, store.ErrDuplicateKey) {
			return NewSyncError(fmt.Errorf("create message: %w", err))
		}
	}

	// Update conversation last-message pointer.
	if err := sm.db.Conversations.UpdateLastMessage(ctx, msg.ConversationID, msg.CreatedAt, msg.MessageID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return NewSyncError(fmt.Errorf("update conversation last message: %w", err))
		}
	}

	// Notify handler.
	if sm.handler != nil {
		if err := sm.handler.OnMessage(ctx, &msg); err != nil {
			sm.logger.Error("handler OnMessage failed", "message_id", msg.ID, "error", err)
		}
	}

	return nil
}

// handleDeleteMessage parses the payload, soft-deletes the local message, and
// notifies the handler.
func (sm *syncManager) handleDeleteMessage(ctx context.Context, payload json.RawMessage) error {
	var dp deleteMessagePayload
	if err := json.Unmarshal(payload, &dp); err != nil {
		return NewSyncError(fmt.Errorf("unmarshal delete_message payload: %w", err))
	}

	// Soft-delete the message. ErrNotFound is acceptable — the message may
	// not have been synced locally yet.
	if err := sm.db.Messages.Delete(ctx, dp.MessageID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return NewSyncError(fmt.Errorf("delete message: %w", err))
		}
	}

	// Notify handler.
	if sm.handler != nil {
		if err := sm.handler.OnDeleteMessage(ctx, dp.MessageID, dp.ConversationID); err != nil {
			sm.logger.Error("handler OnDeleteMessage failed", "message_id", dp.MessageID, "error", err)
		}
	}

	return nil
}

// handleMarkRead parses the payload, updates the conversation read cursor for
// the current user, and notifies the handler.
func (sm *syncManager) handleMarkRead(ctx context.Context, payload json.RawMessage) error {
	var mp markReadPayload
	if err := json.Unmarshal(payload, &mp); err != nil {
		return NewSyncError(fmt.Errorf("unmarshal mark_read payload: %w", err))
	}

	// Update conversation read cursor. The store method determines which
	// column (last_read_message_id1 or 2) based on userID.
	if err := sm.db.Conversations.UpdateLastRead(ctx, mp.ConversationID, sm.userID, mp.MessageID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return NewSyncError(fmt.Errorf("update last read: %w", err))
		}
	}

	// Notify handler.
	if sm.handler != nil {
		if err := sm.handler.OnMarkRead(ctx, mp.ConversationID, mp.MessageID); err != nil {
			sm.logger.Error("handler OnMarkRead failed", "conversation_id", mp.ConversationID, "error", err)
		}
	}

	return nil
}

// handleConversation parses the payload as a model.Conversation and creates or
// updates the local record. If the conversation does not exist it is created;
// otherwise it is updated in place.
func (sm *syncManager) handleConversation(ctx context.Context, payload json.RawMessage) error {
	var conv model.Conversation
	if err := json.Unmarshal(payload, &conv); err != nil {
		return NewSyncError(fmt.Errorf("unmarshal conversation payload: %w", err))
	}

	// Check existence and create or update accordingly.
	existing, err := sm.db.Conversations.Get(ctx, conv.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			if err := sm.db.Conversations.Create(ctx, &conv); err != nil {
				return NewSyncError(fmt.Errorf("create conversation: %w", err))
			}
		} else {
			return NewSyncError(fmt.Errorf("get conversation: %w", err))
		}
	} else {
		// Preserve fields not controlled by the server update.
		conv.CreatedAt = existing.CreatedAt
		if err := sm.db.Conversations.Update(ctx, &conv); err != nil {
			return NewSyncError(fmt.Errorf("update conversation: %w", err))
		}
	}

	// Notify handler.
	if sm.handler != nil {
		if err := sm.handler.OnConversation(ctx, &conv); err != nil {
			sm.logger.Error("handler OnConversation failed", "conversation_id", conv.ID, "error", err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// ApplyUpdates — process a batch of updates
// ---------------------------------------------------------------------------

// ApplyUpdates applies a slice of updates in order. If a gap is detected the
// remaining updates are not processed and errSeqGap is returned so the caller
// can schedule a debounced pull.
func (sm *syncManager) ApplyUpdates(ctx context.Context, updates []protocol.PackageDataUpdate) error {
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
		sm.logger.Error("debounced pull: apply updates", "error", err)
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
