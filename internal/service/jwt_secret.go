package service

import (
	"sync"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
)

// JWTSecretStore 内存中的 JWT 主密钥，支持热轮转。
type JWTSecretStore struct {
	mu     sync.RWMutex
	secret string
}

func NewJWTSecretStore(secret string) *JWTSecretStore {
	return &JWTSecretStore{secret: secret}
}

func (s *JWTSecretStore) Get() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.secret
}

// Rotate 生成新密钥、写入数据库并更新内存；返回新密钥。
// 轮转后所有既有 JWT 立即失效。
func (s *JWTSecretStore) Rotate() (string, error) {
	secret, err := model.RotateJWTSecret()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.secret = secret
	s.mu.Unlock()
	return secret, nil
}
