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

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"DB_DRIVER", "DB_DSN", "PORT",
		"JWT_SECRET", "DEFAULT_QUOTA", "DEFAULT_STORAGE",
		"S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_POLICIES",
		"EDGEONE_BUCKET", "EDGEONE_SECRET_ID", "EDGEONE_SECRET_KEY",
	} {
		t.Setenv(v, "")
	}
}
