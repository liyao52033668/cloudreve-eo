package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
)

// CloudreveDriver 使用 Cloudreve API 实现存储驱动。
// 上传：调用 Cloudreve API 创建会话 → 前端直传 S3 → 回调 Cloudreve
// 下载：调用 Cloudreve POST /file/url 生成带正确文件名的下载链接
// 删除：调用 Cloudreve DELETE /file
type CloudreveDriver struct {
	apiURL   string // Cloudreve API 地址，如 https://pan.cdn.ad
	username string // Cloudreve 登录邮箱
	password string // Cloudreve 登录密码
	basePath string // 存储路径前缀，如 cloudreve-eo
	client   *http.Client

	// Token 缓存
	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewCloudreveDriver 创建 Cloudreve 存储驱动。
// proxyEnabled/proxyURL 控制是否使用代理及代理地址。
func NewCloudreveDriver(apiURL, username, password, basePath string, proxyEnabled bool, proxyURL string) (*CloudreveDriver, error) {
	if apiURL == "" {
		return nil, fmt.Errorf("Cloudreve API 地址不能为空")
	}
	if username == "" {
		return nil, fmt.Errorf("Cloudreve 用户名不能为空")
	}
	if password == "" {
		return nil, fmt.Errorf("Cloudreve 密码不能为空")
	}

	apiURL = strings.TrimRight(apiURL, "/")
	if basePath == "" {
		basePath = "cloudreve-eo"
	}
	basePath = strings.Trim(basePath, "/")

	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	if transport := buildHTTPTransport(proxyEnabled, proxyURL); transport != nil {
		client.Transport = transport
	}

	return &CloudreveDriver{
		apiURL:   apiURL,
		username: username,
		password: password,
		basePath: basePath,
		client:   client,
	}, nil
}

// login 登录 Cloudreve 获取 Bearer Token。
func (d *CloudreveDriver) login() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 检查 token 是否仍然有效（提前 5 分钟刷新）
	if d.token != "" && time.Now().Add(5*time.Minute).Before(d.tokenExp) {
		return nil
	}

	loginURL := d.apiURL + "/api/v4/session/token"
	reqBody := map[string]string{
		"email":    d.username,
		"password": d.password,
	}
	bodyJSON, _ := json.Marshal(reqBody)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", loginURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("创建登录请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("登录失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Token struct {
				AccessToken   string `json:"access_token"`
				AccessExpires string `json:"access_expires"`
			} `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("解析登录响应失败: %w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("登录失败: %s", result.Msg)
	}

	d.token = result.Data.Token.AccessToken
	if t, err := time.Parse(time.RFC3339, result.Data.Token.AccessExpires); err == nil {
		d.tokenExp = t
	} else {
		d.tokenExp = time.Now().Add(1 * time.Hour)
	}
	logx.Info(logx.ModuleStorage, "Cloudreve 登录成功", "expires", result.Data.Token.AccessExpires)
	return nil
}

// apiRequest 执行带 Bearer Token 认证的 Cloudreve API 请求。
func (d *CloudreveDriver) apiRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	if err := d.login(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return d.client.Do(req)
}

// cloudreveURI 构建 Cloudreve URI: cloudreve://my/{key}
// key 已经包含 basePath（由 buildStorageKey 生成），不需要再加
func (d *CloudreveDriver) cloudreveURI(key string) string {
	return "cloudreve://my/" + strings.TrimPrefix(key, "/")
}

// GenerateUploadURL 创建 Cloudreve 上传会话，返回直传 URL。
func (d *CloudreveDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	// Cloudreve 上传需要知道文件大小，但这里不知道，返回错误走服务端上传
	return "", fmt.Errorf("Cloudreve 存储不支持客户端直传，请使用服务端上传")
}

// GenerateDownloadURL 调用 Cloudreve POST /file/url 生成下载链接。
// 直接返回 Cloudreve API 的 S3 预签名 URL，不走服务端代理。
func (d *CloudreveDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	cloudreveURI := d.cloudreveURI(key)

	reqBody := map[string]interface{}{
		"uris": []string{cloudreveURI},
	}
	bodyJSON, _ := json.Marshal(reqBody)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := d.apiRequest(ctx, "POST", d.apiURL+"/api/v4/file/url", bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("调用 Cloudreve 下载接口失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("Cloudreve 下载接口失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			URLs []struct {
				URL string `json:"url"`
			} `json:"urls"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析 Cloudreve 下载响应失败: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("Cloudreve 下载接口返回错误: %s", result.Msg)
	}
	if len(result.Data.URLs) == 0 {
		return "", fmt.Errorf("Cloudreve 未返回下载链接")
	}

	logx.Info(logx.ModuleStorage, "Cloudreve 下载链接已生成", "key", key)
	return result.Data.URLs[0].URL, nil
}

