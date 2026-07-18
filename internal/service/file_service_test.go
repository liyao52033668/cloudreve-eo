package service

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
)

// mockStorageDriver 用于 FileService 单测，避免访问真实对象存储。
type mockStorageDriver struct {
	uploadURLs   map[string]string
	downloadURLs map[string]string
	deleted      []string
}

func newMockStorageDriver() *mockStorageDriver {
	return &mockStorageDriver{
		uploadURLs:   make(map[string]string),
		downloadURLs: make(map[string]string),
	}
}

func (m *mockStorageDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	url := "https://upload.example.com/" + key + "?ct=" + contentType
	m.uploadURLs[key] = url
	return url, nil
}

func (m *mockStorageDriver) GenerateDownloadURL(key string, expire time.Duration) (string, error) {
	url := "https://download.example.com/" + key
	m.downloadURLs[key] = url
	return url, nil
}

func (m *mockStorageDriver) Delete(key string) error {
	m.deleted = append(m.deleted, key)
	return nil
}

func (m *mockStorageDriver) GetSize(key string) (int64, error) {
	return 0, nil
}

func setupFileService(t *testing.T) (*FileService, *mockStorageDriver, *model.User) {
	t.Helper()
	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "file_service.db"),
		},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	user := &model.User{
		Username:     "fileuser",
		PasswordHash: "hash",
		StorageQuota: 1073741824,
		StorageUsed:  0,
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	mock := newMockStorageDriver()
	mgr := storage.NewTestStoragePolicyManager("s3", mock)
	return NewFileService(mgr), mock, user
}

func TestFileService_ListFiles(t *testing.T) {
	svc, _, user := setupFileService(t)

	dir := &model.File{UserID: user.ID, ParentID: 0, Name: "docs", IsDir: true}
	file := &model.File{UserID: user.ID, ParentID: 0, Name: "a.txt", IsDir: false, Size: 10}
	other := &model.File{UserID: user.ID, ParentID: 99, Name: "hidden", IsDir: false}
	if err := model.DB.Create(dir).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Create(file).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Create(other).Error; err != nil {
		t.Fatal(err)
	}

	files, err := svc.ListFiles(user.ID, 0)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	// folders first, then name ASC
	if !files[0].IsDir || files[0].Name != "docs" {
		t.Errorf("files[0] = %+v, want dir docs", files[0])
	}
	if files[1].IsDir || files[1].Name != "a.txt" {
		t.Errorf("files[1] = %+v, want file a.txt", files[1])
	}
}

func TestFileService_Mkdir(t *testing.T) {
	svc, _, user := setupFileService(t)

	dir, err := svc.Mkdir(user.ID, 0, "photos")
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if dir == nil || dir.ID == 0 {
		t.Fatal("expected created dir with ID")
	}
	if !dir.IsDir || dir.Name != "photos" || dir.UserID != user.ID || dir.ParentID != 0 {
		t.Errorf("dir = %+v", dir)
	}

	var got model.File
	if err := model.DB.First(&got, dir.ID).Error; err != nil {
		t.Fatalf("db: %v", err)
	}
	if got.Name != "photos" || !got.IsDir {
		t.Errorf("persisted = %+v", got)
	}
}

func TestFileService_GetUploadURL(t *testing.T) {
	svc, mock, user := setupFileService(t)

	url, key, policy, err := svc.GetUploadURL(user.ID, "report.pdf", "application/pdf", "")
	if err != nil {
		t.Fatalf("GetUploadURL: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty upload URL")
	}
	if key == "" {
		t.Error("expected non-empty storage key")
	}
	if policy != "s3" {
		t.Errorf("policy = %q, want s3", policy)
	}
	wantPrefix := fmt.Sprintf("%d/", user.ID)
	if !strings.HasPrefix(key, wantPrefix) {
		t.Errorf("key = %q, want prefix %q", key, wantPrefix)
	}
	if mock.uploadURLs[key] != url {
		t.Errorf("driver not used: mock=%v url=%s", mock.uploadURLs, url)
	}
}

