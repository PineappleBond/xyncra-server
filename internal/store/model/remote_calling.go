package model

import (
	"time"

	"gorm.io/gorm"
)

// RemoteCalling represents a remote call initiated by an Agent during execution.
// It unifies HITL questions and client function calls into a single model.
// RemoteCallings are persisted to database to survive server restarts and
// support offline users, multi-device sync, and partial resolution.
//
// D-137: RemoteCalling unified model — Question table deprecated.
type RemoteCalling struct {
	ID             string         `gorm:"primaryKey;size:36" json:"id"`
	ConversationID string         `gorm:"size:36;index;not null;index:idx_rc_conversation_status,priority:1" json:"conversation_id"`
	CheckpointID   string         `gorm:"size:36;not null;index:idx_rc_checkpoint_status" json:"checkpoint_id"`
	AgentID        string         `gorm:"size:64" json:"agent_id"`
	MessageID      uint32         `gorm:"index" json:"message_id"`          // D-141: optional, links to tool_calling message
	Method         string         `gorm:"size:128;not null" json:"method"`  // e.g. ask_user, pg_chatai_sendMessage
	Params         string         `gorm:"type:text" json:"params"`          // JSON parameters
	InterruptID    string         `gorm:"size:64" json:"interrupt_id"`      // Eino interrupt ID (ask_user only)
	DeviceID       string         `gorm:"size:64" json:"device_id"`          // empty = any device, non-empty = specific device
	// NOTE: standalone index on Status removed — compound indexes
	// (idx_rc_checkpoint_status, idx_rc_conversation_status, idx_rc_status_expires)
	// already cover status-based queries, making a standalone index redundant.
	Status         string         `gorm:"size:16;not null;default:'pending';index:idx_rc_checkpoint_status;index:idx_rc_conversation_status,priority:2;index:idx_rc_status_expires,priority:1" json:"status"` // pending | resolved | cancelled | expired
	Result         string         `gorm:"type:text" json:"result"`          // result on success
	ErrorMessage   string         `gorm:"type:text" json:"error_message"`   // error on failure
	Success        bool           `json:"success"`                          // whether the call succeeded
	CreatedAt      time.Time      `json:"created_at"`
	ResolvedAt     *time.Time     `json:"resolved_at"`
	ExpiresAt      *time.Time     `gorm:"index:idx_rc_status_expires,priority:2" json:"expires_at"`
	CancelledAt    *time.Time     `json:"cancelled_at"`
	CancelledBy    string         `gorm:"size:64" json:"cancelled_by"`
	CancelReason   string         `gorm:"type:text" json:"cancel_reason"`
	DeletedAt      gorm.DeletedAt `gorm:"index"`

	// Conversation relationship
	Conversation Conversation `gorm:"foreignKey:ConversationID" json:"-"`
}

// RemoteCalling status constants.
const (
	RemoteCallingStatusPending   = "pending"
	RemoteCallingStatusResolved  = "resolved"
	RemoteCallingStatusCancelled = "cancelled"
	RemoteCallingStatusExpired   = "expired"
)

// TableName overrides the default table name.
func (RemoteCalling) TableName() string {
	return "remote_callings"
}
