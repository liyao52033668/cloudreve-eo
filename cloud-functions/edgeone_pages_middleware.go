package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// GeoInfo 结构化地理位置信息
// 从 eo-connecting-geo 请求头解析，字段与 Node Cloud Functions 的 context.geo 保持一致
type GeoInfo struct {
	ASN                string
	CountryName        string
	CountryCodeAlpha2  string
	CountryCodeNumeric string
	RegionName         string
	RegionCode         string
	CityName           string
	Latitude           string
	Longitude          string
	CISP               string
}

// contextKey 自定义 context key 类型，避免与其他包的 context key 冲突
type contextKey string

// ParseGeo 从 eo-connecting-geo header 值解析结构化 geo 信息
// 格式: key=value key="quoted value"
// 解析失败或 header 缺失时返回空 GeoInfo
func ParseGeo(headerValue string) GeoInfo {
	if headerValue == "" {
		return GeoInfo{}
	}
	decoded, err := url.QueryUnescape(headerValue)
	if err != nil {
		return GeoInfo{}
	}
	result := make(map[string]string)
	re := regexp.MustCompile(`[a-z_]+="[^"]*"|[a-z_]+=[A-Za-z0-9.-]+`)
	matches := re.FindAllString(decoded, -1)
	for _, match := range matches {
		parts := strings.SplitN(match, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = strings.Trim(parts[1], "\"")
		}
	}
	geo := GeoInfo{
		ASN:                result["asn"],
		CountryName:        result["nation_name"],
		CountryCodeNumeric: result["nation_numeric"],
		RegionName:         result["region_name"],
		RegionCode:         result["region_code"],
		CityName:           result["city_name"],
		Latitude:           result["latitude"],
		Longitude:          result["longitude"],
		CISP:               result["network_operator"],
	}
	if rc := result["region_code"]; rc != "" {
		if idx := strings.Index(rc, "-"); idx != -1 {
			geo.CountryCodeAlpha2 = rc[:idx]
		}
	}
	return geo
}

// GetGeo 从 request context 读取 GeoInfo
func GetGeo(ctx context.Context) GeoInfo {
	if geo, ok := ctx.Value(contextKey("geo")).(GeoInfo); ok {
		return geo
	}
	return GeoInfo{}
}

// GetClientIP 从 request context 读取 client IP
func GetClientIP(ctx context.Context) string {
	if ip, ok := ctx.Value(contextKey("client_ip")).(string); ok {
		return ip
	}
	return ""
}

// __edgeonePagesMiddleware 是由 EdgeOne Makers CLI 自动注入的中间件（函数名保持 __edgeonePagesMiddleware 兼容存量）
// 用于处理 Pages 平台所需的请求/响应日志和 SCF 相关 headers
func __edgeonePagesMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		r := c.Request

		// 0. Parse geo from eo-connecting-geo header
		geo := ParseGeo(r.Header.Get("eo-connecting-geo"))
		clientIP := r.Header.Get("eo-connecting-ip")
		ctx := context.WithValue(r.Context(), contextKey("geo"), geo)
		ctx = context.WithValue(ctx, contextKey("client_ip"), clientIP)
		c.Request = c.Request.WithContext(ctx)

		// 1. Build full path for logging
		var fullPath string
		host := r.Header.Get("eo-pages-host")
		if host == "" {
			fullPath = r.RequestURI
		} else {
			proto := r.Header.Get("x-forwarded-proto")
			if proto == "" {
				proto = "https"
			}
			fullPath = fmt.Sprintf("%s://%s%s", proto, host, r.RequestURI)
		}
		fmt.Printf("Makers request path: %s\n", fullPath)

		// 2. SCF request ID
		scfRequestID := r.Header.Get("x-scf-request-id")
		c.Header("Functions-Request-Id", scfRequestID)

		// 3. Panic recovery with 502 intercept
		defer func() {
			if err := recover(); err != nil {
				fmt.Printf("Handler panic: %v\n", err)
				c.Header("eo-pages-inner-scf-status", "502")
				c.Header("eo-pages-inner-status-intercept", "true")
				c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{
					"error":   "Internal Server Error",
					"code":    "FUNCTION_INVOCATION_FAILED",
					"message": fmt.Sprintf("%v", err),
				})
				fmt.Printf("Makers response status: %d\n", http.StatusBadGateway)
			}
		}()

		// 4. Execute handlers
		c.Next()

		// 5. After handlers: set response status headers
		statusCode := c.Writer.Status()

		// 如果用户代码已经设置了 eo-pages-inner-status-intercept，直接使用
		if c.Writer.Header().Get("eo-pages-inner-status-intercept") == "" {
			if statusCode >= 500 {
				// 服务端错误 → 改为 502 并拦截
				c.Header("eo-pages-inner-scf-status", "502")
				c.Header("eo-pages-inner-status-intercept", "true")
			} else {
				c.Header("eo-pages-inner-scf-status", fmt.Sprintf("%d", statusCode))
				c.Header("eo-pages-inner-status-intercept", "false")
			}
		}

		fmt.Printf("Makers response status: %d\n", statusCode)
	}
}
