package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
)

// errPolicyNotDropbox 策略不是 Dropbox 类型。
var errPolicyNotDropbox = &policyError{msg: "该策略不是 Dropbox 类型"}

// getDropboxDriver 取策略对应的 Dropbox 驱动；非 dropbox 类型报错。
func (h *PolicyHandler) getDropboxDriver(id uint) (*model.StoragePolicy, *storage.DropboxDriver, error) {
	p, err := model.GetStoragePolicyByID(id)
	if err != nil {
		return nil, nil, err
	}
	if p.Type != "dropbox" {
		return p, nil, errPolicyNotDropbox
	}
	driver, err := h.mgr.GetDriver(p.Name)
	if err != nil {
		return p, nil, err
	}
	db, ok := driver.(*storage.DropboxDriver)
	if !ok {
		return p, nil, errPolicyNotDropbox
	}
	return p, db, nil
}

// dropboxRedirectURI 计算 Dropbox OAuth 回调地址（必须是绝对 URI）。
// 优先级：前端显式传入的 origin（浏览器地址栏真实域名，最可靠）
// → 策略的 CustomHost → 请求头推导（X-Forwarded-Host 优先，EdgeOne 转发时 Host 是内部域名）。
func (h *PolicyHandler) dropboxRedirectURI(c *gin.Context, p *model.StoragePolicy) string {
	// 前端传入的 origin（如 https://example.com），去掉末尾斜杠
	origin := strings.TrimRight(strings.TrimSpace(c.Query("origin")), "/")
	if origin != "" && strings.HasPrefix(origin, "http") {
		return origin + "/api/oauth/dropbox/callback"
	}
	if p.CustomHost != "" {
		return strings.TrimRight(p.CustomHost, "/") + "/api/oauth/dropbox/callback"
	}
	scheme := "https"
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
	} else if strings.HasPrefix(c.Request.Host, "localhost") || strings.HasPrefix(c.Request.Host, "127.0.0.1") {
		scheme = "http"
	}
	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}
	return scheme + "://" + host + "/api/oauth/dropbox/callback"
}

// DropboxAuthURL GET /api/admin/storage/policies/:id/dropbox/auth-url
// 返回 Dropbox OAuth 授权地址。
func (h *PolicyHandler) DropboxAuthURL(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	p, db, err := h.getDropboxDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	authURL := db.GetAuthURL(h.dropboxRedirectURI(c, p))
	c.JSON(http.StatusOK, gin.H{"auth_url": authURL})
}

// DropboxAuthByCode POST /api/admin/storage/policies/:id/dropbox/auth-code
// 用授权码换 token。
func (h *PolicyHandler) DropboxAuthByCode(c *gin.Context) {
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
	p, db, err := h.getDropboxDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 回调地址需与生成授权 URL 时一致
	if err := db.GetTokenByCode(req.Code, h.dropboxRedirectURI(c, p)); err != nil {
		logx.Error(logx.ModuleStorage, "Dropbox 授权码换 token 失败", logx.Err(err), "policy", p.Name)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.mgr.ReloadFromDB(); err != nil {
		logx.Error(logx.ModuleStorage, "Dropbox 授权成功但热加载失败", logx.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "授权成功但加载失败: " + err.Error()})
		return
	}
	logx.Info(logx.ModuleStorage, "Dropbox 授权成功", "policy", p.Name)
	c.JSON(http.StatusOK, gin.H{"message": "授权成功"})
}

// DropboxAuthStatus POST /api/admin/storage/policies/:id/dropbox/auth-status
// 查询授权状态。
func (h *PolicyHandler) DropboxAuthStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	_, db, err := h.getDropboxDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status := "unauthorized"
	if db.IsAuthorized() {
		status = "authorized"
	}
	c.JSON(http.StatusOK, gin.H{"status": status})
}

// DropboxOAuthCallback GET /api/oauth/dropbox/callback —— 公开回调路由。
// Dropbox 授权完成后携带 code 跳回本站。
func (h *PolicyHandler) DropboxOAuthCallback(c *gin.Context) {
	// Dropbox 回调页面：向打开它的管理页 postMessage 通知授权结果
	callbackPage := `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>Dropbox 授权</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f6f7}
.box{text-align:center;padding:32px}h1{font-size:18px;margin:0 0 8px}p{color:#666;font-size:14px;margin:0}</style></head>
<body><div class="box"><h1>%s</h1><p>%s</p></div>
<script>try{window.opener&&window.opener.postMessage({event:"dropboxOauthDone",ok:%s,error:"%s"},"*")}catch(e){}</script>
</body></html>`

	render := func(title, detail string, ok bool) {
		okStr := "false"
		errMsg := detail
		if ok {
			okStr = "true"
			errMsg = ""
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8",
			[]byte(fmt.Sprintf(callbackPage, title, htmlEscape(detail), okStr, errMsg)))
	}

	code := c.Query("code")
	if code == "" {
		if errMsg := c.Query("error"); errMsg != "" {
			logx.Warn(logx.ModuleStorage, "Dropbox 授权被拒绝", "error", errMsg)
			render("授权未完成", errMsg, false)
			return
		}
		render("授权失败", "回调缺少授权码（code）", false)
		return
	}

	// 注意：Dropbox 回调没有 state 参数，无法直接定位策略
	// 这里需要前端在授权前记录策略 ID，授权后通过 postMessage 传递
	// 暂时返回成功，让前端处理后续逻辑
	render("Dropbox 授权成功", "请返回管理页面完成授权绑定", true)
}
