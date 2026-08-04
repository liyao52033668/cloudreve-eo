package config

import (
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.DB.Driver != "sqlite" {
		t.Errorf("DB.Driver = %q, want %q", cfg.DB.Driver, "sqlite")
	}
	if cfg.DB.DSN != "cloudreve.db" {
		t.Errorf("DB.DSN = %q, want %q", cfg.DB.DSN, "cloudreve.db")
	}
	if cfg.Server.Port != "8080" {
		t.Errorf("Server.Port = %q, want %q", cfg.Server.Port, "8080")
	}
}

func TestLoad_CustomValues(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DB_DRIVER", "postgres")
	t.Setenv("DB_DSN", "host=127.0.0.1 dbname=cloudreve")
	t.Setenv("PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.DB.Driver != "postgres" {
		t.Errorf("DB.Driver = %q, want postgres", cfg.DB.Driver)
	}
	if cfg.DB.DSN != "host=127.0.0.1 dbname=cloudreve" {
		t.Errorf("DB.DSN = %q", cfg.DB.DSN)
	}
	if cfg.Server.Port != "9090" {
		t.Errorf("Server.Port = %q, want 9090", cfg.Server.Port)
	}
}

func TestLoad_AllEnvVarsRead(t *testing.T) {
	type fieldCheck struct {
		env   string
		value string
		get   func(*Config) string
	}

	checks := []fieldCheck{
		{"DB_DRIVER", "postgres", func(c *Config) string { return c.DB.Driver }},
		{"DB_DSN", "host=localhost dbname=test", func(c *Config) string { return c.DB.DSN }},
		{"PORT", "3000", func(c *Config) string { return c.Server.Port }},
	}

	for _, tc := range checks {
		t.Run(tc.env, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(tc.env, tc.value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			got := tc.get(cfg)
			if got != tc.value {
				t.Errorf("after setting %s=%q: got %q", tc.env, tc.value, got)
			}
		})
	}
}

func TestLoad_IgnoresBusinessEnv(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("JWT_SECRET", "should-be-ignored")
	t.Setenv("S3_BUCKET", "should-be-ignored")
	t.Setenv("DEFAULT_QUOTA", "999")
	t.Setenv("S3_POLICIES", `[{"name":"x"}]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.DB.Driver != "sqlite" {
		t.Errorf("DB.Driver = %q", cfg.DB.Driver)
	}
	if cfg.Server.Port != "8080" {
		t.Errorf("Port = %q", cfg.Server.Port)
	}
}

func TestLoad_PersistEdgeOneBlob(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DB_PERSIST", "edgeone-blob")

	// 缺 SECRET 必须报错
	if _, err := Load(); err == nil {
		t.Fatalf("缺少 DB_PERSIST_EDGEONE_SECRET 时应报错")
	}

	// 仅配 SECRET：BASE_URL 留空，进入懒恢复模式（从首个请求推导）
	t.Setenv("DB_PERSIST_EDGEONE_SECRET", "topsecret")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if !cfg.LazyRestore() {
		t.Errorf("未配置 BASE_URL 时 LazyRestore() = false, want true")
	}

	// 显式配置 BASE_URL：去除末尾斜杠，启动即恢复
	t.Setenv("DB_PERSIST_EDGEONE_BASE_URL", "https://demo.edgeone.run/")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.LazyRestore() {
		t.Errorf("已配置 BASE_URL 时 LazyRestore() = true, want false")
	}
	if cfg.Persist.EdgeOne.BaseURL != "https://demo.edgeone.run" {
		t.Errorf("BaseURL = %q, 末尾斜杠应被去除", cfg.Persist.EdgeOne.BaseURL)
	}
	if cfg.Persist.EdgeOne.Secret != "topsecret" {
		t.Errorf("Secret = %q", cfg.Persist.EdgeOne.Secret)
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"DB_DRIVER", "DB_DSN", "PORT",
		"DB_PERSIST", "DB_PERSIST_INTERVAL",
		"DB_PERSIST_S3_ENDPOINT", "DB_PERSIST_S3_REGION", "DB_PERSIST_S3_BUCKET",
		"DB_PERSIST_S3_ACCESS_KEY", "DB_PERSIST_S3_SECRET_KEY", "DB_PERSIST_S3_KEY", "DB_PERSIST_S3_PATH_STYLE",
		"DB_PERSIST_GITHUB_TOKEN", "DB_PERSIST_GITHUB_REPO", "DB_PERSIST_GITHUB_BRANCH", "DB_PERSIST_GITHUB_PATH",
		"DB_PERSIST_EDGEONE_BASE_URL", "DB_PERSIST_EDGEONE_SECRET",
		"DB_PERSIST_EDGEONE_STORE", "DB_PERSIST_EDGEONE_KEY",
		"JWT_SECRET", "DEFAULT_QUOTA", "DEFAULT_STORAGE",
		"S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_POLICIES",
		"EDGEONE_BUCKET", "EDGEONE_SECRET_ID", "EDGEONE_SECRET_KEY",
	} {
		t.Setenv(v, "")
	}
}
