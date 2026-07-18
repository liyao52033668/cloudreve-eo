package model

import (
	"path/filepath"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

func setupPolicyDB(t *testing.T) {
	t.Helper()
	DB = nil
	t.Cleanup(func() { DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "policy.db"),
		},
	}
	if err := InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
}

func TestCreateStoragePolicy_FirstIsDefault(t *testing.T) {
	setupPolicyDB(t)

	p := &StoragePolicy{
		Name: "minio", Type: "s3", Endpoint: "http://127.0.0.1:9000",
		Region: "us-east-1", Bucket: "b", AccessKey: "ak", SecretKey: "sk",
	}
	if err := CreateStoragePolicy(p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !p.IsDefault {
		t.Error("first policy should be default")
	}
	if p.ID == 0 {
		t.Error("expected ID assigned")
	}
}

func TestSetDefaultAndDelete(t *testing.T) {
	setupPolicyDB(t)

	a := &StoragePolicy{Name: "a", Type: "s3", Endpoint: "http://a", Bucket: "ba", AccessKey: "ak", SecretKey: "sk", IsDefault: true}
	b := &StoragePolicy{Name: "b", Type: "s3", Endpoint: "http://b", Bucket: "bb", AccessKey: "ak", SecretKey: "sk"}
	if err := CreateStoragePolicy(a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := CreateStoragePolicy(b); err != nil {
		t.Fatalf("create b: %v", err)
	}

	if err := SetDefaultStoragePolicy(b.ID); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	got, err := GetStoragePolicyByID(b.ID)
	if err != nil || !got.IsDefault {
		t.Fatalf("b should be default: err=%v default=%v", err, got != nil && got.IsDefault)
	}
	gotA, _ := GetStoragePolicyByID(a.ID)
	if gotA.IsDefault {
		t.Error("a should no longer be default")
	}

	if err := DeleteStoragePolicy(b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// 删除默认后 a 应被提升
	gotA, err = GetStoragePolicyByID(a.ID)
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if !gotA.IsDefault {
		t.Error("remaining policy should become default")
	}
}

func TestStoragePolicy_DefaultQuotaPerPolicy(t *testing.T) {
	setupPolicyDB(t)

	a := &StoragePolicy{
		Name: "a", Type: "s3", Endpoint: "http://a", Bucket: "ba",
		AccessKey: "ak", SecretKey: "sk", DefaultQuota: 1024,
	}
	b := &StoragePolicy{
		Name: "b", Type: "s3", Endpoint: "http://b", Bucket: "bb",
		AccessKey: "ak", SecretKey: "sk", DefaultQuota: 2048,
	}
	if err := CreateStoragePolicy(a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := CreateStoragePolicy(b); err != nil {
		t.Fatalf("create b: %v", err)
	}
	gotA, err := GetStoragePolicyByID(a.ID)
	if err != nil || gotA.DefaultQuota != 1024 {
		t.Fatalf("a quota = %v err=%v", gotA, err)
	}
	gotB, err := GetStoragePolicyByID(b.ID)
	if err != nil || gotB.DefaultQuota != 2048 {
		t.Fatalf("b quota = %v err=%v", gotB, err)
	}

	b.DefaultQuota = 4096
	if err := UpdateStoragePolicy(b.ID, b, false); err != nil {
		t.Fatalf("update b: %v", err)
	}
	gotB, err = GetStoragePolicyByID(b.ID)
	if err != nil || gotB.DefaultQuota != 4096 {
		t.Fatalf("b updated quota = %v err=%v", gotB, err)
	}
	// a 不受影响
	gotA, _ = GetStoragePolicyByID(a.ID)
	if gotA.DefaultQuota != 1024 {
		t.Errorf("a quota changed to %d", gotA.DefaultQuota)
	}
}


func TestStoragePolicy_ForcePathStyleAndBasePath(t *testing.T) {
	setupPolicyDB(t)

	p := &StoragePolicy{
		Name: "minio", Type: "s3", Endpoint: "http://127.0.0.1:9000",
		Region: "us-east-1", Bucket: "b", AccessKey: "ak", SecretKey: "sk",
		ForcePathStyle: true, BasePath: "uploads/prod", DefaultQuota: 1024,
	}
	if err := CreateStoragePolicy(p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := GetStoragePolicyByID(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.ForcePathStyle {
		t.Error("ForcePathStyle should be true")
	}
	if got.BasePath != "uploads/prod" {
		t.Errorf("BasePath = %q", got.BasePath)
	}

	upd := &StoragePolicy{
		Name: "minio", Endpoint: got.Endpoint, Region: got.Region, Bucket: got.Bucket,
		AccessKey: got.AccessKey, ForcePathStyle: false, BasePath: "data", DefaultQuota: 1024,
	}
	if err := UpdateStoragePolicy(p.ID, upd, false); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = GetStoragePolicyByID(p.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.ForcePathStyle {
		t.Error("ForcePathStyle should be false after update")
	}
	if got.BasePath != "data" {
		t.Errorf("BasePath after update = %q", got.BasePath)
	}
}
