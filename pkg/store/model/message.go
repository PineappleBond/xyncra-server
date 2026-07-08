package model

import (
	"time"

	"gorm.io/gorm"
)

// Message represents a single message within a conversation.
type Message struct {
	ID string `gorm:"primaryKey;size:36"`

	ClientMessageID string `gorm:"size:36;uniqueIndex"`

	ConversationID string `gorm:"size:36;index:idx_message_conv_msg_deleted,priority:1"`
	MessageID      uint32 `gorm:"index:idx_message_conv_msg_deleted,priority:2"`
	SenderID       string `gorm:"size:64;index"`
	Content        string `gorm:"type:text"`
	Type           string `gorm:"size:20;default:'text'"`
	ReplyTo        uint32
	Status         string    `gorm:"size:20;default:'sent'"`
	CreatedAt      time.Time `gorm:"index"`

	DeletedAt gorm.DeletedAt `gorm:"index:idx_message_conv_msg_deleted,priority:3;index"`
}
