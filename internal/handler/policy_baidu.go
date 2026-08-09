package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
)

// errPolicyNotBaidu 策略不是百度网盘类型。
var errPolicyNotBaidu = &policyError{msg: "该策略不是百度网盘类型"}

// getBaiduDriver 取策略对应的百度网盘驱动；非 baidu 类型报错。
func (h *PolicyHandler) getBaiduDriver(id uint) (*model.StoragePolicy, *storage.BaiduDriver, error) {
	p, err := model.GetStoragePolicyByID(id)
	if err != nil {
		return nil, nil, err
	}
	if p.Type != "baidu" {
		return p, nil, errPolicyNotBaidu
	}
	driver, err := h.mgr.GetDriver(p.Name)
	if err != nil {
		return p, nil, err
	}
	bd, ok := driver.(*storage.BaiduDriver)
	if !ok {
		return p, nil, errPolicyNotBaidu
	}
	return p, bd, nil
}

// baiduStateTTL OAuth state 有效期（授权窗口时长）。
const baiduStateTTL = 10 * time.Minute

// baiduAuthState 生成带签名的 OAuth state（编码策略 ID，防回调伪造）。
// 格式 base64url(policyID|expire|hex(hmac(secret, policyID|expire)))。
func (h *PolicyHandler) baiduAuthState(policyID uint) string {
	exp := time.Now().Add(baiduStateTTL).Unix()
	payload := fmt.Sprintf("%d|%d", policyID, exp)
	mac := hmac.New(sha256.New, []byte(h.secret()))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
}

// parseBaiduState 校验 state 签名与有效期，返回策略 ID。
func (h *PolicyHandler) parseBaiduState(state string) (uint, error) {
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return 0, fmt.Errorf("state 无效")
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return 0, fmt.Errorf("state 无效")
	}
	payload := parts[0] + "|" + parts[1]
	mac := hmac.New(sha256.New, []byte(h.secret()))
	mac.Write([]byte(payload))
	if !hmac.Equal(mac.Sum(nil), mustHexDecode(parts[2])) {
		return 0, fmt.Errorf("state 签名无效")
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, fmt.Errorf("state 已过期，请重新发起授权")
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("state 无效")
	}
	return uint(id), nil
}

func mustHexDecode(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

// BaiduAuthURL GET /api/admin/storage/policies/:id/baidu/auth-url
// 返回百度 OAuth 网页授权地址（qrcode=1 展示扫码页）。
// mode=oob：code 展示在页面由管理员手动回填；
// mode=redirect：授权后跳回本站回调路由自动完成（state 携带签名策略 ID）。
func (h *PolicyHandler) BaiduAuthURL(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	p, _, err := h.getBaiduDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mode := "oob"
	redirectURI := p.Endpoint
	if redirectURI == "" {
		redirectURI = "oob"
	} else {
		mode = "redirect"
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.AccessKey)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "basic,netdisk")
	q.Set("qrcode", "1")
	q.Set("display", "page")
	if mode == "redirect" {
		q.Set("state", h.baiduAuthState(uint(id)))
	}
	authURL := "https://openapi.baidu.com/oauth/2.0/authorize?" + q.Encode()
	c.JSON(http.StatusOK, gin.H{"auth_url": authURL, "mode": mode})
}

// baiduCallbackPage 回调落地页：向打开它的管理页 postMessage 通知授权结果。
const baiduCallbackPage = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>百度网盘授权</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f6f7}
.box{text-align:center;padding:32px}h1{font-size:18px;margin:0 0 8px}p{color:#666;font-size:14px;margin:0}</style></head>
<body><div class="box"><h1>%s</h1><p>%s</p></div>
<script>try{window.opener&&window.opener.postMessage({event:"baiduOauthDone",ok:%s,error:"%s"},"*")}catch(e){}</script>
</body></html>`

// BaiduOAuthCallback GET /api/oauth/baidu/callback —— 公开回调路由。
// 百度授权完成后携带 code 与 state 跳回本站，服务端直接换 token，
// 并向打开授权弹窗的管理页 postMessage，实现零手动复制授权。
func (h *PolicyHandler) BaiduOAuthCallback(c *gin.Context) {
	render := func(title, detail string, ok bool) {
		okStr := "false"
		errMsg := detail
		if ok {
			okStr = "true"
			errMsg = ""
		}
		// detail 可能来自百度返回的 error_description 等外部文本，转义防 XSS
		c.Data(http.StatusOK, "text/html; charset=utf-8",
			[]byte(fmt.Sprintf(baiduCallbackPage, title, htmlEscape(detail), okStr, url.QueryEscape(errMsg))))
	}

	if errMsg := c.Query("error"); errMsg != "" {
		desc := c.Query("error_description")
		logx.Warn(logx.ModuleStorage, "百度网盘授权被拒绝", "error", errMsg, "desc", desc)
		render("授权未完成", firstNonEmptyStr(desc, errMsg), false)
		return
	}
	code := c.Query("code")
	if code == "" {
		render("授权失败", "回调缺少授权码（code）", false)
		return
	}
	policyID, err := h.parseBaiduState(c.Query("state"))
	if err != nil {
		render("授权失败", err.Error(), false)
		return
	}
	p, bd, err := h.getBaiduDriver(policyID)
	if err != nil {
		render("授权失败", err.Error(), false)
		return
	}
	if p.Endpoint == "" {
		render("授权失败", "该策略未配置回调地址（redirect_uri），请使用 oob 模式手动回填", false)
		return
	}
	if err := bd.GetTokenByCode(code); err != nil {
		logx.Error(logx.ModuleStorage, "百度网盘回调换 token 失败", logx.Err(err), "policy", p.Name)
		render("授权失败", err.Error(), false)
		return
	}
	if err := h.mgr.ReloadFromDB(); err != nil {
		logx.Error(logx.ModuleStorage, "百度网盘授权成功但热加载失败", logx.Err(err))
		render("授权成功但加载失败", "token 已保存，请刷新页面或重新加载策略", true)
		return
	}
	logx.Info(logx.ModuleStorage, "百度网盘授权成功", "policy", p.Name)
	render("百度网盘授权成功", "已完成授权，可关闭本页面", true)
}

// firstNonEmptyStr 返回第一个非空字符串。
func firstNonEmptyStr(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// htmlEscape HTML 转义（回调落地页嵌入外部文本用）。
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// BaiduAuthByCode POST /api/admin/storage/policies/:id/baidu/auth-code —— 用授权码换 token。
func (h *PolicyHandler) BaiduAuthByCode(c *gin.Context) {
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

	_, bd, err := h.getBaiduDriver(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := bd.GetTokenByCode(req.Code); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.mgr.ReloadFromDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "授权成功但热加载失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "百度网盘授权成功"})
}
