package model

import "time"

type File struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	UserID        uint      `gorm:"index;not null" json:"user_id"`
	ParentID      uint      `gorm:"index;not null;default:0" json:"parent_id"`
	Name          string    `gorm:"size:255;not null" json:"name"`
	IsDir         bool      `gorm:"not null;default:false" json:"is_dir"`
	Size          int64     `gorm:"not null;default:0" json:"size"`
	MimeType      string    `gorm:"size:128" json:"mime_type"`
	StorageKey    string    `gorm:"size:512" json:"storage_key"`
	StoragePolicy string    `gorm:"size:32" json:"storage_policy"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
