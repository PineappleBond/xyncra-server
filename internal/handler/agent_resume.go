// agent_resume RPC handler — RemoteCalling unified model (D-137 / D-138).
//
// Flow:
//  1. Parse & validate params (id, agent_id required).
//  2. Fetch RemoteCalling by ID to get conversation_id and checkpoint_id.
//  3. Idempotency check: status != pending → return success.
//  4. Expiration check: expires_at passed → mark as expired.
//  5. Resolve the RemoteCalling (success or error).
//  6. Check if all RemoteCallings for the checkpoint are resolved (D-138).
//     - Still pending → return {status:"partial", pending_count}.
//     - All resolved → enqueue TypeAgentResume MQ task.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// agentResumeParams is the JSON-decoded representation of the client-supplied
// parameters for the "agent_resume" RPC method (D-137).
type agentResumeParams struct {
	ID           string `json:"id"`            // RemoteCalling ID (required)
	Success      bool   `json:"success"`       // whether the call succeeded
	Result       string `json:"result"`        // result on success
	ErrorMessage string `json:"error_message"` // error on failure
	AgentID      string `json:"agent_id"`      // required for enqueue
}

// agentResumeHandler handles the "agent_resume" RPC method (D-137 / D-138).
type agentResumeHandler struct {
	store  store.StoreAPI
	broker mq.Broker
	logger server.Logger
}

// NewAgentResumeHandler creates a handler for the "agent_resume" RPC method.
func NewAgentResumeHandler(s store.StoreAPI, broker mq.Broker, logger server.Logger) *agentResumeHandler {
	if logger == nil {
		logger = defaultLogger{}
	}
	return &agentResumeHandler{store: s, broker: broker, logger: logger}
}

// agentResumeTaskPayload is the MQ task payload for TypeAgentResume.
// The result is already persisted in the RemoteCalling table (D-137).
type agentResumeTaskPayload struct {
	ConversationID string `json:"conversation_id"`
	CheckpointID   string `json:"checkpoint_id"`
	AgentID        string `json:"agent_id"`
	SenderID       string `json:"sender_id"`               // human user who triggered the resume
	DeviceID       string `json:"device_id,omitempty"`      // device that initiated the resume (D-102)
	CancelledBy    string `json:"cancelled_by,omitempty"`   // non-empty when resuming after cancellation
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// HandleRequest implements MethodHandler. It validates params, resolves the
// RemoteCalling, and — only when all RemoteCallings for the checkpoint are
// resolved — enqueues a TypeAgentResume MQ task (D-138).
func (h *agentResumeHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params agentResumeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Validate required fields.
	// NOTE: agent_id is required for protocol consistency and future extensibility,
	// but the actual value used in the MQ payload comes from the database record
	// (rc.AgentID), not from the client request. This prevents a malicious client
	// from specifying an arbitrary agent_id (see BUG-FIX comment in step 10).
	if params.ID == "" || params.AgentID == "" {
		return nil, protocol.NewValidationError("id and agent_id are required")
	}

	// BUG-FIX: Validate Result/ErrorMessage length to prevent oversized payloads.
	// Result max 1MB, ErrorMessage max 10KB.
	const maxResultSize = 1 * 1024 * 1024       // 1MB
	const maxErrorMessageSize = 10 * 1024       // 10KB
	if len(params.Result) > maxResultSize {
		return nil, protocol.NewValidationError("result exceeds maximum size (1MB)")
	}
	if len(params.ErrorMessage) > maxErrorMessageSize {
		return nil, protocol.NewValidationError("error_message exceeds maximum size (10KB)")
	}

	// 3. Fetch RemoteCalling by ID to get conversation_id and checkpoint_id.
	rcs := h.store.RemoteCallingStore()
	if rcs == nil {
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: RemoteCallingStore not available"))
	}

	rc, err := rcs.GetByID(ctx, params.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("remote calling not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: get remote calling: %w", err))
	}

	// 4. Idempotency check: if already resolved/cancelled/expired, return success.
	if rc.Status != model.RemoteCallingStatusPending {
		return json.Marshal(map[string]interface{}{
			"status":  rc.Status,
			"message": "already processed",
		})
	}

	// 5. Expiration check.
	if rc.ExpiresAt != nil && time.Now().After(*rc.ExpiresAt) {
		if err := rcs.MarkExpired(ctx, rc.ID); err != nil {
			return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: mark expired: %w", err))
		}
		return json.Marshal(map[string]interface{}{
			"status":  "expired",
			"message": "remote calling has expired",
		})
	}

	// 6. Resolve the RemoteCalling.
	if params.Success {
		if err := rcs.ResolveResult(ctx, rc.ID, params.Result); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return json.Marshal(map[string]interface{}{
					"status":  "resolved",
					"message": "already resolved",
				})
			}
			return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: resolve result: %w", err))
		}
	} else {
		if err := rcs.ResolveError(ctx, rc.ID, params.ErrorMessage); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return json.Marshal(map[string]interface{}{
					"status":  "resolved",
					"message": "already resolved",
				})
			}
			return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: resolve error: %w", err))
		}
	}

	// 7. Expire any overdue sibling RemoteCallings for this checkpoint before
	// counting pending. This handles the case where some RemoteCallings have
	// passed their expires_at but haven't been cleaned up by the periodic task
	// yet (which runs every 5 minutes). Without this, the conversation would be
	// stuck in tool_calling status until the next cleanup tick.
	if _, err := rcs.MarkExpiredByCheckpoint(ctx, rc.CheckpointID, time.Now()); err != nil {
		// Non-fatal: log and continue with the pending count.
		// The periodic cleanup task will eventually expire them.
		h.logger.Error("agent_resume: mark expired by checkpoint failed (non-fatal)",
			"checkpoint_id", rc.CheckpointID, "error", err)
	}

	// 8. Check if all RemoteCallings for this checkpoint are resolved (D-138).
	// Known limitation: resolve (step 6) and count (step 8) are not atomic.
	// Two concurrent requests may both see pending=0 and both enqueue agent resume.
	// The resume handler's idempotency key (D-121) provides a safety net:
	// only the first resume task will execute; subsequent duplicates are skipped.
	pending, err := rcs.CountPendingByCheckpoint(ctx, rc.CheckpointID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: count pending: %w", err))
	}

	// 9. If still pending, return partial status.
	if pending > 0 {
		return json.Marshal(map[string]interface{}{
			"status":        "partial",
			"pending_count": pending,
		})
	}

	// 10. All resolved → enqueue TypeAgentResume MQ task.
	senderID := ""
	deviceID := ""
	if client != nil {
		senderID = client.UserID()
		deviceID = client.DeviceID()
	}
	// BUG-FIX: Use rc.AgentID (from database record) instead of params.AgentID
	// (from client request) to prevent a malicious client from specifying an
	// arbitrary agent_id. The database record is the source of truth.
	payload := agentResumeTaskPayload{
		ConversationID: rc.ConversationID,
		CheckpointID:   rc.CheckpointID,
		AgentID:        rc.AgentID,
		SenderID:       senderID,
		DeviceID:       deviceID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: marshal payload: %w", err))
	}
	task := &mq.Task{
		Type:    mq.TypeAgentResume,
		Payload: raw,
		Queue:   mq.QueueDefault,
	}
	if _, err := h.broker.Enqueue(ctx, task); err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: enqueue task: %w", err))
	}

	return json.Marshal(map[string]interface{}{
		"status": "queued",
	})
}

