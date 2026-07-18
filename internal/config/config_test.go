package config

import (
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear all relevant env vars so defaults apply.
	// t.Setenv with empty string still sets the var; unset by restoring
	// to empty and relying on Load treating "" as "use default" for
	// vars that have defaults. For no-default vars, empty is correct.
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
	if cfg.JWT.Secret != "" {
		t.Errorf("JWT.Secret = %q, want empty", cfg.JWT.Secret)
	}
	if cfg.Storage.Default != "s3" {
		t.Errorf("Storage.Default = %q, want %q", cfg.Storage.Default, "s3")
	}
	if cfg.S3.Endpoint != "" {
		t.Errorf("S3.Endpoint = %q, want empty", cfg.S3.Endpoint)
	}
	if cfg.S3.Region != "" {
		t.Errorf("S3.Region = %q, want empty", cfg.S3.Region)
	}
	if cfg.S3.Bucket != "" {
		t.Errorf("S3.Bucket = %q, want empty", cfg.S3.Bucket)
	}
	if cfg.S3.AccessKey != "" {
		t.Errorf("S3.AccessKey = %q, want empty", cfg.S3.AccessKey)
	}
	if cfg.S3.SecretKey != "" {
		t.Errorf("S3.SecretKey = %q, want empty", cfg.S3.SecretKey)
	}
	if cfg.EdgeOne.Bucket != "" {
		t.Errorf("EdgeOne.Bucket = %q, want empty", cfg.EdgeOne.Bucket)
	}
	if cfg.EdgeOne.SecretID != "" {
		t.Errorf("EdgeOne.SecretID = %q, want empty", cfg.EdgeOne.SecretID)
	}
	if cfg.EdgeOne.SecretKey != "" {
		t.Errorf("EdgeOne.SecretKey = %q, want empty", cfg.EdgeOne.SecretKey)
	}
	if cfg.Server.Port != "8080" {
		t.Errorf("Server.Port = %q, want %q", cfg.Server.Port, "8080")
	}
	if cfg.Storage.DefaultQuota != 1073741824 {
		t.Errorf("Storage.DefaultQuota = %d, want %d", cfg.Storage.DefaultQuota, 1073741824)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	t.Setenv("DB_DRIVER", "mysql")
	t.Setenv("DB_DSN", "user:pass@tcp(localhost:3306)/cloudreve")
	t.Setenv("JWT_SECRET", "super-secret-key")
	t.Setenv("DEFAULT_STORAGE", "local")
	t.Setenv("S3_ENDPOINT", "https://s3.example.com")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("S3_BUCKET", "my-bucket")
	t.Setenv("S3_ACCESS_KEY", "AKIAEXAMPLE")
	t.Setenv("S3_SECRET_KEY", "secret123")
	t.Setenv("EDGEONE_BUCKET", "eo-bucket")
	t.Setenv("EDGEONE_SECRET_ID", "eo-id")
	t.Setenv("EDGEONE_SECRET_KEY", "eo-key")
	t.Setenv("PORT", "9090")
	t.Setenv("DEFAULT_QUOTA", "2147483648")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.DB.Driver != "mysql" {
		t.Errorf("DB.Driver = %q, want %q", cfg.DB.Driver, "mysql")
	}
	if cfg.DB.DSN != "user:pass@tcp(localhost:3306)/cloudreve" {
		t.Errorf("DB.DSN = %q, want custom DSN", cfg.DB.DSN)
	}
	if cfg.JWT.Secret != "super-secret-key" {
		t.Errorf("JWT.Secret = %q, want %q", cfg.JWT.Secret, "super-secret-key")
	}
	if cfg.Storage.Default != "local" {
		t.Errorf("Storage.Default = %q, want %q", cfg.Storage.Default, "local")
	}
	if cfg.S3.Endpoint != "https://s3.example.com" {
		t.Errorf("S3.Endpoint = %q, want %q", cfg.S3.Endpoint, "https://s3.example.com")
	}
	if cfg.S3.Region != "us-east-1" {
		t.Errorf("S3.Region = %q, want %q", cfg.S3.Region, "us-east-1")
	}
	if cfg.S3.Bucket != "my-bucket" {
		t.Errorf("S3.Bucket = %q, want %q", cfg.S3.Bucket, "my-bucket")
	}
	if cfg.S3.AccessKey != "AKIAEXAMPLE" {
		t.Errorf("S3.AccessKey = %q, want %q", cfg.S3.AccessKey, "AKIAEXAMPLE")
	}
	if cfg.S3.SecretKey != "secret123" {
		t.Errorf("S3.SecretKey = %q, want %q", cfg.S3.SecretKey, "secret123")
	}
	if cfg.EdgeOne.Bucket != "eo-bucket" {
		t.Errorf("EdgeOne.Bucket = %q, want %q", cfg.EdgeOne.Bucket, "eo-bucket")
	}
	if cfg.EdgeOne.SecretID != "eo-id" {
		t.Errorf("EdgeOne.SecretID = %q, want %q", cfg.EdgeOne.SecretID, "eo-id")
	}
	if cfg.EdgeOne.SecretKey != "eo-key" {
		t.Errorf("EdgeOne.SecretKey = %q, want %q", cfg.EdgeOne.SecretKey, "eo-key")
	}
	if cfg.Server.Port != "9090" {
		t.Errorf("Server.Port = %q, want %q", cfg.Server.Port, "9090")
	}
	if cfg.Storage.DefaultQuota != 2147483648 {
		t.Errorf("Storage.DefaultQuota = %d, want %d", cfg.Storage.DefaultQuota, 2147483648)
	}
}

func TestLoad_InvalidDefaultQuota(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DEFAULT_QUOTA", "not-a-number")

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid DEFAULT_QUOTA, got nil")
	}
	if cfg != nil {
		t.Errorf("Load() expected nil config on error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "invalid DEFAULT_QUOTA") {
		t.Errorf("error = %q, want substring %q", err.Error(), "invalid DEFAULT_QUOTA")
	}
}

func TestLoad_EmptyDefaultQuotaUsesDefault(t *testing.T) {
	clearConfigEnv(t)
	// Explicit empty DEFAULT_QUOTA should fall back to 1GB default.
	t.Setenv("DEFAULT_QUOTA", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Storage.DefaultQuota != 1073741824 {
		t.Errorf("Storage.DefaultQuota = %d, want %d", cfg.Storage.DefaultQuota, 1073741824)
	}
}

func TestLoad_AllEnvVarsRead(t *testing.T) {
	// Table-driven check that each env var maps to the correct field.
	type fieldCheck struct {
		env   string
		value string
		get   func(*Config) string
	}

	checks := []fieldCheck{
		{"DB_DRIVER", "postgres", func(c *Config) string { return c.DB.Driver }},
		{"DB_DSN", "host=localhost dbname=test", func(c *Config) string { return c.DB.DSN }},
		{"JWT_SECRET", "jwt-test-secret", func(c *Config) string { return c.JWT.Secret }},
		{"DEFAULT_STORAGE", "edgeone", func(c *Config) string { return c.Storage.Default }},
		{"S3_ENDPOINT", "http://127.0.0.1:9000", func(c *Config) string { return c.S3.Endpoint }},
		{"S3_REGION", "ap-east-1", func(c *Config) string { return c.S3.Region }},
		{"S3_BUCKET", "test-bucket", func(c *Config) string { return c.S3.Bucket }},
		{"S3_ACCESS_KEY", "test-ak", func(c *Config) string { return c.S3.AccessKey }},
		{"S3_SECRET_KEY", "test-sk", func(c *Config) string { return c.S3.SecretKey }},
		{"EDGEONE_BUCKET", "test-eo-bucket", func(c *Config) string { return c.EdgeOne.Bucket }},
		{"EDGEONE_SECRET_ID", "test-eo-id", func(c *Config) string { return c.EdgeOne.SecretID }},
		{"EDGEONE_SECRET_KEY", "test-eo-sk", func(c *Config) string { return c.EdgeOne.SecretKey }},
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

	t.Run("DEFAULT_QUOTA", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("DEFAULT_QUOTA", "512")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() unexpected error: %v", err)
		}
		if cfg.Storage.DefaultQuota != 512 {
			t.Errorf("Storage.DefaultQuota = %d, want 512", cfg.Storage.DefaultQuota)
		}
	})
}

// clearConfigEnv resets all config-related env vars to empty so Load() uses defaults.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"DB_DRIVER",
		"DB_DSN",
		"JWT_SECRET",
		"DEFAULT_STORAGE",
		"S3_ENDPOINT",
		"S3_REGION",
		"S3_BUCKET",
		"S3_ACCESS_KEY",
		"S3_SECRET_KEY",
		"EDGEONE_BUCKET",
		"EDGEONE_SECRET_ID",
		"EDGEONE_SECRET_KEY",
		"S3_POLICIES",
		"PORT",
		"DEFAULT_QUOTA",
	}
	for _, v := range vars {
		t.Setenv(v, "")
	}
}
