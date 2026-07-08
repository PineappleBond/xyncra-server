package model

import "time"

// SyncState stores key-value pairs for client-side synchronization state,
// such as local_max_seq and latest_seq trackers.
type SyncState struct {
	Key       string `gorm:"primaryKey;size:64"`
	Value     string `gorm:"type:text"`
	UpdatedAt time.Time
}
