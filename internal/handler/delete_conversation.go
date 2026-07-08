package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// deleteConversationHandler implements MethodHandler for the "delete_conversation" method.
// It performs a cascade soft delete on the conversation and all its messages (D-013).
//
// The handler is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
type deleteConversationHandler struct {
	store store.StoreAPI
}

// NewDeleteConversationHandler creates a deleteConversationHandler backed by the given Store.
func NewDeleteConversationHandler(store store.StoreAPI) *deleteConversationHandler {
	return &deleteConversationHandler{store: store}
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
	err = h.store.Transaction(ctx, func(tx *gorm.DB) error {
		// 5a. Count non-deleted messages before deleting.
		if err := tx.Model(&model.Message{}).Where("conversation_id = ?", convID).Count(&deletedMessageCount).Error; err != nil {
			return fmt.Errorf("count messages: %w", err)
		}

		// 5b. Soft-delete the conversation.
		result := tx.Delete(&model.Conversation{}, "id = ?", convID)
		if result.Error != nil {
			return fmt.Errorf("delete conversation: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return store.ErrNotFound
		}

		// 5c. Soft-delete all messages in the conversation.
		if err := tx.Where("conversation_id = ?", convID).Delete(&model.Message{}).Error; err != nil {
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

	// 6. Return response.
	return marshalResponse(deleteConversationResponse{
		Status:              "ok",
		DeletedMessageCount: deletedMessageCount,
	})
}
