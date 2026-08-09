package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	dbx "github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
)

// Dropbox 单文件直传上限为 150MB；超过则走分片会话上传。
// 此处取较保守的阈值以减少单次大请求内存峰值。
const dropboxSessionThreshold = 10 << 20

// DropboxDriver 使用 Dropbox 官方（unofficial）Go SDK 实现存储驱动。
// 凭证为访问令牌（access token），由管理员在 Dropbox App Console 生成。
// 下载/预览使用 GetTemporaryLink 生成的临时链接（约 4 小时有效），无需服务端代理。
type DropboxDriver struct {
	token    string // access token
	basePath string // 存储路径前缀（相对 Dropbox 根目录）

	client files.Client
}

// NewDropboxDriver 创建 Dropbox 驱动。
// token=访问令牌；basePath=存储路径前缀（空则 Dropbox 根目录）。
func NewDropboxDriver(token, basePath string) (*DropboxDriver, error) {
	if token == "" {
		return nil, fmt.Errorf("Dropbox Access Token 不能为空")
	}
	return &DropboxDriver{
		token:    token,
		basePath: strings.Trim(basePath, "/"),
		client:   files.New(dbx.Config{Token: token}),
	}, nil
}

// IsConfigured 凭据是否齐备。
func (d *DropboxDriver) IsConfigured() bool {
	return d.token != ""
}

// dropboxPathOf 由对象键得到 Dropbox 内完整路径（以 / 开头）。
func (d *DropboxDriver) dropboxPathOf(key string) string {
	k := strings.TrimPrefix(key, "/")
	if d.basePath == "" {
		return "/" + k
	}
	return "/" + d.basePath + "/" + k
}

// UploadFile 上传文件；大文件自动走分片会话上传。
func (d *DropboxDriver) UploadFile(key string, content []byte) error {
	fullPath := d.dropboxPathOf(key)

	// 确保父目录存在（Dropbox 上传会自动创建，但显式创建保证目录层级）
	if dir := path.Dir(fullPath); dir != "/" && dir != "." {
		_, _ = d.client.CreateFolderV2(&files.CreateFolderArg{Path: dir})
	}

	if len(content) > dropboxSessionThreshold {
		if err := d.uploadSession(fullPath, content); err != nil {
			logx.Error(logx.ModuleStorage, "Dropbox 会话上传失败", logx.Err(err), "key", key)
			return err
		}
	} else {
		if err := d.uploadSingle(fullPath, content); err != nil {
			logx.Error(logx.ModuleStorage, "Dropbox 上传失败", logx.Err(err), "key", key)
			return err
		}
	}
	logx.Info(logx.ModuleStorage, "Dropbox 文件已上传", "key", key)
	return nil
}

// uploadSingle 单次 /files/upload。
func (d *DropboxDriver) uploadSingle(fullPath string, content []byte) error {
	commit := &files.CommitInfo{
		Path:       fullPath,
		Mode:       &files.WriteMode{Tagged: dbx.Tagged{Tag: files.WriteModeOverwrite}},
		Autorename: false,
	}
	if _, err := d.client.Upload(&files.UploadArg{CommitInfo: *commit}, bytes.NewReader(content)); err != nil {
		return err
	}
	return nil
}

