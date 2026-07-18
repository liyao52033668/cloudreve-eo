package storage

import (
	"strings"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

func testS3Config() *config.Config {
	return &config.Config{
		Storage: config.StorageConfig{
			Default:      "s3",
			DefaultQuota: 1073741824,
		},
		S3: config.S3Config{
			Endpoint:  "http://localhost:9000",
			Region:    "us-east-1",
			Bucket:    "test-bucket",
			AccessKey: "minioadmin",
			SecretKey: "minioadmin",
		},
	}
}

func TestNewStoragePolicyManager_CreatesS3Driver(t *testing.T) {
	mgr, err := NewStoragePolicyManager(testS3Config())
	if err != nil {
		t.Fatalf("NewStoragePolicyManager unexpected error: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager is nil")
	}
	if mgr.DefaultPolicy() != "s3" {
		t.Errorf("DefaultPolicy = %q, want %q", mgr.DefaultPolicy(), "s3")
	}
	if mgr.DefaultDriver() == nil {
		t.Fatal("DefaultDriver is nil")
	}

	driver, err := mgr.GetDriver("s3")
	if err != nil {
		t.Fatalf("GetDriver(s3) error: %v", err)
	}
	if driver == nil {
		t.Fatal("GetDriver(s3) returned nil")
	}
	if driver != mgr.DefaultDriver() {
		t.Error("GetDriver(s3) should return the same instance as DefaultDriver")
	}
}

func TestStoragePolicyManager_GetDriver_NotFound(t *testing.T) {
	mgr, err := NewStoragePolicyManager(testS3Config())
	if err != nil {
		t.Fatalf("NewStoragePolicyManager: %v", err)
	}

	_, err = mgr.GetDriver("nonexistent")
	if err == nil {
		t.Fatal("GetDriver(nonexistent) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error %q should mention policy name", err.Error())
	}
}

func TestNewStoragePolicyManager_MissingDefaultPolicy(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Default: "edgeone",
		},
		// 未配置任何 S3 策略
	}

	_, err := NewStoragePolicyManager(cfg)
	if err == nil {
		t.Fatal("expected error when default policy is not available")
	}
	if !strings.Contains(err.Error(), "S3") && !strings.Contains(err.Error(), "edgeone") {
		t.Errorf("error %q should mention missing storage", err.Error())
	}
}

func TestNewStoragePolicyManager_DefaultNotInList(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Default: "missing"},
		S3List: []config.S3Config{
			{Name: "minio", Endpoint: "http://localhost:9001", Region: "us-east-1", Bucket: "a", AccessKey: "ak", SecretKey: "sk"},
		},
	}
	_, err := NewStoragePolicyManager(cfg)
	if err == nil {
		t.Fatal("expected error when default policy not registered")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q should mention default policy name", err.Error())
	}
}