func TestFileService_UploadCallback(t *testing.T) {
	svc, _, user := setupFileService(t)

	file, err := svc.UploadCallback(user.ID, 0, "doc.txt", "1/abc-key", 1024, "text/plain", "")
	if err != nil {
		t.Fatalf("UploadCallback: %v", err)
	}
	if file.ID == 0 || file.Name != "doc.txt" || file.Size != 1024 {
		t.Errorf("file = %+v", file)
	}
	if file.IsDir {
		t.Error("file should not be dir")
	}
	if file.StorageKey != "1/abc-key" || file.StoragePolicy != "s3" {
		t.Errorf("storage fields = key=%s policy=%s", file.StorageKey, file.StoragePolicy)
	}

	var updated model.User
	if err := model.DB.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("user: %v", err)
	}
	if updated.StorageUsed != 1024 {
		t.Errorf("StorageUsed = %d, want 1024", updated.StorageUsed)
	}
}

func TestFileService_GetDownloadURL_Success(t *testing.T) {
	svc, mock, user := setupFileService(t)

	f := &model.File{
		UserID: user.ID, ParentID: 0, Name: "a.txt", IsDir: false,
		Size: 10, StorageKey: "1/key-a", StoragePolicy: "s3",
	}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	url, err := svc.GetDownloadURL(user.ID, f.ID)
	if err != nil {
		t.Fatalf("GetDownloadURL: %v", err)
	}
	if url == "" {
		t.Error("expected download URL")
	}
	if mock.downloadURLs["1/key-a"] != url {
		t.Errorf("unexpected url %s mock=%v", url, mock.downloadURLs)
	}
}

func TestFileService_GetDownloadURL_NotFound(t *testing.T) {
	svc, _, user := setupFileService(t)

	_, err := svc.GetDownloadURL(user.ID, 99999)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if err.Error() != "文件不存在" {
		t.Errorf("error = %q, want 文件不存在", err.Error())
	}
}

func TestFileService_GetDownloadURL_Directory(t *testing.T) {
	svc, _, user := setupFileService(t)

	dir := &model.File{UserID: user.ID, ParentID: 0, Name: "folder", IsDir: true}
	if err := model.DB.Create(dir).Error; err != nil {
		t.Fatal(err)
	}

	_, err := svc.GetDownloadURL(user.ID, dir.ID)
	if err == nil {
		t.Fatal("expected error when downloading directory")
	}
	if err.Error() != "不能下载文件夹" {
		t.Errorf("error = %q, want 不能下载文件夹", err.Error())
	}
}

