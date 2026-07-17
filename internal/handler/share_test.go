package handler

import (
	"bytes"
	"encoding/json"
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

func setupShareHandler(t *testing.T) (*ShareHandler, *gin.Engine, *model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "handler_share.db"),
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
		Username:     "sharehandler",
		PasswordHash: "hash",
		StorageQuota: cfg.Storage.DefaultQuota,
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	mock := &mockStorageDriver{}
	mgr := storage.NewTestStoragePolicyManager("s3", mock)
	ss := service.NewShareService(mgr)
	h := NewShareHandler(ss)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", user.ID)
		c.Next()
	})
	r.POST("/api/shares", h.Create)
	r.GET("/api/shares/:code", h.Get)
	r.GET("/api/shares/:code/download", h.Download)

	return h, r, user
}

func TestShareHandler_Create_Returns201(t *testing.T) {
	_, r, user := setupShareHandler(t)

	file := &model.File{
		UserID: user.ID, ParentID: 0, Name: "a.txt", IsDir: false,
		StorageKey: "k/a", StoragePolicy: "s3",
	}
	if err := model.DB.Create(file).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"file_id": file.ID}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/shares", bytes.NewReader(b))
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
	share, ok := resp["share"].(map[string]any)
	if !ok {
		t.Fatalf("share missing: %v", resp)
	}
	if share["file_id"] != float64(file.ID) {
		t.Errorf("file_id = %v, want %d", share["file_id"], file.ID)
	}
	code, _ := share["code"].(string)
	if len(code) != 8 {
		t.Errorf("code = %q, want 8 chars", code)
	}
}

func TestShareHandler_Create_BadRequest_Returns400(t *testing.T) {
	_, r, _ := setupShareHandler(t)

	// missing required file_id
	body := map[string]any{"password": "x"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/shares", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestShareHandler_Create_BadExpireFormat_Returns400(t *testing.T) {
	_, r, user := setupShareHandler(t)

	file := &model.File{
		UserID: user.ID, ParentID: 0, Name: "b.txt", IsDir: false,
		StorageKey: "k/b", StoragePolicy: "s3",
	}
	if err := model.DB.Create(file).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"file_id": file.ID, "expire_at": "not-a-date"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/shares", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["error"] != "过期时间格式错误" {
		t.Errorf("error = %v, want 过期时间格式错误", resp["error"])
	}
}

func TestShareHandler_Get_Returns200(t *testing.T) {
	_, r, user := setupShareHandler(t)

	file := &model.File{
		UserID: user.ID, ParentID: 0, Name: "c.txt", IsDir: false,
		StorageKey: "k/c", StoragePolicy: "s3", Size: 10,
	}
	if err := model.DB.Create(file).Error; err != nil {
		t.Fatal(err)
	}
	share := &model.Share{UserID: user.ID, FileID: file.ID, Code: "AbCd1234"}
	if err := model.DB.Create(share).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/shares/AbCd1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["share"] == nil || resp["file"] == nil {
		t.Fatalf("missing share/file: %v", resp)
	}
	fileObj := resp["file"].(map[string]any)
	if fileObj["name"] != "c.txt" {
		t.Errorf("file.name = %v", fileObj["name"])
	}
}

func TestShareHandler_Get_NotFound_Returns400(t *testing.T) {
	_, r, _ := setupShareHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/shares/missing1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["error"] != "分享不存在" {
		t.Errorf("error = %v, want 分享不存在", resp["error"])
	}
}

func TestShareHandler_Download_Returns200(t *testing.T) {
	_, r, user := setupShareHandler(t)

	file := &model.File{
		UserID: user.ID, ParentID: 0, Name: "d.txt", IsDir: false,
		StorageKey: "k/d", StoragePolicy: "s3",
	}
	if err := model.DB.Create(file).Error; err != nil {
		t.Fatal(err)
	}
	share := &model.Share{UserID: user.ID, FileID: file.ID, Code: "DlCode01"}
	if err := model.DB.Create(share).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/shares/DlCode01/download", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	url, ok := resp["download_url"].(string)
	if !ok || url == "" {
		t.Fatalf("download_url missing: %v", resp)
	}
	if url != "https://download.example.com/k/d" {
		t.Errorf("download_url = %q", url)
	}
}

func TestShareHandler_Create_WithPasswordAndExpire(t *testing.T) {
	_, r, user := setupShareHandler(t)

	file := &model.File{
		UserID: user.ID, ParentID: 0, Name: "e.txt", IsDir: false,
		StorageKey: "k/e", StoragePolicy: "s3",
	}
	if err := model.DB.Create(file).Error; err != nil {
		t.Fatal(err)
	}

	expire := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339)
	body := map[string]any{
		"file_id":   file.ID,
		"password":  "p@ss",
		"expire_at": expire,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/shares", bytes.NewReader(b))
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
	share := resp["share"].(map[string]any)
	// password is json:"-" so should not appear
	if _, has := share["password"]; has {
		t.Error("password should not be serialized in JSON")
	}
	if share["expire_at"] == nil {
		t.Error("expected expire_at in response")
	}
}
