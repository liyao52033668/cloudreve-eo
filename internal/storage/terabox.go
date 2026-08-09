package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
)

// TeraBox 开放平台常量。
const (
	teraboxAppID       = 250528 // superfile2 分片上传固定 app_id
	teraboxDefaultPart = 16 << 20
	// token 剩余有效期低于该值时自动刷新（旧 token 刷新后 15 秒内仍可用，安全重叠）。
	teraboxRefreshAhead = 30 * time.Minute
	// tokeninfo 域名缓存不超过 1 小时（文档建议）。
	teraboxDomainCacheTTL = 1 * time.Hour
)

// TeraBoxToken TeraBox OAuth 凭据（序列化后持久化到策略的 oauth_token 字段）。
type TeraBoxToken struct {
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token"`
	AccessExpireAt int64  `json:"access_expire_at"` // Unix 秒
}

// TeraBoxDriver 使用 TeraBox 开放平台 API 实现存储驱动。
// 特点：无预签名直传，上传/下载全部经服务端中转；
// 凭证为 OAuth access_token + refresh_token，由管理员在管理页扫码或网页授权获得。
type TeraBoxDriver struct {
	clientID      string
	clientSecret  string
	privateSecret string // 签名私钥（策略的 Region 字段复用存储）
	appRootDir    string // 应用根目录，如 "/From: Other Applications/MyApp-123/"

	mu     sync.Mutex
	token  TeraBoxToken
	loaded bool // token 是否已从策略载入（空 token 待授权）

	domainMu        sync.Mutex
	apiDomain       string
	uploadDomain    string
	domainFetchedAt time.Time

	client *http.Client
	// onTokenRefreshed token 刷新后的持久化回调（写入策略 oauth_token 字段）。
	onTokenRefreshed func(TeraBoxToken)
}

// NewTeraBoxDriver 创建 TeraBox 驱动。
// endpoint 为应用根目录（如 /From: Other Applications/MyApp-123/）；
// region 复用为签名私钥 private_secret；tokenJSON 为已授权凭据（可空，待授权）。
func NewTeraBoxDriver(clientID, clientSecret, privateSecret, endpoint, tokenJSON string) (*TeraBoxDriver, error) {
	if clientID == "" {
		return nil, fmt.Errorf("TeraBox Client ID 不能为空")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("TeraBox Client Secret 不能为空")
	}
	if privateSecret == "" {
		return nil, fmt.Errorf("TeraBox Private Secret 不能为空")
	}
	rootDir := normalizeTeraBoxRootDir(endpoint)
	if rootDir == "" {
		return nil, fmt.Errorf("TeraBox 应用根目录不能为空")
	}

	d := &TeraBoxDriver{
		clientID:      clientID,
		clientSecret:  clientSecret,
		privateSecret: privateSecret,
		appRootDir:    rootDir,
		client:        &http.Client{Timeout: 10 * time.Minute},
	}

	tokenJSON = strings.TrimSpace(tokenJSON)
	if tokenJSON == "" {
		// 未授权：驱动可加载，但需管理员先完成授权才能读写文件。
		return d, nil
	}
	var token TeraBoxToken
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil || token.AccessToken == "" {
		return nil, fmt.Errorf("TeraBox 授权凭据格式错误，请重新授权")
	}
	d.token = token
	d.loaded = true
	return d, nil
}

// normalizeTeraBoxRootDir 规范化应用根目录：补全首尾斜杠。
func normalizeTeraBoxRootDir(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

// IsAuthorized 驱动是否已持有可用授权。
func (d *TeraBoxDriver) IsAuthorized() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loaded && d.token.AccessToken != ""
}

// errTeraBoxUnauthorized 未授权错误（管理页提示去授权）。
var errTeraBoxUnauthorized = errors.New("TeraBox 尚未授权，请到「存储策略」完成授权")

// teraboxAPIError TeraBox 返回 errno != 0。
type teraboxAPIError struct {
	Errno   int
	ShowMsg string
}

