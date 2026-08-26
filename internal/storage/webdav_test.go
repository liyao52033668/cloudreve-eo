package storage

import (
	"net/url"
	"strings"
	"testing"
)

func TestWebDAVDriver_DirectMode(t *testing.T) {
	// 测试直连模式生成带 Basic Auth 的 URL
	d, err := NewWebDAVDriver("https://dav.example.com", "user", "pass", "cloudreve", "", true)
	if err != nil {
		t.Fatal(err)
	}

	// 上传 URL 应该内嵌 Basic Auth 凭据
	uploadURL, err := d.GenerateUploadURL("user123/file.txt", "text/plain", 0)
	if err != nil {
		t.Fatal("直连模式 GenerateUploadURL 不应返回错误:", err)
	}
	if !strings.Contains(uploadURL, "user:pass@") {
		t.Errorf("上传 URL 应包含 user:pass@，实际: %s", uploadURL)
	}
	if !strings.Contains(uploadURL, "cloudreve/user123/file.txt") {
		t.Errorf("上传 URL 应包含完整路径，实际: %s", uploadURL)
	}

	// 下载 URL 同样内嵌 Basic Auth
	downloadURL, err := d.GenerateDownloadURL("user123/file.txt", "file.txt", 0)
	if err != nil {
		t.Fatal("直连模式 GenerateDownloadURL 不应返回错误:", err)
	}
	if !strings.Contains(downloadURL, "user:pass@") {
		t.Errorf("下载 URL 应包含 user:pass@，实际: %s", downloadURL)
	}
}

func TestWebDAVDriver_ProxyMode(t *testing.T) {
	// 测试中转模式（默认）
	d, err := NewWebDAVDriver("https://dav.example.com", "user", "pass", "cloudreve", "", false)
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

func TestWebDAVDirectURL(t *testing.T) {
	// 测试 directURL 生成逻辑
	d := &WebDAVDriver{
		serverURL: "https://dav.example.com",
		username:  "user",
		password:  "pass@word", // 包含特殊字符
		basePath:  "cloudreve",
	}

	result := d.directURL("user123/file.txt")

	// 应包含 URL 编码的凭据
	if !strings.Contains(result, "user:pass%40word@") {
		t.Errorf("directURL 应 URL 编码特殊字符，实际: %s", result)
	}
	// 应包含完整路径
	if !strings.Contains(result, "cloudreve/user123/file.txt") {
		t.Errorf("directURL 应包含完整路径，实际: %s", result)
	}
}
