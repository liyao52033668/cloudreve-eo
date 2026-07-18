package model

import (
	"path/filepath"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

func setupSettingDB(t *testing.T) {
	t.Helper()
	DB = nil
	t.Cleanup(func() { DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "setting.db"),
		},
	}
	if err := InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
}

func TestEnsureJWTSecret_AutoGenerate(t *testing.T) {
	setupSettingDB(t)

	secret, err := EnsureJWTSecret("")
	if err != nil {
		t.Fatalf("EnsureJWTSecret() error = %v", err)
	}
	if secret == "" {
		t.Fatal("expected non-empty auto-generated secret")
	}

	// 再次调用应返回同一密钥
	again, err := EnsureJWTSecret("ignored-env")
	if err != nil {
		t.Fatalf("second EnsureJWTSecret() error = %v", err)
	}
	if again != secret {
		t.Errorf("secret changed: first=%q second=%q", secret, again)
	}
}

func TestEnsureJWTSecret_BootstrapFromEnv(t *testing.T) {
	setupSettingDB(t)

	const envSecret = "bootstrap-from-env-secret"
	secret, err := EnsureJWTSecret(envSecret)
	if err != nil {
		t.Fatalf("EnsureJWTSecret() error = %v", err)
	}
	if secret != envSecret {
		t.Errorf("secret = %q, want %q", secret, envSecret)
	}
}

func TestRotateJWTSecret(t *testing.T) {
	setupSettingDB(t)

	old, err := EnsureJWTSecret("old-secret")
	if err != nil {
		t.Fatalf("EnsureJWTSecret() error = %v", err)
	}

	next, err := RotateJWTSecret()
	if err != nil {
		t.Fatalf("RotateJWTSecret() error = %v", err)
	}
	if next == "" || next == old {
		t.Errorf("rotated secret should differ; old=%q new=%q", old, next)
	}

	stored, err := GetSetting(SettingKeyJWTSecret)
	if err != nil {
		t.Fatalf("GetSetting() error = %v", err)
	}
	if stored != next {
		t.Errorf("stored = %q, want %q", stored, next)
	}
}

func TestIsRegisterAllowed_DefaultAndToggle(t *testing.T) {
	setupSettingDB(t)

	// 无用户、无设置 → 允许
	ok, err := IsRegisterAllowed()
	if err != nil {
		t.Fatalf("IsRegisterAllowed() error = %v", err)
	}
	if !ok {
		t.Error("expected allow when empty system")
	}

	// 写入用户后，无设置仍默认允许
	if err := DB.Create(&User{Username: "admin", PasswordHash: "h", IsAdmin: true}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	ok, err = IsRegisterAllowed()
	if err != nil {
		t.Fatalf("IsRegisterAllowed() error = %v", err)
	}
	if !ok {
		t.Error("expected default true when setting missing")
	}

	// 关闭
	if err := SetAllowRegister(false); err != nil {
		t.Fatalf("SetAllowRegister(false): %v", err)
	}
	ok, err = IsRegisterAllowed()
	if err != nil {
		t.Fatalf("IsRegisterAllowed() error = %v", err)
	}
	if ok {
		t.Error("expected false after disable")
	}

	// 再开启
	if err := SetAllowRegister(true); err != nil {
		t.Fatalf("SetAllowRegister(true): %v", err)
	}
	ok, err = IsRegisterAllowed()
	if err != nil {
		t.Fatalf("IsRegisterAllowed() error = %v", err)
	}
	if !ok {
		t.Error("expected true after enable")
	}
}
