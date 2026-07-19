package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// BroadcastServer is the subset of WebSocketServer that BroadcastHelper needs.
type BroadcastServer interface {
	BroadcastUpdates(userID string, updates *protocol.PackageDataUpdates) error
}

// StreamingPayload is the JSON payload for UpdateTypeStreaming.
type StreamingPayload struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	StreamID       string `json:"stream_id"`
	Text           string `json:"text"`
	IsDone         bool   `json:"is_done"`
	IsAgent        bool   `json:"is_agent"`
	Timestamp      int64  `json:"timestamp"`
}

// TypingPayload is the JSON payload for UpdateTypeTyping.
type TypingPayload struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	IsTyping       bool   `json:"is_typing"`
	IsAgent        bool   `json:"is_agent"`
	Timestamp      int64  `json:"timestamp"`
}

// AgentStatusPayload is the JSON payload for UpdateTypeAgentStatus (D-087).
type AgentStatusPayload struct {
	UserID         string `json:"user_id"` // agent userID
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"` // thinking/tool_calling/generating/idle/asking_user
	Timestamp      int64  `json:"timestamp"`
}

// AgentTimeoutPayload is the JSON payload for UpdateTypeAgentTimeout (D-087).
type AgentTimeoutPayload struct {
	UserID         string `json:"user_id"` // agent userID
	ConversationID string `json:"conversation_id"`
	Reason         string `json:"reason"`
	Timestamp      int64  `json:"timestamp"`
}

// BroadcastHelper sends streaming and typing updates to users via WebSocket
// (C7). All broadcasts are fire-and-forget (D-007): errors are logged but not
// returned to the caller.
type BroadcastHelper struct {
	wsServer BroadcastServer
	registry *AgentRegistry // for IsAgent lookups (D-054 revised); nil = agent detection disabled (D-063)
	logger   Logger
}

// NewBroadcastHelper creates a BroadcastHelper backed by the given WebSocket
// broadcast server.
func NewBroadcastHelper(wsServer BroadcastServer, logger Logger) *BroadcastHelper {
	if logger == nil {
		logger = noopLogger{}
	}
	return &BroadcastHelper{
		wsServer: wsServer,
		logger:   logger,
	}
}

// SetRegistry sets the agent registry used to determine whether a userID
// belongs to a registered agent (D-054 revised). When nil, IsAgent defaults
// to false for all payloads.
func (bh *BroadcastHelper) SetRegistry(registry *AgentRegistry) {
	bh.registry = registry
}

// isAgent reports whether userID corresponds to a registered agent.
// Returns false when the registry is nil (nil-safe, D-063).
func (bh *BroadcastHelper) isAgent(userID string) bool {
	if bh.registry == nil {
		return false
	}
	_, ok := bh.registry.IsAgent(userID)
	return ok
}

// SendStreamUpdate broadcasts an ephemeral streaming update (Seq=0, D-050 /
// D-051) to both the human user and the agent user so that every participant
// sees the streamed text in real time.
// The ctx parameter is reserved for future cancellation support.
func (bh *BroadcastHelper) SendStreamUpdate(ctx context.Context, humanUserID, agentUserID, conversationID, streamID, text string, isDone bool) {
	_ = ctx // reserved for future cancellation
	payload, err := json.Marshal(StreamingPayload{
		UserID:         agentUserID,
		ConversationID: conversationID,
		StreamID:       streamID,
		Text:           text,
		IsDone:         isDone,
		IsAgent:        bh.isAgent(agentUserID),
		Timestamp:      time.Now().Unix(),
	})
	if err != nil {
		bh.logger.Error("broadcast: marshal stream payload failed", "error", err)
		return
	}

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{
				Seq:     0, // ephemeral
				Type:    protocol.UpdateTypeStreaming,
				Payload: payload,
			},
		},
	}

	// Broadcast to both the human user and the agent user (C7).
	for _, userID := range []string{humanUserID, agentUserID} {
		if err := bh.wsServer.BroadcastUpdates(userID, updates); err != nil {
			bh.logger.Error("broadcast: stream update failed", "user_id", userID, "error", err)
		}
	}
}

// SendTyping broadcasts an ephemeral typing indicator (Seq=0, D-050 / D-065)
// to targetUserID — typically the human user who should see the agent typing.
// agentUserID is the agent's identity, placed in the payload's user_id field.
// The ctx parameter is reserved for future cancellation support.
func (bh *BroadcastHelper) SendTyping(ctx context.Context, agentUserID, targetUserID, conversationID string, isTyping bool) {
	_ = ctx // reserved for future cancellation
	payload, err := json.Marshal(TypingPayload{
		UserID:         agentUserID,
		ConversationID: conversationID,
		IsTyping:       isTyping,
		IsAgent:        bh.isAgent(agentUserID),
		Timestamp:      time.Now().Unix(),
	})
	if err != nil {
		bh.logger.Error("broadcast: marshal typing payload failed", "error", err)
		return
	}

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{
				Seq:     0, // ephemeral
				Type:    protocol.UpdateTypeTyping,
				Payload: payload,
			},
		},
	}

	if err := bh.wsServer.BroadcastUpdates(targetUserID, updates); err != nil {
		bh.logger.Error("broadcast: typing update failed", "user_id", targetUserID, "error", err)
	}
}

