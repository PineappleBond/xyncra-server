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

// streamTextParams is the JSON-decoded representation of the client-supplied
// parameters for the "stream_text" method.
type streamTextParams struct {
	ConversationID string `json:"conversation_id"`
	StreamID       string `json:"stream_id"`
	Text           string `json:"text"`
	IsDone         bool   `json:"is_done"`
}

// streamTextResponse is the success response payload returned to the client.
type streamTextResponse struct {
	Status string `json:"status"`
}

// streamingBroadcastPayload is the JSON payload embedded in the streaming update.
type streamingBroadcastPayload struct {
	StreamID       string `json:"stream_id"`
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Text           string `json:"text"`
	IsDone         bool   `json:"is_done"`
	Timestamp      int64  `json:"timestamp"`
}

// --------------------------------------------------------------------------
// Rate limiter (inline token bucket)
// --------------------------------------------------------------------------

// streamingRateLimiter implements a per-user-per-conversation rate limiter
// that allows at most one streaming event per 50ms (20/s).
type streamingRateLimiter struct {
	mu         sync.Mutex
	lastTime   time.Time
	lastAccess time.Time
}

func (rl *streamingRateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	rl.lastAccess = now
	if now.Sub(rl.lastTime) < 50*time.Millisecond {
		return false
	}
	rl.lastTime = now
	return true
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// streamTextHandler implements MethodHandler for the "stream_text" method.
// It broadcasts ephemeral streaming text updates to conversation members
// without persisting anything (Seq=0, D-051).
type streamTextHandler struct {
	store       store.StoreAPI
	broadcastFn func(userID string, updates *protocol.PackageDataUpdates) error
	logger      server.Logger
	limiters    sync.Map // key: "userID:convID" -> *streamingRateLimiter
}

// NewStreamTextHandler creates a streamTextHandler and starts a background
// goroutine that periodically evicts stale rate-limit entries.
func NewStreamTextHandler(
	store store.StoreAPI,
	broadcastFn func(userID string, updates *protocol.PackageDataUpdates) error,
	logger server.Logger,
) *streamTextHandler {
	if logger == nil {
		logger = defaultLogger{}
	}
	h := &streamTextHandler{
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
func (h *streamTextHandler) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		h.cleanupStaleLimiters()
	}
}

// cleanupStaleLimiters removes all rate-limit entries whose lastAccess time
// is older than 10 minutes.
func (h *streamTextHandler) cleanupStaleLimiters() {
	cutoff := time.Now().Add(-10 * time.Minute)
	h.limiters.Range(func(key, value any) bool {
		rl := value.(*streamingRateLimiter)
		rl.mu.Lock()
		lastAccess := rl.lastAccess
		rl.mu.Unlock()
		if lastAccess.Before(cutoff) {
			h.limiters.Delete(key)
		}
		return true
	})
}

// HandleRequest implements MethodHandler. It processes a "stream_text" RPC
// call: validates parameters, verifies the caller is a member of the
// conversation, and broadcasts an ephemeral streaming update to all members
// (including the caller, D-050).
//
// The update uses Seq=0 so that it is never persisted, never delivered via
// sync_updates, and silently dropped for offline users.
func (h *streamTextHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params streamTextParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	if params.ConversationID == "" {
		return nil, protocol.NewValidationError("missing required field: conversation_id")
	}
	if params.StreamID == "" {
		return nil, protocol.NewValidationError("missing required field: stream_id")
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

	// 4. Rate limit (per user per conversation, 50ms / 20/s).
	limiterKey := callerID + ":" + params.ConversationID
	limiterI, _ := h.limiters.LoadOrStore(limiterKey, &streamingRateLimiter{})
	limiter := limiterI.(*streamingRateLimiter)
	if !limiter.allow() {
		// Silently return OK — rate limited requests are not errors.
		return marshalResponse(streamTextResponse{Status: "ok"})
	}

	// 5. Build payload.
	payload, err := json.Marshal(streamingBroadcastPayload{
		StreamID:       params.StreamID,
		UserID:         callerID,
		ConversationID: params.ConversationID,
		Text:           params.Text,
		IsDone:         params.IsDone,
		Timestamp:      time.Now().Unix(),
	})
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("marshal streaming payload: %w", err))
	}

	update := protocol.PackageDataUpdate{
		Seq:     0, // ephemeral
		Type:    protocol.UpdateTypeStreaming,
		Payload: payload,
	}
	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{update},
	}

	// 6. Broadcast to ALL members (including the caller, D-050).
	for _, memberID := range members {
		if err := h.broadcastFn(memberID, updates); err != nil {
			h.logger.Info("stream_text: broadcast failed (fire-and-forget)", "userID", memberID, "error", err)
		}
	}

	// 7. Return success.
	return marshalResponse(streamTextResponse{Status: "ok"})
}
