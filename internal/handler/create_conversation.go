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

// createConversationParams is the JSON-decoded representation of the
// client-supplied parameters for the "create_conversation" method.
type createConversationParams struct {
	// UserID is the ID of the other user to create a 1-on-1 conversation
	// with. It is required.
	UserID string `json:"user_id"`

	// Title is an optional human-readable title for the conversation.
	Title string `json:"title"`
}

// createConversationResponse is the success response payload returned to the
// client after a create_conversation call.
type createConversationResponse struct {
	Conversation *model.Conversation `json:"conversation"`
	Duplicate    bool                `json:"duplicate"`
}

// createConversationUpdatePayload is the JSON structure stored inside the
// create_conversation UserUpdate. It wraps the full conversation model with an
// "action" field so that clients can dispatch it through the same handler used
// for delete and restore events (consistent with conversationUpdatePayload in
// pkg/client/sync.go).
type createConversationUpdatePayload struct {
	Action       string              `json:"action"` // "create"
	Conversation *model.Conversation `json:"conversation"`
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// createConversationHandler implements MethodHandler for the
// "create_conversation" method. It uses a find-or-create idempotency model
// (D-011): if a 1-on-1 conversation between the caller and the requested user
// already exists, the existing conversation is returned with Duplicate=true;
// otherwise a new conversation is created.
//
// The handler is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
type createConversationHandler struct {
	store  store.StoreAPI
	broker mq.Broker
}

// NewCreateConversationHandler creates a createConversationHandler backed by
// the given Store and Broker. The broker is used to enqueue a fire-and-forget
// MQ task that pushes the create_conversation update to both conversation
// members' online devices (D-045).
func NewCreateConversationHandler(store store.StoreAPI, broker mq.Broker) *createConversationHandler {
	return &createConversationHandler{store: store, broker: broker}
}

// HandleRequest implements MethodHandler. It processes a "create_conversation"
// RPC call: validates parameters, performs a find-or-create idempotency check
// via GetByUsers, creates a new conversation if none exists, and returns the
// resulting conversation with a Duplicate flag.
//
// Errors:
//   - Missing or empty user_id: "missing required field: user_id"
//   - user_id == caller's user ID: "cannot create conversation with yourself"
//   - Store errors are wrapped and returned to the caller.
func (h *createConversationHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params createConversationParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Validate required fields.
	if params.UserID == "" {
		return nil, protocol.NewValidationError("missing required field: user_id")
	}

	// 3. Validate not creating a conversation with oneself.
	callerID := client.UserID()
	if params.UserID == callerID {
		return nil, protocol.NewValidationError("cannot create conversation with yourself")
	}

	// 4. Find-or-create idempotency check (D-011).
	existing, err := h.store.ConversationStore().GetByUsers(ctx, callerID, params.UserID)
	if err == nil {
		// Conversation already exists — return it with Duplicate=true.
		resp := createConversationResponse{
			Conversation: existing,
			Duplicate:    true,
		}
		return marshalResponse(resp)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, protocol.NewInternalError(fmt.Errorf("check existing conversation: %w", err))
	}

	// 5. Create a new conversation.
	// Normalize user ordering so that the unique (user_id1, user_id2) index
	// prevents duplicates regardless of which user initiates creation.
	user1, user2 := callerID, params.UserID
	if user2 < user1 {
		user1, user2 = user2, user1
	}

	now := time.Now()
	conv := &model.Conversation{
		ID:            uuid.New().String(),
		UserID1:       user1,
		UserID2:       user2,
		Type:          "1-on-1",
		Title:         params.Title,
		CreatedAt:     now,
		UpdatedAt:     now,
		LastMessageAt: now,
	}

	if err := h.store.ConversationStore().Create(ctx, conv); err != nil {
		// If a concurrent request created the same conversation between the
		// same user pair, the unique index fires. Treat this as an idempotent
		// duplicate rather than a hard error (TOCTOU protection for D-011).
		if errors.Is(err, store.ErrDuplicateKey) {
			existing, lookupErr := h.store.ConversationStore().GetByUsers(ctx, callerID, params.UserID)
			if lookupErr == nil {
				resp := createConversationResponse{
					Conversation: existing,
					Duplicate:    true,
				}
				return marshalResponse(resp)
			}
		}
		return nil, protocol.NewInternalError(fmt.Errorf("create conversation: %w", err))
	}

	// 6. Create UserUpdates for both conversation members and broadcast via MQ
	// (D-045: real-time notification on conversation creation). The payload
	// carries the full conversation model wrapped with an "action" field so
	// that clients can dispatch it through the same handler used for delete
	// and restore events.
	//
	// Seq allocation and UserUpdate creation are wrapped in a single database
	// transaction to prevent a TOCTOU race when concurrent operations target
	// the same user (mirrors the pattern used by Store.SendMessage).
	members := []string{conv.UserID1, conv.UserID2}
	convPayload, _ := json.Marshal(createConversationUpdatePayload{
		Action:       "create",
		Conversation: conv,
	})

	now2 := time.Now()
	var updates []model.UserUpdate
	var recipients []sendMessageRecipient

	if err := h.store.Transaction(ctx, func(tx *gorm.DB) error {
		updates = make([]model.UserUpdate, 0, len(members))
		recipients = make([]sendMessageRecipient, 0, len(members))

		for _, memberID := range members {
			var latestSeq uint32
			if err := tx.Model(&model.UserUpdate{}).
				Where("user_id = ?", memberID).
				Select("COALESCE(MAX(seq), 0)").
				Scan(&latestSeq).Error; err != nil {
				return fmt.Errorf("create_conversation: get latest seq for user %s: %w", memberID, err)
			}
			newSeq := latestSeq + 1

			updates = append(updates, model.UserUpdate{
				ID:        uuid.New().String(),
				UserID:    memberID,
				Seq:       newSeq,
				Type:      protocol.UpdateTypeConversation,
				Payload:   convPayload,
				CreatedAt: now2,
			})

			recipients = append(recipients, sendMessageRecipient{
				UserID: memberID,
				Updates: []protocol.PackageDataUpdate{
					{
						Seq:       newSeq,
						Type:      protocol.UpdateTypeConversation,
						Payload:   convPayload,
						CreatedAt: now2,
					},
				},
			})
		}

		if len(updates) > 0 {
			if err := tx.CreateInBatches(updates, 100).Error; err != nil {
				return fmt.Errorf("create_conversation: insert user updates: %w", err)
			}
		}
		return nil
	}); err != nil {
		// UserUpdate creation failure does not affect the main flow (D-007
		// fire-and-forget spirit). The conversation was already created.
		log.Printf("create_conversation: failed to create UserUpdates in transaction: %v", err)
	} else {
		// MQ broadcast to both members' online devices (fire-and-forget, D-007).
		broadcastCreateConversationUpdates(ctx, h.broker, recipients)
	}

	// 7. Return success.
	resp := createConversationResponse{
		Conversation: conv,
		Duplicate:    false,
	}
	return marshalResponse(resp)
}

// broadcastCreateConversationUpdates enqueues a fire-and-forget MQ task to push
// the create_conversation update to both conversation members' online devices.
// It reuses the sendMessageRecipient / sendMessageTaskPayload structures and the
// TypeSendMessage task type because they are sufficiently generic.
//
// Errors are logged but never returned (D-007: MQ failures do not affect data
// integrity — the update was already persisted and will be delivered via
// sync_updates on the next pull).
func broadcastCreateConversationUpdates(ctx context.Context, broker mq.Broker, recipients []sendMessageRecipient) {
	if broker == nil {
		return
	}

	taskPayload := sendMessageTaskPayload{Recipients: recipients}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		log.Printf("create_conversation: failed to marshal MQ payload: %v", err)
		return
	}

	task := &mq.Task{
		Type:    mq.TypeSendMessage,
		Payload: payloadBytes,
	}
	if _, err := broker.Enqueue(ctx, task); err != nil {
		log.Printf("create_conversation: MQ enqueue failed (fire-and-forget): %v", err)
	}
}
