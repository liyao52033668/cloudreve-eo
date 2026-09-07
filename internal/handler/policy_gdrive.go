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

// errPolicyNotGDrive 策略不是 Google Drive 类型。
var errPolicyNotGDrive = &policyError{msg: "该策略不是 Google Drive 类型"}

// getGDriveDriver 取策略对应的 Google Drive 驱动；非 gdrive 类型报错。
func (h *PolicyHandler) getGDriveDriver(id uint) (*model.StoragePolicy, *storage.GDriveDriver, error) {
	p, err := model.GetStoragePolicyByID(id)
	if err != nil {
		return nil, nil, err
	}
	if p.Type != "gdrive" {
		return p, nil, errPolicyNotGDrive
	}
	driver, err := h.mgr.GetDriver(p.Name)
	if err != nil {
		return p, nil, err
	}
	gd, ok := driver.(*storage.GDriveDriver)
	if !ok {
		return p, nil, errPolicyNotGDrive
	}
	return p, gd, nil
}

// gdriveRedirectURI 计算 Google Drive OAuth 回调地址（必须是绝对 URI）。
// 优先级：前端显式传入的 origin（浏览器地址栏真实域名，最可靠）
// → 策略的 CustomHost → 请求头推导（X-Forwarded-Host 优先，EdgeOne 转发时 Host 是内部域名）。
func (h *PolicyHandler) gdriveRedirectURI(c *gin.Context, p *model.StoragePolicy) string {
	// 前端传入的 origin（如 https://example.com），去掉末尾斜杠
	origin := strings.TrimRight(strings.TrimSpace(c.Query("origin")), "/")
	if origin != "" && strings.HasPrefix(origin, "http") {
		return origin + "/api/oauth/gdrive/callback"
	}
	if p.CustomHost != "" {
		return strings.TrimRight(p.CustomHost, "/") + "/api/oauth/gdrive/callback"
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
	return scheme + "://" + host + "/api/oauth/gdrive/callback"
}

// GDriveAuthURL GET /api/admin/storage/policies/:id/gdrive/auth-url
// 返回 Google Drive OAuth 授权地址。
func (h *PolicyHandler) GDriveAuthURL(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	p, gd, err := h.getGDriveDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	authURL := gd.GetAuthURL(h.gdriveRedirectURI(c, p))
	c.JSON(http.StatusOK, gin.H{"auth_url": authURL})
}

// GDriveAuthByCode POST /api/admin/storage/policies/:id/gdrive/auth-code
// 用授权码换 token。
func (h *PolicyHandler) GDriveAuthByCode(c *gin.Context) {
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
	p, gd, err := h.getGDriveDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// origin 放入 Query 供 gdriveRedirectURI 统一读取
	if req.Origin != "" {
		c.Request.URL.RawQuery = "origin=" + url.QueryEscape(req.Origin)
	}

	// 回调地址需与生成授权 URL 时一致
	if err := gd.GetTokenByCode(req.Code, h.gdriveRedirectURI(c, p)); err != nil {
		logx.Error(logx.ModuleStorage, "Google Drive 授权码换 token 失败", logx.Err(err), "policy", p.Name)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.mgr.ReloadFromDB(); err != nil {
		logx.Error(logx.ModuleStorage, "Google Drive 授权成功但热加载失败", logx.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "授权成功但加载失败: " + err.Error()})
		return
	}
	logx.Info(logx.ModuleStorage, "Google Drive 授权成功", "policy", p.Name)
	c.JSON(http.StatusOK, gin.H{"message": "授权成功"})
}

// GDriveAuthStatus POST /api/admin/storage/policies/:id/gdrive/auth-status
// 查询授权状态。
func (h *PolicyHandler) GDriveAuthStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	_, gd, err := h.getGDriveDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status := "unauthorized"
	if gd.IsAuthorized() {
		status = "authorized"
	}
	c.JSON(http.StatusOK, gin.H{"status": status})
}

// GDriveOAuthCallback GET /api/oauth/gdrive/callback —— 公开回调路由。
// Google Drive 授权完成后携带 code 跳回本站。
func (h *PolicyHandler) GDriveOAuthCallback(c *gin.Context) {
	// Google Drive 回调页面：向打开它的管理页 postMessage 通知授权结果
	callbackPage := `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>Google Drive 授权</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f6f7}
.box{text-align:center;padding:32px}h1{font-size:18px;margin:0 0 8px}p{color:#666;font-size:14px;margin:0}</style></head>
<body><div class="box"><h1>%s</h1><p>%s</p></div>
<script>try{window.opener&&window.opener.postMessage({event:"gdriveOauthDone",ok:%s,error:"%s"},"*")}catch(e){}</script>
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
			logx.Warn(logx.ModuleStorage, "Google Drive 授权被拒绝", "error", errMsg)
			render("授权未完成", errMsg, false)
			return
		}
		render("授权失败", "回调缺少授权码（code）", false)
		return
	}

	// 自动处理：提取 code 并通过 postMessage 传给弹窗，弹窗收到后自动调用 API 换 token。
	// 兜底：若 opener 为 null（浏览器拦截）或 postMessage 失败，显示 code 让用户手动复制。
	// code 来自 URL 参数，用 JSON 编码防 XSS。
	codeJSON, _ := json.Marshal(code)
	callbackPageWithCode := `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>Google Drive 授权成功</title>
<style>
body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f6f7}
.box{text-align:center;padding:32px;max-width:500px}
h1{font-size:18px;margin:0 0 8px;color:#52c41a}
p{color:#666;font-size:14px;margin:0 0 16px}
.manual{display:none;margin-top:24px;padding:16px;background:#fff;border-radius:8px;border:1px solid #d9d9d9}
.manual.show{display:block}
.code-box{background:#f5f5f5;padding:8px 12px;border-radius:4px;font-family:monospace;font-size:12px;word-break:break-all;margin:8px 0;cursor:pointer;user-select:all}
.btn{display:inline-block;padding:8px 20px;background:#1677ff;color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:14px}
.btn:hover{background:#4096ff}
</style></head>
<body><div class="box">
<h1>✓ 授权成功</h1>
<p id="status">正在自动完成授权，请稍候...</p>
<div class="manual" id="manual">
<p style="color:#fa8c16">自动流程未完成，请手动复制授权码</p>
<div class="code-box" id="codeBox" onclick="this.select();document.execCommand('copy');this.textContent='已复制！'"></div>
<button class="btn" onclick="window.close()">关闭此页</button>
</div>
</div>
<script>
const code = %s;
const hasOpener = (() => { try { return !!window.opener; } catch(e) { return false; } })();
let posted = false;
if (hasOpener) {
  try {
    window.opener.postMessage({event:"gdriveOauthDone",ok:true,code:code},"*");
    posted = true;
  } catch(e) {}
}
if (!posted) {
  document.getElementById("status").textContent = "授权成功！请复制下方授权码并返回管理页面提交";
  document.getElementById("manual").classList.add("show");
  document.getElementById("codeBox").textContent = code;
}
</script>
</body></html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(callbackPageWithCode, string(codeJSON))))
}
