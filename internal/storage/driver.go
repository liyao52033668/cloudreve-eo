package storage

import (
	"errors"
	"io"
	"time"
)

// ErrBucketCORSNotSupported 表示存储服务不实现 PutBucketCors 管理 API
// （如 EdgeOne 对象存储等部分 S3 兼容服务），跨域规则需到控制台手动配置。
var ErrBucketCORSNotSupported = errors.New("当前存储服务不支持通过 S3 API 设置 CORS")

// CompletedPart 分片上传完成时各分片的编号与 ETag。
type CompletedPart struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
}

// StorageDriver 定义统一的对象存储驱动接口。
type StorageDriver interface {
	GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error)
	// GenerateDownloadURL fileName 非空时强制浏览器下载（Content-Disposition: attachment）；空则内联展示（预览）。
	GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error)
	Delete(key string) error
	GetSize(key string) (int64, error)
	// Read 打开对象并返回内容流，调用方负责关闭（文件夹打包下载用）。
	Read(key string) (io.ReadCloser, error)

	// 分片上传（S3 Multipart Upload）
	InitMultipartUpload(key string, contentType string) (uploadID string, err error)
	GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error)
	CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error
	AbortMultipartUpload(key string, uploadID string) error
	// ListUploadedParts 列出已成功上传的分片（断点续传用）。
	ListUploadedParts(key string, uploadID string) ([]CompletedPart, error)

	// SetBucketCORS 为存储桶写入允许浏览器直传的 CORS 规则。
	SetBucketCORS() error

	// UploadFile 服务端直接上传文件内容。不支持的驱动返回 ErrServerUploadNotSupported。
	UploadFile(key string, content []byte) error
}

// RangeReader 驱动可选实现：按字节区间读取对象内容（start/end 为闭区间）。
// 大文件代理下载据此支持 HTTP Range 分段（206）与断点续传，
// 把一次超长执行拆成多次短执行；未实现的驱动由调用方回退整文件读取。
type RangeReader interface {
	ReadRange(key string, start, end int64) (io.ReadCloser, error)
}

// ServerChunkedUploader 驱动可选实现：服务端中转的分块上传。
// 用于网关限制单次请求 body 的场景（如 EdgeOne 上限 6MB）：
// 客户端把文件切成 ≤5MB 的块逐块提交，服务端逐块转发给存储，
// 任何时刻函数内存中只持有一块，完整文件不整体经过网关。
type ServerChunkedUploader interface {
	// InitChunkedUpload 预创建上传。blockMD5s 为客户端计算的各块 MD5。
	// fastUpload=true 表示上传已完成（秒传/空文件等），无需再传任何块。
	// 部分存储（Dropbox）会话需首块数据才能创建，此时返回空 uploadID，
	// 由首块请求创建会话并经返回值传回真实会话 ID。
	InitChunkedUpload(key string, size int64, blockMD5s []string) (uploadID string, fastUpload bool, err error)
	// UploadChunk 上传单个块；返回后续块应继续使用的 uploadID
	//（Dropbox 首块返回真实 session ID；其余驱动原样返回）。
	// offset 为本块在完整文件中的字节偏移（Dropbox 会话上传需要）。
	UploadChunk(key string, uploadID string, partSeq int, offset int64, data []byte) (string, error)
	// CompleteChunkedUpload 合并完成上传。
	CompleteChunkedUpload(key string, uploadID string, size int64, blockMD5s []string) error
}

// ErrRangeNotSatisfiable Range 起始位置超出文件大小（handler 应返回 416）。
var ErrRangeNotSatisfiable = errors.New("Range 超出文件大小")
