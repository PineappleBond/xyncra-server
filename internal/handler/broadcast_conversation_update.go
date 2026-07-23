// broadcast_conversation_update.go provides a reusable function for creating
// persisted UserUpdate records and enqueuing MQ push notifications for
// conversation members. This replaces the ephemeral (Seq=0) broadcasts used
// by RemoteCalling insert, agent_resume piggyback, and cancel_remote_calls.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"gorm.io/gorm"
)

// BroadcastConversationUpdateFunc is the function signature for broadcasting
// persisted conversation updates. Agent package callers can receive this as a
// dependency injection to avoid importing the handler package (which would
// create a circular dependency since handler imports agent).
type BroadcastConversationUpdateFunc func(
	ctx context.Context,
	conversationID string,
	memberIDs []string,
	action string,
) error

// conversationUpdatePayload is the JSON payload stored inside the
// conversation-type UserUpdate. It carries the conversation ID and an action
// string so clients can dispatch it through the same handler used for
// create/delete/restore events.
type conversationUpdatePayload struct {
	ConversationID string `json:"conversation_id"`
	Action         string `json:"action"`
}

// broadcastConversationUpdateToMembers creates persisted UserUpdate records for
// each conversation member and enqueues an MQ task to push the updates to
// online devices. This is the standard flow for conversation state changes that
// need to survive sync_updates pulls (as opposed to ephemeral Seq=0 broadcasts).
//
// The function is safe for concurrent use — it is stateless and receives all
// dependencies as parameters.
//
// Parameters:
//   - ctx: request context for tracing and cancellation
//   - store: data access layer for UserUpdate persistence
//   - broker: message queue for push notification (may be nil, in which case
//     only persistence is performed)
//   - logger: structured logger (may be nil, defaults to slog.Default())
//   - conversationID: the conversation that changed
//   - memberIDs: user IDs to receive the update (typically conversation members)
//   - action: the action string (e.g. "update", "remote_calling_created",
//     "remote_calling_resolved", "cancel_remote_calls")
//
// Errors during UserUpdate creation are returned to the caller so they can
// decide how to handle them. MQ enqueue errors are logged but not returned
// (fire-and-forget, D-007: MQ failures do not affect data integrity — the
// update was already persisted and will be delivered via sync_updates on the
// next pull).
func broadcastConversationUpdateToMembers(
	ctx context.Context,
	store store.StoreAPI,
	broker mq.Broker,
	logger server.Logger,
	conversationID string,
	memberIDs []string,
	action string,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	if len(memberIDs) == 0 {
		return nil
	}

	// Build the payload once — shared by all members.
	payload, err := json.Marshal(conversationUpdatePayload{
		ConversationID: conversationID,
		Action:         action,
	})
	if err != nil {
		return fmt.Errorf("broadcast_conversation_update: marshal payload: %w", err)
	}

	now := time.Now()
	var updates []model.UserUpdate
	var recipients []sendMessageRecipient

	// Allocate seq and create UserUpdate records in a single transaction to
	// prevent TOCTOU races when concurrent operations target the same user
	// (mirrors the pattern used by create_conversation.go).
	if err := store.Transaction(ctx, func(tx *gorm.DB) error {
		updates = make([]model.UserUpdate, 0, len(memberIDs))
		recipients = make([]sendMessageRecipient, 0, len(memberIDs))

		for _, memberID := range memberIDs {
			var latestSeq uint32
			if err := tx.Model(&model.UserUpdate{}).
				Where("user_id = ?", memberID).
				Select("COALESCE(MAX(seq), 0)").
				Scan(&latestSeq).Error; err != nil {
				return fmt.Errorf("broadcast_conversation_update: get latest seq for user %s: %w", memberID, err)
			}
			newSeq := latestSeq + 1

			updates = append(updates, model.UserUpdate{
				ID:        uuid.New().String(),
				UserID:    memberID,
				Seq:       newSeq,
				Type:      protocol.UpdateTypeConversation,
				Payload:   payload,
				CreatedAt: now,
			})

			recipients = append(recipients, sendMessageRecipient{
				UserID: memberID,
				Updates: []protocol.PackageDataUpdate{
					{
						Seq:       newSeq,
						Type:      protocol.UpdateTypeConversation,
						Payload:   payload,
						CreatedAt: now,
					},
				},
			})
		}

		if len(updates) > 0 {
			if err := tx.CreateInBatches(updates, 100).Error; err != nil {
				return fmt.Errorf("broadcast_conversation_update: insert user updates: %w", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// MQ broadcast to all members' online devices (fire-and-forget, D-007).
	// Failures are logged but not returned — the update was already persisted
	// and will be delivered via sync_updates on the next pull.
	if broker != nil {
		taskPayload := sendMessageTaskPayload{Recipients: recipients}
		payloadBytes, err := json.Marshal(taskPayload)
		if err != nil {
			logger.Error("broadcast_conversation_update: failed to marshal MQ payload", "error", err)
			return nil // non-fatal: update was persisted
		}

		task := &mq.Task{
			Type:    mq.TypeSendMessage,
			Payload: payloadBytes,
		}
		if _, err := broker.Enqueue(ctx, task); err != nil {
			logger.Info("broadcast_conversation_update: MQ enqueue failed (fire-and-forget)", "error", err)
		}
	}

	return nil
}

// NewBroadcastConversationUpdateFunc returns a BroadcastConversationUpdateFunc
// that captures the store, broker, and logger dependencies. This is intended
// for dependency injection into the agent package (which cannot import handler
// directly due to circular dependency).
//
// Example usage in agent package:
//
//	type BroadcastConversationUpdateFunc func(ctx, conversationID, memberIDs, action) error
//	// Set via ExecutorOption or direct field assignment.
func NewBroadcastConversationUpdateFunc(
	store store.StoreAPI,
	broker mq.Broker,
	logger server.Logger,
) BroadcastConversationUpdateFunc {
	return func(ctx context.Context, conversationID string, memberIDs []string, action string) error {
		return broadcastConversationUpdateToMembers(ctx, store, broker, logger, conversationID, memberIDs, action)
	}
}