// uploadSession 分片会话上传（start → append* → finish）。
func (d *DropboxDriver) uploadSession(fullPath string, content []byte) error {
	const chunk = dropboxSessionThreshold // 每片大小

	// 1. start
	startRes, err := d.client.UploadSessionStart(&files.UploadSessionStartArg{Close: false}, bytes.NewReader(content[:chunk]))
	if err != nil {
		return fmt.Errorf("upload session start: %w", err)
	}
	sessionID := startRes.SessionId
	offset := uint64(chunk)

	// 2. append 剩余分片
	for offset < uint64(len(content)) {
		end := offset + uint64(chunk)
		if end > uint64(len(content)) {
			end = uint64(len(content))
		}
		err := d.client.UploadSessionAppendV2(&files.UploadSessionAppendArg{
			Cursor: &files.UploadSessionCursor{SessionId: sessionID, Offset: offset},
			Close:  false,
		}, bytes.NewReader(content[offset:end]))
		if err != nil {
			return fmt.Errorf("upload session append: %w", err)
		}
		offset = end
	}

	// 3. finish（提交为空内容，数据已通过 append 上传完毕）
	commit := &files.CommitInfo{
		Path:       fullPath,
		Mode:       &files.WriteMode{Tagged: dbx.Tagged{Tag: files.WriteModeOverwrite}},
		Autorename: false,
	}
	if _, err := d.client.UploadSessionFinish(&files.UploadSessionFinishArg{
		Cursor: &files.UploadSessionCursor{SessionId: sessionID, Offset: offset},
		Commit: commit,
	}, bytes.NewReader(nil)); err != nil {
		return fmt.Errorf("upload session finish: %w", err)
	}
	return nil
}

// InitChunkedUpload Dropbox upload session 状态在 Dropbox 服务端，无需本地缓冲。
// 会话需首块数据才能创建，故此处返回空 uploadID，由首块请求创建会话并经响应返回。
// 空文件直接在 Init 时提交完成（fastUpload=true）。
func (d *DropboxDriver) InitChunkedUpload(key string, size int64, blockMD5s []string) (string, bool, error) {
	fullPath := d.dropboxPathOf(key)
	if dir := path.Dir(fullPath); dir != "/" && dir != "." {
		_, _ = d.client.CreateFolderV2(&files.CreateFolderArg{Path: dir})
	}
	if size == 0 {
		// 空文件直接提交完成
		if err := d.uploadSingle(fullPath, nil); err != nil {
			return "", false, fmt.Errorf("Dropbox 上传失败: %w", err)
		}
		return "", true, nil
	}
	return "", false, nil
}

// UploadChunk Dropbox upload session 分块上传。
// 首块（uploadID 为空）创建会话并返回真实 session ID；后续块按 offset 追加
//（offset 正确性由 Dropbox 服务端校验，错误会返回 offset_mismatch）。
func (d *DropboxDriver) UploadChunk(key string, uploadID string, partSeq int, offset int64, data []byte) (string, error) {
	// Dropbox append/finish 只需 cursor（session ID + offset），无需 path。
	if uploadID == "" {
		// 首块：创建会话
		startRes, err := d.client.UploadSessionStart(&files.UploadSessionStartArg{Close: false}, bytes.NewReader(data))
		if err != nil {
			return "", fmt.Errorf("创建上传会话失败: %w", err)
		}
		return startRes.SessionId, nil
	}
	// 后续块：追加（末块也用 append，finish 以空内容提交）
	err := d.client.UploadSessionAppendV2(&files.UploadSessionAppendArg{
		Cursor: &files.UploadSessionCursor{SessionId: uploadID, Offset: uint64(offset)},
		Close:  false,
	}, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("追加上传块失败: %w", err)
	}
	return uploadID, nil
}

// CompleteChunkedUpload 以 offset=size 提交完成 Dropbox upload session。
func (d *DropboxDriver) CompleteChunkedUpload(key string, uploadID string, size int64, blockMD5s []string) error {
	fullPath := d.dropboxPathOf(key)
	commit := &files.CommitInfo{
		Path:       fullPath,
		Mode:       &files.WriteMode{Tagged: dbx.Tagged{Tag: files.WriteModeOverwrite}},
		Autorename: false,
	}
	if _, err := d.client.UploadSessionFinish(&files.UploadSessionFinishArg{
		Cursor: &files.UploadSessionCursor{SessionId: uploadID, Offset: uint64(size)},
		Commit: commit,
	}, bytes.NewReader(nil)); err != nil {
		return fmt.Errorf("完成上传会话失败: %w", err)
	}
	logx.Info(logx.ModuleStorage, "Dropbox 分块上传完成", "key", key, "size", size)
	return nil
}

// GenerateUploadURL Dropbox 无预签名直传，走服务端上传。
func (d *DropboxDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return "", fmt.Errorf("Dropbox 存储不支持客户端直传，请使用服务端上传")
}

