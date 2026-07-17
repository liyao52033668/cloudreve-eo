package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
)

func setupShareService(t *testing.T) (*ShareService, *mockStorageDriver, *model.User) {
	t.Helper()
	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "share_service.db"),
		},
		Storage: config.StorageConfig{
			Default:      "s3",
			DefaultQuota: 1073741824,
		},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	user := &model.User{
		Username:     "shareuser",
		PasswordHash: "hash",
		StorageQuota: cfg.Storage.DefaultQuota,
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	mock := newMockStorageDriver()
	mgr := storage.NewTestStoragePolicyManager("s3", mock)
	return NewShareService(mgr), mock, user
}

func createTestFile(t *testing.T, userID uint, name string, isDir bool) *model.File {
	t.Helper()
	file := &model.File{
		UserID:        userID,
		ParentID:      0,
		Name:          name,
		IsDir:         isDir,
		Size:          100,
		StorageKey:    "keys/" + name,
		StoragePolicy: "s3",
	}
	if err := model.DB.Create(file).Error; err != nil {
		t.Fatalf("create file: %v", err)
	}
	return file
}

func TestShareService_Create_NoPasswordNoExpire(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "a.txt", false)

	share, err := svc.Create(user.ID, file.ID, "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if share == nil || share.ID == 0 {
		t.Fatal("expected share with ID")
	}
	if share.UserID != user.ID || share.FileID != file.ID {
		t.Errorf("share user/file = %d/%d, want %d/%d", share.UserID, share.FileID, user.ID, file.ID)
	}
	if len(share.Code) != 8 {
		t.Errorf("code length = %d, want 8; code=%q", len(share.Code), share.Code)
	}
	if share.Password != "" {
		t.Errorf("password = %q, want empty", share.Password)
	}
	if share.ExpireAt != nil {
		t.Errorf("expire_at = %v, want nil", share.ExpireAt)
	}
}

func TestShareService_Create_WithPasswordAndExpire(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "b.txt", false)
	expire := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)

	share, err := svc.Create(user.ID, file.ID, "pass123", &expire)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if share.Password != "pass123" {
		t.Errorf("password = %q, want pass123", share.Password)
	}
	if share.ExpireAt == nil || !share.ExpireAt.Equal(expire) {
		t.Errorf("expire_at = %v, want %v", share.ExpireAt, expire)
	}
}

func TestShareService_Create_FileNotFound(t *testing.T) {
	svc, _, user := setupShareService(t)

	_, err := svc.Create(user.ID, 99999, "", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if err.Error() != "文件不存在" {
		t.Errorf("error = %q, want 文件不存在", err.Error())
	}
}

func TestShareService_Create_FileNotOwned(t *testing.T) {
	svc, _, user := setupShareService(t)

	other := &model.User{Username: "other", PasswordHash: "h", StorageQuota: 1}
	if err := model.DB.Create(other).Error; err != nil {
		t.Fatal(err)
	}
	file := createTestFile(t, other.ID, "other.txt", false)

	_, err := svc.Create(user.ID, file.ID, "", nil)
	if err == nil {
		t.Fatal("expected error when file not owned")
	}
	if err.Error() != "文件不存在" {
		t.Errorf("error = %q, want 文件不存在", err.Error())
	}
}

func TestShareService_GetByCode_NoPassword(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "c.txt", false)
	share, err := svc.Create(user.ID, file.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	gotShare, gotFile, err := svc.GetByCode(share.Code, "")
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if gotShare.ID != share.ID {
		t.Errorf("share.ID = %d, want %d", gotShare.ID, share.ID)
	}
	if gotFile.ID != file.ID || gotFile.Name != "c.txt" {
		t.Errorf("file = %+v", gotFile)
	}
	if gotShare.Views != 0 {
		// Views on returned struct may be pre-increment; check DB
	}
	var persisted model.Share
	if err := model.DB.First(&persisted, share.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Views != 1 {
		t.Errorf("views = %d, want 1", persisted.Views)
	}
}

func TestShareService_GetByCode_WithCorrectPassword(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "d.txt", false)
	share, err := svc.Create(user.ID, file.ID, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}

	gotShare, gotFile, err := svc.GetByCode(share.Code, "secret")
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if gotShare.Code != share.Code || gotFile.ID != file.ID {
		t.Errorf("share/file mismatch: %+v %+v", gotShare, gotFile)
	}
}

func TestShareService_GetByCode_WrongPassword(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "e.txt", false)
	share, err := svc.Create(user.ID, file.ID, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.GetByCode(share.Code, "wrong")
	if err == nil {
		t.Fatal("expected password error")
	}
	if err.Error() != "提取码错误" {
		t.Errorf("error = %q, want 提取码错误", err.Error())
	}
}

func TestShareService_GetByCode_Expired(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "f.txt", false)
	past := time.Now().Add(-time.Hour)
	share, err := svc.Create(user.ID, file.ID, "", &past)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.GetByCode(share.Code, "")
	if err == nil {
		t.Fatal("expected expired error")
	}
	if err.Error() != "分享已过期" {
		t.Errorf("error = %q, want 分享已过期", err.Error())
	}
}

func TestShareService_GetByCode_NotFound(t *testing.T) {
	svc, _, _ := setupShareService(t)

	_, _, err := svc.GetByCode("notexist", "")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if err.Error() != "分享不存在" {
		t.Errorf("error = %q, want 分享不存在", err.Error())
	}
}

func TestShareService_GetDownloadURL_Success(t *testing.T) {
	svc, mock, user := setupShareService(t)
	file := createTestFile(t, user.ID, "g.txt", false)
	share, err := svc.Create(user.ID, file.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	url, err := svc.GetDownloadURL(share.Code, "")
	if err != nil {
		t.Fatalf("GetDownloadURL: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty download URL")
	}
	if mock.downloadURLs[file.StorageKey] == "" {
		t.Error("expected mock GenerateDownloadURL to be called")
	}
	if url != "https://download.example.com/"+file.StorageKey {
		t.Errorf("url = %q", url)
	}
}

func TestShareService_GetDownloadURL_Directory(t *testing.T) {
	svc, _, user := setupShareService(t)
	dir := createTestFile(t, user.ID, "folder", true)
	share, err := svc.Create(user.ID, dir.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.GetDownloadURL(share.Code, "")
	if err == nil {
		t.Fatal("expected error downloading directory")
	}
	if err.Error() != "不能下载文件夹" {
		t.Errorf("error = %q, want 不能下载文件夹", err.Error())
	}
}

func TestGenerateCode(t *testing.T) {
	code := generateCode()
	if len(code) != 8 {
		t.Fatalf("len = %d, want 8", len(code))
	}
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for _, c := range code {
		found := false
		for _, ok := range chars {
			if c == ok {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("invalid char %q in code %q", c, code)
		}
	}
}
