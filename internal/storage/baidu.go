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

// 百度网盘开放平台常量（接口定义与 github.com/lfhy/xpan SDK 一致）。
const (
	baiduAuthURL = "https://openapi.baidu.com/oauth/2.0/token"
	baiduPanURL  = "https://pan.baidu.com"
	baiduPCSURL  = "https://d.pcs.baidu.com"

	baiduFileRoute = "/rest/2.0/xpan/file"
	baiduMetaRoute = "/rest/2.0/xpan/multimedia"
	baiduPCSRoute  = "/rest/2.0/pcs/superfile2"

	// 开放平台要求分片 4MB~5GB；单分片上传可以更小，但 4MB 是文档建议值。
	baiduPartSize = 4 << 20
	// 百度最多 1024 分片，4MB/片 即单文件上限约 4GB；超过则加大分片。
	baiduMaxParts = 1024
	// superfile2 上传域名缓存不超过 1 小时（locateupload 返回的 expire 上限）。
	baiduUploadHostCacheTTL = 1 * time.Hour
	// token 剩余有效期低于该值时自动刷新（旧 token 刷新后 10 秒内仍可用，安全重叠）。
	baiduRefreshAhead = 30 * time.Minute
	// 默认存储根目录（网盘根目录下），避免污染用户网盘。
	baiduDefaultRootDir = "/apps/cloudreve-eo"

	// 开放平台限流：令牌桶 30 req/s（突发 60），429 时指数退避重试。
	baiduRatePerSecond = 30.0
	baiduRateBurst     = 60
	baiduMaxRetries    = 5
)

// BaiduToken 百度网盘 OAuth 凭据（序列化后持久化到策略的 oauth_token 字段）。
type BaiduToken struct {
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token"`
	AccessExpireAt int64  `json:"access_expire_at"` // Unix 秒
}

// BaiduDriver 使用百度网盘开放平台 API 实现存储驱动。
// 特点：无预签名直传，上传/下载全部经服务端中转（下载为临时 dlink，程序 GET 拉取）；
// 凭证为 OAuth access_token + refresh_token，由管理员在管理页授权获得。
type BaiduDriver struct {
	clientID     string
	clientSecret string
	redirectURI  string // 应用回调地址，须与开放平台配置一致
	rootDir      string // 网盘内存储根目录，如 /apps/cloudreve-eo

	// 服务地址（单测可替换为 httptest 服务器）。
	authURL string
	panURL  string
	pcsURL  string

	mu     sync.Mutex
	token  BaiduToken
	loaded bool // token 是否已从策略载入（空 token 待授权）

	refreshMu sync.Mutex // 串行化 token 刷新（singleflight）

	hostMu     sync.Mutex
	uploadHost string
	hostAt     time.Time

	// dirCache 已确认存在的目录（避免每次上传都 list 查目录，节省 QPS）。
	dirCache sync.Map
	// fsidCache 路径 → fs_id（预览/下载重复取 dlink 时免去 list，节省 QPS）。
	fsidCache sync.Map
	// dlinkCache 下载链接短期缓存（TTL 1 小时）：Range 分段下载的多个请求共享，
	// 避免每段都调 filemetas 触发开放平台限流。
	dlinkMu    sync.Mutex
	dlinkCache map[string]baiduDlinkEntry

	limiter *baiduLimiter
	client  *http.Client
	// onTokenRefreshed token 刷新后的持久化回调（写入策略 oauth_token 字段）。
	onTokenRefreshed func(BaiduToken)
	// proxyURL 生成带签名的服务端代理下载 URL（由 manager 注入）。
	proxyURL func(storageKey, attachment string) (string, error)
}

// NewBaiduDriver 创建百度网盘驱动。
// endpoint 复用为 OAuth 回调地址（redirect_uri，可空走默认 oob）；
// basePath 复用为网盘内存储根目录；tokenJSON 为已授权凭据（可空，待授权）。
func NewBaiduDriver(clientID, clientSecret, endpoint, basePath, tokenJSON string) (*BaiduDriver, error) {
	if clientID == "" {
		return nil, fmt.Errorf("百度网盘 AppKey 不能为空")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("百度网盘 SecretKey 不能为空")
	}

	d := &BaiduDriver{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURI:  strings.TrimSpace(endpoint),
		rootDir:      normalizeBaiduRootDir(basePath),
		authURL:      baiduAuthURL,
		panURL:       baiduPanURL,
		pcsURL:       baiduPCSURL,
		limiter:      newBaiduLimiter(baiduRatePerSecond, baiduRateBurst),
		dlinkCache:   make(map[string]baiduDlinkEntry),
		// 分片上传单片 4MB、单文件可能数 GB，整体超时放宽到 30 分钟
		client: &http.Client{Timeout: 30 * time.Minute},
	}

	tokenJSON = strings.TrimSpace(tokenJSON)
	if tokenJSON == "" {
		// 未授权：驱动可加载，但需管理员先完成授权才能读写文件。
		return d, nil
	}
	var token BaiduToken
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil || token.AccessToken == "" {
		return nil, fmt.Errorf("百度网盘授权凭据格式错误，请重新授权")
	}
	d.token = token
	d.loaded = true
	return d, nil
}

