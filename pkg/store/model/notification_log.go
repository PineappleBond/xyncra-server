package model

import "time"

// NotificationLog records a received push notification for deduplication
// and auditing.
type NotificationLog struct {
	ID        string    `gorm:"primaryKey;size:36"`
	Seq       uint32    `gorm:"uniqueIndex"`
	Type      string    `gorm:"size:20;index"`
	Payload   []byte    `gorm:"type:blob"`
	CreatedAt time.Time `gorm:"index"`
}