func (e *teraboxAPIError) Error() string {
	if e.ShowMsg != "" {
		return fmt.Sprintf("TeraBox API 错误(errno=%d): %s", e.Errno, e.ShowMsg)
	}
	return fmt.Sprintf("TeraBox API 错误(errno=%d)", e.Errno)
}

// teraboxSign 生成动态签名：md5(client_id_timestamp_client_secret_private_secret)。
func (d *TeraBoxDriver) teraboxSign(ts int64) string {
	raw := fmt.Sprintf("%s_%d_%s_%s", d.clientID, ts, d.clientSecret, d.privateSecret)
	sum := md5.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// contextWithTimeout 带超时的后台上下文。
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// oauthFormRequest 向 /oauth 系列接口发送表单请求。
func (d *TeraBoxDriver) oauthFormRequest(ctx context.Context, path string, form url.Values) (map[string]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.terabox.com"+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "CloudreveEO")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TeraBox 授权服务异常: HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		Errno   int                        `json:"errno"`
		ShowMsg string                     `json:"show_msg"`
		Data    map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析 TeraBox 授权响应失败: %s", truncate(string(body), 200))
	}
	if envelope.Errno != 0 {
		return nil, fmt.Errorf("TeraBox 授权失败(errno=%d): %s", envelope.Errno, teraboxAuthErrMsg(envelope.Errno, envelope.ShowMsg))
	}
	return envelope.Data, nil
}

// teraboxAuthErrMsg 常见授权错误码的中文说明。
func teraboxAuthErrMsg(errno int, showMsg string) string {
	switch errno {
	case 100001:
		return "Client ID 或 Client Secret 无效"
	case 100002:
		return "授权码无效或已过期，请重新授权"
	case 200002, 200003:
		return "access_token 无效或已过期，请重新授权"
	case 200004, 200005:
		return "refresh_token 无效或已过期，请重新授权"
	case 300001:
		return "请求过于频繁，请稍后重试"
	case 400001:
		return "用户尚未扫码授权"
	}
	if showMsg != "" {
		return showMsg
	}
	return "未知错误"
}

