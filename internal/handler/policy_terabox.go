package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
)

// PolicyHandler 内嵌的扫码授权会话表（内存态）；key 为策略 ID。
var (
	teraboxSessionsMu sync.Mutex
	teraboxSessions   = make(map[uint]*storage.TeraBoxAuthSession)
)

// getTeraBoxDriver 取策略对应的 TeraBox 驱动；非 TeraBox 类型报错。
func (h *PolicyHandler) getTeraBoxDriver(id uint) (*model.StoragePolicy, *storage.TeraBoxDriver, error) {
	p, err := model.GetStoragePolicyByID(id)
	if err != nil {
		return nil, nil, err
	}
	if p.Type != "terabox" {
		return p, nil, errPolicyNotTerabox
	}
	driver, err := h.mgr.GetDriver(p.Name)
	if err != nil {
		return p, nil, err
	}
	tb, ok := driver.(*storage.TeraBoxDriver)
	if !ok {
		return p, nil, errPolicyNotTerabox
	}
	return p, tb, nil
}

var errPolicyNotTerabox = &policyError{msg: "该策略不是 TeraBox 类型"}

type policyError struct{ msg string }

func (e *policyError) Error() string { return e.msg }

// TeraBoxAuthURL GET /api/admin/storage/policies/:id/terabox/auth-url
// 返回网页授权（iframe）地址，管理员页面内嵌完成授权后回调携 code。
func (h *PolicyHandler) TeraBoxAuthURL(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	p, _, err := h.getTeraBoxDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	authURL := "https://www.terabox.com/wap/outside/login?clientId=" + url.QueryEscape(p.AccessKey)
	c.JSON(http.StatusOK, gin.H{"auth_url": authURL})
}

// TeraBoxAuthByCode POST /api/admin/storage/policies/:id/terabox/auth-code —— 用授权码换 token。
func (h *PolicyHandler) TeraBoxAuthByCode(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	_, tb, err := h.getTeraBoxDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := tb.GetTokenByCode(req.Code); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.mgr.ReloadFromDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "授权成功但热加载失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "TeraBox 授权成功"})
}

// TeraBoxDeviceCode POST /api/admin/storage/policies/:id/terabox/devicecode —— 获取扫码授权二维码。
func (h *PolicyHandler) TeraBoxDeviceCode(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	p, tb, err := h.getTeraBoxDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	qr, session, err := tb.RequestDeviceCode()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	teraboxSessionsMu.Lock()
	teraboxSessions[uint(id)] = session
	teraboxSessionsMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"qrcode":      qr,
		"policy_name": p.Name,
		"expires_in":  session.ExpiresAt - time.Now().Unix(),
		"interval":    session.Interval,
	})
}

// TeraBoxAuthStatus POST /api/admin/storage/policies/:id/terabox/auth-status —— 轮询扫码授权结果。
func (h *PolicyHandler) TeraBoxAuthStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	_, tb, err := h.getTeraBoxDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	teraboxSessionsMu.Lock()
	session := teraboxSessions[uint(id)]
	teraboxSessionsMu.Unlock()
	if session == nil {
		c.JSON(http.StatusOK, gin.H{"status": "no_session"})
		return
	}
	if time.Now().Unix() > session.ExpiresAt {
		c.JSON(http.StatusOK, gin.H{"status": "expired"})
		return
	}

	pending, err := tb.PollDeviceCode(session.DeviceCode)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "error", "error": err.Error()})
		return
	}
	if pending {
		c.JSON(http.StatusOK, gin.H{"status": "pending"})
		return
	}

	teraboxSessionsMu.Lock()
	delete(teraboxSessions, uint(id))
	teraboxSessionsMu.Unlock()
	if err := h.mgr.ReloadFromDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "授权成功但热加载失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "authorized", "message": "TeraBox 授权成功"})
}
