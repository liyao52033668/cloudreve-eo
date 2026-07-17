package storage

import (
	"strings"
	"testing"
	"time"
)

func TestEdgeOneDriver_ImplementsStorageDriver(t *testing.T) {
	// 编译期 + 运行期接口满足检查
	var _ StorageDriver = (*EdgeOneDriver)(nil)

	driver, err := NewEdgeOneDriver("test-bucket", "secret-id", "secret-key")
	if err != nil {
		t.Fatalf("NewEdgeOneDriver: %v", err)
	}

	var iface StorageDriver = driver
	if iface == nil {
		t.Fatal("EdgeOneDriver should satisfy StorageDriver")
	}
}

func TestNewEdgeOneDriver_CreatesInstance(t *testing.T) {
	driver, err := NewEdgeOneDriver("my-bucket", "sid", "skey")
	if err != nil {
		t.Fatalf("NewEdgeOneDriver unexpected error: %v", err)
	}
	if driver == nil {
		t.Fatal("NewEdgeOneDriver returned nil")
	}
	if driver.s3 == nil {
		t.Fatal("internal S3Driver is nil")
	}
	if driver.s3.bucket != "my-bucket" {
		t.Errorf("s3.bucket = %q, want %q", driver.s3.bucket, "my-bucket")
	}
	if driver.s3.client == nil {
		t.Fatal("internal S3 client is nil")
	}
}

func TestEdgeOneDriver_GenerateUploadURL_Delegates(t *testing.T) {
	driver, err := NewEdgeOneDriver("eo-bucket", "sid", "skey")
	if err != nil {
		t.Fatalf("NewEdgeOneDriver: %v", err)
	}

	url, err := driver.GenerateUploadURL("user/1/file.txt", "text/plain", time.Hour)
	if err != nil {
		t.Fatalf("GenerateUploadURL error: %v", err)
	}
	if url == "" {
		t.Fatal("GenerateUploadURL returned empty URL")
	}

	// 应使用 EdgeOne endpoint 格式，并通过内部 S3Driver 预签名
	if !strings.Contains(url, "cos.eo-bucket.myqcloud.com") {
		t.Errorf("upload URL %q should use EdgeOne endpoint cos.eo-bucket.myqcloud.com", url)
	}
	if !strings.Contains(url, "eo-bucket") {
		t.Errorf("upload URL %q should contain bucket name", url)
	}
	if !strings.Contains(url, "user/1/file.txt") && !strings.Contains(url, "user%2F1%2Ffile.txt") {
		t.Errorf("upload URL %q should contain object key", url)
	}

	// 与直接调用内部 S3 的结果结构一致（都能成功生成 URL）
	directURL, err := driver.s3.GenerateUploadURL("user/1/file.txt", "text/plain", time.Hour)
	if err != nil {
		t.Fatalf("direct s3.GenerateUploadURL: %v", err)
	}
	if !strings.Contains(directURL, "cos.eo-bucket.myqcloud.com") {
		t.Errorf("direct s3 URL %q should share EdgeOne endpoint", directURL)
	}
}

func TestEdgeOneDriver_GenerateDownloadURL_Delegates(t *testing.T) {
	driver, err := NewEdgeOneDriver("eo-bucket", "sid", "skey")
	if err != nil {
		t.Fatalf("NewEdgeOneDriver: %v", err)
	}

	url, err := driver.GenerateDownloadURL("user/1/file.txt", 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateDownloadURL error: %v", err)
	}
	if url == "" {
		t.Fatal("GenerateDownloadURL returned empty URL")
	}
	if !strings.Contains(url, "cos.eo-bucket.myqcloud.com") {
		t.Errorf("download URL %q should use EdgeOne endpoint", url)
	}
	if !strings.Contains(url, "eo-bucket") {
		t.Errorf("download URL %q should contain bucket name", url)
	}
}

func TestEdgeOneDriver_Delete_Delegates(t *testing.T) {
	driver, err := NewEdgeOneDriver("eo-bucket", "sid", "skey")
	if err != nil {
		t.Fatalf("NewEdgeOneDriver: %v", err)
	}

	// 无真实 COS 服务时，Delete 应委托给 S3 并返回错误（而非 panic / nil）
	err = driver.Delete("user/1/missing.txt")
	if err == nil {
		t.Fatal("Delete expected error when backend unreachable, got nil")
	}
	if !strings.Contains(err.Error(), "删除对象失败") {
		t.Errorf("Delete error %q should come from S3Driver wrapper", err.Error())
	}
}

func TestEdgeOneDriver_GetSize_Delegates(t *testing.T) {
	driver, err := NewEdgeOneDriver("eo-bucket", "sid", "skey")
	if err != nil {
		t.Fatalf("NewEdgeOneDriver: %v", err)
	}

	// 无真实 COS 服务时，GetSize 应委托给 S3 并返回错误
	size, err := driver.GetSize("user/1/missing.txt")
	if err == nil {
		t.Fatal("GetSize expected error when backend unreachable, got nil")
	}
	if size != 0 {
		t.Errorf("GetSize = %d, want 0 on error", size)
	}
	if !strings.Contains(err.Error(), "获取对象大小失败") {
		t.Errorf("GetSize error %q should come from S3Driver wrapper", err.Error())
	}
}
