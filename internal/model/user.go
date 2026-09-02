package model

import (
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	Username       string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash   string    `gorm:"size:128;not null" json:"-"`
	IsAdmin        bool      `gorm:"not null;default:false" json:"is_admin"`
	GroupID        uint      `gorm:"not null;default:0;index" json:"group_id"`
	StorageQuota   int64     `gorm:"not null;default:0" json:"storage_quota"`
	StorageUsed    int64     `gorm:"not null;default:0" json:"storage_used"`
	Banned         bool      `gorm:"not null;default:false" json:"banned"`
	WebDAVPassword string    `gorm:"column:webdav_password;size:128" json:"-"` // WebDAV 专用密码（bcrypt hash）
	CreatedAt      time.Time `json:"created_at"`
}

// MarshalJSON 自定义 JSON 序列化，格式化时间为易读格式，ID 转为字符串避免 JS 精度丢失。
func (u User) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		ID           string `json:"id"`
		Username     string `json:"username"`
		IsAdmin      bool   `json:"is_admin"`
		GroupID      uint   `json:"group_id"`
		StorageQuota int64  `json:"storage_quota"`
		StorageUsed  int64  `json:"storage_used"`
		Banned       bool   `json:"banned"`
		CreatedAt    string `json:"created_at"`
	}{
		ID:           fmt.Sprintf("%d", u.ID),
		Username:     u.Username,
		IsAdmin:      u.IsAdmin,
		GroupID:      u.GroupID,
		StorageQuota: u.StorageQuota,
		StorageUsed:  u.StorageUsed,
		Banned:       u.Banned,
		CreatedAt:    u.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}

// IsUserAdmin 查询用户是否为管理员。
func IsUserAdmin(userID int64) (bool, error) {
	var user User
	if err := DB.Select("id", "is_admin").First(&user, userID).Error; err != nil {
		return false, err
	}
	return user.IsAdmin, nil
}

// GetUserByID 按 ID 查询用户。
func GetUserByID(id int64) (*User, error) {
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

// ListUsers 全部用户，管理员优先，再按创建时间倒序。
func ListUsers() ([]User, error) {
	var users []User
	err := DB.Order("is_admin DESC, id DESC").Find(&users).Error
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

// SetWebDAVPassword 设置用户的 WebDAV 密码（bcrypt hash）。
func SetWebDAVPassword(userID int64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return DB.Model(&User{}).Where("id = ?", userID).Update("webdav_password", string(hash)).Error
}

// VerifyWebDAVPassword 验证用户的 WebDAV 密码。
func VerifyWebDAVPassword(userID int64, password string) (bool, error) {
	var user User
	if err := DB.Select("id", "webdav_password").First(&user, userID).Error; err != nil {
		return false, err
	}
	if user.WebDAVPassword == "" {
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword([]byte(user.WebDAVPassword), []byte(password))
	return err == nil, nil
}

// GetWebDAVPasswordHash 获取用户的 WebDAV 密码 hash（用于 Basic Auth）。
func GetWebDAVPasswordHash(userID int64) (string, error) {
	var user User
	if err := DB.Select("id", "webdav_password").First(&user, userID).Error; err != nil {
		return "", err
	}
	return user.WebDAVPassword, nil
}
