package model

import (
	"time"

	"gorm.io/gorm"
)

type Message struct {
	ID              string    `gorm:"primaryKey;size:36" json:"id"`
	ClientMessageID string    `gorm:"size:36;uniqueIndex:idx_msg_client_id_sender,priority:1" json:"client_message_id"`
	ConversationID  string    `gorm:"size:36;index:idx_message_conv_msg_deleted,priority:1" json:"conversation_id"`
	MessageID       uint32    `gorm:"index:idx_message_conv_msg_deleted,priority:2" json:"message_id"`
	SenderID        string    `gorm:"size:64;index;uniqueIndex:idx_msg_client_id_sender,priority:2" json:"sender_id"`
	Content         string    `gorm:"type:text" json:"content"`
	Type            string    `gorm:"size:20;default:'text'" json:"type"`
	ReplyTo         uint32    `json:"reply_to"`
	Status          string    `gorm:"size:20;default:'sent'" json:"status"`
	CreatedAt       time.Time `gorm:"index" json:"created_at"`

	DeletedAt gorm.DeletedAt `gorm:"index:idx_message_conv_msg_deleted,priority:3;index"`
}
