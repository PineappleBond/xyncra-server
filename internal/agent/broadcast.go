package agent

import (
	"context"
	"encoding/json"
	"log"
	"os"
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

// BroadcastHelper sends streaming and typing updates to users via WebSocket
// (C7). All broadcasts are fire-and-forget (D-007): errors are logged but not
// returned to the caller.
type BroadcastHelper struct {
	wsServer BroadcastServer
	logger   *log.Logger
}

// NewBroadcastHelper creates a BroadcastHelper backed by the given WebSocket
// broadcast server.
func NewBroadcastHelper(wsServer BroadcastServer) *BroadcastHelper {
	return &BroadcastHelper{
		wsServer: wsServer,
		logger:   log.New(os.Stderr, "[agent-broadcast] ", log.LstdFlags),
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
		bh.logger.Printf("SendStreamUpdate: marshal payload: %v", err)
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
			bh.logger.Printf("SendStreamUpdate: broadcast to user %s failed (fire-and-forget): %v", userID, err)
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
		bh.logger.Printf("SendTyping: marshal payload: %v", err)
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
		bh.logger.Printf("SendTyping: broadcast to user %s failed (fire-and-forget): %v", targetUserID, err)
	}
}
