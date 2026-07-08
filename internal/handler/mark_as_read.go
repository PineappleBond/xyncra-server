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

// markAsReadParams is the JSON-decoded representation of the client-supplied
// parameters for the "mark_as_read" method.
type markAsReadParams struct {
	ConversationID string `json:"conversation_id"` // required
	MessageID      uint32 `json:"message_id"`      // optional, defaults to LastProcessedMessageID (mark all as read)
}

// markAsReadResponse is the success response payload returned to the client
// after a mark_as_read call.
type markAsReadResponse struct {
	Status            string `json:"status"`
	UnreadCount       int64  `json:"unread_count"`
	LastReadMessageID uint32 `json:"last_read_message_id"`
}

// markReadUpdatePayload is the payload stored inside the mark_read UserUpdate.
// It carries the conversation ID and the new read cursor so that other devices
// of the same user can synchronise their local state.
type markReadUpdatePayload struct {
	ConversationID    string `json:"conversation_id"`
	LastReadMessageID uint32 `json:"last_read_message_id"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// markAsReadHandler implements MethodHandler for the "mark_as_read" method.
// It updates the caller's read cursor position in a conversation using MAX
// semantics (D-012): the read cursor can only advance forward, never go back.
//
// The handler is stateless (only holds immutable dependency references) and
// therefore safe for concurrent use.
type markAsReadHandler struct {
	store  store.StoreAPI
	broker mq.Broker
}

// NewMarkAsReadHandler creates a markAsReadHandler backed by the given Store
// and Broker. The broker is used to enqueue a fire-and-forget MQ task that
// pushes the mark_read update to the caller's other online devices.
func NewMarkAsReadHandler(store store.StoreAPI, broker mq.Broker) *markAsReadHandler {
	return &markAsReadHandler{store: store, broker: broker}
}

// HandleRequest implements MethodHandler. It processes a "mark_as_read" RPC
// call: validates parameters, verifies the caller is a member of the
// conversation, updates the read cursor with MAX semantics (D-012), and
// returns the updated unread count.
//
// Errors:
//   - Missing or empty conversation_id: "missing required field: conversation_id"
//   - Conversation not found: "conversation not found"
//   - Caller is not a member: "user is not a member of the conversation"
//   - Store errors are wrapped and returned to the caller.
func (h *markAsReadHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params markAsReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// Validate required fields.
	if params.ConversationID == "" {
		return nil, protocol.NewValidationError("missing required field: conversation_id")
	}

	// 2. Fetch conversation and verify it exists.
	conv, err := h.store.ConversationStore().Get(ctx, params.ConversationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("get conversation: %w", err))
	}

	// 3. Verify membership.
	userID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, userID) {
		return nil, protocol.NewPermissionDeniedError("user is not a member of the conversation")
	}

	// 4. Determine target messageID.
	// If params.MessageID > 0, use it. Otherwise use LastProcessedMessageID
	// (mark ALL as read).
	messageID := params.MessageID
	if messageID == 0 {
		messageID = conv.LastProcessedMessageID
	}

	// 5. Clamp: if messageID > LastProcessedMessageID, set to LastProcessedMessageID.
	if messageID > conv.LastProcessedMessageID {
		messageID = conv.LastProcessedMessageID
	}

	// 6. Update read cursor (MAX semantics enforced by store, D-012).
	if err := h.store.ConversationStore().UpdateLastRead(ctx, params.ConversationID, userID, messageID); err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("update last read: %w", err))
	}

	// 7. Calculate unread count.
	unreadCount, err := h.store.MessageStore().CountUnread(ctx, params.ConversationID, messageID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("count unread: %w", err))
	}

	// 8. Create a UserUpdate for the operating user only (D-012: mark_read is
	// not exposed to the other party). This lets the user's other devices
	// synchronise the read cursor via sync_updates.
	updatePayload, _ := json.Marshal(markReadUpdatePayload{
		ConversationID:    params.ConversationID,
		LastReadMessageID: messageID,
	})

	latestSeq, err := h.store.UserUpdateStore().GetLatestSeq(ctx, userID)
	if err != nil {
		log.Printf("mark_as_read: failed to get latest seq for user %s (skipping UserUpdate): %v", userID, err)
	} else {
		newSeq := latestSeq + 1
		now := time.Now()
		update := model.UserUpdate{
			ID:        uuid.New().String(),
			UserID:    userID,
			Seq:       newSeq,
			Type:      protocol.UpdateTypeMarkRead,
			Payload:   updatePayload,
			CreatedAt: now,
		}
		if err := h.store.UserUpdateStore().Create(ctx, []model.UserUpdate{update}); err != nil {
			// UserUpdate creation failure does not affect the main flow (D-007
			// fire-and-forget spirit). The read cursor was already persisted.
			log.Printf("mark_as_read: failed to create UserUpdate for user %s: %v", userID, err)
		}

		// 9. MQ broadcast to the operating user's other devices (fire-and-forget,
		// D-007). Reuses the sendMessageRecipient / sendMessageTaskPayload types
		// and TypeSendMessage task type since they are sufficiently generic.
		broadcastMarkReadUpdate(h.broker, userID, newSeq, updatePayload, now)
	}

	// 10. Return success.
	resp := markAsReadResponse{
		Status:            "ok",
		UnreadCount:       unreadCount,
		LastReadMessageID: messageID,
	}
	return marshalResponse(resp)
}

// broadcastMarkReadUpdate enqueues a fire-and-forget MQ task to push the
// mark_read update to the user's other online devices. It reuses the
// sendMessageRecipient / sendMessageTaskPayload structures and the
// TypeSendMessage task type because they are sufficiently generic.
//
// Errors are logged but never returned (D-007: MQ failures do not affect data
// integrity — the update was already persisted and will be delivered via
// sync_updates on the next pull).
func broadcastMarkReadUpdate(
	broker mq.Broker,
	userID string,
	seq uint32,
	payload json.RawMessage,
	createdAt time.Time,
) {
	if broker == nil {
		return
	}

	recipient := sendMessageRecipient{
		UserID: userID,
		Updates: []protocol.PackageDataUpdate{
			{
				Seq:       seq,
				Type:      protocol.UpdateTypeMarkRead,
				Payload:   payload,
				CreatedAt: createdAt,
			},
		},
	}
	taskPayload := sendMessageTaskPayload{Recipients: []sendMessageRecipient{recipient}}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		log.Printf("mark_as_read: failed to marshal MQ payload: %v", err)
		return
	}

	task := &mq.Task{
		Type:    mq.TypeSendMessage,
		Payload: payloadBytes,
	}
	if _, err := broker.Enqueue(context.Background(), task); err != nil {
		log.Printf("mark_as_read: MQ enqueue failed (fire-and-forget): %v", err)
	}
}
