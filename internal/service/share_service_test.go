package service

import (
	"archive/zip"
	"bytes"
	"io"
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
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	user := &model.User{
		Username:     "shareuser",
		PasswordHash: "hash",
		StorageQuota: 1073741824,
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	mock := newMockStorageDriver()
	mgr := storage.NewTestStoragePolicyManager("s3", mock)
	return NewShareService(mgr), mock, user
}

func createTestFile(t *testing.T, userID int64, name string, isDir bool) *model.File {
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

	share, err := svc.Create(user.ID, []uint{file.ID}, "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if share == nil || share.ID == 0 {
		t.Fatal("expected share with ID")
	}
	if share.UserID != user.ID {
		t.Errorf("share user = %d, want %d", share.UserID, user.ID)
	}
	ids, err := RootFileIDs(share)
	if err != nil || len(ids) != 1 || ids[0] != file.ID {
		t.Errorf("RootFileIDs = %v, %v, want [%d]", ids, err, file.ID)
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

func TestShareService_Create_MultipleFiles(t *testing.T) {
	svc, _, user := setupShareService(t)
	a := createTestFile(t, user.ID, "a.txt", false)
	b := createTestFile(t, user.ID, "b.txt", false)

	share, err := svc.Create(user.ID, []uint{a.ID, b.ID}, "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ids, err := RootFileIDs(share)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != a.ID || ids[1] != b.ID {
		t.Errorf("RootFileIDs = %v, want [%d %d]", ids, a.ID, b.ID)
	}
}

func TestShareService_Create_EmptyIDs(t *testing.T) {
	svc, _, user := setupShareService(t)
	_, err := svc.Create(user.ID, nil, "", nil)
	if err == nil {
		t.Fatal("expected error for empty file IDs")
	}
}

func TestShareService_Create_WithPasswordAndExpire(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "b.txt", false)
	expire := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)

	share, err := svc.Create(user.ID, []uint{file.ID}, "pass123", &expire)
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

	_, err := svc.Create(user.ID, []uint{99999}, "", nil)
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

	_, err := svc.Create(user.ID, []uint{file.ID}, "", nil)
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
	share, err := svc.Create(user.ID, []uint{file.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	gotShare, files, err := svc.GetByCode(share.Code, "")
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if gotShare.ID != share.ID {
		t.Errorf("share.ID = %d, want %d", gotShare.ID, share.ID)
	}
	if len(files) != 1 || files[0].ID != file.ID || files[0].Name != "c.txt" {
		t.Errorf("files = %+v", files)
	}
	var persisted model.Share
	if err := model.DB.First(&persisted, share.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Views != 1 {
		t.Errorf("views = %d, want 1", persisted.Views)
	}
}

func TestShareService_GetByCode_MultipleFiles(t *testing.T) {
	svc, _, user := setupShareService(t)
	a := createTestFile(t, user.ID, "a.txt", false)
	b := createTestFile(t, user.ID, "b.txt", false)
	share, err := svc.Create(user.ID, []uint{a.ID, b.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, files, err := svc.GetByCode(share.Code, "")
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
}

func TestShareService_GetByCode_LegacySingleFileID(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "legacy.txt", false)
	// 旧数据：FileID 单字段，FileIDs 为空
	share := &model.Share{UserID: user.ID, FileID: file.ID, Code: generateCode()}
	if err := model.DB.Create(share).Error; err != nil {
		t.Fatal(err)
	}

	_, files, err := svc.GetByCode(share.Code, "")
	if err != nil {
		t.Fatalf("GetByCode legacy: %v", err)
	}
	if len(files) != 1 || files[0].ID != file.ID {
		t.Errorf("files = %+v, want legacy file %d", files, file.ID)
	}
}

func TestShareService_GetByCode_WithCorrectPassword(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "d.txt", false)
	share, err := svc.Create(user.ID, []uint{file.ID}, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}

	gotShare, files, err := svc.GetByCode(share.Code, "secret")
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if gotShare.Code != share.Code || len(files) != 1 || files[0].ID != file.ID {
		t.Errorf("share/file mismatch: %+v %+v", gotShare, files)
	}
}

func TestShareService_GetByCode_WrongPassword(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "e.txt", false)
	share, err := svc.Create(user.ID, []uint{file.ID}, "secret", nil)
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
	share, err := svc.Create(user.ID, []uint{file.ID}, "", &past)
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
	share, err := svc.Create(user.ID, []uint{file.ID}, "", nil)
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
	share, err := svc.Create(user.ID, []uint{dir.ID}, "", nil)
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

func TestShareService_GetDownloadURL_MultiFileShare(t *testing.T) {
	svc, _, user := setupShareService(t)
	a := createTestFile(t, user.ID, "a.txt", false)
	b := createTestFile(t, user.ID, "b.txt", false)
	share, err := svc.Create(user.ID, []uint{a.ID, b.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.GetDownloadURL(share.Code, "")
	if err == nil {
		t.Fatal("expected error for multi-file direct download")
	}
	if err.Error() != "多文件分享请使用打包下载" {
		t.Errorf("error = %q, want 多文件分享请使用打包下载", err.Error())
	}
}

func TestShareService_ListChildren_TopLevel(t *testing.T) {
	svc, _, user := setupShareService(t)
	a := createTestFile(t, user.ID, "a.txt", false)
	b := createTestFile(t, user.ID, "dir", true)
	share, err := svc.Create(user.ID, []uint{a.ID, b.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	files, err := svc.ListChildren(share.Code, "", 0)
	if err != nil {
		t.Fatalf("ListChildren top: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("top files = %d, want 2", len(files))
	}
}

func TestShareService_ListChildren_WithinShare(t *testing.T) {
	svc, _, user := setupShareService(t)
	dir := createTestFile(t, user.ID, "root", true)
	sub := &model.File{UserID: user.ID, ParentID: dir.ID, Name: "sub", IsDir: true}
	if err := model.DB.Create(sub).Error; err != nil {
		t.Fatal(err)
	}
	child := &model.File{UserID: user.ID, ParentID: dir.ID, Name: "a.txt", IsDir: false, Size: 10, StorageKey: "k/a", StoragePolicy: "s3"}
	if err := model.DB.Create(child).Error; err != nil {
		t.Fatal(err)
	}

	share, err := svc.Create(user.ID, []uint{dir.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	files, err := svc.ListChildren(share.Code, "", dir.ID)
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("children = %d, want 2", len(files))
	}

	// 进入子目录再浏览
	subFiles, err := svc.ListChildren(share.Code, "", sub.ID)
	if err != nil {
		t.Fatalf("ListChildren sub: %v", err)
	}
	if len(subFiles) != 0 {
		t.Errorf("sub children = %d, want 0", len(subFiles))
	}
}

func TestShareService_ListChildren_OutsideShare(t *testing.T) {
	svc, _, user := setupShareService(t)
	dir := createTestFile(t, user.ID, "root", true)
	outside := createTestFile(t, user.ID, "outside", true)

	share, err := svc.Create(user.ID, []uint{dir.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ListChildren(share.Code, "", outside.ID)
	if err == nil {
		t.Fatal("expected error when browsing outside share root")
	}
	if err.Error() != "目录不在分享范围内" {
		t.Errorf("error = %q, want 目录不在分享范围内", err.Error())
	}
}

func TestShareService_ListChildren_FileAsParent(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "a.txt", false)
	share, err := svc.Create(user.ID, []uint{file.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ListChildren(share.Code, "", file.ID)
	if err == nil {
		t.Fatal("expected error for non-folder parent")
	}
	if err.Error() != "该分享不是文件夹" {
		t.Errorf("error = %q, want 该分享不是文件夹", err.Error())
	}
}

func TestShareService_DownloadDir_Zip(t *testing.T) {
	svc, _, user := setupShareService(t)
	dir := createTestFile(t, user.ID, "root", true)
	child := &model.File{UserID: user.ID, ParentID: dir.ID, Name: "a.txt", IsDir: false, Size: 100, StorageKey: "k/a", StoragePolicy: "s3"}
	if err := model.DB.Create(child).Error; err != nil {
		t.Fatal(err)
	}
	emptySub := &model.File{UserID: user.ID, ParentID: dir.ID, Name: "empty", IsDir: true}
	if err := model.DB.Create(emptySub).Error; err != nil {
		t.Fatal(err)
	}

	share, err := svc.Create(user.ID, []uint{dir.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	fileName, err := svc.DownloadDir(share.Code, "", &buf, nil)
	if err != nil {
		t.Fatalf("DownloadDir: %v", err)
	}
	if fileName != "root.zip" {
		t.Errorf("fileName = %q, want root.zip", fileName)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	got := map[string]string{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			got[f.Name] = "[dir]"
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(data)
	}

	if got["root/a.txt"] != "mock-content" {
		t.Errorf("root/a.txt = %q, want mock-content", got["root/a.txt"])
	}
	if _, ok := got["root/"]; !ok {
		t.Error("expected root/ dir entry")
	}
	if got["root/empty/"] != "[dir]" {
		t.Errorf("root/empty/ = %q, want dir entry", got["root/empty/"])
	}
}

func TestShareService_DownloadDir_MultiFileZip(t *testing.T) {
	svc, _, user := setupShareService(t)
	a := createTestFile(t, user.ID, "a.txt", false)
	b := createTestFile(t, user.ID, "b.txt", false)
	share, err := svc.Create(user.ID, []uint{a.ID, b.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	var headerFileName string
	fileName, err := svc.DownloadDir(share.Code, "", &buf, func(fn string) { headerFileName = fn })
	if err != nil {
		t.Fatalf("DownloadDir multi: %v", err)
	}
	if fileName != "批量下载.zip" || headerFileName != "批量下载.zip" {
		t.Errorf("fileName = %q / header %q, want 批量下载.zip", fileName, headerFileName)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["a.txt"] || !names["b.txt"] {
		t.Errorf("zip entries = %v, want a.txt & b.txt", names)
	}
}

func TestShareService_DownloadDir_FileShare(t *testing.T) {
	svc, _, user := setupShareService(t)
	file := createTestFile(t, user.ID, "a.txt", false)
	share, err := svc.Create(user.ID, []uint{file.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 单文件分享也支持打包下载
	var buf bytes.Buffer
	fileName, err := svc.DownloadDir(share.Code, "", &buf, nil)
	if err != nil {
		t.Fatalf("DownloadDir file share: %v", err)
	}
	if fileName != "a.txt.zip" {
		t.Errorf("fileName = %q, want a.txt.zip", fileName)
	}
}

func TestShareService_GetChildDownloadURL(t *testing.T) {
	svc, mock, user := setupShareService(t)
	dir := createTestFile(t, user.ID, "root", true)
	child := &model.File{UserID: user.ID, ParentID: dir.ID, Name: "a.txt", IsDir: false, Size: 100, StorageKey: "k/a", StoragePolicy: "s3"}
	if err := model.DB.Create(child).Error; err != nil {
		t.Fatal(err)
	}
	outside := createTestFile(t, user.ID, "outside.txt", false)

	share, err := svc.Create(user.ID, []uint{dir.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	url, err := svc.GetChildDownloadURL(share.Code, "", child.ID)
	if err != nil {
		t.Fatalf("GetChildDownloadURL: %v", err)
	}
	if url != "https://download.example.com/k/a" {
		t.Errorf("url = %q", url)
	}
	if mock.downloadURLs["k/a"] == "" {
		t.Error("expected mock GenerateDownloadURL called")
	}

	// 分享范围外的文件不可下载
	_, err = svc.GetChildDownloadURL(share.Code, "", outside.ID)
	if err == nil {
		t.Fatal("expected error for outside file")
	}

	// 子目录不可单文件下载
	_, err = svc.GetChildDownloadURL(share.Code, "", dir.ID)
	if err == nil {
		t.Fatal("expected error for dir")
	}
}

func TestShareService_GetChildDownloadURL_MultiFileRoot(t *testing.T) {
	svc, _, user := setupShareService(t)
	a := createTestFile(t, user.ID, "a.txt", false)
	b := createTestFile(t, user.ID, "b.txt", false)
	share, err := svc.Create(user.ID, []uint{a.ID, b.ID}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 多文件分享的根文件本身可直接下载
	url, err := svc.GetChildDownloadURL(share.Code, "", a.ID)
	if err != nil {
		t.Fatalf("GetChildDownloadURL root file: %v", err)
	}
	if url != "https://download.example.com/keys/a.txt" {
		t.Errorf("url = %q", url)
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