// normalizeBaiduRootDir 规范化网盘存储根目录：补全首部斜杠、去掉尾部斜杠。
func normalizeBaiduRootDir(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return baiduDefaultRootDir
	}
	p = strings.Trim(p, "/")
	return "/" + p
}

// IsAuthorized 驱动是否已持有可用授权。
func (d *BaiduDriver) IsAuthorized() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loaded && d.token.AccessToken != ""
}

// errBaiduUnauthorized 未授权错误（管理页提示去授权）。
var errBaiduUnauthorized = errors.New("百度网盘尚未授权，请到「存储策略」完成授权")

// errBaiduNotFound 远端文件不存在。
var errBaiduNotFound = errors.New("文件不存在")

// baiduAPIError 百度网盘返回 errno != 0。
type baiduAPIError struct {
	Errno     int
	ErrorCode int
	ErrMsg    string
}

func (e *baiduAPIError) Error() string {
	if e.ErrMsg != "" {
		return fmt.Sprintf("百度网盘 API 错误(errno=%d): %s", e.Errno, e.ErrMsg)
	}
	return fmt.Sprintf("百度网盘 API 错误(errno=%d)", e.Errno)
}

// baiduAuthErrMsg 常见 OAuth 错误码的中文说明。
func baiduAuthErrMsg(errorCode int, msg string) string {
	switch errorCode {
	case 110:
		return "access_token 无效或已过期，请重新授权"
	case 111:
		return "refresh_token 无效或已过期，请重新授权"
	case 26:
		return "应用未上线或未通过审核"
	case 3:
		return "参数错误：请核对 AppKey / SecretKey / 回调地址"
	}
	if msg != "" {
		return msg
	}
	return "未知错误"
}