// Delete 调用 Cloudreve DELETE /file 删除文件。
func (d *CloudreveDriver) Delete(key string) error {
	cloudreveURI := d.cloudreveURI(key)

	reqBody := map[string]interface{}{
		"uris": []string{cloudreveURI},
	}
	bodyJSON, _ := json.Marshal(reqBody)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := d.apiRequest(ctx, "DELETE", d.apiURL+"/api/v4/file", bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("调用 Cloudreve 删除接口失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("Cloudreve 删除接口失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("解析 Cloudreve 删除响应失败: %w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("Cloudreve 删除接口返回错误: %s", result.Msg)
	}

	logx.Info(logx.ModuleStorage, "Cloudreve 文件已删除", "key", key)
	return nil
}

// GetSize 调用 Cloudreve GET /file/info 获取文件大小。
func (d *CloudreveDriver) GetSize(key string) (int64, error) {
	cloudreveURI := d.cloudreveURI(key)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := d.apiURL + "/api/v4/file/info?uri=" + cloudreveURI
	resp, err := d.apiRequest(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("调用 Cloudreve 文件信息接口失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return 0, fmt.Errorf("Cloudreve 文件信息接口失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Size int64 `json:"size"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("解析 Cloudreve 文件信息响应失败: %w", err)
	}
	if result.Code != 0 {
		return 0, fmt.Errorf("Cloudreve 文件信息接口返回错误: %s", result.Msg)
	}

	return result.Data.Size, nil
}

// Read 下载文件内容（通过 Cloudreve 下载链接）。
func (d *CloudreveDriver) Read(key string) (io.ReadCloser, error) {
	downloadURL, err := d.GenerateDownloadURL(key, "", 0)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("下载文件失败: %w", err)
	}

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("下载文件失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	return &readCloser{inner: resp.Body, cancel: cancel}, nil
}

// readCloser 关闭时同时释放 context。
type readCloser struct {
	inner  io.ReadCloser
	cancel context.CancelFunc
}

func (r *readCloser) Read(p []byte) (int, error) { return r.inner.Read(p) }
func (r *readCloser) Close() error {
	err := r.inner.Close()
	r.cancel()
	return err
}

// UploadFile 服务端直接上传文件（通过 Cloudreve API）。
func (d *CloudreveDriver) UploadFile(key string, content []byte) error {
	// 创建上传会话
	cloudreveURI := d.cloudreveURI(key)
	reqBody := map[string]interface{}{
		"uri":  cloudreveURI,
		"size": len(content),
	}
	bodyJSON, _ := json.Marshal(reqBody)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	resp, err := d.apiRequest(ctx, "PUT", d.apiURL+"/api/v4/file/upload", bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("创建上传会话失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("创建上传会话失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	var session struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			SessionID      string   `json:"session_id"`
			ChunkSize      int64    `json:"chunk_size"`
			UploadURLs     []string `json:"upload_urls"`
			CompleteURL    string   `json:"completeURL"`
			CallbackSecret string   `json:"callback_secret"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return fmt.Errorf("解析上传会话响应失败: %w", err)
	}
	if session.Code != 0 {
		return fmt.Errorf("创建上传会话失败: %s", session.Msg)
	}

	if len(session.Data.UploadURLs) == 0 {
		return fmt.Errorf("Cloudreve 未返回上传链接")
	}

	// 按 chunk_size 分片上传
	chunkSize := session.Data.ChunkSize
	if chunkSize <= 0 {
		chunkSize = int64(len(content)) // 单片上传
	}
	partCount := (len(content) + int(chunkSize) - 1) / int(chunkSize)
	if partCount == 0 {
		partCount = 1
	}

		etags := make([]string, 0, partCount)
		for i := 0; i < partCount; i++ {
			start := i * int(chunkSize)
			end := start + int(chunkSize)
			if end > len(content) {
				end = len(content)
			}
			chunk := content[start:end]

			var uploadURL string
			if i < len(session.Data.UploadURLs) {
				uploadURL = session.Data.UploadURLs[i]
			}
			if uploadURL == "" {
				if i == 0 {
					return fmt.Errorf("Cloudreve 未返回分片 %d 的上传链接", i+1)
				}
				uploadURL = session.Data.UploadURLs[0] // fallback
			}

		req, err := http.NewRequestWithContext(ctx, "PUT", uploadURL, bytes.NewReader(chunk))
		if err != nil {
			return fmt.Errorf("创建分片 %d 上传请求失败: %w", i+1, err)
		}
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(chunk)))

		resp2, err := d.client.Do(req)
		if err != nil {
			return fmt.Errorf("分片 %d 上传到 S3 失败: %w", i+1, err)
		}

		if resp2.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp2.Body, 1024))
			resp2.Body.Close()
			return fmt.Errorf("分片 %d 上传到 S3 失败: HTTP %d, body: %s", i+1, resp2.StatusCode, string(body))
		}

		etag := resp2.Header.Get("ETag")
		resp2.Body.Close()

		if etag == "" {
			return fmt.Errorf("分片 %d 未返回 ETag", i+1)
		}
		etags = append(etags, etag)
	}

	// 完成分片上传，声明所有分片
	var partsXML strings.Builder
	partsXML.WriteString(`<CompleteMultipartUpload>`)
	for i, etag := range etags {
		partsXML.WriteString(fmt.Sprintf(`<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>`, i+1, etag))
	}
	partsXML.WriteString(`</CompleteMultipartUpload>`)
	completeXML := partsXML.String()
	req3, err := http.NewRequestWithContext(ctx, "POST", session.Data.CompleteURL, strings.NewReader(completeXML))
	if err != nil {
		return fmt.Errorf("创建完成请求失败: %w", err)
	}
	req3.Header.Set("Content-Type", "application/xml")

	resp3, err := d.client.Do(req3)
	if err != nil {
		return fmt.Errorf("完成上传失败: %w", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp3.Body, 1024))
		return fmt.Errorf("完成上传失败: HTTP %d, body: %s", resp3.StatusCode, string(body))
	}

	// 回调 Cloudreve
	callbackURL := fmt.Sprintf("%s/api/v4/callback/s3/%s/%s", d.apiURL, session.Data.SessionID, session.Data.CallbackSecret)
	req4, err := http.NewRequestWithContext(ctx, "GET", callbackURL, nil)
	if err != nil {
		return fmt.Errorf("创建回调请求失败: %w", err)
	}
	req4.Header.Set("Authorization", "Bearer "+d.token)

	resp4, err := d.client.Do(req4)
	if err != nil {
		return fmt.Errorf("回调失败: %w", err)
	}
	defer resp4.Body.Close()

	if resp4.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp4.Body, 1024))
		return fmt.Errorf("回调失败: HTTP %d, body: %s", resp4.StatusCode, string(body))
	}

	logx.Info(logx.ModuleStorage, "Cloudreve 文件已上传", "key", key, "size", len(content))
	return nil
}

