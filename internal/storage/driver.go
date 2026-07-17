package storage

import "time"

// StorageDriver 定义统一的对象存储驱动接口。
type StorageDriver interface {
	GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error)
	GenerateDownloadURL(key string, expire time.Duration) (string, error)
	Delete(key string) error
	GetSize(key string) (int64, error)
}