// contextWithTimeoutFor 带超时的后台上下文（避免与 terabox.go 的 contextWithTimeout 重名）。
func baiduContext(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// requestToken 调用 OAuth token 端点（授权码换 token / 刷新 token 共用）。
func (d *BaiduDriver) requestToken(ctx context.Context, params url.Values) error {
	q := url.Values{}
	q.Set("grant_type", params.Get("grant_type"))
	q.Set("client_id", d.clientID)
	q.Set("client_secret", d.clientSecret)
	if code := params.Get("code"); code != "" {
		q.Set("code", code)
	}
	if refresh := params.Get("refresh_token"); refresh != "" {
		q.Set("refresh_token", refresh)
	}
	redirect := d.redirectURI
	if redirect == "" {
		redirect = "oob"
	}
	q.Set("redirect_uri", redirect)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.authURL+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "CloudreveEO")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var envelope struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int64  `json:"expires_in"`
		ErrorCode        string `json:"error"`
		ErrorDescription string `json:"error_description"`
		ErrorNum         int    `json:"error_code"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("解析百度网盘授权响应失败: %s", truncate(string(body), 200))
	}
	if envelope.ErrorCode != "" || envelope.AccessToken == "" {
		return fmt.Errorf("百度网盘授权失败: %s", baiduAuthErrMsg(envelope.ErrorNum, envelope.ErrorDescription))
	}

	expiresIn := envelope.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 30 * 24 * 3600
	}
	token := BaiduToken{
		AccessToken:    envelope.AccessToken,
		RefreshToken:   envelope.RefreshToken,
		AccessExpireAt: time.Now().Unix() + expiresIn,
	}
	d.mu.Lock()
	d.token = token
	d.loaded = true
	d.mu.Unlock()

	// 上传域名可能随账号区域变化，清缓存强制重取。
	d.hostMu.Lock()
	d.uploadHost = ""
	d.hostMu.Unlock()

	if d.onTokenRefreshed != nil {
		d.onTokenRefreshed(token)
	}
	return nil
}

// GetTokenByCode 用授权码换取 token。成功后立即持久化。
func (d *BaiduDriver) GetTokenByCode(code string) error {
	ctx, cancel := baiduContext(30 * time.Second)
	defer cancel()
	params := url.Values{}
	params.Set("grant_type", "authorization_code")
	params.Set("code", code)
	return d.requestToken(ctx, params)
}

// RefreshToken 用 refresh_token 刷新 access_token。成功后立即持久化。
func (d *BaiduDriver) RefreshToken() error {
	d.mu.Lock()
	refresh := d.token.RefreshToken
	d.mu.Unlock()
	if refresh == "" {
		return fmt.Errorf("无 refresh_token，请重新授权")
	}
	ctx, cancel := baiduContext(30 * time.Second)
	defer cancel()
	params := url.Values{}
	params.Set("grant_type", "refresh_token")
	params.Set("refresh_token", refresh)
	return d.requestToken(ctx, params)
}

// currentToken 返回未过期的 access_token；临近过期时先用 refresh_token 自动刷新，
// 并发的刷新请求只执行一次（singleflight）。
func (d *BaiduDriver) currentToken() (string, error) {
	d.mu.Lock()
	if !d.loaded || d.token.AccessToken == "" {
		d.mu.Unlock()
		return "", errBaiduUnauthorized
	}
	expireAt := d.token.AccessExpireAt
	d.mu.Unlock()

	if time.Now().Unix()+int64(baiduRefreshAhead/time.Second) >= expireAt {
		d.refreshOnce()
		d.mu.Lock()
		expireAt = d.token.AccessExpireAt
		d.mu.Unlock()
	}
	if time.Now().Unix() >= expireAt {
		return "", fmt.Errorf("百度网盘 token 已过期且刷新失败，请到「存储策略」重新授权")
	}

	d.mu.Lock()
	token := d.token.AccessToken
	loaded := d.loaded
	d.mu.Unlock()
	if !loaded || token == "" {
		return "", errBaiduUnauthorized
	}
	return token, nil
}

// refreshMu 定义在结构体字段；refreshOnce 保证同一时刻只有一个刷新在执行。
func (d *BaiduDriver) refreshOnce() {
	d.refreshMu.Lock()
	defer d.refreshMu.Unlock()
	// 双检：等到锁后可能已被其它协程刷新过。
	d.mu.Lock()
	expireAt := d.token.AccessExpireAt
	d.mu.Unlock()
	if time.Now().Unix()+int64(baiduRefreshAhead/time.Second) < expireAt {
		return
	}
	if err := d.RefreshToken(); err != nil {
		// 刷新失败但旧 token 仍在有效期内时继续用旧 token。
		if time.Now().Unix() < expireAt {
			logx.Warn(logx.ModuleStorage, "百度网盘 token 刷新失败，暂用旧 token", "err", err.Error())
		} else {
			logx.Error(logx.ModuleStorage, "百度网盘 token 已过期且刷新失败", logx.Err(err))
		}
	}
}

// baiduLimiter 简单令牌桶限流器（开放平台有 QPS 限制，批量操作必须限流）。
type baiduLimiter struct {
	mu       sync.Mutex
	capacity float64
	tokens   float64
	rate     float64 // 每秒补充速率
	last     time.Time
}

func newBaiduLimiter(perSecond float64, burst int) *baiduLimiter {
	return &baiduLimiter{
		capacity: float64(burst),
		tokens:   float64(burst),
		rate:     perSecond,
		last:     time.Now(),
	}
}

// Wait 阻塞直到获得一个令牌。
func (l *baiduLimiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens += now.Sub(l.last).Seconds() * l.rate
		if l.tokens > l.capacity {
			l.tokens = l.capacity
		}
		l.last = now
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		need := (1 - l.tokens) / l.rate
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(need*float64(time.Second)) + time.Millisecond):
		}
	}
}

// baiduCall 调用 pan.baidu.com 业务 API：自动限流、token 注入、
// token 失效刷新重试一次、429/5xx 指数退避重试。
// method 为 GET/POST；form 非空时作为 x-www-form-urlencoded 请求体。
func (d *BaiduDriver) baiduCall(ctx context.Context, method, base, route, apiMethod string, query url.Values, form url.Values) (json.RawMessage, error) {
	var lastErr error
	retriedToken := false
	for attempt := 0; attempt <= baiduMaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		if err := d.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		body, statusCode, err := d.doBaiduRequest(ctx, method, base, route, apiMethod, query, form)
		if err != nil {
			return nil, err
		}

		if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
			lastErr = fmt.Errorf("百度网盘服务异常: HTTP %d", statusCode)
			logx.Warn(logx.ModuleStorage, "百度网盘请求被限流或服务异常，准备重试",
				"status", statusCode, "attempt", attempt+1, "api", apiMethod)
			continue
		}

		var envelope struct {
			Errno     int    `json:"errno"`
			ErrMsg    string `json:"errmsg"`
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("解析百度网盘响应失败: %s", truncate(string(body), 200))
		}
		if envelope.ErrorCode == 110 || envelope.ErrorCode == 111 {
			if !retriedToken {
				retriedToken = true
				logx.Info(logx.ModuleStorage, "百度网盘 access_token 失效，刷新后重试", "api", apiMethod)
				d.mu.Lock()
				d.token.AccessExpireAt = 0 // 强制刷新
				d.mu.Unlock()
				attempt-- // token 刷新重试不计入退避次数
				continue
			}
			return nil, fmt.Errorf("百度网盘 access_token 无效且刷新失败: %s", baiduAuthErrMsg(envelope.ErrorCode, envelope.ErrorMsg))
		}
		if envelope.Errno != 0 || envelope.ErrorCode != 0 {
			return nil, &baiduAPIError{Errno: envelope.Errno, ErrorCode: envelope.ErrorCode, ErrMsg: firstNonEmpty(envelope.ErrMsg, envelope.ErrorMsg)}
		}
		return body, nil
	}
	return nil, fmt.Errorf("百度网盘请求重试次数耗尽: %w", lastErr)
}

// doBaiduRequest 执行单次 HTTP 请求，返回响应体与状态码。
func (d *BaiduDriver) doBaiduRequest(ctx context.Context, method, base, route, apiMethod string, query url.Values, form url.Values) ([]byte, int, error) {
	token, err := d.currentToken()
	if err != nil {
		return nil, 0, err
	}
	q := url.Values{}
	q.Set("method", apiMethod)
	q.Set("access_token", token)
	for k, vs := range query {
		for _, v := range vs {
			q.Add(k, v)
		}
	}

	var bodyReader io.Reader
	if form != nil {
		bodyReader = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, base+route+"?"+q.Encode(), bodyReader)
	if err != nil {
		return nil, 0, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", "pan.baidu.com")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return body, resp.StatusCode, nil
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// rootPath 将相对 key 拼到存储根目录下（/apps/cloudreve-eo/xxx）。
func (d *BaiduDriver) rootPath(key string) string {
	return d.rootDir + "/" + strings.TrimPrefix(strings.TrimSpace(key), "/")
}

// ensureParentDirs 逐级 mkdir 保证文件父目录存在（method=create 不会自动建目录）。
func (d *BaiduDriver) ensureParentDirs(ctx context.Context, fullPath string) error {
	idx := strings.LastIndex(fullPath, "/")
	if idx <= 0 {
		return nil // 根目录
	}
	parent := fullPath[:idx]
	// 自顶向下收集缺失目录
	var missing []string
	cur := parent
	for cur != "" && cur != "/" {
		if d.dirExists(ctx, cur) {
			break
		}
		missing = append(missing, cur)
		cur = cur[:strings.LastIndex(cur, "/")]
	}
	// 从最浅层开始创建
	for i := len(missing) - 1; i >= 0; i-- {
		form := url.Values{}
		form.Set("path", missing[i])
		form.Set("isdir", "1")
		form.Set("rtype", "0")
		if _, err := d.baiduCall(ctx, http.MethodPost, d.panURL, baiduFileRoute, "create", nil, form); err != nil {
			var apiErr *baiduAPIError
			// errno -8 / 12 表示目录已存在，可忽略
			if errors.As(err, &apiErr) && (apiErr.Errno == -8 || apiErr.Errno == 12) {
				d.dirCache.Store(missing[i], true)
				continue
			}
			return fmt.Errorf("创建目录 %s 失败: %w", missing[i], err)
		}
		d.dirCache.Store(missing[i], true)
	}
	return nil
}

// dirExists 检查目录是否存在（list 一次即可；命中过的目录记入缓存）。
func (d *BaiduDriver) dirExists(ctx context.Context, dir string) bool {
	if dir == "/" {
		return true
	}
	if _, ok := d.dirCache.Load(dir); ok {
		return true
	}
	query := url.Values{}
	query.Set("dir", dir)
	query.Set("limit", "1")
	if _, err := d.baiduCall(ctx, http.MethodGet, d.panURL, baiduFileRoute, "list", query, nil); err != nil {
		return false
	}
	d.dirCache.Store(dir, true)
	return true
}

// locateUploadHost 获取并缓存 superfile2 上传域名（locateupload 接口）。
func (d *BaiduDriver) locateUploadHost(ctx context.Context, fullPath, uploadID string) (string, error) {
	d.hostMu.Lock()
	if d.uploadHost != "" && time.Since(d.hostAt) < baiduUploadHostCacheTTL {
		host := d.uploadHost
		d.hostMu.Unlock()
		return host, nil
	}
	d.hostMu.Unlock()

	query := url.Values{}
	query.Set("appid", "250528")
	query.Set("upload_version", "2.0")
	query.Set("path", fullPath)
	if uploadID != "" {
		query.Set("uploadid", uploadID)
	}
	raw, err := d.baiduCall(ctx, http.MethodGet, d.pcsURL, "/rest/2.0/pcs/file", "locateupload", query, nil)
	if err != nil {
		return "", fmt.Errorf("获取上传域名失败: %w", err)
	}
	var res struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.Host == "" {
		return "", fmt.Errorf("locateupload 未返回上传域名")
	}

	d.hostMu.Lock()
	d.uploadHost = res.Host
	d.hostAt = time.Now()
	d.hostMu.Unlock()
	return res.Host, nil
}

// UploadFile 分片上传：precreate → 逐片 superfile2 → create 合并。
// 开放平台要求大文件必须分片上传，直接整体传会失败。
func (d *BaiduDriver) UploadFile(key string, content []byte) error {
	ctx, cancel := baiduContext(30 * time.Minute)
	defer cancel()
	fullPath := d.rootPath(key)

	if err := d.ensureParentDirs(ctx, fullPath); err != nil {
		return err
	}

	partSize := baiduPartSize
	if count := (len(content) + partSize - 1) / partSize; count > baiduMaxParts {
		// 超出分片数上限时加大分片（如 8GB → 8MB/片）
		partSize = (len(content) + baiduMaxParts - 1) / baiduMaxParts
	}
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
	blockListJSON, _ := json.Marshal(blockMD5s)

	// 1. precreate
	form := url.Values{}
	form.Set("path", fullPath)
	form.Set("size", strconv.Itoa(len(content)))
	form.Set("isdir", "0")
	form.Set("autoinit", "1")
	form.Set("rtype", "3") // 同名覆盖（重试上传幂等）
	form.Set("block_list", string(blockListJSON))
	raw, err := d.baiduCall(ctx, http.MethodPost, d.panURL, baiduFileRoute, "precreate", nil, form)
	if err != nil {
		return fmt.Errorf("precreate 失败: %w", err)
	}
	var pre struct {
		ReturnType int    `json:"return_type"`
		UploadID   string `json:"uploadid"`
		BlockList  []int  `json:"block_list"`
	}
	if err := json.Unmarshal(raw, &pre); err != nil {
		return fmt.Errorf("解析 precreate 响应失败: %w", err)
	}
	if pre.ReturnType == 2 {
		// 云端秒传命中，上传已完成
		logx.Info(logx.ModuleStorage, "百度网盘秒传命中", "key", key)
		return nil
	}
	if pre.UploadID == "" {
		return fmt.Errorf("precreate 未返回 uploadid")
	}
	if len(pre.BlockList) == 0 {
		pre.BlockList = []int{0}
	}

	// 2. 逐片上传 superfile2
	host, err := d.locateUploadHost(ctx, fullPath, pre.UploadID)
	if err != nil {
		return err
	}
	for _, seq := range pre.BlockList {
		start := seq * partSize
		if start >= len(content) {
			start = len(content)
		}
		end := start + partSize
		if end > len(content) {
			end = len(content)
		}
		if err := d.uploadPart(ctx, host, fullPath, pre.UploadID, seq, content[start:end]); err != nil {
			return fmt.Errorf("上传分片 %d 失败: %w", seq, err)
		}
	}

	// 3. create 合并
	createForm := url.Values{}
	createForm.Set("path", fullPath)
	createForm.Set("size", strconv.Itoa(len(content)))
	createForm.Set("isdir", "0")
	createForm.Set("uploadid", pre.UploadID)
	createForm.Set("block_list", string(blockListJSON))
	createForm.Set("rtype", "3")
	if _, err := d.baiduCall(ctx, http.MethodPost, d.panURL, baiduFileRoute, "create", nil, createForm); err != nil {
		return fmt.Errorf("create 合并失败: %w", err)
	}
	logx.Info(logx.ModuleStorage, "百度网盘上传完成", "key", key, "size", len(content), "parts", len(pre.BlockList))
	return nil
}

// uploadPart 上传单个分片到 superfile2（带限流与重试）。
func (d *BaiduDriver) uploadPart(ctx context.Context, host, fullPath, uploadID string, partSeq int, data []byte) error {
	var lastErr error
	for attempt := 0; attempt <= baiduMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<uint(attempt-1)) * time.Second):
			}
		}
		if err := d.limiter.Wait(ctx); err != nil {
			return err
		}

		token, err := d.currentToken()
		if err != nil {
			return err
		}
		query := url.Values{}
		query.Set("method", "upload")
		query.Set("type", "tmpfile")
		query.Set("path", fullPath)
		query.Set("uploadid", uploadID)
		query.Set("partseq", strconv.Itoa(partSeq))
		query.Set("access_token", token)

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

		u := strings.TrimSuffix(host, "/") + baiduPCSRoute + "?" + query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("User-Agent", "pan.baidu.com")

		resp, err := d.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			logx.Warn(logx.ModuleStorage, "百度网盘分片上传被限流，准备重试",
				"status", resp.StatusCode, "part", partSeq, "attempt", attempt+1)
			continue
		}
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
			return &baiduAPIError{Errno: envelope.Errno}
		}
		return nil
	}
	return fmt.Errorf("分片上传重试次数耗尽: %w", lastErr)
}

// GenerateUploadURL 百度网盘不支持浏览器预签名直传，由服务端中转上传。
func (d *BaiduDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return "", fmt.Errorf("百度网盘存储不支持客户端直传，请使用服务端上传")
}

// GenerateDownloadURL 返回带签名的服务端代理下载 URL。
// 百度 dlink 是临时短链（约 8 小时过期且与服务器 IP/UA 绑定），
// 不能直接交给浏览器，必须经服务端 GET 拉取后中转。
func (d *BaiduDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	if !d.IsAuthorized() {
		return "", errBaiduUnauthorized
	}
	if d.proxyURL == nil {
		return "", fmt.Errorf("百度网盘代理下载未配置")
	}
	return d.proxyURL(key, fileName)
}

// baiduDlinkCacheTTL 下载链接缓存时长（dlink 约 8 小时有效，留足余量）。
const baiduDlinkCacheTTL = 1 * time.Hour

// baiduDlinkEntry 缓存的下载链接与文件大小。
type baiduDlinkEntry struct {
	dlink string
	size  int64
	at    time.Time
}

// fileMeta 返回文件元信息（大小 + 下载链接），dlink 走短期缓存（Range 分段共享）。
func (d *BaiduDriver) fileMeta(ctx context.Context, key string) (size int64, dlink string, err error) {
	fullPath := d.rootPath(key)

	d.dlinkMu.Lock()
	entry, ok := d.dlinkCache[fullPath]
	d.dlinkMu.Unlock()
	if ok && time.Since(entry.at) < baiduDlinkCacheTTL {
		return entry.size, entry.dlink, nil
	}

	size, dl, err := d.fetchFileMeta(ctx, fullPath)
	if err != nil {
		return 0, "", err
	}
	d.dlinkMu.Lock()
	// 顺手清理过期条目，防止长期运行后条目堆积
	if len(d.dlinkCache) > 64 {
		for k, v := range d.dlinkCache {
			if time.Since(v.at) >= baiduDlinkCacheTTL {
				delete(d.dlinkCache, k)
			}
		}
	}
	d.dlinkCache[fullPath] = baiduDlinkEntry{dlink: dl, size: size, at: time.Now()}
	d.dlinkMu.Unlock()
	return size, dl, nil
}

// forgetDlink 清除文件的下载链接缓存（链接失效/文件删除时调用）。
func (d *BaiduDriver) forgetDlink(key string) {
	d.dlinkMu.Lock()
	delete(d.dlinkCache, d.rootPath(key))
	d.dlinkMu.Unlock()
}

// fetchFileMeta 通过 filemetas 查询文件元信息（需先经 list 拿到 fs_id）。
// fsid 缓存失效（文件被删后重建）时自动清缓存重查一次。
func (d *BaiduDriver) fetchFileMeta(ctx context.Context, fullPath string) (size int64, dlink string, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		fsID, err := d.lookupFsID(ctx, fullPath)
		if err != nil {
			return 0, "", err
		}
		fsIDsJSON, _ := json.Marshal([]uint64{fsID})
		query := url.Values{}
		query.Set("fsids", string(fsIDsJSON))
		query.Set("dlink", "1")
		raw, err := d.baiduCall(ctx, http.MethodGet, d.panURL, baiduMetaRoute, "filemetas", query, nil)
		if err != nil {
			return 0, "", err
		}
		var result struct {
			List []struct {
				Size  int64  `json:"size"`
				Dlink string `json:"dlink"`
				IsDir int    `json:"isdir"`
			} `json:"list"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return 0, "", fmt.Errorf("解析 filemetas 响应失败: %w", err)
		}
		if len(result.List) > 0 {
			return result.List[0].Size, result.List[0].Dlink, nil
		}
		if attempt == 0 {
			// fs_id 已失效，清缓存后重查
			d.fsidCache.Delete(fullPath)
			continue
		}
	}
	return 0, "", errBaiduNotFound
}