// SendAgentStatus broadcasts an ephemeral agent status update (Seq=0, D-050 /
// D-087) to the human user. status is one of: thinking, tool_calling,
// generating, idle, asking_user.
func (bh *BroadcastHelper) SendAgentStatus(ctx context.Context, humanUserID, agentUserID, conversationID, status string) {
	_ = ctx
	payload, err := json.Marshal(AgentStatusPayload{
		UserID:         agentUserID,
		ConversationID: conversationID,
		Status:         status,
		Timestamp:      time.Now().Unix(),
	})
	if err != nil {
		bh.logger.Error("broadcast: marshal agent_status payload failed", "error", err)
		return
	}
	bh.broadcastEphemeral(humanUserID, protocol.UpdateTypeAgentStatus, payload)
}

// SendAgentTimeout broadcasts an ephemeral agent timeout update (Seq=0,
// D-050 / D-087) to the human user.
func (bh *BroadcastHelper) SendAgentTimeout(ctx context.Context, humanUserID, agentUserID, conversationID, reason string) {
	_ = ctx
	payload, err := json.Marshal(AgentTimeoutPayload{
		UserID:         agentUserID,
		ConversationID: conversationID,
		Reason:         reason,
		Timestamp:      time.Now().Unix(),
	})
	if err != nil {
		bh.logger.Error("broadcast: marshal agent_timeout payload failed", "error", err)
		return
	}
	bh.broadcastEphemeral(humanUserID, protocol.UpdateTypeAgentTimeout, payload)
}

// SendConversationUpdate broadcasts a lightweight conversation update notification
// (Seq=0, ephemeral). This implements the pull-on-notification pattern: the client
// receives a conversation_id and fetches full conversation state (including
// questions) via get_conversation RPC.
//
// The updatedAt parameter is included in the payload as "updated_at" (Unix seconds)
// when non-zero, allowing clients to detect stale updates (D-124).
func (bh *BroadcastHelper) SendConversationUpdate(ctx context.Context, humanUserID, conversationID string, updatedAt time.Time) {
	_ = ctx // reserved for future cancellation
	payloadMap := map[string]any{
		"conversation_id": conversationID,
		"action":          "update",
	}
	if !updatedAt.IsZero() {
		payloadMap["updated_at"] = updatedAt.Unix()
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		bh.logger.Error("broadcast: marshal conversation update payload failed", "error", err)
		return
	}
	bh.broadcastEphemeral(humanUserID, protocol.UpdateTypeConversation, payload)
}

// broadcastEphemeral sends a single ephemeral update (Seq=0) to one user.
func (bh *BroadcastHelper) broadcastEphemeral(targetUserID, updateType string, payload json.RawMessage) {
	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{
				Seq:     0, // ephemeral
				Type:    updateType,
				Payload: payload,
			},
		},
	}
	if err := bh.wsServer.BroadcastUpdates(targetUserID, updates); err != nil {
		bh.logger.Error("broadcast: ephemeral update failed", "user_id", targetUserID, "type", updateType, "error", err)
	}
}

// BroadcastRaw sends a pre-built PackageDataUpdates to the given user.
// Used by the resume handler to deliver persisted message updates in real-time.
func (bh *BroadcastHelper) BroadcastRaw(targetUserID string, updates *protocol.PackageDataUpdates) error {
	return bh.wsServer.BroadcastUpdates(targetUserID, updates)
}

// BroadcastMessageUpdate broadcasts persisted message updates to each user
// with their real DB-allocated seq numbers. This is needed after the agent
// executor persists a message via store.SendMessage — the DB records are
// created, but without this broadcast the clients won't receive the update
// until the next sync_updates call.
//
// userUpdates must come from store.SendMessageResult.Updates (each entry
// already has the correct UserID, Seq, Payload, and CreatedAt).
func (bh *BroadcastHelper) BroadcastMessageUpdate(ctx context.Context, userUpdates []model.UserUpdate) {
	_ = ctx // reserved for future cancellation
	for _, u := range userUpdates {
		updates := &protocol.PackageDataUpdates{
			Updates: []protocol.PackageDataUpdate{
				{
					Seq:       u.Seq,
					Type:      protocol.UpdateTypeMessage,
					Payload:   u.Payload,
					CreatedAt: u.CreatedAt,
				},
			},
		}
		if err := bh.wsServer.BroadcastUpdates(u.UserID, updates); err != nil {
			bh.logger.Error("broadcast: message update failed", "user_id", u.UserID, "seq", u.Seq, "error", err)
		}
	}
}
