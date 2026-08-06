package storage

import (
	"fmt"
	"io"
	"time"
)

// EdgeOneDriver 封装 EdgeOne 对象存储（S3 兼容协议）。
type EdgeOneDriver struct {
	s3 *S3Driver
}

// NewEdgeOneDriver 创建 EdgeOne 存储驱动。
// 内部复用 S3Driver，endpoint 固定为 COS 兼容地址格式。
func NewEdgeOneDriver(bucket, secretID, secretKey string) (*EdgeOneDriver, error) {
	// EdgeOne 对象存储兼容 S3 协议
	// endpoint 格式根据实际 EdgeOne 文档配置
	endpoint := fmt.Sprintf("https://cos.%s.myqcloud.com", bucket)
	s3Driver, err := NewS3Driver(endpoint, "", bucket, secretID, secretKey, true, "")
	if err != nil {
		return nil, fmt.Errorf("初始化 EdgeOne 驱动失败: %w", err)
	}
	return &EdgeOneDriver{s3: s3Driver}, nil
}

func (d *EdgeOneDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return d.s3.GenerateUploadURL(key, contentType, expire)
}

func (d *EdgeOneDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	return d.s3.GenerateDownloadURL(key, fileName, expire)
}

func (d *EdgeOneDriver) Delete(key string) error {
	return d.s3.Delete(key)
}

func (d *EdgeOneDriver) GetSize(key string) (int64, error) {
	return d.s3.GetSize(key)
}

func (d *EdgeOneDriver) Read(key string) (io.ReadCloser, error) {
	return d.s3.Read(key)
}

func (d *EdgeOneDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return d.s3.InitMultipartUpload(key, contentType)
}

func (d *EdgeOneDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return d.s3.GenerateUploadPartURL(key, uploadID, partNumber, expire)
}

func (d *EdgeOneDriver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	return d.s3.CompleteMultipartUpload(key, uploadID, parts)
}

func (d *EdgeOneDriver) AbortMultipartUpload(key string, uploadID string) error {
	return d.s3.AbortMultipartUpload(key, uploadID)
}

func (d *EdgeOneDriver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	return d.s3.ListUploadedParts(key, uploadID)
}

func (d *EdgeOneDriver) SetBucketCORS() error {
	return d.s3.SetBucketCORS()
}

func (d *EdgeOneDriver) UploadFile(key string, content []byte) error {
	return d.s3.UploadFile(key, content)
}

// 确保 EdgeOneDriver 实现 StorageDriver 接口
var _ StorageDriver = (*EdgeOneDriver)(nil)
