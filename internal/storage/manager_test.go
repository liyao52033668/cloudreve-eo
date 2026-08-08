package storage

import (
	"net/url"
	"strings"
	"testing"
	"time"
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

func TestSignVerifyProxyURL(t *testing.T) {
	mgr := NewTestStoragePolicyManager("filen", &mockDriver{})
	mgr.SetProxySigner(func() string { return "test-secret" }, "/api/files/proxy")

	proxyURL, err := mgr.SignProxyURL("filen", "1/abc.jpg", "照片.jpg", time.Minute)
	if err != nil {
		t.Fatalf("SignProxyURL: %v", err)
	}
	if !strings.Contains(proxyURL, "/api/files/proxy?") {
		t.Errorf("URL 缺少基础地址: %s", proxyURL)
	}
	// 解析 query
	q := proxyURL[strings.Index(proxyURL, "?")+1:]
	vals := map[string]string{}
	for _, kv := range strings.Split(q, "&") {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			got, _ := url.QueryUnescape(parts[1])
			vals[parts[0]] = got
		}
	}

	// 合法签名通过
	if err := mgr.VerifyProxyURL(vals["policy"], vals["key"], vals["name"], vals["exp"], vals["sig"]); err != nil {
		t.Errorf("VerifyProxyURL 合法签名被拒: %v", err)
	}
	// 篡改 key 应失败
	if err := mgr.VerifyProxyURL(vals["policy"], "2/tampered.jpg", vals["name"], vals["exp"], vals["sig"]); err == nil {
		t.Error("VerifyProxyURL 未检出篡改的 key")
	}
	// 篡改文件名应失败
	if err := mgr.VerifyProxyURL(vals["policy"], vals["key"], "evil.jpg", vals["exp"], vals["sig"]); err == nil {
		t.Error("VerifyProxyURL 未检出篡改的文件名")
	}
	// 篡改签名应失败
	if err := mgr.VerifyProxyURL(vals["policy"], vals["key"], vals["name"], vals["exp"], "deadbeef"); err == nil {
		t.Error("VerifyProxyURL 未检出伪造签名")
	}
	// 过期应失败
	if err := mgr.VerifyProxyURL(vals["policy"], vals["key"], vals["name"], "1", vals["sig"]); err == nil {
		t.Error("VerifyProxyURL 未检出过期链接")
	}
}

