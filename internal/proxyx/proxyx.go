// Package proxyx 提供全局 HTTP 代理配置：
// 管理员在「参数设置」填写代理地址后，进程内所有出站 HTTP 请求
// （存储驱动、数据库持久化等）统一经该代理转发；留空则直连。
package proxyx

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Set 设置全局代理地址。空字符串表示清除代理（直连）。
// 通过设置 HTTP_PROXY/HTTPS_PROXY 环境变量实现，所有使用标准 http.Client 的代码自动生效。
func Set(proxyURL string) {
	proxyURL = strings.TrimSpace(proxyURL)

	if proxyURL == "" {
		// 清除代理：删除环境变量
		os.Unsetenv("HTTP_PROXY")
		os.Unsetenv("HTTPS_PROXY")
		os.Unsetenv("http_proxy")
		os.Unsetenv("https_proxy")
		return
	}

	// 设置代理环境变量
	os.Setenv("HTTP_PROXY", proxyURL)
	os.Setenv("HTTPS_PROXY", proxyURL)
	os.Setenv("http_proxy", proxyURL)
	os.Setenv("https_proxy", proxyURL)
}

// Get 获取当前代理地址。空字符串表示直连。
func Get() string {
	// 优先读取大写形式（Go 标准库约定）
	if v := os.Getenv("HTTP_PROXY"); v != "" {
		return v
	}
	if v := os.Getenv("http_proxy"); v != "" {
		return v
	}
	return ""
}

// Validate 校验代理地址格式。返回 nil 表示有效。
func Validate(proxyURL string) error {
	if proxyURL == "" {
		return nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return err
	}
	if u.Scheme == "" || u.Host == "" {
		return http.ErrNotSupported
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" {
		return http.ErrNotSupported
	}
	return nil
}
