package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
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
	store store.StoreAPI
}

// NewCreateConversationHandler creates a createConversationHandler backed by
// the given Store.
func NewCreateConversationHandler(store store.StoreAPI) *createConversationHandler {
	return &createConversationHandler{store: store}
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
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// 2. Validate required fields.
	if params.UserID == "" {
		return nil, fmt.Errorf("missing required field: user_id")
	}

	// 3. Validate not creating a conversation with oneself.
	callerID := client.UserID()
	if params.UserID == callerID {
		return nil, fmt.Errorf("cannot create conversation with yourself")
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
		return nil, fmt.Errorf("check existing conversation: %w", err)
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
		return nil, fmt.Errorf("create conversation: %w", err)
	}

	// 6. Return success.
	resp := createConversationResponse{
		Conversation: conv,
		Duplicate:    false,
	}
	return marshalResponse(resp)
}
