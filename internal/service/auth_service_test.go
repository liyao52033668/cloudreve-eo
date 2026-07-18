package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"golang.org/x/crypto/bcrypt"
)

func setupTestDB(t *testing.T) *config.Config {
	t.Helper()
	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "auth.db"),
		},
		Storage: config.StorageConfig{
			DefaultQuota: 2 * 1024 * 1024 * 1024, // 2GB for tests
		},
		JWT: config.JWTConfig{Secret: "test-secret"},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	return cfg
}

func TestAuthService_Register_Success(t *testing.T) {
	cfg := setupTestDB(t)
	svc := NewAuthService(cfg)

	user, err := svc.Register("alice", "password123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if user == nil {
		t.Fatal("Register() returned nil user")
	}
	if user.ID == 0 {
		t.Error("expected user.ID to be assigned")
	}
	if user.Username != "alice" {
		t.Errorf("Username = %q, want alice", user.Username)
	}
	if user.StorageQuota != cfg.Storage.DefaultQuota {
		t.Errorf("StorageQuota = %d, want %d", user.StorageQuota, cfg.Storage.DefaultQuota)
	}
	if !user.IsAdmin {
		t.Error("first registered user should be admin")
	}
	if user.PasswordHash == "" || user.PasswordHash == "password123" {
		t.Error("password should be bcrypt-hashed, not plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("password123")); err != nil {
		t.Errorf("stored hash does not match password: %v", err)
	}

	// 第二个用户不应为管理员
	user2, err := svc.Register("bob", "password123")
	if err != nil {
		t.Fatalf("second Register() error = %v", err)
	}
	if user2.IsAdmin {
		t.Error("second registered user should not be admin")
	}
}

func TestAuthService_Register_DuplicateUsername(t *testing.T) {
	cfg := setupTestDB(t)
	svc := NewAuthService(cfg)

	if _, err := svc.Register("bob", "password123"); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	_, err := svc.Register("bob", "otherpass")
	if err == nil {
		t.Fatal("Register() expected error for duplicate username, got nil")
	}
	if !strings.Contains(err.Error(), "创建用户失败") {
		t.Errorf("error = %q, want substring 创建用户失败", err.Error())
	}
}

func TestAuthService_Register_Disabled(t *testing.T) {
	cfg := setupTestDB(t)
	svc := NewAuthService(cfg)

	// 先注册首个管理员
	if _, err := svc.Register("admin", "password123"); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if err := model.SetAllowRegister(false); err != nil {
		t.Fatalf("SetAllowRegister: %v", err)
	}

	_, err := svc.Register("newbie", "password123")
	if err == nil {
		t.Fatal("Register() expected error when disabled")
	}
	if !strings.Contains(err.Error(), "当前未开放注册") {
		t.Errorf("error = %q, want 当前未开放注册", err.Error())
	}
}

func TestAuthService_Login_Success(t *testing.T) {
	cfg := setupTestDB(t)
	svc := NewAuthService(cfg)

	created, err := svc.Register("carol", "secret999")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	user, err := svc.Login("carol", "secret999")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if user.ID != created.ID {
		t.Errorf("user.ID = %d, want %d", user.ID, created.ID)
	}
	if user.Username != "carol" {
		t.Errorf("Username = %q, want carol", user.Username)
	}
}

func TestAuthService_Login_WrongPassword(t *testing.T) {
	cfg := setupTestDB(t)
	svc := NewAuthService(cfg)

	if _, err := svc.Register("dave", "correct-pass"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := svc.Login("dave", "wrong-pass")
	if err == nil {
		t.Fatal("Login() expected error for wrong password, got nil")
	}
	if err.Error() != "用户名或密码错误" {
		t.Errorf("error = %q, want 用户名或密码错误", err.Error())
	}
}

func TestAuthService_Login_UserNotFound(t *testing.T) {
	cfg := setupTestDB(t)
	svc := NewAuthService(cfg)

	_, err := svc.Login("nobody", "whatever")
	if err == nil {
		t.Fatal("Login() expected error for missing user, got nil")
	}
	if err.Error() != "用户名或密码错误" {
		t.Errorf("error = %q, want 用户名或密码错误", err.Error())
	}
}
