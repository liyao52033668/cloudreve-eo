package storage

import (
	"net/url"
	"strings"
	"testing"
)

func TestWebDAVDriver_ProxyMode(t *testing.T) {
	// 测试中转模式
	d, err := NewWebDAVDriver("https://dav.example.com", "user", "pass", "cloudreve", "")
	if err != nil {
		t.Fatal(err)
	}

	// 注入代理 URL 生成器
	d.proxyURL = func(storageKey, attachment string) (string, error) {
		return "/api/files/proxy?policy=test&key=" + url.QueryEscape(storageKey), nil
	}

	// 上传 URL 应返回错误（走服务端中转）
	_, err = d.GenerateUploadURL("user123/file.txt", "text/plain", 0)
	if err == nil {
		t.Error("中转模式 GenerateUploadURL 应返回错误")
	}

	// 下载 URL 应返回代理 URL
	downloadURL, err := d.GenerateDownloadURL("user123/file.txt", "file.txt", 0)
	if err != nil {
		t.Fatal("中转模式 GenerateDownloadURL 不应返回错误:", err)
	}
	if !strings.Contains(downloadURL, "/api/files/proxy") {
		t.Errorf("下载 URL 应为代理 URL，实际: %s", downloadURL)
	}
}

func TestWebDAVDriver_NewValidation(t *testing.T) {
	// 测试组件验证
	_, err := NewWebDAVDriver("", "user", "pass", "cloudreve", "")
	if err == nil {
		t.Error("空 serverURL 应返回错误")
	}

	_, err = NewWebDAVDriver("https://dav.example.com", "", "pass", "cloudreve", "")
	if err == nil {
		t.Error("空 username 应返回错误")
	}

	_, err = NewWebDAVDriver("https://dav.example.com", "user", "", "cloudreve", "")
	if err == nil {
		t.Error("空 password 应返回错误")
	}

	// 正常初始化
	d, err := NewWebDAVDriver("https://dav.example.com", "user", "pass", "cloudreve", "")
	if err != nil {
		t.Fatal("正常初始化不应返回错误:", err)
	}
	if !d.IsConfigured() {
		t.Error("IsConfigured 应返回 true")
	}

	// 验证 basePath 默认值
	if d.basePath != "cloudreve" {
		t.Errorf("basePath 应为 cloudreve，实际: %s", d.basePath)
	}

	// 验证 serverURL 末尾斜杠被清理
	if d.serverURL != "https://dav.example.com" {
		t.Errorf("serverURL 不应包含末尾斜杠，实际: %s", d.serverURL)
	}
}
