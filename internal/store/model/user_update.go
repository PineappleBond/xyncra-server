package model

import "time"

type UserUpdate struct {
	ID        string `gorm:"primaryKey;size:36"`
	UserID    string `gorm:"size:64;index:idx_user_update_user_seq,priority:1;index"`
	Seq       uint32 `gorm:"index:idx_user_update_user_seq,priority:2"`
	Type      string `gorm:"size:20;default:'message';index"`
	Payload   []byte
	CreatedAt time.Time `gorm:"index"`
}
