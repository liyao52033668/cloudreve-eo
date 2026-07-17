package storage

import (
	"testing"
	"time"
)

// mockDriver is a minimal StorageDriver used to verify the interface contract.
type mockDriver struct{}

func (m *mockDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return "upload://" + key + "?" + contentType, nil
}

func (m *mockDriver) GenerateDownloadURL(key string, expire time.Duration) (string, error) {
	return "download://" + key, nil
}

func (m *mockDriver) Delete(key string) error {
	return nil
}

func (m *mockDriver) GetSize(key string) (int64, error) {
	return 42, nil
}

func TestStorageDriver_InterfaceContract(t *testing.T) {
	// Compile-time and runtime check that the interface methods match expectations.
	var _ StorageDriver = (*mockDriver)(nil)

	d := StorageDriver(&mockDriver{})

	uploadURL, err := d.GenerateUploadURL("obj/key", "text/plain", time.Hour)
	if err != nil {
		t.Fatalf("GenerateUploadURL error: %v", err)
	}
	if uploadURL == "" {
		t.Fatal("GenerateUploadURL returned empty URL")
	}

	downloadURL, err := d.GenerateDownloadURL("obj/key", time.Minute)
	if err != nil {
		t.Fatalf("GenerateDownloadURL error: %v", err)
	}
	if downloadURL == "" {
		t.Fatal("GenerateDownloadURL returned empty URL")
	}

	if err := d.Delete("obj/key"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	size, err := d.GetSize("obj/key")
	if err != nil {
		t.Fatalf("GetSize error: %v", err)
	}
	if size != 42 {
		t.Errorf("GetSize = %d, want 42", size)
	}
}
