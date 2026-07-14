package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

// deleteConversationParams is the JSON-decoded representation of the client-supplied
// parameters for the "delete_conversation" method.
type deleteConversationParams struct {
	ConversationID string `json:"conversation_id"` // required
}

// deleteConversationResponse is the success response payload returned to the client.
type deleteConversationResponse struct {
	Status              string `json:"status"`                // "ok"
	DeletedMessageCount int64  `json:"deleted_message_count"` // cascade-deleted message count
}

// deleteConversationUpdatePayload is the payload stored inside the
// delete_conversation UserUpdate. It carries the conversation ID and the
// action performed so that clients can update local state.
type deleteConversationUpdatePayload struct {
	ConversationID string `json:"conversation_id"`
	Action         string `json:"action"` // "delete"
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// deleteConversationHandler implements MethodHandler for the "delete_conversation" method.
// It performs a cascade soft delete on the conversation and all its messages (D-013).
//
// The handler is stateless (only holds immutable dependency references) and
// therefore safe for concurrent use.
type deleteConversationHandler struct {
	store  store.StoreAPI
	broker mq.Broker
}

// NewDeleteConversationHandler creates a deleteConversationHandler backed by the given Store
// and Broker. The broker is used to enqueue a fire-and-forget MQ task that
// pushes the delete_conversation update to all conversation members' online devices.
func NewDeleteConversationHandler(store store.StoreAPI, broker mq.Broker) *deleteConversationHandler {
	return &deleteConversationHandler{store: store, broker: broker}
}

// HandleRequest implements MethodHandler. It processes a "delete_conversation" RPC
// call: parses parameters, validates the conversation_id, fetches the
// conversation, verifies membership, then performs a cascade soft delete of
// the conversation and all its messages in a single transaction (D-013).
func (h *deleteConversationHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params deleteConversationParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Validate required fields.
	if params.ConversationID == "" {
		return nil, protocol.NewValidationError("missing required field: conversation_id")
	}

	convID := params.ConversationID

	// 3. Fetch conversation and verify existence.
	conv, err := h.store.ConversationStore().Get(ctx, convID)
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

	// 5. Cascade soft delete in a transaction (D-013).
	var deletedMessageCount int64
	// 使用统一时间戳，确保级联删除的消息共享同一个 deleted_at 值，
	// 以便恢复时区分"因会话删除而级联删除的消息"和"之前已单独删除的消息"。
	now := time.Now().Truncate(time.Millisecond)
	err = h.store.Transaction(ctx, func(tx *gorm.DB) error {
		// 5a. Count non-deleted messages before deleting (GORM 自动过滤已删除记录).
		if err := tx.Model(&model.Message{}).Where("conversation_id = ?", convID).Count(&deletedMessageCount).Error; err != nil {
			return fmt.Errorf("count messages: %w", err)
		}

		// 5b. 软删除会话 — 使用原生 SQL 确保 deleted_at 值被正确设置。
		// GORM 的软删除机制可能拦截 Update("deleted_at", ...) 并覆盖显式值，
		// 因此必须使用 Exec 直接执行 SQL。
		result := tx.Exec(
			"UPDATE conversations SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL",
			now, convID,
		)
		if result.Error != nil {
			return fmt.Errorf("delete conversation: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return store.ErrNotFound
		}

		// 5c. 软删除所有未删除的消息 — 使用同一时间戳，通过原生 SQL 确保一致性.
		if err := tx.Exec(
			"UPDATE messages SET deleted_at = ? WHERE conversation_id = ? AND deleted_at IS NULL",
			now, convID,
		).Error; err != nil {
			return fmt.Errorf("delete messages: %w", err)
		}

		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("delete conversation: %w", err))
	}

	// 6. Create UserUpdates for ALL conversation members (D-013: cascade
	// delete affects all members' devices) and broadcast via MQ.
	updatePayload, _ := json.Marshal(deleteConversationUpdatePayload{
		ConversationID: convID,
		Action:         "delete",
	})

	// now 已在事务前声明（与级联删除使用同一时间戳）
	updates := make([]model.UserUpdate, 0, len(members))
	recipients := make([]sendMessageRecipient, 0, len(members))

	for _, memberID := range members {
		latestSeq, err := h.store.UserUpdateStore().GetLatestSeq(ctx, memberID)
		if err != nil {
			log.Printf("delete_conversation: failed to get latest seq for user %s (skipping UserUpdate): %v", memberID, err)
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
		// fire-and-forget spirit). The conversation was already soft-deleted.
		log.Printf("delete_conversation: failed to create UserUpdates: %v", err)
	}

	// 7. MQ broadcast to all members' online devices (fire-and-forget, D-007).
	broadcastDeleteConversationUpdates(h.broker, recipients)

	// 8. Return response.
	return marshalResponse(deleteConversationResponse{
		Status:              "ok",
		DeletedMessageCount: deletedMessageCount,
	})
}

// broadcastDeleteConversationUpdates enqueues a fire-and-forget MQ task to push
// the delete_conversation update to all conversation members' online devices. It
// reuses the sendMessageRecipient / sendMessageTaskPayload structures and the
// TypeSendMessage task type because they are sufficiently generic.
//
// Errors are logged but never returned (D-007: MQ failures do not affect data
// integrity — the update was already persisted and will be delivered via
// sync_updates on the next pull).
func broadcastDeleteConversationUpdates(broker mq.Broker, recipients []sendMessageRecipient) {
	if broker == nil {
		return
	}

	taskPayload := sendMessageTaskPayload{Recipients: recipients}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		log.Printf("delete_conversation: failed to marshal MQ payload: %v", err)
		return
	}

	task := &mq.Task{
		Type:    mq.TypeSendMessage,
		Payload: payloadBytes,
	}
	if _, err := broker.Enqueue(context.Background(), task); err != nil {
		log.Printf("delete_conversation: MQ enqueue failed (fire-and-forget): %v", err)
	}
}
