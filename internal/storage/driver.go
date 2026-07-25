package storage

import "time"

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

	// 分片上传（S3 Multipart Upload）
	InitMultipartUpload(key string, contentType string) (uploadID string, err error)
	GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error)
	CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error
	AbortMultipartUpload(key string, uploadID string) error
	// ListUploadedParts 列出已成功上传的分片（断点续传用）。
	ListUploadedParts(key string, uploadID string) ([]CompletedPart, error)

	// SetBucketCORS 为存储桶写入允许浏览器直传的 CORS 规则。
	SetBucketCORS() error
}
