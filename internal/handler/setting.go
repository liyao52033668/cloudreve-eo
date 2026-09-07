package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/proxyx"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/gin-gonic/gin"
)

// SettingHandler 系统参数（管理员）。
type SettingHandler struct {
	jwtSecrets *service.JWTSecretStore
}

func NewSettingHandler(jwtSecrets *service.JWTSecretStore) *SettingHandler {
	return &SettingHandler{jwtSecrets: jwtSecrets}
}

// GetSecurity GET /api/settings/security
func (h *SettingHandler) GetSecurity(c *gin.Context) {
	allowRegister, err := model.IsRegisterAllowed()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"jwt_secret":     h.jwtSecrets.Get(),
		"allow_register": allowRegister,
	})
}

// RotateJWTSecret POST /api/settings/security/rotate-jwt
// 轮转后所有既有用户令牌立即失效。
func (h *SettingHandler) RotateJWTSecret(c *gin.Context) {
	secret, err := h.jwtSecrets.Rotate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"jwt_secret": secret,
		"message":    "主密钥已轮转，所有用户令牌已失效，请重新登录",
	})
}

type updateRegisterRequest struct {
	AllowRegister *bool `json:"allow_register" binding:"required"`
}

// UpdateRegister PUT /api/settings/register
// 管理员开关：是否允许新用户通过前台注册。
func (h *SettingHandler) UpdateRegister(c *gin.Context) {
	var req updateRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	if err := model.SetAllowRegister(*req.AllowRegister); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"allow_register": *req.AllowRegister,
		"message":        "注册开关已更新",
	})
}

// GetPublicSite GET /api/site（公开，供登录页判断是否展示注册入口）
func (h *SettingHandler) GetPublicSite(c *gin.Context) {
	allowRegister, err := model.IsRegisterAllowed()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"allow_register": allowRegister,
	})
}

// GetWebDAV GET /api/settings/webdav
// 获取 WebDAV 服务配置（管理员）。
func (h *SettingHandler) GetWebDAV(c *gin.Context) {
	enabled, err := model.IsWebDAVEnabled()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": enabled,
	})
}

type updateWebDAVRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// UpdateWebDAV PUT /api/settings/webdav
// 管理员开关：是否启用 WebDAV 服务。
func (h *SettingHandler) UpdateWebDAV(c *gin.Context) {
	var req updateWebDAVRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	if err := model.SetWebDAVEnabled(*req.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": *req.Enabled,
		"message": "WebDAV 服务已" + map[bool]string{true: "启用", false: "禁用"}[*req.Enabled],
	})
}

// GetProxy GET /api/settings/proxy
// 获取出站代理配置（管理员）。
func (h *SettingHandler) GetProxy(c *gin.Context) {
	proxyURL, err := model.GetProxyURL()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"proxy_url": proxyURL,
		"enabled":   proxyURL != "",
	})
}

type updateProxyRequest struct {
	ProxyURL string `json:"proxy_url"`
}

// UpdateProxy PUT /api/settings/proxy
// 管理员设置出站代理地址；空字符串表示清除代理（直连）。
func (h *SettingHandler) UpdateProxy(c *gin.Context) {
	var req updateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	proxyURL := strings.TrimSpace(req.ProxyURL)

	// 校验代理地址格式（非空时）
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "代理地址格式无效，应为 http://host:port 或 socks5://host:port"})
			return
		}
		if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 http、https、socks5 协议"})
			return
		}
	}

	if err := model.SetProxyURL(proxyURL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 立即生效：更新全局代理配置
	proxyx.Set(proxyURL)

	msg := "代理已设置"
	if proxyURL == "" {
		msg = "代理已清除，恢复直连"
	}
	c.JSON(http.StatusOK, gin.H{
		"proxy_url": proxyURL,
		"enabled":   proxyURL != "",
		"message":   msg,
	})
}