// GetTokenByCode 用授权码换取 token（授权码模式）。成功后立即持久化。
func (d *TeraBoxDriver) GetTokenByCode(code string) error {
	ts := time.Now().Unix()
	form := url.Values{}
	form.Set("client_id", d.clientID)
	form.Set("client_secret", d.clientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("timestamp", strconv.FormatInt(ts, 10))
	form.Set("sign", d.teraboxSign(ts))

	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()
	data, err := d.oauthFormRequest(ctx, "/oauth/gettoken", form)
	if err != nil {
		return err
	}
	return d.applyTokenData(data)
}

// RefreshToken 用 refresh_token 刷新。成功后立即持久化。
func (d *TeraBoxDriver) RefreshToken() error {
	d.mu.Lock()
	refresh := d.token.RefreshToken
	d.mu.Unlock()
	if refresh == "" {
		return fmt.Errorf("无 refresh_token，请重新授权")
	}

	ts := time.Now().Unix()
	form := url.Values{}
	form.Set("client_id", d.clientID)
	form.Set("client_secret", d.clientSecret)
	form.Set("refresh_token", refresh)
	form.Set("timestamp", strconv.FormatInt(ts, 10))
	form.Set("sign", d.teraboxSign(ts))

	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()
	data, err := d.oauthFormRequest(ctx, "/oauth/refreshtoken", form)
	if err != nil {
		return err
	}
	return d.applyTokenData(data)
}

// TeraBoxAuthSession 扫码授权会话（获取二维码后用于轮询换 token）。
type TeraBoxAuthSession struct {
	DeviceCode string `json:"device_code"`
	ExpiresAt  int64  `json:"expires_at"` // Unix 秒
	Interval   int    `json:"interval"`
}

// RequestDeviceCode 获取设备码与二维码（base64 图片）。
func (d *TeraBoxDriver) RequestDeviceCode() (qrcode string, session *TeraBoxAuthSession, err error) {
	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()

	u := url.URL{
		Scheme:   "https",
		Host:     "www.terabox.com",
		Path:     "/oauth/devicecode",
		RawQuery: "client_id=" + url.QueryEscape(d.clientID),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "CloudreveEO")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("TeraBox 授权服务异常: HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		Errno   int    `json:"errno"`
		ShowMsg string `json:"show_msg"`
		Data    struct {
			DeviceCode string `json:"device_code"`
			QrcodeURL  string `json:"qrcode_url"`
			ExpiresIn  int64  `json:"expires_in"`
			Interval   int    `json:"interval"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", nil, fmt.Errorf("解析 TeraBox 响应失败: %s", truncate(string(body), 200))
	}
	if envelope.Errno != 0 {
		return "", nil, fmt.Errorf("获取设备码失败(errno=%d): %s", envelope.Errno, teraboxAuthErrMsg(envelope.Errno, envelope.ShowMsg))
	}
	if envelope.Data.DeviceCode == "" {
		return "", nil, fmt.Errorf("TeraBox 未返回设备码")
	}

	expiresIn := envelope.Data.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 300
	}
	interval := envelope.Data.Interval
	if interval <= 0 {
		interval = 2
	}
	session = &TeraBoxAuthSession{
		DeviceCode: envelope.Data.DeviceCode,
		ExpiresAt:  time.Now().Unix() + expiresIn,
		Interval:   interval,
	}
	return envelope.Data.QrcodeURL, session, nil
}

// PollDeviceCode 用设备码轮询换取 token。
// pending=true 表示用户尚未扫码（调用方应继续轮询）；err 非 nil 表示失败。
func (d *TeraBoxDriver) PollDeviceCode(deviceCode string) (pending bool, err error) {
	ts := time.Now().Unix()
	form := url.Values{}
	form.Set("client_id", d.clientID)
	form.Set("client_secret", d.clientSecret)
	form.Set("grant_type", "device_code")
	form.Set("code", deviceCode)
	form.Set("timestamp", strconv.FormatInt(ts, 10))
	form.Set("sign", d.teraboxSign(ts))

	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()
	data, err := d.oauthFormRequest(ctx, "/oauth/gettoken", form)
	if err != nil {
		if strings.Contains(err.Error(), "errno=400001") {
			return true, nil
		}
		return false, err
	}
	return false, d.applyTokenData(data)
}

// applyTokenData 解析 gettoken/refreshtoken 的 data 并保存。
func (d *TeraBoxDriver) applyTokenData(data map[string]json.RawMessage) error {
	var access, refresh string
	var expiresIn int64
	if raw, ok := data["access_token"]; ok {
		_ = json.Unmarshal(raw, &access)
	}
	if raw, ok := data["refresh_token"]; ok {
		_ = json.Unmarshal(raw, &refresh)
	}
	if raw, ok := data["expires_in"]; ok {
		_ = json.Unmarshal(raw, &expiresIn)
	}
	if access == "" || refresh == "" {
		return fmt.Errorf("TeraBox 返回的 token 不完整")
	}
	if expiresIn <= 0 {
		expiresIn = 2 * 24 * 3600
	}

	token := TeraBoxToken{
		AccessToken:    access,
		RefreshToken:   refresh,
		AccessExpireAt: time.Now().Unix() + expiresIn,
	}
	d.mu.Lock()
	d.token = token
	d.loaded = true
	d.mu.Unlock()

	// 域名可能随用户区域变化，清缓存强制重取。
	d.domainMu.Lock()
	d.apiDomain = ""
	d.uploadDomain = ""
	d.domainMu.Unlock()

	if d.onTokenRefreshed != nil {
		d.onTokenRefreshed(token)
	}
	return nil
}

// currentToken 返回未过期的 access_token；临近过期时先刷新。
func (d *TeraBoxDriver) currentToken() (string, error) {
	d.mu.Lock()
	if !d.loaded || d.token.AccessToken == "" {
		d.mu.Unlock()
		return "", errTeraBoxUnauthorized
	}
	expireAt := d.token.AccessExpireAt
	d.mu.Unlock()

	if time.Now().Unix()+int64(teraboxRefreshAhead/time.Second) >= expireAt {
		if err := d.RefreshToken(); err != nil {
			// 刷新失败但 token 仍在有效期内时继续用旧 token。
			if time.Now().Unix() < expireAt {
				logx.Warn(logx.ModuleStorage, "TeraBox token 刷新失败，暂用旧 token", "err", err.Error())
			} else {
				return "", fmt.Errorf("TeraBox token 已过期且刷新失败: %w", err)
			}
		}
	}

	d.mu.Lock()
	token := d.token.AccessToken
	d.mu.Unlock()
	return token, nil
}

// ensureDomains 获取并缓存 tokeninfo 中的 api_domain / upload_domain。
func (d *TeraBoxDriver) ensureDomains() error {
	d.domainMu.Lock()
	if d.apiDomain != "" && time.Since(d.domainFetchedAt) < teraboxDomainCacheTTL {
		d.domainMu.Unlock()
		return nil
	}
	d.domainMu.Unlock()

	token, err := d.currentToken()
	if err != nil {
		return err
	}

	form := url.Values{}
	form.Set("access_token", token)
	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()
	data, err := d.oauthFormRequest(ctx, "/oauth/tokeninfo", form)
	if err != nil {
		return err
	}
	var apiDomain, uploadDomain string
	if raw, ok := data["api_domain"]; ok {
		_ = json.Unmarshal(raw, &apiDomain)
	}
	if raw, ok := data["upload_domain"]; ok {
		_ = json.Unmarshal(raw, &uploadDomain)
	}
	if apiDomain == "" {
		return fmt.Errorf("TeraBox tokeninfo 未返回 api_domain")
	}
	if uploadDomain == "" {
		uploadDomain = apiDomain
	}

	d.domainMu.Lock()
	d.apiDomain = apiDomain
	d.uploadDomain = uploadDomain
	d.domainFetchedAt = time.Now()
	d.domainMu.Unlock()
	return nil
}

func (d *TeraBoxDriver) domains() (api, upload string, err error) {
	if err := d.ensureDomains(); err != nil {
		return "", "", err
	}
	d.domainMu.Lock()
	api, upload = d.apiDomain, d.uploadDomain
	d.domainMu.Unlock()
	return api, upload, nil
}

// teraboxCall 调用业务能力 API（access_tokens 走 query）。
// path 形如 "/openapi/api/quota"；domain 为空时用 api_domain。
func (d *TeraBoxDriver) teraboxCall(ctx context.Context, method, domain, path string, query url.Values, form url.Values) (json.RawMessage, error) {
	if domain == "" {
		api, _, err := d.domains()
		if err != nil {
			return nil, err
		}
		domain = api
	}
	token, err := d.currentToken()
	if err != nil {
		return nil, err
	}

	if query == nil {
		query = url.Values{}
	}
	query.Set("access_tokens", token)
	u := url.URL{Scheme: "https", Host: domain, Path: path, RawQuery: query.Encode()}

	var bodyReader io.Reader
	if form != nil {
		bodyReader = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", "CloudreveEO")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TeraBox API 异常: HTTP %d %s", resp.StatusCode, truncate(string(body), 200))
	}

	var envelope struct {
		Errno   int             `json:"errno"`
		ShowMsg string          `json:"show_msg"`
		Raw     json.RawMessage `json:"-"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析 TeraBox 响应失败: %s", truncate(string(body), 200))
	}
	if envelope.Errno != 0 {
		return nil, &teraboxAPIError{Errno: envelope.Errno, ShowMsg: envelope.ShowMsg}
	}
	return body, nil
}

// rootPath 将相对 key 拼到应用根目录下。
func (d *TeraBoxDriver) rootPath(key string) string {
	return d.appRootDir + strings.TrimPrefix(strings.TrimSpace(key), "/")
}

// UploadFile 三步上传：precreate → 分片 superfile2 → create 合并。
func (d *TeraBoxDriver) UploadFile(key string, content []byte) error {
	ctx, cancel := contextWithTimeout(10 * time.Minute)
	defer cancel()
	fullPath := d.rootPath(key)

	// 单分片；文件小于分片大小时 block_list 只有一片，
	// 文档要求"每片大于 4MB"针对多分片场景，最后/唯一一片可小于该值。
	partSize := teraboxDefaultPart
	partCount := (len(content) + partSize - 1) / partSize
	if partCount == 0 {
		partCount = 1
	}
	blockMD5s := make([]string, 0, partCount)
	for i := 0; i < partCount; i++ {
		start := i * partSize
		end := start + partSize
		if end > len(content) {
			end = len(content)
		}
		sum := md5.Sum(content[start:end])
		blockMD5s = append(blockMD5s, hex.EncodeToString(sum[:]))
	}

	uploadID, fast, err := d.InitChunkedUpload(key, int64(len(content)), blockMD5s)
	if err != nil || fast {
		return err
	}

	// 2. 逐片上传
	_, uploadDomain, err := d.domains()
	if err != nil {
		return err
	}
	token, err := d.currentToken()
	if err != nil {
		return err
	}
	for seq := 0; seq < partCount; seq++ {
		start := seq * partSize
		if start > len(content) {
			start = len(content)
		}
		end := start + partSize
		if end > len(content) {
			end = len(content)
		}
		if err := d.uploadPart(ctx, uploadDomain, token, fullPath, uploadID, seq, content[start:end]); err != nil {
			return fmt.Errorf("上传分片 %d 失败: %w", seq, err)
		}
	}

	// 3. create 合并
	return d.CompleteChunkedUpload(key, uploadID, int64(len(content)), blockMD5s)
}

// InitChunkedUpload 预创建上传（precreate）。blockMD5s 为客户端计算的各块 MD5。
// fastUpload=true 表示云端秒传命中，无需再传任何块。
func (d *TeraBoxDriver) InitChunkedUpload(key string, size int64, blockMD5s []string) (string, bool, error) {
	ctx, cancel := contextWithTimeout(60 * time.Second)
	defer cancel()
	fullPath := d.rootPath(key)

	blockListJSON, _ := json.Marshal(blockMD5s)
	form := url.Values{}
	form.Set("path", fullPath)
	form.Set("autoinit", "1")
	form.Set("block_list", string(blockListJSON))
	raw, err := d.teraboxCall(ctx, http.MethodPost, "", "/openapi/api/precreate", nil, form)
	if err != nil {
		return "", false, fmt.Errorf("precreate 失败: %w", err)
	}
	var pre struct {
		ReturnType int    `json:"return_type"`
		UploadID   string `json:"uploadid"`
	}
	if err := json.Unmarshal(raw, &pre); err != nil {
		return "", false, fmt.Errorf("解析 precreate 响应失败: %w", err)
	}
	if pre.ReturnType == 2 {
		// 云端秒传命中，上传已完成
		logx.Info(logx.ModuleStorage, "TeraBox 秒传命中", "key", key)
		return "", true, nil
	}
	if pre.UploadID == "" {
		return "", false, fmt.Errorf("precreate 未返回 uploadid")
	}
	return pre.UploadID, false, nil
}

// UploadChunk 上传单个块（superfile2）。
// offset 参数不使用（块序号已隐含偏移）；原样返回 uploadID。
func (d *TeraBoxDriver) UploadChunk(key string, uploadID string, partSeq int, offset int64, data []byte) (string, error) {
	ctx, cancel := contextWithTimeout(2 * time.Minute)
	defer cancel()
	fullPath := d.rootPath(key)
	_, uploadDomain, err := d.domains()
	if err != nil {
		return "", err
	}
	token, err := d.currentToken()
	if err != nil {
		return "", err
	}
	if err := d.uploadPart(ctx, uploadDomain, token, fullPath, uploadID, partSeq, data); err != nil {
		return "", err
	}
	return uploadID, nil
}

// CompleteChunkedUpload 合并完成上传（create）。
func (d *TeraBoxDriver) CompleteChunkedUpload(key string, uploadID string, size int64, blockMD5s []string) error {
	ctx, cancel := contextWithTimeout(60 * time.Second)
	defer cancel()
	fullPath := d.rootPath(key)
	blockListJSON, _ := json.Marshal(blockMD5s)

	createForm := url.Values{}
	createForm.Set("path", fullPath)
	createForm.Set("size", strconv.FormatInt(size, 10))
	createForm.Set("uploadid", uploadID)
	createForm.Set("block_list", string(blockListJSON))
	createForm.Set("rtype", "0")
	if _, err := d.teraboxCall(ctx, http.MethodPost, "", "/openapi/api/create", nil, createForm); err != nil {
		var apiErr *teraboxAPIError
		if errors.As(err, &apiErr) && apiErr.Errno == -8 {
			return fmt.Errorf("目标路径已存在同名文件: %w", err)
		}
		return fmt.Errorf("create 合并失败: %w", err)
	}
	return nil
}

// uploadPart 上传单个分片到 superfile2。
func (d *TeraBoxDriver) uploadPart(ctx context.Context, domain, token, fullPath, uploadID string, partSeq int, data []byte) error {
	query := url.Values{}
	query.Set("method", "upload")
	query.Set("app_id", strconv.Itoa(teraboxAppID))
	query.Set("path", fullPath)
	query.Set("uploadid", uploadID)
	query.Set("partseq", strconv.Itoa(partSeq))
	query.Set("access_tokens", token)
	u := url.URL{Scheme: "https", Host: domain, Path: "/rest/2.0/pcs/superfile2", RawQuery: query.Encode()}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "part")
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", "CloudreveEO")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var envelope struct {
		Errno int `json:"errno"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("解析分片响应失败: %s", truncate(string(body), 200))
	}
	if envelope.Errno != 0 {
		return &teraboxAPIError{Errno: envelope.Errno}
	}
	return nil
}

// GenerateUploadURL TeraBox 不支持浏览器预签名直传，由服务端中转上传。
func (d *TeraBoxDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return "", fmt.Errorf("TeraBox 存储不支持客户端直传，请使用服务端上传")
}

// GenerateDownloadURL 通过 filemetas 获取 dlink 并补上 access_tokens。
func (d *TeraBoxDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	if !d.IsAuthorized() {
		return "", errTeraBoxUnauthorized
	}
	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()

	// target 为 JSON 数组，路径中的 / 需编码
	escaped := strings.ReplaceAll(url.PathEscape(d.rootPath(key)), "%25", "%")
	targetJSON, _ := json.Marshal([]string{escaped})

	query := url.Values{}
	query.Set("target", string(targetJSON))
	query.Set("dlink", "1")
	raw, err := d.teraboxCall(ctx, http.MethodGet, "", "/openapi/api/filemetas", query, nil)
	if err != nil {
		return "", fmt.Errorf("获取下载链接失败: %w", err)
	}
	var result struct {
		Info []struct {
			Dlink string `json:"dlink"`
		} `json:"info"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("解析 filemetas 响应失败: %w", err)
	}
	if len(result.Info) == 0 || result.Info[0].Dlink == "" {
		return "", fmt.Errorf("TeraBox 未返回下载链接")
	}

	dlink, err := url.QueryUnescape(result.Info[0].Dlink)
	if err != nil {
		dlink = result.Info[0].Dlink
	}
	token, err := d.currentToken()
	if err != nil {
		return "", err
	}
	sep := "?"
	if strings.Contains(dlink, "?") {
		sep = "&"
	}
	return dlink + sep + "access_tokens=" + url.QueryEscape(token), nil
}

