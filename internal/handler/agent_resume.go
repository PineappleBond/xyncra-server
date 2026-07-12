// agent_resume RPC handler (Phase 8B / D-085).
package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// agentResumeParams is the JSON-decoded representation of the client-supplied
// parameters for the "agent_resume" RPC method.
type agentResumeParams struct {
	ConversationID string `json:"conversation_id"`
	CheckpointID   string `json:"checkpoint_id"`
	InterruptID    string `json:"interrupt_id"`
	Answer         string `json:"answer"`
	AgentID        string `json:"agent_id"` // agent to resume (e.g. "agent/xxx")
}

// agentResumeHandler handles the "agent_resume" RPC method (D-085).
// It enqueues a TypeAgentResume MQ task so the resume is processed
// asynchronously with the same lock/serialization as normal agent tasks.
type agentResumeHandler struct {
	broker mq.Broker
}

// NewAgentResumeHandler creates a handler for the "agent_resume" RPC method.
func NewAgentResumeHandler(broker mq.Broker) *agentResumeHandler {
	return &agentResumeHandler{broker: broker}
}

// agentResumeTaskPayload is the MQ task payload for TypeAgentResume.
type agentResumeTaskPayload struct {
	ConversationID string `json:"conversation_id"`
	CheckpointID   string `json:"checkpoint_id"`
	InterruptID    string `json:"interrupt_id"`
	Answer         string `json:"answer"`
	SenderID       string `json:"sender_id"` // human user who sent the answer
	AgentID        string `json:"agent_id"`  // agent to resume
	DeviceID       string `json:"device_id"` // Phase 6 (D-102)
}

// HandleRequest implements MethodHandler. It validates the params and enqueues
// a TypeAgentResume task. No auth (D-002).
func (h *agentResumeHandler) HandleRequest(ctx context.Context, client *server.Client, req *protocol.PackageDataRequest) (json.RawMessage, error) {
	var params agentResumeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("agent_resume: invalid params: %w", err)
	}
	if params.ConversationID == "" || params.CheckpointID == "" || params.AgentID == "" {
		return nil, fmt.Errorf("agent_resume: conversation_id, checkpoint_id and agent_id are required")
	}

	// The sender is the authenticated client user.
	senderID := ""
	deviceID := "" // Phase 6 (D-102)
	if client != nil {
		senderID = client.UserID()
		deviceID = client.DeviceID()
	}

	payload := agentResumeTaskPayload{
		ConversationID: params.ConversationID,
		CheckpointID:   params.CheckpointID,
		InterruptID:    params.InterruptID,
		Answer:         params.Answer,
		SenderID:       senderID,
		AgentID:        params.AgentID,
		DeviceID:       deviceID, // Phase 6 (D-102)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("agent_resume: marshal payload: %w", err)
	}

	task := &mq.Task{
		Type:    mq.TypeAgentResume,
		Payload: raw,
		Queue:   mq.QueueDefault,
	}
	if _, err := h.broker.Enqueue(ctx, task); err != nil {
		return nil, fmt.Errorf("agent_resume: enqueue task: %w", err)
	}

	return json.Marshal(map[string]string{"status": "queued"})
}
