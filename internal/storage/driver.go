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