// Delete 通过 filemanager 删除文件。
func (d *TeraBoxDriver) Delete(key string) error {
	if !d.IsAuthorized() {
		return errTeraBoxUnauthorized
	}
	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()

	fileListJSON, _ := json.Marshal([]string{d.rootPath(key)})
	query := url.Values{}
	query.Set("opera", "delete")
	query.Set("async", "0")
	form := url.Values{}
	form.Set("filelist", string(fileListJSON))

	raw, err := d.teraboxCall(ctx, http.MethodPost, "", "/openapi/api/filemanager", query, form)
	if err != nil {
		var apiErr *teraboxAPIError
		if errors.As(err, &apiErr) && apiErr.Errno == -9 {
			return nil // 文件不存在视为删除成功
		}
		logx.Error(logx.ModuleStorage, "TeraBox 删除文件失败", logx.Err(err), "key", key)
		return err
	}

	// 检查逐项结果中的 errno
	var result struct {
		Info []struct {
			Errno int `json:"errno"`
		} `json:"info"`
	}
	if err := json.Unmarshal(raw, &result); err == nil {
		for _, item := range result.Info {
			if item.Errno == -9 {
				return nil
			}
			if item.Errno != 0 {
				return &teraboxAPIError{Errno: item.Errno}
			}
		}
	}
	logx.Info(logx.ModuleStorage, "TeraBox 文件已删除", "key", key)
	return nil
}