// lookupFsID 在文件所在目录中按路径精确匹配拿到 fs_id（结果缓存）。
func (d *BaiduDriver) lookupFsID(ctx context.Context, fullPath string) (uint64, error) {
	if cached, ok := d.fsidCache.Load(fullPath); ok {
		return cached.(uint64), nil
	}
	idx := strings.LastIndex(fullPath, "/")
	if idx <= 0 {
		return 0, errBaiduNotFound
	}
	dir, name := fullPath[:idx], fullPath[idx+1:]
	if dir == "" {
		dir = "/"
	}
	start := 0
	for {
		query := url.Values{}
		query.Set("dir", dir)
		query.Set("start", strconv.Itoa(start))
		query.Set("limit", "1000")
		raw, err := d.baiduCall(ctx, http.MethodGet, d.panURL, baiduFileRoute, "list", query, nil)
		if err != nil {
			return 0, err
		}
		var res struct {
			List []struct {
				ServerFilename string `json:"server_filename"`
				Path           string `json:"path"`
				FsID           uint64 `json:"fs_id"`
			} `json:"list"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return 0, fmt.Errorf("解析 list 响应失败: %w", err)
		}
		for _, item := range res.List {
			if item.ServerFilename == name || item.Path == fullPath {
				d.fsidCache.Store(fullPath, item.FsID)
				return item.FsID, nil
			}
		}
		if len(res.List) < 1000 {
			break
		}
		start += len(res.List)
	}
	return 0, errBaiduNotFound
}

// GetSize 获取文件大小。
func (d *BaiduDriver) GetSize(key string) (int64, error) {
	if !d.IsAuthorized() {
		return 0, errBaiduUnauthorized
	}
	ctx, cancel := baiduContext(30 * time.Second)
	defer cancel()
	size, _, err := d.fileMeta(ctx, key)
	return size, err
}

// Read 打开文件内容流：filemetas 拿临时 dlink，服务端 GET 拉取（文件夹打包下载用）。
func (d *BaiduDriver) Read(key string) (io.ReadCloser, error) {
	return d.ReadRange(key, 0, -1)
}

// ReadRange 按字节区间读取文件内容（start/end 闭区间；end=-1 表示读到文件末尾）。
// 大文件 Range 分段下载时，浏览器/下载工具按段请求，每段一次独立的短函数执行；
// dlink 走缓存，分段请求共享同一链接，不重复消耗开放平台 QPS。
// 百度要求下载时 UA 与获取 dlink 时一致，固定为 pan.baidu.com。
func (d *BaiduDriver) ReadRange(key string, start, end int64) (io.ReadCloser, error) {
	if !d.IsAuthorized() {
		return nil, errBaiduUnauthorized
	}
	if start < 0 {
		return nil, fmt.Errorf("无效的 Range：start 不能为负")
	}
	if end >= 0 && end < start {
		return nil, fmt.Errorf("无效的 Range：end 小于 start")
	}

	// dlink 可能过期（约 8 小时）：首次失败且疑似链接失效时清缓存重试一次
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		rc, err := d.openDlinkRange(key, start, end)
		if err == nil {
			return rc, nil
		}
		lastErr = err
		if !isBaiduDlinkExpired(err) {
			return nil, err
		}
		d.forgetDlink(key)
	}
	return nil, lastErr
}

// openDlinkRange 取 dlink 并发起（可选带 Range 头的）下载请求。
func (d *BaiduDriver) openDlinkRange(key string, start, end int64) (io.ReadCloser, error) {
	ctx, cancel := baiduContext(30 * time.Second)
	defer cancel()
	size, dlink, err := d.fileMeta(ctx, key)
	if err != nil {
		return nil, err
	}
	if dlink == "" {
		return nil, fmt.Errorf("百度网盘未返回下载链接")
	}
	if start >= size {
		return nil, ErrRangeNotSatisfiable
	}

	token, err := d.currentToken()
	if err != nil {
		return nil, err
	}
	sep := "?"
	if strings.Contains(dlink, "?") {
		sep = "&"
	}
	downloadURL := dlink + sep + "access_token=" + url.QueryEscape(token)

	// 流的生命周期超出本函数；整体受 client 超时约束
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pan.baidu.com")
	if start > 0 || end >= 0 {
		rangeEnd := end
		if rangeEnd < 0 || rangeEnd >= size {
			rangeEnd = size - 1
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, rangeEnd))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, &baiduDlinkError{
			status: resp.StatusCode,
			body:   string(body),
		}
	}
	return resp.Body, nil
}

// baiduDlinkError dlink 下载失败；记录状态码用于判断链接是否失效。
type baiduDlinkError struct {
	status int
	body   string
}

func (e *baiduDlinkError) Error() string {
	return fmt.Sprintf("下载文件失败: HTTP %d %s", e.status, truncate(e.body, 200))
}

// isBaiduDlinkExpired 判断错误是否为 dlink 失效（需清缓存重取）。
// 百度 dlink 过期/鉴权失败常见返回 403（errno -20）、404、410。
func isBaiduDlinkExpired(err error) bool {
	var de *baiduDlinkError
	if errors.As(err, &de) {
		return de.status == http.StatusForbidden ||
			de.status == http.StatusNotFound ||
			de.status == http.StatusGone ||
			strings.Contains(de.body, "errno=-20") ||
			strings.Contains(de.body, "errno%3D-20")
	}
	return false
}

// Delete 通过 filemanager 删除文件（带限流与重试）。
func (d *BaiduDriver) Delete(key string) error {
	if !d.IsAuthorized() {
		return errBaiduUnauthorized
	}
	ctx, cancel := baiduContext(60 * time.Second)
	defer cancel()

	fileListJSON, _ := json.Marshal([]string{d.rootPath(key)})
	query := url.Values{}
	query.Set("opera", "delete")
	query.Set("async", "0")
	form := url.Values{}
	form.Set("filelist", string(fileListJSON))

	fullPath := d.rootPath(key)
	raw, err := d.baiduCall(ctx, http.MethodPost, d.panURL, baiduFileRoute, "filemanager", query, form)
	if err != nil {
		var apiErr *baiduAPIError
		// errno -9：文件不存在，视为删除成功
		if errors.As(err, &apiErr) && apiErr.Errno == -9 {
			d.fsidCache.Delete(fullPath)
			d.forgetDlink(key)
			return nil
		}
		logx.Error(logx.ModuleStorage, "百度网盘删除文件失败", logx.Err(err), "key", key)
		return err
	}

	var result struct {
		List []struct {
			Errno int `json:"errno"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &result); err == nil {
		for _, item := range result.List {
			if item.Errno == -9 {
				d.fsidCache.Delete(fullPath)
				d.forgetDlink(key)
				return nil
			}
			if item.Errno != 0 {
				return &baiduAPIError{Errno: item.Errno}
			}
		}
	}
	d.fsidCache.Delete(fullPath)
	d.forgetDlink(key)
	logx.Info(logx.ModuleStorage, "百度网盘文件已删除", "key", key)
	return nil
}

// InitMultipartUpload 不支持 S3 式分片直传；大文件走服务端中转（UploadFile 内部自动分片）。
func (d *BaiduDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return "", fmt.Errorf("百度网盘存储不支持客户端分片直传，请使用服务端上传")
}

// GenerateUploadPartURL 不支持。
func (d *BaiduDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return "", fmt.Errorf("百度网盘存储不支持客户端分片直传，请使用服务端上传")
}

// CompleteMultipartUpload 不支持。
func (d *BaiduDriver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	return fmt.Errorf("百度网盘存储不支持客户端分片直传，请使用服务端上传")
}

// AbortMultipartUpload 不支持。
func (d *BaiduDriver) AbortMultipartUpload(key string, uploadID string) error {
	return fmt.Errorf("百度网盘存储不支持客户端分片直传，请使用服务端上传")
}

// ListUploadedParts 不支持。
func (d *BaiduDriver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	return nil, fmt.Errorf("百度网盘存储不支持客户端分片直传，请使用服务端上传")
}

// SetBucketCORS 百度网盘无此概念。
func (d *BaiduDriver) SetBucketCORS() error {
	return ErrBucketCORSNotSupported
}
