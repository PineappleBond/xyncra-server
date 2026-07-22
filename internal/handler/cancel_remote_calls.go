// cancel_remote_calls RPC handler — cancel all pending RemoteCallings for a checkpoint (D-137).
//
// Flow:
//  1. Parse & validate params (checkpoint_id, reason required).
//  2. Batch cancel all pending RemoteCallings for the checkpoint.
//  3. If no pending remain, enqueue TypeAgentResume MQ task (cancelled_by set).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// cancelRemoteCallsParams is the JSON-decoded representation of the client-supplied
// parameters for the "cancel_remote_calls" RPC method.
type cancelRemoteCallsParams struct {
	CheckpointID string `json:"checkpoint_id"` // required
	Reason       string `json:"reason"`        // required
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// cancelRemoteCallsHandler handles the "cancel_remote_calls" RPC method (D-137).
type cancelRemoteCallsHandler struct {
	store  store.StoreAPI
	broker mq.Broker
}

// NewCancelRemoteCallsHandler creates a handler for the "cancel_remote_calls" RPC method.
func NewCancelRemoteCallsHandler(s store.StoreAPI, broker mq.Broker) *cancelRemoteCallsHandler {
	return &cancelRemoteCallsHandler{store: s, broker: broker}
}

// HandleRequest implements MethodHandler. It batch-cancels all pending RemoteCallings
// for the given checkpoint. If no pending remain, enqueues a TypeAgentResume MQ task
// with CancelledBy set to the caller's user ID.
func (h *cancelRemoteCallsHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params cancelRemoteCallsParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Validate required fields.
	if params.CheckpointID == "" || params.Reason == "" {
		return nil, protocol.NewValidationError("checkpoint_id and reason are required")
	}

	// 2b. Validate Reason length to prevent oversized payloads (review suggestion).
	const maxReasonLength = 1024
	if len(params.Reason) > maxReasonLength {
		return nil, protocol.NewValidationError("reason exceeds maximum length (1024 bytes)")
	}

	// 3. Fetch the checkpoint's conversation and verify the caller is a member.
	rcs := h.store.RemoteCallingStore()
	if rcs == nil {
		return nil, protocol.NewInternalError(fmt.Errorf("cancel_remote_calls: RemoteCallingStore not available"))
	}

	// Use GetByCheckpoint to find the conversation for permission check.
	existingRCs, err := rcs.GetByCheckpoint(ctx, params.CheckpointID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("cancel_remote_calls: get by checkpoint: %w", err))
	}
	if len(existingRCs) == 0 {
		return nil, protocol.NewNotFoundError("no remote callings found for checkpoint")
	}

	// Verify caller is a member of the conversation.
	convID := existingRCs[0].ConversationID
	conv, err := h.store.ConversationStore().Get(ctx, convID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("cancel_remote_calls: get conversation: %w", err))
	}
	members := conversationMembers(conv)
	if !containsUserOrAgentBase(members, client.UserID()) {
		return nil, protocol.NewPermissionDeniedError("user is not a member of the conversation")
	}

	// Get the caller's user ID for cancelled_by field.
	cancelledBy := client.UserID()

	cancelledCount, rcConvID, rcAgentID, err := rcs.CancelByCheckpoint(ctx, params.CheckpointID, params.Reason, cancelledBy)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("cancel_remote_calls: cancel: %w", err))
	}

	// 4. Check if all RemoteCallings for this checkpoint are resolved (D-138).
	// Known limitation: cancel (step 3) and count (step 4) are not atomic.
	// Two concurrent requests may both see pending=0 and both enqueue agent resume.
	// The resume handler's idempotency key (D-121) provides a safety net:
	// only the first resume task will execute; subsequent duplicates are skipped.
	pending, err := rcs.CountPendingByCheckpoint(ctx, params.CheckpointID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("cancel_remote_calls: count pending: %w", err))
	}

	// 5. If no pending remain, enqueue TypeAgentResume MQ task.
	if pending == 0 && rcConvID != "" {
		deviceID := ""
		if client != nil {
			deviceID = client.DeviceID()
		}
		payload := agentResumeTaskPayload{
			ConversationID: rcConvID,
			CheckpointID:   params.CheckpointID,
			AgentID:        rcAgentID,
			SenderID:       cancelledBy, // same user who cancelled
			DeviceID:       deviceID,
			CancelledBy:    cancelledBy,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, protocol.NewInternalError(fmt.Errorf("cancel_remote_calls: marshal payload: %w", err))
		}
		task := &mq.Task{
			Type:    mq.TypeAgentResume,
			Payload: raw,
			Queue:   mq.QueueDefault,
		}
		if _, err := h.broker.Enqueue(ctx, task); err != nil {
			return nil, protocol.NewInternalError(fmt.Errorf("cancel_remote_calls: enqueue task: %w", err))
		}
	}

	// 6. Return response.
	return json.Marshal(map[string]interface{}{
		"cancelled_count": cancelledCount,
	})
}
