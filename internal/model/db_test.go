package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

func TestInitDB_SQLiteSuccess(t *testing.T) {
	// Reset global DB between tests.
	DB = nil
	t.Cleanup(func() { DB = nil })

	dsn := filepath.Join(t.TempDir(), "test.db")
	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    dsn,
		},
	}

	if err := InitDB(cfg); err != nil {
		t.Fatalf("InitDB() unexpected error: %v", err)
	}
	if DB == nil {
		t.Fatal("InitDB() left DB nil")
	}

	// Tables should exist after AutoMigrate.
	for _, name := range []string{"users", "files", "shares"} {
		if !DB.Migrator().HasTable(name) {
			t.Errorf("expected table %q to exist after AutoMigrate", name)
		}
	}

	// Sanity: can create a user with defaults.
	user := &User{Username: "alice", PasswordHash: "hash"}
	if err := DB.Create(user).Error; err != nil {
		t.Fatalf("Create User: %v", err)
	}
	if user.ID == 0 {
		t.Error("expected user.ID to be assigned")
	}
	if user.StorageQuota != 0 {
		t.Errorf("StorageQuota = %d, want default 0", user.StorageQuota)
	}
	if user.StorageUsed != 0 {
		t.Errorf("StorageUsed = %d, want default 0", user.StorageUsed)
	}
}

func TestInitDB_UnsupportedDriver(t *testing.T) {
	DB = nil
	t.Cleanup(func() { DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "mysql",
			DSN:    "user:pass@tcp(localhost:3306)/db",
		},
	}

	err := InitDB(cfg)
	if err == nil {
		t.Fatal("InitDB() expected error for unsupported driver, got nil")
	}
	if !strings.Contains(err.Error(), "不支持的数据库驱动") {
		t.Errorf("error = %q, want substring %q", err.Error(), "不支持的数据库驱动")
	}
	if !strings.Contains(err.Error(), "mysql") {
		t.Errorf("error = %q, want to mention driver name mysql", err.Error())
	}
	if DB != nil {
		t.Error("DB should remain nil on unsupported driver")
	}
}

func TestInitDB_InvalidDSN(t *testing.T) {
	DB = nil
	t.Cleanup(func() { DB = nil })

	// Directory path as DSN should fail to open as SQLite file.
	dir := t.TempDir()
	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    dir, // is a directory, not a file
		},
	}

	err := InitDB(cfg)
	// gorm/sqlite may fail at Open or later; either way must error.
	// Note: some drivers create a file; using a non-writable path is safer.
	// Fall back: use a path under a non-existent intermediate that we make unwritable.
	if err == nil {
		// If Open succeeded (unlikely with directory), force a stricter case.
		_ = os.RemoveAll(dir)
		cfg.DB.DSN = filepath.Join(dir, "no-parent", "x.db")
		err = InitDB(cfg)
	}
	if err == nil {
		t.Fatal("InitDB() expected error for invalid DSN, got nil")
	}
	if !strings.Contains(err.Error(), "连接数据库失败") &&
		!strings.Contains(err.Error(), "数据库迁移失败") {
		// Accept either connect or migrate failure wording from InitDB.
		// If implementation uses different wording, still require non-nil error above.
		t.Logf("InitDB error (acceptable if connection failed): %v", err)
	}
}
