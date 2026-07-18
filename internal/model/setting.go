package model

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Setting 键值配置，用于持久化运行时参数（如 JWT 主密钥）。
type Setting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"type:text;not null" json:"value"`
}

const (
	SettingKeyJWTSecret     = "jwt_secret"
	SettingKeyAllowRegister = "allow_register"
)

// IsRegisterAllowed 是否允许新用户注册。
// 库中无记录时默认 true；当系统尚无任何用户时始终允许（保证首个管理员可注册）。
func IsRegisterAllowed() (bool, error) {
	var count int64
	if err := DB.Model(&User{}).Count(&count).Error; err != nil {
		return false, fmt.Errorf("检查用户数量失败: %w", err)
	}
	if count == 0 {
		return true, nil
	}

	val, err := GetSetting(SettingKeyAllowRegister)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return true, nil // 默认开放注册
		}
		return false, err
	}
	return val == "true" || val == "1", nil
}

// SetAllowRegister 写入「允许新用户注册」开关。
func SetAllowRegister(allow bool) error {
	v := "false"
	if allow {
		v = "true"
	}
	return SetSetting(SettingKeyAllowRegister, v)
}

// GetSetting 读取配置项；不存在时返回 gorm.ErrRecordNotFound。
func GetSetting(key string) (string, error) {
	var s Setting
	if err := DB.Where("key = ?", key).First(&s).Error; err != nil {
		return "", err
	}
	return s.Value, nil
}

// SetSetting 写入或更新配置项。
func SetSetting(key, value string) error {
	s := Setting{Key: key, Value: value}
	return DB.Save(&s).Error
}

// GenerateJWTSecret 生成随机 JWT 主密钥（32 字节，base64）。
func GenerateJWTSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成密钥失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// EnsureJWTSecret 保证库中存在 JWT 密钥并返回。
// 库中已有则直接使用；否则自动生成并写入库（不读环境变量）。
func EnsureJWTSecret() (string, error) {
	val, err := GetSetting(SettingKeyJWTSecret)
	if err == nil && val != "" {
		return val, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("读取 JWT 密钥失败: %w", err)
	}

	secret, genErr := GenerateJWTSecret()
	if genErr != nil {
		return "", genErr
	}
	if err := SetSetting(SettingKeyJWTSecret, secret); err != nil {
		return "", fmt.Errorf("保存 JWT 密钥失败: %w", err)
	}
	return secret, nil
}

// RotateJWTSecret 生成新密钥写入数据库并返回。
func RotateJWTSecret() (string, error) {
	secret, err := GenerateJWTSecret()
	if err != nil {
		return "", err
	}
	if err := SetSetting(SettingKeyJWTSecret, secret); err != nil {
		return "", fmt.Errorf("保存 JWT 密钥失败: %w", err)
	}
	return secret, nil
}

