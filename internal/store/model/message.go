package model

import (
	"time"

	"gorm.io/gorm"
)

type Message struct {
	ID string `gorm:"primaryKey;size:36"`

	ClientMessageID string `gorm:"size:36;uniqueIndex:idx_msg_client_id_sender,priority:1"`

	ConversationID string `gorm:"size:36;index:idx_message_conv_msg_deleted,priority:1"`
	MessageID      uint32 `gorm:"index:idx_message_conv_msg_deleted,priority:2"`
	SenderID       string `gorm:"size:64;index;uniqueIndex:idx_msg_client_id_sender,priority:2"`
	Content        string `gorm:"type:text"`
	Type           string `gorm:"size:20;default:'text'"`
	ReplyTo        uint32
	Status         string    `gorm:"size:20;default:'sent'"`
	CreatedAt      time.Time `gorm:"index"`

	DeletedAt gorm.DeletedAt `gorm:"index:idx_message_conv_msg_deleted,priority:3;index"`
}
