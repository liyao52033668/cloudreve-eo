package model

import "time"

type Share struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	UserID    uint       `gorm:"index;not null" json:"user_id"`
	FileID    uint       `gorm:"index;not null" json:"file_id"`
	Code      string     `gorm:"uniqueIndex;size:16;not null" json:"code"`
	Password  string     `gorm:"size:16" json:"-"`
	ExpireAt  *time.Time `json:"expire_at"`
	Views     int        `gorm:"not null;default:0" json:"views"`
	CreatedAt time.Time  `json:"created_at"`
}
