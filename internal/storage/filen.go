package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	filen "github.com/FilenCloudDienste/filen-sdk-go/filen"
	"github.com/FilenCloudDienste/filen-sdk-go/filen/types"
	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
)

// ErrFilenCredentialsMissing 表示 Filen 凭据未配置，无法登录。
var ErrFilenCredentialsMissing = errors.New("Filen 账号凭据缺失，请到「存储策略」编辑并保存该策略完成登录")

// FilenDriver 使用 Filen 官方 Go SDK 实现存储驱动。
// Filen 为端到端加密网盘：上传/下载在服务端完成加解密，无外链直链，
// 下载与预览经本服务代理（生成带签名的代理 URL）。
// 注意：SDK 的 master key 派生始终需要账号密码，apiKey 无法替代。
type FilenDriver struct {
	email    string
	password string
	basePath string // 存储路径前缀，默认 cloudreve-eo

	mu     sync.Mutex
	client *filen.Filen

	// proxyURL 生成带签名的服务端代理下载 URL（由 manager 注入）。
	proxyURL func(storageKey, attachment string) (string, error)
}

// NewFilenDriver 创建 Filen 驱动。
// AccessKey=email、SecretKey=账号密码、BasePath=存储路径前缀（空则 cloudreve-eo）。
func NewFilenDriver(email, password, basePath string) (*FilenDriver, error) {
	if email == "" {
		return nil, fmt.Errorf("Filen 邮箱不能为空")
	}
	if password == "" {
		return nil, fmt.Errorf("Filen 密码不能为空")
	}
	if basePath == "" {
		basePath = "cloudreve-eo"
	}

	return &FilenDriver{
		email:    email,
		password: password,
		basePath: strings.Trim(basePath, "/"),
	}, nil
}

// IsConfigured 凭据是否齐备（可尝试登录）。
func (d *FilenDriver) IsConfigured() bool {
	return d.email != "" && d.password != ""
}

// ensureClient 惰性初始化 SDK 客户端；未登录时用邮箱+密码登录（2FA 账号暂不支持）。
func (d *FilenDriver) ensureClient(ctx context.Context) (*filen.Filen, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.client != nil {
		return d.client, nil
	}
	if d.password == "" {
		return nil, ErrFilenCredentialsMissing
	}

	// 2FA 未启用时传占位符 XXXXXX
	client, err := filen.New(ctx, d.email, d.password, "XXXXXX")
	if err != nil {
		return nil, fmt.Errorf("Filen 登录失败: %w", err)
	}
	d.client = client
	return client, nil
}

// filenCtx 操作上下文（SDK 内部有 60 分钟请求超时）。
func filenCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// filenPathOf 由对象键得到 Filen 内完整路径。
func (d *FilenDriver) filenPathOf(key string) string {
	return d.basePath + "/" + strings.TrimPrefix(key, "/")
}

// UploadFile 通过 SDK 分片加密上传。
func (d *FilenDriver) UploadFile(key string, content []byte) error {
	return d.uploadFromReader(key, bytes.NewReader(content))
}

// uploadFromReader 从流式读取并分块加密上传（内存峰值由 SDK 控制在单块）。
func (d *FilenDriver) uploadFromReader(key string, reader io.Reader) error {
	ctx, cancel := filenCtx(20 * time.Minute)
	defer cancel()
	client, err := d.ensureClient(ctx)
	if err != nil {
		return err
	}

	fullPath := d.filenPathOf(key)
	parentPath := path.Dir(fullPath)
	fileName := path.Base(fullPath)

	dir, err := client.FindDirectoryOrCreate(ctx, parentPath)
	if err != nil {
		return fmt.Errorf("创建上传目录失败: %w", err)
	}

	now := time.Now()
	incomplete, err := types.NewIncompleteFile(
		client.FileEncryptionVersion,
		fileName,
		"", // mime 由调用方记录在本地数据库，此处留空
		now,
		now,
		dir,
	)
	if err != nil {
		return fmt.Errorf("初始化上传任务失败: %w", err)
	}

	if _, err := client.UploadFromReader(ctx, incomplete, reader); err != nil {
		logx.Error(logx.ModuleStorage, "Filen 上传失败", logx.Err(err), "key", key)
		return err
	}
	logx.Info(logx.ModuleStorage, "Filen 文件已上传", "key", key)
	return nil
}

