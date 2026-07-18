package model

import "time"

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string    `gorm:"size:128;not null" json:"-"`
	IsAdmin      bool      `gorm:"not null;default:false" json:"is_admin"`
	StorageQuota int64     `gorm:"not null;default:0" json:"storage_quota"`
	StorageUsed  int64     `gorm:"not null;default:0" json:"storage_used"`
	CreatedAt    time.Time `json:"created_at"`
}

// IsUserAdmin 查询用户是否为管理员。
func IsUserAdmin(userID uint) (bool, error) {
	var user User
	if err := DB.Select("id", "is_admin").First(&user, userID).Error; err != nil {
		return false, err
	}
	return user.IsAdmin, nil
}
