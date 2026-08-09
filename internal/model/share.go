package model

import "time"

type Share struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	UserID    int64      `gorm:"index;not null" json:"user_id"`
	// 多文件分享：逗号分隔的 File ID 列表（如 "12" 或 "12,34"），与旧字段 FileID 二选一
	FileIDs   string     `gorm:"size:255;index" json:"file_ids"`
	// 已废弃：单文件分享保留兼容旧数据，新分享一律写入 FileIDs
	FileID    uint       `gorm:"index" json:"file_id"`
	Code      string     `gorm:"uniqueIndex;size:16;not null" json:"code"`
	Password  string     `gorm:"size:16" json:"-"`
	ExpireAt  *time.Time `json:"expire_at"`
	Views     int        `gorm:"not null;default:0" json:"views"`
	CreatedAt time.Time  `json:"created_at"`
}
