package model

import (
	"time"

	"gorm.io/gorm"
)

// Conversation represents a 1-on-1 messaging conversation between two users.
type Conversation struct {
	ID                     string `gorm:"primaryKey;size:36"`
	UserID1                string `gorm:"size:64;index:idx_conversation_user1_deleted,priority:1;uniqueIndex:idx_conversation_users_unique,priority:1"`
	UserID2                string `gorm:"size:64;index:idx_conversation_user2_deleted,priority:1;uniqueIndex:idx_conversation_users_unique,priority:2"` // only 1-on-1 not null
	Type                   string `gorm:"size:20;index"`                                                                                                // 1-on-1 / group / channel
	Title                  string `gorm:"size:255"`
	Pinned                 bool
	Muted                  bool
	AvatarURL              string `gorm:"size:512"`
	Description            string `gorm:"type:text"`
	LastProcessedMessageID uint32
	CreatedAt              time.Time `gorm:"index"`
	UpdatedAt              time.Time
	LastMessageAt          time.Time `gorm:"index:idx_conversation_lastmsg_deleted,priority:1"`
	LastReadMessageID1     uint32    // UserID1's read cursor position (D-012)
	LastReadMessageID2     uint32    // UserID2's read cursor position (D-012)

	// HITL state machine fields (D-125). Mirrors the server-side model so that
	// get_conversation responses deserialise correctly.
	AgentStatus       string    `gorm:"size:32;not null;default:'idle';index" json:"agent_status"`
	AgentID           string    `gorm:"size:64" json:"agent_id"`
	CheckpointID      string    `gorm:"size:36" json:"checkpoint_id"`
	AgentLastActivity time.Time `json:"agent_last_activity"`

	DeletedAt gorm.DeletedAt `gorm:"index:idx_conversation_user1_deleted,priority:2;index:idx_conversation_user2_deleted,priority:2;index:idx_conversation_lastmsg_deleted,priority:2;index"`
}

// Agent status constants for the HITL state machine (D-125).
const (
	AgentStatusIdle        = "idle"
	AgentStatusThinking    = "thinking"
	AgentStatusToolCalling = "tool_calling"
	AgentStatusGenerating  = "generating"
	AgentStatusAskingUser  = "asking_user"
	AgentStatusTimeout     = "timeout"
)
