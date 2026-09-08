package storage

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

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

// NormalizeProxyURL 将常见代理格式规范化为标准 URL。
// 支持的格式：
//   - 标准 URL：http://host:port、socks5://user:pass@host:port（保持不变）
//   - host:port:user:pass → http://user:pass@host:port（用户名和密码中可包含冒号）
//   - host:port → http://host:port
//
// 空字符串直接返回空。
func NormalizeProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// 已包含 scheme（如 http://、socks5://），视为标准 URL
	if strings.Contains(raw, "://") {
		return raw
	}

	// 按冒号分割，支持 host:port:user:pass 格式
	// 前两段固定为 host 和 port，剩余部分合并为用户名和密码
	parts := strings.SplitN(raw, ":", 4)
	switch len(parts) {
	case 2:
		// host:port
		return fmt.Sprintf("http://%s:%s", parts[0], parts[1])
	case 4:
		// host:port:user:pass → http://user:pass@host:port
		// parts[3] 可能包含冒号（密码中的冒号）
		userInfo := url.UserPassword(parts[2], parts[3]).String()
		return fmt.Sprintf("http://%s@%s:%s", userInfo, parts[0], parts[1])
	default:
		// 无法识别的格式，返回原值让后续校验处理
		return raw
	}
}
