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
// emitted by delete_conversation, restore_conversation (D-013, D-015), and the
// lightweight "update" action broadcast by SendConversationUpdate (D-118, D-124).
// UpdatedAt carries the server-side updated_at as Unix seconds so the client
// can decide whether to pull (D-124).
//
// UpdatedAt accepts both JSON number (Unix seconds) and string (RFC3339) for
// backward compatibility with payloads that embed the full model.Conversation.
type conversationUpdatePayload struct {
	ConversationID string          `json:"conversation_id"`
	Action         string          `json:"action"` // "delete", "restore", "update", or "" (legacy create)
	UpdatedAt      json.RawMessage `json:"updated_at,omitempty"`
}

// updatedAtUnix parses UpdatedAt as either a JSON number (Unix seconds) or
// RFC3339 string, returning the Unix seconds. Returns 0 if unparseable.
func (p *conversationUpdatePayload) updatedAtUnix() int64 {
	if len(p.UpdatedAt) == 0 {
		return 0
	}
	// Try JSON number first (most common: int64 Unix seconds).
	var n int64
	if err := json.Unmarshal(p.UpdatedAt, &n); err == nil {
		return n
	}
	// Try RFC3339 string (from model.Conversation time.Time fields).
	var s string
	if err := json.Unmarshal(p.UpdatedAt, &s); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.Unix()
		}
	}
	return 0
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
	IsAgent        bool   `json:"is_agent"`
	Timestamp      int64  `json:"timestamp"`
}

// streamingUpdatePayload is the JSON structure of a "streaming" ephemeral update.
type streamingUpdatePayload struct {
	StreamID       string `json:"stream_id"`
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Text           string `json:"text"`
	IsDone         bool   `json:"is_done"`
	IsAgent        bool   `json:"is_agent"`
	Timestamp      int64  `json:"timestamp"`
}

// agentStatusPayload is the JSON structure of an "agent_status" ephemeral update (D-087).
type agentStatusPayload struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"`
	Timestamp      int64  `json:"timestamp"`
}

