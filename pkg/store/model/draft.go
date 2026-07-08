package model

import "time"

// Draft represents a message draft for a conversation.
// Each conversation can have at most one draft (ConversationID is uniqueIndex).
type Draft struct {
	ID             string `gorm:"primaryKey;size:36"`
	ConversationID string `gorm:"size:36;uniqueIndex"`
	Content        string `gorm:"type:text"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
