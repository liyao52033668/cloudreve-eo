package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Google Drive 单文件直传上限为 5MB；超过则走分片上传会话。
const gdriveSessionThreshold = 5 << 20

// Google Drive token 剩余有效期低于该值时自动刷新。
const gdriveRefreshAhead = 10 * time.Minute

// GDriveToken Google Drive OAuth 凭据（序列化后持久化到策略的 oauth_token 字段）。
type GDriveToken struct {
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token"`
	AccessExpireAt int64  `json:"access_expire_at"` // Unix 秒
}

// GDriveDriver 使用 Google Drive API v3 实现存储驱动。
// 凭证为 OAuth access_token + refresh_token，由管理员在管理页授权获得。
// 下载/预览使用 Google Drive 生成的临时下载链接（约 1 小时有效），无需服务端代理。
type GDriveDriver struct {
	clientID     string // Google OAuth Client ID
	clientSecret string // Google OAuth Client Secret
	basePath     string // 存储路径前缀（相对 Google Drive 根目录）

	mu     sync.Mutex
	token  GDriveToken
	loaded bool // token 是否已从策略载入（空 token 待授权）

	client *http.Client
	// onTokenRefreshed token 刷新后的持久化回调（写入策略 oauth_token 字段）。
	onTokenRefreshed func(GDriveToken)

	proxyEnabled bool
	proxyURL     string // HTTP 代理地址
}

// NewGDriveDriver 创建 Google Drive 驱动。
// clientID=OAuth Client ID, clientSecret=OAuth Client Secret；tokenJSON 为已授权凭据（可空，待授权）。
// proxyEnabled/proxyURL 控制是否使用代理及代理地址。
func NewGDriveDriver(clientID, clientSecret, basePath, tokenJSON string, proxyEnabled bool, proxyURL string) (*GDriveDriver, error) {
	if clientID == "" {
		return nil, fmt.Errorf("Google Drive Client ID 不能为空")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("Google Drive Client Secret 不能为空")
	}

	d := &GDriveDriver{
		clientID:     clientID,
		clientSecret: clientSecret,
		basePath:     "", // basePath 已由 buildStorageKey 拼入 key，驱动不再使用
		proxyEnabled: proxyEnabled,
		proxyURL:     proxyURL,
	}

	tokenJSON = strings.TrimSpace(tokenJSON)
	if tokenJSON == "" {
		// 未授权：驱动可加载，但需管理员先完成授权才能读写文件。
		return d, nil
	}
	var token GDriveToken
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil || token.AccessToken == "" {
		return nil, fmt.Errorf("Google Drive 授权凭据格式错误，请重新授权")
	}
	d.token = token
	d.loaded = true

	// 初始化 HTTP 客户端
	d.initClient()

	return d, nil
}

// initClient 初始化 HTTP 客户端。
func (d *GDriveDriver) initClient() {
	var transport http.RoundTripper
	if d.proxyEnabled {
		transport = buildHTTPTransport(d.proxyEnabled, d.proxyURL)
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	d.client = &http.Client{
		Transport: transport,
		Timeout:   5 * time.Minute,
	}
}

// IsConfigured Client ID/Secret 是否齐备。
func (d *GDriveDriver) IsConfigured() bool {
	return d.clientID != "" && d.clientSecret != ""
}

// IsAuthorized 驱动是否已持有可用授权。
func (d *GDriveDriver) IsAuthorized() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loaded && d.token.AccessToken != ""
}

// errGDriveUnauthorized 未授权错误（管理页提示去授权）。
var errGDriveUnauthorized = errors.New("Google Drive 尚未授权，请到「存储策略」完成授权")

// gdrivePathOf 由对象键得到 Google Drive 内完整路径（相对根目录）。
// key 已由 buildStorageKey 拼入 basePath，驱动不再重复拼接。
func (d *GDriveDriver) gdrivePathOf(key string) string {
	return strings.TrimPrefix(key, "/")
}

