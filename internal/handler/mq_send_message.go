package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// MQ task payload types for send_message
// --------------------------------------------------------------------------

// mqSendMessageRecipient describes the push data for a single conversation
// member inside the MQ task payload.
type mqSendMessageRecipient struct {
	UserID  string                       `json:"user_id"`
	Updates []protocol.PackageDataUpdate `json:"updates"`
}

// mqSendMessageTaskPayload is the MQ task payload used to fan out the message
// to each conversation member for real-time delivery.
type mqSendMessageTaskPayload struct {
	Recipients []mqSendMessageRecipient `json:"recipients"`
}

// --------------------------------------------------------------------------
// Task handler factory
// --------------------------------------------------------------------------

// NewSendMessageTaskHandler returns an mq.TaskHandler-compatible function that
// broadcasts real-time updates to each recipient's active connections. It is
// invoked when the broker dequeues a TypeSendMessage task.
//
// broadcastFn is typically (*WebSocketServer).BroadcastUpdates; logger is used
// for structured error reporting.
func NewSendMessageTaskHandler(
	broadcastFn func(userID string, updates *protocol.PackageDataUpdates) error,
	logger server.Logger,
) func(ctx context.Context, task *mq.Task) error {
	return func(ctx context.Context, task *mq.Task) error {
		if task == nil {
			return fmt.Errorf("send_message task: nil task")
		}

		var payload mqSendMessageTaskPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			if logger != nil {
				logger.Error("send_message task: unmarshal payload: %v", err)
			}
			// Data is already persisted; retrying will not help (D-007).
			return nil
		}

		for _, r := range payload.Recipients {
			updates := &protocol.PackageDataUpdates{Updates: r.Updates}
			if err := broadcastFn(r.UserID, updates); err != nil {
				if logger != nil {
					logger.Error("send_message task: broadcast to user %s: %v", r.UserID, err)
				}
				// Broadcast failure is non-fatal: the data is persisted and
				// will be delivered via sync_updates (D-007).
				continue
			}
		}
		return nil
	}
}
