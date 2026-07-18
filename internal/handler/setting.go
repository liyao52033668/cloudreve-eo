package handler

import (
	"net/http"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
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
