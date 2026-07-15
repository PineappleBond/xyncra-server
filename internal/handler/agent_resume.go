// agent_resume RPC handler — HITL Resilience Phase 2 (D-085 / D-116).
//
// Flow:
//  1. Parse & validate params (conversation_id, answer, agent_id required).
//  2. Fetch Conversation to infer checkpoint_id if not supplied.
//  3. Look up pending Questions for that checkpoint.
//  4. Filter by optional interrupt_id, pick the first match.
//  5. Call QuestionStore.UpdateAnswer (idempotent: WHERE status='pending').
//     - ErrConflict → 409 "question_already_answered".
//  6. Count remaining pending questions.
//     - Still pending → return {status:"partial", answered, total, pending}.
//     - All answered  → enqueue TypeAgentResume MQ task (payload has NO answer).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
// parameters for the "agent_resume" RPC method.
type agentResumeParams struct {
	ConversationID string `json:"conversation_id"` // required
	CheckpointID   string `json:"checkpoint_id"`   // optional (inferred from Conversation)
	InterruptID    string `json:"interrupt_id"`    // optional (filter specific interrupt)
	Answer         string `json:"answer"`          // required
	AgentID        string `json:"agent_id"`        // required
}

// agentResumeHandler handles the "agent_resume" RPC method (D-085 / D-116).
type agentResumeHandler struct {
	store  store.StoreAPI
	broker mq.Broker
}

// NewAgentResumeHandler creates a handler for the "agent_resume" RPC method.
func NewAgentResumeHandler(s store.StoreAPI, broker mq.Broker) *agentResumeHandler {
	return &agentResumeHandler{store: s, broker: broker}
}

// agentResumeTaskPayload is the MQ task payload for TypeAgentResume.
// The answer is NOT included — it is already persisted in the Question table (D-116).
type agentResumeTaskPayload struct {
	ConversationID string `json:"conversation_id"`
	CheckpointID   string `json:"checkpoint_id"`
	AgentID        string `json:"agent_id"`
	SenderID       string `json:"sender_id"` // human user who sent the answer
	DeviceID       string `json:"device_id"` // Phase 6 (D-102)
}

// --------------------------------------------------------------------------
// Handler
// --------------------------------------------------------------------------

// HandleRequest implements MethodHandler. It validates params, persists the
// answer to the Question table, and — only when all questions for the
// checkpoint are answered — enqueues a TypeAgentResume MQ task.
func (h *agentResumeHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	// 1. Parse parameters.
	var params agentResumeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewValidationError("invalid params")
	}

	// 2. Validate required fields.
	if params.ConversationID == "" || params.Answer == "" || params.AgentID == "" {
		return nil, protocol.NewValidationError("conversation_id, answer and agent_id are required")
	}

	// 3. Fetch conversation to infer checkpoint_id when not provided.
	checkpointID := params.CheckpointID
	conv, err := h.store.ConversationStore().Get(ctx, params.ConversationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("conversation not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: get conversation: %w", err))
	}
	if checkpointID == "" {
		checkpointID = conv.CheckpointID
	}
	if checkpointID == "" {
		return nil, protocol.NewValidationError("checkpoint_id is required and cannot be inferred from conversation")
	}

	// Client identity.
	senderID := ""
	deviceID := ""
	if client != nil {
		senderID = client.UserID()
		deviceID = client.DeviceID()
	}

	// 4. Look up pending questions for this checkpoint.
	qs := h.store.QuestionStore()
	if qs == nil {
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: QuestionStore not available"))
	}

	questions, err := qs.GetPendingByCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: get pending questions: %w", err))
	}

	// 5. Filter by interrupt_id if provided, then pick the first match.
	var targetID string
	for _, q := range questions {
		if params.InterruptID != "" && q.InterruptID != params.InterruptID {
			continue
		}
		targetID = q.ID
		break
	}
	if targetID == "" {
		// No pending question matched. Check if one was already answered
		// (multi-device conflict / idempotency) → return 409.
		allQ, err := qs.GetByCheckpoint(ctx, checkpointID)
		if err == nil {
			for _, q := range allQ {
				if q.Status == model.QuestionStatusAnswered {
					if params.InterruptID != "" && q.InterruptID == params.InterruptID {
						return nil, &protocol.HandlerError{Code: -409, Message: "question_already_answered"}
					}
				}
			}
		}
		return nil, protocol.NewNotFoundError("no pending question found for the given checkpoint/interrupt")
	}

	// 6. Update the answer (idempotent: WHERE status='pending').
	if err := qs.UpdateAnswer(ctx, targetID, params.Answer, senderID, deviceID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, &protocol.HandlerError{Code: -409, Message: "question_already_answered"}
		}
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.NewNotFoundError("question not found")
		}
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: update answer: %w", err))
	}

	// 7. Check remaining pending count.
	allQuestions, err := qs.GetByCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: get all questions: %w", err))
	}
	total := int64(len(allQuestions))
	pending, err := qs.CountPendingByCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, protocol.NewInternalError(fmt.Errorf("agent_resume: count pending: %w", err))
	}
	answered := total - pending

	// 8. Partial → return status.
	if pending > 0 {
		return json.Marshal(map[string]interface{}{
			"status":   "partial",
			"answered": answered,
			"total":    total,
			"pending":  pending,
		})
	}

	// 9. All answered → enqueue TypeAgentResume MQ task.
	payload := agentResumeTaskPayload{
		ConversationID: params.ConversationID,
		CheckpointID:   checkpointID,
		AgentID:        params.AgentID,
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
		"status":   "queued",
		"answered": answered,
		"total":    total,
	})
}
