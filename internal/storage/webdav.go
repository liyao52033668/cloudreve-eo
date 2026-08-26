package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
)

// WebDAVDriver 使用 WebDAV 协议实现存储驱动。
// 通过 HTTP PUT/GET/DELETE/MKCOL 等标准方法操作远端文件。
// 无预签名直传，上传/下载均经服务端代理。
type WebDAVDriver struct {
	serverURL  string // WebDAV 服务器地址，如 https://dav.example.com
	username   string // 用户名
	password   string // 密码
	basePath   string // 存储路径前缀，如 cloudreve-eo
	customHost string // 自定义下载域名（可选，留空使用 serverURL）
	client     *http.Client
	direct     bool // 是否允许浏览器直连（需服务商开放 CORS）

	// proxyURL 生成带签名的服务端代理下载 URL（由 manager 注入）。
	proxyURL func(storageKey, attachment string) (string, error)
}

// NewWebDAVDriver 创建 WebDAV 存储驱动。
// serverURL: WebDAV 服务器地址，如 https://dav.example.com 或 https://dav.example.com/remote.php/dav/files/user
// username/password: 认证凭据
// basePath: 存储路径前缀，空则默认 cloudreve-eo
// customHost: 自定义下载域名（可选），空则使用 serverURL
// direct: 是否允许浏览器直连（需服务商开放 CORS），false 则走服务端中转
func NewWebDAVDriver(serverURL, username, password, basePath, customHost string, direct bool) (*WebDAVDriver, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("WebDAV 服务器地址不能为空")
	}
	if username == "" {
		return nil, fmt.Errorf("WebDAV 用户名不能为空")
	}
	if password == "" {
		return nil, fmt.Errorf("WebDAV 密码不能为空")
	}

	// 清理 serverURL 末尾斜杠
	serverURL = strings.TrimRight(serverURL, "/")

	if basePath == "" {
		basePath = "cloudreve-eo"
	}
	basePath = strings.Trim(basePath, "/")

	return &WebDAVDriver{
		serverURL:  serverURL,
		username:   username,
		password:   password,
		basePath:   basePath,
		customHost: strings.TrimRight(customHost, "/"),
		client: &http.Client{
			Timeout: 30 * time.Minute, // 大文件上传需要较长超时
		},
		direct: direct,
	}, nil
}

// webdavPathOf 由对象键得到 WebDAV 完整路径。
func (d *WebDAVDriver) webdavPathOf(key string) string {
	return d.basePath + "/" + strings.TrimPrefix(key, "/")
}

// doRequest 执行带 Basic Auth 的 HTTP 请求。
func (d *WebDAVDriver) doRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(d.username, d.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	return d.client.Do(req)
}

// ensureParentDirs 递归创建父目录（MKCOL）。
func (d *WebDAVDriver) ensureParentDirs(ctx context.Context, fullPath string) error {
	// 从 basePath 之后开始逐级创建
	relPath := strings.TrimPrefix(fullPath, d.basePath)
	relPath = strings.TrimPrefix(relPath, "/")
	parts := strings.Split(relPath, "/")
	// 最后一段是文件名，跳过
	parts = parts[:len(parts)-1]

	current := d.serverURL + "/" + d.basePath
	for _, part := range parts {
		if part == "" {
			continue
		}
		current += "/" + part
		// MKCOL 创建目录，已存在则忽略 405
		resp, err := d.doRequest(ctx, "MKCOL", current, nil)
		if err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusConflict {
			// 405 = 已存在，409 = 父目录不存在（继续创建）
			// 其他错误才返回
			if resp.StatusCode >= 400 && resp.StatusCode != http.StatusConflict {
				return fmt.Errorf("创建目录失败: HTTP %d", resp.StatusCode)
			}
		}
	}
	return nil
}

