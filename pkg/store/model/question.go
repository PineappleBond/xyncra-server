package model

import "time"

// Question represents a HITL question (client-side mirror, D-125).
// Only contains fields needed for display; answer-related fields
// (Answer, AnsweredBy, etc.) are server-side only.
type Question struct {
	ID             string    `gorm:"primaryKey;size:36" json:"id"`
	ConversationID string    `gorm:"size:36;index;not null" json:"conversation_id"`
	CheckpointID   string    `gorm:"size:36" json:"checkpoint_id"`
	InterruptID    string    `gorm:"size:64" json:"interrupt_id"`
	QuestionText   string    `gorm:"type:text" json:"question_text"`
	Status         string    `gorm:"size:16;default:'pending'" json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// TableName returns the GORM table name.
func (Question) TableName() string {
	return "questions"
}