// GetAuthURL 生成 OAuth 授权 URL。
func (d *GDriveDriver) GetAuthURL(redirectURI string) string {
	cfg := &oauth2.Config{
		ClientID:     d.clientID,
		ClientSecret: d.clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURI,
		Scopes:       []string{"https://www.googleapis.com/auth/drive.file"},
	}
	// access_type=offline 确保返回 refresh_token
	return cfg.AuthCodeURL("", oauth2.AccessTypeOffline)
}

// GetTokenByCode 用授权码换取 token（授权码模式）。成功后立即持久化。
func (d *GDriveDriver) GetTokenByCode(code, redirectURI string) error {
	cfg := &oauth2.Config{
		ClientID:     d.clientID,
		ClientSecret: d.clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURI,
		Scopes:       []string{"https://www.googleapis.com/auth/drive.file"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("Google Drive 授权码换 token 失败: %w", err)
	}

	return d.applyOAuthToken(tok)
}

// RefreshToken 用 refresh_token 刷新。成功后立即持久化。
func (d *GDriveDriver) RefreshToken() error {
	d.mu.Lock()
	refresh := d.token.RefreshToken
	d.mu.Unlock()
	if refresh == "" {
		return fmt.Errorf("无 refresh_token，请重新授权")
	}

	cfg := &oauth2.Config{
		ClientID:     d.clientID,
		ClientSecret: d.clientSecret,
		Endpoint:     google.Endpoint,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tok, err := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refresh}).Token()
	if err != nil {
		return fmt.Errorf("Google Drive token 刷新失败: %w", err)
	}

	return d.applyOAuthToken(tok)
}

// applyOAuthToken 解析 OAuth token 并保存。
func (d *GDriveDriver) applyOAuthToken(tok *oauth2.Token) error {
	if tok.AccessToken == "" {
		return fmt.Errorf("Google Drive 返回的 access_token 为空")
	}

	// Google OAuth 刷新时通常不返回新的 refresh_token，保留原有的。
	refreshToken := tok.RefreshToken
	if refreshToken == "" {
		d.mu.Lock()
		refreshToken = d.token.RefreshToken
		d.mu.Unlock()
	}

	token := GDriveToken{
		AccessToken:    tok.AccessToken,
		RefreshToken:   refreshToken,
		AccessExpireAt: tok.Expiry.Unix(),
	}

	d.mu.Lock()
	d.token = token
	d.loaded = true
	d.mu.Unlock()

	if d.onTokenRefreshed != nil {
		d.onTokenRefreshed(token)
	}
	return nil
}

// currentToken 返回未过期的 access_token；临近过期时先刷新。
func (d *GDriveDriver) currentToken() (string, error) {
	d.mu.Lock()
	if !d.loaded || d.token.AccessToken == "" {
		d.mu.Unlock()
		return "", errGDriveUnauthorized
	}
	expireAt := d.token.AccessExpireAt
	d.mu.Unlock()

	if time.Now().Unix()+int64(gdriveRefreshAhead/time.Second) >= expireAt {
		if err := d.RefreshToken(); err != nil {
			// 刷新失败但 token 仍在有效期内时继续用旧 token。
			if time.Now().Unix() < expireAt {
				logx.Warn(logx.ModuleStorage, "Google Drive token 刷新失败，暂用旧 token", "err", err.Error())
			} else {
				return "", fmt.Errorf("Google Drive token 已过期且刷新失败: %w", err)
			}
		}
	}

	d.mu.Lock()
	token := d.token.AccessToken
	d.mu.Unlock()
	return token, nil
}

// ensureClient 确保客户端已初始化且 token 有效。
func (d *GDriveDriver) ensureClient() error {
	if _, err := d.currentToken(); err != nil {
		return err
	}
	return nil
}

