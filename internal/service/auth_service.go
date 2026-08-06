package service

import (
	"errors"
	"fmt"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/snowflake"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService struct{}

func NewAuthService() *AuthService {
	return &AuthService{}
}

func (s *AuthService) Register(username, password string) (*model.User, error) {
	allowed, err := model.IsRegisterAllowed()
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errors.New("当前未开放注册")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败: %w", err)
	}

	// 首个注册用户自动成为管理员（可管理 JWT 主密钥等）。
	count, err := model.CountUsers()
	if err != nil {
		return nil, fmt.Errorf("检查用户数量失败: %w", err)
	}

	// 新用户归入默认用户组；尚无用户组时自动创建。
	groupID, err := model.EnsureDefaultGroup()
	if err != nil {
		return nil, fmt.Errorf("初始化默认用户组失败: %w", err)
	}

	// 容量由用户组（绑定存储策略与最大容量）控制；用户级 StorageQuota 字段保留兼容，固定为 0。
	user := &model.User{
		ID:           snowflake.Generate(),
		Username:     username,
		PasswordHash: string(hash),
		IsAdmin:      count == 0,
		GroupID:      groupID,
		StorageQuota: 0,
	}

	if err := model.DB.Create(user).Error; err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}
	return user, nil
}

func (s *AuthService) Login(username, password string) (*model.User, error) {
	var user model.User
	if err := model.DB.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户名或密码错误")
		}
		return nil, err
	}

	if user.Banned {
		return nil, errors.New("账号已被封禁")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("用户名或密码错误")
	}
	return &user, nil
}
