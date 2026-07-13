package protocol

import (
	"encoding/json"
	"time"
)

// PackageType identifies the kind of a WebSocket protocol Package.
type PackageType uint8

const (
	// PackageTypeRequest is a client-initiated request.
	PackageTypeRequest PackageType = iota
	// PackageTypeResponse is a server response to a request.
	PackageTypeResponse
	// PackageTypeUpdates is a push notification with data updates.
	PackageTypeUpdates
)

// Update type constants for UserUpdate.Type and PackageDataUpdate.Type.
const (
	UpdateTypeMessage       = "message"        // New message notification
	UpdateTypeDeleteMessage = "delete_message" // Message deletion
	UpdateTypeMarkRead      = "mark_read"      // Read cursor update
	UpdateTypeConversation  = "conversation"   // Conversation state change (delete/restore)
	UpdateTypeGap           = "gap"            // Synthetic gap filler (runtime only, never persisted)
	UpdateTypeTyping        = "typing"         // ephemeral: Seq=0, never persisted, never pulled
	UpdateTypeStreaming     = "streaming"      // ephemeral: Seq=0, cumulative text streaming

	// Phase 8B: Agent ephemeral Update types (D-087). All use Seq=0 (D-050).
	UpdateTypeAgentStatus            = "agent_status"             // ephemeral: Seq=0, agent status (thinking/tool_calling/generating/idle/asking_user)
	UpdateTypeAgentQuestion          = "agent_question"           // ephemeral: Seq=0, agent asking user a question during HITL
	UpdateTypeAgentCheckpointCreated = "agent_checkpoint_created" // ephemeral: Seq=0, checkpoint was saved
	UpdateTypeAgentTimeout           = "agent_timeout"            // ephemeral: Seq=0, agent execution timed out
)

// Package is the top-level message envelope for the WebSocket protocol.
type Package struct {
	// Version is the protocol version. Defaults to 1 when zero-valued.
	Version uint8           `json:"version,omitempty"`
	Type    PackageType     `json:"type"`
	Data    json.RawMessage `json:"data"`
}

// PackageDataRequest is a client-initiated request to invoke a method.
type PackageDataRequest struct {
	// ID is a unique identifier for correlating requests with responses.
	ID string `json:"id"`
	// Method is the name of the method to invoke on the server.
	Method string `json:"method"`
	// Params contains the method parameters as JSON.
	Params json.RawMessage `json:"params"`
	// IdempotencyKey is a server-generated key (equal to reqID) used for
	// deduplication during replay of timed-out requests (Phase 4, D-104).
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// Seq is a per-device monotonically increasing sequence number assigned
	// by the server for ordering reverse-RPC requests (Phase 4, D-104).
	Seq uint64 `json:"seq,omitempty"`
}

// ResponseCode indicates the result status of a request. Zero or positive
// values indicate success; negative values indicate errors.
type ResponseCode int32

const (
	// ResponseCodeOK indicates the request was processed successfully.
	ResponseCodeOK ResponseCode = 0
	// ResponseCodeError indicates the request failed with an error.
	ResponseCodeError ResponseCode = -1
)

// PackageDataResponse is the server's reply to a PackageDataRequest.
type PackageDataResponse struct {
	// ID correlates this response with the originating request.
	ID string `json:"id"`
	// Code indicates success (0) or an error (negative value).
	Code ResponseCode `json:"code"`
	// Msg provides a human-readable status message.
	Msg string `json:"msg"`
	// Data contains the response payload as JSON.
	Data json.RawMessage `json:"data"`
}

// PackageDataUpdates wraps a batch of data update notifications.
type PackageDataUpdates struct {
	Updates []PackageDataUpdate `json:"updates"`
}

// PackageDataUpdate represents a single incremental data change.
type PackageDataUpdate struct {
	// Seq is a monotonically increasing sequence number for ordering.
	Seq uint32 `json:"seq"`
	// Type identifies the kind of update (e.g. "message", "delete_message").
	Type string `json:"type"`
	// Payload contains the update data as JSON.
	Payload json.RawMessage `json:"payload"`
	// CreatedAt is the timestamp when this update was generated.
	CreatedAt time.Time `json:"created_at,omitempty"`
}
