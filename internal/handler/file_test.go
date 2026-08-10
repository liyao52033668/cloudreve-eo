package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
)

type mockStorageDriver struct {
	deleted []string
}

func (m *mockStorageDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return "https://upload.example.com/" + key, nil
}

func (m *mockStorageDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	return "https://download.example.com/" + key, nil
}

func (m *mockStorageDriver) Delete(key string) error {
	m.deleted = append(m.deleted, key)
	return nil
}

func (m *mockStorageDriver) GetSize(key string) (int64, error) {
	return 0, nil
}

func (m *mockStorageDriver) Read(key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte("mock-content"))), nil
}

func (m *mockStorageDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return "mock-upload-id", nil
}

func (m *mockStorageDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return fmt.Sprintf("https://upload.example.com/%s?partNumber=%d", key, partNumber), nil
}

func (m *mockStorageDriver) CompleteMultipartUpload(key string, uploadID string, parts []storage.CompletedPart) error {
	return nil
}

func (m *mockStorageDriver) AbortMultipartUpload(key string, uploadID string) error {
	return nil
}

func (m *mockStorageDriver) ListUploadedParts(key string, uploadID string) ([]storage.CompletedPart, error) {
	return nil, nil
}

func (m *mockStorageDriver) SetBucketCORS() error {
	return nil
}

func (m *mockStorageDriver) UploadFile(key string, content []byte) error {
	return nil
}

func setupFileHandler(t *testing.T) (*FileHandler, *gin.Engine, *model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "handler_file.db"),
		},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	groupID, err := model.EnsureDefaultGroup()
	if err != nil {
		t.Fatalf("ensure group: %v", err)
	}

	user := &model.User{
		Username:     "handleruser",
		PasswordHash: "hash",
		StorageQuota: 1073741824,
		GroupID:      groupID,
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	mock := &mockStorageDriver{}
	mgr := storage.NewTestStoragePolicyManager("s3", mock)
	fs := service.NewFileService(mgr)
	h := NewFileHandler(fs)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", user.ID)
		c.Next()
	})
	r.GET("/api/files", h.List)
	r.POST("/api/files/mkdir", h.Mkdir)
	r.POST("/api/files/upload", h.Upload)
	r.POST("/api/files/upload/callback", h.UploadCallback)
	r.GET("/api/files/:id/download", h.Download)
	r.DELETE("/api/files/:id", h.Delete)
	r.PUT("/api/files/:id/rename", h.Rename)
	r.PUT("/api/files/:id/move", h.Move)

	return h, r, user
}

func TestFileHandler_List_Returns200(t *testing.T) {
	_, r, user := setupFileHandler(t)

	if err := model.DB.Create(&model.File{
		UserID: user.ID, ParentID: 0, Name: "a.txt", IsDir: false,
	}).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/files?parent_id=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	files, ok := resp["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("files = %v, want 1 item", resp["files"])
	}
}

