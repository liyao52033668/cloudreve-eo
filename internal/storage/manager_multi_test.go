package storage

import (
	"strings"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

func TestNewStoragePolicyManager_MultipleS3(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Default: "cos"},
		S3List: []config.S3Config{
			{Name: "minio", Endpoint: "http://localhost:9001", Region: "us-east-1", Bucket: "a", AccessKey: "ak", SecretKey: "sk"},
			{Name: "cos", Endpoint: "https://cos.example.com", Region: "ap-guangzhou", Bucket: "b", AccessKey: "ak", SecretKey: "sk"},
		},
	}
	mgr, err := NewStoragePolicyManager(cfg)
	if err != nil {
		t.Fatalf("NewStoragePolicyManager: %v", err)
	}
	if mgr.DefaultPolicy() != "cos" {
		t.Errorf("DefaultPolicy = %q", mgr.DefaultPolicy())
	}
	if _, err := mgr.GetDriver("minio"); err != nil {
		t.Errorf("GetDriver minio: %v", err)
	}
	if _, err := mgr.GetDriver("cos"); err != nil {
		t.Errorf("GetDriver cos: %v", err)
	}

	list := mgr.ListPolicies()
	if len(list) != 2 {
		t.Fatalf("ListPolicies len = %d", len(list))
	}
	if !list[0].IsDefault || list[0].Name != "cos" {
		t.Errorf("first policy should be default cos, got %+v", list[0])
	}
}

func TestResolvePolicy(t *testing.T) {
	mockA := &mockDriver{}
	mockB := &mockDriver{}
	mgr := NewTestStoragePolicyManagerMulti("a", map[string]StorageDriver{
		"a": mockA,
		"b": mockB,
	})

	got, err := mgr.ResolvePolicy("")
	if err != nil || got != "a" {
		t.Fatalf("empty → default: got %q err %v", got, err)
	}
	got, err = mgr.ResolvePolicy("b")
	if err != nil || got != "b" {
		t.Fatalf("b: got %q err %v", got, err)
	}
	_, err = mgr.ResolvePolicy("missing")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing error, got %v", err)
	}
}
