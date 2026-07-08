package model

import "time"

// RetryTask represents a pending RPC retry task with exponential backoff.
type RetryTask struct {
	ID          string    `gorm:"primaryKey;size:36"`
	Method      string    `gorm:"size:64;index"`
	Params      []byte    `gorm:"type:blob"`
	Attempt     int       `gorm:"default:0"`
	MaxAttempts int       `gorm:"default:5"`
	NextRetry   time.Time `gorm:"index"`
	Status      string    `gorm:"size:20;default:'pending';index"`
	LastError   string    `gorm:"type:text"`
	CreatedAt   time.Time `gorm:"index"`
}
