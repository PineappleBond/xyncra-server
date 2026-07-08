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

// sendMessageParams is the JSON-decoded representation of the client-supplied
// parameters for the "send_message" method.
type sendMessageParams struct {
	ConversationID  string `json:"conversation_id"`
	ClientMessageID string `json:"client_message_id"`
	Content         string `json:"content"`
	Type            string `json:"type"`
	ReplyTo         uint32 `json:"reply_to"`
}

// sendMessageResponse is the success response payload returned to the client.
type sendMessageResponse struct {
	Message   *model.Message `json:"message"`
	Duplicate bool           `json:"duplicate"`
}

// sendMessageTaskPayload is the MQ task payload used to fan out the message to
// each conversation member for real-time delivery.
type sendMessageTaskPayload struct {
	Recipients []sendMessageRecipient `json:"recipients"`
}

// sendMessageRecipient describes the push data for a single conversation
// member inside the MQ task payload.
type sendMessageRecipient struct {
	UserID  string                       `json:"user_id"`
	Updates []protocol.PackageDataUpdate `json:"updates"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// sendMessageHandler implements MethodHandler for the "send_message" method.
// It is stateless (only holds immutable dependency references) and therefore
// safe for concurrent use.
type sendMessageHandler struct {
	store  store.StoreAPI
	broker mq.Broker
}

// NewSendMessageHandler creates a sendMessageHandler.
func NewSendMessageHandler(store store.StoreAPI, broker mq.Broker) *sendMessageHandler {
	return &sendMessageHandler{
		store:  store,
		broker: broker,
	}
}

// HandleRequest implements MethodHandler. It processes a "send_message" RPC
// call: validates parameters, performs an idempotency check, persists the
// message atomically, enqueues an async MQ delivery task (fire-and-forget),
// and returns the resulting message to the caller.
func (h *sendMessageHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params sendMessageParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// Validate required fields.
	if params.ConversationID == "" {
		return nil, protocol.NewValidationError("missing required field: conversation_id")
	}
	if params.ClientMessageID == "" {
		return nil, protocol.NewValidationError("missing required field: client_message_id")
	}
	if params.Content == "" {
		return nil, protocol.NewValidationError("missing required field: content")
	}

	// Apply default message type.
	if params.Type == "" {
		params.Type = "text"
	}

	// 2. Idempotency check (D-006).
	if existing, err := h.store.MessageStore().GetByClientMessageID(ctx, params.ClientMessageID); err == nil {
		// Duplicate — return the already-persisted message.
		resp := sendMessageResponse{
			Message:   existing,
			Duplicate: true,
		}
		return marshalResponse(resp)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, protocol.NewInternalError(fmt.Errorf("check idempotency: %w", err))
	}

	// 3. Fetch conversation and verify membership.
	conv, err := h.store.ConversationStore().Get(ctx, params.ConversationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("get conversation: %w", err))
	}

	senderID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, senderID) {
		return nil, protocol.NewPermissionDeniedError("user is not a member of the conversation")
	}

	// 4. Allocate MessageID (D-008).
	messageID := conv.LastProcessedMessageID + 1

	// 5. Build model.Message.
	now := time.Now()
	msg := &model.Message{
		ID:              uuid.New().String(),
		ClientMessageID: params.ClientMessageID,
		ConversationID:  conv.ID,
		MessageID:       messageID,
		SenderID:        senderID,
		Content:         params.Content,
		Type:            params.Type,
		ReplyTo:         params.ReplyTo,
		Status:          "sent",
		CreatedAt:       now,
	}

	// 6. Build per-member UserUpdate records.
	msgPayload, err := json.Marshal(msg)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("marshal message: %w", err))
	}

	updates := make([]model.UserUpdate, 0, len(members))
	recipients := make([]sendMessageRecipient, 0, len(members))

	for _, memberID := range members {
		latestSeq, err := h.store.UserUpdateStore().GetLatestSeq(ctx, memberID)
		if err != nil {
			return nil, protocol.NewInternalError(fmt.Errorf("get latest seq for user %s: %w", memberID, err))
		}
		newSeq := latestSeq + 1

		update := model.UserUpdate{
			ID:        uuid.New().String(),
			UserID:    memberID,
			Seq:       newSeq,
			Type:      protocol.UpdateTypeMessage,
			Payload:   msgPayload,
			CreatedAt: now,
		}
		updates = append(updates, update)

		recipients = append(recipients, sendMessageRecipient{
			UserID: memberID,
			Updates: []protocol.PackageDataUpdate{
				{
					Seq:       newSeq,
					Type:      protocol.UpdateTypeMessage,
					Payload:   msgPayload,
					CreatedAt: now,
				},
			},
		})
	}

	// 7. Atomic persist (message + updates + conversation metadata).
	if err := h.store.SendMessage(ctx, msg, updates, conv.ID, msg.CreatedAt, messageID); err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("send message: %w", err))
	}

	// 8-9. Build MQ task and enqueue asynchronously (fire-and-forget, D-007).
	taskPayload := sendMessageTaskPayload{Recipients: recipients}
	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		log.Printf("send_message: failed to marshal MQ payload: %v", err)
	} else {
		task := &mq.Task{
			Type:    mq.TypeSendMessage,
			Payload: payloadBytes,
		}
		if _, err := h.broker.Enqueue(ctx, task); err != nil {
			log.Printf("send_message: MQ enqueue failed (fire-and-forget): %v", err)
		}
	}

	// 10. Return success.
	resp := sendMessageResponse{
		Message:   msg,
		Duplicate: false,
	}
	return marshalResponse(resp)
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// conversationMembers returns the user IDs of a conversation's members. For a
// 1-on-1 conversation both UserID1 and UserID2 are returned; if UserID2 is
// empty (should not happen for 1-on-1 but handled defensively) only UserID1 is
// returned.
func conversationMembers(conv *model.Conversation) []string {
	members := []string{conv.UserID1}
	if conv.UserID2 != "" {
		members = append(members, conv.UserID2)
	}
	return members
}

// containsUser reports whether userID is present in the slice.
func containsUser(members []string, userID string) bool {
	for _, m := range members {
		if m == userID {
			return true
		}
	}
	return false
}
