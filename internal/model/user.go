package model

import "time"

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string    `gorm:"size:128;not null" json:"-"`
	StorageQuota int64     `gorm:"not null;default:1073741824" json:"storage_quota"`
	StorageUsed  int64     `gorm:"not null;default:0" json:"storage_used"`
	CreatedAt    time.Time `json:"created_at"`
}