func TestFileService_Delete_File(t *testing.T) {
	svc, mock, user := setupFileService(t)

	if err := model.DB.Model(user).Update("storage_used", int64(500)).Error; err != nil {
		t.Fatal(err)
	}
	f := &model.File{
		UserID: user.ID, ParentID: 0, Name: "del.txt", IsDir: false,
		Size: 200, StorageKey: "1/del-key", StoragePolicy: "s3",
	}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(user.ID, f.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(mock.deleted) != 1 || mock.deleted[0] != "1/del-key" {
		t.Errorf("deleted keys = %v", mock.deleted)
	}

	var count int64
	model.DB.Model(&model.File{}).Where("id = ?", f.ID).Count(&count)
	if count != 0 {
		t.Error("file row should be deleted")
	}

	var updated model.User
	model.DB.First(&updated, user.ID)
	if updated.StorageUsed != 300 {
		t.Errorf("StorageUsed = %d, want 300", updated.StorageUsed)
	}
}

func TestFileService_Delete_NonEmptyDir(t *testing.T) {
	svc, _, user := setupFileService(t)

	dir := &model.File{UserID: user.ID, ParentID: 0, Name: "folder", IsDir: true}
	if err := model.DB.Create(dir).Error; err != nil {
		t.Fatal(err)
	}
	child := &model.File{UserID: user.ID, ParentID: dir.ID, Name: "c.txt", IsDir: false, Size: 1}
	if err := model.DB.Create(child).Error; err != nil {
		t.Fatal(err)
	}

	err := svc.Delete(user.ID, dir.ID)
	if err == nil {
		t.Fatal("expected error for non-empty directory")
	}
	if err.Error() != "文件夹不为空" {
		t.Errorf("error = %q, want 文件夹不为空", err.Error())
	}
}

func TestFileService_Rename(t *testing.T) {
	svc, _, user := setupFileService(t)

	f := &model.File{UserID: user.ID, ParentID: 0, Name: "old.txt", IsDir: false}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.Rename(user.ID, f.ID, "new.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	var got model.File
	model.DB.First(&got, f.ID)
	if got.Name != "new.txt" {
		t.Errorf("Name = %q, want new.txt", got.Name)
	}
}

func TestFileService_Move_Success(t *testing.T) {
	svc, _, user := setupFileService(t)

	dir := &model.File{UserID: user.ID, ParentID: 0, Name: "target", IsDir: true}
	if err := model.DB.Create(dir).Error; err != nil {
		t.Fatal(err)
	}
	f := &model.File{UserID: user.ID, ParentID: 0, Name: "m.txt", IsDir: false}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.Move(user.ID, f.ID, dir.ID); err != nil {
		t.Fatalf("Move: %v", err)
	}
	var got model.File
	model.DB.First(&got, f.ID)
	if got.ParentID != dir.ID {
		t.Errorf("ParentID = %d, want %d", got.ParentID, dir.ID)
	}
}

func TestFileService_Move_TargetNotFound(t *testing.T) {
	svc, _, user := setupFileService(t)

	f := &model.File{UserID: user.ID, ParentID: 0, Name: "m.txt", IsDir: false}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	err := svc.Move(user.ID, f.ID, 99999)
	if err == nil {
		t.Fatal("expected error when target dir missing")
	}
	if err.Error() != "目标文件夹不存在" {
		t.Errorf("error = %q, want 目标文件夹不存在", err.Error())
	}
}

func TestFileService_UploadCallback_PerPolicyQuota(t *testing.T) {
	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "quota.db"),
		},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	user := &model.User{Username: "quser", PasswordHash: "h", StorageQuota: 0}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	mockA := newMockStorageDriver()
	mockB := newMockStorageDriver()

	// A 配额 1000：已占用 800 后再传 300 应失败，传 200 应成功
	svcA := NewFileService(storage.NewTestStoragePolicyManagerWithQuota("a", mockA, 1000))
	if err := model.DB.Create(&model.File{
		UserID: user.ID, ParentID: 0, Name: "old.bin", IsDir: false,
		Size: 800, StorageKey: "k1", StoragePolicy: "a",
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := svcA.UploadCallback(user.ID, 0, "new.bin", "k2", 300, "application/octet-stream", "a")
	if err == nil || err.Error() != "存储配额不足" {
		t.Fatalf("expected 存储配额不足, got %v", err)
	}
	f, err := svcA.UploadCallback(user.ID, 0, "ok.bin", "k3", 200, "application/octet-stream", "a")
	if err != nil {
		t.Fatalf("UploadCallback 200: %v", err)
	}
	if f.Size != 200 || f.StoragePolicy != "a" {
		t.Errorf("file = %+v", f)
	}

	// B 配额独立且很大：不受 A 已用容量影响
	svcB := NewFileService(storage.NewTestStoragePolicyManagerWithQuota("b", mockB, 1<<40))
	fb, err := svcB.UploadCallback(user.ID, 0, "b.bin", "kb", 5000, "application/octet-stream", "b")
	if err != nil {
		t.Fatalf("policy b upload: %v", err)
	}
	if fb.StoragePolicy != "b" {
		t.Errorf("policy = %s", fb.StoragePolicy)
	}

	// A 的 used 不应计入 B：A 仍满（800+200）
	_, err = svcA.UploadCallback(user.ID, 0, "one.bin", "k4", 1, "application/octet-stream", "a")
	if err == nil || err.Error() != "存储配额不足" {
		t.Fatalf("expected a still full after b upload, got %v", err)
	}
}
