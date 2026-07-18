package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func (m *mockStorageDriver) GenerateDownloadURL(key string, expire time.Duration) (string, error) {
	return "https://download.example.com/" + key, nil
}

func (m *mockStorageDriver) Delete(key string) error {
	m.deleted = append(m.deleted, key)
	return nil
}

func (m *mockStorageDriver) GetSize(key string) (int64, error) {
	return 0, nil
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

	user := &model.User{
		Username:     "handleruser",
		PasswordHash: "hash",
		StorageQuota: 1073741824,
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