// gdriveCall 调用 Google Drive API。
func (d *GDriveDriver) gdriveCall(ctx context.Context, method, urlPath string, body io.Reader, contentType string) ([]byte, error) {
	token, err := d.currentToken()
	if err != nil {
		return nil, err
	}

	fullURL := "https://www.googleapis.com/drive/v3" + urlPath
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Google Drive API 错误 (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// findOrCreateFolder 查找或创建文件夹，返回文件夹 ID。
func (d *GDriveDriver) findOrCreateFolder(ctx context.Context, name, parentID string) (string, error) {
	// 先查找
	query := fmt.Sprintf("name='%s' and mimeType='application/vnd.google-apps.folder' and '%s' in parents and trashed=false",
		escapeGDriveQuery(name), parentID)
	urlPath := fmt.Sprintf("/files?q=%s&fields=files(id,name)&spaces=drive", urlQueryEscape(query))

	resp, err := d.gdriveCall(ctx, http.MethodGet, urlPath, nil, "")
	if err != nil {
		return "", fmt.Errorf("查找文件夹失败: %w", err)
	}

	var result struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("解析文件夹列表失败: %w", err)
	}
	if len(result.Files) > 0 {
		return result.Files[0].ID, nil
	}

	// 未找到，创建
	body := fmt.Sprintf(`{"name": "%s", "mimeType": "application/vnd.google-apps.folder", "parents": ["%s"]}`,
		escapeJSON(name), parentID)
	resp, err = d.gdriveCall(ctx, http.MethodPost, "/files", strings.NewReader(body), "application/json")
	if err != nil {
		return "", fmt.Errorf("创建文件夹失败: %w", err)
	}

	var folder struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp, &folder); err != nil {
		return "", fmt.Errorf("解析文件夹创建响应失败: %w", err)
	}
	return folder.ID, nil
}

// ensureParentDirs 确保父目录存在，返回最终父目录 ID。
func (d *GDriveDriver) ensureParentDirs(ctx context.Context, fullPath string) (string, error) {
	dir := path.Dir(fullPath)
	if dir == "" || dir == "/" || dir == "." {
		return "root", nil
	}

	parts := strings.Split(dir, "/")
	parentID := "root"
	for _, part := range parts {
		if part == "" {
			continue
		}
		folderID, err := d.findOrCreateFolder(ctx, part, parentID)
		if err != nil {
			return "", err
		}
		parentID = folderID
	}
	return parentID, nil
}

