package storage

import (
	"net/http"
	"net/url"

	"github.com/cloudreve-eo/cloudreve-eo/internal/proxyx"
)

// buildHTTPTransport 根据策略的代理配置构建 HTTP Transport。
// 如果策略启用了代理且指定了代理地址，使用该地址；
// 如果策略启用了代理但未指定地址，使用全局代理（动态读取，支持运行时变更）；
// 如果策略未启用代理，返回 nil（使用默认 Transport，即直连）。
func buildHTTPTransport(proxyEnabled bool, proxyURL string) http.RoundTripper {
	if !proxyEnabled {
		return nil // 直连
	}

	// 策略指定了代理地址，使用该地址（静态）
	if proxyURL != "" {
		return &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(proxyURL)),
		}
	}

	// 策略启用代理但未指定地址，使用全局代理（动态读取）
	return &http.Transport{
		Proxy: dynamicGlobalProxy,
	}
}

// dynamicGlobalProxy 动态读取全局代理地址。
// 每次请求都会重新读取，支持运行时变更全局代理设置。
func dynamicGlobalProxy(req *http.Request) (*url.URL, error) {
	globalProxy := proxyx.Get()
	if globalProxy == "" {
		return nil, nil // 直连
	}
	return url.Parse(globalProxy)
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