// agentTimeoutPayload is the JSON structure of an "agent_timeout" ephemeral update (D-087).
type agentTimeoutPayload struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Reason         string `json:"reason"`
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
		// D-124: Conversation "update" actions carry updated_at and trigger a
		// conditional pull from the server. Other ephemeral types go straight
		// to the handler.
		if update.Type == protocol.UpdateTypeConversation {
			sm.handleEphemeralConversationUpdate(ctx, update)
		} else {
			sm.notifyHandler(ctx, update)
		}
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
	txErr := sm.db.Transaction(ctx, func(tx *gorm.DB) error {
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
	case protocol.UpdateTypeAgentStatus,
		protocol.UpdateTypeAgentTimeout:
		// Defense-in-depth: ephemeral agent updates should never have Seq > 0.
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
	case "update":
		// D-124: "update" action normally arrives as ephemeral (Seq=0) and is
		// handled by handleEphemeralConversationUpdate. If it arrives as
		// non-ephemeral (defensive), apply the same updated_at comparison
		// logic and fetch if stale.
		return sm.handleConversationUpdateTx(ctx, tx, peek.ConversationID, peek.updatedAtUnix())
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
// When the local record does not exist at all (ErrNotFound), it falls back to
// fetching the full conversation from the server via RPC and upserting it
// locally, so that the initiator's local DB is correctly synchronised.
func (sm *syncManager) handleConversationRestoreTx(ctx context.Context, tx *gorm.DB, convID string) error {
	if err := sm.db.Conversations.RestoreTx(ctx, tx, convID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Local record doesn't exist — fetch from server and create.
			return sm.fetchAndUpsertConversationTx(ctx, tx, convID)
		}
		return fmt.Errorf("restore conversation locally: %w", err)
	}
	return nil
}

// fetchAndUpsertConversationTx fetches a conversation from the server via the
// get_conversation RPC and upserts it into the local database. This is used as
// a fallback when a restore update arrives but the local record does not exist.
func (sm *syncManager) fetchAndUpsertConversationTx(ctx context.Context, tx *gorm.DB, convID string) error {
	data, err := sm.rpcFn(ctx, "get_conversation", map[string]any{
		"conversation_id": convID,
	})
	if err != nil {
		return fmt.Errorf("fetch conversation from server: %w", err)
	}

	var result struct {
		Conversation   *model.Conversation   `json:"conversation"`
		RemoteCallings []*model.RemoteCalling `json:"remote_callings"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("unmarshal get_conversation response: %w", err)
	}
	if result.Conversation == nil {
		return fmt.Errorf("get_conversation returned nil conversation for %s", convID)
	}

	if err := sm.db.Conversations.UpsertTx(ctx, tx, result.Conversation); err != nil {
		return fmt.Errorf("upsert fetched conversation: %w", err)
	}

	// Clear stale remote callings before upserting new ones (D-137).
	// When agent_status != asking_user: HITL ended, clean all.
	// When new remote callings exist: replace with latest set.
	if sm.db.RemoteCallings != nil {
		if result.Conversation.AgentStatus != "asking_user" || len(result.RemoteCallings) > 0 {
			_ = sm.db.RemoteCallings.DeleteByConversationTx(tx, result.Conversation.ID)
		}
	}

	// Upsert remote callings (best-effort, D-137).
	if sm.db.RemoteCallings != nil && len(result.RemoteCallings) > 0 {
		for _, rc := range result.RemoteCallings {
			if err := sm.db.RemoteCallings.Upsert(ctx, rc); err != nil {
				sm.logger.Error("upsert remote calling", "error", err, "remote_calling_id", rc.ID)
			}
		}
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

// handleConversationUpdateTx processes an "update" action for a conversation
// within the given transaction (D-124). It compares the payload's updated_at
// with the local conversation's UpdatedAt and skips the RPC if the local cache
// is already up-to-date. This is the non-ephemeral counterpart to
// handleEphemeralConversationUpdate's "update" branch.
//
// If updated_at is 0 (old server), the RPC is always executed for backward
// compatibility.
func (sm *syncManager) handleConversationUpdateTx(ctx context.Context, tx *gorm.DB, convID string, updatedAt int64) error {
	// D-124: Skip RPC if local cache is up-to-date.
	if updatedAt > 0 {
		localConv, err := sm.db.Conversations.Get(ctx, convID)
		if err == nil && localConv != nil && updatedAt <= localConv.UpdatedAt.Unix() {
			sm.logger.Debug("skipping conversation update — local cache is current",
				"conversation_id", convID,
				"payload_updated_at", updatedAt,
				"local_updated_at", localConv.UpdatedAt.Unix())
			return nil
		}
		// err != nil (ErrNotFound) or localConv == nil → first fetch.
		// updatedAt > localConv.UpdatedAt → stale cache.
	}

	// Local cache is stale or missing — fetch from server and upsert.
	return sm.fetchAndUpsertConversationTx(ctx, tx, convID)
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
				_ = th.OnTyping(ctx, tp.UserID, tp.ConversationID, tp.IsTyping, tp.IsAgent)
			}
		}
	case protocol.UpdateTypeStreaming:
		var sp streamingUpdatePayload
		if err := json.Unmarshal(update.Payload, &sp); err == nil {
			if sh, ok := sm.handler.(StreamingHandler); ok {
				_ = sh.OnStreaming(ctx, sp.UserID, sp.ConversationID, sp.StreamID, sp.Text, sp.IsDone, sp.IsAgent)
			}
		}
	case protocol.UpdateTypeAgentStatus:
		var sp agentStatusPayload
		if err := json.Unmarshal(update.Payload, &sp); err == nil {
			if sh, ok := sm.handler.(AgentStatusHandler); ok {
				_ = sh.OnAgentStatus(ctx, sp.UserID, sp.ConversationID, sp.Status)
			}
		}
	case protocol.UpdateTypeAgentTimeout:
		var tp agentTimeoutPayload
		if err := json.Unmarshal(update.Payload, &tp); err == nil {
			if th, ok := sm.handler.(AgentTimeoutHandler); ok {
				_ = th.OnAgentTimeout(ctx, tp.UserID, tp.ConversationID, tp.Reason)
			}
		}
	case protocol.UpdateTypeGap:
		_ = sm.handler.OnGap(ctx, update.Seq)
	}
}

// ---------------------------------------------------------------------------
// Ephemeral conversation update (D-118 pull-on-notification, D-124 optimization)
// ---------------------------------------------------------------------------

// handleEphemeralConversationUpdate processes an ephemeral (Seq=0) conversation
// update. For the "update" action it implements the pull-on-notification pattern
// (D-118) with the D-124 optimisation: compare the payload's updated_at against
// the local conversation's UpdatedAt and skip the get_conversation RPC when the
// local cache is already up-to-date.
//
// For non-"update" actions (delete, restore, create, legacy) it falls through
// to the standard handler notification path.
func (sm *syncManager) handleEphemeralConversationUpdate(ctx context.Context, update *protocol.PackageDataUpdate) {
	var peek conversationUpdatePayload
	if err := json.Unmarshal(update.Payload, &peek); err != nil {
		sm.logger.Error("unmarshal ephemeral conversation update", "error", err)
		return
	}

	// Only the "update" action uses the pull-on-notification pattern with
	// D-124 timestamp comparison. Other actions fall through to the standard
	// handler notification path.
	if peek.Action != "update" {
		sm.notifyHandler(ctx, update)
		return
	}

	// D-124: If updated_at is present and the local cache is up-to-date, skip
	// the RPC. updated_at == 0 means old server — always pull for backward
	// compatibility.
	if peek.updatedAtUnix() > 0 {
		localConv, err := sm.db.Conversations.Get(ctx, peek.ConversationID)
		if err == nil && localConv != nil && peek.updatedAtUnix() <= localConv.UpdatedAt.Unix() {
			sm.logger.Debug("skipping conversation update — local cache is current",
				"conversation_id", peek.ConversationID,
				"payload_updated_at", peek.updatedAtUnix(),
				"local_updated_at", localConv.UpdatedAt.Unix())
			// Notify handler with local data (already up-to-date).
			if sm.handler != nil {
				_ = sm.handler.OnConversation(ctx, localConv)
			}
			return
		}
		// err != nil (ErrNotFound) or localConv == nil → first fetch, proceed.
		// peek.UpdatedAt > localConv.UpdatedAt → stale cache, proceed.
	}

	// Local cache is stale or missing — fetch full conversation from server.
	// Retry with a per-attempt timeout to handle transient RPC timeouts.
	// The daemon may receive multiple SendConversationUpdate broadcasts in quick
	// succession (e.g. during cleanup), causing concurrent get_conversation RPCs
	// that can hit database lock contention (SQLite) or network delays.
	// 30s per attempt allows sufficient headroom for:
	//   - Database lock contention under concurrent access
	//   - Multiple sequential queries (Get, CountUnread, GetPendingByConversation)
	//   - Network latency between daemon and server
	const (
		conversationFetchRetries    = 3
		conversationFetchPerAttempt = 30 * time.Second
	)
	var data json.RawMessage
	var err error
	for attempt := 0; attempt < conversationFetchRetries; attempt++ {
		// Use a per-attempt context with a shorter timeout so retries don't
		// consume the entire parent context deadline.
		attemptCtx, attemptCancel := context.WithTimeout(ctx, conversationFetchPerAttempt)
		data, err = sm.rpcFn(attemptCtx, "get_conversation", map[string]any{
			"conversation_id": peek.ConversationID,
		})
		attemptCancel()

		if err == nil {
			break // success
		}
		if attempt < conversationFetchRetries-1 {
			sm.logger.Debug("fetch conversation for update notification: retrying",
				"attempt", attempt+1,
				"error", err,
				"conversation_id", peek.ConversationID)
			// Brief backoff before retry (exponential: 500ms, 1s).
			backoff := time.Duration(500*(1<<attempt)) * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				sm.notifyHandler(ctx, update)
				return
			}
		}
	}
	if err != nil {
		sm.logger.Error("fetch conversation for update notification (all retries exhausted)",
			"error", err, "conversation_id", peek.ConversationID)
		// Fall back to notifying handler with minimal data (D-118 degraded).
		sm.notifyHandler(ctx, update)
		return
	}

	var result struct {
		Conversation   *model.Conversation   `json:"conversation"`
		RemoteCallings []*model.RemoteCalling `json:"remote_callings"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		sm.logger.Error("unmarshal get_conversation response", "error", err)
		sm.notifyHandler(ctx, update)
		return
	}
	if result.Conversation == nil {
		sm.logger.Error("get_conversation returned nil conversation",
			"conversation_id", peek.ConversationID)
		sm.notifyHandler(ctx, update)
		return
	}

	// Upsert into local DB (best-effort; log on failure).
	if err := sm.db.Conversations.Upsert(ctx, result.Conversation); err != nil {
		sm.logger.Error("upsert fetched conversation",
			"error", err, "conversation_id", peek.ConversationID)
	}

	// Clear stale remote callings before upserting new ones (D-137).
	// When agent_status != asking_user: HITL ended, clean all.
	// When new remote callings exist: replace with latest set.
	if sm.db.RemoteCallings != nil {
		if result.Conversation.AgentStatus != "asking_user" || len(result.RemoteCallings) > 0 {
			_ = sm.db.RemoteCallings.DeleteByConversation(ctx, result.Conversation.ID)
		}
	}

	// Upsert remote callings (best-effort, D-137).
	if sm.db.RemoteCallings != nil && len(result.RemoteCallings) > 0 {
		for _, rc := range result.RemoteCallings {
			if err := sm.db.RemoteCallings.Upsert(ctx, rc); err != nil {
				sm.logger.Error("upsert remote calling", "error", err, "remote_calling_id", rc.ID)
			}
		}
	}

	if sm.handler != nil {
		_ = sm.handler.OnConversation(ctx, result.Conversation)
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
