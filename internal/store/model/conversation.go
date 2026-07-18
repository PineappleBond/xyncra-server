package model

import (
	"time"

	"gorm.io/gorm"
)

type Conversation struct {
	ID                     string    `gorm:"primaryKey;size:36" json:"id"`
	UserID1                string    `gorm:"size:64;index:idx_conversation_user1_deleted,priority:1;uniqueIndex:idx_conversation_users_unique,priority:1" json:"user_id1"`
	UserID2                string    `gorm:"size:64;index:idx_conversation_user2_deleted,priority:1;uniqueIndex:idx_conversation_users_unique,priority:2" json:"user_id2"` // only 1-on-1 not null
	Type                   string    `gorm:"size:20;index" json:"type"`                                                                                                    // 1-on-1 / group / channel
	Title                  string    `gorm:"size:255" json:"title"`
	Pinned                 bool      `json:"pinned"`
	Muted                  bool      `json:"muted"`
	AvatarURL              string    `gorm:"size:512" json:"avatar_url"`
	Description            string    `gorm:"type:text" json:"description"`
	LastProcessedMessageID uint32    `json:"last_processed_message_id"`
	CreatedAt              time.Time `gorm:"index" json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
	LastMessageAt          time.Time `gorm:"index:idx_conversation_lastmsg_deleted,priority:1" json:"last_message_at"`
	LastReadMessageID1     uint32    `json:"last_read_message_id1"` // UserID1's read cursor position (D-012)
	LastReadMessageID2     uint32    `json:"last_read_message_id2"` // UserID2's read cursor position (D-012)

	// HITL state machine fields
	AgentStatus       string    `gorm:"size:32;not null;default:'idle';index" json:"agent_status"`
	AgentID           string    `gorm:"size:64" json:"agent_id"`
	CheckpointID      string    `gorm:"size:36" json:"checkpoint_id"`
	AgentLastActivity time.Time `json:"agent_last_activity"`

	DeletedAt gorm.DeletedAt `gorm:"index:idx_conversation_user1_deleted,priority:2;index:idx_conversation_user2_deleted,priority:2;index:idx_conversation_lastmsg_deleted,priority:2;index"`
}

// Agent status constants for the HITL state machine.
const (
	AgentStatusIdle        = "idle"
	AgentStatusThinking    = "thinking"
	AgentStatusToolCalling = "tool_calling"
	AgentStatusGenerating  = "generating"
	AgentStatusAskingUser  = "asking_user"
	AgentStatusTimeout     = "timeout"
)
