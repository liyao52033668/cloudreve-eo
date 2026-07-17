package storage

import (
	"strings"
	"testing"
	"time"
)

func TestNewS3Driver_InitWithMockEndpoint(t *testing.T) {
	driver, err := NewS3Driver(
		"http://localhost:9000",
		"us-east-1",
		"test-bucket",
		"minioadmin",
		"minioadmin",
	)
	if err != nil {
		t.Fatalf("NewS3Driver unexpected error: %v", err)
	}
	if driver == nil {
		t.Fatal("NewS3Driver returned nil driver")
	}
	if driver.bucket != "test-bucket" {
		t.Errorf("bucket = %q, want %q", driver.bucket, "test-bucket")
	}
	if driver.client == nil {
		t.Fatal("S3 client is nil")
	}

	// Compile-time interface satisfaction
	var _ StorageDriver = driver
}

func TestS3Driver_GenerateUploadURL(t *testing.T) {
	driver, err := NewS3Driver(
		"http://localhost:9000",
		"us-east-1",
		"test-bucket",
		"minioadmin",
		"minioadmin",
	)
	if err != nil {
		t.Fatalf("NewS3Driver: %v", err)
	}

	url, err := driver.GenerateUploadURL("user/1/file.txt", "text/plain", time.Hour)
	if err != nil {
		t.Fatalf("GenerateUploadURL error: %v", err)
	}
	if url == "" {
		t.Fatal("GenerateUploadURL returned empty URL")
	}
	if !strings.Contains(url, "test-bucket") {
		t.Errorf("upload URL %q should contain bucket name", url)
	}
	if !strings.Contains(url, "user/1/file.txt") && !strings.Contains(url, "user%2F1%2Ffile.txt") {
		t.Errorf("upload URL %q should contain object key", url)
	}
}

func TestS3Driver_GenerateDownloadURL(t *testing.T) {
	driver, err := NewS3Driver(
		"http://localhost:9000",
		"us-east-1",
		"test-bucket",
		"minioadmin",
		"minioadmin",
	)
	if err != nil {
		t.Fatalf("NewS3Driver: %v", err)
	}

	url, err := driver.GenerateDownloadURL("user/1/file.txt", 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateDownloadURL error: %v", err)
	}
	if url == "" {
		t.Fatal("GenerateDownloadURL returned empty URL")
	}
	if !strings.Contains(url, "test-bucket") {
		t.Errorf("download URL %q should contain bucket name", url)
	}
}
