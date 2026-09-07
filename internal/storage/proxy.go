package storage

import (
	"net/http"
	"net/url"

	"github.com/cloudreve-eo/cloudreve-eo/internal/proxyx"
)

// buildHTTPTransport 根据策略的代理配置构建 HTTP Transport。
// 如果策略启用了代理且指定了代理地址，使用该地址；
// 如果策略启用了代理但未指定地址，使用全局代理；
// 如果策略未启用代理，返回 nil（使用默认 Transport，即直连）。
func buildHTTPTransport(proxyEnabled bool, proxyURL string) http.RoundTripper {
	if !proxyEnabled {
		return nil // 直连
	}

	// 策略指定了代理地址，使用该地址
	if proxyURL != "" {
		return &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(proxyURL)),
		}
	}

	// 策略启用代理但未指定地址，使用全局代理
	globalProxy := proxyx.Get()
	if globalProxy != "" {
		return &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(globalProxy)),
		}
	}

	return nil // 全局也未配置，直连
}

// mustParseURL 解析 URL，失败时 panic（调用前应校验格式）。
func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		// 不应发生：handler 层已校验格式
		panic("invalid proxy URL: " + rawURL)
	}
	return u
}