func TestFileHandler_Mkdir_Returns201(t *testing.T) {
	_, r, _ := setupFileHandler(t)

	body := map[string]any{"parent_id": 0, "name": "docs"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/files/mkdir", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	file, ok := resp["file"].(map[string]any)
	if !ok {
		t.Fatalf("file missing: %v", resp)
	}
	if file["name"] != "docs" || file["is_dir"] != true {
		t.Errorf("file = %v", file)
	}
}

func TestFileHandler_Upload_Returns200(t *testing.T) {
	_, r, _ := setupFileHandler(t)

	body := map[string]any{
		"file_name":    "x.pdf",
		"content_type": "application/pdf",
		"parent_id":    0,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["upload_url"] == nil || resp["upload_url"] == "" {
		t.Errorf("missing upload_url: %v", resp)
	}
	if resp["storage_key"] == nil || resp["storage_key"] == "" {
		t.Errorf("missing storage_key: %v", resp)
	}
}

func TestFileHandler_UploadCallback_Returns201(t *testing.T) {
	_, r, _ := setupFileHandler(t)

	body := map[string]any{
		"file_name":   "x.pdf",
		"storage_key": "1/abc",
		"size":        100,
		"mime_type":   "application/pdf",
		"parent_id":   0,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload/callback", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestFileHandler_Download_Returns200(t *testing.T) {
	_, r, user := setupFileHandler(t)

	f := &model.File{
		UserID: user.ID, ParentID: 0, Name: "a.txt", IsDir: false,
		StorageKey: "1/k", StoragePolicy: "s3", Size: 1,
	}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%d/download", f.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["download_url"] == nil || resp["download_url"] == "" {
		t.Errorf("missing download_url: %v", resp)
	}
}

func TestFileHandler_Download_DirReturns400(t *testing.T) {
	_, r, user := setupFileHandler(t)

	f := &model.File{UserID: user.ID, ParentID: 0, Name: "d", IsDir: true}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%d/download", f.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestFileHandler_Delete_Returns200(t *testing.T) {
	_, r, user := setupFileHandler(t)

	f := &model.File{
		UserID: user.ID, ParentID: 0, Name: "a.txt", IsDir: false,
		StorageKey: "1/k", StoragePolicy: "s3", Size: 1,
	}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/files/%d", f.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestFileHandler_Rename_Returns200(t *testing.T) {
	_, r, user := setupFileHandler(t)

	f := &model.File{UserID: user.ID, ParentID: 0, Name: "old.txt", IsDir: false}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"name": "new.txt"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/files/%d/rename", f.ID), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestFileHandler_Move_Returns200(t *testing.T) {
	_, r, user := setupFileHandler(t)

	dir := &model.File{UserID: user.ID, ParentID: 0, Name: "target", IsDir: true}
	if err := model.DB.Create(dir).Error; err != nil {
		t.Fatal(err)
	}
	f := &model.File{UserID: user.ID, ParentID: 0, Name: "m.txt", IsDir: false}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"parent_id": dir.ID}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/files/%d/move", f.ID), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestFileHandler_Move_TargetMissing_Returns400(t *testing.T) {
	_, r, user := setupFileHandler(t)

	f := &model.File{UserID: user.ID, ParentID: 0, Name: "m.txt", IsDir: false}
	if err := model.DB.Create(f).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"parent_id": 99999}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/files/%d/move", f.ID), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestFileHandler_BadRequest_Returns400(t *testing.T) {
	_, r, _ := setupFileHandler(t)

	// mkdir missing name
	req := httptest.NewRequest(http.MethodPost, "/api/files/mkdir", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("mkdir empty: status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// mockRangeIgnoreDriver 模拟百度 dlink：声明支持 RangeReader，但实际忽略 Range，
// 总是从文件头返回整文件流。用于验证 ProxyDownload 用 LimitReader 截断到声明段长，
// 避免超写 Content-Length 触发 Go 强制关闭连接（浏览器表现为"网络错误"）。
type mockRangeIgnoreDriver struct {
	mockStorageDriver
	size    int64
	content []byte
}

func (m *mockRangeIgnoreDriver) GetSize(key string) (int64, error) { return m.size, nil }
func (m *mockRangeIgnoreDriver) Read(key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.content)), nil
}
func (m *mockRangeIgnoreDriver) ReadRange(key string, start, end int64) (io.ReadCloser, error) {
	// 百度 dlink 对 Range 请求头可能返回 200 整文件（从文件头开始）
	return io.NopCloser(bytes.NewReader(m.content)), nil
}

func TestProxyDownload_IgnoresRange_TruncatesToSegment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	model.DB = nil
	t.Cleanup(func() { model.DB = nil })
	cfg := &config.Config{DB: config.DBConfig{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "proxy.db")}}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	if _, err := model.EnsureDefaultGroup(); err != nil {
		t.Fatalf("ensure group: %v", err)
	}

	// 8MB 文件，模拟上游忽略 Range 时 Go 端声明 206 + 段长、body 却是整文件
	size := int64(8 * 1024 * 1024)
	driver := &mockRangeIgnoreDriver{size: size, content: bytes.Repeat([]byte{0xAB}, int(size))}

	mgr := storage.NewTestStoragePolicyManager("baidu", driver)
	mgr.SetProxySigner(func() string { return "test-secret" }, "/api/files/proxy")
	fs := service.NewFileService(mgr)
	h := NewFileHandler(fs)

	r := gin.New()
	r.GET("/files/proxy", h.ProxyDownload)

	u, err := mgr.SignProxyURL("baidu", "123/a.bin", "a.bin", 10*time.Minute)
	if err != nil {
		t.Fatalf("SignProxyURL: %v", err)
	}
	// SignProxyURL 的 base 是 /api/files/proxy，单测路由挂在 /files/proxy
	reqURL := "/files/proxy" + strings.TrimPrefix(u, "/api/files/proxy")

	// 模拟边缘函数首段请求：bytes=0-1048575（SEG=1MB）
	req := httptest.NewRequest(http.MethodGet, reqURL, nil)
	req.Header.Set("Range", "bytes=0-1048575")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206; body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Length"); got != "1048576" {
		t.Errorf("Content-Length = %q, want 1048576", got)
	}
	if got := w.Body.Len(); got != 1048576 {
		t.Errorf("body bytes = %d, want 1048576（LimitReader 应截断整文件，防止超写断连）", got)
	}
	if cr := w.Header().Get("Content-Range"); cr != "bytes 0-1048575/8388608" {
		t.Errorf("Content-Range = %q, want bytes 0-1048575/8388608", cr)
	}
}
