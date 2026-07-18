package storage

import (
	"strings"
	"testing"
)

func TestNewTestStoragePolicyManager(t *testing.T) {
	mock := &mockDriver{}
	mgr := NewTestStoragePolicyManager("s3", mock)
	if mgr.DefaultPolicy() != "s3" {
		t.Errorf("DefaultPolicy = %q", mgr.DefaultPolicy())
	}
	if mgr.DefaultDriver() != mock {
		t.Error("DefaultDriver mismatch")
	}
	driver, err := mgr.GetDriver("s3")
	if err != nil || driver != mock {
		t.Fatalf("GetDriver: %v", err)
	}
	_, err = mgr.GetDriver("missing")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected missing policy error, got %v", err)
	}
}

func TestStoragePolicyManager_EmptyHasNoDefault(t *testing.T) {
	mgr := &StoragePolicyManager{
		drivers: make(map[string]StorageDriver),
		infos:   make(map[string]PolicyInfo),
	}
	if mgr.DefaultPolicy() != "" {
		t.Errorf("empty default = %q", mgr.DefaultPolicy())
	}
	_, err := mgr.ResolvePolicy("")
	if err == nil || !strings.Contains(err.Error(), "未配置") {
		t.Errorf("ResolvePolicy empty mgr error = %v", err)
	}
	if len(mgr.ListPolicies()) != 0 {
		t.Error("ListPolicies should be empty")
	}
}

func TestNewTestStoragePolicyManagerMulti(t *testing.T) {
	a := &mockDriver{}
	b := &mockDriver{}
	mgr := NewTestStoragePolicyManagerMulti("cos", map[string]StorageDriver{
		"minio": a,
		"cos":   b,
	})
	if mgr.DefaultPolicy() != "cos" {
		t.Errorf("DefaultPolicy = %q", mgr.DefaultPolicy())
	}
	list := mgr.ListPolicies()
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	if !list[0].IsDefault || list[0].Name != "cos" {
		t.Errorf("first = %+v", list[0])
	}
}

func TestStoragePolicyManager_PoliciesAreIndependent(t *testing.T) {
	a := &mockDriver{}
	b := &mockDriver{}
	mgr := NewTestStoragePolicyManagerMulti("a", map[string]StorageDriver{
		"a": a,
		"b": b,
	})
	dA, err := mgr.GetDriver("a")
	if err != nil {
		t.Fatalf("driver a err: %v", err)
	}
	if dA != a {
		t.Fatalf("driver a mismatch: got %p want %p", dA, a)
	}
	dB, err := mgr.GetDriver("b")
	if err != nil {
		t.Fatalf("driver b err: %v", err)
	}
	if dB != b {
		t.Fatalf("driver b mismatch: got %p want %p", dB, b)
	}
	// 解析空策略只回默认，不影响另一套
	name, err := mgr.ResolvePolicy("")
	if err != nil || name != "a" {
		t.Fatalf("ResolvePolicy default = %q %v", name, err)
	}
	name, err = mgr.ResolvePolicy("b")
	if err != nil || name != "b" {
		t.Fatalf("ResolvePolicy b = %q %v", name, err)
	}
}