// UploadFile 通过 HTTP PUT 上传文件。
func (d *WebDAVDriver) UploadFile(key string, content []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	fullPath := d.webdavPathOf(key)

	// 确保父目录存在
	if err := d.ensureParentDirs(ctx, fullPath); err != nil {
		logx.Error(logx.ModuleStorage, "WebDAV 创建目录失败", logx.Err(err), "key", key)
		return err
	}

	url := d.serverURL + "/" + fullPath
	resp, err := d.doRequest(ctx, "PUT", url, bytes.NewReader(content))
	if err != nil {
		logx.Error(logx.ModuleStorage, "WebDAV 上传失败", logx.Err(err), "key", key)
		return fmt.Errorf("上传失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		logx.Error(logx.ModuleStorage, "WebDAV 上传失败", "status", resp.StatusCode, "body", string(body), "key", key)
		return fmt.Errorf("上传失败: HTTP %d", resp.StatusCode)
	}

	logx.Info(logx.ModuleStorage, "WebDAV 文件已上传", "key", key, "size", len(content))
	return nil
}

// GenerateUploadURL WebDAV 无预签名直传。
// direct=true 时返回内嵌 Basic Auth 凭据的直连 PUT URL（浏览器直传，需服务商开放 CORS）；
// 否则返回错误，让调用方回退到服务端中转上传。
func (d *WebDAVDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	if d.direct {
		return d.directURL(key), nil
	}
	return "", fmt.Errorf("WebDAV 存储不支持客户端直传，请使用服务端上传")
}

// GenerateDownloadURL 生成下载 URL。
// direct=true 时返回内嵌 Basic Auth 凭据的直连 GET URL（浏览器原生下载/预览，顶级导航无需 CORS）；
// 否则返回带签名的服务端代理下载 URL。
func (d *WebDAVDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	if d.direct {
		return d.directURL(key), nil
	}
	if d.proxyURL == nil {
		return "", fmt.Errorf("WebDAV 代理下载未初始化")
	}
	return d.proxyURL(key, fileName)
}

// directURL 生成内嵌 Basic Auth 凭据的 WebDAV 直连 URL（https://user:pass@host/path）。
// 浏览器 fetch / XMLHttpRequest / <a> / <img> / <video> 遇到 URL 中的 userinfo 会自动携带
// Authorization: Basic 头，因此前端无需改动即可直连。凭据经 URL 编码避免特殊字符破坏 URL。
func (d *WebDAVDriver) directURL(key string) string {
	host := d.serverURL
	// 自定义域名（可选）：覆盖服务器地址
	if d.customHost != "" {
		host = d.customHost
	}
	cred := url.QueryEscape(d.username) + ":" + url.QueryEscape(d.password)
	// 在 scheme:// 之后插入 user:pass@
	parts := strings.SplitN(host, "://", 2)
	if len(parts) == 2 {
		return parts[0] + "://" + cred + "@" + parts[1] + "/" + d.webdavPathOf(key)
	}
	return cred + "@" + host + "/" + d.webdavPathOf(key)
}

// Delete 删除远端文件。
func (d *WebDAVDriver) Delete(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fullPath := d.webdavPathOf(key)
	url := d.serverURL + "/" + fullPath

	resp, err := d.doRequest(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("删除失败: %w", err)
	}
	defer resp.Body.Close()

	// 404 视为已删除
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("删除失败: HTTP %d", resp.StatusCode)
	}

	logx.Info(logx.ModuleStorage, "WebDAV 文件已删除", "key", key)
	return nil
}

// GetSize 通过 PROPFIND 获取文件大小。
func (d *WebDAVDriver) GetSize(key string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fullPath := d.webdavPathOf(key)
	url := d.serverURL + "/" + fullPath

	req, err := http.NewRequestWithContext(ctx, "PROPFIND", url, nil)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(d.username, d.password)
	req.Header.Set("Depth", "0")

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("查询文件大小失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("文件不存在")
	}
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("查询文件大小失败: HTTP %d", resp.StatusCode)
	}

	// 解析 DAV:propstat/DAV:prop/DAV:getcontentlength
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %w", err)
	}

	// 简单解析 XML 提取 getcontentlength
	content := string(body)
	idx := strings.Index(content, "<D:getcontentlength>")
	if idx < 0 {
		idx = strings.Index(content, "<d:getcontentlength>")
	}
	if idx < 0 {
		return 0, fmt.Errorf("无法解析文件大小")
	}
	start := idx + len("<D:getcontentlength>")
	if strings.Index(content[start-20:start], "<d:") >= 0 {
		start = idx + len("<d:getcontentlength>")
	}
	end := strings.Index(content[start:], "</")
	if end < 0 {
		return 0, fmt.Errorf("无法解析文件大小")
	}
	sizeStr := content[start : start+end]
	var size int64
	fmt.Sscanf(sizeStr, "%d", &size)
	return size, nil
}