// InitChunkedUpload 服务端中转分块上传：本地缓冲各块，complete 时整体经 Cloudreve API 上传。
// Cloudreve 无法跨请求流式转发，各块先按序追加到临时文件。
func (d *CloudreveDriver) InitChunkedUpload(key string, size int64, blockMD5s []string) (string, bool, error) {
	sweepStaleChunkBuffers()
	uploadID, err := newChunkUploadID()
	if err != nil {
		return "", false, err
	}
	if err := createChunkBuffer(uploadID); err != nil {
		return "", false, err
	}
	return uploadID, false, nil
}

// UploadChunk 按序追加一块到缓冲文件。
func (d *CloudreveDriver) UploadChunk(key string, uploadID string, partSeq int, offset int64, data []byte) (string, error) {
	if err := appendChunkBuffer(uploadID, data); err != nil {
		return "", err
	}
	return uploadID, nil
}

// CompleteChunkedUpload 合并缓冲并整体上传（UploadFile 内部按 Cloudreve 会话分片直传 S3）。
func (d *CloudreveDriver) CompleteChunkedUpload(key string, uploadID string, size int64, blockMD5s []string) error {
	defer removeChunkBuffer(uploadID)
	f, err := openChunkBuffer(uploadID)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("读取缓冲文件失败: %w", err)
	}
	return d.UploadFile(key, data)
}

