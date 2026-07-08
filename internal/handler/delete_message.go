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
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// deleteMessageParams is the JSON-decoded representation of the client-supplied
// parameters for the "delete_message" method.
type deleteMessageParams struct {
	MessageID string `json:"message_id"` // required, UUID primary key of the message
}

// deleteMessageResponse is the success response payload returned to the client.
type deleteMessageResponse struct {
	Status string `json:"status"` // "ok"
}

// deleteMessageUpdatePayload is the payload stored inside the delete_message
// UserUpdate. It carries the message ID, conversation ID, and message sequence
// number so that clients can remove the message from local state.
type deleteMessageUpdatePayload struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	MessageIDSeq   uint32 `json:"message_id_seq"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// deleteMessageHandler implements MethodHandler for the "delete_message" method.
// It performs a soft delete on a single message, enforcing that only the original
// sender may delete it (D-014).
//
// The handler is stateless (only holds immutable dependency references) and
// therefore safe for concurrent use.
type deleteMessageHandler struct {
	store  store.StoreAPI
	broker mq.Broker
}

// NewDeleteMessageHandler creates a deleteMessageHandler backed by the given Store
// and Broker. The broker is used to enqueue a fire-and-forget MQ task that
// pushes the delete_message update to all conversation members' online devices.
func NewDeleteMessageHandler(store store.StoreAPI, broker mq.Broker) *deleteMessageHandler {
	return &deleteMessageHandler{store: store, broker: broker}
}

// HandleRequest implements MethodHandler. It processes a "delete_message" RPC
// call: parses parameters, validates the message_id, fetches the message and
// its conversation, verifies membership, checks the sender owns the message
// (D-014), then performs a soft delete.
func (h *deleteMessageHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params deleteMessageParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Validate required fields.
	if params.MessageID == "" {
		return nil, protocol.NewValidationError("missing required field: message_id")
	}

	msgID := params.MessageID

	// 3. Fetch message and verify existence.
	msg, err := h.store.MessageStore().Get(ctx, msgID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("message not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("get message: %w", err))
	}

	// 4. Fetch conversation and verify existence.
	conv, err := h.store.ConversationStore().Get(ctx, msg.ConversationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("get conversation: %w", err))
	}

	// 5. Verify membership (C-3).
	userID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, userID) {
		return nil, protocol.NewPermissionDeniedError("user is not a member of the conversation")
	}

	// 6. Verify caller is the message sender (D-014).
	if msg.SenderID != userID {
		return nil, protocol.NewPermissionDeniedError("only the sender can delete this message")
	}

	// 7. Soft-delete the message.
	if err := h.store.MessageStore().Delete(ctx, msgID); err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("delete message: %w", err))
	}

	// 8. Create UserUpdates for ALL conversation members (D-014: message
	// deletion affects all members' devices) and broadcast via MQ.
	updatePayload, _ := json.Marshal(deleteMessageUpdatePayload{
		MessageID:      msgID,
		ConversationID: msg.ConversationID,
		MessageIDSeq:   msg.MessageID,
	})

	now := time.Now()
	updates := make([]model.UserUpdate, 0, len(members))
	recipients := make([]sendMessageRecipient, 0, len(members))

	for _, memberID := range members {
		latestSeq, err := h.store.UserUpdateStore().GetLatestSeq(ctx, memberID)
		if err != nil {
			log.Printf("delete_message: failed to get latest seq for user %s (skipping UserUpdate): %v", memberID, err)
			continue
		}
		newSeq := latestSeq + 1

		updates = append(updates, model.UserUpdate{
			ID:        uuid.New().String(),
			UserID:    memberID,
			Seq:       newSeq,
			Type:      protocol.UpdateTypeDeleteMessage,
			Payload:   updatePayload,
			CreatedAt: now,
		})

		recipients = append(recipients, sendMessageRecipient{
			UserID: memberID,
			Updates: []protocol.PackageDataUpdate{
				{
					Seq:       newSeq,
					Type:      protocol.UpdateTypeDeleteMessage,
					Payload:   updatePayload,
					CreatedAt: now,
				},
			},
		})
	}

	if err := h.store.UserUpdateStore().Create(ctx, updates); err != nil {
		// UserUpdate creation failure does not affect the main flow (D-007
		// fire-and-forget spirit). The message was already soft-deleted.
		log.Printf("delete_message: failed to create UserUpdates: %v", err)
	}

	// 9. MQ broadcast to all members' online devices (fire-and-forget, D-007).
	broadcastDeleteMessageUpdates(h.broker, recipients)

	// 10. Return response.
	return marshalResponse(deleteMessageResponse{Status: "ok"})
}

// broadcastDeleteMessageUpdates enqueues a fire-and-forget MQ task to push the
// delete_message update to all conversation members' online devices. It reuses
// the sendMessageRecipient / sendMessageTaskPayload structures and the
// TypeSendMessage task type because they are sufficiently generic.
//
// Errors are logged but never returned (D-007: MQ failures do not affect data
// integrity — the update was already persisted and will be delivered via
// sync_updates on the next pull).
func broadcastDeleteMessageUpdates(broker mq.Broker, recipients []sendMessageRecipient) {
	if broker == nil {
		return
	}

	taskPayload := sendMessageTaskPayload{Recipients: recipients}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		log.Printf("delete_message: failed to marshal MQ payload: %v", err)
		return
	}

	task := &mq.Task{
		Type:    mq.TypeSendMessage,
		Payload: payloadBytes,
	}
	if _, err := broker.Enqueue(context.Background(), task); err != nil {
		log.Printf("delete_message: MQ enqueue failed (fire-and-forget): %v", err)
	}
}
