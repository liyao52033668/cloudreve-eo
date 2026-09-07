package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
// 返回 Dropbox OAuth 授权地址（state 携带签名的策略 ID，回调时用于定位策略）。
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

	// state 用 HMAC 签名 policyID，回调时原样带回，用于定位策略。
	// 复用 baiduAuthState（同为 policyID 的 HMAC 签名，与提供方无关）。
	state := h.baiduAuthState(uint(id))
	authURL := db.GetAuthURL(state, h.dropboxRedirectURI(c, p))
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
		Code   string `json:"code" binding:"required"`
		Origin string `json:"origin"` // 浏览器真实域名，须与生成授权 URL 时一致
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
	// origin 放入 Query 供 dropboxRedirectURI 统一读取
	if req.Origin != "" {
		c.Request.URL.RawQuery = "origin=" + url.QueryEscape(req.Origin)
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
// Dropbox 授权完成后携带 code 和 state 跳回本站。
// 回调页 JS 直接 fetch 后端 /api/oauth/dropbox/complete 完成换 token，
// 前端弹窗通过轮询授权状态感知完成，完全绕开 postMessage/opener 兼容性问题。
func (h *PolicyHandler) DropboxOAuthCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" {
		if errMsg := c.Query("error"); errMsg != "" {
			logx.Warn(logx.ModuleStorage, "Dropbox 授权被拒绝", "error", errMsg)
			h.renderDropboxCallback(c, "授权未完成", errMsg, false, "", "")
			return
		}
		h.renderDropboxCallback(c, "授权失败", "回调缺少授权码（code）", false, "", "")
		return
	}
	if state == "" {
		h.renderDropboxCallback(c, "授权失败", "回调缺少 state 参数", false, "", "")
		return
	}

	// 回调页 JS：fetch 后端 complete 接口完成换 token。
	// 失败时降级显示 code 供用户手动复制。
	codeJSON, _ := json.Marshal(code)
	stateJSON, _ := json.Marshal(state)
	h.renderDropboxCallback(c, "授权成功", "正在自动完成授权，请稍候...", true, string(codeJSON), string(stateJSON))
}

// DropboxComplete POST /api/oauth/dropbox/complete —— 公开接口，回调页调用。
// 校验 state 签名定位策略，用授权码换 token 并持久化。
func (h *PolicyHandler) DropboxComplete(c *gin.Context) {
	var req struct {
		Code   string `json:"code" binding:"required"`
		State  string `json:"state" binding:"required"`
		Origin string `json:"origin"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	policyID, err := h.parseBaiduState(req.State)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, db, err := h.getDropboxDriver(policyID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 把 origin 放入 Query 供 dropboxRedirectURI 统一读取（与 DropboxAuthByCode 一致）
	if req.Origin != "" {
		c.Request.URL.RawQuery = "origin=" + url.QueryEscape(req.Origin)
	}
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

// renderDropboxCallback 渲染 Dropbox 回调落地页。
// codeJSON/stateJSON 为已 JSON 编码的字符串（防 XSS），可为空。
func (h *PolicyHandler) renderDropboxCallback(c *gin.Context, title, detail string, ok bool, codeJSON, stateJSON string) {
	callbackPage := `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>Dropbox 授权</title>
<style>
body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f6f7}
.box{text-align:center;padding:32px;max-width:500px}
h1{font-size:18px;margin:0 0 8px}
h1.ok{color:#52c41a}
h1.err{color:#ff4d4f}
p{color:#666;font-size:14px;margin:0 0 16px}
.manual{display:none;margin-top:24px;padding:16px;background:#fff;border-radius:8px;border:1px solid #d9d9d9}
.manual.show{display:block}
.code-box{background:#f5f5f5;padding:8px 12px;border-radius:4px;font-family:monospace;font-size:12px;word-break:break-all;margin:8px 0;cursor:pointer;user-select:all}
.btn{display:inline-block;padding:8px 20px;background:#1677ff;color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:14px;margin-top:8px}
.btn:hover{background:#4096ff}
</style></head>
<body><div class="box">
<h1 class="%s">%s</h1>
<p id="status">%s</p>
<div class="manual" id="manual">
<p style="color:#fa8c16">自动流程未完成，请手动复制授权码并返回管理页面提交</p>
<div class="code-box" id="codeBox" onclick="this.select();document.execCommand('copy');this.textContent='已复制！'"></div>
<button class="btn" onclick="window.close()">关闭此页</button>
</div>
</div>
<script>
const code = %s;
const state = %s;
const ok = %s;
if (ok && code && state) {
  fetch('/api/oauth/dropbox/complete', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({code: code, state: state, origin: window.location.origin})
  })
  .then(r => r.json())
  .then(d => {
    if (d.error) {
      document.getElementById('status').textContent = '授权失败: ' + d.error;
      showManual();
    } else {
      document.getElementById('status').textContent = '✓ 授权成功！请返回管理页面';
    }
  })
  .catch(e => {
    document.getElementById('status').textContent = '网络错误，请手动提交授权码';
    showManual();
  });
} else {
  document.getElementById('status').textContent = '%s';
  showManual();
}
function showManual() {
  if (code) {
    document.getElementById('manual').classList.add('show');
    document.getElementById('codeBox').textContent = code;
  }
}
</script>
</body></html>`

	okStr := "false"
	if ok {
		okStr = "true"
	}
	titleClass := "err"
	if ok {
		titleClass = "ok"
	}
	if codeJSON == "" {
		codeJSON = `""`
	}
	if stateJSON == "" {
		stateJSON = `""`
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8",
		[]byte(fmt.Sprintf(callbackPage, titleClass, htmlEscape(title), htmlEscape(detail), codeJSON, stateJSON, okStr, htmlEscape(detail))))
}
