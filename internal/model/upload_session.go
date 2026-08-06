package model

import (
	"time"
)

// UploadSession 分片上传会话，持久化以支持断点续传。
type UploadSession struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	UserID        int64     `gorm:"index;not null" json:"user_id"`
	UploadID      string    `gorm:"size:1024;not null" json:"upload_id"`
	StorageKey    string    `gorm:"size:512;not null;uniqueIndex" json:"storage_key"`
	StoragePolicy string    `gorm:"size:64;not null" json:"storage_policy"`
	FileName      string    `gorm:"size:512;not null" json:"file_name"`
	ContentType   string    `gorm:"size:255" json:"content_type"`
	Size          int64     `gorm:"not null" json:"size"`
	ChunkSize     int64     `gorm:"not null" json:"chunk_size"`
	ParentID      uint      `json:"parent_id"`
	ExpiresAt     time.Time `gorm:"index" json:"expires_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CreateUploadSession 写入会话记录。
func CreateUploadSession(s *UploadSession) error {
	return DB.Create(s).Error
}

// GetUploadSession 按 storage_key 查询当前用户的会话。
func GetUploadSession(userID int64, storageKey string) (*UploadSession, error) {
	var s UploadSession
	if err := DB.Where("user_id = ? AND storage_key = ?", userID, storageKey).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// ListUploadSessions 列出当前用户未过期的会话（供前端恢复未完成上传）。
func ListUploadSessions(userID int64) ([]UploadSession, error) {
	var list []UploadSession
	err := DB.Where("user_id = ? AND expires_at > ?", userID, time.Now()).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

// DeleteUploadSession 删除会话（完成或取消后调用）。
func DeleteUploadSession(userID int64, storageKey string) error {
	return DB.Where("user_id = ? AND storage_key = ?", userID, storageKey).
		Delete(&UploadSession{}).Error
}
