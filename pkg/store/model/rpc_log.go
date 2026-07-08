package model

import "time"

// RPCLog records a single RPC call for observability and debugging.
type RPCLog struct {
	ID             string `gorm:"primaryKey;size:36"`
	Type           string `gorm:"size:16;index"` // "request" or "response"
	RequestID      string `gorm:"size:64;index"`
	Method         string `gorm:"size:64;index"`
	Params         []byte `gorm:"type:blob"`
	Response       []byte `gorm:"type:blob"`
	StatusCode     int    `gorm:"index"`
	ConversationID string `gorm:"size:36;index"`
	Duration       time.Duration
	ErrorMsg       string    `gorm:"type:text"`
	CreatedAt      time.Time `gorm:"index"`
}