// InitChunkedUpload Filen SDK 从流读取并自行切块加密，无远端分片状态，
// 各块缓冲到本地临时文件，complete 时流式提交。
func (d *FilenDriver) InitChunkedUpload(key string, size int64, blockMD5s []string) (string, bool, error) {
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
func (d *FilenDriver) UploadChunk(key string, uploadID string, partSeq int, offset int64, data []byte) (string, error) {
	if err := appendChunkBuffer(uploadID, data); err != nil {
		return "", err
	}
	return uploadID, nil
}

// CompleteChunkedUpload 打开缓冲文件流式提交（内存峰值为 SDK 单块）。
func (d *FilenDriver) CompleteChunkedUpload(key string, uploadID string, size int64, blockMD5s []string) error {
	defer removeChunkBuffer(uploadID)
	f, err := openChunkBuffer(uploadID)
	if err != nil {
		return err
	}
	defer f.Close()
	return d.uploadFromReader(key, f)
}

// GenerateUploadURL Filen 无预签名直传，走服务端上传。
func (d *FilenDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return "", fmt.Errorf("Filen 存储不支持客户端直传，请使用服务端上传")
}

// GenerateDownloadURL 返回带签名的服务端代理下载 URL（服务端解密后流式输出）。
func (d *FilenDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	if !d.IsConfigured() {
		return "", ErrFilenCredentialsMissing
	}
	if d.proxyURL == nil {
		return "", fmt.Errorf("Filen 代理下载未初始化")
	}
	return d.proxyURL(key, fileName)
}

// findFilenFile 按对象键定位 Filen 文件；不存在时返回 (nil, nil)。
func (d *FilenDriver) findFilenFile(ctx context.Context, client *filen.Filen, key string) (*types.File, error) {
	file, err := client.FindFile(ctx, d.filenPathOf(key))
	if err != nil {
		return nil, err
	}
	return file, nil
}

// Delete 将文件移入 Filen 回收站；不存在视为成功。
func (d *FilenDriver) Delete(key string) error {
	ctx, cancel := filenCtx(60 * time.Second)
	defer cancel()
	client, err := d.ensureClient(ctx)
	if err != nil {
		return err
	}

	file, err := d.findFilenFile(ctx, client, key)
	if err != nil {
		return fmt.Errorf("查询文件失败: %w", err)
	}
	if file == nil {
		return nil // 远端不存在视为已删除
	}
	if err := client.TrashFile(ctx, *file); err != nil {
		logx.Error(logx.ModuleStorage, "Filen 删除文件失败", logx.Err(err), "key", key)
		return err
	}
	logx.Info(logx.ModuleStorage, "Filen 文件已删除（移入回收站）", "key", key)
	return nil
}

// GetSize 获取文件大小。
func (d *FilenDriver) GetSize(key string) (int64, error) {
	ctx, cancel := filenCtx(60 * time.Second)
	defer cancel()
	client, err := d.ensureClient(ctx)
	if err != nil {
		return 0, err
	}
	file, err := d.findFilenFile(ctx, client, key)
	if err != nil {
		return 0, fmt.Errorf("查询文件失败: %w", err)
	}
	if file == nil {
		return 0, fmt.Errorf("文件不存在")
	}
	return file.Size, nil
}

// Read 返回解密后的文件内容流（代理下载/打包下载用），调用方负责关闭。
func (d *FilenDriver) Read(key string) (io.ReadCloser, error) {
	ctx, cancel := filenCtx(30 * time.Minute)
	client, err := d.ensureClient(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	file, err := d.findFilenFile(ctx, client, key)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("查询文件失败: %w", err)
	}
	if file == nil {
		cancel()
		return nil, fmt.Errorf("文件不存在")
	}

	reader := client.GetDownloadReader(ctx, file)
	return &filenReadCloser{inner: reader, cancel: cancel}, nil
}

// ReadRange 按 [start, end] 闭区间读取解密后的文件内容流（Range 分段下载用）。
// SDK 按偏移逐块拉取解密，不会读取整个文件；
// 边缘函数流式中继据此把大文件拆成多段分别拉取，绕开云函数响应大小上限。
func (d *FilenDriver) ReadRange(key string, start, end int64) (io.ReadCloser, error) {
	if start < 0 {
		return nil, fmt.Errorf("无效的 Range：start 不能为负")
	}
	if end >= 0 && end < start {
		return nil, fmt.Errorf("无效的 Range：end 小于 start")
	}
	ctx, cancel := filenCtx(30 * time.Minute)
	client, err := d.ensureClient(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	file, err := d.findFilenFile(ctx, client, key)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("查询文件失败: %w", err)
	}
	if file == nil {
		cancel()
		return nil, fmt.Errorf("文件不存在")
	}
	if start >= file.Size {
		cancel()
		return nil, ErrRangeNotSatisfiable
	}
	// SDK 的 limit 为开区间末偏移：闭区间 [start, end] → limit=end+1；end<0 表示读到文件尾
	limit := int64(-1)
	if end >= 0 {
		limit = end + 1
	}
	reader := client.GetDownloadReaderWithOffset(ctx, file, start, limit)
	return &filenReadCloser{inner: reader, cancel: cancel}, nil
}

// filenReadCloser 关闭下载流时同时释放上下文。
type filenReadCloser struct {
	inner  io.ReadCloser
	cancel context.CancelFunc
}

func (r *filenReadCloser) Read(p []byte) (int, error) { return r.inner.Read(p) }
func (r *filenReadCloser) Close() error {
	err := r.inner.Close()
	r.cancel()
	return err
}

// InitMultipartUpload Filen 不支持 S3 式客户端分片直传。
func (d *FilenDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return "", fmt.Errorf("Filen 存储不支持客户端分片直传，请使用服务端上传")
}

// GenerateUploadPartURL 不支持。
func (d *FilenDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return "", fmt.Errorf("Filen 存储不支持客户端分片直传，请使用服务端上传")
}

// CompleteMultipartUpload 不支持。
func (d *FilenDriver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	return fmt.Errorf("Filen 存储不支持客户端分片直传，请使用服务端上传")
}

// AbortMultipartUpload 不支持。
func (d *FilenDriver) AbortMultipartUpload(key string, uploadID string) error {
	return fmt.Errorf("Filen 存储不支持客户端分片直传，请使用服务端上传")
}

// ListUploadedParts 不支持。
func (d *FilenDriver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	return nil, fmt.Errorf("Filen 存储不支持客户端分片直传，请使用服务端上传")
}

// SetBucketCORS Filen 无此概念。
func (d *FilenDriver) SetBucketCORS() error {
	return ErrBucketCORSNotSupported
}
