package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// setTypingParams is the JSON-decoded representation of the client-supplied
// parameters for the "set_typing" method.
type setTypingParams struct {
	ConversationID string `json:"conversation_id"`
	IsTyping       bool   `json:"is_typing"`
}

// setTypingResponse is the success response payload returned to the client.
type setTypingResponse struct {
	Status string `json:"status"`
}

// typingBroadcastPayload is the JSON payload embedded in the typing update.
type typingBroadcastPayload struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	IsTyping       bool   `json:"is_typing"`
	Timestamp      int64  `json:"timestamp"`
}

// --------------------------------------------------------------------------
// Rate limiter (inline token bucket)
// --------------------------------------------------------------------------

// typingRateLimiter implements a simple per-user-per-conversation rate limiter
// that allows at most one typing event per second.
type typingRateLimiter struct {
	mu         sync.Mutex
	lastTime   time.Time
	lastAccess time.Time // updated on every allow() call; used for cleanup
}

// allow returns true if at least one second has elapsed since the last
// allowed event, and records the current time as the last allowed time.
// Every call (whether allowed or not) refreshes lastAccess so the cleanup
// goroutine can evict stale entries.
func (rl *typingRateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	rl.lastAccess = now
	if now.Sub(rl.lastTime) < time.Second {
		return false
	}
	rl.lastTime = now
	return true
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// setTypingHandler implements MethodHandler for the "set_typing" method.
// It broadcasts ephemeral typing indicators to conversation members without
// persisting anything to the database or enqueueing to MQ (Seq=0).
type setTypingHandler struct {
	store       store.StoreAPI
	broadcastFn func(userID string, updates *protocol.PackageDataUpdates) error
	logger      server.Logger
	limiters    sync.Map // key: "userID:convID" -> *typingRateLimiter
}

// NewSetTypingHandler creates a setTypingHandler and starts a background
// goroutine that periodically evicts stale rate-limit entries.
func NewSetTypingHandler(
	store store.StoreAPI,
	broadcastFn func(userID string, updates *protocol.PackageDataUpdates) error,
	logger server.Logger,
) *setTypingHandler {
	if logger == nil {
		logger = defaultLogger{}
	}
	h := &setTypingHandler{
		store:       store,
		broadcastFn: broadcastFn,
		logger:      logger,
	}
	go h.cleanupLoop()
	return h
}

// cleanupLoop runs every 5 minutes and evicts rate-limit entries that have
// not been accessed in the last 10 minutes. This prevents unbounded growth
// of the limiters map when many ephemeral conversations are created.
func (h *setTypingHandler) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		h.cleanupStaleLimiters()
	}
}

// cleanupStaleLimiters removes all rate-limit entries whose lastAccess time
// is older than 10 minutes.
func (h *setTypingHandler) cleanupStaleLimiters() {
	cutoff := time.Now().Add(-10 * time.Minute)
	h.limiters.Range(func(key, value any) bool {
		rl := value.(*typingRateLimiter)
		rl.mu.Lock()
		lastAccess := rl.lastAccess
		rl.mu.Unlock()
		if lastAccess.Before(cutoff) {
			h.limiters.Delete(key)
		}
		return true
	})
}

// HandleRequest implements MethodHandler. It processes a "set_typing" RPC
// call: validates parameters, verifies the caller is a member of the
// conversation, and broadcasts an ephemeral typing update to all members
// (including the caller, D-050).
//
// The update uses Seq=0 so that it is never persisted, never delivered via
// sync_updates, and silently dropped for offline users.
func (h *setTypingHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params setTypingParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	if params.ConversationID == "" {
		return nil, protocol.NewValidationError("missing required field: conversation_id")
	}

	// 2. Fetch conversation.
	conv, err := h.store.ConversationStore().Get(ctx, params.ConversationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("get conversation: %w", err))
	}

	// 3. Verify membership.
	callerID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, callerID) {
		return nil, protocol.NewPermissionDeniedError("user is not a member of the conversation")
	}

	// 4. Rate limit (per user per conversation, 1/sec).
	limiterKey := callerID + ":" + params.ConversationID
	limiterI, _ := h.limiters.LoadOrStore(limiterKey, &typingRateLimiter{})
	limiter := limiterI.(*typingRateLimiter)
	if !limiter.allow() {
		// Silently return OK — rate limited requests are not errors.
		return marshalResponse(setTypingResponse{Status: "ok"})
	}

	// 5. Build payload.
	payload, err := json.Marshal(typingBroadcastPayload{
		UserID:         callerID,
		ConversationID: params.ConversationID,
		IsTyping:       params.IsTyping,
		Timestamp:      time.Now().Unix(),
	})
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("marshal typing payload: %w", err))
	}

	update := protocol.PackageDataUpdate{
		Seq:     0, // ephemeral
		Type:    protocol.UpdateTypeTyping,
		Payload: payload,
	}
	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{update},
	}

	// 6. Broadcast to ALL members (including the caller, D-050).
	for _, memberID := range members {
		if err := h.broadcastFn(memberID, updates); err != nil {
			h.logger.Info("set_typing: broadcast failed (fire-and-forget)", "userID", memberID, "error", err)
		}
	}

	// 7. Return success.
	return marshalResponse(setTypingResponse{Status: "ok"})
}
