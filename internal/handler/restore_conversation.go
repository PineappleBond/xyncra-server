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

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// restoreConversationHandler implements MethodHandler for the "restore_conversation" method.
// It performs a cascade restore on the conversation and all its messages (D-015).
//
// The handler is stateless (only holds an immutable dependency reference) and
// therefore safe for concurrent use.
type restoreConversationHandler struct {
	store store.StoreAPI
}

// NewRestoreConversationHandler creates a restoreConversationHandler backed by the given Store.
func NewRestoreConversationHandler(store store.StoreAPI) *restoreConversationHandler {
	return &restoreConversationHandler{store: store}
}

// HandleRequest implements MethodHandler. It processes a "restore_conversation" RPC
// call: parses parameters, validates the conversation_id, fetches the conversation
// (including soft-deleted), verifies membership, then performs a cascade restore of
// the conversation and all its messages in a single transaction (D-015).
func (h *restoreConversationHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params restoreConversationParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// 2. Validate required fields.
	if params.ConversationID == "" {
		return nil, fmt.Errorf("missing required field: conversation_id")
	}

	convID := params.ConversationID

	// 3. Fetch conversation (including soft-deleted) and verify existence.
	conv, err := h.store.ConversationStore().GetUnscoped(ctx, convID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("conversation not found")
		}
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}

	// 4. Verify membership (C-3).
	userID := client.UserID()
	members := conversationMembers(conv)
	if !containsUser(members, userID) {
		return nil, fmt.Errorf("user is not a member of the conversation")
	}

	// 5. Idempotent: if conversation is NOT soft-deleted, return current state (D-015).
	if !conv.DeletedAt.Valid || conv.DeletedAt.Time.IsZero() {
		// Already active — return as-is with zero restored count.
		restoredConv, getErr := h.store.ConversationStore().Get(ctx, convID)
		if getErr != nil {
			return nil, fmt.Errorf("failed to get conversation: %w", getErr)
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

		// 6b. Restore all messages in the conversation.
		msgResult := tx.Unscoped().Model(&model.Message{}).
			Where("conversation_id = ? AND deleted_at IS NOT NULL", convID).
			Update("deleted_at", nil)
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
			return nil, fmt.Errorf("conversation not found")
		}
		return nil, fmt.Errorf("failed to restore conversation: %w", err)
	}

	// 7. Fetch the restored conversation and return.
	restoredConv, err := h.store.ConversationStore().Get(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("failed to get restored conversation: %w", err)
	}

	return marshalResponse(restoreConversationResponse{
		Conversation:         restoredConv,
		RestoredMessageCount: restoredMessageCount,
	})
}