// Read 返回文件内容流（代理下载/打包下载用），调用方负责关闭。
func (d *WebDAVDriver) Read(key string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)

	fullPath := d.webdavPathOf(key)
	url := d.serverURL + "/" + fullPath

	resp, err := d.doRequest(ctx, "GET", url, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("文件不存在")
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("读取文件失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	// 关闭 body 时同时释放 context
	return &webdavReadCloser{inner: resp.Body, cancel: cancel}, nil
}

// ReadRange 按 [start, end] 闭区间读取文件内容流（Range 分段下载用）。
func (d *WebDAVDriver) ReadRange(key string, start, end int64) (io.ReadCloser, error) {
	if start < 0 {
		return nil, fmt.Errorf("无效的 Range：start 不能为负")
	}
	if end >= 0 && end < start {
		return nil, fmt.Errorf("无效的 Range：end 小于 start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)

	fullPath := d.webdavPathOf(key)
	url := d.serverURL + "/" + fullPath

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.SetBasicAuth(d.username, d.password)

	// 设置 Range 头
	if end < 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	} else {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("文件不存在")
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		resp.Body.Close()
		cancel()
		return nil, ErrRangeNotSatisfiable
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("读取文件失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	return &webdavReadCloser{inner: resp.Body, cancel: cancel}, nil
}

// webdavReadCloser 关闭下载流时同时释放上下文。
type webdavReadCloser struct {
	inner  io.ReadCloser
	cancel context.CancelFunc
}

func (r *webdavReadCloser) Read(p []byte) (int, error) { return r.inner.Read(p) }
func (r *webdavReadCloser) Close() error {
	err := r.inner.Close()
	r.cancel()
	return err
}

// InitMultipartUpload WebDAV 不支持 S3 式客户端分片直传。
func (d *WebDAVDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return "", fmt.Errorf("WebDAV 存储不支持客户端分片直传，请使用服务端上传")
}

// GenerateUploadPartURL 不支持。
func (d *WebDAVDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return "", fmt.Errorf("WebDAV 存储不支持客户端分片直传，请使用服务端上传")
}

// CompleteMultipartUpload 不支持。
func (d *WebDAVDriver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	return fmt.Errorf("WebDAV 存储不支持客户端分片直传，请使用服务端上传")
}

// AbortMultipartUpload 不支持。
func (d *WebDAVDriver) AbortMultipartUpload(key string, uploadID string) error {
	return fmt.Errorf("WebDAV 存储不支持客户端分片直传，请使用服务端上传")
}

// ListUploadedParts 不支持。
func (d *WebDAVDriver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	return nil, fmt.Errorf("WebDAV 存储不支持客户端分片直传，请使用服务端上传")
}

// SetBucketCORS WebDAV 无此概念。
func (d *WebDAVDriver) SetBucketCORS() error {
	return ErrBucketCORSNotSupported
}

// InitChunkedUpload 使用 chunkbuffer 缓冲分块，complete 时整体 PUT 上传。
func (d *WebDAVDriver) InitChunkedUpload(key string, size int64, blockMD5s []string) (string, bool, error) {
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
func (d *WebDAVDriver) UploadChunk(key string, uploadID string, partSeq int, offset int64, data []byte) (string, error) {
	if err := appendChunkBuffer(uploadID, data); err != nil {
		return "", err
	}
	return uploadID, nil
}

// CompleteChunkedUpload 读取缓冲文件并 PUT 上传。
func (d *WebDAVDriver) CompleteChunkedUpload(key string, uploadID string, size int64, blockMD5s []string) error {
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

// 确保 WebDAVDriver 实现 StorageDriver 接口
var _ StorageDriver = (*WebDAVDriver)(nil)

// 确保 WebDAVDriver 实现 RangeReader 接口
var _ RangeReader = (*WebDAVDriver)(nil)

// 确保 WebDAVDriver 实现 ServerChunkedUploader 接口
var _ ServerChunkedUploader = (*WebDAVDriver)(nil)

// IsConfigured 凭据是否齐备。
func (d *WebDAVDriver) IsConfigured() bool {
	return d.serverURL != "" && d.username != "" && d.password != ""
}

// ErrWebDAVCredentialsMissing 表示 WebDAV 凭据未配置。
var ErrWebDAVCredentialsMissing = errors.New("WebDAV 凭据缺失，请到「存储策略」编辑并保存该策略")
