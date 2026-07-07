package model

import (
	"time"

	"gorm.io/gorm"
)

type Conversation struct {
	ID                     string `gorm:"primaryKey;size:36"`
	UserID1                string `gorm:"size:64;index:idx_conversation_user1_deleted,priority:1;index:idx_conversation_users_unique,priority:1"`
	UserID2                string `gorm:"size:64;index:idx_conversation_user2_deleted,priority:1;index:idx_conversation_users_unique,priority:2"` // only 1-on-1 not null
	Type                   string `gorm:"size:20;index"`                                                                                       // 1-on-1 / group / channel
	Title                  string `gorm:"size:255"`
	Pinned                 bool
	Muted                  bool
	AvatarURL              string `gorm:"size:512"`
	Description            string `gorm:"type:text"`
	LastProcessedMessageID uint32
	CreatedAt              time.Time `gorm:"index"`
	UpdatedAt              time.Time
	LastMessageAt          time.Time `gorm:"index:idx_conversation_lastmsg_deleted,priority:1"`

	DeletedAt gorm.DeletedAt `gorm:"index:idx_conversation_user1_deleted,priority:2;index:idx_conversation_user2_deleted,priority:2;index:idx_conversation_lastmsg_deleted,priority:2;index;index:idx_conversation_users_unique,priority:3"`
}