// fileMeta 通过 filemetas 查询文件元信息（size/fs_id）。
func (d *TeraBoxDriver) fileMeta(ctx context.Context, key string) (size int64, fsID int64, err error) {
	escaped := strings.ReplaceAll(url.PathEscape(d.rootPath(key)), "%25", "%")
	targetJSON, _ := json.Marshal([]string{escaped})
	query := url.Values{}
	query.Set("target", string(targetJSON))
	raw, err := d.teraboxCall(ctx, http.MethodGet, "", "/openapi/api/filemetas", query, nil)
	if err != nil {
		return 0, 0, err
	}
	var result struct {
		Info []struct {
			Size  int64 `json:"size"`
			FsID  int64 `json:"fs_id"`
			IsDir int   `json:"isdir"`
		} `json:"info"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, 0, fmt.Errorf("解析 filemetas 响应失败: %w", err)
	}
	if len(result.Info) == 0 {
		return 0, 0, fmt.Errorf("文件不存在")
	}
	return result.Info[0].Size, result.Info[0].FsID, nil
}

// GetSize 获取文件大小。
func (d *TeraBoxDriver) GetSize(key string) (int64, error) {
	if !d.IsAuthorized() {
		return 0, errTeraBoxUnauthorized
	}
	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()
	size, _, err := d.fileMeta(ctx, key)
	return size, err
}

// Read 打开文件内容流（文件夹打包下载用）。
func (d *TeraBoxDriver) Read(key string) (io.ReadCloser, error) {
	downloadURL, err := d.GenerateDownloadURL(key, "", 0)
	if err != nil {
		return nil, err
	}
	// 流的生命周期超出本函数，不用可取消上下文；整体受 client 超时约束。
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CloudreveEO")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("下载文件失败: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// InitMultipartUpload 不支持 S3 式分片直传；大文件走服务端中转（UploadFile）。
func (d *TeraBoxDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return "", fmt.Errorf("TeraBox 存储不支持客户端分片直传，请使用服务端上传")
}

// GenerateUploadPartURL 不支持。
func (d *TeraBoxDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return "", fmt.Errorf("TeraBox 存储不支持客户端分片直传，请使用服务端上传")
}

// CompleteMultipartUpload 不支持。
func (d *TeraBoxDriver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	return fmt.Errorf("TeraBox 存储不支持客户端分片直传，请使用服务端上传")
}

// AbortMultipartUpload 不支持。
func (d *TeraBoxDriver) AbortMultipartUpload(key string, uploadID string) error {
	return fmt.Errorf("TeraBox 存储不支持客户端分片直传，请使用服务端上传")
}

// ListUploadedParts 不支持。
func (d *TeraBoxDriver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	return nil, fmt.Errorf("TeraBox 存储不支持客户端分片直传，请使用服务端上传")
}

// SetBucketCORS TeraBox 无此概念。
func (d *TeraBoxDriver) SetBucketCORS() error {
	return ErrBucketCORSNotSupported
}

// truncate 截断过长文本用于日志/错误。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