// InitMultipartUpload 不支持客户端分片直传。
func (d *CloudreveDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return "", fmt.Errorf("Cloudreve 存储不支持客户端分片直传")
}

// GenerateUploadPartURL 不支持。
func (d *CloudreveDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return "", fmt.Errorf("Cloudreve 存储不支持客户端分片直传")
}

// CompleteMultipartUpload 不支持。
func (d *CloudreveDriver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	return fmt.Errorf("Cloudreve 存储不支持客户端分片直传")
}

// AbortMultipartUpload 不支持。
func (d *CloudreveDriver) AbortMultipartUpload(key string, uploadID string) error {
	return fmt.Errorf("Cloudreve 存储不支持客户端分片直传")
}

// ListUploadedParts 不支持。
func (d *CloudreveDriver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	return nil, fmt.Errorf("Cloudreve 存储不支持客户端分片直传")
}

// SetBucketCORS 不需要。
func (d *CloudreveDriver) SetBucketCORS() error {
	return ErrBucketCORSNotSupported
}

// GetCloudreveAPIURL 返回 Cloudreve API 地址（用于前端回调）。
func (d *CloudreveDriver) GetCloudreveAPIURL() string {
	return d.apiURL
}

// CreateCloudreveSession 创建 Cloudreve 上传会话，返回直传所需信息（供前端直传 S3）。
func (d *CloudreveDriver) CreateCloudreveSession(key string, size int64, fileName string) (*CloudreveUploadSession, error) {
	cloudreveURI := d.cloudreveURI(key)

	reqBody := map[string]interface{}{
		"uri":  cloudreveURI,
		"size": size,
	}
	bodyJSON, _ := json.Marshal(reqBody)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := d.apiRequest(ctx, "PUT", d.apiURL+"/api/v4/file/upload", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("创建上传会话失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("创建上传会话失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	var session struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			SessionID      string   `json:"session_id"`
			ChunkSize      int64    `json:"chunk_size"`
			UploadURLs     []string `json:"upload_urls"`
			CompleteURL    string   `json:"completeURL"`
			CallbackSecret string   `json:"callback_secret"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("解析上传会话响应失败: %w", err)
	}
	if session.Code != 0 {
		return nil, fmt.Errorf("创建上传会话失败: %s", session.Msg)
	}

	if len(session.Data.UploadURLs) == 0 {
		return nil, fmt.Errorf("Cloudreve 未返回直传 URL")
	}

	logx.Info(logx.ModuleStorage, "Cloudreve 直传会话已创建",
		"key", key, "size", size, "upload_urls", len(session.Data.UploadURLs))

	return &CloudreveUploadSession{
		SessionID:      session.Data.SessionID,
		ChunkSize:      session.Data.ChunkSize,
		UploadURLs:     session.Data.UploadURLs,
		CompleteURL:    session.Data.CompleteURL,
		CallbackSecret: session.Data.CallbackSecret,
	}, nil
}

// CallCloudreveCallback 后端代理调用 Cloudreve callback（后端有 Bearer Token）。
func (d *CloudreveDriver) CallCloudreveCallback(sessionID, callbackSecret string) error {
	if err := d.login(); err != nil {
		return err
	}

	callbackURL := fmt.Sprintf("%s/api/v4/callback/s3/%s/%s", d.apiURL, sessionID, callbackSecret)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", callbackURL, nil)
	if err != nil {
		return fmt.Errorf("创建回调请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.token)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("回调失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("回调失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	logx.Info(logx.ModuleStorage, "Cloudreve callback 成功", "session_id", sessionID)
	return nil
}

// 确保 CloudreveDriver 实现 StorageDriver 接口
var _ StorageDriver = (*CloudreveDriver)(nil)

// 确保 CloudreveDriver 实现 CloudreveDirectUploader 接口
var _ CloudreveDirectUploader = (*CloudreveDriver)(nil)

// 确保 CloudreveDriver 实现 ServerChunkedUploader 接口
var _ ServerChunkedUploader = (*CloudreveDriver)(nil)
