package model

import (
	"time"

	"gorm.io/gorm"
)

// RemoteCalling represents a remote call initiated by an Agent (client-side, D-137).
// Unifies HITL questions and client function calls into a single model.
type RemoteCalling struct {
	ID             string         `gorm:"primaryKey;size:36" json:"id"`
	ConversationID string         `gorm:"size:36;index;not null" json:"conversation_id"`
	CheckpointID   string         `gorm:"size:36;not null" json:"checkpoint_id"`
	AgentID        string         `gorm:"size:64" json:"agent_id"`
	Method         string         `gorm:"size:128;not null" json:"method"`
	Params         string         `gorm:"type:text" json:"params"`
	InterruptID    string         `gorm:"size:64" json:"interrupt_id"`
	DeviceID       string         `gorm:"size:64;index" json:"device_id"`
	Status         string         `gorm:"size:16;not null;default:'pending';index" json:"status"`
	Result         string         `gorm:"type:text" json:"result"`
	ErrorMessage   string         `gorm:"type:text" json:"error_message"`
	Success        bool           `json:"success"`
	CreatedAt      time.Time      `json:"created_at"`
	ResolvedAt     *time.Time     `json:"resolved_at"`
	ExpiresAt      *time.Time     `json:"expires_at"`
	CancelledAt    *time.Time     `json:"cancelled_at"`
	CancelledBy    string         `gorm:"size:64" json:"cancelled_by"`
	CancelReason   string         `gorm:"type:text" json:"cancel_reason"`
	DeletedAt      gorm.DeletedAt `gorm:"index"`
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
