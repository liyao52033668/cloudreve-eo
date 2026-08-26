package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
)

// webdavManifest 记录分片文件的元数据。
type webdavManifest struct {
	OriginalSize int64 `json:"original_size"`
	Parts        int   `json:"parts"`
	ChunkSize    int   `json:"chunk_size"`
}

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

	// proxyURL 生成带签名的服务端代理下载 URL（由 manager 注入）。
	proxyURL func(storageKey, attachment string) (string, error)
}

// NewWebDAVDriver 创建 WebDAV 存储驱动。
// serverURL: WebDAV 服务器地址，如 https://dav.example.com 或 https://dav.example.com/remote.php/dav/files/user
// username/password: 认证凭据
// basePath: 存储路径前缀，空则默认 cloudreve-eo
// customHost: 自定义下载域名（可选），空则使用 serverURL
func NewWebDAVDriver(serverURL, username, password, basePath, customHost string) (*WebDAVDriver, error) {
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

// webdavChunkSize 是每次 PUT 的最大字节数，低于 EdgeOne 网关 6MB 限制。
const webdavChunkSize = 5 * 1024 * 1024

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

// GenerateUploadURL WebDAV 无预签名直传，走服务端上传。
func (d *WebDAVDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return "", fmt.Errorf("WebDAV 存储不支持客户端直传，请使用服务端上传")
}

// GenerateDownloadURL 返回带签名的服务端代理下载 URL。
func (d *WebDAVDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	if d.proxyURL == nil {
		return "", fmt.Errorf("WebDAV 代理下载未初始化")
	}
	return d.proxyURL(key, fileName)
}

// getManifest 尝试读取分片元数据，文件不存在或非分片文件时返回 nil。
func (d *WebDAVDriver) getManifest(key string) *webdavManifest {
	manifestKey := key + ".manifest"
	fullPath := d.webdavPathOf(manifestKey)
	url := d.serverURL + "/" + fullPath

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := d.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var m webdavManifest
	if err := json.Unmarshal(data, &m); err != nil || m.Parts <= 0 {
		return nil
	}
	return &m
}

// putSingle 单次 PUT 上传一块数据。
func (d *WebDAVDriver) putSingle(ctx context.Context, key string, data []byte) error {
	fullPath := d.webdavPathOf(key)
	if err := d.ensureParentDirs(ctx, fullPath); err != nil {
		return err
	}
	url := d.serverURL + "/" + fullPath
	resp, err := d.doRequest(ctx, "PUT", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("上传失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("上传失败: HTTP %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}

// deleteSingle 删除单个远端文件（不处理分片）。
func (d *WebDAVDriver) deleteSingle(key string) error {
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
	return nil
}

// Delete 删除远端文件（自动处理分片文件的所有分片和元数据）。
func (d *WebDAVDriver) Delete(key string) error {
	// 检查是否为分片文件
	if m := d.getManifest(key); m != nil {
		// 删除所有分片和元数据
		for i := 1; i <= m.Parts; i++ {
			partKey := fmt.Sprintf("%s.part%d", key, i)
			if err := d.deleteSingle(partKey); err != nil {
				logx.Warn(logx.ModuleStorage, "删除分片失败", "key", key, "part", i, "err", err)
			}
		}
		_ = d.deleteSingle(key + ".manifest")
		logx.Info(logx.ModuleStorage, "WebDAV 分片文件已删除", "key", key, "parts", m.Parts)
		return nil
	}

	// 普通单文件
	if err := d.deleteSingle(key); err != nil {
		return err
	}
	logx.Info(logx.ModuleStorage, "WebDAV 文件已删除", "key", key)
	return nil
}

// GetSize 通过 PROPFIND 获取文件大小（自动处理分片文件）。
func (d *WebDAVDriver) GetSize(key string) (int64, error) {
	// 分片文件：从 manifest 获取原始大小
	if m := d.getManifest(key); m != nil {
		return m.OriginalSize, nil
	}

	return d.getSizeSingle(key)
}

// getSizeSingle 通过 PROPFIND 获取单个文件大小。
func (d *WebDAVDriver) getSizeSingle(key string) (int64, error) {
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

// Read 返回文件内容流（自动处理分片文件的拼接读取）。
func (d *WebDAVDriver) Read(key string) (io.ReadCloser, error) {
	// 分片文件：返回拼接读取器
	if m := d.getManifest(key); m != nil {
		return d.readMultipart(key, m)
	}

	return d.readSingle(key)
}

// readSingle 读取单个普通文件。
func (d *WebDAVDriver) readSingle(key string) (io.ReadCloser, error) {
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

// readMultipart 按顺序读取所有分片并拼接为一个流。
func (d *WebDAVDriver) readMultipart(key string, m *webdavManifest) (io.ReadCloser, error) {
	readers := make([]io.ReadCloser, 0, m.Parts)
	for i := 1; i <= m.Parts; i++ {
		partKey := fmt.Sprintf("%s.part%d", key, i)
		r, err := d.readSingle(partKey)
		if err != nil {
			// 关闭已打开的分片
			for _, prev := range readers {
				prev.Close()
			}
			return nil, fmt.Errorf("读取分片 %d 失败: %w", i, err)
		}
		readers = append(readers, r)
	}
	return &multiReadCloser{reader: io.MultiReader(toReaders(readers)...), closers: readers}, nil
}

// toReaders 将 []io.ReadCloser 转为 []io.Reader（用于 MultiReader）。
func toReadCloser(rc io.ReadCloser) io.Reader { return rc }
func toReaders(rcc []io.ReadCloser) []io.Reader {
	out := make([]io.Reader, len(rcc))
	for i, r := range rcc {
		out[i] = r
	}
	return out
}

// multiReadCloser 将多个 ReadCloser 拼接为一个，Close 时关闭所有。
type multiReadCloser struct {
	reader  io.Reader
	closers []io.ReadCloser
}

func (m *multiReadCloser) Read(p []byte) (int, error) { return m.reader.Read(p) }
func (m *multiReadCloser) Close() error {
	var firstErr error
	for _, c := range m.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ReadRange 按 [start, end] 闭区间读取文件内容流（自动处理分片文件）。
func (d *WebDAVDriver) ReadRange(key string, start, end int64) (io.ReadCloser, error) {
	if start < 0 {
		return nil, fmt.Errorf("无效的 Range：start 不能为负")
	}
	if end >= 0 && end < start {
		return nil, fmt.Errorf("无效的 Range：end 小于 start")
	}

	// 分片文件：计算 Range 落在哪些分片上
	if m := d.getManifest(key); m != nil {
		return d.readRangeMultipart(key, m, start, end)
	}

	return d.readRangeSingle(key, start, end)
}

// readRangeSingle 对单个文件发起 Range 请求。
func (d *WebDAVDriver) readRangeSingle(key string, start, end int64) (io.ReadCloser, error) {
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

// readRangeMultipart 在分片文件上按字节范围读取。
func (d *WebDAVDriver) readRangeMultipart(key string, m *webdavManifest, start, end int64) (io.ReadCloser, error) {
	// end < 0 表示读到末尾
	if end < 0 {
		end = m.OriginalSize - 1
	}

	chunkSize := int64(m.ChunkSize)
	readers := make([]io.ReadCloser, 0)

	for i := 1; i <= m.Parts; i++ {
		partStart := int64(i-1) * chunkSize
		partEnd := partStart + chunkSize - 1
		if partEnd >= m.OriginalSize {
			partEnd = m.OriginalSize - 1
		}

		// 跳过不在范围内的分片
		if partEnd < start || partStart > end {
			continue
		}

		// 计算此分片内的 Range
		localStart := start - partStart
		if localStart < 0 {
			localStart = 0
		}
		localEnd := end - partStart
		if localEnd > partEnd-partStart {
			localEnd = partEnd - partStart
		}

		partKey := fmt.Sprintf("%s.part%d", key, i)
		r, err := d.readRangeSingle(partKey, localStart, localEnd)
		if err != nil {
			for _, prev := range readers {
				prev.Close()
			}
			return nil, fmt.Errorf("读取分片 %d Range 失败: %w", i, err)
		}
		readers = append(readers, r)
	}

	if len(readers) == 0 {
		return nil, ErrRangeNotSatisfiable
	}

	return &multiReadCloser{reader: io.MultiReader(toReaders(readers)...), closers: readers}, nil
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

// InitChunkedUpload 使用 chunkbuffer 缓冲分块。
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

// CompleteChunkedUpload 分片存储大文件，避免 EdgeOne 网关 6MB 限制。
// 小文件（<5MB）直接单次 PUT；大文件拆成多个 <5MB 的分片分别存储，
// 并写入 .manifest 元数据，下载/删除/查询时自动识别并处理。
func (d *WebDAVDriver) CompleteChunkedUpload(key string, uploadID string, size int64, blockMD5s []string) error {
	defer removeChunkBuffer(uploadID)
	f, err := openChunkBuffer(uploadID)
	if err != nil {
		return err
	}
	defer f.Close()

	// 小文件直接整体上传
	if size <= int64(webdavChunkSize) {
		data, err := io.ReadAll(f)
		if err != nil {
			return fmt.Errorf("读取缓冲文件失败: %w", err)
		}
		return d.UploadFile(key, data)
	}

	// 大文件分片存储
	ctx := context.Background()
	buffer := make([]byte, webdavChunkSize)
	var partNum int

	for {
		n, readErr := f.Read(buffer)
		if n > 0 {
			partNum++
			partKey := fmt.Sprintf("%s.part%d", key, partNum)
			if err := d.putSingle(ctx, partKey, buffer[:n]); err != nil {
				// 清理已上传的分片
				for i := 1; i < partNum; i++ {
					_ = d.deleteSingle(fmt.Sprintf("%s.part%d", key, i))
				}
				return fmt.Errorf("上传分片 %d 失败: %w", partNum, err)
			}
			logx.Info(logx.ModuleStorage, "WebDAV 分片已上传", "key", key, "part", partNum, "size", n, "total", size)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			// 清理已上传的分片
			for i := 1; i <= partNum; i++ {
				_ = d.deleteSingle(fmt.Sprintf("%s.part%d", key, i))
			}
			return fmt.Errorf("读取缓冲文件失败: %w", readErr)
		}
	}

	// 保存元数据
	manifest := webdavManifest{
		OriginalSize: size,
		Parts:        partNum,
		ChunkSize:    webdavChunkSize,
	}
	manifestJSON, _ := json.Marshal(manifest)
	if err := d.UploadFile(key+".manifest", manifestJSON); err != nil {
		for i := 1; i <= partNum; i++ {
			_ = d.deleteSingle(fmt.Sprintf("%s.part%d", key, i))
		}
		return fmt.Errorf("上传元数据失败: %w", err)
	}

	logx.Info(logx.ModuleStorage, "WebDAV 大文件分片上传完成", "key", key, "size", size, "parts", partNum)
	return nil
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
