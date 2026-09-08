package storage

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWebDAVDriver_DirectDownloadURL_302Redirect(t *testing.T) {
	// 模拟 WebDAV 服务器返回 302 重定向（如 Cloudreve 行为）
	directLink := "https://dl.cdn.ad/uploads/487/lsNTk4BL?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc123"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证 Basic Auth
		user, pass, ok := r.BasicAuth()
		if !ok || user != "testuser" || pass != "testpass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// 返回 302 重定向到直链
		w.Header().Set("Location", directLink)
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	d, err := NewWebDAVDriver(server.URL, "testuser", "testpass", "cloudreve", "", false, "")
	if err != nil {
		t.Fatal(err)
	}

	// 注入代理 URL 生成器（不应被调用）
	d.proxyURL = func(storageKey, attachment string) (string, error) {
		t.Error("302 重定向时不应调用 proxyURL")
		return "", nil
	}

	downloadURL, err := d.GenerateDownloadURL("user123/file.txt", "file.txt", 0)
	if err != nil {
		t.Fatal("GenerateDownloadURL 不应返回错误:", err)
	}
	if downloadURL != directLink {
		t.Errorf("应返回 Location 头的直链，期望: %s，实际: %s", directLink, downloadURL)
	}
}

func TestWebDAVDriver_DirectDownloadURL_200Fallback(t *testing.T) {
	// 模拟 WebDAV 服务器直接返回文件（无重定向），应返回 Basic Auth 直连 URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("file content"))
	}))
	defer server.Close()

	d, err := NewWebDAVDriver(server.URL, "testuser", "testpass", "cloudreve", "", false, "")
	if err != nil {
		t.Fatal(err)
	}

	downloadURL, err := d.GenerateDownloadURL("user123/file.txt", "file.txt", 0)
	if err != nil {
		t.Fatal("GenerateDownloadURL 不应返回错误:", err)
	}
	// 应返回内嵌 Basic Auth 的直连 URL
	u, err := url.Parse(downloadURL)
	if err != nil {
		t.Fatal("下载 URL 应可解析:", err)
	}
	if u.User == nil || u.User.Username() != "testuser" {
		t.Errorf("下载 URL 应内嵌用户名，实际: %s", downloadURL)
	}
	if pass, _ := u.User.Password(); pass != "testpass" {
		t.Errorf("下载 URL 应内嵌密码，实际: %s", downloadURL)
	}
	if !strings.Contains(u.Path, "/user123/file.txt") {
		t.Errorf("下载 URL 路径应包含文件路径，实际: %s", u.Path)
	}
}

func TestWebDAVDriver_DirectDownloadURL(t *testing.T) {
	// 测试 WebDAV 返回 302 重定向时，应返回 Location 头的直链
	d, err := NewWebDAVDriver("https://dav.example.com", "user", "pass", "cloudreve", "", false, "")
	if err != nil {
		t.Fatal(err)
	}

	// 注入代理 URL 生成器（不应被调用，现在始终返回直连 URL）
	proxyCalled := false
	d.proxyURL = func(storageKey, attachment string) (string, error) {
		proxyCalled = true
		return "/api/files/proxy?policy=test&key=" + storageKey, nil
	}

	// 上传 URL 应返回错误（走服务端中转）
	_, err = d.GenerateUploadURL("user123/file.txt", "text/plain", 0)
	if err == nil {
		t.Error("中转模式 GenerateUploadURL 应返回错误")
	}

	// HEAD 请求失败（无真实服务器），回退到 Basic Auth 直连 URL
	downloadURL, err := d.GenerateDownloadURL("user123/file.txt", "file.txt", 0)
	if err != nil {
		t.Fatal("GenerateDownloadURL 不应返回错误:", err)
	}
	if proxyCalled {
		t.Error("不应调用 proxyURL，应直接返回 Basic Auth URL")
	}
	u, err := url.Parse(downloadURL)
	if err != nil {
		t.Fatal("下载 URL 应可解析:", err)
	}
	if u.User == nil || u.User.Username() != "user" {
		t.Errorf("下载 URL 应内嵌用户名，实际: %s", downloadURL)
	}
}

func TestWebDAVDriver_DirectDownloadURLCustomHost(t *testing.T) {
	// 配置 customHost 时，应使用自定义域名构造直连 URL
	d, err := NewWebDAVDriver("https://dav.example.com", "user", "pass", "cloudreve", "https://cdn.example.com/dav", false, "")
	if err != nil {
		t.Fatal(err)
	}

	// HEAD 请求失败（无真实服务器），回退到 Basic Auth 直连 URL
	downloadURL, err := d.GenerateDownloadURL("user123/file.txt", "", 0)
	if err != nil {
		t.Fatal("GenerateDownloadURL 不应返回错误:", err)
	}
	u, err := url.Parse(downloadURL)
	if err != nil {
		t.Fatal("下载 URL 应可解析:", err)
	}
	if u.Host != "cdn.example.com" {
		t.Errorf("下载 URL 主机应为 cdn.example.com，实际: %s", u.Host)
	}
	if u.Path != "/dav/user123/file.txt" {
		t.Errorf("下载 URL 路径应为 /dav/user123/file.txt，实际: %s", u.Path)
	}
}

func TestWebDAVDriver_NewValidation(t *testing.T) {
	// 测试组件验证
	_, err := NewWebDAVDriver("", "user", "pass", "cloudreve", "", false, "")
	if err == nil {
		t.Error("空 serverURL 应返回错误")
	}

	_, err = NewWebDAVDriver("https://dav.example.com", "", "pass", "cloudreve", "", false, "")
	if err == nil {
		t.Error("空 username 应返回错误")
	}

	_, err = NewWebDAVDriver("https://dav.example.com", "user", "", "cloudreve", "", false, "")
	if err == nil {
		t.Error("空 password 应返回错误")
	}

	// 正常初始化
	d, err := NewWebDAVDriver("https://dav.example.com", "user", "pass", "cloudreve", "", false, "")
	if err != nil {
		t.Fatal("正常初始化不应返回错误:", err)
	}
	if !d.IsConfigured() {
		t.Error("IsConfigured 应返回 true")
	}

	// 验证 basePath 不再由驱动使用（已由 buildStorageKey 拼入 key）
	if d.basePath != "" {
		t.Errorf("basePath 应为空（驱动不再使用），实际: %s", d.basePath)
	}

	// 验证 serverURL 末尾斜杠被清理
	if d.serverURL != "https://dav.example.com" {
		t.Errorf("serverURL 不应包含末尾斜杠，实际: %s", d.serverURL)
	}
}