// findFile 查找文件，返回文件 ID 和大小。
func (d *GDriveDriver) findFile(ctx context.Context, name, parentID string) (fileID string, size int64, err error) {
	query := fmt.Sprintf("name='%s' and '%s' in parents and trashed=false and mimeType!='application/vnd.google-apps.folder'",
		escapeGDriveQuery(name), parentID)
	urlPath := fmt.Sprintf("/files?q=%s&fields=files(id,name,size)&spaces=drive", urlQueryEscape(query))

	resp, err := d.gdriveCall(ctx, http.MethodGet, urlPath, nil, "")
	if err != nil {
		return "", 0, fmt.Errorf("查找文件失败: %w", err)
	}

	var result struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Size int64  `json:"size,string"`
		} `json:"files"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", 0, fmt.Errorf("解析文件列表失败: %w", err)
	}
	if len(result.Files) == 0 {
		return "", 0, fmt.Errorf("文件不存在")
	}
	return result.Files[0].ID, result.Files[0].Size, nil
}

// UploadFile 上传文件；大文件自动走分片上传。
func (d *GDriveDriver) UploadFile(key string, content []byte) error {
	if err := d.ensureClient(); err != nil {
		return err
	}

	ctx, cancel := contextWithTimeout(5 * time.Minute)
	defer cancel()

	fullPath := d.gdrivePathOf(key)
	fileName := path.Base(fullPath)

	// 确保父目录存在
	parentID, err := d.ensureParentDirs(ctx, fullPath)
	if err != nil {
		return err
	}

	if len(content) > gdriveSessionThreshold {
		if err := d.uploadResumable(ctx, fileName, parentID, content); err != nil {
			logx.Error(logx.ModuleStorage, "Google Drive 分片上传失败", logx.Err(err), "key", key)
			return err
		}
	} else {
		if err := d.uploadSimple(ctx, fileName, parentID, content); err != nil {
			logx.Error(logx.ModuleStorage, "Google Drive 上传失败", logx.Err(err), "key", key)
			return err
		}
	}
	logx.Info(logx.ModuleStorage, "Google Drive 文件已上传", "key", key)
	return nil
}

// uploadSimple 单次上传（小文件）。
func (d *GDriveDriver) uploadSimple(ctx context.Context, fileName, parentID string, content []byte) error {
	token, err := d.currentToken()
	if err != nil {
		return err
	}

	// 使用 multipart/related 上传元数据和内容
	metadata := fmt.Sprintf(`{"name": "%s", "parents": ["%s"]}`, escapeJSON(fileName), parentID)
	boundary := "cloudreve_gdrive_boundary"

	var buf bytes.Buffer
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: application/json; charset=UTF-8\r\n\r\n")
	buf.WriteString(metadata + "\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	buf.Write(content)
	buf.WriteString("\r\n--" + boundary + "--")

	uploadURL := "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "multipart/related; boundary="+boundary)

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("上传失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// uploadResumable 分片上传（大文件）。
func (d *GDriveDriver) uploadResumable(ctx context.Context, fileName, parentID string, content []byte) error {
	token, err := d.currentToken()
	if err != nil {
		return err
	}

	// 1. 发起分片上传
	metadata := fmt.Sprintf(`{"name": "%s", "parents": ["%s"]}`, escapeJSON(fileName), parentID)
	uploadURL := "https://www.googleapis.com/upload/drive/v3/files?uploadType=resumable"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, strings.NewReader(metadata))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", len(content)))

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("发起分片上传失败: HTTP %d", resp.StatusCode)
	}

	sessionURL := resp.Header.Get("Location")
	if sessionURL == "" {
		return fmt.Errorf("未返回分片上传 URL")
	}

	// 2. 分块上传
	const chunkSize = 10 << 20 // 10MB per chunk
	totalSize := int64(len(content))
	offset := int64(0)

	for offset < totalSize {
		end := offset + chunkSize
		if end > totalSize {
			end = totalSize
		}
		chunk := content[offset:end]

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, sessionURL, bytes.NewReader(chunk))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, end-1, totalSize))
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(chunk)))

		resp, err := d.client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()

		// 200/201 = 上传完成；308 Resume Incomplete = 继续上传下一块
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != 308 {
			return fmt.Errorf("上传分片失败: HTTP %d", resp.StatusCode)
		}

		offset = end
	}

	return nil
}

// GenerateUploadURL Google Drive 不支持浏览器预签名直传，由服务端中转上传。
func (d *GDriveDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return "", fmt.Errorf("Google Drive 存储不支持客户端直传，请使用服务端上传")
}

// GenerateDownloadURL 返回 Google Drive 文件临时下载链接。
// 后端调用 Drive API（不跟随重定向），提取 302 Location 的 googleusercontent.com 签名 URL，
// 该 URL 免认证、有时效（约 1 小时），前端直连下载，不经服务端代理。
func (d *GDriveDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	if err := d.ensureClient(); err != nil {
		return "", err
	}

	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()

	fullPath := d.gdrivePathOf(key)
	name := path.Base(fullPath)

	// 查找父目录 ID
	parentID, err := d.ensureParentDirs(ctx, fullPath)
	if err != nil {
		return "", err
	}

	// 查找文件
	fileID, _, err := d.findFile(ctx, name, parentID)
	if err != nil {
		return "", fmt.Errorf("文件不存在: %w", err)
	}

	// 获取 OAuth token
	token, err := d.currentToken()
	if err != nil {
		return "", err
	}

	// 创建不跟随重定向的 HTTP 客户端，提取 Google 返回的签名 URL
	noRedirectClient := &http.Client{
		Transport: d.client.Transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // 不跟随重定向
		},
	}

	downloadURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?alt=media&supportsAllDrives=true", fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 Google Drive 下载链接失败: %w", err)
	}
	defer resp.Body.Close()

	// Google Drive API 返回 302 重定向到 googleusercontent.com 签名 URL
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusTemporaryRedirect {
		location := resp.Header.Get("Location")
		if location != "" {
			logx.Info(logx.ModuleStorage, "生成 Google Drive 临时下载链接", "fileID", fileID, "expire", "1h")
			return location, nil
		}
	}

	// HTTP 200：Google Drive 直接返回文件内容（某些文件类型/大小不触发 302 重定向）。
	// 使用 Google Drive 原生下载链接，前端可直接访问。
	if resp.StatusCode == http.StatusOK {
		nativeURL := fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)
		logx.Info(logx.ModuleStorage, "Google Drive 返回 HTTP 200，使用原生下载链接", "fileID", fileID)
		return nativeURL, nil
	}

	// 其他状态码：读取错误信息
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return "", fmt.Errorf("Google Drive 未返回临时下载链接 (HTTP %d): %s", resp.StatusCode, string(respBody))
}

// Delete 删除文件；不存在视为成功。
func (d *GDriveDriver) Delete(key string) error {
	if err := d.ensureClient(); err != nil {
		return err
	}

	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()

	fullPath := d.gdrivePathOf(key)
	dir := path.Dir(fullPath)
	name := path.Base(fullPath)

	parentID, err := d.ensureParentDirs(ctx, fullPath)
	if err != nil {
		return err
	}
	_ = dir

	fileID, _, err := d.findFile(ctx, name, parentID)
	if err != nil {
		// 文件不存在视为已删除
		if strings.Contains(err.Error(), "不存在") {
			return nil
		}
		return err
	}

	urlPath := fmt.Sprintf("/files/%s", fileID)
	_, err = d.gdriveCall(ctx, http.MethodDelete, urlPath, nil, "")
	if err != nil {
		// 404 视为已删除
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "notFound") {
			return nil
		}
		logx.Error(logx.ModuleStorage, "Google Drive 删除文件失败", logx.Err(err), "key", key)
		return err
	}
	logx.Info(logx.ModuleStorage, "Google Drive 文件已删除", "key", key)
	return nil
}

// GetSize 获取文件大小。
func (d *GDriveDriver) GetSize(key string) (int64, error) {
	if err := d.ensureClient(); err != nil {
		return 0, err
	}

	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()

	fullPath := d.gdrivePathOf(key)
	parentID, err := d.ensureParentDirs(ctx, fullPath)
	if err != nil {
		return 0, err
	}

	name := path.Base(fullPath)
	_, size, err := d.findFile(ctx, name, parentID)
	if err != nil {
		return 0, err
	}
	return size, nil
}

// Read 返回文件内容流（打包下载用），调用方负责关闭。
func (d *GDriveDriver) Read(key string) (io.ReadCloser, error) {
	if err := d.ensureClient(); err != nil {
		return nil, err
	}

	ctx, cancel := contextWithTimeout(5 * time.Minute)
	defer cancel()

	fullPath := d.gdrivePathOf(key)
	parentID, err := d.ensureParentDirs(ctx, fullPath)
	if err != nil {
		return nil, err
	}

	name := path.Base(fullPath)
	fileID, _, err := d.findFile(ctx, name, parentID)
	if err != nil {
		return nil, err
	}

	token, err := d.currentToken()
	if err != nil {
		return nil, err
	}

	downloadURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?alt=media", fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

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

// InitMultipartUpload Google Drive 不支持 S3 式客户端分片直传。
func (d *GDriveDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return "", fmt.Errorf("Google Drive 存储不支持客户端分片直传，请使用服务端上传")
}

// GenerateUploadPartURL 不支持。
func (d *GDriveDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return "", fmt.Errorf("Google Drive 存储不支持客户端分片直传，请使用服务端上传")
}

// CompleteMultipartUpload 不支持。
func (d *GDriveDriver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	return fmt.Errorf("Google Drive 存储不支持客户端分片直传，请使用服务端上传")
}

// AbortMultipartUpload 不支持。
func (d *GDriveDriver) AbortMultipartUpload(key string, uploadID string) error {
	return fmt.Errorf("Google Drive 存储不支持客户端分片直传，请使用服务端上传")
}

// ListUploadedParts 不支持。
func (d *GDriveDriver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	return nil, fmt.Errorf("Google Drive 存储不支持客户端分片直传，请使用服务端上传")
}

// SetBucketCORS Google Drive 无此概念。
func (d *GDriveDriver) SetBucketCORS() error {
	return ErrBucketCORSNotSupported
}

// gdriveSessionID 编码上传会话信息到 uploadID（base64 JSON）。
type gdriveSessionID struct {
	SessionURL string `json:"u"`
	TotalSize  int64  `json:"s"`
}

func encodeGDriveSessionID(sessionURL string, totalSize int64) string {
	sid := gdriveSessionID{SessionURL: sessionURL, TotalSize: totalSize}
	data, _ := json.Marshal(sid)
	return base64.URLEncoding.EncodeToString(data)
}

func decodeGDriveSessionID(encoded string) (*gdriveSessionID, error) {
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("解码上传会话 ID 失败: %w", err)
	}
	var sid gdriveSessionID
	if err := json.Unmarshal(data, &sid); err != nil {
		return nil, fmt.Errorf("解析上传会话 ID 失败: %w", err)
	}
	return &sid, nil
}

// InitChunkedUpload 创建分片上传会话。
// 空文件直接上传完成（fastUpload=true）。
func (d *GDriveDriver) InitChunkedUpload(key string, size int64, blockMD5s []string) (string, bool, error) {
	if err := d.ensureClient(); err != nil {
		return "", false, err
	}

	ctx, cancel := contextWithTimeout(60 * time.Second)
	defer cancel()

	fullPath := d.gdrivePathOf(key)
	fileName := path.Base(fullPath)

	// 确保父目录存在
	parentID, err := d.ensureParentDirs(ctx, fullPath)
	if err != nil {
		return "", false, err
	}

	if size == 0 {
		// 空文件直接上传
		if err := d.uploadSimple(ctx, fileName, parentID, nil); err != nil {
			return "", false, fmt.Errorf("Google Drive 上传失败: %w", err)
		}
		return "", true, nil
	}

	// 发起分片上传
	token, err := d.currentToken()
	if err != nil {
		return "", false, err
	}

	metadata := fmt.Sprintf(`{"name": "%s", "parents": ["%s"]}`, escapeJSON(fileName), parentID)
	uploadURL := "https://www.googleapis.com/upload/drive/v3/files?uploadType=resumable"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, strings.NewReader(metadata))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", size))

	resp, err := d.client.Do(req)
	if err != nil {
		return "", false, err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("发起分片上传失败: HTTP %d", resp.StatusCode)
	}

	sessionURL := resp.Header.Get("Location")
	if sessionURL == "" {
		return "", false, fmt.Errorf("未返回分片上传 URL")
	}

	// 编码会话信息到 uploadID
	uploadID := encodeGDriveSessionID(sessionURL, size)
	return uploadID, false, nil
}

// UploadChunk 上传单个块（PUT 到 sessionURL，带 Content-Range）。
func (d *GDriveDriver) UploadChunk(key string, uploadID string, partSeq int, offset int64, data []byte) (string, error) {
	if err := d.ensureClient(); err != nil {
		return "", err
	}

	sid, err := decodeGDriveSessionID(uploadID)
	if err != nil {
		return "", err
	}

	ctx, cancel := contextWithTimeout(2 * time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, sid.SessionURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}

	end := offset + int64(len(data)) - 1
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, end, sid.TotalSize))
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))

	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 200/201 = 上传完成；308 = 继续上传下一块
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != 308 {
		return "", fmt.Errorf("上传分片失败: HTTP %d", resp.StatusCode)
	}

	return uploadID, nil
}

// CompleteChunkedUpload Google Drive 分片上传在最后一块上传时自动完成，此处为 no-op。
func (d *GDriveDriver) CompleteChunkedUpload(key string, uploadID string, size int64, blockMD5s []string) error {
	logx.Info(logx.ModuleStorage, "Google Drive 分块上传完成", "key", key, "size", size)
	return nil
}

// escapeGDriveQuery 转义 Google Drive 查询字符串中的特殊字符。
func escapeGDriveQuery(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// escapeJSON 转义 JSON 字符串中的特殊字符。
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