// GenerateDownloadURL 返回 Dropbox 临时下载链接。
// fileName 非空时追加 dl=1 强制下载；否则内联预览。
func (d *DropboxDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	if !d.IsConfigured() {
		return "", fmt.Errorf("Dropbox Access Token 缺失")
	}
	res, err := d.client.GetTemporaryLink(&files.GetTemporaryLinkArg{Path: d.dropboxPathOf(key)})
	if err != nil {
		return "", fmt.Errorf("生成 Dropbox 下载链接失败: %w", err)
	}
	link := res.Link
	if fileName != "" {
		sep := "?"
		if strings.Contains(link, "?") {
			sep = "&"
		}
		link += sep + "dl=1"
	}
	return link, nil
}

// Delete 删除文件；不存在视为成功。
func (d *DropboxDriver) Delete(key string) error {
	_, err := d.client.DeleteV2(&files.DeleteArg{Path: d.dropboxPathOf(key)})
	if err != nil {
		// not_found 视为已删除
		if isDropboxNotFound(err) {
			return nil
		}
		logx.Error(logx.ModuleStorage, "Dropbox 删除文件失败", logx.Err(err), "key", key)
		return err
	}
	logx.Info(logx.ModuleStorage, "Dropbox 文件已删除", "key", key)
	return nil
}

// isDropboxNotFound 判定删除/查询错误是否为路径不存在。
func isDropboxNotFound(err error) bool {
	var apiErr *files.DeleteV2APIError
	if errors.As(err, &apiErr) {
		if apiErr.EndpointError != nil && apiErr.EndpointError.PathLookup != nil {
			return apiErr.EndpointError.PathLookup.Tag == files.LookupErrorNotFound
		}
	}
	// 兜底：错误信息中包含 not_found
	return strings.Contains(err.Error(), "not_found") || strings.Contains(err.Error(), "path/not_found")
}

// GetSize 获取文件大小。
func (d *DropboxDriver) GetSize(key string) (int64, error) {
	meta, err := d.client.GetMetadata(&files.GetMetadataArg{Path: d.dropboxPathOf(key)})
	if err != nil {
		return 0, fmt.Errorf("获取 Dropbox 文件信息失败: %w", err)
	}
	if fm, ok := meta.(*files.FileMetadata); ok {
		return int64(fm.Size), nil
	}
	return 0, fmt.Errorf("对象不是文件")
}

// Read 返回文件内容流（打包下载用），调用方负责关闭。
func (d *DropboxDriver) Read(key string) (io.ReadCloser, error) {
	_, rc, err := d.client.Download(&files.DownloadArg{Path: d.dropboxPathOf(key)})
	if err != nil {
		return nil, fmt.Errorf("下载 Dropbox 文件失败: %w", err)
	}
	return rc, nil
}

// InitMultipartUpload Dropbox 不支持 S3 式客户端分片直传。
func (d *DropboxDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return "", fmt.Errorf("Dropbox 存储不支持客户端分片直传，请使用服务端上传")
}

// GenerateUploadPartURL 不支持。
func (d *DropboxDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return "", fmt.Errorf("Dropbox 存储不支持客户端分片直传，请使用服务端上传")
}

// CompleteMultipartUpload 不支持。
func (d *DropboxDriver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	return fmt.Errorf("Dropbox 存储不支持客户端分片直传，请使用服务端上传")
}

// AbortMultipartUpload 不支持。
func (d *DropboxDriver) AbortMultipartUpload(key string, uploadID string) error {
	return fmt.Errorf("Dropbox 存储不支持客户端分片直传，请使用服务端上传")
}

// ListUploadedParts 不支持。
func (d *DropboxDriver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	return nil, fmt.Errorf("Dropbox 存储不支持客户端分片直传，请使用服务端上传")
}

// SetBucketCORS Dropbox 无此概念。
func (d *DropboxDriver) SetBucketCORS() error {
	return ErrBucketCORSNotSupported
}
