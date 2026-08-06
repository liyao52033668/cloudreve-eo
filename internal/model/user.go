package model

import "time"

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string    `gorm:"size:128;not null" json:"-"`
	IsAdmin      bool      `gorm:"not null;default:false" json:"is_admin"`
	GroupID      uint      `gorm:"not null;default:0;index" json:"group_id"`
	StorageQuota int64     `gorm:"not null;default:0" json:"storage_quota"`
	StorageUsed  int64     `gorm:"not null;default:0" json:"storage_used"`
	Banned       bool      `gorm:"not null;default:false" json:"banned"`
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

// GetUserByID 按 ID 查询用户。
func GetUserByID(id uint) (*User, error) {
	var user User
	if err := DB.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByUsername 按用户名查询。
func GetUserByUsername(username string) (*User, error) {
	var user User
	if err := DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// ListUsers 全部用户，按创建时间倒序。
func ListUsers() ([]User, error) {
	var users []User
	err := DB.Order("id DESC").Find(&users).Error
	return users, err
}

// CountUsers 用户总数（用于判断首个用户自动成为管理员）。
func CountUsers() (int64, error) {
	var count int64
	if err := DB.Model(&User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
