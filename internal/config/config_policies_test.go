package config

import (
	"strings"
	"testing"
)

func TestLoad_S3PoliciesMulti(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DEFAULT_STORAGE", "cos")
	t.Setenv("S3_POLICIES", `[
		{"name":"minio","endpoint":"http://127.0.0.1:9001","region":"us-east-1","bucket":"a","access_key":"ak1","secret_key":"sk1"},
		{"name":"cos","endpoint":"https://cos.example.com","region":"ap-guangzhou","bucket":"b","access_key":"ak2","secret_key":"sk2"}
	]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.S3List) != 2 {
		t.Fatalf("S3List len = %d, want 2", len(cfg.S3List))
	}
	if cfg.S3List[0].Name != "minio" || cfg.S3List[1].Name != "cos" {
		t.Errorf("names = %q, %q", cfg.S3List[0].Name, cfg.S3List[1].Name)
	}
	if cfg.Storage.Default != "cos" {
		t.Errorf("Default = %q, want cos", cfg.Storage.Default)
	}
}

func TestLoad_S3PoliciesInvalidJSON(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("S3_POLICIES", `{not-json}`)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "S3_POLICIES") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestLoad_LegacySingleS3StillWorks(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("S3_BUCKET", "only-one")
	t.Setenv("S3_ENDPOINT", "http://localhost:9000")
	t.Setenv("S3_ACCESS_KEY", "ak")
	t.Setenv("S3_SECRET_KEY", "sk")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.S3List) != 1 {
		t.Fatalf("S3List len = %d, want 1", len(cfg.S3List))
	}
	if cfg.S3List[0].Name != "s3" || cfg.S3List[0].Bucket != "only-one" {
		t.Errorf("legacy policy = %+v", cfg.S3List[0])
	}
	if cfg.Storage.Default != "s3" {
		t.Errorf("Default = %q, want s3", cfg.Storage.Default)
	}
}
