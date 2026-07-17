package handler

import (
	"context"
	"encoding/json"
	"errors"
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

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// restoreConversationParams is the JSON-decoded representation of the client-supplied
// parameters for the "restore_conversation" method.
type restoreConversationParams struct {
	ConversationID string `json:"conversation_id"` // required
}

// restoreConversationResponse is the success response payload returned to the client.
type restoreConversationResponse struct {
	Conversation         *model.Conversation `json:"conversation"`
	RestoredMessageCount int64               `json:"restored_message_count"`
}

// restoreConversationUpdatePayload is the payload stored inside the
// restore_conversation UserUpdate. It carries the conversation ID and the
// action performed so that clients can update local state.
type restoreConversationUpdatePayload struct {
	ConversationID string `json:"conversation_id"`
	Action         string `json:"action"` // "restore"
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// restoreConversationHandler implements MethodHandler for the "restore_conversation" method.
// It performs a cascade restore on the conversation and all its messages (D-015).
//
// The handler is stateless (only holds immutable dependency references) and
// therefore safe for concurrent use.
type restoreConversationHandler struct {
	store  store.StoreAPI
	broker mq.Broker
	logger server.Logger
}

// NewRestoreConversationHandler creates a restoreConversationHandler backed by the given Store
// and Broker. The broker is used to enqueue a fire-and-forget MQ task that
// pushes the restore_conversation update to all conversation members' online devices.
func NewRestoreConversationHandler(store store.StoreAPI, broker mq.Broker, logger server.Logger) *restoreConversationHandler {
	if logger == nil {
		logger = defaultLogger{}
	}
	return &restoreConversationHandler{store: store, broker: broker, logger: logger}
}

// HandleRequest implements MethodHandler. It processes a "restore_conversation" RPC
// call: parses parameters, validates the conversation_id, fetches the conversation
// (including soft-deleted), verifies membership, then performs a cascade restore of
// the conversation and all its messages in a single transaction (D-015).
func (h *restoreConversationHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params restoreConversationParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Validate required fields.
	if params.ConversationID == "" {
		return nil, protocol.NewValidationError("missing required field: conversation_id")
	}

	convID := params.ConversationID

	// 3. Fetch conversation (including soft-deleted) and verify existence.
	conv, err := h.store.ConversationStore().GetUnscoped(ctx, convID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("get conversation: %w", err))
	}

	// 4. Verify membership (C-3).
	userID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, userID) {
		return nil, protocol.NewPermissionDeniedError("user is not a member of the conversation")
	}

	// 5. Idempotent: if conversation is NOT soft-deleted, return current state (D-015).
	if !conv.DeletedAt.Valid || conv.DeletedAt.Time.IsZero() {
		// Already active — return as-is with zero restored count.
		restoredConv, getErr := h.store.ConversationStore().Get(ctx, convID)
		if getErr != nil {
			return nil, protocol.NewInternalError(fmt.Errorf("get conversation: %w", getErr))
		}
		return marshalResponse(restoreConversationResponse{
			Conversation:         restoredConv,
			RestoredMessageCount: 0,
		})
	}

	// 6. Cascade restore in a transaction (D-015).
	var restoredMessageCount int64
	err = h.store.Transaction(ctx, func(tx *gorm.DB) error {
		// 6a. Restore the conversation.
		result := tx.Unscoped().Model(&model.Conversation{}).
			Where("id = ? AND deleted_at IS NOT NULL", convID).
			Update("deleted_at", nil)
		if result.Error != nil {
			return fmt.Errorf("restore conversation: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return store.ErrNotFound
		}

		// 6b. 仅恢复因本次会话删除而级联删除的消息（deleted_at 等于会话的 deleted_at），
		// 不影响之前已单独删除的消息。使用原生 SQL 确保时间戳精确匹配，
		// 避免 GORM 软删除机制干扰查询和更新行为。
		convDeletedAt := conv.DeletedAt.Time
		msgResult := tx.Exec(
			"UPDATE messages SET deleted_at = NULL WHERE conversation_id = ? AND deleted_at = ?",
			convID, convDeletedAt,
		)
		if msgResult.Error != nil {
			return fmt.Errorf("restore messages: %w", msgResult.Error)
		}
		restoredMessageCount = msgResult.RowsAffected

		// 6c. Recalculate LastProcessedMessageID and LastMessageAt from
		// the latest message (unscoped, to include restored messages).
		var latestMsg model.Message
		if err := tx.Unscoped().Model(&model.Message{}).
			Where("conversation_id = ?", convID).
			Order("message_id DESC").
			First(&latestMsg).Error; err == nil {
			// Found a latest message — update conversation metadata.
			updateResult := tx.Model(&model.Conversation{}).
				Where("id = ?", convID).
				Updates(map[string]interface{}{
					"last_processed_message_id": latestMsg.MessageID,
					"last_message_at":           latestMsg.CreatedAt,
				})
			if updateResult.Error != nil {
				return fmt.Errorf("update conversation metadata: %w", updateResult.Error)
			}
		}
		// If no messages found, leave metadata as-is (conversation-level
		// restore still succeeded).

		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("restore conversation: %w", err))
	}

	// 7. Create UserUpdates for ALL conversation members (D-015: cascade
	// restore affects all members' devices) and broadcast via MQ.
	updatePayload, _ := json.Marshal(restoreConversationUpdatePayload{
		ConversationID: convID,
		Action:         "restore",
	})

	now := time.Now()
	updates := make([]model.UserUpdate, 0, len(members))
	recipients := make([]sendMessageRecipient, 0, len(members))

	for _, memberID := range members {
		latestSeq, err := h.store.UserUpdateStore().GetLatestSeq(ctx, memberID)
		if err != nil {
			h.logger.Error("restore_conversation: failed to get latest seq (skipping UserUpdate)", "userID", memberID, "error", err)
			continue
		}
		newSeq := latestSeq + 1

		updates = append(updates, model.UserUpdate{
			ID:        uuid.New().String(),
			UserID:    memberID,
			Seq:       newSeq,
			Type:      protocol.UpdateTypeConversation,
			Payload:   updatePayload,
			CreatedAt: now,
		})

		recipients = append(recipients, sendMessageRecipient{
			UserID: memberID,
			Updates: []protocol.PackageDataUpdate{
				{
					Seq:       newSeq,
					Type:      protocol.UpdateTypeConversation,
					Payload:   updatePayload,
					CreatedAt: now,
				},
			},
		})
	}

	if err := h.store.UserUpdateStore().Create(ctx, updates); err != nil {
		// UserUpdate creation failure does not affect the main flow (D-007
		// fire-and-forget spirit). The conversation was already restored.
		h.logger.Error("restore_conversation: failed to create UserUpdates", "error", err)
	}

	// 8. MQ broadcast to all members' online devices (fire-and-forget, D-007).
	broadcastRestoreConversationUpdates(h.broker, h.logger, recipients)

	// 9. Fetch the restored conversation and return.
	restoredConv, err := h.store.ConversationStore().Get(ctx, convID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("get restored conversation: %w", err))
	}

	return marshalResponse(restoreConversationResponse{
		Conversation:         restoredConv,
		RestoredMessageCount: restoredMessageCount,
	})
}

// broadcastRestoreConversationUpdates enqueues a fire-and-forget MQ task to push
// the restore_conversation update to all conversation members' online devices. It
// reuses the sendMessageRecipient / sendMessageTaskPayload structures and the
// TypeSendMessage task type because they are sufficiently generic.
//
// Errors are logged but never returned (D-007: MQ failures do not affect data
// integrity — the update was already persisted and will be delivered via
// sync_updates on the next pull).
func broadcastRestoreConversationUpdates(broker mq.Broker, logger server.Logger, recipients []sendMessageRecipient) {
	if broker == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	taskPayload := sendMessageTaskPayload{Recipients: recipients}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Error("restore_conversation: failed to marshal MQ payload", "error", err)
		return
	}

	task := &mq.Task{
		Type:    mq.TypeSendMessage,
		Payload: payloadBytes,
	}
	if _, err := broker.Enqueue(context.Background(), task); err != nil {
		logger.Info("restore_conversation: MQ enqueue failed (fire-and-forget)", "error", err)
	}
}
