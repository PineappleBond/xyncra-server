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
// message atomically (with MessageID and seq allocation inside the
// transaction), enqueues an async MQ delivery task (fire-and-forget), and
// returns the resulting message to the caller.
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

	// 3. Fetch conversation and verify membership. This is a preliminary
	// check for a clear error message; the store's SendMessage transaction
	// also reads the conversation atomically (D-008).
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

	// 4. Build model.Message. MessageID is left at zero; the store allocates
	// it atomically inside the transaction (D-008).
	now := time.Now()
	msg := &model.Message{
		ID:              uuid.New().String(),
		ClientMessageID: params.ClientMessageID,
		ConversationID:  conv.ID,
		SenderID:        senderID,
		Content:         params.Content,
		Type:            params.Type,
		ReplyTo:         params.ReplyTo,
		Status:          "sent",
		CreatedAt:       now,
	}

	// 5. Atomic persist: the store allocates MessageID and per-user seq
	// values inside the transaction, then inserts the message, user updates,
	// and updates the conversation metadata.
	sendResult, err := h.store.SendMessage(ctx, msg, members)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("send message: %w", err))
	}

	// 6. Build MQ task from the allocated result and enqueue asynchronously
	//    (fire-and-forget, D-007).
	recipients := make([]sendMessageRecipient, 0, len(sendResult.Updates))
	for _, u := range sendResult.Updates {
		recipients = append(recipients, sendMessageRecipient{
			UserID: u.UserID,
			Updates: []protocol.PackageDataUpdate{
				{
					Seq:       u.Seq,
					Type:      protocol.UpdateTypeMessage,
					Payload:   u.Payload,
					CreatedAt: u.CreatedAt,
				},
			},
		})
	}

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

	// 7. Return success.
	resp := sendMessageResponse{
		Message:   sendResult.Message,
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
