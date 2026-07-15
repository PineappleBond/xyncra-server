package model

import (
	"time"

	"gorm.io/gorm"
)

// Question represents a HITL question asked by an Agent to a user.
// Questions are persisted to database to survive server restarts and
// support offline users, multi-device sync, and partial answers.
type Question struct {
	ID               string         `gorm:"primaryKey;size:36" json:"id"`
	ConversationID   string         `gorm:"size:36;index;not null" json:"conversation_id"`
	CheckpointID     string         `gorm:"size:36;not null" json:"checkpoint_id"`
	InterruptID      string         `gorm:"size:64;not null" json:"interrupt_id"`
	QuestionText     string         `gorm:"type:text;not null" json:"question_text"`
	Status           string         `gorm:"size:16;not null;default:'pending';index" json:"status"` // "pending" | "answered"
	Answer           string         `gorm:"type:text" json:"answer"`
	AnsweredBy       string         `gorm:"size:64" json:"answered_by"`
	AnsweredDeviceID string         `gorm:"size:64" json:"answered_device_id"`
	CreatedAt        time.Time      `json:"created_at"`
	AnsweredAt       *time.Time     `json:"answered_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index"`

	// Conversation relationship
	Conversation Conversation `gorm:"foreignKey:ConversationID" json:"-"`
}

// Question status constants
const (
	QuestionStatusPending  = "pending"
	QuestionStatusAnswered = "answered"
)

// TableName overrides the default table name
func (Question) TableName() string {
	return "questions"
}
