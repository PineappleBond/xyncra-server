package agent

import (
	"context"
	"encoding/json"
	"time"

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
	Timestamp      int64  `json:"timestamp"`
}

// TypingPayload is the JSON payload for UpdateTypeTyping.
type TypingPayload struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	IsTyping       bool   `json:"is_typing"`
	Timestamp      int64  `json:"timestamp"`
}

// AgentStatusPayload is the JSON payload for UpdateTypeAgentStatus (D-087).
type AgentStatusPayload struct {
	UserID         string `json:"user_id"` // agent userID
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"` // thinking/tool_calling/generating/idle/asking_user
	Timestamp      int64  `json:"timestamp"`
}

// AgentQuestionPayload is the JSON payload for UpdateTypeAgentQuestion (D-087).
type AgentQuestionPayload struct {
	UserID         string `json:"user_id"` // agent userID
	ConversationID string `json:"conversation_id"`
	Question       string `json:"question"`
	CheckpointID   string `json:"checkpoint_id"`
	InterruptID    string `json:"interrupt_id"`
	Timestamp      int64  `json:"timestamp"`
}

// AgentCheckpointCreatedPayload is the JSON payload for
// UpdateTypeAgentCheckpointCreated (D-087).
type AgentCheckpointCreatedPayload struct {
	UserID         string `json:"user_id"` // agent userID
	ConversationID string `json:"conversation_id"`
	CheckpointID   string `json:"checkpoint_id"`
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

// SendAgentQuestion broadcasts an ephemeral agent question update (Seq=0,
// D-050 / D-087) to the human user during HITL interruption.
func (bh *BroadcastHelper) SendAgentQuestion(ctx context.Context, humanUserID, agentUserID, conversationID, question, checkpointID, interruptID string) {
	_ = ctx
	payload, err := json.Marshal(AgentQuestionPayload{
		UserID:         agentUserID,
		ConversationID: conversationID,
		Question:       question,
		CheckpointID:   checkpointID,
		InterruptID:    interruptID,
		Timestamp:      time.Now().Unix(),
	})
	if err != nil {
		bh.logger.Error("broadcast: marshal agent_question payload failed", "error", err)
		return
	}
	bh.broadcastEphemeral(humanUserID, protocol.UpdateTypeAgentQuestion, payload)
}

// SendAgentCheckpointCreated broadcasts an ephemeral checkpoint-created update
// (Seq=0, D-050 / D-087) to the human user.
func (bh *BroadcastHelper) SendAgentCheckpointCreated(ctx context.Context, humanUserID, agentUserID, conversationID, checkpointID string) {
	_ = ctx
	payload, err := json.Marshal(AgentCheckpointCreatedPayload{
		UserID:         agentUserID,
		ConversationID: conversationID,
		CheckpointID:   checkpointID,
		Timestamp:      time.Now().Unix(),
	})
	if err != nil {
		bh.logger.Error("broadcast: marshal agent_checkpoint_created payload failed", "error", err)
		return
	}
	bh.broadcastEphemeral(humanUserID, protocol.UpdateTypeAgentCheckpointCreated, payload)
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
func (bh *BroadcastHelper) SendConversationUpdate(ctx context.Context, humanUserID, conversationID string) {
	_ = ctx // reserved for future cancellation
	payload, err := json.Marshal(map[string]string{
		"conversation_id": conversationID,
		"action":          "update",
	})
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
